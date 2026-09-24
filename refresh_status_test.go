package main

import (
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func isolateRefreshState(t *testing.T, transport freeModelRoundTripper) {
	t.Helper()
	oldPool, oldPoolPath, oldTransport := pool, poolPath, httpClient.Transport
	poolPath = filepath.Join(t.TempDir(), ".cline-accounts.json")
	httpClient.Transport = transport
	t.Cleanup(func() {
		pool, poolPath, httpClient.Transport = oldPool, oldPoolPath, oldTransport
	})
}

func refreshStatusResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
		Request:    req,
	}
}

func TestRefreshAccountTokenCoolsDownInsteadOfExpiringOnTransportError(t *testing.T) {
	isolateRefreshState(t, func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("dial tcp: lookup api.cline.bot: no such host")
	})
	acc := &Account{AccountID: "net", Email: "net@example.com", RefreshToken: "rt", Status: "active"}
	pool = &AccountPool{Accounts: []*Account{acc}}

	if err := refreshAccountToken(acc); err == nil {
		t.Fatal("refreshAccountToken should report the transport error")
	}
	if acc.Status != "cooldown" {
		t.Fatalf("status = %q, want a short cooldown after a transient network failure", acc.Status)
	}
}

func TestRefreshAccountTokenCoolsDownInsteadOfExpiringOnServerError(t *testing.T) {
	isolateRefreshState(t, func(req *http.Request) (*http.Response, error) {
		return refreshStatusResponse(req, http.StatusServiceUnavailable, `{"error":"unavailable"}`), nil
	})
	acc := &Account{AccountID: "5xx", Email: "5xx@example.com", RefreshToken: "rt", Status: "active"}
	pool = &AccountPool{Accounts: []*Account{acc}}

	if err := refreshAccountToken(acc); err == nil {
		t.Fatal("refreshAccountToken should report the server error")
	}
	if acc.Status != "cooldown" {
		t.Fatalf("status = %q, want a short cooldown after an upstream 503", acc.Status)
	}
}

func TestRefreshAccountTokenExpiresAccountWhenRefreshIsRejected(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden} {
		isolateRefreshState(t, func(req *http.Request) (*http.Response, error) {
			return refreshStatusResponse(req, status, `{"error":"invalid_grant"}`), nil
		})
		acc := &Account{AccountID: "rejected", Email: "rejected@example.com", RefreshToken: "rt", Status: "active"}
		pool = &AccountPool{Accounts: []*Account{acc}}

		if err := refreshAccountToken(acc); err == nil {
			t.Fatalf("status %d: refreshAccountToken should fail", status)
		}
		if acc.Status != "expired" {
			t.Fatalf("status %d: account status = %q, want expired", status, acc.Status)
		}
	}
}

func TestCallClineAPIWithAccountDoesNotExpireWhenRefreshAfter401HitsNetworkError(t *testing.T) {
	isolateRefreshState(t, func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, "/auth/refresh") {
			return nil, errors.New("dial tcp: lookup api.cline.bot: no such host")
		}
		return refreshStatusResponse(req, http.StatusUnauthorized, `{"error":"unauthorized"}`), nil
	})
	acc := &Account{
		AccountID:    "chat-401",
		Email:        "chat-401@example.com",
		AccessToken:  "stale",
		RefreshToken: "rt",
		ExpiresAt:    time.Now().Add(time.Hour).UnixMilli(),
		Status:       "active",
	}
	pool = &AccountPool{Accounts: []*Account{acc}}

	_, _, err := callClineAPIWithAccount(acc, map[string]any{
		"model":    freeModelFallback,
		"messages": []any{map[string]any{"role": "user", "content": "hello"}},
	}, false)
	if err == nil {
		t.Fatal("callClineAPIWithAccount should fail when refresh cannot reach Cline")
	}
	if acc.Status == "expired" {
		t.Fatalf("status = %q, want a recoverable state after a transient refresh failure", acc.Status)
	}
}
