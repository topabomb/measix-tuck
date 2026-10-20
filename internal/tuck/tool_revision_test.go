package tuck

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestToolTextResources(t *testing.T) {
	for id := range modelTexts {
		if strings.HasPrefix(string(id), "tuck.agent.") {
			continue
		}
		text, e := modelContent.ResolveText(id, map[string]string{"provider": "Example {provider}", "model": "image-1"})
		if e != nil || strings.TrimSpace(text) == "" {
			t.Fatal(id, e)
		}
	}
	if _, e := modelContent.ResolveText("missing", nil); e == nil {
		t.Fatal("missing resource accepted")
	}
	if _, e := modelContent.ResolveText(textGenerateModel, nil); e == nil {
		t.Fatal("missing template value accepted")
	}
	var check func(M)
	check = func(schema M) {
		for name, v := range O(schema["properties"]) {
			property := O(v)
			if S(property["description"]) == "" {
				t.Error("missing parameter description", name)
			}
			check(property)
			check(O(property["items"]))
		}
	}
	definitions := modelContent.Tools(modelContent.ToolNames(), M{"upstreamModelKey": "image-1", "providerName": "Test Provider"})
	for _, definition := range definitions {
		check(O(definition["inputSchema"]))
		if definition["name"] == "generate_images" && !strings.Contains(S(definition["description"]), "Provider: Test Provider. Model: image-1.") {
			t.Fatal(definition)
		}
	}
}

func TestInspectionLocalRootReplacedAndRemoteDecode(t *testing.T) {
	f := newWorkspaceFixture(t)
	c := inspectionConfig(f.p, "OPENAI_CHAT_COMPLETIONS", "OPENAI_CHAT_COMPLETIONS")
	f.p.save(c)
	l, turn := f.turn(t)
	outside := t.TempDir()
	raw := testPNG(t, 20, 12, false)
	if e := os.WriteFile(filepath.Join(outside, "secret.png"), raw, 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.Rename(turn.Workspace, turn.Workspace+"-original"); e != nil {
		t.Fatal(e)
	}
	if e := directoryLink(outside, turn.Workspace); e != nil {
		t.Fatal(e)
	}
	result := l.inspectAttachments(context.Background(), turn, "root", M{"attachments": []any{M{"path": "secret.png"}}, "request": "Inspect"})
	if result["code"] != "workspace_changed" {
		t.Fatal("rebound local root accepted", result)
	}
	f.mu.Lock()
	f.files["bad.png"] = raw[:16]
	f.mu.Unlock()
	result = l.inspectAttachments(context.Background(), turn, "root", M{"attachments": []any{M{"source": "runcove", "path": "bad.png"}}, "request": "Inspect"})
	if result["code"] != "attachment_invalid_image" || Has(result, "reason") {
		t.Fatal("unclassified remote decode", result)
	}
}

func TestListConversationsLimit(t *testing.T) {
	f := newPlatformFixture(t, 5)
	app := f.startApp()
	sender := newParityTask(t, app, nil)
	session := connectTuckTools(t, app, sender, "root")
	for i := 0; i < 11; i++ {
		newParityTask(t, app, nil)
	}
	for _, tt := range []struct {
		args          M
		count, status int
	}{{M{}, 10, 200}, {M{"limit": 1}, 1, 200}, {M{"limit": 100}, 12, 200}, {M{"limit": 0}, 0, 400}, {M{"limit": 101}, 0, 400}, {M{"limit": 1.5}, 0, 400}} {
		result := callTuckTool(t, session, "list_conversations", tt.args, tt.status != 200)
		if tt.status == 200 && len(A(result["conversations"])) != tt.count {
			t.Fatal(tt, result)
		}
	}
}

func TestRuncoveDeliverySnapshotsAndNoDirectory(t *testing.T) {
	f := newWorkspaceFixture(t)
	l, turn := f.turn(t)
	if e := os.WriteFile(filepath.Join(turn.Workspace, "local.txt"), []byte("local"), 0600); e != nil {
		t.Fatal(e)
	}
	args := M{"summary": "Mixed sources", "files": []any{M{"path": "local.txt"}, M{"source": "runcove", "path": "hello.txt"}}}
	result := l.Delivery.Submit(context.Background(), turn, args)
	if result["status"] != "completed" {
		t.Fatal(result)
	}
	saved, e := l.Delivery.Store.Delivery(turn.TaskID, "latest")
	if e != nil {
		t.Fatal(e)
	}
	f.mu.Lock()
	f.files["hello.txt"] = []byte("changed remotely")
	f.mu.Unlock()
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	if e = l.Delivery.ServeFile(w, r, turn.TaskID, "1", S(O(A(saved["files"])[1])["id"])); e != nil || w.Body.String() != "hello world" {
		t.Fatal("mutable remote delivery", e, w.Body.String())
	}
	turn.Task["workspaceMode"], turn.Task["workspace"], turn.Workspace = "none", "", ""
	result = l.Delivery.Submit(context.Background(), turn, M{"summary": "Remote only", "files": []any{M{"source": "runcove", "path": "hello.txt"}}})
	if result["status"] != "completed" {
		t.Fatal("remote delivery required local directory", result)
	}
	result = l.Transfer(context.Background(), turn, "download_runcove_files", M{"remote_paths": []string{"hello.txt"}, "local_directory": "."})
	if result["status"] != "failed" {
		t.Fatal("none allowed local write", result)
	}
	result = l.Delivery.Submit(context.Background(), turn, M{"summary": "Missing", "files": []any{M{"source": "runcove", "path": "missing.txt"}}})
	latest, _ := l.Delivery.Store.Delivery(turn.TaskID, "latest")
	if result["status"] != "failed" || I(latest["version"]) != 2 {
		t.Fatal("failed delivery published", result, latest)
	}
	staging, _ := os.ReadDir(filepath.Join(l.Delivery.Root, ".staging"))
	if len(staging) != 0 {
		t.Fatal("staging leaked", staging)
	}
}

func TestRuncoveInspectionNativeAndPreview(t *testing.T) {
	for _, protocol := range []string{"OPENAI_CHAT_COMPLETIONS", "OPENAI_RESPONSES"} {
		t.Run(protocol, func(t *testing.T) {
			f := newWorkspaceFixture(t)
			c := inspectionConfig(f.p, "OPENAI_CHAT_COMPLETIONS", protocol)
			f.p.save(c)
			raw := testPNG(t, 20, 12, false)
			prepared, _, _ := PrepareModelImage(raw)
			f.files["picture.png"] = raw
			var calls atomic.Int32
			var resultSeen atomic.Bool
			f.p.change(func() {
				f.p.hook = func(w http.ResponseWriter, r *http.Request, body M) bool {
					if strings.HasPrefix(r.URL.Path, f.davRoot) || strings.Contains(r.URL.Path, "/workspace") {
						f.serve(w, r, body)
						return true
					}
					if S(body["model"]) == "vision-upstream" {
						calls.Add(1)
						if Has(body, "tools") || Has(body, "previous_response_id") || !strings.Contains(string(JSON(body)), "data:image/") {
							t.Error("invalid sidecar request", body)
						}
						inspectionReply(w, protocol == "OPENAI_RESPONSES", "Remote image inspected")
						return true
					}
					if strings.Contains(string(JSON(body["messages"])), "Recall evidence") {
						nativeDelta(w, M{"content": "Prior inspection retained"})
						return true
					}
					_, results := recentNative(body)
					if len(results) == 0 {
						nativeDelta(w, nativeCall(inspectionTool, M{"attachments": []any{M{"source": "runcove", "path": "picture.png"}}, "request": "Describe this image"}, "remote-inspect"))
					} else {
						for _, result := range results {
							value := O(parseValue(result["content"]))
							if value["text"] == "Remote image inspected" && len(value) != 2 {
								t.Error("inspection metadata leaked to model", value)
							}
						}
						if strings.Contains(string(JSON(results)), "Remote image inspected") && !strings.Contains(string(JSON(body)), "data:image/") {
							resultSeen.Store(true)
						}
						nativeDelta(w, M{"content": "Remote inspection done"})
					}
					return true
				}
			})
			app := f.p.startApp()
			task := newParityTask(t, app, M{"workspaceMode": "none"})
			id := S(task["id"])
			submitTask(t, app.Manager, id, "Inspect a remote image")
			waitIdle(t, app.Manager, id)
			if calls.Load() != 1 || !resultSeen.Load() {
				t.Fatal("remote multimodal chain", calls.Load(), resultSeen.Load())
			}
			messages, _ := app.Store.Messages(id, 0, 100)
			src := ""
			for _, m := range messages {
				view := WorkbenchMessage(id, m)
				for _, b := range A(view["blocks"]) {
					data := O(O(b)["data"])
					if data["name"] == inspectionTool && len(A(data["inputAttachments"])) > 0 {
						src = S(O(A(data["inputAttachments"])[0])["src"])
					}
				}
			}
			if src == "" {
				t.Fatal("missing remote input preview", messages)
			}
			response, e := http.Get(app.URL + src)
			if e != nil {
				t.Fatal(e)
			}
			data, _ := io.ReadAll(response.Body)
			response.Body.Close()
			if response.StatusCode != 200 || !bytes.Equal(data, prepared) {
				t.Fatal("remote preview", response.StatusCode, string(data))
			}
			// Fork/import may place several calls in one assistant message.
			for _, m := range messages {
				if findBlock(m, "tool-call") == nil {
					continue
				}
				data := O(findBlock(m, "tool-call")["data"])
				if data["name"] != inspectionTool {
					continue
				}
				decoy := newBlock("call:other", "tool-call", M{"name": "list_conversations", "callId": "other", "input": M{}})
				m["blocks"] = append([]any{decoy}, A(m["blocks"])...)
				if e := app.Store.SaveMessage(id, m); e != nil {
					t.Fatal(e)
				}
				break
			}
			response, e = http.Get(app.URL + src)
			if e != nil {
				t.Fatal(e)
			}
			response.Body.Close()
			if response.StatusCode != 200 {
				t.Fatal("multiple call blocks broke preview", response.StatusCode)
			}
			submitTask(t, app.Manager, id, "Recall evidence")
			waitIdle(t, app.Manager, id)
			options, e := app.Manager.ForkOptions(context.Background(), id)
			if e != nil || len(options) == 0 {
				t.Fatal(e, options)
			}
			fork, e := app.Manager.Fork(context.Background(), id, I(options[len(options)-1]["index"]))
			if e != nil {
				t.Fatal(e)
			}
			forkID := S(O(fork["task"])["id"])
			forkMessages, _ := app.Store.Messages(forkID, 0, 100)
			forkPreview := ""
			for _, m := range forkMessages {
				view := WorkbenchMessage(forkID, m)
				for _, b := range A(view["blocks"]) {
					data := O(O(b)["data"])
					if data["name"] == inspectionTool && len(A(data["inputAttachments"])) > 0 {
						forkPreview = S(O(A(data["inputAttachments"])[0])["src"])
					}
				}
			}
			if forkPreview == "" {
				t.Fatal("fork lost remote input preview", forkMessages)
			}
			response, e = http.Get(app.URL + forkPreview)
			if e != nil {
				t.Fatal(e)
			}
			response.Body.Close()
			if response.StatusCode != 200 {
				t.Fatal("fork remote preview", response.StatusCode)
			}
			submitTask(t, app.Manager, forkID, "Recall evidence again")
			waitIdle(t, app.Manager, forkID)
			secondOptions, e := app.Manager.ForkOptions(context.Background(), forkID)
			if e != nil || len(secondOptions) == 0 {
				t.Fatal(e, secondOptions)
			}
			secondFork, e := app.Manager.Fork(context.Background(), forkID, I(secondOptions[len(secondOptions)-1]["index"]))
			if e != nil {
				t.Fatal(e)
			}
			secondID := S(O(secondFork["task"])["id"])
			secondMessages, _ := app.Store.Messages(secondID, 0, 100)
			secondPreview := ""
			for _, m := range secondMessages {
				for _, b := range A(WorkbenchMessage(secondID, m)["blocks"]) {
					data := O(O(b)["data"])
					if data["name"] == inspectionTool && len(A(data["inputAttachments"])) > 0 {
						secondPreview = S(O(A(data["inputAttachments"])[0])["src"])
					}
				}
			}
			if secondPreview == "" {
				t.Fatal("second fork lost preview")
			}
			response, e = http.Get(app.URL + secondPreview)
			if e != nil {
				t.Fatal(e)
			}
			response.Body.Close()
			if response.StatusCode != 200 {
				t.Fatal("second fork preview", response.StatusCode)
			}
			f.mu.Lock()
			O(f.projection["webdav"])["bindingRevision"] = 2
			f.mu.Unlock()
			response, e = http.Get(app.URL + src)
			if e != nil {
				t.Fatal(e)
			}
			response.Body.Close()
			if response.StatusCode < 400 {
				t.Fatal("preview crossed workspace binding")
			}
		})
	}
}
