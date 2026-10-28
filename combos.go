package main

// 模型组合（Named Model Combos）+ 有序回退 —— Phase 1。
//
// 设计目标：管理员可配置若干具名组合（如 premium-coding / fast-free），每个组合
// 是一串**有序**的现有模型条目；客户端把组合 ID 当作普通模型名请求，路由器按配置
// 顺序逐个尝试，遇到可重试的上游失败就换下一个条目，直到成功或耗尽。
//
// 关键约束（实现时严格遵守）：
//   - 组合内的顺序是唯一权威。**不**按用量/延迟/质量/随机/账号轮询重排健康条目；
//     运行时可用性只会让某个条目被「跳过」，不会改变配置顺序。
//   - 每个条目的派发复用既有路径：zen 免费模型走 callZenAPI；自定义 provider 走
//     callCustomProviderAPI（含冗余组、粘性会话）；其余走 Cline 账号池（含账号轮询、
//     模型级冷却、402/429 处理）。**不**对每个条目递归调用全局 ModelChain / free 链。
//   - 默认情况下组合只走自己的序列；只有管理员显式开启 emergencyFallback 才允许
//     落到全局 free 链（应急），且该行为在 UI 与报告中明确标注。
//   - 复用既有持久化（.cline-config.json + 原子写 + 损坏备份），不引入新存储。
//
// 数据模型预留了 Phase 2 的扩展位（Category 等）；本阶段不实现任何自动质量评分，
// 也**不**从模型名推断质量。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
)

// ComboEntry 是组合里的一个有序条目。
type ComboEntry struct {
	// Model 引用的现有模型 ID（必须是目录里已知模型，不能是 "free" 别名或另一组合）。
	Model string `json:"model"`
	// Disabled 条目级停用：保留在配置里但运行时跳过（不改动组合顺序语义）。
	Disabled bool `json:"disabled,omitempty"`
	// Source 可选上游固定：""=自动(默认优先级) | "cline" | "zen" | "provider"。
	Source string `json:"source,omitempty"`
	// Provider 仅当 Source=="provider" 时生效：目标自定义 Provider 的 Name。
	Provider string `json:"provider,omitempty"`
}

// comboEntryKey 组合条目去重键：同一模型但不同上游固定（Source/Provider）不算重复，
// 允许把同一模型分别钉到不同的上游。
func comboEntryKey(e ComboEntry) string {
	return strings.TrimSpace(e.Model) + "\x00" + strings.TrimSpace(e.Source) + "\x00" + strings.TrimSpace(e.Provider)
}

// ModelCombo 是一个具名、有序、可启停的模型组合。
type ModelCombo struct {
	// ID 稳定唯一标识，直接作为客户端可请求的模型名。不可与任何真实模型 ID/
	// 保留别名冲突（保存时校验），避免静默遮蔽真实上游模型。
	ID string `json:"id"`
	// Name 显示名；Description/Category 为可选元数据。
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Category    string `json:"category,omitempty"`
	// Enabled 整组启停：停用后不进 /v1/models，直接请求返回明确客户端错误。
	Enabled bool `json:"enabled"`
	// EmergencyFallback 显式允许在组合全部条目失败后落到全局 free 链（应急）。
	// 默认 false：组合只走自己的序列，不会隐式触发全局回退。
	EmergencyFallback bool `json:"emergencyFallback,omitempty"`
	// Entries 有序条目列表，顺序即回退优先级。
	Entries   []ComboEntry `json:"entries"`
	CreatedAt time.Time    `json:"createdAt"`
	UpdatedAt time.Time    `json:"updatedAt"`
}

// comboIDRe 组合 ID 允许的字符集：字母数字开头，后接字母数字/点/下划线/连字符。
// 故意不含 "/" 与 ":"，避免与 "provider/model" 形态的真实模型 ID 混淆。
var comboIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

const (
	comboIDMaxLen   = 64
	comboMaxEntries = 64
)

// ============================================================================
// 持久化（复用 .cline-config.json / proxyConfig / 原子写）
// ============================================================================

// cloneCombos 深拷贝，避免调用方在锁外持有内部切片引用。
func cloneCombos(in []ModelCombo) []ModelCombo {
	if len(in) == 0 {
		return nil
	}
	out := make([]ModelCombo, len(in))
	for i, c := range in {
		out[i] = c
		if len(c.Entries) > 0 {
			ents := make([]ComboEntry, len(c.Entries))
			copy(ents, c.Entries)
			out[i].Entries = ents
		}
	}
	return out
}

// listCombos 返回全部组合（含停用）的快照副本。
func listCombos() []ModelCombo {
	proxyConfigMu.Lock()
	defer proxyConfigMu.Unlock()
	if proxyConfig == nil {
		return nil
	}
	return cloneCombos(proxyConfig.Combos)
}

// findCombo 按 ID 查找组合（含停用）。ID 精确匹配（去空白）。
func findCombo(id string) (ModelCombo, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return ModelCombo{}, false
	}
	proxyConfigMu.Lock()
	defer proxyConfigMu.Unlock()
	if proxyConfig == nil {
		return ModelCombo{}, false
	}
	for _, c := range proxyConfig.Combos {
		if c.ID == id {
			cp := c
			if len(c.Entries) > 0 {
				ents := make([]ComboEntry, len(c.Entries))
				copy(ents, c.Entries)
				cp.Entries = ents
			}
			return cp, true
		}
	}
	return ModelCombo{}, false
}

// findEnabledCombo 只在组合存在且启用时返回（路由用）。
func findEnabledCombo(id string) (ModelCombo, bool) {
	c, ok := findCombo(id)
	if !ok || !c.Enabled {
		return ModelCombo{}, false
	}
	return c, true
}

// comboEnabledIDs 返回启用中组合 ID 集合（供 /v1/models 暴露）。
func comboEnabledList() []ModelCombo {
	all := listCombos()
	out := make([]ModelCombo, 0, len(all))
	for _, c := range all {
		if c.Enabled {
			out = append(out, c)
		}
	}
	return out
}

// knownModelIDs 返回「可被引用的模型 ID」集合：池内全部模型 + 启用中 provider
// 声明的模型 + zen 种子/别名。组合 ID 与条目引用都基于这一集合校验。
func knownModelIDs() map[string]bool {
	known := make(map[string]bool)
	for _, m := range getAllModels() {
		if m.ID != "" {
			known[m.ID] = true
		}
	}
	for _, p := range listProviders() {
		if !p.Enabled {
			continue
		}
		for _, id := range p.ModelIDs {
			if id != "" {
				known[id] = true
			}
		}
	}
	for _, sm := range zenSeedModels {
		if sm.ID != "" {
			known[sm.ID] = true
		}
		for _, a := range sm.Aliases {
			if a != "" {
				known[a] = true
			}
		}
	}
	return known
}

// ============================================================================
// 校验
// ============================================================================

// validateCombo 校验组合定义。existing 是除自身以外的现有组合（用于唯一性校验）。
func validateCombo(c ModelCombo, existing []ModelCombo) error {
	id := strings.TrimSpace(c.ID)
	if id == "" {
		return errors.New("combo id is required")
	}
	if len(id) > comboIDMaxLen {
		return fmt.Errorf("combo id is too long (max %d characters)", comboIDMaxLen)
	}
	if !comboIDRe.MatchString(id) {
		return errors.New("combo id may only contain letters, digits, dot, underscore and hyphen")
	}
	if id == "free" {
		return errors.New(`combo id "free" is a reserved alias`)
	}
	if knownModelIDs()[id] {
		return fmt.Errorf("combo id %q collides with an existing model id", id)
	}
	name := strings.TrimSpace(c.Name)
	if name == "" {
		return errors.New("combo name is required")
	}
	for _, e := range existing {
		if e.ID == id {
			return fmt.Errorf("combo id %q already exists", id)
		}
		if strings.EqualFold(strings.TrimSpace(e.Name), name) {
			return fmt.Errorf("combo name %q is already in use", name)
		}
	}
	if len(c.Entries) == 0 {
		return errors.New("combo needs at least one model entry")
	}
	if len(c.Entries) > comboMaxEntries {
		return fmt.Errorf("combo has too many entries (max %d)", comboMaxEntries)
	}
	known := knownModelIDs()
	seen := make(map[string]bool, len(c.Entries))
	enabledCount := 0
	for _, en := range c.Entries {
		m := strings.TrimSpace(en.Model)
		if m == "" {
			return errors.New("combo entry has an empty model id")
		}
		if m == "free" {
			return errors.New(`the "free" alias cannot be used as a combo entry; add concrete models instead`)
		}
		key := comboEntryKey(en)
		if seen[key] {
			return fmt.Errorf("duplicate combo entry %q", m)
		}
		seen[key] = true
		if !known[m] {
			return fmt.Errorf("unknown model reference %q", m)
		}
		if routeModel(m) == "reject" {
			return fmt.Errorf("model %q is a paid opencode model and cannot be used in a combo", m)
		}
		if err := validateComboEntryPin(en, m); err != nil {
			return err
		}
		if !en.Disabled && !isModelDisabled(m) {
			enabledCount++
		}
	}
	if c.Enabled && enabledCount == 0 {
		return errors.New("an enabled combo needs at least one enabled entry")
	}
	return nil
}

// comboPinError 组合条目上游固定的校验错误：携带 i18n key + 英文兜底文本，
// 便于 handler 本地化，同时保证无上下文调用时（测试）仍可读。
type comboPinError struct {
	key  string
	msg  string
	args []any
}

func (e *comboPinError) Error() string { return e.msg }

// localizeComboError 把校验错误本地化：comboPinError 走 tAPI key，其余（既有英文
// 校验信息）原样返回。
func localizeComboError(r *http.Request, err error) string {
	if err == nil {
		return ""
	}
	var pe *comboPinError
	if errors.As(err, &pe) && pe.key != "" {
		return tAPI(r, pe.key, pe.args...)
	}
	return err.Error()
}

// validateComboEntryPin 校验条目的上游固定（Source/Provider）。空 Source 表示自动
// 派发（默认优先级），永远合法；非空时必须指向一个真实可用的目标。
func validateComboEntryPin(en ComboEntry, model string) error {
	switch src := strings.TrimSpace(en.Source); src {
	case "":
		return nil
	case "cline":
		// 必须存在同名、真能由 Cline 账号池服务的池内条目（排除 zen 与
		// provider-only 模型），否则固定到 cline 账号池要么无模型、要么必然 400。
		if !clinePoolModelExists(model) {
			return &comboPinError{
				key:  "combo_entry_cline_unknown",
				msg:  fmt.Sprintf("model %q is not a Cline account-pool model (non-zen); cannot pin source=cline", model),
				args: []any{model},
			}
		}
		return nil
	case "zen":
		zm, ok := resolveZenInfo(model)
		if !ok || !isZenFreeModel(zm) {
			return &comboPinError{
				key:  "combo_entry_zen_not_free",
				msg:  fmt.Sprintf("model %q is not an available free zen model; cannot pin source=zen", model),
				args: []any{model},
			}
		}
		return nil
	case "provider":
		name := strings.TrimSpace(en.Provider)
		if name == "" {
			return &comboPinError{
				key: "combo_entry_provider_required",
				msg: "provider name is required when source=provider",
			}
		}
		if !providerNameServesModel(name, model) {
			return &comboPinError{
				key:  "combo_entry_provider_unknown",
				msg:  fmt.Sprintf("no enabled provider %q serves model %q", name, model),
				args: []any{name, model},
			}
		}
		return nil
	default:
		return &comboPinError{
			key:  "combo_entry_bad_source",
			msg:  fmt.Sprintf("invalid combo entry source %q (allowed: cline, zen, provider)", src),
			args: []any{},
		}
	}
}

// normalizeCombo 规整输入：去空白、丢弃空条目行。
func normalizeCombo(c ModelCombo) ModelCombo {
	c.ID = strings.TrimSpace(c.ID)
	c.Name = strings.TrimSpace(c.Name)
	c.Description = strings.TrimSpace(c.Description)
	c.Category = strings.TrimSpace(c.Category)
	ents := make([]ComboEntry, 0, len(c.Entries))
	for _, en := range c.Entries {
		en.Model = strings.TrimSpace(en.Model)
		en.Source = strings.TrimSpace(en.Source)
		en.Provider = strings.TrimSpace(en.Provider)
		if en.Model == "" {
			continue
		}
		ents = append(ents, en)
	}
	c.Entries = ents
	return c
}

// ============================================================================
// CRUD
// ============================================================================

// upsertCombo 新建或更新组合（ID 为主键）并持久化。
func upsertCombo(c ModelCombo) (ModelCombo, error) {
	c = normalizeCombo(c)

	// 校验在**不持有** proxyConfigMu 的情况下进行：validateCombo 会读取模型目录、
	// provider 列表并做 zen 路由判定（涉及 poolMu / providersMu / zenConfigMu）。
	// 在配置锁内做这些外部读取会扩大锁范围并带来锁序风险，故先快照再校验。
	existing := listCombos()
	others := make([]ModelCombo, 0, len(existing))
	for _, e := range existing {
		if e.ID == c.ID {
			continue
		}
		others = append(others, e)
	}
	if err := validateCombo(c, others); err != nil {
		return ModelCombo{}, err
	}

	proxyConfigMu.Lock()
	defer proxyConfigMu.Unlock()
	if proxyConfig == nil {
		proxyConfig = defaultProxyConfig()
	}
	idx := -1
	for i, e := range proxyConfig.Combos {
		if e.ID == c.ID {
			idx = i
			continue
		}
		// 锁内仅做无外部调用的复核：防止校验与写入之间并发新增导致的名称冲突。
		if strings.EqualFold(strings.TrimSpace(e.Name), c.Name) {
			return ModelCombo{}, fmt.Errorf("combo name %q is already in use", c.Name)
		}
	}
	now := time.Now()
	if idx >= 0 {
		c.CreatedAt = proxyConfig.Combos[idx].CreatedAt
	} else if c.CreatedAt.IsZero() {
		c.CreatedAt = now
	}
	c.UpdatedAt = now
	if idx >= 0 {
		proxyConfig.Combos[idx] = c
	} else {
		proxyConfig.Combos = append(proxyConfig.Combos, c)
	}
	saveProxyConfigLocked()
	return c, nil
}

// deleteCombo 删除组合。
func deleteCombo(id string) bool {
	id = strings.TrimSpace(id)
	proxyConfigMu.Lock()
	defer proxyConfigMu.Unlock()
	if proxyConfig == nil {
		return false
	}
	for i, c := range proxyConfig.Combos {
		if c.ID == id {
			proxyConfig.Combos = append(proxyConfig.Combos[:i], proxyConfig.Combos[i+1:]...)
			saveProxyConfigLocked()
			return true
		}
	}
	return false
}

// setComboEnabled 启停组合。
func setComboEnabled(id string, enabled bool) (ModelCombo, error) {
	id = strings.TrimSpace(id)
	proxyConfigMu.Lock()
	defer proxyConfigMu.Unlock()
	if proxyConfig == nil {
		return ModelCombo{}, errors.New("combo not found")
	}
	for i := range proxyConfig.Combos {
		if proxyConfig.Combos[i].ID == id {
			if enabled {
				enabledCount := 0
				for _, en := range proxyConfig.Combos[i].Entries {
					if !en.Disabled {
						enabledCount++
					}
				}
				if enabledCount == 0 {
					return ModelCombo{}, errors.New("cannot enable a combo with no enabled entries")
				}
			}
			proxyConfig.Combos[i].Enabled = enabled
			proxyConfig.Combos[i].UpdatedAt = time.Now()
			saveProxyConfigLocked()
			cp := proxyConfig.Combos[i]
			if len(cp.Entries) > 0 {
				ents := make([]ComboEntry, len(cp.Entries))
				copy(ents, cp.Entries)
				cp.Entries = ents
			}
			return cp, nil
		}
	}
	return ModelCombo{}, errors.New("combo not found")
}

// cloneCombo 复制一个组合，生成唯一 ID/名称。
func cloneCombo(id string) (ModelCombo, error) {
	src, ok := findCombo(id)
	if !ok {
		return ModelCombo{}, errors.New("combo not found")
	}
	all := listCombos()
	takenID := map[string]bool{}
	takenName := map[string]bool{}
	for _, c := range all {
		takenID[c.ID] = true
		takenName[strings.ToLower(c.Name)] = true
	}
	base := src.ID
	// 预留后缀空间，避免 64 字符 ID 克隆后超长被 validateCombo 拒绝。
	if max := comboIDMaxLen - len("-copy-99"); len(base) > max {
		base = base[:max]
	}
	newID := base + "-copy"
	for i := 2; takenID[newID]; i++ {
		newID = fmt.Sprintf("%s-copy-%d", base, i)
	}
	newName := src.Name + " (copy)"
	for i := 2; takenName[strings.ToLower(newName)]; i++ {
		newName = fmt.Sprintf("%s (copy %d)", src.Name, i)
	}
	cp := src
	cp.ID = newID
	cp.Name = newName
	cp.CreatedAt = time.Time{}
	cp.UpdatedAt = time.Time{}
	return upsertCombo(cp)
}

// ============================================================================
// 组合运行：有序回退
// ============================================================================

// comboRun 是一次组合派发的完整结果。
type comboRun struct {
	Resp     *http.Response
	Acc      *Account
	Upstream string
	Model    string // 实际服务的模型
	Trace    []AttemptLog
	Err      error
}

// comboRetryable 判断某次条目尝试的失败是否可重试（应继续尝试下一个条目）。
//
// 可重试：限流(429)/配额(402)/网关重定向(421)/瞬态 5xx/超时/传输错误/模型不存在(404)。
// 不可重试：客户端/配置类错误 400/401/403 —— 重试下一个模型没有意义，直接失败。
func comboRetryable(err error) bool {
	if err == nil {
		return false
	}
	var acct *clineAccountUnavailableError
	if errors.As(err, &acct) {
		return true // 账号不可用：可能换号/换条目即可恢复
	}
	var apiErr *clineAPIError
	if errors.As(err, &apiErr) {
		switch apiErr.statusCode {
		case 400, 401, 403:
			return false
		case 402, 408, 409, 421, 425, 429:
			return true
		case 404:
			return true
		default:
			return apiErr.statusCode >= 500
		}
	}
	// 其余（zen 网络/上游错误、未知错误）按瞬态处理，交给下一个条目。
	return true
}

// attemptStatus 从错误里尽量提取上游 HTTP 状态码（供尝试轨迹展示）。
func attemptStatus(err error) int {
	var apiErr *clineAPIError
	if errors.As(err, &apiErr) {
		return apiErr.statusCode
	}
	return 0
}

// comboRouteKind 判定组合条目的派发路径：zen / reject（付费 zen）/ cline，以及
// zen-degraded（zen 处于故障转移窗口，条目运行时不可用应跳过）。
//
// 与 routeModel 的唯一区别：routeModel 在 zen 故障转移窗口会把 zen 免费模型降级
// 为 "cline"，而组合**不能**沿用该降级 —— 组合没有全局 free 链可改写，若把 zen
// 模型 ID 原样发给 Cline 会得到 400 "invalid model format"，进而被误判为永久失败
// 并中止整条组合。故这里显式区分「同名 cline 冲突」（走 cline，与 routeModel 一致）
// 与「zen 降级」（走 zen-degraded → 跳过条目）。
func comboRouteKind(model string) string {
	switch routeModel(model) {
	case "reject":
		return "reject"
	case "zen":
		return "zen"
	}
	// routeModel == "cline"
	zm, ok := resolveZenInfo(model)
	if !ok || !isZenFreeModel(zm) {
		return "cline" // 普通 cline 模型
	}
	if clineModelWithIDExists(model) {
		return "cline" // 同名 cline 冲突：cline 优先（与 routeModel 一致）
	}
	return "zen-degraded"
}

// clineModelWithIDExists 判断池内是否存在与给定模型同名的非 zen 模型
// （自定义冲突场景，routeModel 会把同名 zen 模型让给 cline）。
func clineModelWithIDExists(model string) bool {
	id := strings.TrimPrefix(strings.TrimSpace(model), "opencode/")
	p := loadPool()
	poolMu.Lock()
	defer poolMu.Unlock()
	for _, pm := range p.Models {
		if !isZenSource(pm) && pm.ID == id {
			return true
		}
	}
	return false
}

// clinePoolModelExists 判断模型是否真能由 Cline 账号池服务：排除 zen 与自定义
// provider 来源。source=cline 固定必须用它（严格）——provider 同步进池的条目
// 走 Cline 会得到 400 "invalid model format"，进而被组合误判为永久失败。
func clinePoolModelExists(model string) bool {
	id := strings.TrimPrefix(strings.TrimSpace(model), "opencode/")
	p := loadPool()
	poolMu.Lock()
	defer poolMu.Unlock()
	for _, pm := range p.Models {
		if !isZenSource(pm) && pm.Source != "provider" && pm.ID == id {
			return true
		}
	}
	return false
}

// entryRouteKind 在 comboRouteKind 之上叠加条目的上游固定（Source/Provider）。
//
//   - Source == "" → comboRouteKind(Model)（默认自动派发，行为不变）。
//   - "cline"      → 模型真能由 Cline 账号池服务时 "cline"（跳过自定义 provider），
//     否则 "unavailable"（跳过，避免 provider-only 模型被发给 Cline 触发 400 中止）。
//   - "provider"   → "cline"（provider 名称在 cline 分支里强制指定）。
//   - "zen"        → 显式固定压过 AUTO 的同名 cline 冲突降级：可解析且为免费 zen
//     模型时返回 "zen"，zen→cline 故障转移窗口内返回 "zen-degraded"，否则
//     "unavailable"（跳过）。
//   - 其他（未知，通常已被 validateCombo 拦截）→ 回退到自动派发，避免运行时误伤。
func entryRouteKind(e ComboEntry) string {
	base := comboRouteKind(e.Model)
	switch strings.TrimSpace(e.Source) {
	case "":
		return base
	case "cline":
		// 严格判定：模型必须真能由 Cline 账号池服务。provider-only 模型（含同步
		// 进池的 source=provider 行）走 Cline 会 400，故按运行时不可用跳过该条目，
		// 而不是把 provider 的模型 ID 原样发给 Cline 触发整条组合中止。
		if !clinePoolModelExists(e.Model) {
			return "unavailable"
		}
		return "cline"
	case "provider":
		return "cline"
	case "zen":
		// 显式 zen 固定压过 AUTO 的「同名 cline 冲突」降级：这正是用 zen 固定来
		// 消歧的用途。只按 zen 自身状态判定，与 validateComboEntryPin 保持一致。
		zm, ok := resolveZenInfo(e.Model)
		if !ok || !isZenFreeModel(zm) {
			return "unavailable"
		}
		if cfg := getZenConfig(); cfg.Failover && zenFailedNow() {
			return "zen-degraded"
		}
		return "zen"
	default:
		return base
	}
}

// callSingleModel 通过既有 provider/账号路径调用**单个**模型，不触发全局 ModelChain
// 或 free 链：这是组合回退的安全基础。
//
//   - zen 免费模型 → callZenAPI（含匿名免费层处理）
//   - 自定义 provider → callCustomProviderAPI（冗余组 + 粘性会话）；条目固定到
//     某一 provider 时改走 callCustomProviderAPINamed（只看该家，不回退账号池）
//   - 其余 → Cline 账号池（模型级冷却、402/429 换号，账号轮询）
//
// 返回：resp / 使用的账号（若有）/ 上游归因 / error。
func callSingleModel(params map[string]any, entry ComboEntry, stream bool, sessionID string) (*http.Response, *Account, string, error) {
	model := strings.TrimSpace(entry.Model)
	ep := withModel(params, model)
	delete(ep, servedByParam)

	switch entryRouteKind(entry) {
	case "reject":
		return nil, nil, upstreamOpenCode, fmt.Errorf("model %q is a paid opencode model; only free models are proxied", model)
	case "zen-degraded":
		// zen 处于故障转移窗口：绝不把 zen 免费模型 ID 原样发给 Cline（会被判
		// 400 "invalid model format"，进而被组合误判为永久失败）。按运行时不可用
		// 跳过该条目 —— 返回可重试错误，组合继续尝试下一个条目。
		return nil, nil, upstreamOpenCode, &freeModelUnavailableError{
			message: fmt.Sprintf("model %q temporarily unavailable (opencode zen degraded)", model),
		}
	case "unavailable":
		// 条目固定的上游当前无法服务该模型（zen 固定到非免费/非 zen 模型，或
		// cline 固定到 provider-only 模型等）：按运行时不可用跳过，返回可重试错误，
		// 让组合继续尝试下一个条目而不是中止。
		return nil, nil, upstreamOpenCode, &freeModelUnavailableError{
			message: fmt.Sprintf("model %q is not available on the pinned source", model),
		}
	case "zen":
		if zm, ok := resolveZenInfo(model); ok {
			if out := maybeCompact(ep, zm, sessionID); out.changed {
				log.Printf("  combo %s", out.note)
			}
		}
		resp, err := callZenAPI(ep, stream)
		if err != nil {
			return nil, nil, upstreamOpenCode, err
		}
		return resp, nil, upstreamOpenCode, nil
	}

	// cline 侧：默认先自定义 provider（该模型若接入 provider），失败后走账号池。
	switch strings.TrimSpace(entry.Source) {
	case "provider":
		// 显式固定到某一家 provider：只看它，失败也**不**回退到 Cline 账号池
		// （用户明确指定了唯一上游）。
		resp, perr, attempted := callCustomProviderAPINamed(ep, stream, entry.Provider)
		if !attempted {
			return nil, nil, upstreamProvider, &freeModelUnavailableError{
				message: fmt.Sprintf("model %q is not served by provider %q", model, strings.TrimSpace(entry.Provider)),
			}
		}
		if perr != nil {
			// 固定 provider 失败：包装成可重试的「跳过」，避免 400/401/403（如
			// 一把被轮换的 API key）被 comboRetryable 判为永久错误而中止整条组合。
			return nil, nil, upstreamProvider, &freeModelUnavailableError{
				message: fmt.Sprintf("pinned provider %q failed for %q: %s", strings.TrimSpace(entry.Provider), model, truncate(perr.Error(), 200)),
			}
		}
		return resp, nil, upstreamProvider, nil
	case "cline":
		// 显式固定到 Cline 账号池：跳过自定义 provider 尝试，直接走账号池。
	default:
		if resp, perr, attempted := callCustomProviderAPI(ep, stream); attempted {
			if perr == nil {
				return resp, nil, upstreamProvider, nil
			}
			// provider 已逐个记录失败原因；继续用账号池兜底同一模型。
		}
	}

	tried := map[string]bool{}
	for {
		acc := pickAccountForModelStrict(model)
		if acc == nil {
			return nil, nil, upstreamCline, &freeModelUnavailableError{message: fmt.Sprintf("model %q has no available account", model)}
		}
		if tried[acc.AccountID] {
			// 防呆：账号轮询下也不应无限重试同一个账号（fill 策略等边界）。
			return nil, acc, upstreamCline, &freeModelUnavailableError{message: fmt.Sprintf("model %q unavailable on all eligible accounts", model)}
		}
		tried[acc.AccountID] = true

		resp, usedAcc, err := callClineAPIWithAccountCtx(context.Background(), acc, ep, stream)
		if err == nil {
			return resp, usedAcc, upstreamCline, nil
		}
		var accountErr *clineAccountUnavailableError
		if errors.As(err, &accountErr) {
			continue // token 失效等：换下一个账号
		}
		if apiErr, ok := err.(*clineAPIError); ok {
			switch apiErr.statusCode {
			case 402:
				// 余额不足是账号级问题：冷却该账号该模型后换号，仍试同一模型。
				setModelCooldown(usedAcc, model, time.Now().Add(30*time.Minute))
				log.Printf("  combo: model %q on %s: insufficient credits (402), cooling 30m, next account", model, truncateEmail(usedAcc.Email))
				continue
			case 429:
				continue // 模型级冷却已记录，换号
			}
		}
		return nil, usedAcc, upstreamCline, err
	}
}

// runCombo 按配置顺序执行组合，返回首个成功的上游响应；失败/跳过的条目记入轨迹。
func runCombo(params map[string]any, combo ModelCombo, stream bool, sessionID string) comboRun {
	trace := make([]AttemptLog, 0, len(combo.Entries))
	tried := make(map[string]bool, len(combo.Entries))
	failed := 0
	var lastErr error

	for i, entry := range combo.Entries {
		m := strings.TrimSpace(entry.Model)
		if m == "" {
			continue
		}
		order := i + 1
		if entry.Disabled {
			trace = append(trace, AttemptLog{Model: m, Order: order, Outcome: "skipped", Reason: "entry disabled by admin",
				Source: strings.TrimSpace(entry.Source), Provider: strings.TrimSpace(entry.Provider)})
			continue
		}
		key := comboEntryKey(entry)
		if tried[key] {
			trace = append(trace, AttemptLog{Model: m, Order: order, Outcome: "skipped", Reason: "duplicate entry",
				Source: strings.TrimSpace(entry.Source), Provider: strings.TrimSpace(entry.Provider)})
			continue
		}
		if isModelDisabled(m) {
			trace = append(trace, AttemptLog{Model: m, Order: order, Outcome: "skipped", Reason: "model disabled by admin",
				Source: strings.TrimSpace(entry.Source), Provider: strings.TrimSpace(entry.Provider)})
			continue
		}
		tried[key] = true

		start := time.Now()
		resp, acc, upstream, err := callSingleModel(params, entry, stream, sessionID)
		elapsed := time.Since(start).Milliseconds()
		if err == nil {
			params["model"] = m
			if upstream == upstreamProvider {
				params[servedByParam] = upstreamProvider
			} else {
				delete(params, servedByParam)
			}
			trace = append(trace, AttemptLog{
				Model: m, Order: order, Upstream: upstream, Outcome: "success", DurationMs: elapsed,
				Source: strings.TrimSpace(entry.Source), Provider: strings.TrimSpace(entry.Provider),
			})
			return comboRun{Resp: resp, Acc: acc, Upstream: upstream, Model: m, Trace: trace}
		}

		failed++
		lastErr = err
		trace = append(trace, AttemptLog{
			Model: m, Order: order, Upstream: upstream, Status: attemptStatus(err),
			Outcome: "failed", Reason: truncate(err.Error(), 200), DurationMs: elapsed,
			Source: strings.TrimSpace(entry.Source), Provider: strings.TrimSpace(entry.Provider),
		})
		log.Printf("  combo %q: entry %d %q failed (%v)", combo.ID, order, m, err)

		if !comboRetryable(err) {
			// 永久性错误（400/401/403 等）：继续换模型没有意义，直接失败。
			return comboRun{Trace: trace, Err: err}
		}
	}

	// 默认：组合只走自己的序列，不触发全局回退。
	if combo.EmergencyFallback {
		log.Printf("  combo %q: all entries failed, emergency fallback (opt-in) to free chain", combo.ID)
		fp := withModel(params, "free")
		if resp, acc, err := callFreeClineAPI(fp, stream); err == nil {
			fm, _ := fp["model"].(string)
			if fm == "" {
				fm = "free"
			}
			params["model"] = fm
			if v, ok := fp[servedByParam].(string); ok {
				params[servedByParam] = v
			} else {
				delete(params, servedByParam)
			}
			trace = append(trace, AttemptLog{
				Model: fm, Order: -1, Upstream: upstreamCline, Outcome: "success",
				Reason: "emergency fallback outside combo (opt-in)",
			})
			return comboRun{Resp: resp, Acc: acc, Upstream: upstreamCline, Model: fm, Trace: trace}
		} else {
			lastErr = err
			trace = append(trace, AttemptLog{
				Model: "free", Order: -1, Upstream: upstreamCline, Outcome: "failed",
				Reason: truncate(err.Error(), 200), Status: attemptStatus(err),
			})
		}
	}

	if lastErr == nil {
		lastErr = &freeModelUnavailableError{message: fmt.Sprintf("combo %q has no eligible entries", combo.ID)}
	}
	return comboRun{Trace: trace, Err: lastErr}
}

// applyComboLog 把组合运行轨迹写入请求日志（保持 Model 语义为实际服务模型）。
func applyComboLog(reqLog *RequestLog, combo ModelCombo, trace []AttemptLog, servedModel string) {
	reqLog.RequestedModel = combo.ID
	reqLog.ComboID = combo.ID
	reqLog.ComboName = combo.Name
	reqLog.Attempts = trace
	fallback := 0
	for _, a := range trace {
		if a.Outcome == "failed" || a.Outcome == "skipped" {
			fallback++
		}
	}
	reqLog.FallbackCount = fallback
	reqLog.FallbackUsed = fallback > 0
	if servedModel != "" {
		reqLog.Model = servedModel
	}
}

// applyComboTrace 把组合运行结果写入请求日志。
func applyComboTrace(reqLog *RequestLog, combo ModelCombo, run comboRun) {
	applyComboLog(reqLog, combo, run.Trace, run.Model)
}

// comboTraceSummary 生成简洁的失败说明（用于最终错误信息）。
func comboTraceSummary(combo ModelCombo, run comboRun) string {
	var attempted []string
	for _, a := range run.Trace {
		if a.Outcome == "success" {
			continue
		}
		attempted = append(attempted, fmt.Sprintf("%s(%s)", a.Model, a.Outcome))
	}
	sort.Strings(attempted)
	if len(attempted) == 0 {
		return fmt.Sprintf("model %q unavailable", combo.ID)
	}
	return fmt.Sprintf("model %q unavailable: %s", combo.ID, strings.Join(attempted, ", "))
}

// ============================================================================
// 管理 API
// ============================================================================

// combosCatalog 返回组合条目可引用的模型目录（供管理页选择器使用）。
func combosCatalog() []map[string]any {
	models := getAllModels()
	out := make([]map[string]any, 0, len(models))
	for _, m := range models {
		cost := m.Cost
		if isZenSource(m) && isZenFreeModel(m) {
			cost = "free" // 与路由判定保持一致
		}
		out = append(out, map[string]any{
			"id":       m.ID,
			"provider": m.Provider,
			"source":   m.Source,
			"cost":     cost,
			"disabled": m.Disabled,
			"delisted": m.Delisted,
		})
	}
	return out
}

// GET /admin/api/combos
func handleCombosList(w http.ResponseWriter, r *http.Request) {
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{
		"combos":          listCombos(),
		"modelCatalog":    combosCatalog(),
		"reservedAliases": []string{"free"},
	}})
}

// POST /admin/api/combos/save  body: ModelCombo
func handleComboSave(w http.ResponseWriter, r *http.Request) {
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
	var combo ModelCombo
	if err := json.Unmarshal(body, &combo); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: tAPI(r, "invalid_json")})
		return
	}
	// 保留既有条目的创建时间等信息
	if existing, ok := findCombo(combo.ID); ok && combo.CreatedAt.IsZero() {
		combo.CreatedAt = existing.CreatedAt
	}
	saved, err := upsertCombo(combo)
	if err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: localizeComboError(r, err)})
		return
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: tAPI(r, "combo_saved"), Data: map[string]any{"combo": saved}})
}

// POST /admin/api/combos/delete  body: {id}
func handleComboDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: tAPI(r, "method_not_allowed")})
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: tAPI(r, "invalid_json")})
		return
	}
	if !deleteCombo(req.ID) {
		writeAPI(w, http.StatusNotFound, apiResponse{Error: tAPI(r, "combo_not_found")})
		return
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: tAPI(r, "combo_deleted")})
}

// POST /admin/api/combos/toggle  body: {id, enabled}
func handleComboToggle(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: tAPI(r, "method_not_allowed")})
		return
	}
	var req struct {
		ID      string `json:"id"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: tAPI(r, "invalid_json")})
		return
	}
	combo, err := setComboEnabled(req.ID, req.Enabled)
	if err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"combo": combo}})
}

// POST /admin/api/combos/clone  body: {id}
func handleComboClone(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: tAPI(r, "method_not_allowed")})
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: tAPI(r, "invalid_json")})
		return
	}
	combo, err := cloneCombo(req.ID)
	if err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: localizeComboError(r, err)})
		return
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: tAPI(r, "combo_cloned"), Data: map[string]any{"combo": combo}})
}
