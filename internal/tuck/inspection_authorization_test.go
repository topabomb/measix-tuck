package tuck

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"
)

func TestInspectionRechecksRemoteAccessAfterModel(t *testing.T) {
	for _, protocol := range []string{"OPENAI_CHAT_COMPLETIONS", "OPENAI_RESPONSES"} {
		for _, change := range []string{"unchanged", "binding", "authorization"} {
			t.Run(protocol+"/"+change, func(t *testing.T) {
				f := newWorkspaceFixture(t)
				f.p.save(inspectionConfig(f.p, "OPENAI_CHAT_COMPLETIONS", protocol))
				f.files["picture.png"] = testPNG(t, 20, 12, false)
				l, turn := f.turn(t)
				started, release := make(chan struct{}), make(chan struct{})
				var releaseOnce sync.Once
				unblock := func() { releaseOnce.Do(func() { close(release) }) }
				defer unblock()
				f.p.change(func() {
					previous := f.p.hook
					f.p.hook = func(w http.ResponseWriter, r *http.Request, body M) bool {
						if body["model"] != "vision-upstream" {
							return previous(w, r, body)
						}
						close(started)
						select {
						case <-release:
							inspectionReply(w, protocol == "OPENAI_RESPONSES", "Remote image inspected")
						case <-r.Context().Done():
						}
						return true
					}
				})
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				done := make(chan M, 1)
				go func() {
					done <- l.inspectAttachments(ctx, turn, "root", M{"attachments": []any{M{"source": "runcove", "path": "picture.png"}}, "request": "Describe the image"})
				}()
				select {
				case <-started:
				case result := <-done:
					t.Fatal("inspection ended before model request", result)
				case <-ctx.Done():
					t.Fatal("model request did not start")
				}
				f.mu.Lock()
				switch change {
				case "binding":
					O(f.projection["webdav"])["bindingRevision"] = 2
				case "authorization":
					f.projection["filesAvailable"] = false
					f.projection["filesReason"] = "disabled"
				}
				f.mu.Unlock()
				unblock()
				result := <-done
				if change == "unchanged" {
					if result["status"] != "completed" || result["text"] != "Remote image inspected" {
						t.Fatal("unchanged access rejected", result)
					}
				} else if result["status"] != "failed" || result["code"] != "tool_not_permitted" || Has(result, "text") {
					t.Fatal("inspection published after remote access changed", result)
				}
			})
		}
	}
}
