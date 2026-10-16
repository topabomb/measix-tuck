package tuck

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestParityRenameFailureAndHTTPOrigins(t *testing.T) {
	m, s, n, id := taskFixture(t)
	before := mustTask(t, m, id)
	for _, bad := range []string{"", "  ", "line\nline", strings.Repeat("字", 121), "control\x00"} {
		if _, e := m.Rename(context.Background(), id, bad); Status(e) != 400 {
			t.Fatal(bad, e)
		}
	}
	n.request = func(context.Context, string, string, any) (M, error) {
		return nil, Problem(502, "native_failed", "failed")
	}
	if _, e := m.Rename(context.Background(), id, "different"); e == nil {
		t.Fatal("native failure accepted")
	}
	stored, _ := s.Task(id)
	if stored["title"] != before["title"] || mustTask(t, m, id)["title"] != before["title"] {
		t.Fatal("title saved before native success")
	}
	f := newPlatformFixture(t, 5)
	a := f.startApp()
	for _, origin := range []string{"null", "https://foreign.example", a.URL + "/"} {
		status, _, _ := requestApp(t, a, "GET", "/api/tasks", nil, http.Header{"Origin": {origin}})
		if status != 403 {
			t.Fatal(origin, status)
		}
	}
	r, _ := http.NewRequest("GET", a.URL+"/api/tasks", nil)
	r.Host = "rebind.example"
	response, e := http.DefaultClient.Do(r)
	if e != nil {
		t.Fatal(e)
	}
	response.Body.Close()
	if response.StatusCode != 403 {
		t.Fatal("rebinding accepted", response.StatusCode)
	}
}

func TestParitySettingsProtectedAPIsAndPartialSave(t *testing.T) {
	f := newPlatformFixture(t, 5)
	a := f.startApp()
	hash, _ := PasswordHash("six-ok", 6)
	c, rev, _ := a.Config.Read()
	O(c["security"])["settingsProtection"] = M{"passwordHash": hash, "categories": protectable}
	a.Config.Save(rev, c)
	headers := http.Header{"Origin": {a.URL}}
	for _, in := range []struct {
		method, path string
		body         M
	}{{"GET", "/api/settings/runtime", nil}, {"POST", "/api/settings/model-test", M{}}, {"POST", "/api/settings/tools", M{"kind": "builtin"}}, {"POST", "/api/settings/tools", M{"kind": "mcp"}}, {"GET", "/api/settings/engine", nil}, {"POST", "/api/settings/engine/check", M{}}, {"POST", "/api/connection/sync", M{}}, {"PUT", "/api/settings/runtime?section=agents", M{"config": M{"agents": M{}}}}, {"PUT", "/api/settings/orchestration", M{}}} {
		status, v, _ := requestApp(t, a, in.method, in.path, in.body, headers)
		if status != 403 {
			t.Fatal(in.path, status, v)
		}
	}
	if status, _, _ := requestApp(t, a, "GET", "/api/runtime", nil, nil); status != 200 {
		t.Fatal("working catalog unavailable", status)
	}
	status, _, h := requestApp(t, a, "POST", "/api/settings/unlock", M{"password": "six-ok"}, headers)
	if status != 200 {
		t.Fatal(status)
	}
	headers.Set("Cookie", strings.Split(h.Get("Set-Cookie"), ";")[0])
	if status, _, _ := requestApp(t, a, "GET", "/api/settings/runtime", nil, headers); status != 200 {
		t.Fatal(status)
	}
	if status, _, _ := requestApp(t, a, "POST", "/api/settings/lock", M{}, headers); status != 200 {
		t.Fatal(status)
	}
	if status, _, _ := requestApp(t, a, "GET", "/api/settings/engine", nil, headers); status != 403 {
		t.Fatal(status)
	}
	// Only MCP remains editable. Saving it cannot replace protected sections.
	c, rev, _ = a.Config.Read()
	O(O(c["security"])["settingsProtection"])["categories"] = []any{"providers", "agents"}
	a.Config.Save(rev, c)
	before := Clone(c)
	status, view, _ := requestApp(t, a, "GET", "/api/settings/runtime?section=mcp", nil, nil)
	if status != 200 || Has(O(view["config"]), "providers") || Has(O(view["config"]), "agents") {
		t.Fatal(status, view)
	}
	status, v, _ := requestApp(t, a, "PUT", "/api/settings/runtime?section=mcp", M{"revision": view["revision"], "config": M{"localMcp": []any{}, "platformMcpTools": M{}}, "defer": true}, http.Header{"Origin": {a.URL}})
	if status != 200 {
		t.Fatal(status, v)
	}
	after, rev, _ := a.Config.Read()
	for _, field := range []string{"agents", "providers", "security"} {
		if !bytes.Equal(JSON(before[field]), JSON(after[field])) {
			t.Fatal("hidden section overwritten", field)
		}
	}
	status, _, _ = requestApp(t, a, "PUT", "/api/settings/runtime?section=mcp", M{"revision": rev, "config": M{"agents": M{}}, "defer": true}, http.Header{"Origin": {a.URL}})
	if status != 403 {
		t.Fatal("protected injection", status)
	}
	_, _, h = requestApp(t, a, "POST", "/api/settings/unlock", M{"password": "six-ok"}, http.Header{"Origin": {a.URL}})
	headers.Set("Cookie", strings.Split(h.Get("Set-Cookie"), ";")[0])
	_, rev, _ = a.Config.Read()
	status, v, _ = requestApp(t, a, "PUT", "/api/settings/protection", M{"revision": rev, "password": "", "categories": []any{}}, headers)
	if status != 200 || B(v["enabled"]) {
		t.Fatal(status, v)
	}
}

func TestParityProtectionDuringActiveWorkPreservesPending(t *testing.T) {
	f, a := parityApp(t)
	started := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	nativeScript(f, func(w http.ResponseWriter, r *http.Request, b M) {
		close(started)
		select {
		case <-release:
			nativeDelta(w, M{"content": "done"})
		case <-r.Context().Done():
		}
	})
	task := newParityTask(t, a, M{})
	id := S(task["id"])
	if _, e := a.Manager.Submit(id, "hold", nil, nil, nil); e != nil {
		t.Fatal(e)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("native did not start")
	}
	_, rev, _ := a.Config.Read()
	status, v, _ := requestApp(t, a, "PUT", "/api/settings/runtime?section=agents", M{"revision": rev, "config": M{"workbench": Merge(O(a.Models.Config()["workbench"]), M{"requireDelivery": true})}, "defer": true}, http.Header{"Origin": {a.URL}})
	if status != 200 {
		t.Fatal(status, v)
	}
	_, rev, _ = a.Config.Read()
	status, v, _ = requestApp(t, a, "PUT", "/api/settings/protection", M{"revision": rev, "password": "six-ok", "categories": []any{"agents"}}, http.Header{"Origin": {a.URL}})
	if status != 200 || !B(v["enabled"]) || !a.Config.Pending() || !a.Manager.Running(id) || B(O(a.Models.Config()["workbench"])["requireDelivery"]) {
		t.Fatal(status, v, a.Config.Pending(), mustTask(t, a.Manager, id))
	}
	if status, _, _ := requestApp(t, a, "GET", "/api/settings/runtime?section=agents", nil, nil); status != 403 {
		t.Fatal("protection not immediate", status)
	}
}

func TestParityLANReverseProxyLoginAndCSRF(t *testing.T) {
	f := newPlatformFixture(t, 5)
	a := f.startApp()
	c, rev, _ := a.Config.Read()
	hash, _ := PasswordHash(testPassword, 12)
	O(c["security"])["auth"] = M{"enabled": true, "username": "operator", "passwordHash": hash}
	a.Config.Save(rev, c)
	a.Config.MarkApplied()
	// The second listener really binds every interface; requests arrive via a reverse proxy.
	lan := httptest.NewUnstartedServer(http.HandlerFunc(a.serve))
	lan.Listener.Close()
	listener, e := net.Listen("tcp4", "0.0.0.0:0")
	if e != nil {
		t.Fatal(e)
	}
	lan.Listener = listener
	lan.Start()
	defer lan.Close()
	target, _ := url.Parse("http://127.0.0.1:" + strings.Split(lan.Listener.Addr().String(), ":")[1])
	proxy := httptest.NewServer(httputil.NewSingleHostReverseProxy(target))
	defer proxy.Close()
	do := func(method, path string, data M, h http.Header) (int, M, http.Header) {
		r, _ := http.NewRequest(method, proxy.URL+path, bytes.NewReader(JSON(data)))
		r.Header = h
		r.Header.Set("Origin", proxy.URL)
		r.Header.Set("X-Forwarded-Host", "forged.example")
		r.Header.Set("X-Forwarded-Proto", "https")
		response, e := http.DefaultClient.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		defer response.Body.Close()
		raw, _ := io.ReadAll(response.Body)
		value, _ := DecodeJSON(raw, 4<<20)
		return response.StatusCode, O(value), response.Header
	}
	status, login, h := do("POST", "/api/auth/login", M{"username": "operator", "password": testPassword, "rememberMe": false}, http.Header{})
	if status != 200 || strings.Contains(h.Get("Set-Cookie"), "Secure;") {
		t.Fatal(status, login, h)
	}
	auth := http.Header{"Cookie": {strings.Split(h.Get("Set-Cookie"), ";")[0]}}
	if status, _, _ := do("POST", "/api/tasks", M{}, auth); status != 403 {
		t.Fatal("CSRF bypass", status)
	}
	auth.Set("X-Tuck-CSRF", S(login["csrf"]))
	if status, v, _ := do("POST", "/api/tasks", M{}, auth); status != 201 {
		t.Fatal(status, v)
	}
}
