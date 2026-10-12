package main

import (
	"testing"
	"time"
)

func TestPruneRequestLogsBoundsAgeAndCount(t *testing.T) {
	now := time.Now()
	old := now.Add(-31 * 24 * time.Hour)
	entries := []RequestLog{
		{ID: "old", StartedAt: old},
		{ID: "keep1", StartedAt: now.Add(-time.Hour)},
		{ID: "keep2", StartedAt: now.Add(-2 * time.Hour)},
	}

	requestLogsMu.Lock()
	requestLogs = pruneRequestLogsLocked(entries)
	requestLogsMu.Unlock()

	if len(requestLogs) != 2 {
		t.Fatalf("expected 2 entries after age pruning, got %d", len(requestLogs))
	}
	if requestLogs[0].ID != "keep1" {
		t.Fatalf("expected newest first, got %q", requestLogs[0].ID)
	}
}

func TestListRequestLogsCursorPagination(t *testing.T) {
	now := time.Now()
	entries := make([]RequestLog, 0, 75)
	for i := 0; i < 75; i++ {
		entries = append(entries, RequestLog{
			ID:        "req_" + string(rune('a'+i)),
			StartedAt: now.Add(-time.Duration(75-i) * time.Second),
		})
	}

	requestLogsMu.Lock()
	requestLogs = pruneRequestLogsLocked(entries)
	requestLogsMu.Unlock()

	page1, err := listRequestLogs(50, "")
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if len(page1.Items) != 50 || !page1.HasMore {
		t.Fatalf("page1 unexpected: len=%d hasMore=%v", len(page1.Items), page1.HasMore)
	}
	if page1.Items[0].ID != requestLogs[0].ID {
		t.Fatalf("page1 first item = %q, want %q", page1.Items[0].ID, requestLogs[0].ID)
	}

	page2, err := listRequestLogs(50, page1.NextCursor)
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(page2.Items) != 25 || page2.HasMore {
		t.Fatalf("page2 unexpected: len=%d hasMore=%v", len(page2.Items), page2.HasMore)
	}

	if page2.Items[0].ID != requestLogs[50].ID {
		t.Fatalf("page2 first item = %q, want %q", page2.Items[0].ID, requestLogs[50].ID)
	}
}

func TestListRequestLogsRejectsInvalidCursor(t *testing.T) {
	if _, err := listRequestLogs(50, "not-a-valid-cursor"); err == nil {
		t.Fatal("expected error for invalid cursor")
	}
}

func TestParseTokenUsageCachePrecedence(t *testing.T) {
	usage := parseTokenUsage(map[string]any{
		"prompt_tokens": float64(100),
		"prompt_tokens_details": map[string]any{
			"cached_tokens": float64(40),
		},
		"cache_read_input_tokens":     float64(10),
		"cache_creation_input_tokens": float64(5),
	})
	if usage.Cached != 40 {
		t.Fatalf("expected nested precedence cached=40, got %d", usage.Cached)
	}
	if usage.Prompt != 100 {
		t.Fatalf("expected prompt=100, got %d", usage.Prompt)
	}
}

func TestModelUsageStats(t *testing.T) {
	now := time.Now()
	yesterday := now.Add(-24 * time.Hour)
	requestLogsMu.Lock()
	requestLogs = []RequestLog{
		// 模型 A：今日 2 次 + 昨日 1 次，两个上游（故障转移场景）
		{ID: "a1", Model: "mimo-v2.6-flash-free", Upstream: "opencode", StartedAt: now, InputTokens: 100, OutputTokens: 50, CachedTokens: 10, TotalTokens: 150},
		{ID: "a2", Model: "mimo-v2.6-flash-free", Upstream: "opencode", StartedAt: now, InputTokens: 200, OutputTokens: 80, CachedTokens: 0, TotalTokens: 280},
		{ID: "a3", Model: "mimo-v2.6-flash-free", Upstream: "cline", StartedAt: yesterday, InputTokens: 10, OutputTokens: 5, CachedTokens: 0, TotalTokens: 15},
		// 模型 B：仅昨日
		{ID: "b1", Model: "cline-free/mimo-v2.6-flash", Upstream: "cline", StartedAt: yesterday, InputTokens: 7, OutputTokens: 3, CachedTokens: 0, TotalTokens: 10},
	}
	requestLogsMu.Unlock()

	got := modelUsageStats()
	if len(got) != 2 {
		t.Fatalf("expected 2 models, got %d: %+v", len(got), got)
	}
	// 按累计总 Token 降序：A(445) 在前
	if got[0].Model != "mimo-v2.6-flash-free" {
		t.Fatalf("expected model A first, got %s", got[0].Model)
	}
	a := got[0]
	if a.Requests != 3 || a.InputTokens != 310 || a.OutputTokens != 135 || a.TotalTokens != 445 {
		t.Fatalf("A cumulative wrong: %+v", a)
	}
	if a.TodayRequests != 2 || a.TodayTotalTokens != 430 {
		t.Fatalf("A today wrong: %+v", a)
	}
	if a.Upstream != "cline/opencode" {
		t.Fatalf("A upstream expected cline/opencode, got %q", a.Upstream)
	}
	b := got[1]
	if b.TodayRequests != 0 || b.TotalTokens != 10 {
		t.Fatalf("B wrong: %+v", b)
	}
}
