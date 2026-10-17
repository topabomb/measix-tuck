package tuck

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

func (m *Models) forward(w http.ResponseWriter, r *http.Request) {
	if !Equal(r.Header.Get("Authorization"), "Bearer "+m.Secret) {
		WriteJSON(w, 401, M{"error": M{"message": "本地模型通道凭据无效", "code": "gateway_unauthorized"}})
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	stop := context.AfterFunc(m.ctx, cancel)
	defer stop()
	defer cancel()
	r = r.WithContext(ctx)
	if strings.HasPrefix(r.URL.Path, "/mcp/") {
		m.MCP.ServeHTTP(w, r)
		return
	}
	if m.Local != nil && r.URL.Path == tuckToolsPath {
		m.Local.ServeHTTP(w, r)
		return
	}
	if r.Method != "POST" || (r.URL.Path != "/v1/chat/completions" && r.URL.Path != "/v1/responses") {
		WriteError(w, Problem(404, "gateway_route", "模型通道路径无效"))
		return
	}
	e := m.forwardModel(w, r)
	if e != nil {
		WriteJSON(w, Status(e), M{"error": M{"message": e.Error(), "code": Code(e), "type": "tuck_model_error"}})
	}
}
func (m *Models) forwardModel(w http.ResponseWriter, r *http.Request) error {
	body, e := ReadJSON(r.Body, AgentRequestBytes)
	if e != nil {
		return e
	}
	selector := S(body["model"])
	var model M
	effort := "default"
	for _, entry := range m.Catalog() {
		key := S(entry["key"])
		if key == selector {
			model = entry
			break
		}
		for _, value := range ModelReasoningOptions(entry) {
			if value != "default" && key+"--reasoning-"+value == selector {
				model = entry
				effort = value
				break
			}
		}
	}
	if !B(model["selectable"]) {
		return Problem(428, "model_unavailable", "当前模型资源不可用，请同步配置或选择其他模型后继续")
	}
	responses := strings.HasSuffix(r.URL.Path, "/responses")
	if responses != (S(model["protocol"]) == "OPENAI_RESPONSES") {
		return Problem(422, "protocol_mismatch", "模型请求协议与目录不一致")
	}
	if responses {
		if body["previous_response_id"] != nil || Has(body, "store") && body["store"] != false {
			return Problem(422, "responses_state", "Responses 必须使用完整输入和 store=false")
		}
		body["store"] = false
	}
	if effort != "default" {
		if responses {
			body["reasoning"] = Merge(O(body["reasoning"]), M{"effort": effort})
		} else {
			body["reasoning_effort"] = effort
		}
	}
	resource := Merge(model, M{"resourceId": model["modelId"]})
	if S(model["source"]) == "local" {
		resource["runtimePath"] = map[bool]string{true: "/responses", false: "/chat/completions"}[responses]
	}
	target, headers, e := m.resourceAccess(r.Context(), m.Config(), m.Platform.Binding(), resource)
	if e != nil {
		return e
	}
	for _, pair := range [][2]string{{"temperature", "temperature"}, {"topP", "top_p"}} {
		if Has(model, pair[0]) {
			body[pair[1]] = model[pair[0]]
		}
	}
	body["model"] = model["upstreamModelKey"]
	if S(body["model"]) == "" {
		body["model"] = model["modelId"]
	}
	req, e := http.NewRequestWithContext(r.Context(), "POST", target, bytes.NewReader(JSON(body)))
	if e != nil {
		return e
	}
	req.Header = headers
	upstream, e := m.client.Do(req)
	if e != nil {
		return Problem(502, "model_unreachable", "模型网络连接失败")
	}
	defer upstream.Body.Close()
	if upstream.StatusCode < 200 || upstream.StatusCode >= 300 {
		payload, _ := ReadJSON(upstream.Body, 1<<20)
		message, code := ModelRequestError(upstream.StatusCode, payload, effort, S(model["displayName"]), []string{m.Secret, strings.TrimPrefix(headers.Get("Authorization"), "Bearer ")})
		m.mu.Lock()
		for _, v := range m.catalog {
			if v["modelId"] == model["modelId"] {
				v["lastError"] = message
			}
		}
		m.mu.Unlock()
		return Problem(upstream.StatusCode, code, message)
	}
	m.mu.Lock()
	for _, v := range m.catalog {
		if v["modelId"] == model["modelId"] {
			delete(v, "lastError")
		}
	}
	m.mu.Unlock()
	w.Header().Set("Content-Type", upstream.Header.Get("Content-Type"))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(upstream.StatusCode)
	buffer := make([]byte, 32<<10)
	for {
		n, e := upstream.Body.Read(buffer)
		if n > 0 {
			if _, err := w.Write(buffer[:n]); err != nil {
				return nil
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
		if e == io.EOF {
			return nil
		}
		if e != nil {
			panic(http.ErrAbortHandler)
		}
	}
}
func ModelRequestError(status int, payload M, effort, name string, secrets []string) (string, string) {
	remote := O(payload["error"])
	if len(remote) == 0 {
		remote = payload
	}
	code := firstString(payload["code"], remote["code"], "model_request_failed")
	description := firstString(remote["message"], payload["message"], remote["detail"], payload["detail"], remote["title"], payload["title"])
	evidence := strings.ToLower(strings.Join([]string{code, S(remote["param"]), description}, " "))
	if (status == 400 || status == 422) && regexp.MustCompile(`reasoning|effort|thinking|推理`).MatchString(evidence) && !strings.Contains(evidence, "reasoning_content") {
		label := map[string]string{"default": "默认", "low": "低", "medium": "中", "high": "高"}[effort]
		if label == "" {
			label = effort
		}
		action := "请选择默认或其他推理等级，也可更换模型。"
		if effort == "default" {
			action = "请同步平台配置或选择其他模型。"
		}
		return fmt.Sprintf("%s 拒绝推理配置「%s」（%s）。%s本次未自动降级。", name, label, effort, action), "reasoning_effort_rejected"
	}
	code = regexp.MustCompile(`[^A-Za-z0-9_.-]`).ReplaceAllString(code, "")
	if len(code) > 64 {
		code = code[:64]
	}
	if code == "" {
		code = "model_request_failed"
	}
	detail := cleanSecret(description, secrets...)
	detail = regexp.MustCompile(`(?i)Bearer\s+[^\s"'<>]+`).ReplaceAllString(detail, "Bearer [redacted]")
	detail = regexp.MustCompile(`(?i)data:[^\s"'<>]+`).ReplaceAllString(detail, "[data omitted]")
	detail = strings.TrimSpace(regexp.MustCompile(`[\x00-\x1f\x7f]+`).ReplaceAllString(detail, " "))
	runes := []rune(detail)
	if len(runes) > 800 {
		detail = string(runes[:800])
	}
	message := fmt.Sprintf("模型调用失败（HTTP %d，%s）", status, code)
	if detail != "" {
		message += "：" + detail
	}
	if payload["forwarded"] == false {
		message += "；服务端报告请求未转发至上游，请检查请求合同或资源配置"
	}
	return message, code
}
