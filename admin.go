package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// In-memory OAuth login state for async browser login
var (
	oauthSessions   = make(map[string]*oauthSessionState)
	oauthSessionsMu sync.Mutex
)

type oauthSessionState struct {
	DeviceCode string
	UserCode   string
	AuthURL    string
	CreatedAt  time.Time
	Done       bool
	Success    bool
	Email      string
	Error      string
}

type apiResponse struct {
	Success bool   `json:"success"`
	Data    any    `json:"data,omitempty"`
	Error   string `json:"error,omitempty"`
	Message string `json:"message,omitempty"`
}

func writeAPI(w http.ResponseWriter, status int, resp apiResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(resp)
}

// 管理后台登录会话（内存态，程序重启后需重新登录）。
var (
	adminSessions   = make(map[string]time.Time)
	adminSessionsMu sync.Mutex
)

const (
	adminSessionCookie = "cline_admin_session"
	adminSessionTTL    = 24 * time.Hour
)

func registerAdminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/admin/", adminStaticHandler)
	// 无需登录的接口
	mux.HandleFunc("/admin/api/login", corsHandler(handleAdminLogin))
	mux.HandleFunc("/admin/api/logout", corsHandler(handleAdminLogout))
	// 其余 API 全部需要后台鉴权（设置了密码后）
	auth := func(h http.HandlerFunc) http.HandlerFunc {
		return requireAdminAuth(corsHandler(h))
	}
	mux.HandleFunc("/admin/api/accounts", auth(handleAdminAccounts))
	mux.HandleFunc("/admin/api/accounts/add", auth(handleAdminAccountAdd))
	mux.HandleFunc("/admin/api/accounts/delete", auth(handleAdminAccountDelete))
	mux.HandleFunc("/admin/api/accounts/export", auth(handleExportAccounts))
	mux.HandleFunc("/admin/api/oauth/start", auth(handleOAuthStart))
	mux.HandleFunc("/admin/api/oauth/status", auth(handleOAuthStatus))
	mux.HandleFunc("/admin/api/sso/import", auth(handleSSOImport))
	mux.HandleFunc("/admin/api/stats", auth(handleAdminStats))
	mux.HandleFunc("/admin/api/batch-import", auth(handleBatchImport))
	mux.HandleFunc("/admin/api/accounts/refresh-all", auth(handleAdminRefreshAll))
	mux.HandleFunc("/admin/api/accounts/delete-all", auth(handleAdminDeleteAll))
	mux.HandleFunc("/admin/api/accounts/reset", auth(handleAdminAccountReset))
	mux.HandleFunc("/admin/api/accounts/test", auth(handleAdminAccountTest))
	mux.HandleFunc("/admin/api/keys", auth(handleAdminGetKeys))
	mux.HandleFunc("/admin/api/keys/generate", auth(handleAdminGenerateKey))
	mux.HandleFunc("/admin/api/keys/delete", auth(handleAdminDeleteKey))
	mux.HandleFunc("/admin/api/models", auth(handleAdminModels))
	mux.HandleFunc("/admin/api/models/sync", auth(handleAdminModelSync))
	mux.HandleFunc("/admin/api/opencode/config", auth(handleOpenCodeConfig))
	mux.HandleFunc("/admin/api/opencode/config/update", auth(handleOpenCodeConfigUpdate))
	mux.HandleFunc("/admin/api/opencode/models/sync", auth(handleOpenCodeModelSync))
	mux.HandleFunc("/admin/api/cline-proxy/config", auth(handleClineProxyConfig))
	mux.HandleFunc("/admin/api/cline-proxy/config/update", auth(handleClineProxyConfigUpdate))
	mux.HandleFunc("/admin/api/models/add", auth(handleAdminModelAdd))
	mux.HandleFunc("/admin/api/models/delete", auth(handleAdminModelDelete))
	mux.HandleFunc("/admin/api/models/context", auth(handleAdminModelContext))
	mux.HandleFunc("/admin/api/models/toggle", auth(handleAdminModelToggle))
	mux.HandleFunc("/admin/api/config", auth(handleAdminConfig))
	mux.HandleFunc("/admin/api/config/update", auth(handleAdminUpdateConfig))
	mux.HandleFunc("/admin/api/providers", auth(handleProvidersList))
	mux.HandleFunc("/admin/api/providers/save", auth(handleProviderSave))
	mux.HandleFunc("/admin/api/providers/delete", auth(handleProviderDelete))
	mux.HandleFunc("/admin/api/providers/test", auth(handleProviderTest))
	mux.HandleFunc("/admin/api/providers/presets", auth(handleProviderPresets))
	mux.HandleFunc("/admin/api/combos", auth(handleCombosList))
	mux.HandleFunc("/admin/api/combos/save", auth(handleComboSave))
	mux.HandleFunc("/admin/api/combos/delete", auth(handleComboDelete))
	mux.HandleFunc("/admin/api/combos/toggle", auth(handleComboToggle))
	mux.HandleFunc("/admin/api/combos/clone", auth(handleComboClone))
	mux.HandleFunc("/admin/api/password", auth(handleAdminPassword))
	mux.HandleFunc("/admin/api/request-logs", auth(handleAdminRequestLogs))
	mux.HandleFunc("/admin/api/open-external", auth(handleOpenExternal))
}

// requireAdminAuth 后台访问鉴权中间件：未设置密码直接放行，否则校验会话 cookie。
func requireAdminAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if loadPool().AdminPasswordHash == "" {
			next(w, r)
			return
		}
			c, err := r.Cookie(adminSessionCookie)
			if err != nil {
				writeAPI(w, http.StatusUnauthorized, apiResponse{Error: tAPI(r, "login_required")})
				return
			}
		adminSessionsMu.Lock()
		expiry, ok := adminSessions[c.Value]
		if ok {
			if time.Now().Before(expiry) {
				adminSessionsMu.Unlock()
				next(w, r)
				return
			}
			delete(adminSessions, c.Value)
		}
			adminSessionsMu.Unlock()
			writeAPI(w, http.StatusUnauthorized, apiResponse{Error: tAPI(r, "session_expired")})
	}
}

// hashAdminPassword 生成加盐密码哈希：hex(sha256(salt+password))。
func hashAdminPassword(saltHex, password string) string {
	sum := sha256.Sum256([]byte(saltHex + password))
	return hex.EncodeToString(sum[:])
}

// setAdminPassword 设置/修改/清除后台密码（空 = 清除），并清空所有会话强制重新登录。
func setAdminPassword(password string) {
	p := loadPool()
	poolMu.Lock()
	if password == "" {
		p.AdminPasswordHash = ""
		p.AdminPasswordSalt = ""
	} else {
		salt := make([]byte, 16)
		if _, err := rand.Read(salt); err != nil {
			salt = []byte(time.Now().Format("20060102150405"))
		}
		p.AdminPasswordSalt = hex.EncodeToString(salt)
		p.AdminPasswordHash = hashAdminPassword(p.AdminPasswordSalt, password)
	}
	poolMu.Unlock()
	savePool()
	adminSessionsMu.Lock()
	adminSessions = make(map[string]time.Time)
	adminSessionsMu.Unlock()
}

// verifyAdminPassword 校验后台密码（未设置密码时返回 false）。
func verifyAdminPassword(password string) bool {
	p := loadPool()
	poolMu.Lock()
	defer poolMu.Unlock()
	if p.AdminPasswordHash == "" {
		return false
	}
	return hashAdminPassword(p.AdminPasswordSalt, password) == p.AdminPasswordHash
}

// randomHex 生成 n 字节随机数的 hex 字符串。
func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}

// POST /admin/api/login  body: {password}
func handleAdminLogin(w http.ResponseWriter, r *http.Request) {
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
		var req struct {
			Password string `json:"password"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			writeAPI(w, http.StatusBadRequest, apiResponse{Error: tAPI(r, "invalid_json")})
			return
		}
		if loadPool().AdminPasswordHash == "" {
			writeAPI(w, http.StatusBadRequest, apiResponse{Error: tAPI(r, "password_not_enabled")})
			return
		}
		if !verifyAdminPassword(req.Password) {
			time.Sleep(500 * time.Millisecond) // 防爆破
			writeAPI(w, http.StatusUnauthorized, apiResponse{Error: tAPI(r, "wrong_password")})
			return
		}
	token := randomHex(32)
	adminSessionsMu.Lock()
	adminSessions[token] = time.Now().Add(adminSessionTTL)
	adminSessionsMu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name:     adminSessionCookie,
		Value:    token,
		Path:     "/admin",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(adminSessionTTL.Seconds()),
	})
		writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: tAPI(r, "login_ok")})
	}

	// POST /admin/api/logout
	func handleAdminLogout(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(adminSessionCookie); err == nil {
			adminSessionsMu.Lock()
			delete(adminSessions, c.Value)
			adminSessionsMu.Unlock()
		}
		http.SetCookie(w, &http.Cookie{Name: adminSessionCookie, Value: "", Path: "/admin", MaxAge: -1})
		writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: tAPI(r, "logout_ok")})
	}

// POST /admin/api/password  body: {password}（空 = 清除密码，恢复无密码访问）
func handleAdminPassword(w http.ResponseWriter, r *http.Request) {
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
		var req struct {
			Password string `json:"password"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			writeAPI(w, http.StatusBadRequest, apiResponse{Error: tAPI(r, "invalid_json")})
			return
		}
		setAdminPassword(req.Password)
		if req.Password == "" {
			writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: tAPI(r, "password_cleared")})
		} else {
			writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: tAPI(r, "password_updated")})
		}
}

func adminStaticHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/admin/" || r.URL.Path == "/admin" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(adminHTML))
		return
	}
	http.NotFound(w, r)
}

// GET /admin/api/accounts
func handleAdminAccounts(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: tAPI(r, "method_not_allowed")})
		return
	}
	accounts := listAccounts()
	writeAPI(w, http.StatusOK, apiResponse{
		Success: true,
		Data: map[string]any{
			"accounts":  accounts,
			"total":     len(accounts),
			"poolIndex": loadPool().CurrentIdx,
		},
	})
}

// POST /admin/api/accounts/add  body: { refreshToken, email }
func handleAdminAccountAdd(w http.ResponseWriter, r *http.Request) {
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

	var req struct {
		RefreshToken string `json:"refreshToken"`
		Email        string `json:"email"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: tAPI(r, "invalid_json")})
		return
	}

		if req.RefreshToken == "" {
			writeAPI(w, http.StatusBadRequest, apiResponse{Error: tAPI(r, "refresh_token_required")})
			return
		}

	// 去重：该 refreshToken 已存在时直接返回，不重复添加
	if existing := findAccountByRefreshToken(req.RefreshToken); existing != nil {
		writeAPI(w, http.StatusOK, apiResponse{
			Success: true,
			Message: tAPI(r, "account_exists", existing.Email),
			Data: map[string]any{
				"accountId": existing.AccountID,
				"email":     existing.Email,
				"status":    existing.Status,
				"duplicate": true,
			},
		})
		return
	}

		// Validate by refreshing
		resp, err := refreshClineToken(req.RefreshToken)
		if err != nil {
			writeAPI(w, http.StatusBadRequest, apiResponse{Error: tAPI(r, "invalid_refresh_token", err.Error())})
			return
		}

	if req.Email == "" {
		req.Email = fmt.Sprintf("user_%d", len(loadPool().Accounts)+1)
	}

	acc := &Account{
		AccountID:    fmt.Sprintf("acc_%d", time.Now().UnixMilli()),
		Email:        req.Email,
		RefreshToken: req.RefreshToken,
		AccessToken:  "workos:" + resp.Data.AccessToken,
		ExpiresAt:    parseExpiry(resp.Data.ExpiresAt) - 60000,
		Status:       "active",
		CreatedAt:    time.Now(),
	}
	if resp.Data.RefreshToken != "" {
		acc.RefreshToken = resp.Data.RefreshToken
	}

	addAccount(acc)
	log.Printf("Account added via API: %s", req.Email)

		writeAPI(w, http.StatusOK, apiResponse{
			Success: true,
			Message: tAPI(r, "account_added", req.Email),
		Data: map[string]any{
			"accountId": acc.AccountID,
			"email":     acc.Email,
			"status":    acc.Status,
		},
	})
}

// POST /admin/api/accounts/delete  body: { accountId }
func handleAdminAccountDelete(w http.ResponseWriter, r *http.Request) {
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

	var req struct {
		AccountID string `json:"accountId"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: tAPI(r, "invalid_json")})
		return
	}

		if req.AccountID == "" {
			writeAPI(w, http.StatusBadRequest, apiResponse{Error: tAPI(r, "account_id_required")})
			return
		}

		if removeAccount(req.AccountID) {
			writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: tAPI(r, "account_deleted")})
		} else {
			writeAPI(w, http.StatusNotFound, apiResponse{Error: tAPI(r, "account_not_found")})
		}
}

// POST /admin/api/oauth/start  -- Start OAuth device login, returns URL
func handleOAuthStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: tAPI(r, "method_not_allowed")})
		return
	}

	device, err := workosDeviceAuth()
	if err != nil {
		writeAPI(w, http.StatusInternalServerError, apiResponse{Error: err.Error()})
		return
	}

	authURL := device.VerificationURIComplete
	if authURL == "" {
		authURL = device.VerificationURI
	}

	sessionID := fmt.Sprintf("oauth_%d", time.Now().UnixMilli())
	state := &oauthSessionState{
		DeviceCode: device.DeviceCode,
		UserCode:   device.UserCode,
		AuthURL:    authURL,
		CreatedAt:  time.Now(),
	}

	oauthSessionsMu.Lock()
	oauthSessions[sessionID] = state
	oauthSessionsMu.Unlock()

	// Start polling in background
	go func() {
		interval := device.Interval
		if interval < 5 {
			interval = 5
		}
		expiresIn := device.ExpiresIn
		if expiresIn <= 0 {
			expiresIn = 300
		}

		workosTok, err := pollWorkosToken(device.DeviceCode, interval, expiresIn)
		if err != nil {
			oauthSessionsMu.Lock()
			state.Error = err.Error()
			state.Done = true
			state.Success = false
			oauthSessionsMu.Unlock()
			return
		}

		cline, err := registerWithCline(workosTok.AccessToken, workosTok.RefreshToken)
		if err != nil {
			oauthSessionsMu.Lock()
			state.Error = err.Error()
			state.Done = true
			state.Success = false
			oauthSessionsMu.Unlock()
			return
		}

		email := "unknown"
		if cline.Data.UserInfo != nil && cline.Data.UserInfo.Email != "" {
			email = cline.Data.UserInfo.Email
		}

		acc := &Account{
			AccountID:    fmt.Sprintf("acc_%d", time.Now().UnixMilli()),
			Email:        email,
			RefreshToken: cline.Data.RefreshToken,
			AccessToken:  "workos:" + cline.Data.AccessToken,
			ExpiresAt:    parseExpiry(cline.Data.ExpiresAt) - 60000,
			Status:       "active",
			CreatedAt:    time.Now(),
		}
		addAccount(acc)

		oauthSessionsMu.Lock()
		state.Done = true
		state.Success = true
		state.Email = email
		oauthSessionsMu.Unlock()
		log.Printf("OAuth account added: %s", email)
	}()

	writeAPI(w, http.StatusOK, apiResponse{
		Success: true,
		Data: map[string]any{
			"sessionId":       sessionID,
			"verificationUri": authURL,
			"userCode":        device.UserCode,
		},
	})
}

// GET /admin/api/oauth/status?sessionId=xxx
func handleOAuthStatus(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("sessionId")
	if sessionID == "" {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: tAPI(r, "session_id_required")})
		return
	}

	oauthSessionsMu.Lock()
	state, ok := oauthSessions[sessionID]
	oauthSessionsMu.Unlock()

	if !ok {
		writeAPI(w, http.StatusNotFound, apiResponse{Error: tAPI(r, "session_not_found")})
		return
	}

	resp := map[string]any{
		"done":    state.Done,
		"success": state.Success,
	}
	if state.Done {
		resp["email"] = state.Email
		if !state.Success {
			resp["error"] = state.Error
		}
	}

	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: resp})
}

// POST /admin/api/sso/import  body: { ssoCookies: string, email?: string }
func handleSSOImport(w http.ResponseWriter, r *http.Request) {
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

	var req struct {
		SSOCookies string `json:"ssoCookies"`
		Email      string `json:"email"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: tAPI(r, "invalid_json")})
		return
	}

	if req.SSOCookies == "" {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: tAPI(r, "sso_cookies_required")})
		return
	}

	// SSO cookies import - try to use WorkOS device auth (requires browser)
	// For direct SSO cookie conversion, we'd need the WorkOS session cookie
	// to exchange for tokens. This is a placeholder that accepts WorkOS session
	// cookies. In practice, users should use OAuth or direct refreshToken.
	//
	// SSO cookie format expected: workos_session=xxx or similar
	lines := strings.Split(req.SSOCookies, "\n")
	imported := 0
	duplicates := 0
	errors := []string{}
	seen := make(map[string]bool)

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Try to use the cookie as a refresh token directly (common format)
		if strings.HasPrefix(line, "workos:") || len(line) > 20 {
			token := strings.TrimSpace(strings.TrimPrefix(line, "workos:"))
			if token == "" {
				continue
			}
			// 去重：与账号池中已有账号或本批次内重复的 token，跳过而不是重复添加
			if isDuplicateImportToken(token, seen) {
				duplicates++
				continue
			}
			resp, err := refreshClineToken(token)
			if err != nil {
				errors = append(errors, fmt.Sprintf("token %s...: %v", truncate(token, 16), err))
				continue
			}
			email := req.Email
			if email == "" {
				email = fmt.Sprintf("sso_user_%d", time.Now().UnixMilli())
			}

			acc := &Account{
				AccountID:    fmt.Sprintf("acc_%d", time.Now().UnixMilli()),
				Email:        email,
				RefreshToken: token,
				AccessToken:  "workos:" + resp.Data.AccessToken,
				ExpiresAt:    parseExpiry(resp.Data.ExpiresAt) - 60000,
				Status:       "active",
				CreatedAt:    time.Now(),
			}
			addAccount(acc)
			imported++
		}
	}

	result := map[string]any{
		"imported":   imported,
		"failed":     len(errors),
		"duplicates": duplicates,
	}
	if len(errors) > 0 {
		result["errors"] = errors
	}

		writeAPI(w, http.StatusOK, apiResponse{
			Success: true,
			Message: tAPI(r, "imported_accounts", imported, len(errors), duplicates),
			Data:    result,
		})
}

// POST /admin/api/batch-import  body: { tokens: [{ refreshToken, email }] }
func handleBatchImport(w http.ResponseWriter, r *http.Request) {
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

	var req struct {
		Tokens []struct {
			RefreshToken string `json:"refreshToken"`
			Email        string `json:"email"`
		} `json:"tokens"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: tAPI(r, "invalid_json")})
		return
	}

		if len(req.Tokens) == 0 {
			writeAPI(w, http.StatusBadRequest, apiResponse{Error: tAPI(r, "tokens_empty")})
			return
		}

	imported := 0
	duplicates := 0
	errors := []string{}
	seen := make(map[string]bool)

	for _, t := range req.Tokens {
		token := strings.TrimSpace(t.RefreshToken)
		if token == "" {
			continue
		}
		// 去重：与账号池中已有账号或本批次内重复的 token，跳过而不是重复添加
		if isDuplicateImportToken(token, seen) {
			duplicates++
			continue
		}
		resp, err := refreshClineToken(token)
		if err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", t.Email, err))
			continue
		}
		email := t.Email
		if email == "" {
			email = fmt.Sprintf("batch_%d", time.Now().UnixMilli())
		}
		acc := &Account{
			AccountID:    fmt.Sprintf("acc_%d", time.Now().UnixMilli()),
			Email:        email,
			RefreshToken: token,
			AccessToken:  "workos:" + resp.Data.AccessToken,
			ExpiresAt:    parseExpiry(resp.Data.ExpiresAt) - 60000,
			Status:       "active",
			CreatedAt:    time.Now(),
		}
		addAccount(acc)
		imported++
	}

		writeAPI(w, http.StatusOK, apiResponse{
			Success: true,
			Message: tAPI(r, "imported_accounts", imported, len(errors), duplicates),
		Data: map[string]any{
			"imported":   imported,
			"failed":     len(errors),
			"duplicates": duplicates,
			"errors":     errors,
		},
	})
}

// GET /admin/api/accounts/export — 导出账号为批量导入兼容格式
// POST body: { ids: [accountId...] } — 仅导出勾选的账号（管理页勾选导出）
func handleExportAccounts(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "POST" {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: tAPI(r, "method_not_allowed")})
		return
	}

	p := loadPool()
	want := map[string]bool{}
	if r.Method == "POST" {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
			return
		}
		defer r.Body.Close()
		var req struct {
			IDs []string `json:"ids"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			writeAPI(w, http.StatusBadRequest, apiResponse{Error: tAPI(r, "invalid_json")})
			return
		}
		for _, id := range req.IDs {
			if id != "" {
				want[id] = true
			}
		}
	}

	type exportToken struct {
		RefreshToken string `json:"refreshToken"`
		Email        string `json:"email"`
	}
	tokens := make([]exportToken, 0, len(p.Accounts))
	for _, acc := range p.Accounts {
		if acc.RefreshToken != "" && (len(want) == 0 || want[acc.AccountID]) {
			tokens = append(tokens, exportToken{
				RefreshToken: acc.RefreshToken,
				Email:        acc.Email,
			})
		}
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="cline-accounts-export.json"`)
	json.NewEncoder(w).Encode(map[string]any{
		"tokens":     tokens,
		"exportedAt": time.Now().Format(time.RFC3339),
	})
}

// GET /admin/api/open-external?url=... — 用系统默认浏览器打开外部链接
func handleOpenExternal(w http.ResponseWriter, r *http.Request) {
	url := r.URL.Query().Get("url")
		if url == "" {
			writeAPI(w, http.StatusBadRequest, apiResponse{Error: tAPI(r, "url_required")})
			return
		}
		// 仅允许 http/https，防止任意命令执行
		if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			writeAPI(w, http.StatusBadRequest, apiResponse{Error: tAPI(r, "url_http_only")})
			return
		}
	if err := openBrowser(url); err != nil {
		writeAPI(w, http.StatusInternalServerError, apiResponse{Error: err.Error()})
		return
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true})
}

// POST /admin/api/accounts/refresh-all
func handleAdminRefreshAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: tAPI(r, "method_not_allowed")})
		return
	}
	p := loadPool()
	poolMu.Lock()
	for _, a := range p.Accounts {
		if err := refreshAccountToken(a); err != nil {
			log.Printf("Refresh failed for %s: %v", a.Email, err)
		}
	}
	poolMu.Unlock()
		writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: tAPI(r, "tokens_refreshed")})
}

// POST /admin/api/accounts/delete-all
func handleAdminDeleteAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: tAPI(r, "method_not_allowed")})
		return
	}
	poolMu.Lock()
	pool = &AccountPool{Accounts: []*Account{}, Keys: []string{}}
	poolMu.Unlock()
	savePool()
		writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: tAPI(r, "accounts_deleted")})
}

// POST /admin/api/accounts/reset  body: { accountId }
func handleAdminAccountReset(w http.ResponseWriter, r *http.Request) {
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

	var req struct {
		AccountID string `json:"accountId"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: tAPI(r, "invalid_json")})
		return
	}

		acc := getAccountByID(req.AccountID)
		if acc == nil {
			writeAPI(w, http.StatusNotFound, apiResponse{Error: tAPI(r, "account_not_found")})
			return
		}

		// Reset status to active and refresh token, but preserve usage/token statistics.
		acc.Status = "active"
		if err := refreshAccountToken(acc); err != nil {
			writeAPI(w, http.StatusInternalServerError, apiResponse{Error: tAPI(r, "reset_failed", err.Error())})
			return
		}

		writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: tAPI(r, "account_reset")})
}

// POST /admin/api/accounts/test  body: { accountId?: "" }
func handleAdminAccountTest(w http.ResponseWriter, r *http.Request) {
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

	var req struct {
		AccountID string `json:"accountId"`
	}
	_ = json.Unmarshal(body, &req)

	p := loadPool()
	var targets []*Account
	if req.AccountID != "" {
		acc := getAccountByID(req.AccountID)
			if acc == nil {
				writeAPI(w, http.StatusNotFound, apiResponse{Error: tAPI(r, "account_not_found")})
				return
			}
		targets = []*Account{acc}
	} else {
		poolMu.Lock()
		targets = make([]*Account, len(p.Accounts))
		copy(targets, p.Accounts)
		poolMu.Unlock()
	}

	results := make([]accountTestResult, 0, len(targets))
	for _, acc := range targets {
		results = append(results, testAccount(acc))
	}

	writeAPI(w, http.StatusOK, apiResponse{
		Success: true,
		Data:    map[string]any{"results": results},
	})
}

// Global proxy config (mutable via API, persisted to .cline-config.json)
var (
	proxyConfig   = loadProxyConfigFromDisk()
	proxyConfigMu sync.Mutex
)

type proxyConfigData struct {
	Strategy string            `json:"strategy"`
	Headers  map[string]string `json:"headers"`
	// ModelChain 冷却/降级时的模型回退顺序（管理员可配）。
	// 空 = 使用内置 free 链（glm-5.3-flash → deepseek-v4-flash → longcat-2.0）。
	ModelChain []string `json:"modelChain,omitempty"`
	OnlyFree   bool     `json:"onlyFree"` // 只显示免费模型：开启后 /models、/v1/models 仅返回免费模型
	// Combos 具名模型组合（有序回退）。复用本配置文件与原子写，不引入新存储。
	Combos []ModelCombo `json:"combos,omitempty"`
}

func defaultProxyConfig() *proxyConfigData {
	return &proxyConfigData{
		Strategy: "round_robin",
		Headers: map[string]string{
			"User-Agent":         "Cline/3.0.47",
			"HTTP-Referer":       "https://cline.bot",
			"X-Title":            "Cline",
			"X-IS-MULTIROOT":     "false",
			"X-CLIENT-TYPE":      "cline-cli",
			"X-CLIENT-VERSION":   "3.0.47",
			"X-PLATFORM":         "terminal",
			"X-PLATFORM-VERSION": "3.0.47",
			"X-CORE-VERSION":     "0.0.66",
		},
	}
}

const proxyConfigPath = ".cline-config.json"

// loadProxyConfigFromDisk 启动时加载持久化的代理配置（轮询策略/请求头），
// 文件不存在或损坏时回退默认值（损坏文件改名备份，不静默覆盖）。
func loadProxyConfigFromDisk() *proxyConfigData {
	cfg := defaultProxyConfig()
	if data, err := os.ReadFile(resolveDataPath(proxyConfigPath)); err == nil {
		if err := json.Unmarshal(data, cfg); err != nil {
			backupCorruptFile(resolveDataPath(proxyConfigPath), err)
		}
	}
	switch cfg.Strategy {
	case "round_robin", "fill", "random", "quality":
	default:
		cfg.Strategy = "round_robin"
	}
	return cfg
}

// saveProxyConfigLocked 落盘当前配置（调用方需持有 proxyConfigMu）。
func saveProxyConfigLocked() {
	data, err := json.MarshalIndent(proxyConfig, "", "  ")
	if err != nil {
		return
	}
	if err := writeFileAtomic(resolveDataPath(proxyConfigPath), data, 0600); err != nil {
		log.Printf("proxy config save failed: %v", err)
	}
}

func getProxyConfig() *proxyConfigData {
	proxyConfigMu.Lock()
	defer proxyConfigMu.Unlock()
	return proxyConfig
}

func setProxyConfig(c *proxyConfigData) {
	proxyConfigMu.Lock()
	defer proxyConfigMu.Unlock()
	proxyConfig = c
	saveProxyConfigLocked()
}

// GET /admin/api/keys
func handleAdminGetKeys(w http.ResponseWriter, r *http.Request) {
	p := loadPool()
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"keys": p.Keys}})
}

// POST /admin/api/keys/generate
func handleAdminGenerateKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: tAPI(r, "method_not_allowed")})
		return
	}
	key := fmt.Sprintf("cline_%x_%x", time.Now().UnixMilli(), time.Now().UnixNano()%1000000)
	p := loadPool()
	poolMu.Lock()
	p.Keys = append(p.Keys, key)
	poolMu.Unlock()
	savePool()
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"key": key}})
}

// POST /admin/api/keys/delete  body: { key }
func handleAdminDeleteKey(w http.ResponseWriter, r *http.Request) {
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
	var req struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: tAPI(r, "invalid_json")})
		return
	}
	p := loadPool()
	poolMu.Lock()
	for i, k := range p.Keys {
		if k == req.Key {
			p.Keys = append(p.Keys[:i], p.Keys[i+1:]...)
			break
		}
	}
	poolMu.Unlock()
	savePool()
		writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: tAPI(r, "key_deleted")})
}

// GET /admin/api/config
func handleAdminConfig(w http.ResponseWriter, r *http.Request) {
	cfg := getProxyConfig()
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{
		"address":      fmt.Sprintf("%s:%d", effectiveAdminHost(listenHost), listenPort),
		"host":         listenHost,
		"strategy":     cfg.Strategy,
		"modelChain":   cfg.ModelChain,
		"version":      appVersion,
		"zenHeaders":   getZenConfig().ZenHeaders,
		"poolPath":     poolPath,
		"defaultModel": getDefaultModel(),
		"headers":      cfg.Headers,
		"onlyFree":     cfg.OnlyFree,
		"localIPs":     detectLocalIPs(),
		"hasPassword":  loadPool().AdminPasswordHash != "",
	}})
}

// POST /admin/api/config  body: { strategy?, headers?, defaultModel?, host? }
func handleAdminUpdateConfig(w http.ResponseWriter, r *http.Request) {
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

	var chainUnverified []string // 回退链中不在可用列表的条目（保存后回显提示）

	var req struct {
		Strategy     string            `json:"strategy"`
		Headers      map[string]string `json:"headers"`
		DefaultModel string            `json:"defaultModel"`
		Host         string            `json:"host"`
		ModelChain   *[]string         `json:"modelChain"`
		OnlyFree     *bool             `json:"onlyFree"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: tAPI(r, "invalid_json")})
		return
	}

	cfg := getProxyConfig()
	changed := false
	restarting := false

	if req.Strategy != "" {
		switch req.Strategy {
		case "round_robin", "fill", "random", "quality":
			cfg.Strategy = req.Strategy
			changed = true
		default:
				writeAPI(w, http.StatusBadRequest, apiResponse{Error: tAPI(r, "invalid_strategy")})
			return
		}
	}

	if req.Headers != nil {
		for k, v := range req.Headers {
			cfg.Headers[k] = v
		}
		changed = true
	}

	if req.ModelChain != nil {
		// 回退链：去空去重，允许不在可用列表里的模型 ID —— 官方列表之外
		// 仍有实测可用的模型（同步列表只是参考，不是白名单）；失效条目由
		// 请求时的模型级冷却与故障转移兜底。不在列表的条目回显给前端提示。
		known := map[string]bool{"free": true}
		for _, m := range getAllModels() {
			known[m.ID] = true
		}
		var chain []string
		var unverified []string
		seen := map[string]bool{}
		for _, raw := range *req.ModelChain {
			id := strings.TrimSpace(raw)
			if id == "" || seen[id] {
				continue
			}
			if !known[id] {
				unverified = append(unverified, id)
			}
			seen[id] = true
			chain = append(chain, id)
		}
		cfg.ModelChain = chain
		if len(unverified) > 0 {
			log.Printf("  model chain: kept out-of-list model(s) %v (not in synced list, may still work)", unverified)
		}
		changed = true
		chainUnverified = unverified
	}

	if req.OnlyFree != nil {
		cfg.OnlyFree = *req.OnlyFree
		changed = true
	}

	if req.DefaultModel != "" {
		// 校验默认模型存在于可用模型列表中
		found := false
		for _, m := range getAllModels() {
			if m.ID == req.DefaultModel {
				found = true
				break
			}
		}
		if !found {
				writeAPI(w, http.StatusBadRequest, apiResponse{Error: tAPI(r, "invalid_default_model")})
			return
		}
		p := loadPool()
		poolMu.Lock()
		p.DefaultModel = req.DefaultModel
		poolMu.Unlock()
		savePool()
	}

	if req.Host != "" {
		// 校验监听地址：回环 / 0.0.0.0 / 本机检测到的 IP
		valid := req.Host == "127.0.0.1" || req.Host == "0.0.0.0" || req.Host == "localhost" || req.Host == "::1"
		if !valid {
			for _, ip := range detectLocalIPs() {
				if ip == req.Host {
					valid = true
					break
				}
			}
		}
		if !valid {
				writeAPI(w, http.StatusBadRequest, apiResponse{Error: tAPI(r, "invalid_host")})
			return
		}
		p := loadPool()
		poolMu.Lock()
		p.ListenHost = req.Host
		poolMu.Unlock()
		savePool()
		restarting = true
	}

	if changed {
		setProxyConfig(cfg)
	}

	if restarting {
		// 异步重启监听（Shutdown 会等待当前请求完成，不能在 handler 内同步调用）
		go func() {
			if err := restartListener(req.Host, listenPort); err != nil && err != http.ErrServerClosed {
				log.Printf("Listener restart failed: %v", err)
			}
		}()
	}

	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{
		"strategy":         cfg.Strategy,
		"headers":          cfg.Headers,
		"onlyFree":         cfg.OnlyFree,
		"defaultModel":     getDefaultModel(),
		"host":             listenHost,
		"address":          fmt.Sprintf("%s:%d", effectiveAdminHost(listenHost), listenPort),
		"restarting":       restarting,
		"chainUnverified":  chainUnverified,
	}})
}

// GET /admin/api/models
func handleAdminModels(w http.ResponseWriter, r *http.Request) {
	models := getAllModels()
	// zen 模型计费归一化：与路由判定保持一致（种子白名单兜底），避免 UI 分组与分流不一致
	for i := range models {
		if isZenSource(models[i]) && isZenFreeModel(models[i]) && models[i].Cost != "free" {
			models[i].Cost = "free"
		}
	}
	sync := getModelSyncResult()
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{
		"models":   models,
		"lastSync": sync,
	}})
}

// POST /admin/api/models/add  body: { id, provider?, cost? }
func handleAdminModelAdd(w http.ResponseWriter, r *http.Request) {
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

	var req struct {
		ID       string `json:"id"`
		Provider string `json:"provider"`
		Cost     string `json:"cost"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: tAPI(r, "invalid_json")})
		return
	}

		if req.ID == "" {
			writeAPI(w, http.StatusBadRequest, apiResponse{Error: tAPI(r, "model_id_required")})
			return
		}

		// 校验不与已有模型重复
		for _, m := range getAllModels() {
			if m.ID == req.ID {
				writeAPI(w, http.StatusConflict, apiResponse{Error: tAPI(r, "model_exists")})
				return
			}
		}

	// cost 默认为 pass
	cost := req.Cost
	if cost == "" {
		cost = "pass"
	}
	// provider 可选，留空则从 ID 前缀推断
	provider := req.Provider
	if provider == "" {
		if idx := strings.Index(req.ID, "/"); idx > 0 {
			provider = req.ID[:idx]
		} else {
			provider = "custom"
		}
	}

	p := loadPool()
	poolMu.Lock()
	p.Models = append(p.Models, Model{
		ID:       req.ID,
		Provider: provider,
		Cost:     cost,
		Status:   "active",
		Custom:   true,
	})
	poolMu.Unlock()
	savePool()

		writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: tAPI(r, "model_added")})
}

// POST /admin/api/models/delete  body: { id }
func handleAdminModelDelete(w http.ResponseWriter, r *http.Request) {
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

	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: tAPI(r, "invalid_json")})
		return
	}

		if req.ID == "" {
			writeAPI(w, http.StatusBadRequest, apiResponse{Error: tAPI(r, "model_id_required")})
			return
		}

		p := loadPool()
		poolMu.Lock()
		found := false
		for i, m := range p.Models {
			if m.ID == req.ID {
				// 自定义模型可删；同步打上 Delisted 标记的下架模型支持手动移除
				if !m.Custom && !m.Delisted {
					poolMu.Unlock()
					writeAPI(w, http.StatusBadRequest, apiResponse{Error: tAPI(r, "cannot_delete_builtin")})
					return
				}
				p.Models = append(p.Models[:i], p.Models[i+1:]...)
				found = true
				break
			}
		}
		if !found {
			poolMu.Unlock()
			writeAPI(w, http.StatusNotFound, apiResponse{Error: tAPI(r, "model_not_found")})
			return
		}
	// 若删除的是当前默认模型，则清空回退到内置默认
	if p.DefaultModel == req.ID {
		p.DefaultModel = ""
	}
	poolMu.Unlock()
	savePool()

		writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: tAPI(r, "model_deleted")})
}

// POST /admin/api/models/context  body: { id, context, output }
// 手动设置模型的上下文窗口 / 最大输出 token（压缩阈值与 maybeCompact 按此计算）。
// 0 = 清除为未知（zen 同步会回填默认值）。设置后 zen 模型同步保留该值不再覆盖。
func handleAdminModelContext(w http.ResponseWriter, r *http.Request) {
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

	var req struct {
		ID      string `json:"id"`
		Context int    `json:"context"`
		Output  int    `json:"output"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: tAPI(r, "invalid_json")})
		return
	}
	if req.ID == "" {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: tAPI(r, "model_id_required")})
		return
	}
	if req.Context < 0 || req.Output < 0 {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: tAPI(r, "invalid_json")})
		return
	}

	p := loadPool()
	poolMu.Lock()
	found := false
	for i, m := range p.Models {
		if m.ID == req.ID {
			p.Models[i].Context = req.Context
			p.Models[i].Output = req.Output
			// 双清零=回到未知（同步回填默认）；否则视为用户锁定，同步不再覆盖
			p.Models[i].MetaLocked = !(req.Context == 0 && req.Output == 0)
			found = true
			break
		}
	}
	poolMu.Unlock()
	if !found {
		writeAPI(w, http.StatusNotFound, apiResponse{Error: tAPI(r, "model_not_found")})
		return
	}
	savePool()
	log.Printf("  model context updated: %s ctx=%d out=%d", req.ID, req.Context, req.Output)
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: tAPI(r, "model_context_saved")})
}

// POST /admin/api/models/toggle  body: { ids: [...], disabled }
// 启用/停用模型（单个或批量）：停用后不进客户端模型列表、直接请求被拒绝、
// 默认模型与回退链自动跳过；同步保留该标记。停用当前默认模型时清空默认。
func handleAdminModelToggle(w http.ResponseWriter, r *http.Request) {
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

	var req struct {
		IDs      []string `json:"ids"`
		Disabled bool     `json:"disabled"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: tAPI(r, "invalid_json")})
		return
	}
	ids := make([]string, 0, len(req.IDs))
	for _, id := range req.IDs {
		if id = strings.TrimSpace(id); id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: tAPI(r, "model_id_required")})
		return
	}

	target := make(map[string]bool, len(ids))
	for _, id := range ids {
		target[id] = true
	}
	p := loadPool()
	poolMu.Lock()
	changed := 0
	for i, m := range p.Models {
		if !target[m.ID] {
			continue
		}
		p.Models[i].Disabled = req.Disabled
		changed++
		// 停用当前默认模型时清空，回退到自动挑选逻辑（与删除行为一致）
		if req.Disabled && p.DefaultModel == m.ID {
			p.DefaultModel = ""
		}
	}
	poolMu.Unlock()
	if changed == 0 {
		writeAPI(w, http.StatusNotFound, apiResponse{Error: tAPI(r, "model_not_found")})
		return
	}
	savePool()
	log.Printf("  model toggle: %d models -> disabled=%v", changed, req.Disabled)
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: tAPI(r, "model_status_saved")})
}

// GET /admin/api/stats
func handleAdminStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: tAPI(r, "method_not_allowed")})
		return
	}

	p := loadPool()
	active, cooldown, expired := 0, 0, 0
	var usageCount, promptTokens, completionTokens, totalTokens, cachedTokens int64
	for _, a := range p.Accounts {
		usageCount += a.UsageCount
		promptTokens += a.PromptTokens
		completionTokens += a.CompletionTokens
		totalTokens += a.TotalTokens
		cachedTokens += a.CachedTokens
		switch a.Status {
		case "active":
			active++
		case "cooldown":
			cooldown++
		case "expired":
			expired++
		}
	}

	writeAPI(w, http.StatusOK, apiResponse{
		Success: true,
		Data: map[string]any{
			"total":            len(p.Accounts),
			"active":           active,
			"cooldown":         cooldown,
			"expired":          expired,
			"usageCount":       usageCount,
			"promptTokens":     promptTokens,
			"completionTokens": completionTokens,
			"totalTokens":      totalTokens,
			"cachedTokens":     cachedTokens,
			"strategy":         getProxyConfig().Strategy,
			"modelChain":       getProxyConfig().ModelChain,
			"version":          appVersion,
			// opencode zen 免费模型今日用量（从请求日志聚合）
			"opencodeToday": opencodeUsageToday(),
			// 按模型聚合的用量（累计 + 今日，仪表盘「模型用量统计」）
			"modelUsage": modelUsageStats(),
		},
	})
}

// GET /admin/api/request-logs?limit=50&cursor=...
func handleAdminRequestLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: tAPI(r, "method_not_allowed")})
		return
	}

	limit := requestLogDefaultLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err != nil || n <= 0 {
				writeAPI(w, http.StatusBadRequest, apiResponse{Error: tAPI(r, "invalid_limit")})
			return
		}
		limit = n
	}
	cursor := r.URL.Query().Get("cursor")

	page, err := listRequestLogs(limit, cursor)
	if err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: page})
}

// GET /admin/api/opencode/config — opencode zen 配置 + 运行状态
func handleOpenCodeConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: tAPI(r, "method_not_allowed")})
		return
	}
	cfg := getZenConfig()
	maskedProxies := make([]string, 0, len(cfg.Proxies))
	for _, p := range cfg.Proxies {
		maskedProxies = append(maskedProxies, maskProxyURL(p))
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{
		"enabled":         cfg.Enabled,
		"key":             cfg.Key,
		"baseURL":         cfg.BaseURL,
		"proxies":         maskedProxies,
		"proxyStrategy":   cfg.ProxyStrategy,
		"proxyCooldowns":  zenProxyCooldownStatus(),
		"maxConcurrency":  cfg.MaxConcurrency,
		"retries":         cfg.Retries,
		"failover":        cfg.Failover,
		"failoverCount":   cfg.FailoverCount,
		"failoverMinutes": cfg.FailoverMinutes,
		"zenHeaders":      cfg.ZenHeaders,
		"compaction":      cfg.Compaction,
		"runtime": map[string]any{
			"failoverActive": zenFailedNow(),
		},
		"syncedModels": len(currentZenModels()),
		"lastSync":     lastZenModelSync(),
	}})
}

// POST /admin/api/opencode/config/update — 更新 opencode zen 配置（指针式补丁）
func handleOpenCodeConfigUpdate(w http.ResponseWriter, r *http.Request) {
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

	var req struct {
		Enabled         *bool             `json:"enabled"`
		Key             *string           `json:"key"`
		BaseURL         *string           `json:"baseURL"`
		Proxies         []string          `json:"proxies"`
		ProxyStrategy   *string           `json:"proxyStrategy"`
		MaxConcurrency  *int              `json:"maxConcurrency"`
		Retries         *int              `json:"retries"`
		Failover        *bool             `json:"failover"`
		FailoverCount   *int              `json:"failoverCount"`
		FailoverMinutes *int              `json:"failoverMinutes"`
		ZenHeaders      *map[string]string `json:"zenHeaders"`
		Compaction      *zenCompactConfig `json:"compaction"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: tAPI(r, "invalid_json")})
		return
	}

	cfg := getZenConfig()
	if req.Enabled != nil {
		cfg.Enabled = *req.Enabled
	}
	if req.Key != nil {
		cfg.Key = strings.TrimSpace(*req.Key)
	}
	if req.BaseURL != nil {
		u := strings.TrimSpace(*req.BaseURL)
		if u != "" && !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
			writeAPI(w, http.StatusBadRequest, apiResponse{Error: tAPI(r, "invalid_base_url")})
			return
		}
		cfg.BaseURL = u
	}
	if req.Proxies != nil {
		if err := validateProxyList(req.Proxies); err != nil {
			writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
			return
		}
		var cleaned []string
		for _, p := range req.Proxies {
			if line := strings.TrimSpace(p); line != "" {
				cleaned = append(cleaned, line)
			}
		}
		cfg.Proxies = cleaned
	}
	if req.ProxyStrategy != nil {
		switch *req.ProxyStrategy {
		case "round_robin", "random", "fill":
			cfg.ProxyStrategy = *req.ProxyStrategy
		default:
			writeAPI(w, http.StatusBadRequest, apiResponse{Error: tAPI(r, "invalid_proxy_strategy")})
			return
		}
	}
	if req.MaxConcurrency != nil {
		if *req.MaxConcurrency < 1 || *req.MaxConcurrency > 64 {
			writeAPI(w, http.StatusBadRequest, apiResponse{Error: tAPI(r, "invalid_concurrency")})
			return
		}
		cfg.MaxConcurrency = *req.MaxConcurrency
	}
	if req.Retries != nil {
		if *req.Retries < 0 || *req.Retries > 10 {
			writeAPI(w, http.StatusBadRequest, apiResponse{Error: tAPI(r, "invalid_retries")})
			return
		}
		cfg.Retries = *req.Retries
	}
	if req.Failover != nil {
		cfg.Failover = *req.Failover
	}
	if req.FailoverCount != nil {
		if *req.FailoverCount < 1 || *req.FailoverCount > 20 {
			writeAPI(w, http.StatusBadRequest, apiResponse{Error: tAPI(r, "invalid_failover")})
			return
		}
		cfg.FailoverCount = *req.FailoverCount
	}
	if req.FailoverMinutes != nil {
		if *req.FailoverMinutes < 1 || *req.FailoverMinutes > 120 {
			writeAPI(w, http.StatusBadRequest, apiResponse{Error: tAPI(r, "invalid_failover")})
			return
		}
		cfg.FailoverMinutes = *req.FailoverMinutes
	}
	if req.ZenHeaders != nil {
		cleaned := map[string]string{}
		for k, v := range *req.ZenHeaders {
			k = strings.TrimSpace(k)
			if k == "" || strings.TrimSpace(v) == "" {
				continue
			}
			cleaned[k] = strings.TrimSpace(v)
		}
		cfg.ZenHeaders = cleaned
	}
	if req.Compaction != nil {
		c := req.Compaction
		if c.Buffer < 0 || c.KeepTokens < 0 || c.MaxSummary < 0 {
			writeAPI(w, http.StatusBadRequest, apiResponse{Error: tAPI(r, "invalid_compaction")})
			return
		}
		cfg.Compaction.Auto = c.Auto
		cfg.Compaction.Buffer = c.Buffer
		cfg.Compaction.KeepTokens = c.KeepTokens
		cfg.Compaction.SummaryModel = strings.TrimSpace(c.SummaryModel)
		cfg.Compaction.MaxSummary = c.MaxSummary
	}

	setZenConfig(cfg)
	log.Printf("admin: opencode config updated (enabled=%v)", cfg.Enabled)
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: tAPI(r, "opencode_config_saved")})
}

// GET /admin/api/cline-proxy/config — Cline 出口代理配置（代理地址脱敏返回）
func handleClineProxyConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: tAPI(r, "method_not_allowed")})
		return
	}
	cfg := getClineProxyConfig()
	maskedProxies := make([]string, 0, len(cfg.Proxies))
	for _, p := range cfg.Proxies {
		maskedProxies = append(maskedProxies, maskProxyURL(p))
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{
		"proxies":       maskedProxies,
		"proxyStrategy": cfg.ProxyStrategy,
	}})
}

// POST /admin/api/cline-proxy/config/update — 更新 Cline 出口代理配置
func handleClineProxyConfigUpdate(w http.ResponseWriter, r *http.Request) {
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

	var req struct {
		Proxies       []string `json:"proxies"`
		ProxyStrategy *string  `json:"proxyStrategy"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: tAPI(r, "invalid_json")})
		return
	}

	if req.Proxies != nil {
		if err := validateProxyList(req.Proxies); err != nil {
			writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
			return
		}
	}
	cfg := getClineProxyConfig()
	updated := &clineProxyConfigData{
		Proxies:       cfg.Proxies,
		ProxyStrategy: cfg.ProxyStrategy,
	}
	if req.Proxies != nil {
		updated.Proxies = req.Proxies
	}
	if req.ProxyStrategy != nil {
		switch *req.ProxyStrategy {
		case "round_robin", "random", "fill":
			updated.ProxyStrategy = *req.ProxyStrategy
		default:
			writeAPI(w, http.StatusBadRequest, apiResponse{Error: tAPI(r, "invalid_proxy_strategy")})
			return
		}
	}
	setClineProxyConfig(updated)
	if err := getClineProxyPersistErr(); err != nil {
		writeAPI(w, http.StatusInternalServerError, apiResponse{Error: err.Error()})
		return
	}
	log.Printf("admin: cline proxy config updated (%d proxies, %s)", len(updated.Proxies), updated.ProxyStrategy)
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: tAPI(r, "cline_proxy_saved")})
}

// POST /admin/api/opencode/models/sync — 手动触发一次 opencode 模型同步
func handleOpenCodeModelSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: tAPI(r, "method_not_allowed")})
		return
	}
	res := syncZenModels()
	setLastZenModelSync(res)
	if res.Error != "" {
		writeAPI(w, http.StatusBadGateway, apiResponse{Success: false, Error: res.Error, Message: tAPI(r, "model_sync_failed")})
		return
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: res, Message: tAPI(r, "model_sync_done")})
}
