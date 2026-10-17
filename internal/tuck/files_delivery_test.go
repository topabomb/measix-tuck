package tuck

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func uploadData(name string, raw []byte) M {
	return M{"name": name, "base64": base64.StdEncoding.EncodeToString(raw)}
}

func TestArtifactDirectoryLinksAndDownloadScope(t *testing.T) {
	for _, kind := range []string{"uploads", "images", "downloads"} {
		t.Run(kind, func(t *testing.T) {
			f := newWorkspaceFixture(t)
			l, turn := f.turn(t)
			outside := filepath.Join(turn.Workspace, "project")
			if e := os.MkdirAll(outside, 0700); e != nil {
				t.Fatal(e)
			}
			if e := os.MkdirAll(filepath.Join(turn.Workspace, TaskArtifactsDirectory), 0700); e != nil {
				t.Fatal(e)
			}
			if e := directoryLink(outside, filepath.Join(turn.Workspace, TaskArtifactsDirectory, kind)); e != nil {
				t.Fatal(e)
			}
			root, e := openArtifactRoot(turn.Task, kind)
			if root != nil {
				root.Close()
			}
			if Code(e) != "artifact_directory_changed" {
				t.Fatal("accepted redirected artifact root", kind, e)
			}
			entries, e := os.ReadDir(outside)
			if e != nil || len(entries) != 0 {
				t.Fatal("redirected directory changed", entries, e)
			}
			_ = l
		})
	}
	f := newWorkspaceFixture(t)
	l, turn := f.turn(t)
	root, e := openArtifactRoot(turn.Task, "downloads")
	if e != nil {
		t.Fatal(e)
	}
	root.Close()
	outside := filepath.Join(turn.Workspace, "project")
	if e = os.MkdirAll(outside, 0700); e != nil {
		t.Fatal(e)
	}
	if e = directoryLink(outside, filepath.Join(turn.Workspace, "escape")); e != nil {
		t.Fatal(e)
	}
	for _, directory := range []string{"../images", outside} {
		out := l.Transfer(turn.ctx, turn, "download_runcove_files", M{"remote_paths": []any{"hello.txt"}, "local_directory": directory})
		if out["status"] != "failed" {
			t.Fatal("download escaped type directory", directory, out)
		}
	}
	entries, e := os.ReadDir(outside)
	if e != nil || len(entries) != 0 {
		t.Fatal("project directory changed", entries, e)
	}
	out := l.Transfer(turn.ctx, turn, "download_runcove_files", M{"remote_paths": []any{"hello.txt"}, "local_directory": "."})
	if out["status"] != "completed" || out["local_directory"] != "." || O(O(A(out["files"])[0])["file"])["path"] != "hello.txt" {
		t.Fatal("download path contract", out)
	}
}
func testPNG(t *testing.T, width, height int, random bool) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	if random {
		if _, e := rand.Read(img.Pix); e != nil {
			t.Fatal(e)
		}
	} else {
		for i := 0; i < len(img.Pix); i += 4 {
			copy(img.Pix[i:], []byte{18, 164, 86, 255})
		}
	}
	var out bytes.Buffer
	if e := png.Encode(&out, img); e != nil {
		t.Fatal(e)
	}
	return out.Bytes()
}
func fileFixture(t *testing.T) (*Files, *Manager, *Store, string, *platformFixture) {
	t.Helper()
	m, s, _, id := taskFixture(t)
	p := newPlatformFixture(t, 5)
	O(A(p.snapshot["models"])[0])["inputModalities"] = []any{"TEXT", "IMAGE"}
	p.enroll()
	mustUpdate(t, m, id, M{"modelId": p.models.Catalog()[0]["modelId"]})
	return &Files{Manager: m, Models: p.models}, m, s, id, p
}
func TestFilesUploadPreparationAndBounds(t *testing.T) {
	f, m, s, id, _ := fileFixture(t)
	task := mustTask(t, m, id)
	for _, data := range []M{uploadData("bad.png", []byte("invalid")), {"name": "bad.txt", "base64": "%%"}, uploadData("invalid.png", []byte{137, 80, 78, 71, 13, 10, 26, 10})} {
		_, e := f.Upload(task, data)
		assertStatus(t, e, 400)
	}
	empty, e := f.Upload(task, uploadData("empty.txt", nil))
	if e != nil || I(empty["size"]) != 0 {
		t.Fatal(e)
	}
	ordinary, e := f.Upload(task, uploadData("notes.txt", []byte("PRIVATE_DOCUMENT_CONTENT")))
	if e != nil {
		t.Fatal(e)
	}
	pic, e := f.Upload(task, uploadData("evidence.png", testPNG(t, 32, 24, false)))
	if e != nil {
		t.Fatal(e)
	}
	task = mustTask(t, m, id)
	if I(task["logicalCount"]) != 0 || len(A(task["uploads"])) != 3 {
		t.Fatal(task)
	}
	stored, _ := s.Task(id)
	if len(A(stored["uploads"])) != 3 {
		t.Fatal("upload not persisted")
	}
	native, blocks, e := f.Prepare(task, "inspect", []string{S(pic["id"]), S(ordinary["id"])})
	if e != nil {
		t.Fatal(e)
	}
	if len(blocks) != 2 || len(A(native["multi_content"])) != 2 || strings.Contains(string(JSON(native)), "PRIVATE_DOCUMENT_CONTENT") || strings.Count(S(native["content"]), "User attachments") != 1 || !strings.Contains(S(native["content"]), "No remote download is needed") {
		t.Fatal(native)
	}
	encoded := S(O(O(A(native["multi_content"])[1])["document"])["source"].(M)["inline_data"])
	raw, _ := base64.StdEncoding.DecodeString(encoded)
	config, _, e := image.DecodeConfig(bytes.NewReader(raw))
	if e != nil || config.Width != 32 || config.Height != 24 {
		t.Fatal(config, e)
	}
	for _, ids := range [][]string{{"foreign"}, {S(pic["id"]), S(pic["id"])}, {"1", "2", "3", "4", "5", "6", "7", "8", "9"}} {
		if _, e = f.attachments(task, ids); e == nil {
			t.Fatal(ids)
		}
	}
	task["modelId"] = "missing"
	_, e = f.attachments(task, []string{S(pic["id"])})
	assertStatus(t, e, 409)
	task = mustTask(t, m, id)
	if e = os.WriteFile(filepath.Join(S(task["workspace"]), S(pic["path"])), []byte("changed"), 0600); e != nil {
		t.Fatal(e)
	}
	_, _, e = f.Prepare(task, "", []string{S(pic["id"])})
	assertStatus(t, e, 400)
	for _, patch := range []M{{"archived": true}, {"parentTaskId": "parent"}} {
		readonly := Merge(task, patch)
		_, e = f.Upload(readonly, uploadData("x.txt", nil))
		assertStatus(t, e, 409)
	}
	_, e = f.Upload(task, uploadData("large.bin", make([]byte, 8<<20)))
	if e != nil {
		t.Fatal(e)
	}
	_, e = f.Upload(task, M{"name": "oversized.bin", "base64": strings.Repeat("A", ((MaxUploadBytes+2)/3)*4+4)})
	assertStatus(t, e, 413)
}
func TestFilesListingEditingRangeAndContainment(t *testing.T) {
	f, m, _, id, _ := fileFixture(t)
	task := mustTask(t, m, id)
	root := S(task["workspace"])
	for i := 0; i < 550; i++ {
		dir := filepath.Join(root, fmt.Sprint(i%3))
		os.MkdirAll(dir, 0700)
		if e := os.WriteFile(filepath.Join(dir, fmt.Sprint(i)+".txt"), []byte("abc"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	files, e := f.List(task)
	if e != nil || len(files) != 500 {
		t.Fatal(len(files), e)
	}
	path := filepath.Join(root, "editable.txt")
	os.WriteFile(path, []byte("old"), 0600)
	if e = f.Save(task, M{"path": "editable.txt", "content": "new bytes"}); e != nil {
		t.Fatal(e)
	}
	r := httptest.NewRequest("GET", "http://localhost/?path=editable.txt", nil)
	r.Header.Set("Range", "bytes=1-3")
	w := httptest.NewRecorder()
	if e = f.Serve(w, r, task); e != nil || w.Code != 206 || w.Body.String() != "ew " {
		t.Fatal(e, w.Code, w.Body.String())
	}
	for _, p := range []string{"../outside.txt", filepath.Join(t.TempDir(), "outside.txt")} {
		if f.Save(task, M{"path": p, "content": "bad"}) == nil {
			t.Fatal("escape", p)
		}
	}
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0600)
	if e = os.Symlink(outside, filepath.Join(root, "link")); e == nil {
		if f.Save(task, M{"path": "link/secret.txt", "content": "bad"}) == nil {
			t.Fatal("symlink escape")
		}
		data, _ := os.ReadFile(filepath.Join(outside, "secret.txt"))
		if string(data) != "secret" {
			t.Fatal("outside changed")
		}
	}
	big, e := os.Create(filepath.Join(root, "big.txt"))
	if e != nil {
		t.Fatal(e)
	}
	big.Truncate((2 << 20) + 1)
	big.Close()
	e = f.Serve(httptest.NewRecorder(), httptest.NewRequest("GET", "http://localhost/?path=big.txt&preview=1", nil), task)
	assertStatus(t, e, 413)
}
func TestImageNormalizationAndAnimation(t *testing.T) {
	animated, e := os.ReadFile(filepath.Join("..", "..", "tests", "media", "animated.webp"))
	if e != nil {
		t.Fatal(e)
	}
	prepared, kind, e := PrepareModelImage(animated)
	if e != nil || kind != "image/webp" || !bytes.Equal(animated, prepared) {
		t.Fatal("animated WebP", kind, e)
	}
	raw := testPNG(t, 2500, 100, false)
	out, mime, e := PrepareModelImage(raw)
	if e != nil {
		t.Fatal(e)
	}
	cfg, _, e := image.DecodeConfig(bytes.NewReader(out))
	if e != nil || cfg.Width != 2048 || cfg.Height > 100 || mime != "image/jpeg" {
		t.Fatal(cfg, mime, e)
	}
	raw = testPNG(t, 32, 24, true)
	out, mime, e = PrepareModelImage(raw)
	if e != nil || mime != "image/png" || len(out) == 0 {
		t.Fatal(mime, e)
	}
	palette := color.Palette{color.Black, color.White}
	a := image.NewPaletted(image.Rect(0, 0, 4, 4), palette)
	b := image.NewPaletted(image.Rect(0, 0, 4, 4), palette)
	b.SetColorIndex(1, 1, 1)
	var buf bytes.Buffer
	if e = gif.EncodeAll(&buf, &gif.GIF{Image: []*image.Paletted{a, b}, Delay: []int{10, 10}}); e != nil {
		t.Fatal(e)
	}
	out, mime, e = PrepareModelImage(buf.Bytes())
	if e != nil || mime != "image/gif" || !bytes.Equal(out, buf.Bytes()) {
		t.Fatal(mime, e)
	}
	if _, _, e = PrepareModelImage(buf.Bytes()[:buf.Len()-3]); e == nil {
		t.Fatal("truncated animation accepted")
	}
}
func deliveryFixture(t *testing.T) (*Deliveries, *toolTurn, *Manager, *Store, *platformFixture) {
	t.Helper()
	files, m, s, id, p := fileFixture(t)
	task := mustUpdate(t, m, id, M{"turnId": UUID()})
	d, e := NewDeliveries(m, s, p.models, filepath.Join(t.TempDir(), "deliveries"))
	if e != nil {
		t.Fatal(e)
	}
	l := NewLocalTools(p.models, m, files, d)
	d.Local = l
	t.Cleanup(l.Close)
	ctx, cancel := context.WithCancel(context.Background())
	turn := &toolTurn{Key: UUID(), TaskID: id, TurnID: S(task["turnId"]), Workspace: S(task["workspace"]), Task: task, ctx: ctx, cancel: cancel}
	turn.workspaceInfo, e = os.Stat(turn.Workspace)
	if e != nil {
		t.Fatal(e)
	}
	l.turns[turn.Key] = turn
	return d, turn, m, s, p
}
func deliveryOK(t *testing.T, d *Deliveries, turn *toolTurn, summary string, files []any) M {
	t.Helper()
	r := d.Submit(context.Background(), turn, M{"summary": summary, "files": files})
	if r["status"] != "completed" {
		t.Fatal(r)
	}
	return r
}
func TestDeliveryVersionsRetentionSnapshotsAndRecovery(t *testing.T) {
	d, turn, m, s, p := deliveryFixture(t)
	file := filepath.Join(turn.Workspace, "result.txt")
	os.WriteFile(file, []byte("first"), 0600)
	args := []any{M{"path": "result.txt"}}
	first := deliveryOK(t, d, turn, "一份交付", args)
	os.WriteFile(file, []byte("second"), 0600)
	second := deliveryOK(t, d, turn, "二份交付", args)
	if I(first["version"]) != 1 || I(second["version"]) != 2 {
		t.Fatal(first, second)
	}
	stored, _ := s.Delivery(turn.TaskID, "1")
	item := O(A(stored["files"])[0])
	r := httptest.NewRequest("GET", "http://localhost/", nil)
	r.Header.Set("Range", "bytes=0-2")
	w := httptest.NewRecorder()
	if e := d.ServeFile(w, r, turn.TaskID, "1", S(item["id"])); e != nil || w.Code != 206 || w.Body.String() != "fir" {
		t.Fatal(e, w.Code, w.Body.String())
	}
	c := p.models.Config()
	O(c["workbench"])["deliveryRetentionCount"] = 1
	for _, a := range A(O(c["workbench"])["assistants"]) {
		delete(O(a), "deliveryRetentionCount")
	}
	if e := p.models.SetConfig(c); e != nil {
		t.Fatal(e)
	}
	third := deliveryOK(t, d, turn, "三份交付", nil)
	if I(third["version"]) != 3 {
		t.Fatal(third)
	}
	all, _ := s.Deliveries(turn.TaskID, 0, 20)
	if len(all) != 1 {
		t.Fatal(all)
	}
	orphan := ID()
	staging := ID()
	os.MkdirAll(filepath.Join(d.Root, "snapshots", turn.TaskID, orphan), 0700)
	os.MkdirAll(filepath.Join(d.Root, ".staging", staging), 0700)
	os.MkdirAll(filepath.Join(d.Root, ".staging", "keep-unrelated"), 0700)
	if e := d.recover(); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(filepath.Join(d.Root, "snapshots", turn.TaskID, orphan)); !os.IsNotExist(e) {
		t.Fatal("orphan retained")
	}
	if _, e := os.Stat(filepath.Join(d.Root, ".staging", "keep-unrelated")); e != nil {
		t.Fatal("unrelated removed")
	}
	if I(O(mustTask(t, m, turn.TaskID)["latestDelivery"])["version"]) != 3 {
		t.Fatal("latest not recovered")
	}
}
func TestDeliveryValidationCancellationAndLimits(t *testing.T) {
	d, turn, m, s, _ := deliveryFixture(t)
	for _, args := range []M{{"summary": ""}, {"summary": strings.Repeat("字", 1001)}, {"summary": "ok", "files": make([]any, 11)}, {"summary": "ok", "files": []any{M{"path": "missing"}}}, {"summary": "ok", "files": []any{M{"path": "../outside"}}}} {
		if d.Submit(context.Background(), turn, args)["status"] != "failed" {
			t.Fatal(args)
		}
	}
	large, e := os.Create(filepath.Join(turn.Workspace, "large.bin"))
	if e != nil {
		t.Fatal(e)
	}
	large.Truncate(MaxDeliveryFile + 1)
	large.Close()
	r := d.Submit(context.Background(), turn, M{"summary": "too big", "files": []any{M{"path": "large.bin"}}})
	if r["code"] != "file_too_large" {
		t.Fatal(r)
	}
	os.Truncate(filepath.Join(turn.Workspace, "large.bin"), MaxDeliveryFile)
	files := []any{}
	for i := 0; i < 5; i++ {
		name := fmt.Sprint(i) + ".bin"
		f, e := os.Create(filepath.Join(turn.Workspace, name))
		if e != nil {
			t.Fatal(e)
		}
		f.Truncate(MaxDeliveryFile)
		f.Close()
		files = append(files, M{"path": name})
	}
	r = d.Submit(context.Background(), turn, M{"summary": "too many bytes", "files": files})
	if r["code"] != "delivery_too_large" {
		t.Fatal(r)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if d.Submit(ctx, turn, M{"summary": "cancelled"})["status"] != "failed" {
		t.Fatal("canceled committed")
	}
	all, _ := s.Deliveries(turn.TaskID, 0, 20)
	if len(all) != 0 {
		t.Fatal(all)
	}
	forged := &toolTurn{Key: "forged", TaskID: turn.TaskID, TurnID: turn.TurnID, Workspace: turn.Workspace, Task: Clone(turn.Task), ctx: turn.ctx, cancel: turn.cancel}
	if d.Submit(context.Background(), forged, M{"summary": "forged"})["code"] != "tool_not_permitted" {
		t.Fatal("forged turn")
	}
	mustUpdate(t, m, turn.TaskID, M{"turnId": UUID()})
	if d.Submit(context.Background(), turn, M{"summary": "expired"})["code"] != "tool_not_permitted" {
		t.Fatal("expired turn")
	}
}
func TestDeliveryConcurrentIdentityAndCommitFailure(t *testing.T) {
	d, turn, m, s, _ := deliveryFixture(t)
	second, e := m.Create(context.Background(), M{})
	if e != nil {
		t.Fatal(e)
	}
	id := S(second["id"])
	second = mustUpdate(t, m, id, M{"turnId": UUID()})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	other := &toolTurn{Key: UUID(), TaskID: id, TurnID: S(second["turnId"]), Workspace: S(second["workspace"]), Task: second, ctx: ctx, cancel: cancel}
	other.workspaceInfo, e = os.Stat(other.Workspace)
	if e != nil {
		t.Fatal(e)
	}
	d.Local.turns[other.Key] = other
	var wg sync.WaitGroup
	results := make(chan M, 2)
	for _, tr := range []*toolTurn{turn, other} {
		wg.Add(1)
		go func() { defer wg.Done(); results <- d.Submit(context.Background(), tr, M{"summary": tr.TaskID}) }()
	}
	wg.Wait()
	close(results)
	for r := range results {
		if r["status"] != "completed" || I(r["version"]) != 1 {
			t.Fatal(r)
		}
	}
	if _, e = s.DB.Exec("CREATE TRIGGER reject_delivery BEFORE INSERT ON task_deliveries BEGIN SELECT RAISE(FAIL,'fixture storage failure'); END"); e != nil {
		t.Fatal(e)
	}
	r := d.Submit(context.Background(), turn, M{"summary": "must not commit"})
	if r["code"] != "persistence_failed" {
		t.Fatal(r)
	}
	all, _ := s.Deliveries(turn.TaskID, 0, 20)
	if len(all) != 1 {
		t.Fatal(all)
	}
	entries, _ := os.ReadDir(filepath.Join(d.Root, "snapshots", turn.TaskID))
	if len(entries) != 1 {
		t.Fatal("failed snapshot retained")
	}
}
func TestOfficialAttachmentLargeImage(t *testing.T) {
	f := newPlatformFixture(t, 5)
	O(A(f.snapshot["models"])[0])["inputModalities"] = []any{"TEXT", "IMAGE"}
	f.enroll()
	app := f.startApp()
	task, e := app.Manager.Create(context.Background(), M{"modelId": f.models.Catalog()[0]["modelId"], "approvalMode": "auto"})
	if e != nil {
		t.Fatal(e)
	}
	pic, e := app.Files.Upload(task, uploadData("large.png", testPNG(t, 2048, 1024, true)))
	if e != nil {
		t.Fatal(e)
	}
	task = mustTask(t, app.Manager, S(task["id"]))
	native, _, e := app.Files.Prepare(task, "LARGE_IMAGE_PROOF", []string{S(pic["id"])})
	if e != nil || len(JSON(native)) <= 10<<20 {
		t.Fatal("large image preparation", len(JSON(native)), e)
	}
	if _, e = app.Manager.Submit(S(task["id"]), "LARGE_IMAGE_PROOF", []string{S(pic["id"])}, nil, nil); e != nil {
		t.Fatal(e)
	}
	settled := waitIdle(t, app.Manager, S(task["id"]))
	if settled["lastTurnReason"] != "completed" {
		t.Fatal(settled["lastFailure"])
	}
	found := false
	for _, r := range f.requestList() {
		if strings.HasPrefix(S(r["path"]), "/runtime/") && len(JSON(r["body"])) > 10<<20 {
			found = true
		}
	}
	if !found {
		t.Fatal("large body did not reach provider")
	}
}
func TestDeliveryTwoGiBStreaming(t *testing.T) {
	d, turn, _, _, _ := deliveryFixture(t)
	free, e := availableBytes(d.Root)
	if e != nil || free < 5<<30 {
		t.Fatal("2 GiB streaming acceptance requires 5 GiB free in the test temp directory", free, e)
	}
	files := []any{}
	for i := 0; i < 4; i++ {
		name := fmt.Sprint(i) + ".bin"
		f, e := os.Create(filepath.Join(turn.Workspace, name))
		if e != nil {
			t.Fatal(e)
		}
		if e = f.Truncate(MaxDeliveryFile); e != nil {
			t.Fatal(e)
		}
		f.Close()
		files = append(files, M{"path": name})
	}
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	baselineRSS, e := processRSS()
	if e != nil {
		t.Fatal(e)
	}
	var peakRSS atomic.Uint64
	peakRSS.Store(baselineRSS)
	samplingDone := make(chan struct{})
	sampled := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			rss, e := processRSS()
			if e != nil {
				sampled <- e
				return
			}
			if rss > peakRSS.Load() {
				peakRSS.Store(rss)
			}
			select {
			case <-samplingDone:
				sampled <- nil
				return
			case <-ticker.C:
			}
		}
	}()
	result := deliveryOK(t, d, turn, "exactly 2 GiB", files)
	close(samplingDone)
	if e := <-sampled; e != nil {
		t.Fatal(e)
	}
	growth := peakRSS.Load() - baselineRSS
	t.Logf("service process RSS baseline %.2f MiB, sampled peak %.2f MiB, growth %.2f MiB", float64(baselineRSS)/(1<<20), float64(peakRSS.Load())/(1<<20), float64(growth)/(1<<20))
	if growth >= 256<<20 {
		t.Fatal("RSS growth exceeded 256 MiB", growth)
	}
	runtime.ReadMemStats(&after)
	heapGrowth := int64(after.HeapInuse) - int64(before.HeapInuse)
	if int64(N(result["total_bytes"])) != MaxDeliveryBytes || heapGrowth > 256<<20 {
		t.Fatal(result, "heap growth", heapGrowth)
	}
	stored, _ := d.Store.Delivery(turn.TaskID, "1")
	var total int64
	for _, v := range A(stored["files"]) {
		item := O(v)
		f, e := os.Open(filepath.Join(d.Root, "snapshots", turn.TaskID, S(stored["id"]), "files", S(item["id"])))
		if e != nil {
			t.Fatal(e)
		}
		n, e := io.Copy(io.Discard, f)
		f.Close()
		if e != nil || n != MaxDeliveryFile {
			t.Fatal(n, e)
		}
		total += n
	}
	if total != MaxDeliveryBytes {
		t.Fatal(total)
	}
}
