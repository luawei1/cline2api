package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// ============================================================================
// 自定义 OpenAI 兼容 Provider
// 用户可接入任意 OpenAI 兼容上游（OpenRouter / Groq / Cerebras / Gemini 兼容层 /
// Mistral / 自建 vLLM 等）。每个 provider 声明 baseURL、apiKey、自定义请求头、
// 模型列表；请求按模型归属路由，失败后按 回退链 → 自动兜底 降级。
// ============================================================================

// CustomProvider 一个 OpenAI 兼容上游。
type CustomProvider struct {
	ID          string            `json:"id"`          // 稳定 ID（生成）
	Name        string            `json:"name"`        // 显示名
	BaseURL     string            `json:"baseURL"`     // 如 https://openrouter.ai/api/v1
	APIKey      string            `json:"apiKey"`      // Bearer token
	ModelIDs    []string          `json:"modelIds"`    // 该上游暴露的模型 ID（如 "z-ai/glm-5.3-flash"）
	Headers     map[string]string `json:"headers,omitempty"` // 自定义请求头（如 OpenRouter 的 HTTP-Referer）
	Enabled     bool              `json:"enabled"`
	Priority    int               `json:"priority"`          // 越小越优先（同模型多上游时）
	TimeoutSec  int               `json:"timeoutSec,omitempty"` // 默认 300
	Free        bool              `json:"free"`              // 标记免费来源（供统计/兜底）
	CreatedAt   time.Time         `json:"createdAt"`
}

// providerRegistry 内存态 + 落盘（.cline-providers.json）。
var (
	providersMu       sync.Mutex
	customProviders   []*CustomProvider
	providersLoaded   bool
	providersFilePath string
	// modelCooldownsPerProvider providerID:model → 冷却截止
	providerCooldowns = map[string]time.Time{}
)

func init() {
	providersFilePath = resolveDataPath(".cline-providers.json")
}

func loadProviders() {
	providersMu.Lock()
	defer providersMu.Unlock()
	if providersLoaded {
		return
	}
	providersLoaded = true
	data, err := os.ReadFile(providersFilePath)
	if err != nil {
		return
	}
	var list []*CustomProvider
	if err := json.Unmarshal(data, &list); err != nil {
		log.Printf("providers parse failed: %v", err)
		return
	}
	customProviders = list
}

func saveProvidersLocked() {
	data, err := json.MarshalIndent(customProviders, "", "  ")
	if err != nil {
		return
	}
	if err := os.WriteFile(providersFilePath, data, 0600); err != nil {
		log.Printf("providers save failed: %v", err)
	}
}

// listProviders 返回副本列表（按 Priority, CreatedAt 排序）。
func listProviders() []CustomProvider {
	loadProviders()
	providersMu.Lock()
	defer providersMu.Unlock()
	out := make([]CustomProvider, 0, len(customProviders))
	for _, p := range customProviders {
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority < out[j].Priority
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}

func genProviderID() string {
	return "prov_" + fmt.Sprintf("%x", time.Now().UnixNano())
}

// upsertProvider 新增或更新（按 ID）。返回最终 provider。
func upsertProvider(p *CustomProvider) CustomProvider {
	loadProviders()
	providersMu.Lock()
	defer providersMu.Unlock()
	if p.ID == "" {
		p.ID = genProviderID()
		p.CreatedAt = time.Now()
		found := false
		for _, e := range customProviders {
			if e.ID == p.ID {
				*e = *p
				found = true
				break
			}
		}
		if !found {
			customProviders = append(customProviders, p)
		}
	} else {
		found := false
		for _, e := range customProviders {
			if e.ID == p.ID {
				*e = *p
				found = true
				break
			}
		}
		if !found {
			p.CreatedAt = time.Now()
			customProviders = append(customProviders, p)
		}
	}
	saveProvidersLocked()
	return *p
}

func deleteProvider(id string) bool {
	loadProviders()
	providersMu.Lock()
	defer providersMu.Unlock()
	for i, p := range customProviders {
		if p.ID == id {
			customProviders = append(customProviders[:i], customProviders[i+1:]...)
			saveProvidersLocked()
			return true
		}
	}
	return false
}

// resolveProviderForModel 找到该模型的首选 provider（启用中、未冷却、优先级最小）。
func resolveProviderForModel(model string) *CustomProvider {
	loadProviders()
	providersMu.Lock()
	defer providersMu.Unlock()
	var best *CustomProvider
	for _, p := range customProviders {
		if !p.Enabled {
			continue
		}
		for _, m := range p.ModelIDs {
			if m == model {
				if until, cool := providerCooldowns[p.ID+":"+model]; cool && time.Now().Before(until) {
					break // 该 provider 的该模型冷却中
				}
				if best == nil || p.Priority < best.Priority {
					best = p
				}
				break
			}
		}
	}
	return best
}

// providerFallbackModels 该模型的所有 provider 候选按优先级排序的模型链：
// 原模型优先，然后是 modelChain（配置链）中启用了 provider 的模型。
func customProviderServes(model string) bool {
	return resolveProviderForModel(model) != nil
}

// setProviderCooldown 记录 provider 模型冷却（429/5xx 时）。
func setProviderCooldown(providerID, model string, until time.Time) {
	providersMu.Lock()
	providerCooldowns[providerID+":"+model] = until
	providersMu.Unlock()
}



// callProvider 调用自定义 provider 的 /chat/completions。
func callProvider(p *CustomProvider, params map[string]any, stream bool) (*http.Response, error) {
	body := map[string]any{}
	for _, k := range passThroughKeys {
		if v, ok := params[k]; ok {
			body[k] = v
		}
	}
	for _, k := range []string{"model", "messages", "max_tokens", "max_completion_tokens", "stream"} {
		if v, ok := params[k]; ok {
			body[k] = v
		}
	}
	if msgs, ok := params["messages"].([]any); ok {
		body["messages"] = sanitizeMessages(msgs)
	}
	body["stream"] = stream
	if _, ok := body["max_tokens"]; !ok {
		if mt, ok := params["max_tokens"].(float64); ok {
			body["max_tokens"] = mt
		}
	}
	// 与 buildUpstreamBody 对称：低于上游硬下限的输出预算兜到默认值
	if mt, ok := body["max_tokens"].(float64); ok && mt < minUpstreamMaxTokens {
		log.Printf("  provider clamp max_tokens=%d -> %d (upstream requires >= %d)", int(mt), defaultMaxTokens, minUpstreamMaxTokens)
		body["max_tokens"] = defaultMaxTokens
	}
	if mt, ok := body["max_completion_tokens"].(float64); ok && mt < minUpstreamMaxTokens {
		log.Printf("  provider clamp max_completion_tokens=%d -> %d (upstream requires >= %d)", int(mt), defaultMaxTokens, minUpstreamMaxTokens)
		body["max_completion_tokens"] = defaultMaxTokens
	}
	// 与 buildUpstreamBody 对称：按模型已知硬上限封顶（gemini-3.8-flash 等）
	if modelID, _ := body["model"].(string); modelID != "" {
		if limit := modelMaxOutputLimit(modelID); limit > 0 {
			if mt, ok := body["max_tokens"].(float64); ok && mt > float64(limit) {
				log.Printf("  provider clamp max_tokens=%d -> %d (model %q output limit)", int(mt), limit, modelID)
				body["max_tokens"] = float64(limit)
			}
			if mt, ok := body["max_completion_tokens"].(float64); ok && mt > float64(limit) {
				log.Printf("  provider clamp max_completion_tokens=%d -> %d (model %q output limit)", int(mt), limit, modelID)
				body["max_completion_tokens"] = float64(limit)
			}
		}
	}

	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal provider body: %w", err)
	}

	endpoint := strings.TrimRight(p.BaseURL, "/") + "/chat/completions"
	req, err := http.NewRequest("POST", endpoint, bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("create provider request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.APIKey)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range p.Headers {
		if v == "" {
			req.Header.Del(k)
			continue
		}
		req.Header.Set(k, v)
	}

	timeout := time.Duration(p.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	// 复用全局 transport（测试/代理定制经由 httpClient.Transport 生效）
	client := &http.Client{Transport: httpClient.Transport, Timeout: timeout}

	log.Printf("  provider upstream: name=%s model=%v stream=%v msgs=%d", p.Name, params["model"], stream, getMsgCount(params))
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("provider request: %w", err)
	}
	if resp.StatusCode == http.StatusOK {
		return resp, nil
	}
	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	resp.Body.Close()
	// 429 / 5xx：该 provider 模型冷却 5 分钟（有 Retry-After 时优先）
	until := time.Now().Add(5 * time.Minute)
	if ra := parseRetryAfter(resp.Header.Get("Retry-After")); ra > 0 {
		until = time.Now().Add(ra)
	}
	setProviderCooldown(p.ID, fmt.Sprintf("%v", params["model"]), until)
	return nil, &clineAPIError{statusCode: resp.StatusCode, message: truncate(string(bodyBytes), 500)}
}

// handleProviderStreamResponse 转发 provider 的流式响应（OpenAI 格式）。
func handleProviderStreamResponse(w http.ResponseWriter, upstream *http.Response, reqLog *RequestLog) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	flusher, ok := w.(http.Flusher)
	if !ok {
		return
	}
	reader := bufio.NewReader(upstream.Body)
	var latestUsage tokenUsage
	var firstOutputAt time.Time
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF && line != "" {
				w.Write([]byte(line + "\n"))
				flusher.Flush()
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
			var obj map[string]any
			if json.Unmarshal([]byte(payload), &obj) == nil {
				normalized := normalizeOpenAIResponse(obj)
				if u := parseTokenUsage(normalized["usage"]); u.Valid {
					latestUsage = mergeTokenUsage(latestUsage, u)
				}
				if firstOutputAt.IsZero() && hasFirstOutput(normalized) {
					firstOutputAt = time.Now()
				}
				if b, err := json.Marshal(normalized); err == nil {
					w.Write([]byte("data: " + string(b) + "\n\n"))
					flusher.Flush()
					continue
				}
			}
		}
		w.Write([]byte(line + "\n"))
		flusher.Flush()
	}
	recordTokenUsage(nil, reqLog.Model, latestUsage)
	finalizeRequestLog(reqLog, latestUsage, firstOutputAt, reqLog.StartedAt, true, "")
}

// ============================================================================
// Provider 预设（市面常见免费/低成本 OpenAI 兼容上游）
// ============================================================================

type providerPreset struct {
	Name     string
	BaseURL  string
	Headers  map[string]string
	Notes    string
	FreeTier bool
}

// providerPresets 常见 OpenAI 兼容上游预设。Key 为前端展示名。
var providerPresets = map[string]providerPreset{
	"openrouter": {
		Name:    "OpenRouter",
		BaseURL: "https://openrouter.ai/api/v1",
		Headers: map[string]string{
			"HTTP-Referer": "https://cline.bot",
			"X-Title":      "Cline Proxy",
		},
		Notes:    "300+ models; many :free variants. Key: openrouter.ai/keys",
		FreeTier: true,
	},
	"groq": {
		Name:     "Groq",
		BaseURL:  "https://api.groq.com/openai/v1",
		Notes:    "Fast free tier (Llama/Qwen). Key: console.groq.com/keys",
		FreeTier: true,
	},
	"cerebras": {
		Name:     "Cerebras",
		BaseURL:  "https://api.cerebras.ai/v1",
		Notes:    "Free tier, very high throughput. Key: cloud.cerebras.ai",
		FreeTier: true,
	},
	"gemini": {
		Name:    "Google AI Studio",
		BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai",
		Notes:   "Gemini free tier via OpenAI-compat layer. Key: aistudio.google.com/apikey",
		FreeTier: true,
	},
	"mistral": {
		Name:     "Mistral (La Plateforme)",
		BaseURL:  "https://api.mistral.ai/v1",
		Notes:    "Free experiment tier. Key: console.mistral.ai",
		FreeTier: true,
	},
	"together": {
		Name:    "Together AI",
		BaseURL: "https://api.together.xyz/v1",
		Notes:   "Some free models (e.g. Llama Vision free). Key: api.together.ai",
		FreeTier: true,
	},
	"deepseek": {
		Name:    "DeepSeek",
		BaseURL: "https://api.deepseek.com/v1",
		Notes:   "Paid, very cheap. Key: platform.deepseek.com",
	},
	"openai": {
		Name:    "OpenAI",
		BaseURL: "https://api.openai.com/v1",
		Notes:   "Paid. Key: platform.openai.com",
	},
	"vllm-local": {
		Name:    "Local vLLM / Ollama",
		BaseURL: "http://127.0.0.1:8000/v1",
		Notes:   "Self-hosted OpenAI-compatible server (no key needed usually)",
		FreeTier: true,
	},
}

// ============================================================================
// Fallback 集成：自定义 provider 参与 modelFallbackChain
// ============================================================================

// callCustomProviderAPI 尝试用自定义 provider 服务请求；成功返回响应。
// 失败返回 error（调用方决定是否降级）。
func callCustomProviderAPI(params map[string]any, stream bool) (*http.Response, error, bool) {
	model, _ := params["model"].(string)
	p := resolveProviderForModel(model)
	if p == nil {
		return nil, nil, false
	}
	resp, err := callProvider(p, params, stream)
	if err != nil {
		return nil, err, true
	}
	return resp, nil, true
}

// ============================================================================
// Admin API handlers
// ============================================================================

func handleProvidersList(w http.ResponseWriter, r *http.Request) {
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"providers": listProviders()}})
}

func handleProviderSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: tAPI(r, "method_not_allowed")})
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	defer r.Body.Close()
	var p CustomProvider
	if err := json.Unmarshal(body, &p); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid_json"})
		return
	}
	p.Name = strings.TrimSpace(p.Name)
	p.BaseURL = strings.TrimSpace(strings.TrimRight(p.BaseURL, "/"))
	p.APIKey = strings.TrimSpace(p.APIKey)
	if p.Name == "" || p.BaseURL == "" {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "name and baseURL are required"})
		return
	}
	if !strings.HasPrefix(p.BaseURL, "http://") && !strings.HasPrefix(p.BaseURL, "https://") {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "baseURL must start with http:// or https://"})
		return
	}
	// 清洗模型 ID 与请求头
	var models []string
	seen := map[string]bool{}
	for _, m := range p.ModelIDs {
		m = strings.TrimSpace(m)
		if m != "" && !seen[m] {
			seen[m] = true
			models = append(models, m)
		}
	}
	p.ModelIDs = models
	hdrs := map[string]string{}
	for k, v := range p.Headers {
		k = strings.TrimSpace(k)
		if k != "" {
			hdrs[k] = strings.TrimSpace(v)
		}
	}
	p.Headers = hdrs
	if p.TimeoutSec < 0 || p.TimeoutSec > 600 {
		p.TimeoutSec = 0
	}
	saved := upsertProvider(&p)
	log.Printf("admin: provider saved name=%s models=%d enabled=%v", saved.Name, len(saved.ModelIDs), saved.Enabled)
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"provider": saved}})
}

func handleProviderDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: tAPI(r, "method_not_allowed")})
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid_json"})
		return
	}
	if !deleteProvider(req.ID) {
		writeAPI(w, http.StatusNotFound, apiResponse{Error: "provider not found"})
		return
	}
	log.Printf("admin: provider deleted id=%s", req.ID)
	writeAPI(w, http.StatusOK, apiResponse{Success: true})
}

// handleProviderTest 发一条 min-token 请求验证 provider 可用性。
func handleProviderTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: tAPI(r, "method_not_allowed")})
		return
	}
	var req struct {
		ID    string `json:"id"`
		Model string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid_json"})
		return
	}
	loadProviders()
	providersMu.Lock()
	var target *CustomProvider
	for _, p := range customProviders {
		if p.ID == req.ID {
			cp := *p
			target = &cp
			break
		}
	}
	providersMu.Unlock()
	if target == nil {
		writeAPI(w, http.StatusNotFound, apiResponse{Error: "provider not found"})
		return
	}
	model := strings.TrimSpace(req.Model)
	if model == "" && len(target.ModelIDs) > 0 {
		model = target.ModelIDs[0]
	}
	params := map[string]any{
		"model":      model,
		"max_tokens": 16,
		"messages":   []any{map[string]any{"role": "user", "content": "hi"}},
	}
	started := time.Now()
	resp, err := callProvider(target, params, false)
	result := map[string]any{"durationMs": time.Since(started).Milliseconds(), "model": model}
	if err != nil {
		result["ok"] = false
		result["error"] = truncate(err.Error(), 300)
		writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: result})
		return
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	var obj map[string]any
	if json.Unmarshal(raw, &obj) == nil {
		if data, ok := obj["data"].(map[string]any); ok {
			obj = data
		}
		obj = normalizeOpenAIResponse(obj)
	}
	result["ok"] = true
	if u := parseTokenUsage(obj["usage"]); u.Valid {
		result["inputTokens"] = u.Prompt
		result["outputTokens"] = u.Completion
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: result})
}

func handleProviderPresets(w http.ResponseWriter, r *http.Request) {
	type presetOut struct {
		Key      string            `json:"key"`
		Name     string            `json:"name"`
		BaseURL  string            `json:"baseURL"`
		Headers  map[string]string `json:"headers"`
		Notes    string            `json:"notes"`
		FreeTier bool              `json:"freeTier"`
	}
	keys := make([]string, 0, len(providerPresets))
	for k := range providerPresets {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]presetOut, 0, len(keys))
	for _, k := range keys {
		p := providerPresets[k]
		out = append(out, presetOut{Key: k, Name: p.Name, BaseURL: p.BaseURL, Headers: p.Headers, Notes: p.Notes, FreeTier: p.FreeTier})
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"presets": out}})
}
