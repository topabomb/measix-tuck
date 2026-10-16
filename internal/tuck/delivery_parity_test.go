package tuck

import (
	"bytes"
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestParityDeliveryArgumentsAndRetention(t *testing.T) {
	d, tr, _, s, p := deliveryFixture(t)
	if len(protectable) != 6 {
		t.Fatal(protectable)
	}
	schema := O(deliveryDefinition()["inputSchema"])
	properties := O(schema["properties"])
	fileSchema := O(properties["files"])
	if string(JSON(schema["required"])) != `["summary"]` || I(O(properties["summary"])["maxLength"]) != 1000 || I(fileSchema["maxItems"]) != 10 || string(JSON(O(fileSchema["items"])["required"])) != `["path"]` {
		t.Fatal("delivery schema contract changed", schema)
	}
	summary := strings.Repeat("😀", 1000)
	for _, args := range []M{{"summary": summary}, {"summary": "nullable", "files": nil}} {
		r := d.Submit(context.Background(), tr, args)
		if r["status"] != "completed" {
			t.Fatal(r)
		}
	}
	files := []any{}
	for i := 0; i < 10; i++ {
		name := fmt.Sprintf("%d.txt", i)
		os.WriteFile(filepath.Join(tr.Workspace, name), []byte(name), 0600)
		files = append(files, M{"path": name, "description": nil})
	}
	O(files[0])["description"] = "key file"
	delete(O(files[2]), "description")
	if d.Submit(context.Background(), tr, M{"summary": "ten", "files": files})["status"] != "completed" {
		t.Fatal("ten files rejected")
	}
	latest, err := s.Delivery(tr.TaskID, "latest")
	if err != nil || len(A(latest["files"])) != 10 {
		t.Fatal(latest, err)
	}
	for i, item := range A(latest["files"]) {
		file := O(item)
		if file["path"] != O(files[i])["path"] || i == 0 && file["description"] != "key file" || i > 0 && Has(file, "description") {
			t.Fatal("nullable description or file identity changed", i, file)
		}
	}
	for _, bad := range []M{{"summary": summary + "😀"}, {"summary": " ", "files": nil}, {"summary": "ok", "files": "bad"}, {"summary": "ok", "files": append(files, files[0])}, {"summary": "ok", "request_id": "forged"}} {
		if d.Submit(context.Background(), tr, bad)["status"] != "failed" {
			t.Fatal(bad)
		}
	}
	for _, bad := range []any{-1, 1.5, "1", nil, 101} {
		c := p.models.Config()
		O(c["workbench"])["deliveryRetentionCount"] = bad
		if ValidateConfig(c, false) == nil {
			t.Fatal("invalid retention", bad)
		}
		delete(O(c["workbench"]), "deliveryRetentionCount")
		O(A(O(c["workbench"])["assistants"])[0])["deliveryRetentionCount"] = bad
		if ValidateConfig(c, false) == nil {
			t.Fatal("invalid assistant retention", bad)
		}
	}
	for i := 0; i < 8; i++ {
		deliveryOK(t, d, tr, fmt.Sprint("default ten ", i), nil)
	}
	defaultVersions, _ := s.Deliveries(tr.TaskID, 0, 100)
	if len(defaultVersions) != 10 {
		t.Fatal("default retention is not ten", len(defaultVersions))
	}
	c := p.models.Config()
	O(c["workbench"])["deliveryRetentionCount"] = 2
	for _, v := range A(O(c["workbench"])["assistants"]) {
		delete(O(v), "deliveryRetentionCount")
	}
	if e := p.models.SetConfig(c); e != nil {
		t.Fatal(e)
	}
	deliveryOK(t, d, tr, "inherit one", nil)
	deliveryOK(t, d, tr, "inherit two", nil)
	all, _ := s.Deliveries(tr.TaskID, 0, 100)
	if len(all) != 2 {
		t.Fatal(all)
	}
	c = p.models.Config()
	for _, v := range A(O(c["workbench"])["assistants"]) {
		if O(v)["agent"] == tr.Task["agent"] {
			O(v)["deliveryRetentionCount"] = 0
		}
	}
	if e := p.models.SetConfig(c); e != nil {
		t.Fatal(e)
	}
	deliveryOK(t, d, tr, "zero retains latest", nil)
	all, _ = s.Deliveries(tr.TaskID, 0, 100)
	if len(all) != 1 {
		t.Fatal(all)
	}
	d.Submit(context.Background(), tr, M{"summary": "invalid", "files": []any{M{"path": "missing"}}})
	after, _ := s.Deliveries(tr.TaskID, 0, 100)
	if string(JSON(all)) != string(JSON(after)) {
		t.Fatal("failure pruned deliveries")
	}
}

func TestParityDeliveryCleanupFailureAndBackup(t *testing.T) {
	d, tr, _, s, p := deliveryFixture(t)
	os.WriteFile(filepath.Join(tr.Workspace, "result.txt"), []byte("immutable bytes"), 0600)
	c := p.models.Config()
	O(c["workbench"])["deliveryRetentionCount"] = 1
	for _, v := range A(O(c["workbench"])["assistants"]) {
		delete(O(v), "deliveryRetentionCount")
	}
	p.models.SetConfig(c)
	deliveryOK(t, d, tr, "first", []any{M{"path": "result.txt"}})
	d.removeSnapshot = func(*os.Root, string) error { return os.ErrPermission }
	r := deliveryOK(t, d, tr, "second", []any{M{"path": "result.txt"}})
	if !B(r["cleanup_pending"]) {
		t.Fatal(r)
	}
	if e := d.recover(); e != nil {
		t.Fatal("cleanup prevented startup", e)
	}
	entries, _ := os.ReadDir(filepath.Join(d.Root, "snapshots", tr.TaskID))
	if len(entries) != 2 {
		t.Fatal(entries)
	}
	d.removeSnapshot = nil
	deliveryOK(t, d, tr, "third", []any{M{"path": "result.txt"}})
	entries, _ = os.ReadDir(filepath.Join(d.Root, "snapshots", tr.TaskID))
	if len(entries) != 1 {
		t.Fatal(entries)
	}
	backupRoot := t.TempDir()
	dbPath := filepath.Join(backupRoot, "tuck.db")
	if _, e := s.DB.Exec("VACUUM INTO ?", dbPath); e != nil {
		t.Fatal(e)
	}
	if e := os.CopyFS(filepath.Join(backupRoot, "deliveries"), os.DirFS(d.Root)); e != nil {
		t.Fatal(e)
	}
	copyStore, e := NewStore(dbPath)
	if e != nil {
		t.Fatal(e)
	}
	defer copyStore.Close()
	if e = copyStore.Check(); e != nil {
		t.Fatal(e)
	}
	orig, _ := s.Delivery(tr.TaskID, "latest")
	restored, _ := copyStore.Delivery(tr.TaskID, "latest")
	if string(JSON(orig)) != string(JSON(restored)) {
		t.Fatal("backup metadata changed")
	}
	copyDelivery := &Deliveries{Store: copyStore, Root: filepath.Join(backupRoot, "deliveries")}
	file := O(A(restored["files"])[0])
	w := httptest.NewRecorder()
	if e = copyDelivery.ServeFile(w, httptest.NewRequest("GET", "http://localhost/", nil), tr.TaskID, "latest", S(file["id"])); e != nil || w.Body.String() != "immutable bytes" {
		t.Fatal(e, w.Body.String())
	}
}

type cancelCopyContext struct {
	context.Context
	cancel context.CancelFunc
	calls  atomic.Int32
	at     int32
}

func (c *cancelCopyContext) Err() error {
	at := c.at
	if at == 0 {
		at = 20
	}
	if c.calls.Add(1) >= at {
		c.cancel()
	}
	return c.Context.Err()
}
func TestParityDeliveryMidCopyAndFileIdentity(t *testing.T) {
	d, tr, _, s, _ := deliveryFixture(t)
	source := filepath.Join(tr.Workspace, "source.bin")
	f, e := os.Create(source)
	if e != nil {
		t.Fatal(e)
	}
	f.Write(bytes.Repeat([]byte{71}, 4<<20))
	f.Truncate(64 << 20)
	f.Close()
	beforeHash, _ := FileHash(source)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	counted := &cancelCopyContext{Context: ctx, cancel: cancel}
	r := d.Submit(counted, tr, M{"summary": "canceled copy", "files": []any{M{"path": "source.bin"}}})
	if r["code"] != "delivery_canceled" || counted.calls.Load() < 20 {
		t.Fatal(r, counted.calls.Load())
	}
	all, _ := s.Deliveries(tr.TaskID, 0, 100)
	if len(all) != 0 {
		t.Fatal(all)
	}
	for _, dir := range []string{filepath.Join(d.Root, ".staging"), filepath.Join(d.Root, "snapshots", tr.TaskID)} {
		entries, _ := os.ReadDir(dir)
		if len(entries) != 0 {
			t.Fatal("uncommitted files retained", entries)
		}
	}
	info, _ := os.Stat(source)
	afterHash, _ := FileHash(source)
	if beforeHash != afterHash {
		t.Fatal("source changed during cancellation")
	}
	if info.Size() != 64<<20 {
		t.Fatal(info)
	}
	os.Mkdir(filepath.Join(tr.Workspace, "directory"), 0700)
	for _, files := range [][]any{{M{"path": "directory"}}, {M{"path": "source.bin"}, M{"path": "source.bin"}}} {
		if d.Submit(context.Background(), tr, M{"summary": "invalid identity", "files": files})["status"] != "failed" {
			t.Fatal(files)
		}
	}
	if e = os.Link(source, filepath.Join(tr.Workspace, "hard.bin")); e == nil {
		if d.Submit(context.Background(), tr, M{"summary": "hardlink duplicate", "files": []any{M{"path": "source.bin"}, M{"path": "hard.bin"}}})["status"] != "failed" {
			t.Fatal("hardlink aliases accepted")
		}
	}
	outside := t.TempDir()
	if e = os.WriteFile(filepath.Join(outside, "private"), []byte("private outside bytes"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.Symlink(outside, filepath.Join(tr.Workspace, "outside")); e == nil {
		if r := d.Submit(context.Background(), tr, M{"summary": "escape", "files": []any{M{"path": "outside/private"}}}); r["code"] != "path_outside_workspace" {
			t.Fatal("symlink escape accepted")
		}
	}
}

func TestParityDeliveryCancellationAtCommitBoundary(t *testing.T) {
	d, tr, manager, s, _ := deliveryFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Empty deliveries check cancellation once before publication, then again
	// after taking the manager lock in commit. Cancel at that second boundary.
	counted := &cancelCopyContext{Context: ctx, cancel: cancel, at: 2}
	r := d.Submit(counted, tr, M{"summary": "cancel at commit"})
	if r["code"] != "delivery_canceled" {
		t.Fatal("cancellation misreported as storage failure", r)
	}
	task := mustTask(t, manager, tr.TaskID)
	if B(task["queuePaused"]) || task["lastFailure"] != nil {
		t.Fatal("cancellation changed task failure state", task)
	}
	all, err := s.Deliveries(tr.TaskID, 0, 100)
	if err != nil || len(all) != 0 {
		t.Fatal(all, err)
	}
	for _, dir := range []string{filepath.Join(d.Root, ".staging"), filepath.Join(d.Root, "snapshots", tr.TaskID)} {
		entries, err := os.ReadDir(dir)
		if err != nil && !os.IsNotExist(err) || len(entries) != 0 {
			t.Fatal("canceled delivery retained snapshot", entries, err)
		}
	}
}
