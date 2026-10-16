package tuck

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestLocalMCPCatalogCookieTurnAndPeerLifecycle(t *testing.T) {
	f := newWorkspaceFixture(t)
	l, first := f.turn(t)
	server := httptest.NewServer(l)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var callKey atomic.Value
	callKey.Store(first.Key)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar, Transport: runtimeTransport(func(r *http.Request) (*http.Response, error) {
		r = r.Clone(r.Context())
		r.Header.Set("Authorization", "Bearer "+l.Models.Secret)
		// Match the engine's public limitation: only discover/call carry a
		// turn context. Background catalog requests must use the cookie.
		switch r.Header.Get("Mcp-Method") {
		case "server/discover":
			r.Header.Set("X-Tuck-Workspace-Turn", first.Key)
		case "tools/call":
			r.Header.Set("X-Tuck-Workspace-Turn", callKey.Load().(string))
		default:
			r.Header.Del("X-Tuck-Workspace-Turn")
		}
		return http.DefaultTransport.RoundTrip(r)
	})}
	changes := make(chan int, 8)
	clientErrors := make(chan error, 8)
	sdk := mcp.NewClient(&mcp.Implementation{Name: "lifecycle-review", Version: "1"}, &mcp.ClientOptions{ToolListChangedHandler: func(ctx context.Context, req *mcp.ToolListChangedRequest) {
		tools, e := req.Session.ListTools(ctx, nil)
		if e != nil {
			clientErrors <- e
			return
		}
		changes <- len(tools.Tools)
	}})
	session, err := sdk.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL + "/workspace-tools", HTTPClient: client}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	tools, err := session.ListTools(ctx, nil)
	if err != nil || len(tools.Tools) != 3 {
		t.Fatal(tools, err)
	}
	advance := func(available bool) (string, func()) {
		t.Helper()
		f.mu.Lock()
		f.projection["filesAvailable"] = available
		f.mu.Unlock()
		task := mustUpdate(t, l.Manager, first.TaskID, M{"turnId": UUID()})
		headers, finish, e := l.Begin(ctx, task)
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(finish)
		want := 1 // Image generation remains available without WebDAV.
		if available {
			want = 3
		}
		select {
		case got := <-changes:
			if got != want {
				t.Fatal("catalog did not follow the new turn", got, want)
			}
		case e := <-clientErrors:
			t.Fatal(e)
		case <-ctx.Done():
			t.Fatal("catalog subscription failed", ctx.Err())
		}
		return headers["X-Tuck-Workspace-Turn"], finish
	}
	first.cancel()
	l.mu.Lock()
	delete(l.turns, first.Key)
	l.mu.Unlock()
	_, finishSecond := advance(false)
	finishSecond()
	thirdKey, finishThird := advance(true)
	args := &mcp.CallToolParams{Name: "download_runcove_files", Arguments: M{"remote_paths": []any{"hello.txt"}, "local_directory": "fresh-turn"}}
	result, err := session.CallTool(ctx, args)
	if err != nil || !result.IsError || !strings.Contains(string(JSON(result)), "tool_not_permitted") {
		t.Fatal("expired execution header gained catalog-only authority", result, err)
	}
	callKey.Store(thirdKey)
	result, err = session.CallTool(ctx, args)
	if err != nil || result.IsError {
		t.Fatal("live turn could not reuse the catalog subscription", result, err)
	}
	data, err := os.ReadFile(filepath.Join(first.Workspace, "fresh-turn", "hello.txt"))
	if err != nil || string(data) != "hello world" {
		t.Fatal(string(data), err)
	}
	entered, disconnected := make(chan struct{}), make(chan struct{})
	f.setDAVHook(func(w http.ResponseWriter, r *http.Request, name string) bool {
		if r.Method == "GET" && name == "second.txt" {
			w.Header().Set("Content-Length", "10000")
			w.Write([]byte("unfinished"))
			w.(http.Flusher).Flush()
			close(entered)
			<-r.Context().Done()
			close(disconnected)
			return true
		}
		return false
	})
	callDone := make(chan error, 1)
	go func() {
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "download_runcove_files", Arguments: M{"remote_paths": []any{"second.txt"}, "local_directory": "canceled-turn"}})
		if err == nil && !result.IsError {
			err = Problem(500, "unexpected_success", "ended turn completed its transfer")
		}
		callDone <- err
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("in-flight tool never started")
	}
	finishThird()
	select {
	case <-disconnected:
	case <-ctx.Done():
		t.Fatal("ended turn retained upstream request")
	}
	select {
	case e := <-callDone:
		if e != nil {
			t.Fatal("turn cancellation lost the MCP result", e)
		}
	case <-ctx.Done():
		t.Fatal("ended turn retained its tool call")
	}
	entries, err := os.ReadDir(filepath.Join(first.Workspace, "canceled-turn"))
	if err != nil && !os.IsNotExist(err) || len(entries) != 0 {
		t.Fatal("ended turn published partial bytes", entries, err)
	}
	if err = session.Close(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		l.mu.RLock()
		peers, turns := len(l.peers), len(l.turns)
		l.mu.RUnlock()
		if peers == 0 && turns == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("closed client retained an owner", peers, turns)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestNoneWorkspaceRejectsTuckWrites(t *testing.T) {
	f := newWorkspaceFixture(t)
	l, turn := f.turn(t)
	turn.Task = mustUpdate(t, l.Manager, turn.TaskID, M{"workspaceMode": "none"})
	path := filepath.Join(turn.Workspace, "existing.txt")
	if e := os.WriteFile(path, []byte("keep"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := l.Files.Upload(turn.Task, uploadData("new.txt", []byte("new"))); Code(e) != "workspace_required" {
		t.Fatal("upload reached native directory", e)
	}
	if e := l.Files.Save(turn.Task, M{"path": "existing.txt", "content": "changed"}); Code(e) != "workspace_required" {
		t.Fatal("editor reached native directory", e)
	}
	if _, e := l.Files.SaveGeneratedImages(turn.ctx, turn.Task, []GeneratedImage{{Bytes: []byte("image"), MIME: "image/png"}}, "prompt"); Code(e) != "workspace_required" {
		t.Fatal("image publication reached native directory", e)
	}
	f.mu.Lock()
	before := len(f.calls)
	f.mu.Unlock()
	result := l.Transfer(turn.ctx, turn, "download_runcove_files", M{"remote_paths": []any{"hello.txt"}, "local_directory": "."})
	if result["code"] != "workspace_required" {
		t.Fatal("download reached native directory", result)
	}
	f.mu.Lock()
	after := len(f.calls)
	f.mu.Unlock()
	if before != after {
		t.Fatal("rejected download reached upstream")
	}
	entries, e := os.ReadDir(turn.Workspace)
	data, readErr := os.ReadFile(path)
	if e != nil || readErr != nil || len(entries) != 1 || string(data) != "keep" {
		t.Fatal("none directory changed", entries, e, readErr)
	}
	peer := &toolPeer{kind: "workspace", server: mcp.NewServer(&mcp.Implementation{Name: "none-catalog", Version: "1"}, nil)}
	l.installTools(peer, turn)
	if strings.Contains(peer.catalog, "download_runcove_files") || strings.Contains(peer.catalog, "generate_images") || !strings.Contains(peer.catalog, "upload_runcove_file") {
		t.Fatal("wrong none-mode catalog", peer.catalog)
	}
}
