package tuck

import (
	"gopkg.in/yaml.v3"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestTuckToolNamesOfficial(t *testing.T) {
	all := modelContent.ToolNames()
	for _, protocol := range []string{"OPENAI_CHAT_COMPLETIONS", "OPENAI_RESPONSES"} {
		for _, names := range [][]string{all, {"list_conversations", "send_conversation_message", "deliver_result"}, {"inspect_attachments"}, {}} {
			t.Run(protocol+"/"+strings.Join(names, "+"), func(t *testing.T) {
				f := newWorkspaceFixture(t).p
				workspaceHook := f.hook
				c := inspectionConfig(f, protocol, "OPENAI_CHAT_COMPLETIONS")
				withNativeDiscovery := len(names) == len(all)
				if withNativeDiscovery {
					agent := O(O(c["agents"])["root"])
					agent["toolsets"] = append(A(agent["toolsets"]),
						M{"type": "session_context"},
						M{"type": "tasks", "path": filepath.Join(t.TempDir(), "native-tasks.json")})
				}
				localImageConfig(c)
				O(A(c["imageGenerators"])[0])["providerId"] = "configured"
				disabled := []string{}
				for _, name := range all {
					if name != "deliver_result" && !Contains(names, name) {
						disabled = append(disabled, name)
					}
				}
				O(O(c["agents"])["root"])["disabledTuckTools"] = disabled
				for _, v := range A(O(c["workbench"])["assistants"]) {
					if S(O(v)["agent"]) == "root" {
						O(v)["deliveryTool"] = Contains(names, "deliver_result")
					}
				}
				f.save(c)
				expected := modelContent.Instructions(modelContent.Tools(names, nil))
				var checked atomic.Int32
				f.change(func() {
					f.hook = func(w http.ResponseWriter, r *http.Request, body M) bool {
						if workspaceHook(w, r, body) {
							return true
						}
						if !strings.HasSuffix(r.URL.Path, "/chat/completions") && !strings.HasSuffix(r.URL.Path, "/responses") {
							return false
						}
						counts := map[string]int{}
						for _, v := range A(body["tools"]) {
							tool := O(v)
							name := firstString(O(tool["function"])["name"], tool["name"])
							counts[name]++
						}
						for _, name := range all {
							want := 0
							if Contains(names, name) {
								want = 1
							}
							if counts[name] != want {
								t.Errorf("tool %s count %d, want %d", name, counts[name], want)
							}
						}
						if counts["send_session_message"] != 0 {
							t.Error("obsolete Tuck message tool exposed")
						}
						for _, name := range []string{"list_sessions", "list_tasks"} {
							want := 0
							if withNativeDiscovery {
								want = 1
							}
							if counts[name] != want {
								t.Errorf("native tool %s count %d, want %d", name, counts[name], want)
							}
						}
						// Only model instructions, not descriptions or user content, may carry
						// the inventory. Verify both provider protocols and ordinary continuation.
						system := S(body["instructions"])
						for _, key := range []string{"messages", "input"} {
							for _, v := range A(body[key]) {
								m := O(v)
								if S(m["role"]) == "system" || S(m["role"]) == "developer" {
									system += string(JSON(m["content"]))
								}
							}
						}
						if expected == "" {
							if strings.Contains(system, "The Tuck toolset provides") {
								t.Error("empty toolset instructions", system)
							}
						} else if strings.Count(system, expected) != 1 {
							t.Errorf("missing or repeated instructions: %s", system)
						}
						checked.Add(1)
						inspectionNativeReply(w, protocol, "", nil, "Tool catalog verified")
						return true
					}
				})
				app := f.startApp()
				path, e := app.Models.WriteConfig()
				if e != nil {
					t.Fatal(e)
				}
				raw, e := os.ReadFile(path)
				if e != nil {
					t.Fatal(e)
				}
				var projection M
				if e = yaml.Unmarshal(raw, &projection); e != nil {
					t.Fatal(e)
				}
				if S(projection["version"]) != "16" {
					t.Fatal("wrong native configuration version", projection["version"])
				}
				count := 0
				for _, v := range A(O(O(projection["agents"])["root"])["toolsets"]) {
					tool := O(v)
					if S(tool["type"]) == "api" {
						t.Fatal("parallel API tool", tool)
					}
					if strings.Contains(S(O(tool["remote"])["url"]), tuckToolsPath) {
						count++
					}
				}
				want := 1
				if len(names) == 0 {
					want = 0
				}
				if count != want {
					t.Fatal("wrong MCP service count", count, want)
				}
				task := newParityTask(t, app, nil)
				for _, prompt := range []string{"Inspect tool catalog", "Continue with the same tools"} {
					submitTask(t, app.Manager, S(task["id"]), prompt)
					waitIdle(t, app.Manager, S(task["id"]))
				}
				if checked.Load() != 2 {
					t.Fatal("model requests", checked.Load())
				}
			})
		}
	}
}

func TestUnifiedMCPInstructionsAfterApply(t *testing.T) {
	f := newPlatformFixture(t, 5)
	c := f.models.Config()
	c["providers"] = []any{localProvider(f.server.URL)}
	c["defaultModelId"] = "proof-model"
	f.save(c)
	var want atomic.Value
	want.Store([]string{"list_conversations", "send_conversation_message", "deliver_result", "inspect_attachments"})
	var checked atomic.Int32
	f.change(func() {
		f.hook = func(w http.ResponseWriter, r *http.Request, body M) bool {
			if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
				return false
			}
			names := want.Load().([]string)
			system := ""
			for _, v := range A(body["messages"]) {
				m := O(v)
				if m["role"] == "system" {
					system += S(m["content"])
				}
			}
			expected := modelContent.Instructions(modelContent.Tools(names, nil))
			if strings.Count(system, expected) != 1 || strings.Count(system, "The Tuck toolset provides") != 1 {
				t.Error("stale or repeated inventory", system, expected)
			}
			for _, v := range A(body["tools"]) {
				name := S(O(O(v)["function"])["name"])
				if Contains(modelContent.ToolNames(), name) && !Contains(names, name) {
					t.Error("disabled tool survived apply", name)
				}
			}
			checked.Add(1)
			inspectionNativeReply(w, "OPENAI_CHAT_COMPLETIONS", "", nil, "Verified")
			return true
		}
	})
	app := f.startApp()
	task := newParityTask(t, app, nil)
	id := S(task["id"])
	runAppTask(t, app, id, "Before apply")
	status, data := appRequest(t, app, "GET", "/api/settings/runtime", nil)
	if status != 200 {
		t.Fatal(status, data)
	}
	O(O(O(data["config"])["agents"])["root"])["disabledTuckTools"] = []string{"list_conversations", "send_conversation_message"}
	want.Store([]string{"deliver_result", "inspect_attachments"})
	status, data = appRequest(t, app, "PUT", "/api/settings/runtime", data)
	if status != 200 {
		t.Fatal(status, data)
	}
	runAppTask(t, app, id, "After apply")
	if checked.Load() != 2 {
		t.Fatal("model request count", checked.Load())
	}
}
