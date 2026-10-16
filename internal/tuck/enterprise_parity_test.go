package tuck

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParityWorkspaceConditionsErrorsAndMutations(t *testing.T) {
	f := newWorkspaceFixture(t)
	_, s, q := f.proxy(t)
	base := s.URL + "/api/enterprise-workspace"
	do := func(method, endpoint string, body io.Reader, h http.Header) (int, string, http.Header) {
		r, _ := http.NewRequest(method, base+endpoint, body)
		r.Header = h
		response, e := http.DefaultClient.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		defer response.Body.Close()
		raw, _ := io.ReadAll(response.Body)
		return response.StatusCode, string(raw), response.Header
	}
	status, body, h := do("HEAD", "/content?"+q+"&path=hello.txt", nil, nil)
	if status != 200 || body != "" || h.Get("ETag") == "" || h.Get("Content-Length") != "11" {
		t.Fatal(status, body, h)
	}
	etag := h.Get("ETag")
	status, body, h = do("GET", "/content?"+q+"&path=hello.txt", nil, http.Header{"If-None-Match": {etag}})
	if status != 304 || body != "" || h.Get("ETag") != etag {
		t.Fatal(status, body, h)
	}
	status, _, h = do("GET", "/content?"+q+"&path=hello.txt", nil, http.Header{"Range": {"bytes=999-"}})
	if status != 416 || h.Get("Content-Range") != "bytes */11" {
		t.Fatal(status, h)
	}
	status, body, _ = do("PUT", "/content?"+q+"&path=hello.txt", strings.NewReader("replacement"), http.Header{"If-Match": {etag}})
	if status != 200 {
		t.Fatal(status, body)
	}
	for _, mutation := range []M{{"action": "MKCOL", "path": "created"}, {"action": "DELETE", "path": "second.txt"}} {
		status, body, _ = do("POST", "/files?"+q, strings.NewReader(string(JSON(mutation))), http.Header{"Content-Type": {"application/json"}})
		if status != 200 {
			t.Fatal(status, body)
		}
	}
	f.mu.Lock()
	if !f.dirs["created"] || f.files["second.txt"] != nil || string(f.files["hello.txt"]) != "replacement" {
		t.Fatal(f.files, f.dirs)
	}
	f.mu.Unlock()
	for _, code := range []int{400, 401, 403, 404, 408, 409, 412, 413, 416, 422, 428, 429, 500, 503} {
		f.setDAVHook(func(w http.ResponseWriter, r *http.Request, name string) bool {
			if strings.HasSuffix(r.URL.Path, "/content") {
				WriteJSON(w, code, M{"code": "upstream_failure", "message": "PRIVATE upstream prose synthetic.access.token"})
				return true
			}
			return false
		})
		status, body, _ = do("GET", "/content?"+q+"&path=hello.txt", nil, nil)
		want := code
		if code == 401 {
			want = 409
		}
		if status != want || strings.Contains(body, "PRIVATE") || strings.Contains(body, "synthetic.access") || !strings.Contains(body, "upstream_failure") {
			t.Fatal(code, status, body)
		}
	}
}

func TestParityWorkspaceUploadCancelAndHeaderTimeout(t *testing.T) {
	f := newWorkspaceFixture(t)
	e, s, q := f.proxy(t)
	started := make(chan struct{})
	disconnected := make(chan struct{})
	f.setDAVHook(func(w http.ResponseWriter, r *http.Request, name string) bool {
		if r.Method == "PUT" {
			b := make([]byte, 1)
			r.Body.Read(b)
			close(started)
			io.Copy(io.Discard, r.Body)
			close(disconnected)
			return true
		}
		return false
	})
	reader, writer := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r, _ := http.NewRequestWithContext(ctx, "PUT", s.URL+"/api/enterprise-workspace/content?"+q+"&path=upload.bin", reader)
	done := make(chan error, 1)
	go func() {
		response, err := http.DefaultClient.Do(r)
		if response != nil {
			response.Body.Close()
		}
		done <- err
	}()
	writer.Write([]byte("x"))
	<-started
	cancel()
	writer.CloseWithError(context.Canceled)
	select {
	case <-disconnected:
	case <-time.After(2 * time.Second):
		t.Fatal("upstream PUT not canceled")
	}
	<-done
	deadline := time.Now().Add(time.Second)
	for {
		e.mu.Lock()
		count := len(e.leases)
		e.mu.Unlock()
		if count == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("upload lease retained")
		}
		time.Sleep(time.Millisecond)
	}
	// Exercise the actual response-header timeout without waiting fifteen seconds.
	e.client.Transport = newTransport(40 * time.Millisecond)
	f.setDAVHook(func(w http.ResponseWriter, r *http.Request, name string) bool {
		if name == "slow.mp4" {
			<-r.Context().Done()
			return true
		}
		if name == "broken.mp4" {
			w.Header().Set("Content-Length", "1000")
			w.Write([]byte("partial"))
			return true
		}
		return false
	})
	response, err := http.Get(s.URL + "/api/enterprise-workspace/content?" + q + "&path=slow.mp4&preview=1")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 502 {
		t.Fatal(response.StatusCode)
	}
	response, err = http.Get(s.URL + "/api/enterprise-workspace/content?" + q + "&path=broken.mp4&preview=1")
	if err == nil {
		_, err = io.ReadAll(response.Body)
		response.Body.Close()
	}
	if err == nil {
		t.Fatal("truncated media reported complete")
	}
	start := time.Now()
	for _, endpoint := range []string{"", "/files?" + q, "/content?" + q + "&path=hello.txt"} {
		response, err = http.Get(s.URL + "/api/enterprise-workspace" + endpoint)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, response.Body)
		response.Body.Close()
		if response.StatusCode != 200 {
			t.Fatal(endpoint, response.StatusCode)
		}
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("media failure blocked controls")
	}
}

func TestParityWorkspaceAuthenticationLogoutAndConnectionGate(t *testing.T) {
	f := newWorkspaceFixture(t)
	a := f.p.startApp()
	hash, _ := PasswordHash(testPassword, 12)
	c, rev, _ := a.Config.Read()
	O(c["security"])["auth"] = M{"enabled": true, "username": "operator", "passwordHash": hash}
	a.Config.Save(rev, c)
	a.Config.MarkApplied()
	q := url.Values{"agentSpaceId": {f.space}, "subject": {workspaceSubject(a.Platform.Binding())}, "path": {"video.mp4"}}
	endpoint := "/api/enterprise-workspace/content?" + q.Encode()
	status, _, _ := requestApp(t, a, "GET", endpoint, nil, nil)
	if status != 401 {
		t.Fatal(status)
	}
	status, login, h := requestApp(t, a, "POST", "/api/auth/login", M{"username": "operator", "password": testPassword, "rememberMe": false}, http.Header{"Origin": {a.URL}})
	if status != 200 {
		t.Fatal(status, login)
	}
	auth := http.Header{"Origin": {a.URL}, "Cookie": {strings.Split(h.Get("Set-Cookie"), ";")[0]}}
	status, _, _ = requestApp(t, a, "PUT", endpoint, M{}, auth)
	if status != 403 {
		t.Fatal("enterprise CSRF bypass", status)
	}
	auth.Set("X-Tuck-CSRF", S(login["csrf"]))
	started := make(chan struct{})
	disconnected := make(chan struct{})
	f.setDAVHook(func(w http.ResponseWriter, r *http.Request, name string) bool {
		if name == "video.mp4" {
			w.Header().Set("Content-Length", "100000")
			w.Write([]byte(strings.Repeat("x", 64<<10)))
			w.(http.Flusher).Flush()
			close(started)
			<-r.Context().Done()
			close(disconnected)
			return true
		}
		return false
	})
	r, _ := http.NewRequest("GET", a.URL+endpoint, nil)
	r.Header = auth.Clone()
	response, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	<-started
	status, _, _ = requestApp(t, a, "POST", "/api/auth/logout", M{}, auth)
	if status != 200 {
		t.Fatal(status)
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	select {
	case <-disconnected:
	case <-time.After(time.Second):
		t.Fatal("logout did not cancel media")
	}
	a.Security.mu.Lock()
	if len(a.Security.watches) != 0 {
		t.Fatal("watch retained", a.Security.watches)
	}
	a.Security.mu.Unlock()
	// A real connection operation remains gated until its Core response completes.
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	f.p.change(func() {
		f.p.hook = func(w http.ResponseWriter, r *http.Request, b M) bool {
			if strings.HasSuffix(r.URL.Path, "/state") {
				close(entered)
				select {
				case <-release:
				case <-r.Context().Done():
				}
				WriteJSON(w, 503, M{"code": "unavailable"})
				return true
			}
			if strings.Contains(r.URL.Path, "/workspace") {
				f.serve(w, r, b)
				return true
			}
			return false
		}
	})
	status, login, h = requestApp(t, a, "POST", "/api/auth/login", M{"username": "operator", "password": testPassword, "rememberMe": false}, http.Header{"Origin": {a.URL}})
	if status != 200 {
		t.Fatal(status)
	}
	auth.Set("Cookie", strings.Split(h.Get("Set-Cookie"), ";")[0])
	auth.Set("X-Tuck-CSRF", S(login["csrf"]))
	completed := make(chan struct{})
	go func() {
		defer close(completed)
		r, _ := http.NewRequest("POST", a.URL+"/api/connection/sync", strings.NewReader("{}"))
		r.Header = auth.Clone()
		res, _ := http.DefaultClient.Do(r)
		if res != nil {
			res.Body.Close()
		}
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("sync not entered")
	}
	for range 3 {
		status, v, _ := requestApp(t, a, "GET", "/api/enterprise-workspace", nil, auth)
		if status != 409 || v["code"] != "workspace_connection_changing" {
			t.Fatal(status, v)
		}
	}
	// Explicit Core expiry is mapped to a workspace conflict, while Tuck login remains valid.
	once.Do(func() { close(release) })
	<-completed
	f.setDAVHook(func(w http.ResponseWriter, r *http.Request, name string) bool {
		WriteJSON(w, 401, M{"code": "session_expired", "message": "PRIVATE origin text"})
		return true
	})
	status, value, _ := requestApp(t, a, "GET", endpoint, nil, auth)
	if status != 409 || strings.Contains(string(JSON(value)), "PRIVATE") {
		t.Fatal(status, value)
	}
	status, value, _ = requestApp(t, a, "GET", "/api/auth/session", nil, auth)
	if status != 200 || !B(value["authenticated"]) {
		t.Fatal("Core expired local login", status, value)
	}
}
