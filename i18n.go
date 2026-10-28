package main

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

const adminLangCookie = "cline_admin_lang"

type locale string

const (
	localeZH locale = "zh"
	localeEN locale = "en"
)

var apiMessages = map[string]map[locale]string{
	"login_required": {
		localeZH: "需要登录",
		localeEN: "Login required",
	},
	"session_expired": {
		localeZH: "登录已过期，请重新登录",
		localeEN: "Session expired, please sign in again",
	},
	"password_not_enabled": {
		localeZH: "后台未启用密码",
		localeEN: "Admin password is not enabled",
	},
	"wrong_password": {
		localeZH: "密码错误",
		localeEN: "Incorrect password",
	},
	"login_ok": {
		localeZH: "登录成功",
		localeEN: "Signed in",
	},
	"logout_ok": {
		localeZH: "已退出登录",
		localeEN: "Signed out",
	},
	"password_cleared": {
		localeZH: "已清除后台密码",
		localeEN: "Admin password cleared",
	},
	"password_updated": {
		localeZH: "后台密码已更新",
		localeEN: "Admin password updated",
	},
	"method_not_allowed": {
		localeZH: "方法不允许",
		localeEN: "method not allowed",
	},
	"invalid_json": {
		localeZH: "JSON 无效",
		localeEN: "invalid JSON",
	},
	"refresh_token_required": {
		localeZH: "必须提供 refreshToken",
		localeEN: "refreshToken is required",
	},
	"invalid_refresh_token": {
		localeZH: "refreshToken 无效：%s",
		localeEN: "invalid refreshToken: %s",
	},
	"account_added": {
		localeZH: "账号 %s 已添加",
		localeEN: "Account %s added",
	},
	"account_id_required": {
		localeZH: "必须提供 accountId",
		localeEN: "accountId is required",
	},
	"account_deleted": {
		localeZH: "账号已删除",
		localeEN: "Account deleted",
	},
	"account_not_found": {
		localeZH: "账号不存在",
		localeEN: "Account not found",
	},
	"session_id_required": {
		localeZH: "必须提供 sessionId",
		localeEN: "sessionId required",
	},
	"session_not_found": {
		localeZH: "会话不存在",
		localeEN: "session not found",
	},
	"sso_cookies_required": {
		localeZH: "必须提供 ssoCookies",
		localeEN: "ssoCookies is required",
	},
	"tokens_empty": {
		localeZH: "tokens 数组为空",
		localeEN: "tokens array is empty",
	},
	"imported_accounts": {
		localeZH: "已导入 %d 个账号，失败 %d 个，跳过重复 %d 个",
		localeEN: "Imported %d accounts, %d failed, %d duplicates skipped",
	},
	"account_exists": {
		localeZH: "账号 %s 已存在，跳过重复添加",
		localeEN: "Account %s already exists, skipped duplicate",
	},
	"url_required": {
		localeZH: "必须提供 url",
		localeEN: "url required",
	},
	"url_http_only": {
		localeZH: "仅允许 http/https 链接",
		localeEN: "only http/https URLs allowed",
	},
	"tokens_refreshed": {
		localeZH: "全部 Token 已刷新",
		localeEN: "All tokens refreshed",
	},
	"accounts_deleted": {
		localeZH: "全部账号已删除",
		localeEN: "All accounts deleted",
	},
	"reset_failed": {
		localeZH: "重置失败：%s",
		localeEN: "reset failed: %s",
	},
	"account_reset": {
		localeZH: "账号已重置",
		localeEN: "Account reset",
	},
	"key_deleted": {
		localeZH: "密钥已删除",
		localeEN: "Key deleted",
	},
	"invalid_strategy": {
		localeZH: "无效的轮询策略，可选：round_robin、fill、random",
		localeEN: "invalid strategy, must be: round_robin, fill, random",
	},
	"invalid_default_model": {
		localeZH: "无效的默认模型，不在可用模型列表中",
		localeEN: "invalid default model, not in available models list",
	},
	"invalid_host": {
		localeZH: "无效的监听地址，必须是 127.0.0.1、0.0.0.0 或本机 IP",
		localeEN: "invalid host, must be 127.0.0.1, 0.0.0.0 or a local IP",
	},
	"model_id_required": {
		localeZH: "必须提供模型 ID",
		localeEN: "model id is required",
	},
	"model_exists": {
		localeZH: "模型已存在",
		localeEN: "model already exists",
	},
	"model_added": {
		localeZH: "模型已添加",
		localeEN: "model added",
	},
	"cannot_delete_builtin": {
		localeZH: "不能删除内置模型",
		localeEN: "cannot delete builtin model",
	},
	"model_not_found": {
		localeZH: "模型不存在",
		localeEN: "model not found",
	},
	"model_deleted": {
		localeZH: "模型已删除",
		localeEN: "model deleted",
	},
	"model_disabled": {
		localeZH: "模型已停用",
		localeEN: "model is disabled",
	},
	"model_status_saved": {
		localeZH: "模型状态已更新",
		localeEN: "model status updated",
	},
	"invalid_limit": {
		localeZH: "无效的 limit",
		localeEN: "invalid limit",
	},
	"model_sync_done": {
		localeZH: "模型已同步",
		localeEN: "Models synced",
	},
	"model_sync_failed": {
		localeZH: "模型同步失败",
		localeEN: "Model sync failed",
	},
	// opencode zen 相关
	"opencode_config_saved": {
		localeZH: "opencode 配置已保存",
		localeEN: "OpenCode config saved",
	},
	// Cline 出口代理相关
	"cline_proxy_saved": {
		localeZH: "Cline 代理配置已保存",
		localeEN: "Cline proxy config saved",
	},
	"invalid_base_url": {
		localeZH: "无效的 Base URL，必须以 http:// 或 https:// 开头",
		localeEN: "invalid base URL, must start with http:// or https://",
	},
	"invalid_proxy_strategy": {
		localeZH: "无效的代理策略，可选：round_robin、random、fill",
		localeEN: "invalid proxy strategy, must be: round_robin, random, fill",
	},
	"invalid_concurrency": {
		localeZH: "最大并发必须在 1-64 之间",
		localeEN: "max concurrency must be between 1 and 64",
	},
	"invalid_retries": {
		localeZH: "重试次数必须在 0-10 之间",
		localeEN: "retries must be between 0 and 10",
	},
	"invalid_failover": {
		localeZH: "故障转移参数超出允许范围",
		localeEN: "failover parameters out of range",
	},
	"invalid_compaction": {
		localeZH: "压缩参数不能为负数",
		localeEN: "compaction values cannot be negative",
	},
	// 模型组合（Named Model Combos）相关
	"combo_saved": {
		localeZH: "模型组合已保存",
		localeEN: "Model combo saved",
	},
	"combo_deleted": {
		localeZH: "模型组合已删除",
		localeEN: "Model combo deleted",
	},
	"combo_cloned": {
		localeZH: "模型组合已复制",
		localeEN: "Model combo cloned",
	},
	"combo_not_found": {
		localeZH: "模型组合不存在",
		localeEN: "Model combo not found",
	},
	// 组合条目的上游固定（Source/Provider）校验
	"combo_entry_bad_source": {
		localeZH: "组合条目的上游固定值无效（可选：cline、zen、provider）",
		localeEN: "invalid combo entry source (allowed: cline, zen, provider)",
	},
	"combo_entry_provider_required": {
		localeZH: "固定到 provider 时必须填写 Provider 名称",
		localeEN: "provider name is required when source=provider",
	},
	"combo_entry_provider_unknown": {
		localeZH: "没有启用的 Provider %q 提供模型 %q",
		localeEN: "no enabled provider %q serves model %q",
	},
	"combo_entry_zen_not_free": {
		localeZH: "模型 %q 不是可用的免费 zen 模型，无法固定到 zen",
		localeEN: "model %q is not an available free zen model; cannot pin source=zen",
	},
	"combo_entry_cline_unknown": {
		localeZH: "模型 %q 不在 Cline 账号池中（非 zen 来源），无法固定到 cline",
		localeEN: "model %q is not a Cline account-pool model (non-zen); cannot pin source=cline",
	},
}

func requestLocale(r *http.Request) locale {
	if r == nil {
		return localeZH
	}
	if c, err := r.Cookie(adminLangCookie); err == nil {
		if loc := parseLangTag(c.Value); loc != "" {
			return loc
		}
	}
	return parseAcceptLanguage(r.Header.Get("Accept-Language"))
}

func parseLangTag(tag string) locale {
	tag = strings.ToLower(strings.TrimSpace(tag))
	if tag == "" {
		return ""
	}
	if i := strings.IndexAny(tag, "-_"); i >= 0 {
		tag = tag[:i]
	}
	switch tag {
	case "en":
		return localeEN
	case "zh":
		return localeZH
	default:
		return ""
	}
}

func parseAcceptLanguage(header string) locale {
	header = strings.TrimSpace(header)
	if header == "" {
		return localeZH
	}
	var zhQ, enQ float64
	var hasZH, hasEN bool
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		lang := part
		q := 1.0
		if i := strings.Index(part, ";"); i >= 0 {
			lang = strings.TrimSpace(part[:i])
			params := part[i+1:]
			for _, p := range strings.Split(params, ";") {
				p = strings.TrimSpace(strings.ToLower(p))
				if strings.HasPrefix(p, "q=") {
					if v, err := strconv.ParseFloat(strings.TrimSpace(p[2:]), 64); err == nil {
						q = v
					}
				}
			}
		}
		lang = strings.ToLower(strings.TrimSpace(lang))
		switch {
		case lang == "zh" || strings.HasPrefix(lang, "zh-"):
			if !hasZH || q > zhQ {
				zhQ, hasZH = q, true
			}
		case lang == "en" || strings.HasPrefix(lang, "en-"):
			if !hasEN || q > enQ {
				enQ, hasEN = q, true
			}
		}
	}
	switch {
	case hasEN && (!hasZH || enQ > zhQ):
		return localeEN
	case hasZH:
		return localeZH
	default:
		return localeZH
	}
}

func tAPI(r *http.Request, key string, args ...any) string {
	loc := requestLocale(r)
	entry, ok := apiMessages[key]
	if !ok {
		return key
	}
	msg := entry[loc]
	if msg == "" {
		msg = entry[localeZH]
	}
	if len(args) > 0 {
		return fmt.Sprintf(msg, args...)
	}
	return msg
}
