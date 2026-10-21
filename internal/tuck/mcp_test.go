package tuck

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

type mcpPeer struct {
	f             *platformFixture
	path, session string
}

func newMCPPeer(t *testing.T, f *platformFixture) *mcpPeer {
	t.Helper()
	key := S(O(A(f.snapshot["mcp"])[0])["mcpServerId"])
	f.models.MCP.mu.RLock()
	entry := f.models.MCP.entries[key]
	path := "/mcp/" + url.PathEscape(entry.Scope) + "/" + key
	f.models.MCP.mu.RUnlock()
	p := &mcpPeer{f: f, path: path}
	r, e := p.request(context.Background(), "POST", M{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": M{"protocolVersion": "2025-11-25", "capabilities": M{}, "clientInfo": M{"name": "fixture", "version": "1"}}}, nil)
	if e != nil {
		t.Fatal(e)
	}
	p.session = r.Header.Get("Mcp-Session-Id")
	v, e := readRPC(r, float64(1))
	if e != nil || S(O(v["result"])["protocolVersion"]) != "2025-11-25" {
		t.Fatal(v, e)
	}
	return p
}
func (p *mcpPeer) request(ctx context.Context, method string, body M, extra http.Header) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(JSON(body))
	}
	r, _ := http.NewRequestWithContext(ctx, method, p.f.models.URL+p.path, reader)
	r.Header.Set("Authorization", "Bearer "+p.f.models.Secret)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	r.Header.Set("Mcp-Session-Id", p.session)
	r.Header.Set("Mcp-Protocol-Version", "2025-11-25")
	for k, v := range extra {
		r.Header[k] = v
	}
	return http.DefaultClient.Do(r)
}
func (p *mcpPeer) rpc(t *testing.T, method string, params M) (int, M) {
	t.Helper()
	r, e := p.request(context.Background(), "POST", M{"jsonrpc": "2.0", "id": 7, "method": method, "params": params}, nil)
	if e != nil {
		t.Fatal(e)
	}
	status := r.StatusCode
	v, e := readRPC(r, float64(7))
	if e != nil {
		t.Fatal(e)
	}
	return status, v
}
func requestCount(f *platformFixture, method string) int {
	count := 0
	for _, r := range f.requestList() {
		if O(r["body"])["method"] == method {
			count++
		}
	}
	return count
}
func preparedMCP(t *testing.T) *platformFixture {
	t.Helper()
	f := newPlatformFixture(t, 5)
	f.enroll()
	f.models.MCP.Prepare(context.Background(), true)
	if S(f.models.MCP.Summary(false)[0]["status"]) != "ready" {
		t.Fatal(f.models.MCP.Summary(false))
	}
	return f
}

func TestMCPContractGrantsAndDrift(t *testing.T) {
	f := preparedMCP(t)
	f.change(func() { f.controls["mcpSse"] = true })
	p := newMCPPeer(t, f)
	_, v := p.rpc(t, "tools/list", M{})
	tools := A(O(v["result"])["tools"])
	if len(tools) != 1 || O(tools[0])["name"] != ToolAlias("search_records") || !B(O(O(tools[0])["extension"])["mustBeHashed"]) {
		t.Fatal(v)
	}
	_, v = p.rpc(t, "tools/call", M{"name": ToolAlias("search_records"), "arguments": M{"query": "hello"}})
	if B(O(v["result"])["isError"]) {
		t.Fatal(v)
	}
	for _, r := range f.requestList() {
		if O(r["body"])["method"] == "tools/call" {
			h := O(r["headers"])
			if O(O(r["body"])["params"])["name"] != "search_records" || h["mcp-session-id"] != p.session || h["mcp-protocol-version"] != "2025-11-25" || h["x-measix-managed-generation"] != fmt.Sprint(I(f.snapshot["managedGeneration"])) {
				t.Fatal(r)
			}
		}
	}
	f.change(func() { O(O(A(f.controls["mcpTools"])[0])["extension"])["mustBeHashed"] = false })
	_, v = p.rpc(t, "tools/list", M{})
	if len(A(O(v["result"])["tools"])) != 0 {
		t.Fatal("drift leaked tool")
	}
	_, v = p.rpc(t, "tools/call", M{"name": ToolAlias("search_records"), "arguments": M{}})
	if !B(O(v["result"])["isError"]) || requestCount(f, "tools/call") != 1 {
		t.Fatal("drift called upstream")
	}
	f.change(func() { O(f.controls["state"])["activeManagedGeneration"] = I(f.snapshot["managedGeneration"]) + 1 })
	_, v = p.rpc(t, "tools/call", M{"name": ToolAlias("search_records"), "arguments": M{}})
	if !B(O(v["result"])["isError"]) || requestCount(f, "tools/call") != 1 {
		t.Fatal("stale generation called upstream")
	}
	if f.models.MCP.Summary(false)[0]["status"] != "ready" {
		t.Fatal("validation fact replaced by invented live state")
	}
}
func TestMCPRefreshSessionAndFailures(t *testing.T) {
	f := preparedMCP(t)
	f.change(func() { f.controls["mcpStrictSession"] = true })
	p := newMCPPeer(t, f)
	p.rpc(t, "tools/list", M{})
	handshakes := requestCount(f, "initialize")
	scope := f.models.MCP.scope()
	d := f.platform.Data()
	O(d["binding"])["accessTokenExpiresAt"] = time.Unix(0, 0).UTC().Format(time.RFC3339)
	if e := f.platform.persist(d); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	errs := make(chan string, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := p.request(context.Background(), "POST", M{"jsonrpc": "2.0", "id": 8, "method": "tools/call", "params": M{"name": ToolAlias("search_records"), "arguments": M{}}}, nil)
			if e != nil {
				errs <- e.Error()
				return
			}
			v, e := readRPC(r, float64(8))
			if e != nil || B(O(v["result"])["isError"]) {
				errs <- fmt.Sprint(v, e)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
	refresh := 0
	for _, r := range f.requestList() {
		if strings.HasSuffix(S(r["path"]), "/sessions/refresh") {
			refresh++
		}
		if O(r["body"])["method"] == "tools/call" {
			if O(r["headers"])["authorization"] != "Bearer synthetic-rotated-access" || O(r["headers"])["mcp-session-id"] != p.session {
				t.Fatal(r)
			}
		}
	}
	if refresh != 1 || requestCount(f, "initialize") != handshakes || f.models.MCP.scope() != scope {
		t.Fatal("refresh changed protocol lifecycle")
	}
	for _, status := range []int{401, 404, 502} {
		before := requestCount(f, "tools/call")
		f.change(func() { f.controls["mcpFailure"] = M{"method": "tools/call", "status": status} })
		r, e := p.request(context.Background(), "POST", M{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": M{"name": ToolAlias("search_records"), "arguments": M{}}}, nil)
		if e != nil {
			t.Fatal(e)
		}
		r.Body.Close()
		if r.StatusCode != status || requestCount(f, "tools/call") != before+1 || requestCount(f, "initialize") != handshakes {
			t.Fatal("HTTP failure replayed or hidden")
		}
	}
	before := requestCount(f, "tools/call")
	f.change(func() { f.controls["mcpFailure"] = M{"method": "tools/call", "drop": true} })
	r, e := p.request(context.Background(), "POST", M{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": M{"name": ToolAlias("search_records"), "arguments": M{}}}, nil)
	if e == nil {
		r.Body.Close()
		t.Fatal("socket loss hidden")
	}
	if requestCount(f, "tools/call") != before+1 {
		t.Fatal("proxy retried")
	}
	f.change(func() { f.controls["mcpSession"] = "restarted-session" })
	r, e = p.request(context.Background(), "POST", M{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": M{"name": ToolAlias("search_records"), "arguments": M{}}}, nil)
	if e != nil {
		t.Fatal(e)
	}
	r.Body.Close()
	if r.StatusCode != 401 {
		t.Fatal("session reset hidden")
	}
	fresh := newMCPPeer(t, f)
	fresh.rpc(t, "tools/list", M{})
	_, v := fresh.rpc(t, "tools/call", M{"name": ToolAlias("search_records"), "arguments": M{}})
	if B(O(v["result"])["isError"]) {
		t.Fatal(v)
	}
}
func TestMCPVerificationAndMalformedCatalog(t *testing.T) {
	f := preparedMCP(t)
	key := S(O(A(f.snapshot["mcp"])[0])["mcpServerId"])
	for _, status := range []int{503, 401} {
		before := requestCount(f, "initialize")
		f.change(func() {
			f.controls["runtimeRejection"] = M{"status": status, "body": M{"code": "upstream_unavailable"}}
		})
		if e := f.models.MCP.Discover(context.Background(), key); e == nil {
			t.Fatal("verification succeeded")
		}
		summary := f.models.MCP.Summary(false)[0]
		if summary["status"] != "unavailable" || I(summary["toolCount"]) != 1 || requestCount(f, "initialize") != before+1 {
			t.Fatal(summary)
		}
		f.change(func() { delete(f.controls, "runtimeRejection") })
		if e := f.models.MCP.Discover(context.Background(), key); e != nil {
			t.Fatal(e)
		}
	}
	before := requestCount(f, "initialize")
	f.change(func() { O(f.controls["state"])["runtimeBlocked"] = true })
	if e := f.models.MCP.Discover(context.Background(), key); e == nil {
		t.Fatal("admission accepted")
	}
	if requestCount(f, "initialize") != before {
		t.Fatal("admission reached upstream")
	}
	for _, sse := range []bool{false, true} {
		for _, raw := range []string{`{"jsonrpc":"2.0","id":2,"result":{"tools":[],"tools":[]}}`, `{"jsonrpc":"2.0","id":2,"result":{"tools":[],"bad":"\ud800"}}`, `{"padding":"` + strings.Repeat("x", 4<<20) + `"}`} {
			f := newPlatformFixture(t, 5)
			f.enroll()
			f.change(func() { f.controls["mcpSse"] = sse; f.controls["mcpRaw"] = raw })
			f.models.MCP.Prepare(context.Background(), true)
			summary := f.models.MCP.Summary(false)[0]
			if summary["status"] != "pending" || I(summary["toolCount"]) != 0 {
				t.Fatal(summary)
			}
		}
	}
	f2 := newPlatformFixture(t, 5)
	f2.enroll()
	f2.change(func() { f2.controls["mcpVersion"] = "2024-11-05" })
	f2.models.MCP.Prepare(context.Background(), true)
	summary := f2.models.MCP.Summary(false)[0]
	if summary["status"] != "pending" || I(summary["toolCount"]) != 0 || S(summary["error"]) == "" {
		t.Fatal(summary)
	}
	before = requestCount(f2, "initialize")
	f2.models.MCP.Prepare(context.Background(), false)
	if requestCount(f2, "initialize") != before {
		t.Fatal("implicit verification retry")
	}
	f2.change(func() { f2.controls["mcpVersion"] = "2025-03-26" })
	f2.models.MCP.Prepare(context.Background(), true)
	if f2.models.MCP.Summary(false)[0]["status"] != "ready" {
		t.Fatal("explicit recovery failed")
	}
}
func TestMCPRawSSEAndRedaction(t *testing.T) {
	f := preparedMCP(t)
	p := newMCPPeer(t, f)
	p.rpc(t, "tools/list", M{})
	before := requestCount(f, "tools/call")
	f.change(func() {
		f.controls["mcpCallError"] = M{"code": -32602, "message": "alexandria: expected object or array; current=synthetic.access.token; refresh=synthetic-refresh-token; Bearer opaque-secret", "data": M{"detail": []any{"synthetic.access.token", "synthetic-refresh-token"}}}
	})
	_, v := p.rpc(t, "tools/call", M{"name": ToolAlias("search_records"), "arguments": M{}})
	raw := string(JSON(v))
	if I(O(v["error"])["code"]) != -32602 || !strings.Contains(raw, "alexandria") || strings.Contains(raw, "synthetic.access") || strings.Contains(raw, "synthetic-refresh") || strings.Contains(raw, "opaque-secret") || requestCount(f, "tools/call") != before+1 {
		t.Fatal(raw)
	}
	f.change(func() { delete(f.controls, "mcpCallError") })
	_, v = p.rpc(t, "tools/call", M{"name": ToolAlias("search_records"), "arguments": M{}})
	if B(O(v["result"])["isError"]) {
		t.Fatal(v)
	}
	status, _ := p.rpc(t, "resources/read", M{"uri": "secret"})
	if status != 403 || requestCount(f, "resources/read") != 0 {
		t.Fatal("nontool escaped")
	}
	_, v = p.rpc(t, "prompts/list", M{})
	if !reflect.DeepEqual(A(O(v["result"])["prompts"]), []any{}) {
		t.Fatal(v)
	}
	incoming := ": connected\r\n\r\nid: α-1\r\nevent: message\r\nretry: 1500\r\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/tools/list_changed\"}\r\n\r\nid: second\ndata: {\"jsonrpc\":\"2.0\",\"id\":7,\"result\":{}}\n\n"
	var out bytes.Buffer
	if e := mapSSE(&oneByteReader{incoming}, &out, func(v M) (M, error) { return v, nil }); e != nil {
		t.Fatal(e)
	}
	text := out.String()
	for _, expected := range []string{"id: α-1", "event: message", "retry: 1500", "notifications/tools/list_changed", "id: second"} {
		if !strings.Contains(text, expected) {
			t.Fatal(text)
		}
	}
	if e := mapSSE(strings.NewReader(`data: {"id":1}`), io.Discard, func(v M) (M, error) { return v, nil }); e == nil {
		t.Fatal("accepted truncated SSE")
	}
}

func TestExternalMCPImageResultIsUnchanged(t *testing.T) {
	f := preparedMCP(t)
	p := newMCPPeer(t, f)
	p.rpc(t, "tools/list", M{})
	raw := testPNG(t, 3000, 16, true)
	want := M{
		"content":           []any{M{"type": "image", "mimeType": "image/png", "data": base64.StdEncoding.EncodeToString(raw)}},
		"structuredContent": M{"width": 3000, "height": 16, "alpha": true},
		"isError":           false,
	}
	f.change(func() {
		f.hook = func(w http.ResponseWriter, r *http.Request, body M) bool {
			if !strings.Contains(r.URL.Path, "/mcp_") || body["method"] != "tools/call" {
				return false
			}
			WriteJSON(w, 200, M{"jsonrpc": "2.0", "id": body["id"], "result": want})
			return true
		}
	})
	status, response := p.rpc(t, "tools/call", M{"name": ToolAlias("search_records"), "arguments": M{"query": "image"}})
	if status != 200 || string(JSON(response["result"])) != string(JSON(want)) {
		t.Fatal("external image bytes or metadata were changed", status)
	}
}

func TestMCPSSEPrimingEvent(t *testing.T) {
	const reply = "id: result\nevent: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":7,\"result\":{\"tools\":[]}}\n\n"
	for name, prime := range map[string]string{
		"LF":   "id: first\ndata:\n\n",
		"CRLF": ": ready\r\nid: α-1\r\nevent: prime\r\nretry: 1500\r\ndata: \r\n\r\n",
	} {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			calls := 0
			err := mapSSE(&oneByteReader{prime + reply}, &out, func(v M) (M, error) {
				calls++
				if I(v["id"]) != 7 {
					t.Fatalf("priming event reached JSON transform: %s", JSON(v))
				}
				O(v["result"])["verified"] = true
				return v, nil
			})
			if err != nil || calls != 1 {
				t.Fatalf("JSON response after priming event was not transformed: calls=%d error=%v", calls, err)
			}
			if !strings.HasPrefix(out.String(), prime) {
				t.Fatalf("priming bytes changed: got %q, want prefix %q", out.String(), prime)
			}
			response := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(bytes.NewReader(out.Bytes()))}
			value, err := readRPC(response, float64(7))
			if err != nil || !B(O(value["result"])["verified"]) {
				t.Fatalf("RPC result after priming event was not read: %s error=%v", JSON(value), err)
			}
		})
	}
	for _, invalid := range []string{"{", "null", `{"jsonrpc":"2.0","id":7,"id":8}`} {
		t.Run("invalid_"+invalid, func(t *testing.T) {
			calls := 0
			err := mapSSE(strings.NewReader("data: "+invalid+"\n\n"), io.Discard, func(v M) (M, error) {
				calls++
				return v, nil
			})
			if err == nil || calls != 0 {
				t.Fatalf("accepted invalid nonempty MCP data: calls=%d error=%v", calls, err)
			}
		})
	}
}

func TestMCPEmptyNotificationAcknowledgment(t *testing.T) {
	f := newPlatformFixture(t, 5)
	f.change(func() {
		f.hook = func(w http.ResponseWriter, r *http.Request, body M) bool {
			if S(body["method"]) != "notifications/initialized" && r.Method != "DELETE" {
				return false
			}
			w.Header().Set("Content-Type", "application/json")
			status := http.StatusAccepted
			if r.Method == "DELETE" {
				status = http.StatusNoContent
			}
			w.WriteHeader(status)
			return true
		}
	})
	f.enroll()
	f.models.MCP.Prepare(context.Background(), true)
	if S(f.models.MCP.Summary(false)[0]["status"]) != "ready" {
		t.Fatal("verification rejected the empty notification acknowledgment", f.models.MCP.Summary(false))
	}
	p := newMCPPeer(t, f)
	response, err := p.request(context.Background(), "POST", M{"jsonrpc": "2.0", "method": "notifications/initialized"}, nil)
	if err != nil {
		t.Fatal("native notification acknowledgment did not pass through the proxy:", err)
	}
	raw, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != http.StatusAccepted || len(raw) != 0 || response.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("empty acknowledgment changed: status=%d headers=%v bytes=%d error=%v", response.StatusCode, response.Header, len(raw), err)
	}
	status, value := p.rpc(t, "tools/list", M{})
	if status != http.StatusOK || len(A(O(value["result"])["tools"])) != 1 {
		t.Fatalf("initialized client could not read the granted tools: status=%d result=%s", status, JSON(value))
	}
	response, err = p.request(context.Background(), "DELETE", nil, nil)
	if err != nil {
		t.Fatal("empty session deletion acknowledgment failed:", err)
	}
	raw, err = io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != http.StatusNoContent || len(raw) != 0 || response.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("empty deletion acknowledgment changed: status=%d headers=%v bytes=%d error=%v", response.StatusCode, response.Header, len(raw), err)
	}
}

func TestMCPAcknowledgmentsRetainJSONValidation(t *testing.T) {
	for _, test := range []struct {
		name, method, raw string
		status            int
	}{
		{"empty_initialize", "initialize", "", http.StatusOK},
		{"accepted_initialize", "initialize", "", http.StatusAccepted},
		{"invalid_initialize", "initialize", "{", http.StatusOK},
		{"invalid_notification_ack", "notifications/initialized", "{", http.StatusAccepted},
		{"invalid_notification_error", "notifications/initialized", "{", http.StatusServiceUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := preparedMCP(t)
			p := newMCPPeer(t, f)
			f.change(func() {
				f.hook = func(w http.ResponseWriter, r *http.Request, body M) bool {
					if S(body["method"]) != test.method {
						return false
					}
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(test.status)
					io.WriteString(w, test.raw)
					return true
				}
			})
			body := M{"jsonrpc": "2.0", "method": test.method}
			if test.method == "initialize" {
				body["id"] = 7
			}
			response, err := p.request(context.Background(), "POST", body, nil)
			if err == nil {
				raw, readErr := io.ReadAll(response.Body)
				response.Body.Close()
				if readErr == nil {
					t.Fatalf("invalid JSON response was not rejected: status=%d body=%q", response.StatusCode, raw)
				}
			}
		})
	}
	t.Run("notification_error_redaction", func(t *testing.T) {
		f := preparedMCP(t)
		p := newMCPPeer(t, f)
		f.change(func() {
			f.hook = func(w http.ResponseWriter, r *http.Request, body M) bool {
				if S(body["method"]) != "notifications/initialized" {
					return false
				}
				WriteJSON(w, http.StatusAccepted, M{"jsonrpc": "2.0", "error": M{"code": -32600, "message": "notification failed synthetic.access.token Bearer opaque-secret"}})
				return true
			}
		})
		response, err := p.request(context.Background(), "POST", M{"jsonrpc": "2.0", "method": "notifications/initialized"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		value, err := ReadJSON(response.Body, 4096)
		response.Body.Close()
		raw := string(JSON(value))
		if err != nil || response.StatusCode != http.StatusAccepted || I(O(value["error"])["code"]) != -32600 || !strings.Contains(raw, "notification failed") || strings.Contains(raw, "synthetic.access.token") || strings.Contains(raw, "opaque-secret") {
			t.Fatalf("nonempty acknowledgment bypassed error validation/redaction: status=%d body=%s error=%v", response.StatusCode, raw, err)
		}
	})
}

func TestMCPAllAndCanonicalHash(t *testing.T) {
	f := newPlatformFixture(t, 5)
	f.change(func() {
		resource := O(A(f.snapshot["mcp"])[0])
		resource["toolAccessMode"] = "ALL"
		resource["allowedTools"] = []any{}
		f.snapshot["mcp"] = append(A(f.snapshot["mcp"]), Merge(resource, M{"mcpServerId": "mcp_aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "enabled": false}))
	})
	f.enroll()
	f.models.MCP.Prepare(context.Background(), true)
	ready, disabled := false, false
	for _, m := range f.models.MCP.Summary(false) {
		ready = ready || m["status"] == "ready" && I(m["toolCount"]) == 2
		disabled = disabled || m["status"] == "disabled" && I(m["toolCount"]) == 0
	}
	if !ready || !disabled || len(f.models.MCP.ApprovalTools()) != 0 {
		t.Fatal(f.models.MCP.Summary(false))
	}
	p := newMCPPeer(t, f)
	_, v := p.rpc(t, "tools/list", M{})
	if len(A(O(v["result"])["tools"])) != 2 {
		t.Fatal(v)
	}
	f.change(func() {
		f.controls["mcpTools"] = append(A(f.controls["mcpTools"]), M{"name": "newly_added", "inputSchema": M{"type": "object"}})
	})
	_, v = p.rpc(t, "tools/list", M{})
	if len(A(O(v["result"])["tools"])) != 3 {
		t.Fatal(v)
	}
	a, e := ToolHash(M{"name": "x", "inputSchema": M{"type": "object"}, "extension": 1})
	if e != nil {
		t.Fatal(e)
	}
	b, e := ToolHash(M{"extension": 1.0, "inputSchema": M{"type": "object"}, "name": "x"})
	if e != nil || a != b {
		t.Fatal("noncanonical number/order", a, b, e)
	}
}

func TestOfficialEnterpriseMCPApprovalAndRecovery(t *testing.T) {
	f := preparedMCP(t)
	f.change(func() { f.controls["chatTool"] = ToolAlias("search_records") })
	m := f.startNative()
	task, e := m.Create(context.Background(), M{"approvalMode": "auto"})
	if e != nil {
		t.Fatal(e)
	}
	id := S(task["id"])
	submitTask(t, m, id, "MCP_LOOP")
	task = waitTask(t, m, id, func(task M) bool { return task["pendingInteraction"] != nil || !m.Running(id) })
	pending := O(task["pendingInteraction"])
	if pending["kind"] != "approval" || requestCount(f, "tools/call") != 0 {
		t.Fatalf("enterprise approval bypassed: %s", JSON(task))
	}
	if e = m.ResolveInteraction(context.Background(), id, M{"interactionId": pending["id"], "kind": "approval", "approved": true}); e != nil {
		t.Fatal(e)
	}
	task = waitIdle(t, m, id)
	if task["lastTurnReason"] != "completed" || requestCount(f, "tools/call") != 1 {
		t.Fatal(task)
	}
	for _, failure := range []M{{"status": 404}, {"drop": true}} {
		before := requestCount(f, "tools/call")
		f.change(func() { f.controls["mcpFailure"] = Merge(failure, M{"method": "tools/call"}) })
		submitTask(t, m, id, "MCP_LOOP again")
		task = waitTask(t, m, id, func(task M) bool { return task["pendingInteraction"] != nil || !m.Running(id) })
		pending = O(task["pendingInteraction"])
		if pending["id"] != nil {
			if e = m.ResolveInteraction(context.Background(), id, M{"interactionId": pending["id"], "kind": "approval", "approved": true}); e != nil {
				t.Fatal(e)
			}
		}
		task = waitIdle(t, m, id)
		if task["lastTurnReason"] != "completed" || requestCount(f, "tools/call") != before+2 {
			t.Fatalf("native recovery %s: %s calls %d", JSON(failure), JSON(task), requestCount(f, "tools/call")-before)
		}
		messages, e := f.store.Messages(id, 0, 10000)
		if e != nil {
			t.Fatal(e)
		}
		var result M
		for _, message := range messages {
			for _, block := range A(message["blocks"]) {
				if O(block)["type"] == "tool-result" {
					result = O(O(block)["data"])
				}
			}
		}
		if result["status"] != "success" {
			t.Fatal(result)
		}
	}
}

func TestOfficialEnterpriseMCPLongCallAndSSE(t *testing.T) {
	f := newPlatformFixture(t, 5)
	f.change(func() {
		O(A(O(A(f.snapshot["mcp"])[0])["allowedTools"])[0])["approvalPolicy"] = "AUTO"
		f.controls["chatTool"] = ToolAlias("search_records")
	})
	f.enroll()
	f.models.MCP.Prepare(context.Background(), true)
	m := f.startNative()
	notice := make(chan struct{})
	entered := make(chan struct{}, 1)
	f.change(func() {
		f.hook = func(w http.ResponseWriter, r *http.Request, b M) bool {
			if !strings.Contains(r.URL.Path, "/mcp_") {
				return false
			}
			if r.Method == "GET" {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, ": connected\n\n")
				w.(http.Flusher).Flush()
				select {
				case <-notice:
					fmt.Fprint(w, "id: notice-1\nevent: message\nretry: 1500\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/tools/list_changed\"}\n\n")
					w.(http.Flusher).Flush()
				case <-r.Context().Done():
				}
				return true
			}
			if b["method"] == "tools/call" {
				entered <- struct{}{}
				select {
				case <-time.After(122 * time.Second):
					WriteJSON(w, 200, M{"jsonrpc": "2.0", "id": b["id"], "result": M{"content": []any{M{"type": "text", "text": "Long call complete"}}, "isError": false}})
				case <-r.Context().Done():
				}
				return true
			}
			return false
		}
	})
	p := newMCPPeer(t, f)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, e := p.request(ctx, "GET", nil, http.Header{"Last-Event-Id": []string{"previous"}})
	if e != nil {
		t.Fatal(e)
	}
	defer stream.Body.Close()
	buf := make([]byte, 1024)
	if n, e := stream.Body.Read(buf); e != nil || !strings.Contains(string(buf[:n]), "connected") {
		t.Fatal(e)
	}
	task, e := m.Create(context.Background(), M{"approvalMode": "auto"})
	if e != nil {
		t.Fatal(e)
	}
	id := S(task["id"])
	start := time.Now()
	submitTask(t, m, id, "MCP_LOOP long")
	select {
	case <-entered:
	case <-time.After(15 * time.Second):
		t.Fatal("native call did not enter")
	}
	deadline := time.Now().Add(145 * time.Second)
	for m.Running(id) && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	task = mustTask(t, m, id)
	if time.Since(start) < 122*time.Second || task["lastTurnReason"] != "completed" || requestCount(f, "tools/call") != 1 {
		t.Fatalf("long native call failed: %s", JSON(task))
	}
	close(notice)
	n, e := stream.Body.Read(buf)
	if (e != nil && e != io.EOF) || !strings.Contains(string(buf[:n]), "notice-1") {
		t.Fatal("long SSE lost", e, string(buf[:n]))
	}
	if f.models.MCP.client.Timeout != 0 || f.models.MCP.client.Transport.(*http.Transport).ResponseHeaderTimeout != 0 {
		t.Fatal("MCP has total/header lifetime")
	}
}

func TestMCPLocalCacheVerificationAndRemoval(t *testing.T) {
	f := newPlatformFixture(t, 5)
	c := f.models.Config()
	c["localMcp"] = []any{M{"name": "proof", "url": f.server.URL + "/mcp", "enabled": true, "agents": []any{"root"}, "authToken": "synthetic-local-token"}}
	f.save(c)
	owner := f.models.MCP
	key := "local:proof"
	if e := owner.Discover(context.Background(), key); e != nil {
		t.Fatal(e)
	}
	original := owner.entries[key]
	restored, e := NewModels(f.config, f.store, f.platform)
	if e != nil {
		t.Fatal(e)
	}
	defer restored.Close(context.Background())
	if restored.MCP.Summary(true)[0]["status"] != "ready" {
		t.Fatal("verification cache not restored")
	}
	f.change(func() { f.controls["mcpFailure"] = M{"method": "initialize", "status": 503} })
	if e := restored.MCP.Discover(context.Background(), key); e == nil {
		t.Fatal("offline verification succeeded")
	}
	state := restored.MCP.Summary(true)[0]
	if state["status"] != "unavailable" || !B(state["validated"]) || I(state["toolCount"]) != 2 {
		t.Fatal(state)
	}
	app := &App{Models: restored, Config: f.config}
	reply, e := DiscoverTools(context.Background(), app, M{"kind": "mcp", "mcp": M{"name": "proof", "url": f.server.URL + "/mcp"}})
	if e != nil || O(reply["state"])["status"] != "ready" {
		t.Fatal(reply, e)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	f.change(func() {
		f.hook = func(w http.ResponseWriter, r *http.Request, body M) bool {
			if r.URL.Path != "/mcp" || body["method"] != "initialize" {
				return false
			}
			close(entered)
			select {
			case <-release:
				WriteJSON(w, 200, M{})
			case <-r.Context().Done():
			}
			return true
		}
	})
	done := make(chan error, 1)
	go func() { done <- owner.Discover(context.Background(), key) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("verification not started")
	}
	c["localMcp"] = []any{}
	f.save(c)
	close(release)
	if e = <-done; e == nil {
		t.Fatal("retired verification succeeded")
	}
	if original.ctx.Err() == nil || owner.entries[key] != nil {
		t.Fatal("registration not retired")
	}
	var count int
	if e = f.store.DB.QueryRow("SELECT count(*) FROM mcp_catalog WHERE resource_key=?", key).Scan(&count); e != nil || count != 0 {
		t.Fatal("late verification recreated cache", count, e)
	}
}

func TestMCPIndependentCancellationAndSSE(t *testing.T) {
	f := preparedMCP(t)
	p := newMCPPeer(t, f)
	p.rpc(t, "tools/list", M{})
	initialized := requestCount(f, "initialize")
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	notice := make(chan struct{})
	f.change(func() {
		f.hook = func(w http.ResponseWriter, r *http.Request, b M) bool {
			if !strings.Contains(r.URL.Path, "/mcp_") {
				return false
			}
			if r.Method == "GET" {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, ": connected\n\n")
				w.(http.Flusher).Flush()
				select {
				case <-notice:
					fmt.Fprint(w, "id: notice-1\nevent: message\nretry: 1500\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/tools/list_changed\"}\n\n")
					w.(http.Flusher).Flush()
				case <-r.Context().Done():
				}
				return true
			}
			if b["method"] == "tools/call" {
				entered <- struct{}{}
				select {
				case <-release:
					WriteJSON(w, 200, M{"jsonrpc": "2.0", "id": b["id"], "result": M{"isError": false, "content": []any{}}})
				case <-r.Context().Done():
				}
				return true
			}
			return false
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	results := make(chan error, 2)
	call := func(ctx context.Context) {
		r, e := p.request(ctx, "POST", M{"jsonrpc": "2.0", "id": 4, "method": "tools/call", "params": M{"name": ToolAlias("search_records"), "arguments": M{}}}, nil)
		if e == nil {
			_, e = readRPC(r, float64(4))
		}
		results <- e
	}
	go call(ctx)
	go call(context.Background())
	for range 2 {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("calls not entered")
		}
	}
	cancel()
	if e := <-results; e == nil {
		t.Fatal("cancel ignored")
	}
	close(release)
	if e := <-results; e != nil {
		t.Fatal("other call canceled", e)
	}
	stream, e := p.request(context.Background(), "GET", nil, http.Header{"Last-Event-Id": []string{"previous"}})
	if e != nil {
		t.Fatal(e)
	}
	defer stream.Body.Close()
	buf := make([]byte, 1024)
	n, e := stream.Body.Read(buf)
	if e != nil || !strings.Contains(string(buf[:n]), "connected") {
		t.Fatal(e)
	}
	close(notice)
	n, e = stream.Body.Read(buf)
	if e != nil && e != io.EOF || !strings.Contains(string(buf[:n]), "id: notice-1\nevent: message\nretry: 1500") || !strings.Contains(string(buf[:n]), "notifications/tools/list_changed") {
		t.Fatal(string(buf[:n]), e)
	}
	if requestCount(f, "initialize") != initialized {
		t.Fatal("single request cancellation closed shared session")
	}
	found := false
	for _, r := range f.requestList() {
		if r["method"] == "GET" && strings.Contains(S(r["path"]), "/mcp_") {
			found = O(r["headers"])["last-event-id"] == "previous"
		}
	}
	if !found {
		t.Fatal("raw event cursor lost")
	}
}
