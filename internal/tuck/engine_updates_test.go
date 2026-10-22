package tuck

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type testTransport func(*http.Request) (*http.Response, error)

func (f testTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func updateClient(t *testing.T, binary string, corrupt bool) *http.Client {
	t.Helper()
	hash, e := FileHash(binary)
	if e != nil {
		t.Fatal(e)
	}
	info, e := os.Stat(binary)
	if e != nil {
		t.Fatal(e)
	}
	name := "docker-agent-" + runtime.GOOS + "-" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	release := M{"tag_name": "v1.152.0", "draft": false, "prerelease": false, "assets": []any{M{"name": name, "size": info.Size(), "digest": "sha256:" + hash, "browser_download_url": "https://github.com/docker/docker-agent/releases/download/v1.152.0/" + name}}}
	return &http.Client{Transport: testTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "api.github.com" {
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(bytes.NewReader(JSON(release))), Request: r}, nil
		}
		file, e := os.Open(binary)
		if e != nil {
			return nil, e
		}
		if corrupt {
			file.Close()
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("corrupt")), Header: http.Header{}, Request: r}, nil
		}
		return &http.Response{StatusCode: 200, Body: file, ContentLength: info.Size(), Header: http.Header{}, Request: r}, nil
	})}
}
func managedTestUpdate(t *testing.T) (*App, *platformFixture) {
	t.Helper()
	f := newPlatformFixture(t, 5)
	f.enroll()
	a := f.startApp()
	c, rev, _ := a.Config.Read()
	O(c["engine"])["binary"] = ""
	if _, e := a.Config.Save(rev, c); e != nil {
		t.Fatal(e)
	}
	a.Config.MarkApplied()
	if e := a.Models.SetConfig(c); e != nil {
		t.Fatal(e)
	}
	a.Engine.mu.Lock()
	a.Engine.version = ""
	a.Engine.mu.Unlock()
	a.Updates.Client = updateClient(t, officialTestBinary(t), false)
	return a, f
}
func TestEngineOfficialArtifactUpdateAndPendingSelection(t *testing.T) {
	a, _ := managedTestUpdate(t)
	u := a.Updates
	ctx := context.Background()
	if e := u.Check(ctx); e != nil {
		t.Fatal(e)
	}
	if e := u.Download(ctx, "v1.152.0"); e != nil {
		t.Fatal(e)
	}
	if e := u.Verify("v1.152.0"); e != nil {
		t.Fatal(e)
	}
	current, revision, _ := a.Config.Read()
	O(current["workbench"])["requireDelivery"] = true
	if _, e := a.Config.Save(revision, current); e != nil {
		t.Fatal(e)
	}
	if !a.Config.Pending() {
		t.Fatal("pending settings missing")
	}
	if e := u.Apply(ctx, "v1.152.0", func() error { return nil }); e != nil {
		t.Fatal(e)
	}
	saved, _, _ := a.Config.Read()
	if O(saved["engine"])["version"] != "v1.152.0" || !a.Config.Pending() || B(O(a.Models.Config()["workbench"])["requireDelivery"]) {
		t.Fatal("update applied unrelated pending settings", saved)
	}
	if a.Engine.State()["version"] != "v1.152.0" || !strings.Contains(a.Engine.Options().Binary, filepath.Join("engines", "v1.152.0")) {
		t.Fatal(a.Engine.State(), a.Engine.Options())
	}
}
func TestEngineCorruptionAuthorizationAndStartupRollback(t *testing.T) {
	a, f := managedTestUpdate(t)
	task, e := a.Manager.Create(context.Background(), M{"modelId": f.models.Catalog()[0]["modelId"], "approvalMode": "auto"})
	if e != nil {
		t.Fatal(e)
	}
	runAppTask(t, a, S(task["id"]), "durable history before update")
	nativeID := S(task["nativeSessionId"])
	u := a.Updates
	u.Client = updateClient(t, officialTestBinary(t), true)
	if e = u.Check(context.Background()); e != nil {
		t.Fatal(e)
	}
	if e = u.Download(context.Background(), "v1.152.0"); e == nil {
		t.Fatal("corruption accepted")
	}
	if a.Engine.State()["state"] != "ready" {
		t.Fatal("corrupt download stopped engine")
	}
	directory := t.TempDir()
	invalid := filepath.Join(directory, ExecutableName())
	os.WriteFile(invalid, []byte("not an executable"), 0700)
	hash, _ := FileHash(invalid)
	u.mu.Lock()
	u.path = invalid
	u.downloaded = &EngineRelease{Version: "v1.150.1", SHA256: hash}
	u.mu.Unlock()
	previous := a.Engine.Options()
	before, _ := os.ReadFile(a.Config.Path)
	e = u.Apply(context.Background(), "v1.150.1", func() error { return Problem(403, "settings_locked", "locked") })
	if e == nil || a.Engine.Options().Binary != previous.Binary || a.Engine.State()["state"] != "ready" {
		t.Fatal("unauthorized apply stopped engine", e)
	}
	e = u.Apply(context.Background(), "v1.150.1", func() error { return nil })
	if e == nil || a.Engine.Options().Binary != previous.Binary || a.Engine.State()["state"] != "ready" {
		t.Fatal("startup rollback failed", e, a.Engine.State())
	}
	after, _ := os.ReadFile(a.Config.Path)
	if !bytes.Equal(before, after) {
		t.Fatal("failed update saved config")
	}
	history, e := a.Engine.Request(context.Background(), "GET", "/api/sessions/"+nativeID, nil)
	if e != nil || history == nil {
		t.Fatal("opaque native history not restored", e)
	}
}
func TestEngineUpdateConcurrencyExternalAndCancellation(t *testing.T) {
	a, _ := managedTestUpdate(t)
	u := a.Updates
	started := make(chan struct{})
	u.Client = &http.Client{Transport: testTransport(func(r *http.Request) (*http.Response, error) {
		close(started)
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	done := make(chan error, 1)
	go func() { done <- u.Check(context.Background()) }()
	<-started
	if e := u.Check(context.Background()); Status(e) != 409 {
		t.Fatal("concurrent operation accepted", e)
	}
	u.Close()
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("closing check succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("check was not cancelled")
	}
	a.Engine.mu.Lock()
	a.Engine.options.External = true
	a.Engine.mu.Unlock()
	if e := u.Download(context.Background(), "v1.152.0"); Code(e) != "external_engine" {
		t.Fatal(e)
	}
	for _, bad := range []EngineRelease{{Version: "v1.152.0", URL: "http://github.com/docker/docker-agent/releases/download/v1.152.0/x", SHA256: strings.Repeat("0", 64)}, {Version: "v1.152.0", URL: "https://attacker.invalid/x", SHA256: strings.Repeat("0", 64)}} {
		if e := DownloadEngine(context.Background(), http.DefaultClient, bad, filepath.Join(t.TempDir(), "binary")); e == nil {
			t.Fatal("nonofficial release accepted")
		}
	}
}
func TestEngineInvalidApplyLeavesRunningTaskAlone(t *testing.T) {
	a, _ := managedTestUpdate(t)
	task, e := a.Manager.Create(context.Background(), M{})
	if e != nil {
		t.Fatal(e)
	}
	mustUpdate(t, a.Manager, S(task["id"]), M{"status": "working", "lastTurnReason": nil})
	status, body, _ := requestApp(t, a, "POST", "/api/settings/engine/apply", M{"version": "v9.9.9", "interrupt": true}, nil)
	if status != 409 || S(body["code"]) != "engine_download_required" {
		t.Fatal(status, body)
	}
	if mustTask(t, a.Manager, S(task["id"]))["status"] != "working" {
		t.Fatal("invalid update interrupted task")
	}
	t.Log(fmt.Sprint(a.Engine.State()["state"]))
}

func TestParityEngineReleaseRequiresDigestBeforeStop(t *testing.T) {
	a, _ := managedTestUpdate(t)
	binary := officialTestBinary(t)
	client := updateClient(t, binary, false)
	original := client.Transport
	client.Transport = testTransport(func(r *http.Request) (*http.Response, error) {
		response, e := original.RoundTrip(r)
		if e != nil {
			return nil, e
		}
		value, e := ReadJSON(response.Body, 4<<20)
		response.Body.Close()
		if e != nil {
			return nil, e
		}
		for _, v := range A(value["assets"]) {
			delete(O(v), "digest")
		}
		response.Body = io.NopCloser(bytes.NewReader(JSON(value)))
		return response, nil
	})
	a.Updates.Client = client
	if e := a.Updates.Check(context.Background()); e == nil {
		t.Fatal("missing SHA accepted")
	}
	if a.Engine.State()["state"] != "ready" {
		t.Fatal("release check stopped native engine")
	}
}
