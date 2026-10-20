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
		delete(a, "disabledTuckTools")
		delete(a, "disabledEnterpriseMcp")
		toolsets := A(a["toolsets"])
		toolsets = append(toolsets, m.MCP.Toolsets(name, B(a["readonly"]))...)
		if names := configuredTuckTools(c, name, true); len(names) > 0 {
			toolsets = append(toolsets, tuckToolset(m.URL, name, B(a["readonly"])))
		}
		a["toolsets"] = toolsets
	}
	raw, e := yaml.Marshal(M{"version": "16", "providers": providers, "models": models, "agents": agents, "permissions": c["permissions"]})
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
func tuckToolset(baseURL, agent string, readonly bool) M {
	endpoint := baseURL + tuckToolsPath + "?agent=" + url.QueryEscape(agent)
	// Omitting the public MCP name keeps our tool names unprefixed. Endpoints
	// and headers still provide the existing ownership and lifecycle boundaries.
	v := M{"type": "mcp", "allow_private_ips": true, "remote": M{"url": endpoint, "transport_type": "streamable-http", "headers": M{"Authorization": "Bearer ${env.TUCK_GATEWAY_TOKEN}", tuckTurnHeader: "${headers." + tuckTurnHeader + "}"}}}
	if readonly {
		v["readonly"] = true
	}
	return v
}
