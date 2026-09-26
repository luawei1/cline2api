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
	if anonymous && wireStream {
		body["stream_options"] = map[string]any{"include_usage": true}
		ensureAnonymousTools(body)
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
	bodyJSON, err := json.Marshal(buildZenBody(params, stream, anonymous))
	if err != nil {
		return nil, fmt.Errorf("marshal zen body: %w", err)
	}
	endpoint := strings.TrimRight(cfg.BaseURL, "/") + "/chat/completions"

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
		log.Printf("  zen upstream: model=%v stream=%v(下游=%v) msgs=%d via=%s attempt=%d session=%s",
			bodyParamsModel(params), anonymous, stream, getMsgCount(params), describeZenProxy(), attempt+1, truncate(sess, 30))

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
	log.Printf("  zen collapse: stream -> json (content_len=%d tool_calls=%d)", len(acc.Content), len(acc.ToolCalls))
	return &http.Response{
		Status:        "200 OK",
		StatusCode:    http.StatusOK,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        http.Header{"Content-Type": []string{"application/json"}},
		Body:          io.NopCloser(bytes.NewReader(data)),
		ContentLength: int64(len(data)),
		Request:       resp.Request,
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
	for id := range oldIDs {
		if !seen[id] {
			res.Removed = append(res.Removed, id)
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
