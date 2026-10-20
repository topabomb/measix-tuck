package tuck

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func addDeleteTask(t *testing.T, m *Manager, parent, native string) string {
	t.Helper()
	task := BaseTask(ID(), native, "删除验证", m.options.WorkspaceRoot, "root", "tuck")
	task["parentTaskId"] = parent
	m.mu.Lock()
	defer m.mu.Unlock()
	id := S(task["id"])
	m.tasks[id] = task
	if err := m.publishLocked(task, M{"type": "created", "task": task}, nil); err != nil {
		t.Fatal(err)
	}
	return id
}

func deletionNative(engine *fakeNative, existing map[string]bool, calls *[]string) {
	engine.request = func(_ context.Context, method, path string, _ any) (M, error) {
		key := strings.TrimSuffix(strings.TrimPrefix(path, "/api/sessions/"), "?wait=true")
		if method == "GET" {
			if !existing[key] {
				return nil, nativeError(404, []byte(`{"message":"session not found: session not found"}`))
			}
			return M{"id": key}, nil
		}
		if method == "DELETE" {
			*calls = append(*calls, key)
			delete(existing, key)
			return M{"message": "session deleted"}, nil
		}
		return nil, errors.New("unexpected native request")
	}
}

func TestDeleteConversationSubtreeRetainsForkAndSharedFiles(t *testing.T) {
	m, s, engine, id := taskFixture(t)
	root := mustTask(t, m, id)
	child := addDeleteTask(t, m, id, "child")
	grandchild := addDeleteTask(t, m, child, "grandchild")
	fork := addDeleteTask(t, m, "", "fork")
	mustUpdate(t, m, fork, M{"forkedFrom": M{"id": id}, "workspace": root["workspace"], "historyTiming": M{"previousTurns": 5, "previousMs": 5000}})
	before := mustTask(t, m, fork)
	for _, key := range []string{id, child, grandchild, fork} {
		if err := s.SaveMessage(key, M{"id": key + ":m-0", "index": 0, "revision": 0, "blocks": []any{}}); err != nil {
			t.Fatal(err)
		}
	}
	source := filepath.Join(S(root["workspace"]), "shared.txt")
	if err := os.WriteFile(source, []byte("user-owned"), 0600); err != nil {
		t.Fatal(err)
	}
	calls := []string{}
	beforeStats, err := s.Statistics(before)
	if err != nil {
		t.Fatal(err)
	}
	cursor, err := s.Cursor()
	if err != nil {
		t.Fatal(err)
	}
	deletionNative(engine, map[string]bool{S(root["nativeSessionId"]): true, "child": true, "grandchild": true, "fork": true}, &calls)
	result, err := m.Delete(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(calls, []string{"grandchild", "child", S(root["nativeSessionId"])}) {
		t.Fatal(calls)
	}
	for _, key := range []string{id, child, grandchild} {
		if _, err := m.Get(key); Code(err) != "task_not_found" {
			t.Fatal(key, err)
		}
		for _, table := range []string{"messages", "events", "task_deliveries"} {
			var count int
			if err := s.DB.QueryRow("SELECT COUNT(*) FROM "+table+" WHERE task_id=?", key).Scan(&count); err != nil || count != 0 {
				t.Fatal(table, count, err)
			}
		}
	}
	if !reflect.DeepEqual(before, mustTask(t, m, fork)) {
		t.Fatal("independent fork changed")
	}
	afterStats, err := s.Statistics(mustTask(t, m, fork))
	if err != nil || !reflect.DeepEqual(beforeStats, afterStats) || int64(I(result["seq"])) <= cursor {
		t.Fatal("other statistics or cursor changed", beforeStats, afterStats, result, err)
	}
	if raw, err := os.ReadFile(source); err != nil || string(raw) != "user-owned" {
		t.Fatal("shared source changed", err)
	}
	var raw string
	var taskID *string
	if err := s.DB.QueryRow("SELECT task_id,payload_json FROM events WHERE seq=?", result["seq"]).Scan(&taskID, &raw); err != nil || taskID != nil || !strings.Contains(raw, `"deletedTaskIds"`) {
		t.Fatal(taskID, raw, err)
	}
	restored, err := NewManager(m.options)
	if err != nil || len(restored.List()) != 1 || S(restored.List()[0]["id"]) != fork {
		t.Fatal("restart restored deleted tasks", err)
	}
}

func TestDeleteConversationRejectsBusyDescendantsAndChildEntry(t *testing.T) {
	for _, patch := range []M{
		{"status": "working"}, {"status": "waiting"},
		{"pendingInteraction": M{"id": "approval"}},
		{"queuePaused": true, "queue": []any{M{"prompt": "do not lose"}}},
		{"queuedPrompts": 1},
		{"backgroundRuns": []any{M{"status": "running"}}},
	} {
		t.Run(string(JSON(patch)), func(t *testing.T) {
			m, _, engine, id := taskFixture(t)
			child := addDeleteTask(t, m, id, "child")
			mustUpdate(t, m, child, patch)
			engine.request = func(context.Context, string, string, any) (M, error) {
				t.Fatal("busy task reached engine")
				return nil, nil
			}
			if _, err := m.Delete(t.Context(), id); Code(err) != "task_active" {
				t.Fatal(err)
			}
			if _, err := m.Delete(t.Context(), child); Code(err) != "child_delete" {
				t.Fatal(err)
			}
		})
	}
}

func TestDeleteConversationFailureRetryAndTransactionRollback(t *testing.T) {
	m, s, engine, id := taskFixture(t)
	root := mustTask(t, m, id)
	if err := s.SaveMessage(id, M{"id": "history", "index": 0, "revision": 0}); err != nil {
		t.Fatal(err)
	}
	calls := []string{}
	existing := map[string]bool{S(root["nativeSessionId"]): true}
	deletionNative(engine, existing, &calls)
	if _, err := s.DB.Exec("CREATE TRIGGER prevent_delete BEFORE DELETE ON tasks BEGIN SELECT RAISE(ABORT,'injected'); END"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Delete(t.Context(), id); Code(err) != "delete_commit_failed" {
		t.Fatal(err)
	}
	if msg, _ := s.Message(id, 0); msg == nil {
		t.Fatal("transaction did not roll back messages")
	}
	mustTask(t, m, id)
	if _, err := s.DB.Exec("DROP TRIGGER prevent_delete"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Delete(t.Context(), id); err != nil {
		t.Fatal("retry should accept confirmed native absence", err)
	}
	if len(calls) != 1 {
		t.Fatal("deleted native session again", calls)
	}
}

func TestDeleteConversationUnconfirmedNativePreservesLocal(t *testing.T) {
	for _, mode := range []string{"network", "wrong404", "accepted", "delete-failure", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			m, s, engine, id := taskFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			engine.request = func(_ context.Context, method, _ string, _ any) (M, error) {
				if method == "GET" {
					switch mode {
					case "network":
						return nil, errors.New("offline")
					case "wrong404":
						return nil, nativeError(404, []byte(`{"message":"route not found"}`))
					}
					return M{}, nil
				}
				switch mode {
				case "accepted":
					return M{"message": "session deleted, stop still in progress"}, nil
				case "delete-failure":
					return nil, errors.New("delete failed")
				case "canceled":
					cancel()
				}
				return M{"message": "session deleted"}, nil
			}
			if _, err := m.Delete(ctx, id); err == nil {
				t.Fatal("unconfirmed deletion succeeded")
			}
			mustTask(t, m, id)
			if task, _ := s.Task(id); task == nil {
				t.Fatal("lost local task")
			}
		})
	}
}

func TestDeleteConversationPartialNativeFailureCanRetry(t *testing.T) {
	m, s, engine, id := taskFixture(t)
	root := mustTask(t, m, id)
	child := addDeleteTask(t, m, id, "child")
	existing := map[string]bool{S(root["nativeSessionId"]): true, "child": true}
	calls := []string{}
	deletionNative(engine, existing, &calls)
	normal := engine.request
	engine.request = func(ctx context.Context, method, path string, body any) (M, error) {
		if method == "DELETE" && path == nativePath(root, "?wait=true") {
			return nil, errors.New("parent deletion unavailable")
		}
		return normal(ctx, method, path, body)
	}
	if _, err := m.Delete(t.Context(), id); Code(err) != "native_delete_failed" {
		t.Fatal(err)
	}
	for _, key := range []string{id, child} {
		mustTask(t, m, key)
		if task, _ := s.Task(key); task == nil {
			t.Fatal("partial native failure removed local history")
		}
	}
	engine.request = normal
	if _, err := m.Delete(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(calls, []string{"child", S(root["nativeSessionId"])}) {
		t.Fatal("retry repeated an already deleted child", calls)
	}
}

func TestDeleteConversationGuardsConcurrentMutation(t *testing.T) {
	m, _, engine, id := taskFixture(t)
	_, finish, err := m.beginNativeChange(id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Delete(t.Context(), id); Code(err) != "task_active" {
		t.Fatal(err)
	}
	finish()
	entered, release := make(chan struct{}), make(chan struct{})
	engine.request = func(_ context.Context, method, _ string, _ any) (M, error) {
		if method == "DELETE" {
			close(entered)
			<-release
			return M{"message": "session deleted"}, nil
		}
		return M{}, nil
	}
	done := make(chan error, 1)
	go func() { _, err := m.Delete(t.Context(), id); done <- err }()
	<-entered
	_, updateErr := m.Update(id, func(M) error { t.Error("mutation reached deleted task"); return nil })
	_, submitErr := m.Submit(id, "concurrent", nil, nil, nil)
	_, deleteErr := m.Delete(t.Context(), id)
	active := m.Active()
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if Code(updateErr) != "task_deleting" || Code(submitErr) != "task_deleting" || Code(deleteErr) != "task_deleting" || !active {
		t.Fatal(updateErr, submitErr, deleteErr, active)
	}
}

func TestDeleteConversationDeliveryCleanupBoundaries(t *testing.T) {
	d, turn, _, s, _ := deliveryFixture(t)
	source := filepath.Join(turn.Workspace, "shared.txt")
	if err := os.WriteFile(source, []byte("keep source"), 0600); err != nil {
		t.Fatal(err)
	}
	deliveryOK(t, d, turn, "snapshot", []any{M{"path": "shared.txt"}})
	latest, err := s.Delivery(turn.TaskID, "latest")
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(d.Root, "snapshots", turn.TaskID, S(latest["id"]))
	if _, err := s.DeleteTasks(t.Context(), []string{turn.TaskID}); err != nil {
		t.Fatal(err)
	}
	d.removeSnapshot = func(*os.Root, string) error { return os.ErrPermission }
	if err := d.cleanup(turn.TaskID); err == nil {
		t.Fatal("cleanup failure was hidden")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatal("failed cleanup lost snapshot", err)
	}
	d.removeSnapshot = nil
	if err := d.cleanup(turn.TaskID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("snapshot retained", err)
	}
	if raw, err := os.ReadFile(source); err != nil || string(raw) != "keep source" {
		t.Fatal("deleted workspace file", err)
	}
}

func TestDeleteConversationCleanupRejectsSymlink(t *testing.T) {
	m, s, _, _ := taskFixture(t)
	d, err := NewDeliveries(m, s, nil, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	shared := t.TempDir()
	delivery := ID()
	if err := os.MkdirAll(filepath.Join(shared, delivery), 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(shared, delivery, "keep.txt")
	if err := os.WriteFile(file, []byte("shared"), 0600); err != nil {
		t.Fatal(err)
	}
	id := ID()
	if err := os.Symlink(shared, filepath.Join(d.Root, "snapshots", id)); err != nil {
		t.Skip("symlink not supported:", err)
	}
	if err := d.cleanup(id); err == nil {
		t.Fatal("followed task symlink")
	}
	if raw, err := os.ReadFile(file); err != nil || string(raw) != "shared" {
		t.Fatal("symlink target changed", err)
	}
}
