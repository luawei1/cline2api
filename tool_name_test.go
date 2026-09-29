package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// 测试用 71 字符工具名：与 ZCode android-emulator 插件实际产生的超长 MCP 工具名同形。
const testLongToolName = "mcp__plugin_android-emulator_android-emulator__android_discover_project"

func TestShortenToolName(t *testing.T) {
	got := shortenToolName(testLongToolName)
	if len(got) > maxUpstreamToolNameLength {
		t.Fatalf("shortened name too long: %d (%q)", len(got), got)
	}
	if again := shortenToolName(testLongToolName); again != got {
		t.Fatalf("not deterministic: %q vs %q", got, again)
	}
	// 同前缀不同原名必须得到不同短名（哈希后缀区分）
	other := testLongToolName + "x"
	if shortenToolName(other) == got {
		t.Fatalf("collision between %q and %q", testLongToolName, other)
	}
}

func TestClampParamsToolNames(t *testing.T) {
	params := map[string]any{
		"tools": []any{
			map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        testLongToolName,
					"description": "d",
					"parameters":  map[string]any{"type": "object", "properties": map[string]any{}},
				},
			},
			map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        "mcp__short__ok",
					"description": "d",
				},
			},
		},
		"messages": []any{
			map[string]any{"role": "user", "content": "hi"},
			map[string]any{
				"role": "assistant",
				"tool_calls": []any{
					map[string]any{
						"id":   "call_1",
						"type": "function",
						"function": map[string]any{
							"name":      testLongToolName,
							"arguments": "{}",
						},
					},
				},
			},
		},
	}

	m := clampParamsToolNames(params)
	if m == nil {
		t.Fatal("expected non-nil mapping for long tool name")
	}
	short, ok := params["tools"].([]any)[0].(map[string]any)["function"].(map[string]any)["name"].(string)
	if !ok || len(short) > maxUpstreamToolNameLength {
		t.Fatalf("tools name not clamped: %v", short)
	}
	if m[short] != testLongToolName {
		t.Fatalf("mapping short->orig broken: %q -> %q", short, m[short])
	}
	// 短名工具不动
	if n := params["tools"].([]any)[1].(map[string]any)["function"].(map[string]any)["name"]; n != "mcp__short__ok" {
		t.Fatalf("short tool name was modified: %v", n)
	}
	// 历史 tool_calls 与 tools 定义映射一致
	hist := params["messages"].([]any)[1].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)["name"].(string)
	if hist != short {
		t.Fatalf("history name %q != tools name %q", hist, short)
	}
}

func TestClampParamsToolNamesNoop(t *testing.T) {
	params := map[string]any{
		"tools": []any{
			map[string]any{"type": "function", "function": map[string]any{"name": "bash"}},
		},
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	}
	if m := clampParamsToolNames(params); m != nil {
		t.Fatalf("expected nil mapping when nothing to clamp, got %v", m)
	}
	if n := params["tools"].([]any)[0].(map[string]any)["function"].(map[string]any)["name"]; n != "bash" {
		t.Fatalf("name modified: %v", n)
	}
}

func TestClampParamsToolChoice(t *testing.T) {
	params := map[string]any{
		"tools": []any{
			map[string]any{"type": "function", "function": map[string]any{"name": testLongToolName}},
		},
		"messages":    []any{map[string]any{"role": "user", "content": "hi"}},
		"tool_choice": "auto",
	}
	m := clampParamsToolNames(params)
	if m == nil {
		t.Fatal("expected non-nil mapping")
	}
	// 字符串枚举 tool_choice 不动
	if tc := params["tool_choice"]; tc != "auto" {
		t.Fatalf("string tool_choice modified: %v", tc)
	}

	params2 := map[string]any{
		"tools":       []any{map[string]any{"type": "function", "function": map[string]any{"name": testLongToolName}}},
		"messages":    []any{map[string]any{"role": "user", "content": "hi"}},
		"tool_choice": map[string]any{"type": "function", "function": map[string]any{"name": testLongToolName}},
	}
	m2 := clampParamsToolNames(params2)
	short := shortenToolName(testLongToolName)
	got := params2["tool_choice"].(map[string]any)["function"].(map[string]any)["name"].(string)
	if got != short || m2[short] != testLongToolName {
		t.Fatalf("tool_choice name not clamped: %q (map=%v)", got, m2)
	}
}

func TestRestoreToolCallsInResponse(t *testing.T) {
	m := clampParamsToolNames(map[string]any{
		"tools": []any{map[string]any{"type": "function", "function": map[string]any{"name": testLongToolName}}},
	})
	short := shortenToolName(testLongToolName)

	// 流式 delta 形态
	streamChunk := map[string]any{
		"choices": []any{map[string]any{
			"delta": map[string]any{
				"tool_calls": []any{map[string]any{
					"index":    float64(0),
					"id":       "call_1",
					"function": map[string]any{"name": short, "arguments": "{}"},
				}},
			},
		}},
	}
	restoreToolCallsInResponse(streamChunk, m)
	got := streamChunk["choices"].([]any)[0].(map[string]any)["delta"].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)["name"]
	if got != testLongToolName {
		t.Fatalf("stream delta name not restored: %v", got)
	}

	// 非流式 message 形态
	nonStream := map[string]any{
		"choices": []any{map[string]any{
			"message": map[string]any{
				"role": "assistant",
				"tool_calls": []any{map[string]any{
					"id":       "call_1",
					"function": map[string]any{"name": short, "arguments": "{}"},
				}},
			},
		}},
	}
	restoreToolCallsInResponse(nonStream, m)
	got = nonStream["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)["name"]
	if got != testLongToolName {
		t.Fatalf("message name not restored: %v", got)
	}

	// 空映射 no-op
	unchanged := map[string]any{"choices": []any{}}
	restoreToolCallsInResponse(unchanged, nil)
	b, _ := json.Marshal(unchanged)
	if !strings.Contains(string(b), "choices") {
		t.Fatal("unexpected mutation")
	}
}
