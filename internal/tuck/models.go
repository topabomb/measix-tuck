package tuck

import (
	"context"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
)

const AgentRequestBytes = 100 << 20

type Models struct {
	Platform    *Platform
	Store       *Store
	Settings    *Config
	Secret, URL string
	MCP         *MCPConnections
	Local       *LocalTools
	mu          sync.RWMutex
	config      M
	catalog     []M
	server      *http.Server
	ctx         context.Context
	cancel      context.CancelFunc
	client      *http.Client
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
		}
		inspectionReason := reason
		if inspectionReason == "" && (!Contains(model["inputModalities"], "TEXT") || !Contains(model["inputModalities"], "IMAGE")) {
			inspectionReason = "模型未声明 TEXT、IMAGE 输入"
		}
		model["inspectionSelectable"] = inspectionReason == ""
		model["inspectionReason"] = inspectionReason
		if reason == "" && (!Contains(model["inputModalities"], "TEXT") || !Contains(model["capabilities"], "TOOL")) {
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
		public = append(public, pickFields(model, []string{"modelId", "providerId", "enabled", "displayName", "protocol", "selectable", "reason", "inspectionSelectable", "inspectionReason", "inputModalities", "capabilities", "lastError", "contextWindow", "maxOutputTokens", "settingsSource", "reasoningEfforts", "source", "temperature", "topP", "settingsKey", "platformDefinedFields"}))
	}
	defaultModel := S(c["defaultModelId"])
	if defaultModel == "" {
		defaultModel = S(O(snapshot["policy"])["defaultModelId"])
	}
	v := M{"mode": mode, "status": status, "defaultModelId": nil, "models": public, "error": nil, "mcp": m.MCP.Summary(false), "localMcp": m.MCP.Summary(true)}
	inspection := M{}
	for agent, av := range O(c["agents"]) {
		model, e := m.InspectionModel(agent)
		item := M{"available": e == nil && !Contains(O(av)["disabledTuckTools"], inspectionTool), "modelId": model["modelId"]}
		if e != nil {
			item["reason"] = e.Error()
		}
		inspection[agent] = item
	}
	v["attachmentInspection"] = inspection
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
