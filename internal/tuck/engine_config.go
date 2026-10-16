package tuck

import (
	"gopkg.in/yaml.v3"
	"net/url"
	"os"
	"path/filepath"
)

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
		delete(a, "attachmentInspectionModelId")
		delete(a, "imageGenerator")
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
				toolsets = append(toolsets, localToolset(m.URL+"/api/tools/delivery", "X-Tuck-Delivery-Turn", false, nil))
			}
		}
		toolsets = append(toolsets, m.MCP.Toolsets(name, B(a["readonly"]))...)
		if !Contains(disabled, inspectionTool) {
			toolsets = append(toolsets, localToolset(m.URL+"/attachment-tools?agent="+url.QueryEscape(name), "X-Tuck-Workspace-Turn", true, []any{inspectionTool}))
		}
		selected := []any{}
		for _, tool := range []string{"upload_runcove_file", "download_runcove_files", "generate_images"} {
			if !Contains(disabled, tool) && (tool != "generate_images" || exposed[name] != nil) {
				selected = append(selected, tool)
			}
		}
		if len(selected) > 0 {
			toolsets = append(toolsets, localToolset(m.URL+"/workspace-tools", "X-Tuck-Workspace-Turn", B(a["readonly"]), selected))
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
func localToolset(endpoint, header string, readonly bool, tools []any) M {
	// Omitting the public MCP name keeps our tool names unprefixed. Endpoints
	// and headers still provide the existing ownership and lifecycle boundaries.
	v := M{"type": "mcp", "allow_private_ips": true, "remote": M{"url": endpoint, "transport_type": "streamable-http", "headers": M{"Authorization": "Bearer ${env.TUCK_GATEWAY_TOKEN}", header: "${headers." + header + "}"}}}
	if readonly {
		v["readonly"] = true
	}
	if tools != nil {
		v["tools"] = tools
	}
	return v
}
func sessionToolsets(base string) []M {
	return []M{
		{"type": "api", "allow_private_ips": true, "api_config": M{"name": "list_sessions", "instruction": toolText(textList), "method": "POST", "endpoint": base + "/api/tools/list-sessions", "headers": M{"Authorization": "Bearer ${env.TUCK_WORKBENCH_TOKEN}"}, "args": M{"limit": M{"type": "integer", "description": toolText(textLimit), "default": 10, "minimum": 1, "maximum": 100}}}},
		{"type": "api", "allow_private_ips": true, "api_config": M{"name": "send_session_message", "instruction": toolText(textSend), "method": "POST", "endpoint": base + "/api/tools/send-message", "headers": M{"Authorization": "Bearer ${env.TUCK_WORKBENCH_TOKEN}"}, "required": []any{"target_task_id", "message"}, "args": M{"target_task_id": M{"type": "string", "description": toolText(textTarget)}, "message": M{"type": "string", "description": toolText(textMessage)}}}},
	}
}
