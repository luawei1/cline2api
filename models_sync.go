package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// clineRecommendedModelsURL 是 Cline 官方的「推荐/免费模型」接口（无需认证）。
// 参考 model-api.md：Cline 4.1.15 的 Free Models 由该接口直接返回。
const clineRecommendedModelsURL = "https://api.cline.bot/api/v1/ai/cline/recommended-models"

const modelSyncTimeout = 10 * time.Second

// clineRemoteModel 对应接口返回的单个模型字段。
type clineRemoteModel struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Tags        []string `json:"tags"`
	ContextWin  int      `json:"contextWindow"`
	MaxInput    int      `json:"maxInputTokens"`
	MaxTokens   int      `json:"maxTokens"`
	ReleaseDate string   `json:"releaseDate"`
	Family      string   `json:"family"`
}

// clineRecommendedResponse 对应接口返回结构：recommended / free / clinePass 三个数组。
type clineRecommendedResponse struct {
	Recommended []clineRemoteModel `json:"recommended"`
	Free        []clineRemoteModel `json:"free"`
	ClinePass   []clineRemoteModel `json:"clinePass"`
}

// modelSyncResult 是一次模型同步的结果（供管理后台弹窗展示）。
type modelSyncResult struct {
	Changed  bool     `json:"changed"`
	Added    []string `json:"added"`
	Removed  []string `json:"removed"`
	SyncedAt string   `json:"syncedAt"`
	Total    int      `json:"total"`
	Error    string   `json:"error,omitempty"`
}

var (
	modelSyncMu    sync.Mutex
	lastModelSync  modelSyncResult
	modelSyncRan   bool // 启动后是否已同步过（避免重复）
	modelSyncBusy  bool // 同步进行中（防并发触发）
)

// remoteModelsEnabled 远程同步成功后置 true：此后 getAllModels 以远程模型为主，
// 内置硬编码模型（已失效）仅作为离线 fallback。
var (
	remoteModelsEnabled bool
	remoteModelsEnabledMu sync.Mutex
)

// fetchClineRecommendedModels 拉取并解析 Cline 官方推荐模型接口。
func fetchClineRecommendedModels() (clineRecommendedResponse, error) {
	// 复用全局 transport：模型同步与 Cline 对话同源（api.cline.bot），共用出口代理
	client := &http.Client{Timeout: modelSyncTimeout, Transport: httpTransport}
	resp, err := client.Get(clineRecommendedModelsURL)
	if err != nil {
		return clineRecommendedResponse{}, fmt.Errorf("fetch models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return clineRecommendedResponse{}, fmt.Errorf("models API returned status %d", resp.StatusCode)
	}

	var data clineRecommendedResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return clineRecommendedResponse{}, fmt.Errorf("decode models: %w", err)
	}
	return data, nil
}

// modelMeta 是已知模型的上下文窗口 / 最大输出 token 元数据。
type modelMeta struct {
	Context int
	Output  int
}

// clineModelMeta 是已知 Cline 侧模型的硬限制表（按模型基名匹配）。
// Cline 官方 recommended-models 接口不返回 contextWindow/maxTokens 元数据，
// 只能按实测维护；上游网关按路由（vertex/google）强制校验 maxOutputTokens。
// gemini-3.8-flash：vertex 路由实测支持范围 1~65536 —— maxOutputTokens=128000
// 会被直接 400 invalid argument（2026-09 实测，含 google 路由的同源报错）。
var clineModelMeta = map[string]modelMeta{
	"gemini-3.8-flash": {Context: 1048576, Output: 65536},
}

// clineModelPrefixes 是 Cline 侧模型 ID 的路由前缀（基名之前的限定段）。
var clineModelPrefixes = []string{"cline-free/", "cline-pass/", "google/", "cline/"}

// modelBaseName 去掉模型 ID 的路由前缀得到基名：
// cline-free/gemini-3.8-flash / google/gemini-3.8-flash → gemini-3.8-flash。
func modelBaseName(id string) string {
	id = strings.TrimSpace(id)
	if idx := strings.Index(id, "/"); idx > 0 {
		base := id[idx+1:]
		for _, p := range clineModelPrefixes {
			if strings.HasPrefix(id, p) {
				return base
			}
		}
	}
	return id
}

// lookupClineModelMeta 按基名查已知模型元数据；未收录时 ok=false。
func lookupClineModelMeta(id string) (modelMeta, bool) {
	meta, ok := clineModelMeta[modelBaseName(id)]
	return meta, ok
}

// remoteCost 判断远程模型计费：tags 含 "FREE" 或来自 free 数组 → free，否则 pass。
func remoteCost(m clineRemoteModel, inFreeList bool) string {
	if inFreeList {
		return "free"
	}
	for _, t := range m.Tags {
		if strings.EqualFold(t, "FREE") {
			return "free"
		}
	}
	return "pass"
}

// remoteProvider 从模型 ID 前缀推断 provider，无前缀时归为 "cline"。
func remoteProvider(id string) string {
	if idx := strings.Index(id, "/"); idx > 0 {
		return id[:idx]
	}
	return "cline"
}

// syncClineModels 执行一次模型同步并持久化：
//  1. 拉取远程推荐模型（free / clinePass / recommended）
//  2. 与池中现有 remote 模型比较，得到 added / removed
//  3. 更新 AccountPool.Models（替换 Source=remote 的旧条目），保存
//  4. 记录 lastModelSync 供管理后台弹窗
//
// 任何一步失败都会把错误写进 lastModelSync，不阻塞服务启动。
func syncClineModels() modelSyncResult {
	modelSyncMu.Lock()
	if modelSyncBusy {
		modelSyncMu.Unlock()
		return lastModelSync
	}
	modelSyncBusy = true
	modelSyncMu.Unlock()
	defer func() { modelSyncMu.Lock(); modelSyncBusy = false; modelSyncMu.Unlock() }()

	res := modelSyncResult{SyncedAt: time.Now().Format(time.RFC3339)}
	fail := func(err error) modelSyncResult {
		log.Printf("models sync failed: %v", err)
		res.Error = err.Error()
		modelSyncMu.Lock()
		lastModelSync = res
		modelSyncMu.Unlock()
		return res
	}

	data, err := fetchClineRecommendedModels()
	if err != nil {
		return fail(err)
	}

	// 组装远程模型列表（去重，free 数组在前）。
	// 先取池中旧 remote 条目：管理页锁定过 Context/Output（MetaLocked）的保留原值。
	p := loadPool()
	poolMu.Lock()
	oldRemote := make(map[string]Model)
	for _, m := range p.Models {
		if m.Source == "remote" {
			oldRemote[m.ID] = m
		}
	}
	poolMu.Unlock()

	var remote []Model
	seen := make(map[string]bool)
	addGroup := func(group []clineRemoteModel, inFree bool) {
		for _, m := range group {
			if m.ID == "" || seen[m.ID] {
				continue
			}
			seen[m.ID] = true
			entry := Model{
				ID:       m.ID,
				Provider: remoteProvider(m.ID),
				Cost:     remoteCost(m, inFree),
				Status:   "active",
				Custom:   false,
				Source:   "remote",
			}
			// 远程接口不带 context/maxTokens：按已知硬限制表补全，
			// 用户锁定过的值（MetaLocked）优先于表。
			if old, ok := oldRemote[m.ID]; ok && old.MetaLocked {
				entry.Context, entry.Output, entry.MetaLocked = old.Context, old.Output, true
			} else if meta, ok := lookupClineModelMeta(m.ID); ok {
				entry.Context, entry.Output = meta.Context, meta.Output
			}
			remote = append(remote, entry)
		}
	}
	addGroup(data.Free, true)
	addGroup(data.ClinePass, false)
	addGroup(data.Recommended, false)

	if len(remote) == 0 {
		return fail(fmt.Errorf("models API returned empty list"))
	}

	// 与池中现有 remote 模型比较
	p = loadPool()
	poolMu.Lock()
	oldIDs := make(map[string]bool)
	var kept []Model
	for _, m := range p.Models {
		if m.Source == "remote" {
			oldIDs[m.ID] = true
			continue
		}
		kept = append(kept, m)
	}
	for _, m := range remote {
		if !oldIDs[m.ID] {
			res.Added = append(res.Added, m.ID)
		}
	}
	for id := range oldIDs {
		if !seen[id] {
			res.Removed = append(res.Removed, id)
		}
	}
	kept = append(kept, remote...)
	p.Models = kept
	res.Total = len(remote)
	res.Changed = len(res.Added) > 0 || len(res.Removed) > 0
	poolMu.Unlock()
	savePool()

	remoteModelsEnabledMu.Lock()
	remoteModelsEnabled = true
	remoteModelsEnabledMu.Unlock()

	modelSyncMu.Lock()
	lastModelSync = res
	modelSyncRan = true
	modelSyncMu.Unlock()

	log.Printf("models sync: %d models, +%d added, -%d removed",
		res.Total, len(res.Added), len(res.Removed))
	return res
}

// triggerModelSync 供管理后台手动触发同步；非阻塞等待完成并返回结果。
func triggerModelSync() modelSyncResult {
	return syncClineModels()
}

// getModelSyncResult 返回最近一次同步结果（供管理后台展示）。
func getModelSyncResult() modelSyncResult {
	modelSyncMu.Lock()
	defer modelSyncMu.Unlock()
	if !modelSyncRan {
		return modelSyncResult{SyncedAt: ""}
	}
	return lastModelSync
}

// startModelSync 在服务启动时异步同步一次（不阻塞启动）。
func startModelSync() {
	go func() {
		if !modelSyncRan {
			syncClineModels()
		}
	}()
}

// remoteModelsActive 返回远程模型是否已启用（同步成功过）。
func remoteModelsActive() bool {
	remoteModelsEnabledMu.Lock()
	defer remoteModelsEnabledMu.Unlock()
	return remoteModelsEnabled
}

// POST /admin/api/models/sync — 手动触发一次模型同步
func handleAdminModelSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: tAPI(r, "method_not_allowed")})
		return
	}
	res := triggerModelSync()
	if res.Error != "" {
		writeAPI(w, http.StatusBadGateway, apiResponse{Success: false, Error: res.Error, Message: tAPI(r, "model_sync_failed")})
		return
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: res, Message: tAPI(r, "model_sync_done")})
}
