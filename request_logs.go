package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	requestLogMaxEntries   = 5000
	requestLogMaxAge       = 30 * 24 * time.Hour
	requestLogDefaultLimit = 50
	requestLogMaxLimit     = 100
)

// AttemptLog 是一次上游模型尝试（模型组合有序回退过程中的单个条目）的归因记录。
// 仅在请求命中「模型组合」时填充；普通请求为空。字段均为可安全展示的信息：
// 不含凭据、请求头或请求体内容，reason 经脱敏并截断。
type AttemptLog struct {
	// Model 本次尝试实际请求的模型 ID。
	Model string `json:"model"`
	// Order 该模型在组合配置里的原始顺序（从 1 开始，-1=应急兜底）。
	Order int `json:"order,omitempty"`
	// Upstream 本次尝试走向的上游："cline" / "opencode" / "provider"。
	Upstream string `json:"upstream,omitempty"`
	// Status 上游 HTTP 状态码（可用时）。
	Status int `json:"status,omitempty"`
	// Outcome "success" | "failed" | "skipped"。
	Outcome string `json:"outcome"`
	// Reason 失败/跳过的原因（脱敏、截断）。
	Reason string `json:"reason,omitempty"`
	// DurationMs 本次尝试耗时（毫秒）。
	DurationMs int64 `json:"durationMs,omitempty"`
	// Source 本条目的上游固定（组合条目的 Source 原样回填）；空=自动派发。
	Source string `json:"source,omitempty"`
	// Provider 当 Source=="provider" 时固定到的自定义 Provider 名称；其余为空。
	Provider string `json:"provider,omitempty"`
}

type RequestLog struct {
	ID           string    `json:"id"`
	StartedAt    time.Time `json:"startedAt"`
	FinishedAt   time.Time `json:"finishedAt"`
	AccountID    string    `json:"accountId"`
	AccountEmail string    `json:"accountEmail"`
	Protocol     string    `json:"protocol"`
	// Upstream 标记上游来源："cline"=Cline 账号池，"opencode"=opencode zen 免费模型，
	// "provider"=自定义 OpenAI 兼容 Provider（归因由 stampUpstream 统一判定）
	Upstream string `json:"upstream,omitempty"`
	// Model 实际服务本次请求的模型（回退后回写的生效模型）。为兼容既有消费方，
	// 普通请求语义不变；组合请求也填实际服务模型，另用 RequestedModel 记录原始点名。
	Model          string  `json:"model"`
	Stream         bool    `json:"stream"`
	InputTokens    int64   `json:"inputTokens"`
	OutputTokens   int64   `json:"outputTokens"`
	CachedTokens   int64   `json:"cachedTokens"`
	TotalTokens    int64   `json:"totalTokens"`
	UsageAvailable bool    `json:"usageAvailable"`
	DurationMs     int64   `json:"durationMs"`
	TTFTMs         int64   `json:"ttftMs"`
	OutputTPS      float64 `json:"outputTokensPerSecond"`
	Completed      bool    `json:"completed"`
	Error          string  `json:"error,omitempty"`
	// ===== 模型组合（combo）归因：仅组合请求填充，普通请求省略 =====
	// RequestedModel 是客户端原始点名的模型 ID（组合 ID）。与 Model 分离，
	// 避免用一个字段同时表达「点名」和「实际服务」两种含义。
	RequestedModel string `json:"requestedModel,omitempty"`
	// ComboID / ComboName 命中的组合标识与显示名。
	ComboID   string `json:"comboId,omitempty"`
	ComboName string `json:"comboName,omitempty"`
	// FallbackUsed 是否发生了组合内回退（首个条目未直接成功）。
	FallbackUsed bool `json:"fallbackUsed,omitempty"`
	// FallbackCount 回退次数（= 失败/跳过的条目数）。
	FallbackCount int `json:"fallbackCount,omitempty"`
	// Attempts 有序尝试轨迹（成功后截止；失败时含全部尝试）。
	Attempts []AttemptLog `json:"attempts,omitempty"`
	// toolNames Cline 通道出站截短的工具名映射（短→原始），回程还原用；
	// 未导出字段不落请求日志
	toolNames map[string]string
}

var (
	requestLogs     []RequestLog
	requestLogsMu   sync.Mutex
	requestLogsPath string
)

func init() {
	requestLogsPath = resolveDataPath(".cline-request-logs.json")
}

func loadRequestLogs() {
	data, err := os.ReadFile(requestLogsPath)
	if err != nil {
		return
	}
	var entries []RequestLog
	if err := json.Unmarshal(data, &entries); err != nil {
		backupCorruptFile(requestLogsPath, err)
		return
	}
	requestLogsMu.Lock()
	requestLogs = pruneRequestLogsLocked(entries)
	requestLogsMu.Unlock()
}

func pruneRequestLogsLocked(entries []RequestLog) []RequestLog {
	if len(entries) == 0 {
		return entries
	}
	sort.Slice(entries, func(i, j int) bool {
		if !entries[i].StartedAt.Equal(entries[j].StartedAt) {
			return entries[i].StartedAt.After(entries[j].StartedAt)
		}
		return entries[i].ID > entries[j].ID
	})

	cutoff := time.Now().Add(-requestLogMaxAge)
	pruned := entries[:0]
	for _, e := range entries {
		if e.StartedAt.Before(cutoff) {
			continue
		}
		pruned = append(pruned, e)
	}
	if len(pruned) > requestLogMaxEntries {
		pruned = pruned[:requestLogMaxEntries]
	}
	return pruned
}

func saveRequestLogsLocked() {
	data, err := json.MarshalIndent(requestLogs, "", "  ")
	if err != nil {
		return
	}
	if err := writeFileAtomic(requestLogsPath, data, 0600); err != nil {
		log.Printf("request logs save failed: %v", err)
	}
}

func appendRequestLog(entry RequestLog) {
	if entry.ID == "" {
		entry.ID = fmt.Sprintf("req_%d", entry.StartedAt.UnixNano())
	}
	requestLogsMu.Lock()
	requestLogs = append(requestLogs, entry)
	requestLogs = pruneRequestLogsLocked(requestLogs)
	saveRequestLogsLocked()
	requestLogsMu.Unlock()
}

type requestLogPage struct {
	Items      []RequestLog `json:"items"`
	NextCursor string       `json:"nextCursor"`
	HasMore    bool         `json:"hasMore"`
}

func encodeCursor(entry RequestLog) string {
	key := fmt.Sprintf("%d|%s", entry.StartedAt.UnixNano(), entry.ID)
	return base64.RawURLEncoding.EncodeToString([]byte(key))
}

func decodeCursor(cursor string) (time.Time, string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("invalid cursor")
	}
	var ts int64
	var id string
	if _, err := fmt.Sscanf(string(raw), "%d|%s", &ts, &id); err != nil || id == "" {
		return time.Time{}, "", fmt.Errorf("invalid cursor")
	}
	return time.Unix(0, ts), id, nil
}

func listRequestLogs(limit int, cursor string) (requestLogPage, error) {
	if limit <= 0 {
		limit = requestLogDefaultLimit
	}
	if limit > requestLogMaxLimit {
		limit = requestLogMaxLimit
	}

	var afterTime time.Time
	var afterID string
	if cursor != "" {
		t, id, err := decodeCursor(cursor)
		if err != nil {
			return requestLogPage{}, err
		}
		afterTime = t
		afterID = id
	}

	requestLogsMu.Lock()
	defer requestLogsMu.Unlock()

	result := make([]RequestLog, 0, limit)
	var lastEntry RequestLog
	for _, e := range requestLogs {
		if cursor != "" {
			if e.StartedAt.After(afterTime) {
				continue
			}
			if e.StartedAt.Equal(afterTime) && e.ID >= afterID {
				continue
			}
		}
		result = append(result, e)
		lastEntry = e
		if len(result) >= limit {
			break
		}
	}

	page := requestLogPage{Items: result}
	if len(result) == limit {
		page.NextCursor = encodeCursor(lastEntry)
		page.HasMore = true
	}
	return page, nil
}

func finalizeRequestLog(entry *RequestLog, usage tokenUsage, firstOutputAt time.Time, startedAt time.Time, completed bool, errMsg string) {
	entry.FinishedAt = time.Now()
	entry.DurationMs = entry.FinishedAt.Sub(startedAt).Milliseconds()
	entry.Completed = completed
	entry.Error = truncate(errMsg, 200)

	if usage.Valid {
		entry.UsageAvailable = true
		entry.InputTokens = usage.Prompt
		entry.OutputTokens = usage.Completion
		entry.CachedTokens = usage.Cached
		entry.TotalTokens = usage.Total
	}

	if !firstOutputAt.IsZero() && usage.Valid && usage.Completion > 0 {
		entry.TTFTMs = firstOutputAt.Sub(startedAt).Milliseconds()
		generationMs := entry.FinishedAt.Sub(firstOutputAt).Seconds()
		if generationMs > 0 {
			entry.OutputTPS = float64(usage.Completion) / generationMs
		}
	}

	appendRequestLog(*entry)
}

// modelUsageEntry 按模型聚合的用量（仪表盘「模型用量统计」）。
type modelUsageEntry struct {
	Model        string `json:"model"`
	Upstream     string `json:"upstream"`
	Requests     int64  `json:"requests"`
	InputTokens  int64  `json:"inputTokens"`
	OutputTokens int64  `json:"outputTokens"`
	CachedTokens int64  `json:"cachedTokens"`
	TotalTokens  int64  `json:"totalTokens"`

	TodayRequests     int64 `json:"todayRequests"`
	TodayInputTokens  int64 `json:"todayInputTokens"`
	TodayOutputTokens int64 `json:"todayOutputTokens"`
	TodayTotalTokens  int64 `json:"todayTotalTokens"`
}

// modelUsageStats 把请求日志按模型聚合，累计与今日两套口径，按累计总 Token 降序。
// 同一模型可能经故障转移落到不同上游（reqLog.Upstream 各异），上游去重后用 "/" 连接。
// 只统计内存中保留的日志（requestLogMaxEntries / requestLogMaxAge 窗口内）。
func modelUsageStats() []modelUsageEntry {
	requestLogsMu.Lock()
	defer requestLogsMu.Unlock()

	today := time.Now().Format("2006-01-02")
	type agg struct {
		e   modelUsageEntry
		ups map[string]bool
	}
	order := make([]string, 0, 16)
	byModel := make(map[string]*agg, 16)
	get := func(model string) *agg {
		if a, ok := byModel[model]; ok {
			return a
		}
		a := &agg{ups: make(map[string]bool, 2)}
		a.e.Model = model
		byModel[model] = a
		order = append(order, model)
		return a
	}
	for _, e := range requestLogs {
		a := get(e.Model)
		a.e.Requests++
		a.e.InputTokens += e.InputTokens
		a.e.OutputTokens += e.OutputTokens
		a.e.CachedTokens += e.CachedTokens
		a.e.TotalTokens += e.TotalTokens
		if e.Upstream != "" {
			a.ups[e.Upstream] = true
		}
		if e.StartedAt.Format("2006-01-02") == today {
			a.e.TodayRequests++
			a.e.TodayInputTokens += e.InputTokens
			a.e.TodayOutputTokens += e.OutputTokens
			a.e.TodayTotalTokens += e.TotalTokens
		}
	}

	out := make([]modelUsageEntry, 0, len(order))
	for _, model := range order {
		a := byModel[model]
		ups := make([]string, 0, len(a.ups))
		for u := range a.ups {
			ups = append(ups, u)
		}
		sort.Strings(ups)
		a.e.Upstream = strings.Join(ups, "/")
		out = append(out, a.e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TotalTokens > out[j].TotalTokens })
	return out
}
