package tuck

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type workspaceFixture struct {
	p                  *platformFixture
	mu                 sync.Mutex
	files              map[string][]byte
	dirs               map[string]bool
	calls              []M
	projection         M
	davRoot, space, id string
	hook               func(http.ResponseWriter, *http.Request, string) bool
}

func newWorkspaceFixture(t *testing.T) *workspaceFixture {
	p := newPlatformFixture(t, 5)
	f := &workspaceFixture{p: p, files: map[string][]byte{"hello.txt": []byte("hello world"), "second.txt": []byte("second")}, dirs: map[string]bool{"": true, "folder": true}, space: "spc_22222222-2222-4222-8222-222222222222", id: "dav_11111111-1111-4111-8111-111111111111"}
	f.davRoot = "/runtime/v1/resources/" + f.id + "/spaces/" + f.space + "/dav/"
	p.snapshot["webdav"] = []any{M{"resourceId": f.id, "displayName": "Enterprise files", "enabled": true, "purpose": "WORKSPACE_FILE_EXCHANGE", "authOwnership": "ENTERPRISE_MANAGED", "clientProtocol": "WEBDAV", "rootPathTemplate": strings.ReplaceAll(f.davRoot, f.space, "{agentSpaceId}")}}
	f.projection = M{"schemaVersion": 1, "serviceState": "ENABLED", "state": "CONNECTED", "bindingRevision": 1, "agentSpaceId": f.space, "filesAvailable": true, "filesReason": "available", "mcpAvailable": false, "mcpReason": "disabled", "fileExchangeWebdavResourceId": f.id, "webdav": M{"resourceId": f.id, "available": true, "reason": "available", "accessMode": "FULL", "bindingRevision": 1}}
	p.hook = func(w http.ResponseWriter, r *http.Request, body M) bool {
		if strings.HasPrefix(r.URL.Path, f.davRoot) || strings.Contains(r.URL.Path, "/workspace") {
			f.serve(w, r, body)
			return true
		}
		return false
	}
	p.enroll()
	return f
}
func (f *workspaceFixture) entry(name string) M {
	if f.dirs[name] {
		return M{"path": name, "kind": "DIRECTORY", "etag": "\"dir\""}
	}
	return M{"path": name, "kind": "FILE", "size": len(f.files[name]), "etag": "\"" + Hash(f.files[name]) + "\"", "modifiedAt": "2026-10-15T00:00:00Z"}
}
func (f *workspaceFixture) serve(w http.ResponseWriter, r *http.Request, body M) {
	name := r.URL.Query().Get("path")
	dav := strings.HasPrefix(r.URL.Path, f.davRoot)
	if dav {
		name = strings.TrimPrefix(r.URL.Path, f.davRoot)
	}
	f.mu.Lock()
	f.calls = append(f.calls, M{"path": name, "method": r.Method, "headers": mapHeaders(r.Header)})
	hook := f.hook
	f.mu.Unlock()
	if hook != nil && hook(w, r, name) {
		return
	}
	if r.Header.Get("Authorization") != "Bearer synthetic.access.token" {
		WriteJSON(w, 401, M{"code": "unauthorized"})
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if strings.HasSuffix(r.URL.Path, "/workspace") {
		WriteJSON(w, 200, f.projection)
		return
	}
	value, exists := f.files[name]
	directory := f.dirs[name]
	etag := "\"" + Hash(value) + "\""
	if dav {
		switch r.Method {
		case "PROPFIND":
			if !exists && !directory {
				w.WriteHeader(404)
				return
			}
			kind := ""
			if directory {
				kind = "<d:collection/>"
			}
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(207)
			fmt.Fprintf(w, `<d:multistatus xmlns:d="DAV:"><d:response><d:href>%s</d:href><d:propstat><d:prop><d:resourcetype>%s</d:resourcetype><d:getetag>%s</d:getetag><d:getcontentlength>%d</d:getcontentlength></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response></d:multistatus>`, html.EscapeString(f.davRoot+encodeRemote(name)), kind, etag, len(value))
			return
		case "MKCOL":
			f.dirs[name] = true
			w.WriteHeader(201)
			return
		}
	}
	if strings.HasSuffix(r.URL.Path, "/files") {
		if r.Method == "GET" {
			entries := []any{}
			for n := range f.files {
				parent := path.Dir(n)
				if parent == "." {
					parent = ""
				}
				if parent == name {
					entries = append(entries, f.entry(n))
				}
			}
			for n := range f.dirs {
				parent := path.Dir(n)
				if parent == "." {
					parent = ""
				}
				if n != "" && parent == name {
					entries = append(entries, f.entry(n))
				}
			}
			WriteJSON(w, 200, M{"entries": entries, "usedBytes": 1000, "availableBytes": 1000000})
			return
		}
		if body["action"] == "MKCOL" {
			f.dirs[S(body["path"])] = true
		}
		if body["action"] == "DELETE" {
			delete(f.files, S(body["path"]))
		}
		WriteJSON(w, 200, M{"outcome": "SUCCEEDED", "failures": []any{}, "truncated": false})
		return
	}
	if r.Header.Get("If-Match") != "" && r.Header.Get("If-Match") != etag || r.Header.Get("If-None-Match") == "*" && exists {
		WriteJSON(w, 412, M{"code": "file_version_conflict"})
		return
	}
	if r.Method == "PUT" {
		raw, e := io.ReadAll(r.Body)
		if e != nil {
			return
		}
		f.files[name] = raw
		if dav {
			w.WriteHeader(201)
		} else {
			WriteJSON(w, 200, M{"outcome": "SUCCEEDED", "failures": []any{}, "truncated": false})
		}
		return
	}
	if !exists {
		WriteJSON(w, 404, M{"code": "file_not_found"})
		return
	}
	w.Header().Set("ETag", etag)
	http.ServeContent(w, r, name, time.Unix(0, 0), bytes.NewReader(value))
}
func (f *workspaceFixture) turn(t *testing.T) (*LocalTools, *toolTurn) {
	t.Helper()
	m, s, _, id := taskFixture(t)
	files := &Files{Manager: m, Models: f.p.models}
	d, e := NewDeliveries(m, s, f.p.models, filepath.Join(t.TempDir(), "deliveries"))
	if e != nil {
		t.Fatal(e)
	}
	l := NewLocalTools(f.p.models, m, files, d)
	d.Local = l
	t.Cleanup(l.Close)
	task := mustUpdate(t, m, id, M{"turnId": UUID()})
	headers, finish, e := l.Begin(context.Background(), task)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(finish)
	turn := l.turns[headers["X-Tuck-Workspace-Turn"]]
	if turn.Target == nil {
		t.Fatal("workspace not exposed", f.p.platform.Error())
	}
	return l, turn
}
func TestWorkspaceToolsRoundTripAndPartialBatches(t *testing.T) {
	f := newWorkspaceFixture(t)
	l, tr := f.turn(t)
	os.WriteFile(filepath.Join(tr.Workspace, "source.bin"), []byte{0, 1, 255, 7}, 0600)
	r := l.Transfer(context.Background(), tr, "upload_runcove_file", M{"file_path": "source.bin", "remote_path": "reports/测试 #%.bin"})
	if r["status"] != "completed" {
		t.Fatal(r)
	}
	args := M{"remote_paths": []any{"reports/测试 #%.bin"}, "local_directory": "out"}
	r = l.Transfer(context.Background(), tr, "download_runcove_files", args)
	if r["status"] != "completed" || I(r["completed_count"]) != 1 {
		t.Fatal(r)
	}
	got, e := os.ReadFile(filepath.Join(tr.Workspace, "out/测试 #%.bin"))
	if e != nil || !bytes.Equal(got, []byte{0, 1, 255, 7}) {
		t.Fatal(got, e)
	}
	if r = l.Transfer(context.Background(), tr, "download_runcove_files", args); r["code"] != "local_file_exists" {
		t.Fatal(r)
	}
	args["overwrite"] = true
	if r = l.Transfer(context.Background(), tr, "download_runcove_files", args); r["status"] != "completed" {
		t.Fatal(r)
	}
	r = l.Transfer(context.Background(), tr, "download_runcove_files", M{"remote_paths": []any{"hello.txt", "missing.txt", "second.txt"}, "local_directory": "partial"})
	if r["status"] != "partial" || I(r["completed_count"]) != 2 || I(r["failed_count"]) != 1 || len(A(r["files"])) != 3 {
		t.Fatal(r)
	}
	if strings.Contains(string(JSON(r)), "synthetic.access") {
		t.Fatal("credential exposed")
	}
}
func TestWorkspaceToolsPreflightAndConcurrentPublication(t *testing.T) {
	f := newWorkspaceFixture(t)
	l, tr := f.turn(t)
	for _, args := range []M{{"remote_paths": []any{"a/same.txt", "b/same.txt"}, "local_directory": "not-created", "overwrite": true}, {"remote_paths": []any{"../hello.txt"}, "local_directory": "bad"}, {"remote_paths": []any{"hello.txt"}, "local_directory": "../../outside"}, {"remote_paths": []any{".agent-space/data"}, "local_directory": "bad"}, {"remote_paths": []any{"hello.txt"}, "local_directory": "bad", "overwrite": 123}} {
		r := l.Transfer(context.Background(), tr, "download_runcove_files", args)
		if r["status"] != "failed" {
			t.Fatal(r)
		}
	}
	if _, e := os.Stat(filepath.Join(tr.Workspace, "not-created")); !os.IsNotExist(e) {
		t.Fatal("preflight created directory")
	}
	f.mu.Lock()
	for _, r := range f.calls {
		if r["method"] == "GET" && S(r["path"]) != "" {
			t.Fatal("preflight downloaded")
		}
	}
	f.mu.Unlock()
	os.MkdirAll(filepath.Join(tr.Workspace, "blocked/hello.txt"), 0700)
	r := l.Transfer(context.Background(), tr, "download_runcove_files", M{"remote_paths": []any{"hello.txt"}, "local_directory": "blocked", "overwrite": true})
	if r["code"] != "local_file_exists" {
		t.Fatal(r)
	}
	var wg sync.WaitGroup
	out := make(chan M, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out <- l.Transfer(context.Background(), tr, "download_runcove_files", M{"remote_paths": []any{"hello.txt"}, "local_directory": "concurrent"})
		}()
	}
	wg.Wait()
	close(out)
	success := 0
	for r := range out {
		if r["status"] == "completed" {
			success++
		}
	}
	if success != 1 {
		t.Fatal("both replaced destination", success)
	}
	entries, _ := os.ReadDir(filepath.Join(tr.Workspace, "concurrent"))
	if len(entries) != 1 {
		t.Fatal("temporary files retained", entries)
	}
}
func TestWorkspaceToolsCancellationIdentityAndUnknownUpload(t *testing.T) {
	f := newWorkspaceFixture(t)
	l, tr := f.turn(t)
	os.MkdirAll(filepath.Join(tr.Workspace, "replace"), 0700)
	for _, name := range []string{"hello.txt", "second.txt"} {
		os.WriteFile(filepath.Join(tr.Workspace, "replace", name), []byte("old"), 0600)
	}
	started := make(chan struct{})
	f.mu.Lock()
	f.hook = func(w http.ResponseWriter, r *http.Request, name string) bool {
		if r.Method == "GET" && name == "second.txt" {
			w.Header().Set("Content-Length", "1024")
			w.WriteHeader(200)
			w.Write([]byte("partial"))
			w.(http.Flusher).Flush()
			close(started)
			<-r.Context().Done()
			return true
		}
		return false
	}
	f.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan M, 1)
	go func() {
		done <- l.Transfer(ctx, tr, "download_runcove_files", M{"remote_paths": []any{"hello.txt", "second.txt", "third.txt"}, "local_directory": "replace", "overwrite": true})
	}()
	<-started
	cancel()
	r := <-done
	if r["status"] != "partial" || O(A(r["files"])[1])["code"] != "operation_canceled" || O(A(r["files"])[2])["code"] != "not_attempted" {
		t.Fatal(r)
	}
	old, _ := os.ReadFile(filepath.Join(tr.Workspace, "replace/second.txt"))
	if string(old) != "old" {
		t.Fatal("old bytes changed")
	}
	f.mu.Lock()
	f.hook = func(w http.ResponseWriter, r *http.Request, name string) bool {
		if r.Method == "PUT" {
			io.Copy(io.Discard, r.Body)
			conn, _, _ := w.(http.Hijacker).Hijack()
			conn.Close()
			return true
		}
		return false
	}
	f.mu.Unlock()
	os.WriteFile(filepath.Join(tr.Workspace, "upload.txt"), []byte("unknown result"), 0600)
	r = l.Transfer(context.Background(), tr, "upload_runcove_file", M{"file_path": "upload.txt", "remote_path": "unknown.txt"})
	if r["status"] != "unknown" || r["code"] != "outcome_unknown" {
		t.Fatal(r)
	}
	f.mu.Lock()
	O(f.projection["webdav"])["bindingRevision"] = 2
	f.mu.Unlock()
	r = l.Transfer(context.Background(), tr, "download_runcove_files", M{"remote_paths": []any{"hello.txt"}, "local_directory": "stale"})
	if r["code"] != "tool_not_permitted" {
		t.Fatal(r)
	}
}
func TestWorkspaceToolsLargeFileAndDestinationRedirection(t *testing.T) {
	f := newWorkspaceFixture(t)
	l, tr := f.turn(t)
	blob := bytes.Repeat([]byte{73}, 17<<20)
	f.mu.Lock()
	f.files["large.bin"] = blob
	f.mu.Unlock()
	r := l.Transfer(context.Background(), tr, "download_runcove_files", M{"remote_paths": []any{"large.bin"}, "local_directory": "large"})
	if r["status"] != "completed" || len(JSON(r)) > 4000 {
		t.Fatal(r)
	}
	got, _ := os.ReadFile(filepath.Join(tr.Workspace, "large/large.bin"))
	if !bytes.Equal(got, blob) {
		t.Fatal("large bytes changed")
	}
	outside := t.TempDir()
	dest := filepath.Join(tr.Workspace, "redirect")
	os.MkdirAll(dest, 0700)
	os.WriteFile(filepath.Join(dest, "hello.txt"), []byte("keep"), 0600)
	probe := filepath.Join(tr.Workspace, "symlink-probe")
	if e := directoryLink(outside, probe); e != nil {
		t.Fatal("directory link fixture unavailable", e)
	}
	os.Remove(probe)
	var moveBlocked atomic.Bool
	f.mu.Lock()
	f.hook = func(w http.ResponseWriter, r *http.Request, name string) bool {
		if r.Method == "GET" && name == "hello.txt" {
			if e := os.Rename(dest, filepath.Join(outside, "moved")); e != nil {
				if runtime.GOOS != "windows" || !os.IsPermission(e) {
					t.Error(e)
				}
				moveBlocked.Store(true)
			} else if e := directoryLink(filepath.Join(outside, "moved"), dest); e != nil {
				t.Error(e)
			}
			w.Write([]byte("hello world"))
			return true
		}
		return false
	}
	f.mu.Unlock()
	r = l.Transfer(context.Background(), tr, "download_runcove_files", M{"remote_paths": []any{"hello.txt"}, "local_directory": "redirect", "overwrite": true})
	if moveBlocked.Load() {
		// Windows os.Root keeps the directory open without delete sharing, so
		// the attack is rejected before redirection. No outside file may exist.
		if r["status"] != "completed" {
			t.Fatal(r)
		}
		if _, e := os.Stat(filepath.Join(outside, "moved")); !os.IsNotExist(e) {
			t.Fatal("blocked rename published outside", e)
		}
		t.Log("Windows directory handle rejected redirection before publication")
		return
	}
	if r["status"] != "failed" {
		t.Fatal(r)
	}
	got, _ = os.ReadFile(filepath.Join(outside, "moved/hello.txt"))
	if string(got) != "keep" {
		t.Fatal("outside overwritten")
	}
}
func (f *workspaceFixture) proxy(t *testing.T) (*EnterpriseWorkspace, *httptest.Server, string) {
	t.Helper()
	e := NewEnterpriseWorkspace(f.p.platform)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := e.Handle(w, r); err != nil {
			WriteError(w, err)
		}
	}))
	t.Cleanup(s.Close)
	t.Cleanup(e.Close)
	q := url.Values{"agentSpaceId": {f.space}, "subject": {workspaceSubject(f.p.platform.Binding())}}
	return e, s, q.Encode()
}
func TestEnterpriseWorkspaceStreamingConditionsZIPAndPaths(t *testing.T) {
	f := newWorkspaceFixture(t)
	_, s, q := f.proxy(t)
	base := s.URL + "/api/enterprise-workspace"
	r, e := http.Get(base)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if r.StatusCode != 200 || bytes.Contains(raw, []byte("synthetic.access")) {
		t.Fatal(r.StatusCode, string(raw))
	}
	req, _ := http.NewRequest("GET", base+"/content?"+q+"&path=hello.txt&preview=1", nil)
	req.Header.Set("Range", "bytes=1-3")
	r, e = http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ = io.ReadAll(r.Body)
	r.Body.Close()
	if r.StatusCode != 206 || string(raw) != "ell" || r.Header.Get("Content-Range") == "" {
		t.Fatal(r.StatusCode, string(raw))
	}
	req, _ = http.NewRequest("PUT", base+"/content?"+q+"&path=hello.txt&version="+url.QueryEscape("\"stale\""), strings.NewReader("bad"))
	r, e = http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	r.Body.Close()
	if r.StatusCode != 412 {
		t.Fatal(r.StatusCode)
	}
	r, e = http.Get(base + "/archive?" + q + "&paths=" + url.QueryEscape(`["hello.txt","second.txt"]`))
	if e != nil {
		t.Fatal(e)
	}
	raw, e = io.ReadAll(r.Body)
	r.Body.Close()
	if e != nil {
		t.Fatal(e)
	}
	archive, e := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if e != nil || len(archive.File) != 2 {
		t.Fatal(e, len(raw))
	}
	for _, bad := range []string{"../secret", "/absolute", "a//b", "a/./b", "x\x00", "x\\y"} {
		r, e = http.Get(base + "/content?" + q + "&path=" + url.QueryEscape(bad))
		if e != nil {
			t.Fatal(e)
		}
		r.Body.Close()
		if r.StatusCode < 400 {
			t.Fatal("invalid path accepted", bad)
		}
	}
	if value, e := WorkspacePath("literal%2Fname.txt", false); e != nil || value != "literal%2Fname.txt" {
		t.Fatal("double decoding")
	}
}
func TestEnterpriseWorkspaceTruncationEarlyErrorsAndCancellation(t *testing.T) {
	f := newWorkspaceFixture(t)
	e, s, q := f.proxy(t)
	base := s.URL + "/api/enterprise-workspace"
	f.mu.Lock()
	f.hook = func(w http.ResponseWriter, r *http.Request, name string) bool {
		if strings.HasSuffix(r.URL.Path, "/content") && r.Method == "GET" {
			w.Header().Set("Content-Length", "1000")
			w.Write([]byte("short"))
			return true
		}
		return false
	}
	f.mu.Unlock()
	r, err := http.Get(base + "/archive?" + q + "&paths=" + url.QueryEscape(`["hello.txt"]`))
	if err == nil {
		_, err = io.ReadAll(r.Body)
		r.Body.Close()
	}
	if err == nil {
		t.Fatal("truncated ZIP appeared complete")
	}
	for _, status := range []int{413, 504} {
		f.mu.Lock()
		f.hook = func(w http.ResponseWriter, r *http.Request, name string) bool {
			if r.Method == "PUT" {
				WriteJSON(w, status, M{"code": "upstream_limit", "message": "PRIVATE Bearer synthetic.access.token"})
				return true
			}
			return false
		}
		f.mu.Unlock()
		reader, writer := io.Pipe()
		req, _ := http.NewRequest("PUT", base+"/content?"+q+"&path=large.bin", reader)
		req.ContentLength = 1 << 30
		done := make(chan *http.Response, 1)
		go func() { resp, _ := http.DefaultClient.Do(req); done <- resp }()
		go writer.Write(make([]byte, 32<<10))
		select {
		case response := <-done:
			if response == nil || response.StatusCode != status {
				t.Fatal(status, response)
			}
			raw, _ := io.ReadAll(response.Body)
			response.Body.Close()
			if bytes.Contains(raw, []byte("synthetic.access")) {
				t.Fatal("upstream prose leaked")
			}
		case <-time.After(3 * time.Second):
			t.Fatal("early upstream error lost", status)
		}
		writer.Close()
	}
	started := make(chan struct{})
	closed := make(chan struct{})
	f.mu.Lock()
	f.hook = func(w http.ResponseWriter, r *http.Request, name string) bool {
		if r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/content") {
			w.Header().Set("Content-Length", "2147483648")
			w.Write(make([]byte, 4096))
			w.(http.Flusher).Flush()
			close(started)
			<-r.Context().Done()
			close(closed)
			return true
		}
		return false
	}
	f.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", base+"/content?"+q+"&path=large.bin", nil)
	r, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	<-started
	cancel()
	r.Body.Close()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Core stream not cancelled")
	}
	deadline := time.Now().Add(time.Second)
	for {
		e.mu.Lock()
		count := len(e.leases)
		e.mu.Unlock()
		if count == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("lease retained")
		}
		time.Sleep(time.Millisecond)
	}
}
func TestEnterpriseWorkspaceConcurrentLargeRangeControls(t *testing.T) {
	f := newWorkspaceFixture(t)
	_, s, q := f.proxy(t)
	f.mu.Lock()
	f.hook = func(w http.ResponseWriter, r *http.Request, name string) bool {
		if strings.HasSuffix(r.URL.Path, "/content") && r.Method == "GET" && name == "large.bin" {
			w.Header().Set("Content-Length", "2147483648")
			w.Header().Set("Content-Range", "bytes 0-2147483647/2147483648")
			w.WriteHeader(206)
			w.Write(make([]byte, 64<<10))
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return true
		}
		return false
	}
	f.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	responses := []*http.Response{}
	for range 2 {
		req, _ := http.NewRequestWithContext(ctx, "GET", s.URL+"/api/enterprise-workspace/content?"+q+"&path=large.bin", nil)
		req.Header.Set("Range", "bytes=0-2147483647")
		r, e := http.DefaultClient.Do(req)
		if e != nil || r.StatusCode != 206 {
			t.Fatal(r, e)
		}
		responses = append(responses, r)
	}
	defer func() {
		for _, r := range responses {
			r.Body.Close()
		}
	}()
	for _, route := range []string{"", "/files?" + q + "&path="} {
		start := time.Now()
		r, e := http.Get(s.URL + "/api/enterprise-workspace" + route)
		if e != nil {
			t.Fatal(e)
		}
		io.Copy(io.Discard, r.Body)
		r.Body.Close()
		if time.Since(start) > 2*time.Second || r.StatusCode != 200 {
			t.Fatal("control blocked", time.Since(start), r.StatusCode)
		}
	}
	for _, method := range []string{"HEAD", "PUT"} {
		start := time.Now()
		var body io.Reader
		if method == "PUT" {
			body = strings.NewReader("control upload")
		}
		req, _ := http.NewRequest(method, s.URL+"/api/enterprise-workspace/content?"+q+"&path=hello.txt", body)
		r, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		io.Copy(io.Discard, r.Body)
		r.Body.Close()
		if r.StatusCode != 200 || time.Since(start) > 2*time.Second {
			t.Fatal(method, "blocked", r.StatusCode, time.Since(start))
		}
	}
	t.Log("two 2 GiB Range responses remained active while workspace controls completed under 2 seconds")
}
