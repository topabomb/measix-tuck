package tuck

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// The control plane and providers are synthetic; native execution always uses
// the unmodified official binary. All fixture identities and tokens are fake.
type platformFixture struct {
	t                                                     *testing.T
	mu                                                    sync.Mutex
	server                                                *httptest.Server
	config                                                *Config
	store                                                 *Store
	platform                                              *Platform
	models                                                *Models
	snapshot, discovery, bootstrap, credentials, controls M
	requests                                              []M
	material                                              string
	hook                                                  func(http.ResponseWriter, *http.Request, M) bool
}

func testVector(t *testing.T, name string) M {
	t.Helper()
	raw, e := os.ReadFile(filepath.Join("..", "..", "tests", "contracts", name+".json"))
	if e != nil {
		t.Fatal(e)
	}
	v, e := DecodeJSON(raw, 4<<20)
	if e != nil {
		t.Fatal(e)
	}
	return O(v)
}
func newPlatformFixture(t *testing.T, version int) *platformFixture {
	t.Helper()
	f := &platformFixture{t: t, discovery: testVector(t, "discovery"), bootstrap: testVector(t, "bootstrap"), snapshot: testVector(t, fmt.Sprintf("snapshot-v%d", version)), credentials: testVector(t, "enrollment-response"), controls: M{}}
	f.credentials["accessTokenExpiresAt"] = time.Now().Add(5 * time.Minute).UTC().Format(time.RFC3339Nano)
	f.credentials["refreshExpiresAt"] = time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339Nano)
	f.credentials["sessionIdleExpiresAt"] = f.credentials["refreshExpiresAt"]
	O(A(f.snapshot["models"])[0])["upstreamModelKey"] = "public-chat-alias"
	response := O(A(testVector(t, "snapshot-v4-responses")["models"])[0])
	provider := Merge(O(A(f.snapshot["providers"])[0]), M{"providerId": "prv_cccccccc-cccc-4ccc-8ccc-cccccccccccc", "displayName": "Responses provider", "clientProtocol": "OPENAI_RESPONSES"})
	f.snapshot["providers"] = append(A(f.snapshot["providers"]), provider)
	f.snapshot["models"] = append(A(f.snapshot["models"]), Merge(response, M{"modelId": "mdl_cccccccc-cccc-4ccc-8ccc-cccccccccccc", "providerId": provider["providerId"], "displayName": "Responses model", "upstreamModelKey": "public-responses-alias"}))
	tool := M{"name": "search_records", "description": "Search fixture records", "inputSchema": M{"type": "object", "properties": M{"query": M{"type": "string"}}}, "annotations": M{"readOnlyHint": true}, "extension": M{"mustBeHashed": true}}
	hash, e := ToolHash(tool)
	if e != nil {
		t.Fatal(e)
	}
	if version == 5 {
		O(A(O(A(f.snapshot["mcp"])[0])["allowedTools"])[0])["contractHash"] = hash
	}
	f.controls = M{"state": M{"runtimeStatus": "READY", "activeManagedGeneration": f.snapshot["managedGeneration"], "managedStateRevision": 1, "syncRequired": false, "runtimeBlocked": false}, "etag": "\"" + S(f.snapshot["snapshotHash"]) + "\"", "token": f.credentials["accessToken"], "mcpSession": "fixture-session", "mcpTools": []any{tool, M{"name": "not_granted", "inputSchema": M{"type": "object"}}}}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	dir := t.TempDir()
	path := filepath.Join(dir, "tuck.config.json")
	c := defaultConfig()
	O(c["security"])["credentialKey"] = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{42}, 32))
	O(c["storage"])["dataDir"] = filepath.Join(dir, "data")
	O(c["storage"])["workspaceRoot"] = filepath.Join(dir, "workspaces")
	c["providers"] = []any{}
	c["localMcp"] = []any{}
	c["defaultModelId"] = ""
	c["platformModelSettings"] = M{}
	c["platformMcpTools"] = M{}
	for _, v := range O(c["agents"]) {
		delete(O(v), "modelId")
	}
	if e = os.WriteFile(path, JSON(c), 0600); e != nil {
		t.Fatal(e)
	}
	f.config, e = LoadConfig(path)
	if e != nil {
		t.Fatal(e)
	}
	f.store, e = NewStore(filepath.Join(f.config.DataDir(), "tuck.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = f.store.Close() })
	f.platform, e = NewPlatform(f.store, f.config)
	if e != nil {
		t.Fatal(e)
	}
	f.models, e = NewModels(f.config, f.store, f.platform)
	if e != nil {
		t.Fatal(e)
	}
	f.material = string(JSON(M{"formatVersion": 1, "kind": "PLATFORM_ENROLLMENT", "platformUrl": f.server.URL, "code": "synthetic-one-use-code", "expiresAt": time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)}))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = f.models.Close(ctx)
		f.platform.Close()
		f.server.Close()
		_ = f.store.Close()
	})
	return f
}
func (f *platformFixture) change(fn func()) { f.mu.Lock(); defer f.mu.Unlock(); fn() }
func (f *platformFixture) requestList() []M {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []M{}
	for _, v := range f.requests {
		out = append(out, Clone(v))
	}
	return out
}
func (f *platformFixture) enroll() {
	f.t.Helper()
	if e := f.platform.Enroll(context.Background(), f.material); e != nil {
		f.t.Fatal(e)
	}
	if e := f.models.Rebuild(); e != nil {
		f.t.Fatal(e)
	}
	if f.platform.Error() != "" {
		f.t.Fatal(f.platform.Error())
	}
}
func (f *platformFixture) save(c M) {
	f.t.Helper()
	_, rev, e := f.config.Read()
	if e != nil {
		f.t.Fatal(e)
	}
	if _, e = f.config.Save(rev, c); e != nil {
		f.t.Fatal(e)
	}
	f.config.MarkApplied()
	if e = f.models.SetConfig(c); e != nil {
		f.t.Fatal(e)
	}
}
func (f *platformFixture) send(path string, body M) (int, M, string) {
	f.t.Helper()
	req, e := http.NewRequest("POST", f.models.URL+path, bytes.NewReader(JSON(body)))
	if e != nil {
		f.t.Fatal(e)
	}
	req.Header.Set("Authorization", "Bearer "+f.models.Secret)
	req.Header.Set("Content-Type", "application/json")
	r, e := http.DefaultClient.Do(req)
	if e != nil {
		f.t.Fatal(e)
	}
	defer r.Body.Close()
	raw, e := io.ReadAll(r.Body)
	if e != nil {
		f.t.Fatal(e)
	}
	v, _ := DecodeJSON(raw, AgentRequestBytes)
	return r.StatusCode, O(v), string(raw)
}
func (f *platformFixture) serve(w http.ResponseWriter, r *http.Request) {
	var body M
	if r.Body != nil && !strings.Contains(r.URL.Path, "/dav/") && !(strings.HasSuffix(r.URL.Path, "/workspace/content") && r.Method == "PUT") {
		raw, _ := io.ReadAll(r.Body)
		if len(raw) > 0 {
			v, e := DecodeJSON(raw, AgentRequestBytes)
			if e != nil {
				WriteError(w, e)
				return
			}
			body = O(v)
		}
	}
	f.mu.Lock()
	f.requests = append(f.requests, M{"path": r.URL.Path, "method": r.Method, "body": Clone(body), "headers": mapHeaders(r.Header)})
	hook := f.hook
	f.mu.Unlock()
	if hook != nil && hook(w, r, body) {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	c := f.controls
	path := r.URL.Path
	if path == "/.well-known/measix" {
		WriteJSON(w, 200, f.discovery)
		return
	}
	if strings.HasSuffix(path, "/enrollments/exchange") {
		if B(c["enrollmentUsed"]) {
			WriteJSON(w, 409, M{"code": "enrollment_already_used"})
			return
		}
		c["enrollmentUsed"] = true
		WriteJSON(w, 201, f.credentials)
		return
	}
	if strings.HasSuffix(path, "/sessions/refresh") {
		if B(c["refreshDrop"]) {
			c["refreshDrop"] = false
			conn, _, _ := w.(http.Hijacker).Hijack()
			conn.Close()
			return
		}
		if S(c["refreshCode"]) != "" {
			WriteJSON(w, 409, M{"code": c["refreshCode"]})
			return
		}
		c["token"] = "synthetic-rotated-access"
		WriteJSON(w, 200, Merge(f.credentials, M{"accessToken": c["token"], "refreshToken": "synthetic-rotated-refresh", "accessTokenExpiresAt": time.Now().Add(5 * time.Minute).UTC().Format(time.RFC3339Nano)}))
		return
	}
	if strings.HasSuffix(path, "/sessions/logout") {
		w.WriteHeader(204)
		return
	}
	if path == "/mcp" {
		f.serveMCP(w, r, body)
		return
	}
	if !strings.HasPrefix(path, "/v1/") && r.Header.Get("Authorization") != "Bearer "+S(c["token"]) {
		WriteJSON(w, 401, M{"code": "invalid_session"})
		return
	}
	if strings.HasSuffix(path, "/bootstrap") {
		v := Clone(f.bootstrap)
		v["managedState"] = c["state"]
		if Has(c, "employeeName") {
			O(v["user"])["displayName"] = c["employeeName"]
		}
		WriteJSON(w, 200, v)
		return
	}
	if strings.HasSuffix(path, "/managed/state") {
		WriteJSON(w, 200, c["state"])
		return
	}
	if strings.Contains(path, "/managed/snapshots/") {
		w.Header().Set("ETag", S(c["etag"]))
		if r.Header.Get("If-None-Match") == S(c["etag"]) {
			w.WriteHeader(304)
			return
		}
		v := Clone(f.snapshot)
		if B(c["badResponse"]) {
			O(v["policy"])["allowLocalProviders"] = "false"
		}
		WriteJSON(w, 200, v)
		return
	}
	if strings.HasSuffix(path, "/managed/applied") {
		w.WriteHeader(204)
		return
	}
	if strings.HasPrefix(path, "/runtime/v1/resources/") || strings.HasPrefix(path, "/v1/") {
		if rejection := O(c["runtimeRejection"]); len(rejection) > 0 {
			WriteJSON(w, I(rejection["status"]), rejection["body"])
			return
		}
		if strings.Contains(path, "/mcp_") {
			f.serveMCP(w, r, body)
			return
		}
		if strings.HasSuffix(path, "/responses") {
			item := M{"id": "msg-fixture", "type": "message", "status": "completed", "role": "assistant", "content": []any{M{"type": "output_text", "text": "OK", "annotations": []any{}}}}
			result := M{"id": "resp-fixture", "object": "response", "status": "completed", "output": []any{item}, "usage": M{"input_tokens": 100, "output_tokens": 20, "total_tokens": 120}}
			if B(body["stream"]) {
				w.Header().Set("Content-Type", "text/event-stream")
				for _, event := range []M{{"type": "response.created", "response": Merge(result, M{"status": "in_progress", "output": []any{}})}, {"type": "response.output_item.added", "output_index": 0, "item": item}, {"type": "response.output_text.delta", "output_index": 0, "content_index": 0, "item_id": "msg-fixture", "delta": "OK"}, {"type": "response.output_item.done", "output_index": 0, "item": item}, {"type": "response.completed", "response": result}} {
					fmt.Fprintf(w, "event: %s\ndata: %s\n\n", S(event["type"]), JSON(event))
				}
			} else {
				WriteJSON(w, 200, result)
			}
			return
		}
		if B(body["stream"]) {
			delta, reason := M{"role": "assistant", "content": "OK"}, "stop"
			completed := false
			toolResults := 0
			messages := A(body["messages"])
			for i := len(messages) - 1; i >= 0; i-- {
				msg := O(messages[i])
				if msg["role"] == "user" {
					break
				}
				if msg["role"] == "tool" {
					toolResults++
				}
			}
			completed = toolResults >= max(1, I(c["chatRepeats"]))
			if suffix := S(c["chatTool"]); suffix != "" && !completed {
				for _, v := range A(body["tools"]) {
					name := S(O(O(v)["function"])["name"])
					if strings.HasSuffix(name, suffix) {
						delta = M{"role": "assistant", "tool_calls": []any{M{"index": 0, "id": "fixture-call-" + UUID(), "type": "function", "function": M{"name": name, "arguments": string(JSON(O(c["chatArguments"])))}}}}
						reason = "tool_calls"
						break
					}
				}
			}
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "data: %s\n\n", JSON(M{"id": "fixture", "object": "chat.completion.chunk", "choices": []any{M{"index": 0, "delta": delta, "finish_reason": nil}}}))
			fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", JSON(M{"id": "fixture", "object": "chat.completion.chunk", "choices": []any{M{"index": 0, "delta": M{}, "finish_reason": reason}}, "usage": M{"prompt_tokens": 100, "completion_tokens": 20, "total_tokens": 120}}))
			return
		}
		WriteJSON(w, 200, M{"choices": []any{M{"message": M{"role": "assistant", "content": "OK"}, "finish_reason": "stop"}}})
		return
	}
	WriteJSON(w, 404, M{"code": "not_found"})
}

func (f *platformFixture) startApp() *App {
	t := f.t
	t.Helper()
	c := f.models.Config()
	O(c["server"])["port"] = 0
	O(O(c["security"])["auth"])["enabled"] = false
	O(c["engine"])["binary"] = officialTestBinary(t)
	reserve, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	O(c["engine"])["url"] = "http://" + reserve.Addr().String()
	reserve.Close()
	f.save(c)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if e = f.models.Close(ctx); e != nil {
		t.Fatal(e)
	}
	f.platform.Close()
	f.store.Close()
	app, e := NewApp(f.config)
	if e != nil {
		t.Fatal(e)
	}
	f.models = app.Models
	f.platform = app.Platform
	f.store = app.Store
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		app.Close(ctx)
	})
	if e = app.Start(ctx); e != nil {
		t.Fatal(e, app.Engine.State())
	}
	return app
}

func officialTestBinary(t *testing.T) string {
	t.Helper()
	binary := os.Getenv("TUCK_TEST_ENGINE")
	if binary == "" {
		name := "docker-agent"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		binary = filepath.Join("..", "..", "tests", "runtime", name)
	}
	binary, _ = filepath.Abs(binary)
	if _, e := os.Stat(binary); e != nil {
		t.Fatal("official Docker Agent required: set TUCK_TEST_ENGINE", e)
	}
	return binary
}
func (f *platformFixture) startNative() *Manager {
	t := f.t
	t.Helper()
	c := f.models.Config()
	for _, v := range O(c["agents"]) {
		O(v)["disabledTuckTools"] = tuckTools
	}
	for _, v := range A(O(c["workbench"])["assistants"]) {
		O(v)["deliveryTool"] = false
	}
	if e := f.models.SetConfig(c); e != nil {
		t.Fatal(e)
	}
	file, e := f.models.WriteConfig()
	if e != nil {
		t.Fatal(e)
	}
	reserve, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	address := reserve.Addr().String()
	reserve.Close()
	dir := t.TempDir()
	env := f.models.MCP.Environment()
	env["TUCK_GATEWAY_TOKEN"] = f.models.Secret
	engine := NewEngine(EngineOptions{Binary: officialTestBinary(t), Config: file, DataDir: dir, WorkspaceRoot: filepath.Join(dir, "workspaces"), URL: "http://" + address, ConfigDir: filepath.Join(dir, "config"), Env: env})
	manager, e := NewManager(ManagerOptions{Store: f.store, Engine: engine, Models: f.models, Config: f.models.Config, AgentRef: "agent", WorkspaceRoot: filepath.Join(dir, "workspaces"), NativeDirectory: dir, ApprovalTools: f.models.MCP.ApprovalTools})
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if e = engine.Start(ctx); e != nil {
		t.Fatal(e, engine.State())
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		manager.Close(ctx)
		engine.Close()
	})
	return manager
}
func mapHeaders(h http.Header) M {
	v := M{}
	for k := range h {
		v[strings.ToLower(k)] = h.Get(k)
	}
	return v
}
func (f *platformFixture) serveMCP(w http.ResponseWriter, r *http.Request, body M) {
	c := f.controls
	method := S(body["method"])
	if B(c["mcpStrictSession"]) && method != "initialize" && r.Header.Get("Mcp-Session-Id") != S(c["mcpSession"]) {
		w.WriteHeader(401)
		return
	}
	if fail := O(c["mcpFailure"]); S(fail["method"]) == method && method != "" {
		if !B(fail["persistent"]) {
			delete(c, "mcpFailure")
		}
		if B(fail["drop"]) {
			conn, _, _ := w.(http.Hijacker).Hijack()
			conn.Close()
			return
		}
		w.WriteHeader(I(fail["status"]))
		return
	}
	if r.Method == "GET" {
		w.WriteHeader(405)
		return
	}
	if r.Method == "DELETE" || strings.HasPrefix(method, "notifications/") {
		w.WriteHeader(202)
		return
	}
	result := M{}
	switch method {
	case "initialize":
		result = M{"protocolVersion": firstString(c["mcpVersion"], "2025-11-25"), "capabilities": M{"tools": M{}}, "serverInfo": M{"name": "fixture-mcp", "version": "1.0.0"}}
		w.Header().Set("Mcp-Session-Id", S(c["mcpSession"]))
	case "tools/list":
		result = M{"tools": c["mcpTools"]}
	case "tools/call":
		if c["mcpCallError"] != nil {
			WriteJSON(w, 200, M{"jsonrpc": "2.0", "id": body["id"], "error": c["mcpCallError"]})
			return
		}
		result = M{"content": []any{M{"type": "text", "text": "MCP fixture result: " + S(O(body["params"])["name"])}}, "isError": false}
	}
	raw := JSON(M{"jsonrpc": "2.0", "id": body["id"], "result": result})
	if method == "tools/list" && S(c["mcpRaw"]) != "" {
		raw = []byte(S(c["mcpRaw"]))
	}
	if method == "tools/list" && B(c["mcpSse"]) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "id: exact-ID\nevent: message\nretry: 1500\ndata: %s\n\n", raw)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(raw)
}
