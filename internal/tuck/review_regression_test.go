package tuck

import (
	"context"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"
	"time"
)

func TestAuthenticationChangeRevokesExistingAndLateStreams(t *testing.T) {
	c, _, s, _ := securityFixture(t, true, nil)
	r, _ := loginBrowser(t, s, false)
	session, e := s.Session(r)
	if e != nil {
		t.Fatal(e)
	}
	watch, cancel := s.Watch(context.Background(), session)
	defer cancel()
	nextHash, _ := PasswordHash("a different long password", 12)
	v, revision, _ := c.Read()
	O(O(v["security"])["auth"])["passwordHash"] = nextHash
	own, e := c.Save(revision, v)
	if e != nil {
		t.Fatal(e)
	}
	c.MarkAppliedVersion(v, own)
	select {
	case <-watch.Done():
	case <-time.After(time.Second):
		t.Fatal("old stream survived credential change")
	}
	late, stop := s.Watch(context.Background(), session)
	defer stop()
	if late.Err() == nil {
		t.Fatal("revoked session registered a late stream")
	}
}
func TestAuthenticationChangeDuringPasswordHash(t *testing.T) {
	c, store, s, _ := securityFixture(t, true, nil)
	nextHash, _ := PasswordHash("a different long password", 12)
	r := browserRequest("POST")
	done := make(chan error, 1)
	go func() {
		_, e := s.Login(r, httptest.NewRecorder(), M{"username": "operator", "password": testPassword, "rememberMe": false})
		done <- e
	}()
	deadline := time.Now().Add(time.Second)
	for {
		s.mu.Lock()
		checking := s.checks > 0
		s.mu.Unlock()
		if checking {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("hash did not begin")
		}
		time.Sleep(time.Millisecond)
	}
	v, revision, _ := c.Read()
	O(O(v["security"])["auth"])["passwordHash"] = nextHash
	own, e := c.Save(revision, v)
	if e != nil {
		t.Fatal(e)
	}
	c.MarkAppliedVersion(v, own)
	if e = <-done; Status(e) != 409 {
		t.Fatal("old password obtained a current session", e)
	}
	var count int
	store.DB.QueryRow("SELECT COUNT(*) FROM web_sessions").Scan(&count)
	if count != 0 {
		t.Fatal("session created from obsolete credentials")
	}
}
func TestSettingsHashDetectsConcurrentExternalEdit(t *testing.T) {
	c, _, _, p := securityFixture(t, false, nil)
	v, revision, _ := c.Read()
	done := make(chan error, 1)
	go func() {
		_, _, e := p.Prepare(M{"revision": revision, "password": "six-ok", "categories": []any{"providers"}})
		done <- e
	}()
	deadline := time.Now().Add(time.Second)
	for {
		p.mu.Lock()
		checking := p.checks > 0
		p.mu.Unlock()
		if checking {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("hash did not begin")
		}
		time.Sleep(time.Millisecond)
	}
	O(v["workbench"])["deliveryRetentionCount"] = 3
	if e := AtomicWrite(c.Path, JSON(v), 0600); e != nil {
		t.Fatal(e)
	}
	if e := <-done; Status(e) != 409 {
		t.Fatal("stale prepared config", e)
	}
}
func TestSuccessfulApplyPreservesNewerExternalConfiguration(t *testing.T) {
	f := newPlatformFixture(t, 5)
	app := f.startApp()
	data, e := RuntimeView(app.Config, app.Models, nil)
	if e != nil {
		t.Fatal(e)
	}
	next := O(data["config"])
	O(next["workbench"])["requireDelivery"] = true
	calls := 0
	var external M
	e = app.saveRuntime(browserRequest("PUT"), data, func() error {
		calls++
		if calls == 3 {
			external, _, e = app.Config.Read()
			if e != nil {
				return e
			}
			O(external["workbench"])["deliveryRetentionCount"] = 7
			if e = AtomicWrite(app.Config.Path, JSON(external), 0600); e != nil {
				return e
			}
			_, _, e = app.Config.Read()
			return e
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	if !app.Config.Pending() || I(O(app.Config.Applied()["workbench"])["deliveryRetentionCount"]) == 7 || I(O(app.Models.Config()["workbench"])["deliveryRetentionCount"]) == 7 {
		t.Fatal("external edit incorrectly marked as applied")
	}
	saved, _, _ := app.Config.Read()
	if I(O(saved["workbench"])["deliveryRetentionCount"]) != 7 {
		t.Fatal("external edit overwritten")
	}
}
func TestDeliveryCancellationWhileWaitingForDatabase(t *testing.T) {
	d, tr, m, s, _ := deliveryFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	s.writeMu.Lock()
	done := make(chan error, 1)
	go func() {
		_, e := d.commit(ctx, tr, M{"id": ID(), "taskId": tr.TaskID, "turnId": tr.TurnID, "createdAt": NowMS(), "summary": "must not commit", "fileCount": 0, "totalBytes": 0, "files": []any{}})
		done <- e
	}()
	// Manager owns its task lock while the commit waits for the database writer.
	deadline := time.Now().Add(time.Second)
	for {
		if !m.mu.TryLock() {
			break
		}
		m.mu.Unlock()
		if time.Now().After(deadline) {
			s.writeMu.Unlock()
			t.Fatal("commit did not start")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	s.writeMu.Unlock()
	if e := <-done; e == nil {
		t.Fatal("canceled delivery committed")
	}
	list, e := s.Deliveries(tr.TaskID, 0, 20)
	if e != nil || len(list) != 0 {
		t.Fatal(list, e)
	}
}
func TestEngineSavedSelectionRollbackAfterModelRebuildFailure(t *testing.T) {
	a, _ := managedTestUpdate(t)
	u := a.Updates
	if e := u.Check(context.Background()); e != nil {
		t.Fatal(e)
	}
	if e := u.Download(context.Background(), "v1.150.0"); e != nil {
		t.Fatal(e)
	}
	before, _ := os.ReadFile(a.Config.Path)
	previous := a.Models.Config()
	calls := 0
	var ddl string
	a.Store.DB.QueryRow("SELECT sql FROM sqlite_master WHERE name='mcp_catalog'").Scan(&ddl)
	e := u.Apply(context.Background(), "v1.150.0", func() error {
		calls++
		if calls == 2 {
			_, e := a.Store.DB.Exec("DROP TABLE mcp_catalog")
			return e
		}
		return nil
	})
	if e == nil {
		t.Fatal("failed rebuild unexpectedly applied")
	}
	after, _ := os.ReadFile(a.Config.Path)
	if string(before) != string(after) || !reflect.DeepEqual(previous, a.Models.Config()) {
		t.Fatal("failed update retained new selection")
	}
	if a.Engine.State()["state"] != "ready" {
		t.Fatal("old engine not recovered", a.Engine.State())
	}
	if _, e = a.Store.DB.Exec(ddl); e != nil {
		t.Fatal(e)
	}
}
