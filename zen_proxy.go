package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/http2"
	"golang.org/x/net/proxy"
)

// ============================================================================
// zen 出口代理池
// 配置 http/https/socks5(h) 代理后，所有发往 opencode.ai 的请求经代理池轮询出去；
// 命中限流时冷却当前出口（Retry-After 优先），冷却期内轮询自动跳过。
// HTTPS 统一走 uTLS Chrome_120 指纹 + HTTP/2，避免 Go 原生 TLS 指纹被 Cloudflare 风控。
// ============================================================================

var (
	zenHTTPClient  = &http.Client{Transport: buildZenTransport()}
	zenProxyCount  atomic.Uint64
	zenTransportMu sync.Mutex

	zenProxyCooldowns   = map[int]time.Time{} // 代理索引 → 冷却截止
	zenProxyCooldownsMu sync.Mutex
)

func getZenHTTPClient() *http.Client {
	zenTransportMu.Lock()
	defer zenTransportMu.Unlock()
	return zenHTTPClient
}

// rebuildZenTransport 代理或配置变化时重建 HTTP 客户端。
func rebuildZenTransport() {
	zenTransportMu.Lock()
	defer zenTransportMu.Unlock()
	zenHTTPClient = &http.Client{Transport: buildZenTransport()}
}

func buildZenTransport() *http.Transport {
	t := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	}
	t.DialContext = zenDialContext
	// https 走 uTLS Chrome 指纹；按 ALPN 协商结果在 HTTP/2 / HTTP/1.1 间分流
	t.RegisterProtocol("https", newZenHTTPSRouter(zenHTTP2Transport(), zenHTTP1Transport()))
	return t
}

// zenTLSHandshakeTimeout 限制 TLS 握手时长：GFW 式黑洞（TCP 通、TLS 静默丢弃）
// 会让无超时的握手永久挂起，请求既不失败也不返回（故障转移也无法激活）。
const zenTLSHandshakeTimeout = 15 * time.Second

// errZenALPNNotH2 表示 TLS 握手完成但 ALPN 没协商到 h2。opencode.ai 自 2026-11 起
// （直连与代理出口实测一致）对该路径只选 http/1.1；把 http/1.1 连接交给
// http2.Transport 会把响应当 h2 帧解析，报 "frame too large ... frame header
// looked like an HTTP/1.1 header"（模型同步/zen 对话全挂的根因）。
var errZenALPNNotH2 = errors.New("zen: ALPN did not negotiate h2")

// zenHTTPSRouter 把 https 请求先交给 HTTP/2 传输层；握手期发现 ALPN 是
// http/1.1 时切换到 HTTP/1.1 传输层（进程内只切换一次，之后不再浪费 h2 拨号）。
type zenHTTPSRouter struct {
	h2, h1      http.RoundTripper
	preferHTTP1 atomic.Bool
}

func newZenHTTPSRouter(h2, h1 http.RoundTripper) *zenHTTPSRouter {
	return &zenHTTPSRouter{h2: h2, h1: h1}
}

func (r *zenHTTPSRouter) RoundTrip(req *http.Request) (*http.Response, error) {
	if !r.preferHTTP1.Load() {
		resp, err := r.h2.RoundTrip(req)
		if err == nil {
			return resp, nil
		}
		if !isZenALPNNotH2(err) {
			return nil, err
		}
		r.preferHTTP1.Store(true)
		log.Printf("zen: upstream ALPN is http/1.1, falling back to HTTP/1.1 transport (%v)", err)
	}
	// ALPN 失败发生在拨号/握手阶段，请求体尚未被读取，同一 req 直接重发是安全的。
	return r.h1.RoundTrip(req)
}

// isZenALPNNotH2 识别「ALPN 未协商到 h2」错误；http2 包可能不保留 %w 链，
// 按哨兵文本兜底。
func isZenALPNNotH2(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, errZenALPNNotH2) || strings.Contains(err.Error(), errZenALPNNotH2.Error())
}

// zenDialTLSConn 经 zen 代理池拨号并完成 uTLS Chrome 指纹握手，按 nextProtos
// 提供 ALPN 候选；返回连接与协商结果（调用方按需校验协议）。
func zenDialTLSConn(ctx context.Context, network, addr string, nextProtos []string) (net.Conn, string, error) {
	raw, err := zenDialContext(ctx, network, addr)
	if err != nil {
		return nil, "", err
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		raw.Close()
		return nil, "", err
	}
	uconn := utls.UClient(raw, &utls.Config{
		ServerName: host,
		NextProtos: nextProtos,
	}, utls.HelloChrome_120)
	// 握手限时；握手成功后 cancel 不影响已建立的连接
	hsCtx, cancel := context.WithTimeout(ctx, zenTLSHandshakeTimeout)
	defer cancel()
	if err := uconn.HandshakeContext(hsCtx); err != nil {
		raw.Close()
		return nil, "", err
	}
	return uconn, uconn.ConnectionState().NegotiatedProtocol, nil
}

func zenHTTP2Transport() *http2.Transport {
	return &http2.Transport{
		DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
			conn, proto, err := zenDialTLSConn(ctx, network, addr, []string{"h2", "http/1.1"})
			if err != nil {
				return nil, err
			}
			// http2.Transport 假定连接已协商 h2；拿到 http/1.1 时返回哨兵错误，
			// 由 zenHTTPSRouter 切到 HTTP/1.1 传输层，避免协议错配解析崩溃。
			if proto != "h2" {
				conn.Close()
				return nil, fmt.Errorf("%w (negotiated %q)", errZenALPNNotH2, proto)
			}
			return conn, nil
		},
	}
}

// zenHTTP1Transport 是 ALPN 只协商到 http/1.1 时的回退传输层：同样走代理池 +
// uTLS 指纹，但 NextProtos 只提供 http/1.1，保证连接不会被协商成 h2
// （否则 HTTP/1.1 请求会打在 h2 连接上，复现反向的协议错配）。
func zenHTTP1Transport() *http.Transport {
	return &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			conn, _, err := zenDialTLSConn(ctx, network, addr, []string{"http/1.1"})
			return conn, err
		},
	}
}

func zenDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	p, _ := pickZenProxy()
	if p == "" {
		// 未配置 zen 代理时回退系统代理（HTTPS_PROXY / HTTP_PROXY），再直连
		if u, envErr := http.ProxyFromEnvironment(&http.Request{URL: &url.URL{Scheme: "https", Host: addr}}); envErr == nil && u != nil {
			return dialViaProxy(ctx, u.String(), network, addr)
		}
		d := &net.Dialer{Timeout: 12 * time.Second, KeepAlive: 30 * time.Second}
		return d.DialContext(ctx, network, addr)
	}
	return dialViaProxy(ctx, p, network, addr)
}

// pickZenProxy 按策略选代理，返回 (代理URL, 索引)；未配置返回 ("", -1)。
// 冷却中的代理线性探测跳过；轮询计数与日志索引保持一致。
func pickZenProxy() (string, int) {
	cfg := getZenConfig()
	n := len(cfg.Proxies)
	if n == 0 {
		return "", -1
	}
	idx := int(zenProxyCount.Add(1)-1) % n
	switch cfg.ProxyStrategy {
	case "random":
		idx = randIntn(n)
	case "fill":
		idx = 0
	}
	for i := 0; i < n; i++ {
		if zenProxyAvailable(idx) {
			break
		}
		idx = (idx + 1) % n
	}
	return cfg.Proxies[idx], idx
}

// lastZenProxyIdx 最近一次实际使用的代理索引（日志/冷却定位用）。
func lastZenProxyIdx() int {
	v := int64(zenProxyCount.Load())
	if v <= 0 {
		return -1
	}
	n := len(getZenConfig().Proxies)
	if n == 0 {
		n = 1
	}
	return int((v - 1) % int64(n))
}

func cooldownZenProxy(idx int, d time.Duration) {
	if idx < 0 {
		return
	}
	if d <= 0 {
		d = 10 * time.Minute
	}
	zenProxyCooldownsMu.Lock()
	zenProxyCooldowns[idx] = time.Now().Add(d)
	zenProxyCooldownsMu.Unlock()
	log.Printf("  zen proxy[%d] cooled down for %v", idx+1, d)
}

func zenProxyAvailable(idx int) bool {
	zenProxyCooldownsMu.Lock()
	defer zenProxyCooldownsMu.Unlock()
	until, ok := zenProxyCooldowns[idx]
	if !ok {
		return true
	}
	if time.Now().After(until) {
		delete(zenProxyCooldowns, idx)
		return true
	}
	return false
}

// zenProxyCooldownStatus 供管理后台展示：代理 URL → 冷却截止时刻。
func zenProxyCooldownStatus() map[string]string {
	cfg := getZenConfig()
	zenProxyCooldownsMu.Lock()
	defer zenProxyCooldownsMu.Unlock()
	out := map[string]string{}
	for idx, until := range zenProxyCooldowns {
		if idx >= 0 && idx < len(cfg.Proxies) && time.Now().Before(until) {
			out[cfg.Proxies[idx]] = until.Format("15:04:05")
		}
	}
	return out
}

func maskProxyURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return raw
	}
	u.User = url.User("***")
	return u.String()
}

// dialViaProxy 统一拨号入口：http/https 走 CONNECT 隧道，socks5(h) 走 SOCKS5 握手。
func dialViaProxy(ctx context.Context, raw, network, addr string) (net.Conn, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("bad proxy url: %w", err)
	}
	switch u.Scheme {
	case "http", "https":
		return dialHTTPProxy(ctx, u, network, addr)
	case "socks5", "socks5h":
		auth := &proxy.Auth{}
		if u.User != nil {
			auth.User = u.User.Username()
			auth.Password, _ = u.User.Password()
		}
		d, err := proxy.SOCKS5("tcp", u.Host, auth, proxy.Direct)
		if err != nil {
			return nil, err
		}
		type ctxDialer interface {
			DialContext(context.Context, string, string) (net.Conn, error)
		}
		if cd, ok := d.(ctxDialer); ok {
			return cd.DialContext(ctx, network, addr)
		}
		// 旧接口无 ctx：包装转换
		type result struct {
			c   net.Conn
			err error
		}
		ch := make(chan result, 1)
		go func() {
			c, err := d.Dial(network, addr)
			ch <- result{c, err}
		}()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case r := <-ch:
			return r.c, r.err
		}
	default:
		return nil, fmt.Errorf("unsupported proxy scheme %q", u.Scheme)
	}
}

// dialHTTPProxy 经 http(s) 代理建立 CONNECT 隧道。
func dialHTTPProxy(ctx context.Context, u *url.URL, network, addr string) (net.Conn, error) {
	d := &net.Dialer{Timeout: 12 * time.Second, KeepAlive: 30 * time.Second}
	rawConn, err := d.DialContext(ctx, "tcp", u.Host)
	if err != nil {
		return nil, err
	}
	if u.Scheme == "https" {
		tlsConn := tls.Client(rawConn, &tls.Config{MinVersion: tls.VersionTLS12, ServerName: u.Hostname()})
		hsCtx, hsCancel := context.WithTimeout(ctx, zenTLSHandshakeTimeout)
		defer hsCancel()
		if err := tlsConn.HandshakeContext(hsCtx); err != nil {
			rawConn.Close()
			return nil, err
		}
		rawConn = tlsConn
	}

	req := &http.Request{
		Method: http.MethodConnect,
		URL:    &url.URL{Opaque: addr},
		Host:   addr,
		Header: make(http.Header),
	}
	if u.User != nil {
		cred := base64.StdEncoding.EncodeToString([]byte(u.User.String()))
		req.Header.Set("Proxy-Authorization", "Basic "+cred)
	}
	if err := req.Write(rawConn); err != nil {
		rawConn.Close()
		return nil, err
	}

	br := bufio.NewReader(rawConn)
	resp, err := http.ReadResponse(br, req)
	if err != nil {
		rawConn.Close()
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		rawConn.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("proxy CONNECT %s: %s %s", u.Host, resp.Status, strings.TrimSpace(string(b)))
	}
	return rawConn, nil
}
