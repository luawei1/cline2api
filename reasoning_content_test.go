package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// GLM 系模型（reflection/beam 等）要求历史里每条 assistant 消息带回 reasoning_content，
// 客户端不回传时上游 400。injectReasoningContent 补齐：优先取同消息 reasoning 字段，否则空串。
func TestInjectReasoningContent(t *testing.T) {
	body := map[string]any{"messages": []any{
		map[string]any{"role": "user", "content": "hi"},
		map[string]any{"role": "assistant", "content": "hello"},
		map[string]any{"role": "assistant", "content": "next", "reasoning": "thought hard"},
		map[string]any{"role": "assistant", "content": "third", "reasoning_content": "kept"},
		map[string]any{"role": "tool", "content": "result", "tool_call_id": "c1"},
	}}
	if !injectReasoningContent(body) {
		t.Fatal("injectReasoningContent should report injection when assistant messages lack reasoning_content")
	}
	msgs := body["messages"].([]any)
	if got := msgs[1].(map[string]any)["reasoning_content"]; got != "." {
		t.Fatalf("assistant without reasoning field: reasoning_content = %v, want placeholder %q (empty string is rejected upstream)", got, ".")
	}
	if got := msgs[2].(map[string]any)["reasoning_content"]; got != "thought hard" {
		t.Fatalf("assistant with reasoning field: reasoning_content = %v, want it copied over", got)
	}
	if got := msgs[3].(map[string]any)["reasoning_content"]; got != "kept" {
		t.Fatalf("assistant already carrying reasoning_content = %v, want it untouched", got)
	}
	if _, ok := msgs[4].(map[string]any)["reasoning_content"]; ok {
		t.Fatal("tool message should not receive reasoning_content")
	}
	if injectReasoningContent(map[string]any{"messages": []any{
		map[string]any{"role": "assistant", "content": "x", "reasoning_content": "y"},
	}}) {
		t.Fatal("nothing to inject should return false")
	}
}

// 上游 400 报缺 reasoning_content 时：原账号补齐重试一次，请求体里的 assistant
// 消息带上 reasoning_content；重试 200 则成功返回。
func TestCallClineAPIWithAccountRetriesWithInjectedReasoningContent(t *testing.T) {
	var retryBody []byte
	calls := 0
	isolateRefreshState(t, func(req *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			// Cline 网关把 Vercel 层的 400 包成外层 500（实测），按错误体匹配
			return refreshStatusResponse(req, http.StatusInternalServerError,
				`{"error":"inference request failed: failed to invoke model 'reflection/beam-free' from Vercel: request failed with status 400: {\"error\":{\"message\":\"Missing required parameter: 'messages[2].reasoning_content'. This model needs the reasoning of every earlier assistant message.\",\"type\":\"AI_APICallError\"}}"}`), nil
		}
		retryBody, _ = io.ReadAll(req.Body)
		return refreshStatusResponse(req, http.StatusOK,
			`{"id":"ok","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]}`), nil
	})
	acc := &Account{
		AccountID:   "beam",
		Email:       "beam@example.com",
		AccessToken: "tok",
		ExpiresAt:   time.Now().Add(time.Hour).UnixMilli(),
		Status:      "active",
	}
	pool = &AccountPool{Accounts: []*Account{acc}}

	resp, usedAcc, err := callClineAPIWithAccount(acc, map[string]any{
		"model": "reflection/beam-free",
		"messages": []any{
			map[string]any{"role": "user", "content": "hi"},
			map[string]any{"role": "assistant", "content": "hello"},
			map[string]any{"role": "user", "content": "what did you say?"},
		},
	}, false)
	if err != nil {
		t.Fatalf("callClineAPIWithAccount should succeed via reasoning_content retry: %v", err)
	}
	resp.Body.Close()
	if usedAcc != acc {
		t.Fatal("retry should serve via the same account")
	}
	if calls != 2 {
		t.Fatalf("upstream calls = %d, want exactly one retry", calls)
	}
	var sent struct {
		Messages []map[string]any `json:"messages"`
	}
	if err := json.Unmarshal(retryBody, &sent); err != nil {
		t.Fatalf("decode retry body: %v", err)
	}
	if got := sent.Messages[1]["reasoning_content"]; got != "." {
		t.Fatalf("retry body assistant reasoning_content = %v, want injected placeholder %q", got, ".")
	}
}

// 重试仍失败时错误照常上报（不吞错、不再无限重试）。
func TestCallClineAPIWithAccountReasoningRetryStillFails(t *testing.T) {	calls := 0
	isolateRefreshState(t, func(req *http.Request) (*http.Response, error) {
		calls++
		return refreshStatusResponse(req, http.StatusInternalServerError,
			`{"error":"inference request failed: ... request failed with status 400: {\"error\":{\"message\":\"Missing required parameter: 'messages[2].reasoning_content'.\"}}"}`), nil
	})
	acc := &Account{
		AccountID:   "beam2",
		Email:       "beam2@example.com",
		AccessToken: "tok",
		ExpiresAt:   time.Now().Add(time.Hour).UnixMilli(),
		Status:      "active",
	}
	pool = &AccountPool{Accounts: []*Account{acc}}

	_, _, err := callClineAPIWithAccount(acc, map[string]any{
		"model": "reflection/beam-free",
		"messages": []any{
			map[string]any{"role": "user", "content": "hi"},
			map[string]any{"role": "assistant", "content": "hello"},
		},
	}, false)
	if err == nil {
		t.Fatal("should fail when the injected retry is also rejected")
	}
	if calls != 2 {
		t.Fatalf("upstream calls = %d, want one original + one retry", calls)
	}
	if !strings.Contains(err.Error(), "reasoning_content") {
		t.Fatalf("error should surface the upstream reason, got: %v", err)
	}
}

// 流式请求：Cline 网关对「缺 reasoning_content」返回 200 + SSE 错误事件
// （stream_initialization_failed）——偷看首个 data 行命中后补齐重试一次。
func TestCallClineAPIStreamRetriesOnSSEErrorEvent(t *testing.T) {
	var retryBody []byte
	calls := 0
	isolateRefreshState(t, func(req *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return refreshStatusResponse(req, http.StatusOK,
				"data: {\"error\":{\"code\":\"stream_initialization_failed\",\"message\":\"Failed to create stream: inference request failed: failed to generate stream from Vercel: request failed with status 400: {\\\"error\\\":{\\\"message\\\":\\\"Missing required parameter: 'messages[1].reasoning_content'.\\\"}}\"}}\n\n"), nil
		}
		retryBody, _ = io.ReadAll(req.Body)
		return refreshStatusResponse(req, http.StatusOK,
			"data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"hi\"},\"finish_reason\":null}]}\n\ndata: [DONE]\n\n"), nil
	})
	acc := &Account{
		AccountID:   "beam-s",
		Email:       "beam-s@example.com",
		AccessToken: "tok",
		ExpiresAt:   time.Now().Add(time.Hour).UnixMilli(),
		Status:      "active",
	}
	pool = &AccountPool{Accounts: []*Account{acc}}

	resp, _, err := callClineAPIWithAccount(acc, map[string]any{
		"model": "reflection/beam-free",
		"messages": []any{
			map[string]any{"role": "user", "content": "hi"},
			map[string]any{"role": "assistant", "content": "hello"},
			map[string]any{"role": "user", "content": "what did you say?"},
		},
	}, true)
	if err != nil {
		t.Fatalf("streaming call should succeed via reasoning_content retry: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"content":"hi"`) {
		t.Fatalf("caller should receive the retry stream, got: %s", truncate(string(body), 200))
	}
	if calls != 2 {
		t.Fatalf("upstream calls = %d, want exactly one retry", calls)
	}
	var sent struct {
		Messages []map[string]any `json:"messages"`
	}
	if err := json.Unmarshal(retryBody, &sent); err != nil {
		t.Fatalf("decode retry body: %v", err)
	}
	if got := sent.Messages[1]["reasoning_content"]; got != "." {
		t.Fatalf("retry body assistant reasoning_content = %v, want placeholder %q", got, ".")
	}
}

// 正常流的首个 data 行不是错误事件：已读字节回填，调用方收到完整原流。
func TestCallClineAPIStreamReplaysNormalFirstChunk(t *testing.T) {
	isolateRefreshState(t, func(req *http.Request) (*http.Response, error) {
		return refreshStatusResponse(req, http.StatusOK,
			"data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"ok\"},\"finish_reason\":null}]}\n\ndata: [DONE]\n\n"), nil
	})
	acc := &Account{
		AccountID:   "beam-n",
		Email:       "beam-n@example.com",
		AccessToken: "tok",
		ExpiresAt:   time.Now().Add(time.Hour).UnixMilli(),
		Status:      "active",
	}
	pool = &AccountPool{Accounts: []*Account{acc}}

	resp, _, err := callClineAPIWithAccount(acc, map[string]any{
		"model":    "reflection/beam-free",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	}, true)
	if err != nil {
		t.Fatalf("streaming call should pass through: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"content":"ok"`) || !strings.Contains(string(body), "[DONE]") {
		t.Fatalf("caller should receive the full original stream, got: %s", truncate(string(body), 200))
	}
}
