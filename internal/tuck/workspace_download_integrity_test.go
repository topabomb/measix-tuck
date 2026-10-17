package tuck

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestDAVDownloadRejectsSuccessfulIncompleteOrChangedBody(t *testing.T) {
	for _, scenario := range []string{"short-content-length", "short-chunked", "different-etag", "longer-body"} {
		t.Run(scenario, func(t *testing.T) {
			f := newWorkspaceFixture(t)
			l, turn := f.turn(t)
			directory := filepath.Join(turn.Workspace, "out")
			if e := os.MkdirAll(directory, 0700); e != nil {
				t.Fatal(e)
			}
			if e := os.WriteFile(filepath.Join(directory, "second.txt"), []byte("preserve existing file"), 0600); e != nil {
				t.Fatal(e)
			}
			f.setDAVHook(func(w http.ResponseWriter, r *http.Request, name string) bool {
				if r.Method != "GET" || name != "second.txt" {
					return false
				}
				if r.Header.Get("If-Match") != `"`+Hash([]byte("second"))+`"` {
					t.Error("download did not use the metadata version")
				}
				body := "short"
				w.Header().Set("ETag", r.Header.Get("If-Match"))
				if scenario == "different-etag" {
					body = "SECOND" // Same size, different version.
					w.Header().Set("ETag", `"different-version"`)
				} else if scenario == "longer-body" {
					body = "second changed"
				}
				if scenario == "short-chunked" {
					w.WriteHeader(http.StatusOK)
					w.(http.Flusher).Flush()
				} else {
					w.Header().Set("Content-Length", fmt.Sprint(len(body)))
				}
				_, _ = w.Write([]byte(body))
				return true
			})
			result := modelContent.ToolResult("download_runcove_files", l.Transfer(context.Background(), turn, "download_runcove_files", M{"remote_paths": []any{"hello.txt", "second.txt"}, "local_directory": "out", "overwrite": true}))
			failures := A(result["errors"])
			if result["status"] != "partial" || len(failures) != 1 || O(failures[0])["code"] != "webdav_version_changed" || O(failures[0])["field"] != "remote_paths[1]" {
				t.Fatal("incomplete or different content was accepted", result)
			}
			for name, want := range map[string]string{"hello.txt": "hello world", "second.txt": "preserve existing file"} {
				raw, e := os.ReadFile(filepath.Join(directory, name))
				if e != nil || string(raw) != want {
					t.Fatal(name, string(raw), e)
				}
			}
			entries, e := os.ReadDir(directory)
			if e != nil || len(entries) != 2 {
				t.Fatal("temporary download was retained", entries, e)
			}
		})
	}
}
