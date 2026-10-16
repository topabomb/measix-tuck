package tuck

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestParityDAVBatchReplacementAndPreflight(t *testing.T) {
	f := newWorkspaceFixture(t)
	l, tr := f.turn(t)
	dest := filepath.Join(tr.Workspace, "batch")
	os.Mkdir(dest, 0700)
	for _, name := range []string{"hello.txt", "second.txt", "untouched.txt"} {
		os.WriteFile(filepath.Join(dest, name), []byte("old"), 0600)
	}
	args := M{"remote_paths": []any{"hello.txt", "second.txt"}, "local_directory": "batch"}
	if r := l.Transfer(context.Background(), tr, "webdav_download_files", args); r["reason"] != "local_file_exists" {
		t.Fatal(r)
	}
	f.mu.Lock()
	for _, c := range f.calls {
		if c["method"] == "GET" && c["path"] != "" {
			t.Fatal("download before full preflight", c)
		}
	}
	f.mu.Unlock()
	args["overwrite"] = true
	if r := l.Transfer(context.Background(), tr, "webdav_download_files", args); r["status"] != "completed" || I(r["completed_count"]) != 2 {
		t.Fatal(r)
	}
	for name, want := range map[string]string{"hello.txt": "hello world", "second.txt": "second", "untouched.txt": "old"} {
		raw, _ := os.ReadFile(filepath.Join(dest, name))
		if string(raw) != want {
			t.Fatal(name, string(raw))
		}
	}
	r := l.Transfer(context.Background(), tr, "webdav_download_files", M{"remote_paths": []any{"folder"}, "local_directory": "directory-source"})
	if r["status"] != "failed" || O(A(r["files"])[0])["reason"] != "webdav_not_file" {
		t.Fatal(r)
	}
	if e := os.Symlink(filepath.Join(dest, "untouched.txt"), filepath.Join(dest, "alias.txt")); e == nil {
		f.mu.Lock()
		f.files["alias.txt"] = []byte("replacement")
		f.calls = nil
		f.mu.Unlock()
		r = l.Transfer(context.Background(), tr, "webdav_download_files", M{"remote_paths": []any{"hello.txt", "alias.txt"}, "local_directory": "batch", "overwrite": true})
		if r["reason"] != "local_file_exists" {
			t.Fatal(r)
		}
		f.mu.Lock()
		for _, c := range f.calls {
			if c["method"] == "GET" && c["path"] != "" {
				t.Fatal("symlink preflight downloaded", c)
			}
		}
		f.mu.Unlock()
	}
}

func TestParityDAVBrokenStreamAndFreshCancellation(t *testing.T) {
	f := newWorkspaceFixture(t)
	l, tr := f.turn(t)
	os.Mkdir(filepath.Join(tr.Workspace, "out"), 0700)
	os.WriteFile(filepath.Join(tr.Workspace, "out/second.txt"), []byte("keep old"), 0600)
	f.setDAVHook(func(w http.ResponseWriter, r *http.Request, name string) bool {
		if r.Method == "GET" && name == "second.txt" {
			w.Header().Set("Content-Length", "1000")
			w.Write([]byte("broken"))
			return true
		}
		return false
	})
	r := l.Transfer(context.Background(), tr, "webdav_download_files", M{"remote_paths": []any{"hello.txt", "second.txt"}, "local_directory": "out", "overwrite": true})
	if r["status"] != "partial" {
		t.Fatal(r)
	}
	for name, want := range map[string]string{"hello.txt": "hello world", "second.txt": "keep old"} {
		raw, _ := os.ReadFile(filepath.Join(tr.Workspace, "out", name))
		if string(raw) != want {
			t.Fatal(name, string(raw))
		}
	}
	entries, _ := os.ReadDir(filepath.Join(tr.Workspace, "out"))
	if len(entries) != 2 {
		t.Fatal("temporary file retained", entries)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.setDAVHook(func(w http.ResponseWriter, r *http.Request, name string) bool {
		if r.Method == "GET" && name == "second.txt" {
			w.Header().Set("Content-Length", "1000")
			w.Write([]byte("partial"))
			w.(http.Flusher).Flush()
			cancel()
			return true
		}
		return false
	})
	r = l.Transfer(ctx, tr, "webdav_download_files", M{"remote_paths": []any{"second.txt"}, "local_directory": "fresh"})
	if r["status"] != "failed" {
		t.Fatal(r)
	}
	entries, _ = os.ReadDir(filepath.Join(tr.Workspace, "fresh"))
	if len(entries) != 0 {
		t.Fatal("partial published", entries)
	}
}

func TestParityDAVBooleansEmptyImageAndReservedPaths(t *testing.T) {
	f := newWorkspaceFixture(t)
	l, tr := f.turn(t)
	os.WriteFile(filepath.Join(tr.Workspace, "empty.txt"), nil, 0600)
	for i, flag := range []any{nil, nil, false, " fALse ", true, " TrUE "} {
		remote := fmt.Sprintf("empty-%d.txt", i)
		args := M{"file_path": "empty.txt", "remote_path": remote, "create_parents": nil, "timeout_secs": nil}
		if i > 0 {
			args["overwrite"] = flag
		}
		r := l.Transfer(context.Background(), tr, "webdav_upload", args)
		if r["status"] != "completed" || I(r["bytes"]) != 0 {
			t.Fatal(r)
		}
		r = l.Transfer(context.Background(), tr, "webdav_upload", args)
		overwrite, _ := overwriteFlag(flag)
		if (r["status"] == "completed") != overwrite {
			t.Fatal(flag, r)
		}
	}
	image := testPNG(t, 7, 5, true)
	f.mu.Lock()
	f.files["image.png"] = image
	f.mu.Unlock()
	r := l.Transfer(context.Background(), tr, "webdav_download_files", M{"remote_paths": []any{"empty-0.txt", "image.png"}, "local_directory": `native\nested`})
	if r["status"] != "completed" || len(JSON(r)) > 5000 || strings.Contains(string(JSON(r)), "base64") {
		t.Fatal(r)
	}
	raw, _ := os.ReadFile(filepath.Join(tr.Workspace, "native/nested/image.png"))
	empty, _ := os.Stat(filepath.Join(tr.Workspace, "native/nested/empty-0.txt"))
	if !bytes.Equal(raw, image) || empty.Size() != 0 {
		t.Fatal("bytes changed")
	}
	f.mu.Lock()
	f.calls = nil
	f.mu.Unlock()
	for _, name := range []string{".agent-space/file", "project/.agent-space-private/file"} {
		for _, in := range []struct {
			tool string
			args M
		}{{"webdav_upload", M{"file_path": "empty.txt", "remote_path": name}}, {"webdav_download_files", M{"remote_paths": []any{name}, "local_directory": "reserved-output"}}} {
			r = l.Transfer(context.Background(), tr, in.tool, in.args)
			if r["code"] != "webdav_path_not_permitted" || !strings.Contains(S(r["message"]), "ordinary project") {
				t.Fatal(r)
			}
		}
	}
	f.mu.Lock()
	for _, c := range f.calls {
		if strings.HasPrefix(S(c["path"]), f.davRoot) || c["method"] == "PROPFIND" || c["method"] == "PUT" || c["method"] == "MKCOL" || c["method"] == "GET" && S(c["path"]) != "" {
			t.Fatal("reserved path reached DAV", c)
		}
	}
	f.mu.Unlock()
	f.mu.Lock()
	f.projection["filesAvailable"] = false
	f.mu.Unlock()
	if result := l.Transfer(context.Background(), tr, "webdav_download_files", M{"remote_paths": []any{"image.png"}, "local_directory": "revoked"}); result["reason"] != "tool_not_permitted" {
		t.Fatal(result)
	}
	if _, e := os.Stat(filepath.Join(tr.Workspace, "reserved-output")); !os.IsNotExist(e) {
		t.Fatal("reserved created output")
	}
}

func TestParityDAVStrongETagAndFailureKinds(t *testing.T) {
	f := newWorkspaceFixture(t)
	l, tr := f.turn(t)
	os.WriteFile(filepath.Join(tr.Workspace, "source.txt"), []byte("new"), 0600)
	var weakHead, concurrent atomic.Bool
	f.setDAVHook(func(w http.ResponseWriter, r *http.Request, name string) bool {
		if name == "hello.txt" && r.Method == "PROPFIND" {
			w.WriteHeader(207)
			fmt.Fprintf(w, `<d:multistatus xmlns:d="DAV:"><d:response><d:href>%shello.txt</d:href><d:propstat><d:prop><d:resourcetype/><d:getetag>W/"weak"</d:getetag></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response></d:multistatus>`, f.davRoot)
			return true
		}
		if name == "hello.txt" && r.Method == "HEAD" && weakHead.Load() {
			w.Header().Set("ETag", `W/"weak"`)
			w.WriteHeader(200)
			return true
		}
		if name == "hello.txt" && r.Method == "PUT" && concurrent.Load() {
			if !strongETag(r.Header.Get("If-Match")) {
				t.Error("unconditional overwrite")
			}
			io.Copy(io.Discard, r.Body)
			w.WriteHeader(412)
			return true
		}
		return false
	})
	args := M{"file_path": "source.txt", "remote_path": "hello.txt", "overwrite": true}
	if r := l.Transfer(context.Background(), tr, "webdav_upload", args); r["status"] != "completed" {
		t.Fatal("HEAD fallback", r)
	}
	weakHead.Store(true)
	if r := l.Transfer(context.Background(), tr, "webdav_upload", args); r["code"] != "webdav_version_unavailable" {
		t.Fatal(r)
	}
	weakHead.Store(false)
	concurrent.Store(true)
	if r := l.Transfer(context.Background(), tr, "webdav_upload", args); r["code"] != "webdav_file_exists" {
		t.Fatal(r)
	}
	f.mu.Lock()
	if string(f.files["hello.txt"]) != "new" {
		t.Fatal("race overwrote remote")
	}
	f.mu.Unlock()
	f.mu.Lock()
	O(f.projection["webdav"])["accessMode"] = "READ_ONLY"
	f.mu.Unlock()
	tr.Target["mode"] = "READ_ONLY"
	if r := l.Transfer(context.Background(), tr, "webdav_upload", args); r["code"] != "webdav_read_only" {
		t.Fatal(r)
	}
	f.mu.Lock()
	O(f.projection["webdav"])["accessMode"] = "FULL"
	f.mu.Unlock()
	tr.Target["mode"] = "FULL"
	for _, in := range []M{{"file_path": "source.txt", "remote_path": "folder", "overwrite": true}, {"file_path": "missing", "remote_path": "new.txt"}, {"file_path": "../escape", "remote_path": "new.txt"}} {
		if r := l.Transfer(context.Background(), tr, "webdav_upload", in); r["status"] != "failed" {
			t.Fatal(r)
		}
	}
	f.setDAVHook(func(w http.ResponseWriter, r *http.Request, name string) bool {
		if r.Method == "PROPFIND" {
			w.WriteHeader(403)
			return true
		}
		return false
	})
	if r := l.Transfer(context.Background(), tr, "webdav_upload", args); r["reason"] != "tool_not_permitted" {
		t.Fatal(r)
	}
	f.setDAVHook(nil)
	f.mu.Lock()
	f.calls = nil
	f.mu.Unlock()
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "secret"), []byte("secret"), 0600)
	if e := os.Symlink(outside, filepath.Join(tr.Workspace, "escape")); e == nil {
		if r := l.Transfer(context.Background(), tr, "webdav_upload", M{"file_path": "escape/secret", "remote_path": "new.txt"}); r["status"] != "failed" {
			t.Fatal("source escape accepted", r)
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		if _, exists := f.files["new.txt"]; exists {
			t.Fatal("outside source published remotely")
		}
		for _, call := range f.calls {
			if call["method"] == "PUT" || call["method"] == "MKCOL" {
				t.Fatal("outside source reached a remote write", call)
			}
		}
	}
}

func (f *workspaceFixture) setDAVHook(h func(http.ResponseWriter, *http.Request, string) bool) {
	f.mu.Lock()
	f.hook = h
	f.mu.Unlock()
}
