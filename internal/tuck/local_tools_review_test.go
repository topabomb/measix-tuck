package tuck

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
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
			r.Header.Set("X-Tuck-Turn", first.Key)
		case "tools/call":
			r.Header.Set("X-Tuck-Turn", callKey.Load().(string))
		default:
			r.Header.Del("X-Tuck-Turn")
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
	session, err := sdk.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL + "/tuck-tools?agent=root", HTTPClient: client}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	tools, err := session.ListTools(ctx, nil)
	if err != nil || len(tools.Tools) != 7 {
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
		want := 5 // Only the two WebDAV tools disappear.
		if available {
			want = 7
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
		return headers["X-Tuck-Turn"], finish
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
	turn.Task = mustUpdate(t, l.Manager, turn.TaskID, M{"workspaceMode": "none", "managedWorkspaceId": nil})
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
	peer := &toolPeer{agent: "root", server: mcp.NewServer(&mcp.Implementation{Name: "none-catalog", Version: "1"}, nil)}
	l.installTools(peer, turn)
	if strings.Contains(peer.catalog, "download_runcove_files") || strings.Contains(peer.catalog, "generate_images") || !strings.Contains(peer.catalog, "upload_runcove_file") {
		t.Fatal("wrong none-mode catalog", peer.catalog)
	}
}

func TestLocalMCPReconnectInventory(t *testing.T) {
	f := newWorkspaceFixture(t)
	local, first := f.turn(t)
	server := httptest.NewServer(local)
	t.Cleanup(server.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	connect := func(key string, changes bool) *mcp.ClientSession {
		t.Helper()
		client := &http.Client{Transport: runtimeTransport(func(r *http.Request) (*http.Response, error) {
			r = r.Clone(r.Context())
			r.Header.Set("Authorization", "Bearer "+local.Models.Secret)
			r.Header.Set(tuckTurnHeader, key)
			return http.DefaultTransport.RoundTrip(r)
		})}
		options := &mcp.ClientOptions{}
		if changes {
			options.ToolListChangedHandler = func(ctx context.Context, req *mcp.ToolListChangedRequest) {
				_, _ = req.Session.ListTools(ctx, nil)
			}
		}
		sdk := mcp.NewClient(&mcp.Implementation{Name: "reconnect-review", Version: "1"}, options)
		session, e := sdk.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL + tuckToolsPath + "?agent=root", HTTPClient: client}, nil)
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { session.Close() })
		return session
	}
	initial := connect(first.Key, true)
	if initial.InitializeResult().Instructions != modelContent.Instructions(modelContent.Tools(modelContent.ToolNames(), nil)) {
		t.Fatal("initial inventory", initial.InitializeResult())
	}
	f.mu.Lock()
	f.projection["filesAvailable"] = false
	f.mu.Unlock()
	task := mustUpdate(t, local.Manager, first.TaskID, M{"turnId": UUID()})
	headers, finish, e := local.Begin(ctx, task)
	if e != nil {
		t.Fatal(e)
	}
	defer finish()
	reconnected := connect(headers[tuckTurnHeader], false)
	tools, e := reconnected.ListTools(ctx, nil)
	if e != nil || len(tools.Tools) != 5 {
		t.Fatal(tools, e)
	}
	names := []string{"list_conversations", "send_conversation_message", "deliver_result", "generate_images", "inspect_attachments"}
	expected := modelContent.Instructions(modelContent.Tools(names, nil))
	if reconnected.InitializeResult().Instructions != expected {
		t.Fatal("new discovery retained the previous inventory", reconnected.InitializeResult().Instructions, expected)
	}
}

func TestLocalMCPSecondCatalogClientAcrossTurns(t *testing.T) {
	f := newWorkspaceFixture(t)
	local, first := f.turn(t)
	server := httptest.NewServer(local)
	t.Cleanup(server.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	endpoint := server.URL + tuckToolsPath + "?agent=root"
	endpointURL, _ := url.Parse(endpoint)
	connect := func(key string) (*mcp.ClientSession, *atomic.Value, *cookiejar.Jar) {
		t.Helper()
		callKey := &atomic.Value{}
		callKey.Store(key)
		jar, e := cookiejar.New(nil)
		if e != nil {
			t.Fatal(e)
		}
		client := &http.Client{Jar: jar, Transport: runtimeTransport(func(r *http.Request) (*http.Response, error) {
			r = r.Clone(r.Context())
			r.Header.Set("Authorization", "Bearer "+local.Models.Secret)
			switch r.Header.Get("Mcp-Method") {
			case "server/discover", "tools/call":
				r.Header.Set(tuckTurnHeader, callKey.Load().(string))
			default:
				r.Header.Del(tuckTurnHeader)
			}
			return http.DefaultTransport.RoundTrip(r)
		})}
		sdk := mcp.NewClient(&mcp.Implementation{Name: "two-jar-review", Version: "1"}, &mcp.ClientOptions{ToolListChangedHandler: func(ctx context.Context, req *mcp.ToolListChangedRequest) {
			_, _ = req.Session.ListTools(ctx, nil)
		}})
		session, e := sdk.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint, HTTPClient: client}, nil)
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { session.Close() })
		if _, e = session.ListTools(ctx, nil); e != nil {
			t.Fatal(e)
		}
		return session, callKey, jar
	}
	firstClient, firstKey, _ := connect(first.Key)
	first.cancel()
	local.mu.Lock()
	delete(local.turns, first.Key)
	local.mu.Unlock()
	advance := func() (string, func()) {
		t.Helper()
		task := mustUpdate(t, local.Manager, first.TaskID, M{"turnId": UUID()})
		headers, finish, e := local.Begin(ctx, task)
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(finish)
		return headers[tuckTurnHeader], finish
	}
	secondTurnKey, finishSecond := advance()
	secondClient, secondKey, secondJar := connect(secondTurnKey)
	cookies := secondJar.Cookies(endpointURL)
	if len(cookies) != 1 || cookies[0].Value != first.Key {
		t.Fatal("second client did not retain the selected peer catalog identity", cookies)
	}
	finishSecond()
	thirdTurnKey, _ := advance()
	firstKey.Store(thirdTurnKey)
	secondKey.Store(thirdTurnKey)
	params := &mcp.CallToolParams{Name: "list_conversations", Arguments: M{}}
	firstResult, firstErr := firstClient.CallTool(ctx, params)
	if firstErr != nil || firstResult.IsError {
		t.Fatal("first client failed with live third-turn header", firstResult, firstErr)
	}
	// The stable catalog cookie must never grant execution authority.
	for _, key := range []string{secondTurnKey, ""} {
		secondKey.Store(key)
		result, e := secondClient.CallTool(ctx, params)
		if e != nil || !result.IsError || !strings.Contains(string(JSON(result)), "tool_not_permitted") {
			t.Fatal("expired or absent execution header gained catalog-only authority", result, e)
		}
	}
	foreign, e := local.Manager.Create(ctx, M{})
	if e != nil {
		t.Fatal(e)
	}
	foreign = mustUpdate(t, local.Manager, S(foreign["id"]), M{"turnId": UUID()})
	foreignHeaders, finishForeign, e := local.Begin(ctx, foreign)
	if e != nil {
		t.Fatal(e)
	}
	defer finishForeign()
	secondKey.Store(foreignHeaders[tuckTurnHeader])
	foreignResult, foreignErr := secondClient.CallTool(ctx, params)
	if foreignErr != nil || !foreignResult.IsError || !strings.Contains(string(JSON(foreignResult)), "tool_not_permitted") {
		t.Fatal("foreign task entered the reconnected client's catalog", foreignResult, foreignErr)
	}
	secondKey.Store(thirdTurnKey)
	secondResult, secondErr := secondClient.CallTool(ctx, params)
	if secondErr != nil || secondResult.IsError {
		t.Fatal("second client failed with live third-turn header after reconnecting in turn 2", secondResult, secondErr)
	}
}
