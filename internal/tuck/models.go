package tuck

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

const AgentRequestBytes = 100 << 20

type Models struct {
	Platform                  *Platform
	Store                     *Store
	Settings                  *Config
	Secret, URL, WorkbenchURL string
	MCP                       *MCPConnections
	Local                     *LocalTools
	mu                        sync.RWMutex
	config                    M
	catalog                   []M
	server                    *http.Server
	ctx                       context.Context
	cancel                    context.CancelFunc
	client                    *http.Client
}

func NewModels(config *Config, store *Store, platform *Platform) (*Models, error) {
	ctx, cancel := context.WithCancel(context.Background())
	m := &Models{Settings: config, Store: store, Platform: platform, Secret: Token(), config: config.Applied(), ctx: ctx, cancel: cancel, client: &http.Client{Transport: newTransport(180 * time.Second), CheckRedirect: noRedirect}}
	m.MCP = NewMCPConnections(m)
	if e := m.Rebuild(); e != nil {
		cancel()
		return nil, e
	}
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		cancel()
		return nil, e
	}
	m.URL = "http://" + listener.Addr().String()
	m.server = &http.Server{Handler: http.HandlerFunc(m.forward), ReadHeaderTimeout: 60 * time.Second}
	go m.server.Serve(listener)
	return m, nil
}
func (m *Models) Config() M { m.mu.RLock(); defer m.mu.RUnlock(); return Clone(m.config) }
func (m *Models) SetConfig(c M) error {
	m.mu.Lock()
	m.config = Clone(c)
	m.mu.Unlock()
	return m.Rebuild()
}
func (m *Models) Catalog() []M {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r := make([]M, len(m.catalog))
	for i, v := range m.catalog {
		r[i] = Clone(v)
	}
	return r
}
func configuredModels(c M) []M {
	r := []M{}
	for _, pv := range A(c["providers"]) {
		p := O(pv)
		for _, mv := range A(p["models"]) {
			r = append(r, Merge(O(mv), M{"providerId": p["id"], "protocol": p["protocol"], "url": strings.TrimSuffix(S(p["baseUrl"]), "/") + map[bool]string{true: "/responses", false: "/chat/completions"}[S(p["protocol"]) == "OPENAI_RESPONSES"], "key": p["apiKey"], "enabled": B(p["enabled"]) && B(O(mv)["enabled"])}))
		}
	}
	return r
}
func (m *Models) DefaultsKey(id, source string) string {
	b := m.Platform.Binding()
	if b != nil && source != "local" {
		return S(b["deploymentId"]) + ":" + S(b["userId"]) + ":" + id
	}
	return "direct:" + id
}
func (m *Models) Rebuild() error {
	c := m.Config()
	b := m.Platform.Binding()
	list := []M{}
	snapshot := O(b["snapshot"])
	for _, mv := range A(snapshot["models"]) {
		model := Clone(O(mv))
		model["source"] = "platform"
		for _, pv := range A(snapshot["providers"]) {
			p := O(pv)
			if p["providerId"] == model["providerId"] {
				model["protocol"] = p["clientProtocol"]
				model["providerEnabled"] = p["enabled"]
			}
		}
		list = append(list, model)
	}
	for _, model := range configuredModels(c) {
		model["source"] = "local"
		delete(model, "key")
		delete(model, "url")
		list = append(list, model)
	}
	counts := map[string]int{}
	for _, v := range list {
		counts[S(v["modelId"])]++
	}
	seen := map[string]bool{}
	catalog := []M{}
	for _, model := range list {
		id := S(model["modelId"])
		if seen[id] {
			continue
		}
		seen[id] = true
		reason := ""
		if counts[id] > 1 {
			reason = "平台与本地模型标识重复，请为本地模型使用不同标识"
		} else if !B(model["enabled"]) || model["providerEnabled"] == false {
			reason = "已停用"
		} else if !slices.Contains([]string{"OPENAI_CHAT_COMPLETIONS", "OPENAI_RESPONSES"}, S(model["protocol"])) {
			reason = "尚未适配该模型协议"
		} else if !Contains(model["inputModalities"], "TEXT") || !Contains(model["capabilities"], "TOOL") {
			reason = "模型未声明文字输入和工具能力"
		}
		scope := "direct"
		managed := S(model["source"]) == "platform"
		if managed {
			scope = "platform:" + S(b["origin"]) + ":" + S(b["deploymentId"])
		}
		key := Hash([]byte(scope))[:20] + "--" + Hash([]byte(id))
		settings := Clone(O(c["modelDefaults"]))
		model["settingsSource"] = "default"
		if managed {
			settingsKey := m.DefaultsKey(id, "platform")
			local := O(O(c["platformModelSettings"])[settingsKey])
			settings = Merge(settings, local)
			defined := []string{}
			for _, field := range []string{"contextWindow", "maxOutputTokens", "temperature", "topP"} {
				if Has(model, field) {
					settings[field] = model[field]
					defined = append(defined, field)
				}
			}
			model["settingsKey"] = settingsKey
			model["platformDefinedFields"] = defined
			for k := range local {
				if !slices.Contains(defined, k) {
					model["settingsSource"] = "local"
				}
			}
		} else if Has(model, "contextWindow") {
			settings = Merge(settings, model)
			model["settingsSource"] = "local"
		}
		model = Merge(model, settings, M{"key": key, "nativeModel": "connected_" + Hash([]byte(key))[:20], "provider": map[bool]string{true: "tuck-responses", false: "tuck-chat"}[S(model["protocol"]) == "OPENAI_RESPONSES"], "selectable": reason == "", "reason": nil})
		if reason != "" {
			model["reason"] = reason
		}
		catalog = append(catalog, model)
	}
	m.mu.Lock()
	m.catalog = catalog
	m.mu.Unlock()
	return m.MCP.Rebuild()
}
func (m *Models) Summary() M {
	b := m.Platform.Binding()
	c := m.Config()
	mode := "template"
	status := "ready"
	if len(configuredModels(c)) > 0 {
		mode = "direct"
	}
	snapshot := O(b["snapshot"])
	if b != nil {
		mode = "platform"
		state := m.Platform.State()
		if m.Platform.Error() != "" {
			status = "needs_attention"
		} else if len(snapshot) == 0 || B(state["runtimeBlocked"]) || B(state["syncRequired"]) {
			status = "pending"
		}
	}
	public := []any{}
	for _, model := range m.Catalog() {
		public = append(public, pickFields(model, []string{"modelId", "providerId", "enabled", "displayName", "protocol", "selectable", "reason", "inputModalities", "capabilities", "lastError", "contextWindow", "maxOutputTokens", "settingsSource", "reasoningEfforts", "source", "temperature", "topP", "settingsKey", "platformDefinedFields"}))
	}
	defaultModel := S(c["defaultModelId"])
	if defaultModel == "" {
		defaultModel = S(O(snapshot["policy"])["defaultModelId"])
	}
	v := M{"mode": mode, "status": status, "defaultModelId": nil, "models": public, "error": nil, "mcp": m.MCP.Summary(false), "localMcp": m.MCP.Summary(true)}
	if defaultModel != "" {
		v["defaultModelId"] = defaultModel
	}
	if b != nil {
		v = Merge(v, M{"deploymentName": O(b["discovery"])["deploymentName"], "origin": b["origin"], "employeeName": b["employeeName"], "managedGeneration": snapshot["managedGeneration"], "schemaVersion": snapshot["schemaVersion"], "compatibilityPlatform": "ANDROID"})
	}
	if e := m.Platform.Error(); e != "" {
		v["error"] = e
	}
	return v
}
func ModelReasoningOptions(model M) []string {
	r := []string{"default"}
	if Contains(model["capabilities"], "REASONING") {
		if Has(model, "reasoningEfforts") {
			r = append(r, Strings(model["reasoningEfforts"])...)
		} else {
			r = append(r, "low", "medium", "high")
		}
	}
	return r
}
func (m *Models) find(id string) M {
	for _, model := range m.Catalog() {
		if S(model["modelId"]) == id {
			return model
		}
	}
	return nil
}
func (m *Models) PickForAgent(agent string, modelID any) (M, error) {
	if modelID != nil && (S(modelID) == "" || S(modelID) != strings.TrimSpace(S(modelID))) {
		return nil, Problem(422, "model_required", "请提供有效的模型资源 ID")
	}
	selected := modelID
	if selected == nil {
		field := "agents." + agent + ".modelId"
		selected = O(O(m.Config()["agents"])[agent])["modelId"]
		if S(selected) == "" {
			field = "defaultModelId"
			selected = m.Summary()["defaultModelId"]
		}
		if S(selected) != "" && !B(m.find(S(selected))["selectable"]) {
			return nil, Problem(422, "model_unavailable", field+" 指定的默认模型当前不可用，请恢复配置或明确选择模型")
		}
	}
	if selected == nil || S(selected) == "" {
		return M{"model": nil}, nil
	}
	return m.Pick(selected, "default")
}
func (m *Models) Pick(id any, reasoning string) (M, error) {
	if id != nil && (S(id) == "" || S(id) != strings.TrimSpace(S(id))) {
		return nil, Problem(422, "model_required", "请提供有效的模型资源 ID")
	}
	if id == nil {
		id = m.Summary()["defaultModelId"]
	}
	if S(id) == "" && len(m.Catalog()) > 0 {
		return nil, Problem(400, "model_required", "尚未配置默认模型，请选择模型")
	}
	model := m.find(S(id))
	if !B(model["selectable"]) {
		return nil, Problem(422, "model_unavailable", "请选择可用模型")
	}
	if S(model["source"]) == "platform" && S(m.Summary()["status"]) != "ready" {
		return nil, Problem(409, "platform_pending", "平台配置尚未就绪，请同步配置")
	}
	if reasoning == "" {
		reasoning = "default"
	}
	if !slices.Contains(ModelReasoningOptions(model), reasoning) {
		return nil, Problem(422, "reasoning_unavailable", S(model["displayName"])+" 不支持所选推理配置")
	}
	native := S(model["nativeModel"])
	if reasoning != "default" {
		native += "_" + reasoning
	}
	return M{"modelId": model["modelId"], "modelSource": model["source"], "modelName": model["displayName"], "modelProtocol": model["protocol"], "reasoningEffort": reasoning, "model": native, "contextWindow": model["contextWindow"]}, nil
}
func (m *Models) Bind(task M) (M, error) {
	if S(task["modelId"]) == "" {
		return nil, Problem(400, "model_required", "请选择当前可用的模型后继续此对话")
	}
	source := S(task["modelSource"])
	if source != "local" && source != "platform" {
		return nil, Problem(409, "model_source_required", "请明确选择模型后继续此对话")
	}
	model := m.find(S(task["modelId"]))
	if model != nil && source != S(model["source"]) {
		return nil, Problem(409, "model_source_changed", "该模型的来源已改变，请明确选择模型后继续此对话")
	}
	if !B(model["selectable"]) {
		return nil, Problem(409, "model_unavailable", "所选模型当前不可用，请恢复资源接入或选择其他模型")
	}
	return m.Pick(task["modelId"], S(task["reasoningEffort"]))
}
func (m *Models) WriteConfig() (string, error) {
	c := m.Config()
	models, providers, agents := M{}, M{}, Clone(O(c["agents"]))
	for _, protocol := range []string{"tuck-chat", "tuck-responses"} {
		api := "openai_chatcompletions"
		if protocol == "tuck-responses" {
			api = "openai_responses"
		}
		providers[protocol] = M{"api_type": api, "base_url": m.URL + "/v1", "token_key": "TUCK_GATEWAY_TOKEN", "max_tokens": 32768}
	}
	for _, model := range m.Catalog() {
		if !B(model["selectable"]) {
			continue
		}
		for _, effort := range ModelReasoningOptions(model) {
			alias, key := S(model["nativeModel"]), S(model["key"])
			opts := M{"context_size": model["contextWindow"]}
			if effort != "default" {
				alias += "_" + effort
				key += "--reasoning-" + effort
				extra := M{"reasoning_effort": effort}
				if S(model["protocol"]) == "OPENAI_RESPONSES" {
					extra = M{"reasoning": M{"effort": effort}}
				}
				opts["extra_body"] = extra
			}
			models[alias] = M{"provider": model["provider"], "model": key, "max_tokens": model["maxOutputTokens"], "provider_opts": opts, "capabilities": M{"image": Contains(model["inputModalities"], "IMAGE"), "pdf": false}}
		}
	}
	exposed := map[string]M{}
	for _, v := range A(O(c["workbench"])["assistants"]) {
		exposed[S(O(v)["agent"])] = O(v)
	}
	for name, av := range agents {
		a := O(av)
		defaultID := S(a["modelId"])
		if defaultID == "" {
			defaultID = S(m.Summary()["defaultModelId"])
		}
		model := m.find(defaultID)
		native := "not_configured"
		if B(model["selectable"]) {
			native = S(model["nativeModel"])
		} else {
			models["not_configured"] = M{"provider": "tuck-chat", "model": "not-configured"}
		}
		a["model"] = native
		delete(a, "modelId")
		disabled := a["disabledTuckTools"]
		delete(a, "disabledTuckTools")
		delete(a, "disabledEnterpriseMcp")
		toolsets := A(a["toolsets"])
		if exposed[name] != nil {
			for _, tool := range sessionToolsets(m.WorkbenchURL) {
				if !Contains(disabled, S(O(tool["api_config"])["name"])) {
					toolsets = append(toolsets, tool)
				}
			}
			if exposed[name]["deliveryTool"] != false && !B(a["readonly"]) {
				toolsets = append(toolsets, localToolset("tuck", m.URL+"/api/tools/delivery", "X-Tuck-Delivery-Turn", false, nil))
			}
		}
		toolsets = append(toolsets, m.MCP.Toolsets(name, B(a["readonly"]))...)
		selected := []any{}
		for _, tool := range []string{"workspace_webdav_upload", "workspace_webdav_download_files"} {
			if !Contains(disabled, tool) {
				selected = append(selected, tool)
			}
		}
		if len(selected) > 0 {
			toolsets = append(toolsets, localToolset("workspace", m.URL+"/workspace-tools", "X-Tuck-Workspace-Turn", B(a["readonly"]), selected))
		}
		a["toolsets"] = toolsets
	}
	raw, e := yaml.Marshal(M{"version": "15", "providers": providers, "models": models, "agents": agents, "permissions": c["permissions"]})
	if e != nil {
		return "", e
	}
	directory := filepath.Join(m.Settings.DataDir(), "runtime")
	if e = os.MkdirAll(directory, 0700); e != nil {
		return "", e
	}
	path := filepath.Join(directory, "agent.yaml")
	if e = AtomicWrite(path, raw, 0600); e != nil {
		return "", e
	}
	return path, nil
}
func localToolset(name, endpoint, header string, readonly bool, tools []any) M {
	v := M{"type": "mcp", "name": name, "allow_private_ips": true, "remote": M{"url": endpoint, "transport_type": "streamable-http", "headers": M{"Authorization": "Bearer ${env.TUCK_GATEWAY_TOKEN}", header: "${headers." + header + "}"}}}
	if readonly {
		v["readonly"] = true
	}
	if tools != nil {
		v["tools"] = tools
	}
	return v
}
func sessionToolsets(base string) []M {
	return []M{{"type": "api", "allow_private_ips": true, "api_config": M{"name": "list_sessions", "instruction": "List unarchived Tuck main conversations and exact task IDs. Use these IDs with send_session_message only when the user requests messaging.", "method": "POST", "endpoint": base + "/api/tools/list-sessions", "headers": M{"Authorization": "Bearer ${env.TUCK_WORKBENCH_TOKEN}"}, "args": M{}}}, {"type": "api", "allow_private_ips": true, "api_config": M{"name": "send_session_message", "instruction": "Send a message to an exact Tuck task_id explicitly requested by the user. Acceptance does not mean completion. Do not automatically reply.", "method": "POST", "endpoint": base + "/api/tools/send-message", "headers": M{"Authorization": "Bearer ${env.TUCK_WORKBENCH_TOKEN}"}, "required": []any{"target_task_id", "message"}, "args": M{"target_task_id": M{"type": "string"}, "message": M{"type": "string"}}}}}
}
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
	if m.Local != nil && (r.URL.Path == "/workspace-tools" || r.URL.Path == "/api/tools/delivery") {
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
	target := ""
	headers := http.Header{"Content-Type": []string{"application/json"}}
	if S(model["source"]) == "platform" {
		b := m.Platform.Binding()
		snapshot := O(b["snapshot"])
		if e = m.Platform.Admit(r.Context(), I(snapshot["managedGeneration"])); e != nil {
			return e
		}
		token, e := m.Platform.Access(r.Context())
		if e != nil {
			return e
		}
		target = S(b["origin"]) + strings.TrimSuffix(S(O(b["discovery"])["runtimeApiBase"]), "/") + "/resources/" + url.PathEscape(S(model["modelId"])) + S(model["runtimePath"])
		headers.Set("Authorization", "Bearer "+token)
		headers.Set("X-Measix-Managed-Generation", fmt.Sprint(I(snapshot["managedGeneration"])))
		headers.Set("X-Measix-Interaction-Id", "int_"+UUID())
	} else {
		for _, direct := range configuredModels(m.Config()) {
			if direct["modelId"] == model["modelId"] {
				target = S(direct["url"])
				if S(direct["key"]) != "" {
					headers.Set("Authorization", "Bearer "+S(direct["key"]))
				}
			}
		}
	}
	if target == "" {
		return Problem(428, "model_unavailable", "当前模型连接不可用")
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
	evidence := strings.ToLower(strings.Join([]string{code, S(remote["param"]), S(remote["message"]), S(payload["message"])}, " "))
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
	detail := cleanSecret(firstString(remote["message"], payload["message"]), secrets...)
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
	return message, code
}
func (m *Models) Close(ctx context.Context) error {
	m.cancel()
	if m.Local != nil {
		m.Local.Close()
	}
	m.MCP.Close()
	m.client.CloseIdleConnections()
	e := m.server.Shutdown(ctx)
	if e != nil {
		m.server.Close()
	}
	return e
}
