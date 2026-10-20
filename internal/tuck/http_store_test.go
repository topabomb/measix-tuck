package tuck

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func requestApp(t *testing.T, a *App, method, path string, body M, headers http.Header) (int, M, http.Header) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(JSON(body))
	}
	r, _ := http.NewRequest(method, a.URL+path, reader)
	r.Header = headers.Clone()
	if r.Header == nil {
		r.Header = http.Header{}
	}
	r.Header.Set("Content-Type", "application/json")
	response, e := http.DefaultClient.Do(r)
	if e != nil {
		t.Fatal(e)
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(response.Body)
	v, _ := DecodeJSON(raw, 128<<20)
	return response.StatusCode, O(v), response.Header
}
func TestHTTPAuthenticationFilesSSEAndReverseProxy(t *testing.T) {
	f := newPlatformFixture(t, 5)
	f.enroll()
	app := f.startApp()
	task, e := app.Manager.Create(context.Background(), M{})
	if e != nil {
		t.Fatal(e)
	}
	h, _ := PasswordHash(testPassword, 12)
	c, rev, _ := app.Config.Read()
	O(c["security"])["auth"] = M{"enabled": true, "username": "operator", "passwordHash": h}
	if _, e = app.Config.Save(rev, c); e != nil {
		t.Fatal(e)
	}
	app.Config.MarkApplied()
	for _, path := range []string{"/api/runtime", "/api/tasks", "/api/tasks/" + S(task["id"]) + "/files", "/api/events", "/api/tools/list-sessions"} {
		status, _, _ := requestApp(t, app, "GET", path, nil, nil)
		if status != 401 {
			t.Fatal(path, status)
		}
	}
	status, _, _ := requestApp(t, app, "GET", "/api/tasks", nil, http.Header{"Authorization": {"Bearer " + app.Models.Secret}})
	if status != 401 {
		t.Fatal("internal token escaped scope", status)
	}
	status, info, headers := requestApp(t, app, "POST", "/api/auth/login", M{"username": "operator", "password": testPassword, "rememberMe": true}, http.Header{"Origin": {app.URL}})
	if status != 200 {
		t.Fatal(status, info)
	}
	cookie := strings.Split(headers.Get("Set-Cookie"), ";")[0]
	auth := http.Header{"Cookie": {cookie}, "Origin": {app.URL}, "X-Tuck-Csrf": {S(info["csrf"])}}
	status, _, _ = requestApp(t, app, "GET", "/api/tasks", nil, auth)
	if status != 200 {
		t.Fatal(status)
	}
	csrfMissing := auth.Clone()
	csrfMissing.Del("X-Tuck-CSRF")
	for _, check := range []struct {
		headers http.Header
		status  int
	}{{nil, 401}, {csrfMissing, 403}} {
		status, _, _ = requestApp(t, app, "POST", "/api/tasks/"+S(task["id"])+"/delete", M{"confirmed": true}, check.headers)
		if status != check.status {
			t.Fatal("delete bypassed authentication/CSRF", status)
		}
	}
	status, _, _ = requestApp(t, app, "POST", "/api/tasks", M{}, csrfMissing)
	if status != 403 {
		t.Fatal(status)
	}
	u, _ := url.Parse(app.URL)
	proxy := httptest.NewServer(httputil.NewSingleHostReverseProxy(u))
	defer proxy.Close()
	r, _ := http.NewRequest("GET", proxy.URL+"/api/tasks", nil)
	r.Header.Set("Cookie", cookie)
	r.Header.Set("X-Forwarded-For", "198.51.100.12")
	response, e := http.DefaultClient.Do(r)
	if e != nil {
		t.Fatal(e)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal("reverse proxy auth", response.StatusCode)
	}
	r, _ = http.NewRequest("GET", app.URL+"/api/events", nil)
	r.Header.Set("Cookie", cookie)
	stream, e := http.DefaultClient.Do(r)
	if e != nil {
		t.Fatal(e)
	}
	reader := bufio.NewReader(stream.Body)
	if _, e = reader.ReadString('\n'); e != nil {
		t.Fatal(e)
	}
	status, _, _ = requestApp(t, app, "POST", "/api/auth/logout", M{}, auth)
	if status != 200 {
		t.Fatal(status)
	}
	done := make(chan error, 1)
	go func() { _, e := io.Copy(io.Discard, reader); done <- e }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("logout did not close SSE")
	}
	stream.Body.Close()
	status, _, _ = requestApp(t, app, "GET", "/api/tasks", nil, auth)
	if status != 401 {
		t.Fatal(status)
	}
	session := connectTuckTools(t, app, task, "root")
	callTuckTool(t, session, "list_conversations", M{}, false)
	status, _, _ = requestApp(t, app, "POST", "/api/tools/list-sessions", M{}, http.Header{"Authorization": {"Bearer " + app.Models.Secret}})
	if status != 401 {
		t.Fatal("removed API tool bypassed browser authentication", status)
	}
}
func TestHTTPSettingsProtectionVisibilityAndReplay(t *testing.T) {
	f := newPlatformFixture(t, 5)
	f.enroll()
	app := f.startApp()
	h, _ := PasswordHash("six-ok", 6)
	c, rev, _ := app.Config.Read()
	O(c["security"])["settingsProtection"] = M{"passwordHash": h, "categories": []any{"providers", "platform", "engine"}}
	if _, e := app.Config.Save(rev, c); e != nil {
		t.Fatal(e)
	}
	for _, path := range []string{"/api/settings/engine", "/api/settings/protection"} {
		status, _, _ := requestApp(t, app, "GET", path, nil, nil)
		if status != 403 {
			t.Fatal(path, status)
		}
	}
	status, v, _ := requestApp(t, app, "GET", "/api/settings/runtime?section=agents", nil, nil)
	if status != 200 || O(v["config"])["providers"] != nil {
		t.Fatal("hidden provider config", status, v)
	}
	status, runtimeView, _ := requestApp(t, app, "GET", "/api/runtime", nil, nil)
	if status != 200 || len(A(O(runtimeView["connection"])["models"])) == 0 {
		t.Fatal("working catalog hidden", runtimeView)
	}
	status, _, _ = requestApp(t, app, "PUT", "/api/settings/protection", M{"revision": rev, "categories": []any{}, "password": ""}, nil)
	if status != 403 {
		t.Fatal("originless save", status)
	}
	_, cursor, _ := app.Config.Read()
	_ = cursor
	before, _ := app.Store.Cursor()
	app.Manager.PublishGlobal(M{"type": "connection", "connection": Merge(app.Models.Summary(), M{"employeeName": "PRIVATE_LOCKED_EMPLOYEE"})})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r, _ := http.NewRequestWithContext(ctx, "GET", app.URL+"/api/events?since="+strconv.FormatInt(before, 10), nil)
	response, e := http.DefaultClient.Do(r)
	if e != nil {
		t.Fatal(e)
	}
	reader := bufio.NewReader(response.Body)
	packet := ""
	for {
		line, e := reader.ReadString('\n')
		if e != nil {
			t.Fatal(e)
		}
		packet += line
		if strings.HasPrefix(line, "data:") {
			break
		}
	}
	cancel()
	response.Body.Close()
	if strings.Contains(packet, "PRIVATE_LOCKED_EMPLOYEE") || strings.Contains(packet, h) {
		t.Fatal("replayed locked configuration", packet)
	}
	status, info, headers := requestApp(t, app, "POST", "/api/settings/unlock", M{"password": "six-ok"}, http.Header{"Origin": {app.URL}})
	if status != 200 || !B(info["unlocked"]) {
		t.Fatal(info)
	}
	auth := http.Header{"Origin": {app.URL}, "Cookie": {strings.Split(headers.Get("Set-Cookie"), ";")[0]}}
	status, _, _ = requestApp(t, app, "GET", "/api/settings/engine", nil, auth)
	if status != 200 {
		t.Fatal(status)
	}
}
func TestHTTPReplayBackpressureLargeEventsAndShutdown(t *testing.T) {
	f := newPlatformFixture(t, 5)
	app := f.startApp()
	before, _ := app.Store.Cursor()
	for i := 0; i < 600; i++ {
		if e := app.Manager.PublishGlobal(M{"type": "fixture", "index": i, "padding": strings.Repeat("x", 16384)}); e != nil {
			t.Fatal(e)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r, _ := http.NewRequestWithContext(ctx, "GET", app.URL+"/api/events?since="+fmt.Sprint(before), nil)
	response, e := http.DefaultClient.Do(r)
	if e != nil {
		t.Fatal(e)
	}
	time.Sleep(100 * time.Millisecond) // Hold the reader while the replay reaches socket backpressure.
	app.Manager.PublishGlobal(M{"type": "fixture", "index": 600, "padding": strings.Repeat("x", 2<<20)})
	app.Manager.PublishGlobal(M{"type": "fixture", "index": 601})
	reader := bufio.NewReaderSize(response.Body, 128<<10)
	seq := before
	count := 0
	for count < 602 {
		line, e := reader.ReadString('\n')
		if e != nil {
			t.Fatal(e)
		}
		if strings.HasPrefix(line, "id: ") {
			next, _ := strconv.ParseInt(strings.TrimSpace(line[4:]), 10, 64)
			if next != seq+1 {
				t.Fatal("replay lost an event", seq, next)
			}
			seq = next
			count++
		}
	}
	cancel()
	response.Body.Close()
	for _, value := range []string{"-1", "bogus", "9999999999999"} {
		status, _, _ := requestApp(t, app, "GET", "/api/events?since="+value, nil, nil)
		if status != 400 && status != 409 {
			t.Fatal(value, status)
		}
	}
	paused, e := http.Get(app.URL + "/api/events?since=0")
	if e != nil {
		t.Fatal(e)
	}
	defer paused.Body.Close()
	idle, e := net.Dial("tcp", strings.TrimPrefix(app.URL, "http://"))
	if e != nil {
		t.Fatal(e)
	}
	defer idle.Close()
	closeCtx, closeCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer closeCancel()
	if e = app.Close(closeCtx); e != nil {
		t.Fatal("shutdown waited on a paused SSE or preconnect", e)
	}
	if e = app.Close(closeCtx); e != nil {
		t.Fatal("shutdown not idempotent", e)
	}
}
func TestHTTPStaticAssetsAndMedia(t *testing.T) {
	f := newPlatformFixture(t, 5)
	f.enroll()
	app := f.startApp()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("<!doctype html><title>Tuck</title>"), 0600)
	c, rev, _ := app.Config.Read()
	O(c["server"])["frontendDir"] = dir
	app.Config.Save(rev, c)
	app.Config.MarkApplied()
	for _, path := range []string{"/assets/obsolete.js", "/assets/obsolete.css", "/missing.svg"} {
		status, _, _ := requestApp(t, app, "GET", path, nil, nil)
		if status != 404 {
			t.Fatal(path, status)
		}
	}
	task, e := app.Manager.Create(context.Background(), M{})
	if e != nil {
		t.Fatal(e)
	}
	id := S(task["id"])
	app.Manager.mu.Lock()
	message, e := app.Manager.appendLocked(fixtureTaskLocked(app.Manager, id), M{"role": "assistant", "blocks": []any{M{"id": "media", "type": "attachments", "data": M{"items": []any{M{"id": "image", "name": "pixel.png", "kind": "image", "mimeType": "image/png", "src": "data:image/png;base64," + "aGVsbG8="}}}}}})
	app.Manager.mu.Unlock()
	if e != nil {
		t.Fatal(e)
	}
	mediaURL := app.URL + "/api/tasks/" + id + "/message-media?index=0&block=media&item=image"
	response, e := http.Get(mediaURL)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 200 || string(raw) != "hello" || response.Header.Get("ETag") == "" {
		t.Fatal(response.StatusCode, string(raw))
	}
	r, _ := http.NewRequest("GET", mediaURL, nil)
	r.Header.Set("If-None-Match", response.Header.Get("ETag"))
	response, e = http.DefaultClient.Do(r)
	if e != nil {
		t.Fatal(e)
	}
	response.Body.Close()
	if response.StatusCode != 304 {
		t.Fatal("media not revalidated", response.StatusCode)
	}
	canonical, _ := app.Store.Message(id, I(message["index"]))
	if !strings.Contains(string(JSON(canonical)), "data:image/png") {
		t.Fatal("canonical media replaced")
	}

	beforeCursor, _ := app.Store.Cursor()
	for _, query := range []string{"index=-1&block=media&item=image", "index=0&block=missing&item=image", "index=0&block=media&item=missing"} {
		response, e = http.Get(app.URL + "/api/tasks/" + id + "/message-media?" + query)
		if e != nil {
			t.Fatal(e)
		}
		response.Body.Close()
		if response.StatusCode < 400 {
			t.Fatal("invalid coordinates", query, response.StatusCode)
		}
	}
	// A later canonical revision must be read lazily rather than a side cache.
	O(A(O(O(A(canonical["blocks"])[0])["data"])["items"])[0])["src"] = "data:image/png;base64,d29ybGQ="
	if e = app.Store.Write(func(tx *sql.Tx) error { return saveMessage(tx, id, canonical) }); e != nil {
		t.Fatal(e)
	}
	response, e = http.Get(mediaURL)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ = io.ReadAll(response.Body)
	response.Body.Close()
	if string(raw) != "world" {
		t.Fatal("media not read from latest canonical row", string(raw))
	}
	afterCursor, _ := app.Store.Cursor()
	if beforeCursor != afterCursor {
		t.Fatal("media read appended history")
	}
	status, history, _ := requestApp(t, app, "GET", "/api/tasks/"+id+"/messages?start=0&limit=64", nil, nil)
	if status != 200 || len(A(history["messages"])) != 1 {
		t.Fatal("normal history broken", status, history)
	}
	h, _ := PasswordHash(testPassword, 12)
	c, rev, _ = app.Config.Read()
	O(c["security"])["auth"] = M{"enabled": true, "username": "operator", "passwordHash": h}
	app.Config.Save(rev, c)
	app.Config.MarkApplied()
	response, e = http.Get(mediaURL)
	if e != nil {
		t.Fatal(e)
	}
	response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatal("media unauthenticated", response.StatusCode)
	}
}
func TestStoreSchemaAtomicityAndUnknownDatabase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tuck.db")
	s, e := NewStore(path)
	if e != nil {
		t.Fatal(e)
	}
	task := BaseTask(ID(), UUID(), "persistent", dir, "root", "agent")
	if e = s.SaveTask(task); e != nil {
		t.Fatal(e)
	}
	_, e = s.Publish(task, M{"index": 0, "revision": 0, "blocks": []any{}}, M{"type": "message"})
	if e != nil {
		t.Fatal(e)
	}
	cursor, _ := s.Cursor()
	if _, e = s.DB.Exec("CREATE TRIGGER reject_event BEFORE INSERT ON events BEGIN SELECT RAISE(FAIL,'fixture failure'); END"); e != nil {
		t.Fatal(e)
	}
	changed := Clone(task)
	changed["title"] = "must rollback"
	if _, e = s.Publish(changed, M{"index": 1, "revision": 0}, M{"type": "state"}); e == nil {
		t.Fatal("transaction succeeded")
	}
	stored, _ := s.Task(S(task["id"]))
	messages, _ := s.Messages(S(task["id"]), 0, 20)
	next, _ := s.Cursor()
	if stored["title"] != "persistent" || len(messages) != 1 || next != cursor {
		t.Fatal("partial publication committed")
	}
	s.DB.Exec("DROP TRIGGER reject_event")
	if e = s.Check(); e != nil {
		t.Fatal(e)
	}
	s.Close()
	s, e = NewStore(path)
	if e != nil {
		t.Fatal(e)
	}
	s.Close()
	old := filepath.Join(dir, "old.db")
	db, e := sql.Open("sqlite", old)
	if e != nil {
		t.Fatal(e)
	}
	db.Exec("CREATE TABLE tasks(id TEXT PRIMARY KEY,payload TEXT)")
	db.Close()
	before, _ := os.ReadFile(old)
	if unexpected, e := NewStore(old); e == nil {
		unexpected.Close()
		t.Fatal("old schema accepted")
	}
	after, _ := os.ReadFile(old)
	if !bytes.Equal(before, after) {
		t.Fatal("unknown database modified")
	}
}
func TestConfigurationCASAndSingleFile(t *testing.T) {
	c, _, _, _ := securityFixture(t, false, nil)
	v, revision, _ := c.Read()
	one := Clone(v)
	O(one["workbench"])["requireDelivery"] = true
	own, e := c.Save(revision, one)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = c.Save(revision, v); Status(e) != 409 {
		t.Fatal("stale config saved", e)
	}
	external := Clone(one)
	O(external["workbench"])["deliveryRetentionCount"] = 7
	if e = AtomicWrite(c.Path, JSON(external), 0600); e != nil {
		t.Fatal(e)
	}
	if e = c.Restore(own, v); Status(e) != 409 {
		t.Fatal("rollback overwrote external edit", e)
	}
	current, _, _ := c.Read()
	if I(O(current["workbench"])["deliveryRetentionCount"]) != 7 {
		t.Fatal(current)
	}
	before, _ := os.ReadFile(c.Path)
	if e = InitConfig(c.Path); e != nil {
		t.Fatal(e)
	}
	after, _ := os.ReadFile(c.Path)
	if !bytes.Equal(before, after) {
		t.Fatal("init replaced existing config")
	}
	entries, _ := os.ReadDir(filepath.Dir(c.Path))
	if len(entries) != 1 {
		t.Fatal("extra user configuration files", entries)
	}
}
func TestStoreLargeIndexedDataset(t *testing.T) {
	if os.Getenv("TUCK_PERF") != "1" {
		t.Skip("set TUCK_PERF=1 and run go test ./internal/tuck -run '^TestStoreLargeIndexedDataset$' for the 10k/100k/1m acceptance dataset")
	}
	s, e := NewStore(filepath.Join(t.TempDir(), "performance.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	ids := make([]string, 10000)
	start := time.Now()
	e = s.Write(func(tx *sql.Tx) error {
		tasks, e := tx.Prepare("INSERT INTO tasks VALUES(?,?,?,0,?,?)")
		if e != nil {
			return e
		}
		defer tasks.Close()
		for i := range ids {
			ids[i] = ID()
			task := BaseTask(ids[i], UUID(), "task", "", "root", "agent")
			task["historyTiming"] = M{"previousMs": 99000, "previousTurns": 100, "incomplete": false}
			if _, e = tasks.Exec(ids[i], NowMS(), "idle", task["nativeSessionId"], string(JSON(task))); e != nil {
				return e
			}
		}
		messages, e := tx.Prepare("INSERT INTO messages VALUES(?,?,0,?)")
		if e != nil {
			return e
		}
		defer messages.Close()
		for i := 0; i < 100000; i++ {
			if _, e = messages.Exec(ids[i%10], i/10, fmt.Sprintf(`{"index":%d,"revision":0,"role":"assistant","blocks":[]}`, i/10)); e != nil {
				return e
			}
		}
		events, e := tx.Prepare("INSERT INTO events(task_id,type,created_at_ms,payload_json) VALUES(?,?,?,?)")
		if e != nil {
			return e
		}
		defer events.Close()
		for i := 0; i < 1000000; i++ {
			id, kind, payload := ids[i%10000], "native", `{"type":"native","event":{"type":"heartbeat"}}`
			if i < 100000 {
				id = ids[i%10]
				kind = "state"
				payload = string(JSON(M{"type": "state", "task": M{"turnTiming": M{"turnId": fmt.Sprint("turn_", i/1000), "startedAt": (i / 1000) * 10000, "observedAt": (i/1000)*10000 + i%1000, "incomplete": false}, "padding": strings.Repeat("x", 128)}}))
			}
			if _, e = events.Exec(id, kind, NowMS(), payload); e != nil {
				return e
			}
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	t.Log("dataset seed", time.Since(start))
	durations := []time.Duration{}
	for i := 0; i < 100; i++ {
		start = time.Now()
		task, e := s.Task(ids[i%10])
		if e != nil {
			t.Fatal(e)
		}
		if _, e = s.Messages(ids[i%10], (i*97)%9900, 64); e != nil {
			t.Fatal(e)
		}
		stats, e := s.Statistics(task)
		if e != nil {
			t.Fatal(e)
		}
		if I(stats["previousTurns"]) != 100 || N(stats["previousMs"]) <= 0 {
			t.Fatal("statistics dataset did not exercise historical state", stats)
		}
		if _, e = s.Events(int64(i*9970), 512); e != nil {
			t.Fatal(e)
		}
		durations = append(durations, time.Since(start))
	}
	slices.Sort(durations)
	p95 := durations[94]
	t.Log("indexed task + history + statistics + replay combined p95", p95)
	if p95 > 100*time.Millisecond {
		t.Fatal("p95 exceeded 100ms", p95)
	}
	if e = s.Check(); e != nil {
		t.Fatal(e)
	}
}
