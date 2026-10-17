package tuck

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func inspectionConfig(f *platformFixture, chat, vision string) M {
	c := f.models.Config()
	p := localProvider(f.server.URL)
	p["protocol"] = chat
	v := localProvider(f.server.URL)
	v["id"], v["protocol"] = "vision-provider", vision
	model := O(A(v["models"])[0])
	model["modelId"], model["upstreamModelKey"] = "vision", "vision-upstream"
	model["inputModalities"], model["capabilities"] = []string{"TEXT", "IMAGE"}, []string{}
	c["providers"], c["defaultModelId"], c["defaultAttachmentInspectionModelId"] = []any{p, v}, "proof-model", "vision"
	return c
}

func inspectionReply(w http.ResponseWriter, responses bool, text string) {
	if responses {
		WriteJSON(w, 200, M{"status": "completed", "output": []any{M{"type": "message", "role": "assistant", "content": []any{M{"type": "output_text", "text": text}}}}})
	} else {
		WriteJSON(w, 200, M{"choices": []any{M{"finish_reason": "stop", "message": M{"role": "assistant", "content": text}}}})
	}
}

func inspectionNativeReply(w http.ResponseWriter, protocol, tool string, args M, text string) {
	if protocol == "OPENAI_CHAT_COMPLETIONS" {
		if tool != "" {
			nativeDelta(w, nativeCall(tool, args, "inspection-call"))
		} else {
			nativeDelta(w, M{"content": text})
		}
		return
	}
	item := M{"type": "message", "id": "msg_inspect", "role": "assistant", "status": "completed", "content": []any{M{"type": "output_text", "text": text, "annotations": []any{}}}}
	if tool != "" {
		item = M{"type": "function_call", "id": "fc_inspect", "call_id": "inspection-call", "name": tool, "arguments": string(JSON(args)), "status": "completed"}
	}
	result := M{"id": "resp_inspect", "object": "response", "status": "completed", "output": []any{item}}
	w.Header().Set("Content-Type", "text/event-stream")
	for _, event := range []M{{"type": "response.created", "response": Merge(result, M{"status": "in_progress", "output": []any{}})}, {"type": "response.output_item.added", "output_index": 0, "item": item}, {"type": "response.output_item.done", "output_index": 0, "item": item}, {"type": "response.completed", "response": result}} {
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", S(event["type"]), JSON(event))
	}
}

func TestInspectionNativeContext(t *testing.T) {
	for _, chat := range []string{"OPENAI_CHAT_COMPLETIONS", "OPENAI_RESPONSES"} {
		for _, vision := range []string{"OPENAI_CHAT_COMPLETIONS", "OPENAI_RESPONSES"} {
			t.Run(chat+"/"+vision, func(t *testing.T) {
				f := newPlatformFixture(t, 5)
				c := inspectionConfig(f, chat, vision)
				f.save(c)
				var calls atomic.Int32
				var sawResult atomic.Bool
				var paths []string
				var pixels [][]byte
				f.change(func() {
					f.hook = func(w http.ResponseWriter, r *http.Request, body M) bool {
						encoded := string(JSON(body))
						if S(body["model"]) == "vision-upstream" {
							calls.Add(1)
							if Has(body, "tools") || Has(body, "previous_response_id") || B(body["stream"]) || strings.Contains(encoded, "PRIVATE_HISTORY") {
								t.Error("inspection leaked conversation or enabled tools")
							}
							if vision == "OPENAI_RESPONSES" && (body["store"] != false || !strings.HasSuffix(r.URL.Path, "/responses")) {
								t.Error("responses contract", body)
							}
							matches := regexp.MustCompile(`data:image/[^;]+;base64,([A-Za-z0-9+/=]+)`).FindAllStringSubmatch(encoded, -1)
							if len(matches) != 4 {
								t.Error("missing attachments", len(matches))
							}
							for i, match := range matches {
								raw, e := base64.StdEncoding.DecodeString(match[1])
								if e != nil || i >= len(pixels) || !bytes.Equal(raw, pixels[i]) {
									t.Error("changed image bytes/order", i)
								}
							}
							inspectionReply(w, vision == "OPENAI_RESPONSES", "VISION_EVIDENCE: four images inspected in order")
							return true
						}
						if strings.HasSuffix(r.URL.Path, "/chat/completions") || strings.HasSuffix(r.URL.Path, "/responses") {
							if strings.Contains(encoded, "data:image") {
								t.Error("text-only main model received pixels")
							}
							if strings.Contains(encoded, "VISION_EVIDENCE") {
								sawResult.Store(true)
								inspectionNativeReply(w, chat, "", nil, "Evidence received")
							} else {
								inspectionNativeReply(w, chat, inspectionTool, M{"attachments": inspectionTestRefs(paths), "request": "Compare these four images"}, "")
							}
							return true
						}
						return false
					}
				})
				app := f.startApp()
				task := newParityTask(t, app, nil)
				for i, path := range []string{"project material/第一张.png", "two.png", "artifacts/images/three.png", "root-image.png"} {
					full := filepath.Join(S(task["workspace"]), path)
					if e := os.MkdirAll(filepath.Dir(full), 0700); e != nil {
						t.Fatal(e)
					}
					raw := testPNG(t, 20+i, 12, false)
					if e := os.WriteFile(full, raw, 0600); e != nil {
						t.Fatal(e)
					}
					prepared, _, e := PrepareModelImage(raw)
					if e != nil {
						t.Fatal(e)
					}
					pixels = append(pixels, prepared)
					if i == 3 {
						paths = append(paths, full)
					} else {
						paths = append(paths, path)
					}
				}
				submitTask(t, app.Manager, S(task["id"]), "PRIVATE_HISTORY inspect the local pictures")
				waitIdle(t, app.Manager, S(task["id"]))
				if calls.Load() != 1 || !sawResult.Load() {
					t.Fatal("inspection failed to reach context", calls.Load(), JSON(taskMessages(t, app.Store, S(task["id"]))))
				}
				sawResult.Store(false)
				submitTask(t, app.Manager, S(task["id"]), "Use the prior inspection evidence")
				waitIdle(t, app.Manager, S(task["id"]))
				if calls.Load() != 1 || !sawResult.Load() {
					t.Fatal("inspection was not retained")
				}
				options, e := app.Manager.ForkOptions(context.Background(), S(task["id"]))
				if e != nil {
					t.Fatal(e)
				}
				fork, e := app.Manager.Fork(context.Background(), S(task["id"]), I(options[len(options)-1]["index"]))
				if e != nil {
					t.Fatal(e)
				}
				if !strings.Contains(string(JSON(taskMessages(t, app.Store, S(O(fork["task"])["id"])))), "VISION_EVIDENCE") {
					t.Fatal("fork lost inspection text")
				}
			})
		}
	}
}

func TestInspectionWorkspaceAndValidation(t *testing.T) {
	f := newPlatformFixture(t, 5)
	c := inspectionConfig(f, "OPENAI_CHAT_COMPLETIONS", "OPENAI_CHAT_COMPLETIONS")
	f.save(c)
	app := f.startApp()
	task := newParityTask(t, app, nil)
	workspace := S(task["workspace"])
	raw := testPNG(t, 30, 20, false)
	if e := os.WriteFile(filepath.Join(workspace, "ok.png"), raw, 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(workspace, "bad.png"), []byte("not pixels"), 0600); e != nil {
		t.Fatal(e)
	}
	outside := t.TempDir()
	if e := os.WriteFile(filepath.Join(outside, "ok.png"), raw, 0600); e != nil {
		t.Fatal(e)
	}
	if e := directoryLink(outside, filepath.Join(workspace, "escape")); e != nil {
		t.Fatal(e)
	}
	for _, path := range []string{"../ok.png", filepath.Join(outside, "ok.png"), "escape/ok.png", "https://example.com/x.png", "data:image/png;base64,AA", "bad.png", "missing.png", "."} {
		if _, e := app.Files.ReadInspectionAttachments(context.Background(), task, []any{path}); e == nil {
			t.Error("accepted", path)
		}
	}
	for _, paths := range [][]any{nil, {"ok.png", "ok.png"}, {"ok.png", "a", "b", "c", "d"}} {
		if _, e := app.Files.ReadInspectionAttachments(context.Background(), task, paths); e == nil {
			t.Error("invalid paths accepted", paths)
		}
	}
	for _, mode := range []string{"auto", "specified", "none"} {
		images, e := app.Files.ReadInspectionAttachments(context.Background(), Merge(task, M{"workspaceMode": mode}), []any{filepath.Join(workspace, "ok.png")})
		if e != nil || len(images) != 1 {
			t.Fatal("read-only mode", mode, e)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := app.Files.ReadInspectionAttachments(ctx, task, []any{"ok.png"}); Code(e) != "operation_canceled" {
		t.Fatal(e)
	}
	if _, e := app.Files.ReadInspectionAttachments(context.Background(), M{}, []any{"ok.png"}); Code(e) != "workspace_unavailable" {
		t.Fatal(e)
	}
}

func TestInspectionInputPreview(t *testing.T) {
	f := newPlatformFixture(t, 5)
	app := f.startApp()
	task := newParityTask(t, app, nil)
	workspace, id := S(task["workspace"]), S(task["id"])
	raw := testPNG(t, 30, 20, false)
	prepared, mime, e := PrepareModelImage(raw)
	if e != nil {
		t.Fatal(e)
	}
	paths := []any{"picture.png", filepath.Join(workspace, "无扩展名图片")}
	for _, path := range []string{"picture.png", "无扩展名图片"} {
		if e := os.WriteFile(filepath.Join(workspace, path), raw, 0600); e != nil {
			t.Fatal(e)
		}
	}
	message := M{"blocks": []any{newBlock("call:inspect", "tool-call", M{"name": inspectionTool, "input": M{"attachments": inspectionTestRefs(paths), "request": "Compare"}})}}
	view := WorkbenchMessage(id, message)
	items := A(O(findBlock(view, "tool-call")["data"])["inputAttachments"])
	if len(items) != 2 || Has(O(findBlock(message, "tool-call")["data"]), "inputAttachments") || strings.Contains(string(JSON(view)), "base64") {
		t.Fatal("preview must be a browser-only projection", view, message)
	}
	for _, item := range items {
		response, e := http.Get(app.URL + S(O(item)["src"]))
		if e != nil {
			t.Fatal(e)
		}
		body, e := io.ReadAll(response.Body)
		response.Body.Close()
		if e != nil || response.StatusCode != 200 || response.Header.Get("Content-Type") != mime || response.Header.Get("Cache-Control") != "no-store" || !bytes.Equal(body, prepared) {
			t.Fatal("preview response", e, response.StatusCode, response.Header)
		}
		if _, _, e := PrepareModelImage(body); e != nil {
			t.Fatal("preview was not an actual image", e)
		}
	}
	outside := t.TempDir()
	if e := os.WriteFile(filepath.Join(outside, "secret.png"), raw, 0600); e != nil {
		t.Fatal(e)
	}
	if e := directoryLink(outside, filepath.Join(workspace, "escape")); e != nil {
		t.Fatal(e)
	}
	if e := os.Remove(filepath.Join(workspace, "picture.png")); e != nil {
		t.Fatal(e)
	}
	for _, path := range []string{"picture.png", "../secret.png", filepath.Join(outside, "secret.png"), "escape/secret.png", "https://example.com/image.png", "data:image/png;base64,AA"} {
		response, e := http.Get(app.URL + "/api/tasks/" + id + "/files?inspection=1&path=" + url.QueryEscape(path))
		if e != nil {
			t.Fatal(e)
		}
		response.Body.Close()
		if response.StatusCode < 400 {
			t.Fatal("preview accepted missing or outside file", path)
		}
	}
}

func TestInspectionSelectionAndErrors(t *testing.T) {
	f := newPlatformFixture(t, 5)
	c := inspectionConfig(f, "OPENAI_CHAT_COMPLETIONS", "OPENAI_CHAT_COMPLETIONS")
	f.save(c)
	if model, e := f.models.InspectionModel("root"); e != nil || B(model["selectable"]) || !B(model["inspectionSelectable"]) {
		t.Fatal("vision-only model", model, e)
	}
	O(O(c["agents"])["worker"])["attachmentInspectionModelId"] = "proof-model"
	f.save(c)
	if _, e := f.models.InspectionModel("worker"); Code(e) != "inspection_model_unavailable" {
		t.Fatal("invalid override fell back", e)
	}
	delete(O(O(c["agents"])["worker"]), "attachmentInspectionModelId")
	c["defaultAttachmentInspectionModelId"] = ""
	f.save(c)
	if _, e := f.models.InspectionModel("root"); Code(e) != "inspection_model_required" {
		t.Fatal(e)
	}
	f.enroll()
	model, e := f.models.InspectionModel("root")
	if e != nil || S(model["source"]) != "platform" {
		t.Fatal("platform default", model, e)
	}
	var calls atomic.Int32
	f.change(func() {
		f.hook = func(w http.ResponseWriter, r *http.Request, body M) bool {
			if !strings.Contains(r.URL.Path, "/resources/"+S(model["modelId"])+"/") {
				return false
			}
			calls.Add(1)
			if r.Header.Get("Authorization") != "Bearer "+S(f.controls["token"]) || r.Header.Get("X-Measix-Managed-Generation") == "" || S(body["model"]) != S(model["upstreamModelKey"]) {
				t.Error("platform contract")
			}
			WriteJSON(w, 400, M{"error": M{"code": "unsupported_image", "message": "Image input not supported Bearer " + S(f.controls["token"])}})
			return true
		}
	})
	raw := testPNG(t, 20, 12, false)
	_, e = f.models.InspectAttachments(context.Background(), c, f.platform.Binding(), model, "inspect", []InspectedAttachment{{Path: "ok.png", MIME: "image/png", Bytes: raw}})
	if Code(e) != "upstream_rejected" || modelContent.Error(e)["upstream_code"] != "unsupported_image" || strings.Contains(e.Error(), S(f.controls["token"])) || calls.Load() != 1 {
		t.Fatal("rejection/redaction/retry", e, calls.Load())
	}
	f.change(func() { O(f.controls["state"])["runtimeBlocked"] = true })
	before := calls.Load()
	_, e = f.models.InspectAttachments(context.Background(), c, f.platform.Binding(), model, "inspect", []InspectedAttachment{{Path: "ok.png", MIME: "image/png", Bytes: raw}})
	if e == nil || calls.Load() != before {
		t.Fatal("admit bypass", e)
	}
}

func TestInspectionNativeChildOverrideAndFailure(t *testing.T) {
	for _, reject := range []bool{false, true} {
		t.Run(fmt.Sprint(reject), func(t *testing.T) {
			f := newPlatformFixture(t, 5)
			c := inspectionConfig(f, "OPENAI_CHAT_COMPLETIONS", "OPENAI_CHAT_COMPLETIONS")
			provider := O(A(c["providers"])[1])
			provider["models"] = append(A(provider["models"]), Merge(O(A(provider["models"])[0]), M{"modelId": "worker-vision", "upstreamModelKey": "worker-vision"}))
			worker := O(O(c["agents"])["worker"])
			worker["instruction"], worker["readonly"], worker["attachmentInspectionModelId"] = "INSPECTION_WORKER", true, "worker-vision"
			O(O(c["agents"])["root"])["sub_agents"] = []string{"worker"}
			f.save(c)
			var visionCalls atomic.Int32
			var sawFailure atomic.Bool
			f.change(func() {
				f.hook = func(w http.ResponseWriter, r *http.Request, body M) bool {
					if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
						return false
					}
					if S(body["model"]) != "upstream-proof" {
						visionCalls.Add(1)
						if body["model"] != "worker-vision" {
							t.Error("child used global model", body["model"])
						}
						if reject {
							WriteJSON(w, 400, M{"error": M{"code": "unsupported_image", "message": "This model cannot accept images"}})
						} else {
							inspectionReply(w, false, "CHILD_VISUAL_EVIDENCE")
						}
						return true
					}
					encoded := string(JSON(body))
					if strings.Contains(encoded, "data:image") {
						t.Error("inspection pixels leaked to chat model")
					}
					if strings.Contains(encoded, "INSPECTION_WORKER") {
						if strings.Contains(encoded, "unsupported_image") {
							sawFailure.Store(true)
							nativeDelta(w, M{"content": "CHILD_HANDLED_FAILURE: configure a model with image support"})
						} else if strings.Contains(encoded, "CHILD_VISUAL_EVIDENCE") {
							nativeDelta(w, M{"content": "CHILD_DONE"})
						} else {
							nativeDelta(w, nativeCall(inspectionTool, M{"attachments": inspectionTestRefs([]string{"source.png"}), "request": "Describe this picture"}, "child-inspect"))
						}
					} else if strings.Contains(encoded, "CHILD_DONE") || strings.Contains(encoded, "CHILD_HANDLED_FAILURE") {
						nativeDelta(w, M{"content": "PARENT_DONE"})
					} else {
						nativeDelta(w, nativeCall("transfer_task", M{"agent": "worker", "task": "inspect source.png", "expected_output": "visible evidence"}, "delegate-inspect"))
					}
					return true
				}
			})
			app := f.startApp()
			task := newParityTask(t, app, nil)
			if e := os.WriteFile(filepath.Join(S(task["workspace"]), "source.png"), testPNG(t, 25, 15, false), 0600); e != nil {
				t.Fatal(e)
			}
			submitTask(t, app.Manager, S(task["id"]), "Ask worker to inspect source.png")
			waitIdle(t, app.Manager, S(task["id"]))
			if visionCalls.Load() != 1 || reject != sawFailure.Load() || !strings.Contains(string(JSON(taskMessages(t, app.Store, S(task["id"])))), "PARENT_DONE") {
				t.Fatal("native child inspection", visionCalls.Load(), sawFailure.Load(), JSON(taskMessages(t, app.Store, S(task["id"]))))
			}
		})
	}
}

func TestInspectionUploadTextOnlyAndPermissions(t *testing.T) {
	f := newPlatformFixture(t, 5)
	c := inspectionConfig(f, "OPENAI_CHAT_COMPLETIONS", "OPENAI_CHAT_COMPLETIONS")
	f.save(c)
	app := f.startApp()
	task := newParityTask(t, app, nil)
	upload, e := app.Files.Upload(task, uploadData("image.png", testPNG(t, 20, 12, false)))
	if e != nil {
		t.Fatal(e)
	}
	task = mustUpdate(t, app.Manager, S(task["id"]), M{"turnId": UUID()})
	native, blocks, e := app.Files.Prepare(task, "What is visible?", []string{S(upload["id"])})
	if e != nil || Has(native, "multi_content") || !strings.Contains(S(native["content"]), inspectionTool) || len(blocks) != 2 {
		t.Fatal("text-only upload contract", native, e)
	}
	headers, finish, e := app.Local.Begin(context.Background(), task)
	if e != nil {
		t.Fatal(e)
	}
	defer finish()
	turn := app.Local.turns[headers["X-Tuck-Turn"]]
	args := M{"attachments": inspectionTestRefs([]string{S(upload["path"])}), "request": "Inspect"}
	if result := app.Local.inspectAttachments(context.Background(), turn, "unknown-agent", args); result["code"] != "tool_not_permitted" {
		t.Fatal(result)
	}
	for _, invalid := range []M{{}, {"attachments": []any{}, "request": "x"}, {"attachments": []string{"x"}, "request": " "}, Merge(args, M{"model": "vision"})} {
		if result := app.Local.inspectAttachments(context.Background(), turn, "root", invalid); result["code"] != "invalid_arguments" {
			t.Fatal(result)
		}
	}
	// Valid signatures must not turn decode failures into unclassified tool errors.
	oversized := testPNG(t, 20, 12, false)
	binary.BigEndian.PutUint32(oversized[16:20], 40000001)
	binary.BigEndian.PutUint32(oversized[29:33], crc32.ChecksumIEEE(oversized[12:29]))
	for name, raw := range map[string][]byte{"truncated.png": testPNG(t, 20, 12, false)[:16], "oversized.png": oversized} {
		if e := os.WriteFile(filepath.Join(S(task["workspace"]), name), raw, 0600); e != nil {
			t.Fatal(e)
		}
		result := app.Local.inspectAttachments(context.Background(), turn, "root", M{"attachments": inspectionTestRefs([]string{name}), "request": "Inspect"})
		if result["code"] != "attachment_invalid_image" || Has(result, "reason") || result["status"] != "failed" {
			t.Fatal("invalid image error contract", name, result)
		}
	}
	c = app.Models.Config()
	O(O(c["agents"])["root"])["disabledTuckTools"] = []string{inspectionTool}
	if e := app.Models.SetConfig(c); e != nil {
		t.Fatal(e)
	}
	if result := app.Local.inspectAttachments(context.Background(), turn, "root", args); result["code"] != "tool_not_permitted" {
		t.Fatal(result)
	}
	if _, _, e = app.Files.Prepare(task, "x", []string{S(upload["id"])}); Code(e) != "image_unsupported" {
		t.Fatal("disabled inspection accepted image", e)
	}
	finish()
	if result := app.Local.inspectAttachments(context.Background(), turn, "root", args); result["code"] != "tool_not_permitted" {
		t.Fatal(result)
	}
}

func TestNativeTextOnlyToolImageBehavior(t *testing.T) {
	f := newPlatformFixture(t, 5)
	c := f.models.Config()
	c["providers"], c["defaultModelId"] = []any{localProvider(f.server.URL)}, "proof-model"
	localImageConfig(c)
	// The image resource references the existing provider, but the chat model
	// deliberately declares no IMAGE capability.
	O(A(c["imageGenerators"])[0])["providerId"] = "configured"
	// An explicit Agent resource must be used even when the global choice is unavailable.
	O(O(c["agents"])["root"])["imageGenerator"] = M{"source": "local", "imageId": "poster"}
	c["defaultImageGenerator"] = M{"source": "local", "imageId": "missing"}
	f.save(c)
	raw := testPNG(t, 30, 20, false)
	var sawTool atomic.Bool
	f.change(func() {
		f.hook = func(w http.ResponseWriter, r *http.Request, body M) bool {
			if strings.HasSuffix(r.URL.Path, "/images/generations") {
				if body["model"] != "image-model" {
					t.Error("Agent image override was not used", body["model"])
				}
				WriteJSON(w, 200, M{"data": []any{M{"b64_json": base64.StdEncoding.EncodeToString(raw)}}})
				return true
			}
			if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
				return false
			}
			encoded := string(JSON(body))
			if strings.Contains(encoded, "data:image") {
				t.Error("official text-only configuration unexpectedly forwarded image pixels")
			}
			if strings.Contains(encoded, "prompt_path") {
				sawTool.Store(true)
				nativeDelta(w, M{"content": "Only file metadata was received"})
			} else {
				nativeDelta(w, nativeCall("generate_images", M{"prompt": "A blue square"}, "generate"))
			}
			return true
		}
	})
	app := f.startApp()
	task := newParityTask(t, app, nil)
	submitTask(t, app.Manager, S(task["id"]), "Generate a square")
	waitIdle(t, app.Manager, S(task["id"]))
	if !sawTool.Load() {
		t.Fatal("native tool did not finish", JSON(taskMessages(t, app.Store, S(task["id"]))))
	}
	native, e := app.Manager.options.Engine.Request(context.Background(), "GET", nativePath(task, ""), nil)
	if e != nil || !strings.Contains(string(JSON(native)), "data:image") {
		t.Fatal("public native history should retain tool image", e)
	}
}

func TestNativeDeclaredImageRejected(t *testing.T) {
	for _, protocol := range []string{"OPENAI_CHAT_COMPLETIONS", "OPENAI_RESPONSES"} {
		t.Run(protocol, func(t *testing.T) {
			f := newPlatformFixture(t, 5)
			c := inspectionConfig(f, protocol, "OPENAI_CHAT_COMPLETIONS")
			O(A(O(A(c["providers"])[0])["models"])[0])["inputModalities"] = []string{"TEXT", "IMAGE"}
			f.save(c)
			var sawPixels atomic.Bool
			f.change(func() {
				f.hook = func(w http.ResponseWriter, r *http.Request, body M) bool {
					if strings.Contains(string(JSON(body)), "data:image/") {
						sawPixels.Store(true)
					}
					WriteJSON(w, 400, M{"error": M{"code": "unsupported_image", "message": "This chat model does not accept images"}})
					return true
				}
			})
			app := f.startApp()
			task := newParityTask(t, app, nil)
			pic, e := app.Files.Upload(task, uploadData("image.png", testPNG(t, 20, 12, false)))
			if e != nil {
				t.Fatal(e)
			}
			id := S(task["id"])
			if _, e = app.Manager.Submit(id, "Describe the uploaded image", []string{S(pic["id"])}, nil, nil); e != nil {
				t.Fatal(e)
			}
			failure := waitIdle(t, app.Manager, id)
			if !sawPixels.Load() || S(failure["lastTurnReason"]) != "error" || !strings.Contains(S(O(failure["lastFailure"])["message"]), "does not accept images") {
				t.Fatal("native image rejection must remain an explicit model failure", failure)
			}
		})
	}
}

func TestInspectionCancellation(t *testing.T) {
	f := newPlatformFixture(t, 5)
	c := inspectionConfig(f, "OPENAI_CHAT_COMPLETIONS", "OPENAI_CHAT_COMPLETIONS")
	f.save(c)
	started := make(chan struct{})
	f.change(func() {
		f.hook = func(w http.ResponseWriter, r *http.Request, _ M) bool {
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"choices":`))
			w.(http.Flusher).Flush()
			close(started)
			<-r.Context().Done()
			return true
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	model, _ := f.models.InspectionModel("root")
	done := make(chan error, 1)
	go func() { _, e := f.models.InspectAttachments(ctx, c, nil, model, "inspect", nil); done <- e }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("provider not entered")
	}
	cancel()
	select {
	case e := <-done:
		if Code(e) != "operation_canceled" {
			t.Fatal(e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancellation blocked")
	}
}

func inspectionTestRefs(paths any) []any {
	result := []any{}
	for _, p := range A(paths) {
		result = append(result, M{"path": p})
	}
	return result
}
