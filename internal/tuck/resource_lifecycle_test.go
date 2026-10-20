package tuck

import (
	"bytes"
	"context"
	_ "embed"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

//go:embed testdata/config-v3.json
var releasedConfigV3 []byte

func retentionFixture(t *testing.T) (*Manager, *Store, *fakeNative, string, M) {
	t.Helper()
	m, s, engine, id := taskFixture(t)
	config := m.Config()
	config["retention"] = defaultRetention()
	m.options.Config = func() M { return Clone(config) }
	return m, s, engine, id, config
}

func TestLifecycleCloseWaitsForInFlightOperation(t *testing.T) {
	_, store, _, _, _ := retentionFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	a := &App{Store: store, ctx: ctx, cancel: cancel}
	op, finish, err := a.lifecycleOperation(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- a.Close(context.Background()) }()
	select {
	case <-op.Done():
	case <-time.After(time.Second):
		t.Fatal("shutdown did not cancel operation")
	}
	if err := store.DB.Ping(); err != nil {
		t.Fatal("store closed before in-flight operation ended", err)
	}
	if _, _, err := a.lifecycleOperation(context.Background()); Code(err) != "service_closing" {
		t.Fatal(err)
	}
	finish()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown did not drain")
	}
	if err := store.DB.Ping(); err == nil {
		t.Fatal("store remains open")
	}
}

func TestLifecycleTurnReleaseIsIdempotent(t *testing.T) {
	f := newWorkspaceFixture(t)
	local, original := f.turn(t)
	task := mustTask(t, local.Manager, original.TaskID)
	_, finish, err := local.Begin(t.Context(), task)
	if err != nil {
		t.Fatal(err)
	}
	finish()
	finish()
	local.Manager.mu.Lock()
	uses := local.Manager.taskUses[original.TaskID]
	local.Manager.mu.Unlock()
	if uses != 1 {
		t.Fatal("finishing one turn released another turn's use", uses)
	}
}

func TestLifecycleCloseDrainsHTTPBeforeStore(t *testing.T) {
	_, store, _, _, _ := retentionFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	a := &App{Store: store, ctx: ctx, cancel: cancel}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	started, returned := make(chan struct{}), make(chan struct{})
	a.listener = listener
	a.Server = &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, finish, e := a.lifecycleOperation(r.Context())
		if e != nil {
			return
		}
		defer finish()
		defer close(returned)
		close(started)
		// Deliberately wait on the transport's context, not the application
		// operation context. Socket writes likewise require transport closure.
		<-r.Context().Done()
	})}
	go func() { _ = a.Server.Serve(listener) }()
	defer a.Server.Close()
	client, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if _, err = io.WriteString(client, "GET / HTTP/1.1\r\nHost: localhost\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("request did not start")
	}
	done := make(chan error, 1)
	go func() { done <- a.Close(context.Background()) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown waited for HTTP handler before closing transport")
	}
	select {
	case <-returned:
	default:
		t.Fatal("storage closed before handler returned")
	}
}

func TestIdlePlanRehydratesForIncrementalUpdate(t *testing.T) {
	m, _, _, id, _ := retentionFixture(t)
	m.mu.Lock()
	task, err := m.getLocked(id)
	if err == nil {
		err = m.normalizer.plan(task, "create_todo", M{}, M{"id": "todo-1", "description": "keep plan", "status": "pending"})
	}
	m.unlock()
	if err != nil {
		t.Fatal(err)
	}
	if len(m.tasks) != 0 || len(m.normalizer.todos) != 0 {
		t.Fatal("idle plan still resident")
	}
	m.mu.Lock()
	task, err = m.getLocked(id)
	if err == nil {
		err = m.normalizer.plan(task, "update_todos", M{"updates": []any{M{"id": "todo-1", "status": "completed"}}}, nil)
	}
	m.unlock()
	if err != nil {
		t.Fatal(err)
	}
	items := A(O(mustTask(t, m, id)["activePlan"])["items"])
	if len(items) != 1 || O(items[0])["text"] != "keep plan" || O(items[0])["status"] != "completed" {
		t.Fatal(items)
	}
}

func TestManagedWorkspaceSymlinkBoundary(t *testing.T) {
	m, _, engine, id, config := retentionFixture(t)
	O(O(config["retention"])["managedWorkspaces"])["autoCleanup"] = true
	path := attachManagedFixture(t, m, id)
	external := t.TempDir()
	marker := filepath.Join(external, "outside")
	if err := os.WriteFile(marker, []byte("outside must remain"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(path, "link")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	root := mustTask(t, m, id)
	calls := []string{}
	deletionNative(engine, map[string]bool{S(root["nativeSessionId"]): true}, &calls)
	if _, err := m.Delete(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	if pending, err := (&Files{Manager: m}).cleanupWorkspaces(t.Context()); err != nil || pending {
		t.Fatal(pending, err)
	}
	if raw, err := os.ReadFile(marker); err != nil || string(raw) != "outside must remain" {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestLifecyclePersistentFactsRejectInvalidData(t *testing.T) {
	m, s, _, id, _ := retentionFixture(t)
	original := mustTask(t, m, id)
	for _, value := range []any{"invalid", M{"keep": "true"}, M{"purpose": "automatic"}, M{"archivedAt": -1}, M{"lastActivityAt": 1.5}} {
		next := Clone(original)
		next["lifecycle"] = value
		if err := s.SaveTask(next); Code(err) != "persistent_lifecycle" {
			t.Fatal(value, err)
		}
	}
	if _, err := s.DB.Exec(`UPDATE tasks SET payload_json=json_set(payload_json,'$.lifecycle.keep','invalid') WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	if err := checkLifecycleData(s.DB); Code(err) != "persistent_lifecycle" {
		t.Fatal(err)
	}
}

func TestManagedWorkspaceOptInAppliesOnlyToNewDirectories(t *testing.T) {
	m, s, engine, _, config := retentionFixture(t)
	before, err := m.Create(t.Context(), M{})
	if err != nil {
		t.Fatal(err)
	}
	O(O(config["retention"])["managedWorkspaces"])["autoCleanup"] = true
	after, err := m.Create(t.Context(), M{})
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := s.managedWorkspaces()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, w := range workspaces {
		if w.ID == S(before["managedWorkspaceId"]) || w.ID == S(after["managedWorkspaceId"]) {
			if w.State != "attached" || w.CleanupAllowed != (w.ID == S(after["managedWorkspaceId"])) {
				t.Fatal(w)
			}
			seen[w.ID] = true
		}
	}
	if len(seen) != 2 {
		t.Fatal(workspaces)
	}
	calls := []string{}
	deletionNative(engine, map[string]bool{S(before["nativeSessionId"]): true}, &calls)
	if _, err = m.Delete(t.Context(), S(before["id"])); err != nil {
		t.Fatal(err)
	}
	var retained int
	if err = s.DB.QueryRow("SELECT COUNT(*) FROM managed_workspaces WHERE id=?", S(before["managedWorkspaceId"])).Scan(&retained); err != nil || retained != 0 {
		t.Fatal(retained, err)
	}
	if _, err = os.Stat(S(before["workspace"])); err != nil {
		t.Fatal("unmanaged directory changed", err)
	}
	// Applying unrelated runtime settings must not silently activate a policy
	// that was edited on disk after the current configuration was applied.
	c := &Config{applied: defaultConfig()}
	next := c.Applied()
	O(O(next["retention"])["conversations"])["autoDelete"] = true
	if err = c.RequireRuntimeCompatible(next); Code(err) != "retention_apply_required" {
		t.Fatal(err)
	}
}

func TestIdleTaskHeapRetention(t *testing.T) {
	if os.Getenv("TUCK_PERF") != "1" {
		t.Skip("set TUCK_PERF=1 for heap comparison")
	}
	m, s, _, id, _ := retentionFixture(t)
	base := mustTask(t, m, id)
	const count = 64
	for i := 0; i < count; i++ {
		task := Clone(base)
		task["id"] = ID()
		task["nativeSessionId"] = UUID()
		task["artifacts"] = []any{M{"path": strings.Repeat("payload", 75000)}}
		if err := s.SaveTask(task); err != nil {
			t.Fatal(err)
		}
		if _, err := m.Get(S(task["id"])); err != nil {
			t.Fatal(err)
		}
	}
	runtime.GC()
	var idle, full runtime.MemStats
	runtime.ReadMemStats(&idle)
	// The old design kept this full dataset reachable for the Manager lifetime.
	snapshots, err := m.Snapshots()
	if err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	runtime.ReadMemStats(&full)
	runtime.KeepAlive(snapshots)
	if len(m.tasks) != 0 {
		t.Fatal("idle dataset cached", len(m.tasks))
	}
	growth := int64(full.HeapAlloc) - int64(idle.HeapAlloc)
	t.Logf("%d idle tasks: on-demand heap=%d, retained full snapshots=%d, avoided=%d bytes", count, idle.HeapAlloc, full.HeapAlloc, growth)
	if growth < 24<<20 {
		t.Fatal("heap comparison did not retain expected dataset", growth)
	}
}

func TestRetentionDeadlineKeepRestoreAndChildActivity(t *testing.T) {
	m, _, _, id, config := retentionFixture(t)
	now := int64(200) * retentionDay
	mustUpdate(t, m, id, M{"lifecycle": M{"lastActivityAt": now - 30*retentionDay, "purpose": "conversation"}})
	state, err := m.RetentionState(id, O(config["retention"]), now-1)
	if err != nil || state["action"] != "" {
		t.Fatal(state, err)
	}
	state, _ = m.RetentionState(id, O(config["retention"]), now)
	if state["action"] != "archive" {
		t.Fatal(state)
	}
	if _, err = m.Keep(id, true); err != nil {
		t.Fatal(err)
	}
	if _, err = m.setArchived(id, true, true, now); Code(err) != "retention_changed" {
		t.Fatal(err)
	}
	if _, err = m.Keep(id, false); err != nil {
		t.Fatal(err)
	}
	child := addDeleteTask(t, m, id, "retention-child")
	mustUpdate(t, m, child, M{"lifecycle": M{"lastActivityAt": now}})
	state, _ = m.RetentionState(id, O(config["retention"]), now)
	if state["action"] != "" {
		t.Fatal("recent child was ignored", state)
	}
	mustUpdate(t, m, child, M{"lifecycle": M{"lastActivityAt": now - 30*retentionDay}})
	if _, err = m.setArchived(id, true, true, now); err != nil {
		t.Fatal(err)
	}
	O(O(config["retention"])["conversations"])["autoDelete"] = true
	state, _ = m.RetentionState(id, O(config["retention"]), now+90*retentionDay)
	if state["action"] != "delete" {
		t.Fatal(state)
	}
	if _, err = m.setArchived(id, false, false, now+90*retentionDay); err != nil {
		t.Fatal(err)
	}
	state, _ = m.RetentionState(id, O(config["retention"]), now+90*retentionDay)
	if state["action"] != "" || N(O(mustTask(t, m, id)["lifecycle"])["archivedAt"]) != 0 {
		t.Fatal(state)
	}
}

func TestRetentionUnknownHistoryAndReadsDoNotMoveAnchors(t *testing.T) {
	m, s, _, id, config := retentionFixture(t)
	mustUpdate(t, m, id, M{"lifecycle": nil, "archived": true})
	now := int64(200) * retentionDay
	for i := 0; i < 3; i++ {
		_, _ = m.RetentionState(id, O(config["retention"]), now+int64(i))
	}
	if len(O(mustTask(t, m, id)["lifecycle"])) != 0 {
		t.Fatal("read mutated history")
	}
	if err := m.observeRetention(id, now); err != nil {
		t.Fatal(err)
	}
	if N(O(mustTask(t, m, id)["lifecycle"])["archiveObservedAt"]) != 0 {
		t.Fatal("disabled policy started clock")
	}
	O(O(config["retention"])["conversations"])["autoDelete"] = true
	if err := m.observeRetention(id, now); err != nil {
		t.Fatal(err)
	}
	restored, err := NewManager(m.options)
	if err != nil {
		t.Fatal(err)
	}
	if err = restored.observeRetention(id, now+retentionDay); err != nil {
		t.Fatal(err)
	}
	task, err := s.Task(id)
	if err != nil || int64(N(O(task["lifecycle"])["archiveObservedAt"])) != now {
		t.Fatal(task, err)
	}
}

func TestRetentionStrictConfiguration(t *testing.T) {
	if err := validateRetention(defaultRetention()); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(M){
		func(p M) { p["unknown"] = true },
		func(p M) { delete(p, "managedWorkspaces") },
		func(p M) { O(p["conversations"])["inactiveDays"] = 0 },
		func(p M) { O(p["temporaryRuns"])["archivedDays"] = 1.5 },
		func(p M) { O(p["conversations"])["autoDelete"] = "false" },
		func(p M) { O(p["conversations"])["archivedDays"] = 36501 },
	} {
		p := defaultRetention()
		change(p)
		if err := validateRetention(p); err == nil {
			t.Fatal(p)
		}
	}
	p := defaultRetention()
	next := Clone(p)
	O(next["conversations"])["autoDelete"] = true
	if !retentionRequiresConfirmation(p, next) {
		t.Fatal("missing delete confirmation")
	}
	p = Clone(next)
	O(next["conversations"])["archivedDays"] = 30
	if !retentionRequiresConfirmation(p, next) {
		t.Fatal("missing shorter retention confirmation")
	}
	O(next["conversations"])["autoDelete"] = false
	if retentionRequiresConfirmation(p, next) {
		t.Fatal("disabling asks for destructive approval")
	}
}

func TestMigrationV3PreservesReplayAndUnknownLifecycle(t *testing.T) {
	db, err := openDatabase(filepath.Join(t.TempDir(), "v3.db"), "rwc")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, ddl := range []string{schemaV1, schemaV2, schemaV3} {
		if _, err = db.Exec(ddl); err != nil {
			t.Fatal(err)
		}
	}
	s := &Store{DB: db}
	task := BaseTask(ID(), "original", "history", t.TempDir(), "root", "agent")
	delete(task, "lifecycle")
	task["archived"] = true
	if _, err = s.Publish(task, nil, M{"type": "state", "task": task}); err != nil {
		t.Fatal(err)
	}
	before, err := s.Events(0, 10)
	if err != nil {
		t.Fatal(err)
	}
	v, err := DecodeJSON(releasedConfigV3, 4<<20)
	if err != nil {
		t.Fatal(err)
	}
	O(O(v)["security"])["credentialKey"] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	config, err := migrateConfiguration(O(v))
	if err != nil {
		t.Fatal(err)
	}
	if I(config["schemaVersion"]) != 4 || !reflect.DeepEqual(Clone(O(config["retention"])), Clone(defaultRetention())) {
		t.Fatal(config["retention"])
	}
	if err = migrateDatabase(s, config, 3); err != nil {
		t.Fatal(err)
	}
	if err = migrateDatabase(s, config, 4); err != nil {
		t.Fatal(err)
	}
	after, err := s.Events(0, 10)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("v3 replay changed", err)
	}
	loaded, err := s.Task(S(task["id"]))
	if err != nil || !reflect.DeepEqual(Clone(task), loaded) {
		t.Fatal("legacy facts changed", err)
	}
	var count int
	if err = db.QueryRow("SELECT COUNT(*) FROM managed_workspaces").Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
}

func attachManagedFixture(t *testing.T, m *Manager, id string) string {
	t.Helper()
	path, err := m.allocateWorkspace(ID(), true)
	if err != nil {
		t.Fatal(err)
	}
	wid := filepath.Base(path)
	mustUpdate(t, m, id, M{"managedWorkspaceId": wid, "workspace": path, "workspaceMode": "auto"})
	if err = os.WriteFile(filepath.Join(path, "keep.txt"), []byte("important bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestManagedWorkspaceSharedForkAndInFlightUse(t *testing.T) {
	m, s, engine, id, config := retentionFixture(t)
	O(O(config["retention"])["managedWorkspaces"])["autoCleanup"] = true
	path := attachManagedFixture(t, m, id)
	root := mustTask(t, m, id)
	fork := addDeleteTask(t, m, "", "independent")
	mustUpdate(t, m, fork, M{"workspace": path, "workspaceMode": "auto", "managedWorkspaceId": root["managedWorkspaceId"], "forkedFrom": M{"id": id}})
	calls := []string{}
	deletionNative(engine, map[string]bool{S(root["nativeSessionId"]): true, "independent": true}, &calls)
	if _, err := m.Delete(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	files := &Files{Manager: m}
	if _, err := files.cleanupWorkspaces(t.Context()); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(filepath.Join(path, "keep.txt")); err != nil || string(raw) != "important bytes" {
		t.Fatal(err)
	}
	release, err := m.useWorkspace(mustTask(t, m, fork))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Delete(t.Context(), fork); Code(err) != "task_active" {
		t.Fatal(err)
	}
	release()
	if _, err = m.Delete(t.Context(), fork); err != nil {
		t.Fatal(err)
	}
	if pending, err := files.cleanupWorkspaces(t.Context()); err != nil || pending {
		t.Fatal(pending, err)
	}
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("directory survived", err)
	}
	var count int
	if err = s.DB.QueryRow("SELECT COUNT(*) FROM managed_workspaces WHERE id=?", root["managedWorkspaceId"]).Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
}

func TestManagedWorkspaceSpecifiedOverlapAndDisabledRecovery(t *testing.T) {
	m, _, engine, id, config := retentionFixture(t)
	path := attachManagedFixture(t, m, id)
	root := mustTask(t, m, id)
	specified := addDeleteTask(t, m, "", "specified")
	mustUpdate(t, m, specified, M{"workspace": filepath.Dir(path), "workspaceMode": "specified"})
	calls := []string{}
	deletionNative(engine, map[string]bool{S(root["nativeSessionId"]): true, "specified": true}, &calls)
	if _, err := m.Delete(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	files := &Files{Manager: m}
	if _, err := files.cleanupWorkspaces(t.Context()); err != nil {
		t.Fatal(err)
	}
	O(O(config["retention"])["managedWorkspaces"])["autoCleanup"] = true
	if pending, err := files.cleanupWorkspaces(t.Context()); err != nil || !pending {
		t.Fatal("overlapping specified reference not protected", pending, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Delete(t.Context(), specified); err != nil {
		t.Fatal(err)
	}
	restored, err := NewManager(m.options)
	if err != nil {
		t.Fatal(err)
	}
	if pending, err := (&Files{Manager: restored}).cleanupWorkspaces(t.Context()); err != nil || pending {
		t.Fatal(pending, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestManagedWorkspaceReplacementPreservesForeignFiles(t *testing.T) {
	m, _, engine, id, config := retentionFixture(t)
	O(O(config["retention"])["managedWorkspaces"])["autoCleanup"] = true
	path := attachManagedFixture(t, m, id)
	root := mustTask(t, m, id)
	calls := []string{}
	deletionNative(engine, map[string]bool{S(root["nativeSessionId"]): true}, &calls)
	if _, err := m.Delete(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	// Keep the original inode alive to avoid platform-specific inode reuse.
	if err := os.Rename(path, path+"-original"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "foreign.txt"), []byte("must survive"), 0600); err != nil {
		t.Fatal(err)
	}
	if pending, err := (&Files{Manager: m}).cleanupWorkspaces(t.Context()); err != nil || !pending {
		t.Fatal(pending, err)
	}
	if raw, err := os.ReadFile(filepath.Join(path, "foreign.txt")); err != nil || string(raw) != "must survive" {
		t.Fatal(err)
	}
	workspaces, err := m.options.Store.managedWorkspaces()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range workspaces {
		if w.ID == S(root["managedWorkspaceId"]) {
			found = strings.Contains(w.LastError, "身份")
		}
	}
	if !found {
		t.Fatal("cleanup failure not retained")
	}
}

func TestIdleTaskPayloadsAreReadOnDemand(t *testing.T) {
	m, s, _, id, _ := retentionFixture(t)
	payload := strings.Repeat("large idle payload", 65536)
	mustUpdate(t, m, id, M{"artifacts": []any{M{"path": payload}}})
	for i := 0; i < 5; i++ {
		task := mustTask(t, m, id)
		if S(O(A(task["artifacts"])[0])["path"]) != payload {
			t.Fatal("payload lost")
		}
		m.mu.Lock()
		resident := len(m.tasks)
		m.mu.Unlock()
		if resident != 0 {
			t.Fatalf("idle payload remains resident: %d", resident)
		}
	}
	// Storage read errors must surface, never look like an empty task list.
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Snapshots(); err == nil {
		t.Fatal("storage error hidden")
	}
}

func TestIdleTaskOperationsReleaseWithoutAnotherRead(t *testing.T) {
	for _, operation := range []string{"policy", "policy-error", "clear-queue", "abort", "fork-options", "resolve-race"} {
		t.Run(operation, func(t *testing.T) {
			m, store, engine, id, _ := retentionFixture(t)
			var err error
			switch operation {
			case "policy", "policy-error":
				if operation == "policy-error" {
					engine.request = func(context.Context, string, string, any) (M, error) {
						return nil, Problem(502, "native_failed", "failed")
					}
				}
				_, err = m.SetApprovalMode(t.Context(), id, "auto")
				if operation == "policy-error" && Code(err) == "native_failed" {
					err = nil
				}
			case "clear-queue":
				mustUpdate(t, m, id, M{"queue": []any{M{"id": "queued", "prompt": "pending"}}, "queuedPrompts": 1, "queuePaused": true})
				_, err = m.UpdateQueue(t.Context(), id, M{"action": "clear"})
			case "abort":
				err = m.Abort(t.Context(), id)
			case "fork-options":
				_, err = m.ForkOptions(t.Context(), id)
			case "resolve-race":
				pending := M{"id": "approval", "kind": "approval"}
				mustUpdate(t, m, id, M{"status": "waiting", "pendingInteraction": pending})
				engine.request = func(context.Context, string, string, any) (M, error) {
					// The native stream can finish before the resume response arrives.
					_, e := m.Update(id, func(task M) error {
						task["status"], task["pendingInteraction"] = "idle", nil
						return nil
					})
					return M{}, e
				}
				err = m.ResolveInteraction(t.Context(), id, M{"interactionId": "approval", "kind": "approval", "approved": true})
			}
			if err != nil {
				t.Fatal(err)
			}
			m.mu.Lock()
			resident := len(m.tasks)
			m.mu.Unlock()
			if resident != 0 {
				t.Fatalf("completed %s retained %d idle payloads", operation, resident)
			}
			if task, err := store.Task(id); err != nil || task == nil {
				t.Fatal("persistent task lost", err)
			}
		})
	}
}

func TestLifecycleMaintenanceUsesCurrentPolicy(t *testing.T) {
	m, s, engine, id, config := retentionFixture(t)
	now := NowMS()
	mustUpdate(t, m, id, M{"lifecycle": M{"lastActivityAt": now - 31*retentionDay, "purpose": "conversation"}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := &App{Manager: m, Store: s, Models: &Models{config: config}, Files: &Files{Manager: m}, ctx: ctx}
	a.Delivery = &Deliveries{Store: s, Root: t.TempDir()}
	if err := a.maintainLifecycle(ctx, now); err != nil {
		t.Fatal(err)
	}
	task := mustTask(t, m, id)
	if !B(task["archived"]) {
		t.Fatal(task)
	}
	calls := []string{}
	deletionNative(engine, map[string]bool{S(task["nativeSessionId"]): true}, &calls)
	if err := a.maintainLifecycle(ctx, now+91*retentionDay); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 0 {
		t.Fatal("default policy deleted history")
	}
	O(O(config["retention"])["conversations"])["autoDelete"] = true
	if err := a.maintainLifecycle(ctx, now+91*retentionDay); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Get(id); Code(err) != "task_not_found" {
		t.Fatal(err)
	}
	if len(calls) != 1 {
		t.Fatal(calls)
	}
}

func TestReleasedV3TemplateHasNoRetention(t *testing.T) {
	if bytes.Contains(releasedConfigV3, []byte(`"retention"`)) {
		t.Fatal("v3 fixture was not frozen")
	}
}

func TestMigrationV3OfficialHistoryResumeAndDelete(t *testing.T) {
	f, a := parityApp(t)
	nativeScript(f, func(w http.ResponseWriter, _ *http.Request, b M) {
		nativeDelta(w, M{"content": "history retained"})
	})
	task := newParityTask(t, a, M{"approvalMode": "auto"})
	id := S(task["id"])
	submitTask(t, a.Manager, id, "original v3 history")
	waitTask(t, a.Manager, id, func(task M) bool { return task["lastTurnReason"] == "completed" && !a.Manager.Running(id) })
	before, err := a.Store.Messages(id, 0, 64)
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(S(task["workspace"]), "legacy.txt")
	if err = os.WriteFile(marker, []byte("legacy directory"), 0600); err != nil {
		t.Fatal(err)
	}
	current := a.Config.Applied()
	path, database := a.Config.Path, a.Store.Path
	if err = a.Close(t.Context()); err != nil {
		t.Fatal(err)
	}

	// Build the released Tuck v3 DDL, with real public-engine conversation
	// identities and history. Native assets remain opaque and untouched.
	legacyPath := database + ".v3-fixture"
	db, err := openDatabase(legacyPath, "rwc")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, ddl := range []string{schemaV1, schemaV2, schemaV3} {
		if _, err = db.Exec(ddl); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.Exec("ATTACH DATABASE ? AS source", database); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO tasks SELECT id,created_at_ms,status,archived,native_session_id,json_remove(payload_json,'$.lifecycle','$.managedWorkspaceId') FROM source.tasks`); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"messages", "task_deliveries", "web_sessions", "login_limits", "platform_session", "mcp_catalog"} {
		if _, err = db.Exec("INSERT INTO " + table + " SELECT * FROM source." + table); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.Exec(`INSERT INTO events(seq,task_id,type,created_at_ms,payload_json) SELECT seq,task_id,type,created_at_ms,payload_json FROM source.events`); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(database, database+".current-fixture"); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(legacyPath, database); err != nil {
		t.Fatal(err)
	}
	value, err := DecodeJSON(releasedConfigV3, 4<<20)
	if err != nil {
		t.Fatal(err)
	}
	legacy := O(value)
	for _, key := range []string{"server", "storage", "engine", "security", "providers", "defaultModelId", "modelDefaults", "platformModelSettings", "platformMcpTools"} {
		legacy[key] = current[key]
	}
	if err = AtomicWrite(path, JSON(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	if err = MigrateCommand(path, []string{"--backup", filepath.Join(t.TempDir(), "backup")}, io.Discard); err != nil {
		t.Fatal(err)
	}
	c, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := NewApp(c)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close(context.Background())
	if err = restarted.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	after, err := restarted.Store.Messages(id, 0, 64)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("migration altered history", err)
	}
	restored := mustTask(t, restarted.Manager, id)
	if restored["nativeSessionId"] != task["nativeSessionId"] || restored["managedWorkspaceId"] != nil {
		t.Fatal(restored)
	}
	if _, err = restarted.Manager.Archive(id, true); err != nil {
		t.Fatal(err)
	}
	if _, err = restarted.Manager.Archive(id, false); err != nil {
		t.Fatal(err)
	}
	var seenHistory atomic.Bool
	nativeScript(f, func(w http.ResponseWriter, _ *http.Request, b M) {
		seenHistory.Store(bytes.Contains(JSON(b["messages"]), []byte("original v3 history")))
		nativeDelta(w, M{"content": "continued history"})
	})
	submitTask(t, restarted.Manager, id, "continue after v4 migration")
	waitTask(t, restarted.Manager, id, func(task M) bool {
		return I(task["turnCount"]) == 2 && task["lastTurnReason"] == "completed" && !restarted.Manager.Running(id)
	})
	if !seenHistory.Load() {
		t.Fatal("native model input lost old history")
	}
	if _, err = restarted.deleteConversation(t.Context(), id, false, NowMS()); err != nil {
		t.Fatal(err)
	}
	if _, err = restarted.Engine.Request(t.Context(), "GET", nativePath(task, ""), nil); !nativeSessionMissing(err) {
		t.Fatal("native session not deleted", err)
	}
	if raw, err := os.ReadFile(marker); err != nil || string(raw) != "legacy directory" {
		t.Fatal("legacy files changed", err)
	}
}

func TestRetentionSettingsHotApplyPreservesPendingConfiguration(t *testing.T) {
	f := newPlatformFixture(t, 5)
	a := f.startApp()
	headers := http.Header{"Origin": {a.URL}}
	a.Engine.mu.Lock()
	process := a.Engine.process
	a.Engine.mu.Unlock()
	original := a.Models.Config()
	task, err := a.Manager.Create(t.Context(), M{})
	if err != nil {
		t.Fatal(err)
	}
	id := S(task["id"])
	mustUpdate(t, a.Manager, id, M{"status": "waiting", "pendingInteraction": M{"id": "keep-approval", "kind": "question"}})
	// An unrelated on-disk edit remains pending after retention-only apply.
	current, revision, err := a.Config.Read()
	if err != nil {
		t.Fatal(err)
	}
	O(current["server"])["frontendDir"] = "pending-frontend"
	if _, err = a.Config.Save(revision, current); err != nil {
		t.Fatal(err)
	}
	status, view, _ := requestApp(t, a, "GET", "/api/settings/retention", nil, headers)
	if status != 200 {
		t.Fatal(status, view)
	}
	next := Clone(O(view["retention"]))
	O(next["conversations"])["autoDelete"] = true
	body := M{"retention": next, "revision": view["revision"]}
	status, _, _ = requestApp(t, a, "PUT", "/api/settings/retention", body, headers)
	if status != 409 {
		t.Fatal("unconfirmed policy accepted", status)
	}
	status, preview, _ := requestApp(t, a, "POST", "/api/settings/retention", body, headers)
	if status != 200 || !B(preview["requiresConfirmation"]) {
		t.Fatal(status, preview)
	}
	body["confirmation"] = preview["confirmation"]
	status, _, _ = requestApp(t, a, "PUT", "/api/settings/retention", body, headers)
	if status != 200 {
		t.Fatal(status)
	}
	if !a.Config.Pending() || O(a.Config.Applied()["server"])["frontendDir"] != O(original["server"])["frontendDir"] {
		t.Fatal("unrelated setting applied")
	}
	if !B(O(O(a.Models.Config()["retention"])["conversations"])["autoDelete"]) {
		t.Fatal("policy not applied")
	}
	if mustTask(t, a.Manager, id)["status"] != "waiting" {
		t.Fatal("policy change interrupted task")
	}
	a.Engine.mu.Lock()
	sameProcess := process == a.Engine.process
	a.Engine.mu.Unlock()
	if !sameProcess {
		t.Fatal("policy update restarted engine")
	}
	status, _, _ = requestApp(t, a, "PUT", "/api/settings/retention", body, headers)
	if status != 409 {
		t.Fatal("stale revision accepted", status)
	}
}

func TestManagedWorkspacePendingFailureAndNewReference(t *testing.T) {
	m, _, engine, id, config := retentionFixture(t)
	O(O(config["retention"])["managedWorkspaces"])["autoCleanup"] = true
	path := attachManagedFixture(t, m, id)
	root := mustTask(t, m, id)
	calls := []string{}
	deletionNative(engine, map[string]bool{S(root["nativeSessionId"]): true}, &calls)
	if _, err := m.Delete(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	files := &Files{Manager: m}
	// Cancellation after the DB commit preserves cleanup responsibility.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := files.cleanupWorkspaces(ctx); err != context.Canceled {
		t.Fatal(err)
	}
	other := addDeleteTask(t, m, "", "later")
	mustUpdate(t, m, other, M{"workspace": path, "workspaceMode": "auto", "managedWorkspaceId": root["managedWorkspaceId"]})
	workspaces, err := m.options.Store.managedWorkspaces()
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range workspaces {
		if w.ID == S(root["managedWorkspaceId"]) && w.State != "attached" {
			t.Fatal(w)
		}
	}
	if _, err = files.cleanupWorkspaces(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(path, "keep.txt")); err != nil {
		t.Fatal(err)
	}
}

func TestManagedWorkspaceCreationCollisionNeverAdoptsDirectory(t *testing.T) {
	m, _, _, _, config := retentionFixture(t)
	O(O(config["retention"])["managedWorkspaces"])["autoCleanup"] = true
	id := ID()
	path := filepath.Join(m.options.WorkspaceRoot, id)
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "foreign"), []byte("retain"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.allocateWorkspace(id, true); err == nil {
		t.Fatal("collision adopted")
	}
	if _, err := (&Files{Manager: m}).cleanupWorkspaces(t.Context()); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(path, "foreign")); err != nil || string(data) != "retain" {
		t.Fatal(err)
	}
}

func TestRetentionAutomaticDeleteRechecksKeepAndBusyDescendants(t *testing.T) {
	m, _, _, id, config := retentionFixture(t)
	now := NowMS()
	O(O(config["retention"])["conversations"])["autoDelete"] = true
	mustUpdate(t, m, id, M{"archived": true, "lifecycle": M{"archivedAt": now - 91*retentionDay}})
	state, _ := m.RetentionState(id, O(config["retention"]), now)
	if state["action"] != "delete" {
		t.Fatal(state)
	}
	if _, err := m.Keep(id, true); err != nil {
		t.Fatal(err)
	}
	if _, err := m.deleteConversation(t.Context(), id, true, now); Code(err) != "retention_changed" {
		t.Fatal(err)
	}
	if _, err := m.Keep(id, false); err != nil {
		t.Fatal(err)
	}
	child := addDeleteTask(t, m, id, "busy")
	mustUpdate(t, m, child, M{"queue": []any{M{"prompt": "pause"}}, "queuePaused": true})
	if _, err := m.deleteConversation(t.Context(), id, true, now); Code(err) != "task_active" {
		t.Fatal(err)
	}
}
