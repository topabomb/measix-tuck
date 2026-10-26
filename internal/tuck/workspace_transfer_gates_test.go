package tuck

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestWorkspaceTransferRequestPhases(t *testing.T) {
	for _, count := range []int{1, 2} {
		f := newWorkspaceFixture(t)
		l, turn := f.turn(t)
		trace := []string{}
		var mu sync.Mutex
		snapshot := func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), trace...) }
		f.setDAVHook(func(w http.ResponseWriter, r *http.Request, name string) bool {
			phase := r.Method + ":" + name
			if strings.HasSuffix(r.URL.Path, "/workspace") {
				phase = "status"
			}
			mu.Lock()
			trace = append(trace, phase)
			mu.Unlock()
			return false
		})
		paths := []any{"hello.txt", "second.txt"}[:count]
		result := l.Transfer(context.Background(), turn, "download_runcove_files", M{"remote_paths": paths, "local_directory": "out"})
		want := []string{"status"}
		for _, p := range paths {
			want = append(want, "status", "PROPFIND:"+S(p), "GET:"+S(p), "status")
		}
		if got := snapshot(); result["status"] != "completed" || !reflect.DeepEqual(got, want) {
			t.Fatal(result, got, want)
		}
		if err := os.WriteFile(filepath.Join(turn.Workspace, "source.txt"), []byte("source"), 0600); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		trace = nil
		mu.Unlock()
		result = l.Transfer(context.Background(), turn, "upload_runcove_file", M{"file_path": "source.txt", "remote_path": "hello.txt", "overwrite": true})
		want = []string{"status", "PROPFIND:hello.txt", "status", "PUT:hello.txt"}
		if got := snapshot(); result["status"] != "completed" || !reflect.DeepEqual(got, want) {
			t.Fatal(result, got, want)
		}
	}
}

func TestWorkspaceDownloadGatesRejectChangesBeforeReadAndPublication(t *testing.T) {
	for _, phase := range []int{2, 3} {
		for _, change := range []string{"permission", "space", "revision", "mode", "resource", "turn", "account"} {
			t.Run(change+string(rune('0'+phase)), func(t *testing.T) {
				f := newWorkspaceFixture(t)
				l, turn := f.turn(t)
				statuses, reads := 0, 0
				var mu sync.Mutex
				f.setDAVHook(func(w http.ResponseWriter, r *http.Request, name string) bool {
					mu.Lock()
					defer mu.Unlock()
					if strings.HasSuffix(r.URL.Path, "/workspace") {
						statuses++
						if statuses == phase {
							f.mu.Lock()
							switch change {
							case "permission":
								f.projection["filesAvailable"] = false
							case "space":
								f.projection["agentSpaceId"] = "spc_33333333-3333-4333-8333-333333333333"
							case "revision":
								O(f.projection["webdav"])["bindingRevision"] = 2
							case "mode":
								O(f.projection["webdav"])["accessMode"] = "READ_ONLY"
							case "resource":
								f.projection["fileExchangeWebdavResourceId"] = "dav_33333333-3333-4333-8333-333333333333"
							case "account":
								f.p.platform.mu.Lock()
								O(f.p.platform.data["binding"])["userId"] = "other-user"
								f.p.platform.mu.Unlock()
							}
							f.mu.Unlock()
						}
					} else if r.Method == "GET" {
						reads++
						if change == "turn" {
							mustUpdate(t, l.Manager, turn.TaskID, M{"turnId": UUID()})
						}
					}
					return false
				})
				if change == "turn" && phase == 2 {
					mustUpdate(t, l.Manager, turn.TaskID, M{"turnId": UUID()})
				}
				result := l.Transfer(context.Background(), turn, "download_runcove_files", M{"remote_paths": []any{"hello.txt", "second.txt"}, "local_directory": "out"})
				mu.Lock()
				gotStatuses, gotReads := statuses, reads
				mu.Unlock()
				if result["status"] != "failed" || phase == 2 && gotReads != 0 || phase == 3 && gotReads != 1 {
					t.Fatal(result, gotStatuses, gotReads)
				}
				if change != "turn" || phase != 2 {
					items := A(result["files"])
					if len(items) != 2 || O(items[0])["code"] != "tool_not_permitted" || O(items[1])["code"] != "not_attempted" {
						t.Fatal(result)
					}
				}
				entries, err := os.ReadDir(filepath.Join(turn.Workspace, "out"))
				if len(entries) != 0 || err != nil && !os.IsNotExist(err) {
					t.Fatal("rejected transfer published files", entries, err)
				}
			})
		}
	}
}
