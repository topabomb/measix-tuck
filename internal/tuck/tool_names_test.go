package tuck

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func TestTuckToolNamesOfficial(t *testing.T) {
	f := newPlatformFixture(t, 5)
	c := inspectionConfig(f, "OPENAI_CHAT_COMPLETIONS", "OPENAI_CHAT_COMPLETIONS")
	localImageConfig(c)
	O(A(c["imageGenerators"])[0])["providerId"] = "configured"
	f.save(c)
	want := []string{"list_sessions", "send_session_message", "deliver_result", "upload_runcove_file", "download_runcove_files", "generate_images", "inspect_attachments"}
	var checked atomic.Bool
	f.change(func() {
		f.hook = func(w http.ResponseWriter, r *http.Request, body M) bool {
			names := map[string]int{}
			for _, value := range A(body["tools"]) {
				name := S(O(O(value)["function"])["name"])
				names[name]++
				if strings.HasPrefix(name, "workspace_") || strings.HasPrefix(name, "attachments_") || strings.HasPrefix(name, "tuck_") {
					t.Error("unexpected product tool prefix", name)
				}
			}
			for _, name := range want {
				if names[name] != 1 {
					t.Error("missing or duplicated tool", name, names)
				}
			}
			checked.Store(true)
			nativeDelta(w, M{"content": "Tool names verified"})
			return true
		}
	})
	app := f.startApp()
	begin := app.Manager.options.BeginTurn
	app.Manager.options.BeginTurn = func(ctx context.Context, task M) (map[string]string, func(), error) {
		headers, finish, e := begin(ctx, task)
		if e == nil {
			// Catalog inspection only; no DAV request is made against this target.
			app.Local.mu.Lock()
			for _, turn := range app.Local.turns {
				if turn.TaskID == S(task["id"]) {
					turn.Target = M{"name": "test workspace"}
				}
			}
			app.Local.mu.Unlock()
		}
		return headers, finish, e
	}
	task := newParityTask(t, app, nil)
	submitTask(t, app.Manager, S(task["id"]), "Inspect the tool catalog")
	waitIdle(t, app.Manager, S(task["id"]))
	if !checked.Load() {
		t.Fatal("official engine did not issue the model request")
	}
}
