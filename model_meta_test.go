package main

import "testing"

// 已知硬限制表按基名匹配：Cline 路由前缀（cline-free/ cline-pass/ google/ cline/）
// 剥掉后查表；其他 provider 前缀（z-ai/ 等）不属于 Cline 路由，整段保留。
func TestModelBaseName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"cline-free/gemini-3.8-flash", "gemini-3.8-flash"},
		{"cline-pass/gemini-3.8-flash", "gemini-3.8-flash"},
		{"google/gemini-3.8-flash", "gemini-3.8-flash"},
		{"cline/gemini-3.8-flash", "gemini-3.8-flash"},
		{"gemini-3.8-flash", "gemini-3.8-flash"},
		{"z-ai/glm-5.3-flash", "z-ai/glm-5.3-flash"},
		{"", ""},
	}
	for _, c := range cases {
		if got := modelBaseName(c.in); got != c.want {
			t.Errorf("modelBaseName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestLookupClineModelMeta(t *testing.T) {
	for _, id := range []string{"gemini-3.8-flash", "google/gemini-3.8-flash", "cline-free/gemini-3.8-flash"} {
		meta, ok := lookupClineModelMeta(id)
		if !ok {
			t.Errorf("lookupClineModelMeta(%q) should hit", id)
			continue
		}
		if meta.Output != 65536 {
			t.Errorf("lookupClineModelMeta(%q).Output = %d, want 65536", id, meta.Output)
		}
		if meta.Context != 1048576 {
			t.Errorf("lookupClineModelMeta(%q).Context = %d, want 1048576", id, meta.Context)
		}
	}
	if _, ok := lookupClineModelMeta("z-ai/glm-5.3-flash"); ok {
		t.Error("unknown model should miss the meta table")
	}
}

// modelMaxOutputLimit：已知表优先，其次池中模型 Output（zen 估值不算硬上限）。
func TestModelMaxOutputLimit(t *testing.T) {
	oldPool := pool
	t.Cleanup(func() { pool = oldPool })

	pool = &AccountPool{Models: []Model{
		{ID: "my-model", Output: 8192},
		{ID: "deepseek-v4-flash-free", Source: "zen", Output: 128000},
	}}

	if got := modelMaxOutputLimit("google/gemini-3.8-flash"); got != 65536 {
		t.Errorf("table limit = %d, want 65536", got)
	}
	if got := modelMaxOutputLimit("my-model"); got != 8192 {
		t.Errorf("pool model limit = %d, want 8192", got)
	}
	if got := modelMaxOutputLimit("deepseek-v4-flash-free"); got != 0 {
		t.Errorf("zen model output is a compaction estimate, must not cap (got %d)", got)
	}
	if got := modelMaxOutputLimit("unknown-model"); got != 0 {
		t.Errorf("unknown model limit = %d, want 0", got)
	}
}

// gemini-3.8-flash 输出预算封顶：默认 128000 / 超大 200000 都收到 65536，
// 低于上限的原样透传；其他模型不受影响。
// 背景：Vercel 网关 vertex 路由 maxOutputTokens 支持范围 1~65536，
// 128000 直接 400 invalid argument（429 配额限流回退路由后必现）。
func TestBuildUpstreamBodyCapsGeminiMaxTokens(t *testing.T) {
	oldPool := pool
	t.Cleanup(func() { pool = oldPool })
	pool = &AccountPool{}

	cases := []struct {
		name   string
		params map[string]any
		want   int
	}{
		{"gemini-default", map[string]any{"model": "google/gemini-3.8-flash"}, 65536},
		{"gemini-free-alias", map[string]any{"model": "cline-free/gemini-3.8-flash"}, 65536},
		{"gemini-huge-explicit", map[string]any{"model": "google/gemini-3.8-flash", "max_tokens": float64(200000)}, 65536},
		{"gemini-under-limit", map[string]any{"model": "google/gemini-3.8-flash", "max_tokens": float64(1024)}, 1024},
		{"gemini-at-limit", map[string]any{"model": "google/gemini-3.8-flash", "max_tokens": float64(65536)}, 65536},
		{"other-model-unaffected", map[string]any{"model": "z-ai/glm-5.3-flash"}, defaultMaxTokens},
	}
	for _, tc := range cases {
		body := buildUpstreamBody(tc.params, false)
		if got, _ := body["max_tokens"].(int); got != tc.want {
			t.Errorf("%s: max_tokens = %d, want %d", tc.name, got, tc.want)
		}
	}
}
