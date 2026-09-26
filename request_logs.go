package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"
)

const (
	requestLogMaxEntries   = 5000
	requestLogMaxAge       = 30 * 24 * time.Hour
	requestLogDefaultLimit = 50
	requestLogMaxLimit     = 100
)

type RequestLog struct {
	ID             string    `json:"id"`
	StartedAt      time.Time `json:"startedAt"`
	FinishedAt     time.Time `json:"finishedAt"`
	AccountID      string    `json:"accountId"`
	AccountEmail   string    `json:"accountEmail"`
	Protocol       string    `json:"protocol"`
	// Upstream 标记上游来源："cline"=Cline 账号池，"opencode"=opencode zen 免费模型，
	// "provider"=自定义 OpenAI 兼容 Provider（归因由 stampUpstream 统一判定）
	Upstream     string `json:"upstream,omitempty"`
	Model        string `json:"model"`
	Stream       bool   `json:"stream"`
	InputTokens  int64  `json:"inputTokens"`
	OutputTokens int64  `json:"outputTokens"`
	CachedTokens int64  `json:"cachedTokens"`
	TotalTokens  int64  `json:"totalTokens"`
	UsageAvailable bool `json:"usageAvailable"`
	DurationMs   int64     `json:"durationMs"`
	TTFTMs       int64     `json:"ttftMs"`
	OutputTPS    float64   `json:"outputTokensPerSecond"`
	Completed    bool      `json:"completed"`
	Error        string    `json:"error,omitempty"`
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
	tmp := requestLogsPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return
	}
	_ = os.Rename(tmp, requestLogsPath)
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
