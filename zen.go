package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
)

// ============================================================================
// opencode Zen 免费模型支持
// 按请求中的 model 自动分流：zen 免费模型 → https://opencode.ai/zen/v1；
// zen 付费模型 → 400 拒绝；其余 → Cline 账号池。
// zen 模型与 Cline 远程模型同存于 pool.Models（Source="zen"），
// 同步采用全量替换：官方下架的模型自动移除，不会残留僵尸条目。
// ============================================================================

const zenAPIBase = "https://opencode.ai/zen/v1"

// 请求日志中的上游标记
const (
	upstreamCline    = "cline"
	upstreamOpenCode = "opencode"
	upstreamProvider = "provider"
)

const zenModelSyncInterval = 10 * time.Minute

// zenHeaderWatchdogTimeout 限制 zen 上游"发出请求 → 收到响应头"的最长等待。
const zenHeaderWatchdogTimeout = 60 * time.Second

// zenMaxRetryWait 单次重试的最大等待（上游 Retry-After 可达 13h，绝不能在请求里睡数小时）。
const zenMaxRetryWait = 60 * time.Second

// zenMaxProxyCooldown 出口代理冷却的上限（本地代理端口背后可切换节点，长冷却有害）。
const zenMaxProxyCooldown = 30 * time.Minute

// withCancelOnClose 包装响应 body：调用方关闭 body 时同步释放请求 ctx，
// 避免长生命周期流式响应泄漏 cancel 函数。
func withCancelOnClose(resp *http.Response, cancel context.CancelFunc) *http.Response {
	resp.Body = &cancelOnCloseBody{ReadCloser: resp.Body, cancel: cancel}
	return resp
}

type cancelOnCloseBody struct {
	io.ReadCloser
	cancel context.CancelFunc
	once   sync.Once
}

func (b *cancelOnCloseBody) Close() error {
	b.once.Do(b.cancel)
	return b.ReadCloser.Close()
}

// zenSeedModels 内置 zen 免费模型种子表（含别名），仅作为从未同步成功时的离线 fallback。
// 与 builtinModels（Cline 侧）同一模式：同步成功后以远程列表为准。
// 列表与 2026-09-22 线上 /models 实测的免费模型保持一致。
type zenSeedModel struct {
	ID      string
	Aliases []string
	Context int
	Output  int
}

// zenSeedModels 种子表三用途：离线 fallback、免费判定白名单、别名解析。
// Context 默认 1M（2026-09 主流模型普遍 1M 上下文；压缩阈值按此计算，
// 偏小会让压缩过早触发——曾导致 200K 默认值把 1M 模型压到几万 token）。
var zenSeedModels = []zenSeedModel{
	{ID: "deepseek-v4-flash-free", Aliases: []string{"deepseek-v4-flash", "deepseek-v4"}, Context: 1000000, Output: 128000},
	{ID: "mimo-v2.6-flash-free", Aliases: []string{"mimo-v2.6-flash", "mimo-v2.6", "mimo"}, Context: 1000000, Output: 32000},
	{ID: "mimo-v2.5-free", Aliases: []string{"mimo-v2.5"}, Context: 1000000, Output: 32000},
	{ID: "ling-3.0-flash-fin-free", Aliases: []string{"ling-3.0-flash", "ling"}, Context: 1000000, Output: 32768},
	{ID: "nemotron-3-ultra-free", Aliases: []string{"nemotron-3-ultra", "nemotron"}, Context: 1000000, Output: 128000},
	{ID: "nemotron-3.5-lightning-free", Aliases: []string{"nemotron-3.5-lightning"}, Context: 1000000, Output: 32768},
	{ID: "jev-1.13-free", Context: 1000000, Output: 32768},
	{ID: "muse-spark-1.3-contributor-free", Context: 1000000, Output: 32768},
	{ID: "muse-spark-1.2-contributor-free", Context: 1000000, Output: 32768},
	{ID: "big-pickle", Context: 1000000, Output: 32000},
}

// builtinZenModels 把种子表转成 Model 条目（离线 fallback 用，Source="seed"）。
func builtinZenModels() []Model {
	out := make([]Model, 0, len(zenSeedModels))
	for _, m := range zenSeedModels {
		out = append(out, Model{
			ID:       m.ID,
			Provider: "opencode",
			Cost:     "free",
			Status:   "active",
			Custom:   false,
			Source:   "seed",
			Context:  m.Context,
			Output:   m.Output,
		})
	}
	return out
}

// remoteZenEnabled：zen 官方 /models 同步成功过 → 以远程列表为准，
// 种子表中已下架的模型自动休眠（不再出现在列表和路由里）。与 remoteModelsEnabled 同一模式。
var (
	remoteZenEnabled   bool
	remoteZenEnabledMu sync.Mutex
)

func remoteZenActive() bool {
	remoteZenEnabledMu.Lock()
	defer remoteZenEnabledMu.Unlock()
	return remoteZenEnabled
}

// isZenSource 判断模型来源是否属于 opencode 体系（同步条目 "zen" / 内置种子 "seed"）。
func isZenSource(m Model) bool {
	return m.Source == "zen" || m.Source == "seed"
}

// currentZenModels 返回当前生效的 zen 模型（pool 中 zen 来源条目；
// 从未同步成功时回退到种子表）。
func currentZenModels() []Model {
	p := loadPool()
	poolMu.Lock()
	var zen []Model
	for _, m := range p.Models {
		if isZenSource(m) {
			zen = append(zen, m)
		}
	}
	poolMu.Unlock()
	if len(zen) > 0 || remoteZenActive() {
		return zen
	}
	return builtinZenModels()
}

// resolveZenInfo 解析模型名到当前生效的 zen 模型。支持别名与 "opencode/" 前缀。
// 别名优先于精确 ID 匹配之后、但优先级高于付费同名 ID（种子的 free 别名不会被覆盖）。
func resolveZenInfo(id string) (Model, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Model{}, false
	}
	models := currentZenModels()

	// 别名表：seed 模型的别名 → 正式 ID（用种子表数据补全上下文）
	contextOf := func(m Model) Model { return m }
	for _, sm := range zenSeedModels {
		for _, a := range sm.Aliases {
			if a == id {
				// 种子模型必须在当前生效列表里才算数（否则就是被下架了）
				for _, m := range models {
					if m.ID == sm.ID && m.Cost == "free" {
						return contextOf(m), true
					}
				}
				break
			}
		}
	}

	tryOne := func(name string) (Model, bool) {
		for _, m := range models {
			if m.ID == name {
				return m, true
			}
		}
		return Model{}, false
	}
	if m, ok := tryOne(id); ok {
		return m, true
	}
	if strings.HasPrefix(id, "opencode/") {
		if m, ok := tryOne(strings.TrimPrefix(id, "opencode/")); ok {
			return m, true
		}
	}
	return Model{}, false
}

// isZenFreeModel 判定 zen 模型是否免费（用于路由：非免费的 zen 模型直接拒绝）。
// 种子白名单兜底：官方免费模型即使同步条目漏标 -free 后缀也不会被误拒。
func isZenFreeModel(m Model) bool {
	if m.Cost == "free" || strings.HasSuffix(m.ID, "-free") {
		return true
	}
	for _, sm := range zenSeedModels {
		if sm.ID == m.ID {
			return true
		}
	}
	return false
}

// routeModel 三态路由："zen" / "reject" / "cline"。
// 故障转移开启且 zen 连续失败期间，免费模型请求临时改走 cline 账号池。
func routeModel(id string) string {
	m, ok := resolveZenInfo(id)
	if !ok {
		return "cline"
	}
	if !isZenFreeModel(m) {
		return "reject"
	}
	// 极端情况：同名 ID 同时是 cline 模型（自定义冲突）→ 让给 cline
	p := loadPool()
	poolMu.Lock()
	for _, pm := range p.Models {
		if !isZenSource(pm) && pm.ID == strings.TrimPrefix(strings.TrimSpace(id), "opencode/") {
			poolMu.Unlock()
			return "cline"
		}
	}
	poolMu.Unlock()

	cfg := getZenConfig()
	if cfg.Failover && zenFailedNow() {
		log.Printf("  failover: zen degraded, %q routed to cline pool", id)
		return "cline"
	}
	return "zen"
}

// ============ zen 配置 ============

type zenCompactConfig struct {
	Auto         bool   `json:"auto"`         // 超限自动摘要压缩，默认开启
	Buffer       int    `json:"buffer"`       // 预留输出缓冲 token，默认 20000
	KeepTokens   int    `json:"keepTokens"`   // 压缩后尾部保留 token 预算，默认 8000
	SummaryModel string `json:"summaryModel"` // 摘要生成模型，空=用请求模型本身
	MaxSummary   int    `json:"maxSummary"`   // 摘要最大输出 token，默认 4096
}

type zenConfigData struct {
	Enabled         bool     `json:"enabled"`
	Key             string   `json:"key"`
	BaseURL         string   `json:"baseURL"`
	Proxies         []string `json:"proxies"`
	ProxyStrategy   string   `json:"proxyStrategy"` // round_robin / random / fill
	MaxConcurrency  int      `json:"maxConcurrency"`
	Retries         int      `json:"retries"`
	Failover        bool     `json:"failover"`
	FailoverCount   int      `json:"failoverCount"`
	FailoverMinutes int      `json:"failoverMinutes"`
	// ZenHeaders 管理员自定义请求头：覆盖内置指纹头（User-Agent / x-opencode-*）。
	// 特殊值 "$session"/"$request"/"$project"/"$client" 注入每请求的动态身份。
	ZenHeaders map[string]string `json:"zenHeaders,omitempty"`
	Compaction zenCompactConfig  `json:"compaction"`
}

func defaultZenConfig() *zenConfigData {
	return &zenConfigData{
		Enabled:         true,
		Key:             "public",
		BaseURL:         zenAPIBase,
		ProxyStrategy:   "round_robin",
		MaxConcurrency:  8,
		Retries:         3,
		Failover:        true,
		FailoverCount:   3,
		FailoverMinutes: 5,
		Compaction: zenCompactConfig{
			Auto:       true,
			Buffer:     20000,
			KeepTokens: 8000,
			MaxSummary: 4096,
		},
	}
}

var (
	zenConfig   *zenConfigData
	zenConfigMu sync.Mutex
)

// getZenConfig 惰性加载配置（避免依赖包初始化顺序）。
func getZenConfig() *zenConfigData {
	zenConfigMu.Lock()
	defer zenConfigMu.Unlock()
	if zenConfig == nil {
		cfg := defaultZenConfig()
		if data, err := os.ReadFile(resolveDataPath(".cline-zen.json")); err == nil {
			if err := json.Unmarshal(data, cfg); err != nil {
				log.Printf("zen config parse failed: %v", err)
			}
		}
		if cfg.Key == "" {
			cfg.Key = "public"
		}
		if cfg.BaseURL == "" {
			cfg.BaseURL = zenAPIBase
		}
		if cfg.ProxyStrategy != "random" && cfg.ProxyStrategy != "fill" {
			cfg.ProxyStrategy = "round_robin"
		}
		zenConfig = cfg
	}
	return zenConfig
}

// setZenConfig 原子替换配置并持久化，重建信号量与 HTTP 传输层。
func setZenConfig(c *zenConfigData) {
	c.BaseURL = strings.TrimSpace(c.BaseURL)
	if c.BaseURL == "" {
		c.BaseURL = zenAPIBase
	}
	if c.Key == "" {
		c.Key = "public"
	}
	if c.ProxyStrategy != "round_robin" && c.ProxyStrategy != "random" && c.ProxyStrategy != "fill" {
		c.ProxyStrategy = "round_robin"
	}
	if c.MaxConcurrency <= 0 {
		c.MaxConcurrency = 8
	}
	if c.Retries < 0 {
		c.Retries = 3
	}
	zenConfigMu.Lock()
	zenConfig = c
	zenConfigMu.Unlock()

	data, _ := json.MarshalIndent(c, "", "  ")
	if err := os.WriteFile(resolveDataPath(".cline-zen.json"), data, 0600); err != nil {
		log.Printf("zen config save failed: %v", err)
	}
	rebuildZenTransport()
	rebuildZenSem()
}

// ============ 限流防御状态机 ============

var (
	zenSem       chan struct{} // 并发信号量（防上游瞬时超限）
	zenFailCount int           // 连续失败计数
	zenFailUntil time.Time     // 故障转移截止时间
	zenStateMu   sync.Mutex
)

func rebuildZenSem() {
	n := getZenConfig().MaxConcurrency
	if n <= 0 {
		n = 8
	}
	zenStateMu.Lock()
	zenSem = make(chan struct{}, n)
	zenStateMu.Unlock()
}

func markZenSuccess() {
	zenStateMu.Lock()
	zenFailCount = 0
	zenFailUntil = time.Time{}
	zenStateMu.Unlock()
}

func markZenFail() {
	cfg := getZenConfig()
	thr := cfg.FailoverCount
	if thr <= 0 {
		thr = 3
	}
	window := cfg.FailoverMinutes
	if window <= 0 {
		window = 5
	}
	zenStateMu.Lock()
	zenFailCount++
	if zenFailCount >= thr {
		zenFailUntil = time.Now().Add(time.Duration(window) * time.Minute)
		log.Printf("  zen failover armed: %d consecutive failures, routing to cline pool for %dm", zenFailCount, window)
	}
	zenStateMu.Unlock()
}

// zenFailedNow 当前是否处于故障转移窗口内。
func zenFailedNow() bool {
	zenStateMu.Lock()
	defer zenStateMu.Unlock()
	if zenFailUntil.IsZero() {
		return false
	}
	if time.Now().After(zenFailUntil) {
		zenFailCount = 0
		zenFailUntil = time.Time{}
		return false
	}
	return true
}

// isRateLimited 限流信号识别：429/503 直接命中；502/403 按错误体关键词。
func isRateLimited(status int, body string) bool {
	if status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable {
		return true
	}
	if status == http.StatusBadGateway || status == http.StatusForbidden {
		low := strings.ToLower(body)
		for _, kw := range []string{"resourceexhausted", "limit reached", "rate limit", "too many", "overloaded", "busy"} {
			if strings.Contains(low, kw) {
				return true
			}
		}
	}
	return false
}

// parseRetryAfter 解析 Retry-After 响应头（秒数或 HTTP 日期）；解析失败返回 0。
func parseRetryAfter(h string) time.Duration {
	h = strings.TrimSpace(h)
	if h == "" {
		return 0
	}
	var secs int
	if _, err := fmt.Sscanf(h, "%d", &secs); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(h); err == nil {
		d := time.Until(t)
		if d > 0 {
			return d
		}
	}
	return 0
}

// validateProxyList 校验代理列表格式：支持 http/https/socks5/socks5h，必须含 host:port。
func validateProxyList(proxies []string) error {
	for _, p := range proxies {
		line := strings.TrimSpace(p)
		if line == "" {
			continue
		}
		u, err := url.Parse(line)
		if err != nil {
			return fmt.Errorf("proxy %q invalid: %v", line, err)
		}
		switch u.Scheme {
		case "http", "https", "socks5", "socks5h":
		default:
			return fmt.Errorf("proxy %q: unsupported scheme (http/https/socks5/socks5h)", line)
		}
		if u.Host == "" {
			return fmt.Errorf("proxy %q: missing host:port", line)
		}
		if _, _, err := net.SplitHostPort(u.Host); err != nil {
			return fmt.Errorf("proxy %q: missing port", line)
		}
	}
	return nil
}

// ============ 客户端身份 ============
// 规范会话格式（ses_ + 12 位 hex 时间戳 + 14 位 base62）自 2026-09-16 起
// 被免费层强校验，其他形态一律 403 FreeTierError（参考 opencode2api 的实测结论）。
// 会话按对话内容稳定派生：同一会话复用同一 upstream session，保留 prompt-cache 亲和；
// request-id 每次随机，规避请求维度的限流记账。
// 格式对齐官方客户端（sst/opencode v1.18.x）request.ts：
//   User-Agent:          opencode/<version>
//   x-opencode-project:  "global"（官方无仓库场景的静态 project id）
//   x-opencode-session:  "ses_" + 26 位标识（6 位时间 hex + 14 位 base62）
//   x-opencode-request:  "msg" + 26 位标识（消息 id）
//   x-opencode-client:   "cli"

// zenClientVersion 跟随 opencode 最新发布版本（packages/opencode/package.json）。
const zenClientVersion = "1.18.31"

const zenBase62 = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// zenIdentifier 生成官方 identifier 风格的 26 位串：前 12 位为时间排序 hex，
// 后 14 位随机 base62。与官方 descending() 的可见格式一致。
func zenIdentifier() string {
	nano := uint64(time.Now().UnixNano())
	prefix := make([]byte, 12)
	for i := 0; i < 6; i++ {
		b := byte(nano >> (40 - 8*i))
		prefix[i*2] = hexDigits[b>>4]
		prefix[i*2+1] = hexDigits[b&0x0f]
	}
	random := make([]byte, 14)
	rand.Read(random)
	for i, b := range random {
		random[i] = zenBase62[int(b)%len(zenBase62)]
	}
	return string(prefix) + string(random)
}

const hexDigits = "0123456789abcdef"

func randHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func randIntn(n int) int {
	if n <= 0 {
		return 0
	}
	b := make([]byte, 4)
	rand.Read(b)
	v := int(b[0])<<24 | int(b[1])<<16 | int(b[2])<<8 | int(b[3])
	if v < 0 {
		v = -v
	}
	return v % n
}

// withRetryJitter 在退避时长上叠加 0~25% 抖动，错开并发重试。
func withRetryJitter(delay time.Duration) time.Duration {
	if delay <= 0 {
		return delay
	}
	return delay + time.Duration(float64(delay)*float64(randIntn(26))/100)
}

// canonicalZenSession 把种子确定性地哈希进规范会话形态。
func canonicalZenSession(seed []byte) string {
	sum := sha256.Sum256(seed)
	timePart := hex.EncodeToString(sum[:6])
	rest := make([]byte, 14)
	n := new(big.Int).SetBytes(sum[6:16])
	base := big.NewInt(62)
	remainder := new(big.Int)
	for i := 13; i >= 0; i-- {
		n.DivMod(n, base, remainder)
		rest[i] = zenBase62[remainder.Int64()]
	}
	return "ses_" + timePart + string(rest)
}

// zenConversationSeed 提取对话稳定种子：客户端会话信号优先，其次首条用户消息内容。
func zenConversationSeed(params map[string]any) string {
	if meta, ok := params["metadata"].(map[string]any); ok {
		if sid, _ := meta["session_id"].(string); sid != "" {
			return sid
		}
	}
	msgs, ok := params["messages"].([]any)
	if !ok {
		return ""
	}
	for _, m := range msgs {
		mm, ok := m.(map[string]any)
		if !ok || mm["role"] != "user" {
			continue
		}
		encoded, _ := json.Marshal(mm["content"])
		if len(encoded) > 0 && string(encoded) != "null" {
			return string(encoded)
		}
	}
	return ""
}

// zenSessionID 返回本次上游请求的会话 ID（规范形态，按对话稳定）。
func zenSessionID(params map[string]any) string {
	if signal := zenConversationSeed(params); signal != "" {
		return canonicalZenSession([]byte("ses\x00" + signal))
	}
	b := make([]byte, 16)
	rand.Read(b)
	return canonicalZenSession(b)
}

// zenUserAgent 真实客户端经 AI SDK 发出的 UA 形态（实测可通过免费层校验）。
func zenUserAgent() string {
	return fmt.Sprintf("opencode/%s (%s %s; %s)", zenClientVersion, runtime.GOOS, runtime.GOARCH, runtime.Version())
}

// ============ zen 上游调用 ============

// anonymousCoreTools 是匿名免费层期望的核心工具名（agent 形态校验，参考 opencode2api）。
var anonymousCoreTools = []string{"bash", "edit", "glob", "grep", "read"}

// ensureAnonymousTools 补齐缺失的核心工具定义，让请求读作 agent 会话。
// 客户端已声明的工具保持原样。
func ensureAnonymousTools(body map[string]any) {
	raw, exists := body["tools"]
	if !exists {
		body["tools"] = anonymousToolset(nil)
		return
	}
	items, ok := raw.([]any)
	if !ok {
		return
	}
	present := make(map[string]bool, len(items))
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		fn, ok := entry["function"].(map[string]any)
		if !ok {
			continue
		}
		if name, _ := fn["name"].(string); name != "" {
			present[name] = true
		}
	}
	missing := make([]string, 0, len(anonymousCoreTools))
	for _, name := range anonymousCoreTools {
		if !present[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return
	}
	body["tools"] = append(items, anonymousToolset(missing)...)
}

func anonymousToolset(names []string) []any {
	if names == nil {
		names = anonymousCoreTools
	}
	tools := make([]any, 0, len(names))
	for _, name := range names {
		tools = append(tools, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        name,
				"description": "Agent tool " + name,
				"parameters":  map[string]any{"type": "object", "properties": map[string]any{}},
			},
		})
	}
	return tools
}

// buildZenBody 构造 zen 请求体：只带 OpenAI 兼容字段，模型名改写为 zen 正式 ID。
// 匿名免费层只接受 agent 形态的流式请求：强制 stream:true + include_usage + 注入核心工具。
func buildZenBody(params map[string]any, stream bool, anonymous bool) map[string]any {
	body := map[string]any{}
	for _, key := range passThroughKeys {
		if val, ok := params[key]; ok {
			body[key] = val
		}
	}
	for _, key := range []string{"model", "max_tokens", "max_completion_tokens"} {
		if val, ok := params[key]; ok {
			body[key] = val
		}
	}
	// messages 需先清洗畸形 tool_calls 再透传
	if msgsRaw, ok := params["messages"]; ok {
		if msgsArr, ok := msgsRaw.([]any); ok {
			body["messages"] = sanitizeMessages(msgsArr)
		} else {
			body["messages"] = msgsRaw
		}
	}
	wireStream := stream
	if anonymous {
		wireStream = true
	}
	body["stream"] = wireStream
	// 免费层 agent 形态校验对带 key 的请求同样生效（2026-09-29 实测）：
	// 核心工具 bash/edit/glob/grep/read 缺一则 403 FreeTierError，这里统一补齐
	ensureAnonymousTools(body)
	if anonymous && wireStream {
		body["stream_options"] = map[string]any{"include_usage": true}
	}
	if model, ok := params["model"].(string); ok {
		if m, ok := resolveZenInfo(model); ok {
			body["model"] = m.ID
		} else if model != "" {
			body["model"] = strings.TrimPrefix(model, "opencode/")
		}
	}
	delete(body, "reasoning_effort")
	delete(body, "reasoningEffort")
	return body
}

// callZenAPI 调用 zen 上游：并发信号量 + 指数退避重试 + 代理冷却 + 故障转移计数。
// 匿名（public key）免费层自 2026-09 起只接受 agent 形态的流式请求：
// 上游强制 stream:true，下游要 JSON 时把 SSE 折叠回单个 chat.completion 响应。
// 返回的响应由调用方关闭。
func callZenAPI(params map[string]any, stream bool) (*http.Response, error) {
	cfg := getZenConfig()
	anonymous := cfg.Key == "public"
	// muse-spark 系走 OpenAI Responses 协议（/responses），其余走 chat/completions；
	// 两条路径统一在返回前转回 chat 协议，调用方无感
	useResponses := zenUpstreamNeedsResponses(params)
	protoName := "chat"
	var bodyJSON []byte
	var err error
	if useResponses {
		protoName = "responses"
		bodyJSON, err = json.Marshal(buildZenResponsesBody(params))
	} else {
		bodyJSON, err = json.Marshal(buildZenBody(params, stream, anonymous))
	}
	if err != nil {
		return nil, fmt.Errorf("marshal zen body: %w", err)
	}
	zenPath := "/chat/completions"
	if useResponses {
		zenPath = "/responses"
	}
	endpoint := strings.TrimRight(cfg.BaseURL, "/") + zenPath

	zenStateMu.Lock()
	sem := zenSem
	if sem == nil {
		rebuildZenSemLocked()
		sem = zenSem
	}
	zenStateMu.Unlock()
	sem <- struct{}{}
	defer func() { <-sem }()

	retries := cfg.Retries
	if retries <= 0 {
		retries = 3
	}
	delay := time.Second

	for attempt := 0; ; attempt++ {
		req, err := http.NewRequest("POST", endpoint, bytes.NewReader(bodyJSON))
		if err != nil {
			return nil, fmt.Errorf("create zen request: %w", err)
		}
		sess := zenSessionID(params)
		user := "req_" + randHex(8)
		ua := zenUserAgent()
		req.Header.Set("Authorization", "Bearer "+cfg.Key)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("User-Agent", ua)
		req.Header.Set("x-opencode-project", "global")
		req.Header.Set("x-opencode-session", sess)
		req.Header.Set("x-opencode-request", user)
		req.Header.Set("x-opencode-client", "cli")
		req.Header.Set("x-session-affinity", sess)
		req.Header.Set("X-Session-Id", sess)
		// 管理员自定义头：支持 $session/$request/$project/$client 动态占位符，其余原样覆盖
		for k, v := range cfg.ZenHeaders {
			switch v {
			case "$session":
				req.Header.Set(k, sess)
			case "$request":
				req.Header.Set(k, user)
			case "$project":
				req.Header.Set(k, "global")
			case "$client":
				req.Header.Set(k, "cli")
			default:
				req.Header.Set(k, v)
			}
		}
		if model, _ := params["model"].(string); model != "" {
			if m, ok := resolveZenInfo(model); ok {
				req.Header.Set("x-opencode-model", m.ID)
			}
		}
		log.Printf("  zen upstream: model=%v proto=%s stream=%v(下游=%v) msgs=%d via=%s attempt=%d session=%s",
			bodyParamsModel(params), protoName, anonymous, stream, getMsgCount(params), describeZenProxy(), attempt+1, truncate(sess, 30))

		// 响应头看门狗：黑洞场景（TCP 通、握手/响应头静默丢弃）请求会永久挂起，
		// 且 callZenAPI 不返回则 markZenFail 不触发、故障转移永远无法激活。
		// 60s 内未收到响应头则取消本次尝试（计为网络错误、走重试/故障转移）；
		// 响应头到达后立即停掉看门狗，流式传输时长不受限制。
		watchCtx, watchCancel := context.WithCancel(context.Background())
		watchdog := time.AfterFunc(zenHeaderWatchdogTimeout, watchCancel)
		req = req.WithContext(watchCtx)

		resp, err := getZenHTTPClient().Do(req)
		if err != nil {
			watchdog.Stop()
			watchCancel()
			// 网络错误：退避重试；重试耗尽计一次失败（网络类故障也参与故障转移，
			// 持续网络不可达时才会切 Cline 池）
			if attempt < retries {
				log.Printf("  zen network error (%v), retry %d/%d after %v", err, attempt+1, retries, delay)
				time.Sleep(withRetryJitter(delay))
				delay *= 2
				continue
			}
			markZenFail()
			msg := fmt.Errorf("zen request: %w", err)
			if strings.Contains(err.Error(), "timeout") || strings.Contains(err.Error(), "handshake") ||
				strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "unreachable") ||
				strings.Contains(err.Error(), "context canceled") {
				return nil, fmt.Errorf("%w（opencode.ai 网络不可达，可在管理页「上游服务 → opencode 出口代理」配置代理）", msg)
			}
			return nil, msg
		}
		watchdog.Stop()
		if resp.StatusCode == http.StatusOK {
			markZenSuccess()
			if useResponses {
				// Responses 上游统一流式：下游要 SSE 时转写成 chat chunk，要 JSON 时折叠
				if stream {
					return streamZenResponsesAsChat(resp, zenUpstreamModelID(params), watchCancel), nil
				}
				return collapseZenResponsesStream(resp, zenUpstreamModelID(params), watchCancel)
			}
			if anonymous && !stream {
				// 折叠路径内部会关闭 body（连带释放 watchCtx）
				return collapseZenStreamResponse(resp, bodyParamsModel(params))
			}
			return withCancelOnClose(resp, watchCancel), nil
		}

		watchCancel()
		bodyBytes := readAllLimited(resp.Body, 64<<10)
		resp.Body.Close()
		reason := fmt.Sprintf("zen API %d: %s", resp.StatusCode, truncate(string(bodyBytes), 500))

		// 上游明确报「模型不存在」时清理下架残留（同步标记 Delisted 保留的模型）
		if model := bodyParamsModel(params); model != "" && isModelGoneError(resp.StatusCode, string(bodyBytes)) {
			markModelGone(model)
		}

		// 上游 500/502/504 多为瞬时故障，退避重试（503 走限流分支）
		if resp.StatusCode == 500 || resp.StatusCode == 502 || resp.StatusCode == 504 {
			if attempt < retries {
				log.Printf("  zen server error (%d), retry %d/%d after %v", resp.StatusCode, attempt+1, retries, delay)
				time.Sleep(withRetryJitter(delay))
				delay *= 2
				continue
			}
			markZenFail()
			return nil, fmt.Errorf("%s", reason)
		}

		if isRateLimited(resp.StatusCode, string(bodyBytes)) {
			ra := parseRetryAfter(resp.Header.Get("Retry-After"))
			// 冷却当前出口代理（Retry-After 优先，默认 10 分钟，封顶 30 分钟）：
			// 单个本地代理端口背后可切换节点（出口 IP 变化），长冷却有害无益
			if idx := lastZenProxyIdx(); idx >= 0 {
				d := ra
				if d <= 0 {
					d = 10 * time.Minute
				}
				if d > zenMaxProxyCooldown {
					d = zenMaxProxyCooldown
				}
				cooldownZenProxy(idx, d)
			}
			// 短限流（≤60s）值得按 Retry-After 等待重试；
			// 长限流（实测可达 13h）重试无意义，立即失败触发故障转移
			if attempt < retries && ra > 0 && ra <= zenMaxRetryWait {
				log.Printf("  zen rate limited (%d), retry %d/%d after %v", resp.StatusCode, attempt+1, retries, ra)
				time.Sleep(withRetryJitter(ra))
				delay *= 2
				continue
			}
			if attempt < retries && ra <= 0 {
				log.Printf("  zen rate limited (%d), retry %d/%d after %v", resp.StatusCode, attempt+1, retries, delay)
				time.Sleep(withRetryJitter(delay))
				delay *= 2
				continue
			}
			markZenFail()
			if ra > 10*time.Minute {
				return nil, fmt.Errorf("%s（opencode 免费层出口 IP 限流 ~%s，切换代理节点后可立即重试）", reason, ra.Truncate(time.Minute))
			}
			return nil, fmt.Errorf("%s", reason)
		}

		markZenFail()
		return nil, fmt.Errorf("%s", reason)
	}
}

func bodyParamsModel(params map[string]any) string {
	m, _ := params["model"].(string)
	return m
}

// collapseZenStreamResponse 消费匿名免费层强制返回的 SSE 流，
// 折叠成等价的单个 chat.completion JSON 响应（下游要 JSON 时保持透明）。
func collapseZenStreamResponse(resp *http.Response, model string) (*http.Response, error) {
	defer resp.Body.Close()
	acc := &zenCollapseAcc{Model: model, Created: time.Now().Unix()}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(line[5:])
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var chunk struct {
			ID      string `json:"id"`
			Model   string `json:"model"`
			Created int64  `json:"created"`
			Choices []struct {
				FinishReason any `json:"finish_reason"`
				Delta        struct {
					Content          string `json:"content"`
					ReasoningContent any    `json:"reasoning_content"`
					ToolCalls        []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Name     string `json:"name"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
			Usage map[string]any `json:"usage"`
		}
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}
		if chunk.ID != "" {
			acc.ID = chunk.ID
		}
		if chunk.Model != "" {
			acc.Model = chunk.Model
		}
		if chunk.Created != 0 {
			acc.Created = chunk.Created
		}
		if len(chunk.Choices) > 0 {
			choice := chunk.Choices[0]
			acc.Content += choice.Delta.Content
			if rc, ok := choice.Delta.ReasoningContent.(string); ok {
				acc.Reasoning += rc
			}
			if choice.FinishReason != nil {
				switch v := choice.FinishReason.(type) {
				case string:
					acc.FinishReason = v
				}
			}
			for _, tc := range choice.Delta.ToolCalls {
				for len(acc.ToolCalls) <= tc.Index {
					acc.ToolCalls = append(acc.ToolCalls, zenCollapseTool{})
				}
				t := &acc.ToolCalls[tc.Index]
				if tc.ID != "" {
					t.ID = tc.ID
				}
				if tc.Function.Name != "" {
					t.Name += tc.Function.Name
				}
				t.Arguments += tc.Function.Arguments
			}
		}
		if chunk.Usage != nil {
			acc.Usage = mergeTokenUsage(acc.Usage, parseTokenUsage(chunk.Usage))
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("zen stream read: %w", err)
	}
	log.Printf("  zen collapse: stream -> json (content_len=%d tool_calls=%d)", len(acc.Content), len(acc.ToolCalls))
	return zenCollapseToChatResponse(acc, resp.Request)
}

// zenCollapseToChatResponse 把折叠累计器组装成等价的单个 chat.completion JSON 响应。
// 供 chat SSE 与 Responses 事件流两条折叠路径复用。
func zenCollapseToChatResponse(acc *zenCollapseAcc, req *http.Request) (*http.Response, error) {
	msg := map[string]any{"role": "assistant", "content": acc.Content}
	if acc.Reasoning != "" {
		msg["reasoning_content"] = acc.Reasoning
	}
	if len(acc.ToolCalls) > 0 {
		calls := make([]any, 0, len(acc.ToolCalls))
		for i, t := range acc.ToolCalls {
			if t.ID == "" {
				t.ID = fmt.Sprintf("call_%d", i)
			}
			calls = append(calls, map[string]any{
				"id":       t.ID,
				"type":     "function",
				"function": map[string]any{"name": t.Name, "arguments": t.Arguments},
			})
		}
		msg["tool_calls"] = calls
	}
	finish := acc.FinishReason
	if finish == "" {
		finish = "stop"
	}
	out := map[string]any{
		"id":      acc.ID,
		"object":  "chat.completion",
		"created": acc.Created,
		"model":   acc.Model,
		"choices": []any{map[string]any{
			"index":         0,
			"message":       msg,
			"finish_reason": finish,
		}},
		"usage": map[string]any{
			"prompt_tokens":     acc.Usage.Prompt,
			"completion_tokens": acc.Usage.Completion,
			"total_tokens":      acc.Usage.Total,
		},
	}
	data, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("collapse zen stream: %w", err)
	}
	return &http.Response{
		Status:        "200 OK",
		StatusCode:    http.StatusOK,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        http.Header{"Content-Type": []string{"application/json"}},
		Body:          io.NopCloser(bytes.NewReader(data)),
		ContentLength: int64(len(data)),
		Request:       req,
	}, nil
}

type zenCollapseTool struct {
	ID        string
	Name      string
	Arguments string
}

type zenCollapseAcc struct {
	ID           string
	Model        string
	Created      int64
	Content      string
	Reasoning    string
	FinishReason string
	ToolCalls    []zenCollapseTool
	Usage        tokenUsage
}

// readAllLimited 读取响应体，最多 limit 字节（防御异常大的错误页）。
func readAllLimited(r io.Reader, limit int64) []byte {
	data, _ := io.ReadAll(io.LimitReader(r, limit))
	return data
}

func rebuildZenSemLocked() {
	n := zenConfig.MaxConcurrency
	if n <= 0 {
		n = 8
	}
	zenSem = make(chan struct{}, n)
}

func describeZenProxy() string {
	cfg := getZenConfig()
	if len(cfg.Proxies) == 0 {
		return "direct"
	}
	idx := lastZenProxyIdx()
	if idx < 0 {
		idx = 0
	}
	idx %= len(cfg.Proxies)
	return fmt.Sprintf("proxy[%d]=%s", idx+1, truncate(maskProxyURL(cfg.Proxies[idx]), 60))
}

// ============ 模型同步 ============

// syncZenModels 拉取 zen 官方 /models 并全量替换 pool 中 Source=="zen" 条目：
// 计算新增/移除清单 —— 官方下架的模型自动从列表消失，不留僵尸条目。
// 自定义模型（Custom=true 或其他 Source）不受影响。
func syncZenModels() modelSyncResult {
	res := modelSyncResult{SyncedAt: time.Now().Format(time.RFC3339)}
	fail := func(err error) modelSyncResult {
		msg := err.Error()
		// 网络类错误给出代理配置提示（opencode.ai 被网络封锁时直连必然失败）
		if strings.Contains(msg, "timeout") || strings.Contains(msg, "handshake") ||
			strings.Contains(msg, "connection refused") || strings.Contains(msg, "unreachable") ||
			strings.Contains(msg, "connectex") {
			msg += "（opencode.ai 当前网络不可达，可在管理页「上游服务 → opencode 出口代理」配置代理后重试）"
		}
		log.Printf("zen models sync failed: %v", msg)
		res.Error = msg
		return res
	}

	cfg := getZenConfig()
	endpoint := strings.TrimRight(cfg.BaseURL, "/") + "/models"
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return fail(err)
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Key)
	req.Header.Set("User-Agent", "opencode/"+zenClientVersion)
	req.Header.Set("x-opencode-project", "global")
	req.Header.Set("x-opencode-session", "ses_"+zenIdentifier())
	req.Header.Set("x-opencode-client", "cli")
	// 与 chat 同路：zen 代理池 + uTLS 指纹传输层，25s 超时
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	req = req.WithContext(ctx)
	resp, err := getZenHTTPClient().Do(req)
	if err != nil {
		return fail(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fail(fmt.Errorf("models API returned status %d", resp.StatusCode))
	}

	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return fail(err)
	}

	// 组装远程 zen 模型（去重；计费按 -free 后缀或种子白名单判定）
	seedFree := make(map[string]bool, len(zenSeedModels))
	for _, sm := range zenSeedModels {
		seedFree[sm.ID] = true
	}
	seen := make(map[string]bool)
	var remote []Model
	for _, item := range payload.Data {
		id := item.ID
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		cost := "pass"
		if strings.HasSuffix(id, "-free") || seedFree[id] {
			cost = "free"
		}
		remote = append(remote, Model{
			ID:       id,
			Provider: "opencode",
			Cost:     cost,
			Status:   "active",
			Custom:   false,
			Source:   "zen",
		})
	}
	if len(remote) == 0 {
		return fail(fmt.Errorf("models API returned empty list"))
	}

	// 补全上下文信息：远程接口不带 context/output。
	// 用户在管理页锁定过的条目（MetaLocked）保留原值；
	// 其余按种子表刷新（种子值更新时旧条目自动跟进），新模型回退默认。
	p := loadPool()
	oldZen := make(map[string]Model, len(p.Models))
	for _, m := range p.Models {
		if m.Source == "zen" {
			oldZen[m.ID] = m
		}
	}
	fillMeta := func(m Model) Model {
		if om, ok := oldZen[m.ID]; ok && om.MetaLocked && om.Context > 0 {
			m.Context, m.Output = om.Context, om.Output
			m.MetaLocked = true
			return m
		}
		for _, sm := range zenSeedModels {
			if sm.ID == m.ID {
				m.Context, m.Output = sm.Context, sm.Output
				return m
			}
		}
		if m.Context == 0 {
			m.Context = 1000000
		}
		if m.Output == 0 {
			m.Output = 32768
		}
		return m
	}
	for i := range remote {
		remote[i] = fillMeta(remote[i])
	}

	poolMu.Lock()
	oldIDs := make(map[string]bool)
	var kept []Model
	for _, m := range p.Models {
		if m.Source == "zen" {
			oldIDs[m.ID] = true
			continue
		}
		kept = append(kept, m)
	}
	for _, m := range remote {
		if !oldIDs[m.ID] {
			res.Added = append(res.Added, m.ID)
		}
	}
	// 官方列表里消失的旧模型不删除：实测列表移除后模型往往仍可继续用
	// （如 z-ai/glm-5.3-flash），只打上 Delisted 标记（管理页显示「已下架」，
	// 支持手动移除）；重新出现时新条目天然无标记，标记自动清除。
	// res.Removed 只记录新下架的。
	for _, m := range p.Models {
		if m.Source == "zen" && !seen[m.ID] {
			if !m.Delisted {
				res.Removed = append(res.Removed, m.ID)
			}
			m.Delisted = true
			kept = append(kept, m)
		}
	}
	kept = append(kept, remote...)
	p.Models = kept
	res.Total = len(remote)
	res.Changed = len(res.Added) > 0 || len(res.Removed) > 0
	poolMu.Unlock()
	savePool()

	remoteZenEnabledMu.Lock()
	remoteZenEnabled = true
	remoteZenEnabledMu.Unlock()

	log.Printf("zen models sync: %d models, +%d added, -%d removed", res.Total, len(res.Added), len(res.Removed))
	return res
}

// startZenModelsRefresher 启动定时同步（10 分钟一次，不阻塞启动）。
func startZenModelsRefresher() {
	go func() {
		time.Sleep(2 * time.Second) // 错开启动高峰
		if cfg := getZenConfig(); cfg.Enabled {
			setLastZenModelSync(syncZenModels())
		}
		ticker := time.NewTicker(zenModelSyncInterval)
		defer ticker.Stop()
		for range ticker.C {
			if cfg := getZenConfig(); cfg.Enabled {
				setLastZenModelSync(syncZenModels())
			}
		}
	}()
}

// ============ 最近一次同步结果（管理后台展示） ============

var (
	lastZenSync    modelSyncResult
	lastZenSyncRan bool
	lastZenSyncMu  sync.Mutex
)

func setLastZenModelSync(res modelSyncResult) {
	lastZenSyncMu.Lock()
	lastZenSync = res
	lastZenSyncRan = true
	lastZenSyncMu.Unlock()
}

func lastZenModelSync() modelSyncResult {
	lastZenSyncMu.Lock()
	defer lastZenSyncMu.Unlock()
	if !lastZenSyncRan {
		return modelSyncResult{SyncedAt: ""}
	}
	return lastZenSync
}

// opencodeUsageToday 从请求日志聚合今日 opencode 上游用量（后台仪表盘卡片用）。
func opencodeUsageToday() map[string]any {
	requestLogsMu.Lock()
	defer requestLogsMu.Unlock()

	var requests int64
	var input, output, total int64
	today := time.Now().Format("2006-01-02")
	for _, e := range requestLogs {
		if e.Upstream != upstreamOpenCode || e.StartedAt.Format("2006-01-02") != today {
			continue
		}
		requests++
		input += e.InputTokens
		output += e.OutputTokens
		total += e.TotalTokens
	}
	return map[string]any{
		"requests":     requests,
		"inputTokens":  input,
		"outputTokens": output,
		"totalTokens":  total,
	}
}

// ============ Responses 协议上游（muse-spark 系） ============
// 对齐 opencode 官方端点表（opencode.ai/docs/zen）：GPT/Grok/Muse Spark 走
// OpenAI Responses 协议（POST /responses），Claude/Qwen → Anthropic，其余 →
// chat/completions。当前免费层只有 muse-spark 系命中 Responses；
// 参考实现：sub2api DefaultOpenCodeZenProtocolRules。
// 上游统一流式（Responses SSE），下游要 chat SSE 时逐事件转写，要 JSON 时折叠。

// zenResponsesModelPrefixes 需要 Responses 协议的 zen 模型前缀（gpt-* / grok-* 预留）。
var zenResponsesModelPrefixes = []string{"muse-spark-"}

func zenModelUsesResponses(modelID string) bool {
	for _, p := range zenResponsesModelPrefixes {
		if strings.HasPrefix(modelID, p) {
			return true
		}
	}
	return false
}

// zenUpstreamModelID 返回请求模型对应的 zen 正式 ID（未解析到时去掉 opencode/ 前缀）。
func zenUpstreamModelID(params map[string]any) string {
	m, _ := params["model"].(string)
	if m == "" {
		return m
	}
	if zm, ok := resolveZenInfo(m); ok {
		return zm.ID
	}
	return strings.TrimPrefix(m, "opencode/")
}

func zenUpstreamNeedsResponses(params map[string]any) bool {
	return zenModelUsesResponses(zenUpstreamModelID(params))
}

// buildZenResponsesBody 把下游 chat 参数构造成 Responses 请求体。
// 匿名免费层的 agent 形态要求（强制流式 + 核心工具）同样适用于该协议。
func buildZenResponsesBody(params map[string]any) map[string]any {
	body := map[string]any{"stream": true}
	body["model"] = zenUpstreamModelID(params)
	// max_tokens → max_output_tokens；Meta 系上游要求 >= 16，推理模型预算太小
	// 会全耗在 reasoning 上产出空内容，低于下限兜到默认值（同 buildUpstreamBody 约定）。
	// 客户端未指定时不带该字段，交由上游取模型默认。
	// chat 请求经 json.Unmarshal 是 float64，responses/anthropic 转换路径是 int，宽松读取。
	for _, k := range []string{"max_tokens", "max_completion_tokens"} {
		n := zenInt64(params[k])
		if n <= 0 {
			continue
		}
		if n < minUpstreamMaxTokens {
			log.Printf("  zen clamp max_output_tokens=%d -> %d (upstream requires >= %d)", n, defaultMaxTokens, minUpstreamMaxTokens)
			n = defaultMaxTokens
		}
		body["max_output_tokens"] = n
		break
	}
	if v, ok := params["temperature"]; ok {
		body["temperature"] = v
	}
	if v, ok := params["top_p"]; ok {
		body["top_p"] = v
	}

	// tools：先在 chat 嵌套形态上补齐核心工具（免费层 agent 形态校验对
	// 带 key 的请求同样生效），再转 Responses 扁平格式
	pseudo := map[string]any{}
	if raw, ok := params["tools"]; ok {
		pseudo["tools"] = raw
	}
	ensureAnonymousTools(pseudo)
	if rawTools, ok := pseudo["tools"].([]any); ok && len(rawTools) > 0 {
		flat := make([]any, 0, len(rawTools))
		for _, t := range rawTools {
			tm, ok := t.(map[string]any)
			if !ok {
				continue
			}
			fn, _ := tm["function"].(map[string]any)
			if fn == nil {
				continue
			}
			flat = append(flat, map[string]any{
				"type":        "function",
				"name":        fn["name"],
				"description": fn["description"],
				"parameters":  fn["parameters"],
			})
		}
		if len(flat) > 0 {
			body["tools"] = flat
		}
	}
	switch tc := params["tool_choice"].(type) {
	case string:
		body["tool_choice"] = tc
	case map[string]any:
		fn, _ := tc["function"].(map[string]any)
		if name, _ := fn["name"].(string); name != "" {
			body["tool_choice"] = map[string]any{"type": "function", "name": name}
		}
	}

	// messages → instructions（system）+ input（对话与工具往返）
	var instr []string
	input := make([]any, 0, 16)
	if msgs, ok := params["messages"].([]any); ok {
		for _, m := range sanitizeMessages(msgs) {
			mm, ok := m.(map[string]any)
			if !ok {
				continue
			}
			switch role, _ := mm["role"].(string); role {
			case "system", "developer":
				if s := stringifyResponsesContent(mm["content"]); s != "" {
					instr = append(instr, s)
				}
			case "tool":
				input = append(input, map[string]any{
					"type":    "function_call_output",
					"call_id": mm["tool_call_id"],
					"output":  stringifyResponsesContent(mm["content"]),
				})
			case "assistant":
				if tcs, ok := mm["tool_calls"].([]any); ok {
					for _, tc := range tcs {
						cm, ok := tc.(map[string]any)
						if !ok {
							continue
						}
						fn, _ := cm["function"].(map[string]any)
						if fn == nil {
							continue
						}
						args, _ := fn["arguments"].(string)
						if args == "" {
							if am, ok := fn["arguments"].(map[string]any); ok {
								if b, err := json.Marshal(am); err == nil {
									args = string(b)
								}
							}
						}
						input = append(input, map[string]any{
							"type":      "function_call",
							"call_id":   cm["id"],
							"name":      fn["name"],
							"arguments": args,
						})
					}
				}
				if s := stringifyResponsesContent(mm["content"]); s != "" {
					input = append(input, map[string]any{"type": "message", "role": "assistant", "content": s})
				}
			default: // user
				input = append(input, map[string]any{
					"type":    "message",
					"role":    "user",
					"content": zenChatContentToResponsesInput(mm["content"]),
				})
			}
		}
	}
	if len(instr) > 0 {
		body["instructions"] = strings.Join(instr, "\n\n")
	}
	body["input"] = input
	return body
}

// zenChatContentToResponsesInput 把 chat content 转成 Responses 输入内容：
// 字符串原样；部件数组映射 text → input_text、image_url → input_image。
func zenChatContentToResponsesInput(content any) any {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		parts := make([]any, 0, len(v))
		for _, p := range v {
			pm, ok := p.(map[string]any)
			if !ok {
				continue
			}
			switch pm["type"] {
			case "text":
				if t, _ := pm["text"].(string); t != "" {
					parts = append(parts, map[string]any{"type": "input_text", "text": t})
				}
			case "image_url":
				iu, _ := pm["image_url"].(map[string]any)
				if u, _ := iu["url"].(string); u != "" {
					parts = append(parts, map[string]any{"type": "input_image", "image_url": u})
				}
			}
		}
		if len(parts) > 0 {
			return parts
		}
	}
	return ""
}

// zenInt64 宽松地把 JSON 数值转成 int64。
func zenInt64(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	}
	return 0
}

// zenResponsesUsage 把 Responses usage（input_tokens/output_tokens）映射为 tokenUsage。
func zenResponsesUsage(raw any) tokenUsage {
	m, ok := raw.(map[string]any)
	if !ok {
		return tokenUsage{}
	}
	u := tokenUsage{Valid: true}
	u.Prompt = zenInt64(m["input_tokens"])
	u.Completion = zenInt64(m["output_tokens"])
	u.Total = zenInt64(m["total_tokens"])
	if u.Total == 0 {
		u.Total = u.Prompt + u.Completion
	}
	if d, ok := m["input_tokens_details"].(map[string]any); ok {
		u.Cached = zenInt64(d["cached_tokens"])
	}
	return u
}

// scanSSEData 逐条读取 SSE data: 载荷并回调（忽略 event:/注释行与 [DONE]）。
func scanSSEData(src io.Reader, handle func(obj map[string]any)) error {
	scanner := bufio.NewScanner(src)
	scanner.Buffer(make([]byte, 0, 64<<10), 8<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(line[5:])
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var obj map[string]any
		if json.Unmarshal([]byte(payload), &obj) != nil {
			continue
		}
		handle(obj)
	}
	return scanner.Err()
}

// zenResponsesSink 消费 Responses 事件流的统一出口：流式转写与折叠两种实现。
// itemID 唯一标识一个 function_call 输出项；final=true 的 args 为权威全量参数。
type zenResponsesSink interface {
	open(id, model string)
	content(text string)
	reasoning(text string)
	toolStart(itemID, callID, name string)
	toolArgs(itemID, args string, final bool)
	done(model, status string, usage tokenUsage)
}

// pumpZenResponsesEvents 解析 Responses SSE 事件并驱动 sink。
func pumpZenResponsesEvents(src io.Reader, sink zenResponsesSink) error {
	opened := false
	return scanSSEData(src, func(obj map[string]any) {
		evType, _ := obj["type"].(string)
		resp, _ := obj["response"].(map[string]any)
		switch evType {
		case "response.created":
			if !opened && resp != nil {
				opened = true
				id, _ := resp["id"].(string)
				model, _ := resp["model"].(string)
				sink.open(id, model)
			}
		case "response.output_text.delta":
			if d, _ := obj["delta"].(string); d != "" {
				sink.content(d)
			}
		case "response.reasoning_text.delta", "response.reasoning_summary_text.delta":
			if d, _ := obj["delta"].(string); d != "" {
				sink.reasoning(d)
			}
		case "response.output_item.added":
			item, _ := obj["item"].(map[string]any)
			if it, _ := item["type"].(string); it == "function_call" {
				itemID, _ := item["id"].(string)
				callID, _ := item["call_id"].(string)
				name, _ := item["name"].(string)
				sink.toolStart(itemID, callID, name)
			}
		case "response.function_call_arguments.delta":
			itemID, _ := obj["item_id"].(string)
			if d, _ := obj["delta"].(string); d != "" {
				sink.toolArgs(itemID, d, false)
			}
		case "response.function_call_arguments.done":
			itemID, _ := obj["item_id"].(string)
			args, _ := obj["arguments"].(string)
			sink.toolArgs(itemID, args, true)
		case "response.completed", "response.incomplete", "response.failed":
			model := ""
			var usage tokenUsage
			if resp != nil {
				model, _ = resp["model"].(string)
				usage = zenResponsesUsage(resp["usage"])
			}
			sink.done(model, strings.TrimPrefix(evType, "response."), usage)
		}
	})
}

// zenChatChunkSink 把 Responses 事件流实时转写成 chat.completion.chunk SSE。
type zenChatChunkSink struct {
	w        io.Writer
	id       string
	created  int64
	model    string
	toolIdx  map[string]int
	toolSeen map[string]bool
	toolN    int
	hasTools bool
	err      error
}

func (s *zenChatChunkSink) write(chunk map[string]any) {
	if s.err != nil {
		return
	}
	b, err := json.Marshal(chunk)
	if err != nil {
		s.err = err
		return
	}
	_, s.err = fmt.Fprintf(s.w, "data: %s\n\n", b)
}

func (s *zenChatChunkSink) chunk(delta map[string]any, finish any) {
	s.write(map[string]any{
		"id":      s.id,
		"object":  "chat.completion.chunk",
		"created": s.created,
		"model":   s.model,
		"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}},
	})
}

func (s *zenChatChunkSink) open(id, model string) {
	if id != "" {
		s.id = id
	}
	if model != "" {
		s.model = model
	}
	s.chunk(map[string]any{"role": "assistant", "content": ""}, nil)
}

func (s *zenChatChunkSink) content(text string) {
	s.chunk(map[string]any{"content": text}, nil)
}

func (s *zenChatChunkSink) reasoning(text string) {
	s.chunk(map[string]any{"reasoning_content": text}, nil)
}

func (s *zenChatChunkSink) toolStart(itemID, callID, name string) {
	idx := s.toolN
	s.toolN++
	if itemID == "" {
		itemID = fmt.Sprintf("tc_%d", idx)
	}
	s.toolIdx[itemID] = idx
	s.hasTools = true
	s.chunk(map[string]any{"tool_calls": []any{map[string]any{
		"index": idx, "id": callID, "type": "function",
		"function": map[string]any{"name": name, "arguments": ""},
	}}}, nil)
}

func (s *zenChatChunkSink) toolArgs(itemID, args string, final bool) {
	idx, ok := s.toolIdx[itemID]
	if !ok {
		// 没有配对 start 事件时无法构造合法 tool_call（空名会被客户端当畸形丢弃），放弃
		return
	}
	if final {
		if s.toolSeen[itemID] {
			return // 增量已发过完整参数，done 事件冗余
		}
	} else {
		s.toolSeen[itemID] = true
	}
	s.chunk(map[string]any{"tool_calls": []any{map[string]any{
		"index": idx, "function": map[string]any{"arguments": args},
	}}}, nil)
}

func (s *zenChatChunkSink) done(model, status string, usage tokenUsage) {
	if model != "" {
		s.model = model
	}
	if status == "failed" {
		log.Printf("  zen responses: upstream reported failure mid-stream")
	}
	finish := "stop"
	if status == "incomplete" {
		finish = "length"
	} else if s.hasTools && status == "completed" {
		finish = "tool_calls"
	}
	s.chunk(map[string]any{}, finish)
	if usage.Valid {
		s.write(map[string]any{
			"id":      s.id,
			"object":  "chat.completion.chunk",
			"created": s.created,
			"model":   s.model,
			"choices": []any{},
			"usage": map[string]any{
				"prompt_tokens":         usage.Prompt,
				"completion_tokens":     usage.Completion,
				"total_tokens":          usage.Total,
				"prompt_tokens_details": map[string]any{"cached_tokens": usage.Cached},
			},
		})
	}
	if s.err == nil {
		_, s.err = fmt.Fprint(s.w, "data: [DONE]\n\n")
	}
}

// zenChatAccSink 折叠实现：把 Responses 事件流累计成 chat.completion 结构。
type zenChatAccSink struct {
	acc     *zenCollapseAcc
	toolIdx map[string]int
}

func (s *zenChatAccSink) open(id, model string) {
	if id != "" {
		s.acc.ID = id
	}
	if model != "" {
		s.acc.Model = model
	}
}

func (s *zenChatAccSink) content(text string) {
	s.acc.Content += text
}

func (s *zenChatAccSink) reasoning(text string) {
	s.acc.Reasoning += text
}

func (s *zenChatAccSink) toolStart(itemID, callID, name string) {
	idx := len(s.acc.ToolCalls)
	s.acc.ToolCalls = append(s.acc.ToolCalls, zenCollapseTool{ID: callID, Name: name})
	if itemID == "" {
		itemID = fmt.Sprintf("tc_%d", idx)
	}
	s.toolIdx[itemID] = idx
}

func (s *zenChatAccSink) toolArgs(itemID, args string, final bool) {
	idx, ok := s.toolIdx[itemID]
	if !ok || idx >= len(s.acc.ToolCalls) {
		return
	}
	if final {
		// done 事件携带权威全量参数：非空即覆盖，防增量分片丢失导致参数截断
		if args != "" {
			s.acc.ToolCalls[idx].Arguments = args
		}
		return
	}
	s.acc.ToolCalls[idx].Arguments += args
}

func (s *zenChatAccSink) done(model, status string, usage tokenUsage) {
	if model != "" {
		s.acc.Model = model
	}
	switch status {
	case "incomplete":
		s.acc.FinishReason = "length"
	case "completed":
		if len(s.acc.ToolCalls) > 0 {
			s.acc.FinishReason = "tool_calls"
		} else {
			s.acc.FinishReason = "stop"
		}
	default:
		s.acc.FinishReason = "stop"
	}
	if usage.Valid {
		s.acc.Usage = usage
	}
}

// streamZenResponsesAsChat 把上游 Responses SSE 实时转写成 chat.completion.chunk SSE。
// 转写 goroutine 拥有上游 body 与 watchdog ctx 的生命周期：结束时一并释放；
// 下游断开会导致写管道失败，同样触发释放。
func streamZenResponsesAsChat(resp *http.Response, model string, cancel context.CancelFunc) *http.Response {
	pr, pw := io.Pipe()
	go func() {
		sink := &zenChatChunkSink{
			w:        pw,
			id:       "chatcmpl-" + randHex(12),
			created:  time.Now().Unix(),
			model:    model,
			toolIdx:  map[string]int{},
			toolSeen: map[string]bool{},
		}
		err := pumpZenResponsesEvents(resp.Body, sink)
		if sink.err != nil {
			err = sink.err
		}
		resp.Body.Close()
		cancel()
		pw.CloseWithError(err)
	}()
	return &http.Response{
		Status:        "200 OK",
		StatusCode:    http.StatusOK,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:          pr,
		ContentLength: -1,
		Request:       resp.Request,
	}
}

// collapseZenResponsesStream 消费 Responses 事件流，折叠成单个 chat.completion JSON 响应。
func collapseZenResponsesStream(resp *http.Response, model string, cancel context.CancelFunc) (*http.Response, error) {
	defer resp.Body.Close()
	defer cancel()
	acc := &zenCollapseAcc{Model: model, Created: time.Now().Unix()}
	sink := &zenChatAccSink{acc: acc, toolIdx: map[string]int{}}
	if err := pumpZenResponsesEvents(resp.Body, sink); err != nil {
		return nil, fmt.Errorf("zen responses stream read: %w", err)
	}
	log.Printf("  zen responses collapse: stream -> json (content_len=%d tool_calls=%d)", len(acc.Content), len(acc.ToolCalls))
	return zenCollapseToChatResponse(acc, resp.Request)
}
