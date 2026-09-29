package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestZenModelUsesResponses 协议族判定：muse-spark 系走 Responses，其余仍走 chat。
func TestZenModelUsesResponses(t *testing.T) {
	cases := []struct {
		model string
		want  bool
	}{
		{"muse-spark-1.3-contributor-free", true},
		{"muse-spark-1.3", true},
		{"muse-spark-1.2-contributor-free", true},
		{"mimo-v2.6-flash-free", false},
		{"deepseek-v4-flash-free", false},
		{"nemotron-3-ultra-free", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := zenModelUsesResponses(tc.model); got != tc.want {
			t.Errorf("zenModelUsesResponses(%q) = %v, want %v", tc.model, got, tc.want)
		}
	}
}

// TestBuildZenResponsesBody chat 参数 → Responses 请求体的映射：
// system→instructions、工具往返→function_call/function_call_output、
// tools 扁平化、max_tokens 兜底与透传。
func TestBuildZenResponsesBody(t *testing.T) {
	params := map[string]any{
		"model": "muse-spark-1.3-contributor-free",
		"messages": []any{
			map[string]any{"role": "system", "content": "be brief"},
			map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "text", "text": "hello"},
			}},
			map[string]any{"role": "assistant", "content": "", "tool_calls": []any{
				map[string]any{
					"id":   "call_1",
					"type": "function",
					"function": map[string]any{
						"name":      "bash",
						"arguments": `{"cmd":"ls"}`,
					},
				},
			}},
			map[string]any{"role": "tool", "tool_call_id": "call_1", "content": "file.txt"},
		},
		"tools": []any{
			map[string]any{"type": "function", "function": map[string]any{
				"name": "bash", "description": "run shell",
				"parameters": map[string]any{"type": "object"},
			}},
		},
		"max_tokens":  float64(8),
		"temperature": 0.5,
	}
	body := buildZenResponsesBody(params)

	if body["model"] != "muse-spark-1.3-contributor-free" {
		t.Errorf("model = %v", body["model"])
	}
	if body["stream"] != true {
		t.Errorf("upstream responses must be forced stream, got %v", body["stream"])
	}
	if mt := zenInt64(body["max_output_tokens"]); mt != defaultMaxTokens {
		t.Errorf("max_output_tokens = %v, want clamped %d", body["max_output_tokens"], defaultMaxTokens)
	}
	if body["temperature"] != 0.5 {
		t.Errorf("temperature = %v", body["temperature"])
	}
	if instr, _ := body["instructions"].(string); instr != "be brief" {
		t.Errorf("instructions = %v", body["instructions"])
	}
	tools, _ := body["tools"].([]any)
	if len(tools) < len(anonymousCoreTools) {
		t.Fatalf("tools = %d, want >= %d (core tools topped up)", len(tools), len(anonymousCoreTools))
	}
	names := map[string]bool{}
	for _, item := range tools {
		tm, _ := item.(map[string]any)
		names[tm["name"].(string)] = true
		if _, nested := tm["function"]; nested {
			t.Errorf("tool must be flat, got %v", tm)
		}
	}
	for _, want := range anonymousCoreTools {
		if !names[want] {
			t.Errorf("missing core tool %q in %v", want, names)
		}
	}

	input, _ := body["input"].([]any)
	if len(input) != 3 { // user message + function_call + function_call_output
		t.Fatalf("input len = %d, want 3: %v", len(input), input)
	}
	userMsg, _ := input[0].(map[string]any)
	if userMsg["type"] != "message" || userMsg["role"] != "user" {
		t.Errorf("input[0] = %v", userMsg)
	}
	if content, ok := userMsg["content"].([]any); !ok || len(content) != 1 {
		t.Fatalf("user content = %v, want input_text parts", userMsg["content"])
	} else {
		part, _ := content[0].(map[string]any)
		if part["type"] != "input_text" || part["text"] != "hello" {
			t.Errorf("user part = %v", part)
		}
	}
	call, _ := input[1].(map[string]any)
	if call["type"] != "function_call" || call["call_id"] != "call_1" || call["name"] != "bash" {
		t.Errorf("input[1] = %v", call)
	}
	out, _ := input[2].(map[string]any)
	if out["type"] != "function_call_output" || out["call_id"] != "call_1" || out["output"] != "file.txt" {
		t.Errorf("input[2] = %v", out)
	}
}

// TestBuildZenResponsesBodyTinyToolCallParams 带嵌套 arguments（非 string）的
// 历史 tool_call 应序列化为 JSON 字符串。
func TestBuildZenResponsesBodyTinyToolCallParams(t *testing.T) {
	params := map[string]any{
		"model": "muse-spark-1.3-contributor-free",
		"messages": []any{
			map[string]any{"role": "assistant", "content": "", "tool_calls": []any{
				map[string]any{"id": "c1", "type": "function",
					"function": map[string]any{"name": "edit", "arguments": map[string]any{"path": "a.go"}}},
			}},
		},
		"max_completion_tokens": float64(16),
	}
	body := buildZenResponsesBody(params)
	if mt := zenInt64(body["max_output_tokens"]); mt != 16 {
		t.Errorf("max_output_tokens = %v, want passthrough 16", body["max_output_tokens"])
	}
	input, _ := body["input"].([]any)
	if len(input) != 1 {
		t.Fatalf("input len = %d", len(input))
	}
	call, _ := input[0].(map[string]any)
	if args, _ := call["arguments"].(string); args != `{"path":"a.go"}` {
		t.Errorf("arguments = %v", call["arguments"])
	}
}

// TestBuildZenResponsesBodyAnonymousTools 匿名免费层会补齐核心工具并扁平化。
func TestBuildZenResponsesBodyAnonymousTools(t *testing.T) {
	params := map[string]any{
		"model":    "muse-spark-1.3-contributor-free",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	}
	body := buildZenResponsesBody(params)
	tools, _ := body["tools"].([]any)
	if len(tools) < len(anonymousCoreTools) {
		t.Fatalf("anonymous tools = %d, want >= %d", len(tools), len(anonymousCoreTools))
	}
	names := map[string]bool{}
	for _, item := range tools {
		tm, _ := item.(map[string]any)
		names[tm["name"].(string)] = true
	}
	for _, want := range anonymousCoreTools {
		if !names[want] {
			t.Errorf("missing anonymous core tool %q in %v", want, names)
		}
	}
}

// responsesEventBuilder 便捷构造 Responses SSE 事件行。
func responsesEventBuilder(t *testing.T, events ...map[string]any) string {
	t.Helper()
	var b strings.Builder
	for _, e := range events {
		data, err := json.Marshal(e)
		if err != nil {
			t.Fatalf("marshal event: %v", err)
		}
		b.WriteString("event: " + e["type"].(string) + "\ndata: " + string(data) + "\n\n")
	}
	return b.String()
}

// TestPumpZenResponsesToChatStream 事件流 → chat.completion.chunk SSE 的实时转写。
func TestPumpZenResponsesToChatStream(t *testing.T) {
	sse := responsesEventBuilder(t,
		map[string]any{"type": "response.created", "response": map[string]any{
			"id": "resp_1", "model": "muse-spark-1.3-contributor-free",
		}},
		map[string]any{"type": "response.reasoning_summary_text.delta", "delta": "think"},
		map[string]any{"type": "response.output_text.delta", "delta": "Hel"},
		map[string]any{"type": "response.output_text.delta", "delta": "lo"},
		map[string]any{"type": "response.output_item.added", "item": map[string]any{
			"type": "function_call", "id": "fc_1", "call_id": "call_9", "name": "bash",
		}},
		map[string]any{"type": "response.function_call_arguments.delta", "item_id": "fc_1", "delta": `{"cm`},
		map[string]any{"type": "response.function_call_arguments.delta", "item_id": "fc_1", "delta": `d":"ls"}`},
		map[string]any{"type": "response.function_call_arguments.done", "item_id": "fc_1", "arguments": `{"cmd":"ls"}`},
		map[string]any{"type": "response.completed", "response": map[string]any{
			"id": "resp_1", "model": "muse-spark-1.3-contributor-free",
			"usage": map[string]any{"input_tokens": 10, "output_tokens": 5, "total_tokens": 15,
				"input_tokens_details": map[string]any{"cached_tokens": 4}},
		}},
	)
	sink := &zenChatChunkSink{w: &strings.Builder{}, id: "chatcmpl-x", created: 123, model: "muse-spark-1.3-contributor-free",
		toolIdx: map[string]int{}, toolSeen: map[string]bool{}}
	if err := pumpZenResponsesEvents(strings.NewReader(sse), sink); err != nil {
		t.Fatalf("pump: %v", err)
	}
	if sink.err != nil {
		t.Fatalf("sink err: %v", sink.err)
	}
	out := sink.w.(*strings.Builder).String()
	if !strings.Contains(out, "data: [DONE]") {
		t.Errorf("missing [DONE] terminator:\n%s", out)
	}
	var chunks []map[string]any
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data: ") || strings.Contains(line, "[DONE]") {
			continue
		}
		var c map[string]any
		if err := json.Unmarshal([]byte(line[6:]), &c); err != nil {
			t.Fatalf("bad chunk %q: %v", line, err)
		}
		chunks = append(chunks, c)
	}
	if len(chunks) != 9 { // role + reasoning + 2 content + tool start + 2 args + finish + usage
		t.Fatalf("chunk count = %d:\n%s", len(chunks), out)
	}
	expectDelta := []string{
		`{"content":"","role":"assistant"}`,
		`{"reasoning_content":"think"}`,
		`{"content":"Hel"}`,
		`{"content":"lo"}`,
	}
	for i, want := range expectDelta {
		delta, _ := chunks[i]["choices"].([]any)[0].(map[string]any)["delta"].(map[string]any)
		got, _ := json.Marshal(delta)
		if string(got) != want {
			t.Errorf("chunk %d delta = %s, want %s", i, got, want)
		}
	}
	// 工具调用：start 带名字，参数增量两段，done 与增量重复不得再发
	toolStart, _ := chunks[4]["choices"].([]any)[0].(map[string]any)["delta"].(map[string]any)
	tcs, _ := toolStart["tool_calls"].([]any)
	tc0, _ := tcs[0].(map[string]any)
	if tc0["id"] != "call_9" || tc0["index"] != float64(0) {
		t.Errorf("tool start = %v", tc0)
	}
	fn, _ := tc0["function"].(map[string]any)
	if fn["name"] != "bash" {
		t.Errorf("tool name = %v", fn)
	}
	var args strings.Builder
	for i := 5; i <= 6; i++ {
		delta, _ := chunks[i]["choices"].([]any)[0].(map[string]any)["delta"].(map[string]any)
		if tcs, ok := delta["tool_calls"].([]any); ok {
			tc, _ := tcs[0].(map[string]any)
			fn, _ := tc["function"].(map[string]any)
			args.WriteString(fn["arguments"].(string))
		}
	}
	if args.String() != `{"cmd":"ls"}` {
		t.Errorf("accumulated args = %q", args.String())
	}
	// 末两帧：finish_reason=tool_calls，再跟一帧 usage（choices 为空）
	finish, _ := chunks[7]["choices"].([]any)[0].(map[string]any)["finish_reason"].(string)
	if finish != "tool_calls" {
		t.Errorf("finish_reason = %v", finish)
	}
	usageChunk := chunks[8]
	if usageChunk["usage"] == nil {
		t.Errorf("final usage chunk = %v", usageChunk)
	}
	if choices, _ := usageChunk["choices"].([]any); len(choices) != 0 {
		t.Errorf("usage chunk should carry empty choices, got %v", choices)
	}
}

// TestPumpZenResponsesCollapse 事件流折叠成单个 chat.completion JSON。
func TestPumpZenResponsesCollapse(t *testing.T) {
	sse := responsesEventBuilder(t,
		map[string]any{"type": "response.created", "response": map[string]any{"id": "resp_2", "model": "muse"}},
		map[string]any{"type": "response.output_text.delta", "delta": "answer"},
		map[string]any{"type": "response.output_item.added", "item": map[string]any{
			"type": "function_call", "id": "fc_a", "call_id": "call_a", "name": "read",
		}},
		map[string]any{"type": "response.function_call_arguments.delta", "item_id": "fc_a", "delta": `{"pa`},
		map[string]any{"type": "response.function_call_arguments.done", "item_id": "fc_a", "arguments": `{"path":"x"}`},
		map[string]any{"type": "response.completed", "response": map[string]any{
			"model": "muse-spark-1.3-contributor-free",
			"usage": map[string]any{"input_tokens": 7, "output_tokens": 3},
		}},
	)
	acc := &zenCollapseAcc{Model: "muse", Created: 42}
	sink := &zenChatAccSink{acc: acc, toolIdx: map[string]int{}}
	if err := pumpZenResponsesEvents(strings.NewReader(sse), sink); err != nil {
		t.Fatalf("pump: %v", err)
	}
	if acc.Content != "answer" || acc.ID != "resp_2" {
		t.Errorf("acc = %+v", acc)
	}
	if acc.FinishReason != "tool_calls" {
		t.Errorf("finish_reason = %v", acc.FinishReason)
	}
	if acc.Usage.Prompt != 7 || acc.Usage.Completion != 3 || acc.Usage.Total != 10 || !acc.Usage.Valid {
		t.Errorf("usage = %+v", acc.Usage)
	}
	if len(acc.ToolCalls) != 1 || acc.ToolCalls[0].ID != "call_a" || acc.ToolCalls[0].Name != "read" {
		t.Fatalf("tool calls = %+v", acc.ToolCalls)
	}
	// done 全量参数与增量一致 → 不重复追加
	if acc.ToolCalls[0].Arguments != `{"path":"x"}` {
		t.Errorf("args = %q", acc.ToolCalls[0].Arguments)
	}
	resp, err := zenCollapseToChatResponse(acc, nil)
	if err != nil {
		t.Fatalf("build response: %v", err)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out["object"] != "chat.completion" || out["model"] != "muse-spark-1.3-contributor-free" {
		t.Errorf("response = %v", out)
	}
}
