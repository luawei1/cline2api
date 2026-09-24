package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type freeModelRoundTripper func(*http.Request) (*http.Response, error)

func (f freeModelRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestCallClineAPIRefreshRetryReplaysRequestBody(t *testing.T) {
	oldConfig := getProxyConfig()
	oldTransport := httpClient.Transport
	t.Cleanup(func() {
		setProxyConfig(oldConfig)
		httpClient.Transport = oldTransport
	})

	account := &Account{
		AccountID:    "refresh-account",
		Email:        "refresh@example.com",
		RefreshToken: "refresh-token",
		AccessToken:  "old-access-token",
		ExpiresAt:    time.Now().Add(time.Hour).UnixMilli(),
		Status:       "active",
	}
	setProxyConfig(defaultProxyConfig())

	var requestBodies []map[string]any
	refreshCalls := 0
	httpClient.Transport = freeModelRoundTripper(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/api/v1/auth/refresh":
			refreshCalls++
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"data":{"accessToken":"new-access-token","refreshToken":"new-refresh-token","expiresAt":4102444800000}}`)),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		case "/api/v1/chat/completions":
			body, err := io.ReadAll(req.Body)
			if err != nil {
				return nil, err
			}
			var payload map[string]any
			if err := json.Unmarshal(body, &payload); err != nil {
				return nil, err
			}
			requestBodies = append(requestBodies, payload)
			if len(requestBodies) == 1 {
				return &http.Response{
					StatusCode: http.StatusUnauthorized,
					Body:       io.NopCloser(strings.NewReader(`{"error":"unauthorized"}`)),
					Header:     make(http.Header),
					Request:    req,
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"id":"ok","choices":[]}`)),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		default:
			return nil, fmt.Errorf("unexpected request path %s", req.URL.Path)
		}
	})

	params := map[string]any{
		"model":    freeModelPrimary,
		"messages": []any{map[string]any{"role": "user", "content": "hello"}},
	}
	resp, _, err := callClineAPIWithAccount(account, params, false)
	if err != nil {
		t.Fatalf("callClineAPIWithAccount returned error: %v", err)
	}
	resp.Body.Close()
	if refreshCalls != 1 {
		t.Fatalf("refresh calls = %d, want 1", refreshCalls)
	}
	if len(requestBodies) != 2 {
		t.Fatalf("Cline request count = %d, want 2", len(requestBodies))
	}
	for index, body := range requestBodies {
		if body["model"] != freeModelPrimary {
			t.Fatalf("request %d model = %v, want %q", index, body["model"], freeModelPrimary)
		}
		messages, ok := body["messages"].([]any)
		if !ok || len(messages) == 0 {
			t.Fatalf("request %d messages = %v, want non-empty body", index, body["messages"])
		}
	}
}

func TestCallClineAPIFreeRetriesNextGLMAccountAfterTokenRefreshFailure(t *testing.T) {
	oldPool := pool
	oldConfig := getProxyConfig()
	oldTransport := httpClient.Transport
	t.Cleanup(func() {
		pool = oldPool
		setProxyConfig(oldConfig)
		httpClient.Transport = oldTransport
	})

	first := &Account{
		AccountID:    "refresh-failed",
		Email:        "refresh-failed@example.com",
		RefreshToken: "refresh-one",
		ExpiresAt:    time.Now().Add(-time.Hour).UnixMilli(),
		Status:       "active",
	}
	second := &Account{
		AccountID:   "glm-two",
		Email:       "two@example.com",
		AccessToken: "token-two",
		ExpiresAt:   time.Now().Add(time.Hour).UnixMilli(),
		Status:      "active",
	}
	pool = &AccountPool{Accounts: []*Account{first, second}}
	setProxyConfig(defaultProxyConfig())

	var paths []string
	var attempts []string
	var models []string
	httpClient.Transport = freeModelRoundTripper(func(req *http.Request) (*http.Response, error) {
		paths = append(paths, req.URL.Path)
		switch req.URL.Path {
		case "/api/v1/auth/refresh":
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       io.NopCloser(strings.NewReader(`{"error":"refresh failed"}`)),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		case "/api/v1/chat/completions":
			token := strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer ")
			attempts = append(attempts, token)
			body, err := io.ReadAll(req.Body)
			if err != nil {
				return nil, err
			}
			var upstream map[string]any
			if err := json.Unmarshal(body, &upstream); err != nil {
				return nil, err
			}
			model, _ := upstream["model"].(string)
			models = append(models, model)
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"id":"ok","choices":[]}`)),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		default:
			return nil, fmt.Errorf("unexpected request path %s", req.URL.Path)
		}
	})

	params := map[string]any{
		"model":    "free",
		"messages": []any{map[string]any{"role": "user", "content": "hello"}},
	}
	resp, acc, err := callClineAPI(params, false)
	if err != nil {
		t.Fatalf("callClineAPI returned error: %v", err)
	}
	resp.Body.Close()
	if acc != second {
		t.Fatalf("selected account = %v, want second GLM account", acc)
	}
	if got, want := strings.Join(paths, ","), "/api/v1/auth/refresh,/api/v1/chat/completions"; got != want {
		t.Fatalf("request paths = %q, want %q", got, want)
	}
	if got, want := strings.Join(attempts, ","), "token-two"; got != want {
		t.Fatalf("Cline attempts = %q, want %q", got, want)
	}
	if got, want := strings.Join(models, ","), freeModelPrimary; got != want {
		t.Fatalf("models = %q, want %q", got, want)
	}
	if first.Status != "cooldown" {
		t.Fatalf("first account status = %q, want cooldown (a 500 from /auth/refresh is transient)", first.Status)
	}
}

func TestCallClineAPIFreeRetriesNextGLMAccountAfterTransportFailure(t *testing.T) {
	oldPool := pool
	oldConfig := getProxyConfig()
	oldTransport := httpClient.Transport
	t.Cleanup(func() {
		pool = oldPool
		setProxyConfig(oldConfig)
		httpClient.Transport = oldTransport
	})

	first := &Account{
		AccountID:   "transport-failed",
		Email:       "transport-failed@example.com",
		AccessToken: "token-one",
		ExpiresAt:   time.Now().Add(time.Hour).UnixMilli(),
		Status:      "active",
	}
	second := &Account{
		AccountID:   "glm-two",
		Email:       "two@example.com",
		AccessToken: "token-two",
		ExpiresAt:   time.Now().Add(time.Hour).UnixMilli(),
		Status:      "active",
	}
	pool = &AccountPool{Accounts: []*Account{first, second}}
	setProxyConfig(defaultProxyConfig())

	var attempts []string
	var models []string
	httpClient.Transport = freeModelRoundTripper(func(req *http.Request) (*http.Response, error) {
		token := strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer ")
		attempts = append(attempts, token)
		if token == "token-one" {
			return nil, fmt.Errorf("connection reset")
		}
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		var upstream map[string]any
		if err := json.Unmarshal(body, &upstream); err != nil {
			return nil, err
		}
		model, _ := upstream["model"].(string)
		models = append(models, model)
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"id":"ok","choices":[]}`)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})

	params := map[string]any{
		"model":    "free",
		"messages": []any{map[string]any{"role": "user", "content": "hello"}},
	}
	resp, acc, err := callClineAPI(params, false)
	if err != nil {
		t.Fatalf("callClineAPI returned error: %v", err)
	}
	resp.Body.Close()
	if acc != second {
		t.Fatalf("selected account = %v, want second GLM account", acc)
	}
	if got, want := strings.Join(attempts, ","), "token-one,token-two"; got != want {
		t.Fatalf("Cline attempts = %q, want %q", got, want)
	}
	if got, want := strings.Join(models, ","), freeModelPrimary; got != want {
		t.Fatalf("models = %q, want %q", got, want)
	}
	if first.Status != "cooldown" {
		t.Fatalf("first account status = %q, want cooldown", first.Status)
	}
}

func TestCallClineAPIFreeRetriesNextGLMAccountAfterQuota429(t *testing.T) {
	oldPool := pool
	oldConfig := getProxyConfig()
	oldTransport := httpClient.Transport
	t.Cleanup(func() {
		pool = oldPool
		setProxyConfig(oldConfig)
		httpClient.Transport = oldTransport
	})

	first := &Account{
		AccountID:   "glm-one",
		Email:       "one@example.com",
		AccessToken: "token-one",
		ExpiresAt:   time.Now().Add(time.Hour).UnixMilli(),
		Status:      "active",
	}
	second := &Account{
		AccountID:   "glm-two",
		Email:       "two@example.com",
		AccessToken: "token-two",
		ExpiresAt:   time.Now().Add(time.Hour).UnixMilli(),
		Status:      "active",
	}
	pool = &AccountPool{Accounts: []*Account{first, second}}
	setProxyConfig(defaultProxyConfig())

	var attempts []string
	var models []string
	httpClient.Transport = freeModelRoundTripper(func(req *http.Request) (*http.Response, error) {
		token := strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer ")
		attempts = append(attempts, token)
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		var upstream map[string]any
		if err := json.Unmarshal(body, &upstream); err != nil {
			return nil, err
		}
		model, _ := upstream["model"].(string)
		models = append(models, model)
		if token == "token-one" {
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Body:       io.NopCloser(strings.NewReader(`{"error":"quota","message":"Try again in 1h"}`)),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"id":"ok","choices":[]}`)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})

	params := map[string]any{
		"model":    "free",
		"messages": []any{map[string]any{"role": "user", "content": "hello"}},
	}
	resp, acc, err := callClineAPI(params, false)
	if err != nil {
		t.Fatalf("callClineAPI returned error: %v", err)
	}
	if resp == nil {
		t.Fatal("callClineAPI returned nil response")
	}
	resp.Body.Close()

	if acc != second {
		t.Fatalf("selected account = %v, want second GLM account", acc)
	}
	if got, want := strings.Join(attempts, ","), "token-one,token-two"; got != want {
		t.Fatalf("attempts = %q, want %q", got, want)
	}
	for index, model := range models {
		if model != freeModelPrimary {
			t.Fatalf("attempt %d model = %q, want %q", index, model, freeModelPrimary)
		}
	}
	if got, want := params["model"], freeModelPrimary; got != want {
		t.Fatalf("effective model = %v, want %q", got, want)
	}
}

func TestCallClineAPIFreeFallsBackToDSAfterAllGLMAccountsUnavailable(t *testing.T) {
	oldPool := pool
	oldConfig := getProxyConfig()
	oldTransport := httpClient.Transport
	t.Cleanup(func() {
		pool = oldPool
		setProxyConfig(oldConfig)
		httpClient.Transport = oldTransport
	})

	first := &Account{
		AccountID:   "glm-one",
		Email:       "one@example.com",
		AccessToken: "token-one",
		ExpiresAt:   time.Now().Add(time.Hour).UnixMilli(),
		Status:      "active",
	}
	second := &Account{
		AccountID:   "glm-two",
		Email:       "two@example.com",
		AccessToken: "token-two",
		ExpiresAt:   time.Now().Add(time.Hour).UnixMilli(),
		Status:      "active",
	}
	pool = &AccountPool{Accounts: []*Account{first, second}}
	setProxyConfig(defaultProxyConfig())

	var attempts []string
	var models []string
	httpClient.Transport = freeModelRoundTripper(func(req *http.Request) (*http.Response, error) {
		token := strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer ")
		attempts = append(attempts, token)
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		var upstream map[string]any
		if err := json.Unmarshal(body, &upstream); err != nil {
			return nil, err
		}
		model, _ := upstream["model"].(string)
		models = append(models, model)
		if token == "token-one" && model == "z-ai/glm-5.3-flash" || token == "token-two" {
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Body:       io.NopCloser(strings.NewReader(`{"error":"quota","message":"Try again in 1h"}`)),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"id":"ok","choices":[]}`)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})

	params := map[string]any{
		"model":    "free",
		"messages": []any{map[string]any{"role": "user", "content": "hello"}},
	}
	resp, acc, err := callClineAPI(params, false)
	if err != nil {
		t.Fatalf("callClineAPI returned error: %v", err)
	}
	if resp == nil {
		t.Fatal("callClineAPI returned nil response")
	}
	resp.Body.Close()

	if acc != first {
		t.Fatalf("selected account = %v, want first DS account", acc)
	}
	if got, want := strings.Join(attempts, ","), "token-one,token-two,token-one"; got != want {
		t.Fatalf("attempts = %q, want %q", got, want)
	}
	if got, want := strings.Join(models, ","), "z-ai/glm-5.3-flash,z-ai/glm-5.3-flash,deepseek/deepseek-v4-flash"; got != want {
		t.Fatalf("models = %q, want %q", got, want)
	}
	if got, want := params["model"], "deepseek/deepseek-v4-flash"; got != want {
		t.Fatalf("effective model = %v, want %q", got, want)
	}
}

func TestCallClineAPIFreeRetriesNextDSAccountAfterQuota429(t *testing.T) {
	oldPool := pool
	oldConfig := getProxyConfig()
	oldTransport := httpClient.Transport
	t.Cleanup(func() {
		pool = oldPool
		setProxyConfig(oldConfig)
		httpClient.Transport = oldTransport
	})

	first := &Account{
		AccountID:   "ds-one",
		Email:       "one@example.com",
		AccessToken: "token-one",
		ExpiresAt:   time.Now().Add(time.Hour).UnixMilli(),
		Status:      "active",
		ModelCooldowns: map[string]time.Time{
			freeModelPrimary: time.Now().Add(time.Hour),
		},
	}
	second := &Account{
		AccountID:   "ds-two",
		Email:       "two@example.com",
		AccessToken: "token-two",
		ExpiresAt:   time.Now().Add(time.Hour).UnixMilli(),
		Status:      "active",
		ModelCooldowns: map[string]time.Time{
			freeModelPrimary: time.Now().Add(time.Hour),
		},
	}
	pool = &AccountPool{Accounts: []*Account{first, second}}
	setProxyConfig(defaultProxyConfig())

	var attempts []string
	var models []string
	httpClient.Transport = freeModelRoundTripper(func(req *http.Request) (*http.Response, error) {
		token := strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer ")
		attempts = append(attempts, token)
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		var upstream map[string]any
		if err := json.Unmarshal(body, &upstream); err != nil {
			return nil, err
		}
		model, _ := upstream["model"].(string)
		models = append(models, model)
		if token == "token-one" {
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Body:       io.NopCloser(strings.NewReader(`{"error":"quota","message":"Try again in 1h"}`)),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"id":"ok","choices":[]}`)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})

	params := map[string]any{
		"model":    "free",
		"messages": []any{map[string]any{"role": "user", "content": "hello"}},
	}
	resp, acc, err := callClineAPI(params, false)
	if err != nil {
		t.Fatalf("callClineAPI returned error: %v", err)
	}
	if resp == nil {
		t.Fatal("callClineAPI returned nil response")
	}
	resp.Body.Close()

	if acc != second {
		t.Fatalf("selected account = %v, want second DS account", acc)
	}
	if got, want := strings.Join(attempts, ","), "token-one,token-two"; got != want {
		t.Fatalf("attempts = %q, want %q", got, want)
	}
	if got, want := strings.Join(models, ","), "deepseek/deepseek-v4-flash,deepseek/deepseek-v4-flash"; got != want {
		t.Fatalf("models = %q, want %q", got, want)
	}
	if got, want := params["model"], freeModelFallback; got != want {
		t.Fatalf("effective model = %v, want %q", got, want)
	}
	if !modelCooldownActive(first, freeModelFallback) {
		t.Fatal("first DS account should retain its DS cooldown")
	}
	if !modelCooldownActive(first, freeModelPrimary) {
		t.Fatal("first DS account should retain its GLM cooldown")
	}
}

func TestCallClineAPIFreeReturnsToGLMAfterCooldownExpires(t *testing.T) {
	oldPool := pool
	oldConfig := getProxyConfig()
	oldTransport := httpClient.Transport
	t.Cleanup(func() {
		pool = oldPool
		setProxyConfig(oldConfig)
		httpClient.Transport = oldTransport
	})

	account := &Account{
		AccountID:   "recovery-account",
		Email:       "recovery@example.com",
		AccessToken: "recovery-token",
		ExpiresAt:   time.Now().Add(time.Hour).UnixMilli(),
		Status:      "active",
		ModelCooldowns: map[string]time.Time{
			freeModelPrimary: time.Now().Add(time.Hour),
		},
	}
	pool = &AccountPool{Accounts: []*Account{account}}
	setProxyConfig(defaultProxyConfig())

	var models []string
	httpClient.Transport = freeModelRoundTripper(func(req *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		var upstream map[string]any
		if err := json.Unmarshal(body, &upstream); err != nil {
			return nil, err
		}
		model, _ := upstream["model"].(string)
		models = append(models, model)
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"id":"ok","choices":[]}`)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})

	firstResp, _, err := callClineAPI(map[string]any{"model": "free"}, false)
	if err != nil {
		t.Fatalf("first callClineAPI returned error: %v", err)
	}
	firstResp.Body.Close()
	if got, want := models[0], freeModelFallback; got != want {
		t.Fatalf("first effective model = %q, want %q", got, want)
	}

	account.ModelCooldowns[freeModelPrimary] = time.Now().Add(-time.Minute)
	secondResp, _, err := callClineAPI(map[string]any{"model": "free"}, false)
	if err != nil {
		t.Fatalf("second callClineAPI returned error: %v", err)
	}
	secondResp.Body.Close()
	if got, want := models[1], freeModelPrimary; got != want {
		t.Fatalf("second effective model = %q, want %q", got, want)
	}
}

func TestCallClineAPIFreeKeepsModelCooldownsIndependent(t *testing.T) {
	oldPool := pool
	oldConfig := getProxyConfig()
	oldTransport := httpClient.Transport
	t.Cleanup(func() {
		pool = oldPool
		setProxyConfig(oldConfig)
		httpClient.Transport = oldTransport
	})

	for _, test := range []struct {
		name          string
		cooldownModel string
		wantModel     string
	}{
		{name: "GLM cooldown allows DS", cooldownModel: freeModelPrimary, wantModel: freeModelFallback},
		{name: "DS cooldown allows GLM", cooldownModel: freeModelFallback, wantModel: freeModelPrimary},
	} {
		t.Run(test.name, func(t *testing.T) {
			account := &Account{
				AccountID:   "independent-account",
				Email:       "independent@example.com",
				AccessToken: "independent-token",
				ExpiresAt:   time.Now().Add(time.Hour).UnixMilli(),
				Status:      "active",
				ModelCooldowns: map[string]time.Time{
					test.cooldownModel: time.Now().Add(time.Hour),
				},
			}
			pool = &AccountPool{Accounts: []*Account{account}}
			setProxyConfig(defaultProxyConfig())

			var upstreamModel string
			httpClient.Transport = freeModelRoundTripper(func(req *http.Request) (*http.Response, error) {
				body, err := io.ReadAll(req.Body)
				if err != nil {
					return nil, err
				}
				var upstream map[string]any
				if err := json.Unmarshal(body, &upstream); err != nil {
					return nil, err
				}
				upstreamModel, _ = upstream["model"].(string)
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"id":"ok","choices":[]}`)),
					Header:     make(http.Header),
					Request:    req,
				}, nil
			})

			resp, _, err := callClineAPI(map[string]any{"model": "free"}, false)
			if err != nil {
				t.Fatalf("callClineAPI returned error: %v", err)
			}
			resp.Body.Close()
			if upstreamModel != test.wantModel {
				t.Fatalf("upstream model = %q, want %q", upstreamModel, test.wantModel)
			}
			if !modelCooldownActive(account, test.cooldownModel) {
				t.Fatalf("cooldown for %s was lost", test.cooldownModel)
			}
			if modelCooldownActive(account, test.wantModel) {
				t.Fatalf("cooldown for %s unexpectedly blocked", test.wantModel)
			}
		})
	}
}

func TestCallClineAPIFreeDoesNotPickCoolingAccount(t *testing.T) {
	oldPool := pool
	oldConfig := getProxyConfig()
	oldTransport := httpClient.Transport
	t.Cleanup(func() {
		pool = oldPool
		setProxyConfig(oldConfig)
		httpClient.Transport = oldTransport
	})

	account := &Account{
		AccountID:      "glm-cooling",
		Email:          "cooling@example.com",
		AccessToken:    "token-cooling",
		ExpiresAt:      time.Now().Add(time.Hour).UnixMilli(),
		Status:         "active",
		ModelCooldowns: map[string]time.Time{},
	}
	for _, model := range freeModelChain {
		account.ModelCooldowns[model] = time.Now().Add(time.Hour)
	}
	pool = &AccountPool{Accounts: []*Account{account}}
	setProxyConfig(defaultProxyConfig())

	calls := 0
	httpClient.Transport = freeModelRoundTripper(func(req *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"id":"unexpected","choices":[]}`)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})

	_, _, err := callClineAPI(map[string]any{"model": "free"}, false)
	if err == nil {
		t.Fatal("callClineAPI should fail when every GLM account is cooling")
	}
	if calls != 0 {
		t.Fatalf("upstream calls = %d, want 0", calls)
	}
}

func TestPickAccountForModelStrictPreservesStrategy(t *testing.T) {
	oldPool := pool
	oldConfig := getProxyConfig()
	t.Cleanup(func() {
		pool = oldPool
		setProxyConfig(oldConfig)
	})

	first := &Account{AccountID: "first", Status: "active"}
	second := &Account{AccountID: "second", Status: "active"}
	pool = &AccountPool{Accounts: []*Account{first, second}}

	for _, strategy := range []string{"fill", "round_robin", "random"} {
		t.Run(strategy, func(t *testing.T) {
			pool.CurrentIdx = 0
			cfg := defaultProxyConfig()
			cfg.Strategy = strategy
			setProxyConfig(cfg)

			firstPick := pickAccountForModelStrict(freeModelPrimary)
			if firstPick != first && firstPick != second {
				t.Fatalf("first pick = %v, want an active account", firstPick)
			}
			if strategy == "fill" && firstPick != first {
				t.Fatalf("fill first pick = %v, want first account", firstPick)
			}
			if strategy == "round_robin" {
				secondPick := pickAccountForModelStrict(freeModelPrimary)
				if firstPick != first || secondPick != second {
					t.Fatalf("round_robin picks = %v, %v, want first, second", firstPick, secondPick)
				}
			}
		})
	}
}

// 显式模型请求在 429（模型冷却）时应沿 free 链自动降级到下一个可用模型。
func TestCallClineAPIDirectModelsFallBackOnModelCooldown(t *testing.T) {
	oldPool := pool
	oldConfig := getProxyConfig()
	oldTransport := httpClient.Transport
	t.Cleanup(func() {
		pool = oldPool
		setProxyConfig(oldConfig)
		httpClient.Transport = oldTransport
	})

	for _, model := range []string{"z-ai/glm-5.3-flash", "deepseek/deepseek-v4-flash"} {
		t.Run(model, func(t *testing.T) {
			account := &Account{
				AccountID:   "direct-account",
				Email:       "direct@example.com",
				AccessToken: "direct-token",
				ExpiresAt:   time.Now().Add(time.Hour).UnixMilli(),
				Status:      "active",
			}
			pool = &AccountPool{Accounts: []*Account{account}}
			setProxyConfig(defaultProxyConfig())

			var attempted []string
			httpClient.Transport = freeModelRoundTripper(func(req *http.Request) (*http.Response, error) {
				body, err := io.ReadAll(req.Body)
				if err != nil {
					return nil, err
				}
				var params map[string]any
				if err := json.Unmarshal(body, &params); err != nil {
					return nil, err
				}
				upstreamModel, _ := params["model"].(string)
				attempted = append(attempted, upstreamModel)
				if upstreamModel == model {
					// 点名模型冷却
					return &http.Response{
						StatusCode: http.StatusTooManyRequests,
						Body:       io.NopCloser(strings.NewReader(`{"error":"quota"}`)),
						Header:     make(http.Header),
						Request:    req,
					}, nil
				}
				// 降级模型成功
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"id":"ok","choices":[{"message":{"role":"assistant","content":"hi"}}]}`)),
					Header:     make(http.Header),
					Request:    req,
				}, nil
			})

			params := map[string]any{"model": model}
			resp, _, err := callClineAPI(params, false)
			if err != nil {
				t.Fatalf("expected fallback success, got %v", err)
			}
			defer resp.Body.Close()
			if len(attempted) < 2 {
				t.Fatalf("expected fallback attempts after cooling model, got %v", attempted)
			}
			if attempted[0] != model {
				t.Fatalf("first attempt = %q, want requested %q", attempted[0], model)
			}
			if attempted[len(attempted)-1] == model {
				t.Fatal("fallback should not retry the cooling model")
			}
			// 回写验证：params["model"] 必须反映实际服务模型（供日志归因）
			servedModel, _ := params["model"].(string)
			if servedModel != attempted[len(attempted)-1] {
				t.Fatalf("params model after fallback = %q, want last attempted %q", servedModel, attempted[len(attempted)-1])
			}
		})
	}
}

// 非冷却类上游错误（如 500）会先试完整条回退链，全部失败后才报错。
func TestCallClineAPIDirectModelsNoFallbackOnServerError(t *testing.T) {
	oldPool := pool
	oldConfig := getProxyConfig()
	oldTransport := httpClient.Transport
	t.Cleanup(func() {
		pool = oldPool
		setProxyConfig(oldConfig)
		httpClient.Transport = oldTransport
	})

	model := "z-ai/glm-5.3-flash"
	account := &Account{
		AccountID:   "direct-account",
		Email:       "direct@example.com",
		AccessToken: "direct-token",
		ExpiresAt:   time.Now().Add(time.Hour).UnixMilli(),
		Status:      "active",
	}
	pool = &AccountPool{Accounts: []*Account{account}}
	setProxyConfig(defaultProxyConfig())

	calls := 0
	var upstreamModel string
	httpClient.Transport = freeModelRoundTripper(func(req *http.Request) (*http.Response, error) {
		calls++
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		var params map[string]any
		if err := json.Unmarshal(body, &params); err != nil {
			return nil, err
		}
		upstreamModel, _ = params["model"].(string)
		return &http.Response{
			StatusCode: http.StatusInternalServerError,
			Body:       io.NopCloser(strings.NewReader(`{"error":"boom"}`)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})

	params := map[string]any{"model": model}
	_, _, err := callClineAPI(params, false)
	if err == nil {
		t.Fatal("expected error after exhausting the fallback chain")
	}
	// 链上每个模型都被尝试过（glm → deepseek → longcat）
	if calls != len(freeModelChain) {
		t.Fatalf("upstream calls = %d, want %d (full fallback chain on 500)", calls, len(freeModelChain))
	}
	_ = upstreamModel
}

func TestHandleResponsesFreeReturnsTooManyRequestsWhenBothPoolsUnavailable(t *testing.T) {
	oldPool := pool
	oldConfig := getProxyConfig()
	oldTransport := httpClient.Transport
	requestLogsMu.Lock()
	oldLogs := requestLogs
	requestLogs = nil
	requestLogsMu.Unlock()
	t.Cleanup(func() {
		pool = oldPool
		setProxyConfig(oldConfig)
		httpClient.Transport = oldTransport
		requestLogsMu.Lock()
		requestLogs = oldLogs
		requestLogsMu.Unlock()
	})

	account := &Account{
		AccountID:   "quota-account",
		Email:       "quota@example.com",
		AccessToken: "quota-token",
		ExpiresAt:   time.Now().Add(time.Hour).UnixMilli(),
		Status:      "active",
	}
	pool = &AccountPool{Accounts: []*Account{account}}
	setProxyConfig(defaultProxyConfig())

	calls := 0
	httpClient.Transport = freeModelRoundTripper(func(req *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Body:       io.NopCloser(strings.NewReader(`{"error":"quota","message":"Try again in 1h"}`)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"free","input":"hello"}`))
	recorder := httptest.NewRecorder()
	handleResponses(recorder, req)
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("response status = %d, want %d: %s", recorder.Code, http.StatusTooManyRequests, recorder.Body.String())
	}
	if calls != len(freeModelChain) {
		t.Fatalf("upstream calls = %d, want one attempt per model (%d)", calls, len(freeModelChain))
	}

	requestLogsMu.Lock()
	defer requestLogsMu.Unlock()
	if len(requestLogs) != 1 {
		t.Fatalf("request log count = %d, want 1", len(requestLogs))
	}
	lastModel := freeModelChain[len(freeModelChain)-1]
	if requestLogs[0].Model != lastModel {
		t.Fatalf("request log model = %q, want %q", requestLogs[0].Model, lastModel)
	}
}

func TestPickAccountForModelLeastUsedSpreadsUsage(t *testing.T) {
	oldPool := pool
	t.Cleanup(func() { pool = oldPool })

	heavy := &Account{
		AccountID:   "heavy",
		Email:       "heavy@example.com",
		AccessToken: "token-heavy",
		ExpiresAt:   time.Now().Add(time.Hour).UnixMilli(),
		Status:      "active",
		ModelStats: map[string]*ModelStat{
			freeModelPrimary: {ModelID: freeModelPrimary, UsageCount: 100},
		},
	}
	light := &Account{
		AccountID:   "light",
		Email:       "light@example.com",
		AccessToken: "token-light",
		ExpiresAt:   time.Now().Add(time.Hour).UnixMilli(),
		Status:      "active",
		ModelStats: map[string]*ModelStat{
			freeModelPrimary: {ModelID: freeModelPrimary, UsageCount: 2},
		},
	}
	pool = &AccountPool{Accounts: []*Account{heavy, light}}

	for i := 0; i < 5; i++ {
		acc := pickAccountForModelLeastUsed(freeModelPrimary)
		if acc == nil {
			t.Fatal("pickAccountForModelLeastUsed returned nil with eligible accounts")
		}
		if acc.AccountID != "light" {
			t.Fatalf("pick %d: got account %q, want the least-used account \"light\"", i+1, acc.AccountID)
		}
	}
}

func TestIsFreeModelEntry(t *testing.T) {
	cases := []struct {
		m    Model
		want bool
	}{
		{Model{ID: "z-ai/glm-5.3-flash", Cost: "free"}, true},
		{Model{ID: "cline-pass/glm-5.2", Cost: "pass"}, false},
		// zen 来源：Cost 漏标但命中种子白名单 → 免费
		{Model{ID: "mimo-v2.6-flash-free", Cost: "pass", Source: "zen"}, true},
		// zen 付费模型 → 拒绝
		{Model{ID: "claude-opus-5", Cost: "pass", Source: "zen"}, false},
		// 非 zen 来源的 pass 模型 → 不免费
		{Model{ID: "some-model", Cost: "pass", Source: "remote"}, false},
	}
	for _, c := range cases {
		if got := isFreeModelEntry(c.m); got != c.want {
			t.Errorf("isFreeModelEntry(%+v) = %v, want %v", c.m, got, c.want)
		}
	}
}

func TestCallClineAPIFreePicksLeastUsedAccount(t *testing.T) {
	oldPool := pool
	oldConfig := getProxyConfig()
	oldTransport := httpClient.Transport
	t.Cleanup(func() {
		pool = oldPool
		setProxyConfig(oldConfig)
		httpClient.Transport = oldTransport
	})

	first := &Account{
		AccountID:   "acc-first",
		Email:       "first@example.com",
		AccessToken: "token-first",
		ExpiresAt:   time.Now().Add(time.Hour).UnixMilli(),
		Status:      "active",
		ModelStats: map[string]*ModelStat{
			freeModelPrimary: {ModelID: freeModelPrimary, UsageCount: 50},
		},
	}
	second := &Account{
		AccountID:   "acc-second",
		Email:       "second@example.com",
		AccessToken: "token-second",
		ExpiresAt:   time.Now().Add(time.Hour).UnixMilli(),
		Status:      "active",
	}
	pool = &AccountPool{Accounts: []*Account{first, second}}
	setProxyConfig(defaultProxyConfig())

	var chosen []string
	httpClient.Transport = freeModelRoundTripper(func(req *http.Request) (*http.Response, error) {
		token := strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer ")
		chosen = append(chosen, token)
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"id":"ok","choices":[]}`)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})

	for i := 0; i < 3; i++ {
		resp, _, err := callClineAPI(map[string]any{"model": "free"}, false)
		if err != nil || resp == nil {
			t.Fatalf("call %d failed: %v", i+1, err)
		}
		resp.Body.Close()
		if len(chosen) == 0 || chosen[len(chosen)-1] != "token-second" {
			t.Fatalf("call %d served by %q, want the unused account \"token-second\"", i+1, chosen[len(chosen)-1])
		}
	}
}

func TestSortModelsByAvailabilityPrefersAvailableThenLeastUsed(t *testing.T) {
	oldPool := pool
	t.Cleanup(func() { pool = oldPool })

	acc := &Account{
		AccountID:      "acc-one",
		Email:          "one@example.com",
		AccessToken:    "token",
		ExpiresAt:      time.Now().Add(time.Hour).UnixMilli(),
		Status:         "active",
		ModelCooldowns: map[string]time.Time{}, ModelStats: map[string]*ModelStat{
			freeModelPrimary:  {ModelID: freeModelPrimary, UsageCount: 90},
			freeModelFallback: {ModelID: freeModelFallback, UsageCount: 3},
		},
	}
	pool = &AccountPool{Accounts: []*Account{acc}}

	chain := []string{freeModelPrimary, freeModelFallback}
	got := sortModelsByAvailability(chain)
	if got[0] != freeModelFallback {
		t.Fatalf("first model = %q, want the less-used %q", got[0], freeModelFallback)
	}
	if len(got) != 2 {
		t.Fatalf("chain length = %d, want 2", len(got))
	}
}

func TestOnlyFreeConfigRoundTrip(t *testing.T) {
	oldConfig := getProxyConfig()
	t.Cleanup(func() { setProxyConfig(oldConfig) })

	setProxyConfig(defaultProxyConfig())
	if getProxyConfig().OnlyFree {
		t.Fatal("default OnlyFree = true, want false")
	}

	on := true
	cfg := getProxyConfig()
	cfg.OnlyFree = on
	setProxyConfig(cfg)
	if !getProxyConfig().OnlyFree {
		t.Fatal("OnlyFree not persisted after setProxyConfig")
	}
}
