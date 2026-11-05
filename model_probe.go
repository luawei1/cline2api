package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ============ 免费模型可用性探测 ============
//
// 被动优先 + 主动补测：
//   - 真实流量的成功/失败经 finalizeRequestLog 自动记为探测信号（零成本）；
//   - 后台循环只对「最近没有信号」的免费模型做主动小请求探测。
//
// 探测结果驱动 /v1/models 的排序（可用在前、失败在后）与管理页徽标，
// 并让 getDefaultModel 优先选「最近确认可用」的模型（账号测试不再撞坏模型）。
// 状态仅存内存：重启后短暂「未知」，首轮循环（启动 +10s）自动重建。

const (
	// probeParam 标记本次 params 为探测请求：不外发（buildUpstreamBody 白名单
	// 滤除）、不触发账号 5 分钟冷却、不武装 zen 全局故障转移。
	probeParam = "__probe"

	// modelProbeInterval 主动探测轮询周期。
	modelProbeInterval = 15 * time.Minute
	// modelProbeStaleOK 最近一次信号为成功时，超过该时长才补测（省额度；
	// 热门模型被被动信号持续刷新，永远不需要主动测）。
	modelProbeStaleOK = 2 * time.Hour
	// modelProbeStaleFail 最近一次信号为失败时，超过该时长就补测（尽快发现恢复）。
	modelProbeStaleFail = 30 * time.Minute
	// modelProbeGap 主动探测相邻两个模型之间的间隔，防突发打爆上游。
	modelProbeGap = 300 * time.Millisecond
)

// modelProbeState 单个模型的探测状态（仅内存，不落盘）。
type modelProbeState struct {
	OK         bool      `json:"ok"`                  // 最近一次信号结果
	LastSignal time.Time `json:"lastSignal"`          // 最近信号时间（被动/主动都算）
	LastOK     time.Time `json:"lastOk,omitempty"`    // 最近成功时间
	LastFail   time.Time `json:"lastFail,omitempty"`  // 最近失败时间
	Error      string    `json:"error,omitempty"`     // 最近失败原因（截断）
	LatencyMs  int64     `json:"latencyMs,omitempty"` // 最近成功耗时
	Source     string    `json:"source,omitempty"`    // "passive" | "active"
}

var (
	modelProbeMu     sync.Mutex
	modelProbeStates = map[string]*modelProbeState{}
)

// isProbeRequest 判断 params 是否为探测请求（testModel 注入）。
func isProbeRequest(params map[string]any) bool {
	if params == nil {
		return false
	}
	b, _ := params[probeParam].(bool)
	return b
}

// recordProbeSignal 记录一次探测信号（被动流量与主动探测共用）。
func recordProbeSignal(model string, ok bool, latencyMs int64, errMsg, source string) {
	model = strings.TrimSpace(model)
	if model == "" {
		return
	}
	modelProbeMu.Lock()
	defer modelProbeMu.Unlock()
	st, found := modelProbeStates[model]
	if !found {
		st = &modelProbeState{}
		modelProbeStates[model] = st
	}
	now := time.Now()
	st.OK = ok
	st.LastSignal = now
	st.Source = source
	if ok {
		st.LastOK = now
		st.LatencyMs = latencyMs
		st.Error = ""
	} else {
		st.LastFail = now
		st.Error = truncate(errMsg, 200)
	}
}

// getProbeState 返回模型探测状态的拷贝；无记录时 found=false。
func getProbeState(model string) (modelProbeState, bool) {
	modelProbeMu.Lock()
	defer modelProbeMu.Unlock()
	st, ok := modelProbeStates[model]
	if !ok {
		return modelProbeState{}, false
	}
	return *st, true
}

// probeStatePtr 返回模型探测状态指针（nil=未知），供管理页 API 序列化。
func probeStatePtr(model string) *modelProbeState {
	modelProbeMu.Lock()
	defer modelProbeMu.Unlock()
	st, ok := modelProbeStates[model]
	if !ok {
		return nil
	}
	cp := *st
	return &cp
}

// probeTier 排序等级：最近信号成功=0 → 无信号=1 → 最近信号失败=2。
// 供 /v1/models 稳定排序使用；无任何探测数据时全部为 1，顺序与原池序一致。
func probeTier(model string) int {
	modelProbeMu.Lock()
	defer modelProbeMu.Unlock()
	st, ok := modelProbeStates[model]
	if !ok {
		return 1
	}
	if st.OK {
		return 0
	}
	return 2
}

// needsActiveProbe 判断是否到了主动补测时间：
//   - 从未有信号 → 立即测（首轮建立基线）
//   - 最近信号为失败 → 超过 modelProbeStaleFail 才测（30min，尽快发现恢复）
//   - 最近信号为成功 → 超过 modelProbeStaleOK 才测（2h，省额度）
func needsActiveProbe(st *modelProbeState) bool {
	if st == nil {
		return true
	}
	stale := modelProbeStaleOK
	if !st.OK {
		stale = modelProbeStaleFail
	}
	return time.Since(st.LastSignal) >= stale
}

// passiveErrorAttributable 判断失败是否可归因于模型/上游。
// 客户端断连、取消、本地解码失败、鉴权问题不算模型坏，不记 fail 信号。
func passiveErrorAttributable(errMsg string) bool {
	low := strings.ToLower(errMsg)
	for _, skip := range []string{
		"client disconnected",
		"context canceled",
		"canceled",
		"decode response",
		"invalid api key",
		"auth_error",
	} {
		if strings.Contains(low, skip) {
			return false
		}
	}
	return true
}

// recordPassiveProbe 由 finalizeRequestLog 调用：把真实流量结果记为探测信号。
func recordPassiveProbe(model string, completed bool, errMsg string, durationMs int64) {
	if completed && errMsg == "" {
		recordProbeSignal(model, true, durationMs, "", "passive")
		return
	}
	if !completed {
		if errMsg == "" || !passiveErrorAttributable(errMsg) {
			return
		}
		recordProbeSignal(model, false, 0, errMsg, "passive")
	}
}

// modelProbeResult 单次主动探测结果。
type modelProbeResult struct {
	Model     string
	OK        bool
	LatencyMs int64
	Error     string
}

// probeParams 构造探测用最小请求（max_tokens=1024：推理模型小预算必败，issue #32）。
func probeParams(model string) map[string]any {
	return map[string]any{
		"model":      model,
		"max_tokens": 1024,
		"stream":     false,
		"messages": []any{
			map[string]any{"role": "user", "content": "hi"},
		},
		probeParam: true,
	}
}

// probeSuccess 判定探测响应是否成功：HTTP 层无错 + 200 + body 可解码 + 无顶层 error。
func probeSuccess(resp *http.Response, err error) (bool, string, int64) {
	if err != nil {
		return false, truncate(err.Error(), 200), 0
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return false, "read response: " + truncate(err.Error(), 200), 0
	}
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Sprintf("API %d: %s", resp.StatusCode, truncate(string(raw), 200)), 0
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return false, "decode response: " + truncate(err.Error(), 200), 0
	}
	if e, ok := obj["error"]; ok && e != nil {
		return false, truncate(fmt.Sprintf("%v", e), 200), 0
	}
	return true, "", 0
}

// testModel 对单个模型发最小探测请求，按模型来源路由：
//   - zen（opencode）→ callZenAPI（内部匿名伪装 + SSE 折叠）
//   - 自定义 provider → callCustomProviderAPI
//   - 其余（Cline 远程/内置）→ 账号池最久未用账号，账号不可用逐个换
//
// 不写请求日志、不记 token 统计（testAccount 模式）；params 带 probeParam，
// 使上游调用跳过账号冷却与 zen 故障转移计数。
func testModel(m Model) modelProbeResult {
	started := time.Now()
	params := probeParams(m.ID)

	// zen 模型：zen 未启用时不测（测了必败还浪费额度）
	if isZenSource(m) {
		if !getZenConfig().Enabled {
			return modelProbeResult{Model: m.ID, OK: false, Error: "zen disabled", LatencyMs: 0}
		}
		resp, err := callZenAPI(params, false)
		ok, errMsg, _ := probeSuccess(resp, err)
		return modelProbeResult{Model: m.ID, OK: ok, Error: errMsg, LatencyMs: time.Since(started).Milliseconds()}
	}

	// 自定义 provider 模型
	if m.Source == "provider" {
		resp, err, attempted := callCustomProviderAPI(params, false)
		if !attempted {
			return modelProbeResult{Model: m.ID, OK: false, Error: "provider not attempted"}
		}
		ok, errMsg, _ := probeSuccess(resp, err)
		return modelProbeResult{Model: m.ID, OK: ok, Error: errMsg, LatencyMs: time.Since(started).Milliseconds()}
	}

	// Cline 账号池：照 callFreeClineAPI 的账号循环，全不可用记 fail。
	// 迭代上限 = 账号数：token 失败的账号会被 refreshAccountToken 置冷却/过期
	// 而被 pick 跳过，但上限兜底防止任何意外情况下的死循环。
	var lastErr string
	maxAttempts := len(loadPool().Accounts) + 1
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	for attempt := 0; attempt < maxAttempts; attempt++ {
		acc := pickAccountForModelLeastUsed(m.ID)
		if acc == nil {
			if lastErr == "" {
				lastErr = "no eligible account"
			}
			break
		}
		resp, _, err := callClineAPIWithAccount(acc, params, false)
		if err == nil {
			ok, errMsg, _ := probeSuccess(resp, nil)
			return modelProbeResult{Model: m.ID, OK: ok, Error: errMsg, LatencyMs: time.Since(started).Milliseconds()}
		}
		var accountErr *clineAccountUnavailableError
		if errors.As(err, &accountErr) {
			lastErr = truncate(err.Error(), 200)
			// 网络级失败（上游请求本身发不出去）换账号也没用，直接判模型不可达；
			// token 级失败（refresh 被拒等）才换下一个账号
			if strings.Contains(lastErr, "upstream request") {
				break
			}
			continue
		}
		lastErr = truncate(err.Error(), 200)
		break // API 错误（429/400/500...）：模型级失败，不换账号重试
	}
	return modelProbeResult{Model: m.ID, OK: false, Error: lastErr, LatencyMs: time.Since(started).Milliseconds()}
}

// runModelProbeCycle 执行一轮主动补测：挑出免费、未停用、信号过期的模型逐个测。
func runModelProbeCycle() {
	all := getAllModels()
	zenEnabled := getZenConfig().Enabled

	type target struct {
		m Model
	}
	var targets []target
	for _, m := range all {
		if m.Disabled || !isFreeModelEntry(m) {
			continue // 只主动测免费模型（测付费烧真钱）
		}
		if isZenSource(m) && !zenEnabled {
			continue
		}
		st, found := getProbeState(m.ID)
		var stPtr *modelProbeState
		if found {
			cp := st
			stPtr = &cp
		}
		if needsActiveProbe(stPtr) {
			targets = append(targets, target{m: m})
		}
	}
	if len(targets) == 0 {
		return
	}

	var okCount, failCount int
	var failures []string
	for i, t := range targets {
		if i > 0 {
			time.Sleep(modelProbeGap)
		}
		res := testModel(t.m)
		recordProbeSignal(res.Model, res.OK, res.LatencyMs, res.Error, "active")
		if res.OK {
			okCount++
			log.Printf("  model probe: %s ok (%dms)", res.Model, res.LatencyMs)
		} else {
			failCount++
			failures = append(failures, res.Model+": "+res.Error)
			log.Printf("  model probe: %s FAIL: %s", res.Model, res.Error)
		}
	}
	summary := fmt.Sprintf("model probe: %d tested, %d ok, %d failed", len(targets), okCount, failCount)
	if len(failures) > 0 {
		summary += " (" + truncate(strings.Join(failures, "; "), 300) + ")"
	}
	log.Print(summary)
}

// runModelProbeLoop 按 interval 周期执行探测；stop 关闭后返回（测试注入短间隔用）。
func runModelProbeLoop(stop <-chan struct{}, interval time.Duration, cycle func()) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			cycle()
		}
	}
}

// startModelProbeLoop 启动后台探测：启动 10s 后先跑一轮（错峰模型同步），
// 之后按 modelProbeInterval 周期补测信号过期的模型。
func startModelProbeLoop() {
	go func() {
		time.Sleep(10 * time.Second)
		runModelProbeCycle()
		runModelProbeLoop(nil, modelProbeInterval, runModelProbeCycle)
	}()
}
