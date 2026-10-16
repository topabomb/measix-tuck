package tuck

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func (a *App) settingsRoute(w http.ResponseWriter, r *http.Request) error {
	path := r.URL.Path
	send := func(value M, e error) error {
		if e != nil {
			return e
		}
		WriteJSON(w, 200, value)
		return nil
	}
	switch path {
	case "/api/settings/access":
		if r.Method != "GET" {
			break
		}
		return send(a.Protection.Info(r))
	case "/api/settings/unlock":
		if r.Method != "POST" {
			break
		}
		data, e := ReadJSON(r.Body, 4096)
		if e != nil {
			return e
		}
		return send(a.Protection.Unlock(r, w, data))
	case "/api/settings/lock":
		if r.Method != "POST" {
			break
		}
		return send(a.Protection.Lock(r, w))
	case "/api/settings/protection":
		if e := a.Protection.Require(r, "protection"); e != nil {
			return e
		}
		if r.Method == "GET" {
			info, e := a.Protection.Info(r)
			if e != nil {
				return e
			}
			_, revision, e := a.Config.Read()
			if e != nil {
				return e
			}
			info["revision"] = revision
			return send(info, nil)
		}
		if r.Method != "PUT" {
			break
		}
		if e := a.Protection.sameOrigin(r); e != nil {
			return e
		}
		data, e := ReadJSON(r.Body, 4096)
		if e != nil {
			return e
		}
		if !a.changeMu.TryLock() {
			return Problem(409, "configuration_changing", "配置正在应用，请稍后重试")
		}
		defer a.changeMu.Unlock()
		next, revision, e := a.Protection.Prepare(data)
		if e != nil {
			return e
		}
		if e = a.Protection.Require(r, "protection"); e != nil {
			return e
		}
		pending := a.Config.Pending()
		ownRevision, saveErr := a.Config.Save(revision, next)
		if saveErr != nil {
			return saveErr
		}
		if !pending {
			a.Config.MarkAppliedVersion(next, ownRevision)
			if e = a.Models.SetConfig(next); e != nil {
				return e
			}
		}
		a.Protection.Invalidate()
		return send(a.Protection.Lock(r, w))
	case "/api/settings/runtime":
		section := r.URL.Query().Get("section")
		if section != "" {
			if _, ok := runtimeFields[section]; !ok {
				return Problem(400, "invalid_section", "设置分类无效")
			}
		}
		if r.Method == "GET" {
			if section != "" {
				if e := a.Protection.Require(r, section); e != nil {
					return e
				}
			} else {
				for key := range runtimeFields {
					if e := a.Protection.Require(r, key); e != nil {
						return e
					}
				}
			}
			return send(a.runtimeSettings(r))
		}
		if r.Method != "PUT" {
			break
		}
		data, e := ReadJSON(r.Body, 2<<20)
		if e != nil {
			return e
		}
		check := func() error {
			if section != "" {
				if e := a.Protection.Require(r, section); e != nil {
					return e
				}
			}
			for key, fields := range runtimeFields {
				for _, field := range fields {
					if Has(O(data["config"]), field) {
						if e := a.Protection.Require(r, key); e != nil {
							return e
						}
						break
					}
				}
			}
			return nil
		}
		if e = check(); e != nil {
			return e
		}
		if e = a.saveRuntime(r, data, check); e != nil {
			return e
		}
		return send(a.runtimeSettings(r))
	case "/api/settings/model-test":
		if r.Method != "POST" {
			break
		}
		data, e := ReadJSON(r.Body, 64<<10)
		if e != nil {
			return e
		}
		if e = a.Protection.Require(r, "providers"); e != nil {
			return e
		}
		result, e := VerifyModel(r.Context(), a.Config, data)
		if e != nil {
			return e
		}
		if e = a.Protection.Require(r, "providers"); e != nil {
			return e
		}
		return send(result, nil)
	case "/api/settings/tools":
		if r.Method != "POST" {
			break
		}
		data, e := ReadJSON(r.Body, 64<<10)
		if e != nil {
			return e
		}
		section := "mcp"
		if S(data["kind"]) == "builtin" {
			section = "agents"
		}
		if e = a.Protection.Require(r, section); e != nil {
			return e
		}
		result, e := DiscoverTools(r.Context(), a, data)
		if e != nil {
			return e
		}
		if e = a.Protection.Require(r, section); e != nil {
			return e
		}
		return send(result, nil)
	case "/api/settings/orchestration":
		if e := a.Protection.Require(r, "orchestration"); e != nil {
			return e
		}
		if r.Method == "PUT" {
			data, e := ReadJSON(r.Body, 4096)
			if e != nil {
				return e
			}
			if !noUnknown(data, "requireDelivery") || !isBool(data["requireDelivery"]) {
				return Problem(400, "invalid_orchestration", "交付要求必须为布尔值")
			}
			if !a.changeMu.TryLock() {
				return Problem(409, "configuration_changing", "配置正在应用，请稍后重试")
			}
			defer a.changeMu.Unlock()
			current, revision, e := a.Config.Read()
			if e != nil {
				return e
			}
			if e = a.Protection.Require(r, "orchestration"); e != nil {
				return e
			}
			O(current["workbench"])["requireDelivery"] = data["requireDelivery"]
			O(current["workbench"])["updatedAt"] = max(N(O(current["workbench"])["updatedAt"])+1, float64(NowMS()))
			pending := a.Config.Pending()
			ownRevision, saveErr := a.Config.Save(revision, current)
			if saveErr != nil {
				return saveErr
			}
			if !pending {
				a.Config.MarkAppliedVersion(current, ownRevision)
			}
			applied := a.Models.Config()
			O(applied["workbench"])["requireDelivery"] = data["requireDelivery"]
			O(applied["workbench"])["updatedAt"] = O(current["workbench"])["updatedAt"]
			if e = a.Models.SetConfig(applied); e != nil {
				return e
			}
			if !B(data["requireDelivery"]) {
				for _, task := range a.Manager.List() {
					if _, e = a.Manager.Update(S(task["id"]), func(t M) error {
						queue := []any{}
						for _, item := range A(t["queue"]) {
							if S(O(O(item)["source"])["kind"]) != "delivery_required" {
								queue = append(queue, item)
							}
						}
						t["queue"] = queue
						t["queuedPrompts"] = len(queue)
						return nil
					}); e != nil {
						return e
					}
				}
			}
			if e = a.Manager.PublishGlobal(M{"type": "orchestration", "orchestration": a.orchestration()}); e != nil {
				return e
			}
		} else if r.Method != "GET" {
			break
		}
		return send(a.orchestration(), nil)
	}
	if strings.HasPrefix(path, "/api/settings/engine") {
		return a.engineSettings(w, r)
	}
	return Problem(405, "method_not_allowed", "Method not allowed")
}
func (a *App) saveRuntime(r *http.Request, data M, check func() error) error {
	if a.Engine.Options().External {
		return Problem(409, "external_engine", "外部 Docker Agent 的配置由宿主管理")
	}
	if !a.changeMu.TryLock() {
		return Problem(409, "configuration_changing", "配置正在应用，请稍后重试")
	}
	defer a.changeMu.Unlock()
	locked, e := a.locked(r)
	if e != nil {
		return e
	}
	next, previous, revision, e := PrepareRuntime(a.Config, a.Models, data, locked)
	if e != nil {
		return e
	}
	for _, v := range A(O(next["workbench"])["assistants"]) {
		workspace := O(O(v)["workspace"])
		if S(workspace["mode"]) == "specified" {
			if e = a.Delivery.CheckWorkspace(S(workspace["directory"])); e != nil {
				return e
			}
		}
	}
	if e = check(); e != nil {
		return e
	}
	if B(data["defer"]) {
		_, e = a.Config.Save(revision, next)
		return e
	}
	if e = a.Config.RequireRuntimeCompatible(next); e != nil {
		return e
	}
	a.changing.Store(true)
	a.Manager.SetConfigurationChanging(true)
	defer func() { a.Workspace.Invalidate(); a.changing.Store(false); a.Manager.SetConfigurationChanging(false) }()
	if a.Manager.Active() && !B(data["interrupt"]) {
		return Problem(409, "settings_interrupt_required", "有任务正在执行或等待交互。可中断任务并立即生效，也可保存到下一次应用。")
	}
	a.Workspace.Close()
	for _, task := range a.Manager.List() {
		if a.Manager.Running(S(task["id"])) || S(task["status"]) != "idle" {
			if e = a.Manager.Abort(r.Context(), S(task["id"])); e != nil {
				return e
			}
		}
	}
	if e = check(); e != nil {
		return e
	}
	previousApplied := a.Models.Config()
	ownRevision, e := a.Config.Save(revision, next)
	if e != nil {
		return e
	}
	previousEngine := a.Engine.Options()
	applyErr := func() error {
		if e := a.Models.SetConfig(next); e != nil {
			return e
		}
		a.Models.MCP.Prepare(r.Context(), false)
		if e := a.Engine.Close(); e != nil {
			return e
		}
		if e := a.configureEngine(); e != nil {
			return e
		}
		if e := a.Engine.Start(r.Context()); e != nil {
			return e
		}
		if _, e := a.Engine.Request(r.Context(), "GET", "/api/agents/agent", nil); e != nil {
			return Problem(422, "native_config_invalid", "配置未通过原生加载校验，已恢复原配置")
		}
		return check()
	}()
	if applyErr != nil {
		restoreErr := a.Config.Restore(ownRevision, previous)
		_ = a.Models.SetConfig(previousApplied)
		_ = a.Engine.Close()
		_ = a.Engine.Configure(previousEngine)
		if _, e = a.Models.WriteConfig(); e != nil {
			return e
		}
		restartCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if e = a.Engine.Start(restartCtx); e != nil {
			return Problem(503, "configuration_restore_failed", "配置应用失败，运行服务未就绪，请检查本机服务")
		}
		if restoreErr != nil {
			return Problem(409, "settings_changed", "应用失败；配置已被外部修改，保留外部文件，请重新读取")
		}
		return applyErr
	}
	a.Config.MarkAppliedVersion(next, ownRevision)
	return a.publishConnection()
}
func (a *App) serveMessageMedia(w http.ResponseWriter, r *http.Request, media M) error {
	raw, ok := media["bytes"].([]byte)
	if !ok {
		return Problem(404, "media_not_found", "Message media not found")
	}
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("ETag", S(media["etag"]))
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
	if r.Header.Get("If-None-Match") == S(media["etag"]) {
		w.WriteHeader(304)
		return nil
	}
	mime := S(media["mimeType"])
	disposition := "attachment"
	if strings.HasPrefix(mime, "image/") || strings.HasPrefix(mime, "audio/") || strings.HasPrefix(mime, "video/") {
		disposition = "inline"
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Length", fmt.Sprint(len(raw)))
	w.Header().Set("Content-Disposition", disposition+"; filename*=UTF-8''"+url.PathEscape(S(media["name"])))
	w.WriteHeader(200)
	_, e := w.Write(raw)
	return e
}
func assistantFingerprint(config M) string {
	c := Clone(config)
	delete(c, "security")
	return Hash(JSON(c))
}
func (a *App) assistantSummary(w http.ResponseWriter, r *http.Request, task M) error {
	if a.changing.Load() {
		return Problem(409, "configuration_changing", "配置正在应用，请稍后读取助手信息")
	}
	config := a.Models.Config()
	fingerprint := assistantFingerprint(config)
	agent := S(task["agent"])
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	var count any
	var toolsError any
	native, e := a.Engine.Request(ctx, "GET", "/api/agents/agent/"+url.PathEscape(agent)+"/tools/count", nil)
	if e == nil && Integer(native["available_tools"], 0, 9007199254740991) {
		count = native["available_tools"]
	} else {
		toolsError = "原生工具数量未读取成功"
	}
	if r.Context().Err() != nil {
		return nil
	}
	if _, e = a.Security.Authorize(r); e != nil {
		return e
	}
	if a.changing.Load() || assistantFingerprint(a.Models.Config()) != fingerprint {
		return Problem(409, "configuration_changing", "助手配置已变化，请重新打开面板")
	}
	locked, e := a.locked(r)
	if e != nil {
		return e
	}
	settingsLocked := Contains(locked, "agents")
	mcpLocked := settingsLocked || Contains(locked, "mcp")
	value := M{"settingsLocked": settingsLocked, "mcpLocked": mcpLocked, "availableTools": count, "toolsError": toolsError, "observedAt": NowMS()}
	aConfig := O(O(config["agents"])[agent])
	if len(aConfig) > 0 && !settingsLocked {
		defaultModel := "沿用接入默认"
		if S(aConfig["modelId"]) != "" {
			defaultModel = S(a.Models.find(S(aConfig["modelId"]))["displayName"])
			if defaultModel == "" {
				defaultModel = "配置的模型未在当前目录中"
			}
		}
		preview := []rune(S(aConfig["instruction"]))
		preview = preview[:min(400, len(preview))]
		value["settings"] = M{"readonly": B(aConfig["readonly"]), "maxIterations": aConfig["max_iterations"], "subAgents": Strings(aConfig["sub_agents"]), "defaultModel": defaultModel, "instructionPreview": string(preview)}
	}
	if !mcpLocked {
		value["mcp"] = a.Models.MCP.AssistantSummary(agent)
	}
	WriteJSON(w, 200, value)
	return nil
}
