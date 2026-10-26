package tuck

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestWorkspaceArchiveListsEachSelectedParentOnce(t *testing.T) {
	for _, recursive := range []bool{false, true} {
		t.Run(fmt.Sprint(recursive), func(t *testing.T) {
			f := newWorkspaceFixture(t)
			paths := []string{}
			want := map[string]string{}
			for i := range 10 {
				name := fmt.Sprintf("file-%d.txt", i)
				f.files[name] = []byte(name)
				paths = append(paths, name)
				want["workspace/"+name] = name
			}
			lists := 1
			contentCount := 10
			if recursive {
				f.dirs["other"] = true
				f.dirs["folder/nested"] = true
				f.files["other/a.txt"] = []byte("a")
				f.files["other/b.txt"] = []byte("b")
				f.files["folder/nested/c.txt"] = []byte("c")
				// Interleaved parents, a duplicate, and a descendant selected before
				// its ancestor must not duplicate either requests or ZIP members.
				paths = append([]string{"other/a.txt", "folder/nested/c.txt"}, paths...)
				paths = append(paths, "other/b.txt", "folder", "folder", "file-0.txt")
				want["workspace/other/a.txt"] = "a"
				want["workspace/other/b.txt"] = "b"
				want["workspace/folder/"] = ""
				want["workspace/folder/nested/"] = ""
				want["workspace/folder/nested/c.txt"] = "c"
				lists = 4
				contentCount = 13
			}
			_, server, query := f.proxy(t)
			f.calls = nil
			res, err := http.Get(server.URL + "/api/enterprise-workspace/archive?" + query + "&paths=" + url.QueryEscape(string(JSON(paths))))
			if err != nil {
				t.Fatal(err)
			}
			raw, err := io.ReadAll(res.Body)
			res.Body.Close()
			if err != nil || res.StatusCode != 200 {
				t.Fatal(res.StatusCode, err, string(raw))
			}
			archive, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
			if err != nil {
				t.Fatal(err)
			}
			if len(archive.File) != len(want) {
				t.Fatal("ZIP member count", len(archive.File), len(want))
			}
			for _, file := range archive.File {
				reader, err := file.Open()
				if err != nil {
					t.Fatal(err)
				}
				body, err := io.ReadAll(reader)
				reader.Close()
				expected, ok := want[file.Name]
				if err != nil || !ok || string(body) != expected {
					t.Fatal(file.Name, string(body), err)
				}
				delete(want, file.Name)
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			gotLists, contents := 0, 0
			parents := map[string]bool{}
			for _, call := range f.calls {
				if S(O(call["headers"])["if-match"]) != "" {
					contents++
					continue
				}
				gotLists++
				p := S(call["path"])
				if parents[p] {
					t.Errorf("parent %q listed more than once", p)
				}
				parents[p] = true
			}
			if gotLists != lists || contents != contentCount {
				t.Fatalf("directory requests=%d (want %d), content requests=%d", gotLists, lists, contents)
			}
		})
	}
}

func TestWorkspaceArchiveLimitsAndStreamingMemory(t *testing.T) {
	for _, scenario := range []string{"depth", "entries", "response-size", "stream"} {
		t.Run(scenario, func(t *testing.T) {
			f := newWorkspaceFixture(t)
			_, server, query := f.proxy(t)
			f.mu.Lock()
			f.hook = func(w http.ResponseWriter, r *http.Request, name string) bool {
				if strings.HasSuffix(r.URL.Path, "/files") {
					switch scenario {
					case "depth":
						child := name + "/child"
						if name == "" {
							child = "folder"
						}
						WriteJSON(w, 200, M{"entries": []any{M{"path": child, "kind": "DIRECTORY"}}})
						return true
					case "entries":
						if name == "folder" {
							entries := []any{}
							for i := range 10000 {
								entries = append(entries, M{"path": fmt.Sprintf("folder/%d", i), "kind": "FILE"})
							}
							WriteJSON(w, 200, M{"entries": entries})
							return true
						}
					case "response-size":
						io.WriteString(w, `{"entries":[],"padding":"`+strings.Repeat("x", 4<<20)+`"}`)
						return true
					}
				}
				if scenario == "stream" && strings.HasSuffix(r.URL.Path, "/content") {
					// Generate 64 MiB without retaining the content in either the
					// fixture or the consumer. A whole-file buffer exceeds the budget.
					w.Header().Set("Content-Length", fmt.Sprint(64<<20))
					block := bytes.Repeat([]byte("streaming archive"), 2048)
					for remaining := 64 << 20; remaining > 0; {
						n := min(remaining, len(block))
						if _, err := w.Write(block[:n]); err != nil {
							break
						}
						remaining -= n
					}
					return true
				}
				return false
			}
			f.mu.Unlock()
			paths := `["folder"]`
			if scenario == "stream" {
				paths = `["hello.txt"]`
			}
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			res, err := http.Get(server.URL + "/api/enterprise-workspace/archive?" + query + "&paths=" + url.QueryEscape(paths))
			if err != nil {
				t.Fatal(err)
			}
			_, err = io.Copy(io.Discard, res.Body)
			res.Body.Close()
			runtime.ReadMemStats(&after)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "stream" {
				allocated := after.TotalAlloc - before.TotalAlloc
				if res.StatusCode != 200 || allocated >= 32<<20 {
					t.Fatal("stream buffered content", res.StatusCode, allocated)
				}
				t.Logf("64 MiB archive: total allocated %d bytes", allocated)
			} else if res.StatusCode < 400 {
				t.Fatal("unbounded archive accepted", res.StatusCode)
			}
		})
	}
}

func TestWorkspaceArchiveCancellationReleasesUpstream(t *testing.T) {
	f := newWorkspaceFixture(t)
	_, server, query := f.proxy(t)
	started, stopped := make(chan struct{}), make(chan struct{})
	f.mu.Lock()
	f.hook = func(w http.ResponseWriter, r *http.Request, name string) bool {
		if !strings.HasSuffix(r.URL.Path, "/content") {
			return false
		}
		w.Header().Set("Content-Length", "1000000")
		w.Write([]byte("partial"))
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
		close(stopped)
		return true
	}
	f.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", server.URL+"/api/enterprise-workspace/archive?"+query+"&paths="+url.QueryEscape(`["hello.txt"]`), nil)
	done := make(chan error, 1)
	go func() {
		res, err := http.DefaultClient.Do(req)
		if err == nil {
			_, err = io.Copy(io.Discard, res.Body)
			res.Body.Close()
		}
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("archive did not start")
	}
	cancel()
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("upstream not cancelled")
	}
	if err := <-done; err == nil {
		t.Fatal("cancelled ZIP appeared complete")
	}
}

func TestWorkspaceArchiveRejectsChangedMissingDeniedOrTruncatedFiles(t *testing.T) {
	for _, scenario := range []string{"changed", "missing", "denied", "truncated", "identity", "missing-selection", "denied-list"} {
		t.Run(scenario, func(t *testing.T) {
			f := newWorkspaceFixture(t)
			_, server, query := f.proxy(t)
			var reached atomic.Bool
			f.mu.Lock()
			f.hook = func(w http.ResponseWriter, r *http.Request, name string) bool {
				if strings.HasSuffix(r.URL.Path, "/files") && scenario == "denied-list" {
					reached.Store(true)
					WriteJSON(w, 403, M{"code": "forbidden"})
					return true
				}
				if !strings.HasSuffix(r.URL.Path, "/content") || name != "second.txt" {
					return false
				}
				reached.Store(true)
				switch scenario {
				case "changed":
					if r.Header.Get("If-Match") != S(f.entry(name)["etag"]) {
						t.Error("listed version not retained")
					}
					WriteJSON(w, 412, M{"code": "file_version_conflict"})
				case "missing":
					WriteJSON(w, 404, M{"code": "file_not_found"})
				case "denied":
					WriteJSON(w, 403, M{"code": "forbidden"})
				case "truncated":
					w.Header().Set("Content-Length", "100")
					w.Write([]byte("short"))
				case "identity":
					f.p.platform.mu.Lock()
					O(f.p.platform.data["binding"])["userId"] = "other-user"
					f.p.platform.mu.Unlock()
					return false
				default:
					return false
				}
				return true
			}
			f.mu.Unlock()
			paths := `["hello.txt","second.txt"]`
			if scenario == "missing-selection" {
				paths = `["hello.txt","absent.txt"]`
			}
			res, err := http.Get(server.URL + "/api/enterprise-workspace/archive?" + query + "&paths=" + url.QueryEscape(paths))
			if err == nil {
				raw, readErr := io.ReadAll(res.Body)
				res.Body.Close()
				if scenario == "missing-selection" || scenario == "denied-list" {
					wantStatus := http.StatusNotFound
					if scenario == "denied-list" {
						wantStatus = http.StatusForbidden
					}
					if res.StatusCode != wantStatus {
						t.Fatal(res.StatusCode, string(raw))
					}
				} else if readErr == nil {
					t.Fatal("failed archive appeared complete", res.StatusCode)
				}
			} else if scenario == "missing-selection" || scenario == "denied-list" {
				t.Fatal(err)
			}
			if scenario != "missing-selection" && !reached.Load() {
				t.Fatal("expected archive failure was not reached")
			}
		})
	}
}
