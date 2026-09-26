package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// defaultMaxTokens 是上游请求的默认/上限输出 token 数。
	//
	// 从 128000 降到 32000：日志实测 poolside/laguna-s-2.1:free 直接 400 ——
	// "maximum context length is 262144 tokens, however you requested about 263503
	// tokens (88799 text input, 46704 tool input, 128000 in the output)"。
	// 也就是说 128k 的输出预留本身就能把请求顶出上下文窗口，让本来可用的模型直接失败，
	// 并让其余模型陷入超长预填充/静默。可用 CLINE2API_MAX_TOKENS 调整。
	defaultMaxTokens       = 32000
	defaultReasoningEffort = "high"
	fallbackDefaultModel   = "z-ai/glm-5.3-flash"
	freeModelPrimary       = "z-ai/glm-5.3-flash"
	freeModelFallback      = "deepseek/deepseek-v4-flash"
	freeModelLastResort    = "cline-free/longcat-2.0"
)

// minUpstreamMaxTokens 上游对输出 token 的硬下限：Cline 免费模型经 OpenRouter
// 转发时（如 meta/muse-spark），max_output_tokens < 16 会被上游直接 400。
const minUpstreamMaxTokens = 16

// defaultFirstContentTimeout 是单次上游尝试等待「首个内容事件」的上限。
// 超时即认定该模型此刻不可用：放弃本次尝试、冷却该模型、立刻换到链上下一个模型。
//
// 依据 proxy.log 2026/09/22 12:34~13:04 的实测：同一个 glm-5.3-flash 账号在 93 tools
// 的负载下，有时 1~3 秒就出内容，有时建流后静默 5~10 分钟（超过客户端耐心后
// 只能看到「Combobulating…」然后 timeout），而同一时刻链上的 deepseek-v4.1-flash
// 1.5 秒就返回。继续等待没有收益 —— 快速换模型才有。
//
// 20s 而不是 90s：90s 的实测后果是 13:31:40 那次请求在 GLM 上白等 90s、
// 换到 laguna 立刻 400、再换 solar-pro4 又静默，客户端只看到「Fermenting… (2m51s)」。
// 好几个候选都被跳过时，旧行为（客户端自己超时后重连）体验明显好于我们长时间静默等待，
// 所以这里取「一次不行就赶紧换」的短耐心。可用 CLINE2API_FIRST_CONTENT_TIMEOUT_MS 覆盖。
const defaultFirstContentTimeout = 20 * time.Second

// lastAttemptFirstContentTimeout 是最后一次尝试的等待上限，比普通尝试宽容：
// 候选模型都被跳过时，宁可多等一下也要给出答案，而不是把错误抛给客户端。
// 但仍然收敛在客户端忍耐范围内，不留几分钟的静默「假活着」。
// 可用 CLINE2API_LAST_ATTEMPT_TIMEOUT_MS 覆盖。
const lastAttemptFirstContentTimeout = 45 * time.Second

// maxOutputTokens 是客户端请求中 max_tokens 的上限。
// 客户端显式要更大的输出（例如 128k）也会被夹到这里：预留过大的输出空间会把输入+输出
// 顶出上游上下文窗口（实测 400），收益却接近没有。
func maxOutputTokens() int {
	return envInt("CLINE2API_MAX_TOKENS", defaultMaxTokens)
}

// clampMaxTokens 把请求的输出上限夹到 [1, maxOutputTokens()]。
func clampMaxTokens(n int) int {
	limit := maxOutputTokens()
	if n <= 0 || n > limit {
		return limit
	}
	return n
}

// envInt 读取整数环境变量（缺失/非法/非正数时用默认值）。
func envInt(key string, def int) int {
	n, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key)))
	if err != nil || n <= 0 {
		return def
	}
	return n
}

// envDurationFromMs 读取以毫秒为单位的时长环境变量（缺失/非法/非正数时用默认值）。
func envDurationFromMs(key string, def time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return def
	}
	return time.Duration(n) * time.Millisecond
}

// reasoningEffort 返回下发给上游的推理强度。默认保持 "high"，
// 可用 CLINE2API_REASONING_EFFORT=low|medium|high|none 覆盖：推理模型在大上下文下
// 的思考阶段可能是长时间静默的来源之一，这是用户侧最简单的延迟开关。
func reasoningEffort() string {
	if v := strings.TrimSpace(os.Getenv("CLINE2API_REASONING_EFFORT")); v != "" {
		return v
	}
	return defaultReasoningEffort
}

// freeModelChain 是 model="free" 时的降级顺序。
// 顺序依据 Artificial Analysis Intelligence Index v4.1.1：
// glm-5.3-flash 57 > deepseek-v4-flash 0731 52 > longcat-2.0 34。
var freeModelChain = []string{freeModelPrimary, freeModelFallback, freeModelLastResort}

// builtinModels 是内置默认模型列表（不可删除），仅作为离线 / 未同步时的 fallback。
// 同步 Cline 官方推荐模型成功后，getAllModels 以远程模型为主。
var builtinModels = []Model{
	{ID: "z-ai/glm-5.3-flash", Provider: "z-ai", Cost: "free", Status: "active", Custom: false},
	{ID: "cline-free/longcat-2.0", Provider: "cline-free", Cost: "free", Status: "active", Custom: false},
	{ID: "cline-pass/glm-5.2", Provider: "zai", Cost: "pass", Status: "active", Custom: false},
	{ID: "cline-pass/deepseek-v4-flash", Provider: "deepseek", Cost: "pass", Status: "active", Custom: false},
	{ID: "cline-pass/qwen3.7-max", Provider: "qwen", Cost: "pass", Status: "active", Custom: false},
	{ID: "deepseek/deepseek-v4-flash", Provider: "deepseek", Cost: "free", Status: "active", Custom: false},
	{ID: "poolside/laguna-s-2.1:free", Provider: "poolside", Cost: "free", Status: "active", Custom: false},
}

// getAllModels 返回可用模型列表：
//   - 已同步远程模型：Cline 远程（Source=remote）+ opencode 同步（Source=zen）+ 用户自定义
//   - 未同步 / 离线：内置 fallback（Cline + zen 种子表）+ 用户自定义
func getAllModels() []Model {
	p := loadPool()
	poolMu.Lock()
	defer poolMu.Unlock()

	var custom []Model
	var remote []Model
	var zen []Model
	for _, m := range p.Models {
		switch m.Source {
		case "remote":
			remote = append(remote, m)
		case "zen":
			zen = append(zen, m)
		default:
			custom = append(custom, m)
		}
	}

	if len(remote) > 0 || len(zen) > 0 || remoteZenActive() {
		result := make([]Model, 0, len(remote)+len(zen)+len(custom))
		result = append(result, remote...)
		result = append(result, zen...)
		result = append(result, custom...)
		return result
	}

	builtin := make([]Model, 0, len(builtinModels)+len(zenSeedModels))
	builtin = append(builtin, builtinModels...)
	builtin = append(builtin, builtinZenModels()...)

	result := make([]Model, 0, len(builtin)+len(custom))
	result = append(result, builtin...)
	result = append(result, custom...)
	return result
}

// getDefaultModel 返回用户设置的默认模型；未设置时优先回退到第一个远程 free 模型，
// 否则用内置 fallback。
func getDefaultModel() string {
	p := loadPool()
	poolMu.Lock()
	defer poolMu.Unlock()

	// 管理员设置的默认模型也会随上游下架而失效：远程同步已启用却查不到时
	// 视为已下架，落到下方「第一个远程免费模型」，避免每个无模型请求必败。
	if p.DefaultModel != "" && (!remoteModelsActive() || liveModelSet(p)[p.DefaultModel]) {
		return p.DefaultModel
	}

	for _, m := range p.Models {
		if m.Source == "remote" && m.Cost == "free" {
			return m.ID
		}
	}

	// 远程同步已启用但上游没有免费模型（列表调整 / 接口变化）：从仍然在线的
	// 模型里挑一个，绝不在有实时数据时退回下面的硬编码内置表（写死的模型
	// 已下架时会让每个无模型请求必败）。
	if remoteModelsActive() {
		for _, m := range p.Models {
			if m.Status == "active" {
				return m.ID
			}
		}
	}

	// 仅离线 / 从未同步成功时才允许内置硬编码 fallback。
	for _, m := range builtinModels {
		if m.Cost == "free" {
			return m.ID
		}
	}
	return fallbackDefaultModel
}

// 当前监听地址（startProxy 启动时赋值，供管理后台展示）。
var (
	listenHost string
	listenPort int
)

// HTTP server 实例与路由表（restartListener 换地址重启时复用）。
var (
	serverMux     *http.ServeMux
	currentServer *http.Server
	serverMu      sync.Mutex
)

// restartListener 用新地址重启 HTTP 监听。
// 注意：必须在 goroutine 中调用——Shutdown 会等待当前 HTTP 请求完成，
// 若在 admin handler 内同步调用会死锁。
func restartListener(host string, port int) error {
	if host == "" {
		host = "127.0.0.1"
	}
	addr := fmt.Sprintf("%s:%d", host, port)
	listenHost = host
	listenPort = port

	serverMu.Lock()
	old := currentServer
	server := &http.Server{Addr: addr, Handler: serverMux}
	currentServer = server
	serverMu.Unlock()

	if old != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = old.Shutdown(ctx)
		cancel()
	}

	fmt.Println("")
	fmt.Println(strings.Repeat("=", 58))
	fmt.Printf("  Listener restarted: %s\n", addr)
	if !isLoopbackHost(host) {
		for _, ip := range detectLocalIPs() {
			fmt.Printf("  http://%s:%d (LAN)\n", ip, port)
		}
		fmt.Println("  !!! 监听非本机地址，管理后台无鉴权，请确认网络环境安全")
	}
	fmt.Println(strings.Repeat("=", 58))
	return server.ListenAndServe()
}

// effectiveAdminHost 返回管理后台/浏览器实际可用的访问地址：
// host 为空或通配地址（0.0.0.0 / ::）时展示回环 127.0.0.1，否则返回 host 本身。
func effectiveAdminHost(host string) string {
	switch host {
	case "", "0.0.0.0", "::":
		return "127.0.0.1"
	}
	return host
}

// detectLocalIPs 检测本机所有可用 IPv4 地址（排除回环、链路本地和未启用的网卡）。
func detectLocalIPs() []string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	result := make([]string, 0, len(addrs))
	for _, a := range addrs {
		ipNet, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		ip := ipNet.IP
		if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
			continue
		}
		if v4 := ip.To4(); v4 != nil {
			result = append(result, v4.String())
		}
	}
	return result
}

// isLoopbackHost 判断监听地址是否为回环（127.x / localhost），用于安全提示。
func isLoopbackHost(host string) bool {
	switch host {
	case "", "localhost", "127.0.0.1":
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

var passThroughKeys = []string{
	"tools", "tool_choice", "parallel_tool_calls", "functions", "function_call",
	"temperature", "top_p", "top_k", "stop", "presence_penalty", "frequency_penalty",
	"response_format", "user", "n", "logit_bias", "seed", "logprobs", "top_logprobs",
	"stream_options", "metadata",
}

type chatRequest struct {
	Model               string          `json:"model"`
	Messages            json.RawMessage `json:"messages"`
	Stream              bool            `json:"stream,omitempty"`
	MaxTokens           int             `json:"max_tokens,omitempty"`
	MaxCompletionTokens int             `json:"max_completion_tokens,omitempty"`
	Tools               json.RawMessage `json:"tools,omitempty"`
	ToolChoice          json.RawMessage `json:"tool_choice,omitempty"`
	ReasoningEffort     string          `json:"reasoning_effort,omitempty"`
	ReasoningEffortAlt  string          `json:"reasoningEffort,omitempty"`
	Extra               map[string]any  `json:"-"`
}

// isFreeModelEntry 判断模型是否免费（/models 过滤用）：
// Cost 直接标记 free，或 zen 来源且命中免费判定（种子白名单兜底）。
func isFreeModelEntry(m Model) bool {
	if m.Cost == "free" {
		return true
	}
	return isZenSource(m) && isZenFreeModel(m)
}

func startProxy(host string, port int) error {
	p := loadPool()
	loadRequestLogs()
	activeCount := 0
	for _, a := range p.Accounts {
		if a.Status == "active" {
			// Try to pre-warm tokens
			if a.AccessToken == "" || time.Now().UnixMilli() >= a.ExpiresAt {
				if err := refreshAccountToken(a); err != nil {
					log.Printf("  Pre-warm failed for %s: %v", a.Email, err)
					continue
				}
			}
			activeCount++
		}
	}
	log.Printf("Loaded %d active accounts from pool", activeCount)

	// 启动时异步同步一次 Cline 官方推荐模型（不阻塞启动）
	startModelSync()

	// opencode zen：定时同步免费模型列表 + 压缩会话状态清理
	if getZenConfig().Enabled {
		startZenModelsRefresher()
	}
	startCompactCleanup()

	freePort(port)

	mux := http.NewServeMux()

	mux.HandleFunc("/v1/health", corsHandler(func(w http.ResponseWriter, r *http.Request) {
		info := map[string]any{
			"status":         "ok",
			"version":        appVersion,
			"activeAccounts": activeCount,
		}
		writeJSON(w, http.StatusOK, info)
	}))
	mux.HandleFunc("/health", corsHandler(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":         "ok",
			"version":        appVersion,
			"activeAccounts": activeCount,
		})
	}))

	// Admin API (frontend + REST)
	registerAdminRoutes(mux)

	apiKeyHandler := func(next http.HandlerFunc) http.HandlerFunc {
		return corsHandler(func(w http.ResponseWriter, r *http.Request) {
			// Allow requests without key if no keys configured
			p := loadPool()
			if len(p.Keys) == 0 {
				next(w, r)
				return
			}

			key := r.Header.Get("x-api-key")
			if key == "" {
				if b := r.Header.Get("Authorization"); len(b) > 7 && b[:7] == "Bearer " {
					key = b[7:]
				}
			}

			valid := false
			for _, k := range p.Keys {
				if k == key {
					valid = true
					break
				}
			}

			if !valid {
				writeJSON(w, http.StatusUnauthorized, map[string]any{
					"error": map[string]string{
						"message": "invalid API key. Generate one at /admin/ or set x-api-key header",
						"type":    "auth_error",
					},
				})
				return
			}
			next(w, r)
		})
	}

	modelsHandler := apiKeyHandler(func(w http.ResponseWriter, r *http.Request) {
		onlyFree := getProxyConfig().OnlyFree
		all := getAllModels()
		list := make([]map[string]any, 0, len(all))
		for _, m := range all {
			if onlyFree && !isFreeModelEntry(m) {
				continue
			}
			ownedBy := "cline"
			if m.Source == "zen" || m.Provider == "opencode" {
				ownedBy = "opencode"
			}
			list = append(list, map[string]any{
				"id":       m.ID,
				"object":   "model",
				"created":  time.Now().UnixMilli(),
				"owned_by": ownedBy,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": list})
	})
	mux.HandleFunc("/v1/models", modelsHandler)
	mux.HandleFunc("/models", modelsHandler)

	chatHandler := apiKeyHandler(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": map[string]string{"message": err.Error(), "type": "parse_error"},
			})
			return
		}

		var params map[string]any
		if err := json.Unmarshal(body, &params); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": map[string]string{"message": err.Error(), "type": "parse_error"},
			})
			return
		}

		isStream, _ := params["stream"].(bool)
		toolCount := 0
		if tools, ok := params["tools"]; ok {
			if t, ok := tools.([]any); ok {
				toolCount = len(t)
			}
		}
		model, _ := params["model"].(string)
		log.Printf("  client: stream=%v tools=%d model=%s", isStream, toolCount, model)

		reqLog := RequestLog{StartedAt: time.Now(), Protocol: "openai", Model: model, Stream: isStream}

		// Override system prompt from override.md for OpenAI format
		if override := loadOverrideContent(); override != "" {
			if msgs, ok := params["messages"].([]any); ok {
				found := false
				for _, m := range msgs {
					if mm, ok := m.(map[string]any); ok {
						if mm["role"] == "system" {
							mm["content"] = override
							found = true
							break
						}
					}
				}
				if !found {
					params["messages"] = append([]any{map[string]any{"role": "system", "content": override}}, msgs...)
				}
			}
		}

		// 按 model 自动分流：zen 免费模型 / zen 付费拒绝 / 其余走 Cline 池
		switch routeModel(model) {
		case "reject":
			msg := fmt.Sprintf("model %q is a paid opencode model; only free models are proxied", model)
			finalizeRequestLog(&reqLog, tokenUsage{}, time.Time{}, reqLog.StartedAt, false, msg)
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": map[string]string{"message": msg, "type": "invalid_request_error"},
			})
			return
		case "zen":
			reqLog.Upstream = upstreamOpenCode
			zm, _ := resolveZenInfo(model)
			out := maybeCompact(params, zm, requestSessionID(params, r.Header))
			if out.changed {
				log.Printf("  chat %s", out.note)
			}
			resp, err := callZenAPI(params, isStream)
			if err != nil {
				if fbResp, fbAcc, fbErr, attempted := zenFailoverToCline(params, isStream); attempted {
					if fbErr == nil {
						log.Printf("  chat failover: serving %q via cline pool", model)
						stampUpstream(&reqLog, params) // 归因实际服务方（provider / cline）
						if fbAcc != nil {
							reqLog.AccountID = fbAcc.AccountID
							reqLog.AccountEmail = fbAcc.Email
						}
						defer fbResp.Body.Close()
						if isStream {
							handleStreamResponse(w, fbResp, fbAcc, &reqLog)
						} else {
							handleNonStreamResponse(w, fbResp, fbAcc, &reqLog)
						}
						return
					}
					err = fbErr
				}
				log.Printf("  api error: %v", err)
				finalizeRequestLog(&reqLog, tokenUsage{}, time.Time{}, reqLog.StartedAt, false, err.Error())
				writeJSON(w, http.StatusBadGateway, map[string]any{
					"error": map[string]string{"message": err.Error(), "type": "api_error"},
				})
				return
			}
			defer resp.Body.Close()
			if isStream {
				handleStreamResponse(w, resp, nil, &reqLog)
			} else {
				handleNonStreamResponse(w, resp, nil, &reqLog)
			}
			return
		}

		// cline 池路径才要求账号；zen 免费模型不依赖本地账号池
		if activeCount == 0 && len(loadPool().Accounts) == 0 {
			writeJSON(w, http.StatusUnauthorized, map[string]any{
				"error": map[string]string{
					"message": "No accounts in pool. Run with --add-account or POST /admin/login to add accounts.",
					"type":    "auth_error",
				},
			})
			return
		}

		resp, acc, err := callClineAPI(params, isStream)
		if effectiveModel, ok := params["model"].(string); ok && effectiveModel != "" {
			reqLog.Model = effectiveModel // 含回退后的实际服务模型
		}
		stampUpstream(&reqLog, params) // 归因实际服务方（provider / opencode / cline）
		if err != nil {
			log.Printf("  api error: %v", err)
			finalizeRequestLog(&reqLog, tokenUsage{}, time.Time{}, reqLog.StartedAt, false, err.Error())
			writeJSON(w, clineErrorHTTPStatus(err), map[string]any{
				"error": map[string]string{"message": err.Error(), "type": "api_error"},
			})
			return
		}
		defer resp.Body.Close()
		if acc != nil {
			reqLog.AccountID = acc.AccountID
			reqLog.AccountEmail = acc.Email
		}

		if isStream {
			handleStreamResponse(w, resp, acc, &reqLog)
		} else {
			handleNonStreamResponse(w, resp, acc, &reqLog)
		}
	})
	mux.HandleFunc("/v1/chat/completions", chatHandler)
	mux.HandleFunc("/chat/completions", chatHandler)

	// Anthropic Messages API support
	anthropicHandler := apiKeyHandler(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		handleAnthropicMessages(w, r)
	})
	mux.HandleFunc("/v1/messages", anthropicHandler)
	mux.HandleFunc("/messages", anthropicHandler)

	// OpenAI Responses API support（所有上游：zen 免费模型 + Cline 账号池）
	responsesHandler := apiKeyHandler(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		handleResponses(w, r)
	})
	mux.HandleFunc("/v1/responses", responsesHandler)
	mux.HandleFunc("/responses", responsesHandler)

	if host == "" {
		host = "127.0.0.1"
	}
	addr := fmt.Sprintf("%s:%d", host, port)
	listenHost = host
	listenPort = port
	serverMux = mux
	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}
	serverMu.Lock()
	currentServer = server
	serverMu.Unlock()

	// 启动后台冷却恢复巡检
	startCooldownRecovery()

	fmt.Println("")
	fmt.Println(strings.Repeat("=", 58))
	fmt.Printf("  Cline Go Proxy %s - No CLI Required\n", appVersion)
	fmt.Println(strings.Repeat("=", 58))
	fmt.Printf("  http://%s\n", addr)
	fmt.Printf("  http://%s/v1\n", addr)
	if !isLoopbackHost(host) {
		for _, ip := range detectLocalIPs() {
			fmt.Printf("  http://%s:%d (LAN)\n", ip, port)
		}
		fmt.Println("  !!! 监听非本机地址，管理后台无鉴权，请确认网络环境安全")
	}
	fmt.Println("  API Key: any value")
	fmt.Printf("  Model:   %s\n", getDefaultModel())
	fmt.Printf("  Accounts: %d total, %d active\n", len(loadPool().Accounts), activeCount)
	if zc := getZenConfig(); zc.Enabled {
		fmt.Printf("  OpenCode: enabled (%s free models)\n", strings.TrimRight(zc.BaseURL, "/"))
	} else {
		fmt.Println("  OpenCode: disabled")
	}
	fmt.Println(strings.Repeat("=", 58))

	return server.ListenAndServe()
}

func corsHandler(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, x-api-key, anthropic-version, anthropic-beta")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		h(w, r)
	}
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func cleanMessages(messages []any) []any {
	cleaned := make([]any, 0, len(messages))
	for _, m := range messages {
		msg, ok := m.(map[string]any)
		if !ok {
			cleaned = append(cleaned, m)
			continue
		}
		cleaned = append(cleaned, msg)
	}
	return cleaned
}

// sanitizeMessages 修复出站消息历史中的畸形 tool_calls。
// 背景：上游偶发输出 function.name 为空的 tool call（GLM 流式分片丢失 / 工具调用
// 以文本形式泄漏），客户端执行后会把残缺记录回放进下一轮历史，导致上游恒定 400：
// "tool_calls[N].function.name must be a non-empty string"。
// 处理：
//  1. 丢弃 function.name 为空的 tool_call；
//  2. 过滤后 tool_calls 为空则移除该字段；
//  3. 丢弃没有对应合法 assistant tool_call 的孤儿 tool 结果（自愈被污染的会话）。
func sanitizeMessages(messages []any) []any {
	validToolIDs := make(map[string]bool)
	for _, m := range messages {
		msg, ok := m.(map[string]any)
		if !ok || msg["role"] != "assistant" {
			continue
		}
		tcs, ok := msg["tool_calls"].([]any)
		if !ok {
			continue
		}
		for _, tc := range tcs {
			tcMap, ok := tc.(map[string]any)
			if !ok {
				continue
			}
			fn, _ := tcMap["function"].(map[string]any)
			name, _ := fn["name"].(string)
			id, _ := tcMap["id"].(string)
			if name != "" && id != "" {
				validToolIDs[id] = true
			}
		}
	}

	cleaned := make([]any, 0, len(messages))
	for _, m := range messages {
		msg, ok := m.(map[string]any)
		if !ok {
			cleaned = append(cleaned, m)
			continue
		}
		role, _ := msg["role"].(string)

		// 孤儿 tool 结果：找不到对应的 assistant tool_call
		if role == "tool" {
			id, _ := msg["tool_call_id"].(string)
			if !validToolIDs[id] {
				continue
			}
			cleaned = append(cleaned, msg)
			continue
		}

		// assistant 消息：剔除空名 tool_call
		if role == "assistant" {
			if tcs, ok := msg["tool_calls"].([]any); ok {
				kept := make([]any, 0, len(tcs))
				for _, tc := range tcs {
					tcMap, ok := tc.(map[string]any)
					if !ok {
						continue
					}
					fn, _ := tcMap["function"].(map[string]any)
					name, _ := fn["name"].(string)
					if name == "" {
						continue
					}
					kept = append(kept, tcMap)
				}
				if len(kept) == 0 {
					delete(msg, "tool_calls")
				} else {
					msg["tool_calls"] = kept
				}
			}
		}
		cleaned = append(cleaned, msg)
	}
	return cleaned
}

// genToolUseID 生成 tool_use 块 id（上游未返回 id 时的兜底）。
func genToolUseID() string {
	return fmt.Sprintf("toolu_%x", time.Now().UnixNano())
}

// hasToolUseBlocks 判断 Anthropic content 块数组中是否含有有效的 tool_use 块。
func hasToolUseBlocks(content any) bool {
	blocks, ok := content.([]any)
	if !ok {
		return false
	}
	for _, b := range blocks {
		if bm, ok := b.(map[string]any); ok && bm["type"] == "tool_use" {
			return true
		}
	}
	return false
}

func buildUpstreamBody(params map[string]any, stream bool) map[string]any {
	sessionID := fmt.Sprintf("sess_%d", time.Now().UnixMilli())

	maxTokens := maxOutputTokens()
	source := ""
	if mt, ok := params["max_tokens"].(float64); ok {
		maxTokens, source = clampMaxTokens(int(mt)), "max_tokens"
	} else if mt, ok := params["max_completion_tokens"].(float64); ok {
		maxTokens, source = clampMaxTokens(int(mt)), "max_completion_tokens"
	}
	// 客户端发的 0 视为未设置、1~15 低于上游硬下限：一律兜到默认值，
	// 否则 muse-spark 等模型直接 400 且错误会被回退链吞掉
	if maxTokens < minUpstreamMaxTokens {
		if source != "" {
			log.Printf("  clamp %s=%d -> %d (upstream requires >= %d)", source, maxTokens, maxOutputTokens(), minUpstreamMaxTokens)
		}
		maxTokens = maxOutputTokens()
	}

	model := getDefaultModel()
	if m, ok := params["model"].(string); ok && m != "" {
		model = m
	}

	body := map[string]any{
		"model":            model,
		"max_tokens":       maxTokens,
		"session_id":       sessionID,
		"reasoning_effort": reasoningEffort(),
	}

	if msgsRaw, ok := params["messages"]; ok {
		if msgsArr, ok := msgsRaw.([]any); ok {
			body["messages"] = sanitizeMessages(msgsArr)
		} else {
			body["messages"] = msgsRaw
		}
	}

	if stream {
		body["stream"] = true
	}

	if re, ok := params["reasoning_effort"].(string); ok && re != "" {
		body["reasoning_effort"] = re
	} else if re, ok := params["reasoningEffort"].(string); ok && re != "" {
		body["reasoning_effort"] = re
	}

	// Cline 上游不接受 "none" 枚举：客户端显式关闭推理时直接省略该字段；
	// 空字符串同样省略（避免把非法枚举下发到上游）。
	if re, _ := body["reasoning_effort"].(string); re == "none" || re == "" {
		delete(body, "reasoning_effort")
	}

	for _, key := range passThroughKeys {
		if val, ok := params[key]; ok {
			body[key] = val
		}
	}

	return body
}

func clineHeaders(token, sessionID string) http.Header {
	h := http.Header{}
	h.Set("Authorization", "Bearer "+token)
	h.Set("Content-Type", "application/json")
	h.Set("X-Task-ID", sessionID)

	cfg := getProxyConfig()
	for k, v := range cfg.Headers {
		h.Set(k, v)
	}

	return h
}

type clineAPIError struct {
	statusCode int
	message    string
}

func (e *clineAPIError) Error() string {
	return fmt.Sprintf("API %d: %s", e.statusCode, e.message)
}

type clineAccountUnavailableError struct {
	err error
}

func (e *clineAccountUnavailableError) Error() string {
	return e.err.Error()
}

func (e *clineAccountUnavailableError) Unwrap() error {
	return e.err
}

type freeModelUnavailableError struct {
	message string
}

func (e *freeModelUnavailableError) Error() string {
	return e.message
}

func clineErrorHTTPStatus(err error) int {
	if _, ok := err.(*freeModelUnavailableError); ok {
		return http.StatusTooManyRequests
	}
	return http.StatusInternalServerError
}

// servedByParam 是 callClineAPI 族回写在 params 上的「实际服务方」标记：
// 自定义 provider 成功服务时写入 upstreamProvider。仅用于请求日志归因——
// buildUpstreamBody / callProvider / callZenAPI 只拷贝白名单键，不会外发。
const servedByParam = "__served_by"

// stampUpstream 按实际服务结果写入请求日志的上游归因：
// provider 标记 > 生效模型是 zen 模型（反向故障转移）> Cline 账号池。
func stampUpstream(reqLog *RequestLog, params map[string]any) {
	if v, _ := params[servedByParam].(string); v == upstreamProvider {
		reqLog.Upstream = upstreamProvider
		return
	}
	if m, _ := params["model"].(string); m != "" {
		if _, isZen := resolveZenInfo(m); isZen {
			reqLog.Upstream = upstreamOpenCode
			return
		}
	}
	reqLog.Upstream = upstreamCline
}

func callClineAPI(params map[string]any, stream bool) (*http.Response, *Account, error) {
	model, _ := params["model"].(string)
	delete(params, servedByParam) // 归因标记按次重算，避免流重试沿用上一次结果
	if model == "free" {
		resp, acc, err := callFreeClineAPI(params, stream)
		if err != nil {
			// Cline 残血池整条链耗尽（429 冷却/无账号）→ 落到 zen 免费模型
			if fbResp, attempted := clineFailoverToZen(params, stream); attempted {
				return fbResp, nil, nil
			}
		}
		return resp, acc, err
	}
	// zen 免费模型进入 cline 池仅发生在 zen 故障转移期间：改写成 cline 侧可用的
	// free 模型链，否则 Cline 上游会报 "invalid model format. Expected format:
	// modelType/model"（zen 的裸模型 ID 不符合 Cline 的 provider/model 格式）。
	if zm, ok := resolveZenInfo(model); ok && isZenFreeModel(zm) {
		log.Printf("  zen failover: rewriting zen model %q to cline free chain", model)
		return callFreeClineAPI(params, stream)
	}

	// 显式模型请求降级序列：
	//  1. 自定义 provider（若该模型接入了 provider）
	//  2. 客户端点名模型走 Cline 池
	//  3. modelChain（管理员配置或内置 free 链）逐个降级
	//  4. 全部失败 → 明确报错
	// 可用性感知重排：点名模型保持首位，其余按「未冷却优先、用量少优先」
	// 重排，避免回退流量每次都集中砸在第一个可用模型上直到它也冷却。
	sorted := sortModelsByAvailability(modelFallbackChain(model))
	chain := make([]string, 0, len(sorted)+1)
	chain = append(chain, model)
	for _, m := range sorted {
		if m != model {
			chain = append(chain, m)
		}
	}
	for _, m := range chain {
		// 先试自定义 provider
		if pResp, pErr, attempted := callCustomProviderAPI(withModel(params, m), stream); attempted {
			if pErr == nil {
				if m != model {
					log.Printf("  model fallback: %q unavailable, serving via provider %q", model, m)
				}
				params["model"] = m // 回写实际服务模型，供请求日志归因
				params[servedByParam] = upstreamProvider
				return pResp, nil, nil
			}
			log.Printf("  provider attempt failed for %q: %v", m, pErr)
		}

		// 点名模型保持原有轮询语义；链上的降级模型改用「最久未用优先」，
		// 避免所有降级流量都压到第一个可用账号/模型的额度上。
		pickAcc := pickAccountForModelStrict
		if m != model {
			pickAcc = pickAccountForModelLeastUsed
		}
		for {
			acc := pickAcc(m)
			if acc == nil {
				break // 该模型所有账号均冷却/不可用 → 尝试链上下一个模型
			}
			resp, usedAcc, err := callClineAPIWithAccountCtx(context.Background(), acc, withModel(params, m), stream)
			if err == nil {
				if m != model {
					log.Printf("  model fallback: %q cooling on all accounts, serving via %q", model, m)
				}
				params["model"] = m // 回写实际服务模型，供请求日志归因
				return resp, usedAcc, nil
			}
			var accountErr *clineAccountUnavailableError
			if errors.As(err, &accountErr) {
				continue
			}
			apiErr, ok := err.(*clineAPIError)
			if !ok || apiErr.statusCode != http.StatusTooManyRequests {
				// 非 429 错误：若后面还有候选（provider / 链）则继续降级，否则透传
				if !hasAnyFallbackLeft(model, m) {
					return nil, usedAcc, err
				}
				break
			}
			// 429：模型冷却已记录，换下一个账号；全部冷却后降级到下一个模型
		}
	}
	// 终极兜底：free 链（手动链全部失败时自动切换）
	if model != "free" && !isFreeAliasModel(model) {
		fp := withModel(params, "free")
		if resp, acc, err := callFreeClineAPI(fp, stream); err == nil {
			log.Printf("  auto fallback: all configured options failed for %q, served by free chain", model)
			if fm, ok := fp["model"].(string); ok && fm != "" {
				params["model"] = fm // callFreeClineAPI 就地改写 fp，取回实际服务模型
			}
			if v, ok := fp[servedByParam].(string); ok {
				params[servedByParam] = v // provider 经 free 链服务时同步归因标记
			}
			return resp, acc, nil
		}
	}
	// Cline 侧（含 free 链）全部耗尽 → 反向故障转移到 zen 免费模型
	if fbResp, attempted := clineFailoverToZen(params, stream); attempted {
		return fbResp, nil, nil
	}
	if hasActiveAccounts() {
		return nil, nil, &freeModelUnavailableError{message: fmt.Sprintf("model %q is cooling on all accounts and no fallback model is available", model)}
	}
	return nil, nil, fmt.Errorf("no active accounts available. Use --login or admin API to add accounts")
}

// withModel 返回带指定模型名的参数副本（避免污染调用方 map）。
func withModel(params map[string]any, model string) map[string]any {
	cp := make(map[string]any, len(params)+1)
	for k, v := range params {
		cp[k] = v
	}
	cp["model"] = model
	return cp
}

// isFreeAliasModel 判断是否为 "free" 别名或链内免费模型（避免重复兜底）。
func isFreeAliasModel(model string) bool {
	if model == "free" {
		return true
	}
	for _, m := range defaultFreeChain() {
		if m == model {
			return true
		}
	}
	return false
}

// defaultFreeChain 动态派生默认回退链（管理员未配置 modelChain 时）：
// 优先取 Cline 远程同步的免费模型（有效、不下架），再补内置常量链。
// 已下架的内置模型（如 longcat-2.0，不在远程同步列表）自动排除，不再写死；
// 从未同步成功（离线）时回退内置常量链。
func defaultFreeChain() []string {
	remote := remoteModelsActive()
	p := loadPool()
	known := make(map[string]bool, len(p.Models))
	var remoteFree []string
	for _, m := range p.Models {
		known[m.ID] = true
		if remote && m.Source == "remote" && m.Cost == "free" && m.Status == "active" {
			remoteFree = append(remoteFree, m.ID)
		}
	}
	chain := make([]string, 0, len(remoteFree)+len(freeModelChain))
	seen := make(map[string]bool, len(remoteFree)+len(freeModelChain))
	for _, m := range remoteFree {
		if !seen[m] {
			seen[m] = true
			chain = append(chain, m)
		}
	}
	for _, m := range freeModelChain {
		if seen[m] || (remote && !known[m]) {
			continue
		}
		seen[m] = true
		chain = append(chain, m)
	}
	if len(chain) == 0 {
		return freeModelChain
	}
	return chain
}

// liveModelSet 返回当前仍然「在线」的模型 ID 集合：池内全部模型（远程同步
// remote / opencode zen 同步 / 管理员自定义）+ 启用中自定义 provider 声明的模型。
// 调用方须持有 poolMu；providers 走独立的 providersMu，仓库内不存在反向持锁路径。
func liveModelSet(p *AccountPool) map[string]bool {
	live := make(map[string]bool, len(p.Models))
	for _, m := range p.Models {
		live[m.ID] = true
	}
	for _, sp := range listProviders() {
		if !sp.Enabled {
			continue
		}
		for _, id := range sp.ModelIDs {
			live[id] = true
		}
	}
	return live
}

// filterStaleModels 从回退链里剔除已下架的模型。
//
// 上游免费/推荐模型每日调整，写死或旧配置里的模型会随下架而失效：继续尝试
// 只会白白多一次必败的上游往返（404/400 + 冷却），排在链头时每个请求都要先
// 撞一次失败。判定来源：远程同步成功的模型列表（含 zen/自定义）+ 启用中
// provider 的 modelIds。
//
// 远程同步从未成功（离线冷启动）时不判定、原样保留；剔空时也原样保留
// （宁可按原链试一次，也不要空链直接报错）。
func filterStaleModels(chain []string) []string {
	if len(chain) == 0 || !remoteModelsActive() {
		return chain
	}
	p := loadPool()
	poolMu.Lock()
	live := liveModelSet(p)
	poolMu.Unlock()
	out := make([]string, 0, len(chain))
	var dropped []string
	for _, m := range chain {
		if m == "" || live[m] {
			out = append(out, m)
		} else {
			dropped = append(dropped, m)
		}
	}
	if len(dropped) == 0 || len(out) == 0 {
		return chain
	}
	log.Printf("  model chain: dropped delisted model(s) %v -> %v", dropped, out)
	return out
}

// hasAnyFallbackLeft 判断点名模型之后是否还有候选（决定 500 是否透传）。
func hasAnyFallbackLeft(requested, current string) bool {
	chain := modelFallbackChain(requested)
	for i, m := range chain {
		if m == current {
			return i < len(chain)-1
		}
	}
	return false
}

// modelFallbackChain 显式模型的降级序列：点名模型优先，其后是管理员配置的
// 回退链（modelChain）；未配置时动态派生默认链（排除已下架模型）。均去重。
// 配置链会先经 filterStaleModels 剔除已下架条目；点名模型本身始终保留——
// 它是客户端显式要求的（可能由 zen / provider 服务，不归本仓判定）。
func modelFallbackChain(requested string) []string {
	configured := getProxyConfig().ModelChain
	if len(configured) == 0 {
		configured = defaultFreeChain()
	}
	configured = filterStaleModels(configured)
	chain := make([]string, 0, 1+len(configured))
	chain = append(chain, requested)
	for _, m := range configured {
		if m != requested {
			chain = append(chain, m)
		}
	}
	return chain
}

// hasActiveAccounts 池中是否存在 active 状态的账号。
func hasActiveAccounts() bool {
	p := loadPool()
	poolMu.Lock()
	defer poolMu.Unlock()
	for _, a := range p.Accounts {
		if a.Status == "active" {
			return true
		}
	}
	return false
}

func callFreeClineAPI(params map[string]any, stream bool) (*http.Response, *Account, error) {
	// "free" 别名的实际顺序：管理员配置的回退链优先，否则动态派生默认链。
	configured := getProxyConfig().ModelChain
	chain := configured
	if len(configured) == 0 {
		chain = defaultFreeChain()
	}
	// 先剔除已下架模型，再按可用性重排：把流量摊到用量最少的可用模型上，
	// 而不是顺序打满第一个，也不在必败的死模型上浪费一次上游往返。
	chain = filterStaleModels(chain)
	chain = sortModelsByAvailability(chain)
	delete(params, servedByParam) // 本次调用重新归因
	var lastErr error
	lastWas429 := false
	for _, model := range chain {
		params["model"] = model
		// 与显式模型链一致：链内模型命中自定义 provider 时先走 provider，
		// 失败（记冷却）后继续用账号池兜底——free 别名不再绕过 provider。
		if pResp, pErr, attempted := callCustomProviderAPI(params, stream); attempted {
			if pErr == nil {
				params[servedByParam] = upstreamProvider
				return pResp, nil, nil
			}
			log.Printf("  provider attempt failed for %q: %v", model, pErr)
		}
		// "free" 链是纯降级路径：全部用「最久未用优先」挑账号，摊平用量。
		for {
			acc := pickAccountForModelLeastUsed(model)
			if acc == nil {
				break
			}

			resp, usedAcc, err := callClineAPIWithAccountCtx(context.Background(), acc, params, stream)
			if err == nil {
				return resp, usedAcc, nil
			}
			var accountErr *clineAccountUnavailableError
			if errors.As(err, &accountErr) {
				continue
			}
			lastErr = err
			lastWas429 = false
			if apiErr, ok := err.(*clineAPIError); ok && apiErr.statusCode == http.StatusTooManyRequests {
				// 429 限流：换下一个账号继续试当前模型
				lastWas429 = true
				continue
			}
			// 其他 API 错误（如上游 500）：换链内下一个模型降级
			break
		}
	}
	if lastErr != nil {
		if lastWas429 {
			// 整条链被限流耗尽：按 free 池不可用处理（429）
			return nil, nil, &freeModelUnavailableError{message: "no eligible accounts available for free models"}
		}
		return nil, nil, lastErr
	}
	return nil, nil, &freeModelUnavailableError{message: "no eligible accounts available for free models"}
}

func callClineAPIWithAccount(acc *Account, params map[string]any, stream bool) (*http.Response, *Account, error) {
	return callClineAPIWithAccountCtx(context.Background(), acc, params, stream)
}

func callClineAPIWithAccountCtx(ctx context.Context, acc *Account, params map[string]any, stream bool) (*http.Response, *Account, error) {
	token, err := ensureAccountToken(acc)
	if err != nil {
		// Try other accounts
		return nil, acc, &clineAccountUnavailableError{err: fmt.Errorf("account %s token failed: %w", acc.Email, err)}
	}

	body := buildUpstreamBody(params, stream)
	sessionID, _ := body["session_id"].(string)

	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return nil, acc, fmt.Errorf("marshal body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", clineAPIBase+"/chat/completions", bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, acc, fmt.Errorf("create request: %w", err)
	}
	req.Header = clineHeaders(token, sessionID)

	toolCount := 0
	if tools, ok := params["tools"]; ok {
		if t, ok := tools.([]any); ok {
			toolCount = len(t)
		}
	}
	log.Printf("  upstream: account=%s stream=%v tools=%d msgs=%d max_tokens=%v effort=%v",
		truncateEmail(acc.Email), stream, toolCount, getMsgCount(params), body["max_tokens"], body["reasoning_effort"])

	resp, err := httpClient.Do(req)
	if err != nil {
		acc.Status = "cooldown"
		acc.CooldownUntil = time.Now().Add(5 * time.Minute)
		savePool()
		return nil, acc, &clineAccountUnavailableError{err: fmt.Errorf("upstream request: %w", err)}
	}

	if resp.StatusCode == 401 {
		resp.Body.Close()
		// Refresh token and retry
		if err := refreshAccountToken(acc); err == nil {
			token = acc.AccessToken
			req.Header = clineHeaders(token, sessionID)
			req.Body = io.NopCloser(bytes.NewReader(bodyJSON))
			resp, err = httpClient.Do(req)
			if err != nil {
				acc.Status = "cooldown"
				acc.CooldownUntil = time.Now().Add(5 * time.Minute)
				savePool()
				return nil, acc, &clineAccountUnavailableError{err: fmt.Errorf("upstream retry: %w", err)}
			}
			if resp.StatusCode == 401 {
				resp.Body.Close()
				acc.Status = "expired"
				savePool()
				return nil, acc, &clineAccountUnavailableError{err: fmt.Errorf("account %s token expired permanently", acc.Email)}
			}
		} else {
			acc.Status = "expired"
			savePool()
			return nil, acc, &clineAccountUnavailableError{err: fmt.Errorf("account %s refresh failed: %w", acc.Email, err)}
		}
	}

	if resp.StatusCode != 200 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		bodyStr := string(bodyBytes)
		// 429：模型级冷却 —— 只暂停该模型，账号保持可用，其他模型继续转发
		if resp.StatusCode == 429 {
			model, _ := body["model"].(string)
			until := parseCooldownUntil(bodyStr)
			if model != "" {
				setModelCooldown(acc, model, until)
			} else {
				acc.Status = "cooldown"
				acc.CooldownUntil = until
				savePool()
			}
		}
		return nil, acc, &clineAPIError{statusCode: resp.StatusCode, message: truncate(bodyStr, 500)}
	}

	acc.LastUsed = time.Now()
	acc.UsageCount++
	savePool()
	return resp, acc, nil
}

type accountTestResult struct {
	AccountID    string `json:"accountId"`
	Email        string `json:"email"`
	OK           bool   `json:"ok"`
	DurationMs   int64  `json:"durationMs"`
	InputTokens  int64  `json:"inputTokens"`
	OutputTokens int64  `json:"outputTokens"`
	Error        string `json:"error,omitempty"`
}

// parseCooldownUntil 从 429 响应体中解析 "Try again in 1h 1m" 格式的等待时长，
// 返回预计恢复时间；解析失败则回退到 1 小时后。
var cooldownRe = regexp.MustCompile(`(?i)try\s+again\s+in\s+(\d+)\s*h?(?:\s*(\d+))?\s*m?`)

func parseCooldownUntil(body string) time.Time {
	matches := cooldownRe.FindStringSubmatch(body)
	if len(matches) >= 2 {
		hours, _ := strconv.Atoi(matches[1])
		minutes := 0
		if len(matches) >= 3 && matches[2] != "" {
			minutes, _ = strconv.Atoi(matches[2])
		}
		if hours > 0 || minutes > 0 {
			return time.Now().Add(time.Duration(hours)*time.Hour + time.Duration(minutes)*time.Minute)
		}
	}
	// 解析失败，回退 1 小时
	return time.Now().Add(1 * time.Hour)
}

// startCooldownRecovery 启动后台 goroutine，每 30 秒检查一次 cooldown 账号，
// 对 CooldownUntil 已过期的账号执行探活，成功则自动激活。
func startCooldownRecovery() {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			p := loadPool()
			poolMu.Lock()
			var toRecover []*Account
			for _, acc := range p.Accounts {
				if acc.Status != "cooldown" {
					continue
				}
				// 有恢复时间且已过期 → 探活
				// 无恢复时间（旧数据）→ 也尝试探活
				if acc.CooldownUntil.IsZero() || time.Now().After(acc.CooldownUntil) {
					toRecover = append(toRecover, acc)
				}
			}
			poolMu.Unlock()

			for _, acc := range toRecover {
				log.Printf("cooldown recovery: testing %s", acc.Email)
				result := testAccount(acc)
				if result.OK {
					log.Printf("cooldown recovery: %s reactivated", acc.Email)
				} else {
					log.Printf("cooldown recovery: %s still unavailable: %s", acc.Email, result.Error)
				}
			}
		}
	}()
}

// testAccount sends a minimal "hi" request through a specific account to verify
// it can complete an upstream call. It does not update aggregate token counters
// or request logs; it is a diagnostic-only probe.
func testAccount(acc *Account) accountTestResult {
	result := accountTestResult{AccountID: acc.AccountID, Email: acc.Email}
	started := time.Now()

	params := map[string]any{
		"model":      getDefaultModel(),
		"max_tokens": 16,
		"stream":     false,
		"messages": []any{
			map[string]any{"role": "user", "content": "hi"},
		},
	}

	resp, _, err := callClineAPIWithAccount(acc, params, false)
	if err != nil {
		result.DurationMs = time.Since(started).Milliseconds()
		result.Error = truncate(err.Error(), 200)
		return result
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		result.DurationMs = time.Since(started).Milliseconds()
		result.Error = "read response: " + truncate(err.Error(), 200)
		return result
	}

	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		result.DurationMs = time.Since(started).Milliseconds()
		result.Error = "decode response: " + truncate(err.Error(), 200)
		return result
	}
	if data, ok := obj["data"]; ok {
		if d, ok := data.(map[string]any); ok {
			obj = d
		}
	}
	obj = normalizeOpenAIResponse(obj)
	usage := parseTokenUsage(obj["usage"])

	result.OK = true
	result.DurationMs = time.Since(started).Milliseconds()
	if usage.Valid {
		result.InputTokens = usage.Prompt
		result.OutputTokens = usage.Completion
	}
	// If the account was in cooldown/expired but the test succeeded, restore it.
	if acc.Status != "active" {
		poolMu.Lock()
		acc.Status = "active"
		poolMu.Unlock()
		savePool()
	}
	return result
}

type tokenUsage struct {
	Prompt     int64
	Completion int64
	Total      int64
	Cached     int64
	Valid      bool
}

func parseTokenUsage(value any) tokenUsage {
	usage, ok := value.(map[string]any)
	if !ok {
		return tokenUsage{}
	}
	read := func(keys ...string) int64 {
		for _, key := range keys {
			if value, ok := usage[key].(float64); ok && value >= 0 {
				return int64(value)
			}
		}
		return 0
	}
	readNested := func(parent string, keys ...string) int64 {
		details, ok := usage[parent].(map[string]any)
		if !ok {
			return 0
		}
		for _, key := range keys {
			if value, ok := details[key].(float64); ok && value >= 0 {
				return int64(value)
			}
		}
		return 0
	}
	prompt := read("prompt_tokens", "input_tokens")
	completion := read("completion_tokens", "output_tokens")
	cached := int64(0)
	if nested := readNested("prompt_tokens_details", "cached_tokens"); nested > 0 {
		cached = nested
	} else if nested := readNested("input_tokens_details", "cached_tokens"); nested > 0 {
		cached = nested
	} else if v := read("cache_read_input_tokens") + read("cache_creation_input_tokens"); v > 0 {
		cached = v
	} else {
		cached = read("prompt_cache_hit_tokens", "prompt_cache_creation_tokens", "cached_tokens")
	}
	total := read("total_tokens")
	if total == 0 {
		total = prompt + completion
	}
	_, hasUsage := usage["prompt_tokens"]
	if !hasUsage {
		_, hasUsage = usage["input_tokens"]
		if !hasUsage {
			if _, hasUsage = usage["completion_tokens"]; !hasUsage {
				if _, hasUsage = usage["output_tokens"]; !hasUsage {
					_, hasUsage = usage["total_tokens"]
				}
			}
		}
	}
	if !hasUsage {
		_, hasUsage = usage["cache_read_input_tokens"]
		if !hasUsage {
			_, hasUsage = usage["cache_creation_input_tokens"]
			if !hasUsage {
				_, hasUsage = usage["prompt_tokens_details"]
				if !hasUsage {
					_, hasUsage = usage["input_tokens_details"]
				}
			}
		}
	}
	return tokenUsage{Prompt: prompt, Completion: completion, Total: total, Cached: cached, Valid: hasUsage}
}

func mergeTokenUsage(current, next tokenUsage) tokenUsage {
	if !next.Valid {
		return current
	}
	if next.Prompt != 0 {
		current.Prompt = next.Prompt
	}
	if next.Completion != 0 {
		current.Completion = next.Completion
	}
	if next.Total != 0 {
		current.Total = next.Total
	}
	if next.Cached != 0 {
		current.Cached = next.Cached
	}
	current.Valid = current.Valid || next.Valid
	if current.Total == 0 && (current.Prompt != 0 || current.Completion != 0) {
		current.Total = current.Prompt + current.Completion
	}
	return current
}

func recordTokenUsage(acc *Account, model string, usage tokenUsage) {
	if acc == nil || !usage.Valid {
		return
	}
	// 先判断是否免费模型（getAllModels 会拿 poolMu，必须在持有锁之前计算）
	isFree := model != "" && isFreeModelID(model)
	poolMu.Lock()
	acc.PromptTokens += usage.Prompt
	acc.CompletionTokens += usage.Completion
	acc.TotalTokens += usage.Total
	acc.CachedTokens += usage.Cached
	// 按模型细分统计（仅记录 free 模型）
	if isFree {
		if acc.ModelStats == nil {
			acc.ModelStats = make(map[string]*ModelStat)
		}
		st := acc.ModelStats[model]
		if st == nil {
			st = &ModelStat{ModelID: model, Cost: "free"}
			acc.ModelStats[model] = st
		}
		st.UsageCount++
		st.PromptTokens += usage.Prompt
		st.CompletionTokens += usage.Completion
		st.TotalTokens += usage.Total
		st.CachedTokens += usage.Cached
	}
	poolMu.Unlock()
	savePool()
}

// isFreeModelID 判断模型是否为 free 计费（用于按模型统计和模型级冷却）。
func isFreeModelID(model string) bool {
	for _, m := range getAllModels() {
		if m.ID == model {
			return m.Cost == "free"
		}
	}
	// 未知模型：按 ID 后缀/前缀启发式判断
	return strings.HasSuffix(model, ":free") || strings.Contains(model, "/free/")
}

// modelCooldownActive 判断某账号下该模型是否处于模型级冷却中。
func modelCooldownActive(acc *Account, model string) bool {
	if acc == nil || model == "" {
		return false
	}
	poolMu.Lock()
	defer poolMu.Unlock()
	until, ok := acc.ModelCooldowns[model]
	if !ok {
		return false
	}
	if time.Now().After(until) {
		delete(acc.ModelCooldowns, model)
		savePool()
		return false
	}
	return true
}

// hasAlternativeModel 判断降级链上是否还有未冷却的候选模型。
// 用于避免「所有模型都被冷却」导致整条链不可用。
func hasAlternativeModel(acc *Account, current string) bool {
	for _, m := range modelFallbackChain(current) {
		if m == "" || m == current {
			continue
		}
		if !modelCooldownActive(acc, m) {
			return true
		}
	}
	return false
}

// setModelCooldown 记录模型级冷却（429 或首 token 前失败时调用）：只暂停该模型，账号保持可用。
// fallback 为解析失败时的恢复时长（默认 1 小时）。
func setModelCooldown(acc *Account, model string, until time.Time) {
	if acc == nil || model == "" {
		return
	}
	poolMu.Lock()
	if acc.ModelCooldowns == nil {
		acc.ModelCooldowns = make(map[string]time.Time)
	}
	acc.ModelCooldowns[model] = until
	poolMu.Unlock()
	savePool()
	log.Printf("model cooldown: account=%s model=%s until=%s", truncateEmail(acc.Email), model, until.Format("15:04:05"))
}

func truncateEmail(email string) string {
	if len(email) <= 12 {
		return email
	}
	parts := splitEmail(email)
	if len(parts) == 2 && len(parts[0]) > 3 {
		return parts[0][:3] + "***@" + parts[1]
	}
	if len(email) > 12 {
		return email[:8] + "..."
	}
	return email
}

func splitEmail(email string) []string {
	for i := 0; i < len(email); i++ {
		if email[i] == '@' {
			return []string{email[:i], email[i+1:]}
		}
	}
	return []string{email}
}

func getMsgCount(params map[string]any) int {
	if msgs, ok := params["messages"].([]any); ok {
		return len(msgs)
	}
	return 0
}

func handleStreamResponse(w http.ResponseWriter, upstream *http.Response, acc *Account, reqLog *RequestLog) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		log.Printf("  streaming not supported for client")
		return
	}

	reader := bufio.NewReader(upstream.Body)
	var latestUsage tokenUsage
	var firstOutputAt time.Time
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				if line != "" {
					w.Write([]byte(line + "\n"))
				}
			} else {
				// 记录断流原因（超时/重置），不再无声吞掉
				log.Printf("  chat stream broken: %v", err)
			}
			break
		}

		line = strings.TrimRight(line, "\r\n")

		if strings.HasPrefix(line, "data:") {
			payload := strings.TrimSpace(line[5:])
			if payload == "" || payload == "[DONE]" {
				w.Write([]byte(line + "\n\n"))
				flusher.Flush()
				continue
			}

			// Try to normalize the response
			var obj map[string]any
			if err := json.Unmarshal([]byte(payload), &obj); err == nil {
				// Some Cline responses wrap in {data: {...}}
				if data, ok := obj["data"]; ok {
					if d, ok := data.(map[string]any); ok {
						if _, hasChoices := d["choices"]; hasChoices {
							obj = d
						}
						if _, hasID := d["id"]; hasID {
							obj = d
						}
					}
				}
				normalized := normalizeOpenAIResponse(obj)
				if usage := parseTokenUsage(normalized["usage"]); usage.Valid {
					latestUsage = mergeTokenUsage(latestUsage, usage)
				}
				if firstOutputAt.IsZero() && hasFirstOutput(normalized) {
					firstOutputAt = time.Now()
				}
				if normBytes, err := json.Marshal(normalized); err == nil {
					w.Write([]byte("data: " + string(normBytes) + "\n\n"))
					flusher.Flush()
					continue
				}
			}
		}

		w.Write([]byte(line + "\n"))
		flusher.Flush()
	}
	recordTokenUsage(acc, reqLog.Model, latestUsage)
	finalizeRequestLog(reqLog, latestUsage, firstOutputAt, reqLog.StartedAt, true, "")
}

func hasFirstOutput(obj map[string]any) bool {
	choices, ok := getNested(obj, "choices").([]any)
	if !ok || len(choices) == 0 {
		return false
	}
	choice, _ := choices[0].(map[string]any)
	if choice == nil {
		return false
	}
	if delta, ok := choice["delta"].(map[string]any); ok {
		if c, _ := delta["content"].(string); c != "" {
			return true
		}
		if tc, ok := delta["tool_calls"].([]any); ok && len(tc) > 0 {
			return true
		}
	}
	if msg, ok := choice["message"].(map[string]any); ok {
		if c, _ := msg["content"].(string); c != "" {
			return true
		}
		if tc, ok := msg["tool_calls"].([]any); ok && len(tc) > 0 {
			return true
		}
	}
	return false
}

func handleNonStreamResponse(w http.ResponseWriter, upstream *http.Response, acc *Account, reqLog *RequestLog) {
	var raw map[string]any
	if err := json.NewDecoder(upstream.Body).Decode(&raw); err != nil {
		finalizeRequestLog(reqLog, tokenUsage{}, time.Time{}, reqLog.StartedAt, false, "decode response: "+err.Error())
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": map[string]string{"message": err.Error(), "type": "parse_error"},
		})
		return
	}

	// Some Cline responses wrap in {data: {...}}
	out := raw
	if data, ok := raw["data"]; ok {
		if d, ok := data.(map[string]any); ok {
			out = d
		}
	}

	out = normalizeOpenAIResponse(out)
	usage := parseTokenUsage(out["usage"])
	recordTokenUsage(acc, reqLog.Model, usage)
	finalizeRequestLog(reqLog, usage, time.Time{}, reqLog.StartedAt, true, "")

	if msg, ok := getNested(out, "choices", 0, "message").(map[string]any); ok {
		tc, _ := msg["tool_calls"].([]any)
		content, _ := msg["content"].(string)
		log.Printf("  nonstream finish=%v tool_calls=%d content_len=%d",
			getNested(out, "choices", 0, "finish_reason"),
			len(tc), len(content))
	}

	writeJSON(w, http.StatusOK, out)
}

// Anthropic Messages API support
type anthropicMsg struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type toolAccumulator struct {
	index   int
	id      string
	name    string
	args    string
	emitted bool
}

type anthropicReq struct {
	Model       string          `json:"model"`
	MaxTokens   int             `json:"max_tokens"`
	Messages    []anthropicMsg  `json:"messages"`
	System      json.RawMessage `json:"system,omitempty"`
	Stream      bool            `json:"stream,omitempty"`
	Temperature float64         `json:"temperature,omitempty"`
	TopP        float64         `json:"top_p,omitempty"`
	TopK        int             `json:"top_k,omitempty"`
	Stop        json.RawMessage `json:"stop_sequences,omitempty"`
	Thinking    json.RawMessage `json:"thinking,omitempty"`
	Tools       json.RawMessage `json:"tools,omitempty"`
	ToolChoice  json.RawMessage `json:"tool_choice,omitempty"`
	Metadata    json.RawMessage `json:"metadata,omitempty"`
	Extra       map[string]any  `json:"-"`
}

func loadOverrideContent() string {
	data, err := os.ReadFile("override.md")
	if err != nil {
		log.Printf("  override.md not found: %v", err)
		return ""
	}
	content := strings.TrimSpace(string(data))
	if content != "" {
		log.Printf("  using override.md as system prompt (%d bytes)", len(content))
	} else {
		log.Printf("  override.md is empty")
	}
	return content
}

func extractStringContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	// Try string first
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	// Try array of content blocks
	var blocks []map[string]any
	if err := json.Unmarshal(raw, &blocks); err == nil {
		parts := []string{}
		for _, b := range blocks {
			if b["type"] == "text" {
				if t, ok := b["text"].(string); ok {
					parts = append(parts, t)
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

func anthropicToolsToOpenAI(tools []any) []any {
	out := make([]any, 0, len(tools))
	for _, t := range tools {
		if tMap, ok := t.(map[string]any); ok {
			// Already in OpenAI format
			if tMap["type"] == "function" {
				out = append(out, t)
				continue
			}
			// Convert Anthropic format to OpenAI
			oai := map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        tMap["name"],
					"description": tMap["description"],
					"parameters":  tMap["input_schema"],
				},
			}
			out = append(out, oai)
		}
	}
	return out
}

func anthropicToOpenAI(req anthropicReq) map[string]any {
	openAI := map[string]any{
		"model":      req.Model,
		"max_tokens": req.MaxTokens,
		"stream":     req.Stream,
		"messages":   []any{},
	}
	if req.Temperature != 0 {
		openAI["temperature"] = req.Temperature
	}
	if req.TopP != 0 {
		openAI["top_p"] = req.TopP
	}
	// Anthropic thinking 参数映射为 OpenAI reasoning_effort：
	// disabled → "none"（buildUpstreamBody 会删除，不下发给 Cline 上游），
	// enabled/adaptive → 默认 "high"。
	if req.Thinking != nil {
		var thinking struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(req.Thinking, &thinking); err == nil {
			switch thinking.Type {
			case "enabled", "adaptive":
				openAI["reasoning_effort"] = reasoningEffort()
			case "disabled":
				openAI["reasoning_effort"] = "none"
			}
		}
	}
	// Convert Anthropic tools to OpenAI format
	if req.Tools != nil {
		var toolsArr []any
		if err := json.Unmarshal(req.Tools, &toolsArr); err == nil {
			openAI["tools"] = anthropicToolsToOpenAI(toolsArr)
		}
	}
	if req.ToolChoice != nil {
		openAI["tool_choice"] = req.ToolChoice
	}

	msgs := []any{}

	// System prompt: use override.md if it exists, otherwise use Anthropic's system field
	sysContent := loadOverrideContent()
	if sysContent == "" && req.System != nil {
		sysContent = extractStringContent(req.System)
	}
	if sysContent != "" {
		log.Printf("  system prompt: %d bytes (from override.md)", len(sysContent))
		msgs = append(msgs, map[string]any{"role": "system", "content": sysContent})
	}

	for _, m := range req.Messages {
		switch c := m.Content.(type) {
		case string:
			msgs = append(msgs, map[string]any{"role": m.Role, "content": c})
		case []any:
			textParts := []string{}
			var toolCalls []any
			var toolResults []any

			for _, block := range c {
				if b, ok := block.(map[string]any); ok {
					switch b["type"] {
					case "text":
						if t, ok := b["text"].(string); ok {
							textParts = append(textParts, t)
						}
					case "image":
						// skip images
					case "tool_use":
						// 跳过上游输出的空名 tool_use（畸形 tool call），避免污染历史导致后续 400
						if name, _ := b["name"].(string); name == "" {
							continue
						}
						argsStr := "{}"
						if input, ok := b["input"]; ok && input != nil {
							if s, ok := input.(string); ok {
								argsStr = s
							} else if bts, err := json.Marshal(input); err == nil {
								argsStr = string(bts)
							}
						}
						id, _ := b["id"].(string)
						if id == "" {
							id = genToolUseID()
						}
						tc := map[string]any{
							"id":   id,
							"type": "function",
							"function": map[string]any{
								"name":      b["name"],
								"arguments": argsStr,
							},
						}
						toolCalls = append(toolCalls, tc)
					case "tool_result":
						trContent := b["content"]
						if _, isStr := trContent.(string); !isStr && trContent != nil {
							if bts, err := json.Marshal(trContent); err == nil {
								trContent = string(bts)
							}
						}
						tr := map[string]any{
							"role":         "tool",
							"content":      trContent,
							"tool_call_id": b["tool_use_id"],
						}
						toolResults = append(toolResults, tr)
					}
				}
			}

			if m.Role == "assistant" && len(toolCalls) > 0 {
				msg := map[string]any{
					"role":       "assistant",
					"content":    strings.Join(textParts, "\n"),
					"tool_calls": toolCalls,
				}
				msgs = append(msgs, msg)
			} else if m.Role == "user" && len(toolResults) > 0 {
				for _, tr := range toolResults {
					msgs = append(msgs, tr)
				}
				if len(textParts) > 0 {
					msgs = append(msgs, map[string]any{"role": "user", "content": strings.Join(textParts, "\n")})
				}
			} else {
				content := strings.Join(textParts, "\n")
				msgs = append(msgs, map[string]any{"role": m.Role, "content": content})
			}
		}
	}

	openAI["messages"] = msgs
	return openAI
}

func openAIToAnthropic(openAI map[string]any) map[string]any {
	out := map[string]any{
		"id":    "msg_" + fmt.Sprintf("%x", time.Now().UnixMilli()),
		"type":  "message",
		"role":  "assistant",
		"model": getNested(openAI, "model"),
	}

	choices := getNested(openAI, "choices")
	if choices == nil {
		out["content"] = []any{map[string]any{"type": "text", "text": ""}}
		out["stop_reason"] = "end_turn"
		out["usage"] = map[string]any{"input_tokens": 0, "output_tokens": 0}
		return out
	}

	choice0 := getNested(openAI, "choices", 0).(map[string]any)
	msg, _ := choice0["message"].(map[string]any)
	if msg == nil {
		msg, _ = choice0["delta"].(map[string]any)
	}

	text := ""
	if msg != nil {
		if c, ok := msg["content"].(string); ok {
			text = sanitizeContent(c)
		}
	}

	// 上游 reasoning_content → Anthropic thinking 块（顺序：thinking 在 text 前）
	var thinkingBlock map[string]any
	if msg != nil {
		if rc, ok := msg["reasoning_content"].(string); ok && rc != "" {
			thinkingBlock = map[string]any{"type": "thinking", "thinking": rc, "signature": ""}
		}
	}

	var contentBlocks []any
	if thinkingBlock != nil {
		contentBlocks = append(contentBlocks, thinkingBlock)
	}
	if text != "" || thinkingBlock == nil {
		contentBlocks = append(contentBlocks, map[string]any{"type": "text", "text": text})
	}

	// Convert tool_calls to Anthropic tool_use blocks
	if msg != nil {
		if tc, ok := msg["tool_calls"].([]any); ok && len(tc) > 0 {
			contentBlocks = []any{} // Clear text-only, proper response has both
			if thinkingBlock != nil {
				contentBlocks = append(contentBlocks, thinkingBlock)
			}
			if text != "" {
				contentBlocks = append(contentBlocks, map[string]any{"type": "text", "text": text})
			}
			for _, tcItem := range tc {
				if tcMap, ok := tcItem.(map[string]any); ok {
					funcData, _ := tcMap["function"].(map[string]any)
					// 跳过空名 tool_call（畸形工具调用），避免客户端收到无法执行的空 tool_use 块
					if name, _ := funcData["name"].(string); name == "" {
						continue
					}
					input := funcData["arguments"]
					// OpenAI arguments is a JSON string; Anthropic expects an object
					if argsStr, ok := input.(string); ok {
						var argsObj any
						if json.Unmarshal([]byte(argsStr), &argsObj) == nil {
							input = argsObj
						}
					}
					id, _ := tcMap["id"].(string)
					if id == "" {
						id = genToolUseID()
					}
					block := map[string]any{
						"type":  "tool_use",
						"id":    id,
						"name":  funcData["name"],
						"input": input,
					}
					contentBlocks = append(contentBlocks, block)
				}
			}
		}
	}

	out["content"] = contentBlocks

	switch getNested(openAI, "choices", 0, "finish_reason") {
	case "stop":
		out["stop_reason"] = "end_turn"
	case "length":
		out["stop_reason"] = "max_tokens"
	case "tool_calls":
		out["stop_reason"] = "tool_use"
	default:
		out["stop_reason"] = "end_turn"
	}

	usage := map[string]any{}
	if u := getNested(openAI, "usage"); u != nil {
		if um, ok := u.(map[string]any); ok {
			usage["input_tokens"] = um["prompt_tokens"]
			usage["output_tokens"] = um["completion_tokens"]
		}
	}
	out["usage"] = usage

	return out
}

// zenFailoverToCline zen 调用失败后的透明降级：改走 cline 账号池 free 模型链。
// attempted=false 表示未启用故障转移，调用方维持原错误路径。
// 注意：失败计数由 callZenAPI 内部标记（每请求恰好一次），此处不再重复计数，
// 否则限流/服务错误路径 + 此处各计一次，故障转移会被过早触发。
func zenFailoverToCline(params map[string]any, stream bool) (*http.Response, *Account, error, bool) {
	cfg := getZenConfig()
	if !cfg.Failover {
		return nil, nil, nil, false
	}
	orig, _ := params["model"].(string)
	log.Printf("  zen failover: %q unavailable upstream, falling back to cline free pool", orig)
	params["model"] = "free"
	resp, acc, err := callFreeClineAPI(params, stream)
	return resp, acc, err, true
}

// clineFailoverToZen Cline 侧（含 free 池链）全部耗尽后的反向故障转移：
// 落到 opencode zen 免费模型。与 zenFailoverToCline 方向相反，形成双向闭环——
// 任一免费上游挂掉，流量自动落到另一条。zen 未启用或处于故障转移窗口
// （连续失败被判定不可达）时不尝试；最多试 3 个免费模型，避免在坏模型上反复烧时间。
func clineFailoverToZen(params map[string]any, stream bool) (*http.Response, bool) {
	cfg := getZenConfig()
	if !cfg.Enabled || zenFailedNow() {
		return nil, false
	}
	orig, _ := params["model"].(string)
	attempts := 0
	for _, m := range currentZenModels() {
		if !isZenFreeModel(m) || m.ID == orig {
			continue
		}
		attempts++
		if attempts > 3 {
			break
		}
		log.Printf("  cline failover: %q exhausted, trying zen free model %q", orig, m.ID)
		params["model"] = m.ID
		resp, err := callZenAPI(params, stream)
		if err == nil {
			return resp, true
		}
		log.Printf("  cline failover: zen model %q failed: %v", m.ID, err)
	}
	return nil, false
}

func handleAnthropicMessages(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{"message": err.Error(), "type": "parse_error"},
		})
		return
	}

	var req anthropicReq
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{"message": err.Error(), "type": "parse_error"},
		})
		return
	}

	if len(req.Messages) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{"message": "messages is required", "type": "parse_error"},
		})
		return
	}

	req.MaxTokens = clampMaxTokens(req.MaxTokens)

	openAIReq := anthropicToOpenAI(req)

	log.Printf("  anthropic: model=%s stream=%v msgs=%d", req.Model, req.Stream, len(req.Messages))

	reqLog := RequestLog{StartedAt: time.Now(), Protocol: "anthropic", Model: req.Model, Stream: req.Stream}

	// 按 model 自动分流（与 chat 端点一致）：zen 免费/付费拒绝/Cline 池
	switch routeModel(req.Model) {
	case "reject":
		msg := fmt.Sprintf("model %q is a paid opencode model; only free models are proxied", req.Model)
		finalizeRequestLog(&reqLog, tokenUsage{}, time.Time{}, reqLog.StartedAt, false, msg)
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{"message": msg, "type": "invalid_request_error"},
		})
		return
	case "zen":
		reqLog.Upstream = upstreamOpenCode
		zm, _ := resolveZenInfo(req.Model)
		out := maybeCompact(openAIReq, zm, requestSessionID(map[string]any{"session_id": r.Header.Get("x-opencode-session")}, nil))
		if out.changed {
			log.Printf("  anthropic %s", out.note)
		}
		resp, err := callZenAPI(openAIReq, req.Stream)
		if err != nil {
			if fbResp, fbAcc, fbErr, attempted := zenFailoverToCline(openAIReq, req.Stream); attempted {
				if fbErr == nil {
					log.Printf("  anthropic failover: serving %q via cline pool", req.Model)
					stampUpstream(&reqLog, openAIReq) // 归因实际服务方（provider / cline）
					if fm, ok := openAIReq["model"].(string); ok && fm != "" {
						reqLog.Model = fm // zen 故障转移后记录实际服务模型
					}
					if fbAcc != nil {
						reqLog.AccountID = fbAcc.AccountID
						reqLog.AccountEmail = fbAcc.Email
					}
					defer fbResp.Body.Close()
					if req.Stream {
						sw := newSSEWriter(w)
						// 这条降级路径没有重试，所以用宽容的上限，尽量把慢但能出的流交给客户端。
						if !handleAnthropicStream(r.Context(), sw, fbResp, fbAcc, &reqLog, lastAttemptFirstContentTimeout) {
							log.Printf("  anthropic stream: failover upstream failed before any content")
							finishAnthropicStreamFailure(sw, w, &reqLog, http.StatusBadGateway,
								"stream failed before first token", "upstream stream failed before first token")
						}
					} else {
						var raw map[string]any
						if err := json.NewDecoder(fbResp.Body).Decode(&raw); err != nil {
							finalizeRequestLog(&reqLog, tokenUsage{}, time.Time{}, reqLog.StartedAt, false, "decode response: "+err.Error())
							writeJSON(w, http.StatusInternalServerError, map[string]any{
								"error": map[string]string{"message": err.Error(), "type": "parse_error"},
							})
							return
						}
						out2 := normalizeOpenAIResponse(unwrapDataEnvelope(raw))
						usage := parseTokenUsage(out2["usage"])
						recordTokenUsage(fbAcc, reqLog.Model, usage)
						finalizeRequestLog(&reqLog, usage, time.Time{}, reqLog.StartedAt, true, "")
						anthropicResp := openAIToAnthropic(out2)
						if hasToolUseBlocks(anthropicResp["content"]) {
							anthropicResp["stop_reason"] = "tool_use"
						}
						writeJSON(w, http.StatusOK, anthropicResp)
					}
					return
				}
				err = fbErr
			}
			log.Printf("  anthropic api error: %v", err)
			finalizeRequestLog(&reqLog, tokenUsage{}, time.Time{}, reqLog.StartedAt, false, err.Error())
			writeJSON(w, http.StatusBadGateway, map[string]any{
				"error": map[string]string{"message": err.Error(), "type": "api_error"},
			})
			return
		}
		defer resp.Body.Close()
		if req.Stream {
			sw := newSSEWriter(w)
			// 同样是单次尝试（无重试），用宽容的上限。
			if !handleAnthropicStream(r.Context(), sw, resp, nil, &reqLog, lastAttemptFirstContentTimeout) {
				log.Printf("  anthropic stream: zen upstream failed before any content")
				finishAnthropicStreamFailure(sw, w, &reqLog, http.StatusBadGateway,
					"stream failed before first token", "zen stream failed before first token")
			}
		} else {
			var raw map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
				finalizeRequestLog(&reqLog, tokenUsage{}, time.Time{}, reqLog.StartedAt, false, "decode response: "+err.Error())
				writeJSON(w, http.StatusInternalServerError, map[string]any{
					"error": map[string]string{"message": err.Error(), "type": "parse_error"},
				})
				return
			}
			out2 := normalizeOpenAIResponse(unwrapDataEnvelope(raw))
			usage := parseTokenUsage(out2["usage"])
			finalizeRequestLog(&reqLog, usage, time.Time{}, reqLog.StartedAt, true, "")
			anthropicResp := openAIToAnthropic(out2)
			if hasToolUseBlocks(anthropicResp["content"]) {
				anthropicResp["stop_reason"] = "tool_use"
			}
			writeJSON(w, http.StatusOK, anthropicResp)
		}
		return
	}

	activeCount := 0
	p := loadPool()
	for _, a := range p.Accounts {
		if a.Status == "active" {
			activeCount++
		}
	}

	if activeCount == 0 && len(p.Accounts) == 0 {
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error": map[string]string{
				"message": "No accounts in pool",
				"type":    "auth_error",
			},
		})
		return
	}

	if req.Stream {
		streamAnthropicWithRetry(r.Context(), w, func() (*http.Response, *Account, error) {
			resp, acc, err := callClineAPI(openAIReq, req.Stream)
			if err == nil {
				if effectiveModel, ok := openAIReq["model"].(string); ok && effectiveModel != "" {
					reqLog.Model = effectiveModel // 含回退后的实际服务模型
				}
				stampUpstream(&reqLog, openAIReq) // 归因实际服务方（provider / opencode / cline）
			}
			return resp, acc, err
		}, &reqLog)
		return
	}

	resp, acc, err := callClineAPI(openAIReq, req.Stream)
	if effectiveModel, ok := openAIReq["model"].(string); ok && effectiveModel != "" {
		reqLog.Model = effectiveModel // 含回退后的实际服务模型
	}
	stampUpstream(&reqLog, openAIReq) // 归因实际服务方（provider / opencode / cline）
	if err != nil {
		log.Printf("  anthropic api error: %v", err)
		finalizeRequestLog(&reqLog, tokenUsage{}, time.Time{}, reqLog.StartedAt, false, err.Error())
		writeJSON(w, clineErrorHTTPStatus(err), map[string]any{
			"error": map[string]string{"message": err.Error(), "type": "api_error"},
		})
		return
	}
	defer resp.Body.Close()
	if acc != nil {
		reqLog.AccountID = acc.AccountID
		reqLog.AccountEmail = acc.Email
	}

	{
		var raw map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
			finalizeRequestLog(&reqLog, tokenUsage{}, time.Time{}, reqLog.StartedAt, false, "decode response: "+err.Error())
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"error": map[string]string{"message": err.Error(), "type": "parse_error"},
			})
			return
		}
		out := raw
		if data, ok := raw["data"]; ok {
			if d, ok := data.(map[string]any); ok {
				out = d
			}
		}
		out = normalizeOpenAIResponse(out)
		usage := parseTokenUsage(out["usage"])
		recordTokenUsage(acc, reqLog.Model, usage)
		finalizeRequestLog(&reqLog, usage, time.Time{}, reqLog.StartedAt, true, "")
		anthropicResp := openAIToAnthropic(out)

		if hasToolUseBlocks(anthropicResp["content"]) {
			anthropicResp["stop_reason"] = "tool_use"
		}

		writeJSON(w, http.StatusOK, anthropicResp)
	}
}

// maxStreamAttempts 预提交失败的最大尝试次数（含首次）。实际次数还会被降级链长度限制：
// 3 次 × 短耐心，总等待被压在客户端可接受范围内，而不是在整条链上把时间耗光。
// 链上有几个模型就最多试几个，避免在同一个模型上反复空等。
const maxStreamAttempts = 3

// firstContentTimeoutForAttempt 返回本次尝试等待「首个内容事件」的上限：
// 普通尝试用较短的上限快速换模型；最后一次尝试放宽，宁可慢也尽量把答案交出去。
func firstContentTimeoutForAttempt(attempt, maxAttempts int) time.Duration {
	if attempt >= maxAttempts {
		return envDurationFromMs("CLINE2API_LAST_ATTEMPT_TIMEOUT_MS", lastAttemptFirstContentTimeout)
	}
	return envDurationFromMs("CLINE2API_FIRST_CONTENT_TIMEOUT_MS", defaultFirstContentTimeout)
}

// streamFailoverCooldown 是「上游在产出任何内容前失败」时对该模型施加的冷却时长。
// 目的：点名模型（如 glm-5.3-flash）排队超时 / 空闲 504 / 早断流时，同一次请求内
// 立即换到配置链上的下一个模型，而不是把错误抛给客户端；同时短期内不再把新请求压到它上面。
const streamFailoverCooldown = 2 * time.Minute

// streamAnthropicWithRetry 获取上游响应并转发 Anthropic 流。
//
// 首 token 之前失败（预提交）可以透明重试：客户端只看到已发出的信封与 ping，
// 看不到重试痕迹。每次失败还会把该模型短时冷却，使后续尝试自动切换到配置链上的
// 下一个模型 —— 这是「点名模型老是超时」的缓解：坏模型自动让位给备用模型。
// 预提交失败（上游在任何数据产出前断开/报错/静默超时）时客户端尚未收到任何字节，
// 自动重新获取并重试，对客户端完全透明 —— 这是修复「流式经常断」的核心：
// 排队/思考阶段的 504 idle timeout 以前会直接杀死流，现在变成一次不可见的重试。
// sseWriter 包装 http.ResponseWriter，让预提交重试共享同一个响应：
//   - start() 幂等，重试时不会重复 WriteHeader（Go 会报 superfluous 警告）；
//   - write() 每次写完立即 Flush，保证中间层（Cloudflare 隧道等）及时看到字节。
//
// 同时用 started 记录「是否已有字节发出」，供调用方决定重试用 JSON 还是带内 error。
type sseWriter struct {
	w        http.ResponseWriter
	flusher  http.Flusher
	started  bool // 响应头已发出
	envelope bool // message_start 已发出（跨预提交重试只发一次）
}

func newSSEWriter(w http.ResponseWriter) *sseWriter {
	f, _ := w.(http.Flusher)
	return &sseWriter{w: w, flusher: f}
}

func (s *sseWriter) start() {
	if s.started {
		return
	}
	s.started = true
	s.w.Header().Set("Content-Type", "text/event-stream")
	s.w.Header().Set("Cache-Control", "no-cache")
	s.w.Header().Set("Connection", "keep-alive")
	s.w.Header().Set("Access-Control-Allow-Origin", "*")
	s.w.WriteHeader(http.StatusOK)
}

func (s *sseWriter) write(p []byte) {
	s.start()
	s.w.Write(p)
	if s.flusher != nil {
		s.flusher.Flush()
	}
}

// emitAnthropicErrorEvent 在响应头已发出（SSE 模式）后向客户端报告错误：
// 此时无法再返回 JSON 状态码，改用 Anthropic 协议的 error 事件。
func emitAnthropicErrorEvent(sw *sseWriter, msg string) {
	d, _ := json.Marshal(map[string]any{
		"type":  "error",
		"error": map[string]any{"type": "api_error", "message": msg},
	})
	sw.write([]byte("event: error\ndata: " + string(d) + "\n\n"))
}

// finishAnthropicStreamFailure 统一处理流式尝试失败后的报错：
// 响应头尚未发送 → JSON 状态码；已经发出 → 带内 error 事件（避免重复 WriteHeader）。
func finishAnthropicStreamFailure(sw *sseWriter, w http.ResponseWriter, reqLog *RequestLog, status int, note, msg string) {
	finalizeRequestLog(reqLog, tokenUsage{}, time.Time{}, reqLog.StartedAt, false, note)
	if sw.started {
		log.Printf("  anthropic stream: %s (reported as in-band error event)", note)
		emitAnthropicErrorEvent(sw, msg)
		return
	}
	writeJSON(w, status, map[string]any{
		"error": map[string]string{"message": msg, "type": "api_error"},
	})
}

func streamAnthropicWithRetry(ctx context.Context, w http.ResponseWriter, fetch func() (*http.Response, *Account, error), reqLog *RequestLog) {
	sw := newSSEWriter(w)
	// 尝试次数不超过降级链上的模型数：每个模型只给一次机会。
	maxAttempts := maxStreamAttempts
	if n := len(modelFallbackChain(reqLog.Model)); n > 0 && n < maxAttempts {
		maxAttempts = n
	}
	for attempt := 1; ; attempt++ {
		if ctx.Err() != nil {
			return // 客户端已断开，不再发起上游请求
		}
		resp, acc, err := fetch()
		if err != nil {
			log.Printf("  anthropic api error: %v", err)
			finishAnthropicStreamFailure(sw, w, reqLog, clineErrorHTTPStatus(err), err.Error(), err.Error())
			return
		}
		if acc != nil {
			reqLog.AccountID = acc.AccountID
			reqLog.AccountEmail = acc.Email
		}
		if handleAnthropicStream(ctx, sw, resp, acc, reqLog, firstContentTimeoutForAttempt(attempt, maxAttempts)) {
			return
		}
		resp.Body.Close()
		// 客户端主动断开（Esc 中断 / Claude Code 空闲看门狗重连）不是模型的失败：
		// 此时冷却会把用户点名/配置的模型拉黑 2 分钟，后续请求被静默赶到回退链上，
		// 表现为「配置了模型却总走 fallback」。实测见 proxy.log 2026/09/26 09:52:52 /
		// 09:59:00：「client disconnected before first token」紧跟 pixel-canary 被冷却。
		// ctx 由 r.Context() 派生，在 handler 内只有客户端断开才会取消。
		if ctx.Err() != nil {
			return
		}
		// 上游在产出任何内容前失败（排队超时 / 空闲 504 / 早断流）：把这次实际使用的模型
		// 标记为短时冷却，下一次尝试就会自动落到链上的下一个模型（pool 的选择器会跳过
		// 冷却中的模型）。有备用模型时才冷却，避免把整条链一起冷掉。
		if acc != nil && reqLog.Model != "" && hasAlternativeModel(acc, reqLog.Model) {
			setModelCooldown(acc, reqLog.Model, time.Now().Add(streamFailoverCooldown))
			log.Printf("  anthropic stream: model %q produced no content, cooling it for %s and failing over",
				reqLog.Model, streamFailoverCooldown)
		}
		if attempt >= maxAttempts {
			log.Printf("  anthropic stream: giving up after %d attempts with no content", attempt)
			finishAnthropicStreamFailure(sw, w, reqLog, http.StatusBadGateway,
				"stream failed before first token", "upstream stream failed before first token")
			return
		}
		log.Printf("  anthropic stream: upstream produced no content, retrying (%d/%d)", attempt+1, maxAttempts)
		select {
		case <-ctx.Done():
			return
		case <-time.After(300 * time.Millisecond):
		}
	}
}

// handleAnthropicStream 把上游 OpenAI SSE 流转换为 Anthropic 事件流转发给客户端。
//
// 关键设计：
//  1. 响应头 + message_start 在上游接通后立即发出。保活心跳分两段：
//     尚未出内容时只发 SSE 注释行 `: ping` —— 足以喂饱中间层（Cloudflare 隧道 120s
//     Proxy Read Timeout）的字节级读超时、避免 524，但按规范它不是事件，因此**不会**
//     喂客户端的事件级看门狗（CLAUDE_STREAM_IDLE_TIMEOUT_MS）。这是有意为之：注定失败的
//     尝试宁可让客户端自己超时后重连（用户实测「断开重连」好于长时间静默），也不要
//     一直哄着它假活着；一旦出过内容就改发真实 ping 事件，因为那时保住这一回合的产出
//     比让客户端重跑更划算。
//  2. 信封提前发送但内容不提前：只要还没向客户端发出任何内容事件（文本 / thinking /
//     具名工具调用），本次尝试的失败（排队超时、空闲 504、上游静默、早断流）都可以透明
//     重试；重试沿用同一 message_start（msgID 由请求开始时间推导），客户端只看到一条
//     连续消息。判定依据是「有无内容」而不是「有无首行」—— 上游常常先送一条空 delta
//     （仅 role）就静默数分钟，那种情况必须能换模型，否则用户只能干等到超时。
//  3. firstContentTimeout 是本次尝试等待首个内容事件的上限，超时同样是可重试的失败。
//
// 返回 true 表示客户端响应已完整处理（正常收尾或已发出 error 事件）；
// 返回 false 表示上游尚未产出任何内容 —— 调用方重试，或用 sw.started 判断后发带内 error。
func handleAnthropicStream(ctx context.Context, sw *sseWriter, upstream *http.Response, acc *Account, reqLog *RequestLog, firstContentTimeout time.Duration) bool {
	if upstream.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(upstream.Body, 2048))
		upstream.Body.Close()
		log.Printf("  anthropic stream: upstream status %d before stream: %s", upstream.StatusCode, truncate(string(b), 200))
		return false
	}
	if sw.flusher == nil {
		log.Printf("  streaming not supported for client")
		io.Copy(io.Discard, upstream.Body)
		upstream.Body.Close()
		return false
	}
	log.Printf("  anthropic stream: waiting for first token")

	// 上游 SSE 逐行泵：独立 goroutine 阻塞读取，主循环通过 select 同时处理
	// 心跳、客户端断开（ctx）与上游数据；放弃时关闭 body + pumpStop 确保不泄漏。
	type sseLine struct {
		line string
		err  error
	}
	lines := make(chan sseLine)
	pumpStop := make(chan struct{})
	go func() {
		reader := bufio.NewReader(upstream.Body)
		for {
			l, err := reader.ReadString('\n')
			select {
			case lines <- sseLine{l, err}:
			case <-pumpStop:
				return
			}
			if err != nil {
				return
			}
		}
	}()
	defer close(pumpStop)

	emit := func(event string, data any) {
		d, _ := json.Marshal(data)
		sw.write([]byte(fmt.Sprintf("event: %s\ndata: %s\n\n", event, string(d))))
	}

	// msgID 跨预提交重试保持稳定：整个请求只发一个信封，重试沿用同一条消息。
	msgID := "msg_" + fmt.Sprintf("%x", reqLog.StartedAt.UnixMilli())
	stopReason := "end_turn"
	textIndex := new(int)
	*textIndex = -1
	hasText := false
	sawNamedTool := false
	sawThinking := false
	thinkingIdx := -1
	thinkingOpen := false
	pendingTools := map[int]*toolAccumulator{}
	committed := false

	// hasContent 判断是否已向客户端发出过内容事件。为 false 时本次尝试的失败对客户端
	// 完全不可见（只发了信封与 ping），因此可以安全地冷却该模型并换链上下一个模型。
	hasContent := func() bool { return hasText || sawThinking || sawNamedTool }

	// 信封提前发送：响应头 + message_start + 首个 ping 事件。
	// 必须从第 0 秒起就有事件流动 —— 大上下文下上游排队/预填充实测 TTFT 可达 3~4 分钟，
	// 期间只有注释行（不是事件）时，Claude Code 的流空闲看门狗会把连接当成卡死。
	// 重试沿用同一信封（sw.envelope 幂等），客户端看不到任何重试痕迹。
	sw.start()
	if !sw.envelope {
		sw.envelope = true
		emit("message_start", map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id":          msgID,
				"type":        "message",
				"role":        "assistant",
				"content":     []any{},
				"model":       "",
				"stop_reason": nil,
			},
		})
	}
	// 接通即写一个注释行：从第 0 秒起就有字节流出（Cloudflare 不会 524），
	// 但客户端的事件级看门狗不受影响 —— 这段等待若是白等，客户端会按自己的节奏超时重连。
	sw.write([]byte(": ping\n\n"))

	// commit：上游首条有效数据行到达，开始实时转发（信封已提前发送）。
	// committed=true 后本次尝试不再重试，只能实时转发或发带内 error。
	commit := func() {
		committed = true
		log.Printf("  anthropic stream: starting real-time forward")
	}

	emitToolBlock := func(acc *toolAccumulator, index int) {
		acc.emitted = true
		args := acc.args
		if args == "" {
			args = "{}"
		}
		emit("content_block_start", map[string]any{
			"type":  "content_block_start",
			"index": index,
			"content_block": map[string]any{
				"type":  "tool_use",
				"id":    acc.id,
				"name":  acc.name,
				"input": map[string]any{},
			},
		})
		emit("content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": index,
			"delta": map[string]any{
				"type":         "input_json_delta",
				"partial_json": args,
			},
		})
		emit("content_block_stop", map[string]any{
			"type":  "content_block_stop",
			"index": index,
		})
	}

	var latestUsage tokenUsage
	var firstOutputAt time.Time

	// 心跳：SSE 注释行，所有 SSE 客户端按规范忽略，零开销。
	// 提交前后都要发：提交前是防止中间层（Cloudflare 120s 空闲读超时）掐断连接的关键，
	// 提交后用于长时间思考/工具生成阶段保活。
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	// 首个内容事件的上限：上游连一行 data 都没有、或只发空 delta 就长期静默时，
	// 最多等 firstContentTimeout 就放弃本次尝试并让调用方换链上下一个模型。
	firstContentTimer := time.NewTimer(firstContentTimeout)
	defer firstContentTimer.Stop()

L:
	for {
		select {
		case <-ctx.Done():
			// 客户端断开：停止转发并释放上游连接，不再为已放弃的请求继续耗账号流量
			upstream.Body.Close()
			if committed {
				log.Printf("  anthropic stream aborted: client disconnected")
				finalizeRequestLog(reqLog, latestUsage, firstOutputAt, reqLog.StartedAt, false, "client disconnected")
			} else {
				log.Printf("  anthropic stream: client disconnected before first token")
			}
			return committed
		case <-firstContentTimer.C:
			if !hasContent() {
				log.Printf("  anthropic stream: no content from upstream within %s, aborting attempt", firstContentTimeout)
				upstream.Body.Close()
				return false
			}
		case <-heartbeat.C:
			// 已出过内容 → 发真实事件：这一回合已经有产出，宁可保活到结束也不要让客户端
			// 中途放弃后整轮重跑（长回合重跑的代价远大于多等一会儿）。
			// 还没出内容 → 只发注释行：只喂字节级看门狗（Cloudflare），注定失败的尝试
			// 让客户端按自己的节奏超时并重连（用户偏好断开重连而非长时间静默假活着）。
			if hasContent() {
				emit("ping", map[string]any{"type": "ping"})
			} else {
				sw.write([]byte(": ping\n\n"))
			}
		case sl, ok := <-lines:
			if !ok {
				// 读取泵退出但未送达错误（防御性处理）
				if !hasContent() {
					log.Printf("  anthropic stream: upstream closed before any content, failing over")
					return false
				}
				log.Printf("  anthropic stream broken: upstream closed unexpectedly")
				emit("error", map[string]any{"type": "error", "error": map[string]any{"type": "api_error", "message": "upstream connection closed unexpectedly"}})
				finalizeRequestLog(reqLog, latestUsage, firstOutputAt, reqLog.StartedAt, false, "upstream closed unexpectedly")
				return true
			}
			if sl.err != nil {
				if !hasContent() {
					// 建流后没出任何内容就断了（实测 "unexpected EOF" 会静默 10 分钟才发生）：
					// 不要把它当成「已提交所以只能报错」，换模型重试对客户端完全无感。
					log.Printf("  anthropic stream: upstream ended before any content: %v", sl.err)
					return false
				}
				if errors.Is(sl.err, io.EOF) {
					break L // 正常结束 → 走收尾路径（ReadString 带回的残包与旧实现一致忽略）
				}
				// 真实的读取错误（超时/重置）：记录原因、通知客户端，而不是无声截断
				log.Printf("  anthropic stream broken: %v", sl.err)
				emit("error", map[string]any{"type": "error", "error": map[string]any{"type": "api_error", "message": sl.err.Error()}})
				finalizeRequestLog(reqLog, latestUsage, firstOutputAt, reqLog.StartedAt, false, "stream read error: "+sl.err.Error())
				return true
			}
			line := strings.TrimRight(sl.line, "\r\n")
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			payload := strings.TrimSpace(line[5:])
			if payload == "" {
				continue
			}
			if payload == "[DONE]" {
				if !hasContent() {
					// 上游一个内容块都没给就关闭 —— 视为失败，调用方可换模型重试
					log.Printf("  anthropic stream: upstream sent empty stream before any content")
					return false
				}
				continue
			}

			var obj map[string]any
			if err := json.Unmarshal([]byte(payload), &obj); err != nil {
				continue
			}
			if data, ok := obj["data"]; ok {
				if d, ok := data.(map[string]any); ok {
					obj = d
				}
			}
			if usage := parseTokenUsage(obj["usage"]); usage.Valid {
				latestUsage = mergeTokenUsage(latestUsage, usage)
			}
			if firstOutputAt.IsZero() && hasFirstOutput(obj) {
				firstOutputAt = time.Now()
			}

			// 上游带内错误（如 {"code":504,"message":"Upstream idle timeout exceeded"}）。
			// 只要还没发出内容事件（hasContent 为 false）就仍是可透明重试的失败；
			// 已经发出内容后则转发 error 事件并终止 —— 绝不伪造 end_turn 收尾（旧行为会让
			// 客户端收到 error 后又收到一个「成功的空消息」，表现为静默断流）。
			if errPayload, ok := obj["error"]; ok {
				errBody, _ := json.Marshal(errPayload)
				if !hasContent() {
					log.Printf("  anthropic stream: upstream error before any content: %s", string(errBody))
					return false
				}
				log.Printf("  anthropic stream broken by upstream error: %s", string(errBody))
				emit("error", map[string]any{"type": "error", "error": errPayload})
				finalizeRequestLog(reqLog, latestUsage, firstOutputAt, reqLog.StartedAt, false, "upstream error: "+string(errBody))
				return true
			}

			if !committed {
				commit() // 首条有效数据行到达，提交响应头 + message_start
			}

			choices, _ := getNested(obj, "choices").([]any)
			if len(choices) == 0 {
				continue
			}
			choice, _ := choices[0].(map[string]any)
			if choice == nil {
				continue
			}

			delta, ok := choice["delta"].(map[string]any)
			if !ok {
				delta = choice
			}

			// Reasoning content delta → Anthropic thinking block（thinking 在 text 前）
			if rc, ok := delta["reasoning_content"].(string); ok && rc != "" {
				sawThinking = true
				if !thinkingOpen {
					*textIndex++
					thinkingIdx = *textIndex
					thinkingOpen = true
					emit("content_block_start", map[string]any{
						"type":  "content_block_start",
						"index": thinkingIdx,
						"content_block": map[string]any{
							"type":      "thinking",
							"thinking":  "",
							"signature": "",
						},
					})
				}
				emit("content_block_delta", map[string]any{
					"type":  "content_block_delta",
					"index": thinkingIdx,
					"delta": map[string]any{
						"type":     "thinking_delta",
						"thinking": rc,
					},
				})
			}

			// Text content delta
			if c, ok := delta["content"].(string); ok && c != "" {
				if !hasText {
					hasText = true
					// thinking 块随 text 块开启而结束
					if thinkingOpen {
						emit("content_block_stop", map[string]any{
							"type":  "content_block_stop",
							"index": thinkingIdx,
						})
						thinkingOpen = false
					}
					*textIndex++
					emit("content_block_start", map[string]any{
						"type":  "content_block_start",
						"index": *textIndex,
						"content_block": map[string]any{
							"type": "text",
							"text": "",
						},
					})
				}
				emit("content_block_delta", map[string]any{
					"type":  "content_block_delta",
					"index": *textIndex,
					"delta": map[string]any{
						"type": "text_delta",
						"text": sanitizeContent(c),
					},
				})
			}

			// Tool calls - accumulate and emit when complete
			if tcRaw, ok := delta["tool_calls"].([]any); ok {
				for _, tc := range tcRaw {
					tcMap, _ := tc.(map[string]any)
					if tcMap == nil {
						continue
					}
					idx := 0
					if i, ok := tcMap["index"].(float64); ok {
						idx = int(i)
					}
					acc, exists := pendingTools[idx]
					if !exists {
						acc = &toolAccumulator{index: idx}
						pendingTools[idx] = acc
					}
					if id, ok := tcMap["id"].(string); ok && id != "" {
						acc.id = id
					}
					if fn, ok := tcMap["function"].(map[string]any); ok {
						if name, ok := fn["name"].(string); ok && name != "" {
							acc.name = name
							sawNamedTool = true
						}
						if args, ok := fn["arguments"].(string); ok && args != "" {
							acc.args += args
						}
					}
				}
			}

			// Finish reason
			if fr, ok := choice["finish_reason"].(string); ok && fr != "" {
				switch fr {
				case "length":
					stopReason = "max_tokens"
				case "tool_calls":
					if sawNamedTool {
						stopReason = "tool_use"
					}
				}
			}
		}
	}

	// 上游正常收尾但一个内容事件都没发出（无文本 / 无思考 / 无具名工具调用）：
	// 这不是可用的回答 —— 实测 glm-5.3-flash 会以 4~5 分钟的空 end_turn 结束。
	// 视为本模型失败：返回 false 让调用方冷却该模型并换链上下一个模型重试。
	// 此时客户端只收到信封与 ping，没有任何内容事件，因此重试完全无感
	// （否则会重复 content_block index，客户端会错乱）。
	if !hasText && !sawThinking && !sawNamedTool {
		log.Printf("  anthropic stream: model %q finished with no content, failing over", reqLog.Model)
		return false
	}

	// Stop thinking block if still open (上游没有 text 输出时)
	if thinkingOpen {
		emit("content_block_stop", map[string]any{
			"type":  "content_block_stop",
			"index": thinkingIdx,
		})
		thinkingOpen = false
	}

	// Stop text block if active
	if hasText {
		emit("content_block_stop", map[string]any{
			"type":  "content_block_stop",
			"index": *textIndex,
		})
	}

	// Emit tool blocks in deterministic order with proper indices,
	// streaming args via input_json_delta (required by Anthropic clients).
	nextIndex := *textIndex + 1
	toolKeys := make([]int, 0, len(pendingTools))
	for k := range pendingTools {
		toolKeys = append(toolKeys, k)
	}
	sort.Ints(toolKeys)
	for _, k := range toolKeys {
		acc := pendingTools[k]
		// 跳过没有名字的畸形 tool call（流式分片丢失 name 分片）
		if acc.name == "" {
			continue
		}
		if acc.id == "" {
			acc.id = genToolUseID()
		}
		if !acc.emitted {
			emitToolBlock(acc, nextIndex)
			nextIndex++
		}
	}

	emit("message_delta", map[string]any{
		"type": "message_delta",
		"delta": map[string]any{
			"stop_reason":   stopReason,
			"stop_sequence": nil,
		},
		"usage": map[string]any{
			"output_tokens": latestUsage.Completion,
		},
	})
	recordTokenUsage(acc, reqLog.Model, latestUsage)
	finalizeRequestLog(reqLog, latestUsage, firstOutputAt, reqLog.StartedAt, true, "")

	emit("message_stop", map[string]any{"type": "message_stop"})
	log.Printf("  anthropic stream done: hasText=%v tools=%d reason=%s", hasText, len(pendingTools), stopReason)
	return true
}

func normalizeOpenAIResponse(obj map[string]any) map[string]any {
	out := make(map[string]any)
	for k, v := range obj {
		if k == "provider_metadata" || k == "proxy_metadata" {
			continue
		}
		out[k] = v
	}

	if choices, ok := out["choices"].([]any); ok {
		normalized := make([]any, 0, len(choices))
		for _, ch := range choices {
			if c, ok := ch.(map[string]any); ok {
				nc := make(map[string]any)
				for k, v := range c {
					if k == "provider_metadata" || k == "proxy_metadata" {
						continue
					}
					nc[k] = v
				}
				if msg, ok := nc["message"].(map[string]any); ok {
					nc["message"] = normalizeMessage(msg)
				}
				if delta, ok := nc["delta"].(map[string]any); ok {
					nd := make(map[string]any)
					for k, v := range delta {
						if k == "provider_metadata" || k == "proxy_metadata" {
							continue
						}
						nd[k] = v
					}
					if tc, ok := nd["tool_calls"].([]any); ok && len(tc) > 0 {
						if nd["content"] == nil {
							nd["content"] = ""
						}
					}
					nc["delta"] = nd
				}
				normalized = append(normalized, nc)
			} else {
				normalized = append(normalized, ch)
			}
		}
		out["choices"] = normalized
	}

	return out
}

func sanitizeContent(s string) string {
	return s
}

func normalizeMessage(msg map[string]any) map[string]any {
	out := make(map[string]any)
	for k, v := range msg {
		if k == "provider_metadata" || k == "proxy_metadata" {
			continue
		}
		out[k] = v
	}
	if tc, ok := out["tool_calls"].([]any); ok && len(tc) > 0 {
		if out["content"] == nil {
			out["content"] = ""
		}
	}
	if c, ok := out["content"].(string); ok {
		out["content"] = sanitizeContent(c)
	}
	return out
}

func getNested(obj map[string]any, keys ...any) any {
	current := any(obj)
	for _, key := range keys {
		switch k := key.(type) {
		case string:
			if m, ok := current.(map[string]any); ok {
				current = m[k]
			} else {
				return nil
			}
		case int:
			if arr, ok := current.([]any); ok && k < len(arr) {
				current = arr[k]
			} else {
				return nil
			}
		default:
			return nil
		}
	}
	return current
}

func freePort(port int) {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return // port is free
	}
	conn.Close()

	// Try to kill the process using the port
	cmd := execCommand("powershell", "-Command",
		fmt.Sprintf(`$p=Get-NetTCPConnection -LocalPort %d -ErrorAction SilentlyContinue; if($p){Stop-Process -Id $p.OwningProcess -Force}`, port))
	_ = cmd.Run()
	time.Sleep(500 * time.Millisecond)
}
