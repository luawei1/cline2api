package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// ============================================================================
// OpenAI Responses API (/v1/responses) ↔ chat/completions 双向转换
// 所有上游生效：zen 免费模型与 Cline 账号池均可通过该端点访问（Cursor 等客户端直连）。
// ============================================================================

// responsesToChat 将 Responses 请求体转换为 chat.completions 请求体。
func responsesToChat(body map[string]any) map[string]any {
	out := map[string]any{}
	if m, ok := body["model"].(string); ok {
		out["model"] = m
	}
	if s, ok := body["stream"].(bool); ok {
		out["stream"] = s
	}
	if mt, ok := body["max_output_tokens"].(float64); ok {
		out["max_tokens"] = int(mt)
	}
	for _, k := range []string{"temperature", "top_p", "stop", "seed", "user", "metadata", "logit_bias"} {
		if v, ok := body[k]; ok {
			out[k] = v
		}
	}
	msgs := responsesInputToMessages(body["input"])
	if instr, ok := body["instructions"].(string); ok && instr != "" {
		msgs = append([]any{map[string]any{"role": "system", "content": instr}}, msgs...)
	}
	out["messages"] = msgs
	if tools, ok := body["tools"].([]any); ok {
		out["tools"] = responsesToolsToChat(tools)
	}
	if tc, ok := body["tool_choice"]; ok {
		out["tool_choice"] = tc
	}
	return out
}

// responsesInputToMessages 处理 string / item 数组两种 input 形态。
func responsesInputToMessages(input any) []any {
	var msgs []any
	switch v := input.(type) {
	case string:
		msgs = append(msgs, map[string]any{"role": "user", "content": v})
	case []any:
		for _, item := range v {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			switch m["type"] {
			case "message":
				role, _ := m["role"].(string)
				if role == "" {
					role = "user"
				}
				msgs = append(msgs, map[string]any{"role": role, "content": stringifyResponsesContent(m["content"])})
			case "function_call":
				callID, _ := m["call_id"].(string)
				if callID == "" {
					callID, _ = m["id"].(string)
				}
				name, _ := m["name"].(string)
				args := ""
				switch a := m["arguments"].(type) {
				case string:
					args = a
				case map[string]any:
					if b, err := json.Marshal(a); err == nil {
						args = string(b)
					}
				}
				msgs = append(msgs, map[string]any{
					"role":    "assistant",
					"content": "",
					"tool_calls": []any{map[string]any{
						"id":       callID,
						"type":     "function",
						"function": map[string]any{"name": name, "arguments": args},
					}},
				})
			case "function_call_output":
				callID, _ := m["call_id"].(string)
				if callID == "" {
					callID, _ = m["id"].(string)
				}
				output := ""
				switch o := m["output"].(type) {
				case string:
					output = o
				case map[string]any:
					if b, err := json.Marshal(o); err == nil {
						output = string(b)
					}
				}
				msgs = append(msgs, map[string]any{"role": "tool", "content": output, "tool_call_id": callID})
			case "reasoning":
				// reasoning 输入项无法映射到 chat 输入，忽略
			}
		}
	}
	return msgs
}

func stringifyResponsesContent(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		parts := []string{}
		for _, block := range v {
			if b, ok := block.(map[string]any); ok {
				if t, ok := b["text"].(string); ok {
					parts = append(parts, t)
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

// responsesToolsToChat 扁平 function 工具 → OpenAI 嵌套格式。
func responsesToolsToChat(tools []any) []any {
	out := make([]any, 0, len(tools))
	for _, t := range tools {
		tm, ok := t.(map[string]any)
		if !ok || tm["type"] != "function" {
			continue
		}
		fn := map[string]any{}
		if n, ok := tm["name"].(string); ok {
			fn["name"] = n
		}
		if d, ok := tm["description"].(string); ok {
			fn["description"] = d
		}
		if p, ok := tm["parameters"].(map[string]any); ok {
			fn["parameters"] = p
		}
		out = append(out, map[string]any{"type": "function", "function": fn})
	}
	return out
}

// ============ 非流式响应转换 ============

func newResponseID(prefix string) string {
	return prefix + fmt.Sprintf("%x", time.Now().UnixNano())
}

// chatToResponses 将 chat.completions 响应转换为 Responses 响应。
func chatToResponses(chat map[string]any) map[string]any {
	resp := map[string]any{
		"id":          newResponseID("resp_"),
		"object":      "response",
		"created_at":  time.Now().Unix(),
		"status":      "completed",
		"model":       chat["model"],
		"output":      []any{},
		"output_text": "",
	}
	var outputs []any
	var outputText strings.Builder

	choices, _ := chat["choices"].([]any)
	if len(choices) > 0 {
		if ch, ok := choices[0].(map[string]any); ok {
			msg, _ := ch["message"].(map[string]any)
			if msg == nil {
				msg, _ = ch["delta"].(map[string]any)
			}
			content := []any{}
			var text string
			if msg != nil {
				text, _ = msg["content"].(string)
			}
			if text != "" {
				outputText.WriteString(text)
				content = append(content, map[string]any{"type": "output_text", "text": text, "annotations": []any{}})
			}
			outputs = append(outputs, map[string]any{
				"type":        "message",
				"id":          newResponseID("msg_"),
				"status":      "completed",
				"role":        "assistant",
				"content":     content,
				"output_text": outputText.String(),
			})
			if msg != nil {
				if tc, ok := msg["tool_calls"].([]any); ok {
					for _, c := range tc {
						cm, ok := c.(map[string]any)
						if !ok {
							continue
						}
						fn, _ := cm["function"].(map[string]any)
						// 跳过空名 tool_call（畸形工具调用），客户端无法执行
						var name, args string
						if fn != nil {
							name, _ = fn["name"].(string)
							args, _ = fn["arguments"].(string)
						}
						if name == "" {
							continue
						}
						callID, _ := cm["id"].(string)
						if callID == "" {
							callID = newResponseID("fc_")
						}
						outputs = append(outputs, map[string]any{
							"type":      "function_call",
							"id":        newResponseID("fc_"),
							"call_id":   callID,
							"name":      name,
							"arguments": args,
							"status":    "completed",
						})
					}
				}
			}
		}
	}
	resp["output"] = outputs
	resp["output_text"] = outputText.String()

	if u, ok := chat["usage"].(map[string]any); ok {
		details := map[string]any{}
		if pd, ok := u["prompt_tokens_details"].(map[string]any); ok {
			details["cached_tokens"] = pd["cached_tokens"]
		}
		od := map[string]any{}
		if rd, ok := u["completion_tokens_details"].(map[string]any); ok {
			od["reasoning_tokens"] = rd["reasoning_tokens"]
		} else if rd, ok := u["reasoning_tokens"]; ok {
			od["reasoning_tokens"] = rd
		}
		resp["usage"] = map[string]any{
			"input_tokens":          u["prompt_tokens"],
			"input_tokens_details":  details,
			"output_tokens":         u["completion_tokens"],
			"output_tokens_details": od,
			"total_tokens":          u["total_tokens"],
		}
	}
	return resp
}

// ============ 流式响应转换（chat SSE → Responses SSE） ============

type responsesSSEWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
	msgID   string
	respID  string
	model   string
}

func newResponsesSSE(w http.ResponseWriter) *responsesSSEWriter {
	f, _ := w.(http.Flusher)
	return &responsesSSEWriter{
		w:       w,
		flusher: f,
		msgID:   newResponseID("msg_"),
		respID:  newResponseID("resp_"),
	}
}

func (s *responsesSSEWriter) event(event string, data any) {
	b, _ := json.Marshal(data)
	fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", event, string(b))
	if s.flusher != nil {
		s.flusher.Flush()
	}
}

// chatStreamToResponses 逐行读取上游 chat SSE，转换为完整 Responses 事件生命周期。
// onUsage 在收到上游 usage 时回调（用于请求日志/账号统计）。
func chatStreamToResponses(w http.ResponseWriter, upstream *http.Response, reqLog *RequestLog, acc *Account) {
	s := newResponsesSSE(w)
	s.event("response.created", map[string]any{"type": "response.created"})
	s.event("response.in_progress", map[string]any{"type": "response.in_progress"})

	textEmitted := false
	var curCallID, curCallName string
	var curCallEmitted bool
	var curArgs strings.Builder
	var outText strings.Builder
	var latestUsage tokenUsage
	firstOutputAt := time.Time{}
	startedAt := time.Now()
	if reqLog != nil {
		startedAt = reqLog.StartedAt
	}

	reader := bufio.NewReader(upstream.Body)
	for {
		line, err := reader.ReadString('\n')
		if line != "" {
			line = strings.TrimRight(line, "\r\n")
			if strings.HasPrefix(line, "data:") {
				payload := strings.TrimSpace(line[5:])
				if payload != "" && payload != "[DONE]" {
					var obj map[string]any
					if json.Unmarshal([]byte(payload), &obj) == nil {
						obj = unwrapDataEnvelope(obj)
						if m, ok := obj["model"].(string); ok && m != "" {
							s.model = m
						}
						usage := parseTokenUsage(obj["usage"])
						if usage.Valid {
							latestUsage = mergeTokenUsage(latestUsage, usage)
						}
						delta := getNested(obj, "choices", 0, "delta")
						if delta == nil {
							delta = getNested(obj, "choices", 0)
						}
						if d, ok := delta.(map[string]any); ok {
							s.emitDelta(d, &textEmitted, &outText, &curCallID, &curCallName, &curCallEmitted, &curArgs)
							if firstOutputAt.IsZero() && hasFirstOutput(obj) {
								firstOutputAt = time.Now()
							}
						}
					}
				}
			}
		}
		if err != nil {
			break
		}
	}

	if textEmitted {
		text := outText.String()
		s.event("response.output_text.done", map[string]any{
			"type": "response.output_text.done", "item_id": s.msgID, "output_index": 0, "content_index": 0, "text": text,
		})
		s.event("response.content_part.done", map[string]any{
			"type": "response.content_part.done", "item_id": s.msgID, "output_index": 0, "content_index": 0,
			"part": map[string]any{"type": "output_text", "text": text, "annotations": []any{}},
		})
		s.event("response.output_item.done", map[string]any{
			"type": "response.output_item.done", "output_index": 0,
			"item": map[string]any{"id": s.msgID, "type": "message", "role": "assistant", "status": "completed",
				"content": []any{map[string]any{"type": "output_text", "text": text, "annotations": []any{}}}},
		})
	}
	if curCallEmitted {
		args := curArgs.String()
		s.event("response.function_call_arguments.done", map[string]any{
			"type": "response.function_call_arguments.done", "item_id": "fc_" + curCallName, "output_index": 1, "arguments": args,
		})
		s.event("response.output_item.done", map[string]any{
			"type": "response.output_item.done", "output_index": 1,
			"item": map[string]any{"type": "function_call", "id": "fc_" + curCallName, "call_id": curCallID,
				"name": curCallName, "arguments": args, "status": "completed"},
		})
	}
	s.event("response.completed", map[string]any{
		"type": "response.completed",
		"response": map[string]any{
			"id": s.respID, "object": "response", "created_at": time.Now().Unix(), "status": "completed",
			"model": s.model, "output": []any{}, "output_text": outText.String(),
			"usage": usageToResponses(latestUsage),
		},
	})

	if reqLog != nil {
		if acc != nil && latestUsage.Valid {
			recordTokenUsage(acc, reqLog.Model, latestUsage)
		}
		finalizeRequestLog(reqLog, latestUsage, firstOutputAt, startedAt, true, "")
	}
}

// emitDelta 处理单个 chat 流增量，发出对应的 Responses 事件。
func (s *responsesSSEWriter) emitDelta(delta map[string]any, textEmitted *bool, outText *strings.Builder,
	curCallID, curCallName *string, curCallEmitted *bool, curArgs *strings.Builder) {

	if c, ok := delta["content"].(string); ok && c != "" {
		if !*textEmitted {
			*textEmitted = true
			s.event("response.output_item.added", map[string]any{
				"type": "response.output_item.added", "output_index": 0,
				"item": map[string]any{"id": s.msgID, "type": "message", "role": "assistant", "status": "in_progress", "content": []any{}},
			})
			s.event("response.content_part.added", map[string]any{
				"type": "response.content_part.added", "item_id": s.msgID, "output_index": 0, "content_index": 0,
				"part": map[string]any{"type": "output_text", "text": "", "annotations": []any{}},
			})
		}
		outText.WriteString(c)
		s.event("response.output_text.delta", map[string]any{
			"type": "response.output_text.delta", "item_id": s.msgID, "output_index": 0, "content_index": 0, "delta": c,
		})
	}
	if r, ok := delta["reasoning_content"].(string); ok && r != "" {
		s.event("response.reasoning_summary_text.delta", map[string]any{
			"type": "response.reasoning_summary_text.delta", "item_id": s.msgID, "output_index": 0, "content_index": 0, "delta": r,
		})
	}
	if tc, ok := delta["tool_calls"].([]any); ok {
		for _, c := range tc {
			cm, ok := c.(map[string]any)
			if !ok {
				continue
			}
			if id, ok := cm["id"].(string); ok && id != "" {
				*curCallID = id
			}
			fn, _ := cm["function"].(map[string]any)
			if fn != nil {
				if n, ok := fn["name"].(string); ok && n != "" {
					*curCallName = n
				}
				if a, ok := fn["arguments"].(string); ok && a != "" {
					curArgs.WriteString(a)
				}
			}
			if !*curCallEmitted && *curCallName != "" {
				*curCallEmitted = true
				s.event("response.output_item.added", map[string]any{
					"type": "response.output_item.added", "output_index": 1,
					"item": map[string]any{"type": "function_call", "id": "fc_" + *curCallName, "call_id": *curCallID,
						"name": *curCallName, "arguments": "", "status": "in_progress"},
				})
			}
		}
	}
}

// usageToResponses 把聚合的 tokenUsage 转成 Responses usage 结构。
func usageToResponses(u tokenUsage) map[string]any {
	cached := any(0)
	if u.Cached > 0 {
		cached = u.Cached
	}
	total := u.Total
	if total == 0 {
		total = u.Prompt + u.Completion
	}
	return map[string]any{
		"input_tokens":         u.Prompt,
		"input_tokens_details": map[string]any{"cached_tokens": cached},
		"output_tokens":        u.Completion,
		"total_tokens":         total,
	}
}

// ============ /v1/responses 入口 ============

func handleResponses(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	var params map[string]any
	if err := json.Unmarshal(body, &params); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	model, _ := params["model"].(string)
	isStream, _ := params["stream"].(bool)
	log.Printf("  responses: model=%s stream=%v", model, isStream)

	reqLog := RequestLog{StartedAt: time.Now(), Protocol: "responses", Model: model, Stream: isStream}

	chat := responsesToChat(params)
	// 清洗畸形 tool_calls（空 function.name / 孤儿 tool 结果），避免上游 400
	if msgs, ok := chat["messages"].([]any); ok {
		chat["messages"] = sanitizeMessages(msgs)
	}
	chatModel, _ := chat["model"].(string)
	route := routeModel(chatModel)

	switch route {
	case "reject":
		finalizeRequestLog(&reqLog, tokenUsage{}, time.Time{}, reqLog.StartedAt, false, "paid zen model rejected")
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{
				"message": fmt.Sprintf("model %q is a paid opencode model; only free models are proxied", chatModel),
				"type":    "invalid_request_error",
			},
		})
		return
	case "zen":
		reqLog.Upstream = upstreamOpenCode
		zm, _ := resolveZenInfo(chatModel)
		out := maybeCompact(chat, zm, requestSessionID(chat, r.Header))
		if out.changed {
			log.Printf("  responses %s", out.note)
		}
		upResp, err := callZenAPI(chat, isStream)
		if err != nil {
			if fbResp, fbAcc, fbErr, attempted := zenFailoverToCline(chat, isStream); attempted {
				if fbErr == nil {
					log.Printf("  responses failover: serving %q via cline pool", chatModel)
					stampUpstream(&reqLog, chat) // 归因实际服务方（provider / cline）
					if fm, ok := chat["model"].(string); ok && fm != "" {
						reqLog.Model = fm // zen 故障转移后记录实际服务模型
					}
					if fbAcc != nil {
						reqLog.AccountID = fbAcc.AccountID
						reqLog.AccountEmail = fbAcc.Email
					}
					defer fbResp.Body.Close()
					if isStream {
						w.Header().Set("Content-Type", "text/event-stream")
						w.Header().Set("Cache-Control", "no-cache")
						w.Header().Set("Connection", "keep-alive")
						w.Header().Set("Access-Control-Allow-Origin", "*")
						w.WriteHeader(http.StatusOK)
						chatStreamToResponses(w, fbResp, &reqLog, fbAcc)
						return
					}
					var raw map[string]any
					if err := json.NewDecoder(fbResp.Body).Decode(&raw); err != nil {
						finalizeRequestLog(&reqLog, tokenUsage{}, time.Time{}, reqLog.StartedAt, false, "decode response: "+err.Error())
						writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
						return
					}
					out2 := normalizeOpenAIResponse(unwrapDataEnvelope(raw))
					usage := parseTokenUsage(out2["usage"])
					recordTokenUsage(fbAcc, reqLog.Model, usage)
					finalizeRequestLog(&reqLog, usage, time.Time{}, reqLog.StartedAt, true, "")
					writeJSON(w, http.StatusOK, chatToResponses(out2))
					return
				}
				err = fbErr
			}
			log.Printf("  responses api error: %v", err)
			finalizeRequestLog(&reqLog, tokenUsage{}, time.Time{}, reqLog.StartedAt, false, err.Error())
			writeJSON(w, http.StatusBadGateway, map[string]any{
				"error": map[string]string{"message": err.Error(), "type": "api_error"},
			})
			return
		}
		defer upResp.Body.Close()

		if isStream {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.WriteHeader(http.StatusOK)
			chatStreamToResponses(w, upResp, &reqLog, nil)
			return
		}
		var raw map[string]any
		if err := json.NewDecoder(upResp.Body).Decode(&raw); err != nil {
			finalizeRequestLog(&reqLog, tokenUsage{}, time.Time{}, reqLog.StartedAt, false, "decode response: "+err.Error())
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		out2 := normalizeOpenAIResponse(unwrapDataEnvelope(raw))
		usage := parseTokenUsage(out2["usage"])
		finalizeRequestLog(&reqLog, usage, time.Time{}, reqLog.StartedAt, true, "")
		writeJSON(w, http.StatusOK, chatToResponses(out2))

	default: // cline
		upResp, acc, err := callClineAPI(chat, isStream)
		if effectiveModel, ok := chat["model"].(string); ok && effectiveModel != "" {
			reqLog.Model = effectiveModel // 含回退后的实际服务模型
		}
		stampUpstream(&reqLog, chat) // 归因实际服务方（provider / opencode / cline）
		if err != nil {
			log.Printf("  responses api error: %v", err)
			finalizeRequestLog(&reqLog, tokenUsage{}, time.Time{}, reqLog.StartedAt, false, err.Error())
			writeJSON(w, clineErrorHTTPStatus(err), map[string]any{
				"error": map[string]string{"message": err.Error(), "type": "api_error"},
			})
			return
		}
		defer upResp.Body.Close()
		if acc != nil {
			reqLog.AccountID = acc.AccountID
			reqLog.AccountEmail = acc.Email
		}

		if isStream {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.WriteHeader(http.StatusOK)
			chatStreamToResponses(w, upResp, &reqLog, acc)
			return
		}
		var raw map[string]any
		if err := json.NewDecoder(upResp.Body).Decode(&raw); err != nil {
			finalizeRequestLog(&reqLog, tokenUsage{}, time.Time{}, reqLog.StartedAt, false, "decode response: "+err.Error())
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"error": map[string]string{"message": err.Error(), "type": "parse_error"},
			})
			return
		}
		out2 := normalizeOpenAIResponse(unwrapDataEnvelope(raw))
		usage := parseTokenUsage(out2["usage"])
		if acc != nil {
			recordTokenUsage(acc, reqLog.Model, usage)
		}
		finalizeRequestLog(&reqLog, usage, time.Time{}, reqLog.StartedAt, true, "")
		writeJSON(w, http.StatusOK, chatToResponses(out2))
	}
}

// unwrapDataEnvelope 剥掉部分上游返回的 {data:{...}} 信封。
func unwrapDataEnvelope(obj map[string]any) map[string]any {
	if data, ok := obj["data"]; ok {
		if d, ok := data.(map[string]any); ok {
			if _, hasChoices := d["choices"]; hasChoices {
				return d
			}
			if _, hasID := d["id"]; hasID {
				return d
			}
		}
	}
	return obj
}
