package tuck

import (
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func localProvider(origin string) M {
	return M{"id": "configured", "displayName": "Configured", "enabled": true, "protocol": "OPENAI_CHAT_COMPLETIONS", "baseUrl": origin + "/v1", "apiKey": "synthetic-provider-key", "models": []any{M{"modelId": "proof-model", "upstreamModelKey": "upstream-proof", "displayName": "Proof", "enabled": true, "inputModalities": []any{"TEXT"}, "capabilities": []any{"TOOL", "REASONING"}, "contextWindow": 32768, "maxOutputTokens": 2048, "temperature": 0.3, "topP": 0.8}}}
}

type runtimeTransport func(*http.Request) (*http.Response, error)

func (fn runtimeTransport) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func TestOfficialDeliveryNativeMCP(t *testing.T) {
	f := newPlatformFixture(t, 5)
	c := f.models.Config()
	c["providers"] = []any{localProvider(f.server.URL)}
	c["defaultModelId"] = "proof-model"
	f.save(c)
	f.change(func() {
		f.controls["chatTool"] = "deliver_result"
		f.controls["chatArguments"] = M{"summary": "Native result", "files": []any{M{"path": "result.txt", "description": "Delivered result"}}}
	})
	app := f.startApp()
	task, e := app.Manager.Create(context.Background(), M{"approvalMode": "auto"})
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(S(task["workspace"]), "result.txt"), []byte("original result"), 0600); e != nil {
		t.Fatal(e)
	}
	runAppTask(t, app, S(task["id"]), "Deliver the result")
	task = mustTask(t, app.Manager, S(task["id"]))
	latest := O(task["latestDelivery"])
	if latest["summary"] != "Native result" || I(latest["version"]) != 1 {
		messages, _ := app.Store.Messages(S(task["id"]), 0, 100)
		t.Fatal("native delivery missing", messages)
	}
	runAppTask(t, app, S(task["id"]), "Deliver the result again")
	task = mustTask(t, app.Manager, S(task["id"]))
	if I(O(task["latestDelivery"])["version"]) != 2 {
		t.Fatal("shared MCP session could not bind next turn", task)
	}
}

func TestOfficialWorkspaceNativeMCP(t *testing.T) {
	f := newWorkspaceFixture(t)
	app := f.p.startApp()
	task, e := app.Manager.Create(context.Background(), M{"modelId": f.p.models.Catalog()[0]["modelId"], "approvalMode": "auto"})
	if e != nil {
		t.Fatal(e)
	}
	id := S(task["id"])
	f.p.change(func() {
		f.p.controls["chatTool"] = "download_runcove_files"
		f.p.controls["chatArguments"] = M{"remote_paths": []any{"hello.txt"}, "local_directory": "native-download"}
	})
	runAppTask(t, app, id, "Download enterprise file")
	local := "native-download/hello.txt"
	if raw, e := os.ReadFile(filepath.Join(S(task["workspace"]), local)); e != nil || string(raw) != "hello world" {
		t.Fatal("native WebDAV download failed", e, string(raw))
	}
	f.p.change(func() {
		f.p.controls["chatTool"] = "upload_runcove_file"
		f.p.controls["chatArguments"] = M{"file_path": local, "remote_path": "native-upload/hello.txt"}
	})
	runAppTask(t, app, id, "Upload enterprise result")
	f.mu.Lock()
	raw := string(f.files["native-upload/hello.txt"])
	f.mu.Unlock()
	if raw != "hello world" {
		t.Fatal("native WebDAV upload failed", raw)
	}
}

func TestLocalMCPTaskTurnOwnership(t *testing.T) {
	f := newWorkspaceFixture(t)
	app := f.p.startApp()
	var tasks []M
	var keys []string
	var finishes []func()
	for i := 0; i < 2; i++ {
		task, e := app.Manager.Create(context.Background(), M{"modelId": f.p.models.Catalog()[0]["modelId"]})
		if e != nil {
			t.Fatal(e)
		}
		task, e = app.Manager.Update(S(task["id"]), func(v M) error { v["turnId"] = fmt.Sprintf("test-turn-%d", i); return nil })
		if e != nil {
			t.Fatal(e)
		}
		h, finish, e := app.Local.Begin(context.Background(), task)
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(finish)
		tasks = append(tasks, task)
		keys = append(keys, h["X-Tuck-Workspace-Turn"])
		finishes = append(finishes, finish)
	}
	send := func(session, key, method string, params M) (*http.Response, error) {
		params = Merge(params, M{"_meta": M{"io.modelcontextprotocol/protocolVersion": "2026-07-28", "io.modelcontextprotocol/clientCapabilities": M{}, "io.modelcontextprotocol/clientInfo": M{"name": "ownership", "version": "1"}}})
		r, _ := http.NewRequest("POST", f.p.models.URL+"/workspace-tools", strings.NewReader(string(JSON(M{"jsonrpc": "2.0", "id": float64(1), "method": method, "params": params}))))
		r.Header.Set("Authorization", "Bearer "+f.p.models.Secret)
		r.Header.Set("X-Tuck-Workspace-Turn", key)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json, text/event-stream")
		r.Header.Set("Mcp-Protocol-Version", "2026-07-28")
		r.Header.Set("Mcp-Method", method)
		if method == "tools/call" {
			r.Header.Set("Mcp-Name", S(params["name"]))
		}
		if session != "" {
			r.AddCookie(&http.Cookie{Name: "tuck-workspace-catalog", Value: session})
		}
		return http.DefaultClient.Do(r)
	}
	r, e := send("", keys[0], "server/discover", M{})
	if e != nil {
		t.Fatal(e)
	}
	session := ""
	for _, cookie := range r.Cookies() {
		if cookie.Name == "tuck-workspace-catalog" {
			session = cookie.Value
		}
	}
	if _, e = readRPC(r, float64(1)); e != nil || session == "" {
		t.Fatal(e, "catalog client binding missing")
	}
	params := M{"name": "download_runcove_files", "arguments": M{"remote_paths": []any{"hello.txt"}, "local_directory": "ownership"}}
	r, e = send(session, keys[1], "tools/call", params)
	if e != nil {
		t.Fatal(e)
	}
	v, e := readRPC(r, float64(1))
	if e != nil || !B(O(v["result"])["isError"]) || !strings.Contains(string(JSON(v)), "tool_not_permitted") {
		t.Fatal("foreign task entered session", v, e)
	}
	finishes[0]()
	r, e = send(session, keys[0], "tools/call", params)
	if e != nil {
		t.Fatal(e)
	}
	bytes, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if r.StatusCode < 400 && !strings.Contains(string(bytes), "tool_not_permitted") {
		t.Fatal("ended turn accepted", r.StatusCode, string(bytes))
	}
	for _, task := range tasks {
		if _, e := os.Stat(filepath.Join(S(task["workspace"]), "ownership")); !os.IsNotExist(e) {
			t.Fatal("invalid call wrote files", e)
		}
	}
}

func TestOfficialAssistantSummary(t *testing.T) {
	f := newPlatformFixture(t, 5)
	c := f.models.Config()
	c["providers"] = []any{localProvider(f.server.URL)}
	c["defaultModelId"] = "proof-model"
	c["localMcp"] = []any{M{"name": "proof", "displayName": "Selected MCP", "url": f.server.URL + "/mcp", "enabled": true, "agents": []any{"root"}, "tools": []any{"search_records"}, "authToken": "synthetic-mcp-secret"}, M{"name": "unselected", "displayName": "Other assistant MCP", "url": f.server.URL + "/mcp", "enabled": true, "agents": []any{"worker"}}, M{"name": "disabled", "displayName": "Disabled MCP", "url": f.server.URL + "/mcp", "enabled": false, "agents": []any{"root"}}}
	f.save(c)
	app := f.startApp()
	task, e := app.Manager.Create(context.Background(), M{})
	if e != nil {
		t.Fatal(e)
	}
	path := "/api/tasks/" + S(task["id"]) + "/assistant-summary"
	native, e := app.Engine.Request(context.Background(), "GET", "/api/agents/agent/root/tools/count", nil)
	if e != nil {
		t.Fatal(e)
	}
	status, summary := appRequest(t, app, "GET", path, nil)
	if status != 200 || I(summary["availableTools"]) <= 0 || N(summary["availableTools"]) != N(native["available_tools"]) {
		t.Fatal(summary, native)
	}
	items := A(summary["mcp"])
	if len(items) != 2 || O(items[0])["displayName"] != "Selected MCP" || I(O(items[0])["toolCount"]) != 1 || O(items[0])["status"] != "ready" || O(items[1])["toolCount"] != nil {
		t.Fatal(items)
	}
	if strings.Contains(string(JSON(summary)), f.server.URL) || strings.Contains(string(JSON(summary)), "synthetic-mcp-secret") {
		t.Fatal("summary disclosed credentials")
	}
	_, pending := appRequest(t, app, "GET", "/api/settings/runtime", nil)
	O(O(O(pending["config"])["agents"])["root"])["max_iterations"] = 7
	pending["defer"] = true
	if status, value := appRequest(t, app, "PUT", "/api/settings/runtime", pending); status != 200 {
		t.Fatal(value)
	}
	_, summary = appRequest(t, app, "GET", path, nil)
	if I(O(summary["settings"])["maxIterations"]) == 7 {
		t.Fatal("deferred config leaked into running summary")
	}
	original := app.Engine.client.Transport
	app.Engine.client.Transport = runtimeTransport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/tools/count") {
			return nil, fmt.Errorf("synthetic-mcp-secret")
		}
		return original.RoundTrip(r)
	})
	_, summary = appRequest(t, app, "GET", path, nil)
	if summary["availableTools"] != nil || summary["toolsError"] != "原生工具数量未读取成功" || strings.Contains(string(JSON(summary)), "synthetic-mcp-secret") {
		t.Fatal(summary)
	}
	app.Engine.client.Transport = original
	saveApp(t, app, func(c M) { O(O(c["agents"])["root"])["max_iterations"] = 8 })
	entered, release := make(chan struct{}), make(chan struct{})
	app.Engine.client.Transport = runtimeTransport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/tools/count") {
			close(entered)
			<-release
		}
		return original.RoundTrip(r)
	})
	done := make(chan M, 1)
	go func() { status, v := appRequest(t, app, "GET", path, nil); v["httpStatus"] = status; done <- v }()
	<-entered
	_, protect := appRequest(t, app, "GET", "/api/settings/protection", nil)
	status, value := appRequest(t, app, "PUT", "/api/settings/protection", M{"revision": protect["revision"], "password": "synthetic-lock-password", "categories": []any{"agents", "mcp"}})
	close(release)
	if status != 200 {
		t.Fatal(value)
	}
	summary = <-done
	app.Engine.client.Transport = original
	if I(summary["httpStatus"]) != 200 || !B(summary["settingsLocked"]) || !B(summary["mcpLocked"]) || Has(summary, "settings") || Has(summary, "mcp") || I(summary["availableTools"]) <= 0 {
		t.Fatal(summary)
	}
	if status, _ := appRequest(t, app, "GET", "/api/settings/runtime?section=agents", nil); status != 403 {
		t.Fatal(status)
	}
}
func generatedConfig(t *testing.T, m *Models) M {
	t.Helper()
	path, e := m.WriteConfig()
	if e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	var config M
	if e = yaml.Unmarshal(raw, &config); e != nil {
		t.Fatal(e)
	}
	return config
}
func runtimeData(t *testing.T, f *platformFixture) M {
	t.Helper()
	v, e := RuntimeView(f.config, f.models, nil)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func prepareData(t *testing.T, f *platformFixture, v M) M {
	t.Helper()
	next, _, _, e := PrepareRuntime(f.config, f.models, v, nil)
	if e != nil {
		t.Fatal(e)
	}
	return next
}

func TestModelSourcesDefaultsAndBindings(t *testing.T) {
	f := newPlatformFixture(t, 5)
	dir := filepath.Dir(f.config.Path)
	legacy := filepath.Join(dir, "supplier-keys.env")
	content := []byte("OPENAI_API_KEY=synthetic-unused-key\n")
	if e := os.WriteFile(legacy, content, 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(dir, "model-defaults.json"), []byte("{invalid obsolete data"), 0600); e != nil {
		t.Fatal(e)
	}
	if len(f.models.Catalog()) != 0 || f.models.Summary()["mode"] != "template" {
		t.Fatal("legacy created model")
	}
	c := f.models.Config()
	c["providers"] = []any{localProvider("http://127.0.0.1:1")}
	f.save(c)
	catalog := f.models.Catalog()
	if len(catalog) != 1 || catalog[0]["source"] != "local" || I(catalog[0]["maxOutputTokens"]) != 2048 {
		t.Fatal(catalog)
	}
	raw, _ := os.ReadFile(legacy)
	if !reflect.DeepEqual(raw, content) {
		t.Fatal("legacy file touched")
	}
	if picked, e := f.models.PickForAgent("root", nil); e != nil || picked["model"] != nil {
		t.Fatal(picked, e)
	}
	for _, id := range []any{"", " ", " model", 12, []any{}} {
		if _, e := f.models.PickForAgent("root", id); Status(e) != 422 {
			t.Fatal(id, e)
		}
	}
	c["providers"] = []any{}
	f.save(c)
	if len(f.models.Catalog()) != 0 {
		t.Fatal("empty providers retained local model")
	}
	f.enroll()
	a, b := f.models.Catalog()[0], f.models.Catalog()[1]
	c = f.models.Config()
	O(O(c["agents"])["root"])["modelId"] = b["modelId"]
	O(O(c["agents"])["worker"])["modelId"] = a["modelId"]
	O(O(c["agents"])["reviewer"])["modelId"] = "missing-model"
	if e := f.models.SetConfig(c); e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		agent string
		id    any
		want  any
	}{{"root", nil, b["modelId"]}, {"root", a["modelId"], a["modelId"]}, {"worker", nil, a["modelId"]}, {"image", nil, a["modelId"]}} {
		picked, e := f.models.PickForAgent(tc.agent, tc.id)
		if e != nil || picked["modelId"] != tc.want {
			t.Fatal(tc, picked, e)
		}
	}
	if _, e := f.models.PickForAgent("reviewer", nil); Code(e) != "model_unavailable" || !strings.Contains(e.Error(), "agents.reviewer.modelId") {
		t.Fatal(e)
	}
	y := generatedConfig(t, f.models)
	if O(O(y["agents"])["reviewer"])["model"] != "not_configured" {
		t.Fatal(y)
	}
	for _, v := range O(y["agents"]) {
		if Has(O(v), "modelId") {
			t.Fatal("product selector leaked to engine")
		}
	}
	status, _, _ := f.send("/v1/chat/completions", M{"model": "not-configured", "messages": []any{}})
	if status != 428 {
		t.Fatal(status)
	}
	task := M{"modelId": a["modelId"], "modelSource": "platform", "reasoningEffort": "default"}
	if _, e := f.models.Bind(task); e != nil {
		t.Fatal(e)
	}
	copy := Clone(task)
	if _, e := f.models.Bind(M{"modelId": a["modelId"]}); Code(e) != "model_source_required" {
		t.Fatal(e)
	}
	c = f.models.Config()
	c["providers"] = []any{localProvider("http://127.0.0.1:1")}
	O(A(O(A(c["providers"])[0])["models"])[0])["modelId"] = a["modelId"]
	if e := f.models.SetConfig(c); e != nil {
		t.Fatal(e)
	}
	if B(f.models.find(S(a["modelId"]))["selectable"]) {
		t.Fatal("collision selectable")
	}
	if _, e := f.models.Bind(task); Code(e) != "model_unavailable" {
		t.Fatal(e)
	}
	d := f.platform.Data()
	O(d["binding"])["snapshot"] = nil
	if e := f.platform.persist(d); e != nil {
		t.Fatal(e)
	}
	if e := f.models.Rebuild(); e != nil {
		t.Fatal(e)
	}
	if _, e := f.models.Bind(task); Code(e) != "model_source_changed" {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(copy, task) {
		t.Fatal("binding silently changed task")
	}
}

func TestRuntimeWriteOnlyAndValidation(t *testing.T) {
	f := newPlatformFixture(t, 5)
	c := f.models.Config()
	c["providers"] = []any{localProvider("http://127.0.0.1:1")}
	c["localMcp"] = []any{M{"name": "proof", "url": "http://127.0.0.1:1/mcp", "enabled": true, "agents": []any{"root", "reviewer"}, "authToken": "synthetic-mcp-key", "tools": []any{"echo"}}}
	c["defaultModelId"] = "proof-model"
	f.save(c)
	view := runtimeData(t, f)
	text := string(JSON(view))
	if strings.Contains(text, "synthetic-provider-key") || strings.Contains(text, "synthetic-mcp-key") || !B(O(A(O(view["config"])["providers"])[0])["hasApiKey"]) || !B(O(A(O(view["config"])["localMcp"])[0])["hasAuthToken"]) {
		t.Fatal("write-only secrets leaked/lost")
	}
	next := prepareData(t, f, view)
	if O(A(next["providers"])[0])["apiKey"] != "synthetic-provider-key" || O(A(next["localMcp"])[0])["authToken"] != "synthetic-mcp-key" {
		t.Fatal("masked save lost secret")
	}
	for _, change := range []func(M){func(v M) { O(A(v["providers"])[0])["id"] = "renamed" }, func(v M) { O(A(v["localMcp"])[0])["name"] = "renamed" }, func(v M) { O(A(v["localMcp"])[0])["authTokenEnv"] = "LEGACY" }, func(v M) { O(O(v["agents"])["root"])["modelId"] = "missing" }, func(v M) { O(O(v["agents"])["worker"])["sub_agents"] = []any{"worker"} }, func(v M) { O(O(v["agents"])["root"])["toolsets"] = []any{M{"type": "filesystem", "allow_list": 123}} }} {
		bad := Clone(view)
		change(O(bad["config"]))
		if _, _, _, e := PrepareRuntime(f.config, f.models, bad, nil); Status(e) != 422 {
			t.Fatalf("invalid accepted %s: %v", JSON(bad), e)
		}
	}
	future := Clone(view)
	O(O(O(future["config"])["agents"])["root"])["toolsets"] = []any{M{"type": "future-native-tool", "tools": []any{"future_action"}, "future_option": true}}
	prepareData(t, f, future)
	locked, e := RuntimeView(f.config, f.models, []string{"providers", "mcp"})
	if e != nil {
		t.Fatal(e)
	}
	if Has(O(locked["config"]), "providers") || Has(O(locked["config"]), "localMcp") || Has(locked, "localMcpState") {
		t.Fatal("locked fields shown")
	}
	if _, _, _, e = PrepareRuntime(f.config, f.models, view, []string{"providers"}); Status(e) != 403 {
		t.Fatal(e)
	}
	fresh := Clone(view)
	O(A(O(fresh["config"])["providers"])[0])["apiKey"] = ""
	O(A(O(fresh["config"])["localMcp"])[0])["authToken"] = ""
	f.save(prepareData(t, f, fresh))
	after := runtimeData(t, f)
	if B(O(A(O(after["config"])["providers"])[0])["hasApiKey"]) || B(O(A(O(after["config"])["localMcp"])[0])["hasAuthToken"]) {
		t.Fatal("explicit credential reset ignored")
	}
	if _, _, _, e = PrepareRuntime(f.config, f.models, view, nil); Status(e) != 409 {
		t.Fatal("stale revision accepted", e)
	}
}

func TestNativeConfigLimitsAndSelections(t *testing.T) {
	f := newPlatformFixture(t, 5)
	c := f.models.Config()
	c["providers"] = []any{localProvider("http://127.0.0.1:1")}
	c["defaultModelId"] = "proof-model"
	c["modelDefaults"] = M{"contextWindow": 131072, "maxOutputTokens": 16384}
	c["localMcp"] = []any{M{"name": "synthetic", "url": "http://127.0.0.1:1/mcp", "enabled": true, "agents": []any{"root", "reviewer"}, "authToken": "synthetic-secret"}}
	root := O(O(c["agents"])["root"])
	root["instruction"] = "A custom agent"
	root["max_iterations"] = 0
	for _, threshold := range []any{nil, 0, 8} {
		if threshold == nil {
			delete(root, "max_consecutive_tool_calls")
		} else {
			root["max_consecutive_tool_calls"] = threshold
		}
		if e := ValidateConfig(c, false); e != nil {
			t.Fatal(e)
		}
		if e := f.models.SetConfig(c); e != nil {
			t.Fatal(e)
		}
		y := generatedConfig(t, f.models)
		a := O(O(y["agents"])["root"])
		if I(a["max_iterations"]) != 0 || a["instruction"] != "A custom agent" || !reflect.DeepEqual(a["max_consecutive_tool_calls"], threshold) {
			t.Fatal(a)
		}
	}
	for _, field := range []string{"max_iterations", "max_consecutive_tool_calls"} {
		for _, value := range []any{-1, .5, "0", nil, math.NaN(), 9007199254740992.0} {
			bad := Clone(c)
			O(O(bad["agents"])["root"])[field] = value
			if e := ValidateConfig(bad, false); e == nil {
				t.Fatal("invalid native limit", field, value)
			}
		}
	}
	for _, value := range []any{"", " ", " model", "model name", nil, []any{}, 12} {
		bad := Clone(c)
		O(O(bad["agents"])["root"])["modelId"] = value
		if e := ValidateConfig(bad, false); e == nil {
			t.Fatal("invalid model ID", value)
		}
	}
	entry := O(A(c["localMcp"])[0])
	names := []any{"firecrawl_search", "firecrawl_developer_search", "firecrawl_scrape"}
	entry["tools"] = names
	f.save(c)
	toolsets := f.models.MCP.Toolsets("root", false)
	if len(toolsets) != 1 || !reflect.DeepEqual(A(O(toolsets[0])["tools"]), []any{"synthetic_firecrawl_search", "synthetic_firecrawl_developer_search", "synthetic_firecrawl_scrape"}) || strings.Contains(string(JSON(toolsets)), "synthetic-secret") {
		t.Fatal(toolsets)
	}
	if !B(O(f.models.MCP.Toolsets("reviewer", true)[0])["readonly"]) {
		t.Fatal("readonly binding lost")
	}
	entry["tools"] = []any{}
	f.save(c)
	if len(f.models.MCP.Toolsets("root", false)) != 0 {
		t.Fatal("empty selection not disabled")
	}
	delete(entry, "tools")
	entry["agents"] = []any{}
	f.save(c)
	if len(f.models.MCP.Toolsets("root", false)) != 0 || len(A(f.models.Config()["localMcp"])) != 1 {
		t.Fatal("unassigned registration lost")
	}
	entry["agents"] = []any{"root"}
	f.save(c)
	if len(f.models.MCP.Toolsets("root", false)) != 1 {
		t.Fatal("rebind failed")
	}
	for _, selection := range []any{[]any{""}, []any{" firecrawl_search"}, []any{"x", "x"}, []any{"firecrawl_*"}, "firecrawl_search", nil} {
		bad := Clone(c)
		O(A(bad["localMcp"])[0])["tools"] = selection
		if e := ValidateConfig(bad, false); e == nil {
			t.Fatal("invalid MCP selection accepted")
		}
	}
	for _, legacy := range []any{"TOKEN", "", nil} {
		bad := Clone(c)
		O(A(bad["localMcp"])[0])["authTokenEnv"] = legacy
		if e := ValidateConfig(bad, false); e == nil || !strings.Contains(e.Error(), "authTokenEnv") {
			t.Fatal(e)
		}
	}
}

func TestRuntimePlatformBudgetsAndDefaults(t *testing.T) {
	f := newPlatformFixture(t, 5)
	f.enroll()
	id := S(f.models.Catalog()[0]["modelId"])
	key := f.models.DefaultsKey(id, "platform")
	c := f.models.Config()
	c["platformModelSettings"] = M{key: M{"contextWindow": 131072, "maxOutputTokens": 16384}}
	f.save(c)
	model := f.models.find(id)
	if I(model["contextWindow"]) != 131072 || I(model["maxOutputTokens"]) != 16384 || model["settingsSource"] != "local" {
		t.Fatal(model)
	}
	restored, e := NewModels(f.config, f.store, f.platform)
	if e != nil {
		t.Fatal(e)
	}
	defer restored.Close(context.Background())
	if I(restored.find(id)["maxOutputTokens"]) != 16384 {
		t.Fatal("settings not restored")
	}
	c["platformModelSettings"] = M{}
	f.save(c)
	if I(f.models.find(id)["maxOutputTokens"]) != I(O(c["modelDefaults"])["maxOutputTokens"]) {
		t.Fatal("explicit reset ignored")
	}
	d := f.platform.Data()
	O(A(O(O(d["binding"])["snapshot"])["models"])[0])["contextWindow"] = 65536
	if e = f.platform.persist(d); e != nil {
		t.Fatal(e)
	}
	if e = f.models.Rebuild(); e != nil {
		t.Fatal(e)
	}
	v := runtimeData(t, f)
	O(O(v["config"])["platformModelSettings"])[key] = M{"contextWindow": 131072}
	if _, _, _, e = PrepareRuntime(f.config, f.models, v, nil); Status(e) != 422 {
		t.Fatal("managed budget overwritten", e)
	}
	c = f.models.Config()
	c["providers"] = []any{localProvider("http://127.0.0.1:1")}
	f.save(c)
	v = runtimeData(t, f)
	O(v["config"])["defaultModelId"] = "proof-model"
	f.save(prepareData(t, f, v))
	picked, e := f.models.PickForAgent("root", nil)
	if e != nil || picked["modelId"] != "proof-model" {
		t.Fatal(picked, e)
	}
	v = runtimeData(t, f)
	O(v["config"])["defaultModelId"] = id
	f.save(prepareData(t, f, v))
	picked, e = f.models.PickForAgent("root", nil)
	if e != nil || picked["modelId"] != id {
		t.Fatal(picked, e)
	}
	v = runtimeData(t, f)
	O(v["config"])["defaultModelId"] = "missing"
	if _, _, _, e = PrepareRuntime(f.config, f.models, v, nil); Status(e) != 422 {
		t.Fatal("missing default saved", e)
	}
}

func TestModelAndMCPProbes(t *testing.T) {
	f := newPlatformFixture(t, 5)
	requests := []M{}
	status := 200
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := ReadJSON(r.Body, 1<<20)
		requests = append(requests, M{"path": r.URL.Path, "body": body, "authorization": r.Header.Get("Authorization")})
		if status != 200 {
			WriteJSON(w, status, M{})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/responses") {
			WriteJSON(w, 200, M{"output": []any{M{"content": []any{M{"type": "output_text", "text": "OK"}}}}})
			return
		}
		WriteJSON(w, 200, M{"choices": []any{M{"message": M{"content": "OK"}}}})
	}))
	defer provider.Close()
	c := f.models.Config()
	p := localProvider(provider.URL)
	c["providers"] = []any{p}
	f.save(c)
	view := runtimeData(t, f)
	masked := O(A(O(view["config"])["providers"])[0])
	model := O(A(masked["models"])[0])
	before, _ := os.ReadFile(f.config.Path)
	for _, protocol := range []string{"OPENAI_CHAT_COMPLETIONS", "OPENAI_RESPONSES"} {
		masked["protocol"] = protocol
		v, e := VerifyModel(context.Background(), f.config, M{"provider": masked, "model": model})
		if e != nil || !B(v["ok"]) || v["reply"] != "OK" {
			t.Fatal(v, e)
		}
		request := requests[len(requests)-1]
		body := O(request["body"])
		if request["authorization"] != "Bearer synthetic-provider-key" || body["tools"] != nil {
			t.Fatal(request)
		}
		if protocol == "OPENAI_RESPONSES" {
			if body["store"] != false || I(body["max_output_tokens"]) != 64 {
				t.Fatal(body)
			}
		} else if I(body["max_tokens"]) != 64 || O(A(body["messages"])[0])["content"] != "Reply only OK." {
			t.Fatal(body)
		}
	}
	status = 401
	v, e := VerifyModel(context.Background(), f.config, M{"provider": masked, "model": model})
	if e != nil || B(v["ok"]) || !strings.Contains(S(v["message"]), "认证失败") {
		t.Fatal(v, e)
	}
	after, _ := os.ReadFile(f.config.Path)
	if string(before) != string(after) {
		t.Fatal("probe saved configuration")
	}
	app := &App{Config: f.config, Models: f.models}
	_, e = DiscoverTools(context.Background(), app, M{"kind": "mcp", "mcp": M{"name": "proof", "url": provider.URL, "authTokenEnv": "TOKEN"}})
	if Status(e) != 422 {
		t.Fatal(e)
	}
}

func TestModelBrokenStreamNoRetry(t *testing.T) {
	f := newPlatformFixture(t, 5)
	closeStream := make(chan struct{})
	entered := make(chan struct{}, 2)
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		index := calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n")
		w.(http.Flusher).Flush()
		entered <- struct{}{}
		if index == 1 {
			select {
			case <-closeStream:
				conn, _, _ := w.(http.Hijacker).Hijack()
				conn.Close()
			case <-r.Context().Done():
			}
		} else {
			<-r.Context().Done()
		}
	}))
	defer upstream.Close()
	c := f.models.Config()
	c["providers"] = []any{localProvider(upstream.URL)}
	f.save(c)
	key := f.models.Catalog()[0]["key"]
	send := func(ctx context.Context) *http.Response {
		req, _ := http.NewRequestWithContext(ctx, "POST", f.models.URL+"/v1/chat/completions", strings.NewReader(string(JSON(M{"model": key, "messages": []any{}, "stream": true}))))
		req.Header.Set("Authorization", "Bearer "+f.models.Secret)
		r, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	r := send(context.Background())
	buf := make([]byte, 512)
	r.Body.Read(buf)
	close(closeStream)
	_, e := io.ReadAll(r.Body)
	r.Body.Close()
	if e == nil {
		t.Fatal("broken stream became EOF success")
	}
	ctx, cancel := context.WithCancel(context.Background())
	r = send(ctx)
	r.Body.Read(buf)
	cancel()
	_, e = io.ReadAll(r.Body)
	r.Body.Close()
	if e == nil {
		t.Fatal("stream cancellation ignored")
	}
	if calls.Load() != 2 || f.models.client.Timeout != 0 || f.models.client.Transport.(*http.Transport).ResponseHeaderTimeout != 180000000000 {
		t.Fatal("model retry or lifetime changed", calls.Load())
	}
}

func appRequest(t *testing.T, app *App, method, path string, body any) (int, M) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		reader = strings.NewReader(string(JSON(body)))
	}
	req, _ := http.NewRequest(method, app.URL+path, reader)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", app.URL)
	response, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer response.Body.Close()
	value, e := ReadJSON(response.Body, 4<<20)
	if e != nil {
		t.Fatal(e)
	}
	return response.StatusCode, value
}
func saveApp(t *testing.T, app *App, change func(M)) M {
	t.Helper()
	status, data := appRequest(t, app, "GET", "/api/settings/runtime", nil)
	if status != 200 {
		t.Fatal(data)
	}
	change(O(data["config"]))
	status, data = appRequest(t, app, "PUT", "/api/settings/runtime", data)
	if status != 200 {
		t.Fatal(status, data)
	}
	return data
}
func runAppTask(t *testing.T, app *App, id, prompt string) M {
	t.Helper()
	submitTask(t, app.Manager, id, prompt)
	task := waitIdle(t, app.Manager, id)
	if task["lastTurnReason"] != "completed" {
		t.Fatalf("task failed: %s", JSON(task))
	}
	return task
}

func TestOfficialRuntimeModelAndMCPSettings(t *testing.T) {
	f := newPlatformFixture(t, 5)
	c := f.models.Config()
	c["providers"] = []any{localProvider(f.server.URL)}
	c["defaultModelId"] = "proof-model"
	O(O(c["agents"])["root"])["instruction"] = "SETTINGS_INSTRUCTION_PROOF use proof_echo and report"
	c["localMcp"] = []any{M{"name": "proof", "url": f.server.URL + "/mcp", "enabled": true, "agents": []any{"root", "reviewer"}, "authToken": "synthetic-mcp-key", "tools": []any{"echo"}}}
	f.save(c)
	f.change(func() {
		f.controls["mcpTools"] = []any{M{"name": "echo", "description": "Echo fixture", "inputSchema": M{"type": "object"}, "annotations": M{"readOnlyHint": true}}, M{"name": "hidden", "inputSchema": M{"type": "object"}}}
		f.controls["chatTool"] = "proof_echo"
	})
	app := f.startApp()
	status, view := appRequest(t, app, "GET", "/api/settings/runtime", nil)
	if status != 200 || strings.Contains(string(JSON(view)), "synthetic-mcp-key") || strings.Contains(string(JSON(view)), "synthetic-provider-key") {
		t.Fatal(view)
	}
	task, e := app.Manager.Create(context.Background(), M{"approvalMode": "auto"})
	if e != nil {
		t.Fatal(e)
	}
	id := S(task["id"])
	runAppTask(t, app, id, "Verify saved settings")
	calls := requestCount(f, "tools/call")
	if calls != 1 {
		t.Fatal("MCP was not invoked", calls)
	}
	lists := requestCount(f, "tools/list")
	runAppTask(t, app, id, "Verify native reuse")
	if requestCount(f, "tools/list") != lists {
		t.Fatal("native MCP cache was replaced by workbench lifecycle")
	}
	for _, r := range f.requestList() {
		if S(r["path"]) == "/v1/chat/completions" {
			b := O(r["body"])
			if b["model"] != "upstream-proof" || N(b["temperature"]) != .3 || N(b["top_p"]) != .8 || max(I(b["max_tokens"]), I(b["max_completion_tokens"])) != 2048 || !strings.Contains(string(JSON(b["messages"])), "SETTINGS_INSTRUCTION_PROOF") || O(r["headers"])["authorization"] != "Bearer synthetic-provider-key" {
				t.Fatal(r)
			}
			for _, tool := range A(b["tools"]) {
				if O(O(tool)["function"])["name"] == "proof_hidden" {
					t.Fatal("unselected tool exposed")
				}
			}
		}
		if O(r["body"])["method"] == "tools/call" && O(r["headers"])["authorization"] != "Bearer synthetic-mcp-key" {
			t.Fatal("wrong local MCP credential")
		}
	}
	y := generatedConfig(t, f.models)
	native := string(JSON(y))
	if strings.Contains(native, "synthetic-mcp-key") || strings.Contains(native, "synthetic-provider-key") {
		t.Fatal("native yaml exposes credentials")
	}
	entry := f.models.MCP.entries["local:proof"]
	saveApp(t, app, func(c M) { O(O(c["agents"])["root"])["description"] = "Unrelated edit" })
	if f.models.MCP.entries["local:proof"] != entry {
		t.Fatal("unrelated edit invalidated registration")
	}
	saveApp(t, app, func(c M) { O(A(c["localMcp"])[0])["tools"] = []any{} })
	before := requestCount(f, "tools/call")
	runAppTask(t, app, id, "Inspect disabled MCP")
	if requestCount(f, "tools/call") != before {
		t.Fatal("disabled MCP still invoked")
	}
	saveApp(t, app, func(c M) { delete(O(A(c["localMcp"])[0]), "tools") })
	runAppTask(t, app, id, "Use restored MCP")
	if requestCount(f, "tools/call") != before+1 {
		t.Fatal("restored MCP missing")
	}
	saveApp(t, app, func(c M) { O(A(c["localMcp"])[0])["agents"] = []any{} })
	before = requestCount(f, "tools/call")
	runAppTask(t, app, id, "Inspect unassigned MCP")
	if requestCount(f, "tools/call") != before {
		t.Fatal("unassigned MCP invoked")
	}
	saveApp(t, app, func(c M) {
		O(A(c["localMcp"])[0])["agents"] = []any{"root"}
		model := O(A(O(A(c["providers"])[0])["models"])[0])
		model["reasoningEfforts"] = []any{"none", "high", "xhigh"}
		O(A(c["providers"])[0])["models"] = append(A(O(A(c["providers"])[0])["models"]), Merge(model, M{"modelId": "proof-model--reasoning-xhigh", "upstreamModelKey": "distinct-upstream", "capabilities": []any{"TOOL"}}))
		delete(O(A(O(A(c["providers"])[0])["models"])[1]), "reasoningEfforts")
	})
	if _, e = app.Manager.SelectModel(id, "proof-model", "low"); Status(e) != 422 {
		t.Fatal(e)
	}
	for _, effort := range []string{"xhigh", "none", "default"} {
		if _, e = app.Manager.SelectModel(id, "proof-model", effort); e != nil {
			t.Fatal(e)
		}
		runAppTask(t, app, id, "Verify "+effort)
		var last M
		for _, r := range f.requestList() {
			if r["path"] == "/v1/chat/completions" {
				last = O(r["body"])
			}
		}
		if last["model"] != "upstream-proof" || effort == "default" && Has(last, "reasoning_effort") || effort != "default" && last["reasoning_effort"] != effort {
			t.Fatal(last)
		}
	}
	if _, e = app.Manager.SelectModel(id, "proof-model--reasoning-xhigh", "default"); e != nil {
		t.Fatal(e)
	}
	runAppTask(t, app, id, "Verify distinct alias")
	requests := f.requestList()
	last := O(requests[len(requests)-1]["body"])
	if last["model"] != "distinct-upstream" || Has(last, "reasoning_effort") {
		t.Fatal("resource ID interpreted as reasoning selector", last)
	}
	saveApp(t, app, func(c M) { O(A(c["localMcp"])[0])["enabled"] = false })
	var count int
	if e = f.store.DB.QueryRow("SELECT count(*) FROM mcp_catalog WHERE resource_key='local:proof'").Scan(&count); e != nil || count != 0 {
		t.Fatal("disabled cache retained", count, e)
	}
	saveApp(t, app, func(c M) {
		O(A(c["localMcp"])[0])["enabled"] = true
		O(A(c["localMcp"])[0])["authToken"] = "synthetic-new-token"
	})
	runAppTask(t, app, id, "Use rotated local MCP")
	for _, token := range app.Engine.Options().Env {
		if token == "synthetic-mcp-key" {
			t.Fatal("old native credential retained")
		}
	}
}

func TestOfficialRuntimeApplyInterruptRollback(t *testing.T) {
	f := newPlatformFixture(t, 5)
	c := f.models.Config()
	c["providers"] = []any{localProvider(f.server.URL)}
	c["defaultModelId"] = "proof-model"
	f.save(c)
	app := f.startApp()
	entered := make(chan struct{})
	release := make(chan struct{})
	var once atomic.Bool
	f.change(func() {
		f.hook = func(w http.ResponseWriter, r *http.Request, b M) bool {
			if r.URL.Path != "/v1/chat/completions" || once.Swap(true) {
				return false
			}
			close(entered)
			select {
			case <-release:
			case <-r.Context().Done():
			}
			return true
		}
	})
	task, e := app.Manager.Create(context.Background(), M{"approvalMode": "auto"})
	if e != nil {
		t.Fatal(e)
	}
	id := S(task["id"])
	submitTask(t, app.Manager, id, "Slow settings test")
	select {
	case <-entered:
	case <-time.After(15 * time.Second):
		t.Fatal("provider did not start")
	}
	_, data := appRequest(t, app, "GET", "/api/settings/runtime", nil)
	O(O(O(data["config"])["agents"])["root"])["description"] = "Changed"
	before, _ := os.ReadFile(f.config.Path)
	status, response := appRequest(t, app, "PUT", "/api/settings/runtime", data)
	if status != 409 || response["code"] != "settings_interrupt_required" {
		t.Fatal(status, response)
	}
	raw, _ := os.ReadFile(f.config.Path)
	if string(raw) != string(before) || !app.Manager.Running(id) {
		t.Fatal("unconfirmed apply mutated active state")
	}
	data["defer"] = true
	status, response = appRequest(t, app, "PUT", "/api/settings/runtime", data)
	if status != 200 || !B(response["pending"]) || !app.Manager.Running(id) || O(O(app.Models.Config()["agents"])["root"])["description"] == "Changed" {
		t.Fatal(status, response)
	}
	_, data = appRequest(t, app, "GET", "/api/settings/runtime", nil)
	data["interrupt"] = true
	status, response = appRequest(t, app, "PUT", "/api/settings/runtime", data)
	close(release)
	if status != 200 || app.Manager.Running(id) || mustTask(t, app.Manager, id)["lastTurnReason"] != "interrupted" {
		t.Fatal(status, response, mustTask(t, app.Manager, id))
	}
	_, data = appRequest(t, app, "GET", "/api/settings/runtime", nil)
	before, _ = os.ReadFile(f.config.Path)
	O(O(O(data["config"])["agents"])["root"])["toolsets"] = []any{M{"type": "mcp", "remote": 123}}
	status, response = appRequest(t, app, "PUT", "/api/settings/runtime", data)
	if status < 400 {
		t.Fatal("invalid native config accepted", response)
	}
	raw, _ = os.ReadFile(f.config.Path)
	if string(raw) != string(before) || O(O(app.Models.Config()["agents"])["root"])["description"] != "Changed" || app.Engine.State()["status"] != "ready" {
		t.Fatal("apply rollback failed", app.Engine.State(), response)
	}
	runAppTask(t, app, id, "After restored runtime")
}

func TestOfficialLocalMCPRecoveryAndFailure(t *testing.T) {
	f := newPlatformFixture(t, 5)
	c := f.models.Config()
	c["providers"] = []any{localProvider(f.server.URL)}
	c["defaultModelId"] = "proof-model"
	c["localMcp"] = []any{M{"name": "proof", "url": f.server.URL + "/mcp", "enabled": true, "agents": []any{"root"}, "authToken": "synthetic-lifecycle-key"}}
	f.save(c)
	f.change(func() { f.controls["chatTool"] = "proof_search_records"; f.controls["mcpStrictSession"] = true })
	app := f.startApp()
	task, e := app.Manager.Create(context.Background(), M{"approvalMode": "auto"})
	if e != nil {
		t.Fatal(e)
	}
	id := S(task["id"])
	runAppTask(t, app, id, "First local MCP")
	for _, failure := range []M{{"status": 404}, {"drop": true}, {"drop": true, "persistent": true}} {
		before, handshakes := requestCount(f, "tools/call"), requestCount(f, "initialize")
		f.change(func() { f.controls["mcpFailure"] = Merge(failure, M{"method": "tools/call"}) })
		runAppTask(t, app, id, "Native MCP recovery")
		if requestCount(f, "tools/call") != before+2 || requestCount(f, "initialize") != handshakes+1 {
			t.Fatal("native retry contract", failure, requestCount(f, "tools/call")-before)
		}
		messages, _ := f.store.Messages(id, 0, 10000)
		var last M
		for _, message := range messages {
			for _, b := range A(message["blocks"]) {
				if O(b)["type"] == "tool-result" {
					last = O(O(b)["data"])
				}
			}
		}
		want := "success"
		if B(failure["persistent"]) {
			want = "error"
		}
		if last["status"] != want {
			t.Fatal(last)
		}
		if f.models.MCP.Summary(true)[0]["status"] != "ready" {
			t.Fatal("live result replaced verification fact")
		}
	}
	f.change(func() { delete(f.controls, "mcpFailure") })
	before := requestCount(f, "tools/call")
	runAppTask(t, app, id, "Recovered next turn")
	if requestCount(f, "tools/call") != before+1 {
		t.Fatal("explicit next turn did not recover")
	}
}

func TestOfficialPlatformHistoryParametersAndProtocols(t *testing.T) {
	f := newPlatformFixture(t, 5)
	f.change(func() {
		for _, v := range A(f.snapshot["models"]) {
			O(v)["capabilities"] = append(A(O(v)["capabilities"]), "REASONING")
		}
		f.snapshot["mcp"] = []any{}
		for _, v := range A(f.snapshot["assistants"]) {
			O(v)["mcpBindings"] = []any{}
		}
	})
	f.enroll()
	app := f.startApp()
	model := f.models.Catalog()[0]
	id := S(model["modelId"])
	key := f.models.DefaultsKey(id, "platform")
	saveApp(t, app, func(c M) {
		O(c["platformModelSettings"])[key] = M{"contextWindow": 131072, "maxOutputTokens": 4096, "temperature": .2, "topP": .7}
	})
	task, e := app.Manager.Create(context.Background(), M{"modelId": id, "approvalMode": "auto"})
	if e != nil {
		t.Fatal(e)
	}
	taskID := S(task["id"])
	f.change(func() {
		f.controls["chatTool"] = "write_file"
		f.controls["chatArguments"] = M{"path": "platform-proof.txt", "content": "platform tool proof"}
	})
	runAppTask(t, app, taskID, "HISTORY_MARKER initial request")
	if raw, e := os.ReadFile(filepath.Join(S(task["workspace"]), "platform-proof.txt")); e != nil || string(raw) != "platform tool proof" {
		t.Fatal("platform native tool write failed", e, string(raw))
	}
	f.change(func() { delete(f.controls, "chatTool"); delete(f.controls, "chatArguments") })
	for _, r := range f.requestList() {
		if strings.Contains(S(r["path"]), "/resources/"+id) && strings.HasSuffix(S(r["path"]), "/chat/completions") {
			b := O(r["body"])
			if N(b["temperature"]) != .2 || N(b["top_p"]) != .7 || max(I(b["max_tokens"]), I(b["max_completion_tokens"])) != 4096 {
				t.Fatal(b)
			}
		}
	}
	for _, m := range f.models.Catalog() {
		for _, effort := range []string{"default", "low", "medium", "high"} {
			if _, e = app.Manager.SelectModel(taskID, m["modelId"], effort); e != nil {
				t.Fatal(e)
			}
			runAppTask(t, app, taskID, "Verify protocol reasoning "+effort)
			var request M
			for _, r := range f.requestList() {
				if strings.HasPrefix(S(r["path"]), "/runtime/") {
					request = r
				}
			}
			b := O(request["body"])
			if m["protocol"] == "OPENAI_RESPONSES" {
				if b["store"] != false || b["previous_response_id"] != nil || effort != "default" && O(b["reasoning"])["effort"] != effort {
					t.Fatal(b)
				}
			} else if effort != "default" && b["reasoning_effort"] != effort {
				t.Fatal(b)
			}
			if !strings.Contains(string(JSON(b)), "HISTORY_MARKER") {
				t.Fatal("native history lost switching protocol")
			}
		}
	}
	// A verified snapshot generation refresh keeps the original native session.
	nativeID := mustTask(t, app.Manager, taskID)["nativeSessionId"]
	f.change(func() {
		f.snapshot["managedGeneration"] = I(f.snapshot["managedGeneration"]) + 1
		O(f.controls["state"])["activeManagedGeneration"] = f.snapshot["managedGeneration"]
		f.controls["etag"] = "\"" + S(f.snapshot["snapshotHash"]) + "\""
	})
	status, v := appRequest(t, app, "POST", "/api/connection/sync", M{})
	if status != 200 {
		t.Fatal(status, v)
	}
	runAppTask(t, app, taskID, "After generation update")
	if mustTask(t, app.Manager, taskID)["nativeSessionId"] != nativeID {
		t.Fatal("sync replaced native history")
	}
	selected := S(mustTask(t, app.Manager, taskID)["modelId"])
	f.change(func() {
		for _, v := range A(f.snapshot["models"]) {
			if O(v)["modelId"] == selected {
				O(v)["enabled"] = false
			}
		}
		f.snapshot["managedGeneration"] = I(f.snapshot["managedGeneration"]) + 1
		O(f.controls["state"])["activeManagedGeneration"] = f.snapshot["managedGeneration"]
	})
	status, v = appRequest(t, app, "POST", "/api/connection/sync", M{})
	if status != 200 {
		t.Fatal(status, v)
	}
	if _, e = app.Manager.Submit(taskID, "Blocked missing model", nil, nil, nil); Code(e) != "model_unavailable" {
		t.Fatal(e)
	}
	if _, e = app.Manager.SelectModel(taskID, id, "default"); e != nil {
		t.Fatal(e)
	}
	runAppTask(t, app, taskID, "Explicit restored selection")
	if mustTask(t, app.Manager, taskID)["nativeSessionId"] != nativeID {
		t.Fatal("model recovery replaced native session")
	}
}

func TestOfficialSavedLimitsAndReadonly(t *testing.T) {
	f := newPlatformFixture(t, 5)
	c := f.models.Config()
	c["providers"] = []any{localProvider(f.server.URL)}
	c["defaultModelId"] = "proof-model"
	O(O(c["agents"])["root"])["max_iterations"] = 0
	O(O(c["agents"])["root"])["max_consecutive_tool_calls"] = 8
	c["localMcp"] = []any{M{"name": "proof", "url": f.server.URL + "/mcp", "enabled": true, "agents": []any{"root"}}}
	f.save(c)
	f.change(func() { f.controls["chatTool"] = "proof_search_records"; f.controls["chatRepeats"] = 7 })
	app := f.startApp()
	task, e := app.Manager.Create(context.Background(), M{"approvalMode": "auto"})
	if e != nil {
		t.Fatal(e)
	}
	runAppTask(t, app, S(task["id"]), "Seven identical polls")
	if requestCount(f, "tools/call") != 7 {
		t.Fatal("native execution limits not applied")
	}
	saveApp(t, app, func(c M) { O(O(c["agents"])["root"])["max_consecutive_tool_calls"] = 0 })
	task, e = app.Manager.Create(context.Background(), M{"approvalMode": "auto"})
	if e != nil {
		t.Fatal(e)
	}
	submitTask(t, app.Manager, S(task["id"]), "Native default protection")
	task = waitIdle(t, app.Manager, S(task["id"]))
	if task["lastTurnReason"] != "error" || !strings.Contains(S(O(task["lastFailure"])["message"]), "5 consecutive identical calls") {
		t.Fatalf("native repeat protection changed: %s", JSON(task))
	}
	saveApp(t, app, func(c M) { O(O(c["agents"])["root"])["readonly"] = true; O(c["workbench"])["requireDelivery"] = true })
	f.change(func() { delete(f.controls, "chatTool"); delete(f.controls, "chatRepeats") })
	task, e = app.Manager.Create(context.Background(), M{"approvalMode": "auto"})
	if e != nil {
		t.Fatal(e)
	}
	task = runAppTask(t, app, S(task["id"]), "Readonly execution")
	if I(task["turnCount"]) != 1 {
		t.Fatal("readonly automatic delivery loop")
	}
	for _, v := range A(app.Manager.AssistantCatalog()["items"]) {
		if O(v)["agent"] == "root" && B(O(v)["deliveryTool"]) {
			t.Fatal("readonly advertises delivery")
		}
	}
	var request M
	for _, r := range f.requestList() {
		if r["path"] == "/v1/chat/completions" {
			request = O(r["body"])
		}
	}
	if strings.Contains(string(JSON(request["tools"])), "deliver_result") {
		t.Fatal("readonly native delivery exposed")
	}
}

func TestOfficialAssistantDefaultsAndForks(t *testing.T) {
	f := newPlatformFixture(t, 5)
	c := f.models.Config()
	p := localProvider(f.server.URL)
	models := A(p["models"])
	p["models"] = append(models, Merge(O(models[0]), M{"modelId": "alternate", "upstreamModelKey": "alternate-chat-alias"}))
	c["providers"] = []any{p}
	c["defaultModelId"] = "proof-model"
	O(O(c["agents"])["image"])["modelId"] = "alternate"
	O(O(c["agents"])["worker"])["modelId"] = "proof-model"
	O(O(c["agents"])["worker"])["instruction"] = "DEFAULT_MODEL_CHILD_MARKER"
	f.save(c)
	f.change(func() {
		f.hook = func(w http.ResponseWriter, r *http.Request, b M) bool {
			if r.URL.Path != "/v1/chat/completions" {
				return false
			}
			messages := A(b["messages"])
			child := strings.Contains(string(JSON(messages)), "DEFAULT_MODEL_CHILD_MARKER")
			recentTools := false
			for i := len(messages) - 1; i >= 0; i-- {
				if O(messages[i])["role"] == "user" {
					break
				}
				if O(messages[i])["role"] == "tool" {
					recentTools = true
				}
			}
			delta := M{"content": "Parent completed"}
			reason := "stop"
			if child {
				delta = M{"content": "Child completed"}
			} else if strings.Contains(string(JSON(messages)), "DELEGATE_DEFAULTS") && !recentTools {
				delta = M{"tool_calls": []any{M{"index": 0, "id": "delegate-defaults-" + UUID(), "type": "function", "function": M{"name": "transfer_task", "arguments": `{"agent":"worker","task":"Check own default","expected_output":"Done"}`}}}}
				reason = "tool_calls"
			}
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", JSON(M{"id": "fixture", "object": "chat.completion.chunk", "choices": []any{M{"index": 0, "delta": delta, "finish_reason": reason}}}))
			return true
		}
	})
	app := f.startApp()
	m := app.Manager
	for _, agent := range []string{"worker", "reviewer", "missing"} {
		if _, e := m.Create(context.Background(), M{"agent": agent}); Status(e) != 400 {
			t.Fatal(agent, e)
		}
	}
	task, e := m.Create(context.Background(), M{"agent": "root", "modelId": "alternate", "approvalMode": "auto"})
	if e != nil {
		t.Fatal(e)
	}
	id := S(task["id"])
	runAppTask(t, app, id, "DELEGATE_DEFAULTS")
	childCount := 0
	for _, r := range f.requestList() {
		if r["path"] != "/v1/chat/completions" {
			continue
		}
		b := O(r["body"])
		if strings.Contains(string(JSON(b["messages"])), "DEFAULT_MODEL_CHILD_MARKER") {
			childCount++
			if b["model"] != "upstream-proof" {
				t.Fatal("parent choice leaked into child", b["model"])
			}
		} else if b["model"] != "alternate-chat-alias" {
			t.Fatal("parent override reset", b["model"])
		}
		if Has(b, "reasoning_effort") {
			t.Fatal("automatic reasoning override")
		}
	}
	if childCount == 0 {
		t.Fatal("no native child execution")
	}
	nativeID := task["nativeSessionId"]
	saveApp(t, app, func(c M) {
		O(O(c["agents"])["root"])["modelId"] = "proof-model"
		O(O(c["agents"])["image"])["modelId"] = "proof-model"
	})
	runAppTask(t, app, id, "DELEGATE_DEFAULTS again")
	if mustTask(t, m, id)["modelId"] != "alternate" || mustTask(t, m, id)["nativeSessionId"] != nativeID {
		t.Fatal("configured default reset existing task")
	}
	for _, agent := range []string{"video", "image", "publishing"} {
		before := len(f.requestList())
		task, e := m.Create(context.Background(), M{"agent": agent, "approvalMode": "auto"})
		if e != nil {
			t.Fatal(e)
		}
		runAppTask(t, app, S(task["id"]), "ASSISTANT_SELECTION_"+agent)
		instruction := strings.Split(S(O(O(f.models.Config()["agents"])[agent])["instruction"]), "\n")[0]
		found := false
		for _, r := range f.requestList()[before:] {
			if strings.Contains(string(JSON(O(r["body"])["messages"])), instruction) {
				found = true
			}
		}
		if !found {
			t.Fatal("assistant instruction absent", agent)
		}
		branch, e := m.Fork(context.Background(), S(task["id"]), 0)
		if e != nil || O(branch["task"])["agent"] != agent || O(branch["task"])["assistantName"] != task["assistantName"] {
			t.Fatal(branch, e)
		}
	}
}

func TestOfficialBuiltinDiscovery(t *testing.T) {
	binary := officialTestBinary(t)
	filesystem, e := BuiltinTools(context.Background(), binary, "filesystem")
	if e != nil {
		t.Fatal(e)
	}
	hasRead := false
	for _, v := range A(filesystem[0]["tools"]) {
		hasRead = hasRead || O(v)["name"] == "read_file"
	}
	if !hasRead {
		t.Fatal(filesystem)
	}
	app := &App{Engine: NewEngine(EngineOptions{Binary: binary})}
	catalog, e := DiscoverTools(context.Background(), app, M{"kind": "builtin"})
	if e != nil {
		t.Fatal(e)
	}
	groups := A(catalog["groups"])
	if len(groups) != len(editableToolsets) {
		t.Fatal(catalog)
	}
	shell, session := false, false
	for _, group := range groups {
		g := O(group)
		if g["type"] == "filesystem" && !reflect.DeepEqual(g["tools"], filesystem[0]["tools"]) {
			t.Fatal("batch discovery differs")
		}
		for _, v := range A(g["tools"]) {
			tool := O(v)
			shell = shell || tool["name"] == "shell" && !B(tool["readonly"])
			session = session || tool["name"] == "read_session"
		}
	}
	if !shell || !session {
		t.Fatal(catalog)
	}
}

func TestOfficialEnterpriseSelectionsAndCoexistence(t *testing.T) {
	f := newPlatformFixture(t, 5)
	f.change(func() {
		a := O(A(f.snapshot["models"])[0])
		for _, digit := range []string{"d", "e", "f"} {
			id := "mdl_" + strings.Repeat(digit, 8) + "-" + strings.Repeat(digit, 4) + "-4" + strings.Repeat(digit, 3) + "-8" + strings.Repeat(digit, 3) + "-" + strings.Repeat(digit, 12)
			f.snapshot["models"] = append(A(f.snapshot["models"]), Merge(a, M{"modelId": id, "displayName": "Platform " + digit}))
		}
		O(A(O(A(f.snapshot["mcp"])[0])["allowedTools"])[0])["approvalPolicy"] = "AUTO"
		f.controls["chatTool"] = "proof_search_records"
	})
	c := f.models.Config()
	p := localProvider(f.server.URL)
	p["models"] = append(A(p["models"]), Merge(O(A(p["models"])[0]), M{"modelId": "second-local", "upstreamModelKey": "second-upstream"}))
	c["providers"] = []any{p}
	c["defaultModelId"] = "proof-model"
	c["localMcp"] = []any{M{"name": "proof", "url": f.server.URL + "/mcp", "enabled": true, "agents": []any{"root"}, "authToken": "local-only-key"}}
	f.save(c)
	old, e := f.models.Pick("proof-model", "default")
	if e != nil {
		t.Fatal(e)
	}
	f.enroll()
	app := f.startApp()
	if len(f.models.Catalog()) != 7 {
		t.Fatal(f.models.Catalog())
	}
	now, _ := f.models.Pick("proof-model", "default")
	if now["model"] != old["model"] {
		t.Fatal("enterprise enrollment invalidated local alias")
	}
	saveApp(t, app, func(c M) { O(O(c["agents"])["root"])["modelId"] = "second-local" })
	task, e := app.Manager.Create(context.Background(), M{"approvalMode": "auto"})
	if e != nil {
		t.Fatal(e)
	}
	id := S(task["id"])
	runAppTask(t, app, id, "Use local MCP")
	var request M
	for _, r := range f.requestList() {
		if r["path"] == "/v1/chat/completions" {
			request = r
		}
	}
	body := O(request["body"])
	if body["model"] != "second-upstream" || O(request["headers"])["authorization"] != "Bearer synthetic-provider-key" || !strings.Contains(string(JSON(body["tools"])), ToolAlias("search_records")) || !strings.Contains(string(JSON(body["tools"])), "proof_search_records") {
		t.Fatal("coexistence failed", request)
	}
	peer := newMCPPeer(t, f)
	peer.rpc(t, "tools/list", M{})
	resource := S(O(A(f.snapshot["mcp"])[0])["mcpServerId"])
	key := f.models.DefaultsKey(resource, "platform")
	status, view := appRequest(t, app, "GET", "/api/settings/runtime", nil)
	if status != 200 {
		t.Fatal(view)
	}
	for _, v := range A(view["managedModels"]) {
		if Has(O(v), "runtimePath") {
			t.Fatal("internal model path exposed")
		}
	}
	saveApp(t, app, func(c M) { O(c["platformMcpTools"])[key] = []any{} })
	before := requestCount(f, "tools/call")
	_, reply := peer.rpc(t, "tools/call", M{"name": ToolAlias("search_records"), "arguments": M{}})
	if !B(O(reply["result"])["isError"]) || requestCount(f, "tools/call") != before {
		t.Fatal("stale selected tool reached upstream")
	}
	_, reply = peer.rpc(t, "tools/list", M{})
	if len(A(O(reply["result"])["tools"])) != 0 {
		t.Fatal("disabled platform selection exposed")
	}
	f.change(func() { delete(f.controls, "chatTool") })
	runAppTask(t, app, id, "Inspect disabled platform MCP")
	for _, r := range f.requestList()[len(f.requestList())-1:] {
		if strings.Contains(string(JSON(O(r["body"])["tools"])), ToolAlias("search_records")) {
			t.Fatal("disabled enterprise tool in native model")
		}
	}
	discovery, e := DiscoverTools(context.Background(), app, M{"kind": "platform_mcp", "mcpServerId": resource, "settingsKey": key})
	if e != nil || len(A(discovery["tools"])) != 1 || O(A(discovery["tools"])[0])["name"] != "search_records" {
		t.Fatal(discovery, e)
	}
	saveApp(t, app, func(c M) { O(c["platformMcpTools"])[key] = []any{"search_records", "not_granted"} })
	_, reply = peer.rpc(t, "tools/list", M{})
	if len(A(O(reply["result"])["tools"])) != 1 {
		t.Fatal(reply)
	}
	_, reply = peer.rpc(t, "tools/call", M{"name": ToolAlias("not_granted"), "arguments": M{}})
	if !B(O(reply["result"])["isError"]) {
		t.Fatal("local selection escalated platform grant")
	}
	f.change(func() { f.controls["chatTool"] = ToolAlias("search_records") })
	platformTask, e := app.Manager.Create(context.Background(), M{"modelId": O(A(f.snapshot["models"])[0])["modelId"], "approvalMode": "auto"})
	if e != nil {
		t.Fatal(e)
	}
	runAppTask(t, app, S(platformTask["id"]), "Use selected enterprise MCP")
	saveApp(t, app, func(c M) { O(O(c["agents"])["root"])["disabledEnterpriseMcp"] = []any{resource, "mcp_saved_elsewhere"} })
	f.change(func() { delete(f.controls, "chatTool") })
	runAppTask(t, app, id, "Inspect optout")
	y := generatedConfig(t, f.models)
	if Has(O(O(y["agents"])["root"]), "disabledEnterpriseMcp") {
		t.Fatal("product config leaked")
	}
	root := f.models.MCP.Toolsets("root", false)
	worker := f.models.MCP.Toolsets("worker", false)
	if len(root) != 1 || len(worker) == 0 {
		t.Fatal("agent optout isolation failed", root, worker)
	}
	// A synthetic available turn target is enough to inspect tool visibility;
	// no DAV transfers occur in this declaration test.
	begin := app.Manager.options.BeginTurn
	app.Manager.options.BeginTurn = func(ctx context.Context, task M) (map[string]string, func(), error) {
		headers, finish, e := begin(ctx, task)
		if e != nil {
			return nil, nil, e
		}
		app.Local.mu.Lock()
		for _, turn := range app.Local.turns {
			if turn.TaskID == S(task["id"]) {
				turn.Target = M{"name": "inspection workspace"}
			}
		}
		app.Local.mu.Unlock()
		return headers, finish, nil
	}
	visible := func() []string {
		runAppTask(t, app, id, "Inspect Tuck tools")
		var r M
		for _, q := range f.requestList() {
			if q["path"] == "/v1/chat/completions" {
				r = O(q["body"])
			}
		}
		names := []string{}
		for _, v := range A(r["tools"]) {
			names = append(names, S(O(O(v)["function"])["name"]))
		}
		return names
	}
	saveApp(t, app, func(c M) { O(O(c["agents"])["root"])["disabledTuckTools"] = tuckTools })
	names := visible()
	for _, name := range tuckTools {
		if Contains(names, name) {
			t.Fatal("optout tool visible", name)
		}
	}
	if !Contains(names, "deliver_result") {
		t.Fatal("unrelated delivery removed", names, len(app.Local.peers), app.Engine.State())
	}
	saveApp(t, app, func(c M) {
		O(O(c["agents"])["root"])["disabledTuckTools"] = []any{"upload_runcove_file", "send_session_message"}
	})
	names = visible()
	if !Contains(names, "list_sessions") || Contains(names, "send_session_message") || !Contains(names, "download_runcove_files") || Contains(names, "upload_runcove_file") {
		t.Fatal(names)
	}
	f.change(func() { O(f.controls["state"])["runtimeBlocked"] = true })
	f.platform.ManagedState(context.Background())
	if _, e = f.models.Pick("proof-model", "default"); e != nil {
		t.Fatal("platform blocked local model", e)
	}
	if _, e = f.models.Pick(O(A(f.snapshot["models"])[0])["modelId"], "default"); Code(e) != "platform_pending" {
		t.Fatal(e)
	}
	if e = f.platform.Logout(context.Background()); e != nil {
		t.Fatal(e)
	}
	if e = f.models.Rebuild(); e != nil {
		t.Fatal(e)
	}
	if len(f.models.Catalog()) != 2 {
		t.Fatal("logout removed local catalog")
	}
}

func TestModelOptionalDefaultAndLoginScope(t *testing.T) {
	f := newPlatformFixture(t, 5)
	c := f.models.Config()
	c["providers"] = []any{localProvider(f.server.URL)}
	f.save(c)
	m, e := NewManager(ManagerOptions{Store: f.store, Engine: &fakeNative{}, Models: f.models, Config: f.models.Config, WorkspaceRoot: f.t.TempDir()})
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close(context.Background())
	before := len(m.List())
	if _, e = m.Create(context.Background(), M{"channel": "tasks", "agent": "root"}); Code(e) != "model_required" {
		t.Fatal(e)
	}
	if len(m.List()) != before {
		t.Fatal("failed admission created task")
	}
	chat, e := m.Create(context.Background(), M{"agent": "root"})
	if e != nil || chat["model"] != nil {
		t.Fatal(chat, e)
	}
	task, e := m.Create(context.Background(), M{"channel": "tasks", "agent": "root", "modelId": "proof-model"})
	if e != nil || task["modelId"] != "proof-model" {
		t.Fatal(task, e)
	}
	f.enroll()
	selected, e := f.models.Pick(O(A(f.snapshot["models"])[0])["modelId"], "default")
	if e != nil {
		t.Fatal(e)
	}
	scope := f.models.MCP.scope()
	var old *mcpEntry
	for _, entry := range f.models.MCP.entries {
		if entry.Source == "platform" {
			old = entry
			break
		}
	}
	d := f.platform.Data()
	O(d["binding"])["sessionId"] = "11111111-1111-4111-8111-111111111111"
	if e = f.platform.persist(d); e != nil {
		t.Fatal(e)
	}
	if e = f.models.Rebuild(); e != nil {
		t.Fatal(e)
	}
	bound, e := f.models.Bind(selected)
	if e != nil || bound["model"] != selected["model"] || f.models.MCP.scope() == scope || f.models.MCP.current(old) == nil {
		t.Fatal("login identity failed to rotate MCP separately from model", bound, e)
	}
}
