package tuck

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

var editableToolsets = []string{"filesystem", "file", "shell", "todo", "user_prompt", "background_agents", "background_jobs", "fetch", "git", "think", "plan", "environment", "session_context"}

func pickFields(v M, keys []string) M {
	result := M{}
	for _, key := range keys {
		if value, ok := v[key]; ok {
			result[key] = value
		}
	}
	return result
}
func publicModel(v M) M {
	return pickFields(v, []string{"source", "modelId", "displayName", "enabled", "selectable", "protocol", "inputModalities", "capabilities", "contextWindow", "maxOutputTokens", "reasoningEfforts"})
}
func ConnectionView(connection M, locked []string) M {
	v := Clone(connection)
	if Contains(locked, "providers") {
		models := []any{}
		for _, m := range A(v["models"]) {
			models = append(models, publicModel(O(m)))
		}
		v["models"] = models
	}
	if Contains(locked, "mcp") {
		for _, field := range []string{"localMcp", "mcp"} {
			items := []any{}
			for _, m := range A(v[field]) {
				items = append(items, pickFields(O(m), []string{"name", "mcpServerId", "displayName", "enabled", "status", "toolCount"}))
			}
			v[field] = items
		}
	}
	if Contains(locked, "platform") {
		for _, key := range []string{"origin", "employeeName", "managedGeneration", "schemaVersion", "compatibilityPlatform", "error"} {
			delete(v, key)
		}
	}
	return v
}
func RuntimeView(config *Config, models *Models, locked []string) (M, error) {
	current, revision, e := config.Read()
	if e != nil {
		return nil, e
	}
	providers := []any{}
	savedIDs := []any{}
	for _, v := range A(current["providers"]) {
		p := Clone(O(v))
		p["hasApiKey"] = S(p["apiKey"]) != ""
		delete(p, "apiKey")
		providers = append(providers, p)
		for _, mv := range A(p["models"]) {
			savedIDs = append(savedIDs, O(mv)["modelId"])
		}
	}
	localMcp := []any{}
	for _, v := range A(current["localMcp"]) {
		m := Clone(O(v))
		m["hasAuthToken"] = S(m["authToken"]) != ""
		delete(m, "authToken")
		localMcp = append(localMcp, m)
	}
	snapshot := O(models.Platform.Binding()["snapshot"])
	managedProviders := []any{}
	for _, v := range A(snapshot["providers"]) {
		p := O(v)
		managedProviders = append(managedProviders, M{"providerId": p["providerId"], "displayName": p["displayName"], "enabled": p["enabled"], "protocol": p["clientProtocol"]})
	}
	managedMcp := []any{}
	for _, m := range models.MCP.Summary(false) {
		managedMcp = append(managedMcp, Merge(m, M{"settingsKey": models.DefaultsKey(S(m["mcpServerId"]), "platform")}))
	}
	localState := M{}
	for _, m := range models.MCP.Summary(true) {
		localState[S(m["name"])] = m
	}
	managedModels := []any{}
	modelCatalog := []any{}
	for _, v := range A(models.Summary()["models"]) {
		m := O(v)
		if S(m["source"]) == "platform" {
			managedModels = append(managedModels, m)
			if B(m["selectable"]) {
				modelCatalog = append(modelCatalog, publicModel(m))
			}
		}
	}
	for _, pv := range providers {
		p := O(pv)
		if B(p["enabled"]) {
			for _, mv := range A(p["models"]) {
				m := O(mv)
				if B(m["enabled"]) {
					modelCatalog = append(modelCatalog, Merge(publicModel(m), M{"source": "local"}))
				}
			}
		}
	}
	wb := Clone(O(current["workbench"]))
	if !Has(wb, "defaultAgent") {
		wb["defaultAgent"] = ""
	}
	if !Has(wb, "deliveryRetentionCount") {
		wb["deliveryRetentionCount"] = 10
	}
	draft := M{"providers": providers, "defaultModelId": S(current["defaultModelId"]), "modelDefaults": current["modelDefaults"], "agents": current["agents"], "workbench": wb, "localMcp": localMcp, "platformModelSettings": O(current["platformModelSettings"]), "platformMcpTools": O(current["platformMcpTools"])}
	names := []string{}
	for name := range O(current["agents"]) {
		names = append(names, name)
	}
	view := M{"managedProviders": managedProviders, "managedMcp": managedMcp, "localMcpState": localState, "savedModelIds": savedIDs, "toolsetTypes": editableToolsets, "revision": revision, "pending": config.Pending(), "config": draft, "mode": models.Summary()["mode"], "managedModels": managedModels, "lockedSections": locked, "modelCatalog": modelCatalog, "agentNames": names}
	for _, section := range locked {
		for _, field := range runtimeFields[section] {
			delete(draft, field)
		}
	}
	if Contains(locked, "providers") {
		view["managedProviders"] = []any{}
		view["managedModels"] = []any{}
		view["savedModelIds"] = []any{}
	}
	if Contains(locked, "mcp") {
		view["managedMcp"] = []any{}
		delete(view, "localMcpState")
	}
	return view, nil
}
func PrepareRuntime(config *Config, models *Models, data M, locked []string) (M, M, string, error) {
	current, revision, e := config.Read()
	if e != nil {
		return nil, nil, "", e
	}
	if S(data["revision"]) != revision {
		return nil, nil, "", Problem(409, "settings_changed", "配置已被其他页面或本机修改，请重新打开运行配置后编辑")
	}
	draft, ok := data["config"].(map[string]any)
	if !ok {
		return nil, nil, "", Problem(422, "invalid_config", "配置内容无效")
	}
	if !noUnknown(draft, "providers", "defaultModelId", "modelDefaults", "agents", "workbench", "localMcp", "platformModelSettings", "platformMcpTools") {
		return nil, nil, "", Problem(422, "invalid_config", "配置内容无效")
	}
	if len(locked) > 0 && len(draft) == 0 {
		return nil, nil, "", Problem(403, "settings_locked", "部分配置已锁定，请解锁后编辑")
	}
	for _, section := range locked {
		for _, field := range runtimeFields[section] {
			if Has(draft, field) {
				return nil, nil, "", Problem(403, "settings_locked", "部分配置已锁定，请解锁后编辑")
			}
		}
	}
	next := Merge(current, Clone(draft))
	if Has(draft, "providers") {
		for _, pv := range A(next["providers"]) {
			p := O(pv)
			var old M
			for _, v := range A(current["providers"]) {
				if O(v)["id"] == p["id"] {
					old = O(v)
				}
			}
			if B(p["hasApiKey"]) && old == nil {
				return nil, nil, "", Problem(422, "invalid_config", "已有提供商标识不能修改；请添加新提供商")
			}
			if !Has(p, "apiKey") {
				p["apiKey"] = S(old["apiKey"])
			}
			delete(p, "hasApiKey")
		}
	}
	if Has(draft, "localMcp") {
		for _, mv := range A(next["localMcp"]) {
			m := O(mv)
			var old M
			for _, v := range A(current["localMcp"]) {
				if O(v)["name"] == m["name"] {
					old = O(v)
				}
			}
			if B(m["hasAuthToken"]) && old == nil {
				return nil, nil, "", Problem(422, "invalid_config", "已有 MCP 标识不能修改；请添加新连接")
			}
			if !Has(m, "authToken") {
				m["authToken"] = S(old["authToken"])
			}
			delete(m, "hasAuthToken")
		}
	}
	if e = ValidateConfig(next, true); e != nil {
		return nil, nil, "", e
	}
	snapshot := O(models.Platform.Binding()["snapshot"])
	active := map[string]bool{}
	for _, m := range configuredModels(next) {
		active[S(m["modelId"])] = B(m["enabled"])
	}
	for _, v := range A(snapshot["models"]) {
		m := O(v)
		id := S(m["modelId"])
		for _, local := range configuredModels(next) {
			if S(local["modelId"]) == id {
				return nil, nil, "", Problem(422, "invalid_config", "本地模型标识不能与平台模型重复")
			}
		}
		key := models.DefaultsKey(id, "platform")
		settings := O(O(next["platformModelSettings"])[key])
		previous := O(O(current["platformModelSettings"])[key])
		for _, field := range []string{"contextWindow", "maxOutputTokens", "temperature", "topP"} {
			if Has(m, field) && Has(settings, field) && !reflect.DeepEqual(settings[field], previous[field]) {
				return nil, nil, "", Problem(422, "invalid_config", "平台下发的 "+field+" 不可修改")
			}
		}
		if B(m["enabled"]) && Contains(m["inputModalities"], "TEXT") && Contains(m["capabilities"], "TOOL") {
			for _, pv := range A(snapshot["providers"]) {
				p := O(pv)
				if p["providerId"] == m["providerId"] && B(p["enabled"]) && Contains([]string{"OPENAI_CHAT_COMPLETIONS", "OPENAI_RESPONSES"}, S(p["clientProtocol"])) {
					active[id] = true
				}
			}
		}
	}
	if id := S(next["defaultModelId"]); id != "" && !active[id] {
		return nil, nil, "", Problem(422, "invalid_config", "默认模型必须是已启用且支持工具调用的模型")
	}
	for name, v := range O(next["agents"]) {
		a := O(v)
		if id := S(a["modelId"]); id != "" && !active[id] {
			return nil, nil, "", Problem(422, "invalid_config", "Agent "+name+" 引用的模型不可用")
		}
	}
	return next, current, revision, nil
}
func VerifyModel(ctx context.Context, config *Config, data M) (M, error) {
	provider, model := O(data["provider"]), O(data["model"])
	if !validURL(provider["baseUrl"], false) || !Contains([]string{"OPENAI_CHAT_COMPLETIONS", "OPENAI_RESPONSES"}, S(provider["protocol"])) || !stringField(model["upstreamModelKey"], 1, 256) {
		return nil, Problem(422, "invalid_config", "请填写协议、地址和上游模型名称")
	}
	key := S(provider["apiKey"])
	if !Has(provider, "apiKey") {
		current, _, e := config.Read()
		if e != nil {
			return nil, e
		}
		for _, v := range A(current["providers"]) {
			if O(v)["id"] == provider["id"] {
				key = S(O(v)["apiKey"])
			}
		}
	}
	if !validSecret(key) {
		return nil, Problem(422, "invalid_config", "API Key 格式无效")
	}
	responses := S(provider["protocol"]) == "OPENAI_RESPONSES"
	body := M{"model": model["upstreamModelKey"], "messages": []any{M{"role": "user", "content": "Reply only OK."}}, "max_tokens": 64, "stream": false}
	suffix := "/chat/completions"
	if responses {
		body = M{"model": model["upstreamModelKey"], "input": "Reply only OK.", "max_output_tokens": 64, "stream": false, "store": false}
		suffix = "/responses"
	}
	started := time.Now()
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	request, e := http.NewRequestWithContext(ctx, "POST", strings.TrimSuffix(S(provider["baseUrl"]), "/")+suffix, bytes.NewReader(JSON(body)))
	if e != nil {
		return nil, e
	}
	request.Header.Set("Content-Type", "application/json")
	if key != "" {
		request.Header.Set("Authorization", "Bearer "+key)
	}
	client := &http.Client{Transport: newTransport(20 * time.Second), CheckRedirect: noRedirect}
	defer client.CloseIdleConnections()
	result := M{"ok": false, "message": "连接失败、响应无效或超时，请检查地址和网络"}
	defer func() { result["elapsedMs"] = time.Since(started).Milliseconds() }()
	r, e := client.Do(request)
	if e != nil {
		return result, nil
	}
	defer r.Body.Close()
	if r.StatusCode < 200 || r.StatusCode >= 300 {
		result["message"] = fmt.Sprintf("供应商返回 HTTP %d，请检查模型与协议", r.StatusCode)
		if r.StatusCode == 401 || r.StatusCode == 403 {
			result["message"] = "认证失败，请检查 API Key 和访问权限"
		}
		return result, nil
	}
	raw, e := io.ReadAll(io.LimitReader(r.Body, (256<<10)+1))
	if e != nil || len(raw) > 256<<10 {
		return result, nil
	}
	reply := ""
	if strings.Contains(r.Header.Get("Content-Type"), "text/event-stream") {
		for _, line := range strings.Split(string(raw), "\n") {
			if strings.HasPrefix(line, "data:") {
				v, e := DecodeJSON([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), 256<<10)
				if e == nil {
					item := O(v)
					choices := A(item["choices"])
					if len(choices) > 0 {
						reply += S(O(O(choices[0])["delta"])["content"])
					} else {
						reply += S(item["delta"])
					}
				}
			}
		}
	} else {
		v, e := DecodeJSON(raw, 256<<10)
		if e != nil {
			return result, nil
		}
		data := O(v)
		if responses {
			for _, o := range A(data["output"]) {
				for _, content := range A(O(o)["content"]) {
					if S(O(content)["type"]) == "output_text" {
						reply += S(O(content)["text"])
					}
				}
			}
		} else if choices := A(data["choices"]); len(choices) > 0 {
			reply = S(O(O(choices[0])["message"])["content"])
		}
	}
	result["ok"] = reply != ""
	result["message"] = "请求成功，但未收到文本回复；请检查模型输出能力"
	if reply != "" {
		result["message"] = "基础连接成功"
		if len([]rune(reply)) > 160 {
			reply = string([]rune(reply)[:160])
		}
		result["reply"] = reply
	}
	return result, nil
}
func BuiltinTools(ctx context.Context, binary, kind string) ([]M, error) {
	types := editableToolsets
	if kind != "" {
		if !Contains(types, kind) {
			return nil, Problem(422, "invalid_toolset", "此工具集由原生配置维护")
		}
		types = []string{kind}
	}
	directory, e := os.MkdirTemp("", "tuck-tools-")
	if e != nil {
		return nil, e
	}
	defer os.RemoveAll(directory)
	agents := M{}
	names := []string{}
	for index, kind := range types {
		name := "inspect_" + kind
		if index == 0 {
			name = "root"
		}
		names = append(names, name)
		tool := M{"type": kind}
		if kind == "file" || kind == "filesystem" {
			tool["allow_list"] = []any{"."}
		}
		agents[name] = M{"model": "check", "toolsets": []any{tool}}
	}
	raw, _ := yaml.Marshal(M{"version": "15", "models": M{"check": M{"provider": "openai", "model": "check", "base_url": "http://127.0.0.1:1"}}, "agents": agents})
	file := filepath.Join(directory, "inspect.yaml")
	if e = os.WriteFile(file, raw, 0600); e != nil {
		return nil, e
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "debug", "toolsets", file, "--json")
	cmd.Dir = directory
	cmd.Env = append(os.Environ(), "OPENAI_API_KEY=inspection-only", "TELEMETRY_ENABLED=false")
	output, e := cmd.Output()
	if e != nil {
		return nil, Problem(502, "tool_discovery_failed", "无法读取原生工具列表，请检查运行时是否可用")
	}
	value, e := DecodeJSON(output, 2<<20)
	if e != nil {
		return nil, e
	}
	groups := []M{}
	for index, kind := range types {
		tools := []any{}
		for _, av := range A(value) {
			a := O(av)
			if S(a["agent"]) == names[index] {
				for _, tv := range A(a["tools"]) {
					t := O(tv)
					description := []rune(S(t["description"]))
					description = description[:min(1200, len(description))]
					tools = append(tools, M{"name": t["name"], "description": string(description), "readonly": B(O(t["annotations"])["readOnlyHint"])})
				}
			}
		}
		groups = append(groups, M{"type": kind, "tools": tools})
	}
	return groups, nil
}
func DiscoverTools(ctx context.Context, app *App, data M) (M, error) {
	switch S(data["kind"]) {
	case "builtin":
		groups, e := BuiltinTools(ctx, app.Engine.Options().Binary, S(data["type"]))
		if e != nil && S(data["type"]) == "" {
			groups = []M{}
			for _, kind := range editableToolsets {
				g, er := BuiltinTools(ctx, app.Engine.Options().Binary, kind)
				if er != nil {
					groups = append(groups, M{"type": kind, "tools": []any{}, "error": "当前引擎未提供此类工具清单"})
				} else {
					groups = append(groups, g...)
				}
			}
			e = nil
		}
		if e != nil {
			return nil, e
		}
		if S(data["type"]) != "" {
			return M{"tools": groups[0]["tools"]}, nil
		}
		return M{"groups": groups}, nil
	case "platform_mcp":
		id := S(data["mcpServerId"])
		if S(data["settingsKey"]) != app.Models.DefaultsKey(id, "platform") {
			return nil, Problem(422, "settings_changed", "平台 MCP 或身份已变化，请刷新设置")
		}
		if e := app.Models.MCP.Discover(ctx, id); e != nil {
			return nil, e
		}
		c := app.Models.MCP
		c.mu.RLock()
		entry := c.entries[id]
		tools := []any{}
		for _, tool := range entry.Tools {
			raw := O(tool["raw"])
			tools = append(tools, M{"name": raw["name"], "description": raw["description"], "readonly": B(O(raw["annotations"])["readOnlyHint"])})
		}
		state := M{"status": entry.Status, "validated": entry.Validated, "toolCount": len(entry.Tools), "error": entry.Error}
		c.mu.RUnlock()
		return M{"tools": tools, "state": state}, nil
	case "mcp":
		entry := O(data["mcp"])
		if Has(entry, "authTokenEnv") {
			return nil, Problem(422, "invalid_config", "MCP authTokenEnv 已停用，请使用 authToken")
		}
		if !validURL(entry["url"], true) {
			return nil, Problem(422, "invalid_config", "MCP 地址或认证配置无效")
		}
		token := S(entry["authToken"])
		if !Has(entry, "authToken") {
			current, _, e := app.Config.Read()
			if e != nil {
				return nil, e
			}
			for _, v := range A(current["localMcp"]) {
				if O(v)["name"] == entry["name"] {
					token = S(O(v)["authToken"])
				}
			}
		}
		if !validSecret(token) {
			return nil, Problem(422, "invalid_config", "Token 格式无效")
		}
		// Verification of an unchanged registration updates that owner's facts.
		// A draft endpoint remains a one-off probe and is never persisted.
		owner := app.Models.MCP
		owner.mu.RLock()
		saved := owner.entries["local:"+S(entry["name"])]
		var resource M
		if saved != nil {
			resource = Clone(saved.Resource)
		}
		owner.mu.RUnlock()
		if saved != nil && resource["url"] == entry["url"] && S(resource["authToken"]) == token {
			if e := owner.Discover(ctx, saved.Key); e != nil {
				return nil, e
			}
			owner.mu.RLock()
			summary := []any{}
			for _, tool := range saved.Catalog {
				summary = append(summary, M{"name": tool["name"], "description": tool["description"], "readonly": B(O(tool["annotations"])["readOnlyHint"])})
			}
			state := M{"status": saved.Status, "validated": saved.Validated, "toolCount": len(saved.Tools), "error": saved.Error}
			owner.mu.RUnlock()
			return M{"tools": summary, "state": state}, nil
		}
		headers := http.Header{}
		if token != "" {
			headers.Set("Authorization", "Bearer "+token)
		}
		ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		tools, e := verifyRemote(ctx, app.Models.MCP.client, S(entry["url"]), headers)
		if e != nil {
			return nil, Problem(502, "mcp_verification_failed", "无法读取 MCP 工具，请检查地址、认证及服务状态")
		}
		summary := []any{}
		for _, t := range tools {
			summary = append(summary, M{"name": t["name"], "description": t["description"], "readonly": B(O(t["annotations"])["readOnlyHint"])})
		}
		return M{"tools": summary}, nil
	}
	return nil, Problem(422, "invalid_config", "请选择工具连接")
}

var _ = url.PathEscape
