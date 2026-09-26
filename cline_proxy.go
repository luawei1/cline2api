package main

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/http/httpproxy"
)

// ============================================================================
// Cline 出口代理池
// 国内直连 api.cline.bot / api.workos.com 会命中跨区限制；配置 http/https/socks5(h)
// 代理后，所有发往 Cline 上游的请求（对话、登录/令牌刷新、模型同步，以及复用全局
// transport 的自定义 Provider）经代理池轮询出去。
// 未配置时保持原行为：环境变量代理（HTTPS_PROXY / HTTP_PROXY）→ 直连。
// 拨号复用 zen_proxy.go 的 dialViaProxy；配置与冷却状态与 zen 代理池相互独立。
// ============================================================================

type clineProxyConfigData struct {
	Proxies       []string `json:"proxies"`
	ProxyStrategy string   `json:"proxyStrategy"` // round_robin / random / fill
}

var (
	clineProxyCfg   *clineProxyConfigData
	clineProxyCfgMu sync.Mutex
	clineProxyCount atomic.Uint64
)

func defaultClineProxyConfig() *clineProxyConfigData {
	return &clineProxyConfigData{Proxies: []string{}, ProxyStrategy: "round_robin"}
}

// getClineProxyConfig 惰性加载配置（避免依赖包初始化顺序）。
func getClineProxyConfig() *clineProxyConfigData {
	clineProxyCfgMu.Lock()
	defer clineProxyCfgMu.Unlock()
	if clineProxyCfg == nil {
		cfg := defaultClineProxyConfig()
		if data, err := os.ReadFile(resolveDataPath(".cline-proxy.json")); err == nil {
			if err := json.Unmarshal(data, cfg); err != nil {
				log.Printf("cline proxy config parse failed: %v", err)
			}
		}
		normalizeClineProxyConfig(cfg)
		clineProxyCfg = cfg
	}
	return clineProxyCfg
}

// clineProxyPersistErr 最近一次持久化错误（落盘失败时后台返回 500，避免“保存成功”假象）。
var (
	clineProxyPersistErr   error
	clineProxyPersistErrMu sync.Mutex
)

func setClineProxyPersistErr(err error) {
	clineProxyPersistErrMu.Lock()
	defer clineProxyPersistErrMu.Unlock()
	clineProxyPersistErr = err
}

func getClineProxyPersistErr() error {
	clineProxyPersistErrMu.Lock()
	defer clineProxyPersistErrMu.Unlock()
	return clineProxyPersistErr
}

// setClineProxyConfig 原子替换配置并持久化。传输层按请求读取配置，无需重建。
func setClineProxyConfig(c *clineProxyConfigData) {
	normalizeClineProxyConfig(c)
	clineProxyCfgMu.Lock()
	clineProxyCfg = c
	clineProxyCfgMu.Unlock()

	data, _ := json.MarshalIndent(c, "", "  ")
	if err := os.WriteFile(resolveDataPath(".cline-proxy.json"), data, 0600); err != nil {
		log.Printf("cline proxy config save failed: %v", err)
		setClineProxyPersistErr(err)
		return
	}
	setClineProxyPersistErr(nil)
}

func normalizeClineProxyConfig(c *clineProxyConfigData) {
	cleaned := make([]string, 0, len(c.Proxies))
	for _, p := range c.Proxies {
		if line := strings.TrimSpace(p); line != "" {
			cleaned = append(cleaned, line)
		}
	}
	c.Proxies = cleaned
	if c.ProxyStrategy != "random" && c.ProxyStrategy != "fill" {
		c.ProxyStrategy = "round_robin"
	}
}

// snapshotClineProxyConfig 返回调用时刻的配置快照（深拷贝代理列表）。
// 传输层钩子按「一次请求一次快照」读取，避免同一请求内 active→dial 两次 get
// 之间并发更新导致的判定不一致。
func snapshotClineProxyConfig() (proxies []string, strategy string) {
	cfg := getClineProxyConfig()
	clineProxyCfgMu.Lock()
	defer clineProxyCfgMu.Unlock()
	proxies = append([]string(nil), cfg.Proxies...)
	return proxies, cfg.ProxyStrategy
}

func clineProxiesActive() bool {
	cfg := getClineProxyConfig()
	clineProxyCfgMu.Lock()
	defer clineProxyCfgMu.Unlock()
	return len(cfg.Proxies) > 0
}

// pickClineProxy 按策略选代理；未配置返回 ""。
// ponytail: 无单代理故障冷却（zen 侧有），代理挂了会体现在账号冷却与日志里，
// 升级路径：仿 cooldownZenProxy 增加索引冷却并在上游拨号失败处标记。
func pickClineProxy() string {
	proxies, strategy := snapshotClineProxyConfig()
	return pickFromClineProxies(proxies, strategy)
}

func pickFromClineProxies(proxies []string, strategy string) string {
	n := len(proxies)
	if n == 0 {
		return ""
	}
	idx := int(clineProxyCount.Add(1)-1) % n
	switch strategy {
	case "random":
		idx = randIntn(n)
	case "fill":
		idx = 0
	}
	return proxies[idx]
}

// clineEnvProxy 环境变量代理回退（可注入接缝，测试用替身）。
//
// 不用 http.ProxyFromEnvironment：它在进程内首次调用时就把环境变量缓存住，
// 此后任何修改（t.Setenv / 运行期改环境）都不再生效——只要先发生过一次出站
// 请求，代理配置就被永久冻结在那一刻（实测导致 TestHTTPTransportUsesHTTPSProxy-
// FromEnvironment 顺序相关地失败）。x/net/http/httpproxy 与标准库同一实现，
// 但每次实时读取环境，语义一致、无进程级缓存。
var clineEnvProxy = func(req *http.Request) (*url.URL, error) {
	return httpproxy.FromEnvironment().ProxyFunc()(req.URL)
}

// clineOutboundProxy 作为全局 transport 的 Proxy 钩子：应用内代理池生效时禁用
// 环境变量代理（隧道在 DialContext 内建立，叠加会双重代理），否则维持原行为。
func clineOutboundProxy(req *http.Request) (*url.URL, error) {
	if clineProxiesActive() {
		return nil, nil
	}
	return clineEnvProxy(req)
}

// clineDialContext 全局 transport 的拨号钩子。应用内代理生效时 addr 一定是目标
// 地址（Proxy 钩子已返回 nil），回环/内网目标直连（自定义 Provider 可能指向本机
// 模型服务），其余经代理池隧道；未生效时直连（可能是环境变量代理地址或目标本身）。
func clineDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	if proxies, strategy := snapshotClineProxyConfig(); len(proxies) > 0 && !clineDirectDialHost(addr) {
		if p := pickFromClineProxies(proxies, strategy); p != "" {
			return dialViaProxy(ctx, p, network, addr)
		}
	}
	d := &net.Dialer{Timeout: 12 * time.Second, KeepAlive: 30 * time.Second}
	return d.DialContext(ctx, network, addr)
}

// clineDirectDialHost 回环 / 内网 / 链路本地目标不走代理（本机 Ollama、LAN 网关等）。
// 仅识别 IP 字面量与 localhost；域名解析到内网地址的场景不处理。
func clineDirectDialHost(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast())
}
