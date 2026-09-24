package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	pool     *AccountPool
	poolMu   sync.Mutex
	poolPath string
)

func init() {
	poolPath = resolveDataPath(".cline-accounts.json")
}

// resolveDataPath 按优先级查找数据文件：exe 目录 → 工作目录 → 用户主目录。
// 找到则用该路径（兼容旧版本在项目根目录存储的文件）；
// 都找不到则回退到 exe 目录（首次运行会在该位置创建）。
func resolveDataPath(filename string) string {
	// 1. exe 所在目录
	if exe, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(exe), filename)
		if fileExists(p) {
			return p
		}
	}
	// 2. 当前工作目录
	if pwd, err := os.Getwd(); err == nil {
		p := filepath.Join(pwd, filename)
		if fileExists(p) {
			return p
		}
	}
	// 3. 用户主目录下的 .cline2api/
	if home, err := os.UserHomeDir(); err == nil {
		p := filepath.Join(home, ".cline2api", filename)
		if fileExists(p) {
			return p
		}
	}
	// 回退：exe 目录（首次运行在此创建）
	if exe, err := os.Executable(); err == nil {
		return filepath.Join(filepath.Dir(exe), filename)
	}
	pwd, _ := os.Getwd()
	return filepath.Join(pwd, filename)
}

func loadPool() *AccountPool {
	poolMu.Lock()
	defer poolMu.Unlock()

	if pool != nil {
		return pool
	}

	data, err := os.ReadFile(poolPath)
	if err != nil {
		pool = &AccountPool{Accounts: []*Account{}, Keys: []string{}, Models: []Model{}}
		return pool
	}

	var p AccountPool
	if err := json.Unmarshal(data, &p); err != nil {
		pool = &AccountPool{Accounts: []*Account{}, Keys: []string{}, Models: []Model{}}
		return pool
	}

	if p.Accounts == nil {
		p.Accounts = []*Account{}
	}
	if p.Keys == nil {
		p.Keys = []string{}
	}
	if p.Models == nil {
		p.Models = []Model{}
	}
	pool = &p
	return pool
}

func savePool() {
	data, _ := json.MarshalIndent(pool, "", "  ")
	if err := os.WriteFile(poolPath, data, 0600); err != nil {
		log.Printf("Failed to save accounts: %v", err)
	}
}

func addAccount(acc *Account) {
	p := loadPool()
	poolMu.Lock()
	p.Accounts = append(p.Accounts, acc)
	poolMu.Unlock()
	savePool()
}

// findAccountByRefreshToken 按 refreshToken 查找已有账号（不存在返回 nil）。
// 用于导入时的去重：同一个 refreshToken 只应存在一个账号。
func findAccountByRefreshToken(refreshToken string) *Account {
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		return nil
	}
	p := loadPool()
	poolMu.Lock()
	defer poolMu.Unlock()

	for _, a := range p.Accounts {
		if strings.TrimSpace(a.RefreshToken) == refreshToken {
			return a
		}
	}
	return nil
}

// accountExists 判断该 refreshToken 是否已在账号池中。
func accountExists(refreshToken string) bool {
	return findAccountByRefreshToken(refreshToken) != nil
}

// isDuplicateImportToken 判断待导入的 refreshToken 是否应跳过（导入去重）：
// 账号池中已存在同一 refreshToken，或本批次内已处理过（seen）。
// seen 由调用方维护、在此更新，用于同一批次内的去重。
func isDuplicateImportToken(refreshToken string, seen map[string]bool) bool {
	if seen[refreshToken] || accountExists(refreshToken) {
		return true
	}
	seen[refreshToken] = true
	return false
}

func removeAccount(accountID string) bool {
	p := loadPool()
	poolMu.Lock()
	defer poolMu.Unlock()

	for i, a := range p.Accounts {
		if a.AccountID == accountID {
			p.Accounts = append(p.Accounts[:i], p.Accounts[i+1:]...)
			savePool()
			return true
		}
	}
	return false
}

func getAccountByID(accountID string) *Account {
	p := loadPool()
	poolMu.Lock()
	defer poolMu.Unlock()

	for _, a := range p.Accounts {
		if a.AccountID == accountID {
			return a
		}
	}
	return nil
}

func refreshAccountToken(acc *Account) error {
	resp, err := refreshClineToken(acc.RefreshToken)
	if err != nil {
		if isRefreshRejected(err) {
			acc.Status = "expired"
		} else {
			acc.Status = "cooldown"
			acc.CooldownUntil = time.Now().Add(5 * time.Minute)
		}
		savePool()
		return fmt.Errorf("token refresh failed: %w", err)
	}

	acc.AccessToken = "workos:" + resp.Data.AccessToken
	if resp.Data.RefreshToken != "" {
		acc.RefreshToken = resp.Data.RefreshToken
	}
	acc.ExpiresAt = parseExpiry(resp.Data.ExpiresAt) - 60000
	acc.Status = "active"
	savePool()
	return nil
}

func pickAccount() *Account {
	p := loadPool()
	poolMu.Lock()
	defer poolMu.Unlock()
	return pickAccountLocked(p)
}

// pickAccountForModel 按轮询/策略挑选一个「该模型未处于模型级冷却」的账号；
// 所有 active 账号对该模型都冷却时回退到普通 pickAccount（请求会得到模型级 429 提示）。
// 空模型名等同于 pickAccount。
func pickAccountForModel(model string) *Account {
	return pickAccountForModelWithFallback(model, true)
}

func pickAccountForModelStrict(model string) *Account {
	return pickAccountForModelWithFallback(model, false)
}

func pickAccountForModelWithFallback(model string, fallbackToActive bool) *Account {
	if model == "" {
		return pickAccount()
	}

	p := loadPool()
	poolMu.Lock()
	defer poolMu.Unlock()

	active := make([]*Account, 0)
	for _, a := range p.Accounts {
		if a.Status == "active" {
			active = append(active, a)
		}
	}
	if len(active) == 0 {
		return nil
	}

	// 该模型未冷却的账号列表
	eligible := make([]*Account, 0, len(active))
	for _, a := range active {
		until, cool := a.ModelCooldowns[model]
		if !cool || time.Now().After(until) {
			if cool {
				delete(a.ModelCooldowns, model)
			}
			eligible = append(eligible, a)
		}
	}

	if len(eligible) == 0 {
		if fallbackToActive {
			return pickAccountLocked(p)
		}
		return nil
	}

	cfg := getProxyConfig()
	var acc *Account
	switch cfg.Strategy {
	case "fill":
		acc = eligible[0]
	case "random":
		n := time.Now().UnixNano() % int64(len(eligible))
		acc = eligible[n]
	default: // round_robin
		if p.CurrentIdx >= len(eligible) {
			p.CurrentIdx = 0
		}
		acc = eligible[p.CurrentIdx]
		p.CurrentIdx = (p.CurrentIdx + 1) % len(eligible)
	}
	savePool()
	return acc
}

// pickAccountForModelLeastUsed 在所有「模型未冷却」的 active 账号中，选择该模型
// 历史用量最少的账号（并清掉已过期的冷却记录）。等量时按轮询索引取，保持原有
// 公平性；全部不可用返回 nil。供回退链上的非首选模型使用：流量应摊到较少
// 使用的账号上，而不是每次都砸在第一个可用账号。
func pickAccountForModelLeastUsed(model string) *Account {
	if model == "" {
		return pickAccount()
	}

	p := loadPool()
	poolMu.Lock()
	defer poolMu.Unlock()

	var best *Account
	var bestCount int64
	for _, a := range p.Accounts {
		if a.Status != "active" {
			continue
		}
		if until, cool := a.ModelCooldowns[model]; cool {
			if time.Now().After(until) {
				delete(a.ModelCooldowns, model)
			} else {
				continue // 该账号此模型冷却中
			}
		}
		var cnt int64
		if st, ok := a.ModelStats[model]; ok {
			cnt = st.UsageCount
		}
		if best == nil || cnt < bestCount {
			best, bestCount = a, cnt
		}
	}
	if best == nil {
		return nil
	}
	savePool()
	return best
}

// sortModelsByAvailability 将回退链按「可用性优先」重排：
//  1. 有未冷却账号的模型在前；
//  2. 同组内按该模型的账号总用量升序——把流量摊到用得少的模型上，
//     避免每次都选第一个可用模型、把它的额度打到冷却。
//  3. 稳定排序：可用性与用量相同时保持管理员配置的优先级顺序。
func sortModelsByAvailability(chain []string) []string {
	p := loadPool()
	poolMu.Lock()
	defer poolMu.Unlock()

	now := time.Now()
	type cand struct {
		model     string
		avail     bool
		minUsage  int64
		origOrder int
	}
	cs := make([]cand, 0, len(chain))
	for i, m := range chain {
		avail := false
		minUsage := int64(-1)
		for _, a := range p.Accounts {
			if a.Status != "active" {
				continue
			}
			if until, cool := a.ModelCooldowns[m]; cool {
				if now.After(until) {
					delete(a.ModelCooldowns, m)
				} else {
					continue
				}
			}
			avail = true
			var cnt int64
			if st, ok := a.ModelStats[m]; ok {
				cnt = st.UsageCount
			}
			if minUsage < 0 || cnt < minUsage {
				minUsage = cnt
			}
		}
		cs = append(cs, cand{model: m, avail: avail, minUsage: minUsage, origOrder: i})
	}
	sort.SliceStable(cs, func(x, y int) bool {
		if cs[x].avail != cs[y].avail {
			return cs[x].avail
		}
		if cs[x].avail && cs[x].minUsage != cs[y].minUsage {
			return cs[x].minUsage < cs[y].minUsage
		}
		return cs[x].origOrder < cs[y].origOrder
	})
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.model
	}
	savePool()
	return out
}

// pickAccountLocked 在已持有 poolMu 的前提下执行普通轮询挑选（供 pickAccountForModel 回退用）。
func pickAccountLocked(p *AccountPool) *Account {
	active := make([]*Account, 0)
	for _, a := range p.Accounts {
		if a.Status == "active" {
			active = append(active, a)
		}
	}
	if len(active) == 0 {
		return nil
	}
	cfg := getProxyConfig()
	var acc *Account
	switch cfg.Strategy {
	case "fill":
		acc = active[0]
	case "random":
		n := time.Now().UnixNano() % int64(len(active))
		acc = active[n]
	default:
		if p.CurrentIdx >= len(active) {
			p.CurrentIdx = 0
		}
		acc = active[p.CurrentIdx]
		p.CurrentIdx = (p.CurrentIdx + 1) % len(active)
	}
	savePool()
	return acc
}

func ensureAccountToken(acc *Account) (string, error) {
	if acc.AccessToken != "" && time.Now().UnixMilli() < acc.ExpiresAt {
		return acc.AccessToken, nil
	}

	if err := refreshAccountToken(acc); err != nil {
		return "", err
	}

	return acc.AccessToken, nil
}

func listAccounts() []*Account {
	p := loadPool()
	poolMu.Lock()
	defer poolMu.Unlock()

	result := make([]*Account, len(p.Accounts))
	for i, a := range p.Accounts {
		// Don't expose tokens
		cp := &Account{
			AccountID:        a.AccountID,
			Email:            a.Email,
			Status:           a.Status,
			CooldownUntil:    a.CooldownUntil,
			LastUsed:         a.LastUsed,
			UsageCount:       a.UsageCount,
			PromptTokens:     a.PromptTokens,
			CompletionTokens: a.CompletionTokens,
			TotalTokens:      a.TotalTokens,
			CachedTokens:     a.CachedTokens,
			CreatedAt:        a.CreatedAt,
		}
		// 按模型细分统计（脱敏拷贝）
		if len(a.ModelStats) > 0 {
			cp.ModelStats = make(map[string]*ModelStat, len(a.ModelStats))
			for mid, st := range a.ModelStats {
				sc := *st
				cp.ModelStats[mid] = &sc
			}
		}
		// 模型级冷却（脱敏拷贝）
		if len(a.ModelCooldowns) > 0 {
			cp.ModelCooldowns = make(map[string]time.Time, len(a.ModelCooldowns))
			for mid, until := range a.ModelCooldowns {
				cp.ModelCooldowns[mid] = until
			}
		}
		result[i] = cp
	}
	return result
}

func addAccountFromDeviceAuth() (*Account, error) {
	fmt.Println()
	fmt.Println("=== Add New Cline Account (OAuth) ===")
	fmt.Println()

	device, err := workosDeviceAuth()
	if err != nil {
		return nil, err
	}

	authURL := device.VerificationURIComplete
	if authURL == "" {
		authURL = device.VerificationURI
	}

	fmt.Println("  1. Open this URL in your browser:")
	fmt.Println("     " + authURL)
	fmt.Println("  2. Enter code: " + device.UserCode)
	fmt.Println("  3. Log in with Google, GitHub, or email")
	fmt.Println()

	_ = openBrowser(authURL)
	fmt.Println("  Waiting for authorization...")

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
		return nil, err
	}

	fmt.Println("  WorkOS authorized. Registering with Cline...")

	cline, err := registerWithCline(workosTok.AccessToken, workosTok.RefreshToken)
	if err != nil {
		return nil, err
	}

	if cline.Data.RefreshToken == "" {
		return nil, fmt.Errorf("cline registration missing refresh token")
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
	fmt.Printf("  Account added! Email: %s\n", email)
	return acc, nil
}
