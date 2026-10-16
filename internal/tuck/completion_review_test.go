package tuck

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// This test-only driver places a barrier before the real cursor query, so the
// publication can be interleaved with the HTTP handler without timing sleeps.
type completionAuditDriver struct {
	driver.Driver
	before func(string)
	after  func(string)
}

func (d completionAuditDriver) Open(name string) (driver.Conn, error) {
	c, err := d.Driver.Open(name)
	if err != nil {
		return nil, err
	}
	return completionAuditConn{Conn: c, before: d.before, after: d.after}, nil
}

type completionAuditConn struct {
	driver.Conn
	before func(string)
	after  func(string)
}

func (c completionAuditConn) QueryContext(ctx context.Context, query string, values []driver.NamedValue) (driver.Rows, error) {
	if c.before != nil {
		c.before(query)
	}
	rows, err := c.Conn.(driver.QueryerContext).QueryContext(ctx, query, values)
	if err == nil && c.after != nil {
		return completionAuditRows{Rows: rows, after: func() { c.after(query) }}, nil
	}
	return rows, err
}

type completionAuditRows struct {
	driver.Rows
	after func()
}

type completionAuditStreamWriter struct {
	*httptest.ResponseRecorder
	cancel     context.CancelFunc
	wantReplay bool
}

func (w completionAuditStreamWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseRecorder.Write(p)
	if strings.Contains(w.Body.String(), "after-snapshot") || !w.wantReplay && strings.Contains(w.Body.String(), ": connected") {
		w.cancel()
	}
	return n, err
}

func (r completionAuditRows) Next(values []driver.Value) error {
	err := r.Rows.Next(values)
	if err == nil {
		r.after()
	}
	return err
}

func TestCompletionReviewSnapshotCursorReplay(t *testing.T) {
	for _, action := range []string{"task", "messages"} {
		for _, phase := range []string{"before", "after"} {
			t.Run(action+"/"+phase, func(t *testing.T) {
				store, err := NewStore(filepath.Join(t.TempDir(), "tuck.db"))
				if err != nil {
					t.Fatal(err)
				}
				defer store.Close()
				manager, err := NewManager(ManagerOptions{Store: store, Engine: &fakeNative{}, WorkspaceRoot: t.TempDir()})
				if err != nil {
					t.Fatal(err)
				}
				defer manager.Close(context.Background())
				task, err := manager.Create(context.Background(), M{})
				if err != nil {
					t.Fatal(err)
				}
				id := S(task["id"])
				atCursor, proceed := make(chan struct{}), make(chan struct{})
				var blocked atomic.Bool
				underlying := store.DB
				name := "completion-audit-" + ID()
				barrier := func(query string) {
					if query == "SELECT COALESCE(MAX(seq),0) FROM events" && blocked.CompareAndSwap(false, true) {
						close(atCursor)
						<-proceed
					}
				}
				wrapped := completionAuditDriver{Driver: underlying.Driver()}
				if phase == "before" {
					wrapped.before = barrier
				} else {
					wrapped.after = barrier
				}
				sql.Register(name, wrapped)
				store.DB, err = sql.Open(name, filepath.ToSlash(store.Path)+"?_pragma=foreign_keys(1)&_pragma=synchronous(FULL)&_pragma=busy_timeout(5000)")
				if err != nil {
					t.Fatal(err)
				}
				defer underlying.Close()
				app := &App{Manager: manager, Store: store, Security: &Security{}}
				path := "/api/tasks/" + id
				if action == "messages" {
					path += "/messages?start=0&count=64&view=workbench"
				}
				w := httptest.NewRecorder()
				done := make(chan error, 1)
				go func() { done <- app.taskRoute(w, httptest.NewRequest("GET", path, nil)) }()
				select {
				case <-atCursor:
				case <-time.After(5 * time.Second):
					t.Fatal("snapshot did not reach cursor query")
				}
				if action == "task" {
					_, err = manager.Update(id, func(task M) error { task["title"] = "after-snapshot"; return nil })
				} else {
					manager.mu.Lock()
					_, err = manager.appendLocked(manager.tasks[id], M{"role": "assistant", "blocks": []any{newBlock("audit", "markdown", M{"markdown": "after-snapshot"})}})
					manager.mu.Unlock()
				}
				close(proceed)
				if err != nil {
					t.Fatal(err)
				}
				if err := <-done; err != nil {
					t.Fatal(err)
				}
				value, err := DecodeJSON(w.Body.Bytes(), 1<<20)
				if err != nil {
					t.Fatal(err)
				}
				// A newer HTTP snapshot is safe as long as replay can supply any
				// mutation not included in it. Message revisions make duplicates safe.
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				events := completionAuditStreamWriter{ResponseRecorder: httptest.NewRecorder(), cancel: cancel, wantReplay: phase == "after" || !strings.Contains(w.Body.String(), "after-snapshot")}
				r := httptest.NewRequest("GET", fmt.Sprintf("/api/events?since=%d&view=workbench", I(O(value)["cursor"])), nil).WithContext(ctx)
				if err := app.events(events, r, nil); err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(w.Body.String()+events.Body.String(), "after-snapshot") {
					t.Fatalf("committed mutation absent from HTTP snapshot and SSE replay; snapshot=%s replay=%s", w.Body.String(), events.Body.String())
				}
				if phase == "after" && !strings.Contains(events.Body.String(), "after-snapshot") {
					t.Fatalf("a cursor captured before publication did not replay the mutation: %s", events.Body.String())
				}
			})
		}
	}
}

func TestCompletionReviewImageBytesDeterminePreviewMIME(t *testing.T) {
	manager, _, _, id := taskFixture(t)
	files := &Files{Manager: manager}
	for _, name := range []string{"evidence.txt", "extensionless"} {
		original := testPNG(t, 3, 4, false)
		attachment, err := files.Upload(mustTask(t, manager, id), uploadData(name, original))
		if err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/tasks/"+id+"/files?path="+S(attachment["path"]), nil)
		if err := files.Serve(w, r, mustTask(t, manager, id)); err != nil {
			t.Fatal(err)
		}
		if w.Header().Get("Content-Type") != "image/png" || !strings.HasPrefix(w.Header().Get("Content-Disposition"), "inline;") {
			t.Errorf("valid uploaded PNG %q cannot be previewed: headers=%v", name, w.Header())
		}
		if !bytes.Equal(w.Body.Bytes(), original) {
			t.Fatal("preview changed the original upload bytes")
		}
		w = httptest.NewRecorder()
		r = httptest.NewRequest("GET", "/api/tasks/"+id+"/files?path="+S(attachment["path"])+"&download=1", nil)
		if err := files.Serve(w, r, mustTask(t, manager, id)); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(w.Header().Get("Content-Disposition"), "attachment;") || !bytes.Equal(w.Body.Bytes(), original) {
			t.Fatal("download did not retain attachment disposition and original bytes", w.Header())
		}
	}
	task := mustTask(t, manager, id)
	if err := os.WriteFile(filepath.Join(S(task["workspace"]), "spoof.html"), []byte("<script>bad()</script>"), 0600); err != nil {
		t.Fatal(err)
	}
	task["uploads"] = append(A(task["uploads"]), M{"path": "spoof.html", "kind": "image", "mime": "text/html"})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/tasks/"+id+"/files?path=spoof.html", nil)
	if err := files.Serve(w, r, task); err != nil {
		t.Fatal(err)
	}
	if w.Header().Get("Content-Type") != "text/plain; charset=utf-8" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("unvalidated MIME enabled active HTML content", w.Header())
	}
}

func TestCompletionReviewForkRejectsNonIntegerJSONIndex(t *testing.T) {
	for _, index := range []any{nil, "0", true, 0.5, -0.5, []any{}, M{}} {
		t.Run(fmt.Sprintf("%T-%v", index, index), func(t *testing.T) {
			manager, store, engine, id := taskFixture(t)
			forks := 0
			engine.request = func(_ context.Context, method, path string, _ any) (M, error) {
				if method == "GET" {
					return M{"messages": []any{M{"role": "user", "content": "first prompt"}}}, nil
				}
				if method == "POST" && strings.HasSuffix(path, "/fork") {
					forks++
					return M{"id": UUID(), "title": "fork", "messages": []any{}}, nil
				}
				return M{}, nil
			}
			app := &App{Manager: manager, Store: store}
			body := M{}
			if index != nil {
				body["index"] = index
			}
			r := httptest.NewRequest("POST", "/api/tasks/"+id+"/fork", bytes.NewReader(JSON(body)))
			err := app.taskRoute(httptest.NewRecorder(), r)
			if Status(err) != 400 || forks != 0 || len(manager.List()) != 1 {
				t.Fatalf("invalid fork index was coerced into a real native fork: index=%v err=%v nativeForks=%d tasks=%d", index, err, forks, len(manager.List()))
			}
		})
	}
}

func TestCompletionReviewSubmissionJSONContracts(t *testing.T) {
	for _, action := range []string{"prompts", "steer"} {
		for _, invalid := range []M{{"attachmentIds": nil}, {"attachmentIds": "attachment-id"}, {"attachmentIds": M{}}} {
			t.Run(action+"/"+string(JSON(invalid)), func(t *testing.T) {
				manager, store, _, id := taskFixture(t)
				mustUpdate(t, manager, id, M{"queuePaused": true})
				body := Merge(M{"prompt": "keep the invalid request out of execution"}, invalid)
				r := httptest.NewRequest("POST", "/api/tasks/"+id+"/"+action, bytes.NewReader(JSON(body)))
				app := &App{Manager: manager, Store: store}
				err := app.taskRoute(httptest.NewRecorder(), r)
				if Status(err) != 400 || len(A(mustTask(t, manager, id)["queue"])) != 0 {
					t.Fatalf("invalid attachmentIds silently treated as empty: err=%v queue=%s", err, JSON(mustTask(t, manager, id)["queue"]))
				}
			})
		}
	}
	t.Run("explicit-null-turn-precondition", func(t *testing.T) {
		manager, store, _, id := taskFixture(t)
		mustUpdate(t, manager, id, M{"queuePaused": true})
		r := httptest.NewRequest("POST", "/api/tasks/"+id+"/prompts", bytes.NewReader(JSON(M{"prompt": "continuation", "expectedTurnCount": nil})))
		app := &App{Manager: manager, Store: store}
		err := app.taskRoute(httptest.NewRecorder(), r)
		if Status(err) != 400 || len(A(mustTask(t, manager, id)["queue"])) != 0 {
			t.Fatalf("explicit null bypassed the supplied turn precondition: err=%v queue=%s", err, JSON(mustTask(t, manager, id)["queue"]))
		}
	})
}
