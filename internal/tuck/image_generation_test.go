package tuck

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func localImageConfig(c M) M {
	c["imageGenerators"] = []any{M{"imageId": "poster", "providerId": "local", "displayName": "Poster", "enabled": true, "protocol": imageProtocol, "upstreamModelKey": "image-model"}}
	c["defaultImageGenerator"] = M{"source": "local", "imageId": "poster"}
	return c
}

func TestImageResourceConfiguration(t *testing.T) {
	f := newPlatformFixture(t, 5)
	c := f.models.Config()
	p := localProvider(f.server.URL)
	p["id"] = "local"
	c["providers"] = []any{p}
	localImageConfig(c)
	f.save(c)
	view, e := RuntimeView(f.config, f.models, []string{"providers"})
	if e != nil || Has(O(view["config"]), "imageGenerators") || Has(O(view["config"]), "defaultImageGenerator") || len(A(view["imageCatalog"])) != 1 {
		t.Fatal(view, e)
	}
	choice := O(A(view["imageCatalog"])[0])
	if !noUnknown(choice, "source", "imageId", "displayName", "selectable", "reason") || choice["imageId"] != "poster" || !B(choice["selectable"]) {
		t.Fatal("locked settings must expose only resource choices", choice)
	}
	_, _, _, e = PrepareRuntime(f.config, f.models, M{"revision": view["revision"], "config": M{"imageGenerators": []any{}}}, []string{"providers"})
	if Code(e) != "settings_locked" {
		t.Fatal(e)
	}
	for _, mutate := range []func(M){
		func(c M) { O(A(c["imageGenerators"])[0])["protocol"] = "future" },
		func(c M) { O(A(c["imageGenerators"])[0])["providerId"] = "missing" },
		func(c M) { O(A(c["imageGenerators"])[0])["maxImagesPerRequest"] = 7 },
		func(c M) { O(A(c["imageGenerators"])[0])["allowedSizes"] = []string{"bad"} },
		func(c M) { c["defaultImageGenerator"] = "poster" },
		func(c M) { O(O(c["agents"])["root"])["imageGenerator"] = "poster" },
		func(c M) { O(O(c["agents"])["root"])["imageGenerator"] = M{"source": "local"} },
		func(c M) { O(O(c["agents"])["root"])["imageGenerator"] = M{"source": "other", "imageId": "poster"} },
		func(c M) { O(A(c["imageGenerators"])[0])["responseFormat"] = true },
	} {
		bad := Clone(c)
		mutate(bad)
		if ValidateConfig(bad, false) == nil {
			t.Fatal("accepted invalid image configuration")
		}
	}
	c["defaultImageGenerator"] = M{"source": "platform", "imageId": "poster"}
	if _, e = resolveImage(c, nil, ""); e == nil {
		t.Fatal("cross-source fallback")
	}
	if _, e = resolveImage(c, nil, "local:poster"); e != nil {
		t.Fatal(e)
	}
}

func TestAgentImageSelection(t *testing.T) {
	c := localImageConfig(M{"agents": M{"root": M{}}, "providers": []any{M{"id": "local", "enabled": true}}})
	a := O(O(c["agents"])["root"])
	resolve := func() (M, error) { return resolveImage(c, nil, agentImageSelector(c, "root")) }
	if r, e := resolve(); e != nil || r["selector"] != "local:poster" {
		t.Fatal("inherit global", r, e)
	}
	a["imageGenerator"] = M{"source": "platform", "imageId": "poster"}
	if _, e := resolve(); Code(e) != "image_resource_unavailable" {
		t.Fatal("unavailable explicit override must not fall back", e)
	}
	a["imageGenerator"] = M{}
	if _, e := resolve(); e != nil {
		t.Fatal("empty override inherits", e)
	}
	a["imageGenerator"] = M{"source": "local", "imageId": "poster"}
	c["defaultImageGenerator"] = M{"source": "platform", "imageId": "missing"}
	if _, e := resolve(); e != nil {
		t.Fatal("override does not require a valid global default", e)
	}
	O(A(c["imageGenerators"])[0])["enabled"] = false
	if _, e := resolve(); Code(e) != "image_resource_unavailable" {
		t.Fatal("disabled override must fail", e)
	}
}

func TestImageURLNetworkBoundary(t *testing.T) {
	for _, address := range []string{"127.0.0.1", "::ffff:127.0.0.1", "::1", "10.1.2.3", "172.16.0.1", "192.168.1.1", "169.254.169.254", "100.100.100.200", "198.18.0.1", "192.0.2.1", "0.0.0.1", "240.1.1.1", "fc00::1", "64:ff9b::7f00:1", "2001:db8::1", "2002:7f00:1::"} {
		if publicImageIP(net.ParseIP(address)) {
			t.Fatal("allowed non-public image host", address)
		}
	}
	for _, address := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111"} {
		if !publicImageIP(net.ParseIP(address)) {
			t.Fatal("rejected public host", address)
		}
	}
}

func TestImageGenerationProtocolAndFailures(t *testing.T) {
	f := newPlatformFixture(t, 5)
	c := f.models.Config()
	p := localProvider(f.server.URL)
	p["id"] = "local"
	c["providers"] = []any{p}
	localImageConfig(c)
	f.save(c)
	raw := testPNG(t, 2500, 100, false)
	var calls atomic.Int32
	f.change(func() {
		f.hook = func(w http.ResponseWriter, r *http.Request, body M) bool {
			if !strings.HasSuffix(r.URL.Path, "/images/generations") {
				return false
			}
			calls.Add(1)
			if body["model"] != "image-model" || Has(body, "response_format") || I(body["n"]) != 1 || Has(body, "size") || Has(body, "resolution") {
				t.Error("wrong request", body)
			}
			WriteJSON(w, 200, M{"data": []any{M{"b64_json": base64.StdEncoding.EncodeToString(raw)}}})
			return true
		}
	})
	input := ImageGenerationRequest{Prompt: "poster", Count: 1}
	result, e := f.models.GenerateImage(context.Background(), c, nil, input)
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(result.Images[0].Bytes, raw) || bytes.Equal(result.Images[0].ModelBytes, raw) {
		t.Fatal("original was changed or model image not prepared")
	}
	for _, in := range []ImageGenerationRequest{{Prompt: "poster", Count: 5}, {Prompt: "", Count: 1}, {Prompt: "poster", Count: 0}} {
		if _, e = f.models.GenerateImage(context.Background(), c, nil, in); Code(e) != "invalid_arguments" {
			t.Fatal(e)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("invalid input reached provider")
	}
	for _, payload := range []M{{"data": []any{}}, {"data": []any{M{"b64_json": "not base64"}}}, {"data": []any{M{"b64_json": base64.StdEncoding.EncodeToString([]byte("not an image"))}}}, {"data": []any{M{"url": "https://127.0.0.1/private"}}}} {
		f.change(func() {
			f.hook = func(w http.ResponseWriter, r *http.Request, b M) bool { WriteJSON(w, 200, payload); return true }
		})
		if _, e = f.models.GenerateImage(context.Background(), c, nil, input); e == nil {
			t.Fatal("accepted invalid output")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = f.models.GenerateImage(ctx, c, nil, input); e == nil {
		t.Fatal("ignored cancellation")
	}
}

func TestImageGenerationPlatformAdmission(t *testing.T) {
	f := newPlatformFixture(t, 5)
	f.enroll()
	raw := testPNG(t, 20, 12, false)
	var calls atomic.Int32
	f.change(func() {
		f.hook = func(w http.ResponseWriter, r *http.Request, b M) bool {
			if !strings.HasSuffix(r.URL.Path, "/images/generations") {
				return false
			}
			calls.Add(1)
			if !strings.Contains(r.URL.Path, "/resources/img_") || r.Header.Get("X-Measix-Managed-Generation") == "" || r.Header.Get("Authorization") == "" || b["model"] != "gpt-image-1" || b["size"] != "auto" {
				t.Error("platform contract", r.URL.Path, b)
			}
			WriteJSON(w, 200, M{"data": []any{M{"b64_json": base64.StdEncoding.EncodeToString(raw)}}})
			return true
		}
	})
	in := ImageGenerationRequest{Prompt: "poster", Count: 1}
	if _, e := f.models.GenerateImage(context.Background(), f.models.Config(), f.platform.Binding(), in); e != nil {
		t.Fatal(e)
	}
	f.change(func() { O(f.controls["state"])["runtimeBlocked"] = true })
	if _, e := f.models.GenerateImage(context.Background(), f.models.Config(), f.platform.Binding(), in); e == nil {
		t.Fatal("bypassed admission")
	}
	if calls.Load() != 1 {
		t.Fatal("blocked call reached image service")
	}
	if _, e := f.models.GenerateImage(context.Background(), f.models.Config(), f.platform.Binding(), ImageGenerationRequest{Prompt: "references", Count: 1, References: []ImageReference{{Bytes: raw, MIME: "image/png"}}}); Code(e) != "image_references_unsupported" {
		t.Fatal("inferred an unpublished platform edit route", e)
	}
	if _, e := f.models.GenerateImage(context.Background(), f.models.Config(), f.platform.Binding(), ImageGenerationRequest{Prompt: "count", Count: 5}); Code(e) != "invalid_arguments" {
		t.Fatal("accepted more than four images", e)
	}
	binding := f.platform.Binding()
	r := O(A(O(binding["snapshot"])["imageGenerators"])[0])
	r["maxImagesPerRequest"] = 1
	if _, e := f.models.GenerateImage(context.Background(), f.models.Config(), binding, ImageGenerationRequest{Prompt: "count", Count: 2}); Code(e) != "invalid_arguments" {
		t.Fatal("ignored platform count limit", e)
	}
	r["source"] = "platform"
	properties := O(O(modelContent.Tools([]string{"generate_images"}, r)[0]["inputSchema"])["properties"])
	if I(O(properties["count"])["maximum"]) != 1 || I(O(properties["reference_images"])["maxItems"]) != 4 || Has(properties, "size") || Has(properties, "resource") {
		t.Fatal("schema did not reflect platform limits", properties)
	}
}

func TestImageReferenceValidationAndNoFallback(t *testing.T) {
	f := newPlatformFixture(t, 5)
	c := f.models.Config()
	p := localProvider(f.server.URL)
	p["id"] = "local"
	c["providers"] = []any{p}
	localImageConfig(c)
	f.save(c)
	var calls atomic.Int32
	f.change(func() {
		f.hook = func(w http.ResponseWriter, r *http.Request, _ M) bool {
			calls.Add(1)
			if r.URL.Path != "/v1/images/edits" {
				t.Error("unexpected fallback route", r.URL.Path)
			}
			WriteJSON(w, 400, M{"error": M{"code": "unsupported_references", "message": "Image edits are not supported"}})
			return true
		}
	})
	app := f.startApp()
	task := newParityTask(t, app, M{})
	task = mustUpdate(t, app.Manager, S(task["id"]), M{"turnId": UUID()})
	headers, finish, e := app.Local.Begin(context.Background(), task)
	if e != nil {
		t.Fatal(e)
	}
	defer finish()
	turn := app.Local.turns[headers["X-Tuck-Turn"]]
	workspace := S(task["workspace"])
	raw := testPNG(t, 20, 12, false)
	for name, data := range map[string][]byte{"valid.png": raw, "fake.png": []byte("not an image"), "truncated.png": raw[:20]} {
		if e := os.WriteFile(filepath.Join(workspace, name), data, 0600); e != nil {
			t.Fatal(e)
		}
	}
	if e := os.Mkdir(filepath.Join(workspace, "folder.png"), 0700); e != nil {
		t.Fatal(e)
	}
	oversize, e := os.Create(filepath.Join(workspace, "large.png"))
	if e != nil {
		t.Fatal(e)
	}
	e = oversize.Truncate(MaxUploadBytes + 1)
	oversize.Close()
	if e != nil {
		t.Fatal(e)
	}
	invalid := []M{
		{"count": 0}, {"count": 5}, {"count": 1.5}, {"size": "auto"}, {"resource": "local:poster"},
		{"reference_images": "valid.png"}, {"reference_images": []any{1}},
		{"reference_images": []string{"valid.png", "valid.png"}},
		{"reference_images": []string{"1.png", "2.png", "3.png", "4.png", "5.png"}},
	}
	for _, path := range []string{"../outside.png", filepath.Join(workspace, "valid.png"), "https://example.com/a.png", "data:image/png;base64,AAAA", "folder.png", "fake.png", "truncated.png", "large.png", "missing.png", "folder/../valid.png"} {
		invalid = append(invalid, M{"reference_images": []string{path}})
	}
	out := filepath.Join(t.TempDir(), "outside.png")
	if e := os.WriteFile(out, raw, 0600); e != nil {
		t.Fatal(e)
	}
	// Junctions are available on Windows without symlink privilege.
	link := filepath.Join(workspace, "outside")
	if e := directoryLink(filepath.Dir(out), link); e != nil {
		t.Fatal(e)
	}
	invalid = append(invalid, M{"reference_images": []string{"outside/outside.png"}})
	for _, args := range invalid {
		result := app.Local.generateImage(turn.ctx, turn, Merge(M{"prompt": "reference proof"}, args))
		if !result.IsError {
			t.Fatalf("accepted invalid arguments: %s", JSON(args))
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid references reached provider", calls.Load())
	}
	if e := os.Link(filepath.Join(workspace, "valid.png"), filepath.Join(workspace, "alias.png")); e == nil {
		result := app.Local.generateImage(turn.ctx, turn, M{"prompt": "duplicate", "reference_images": []string{"valid.png", "alias.png"}})
		if !result.IsError || calls.Load() != 0 {
			t.Fatal("accepted duplicate file identity")
		}
	}
	ctx, cancel := context.WithCancel(turn.ctx)
	cancel()
	if result := app.Local.generateImage(ctx, turn, M{"prompt": "canceled", "reference_images": []string{"valid.png"}}); !result.IsError || calls.Load() != 0 {
		t.Fatal("canceled reference reached provider")
	}
	result := app.Local.generateImage(turn.ctx, turn, M{"prompt": "reference proof", "reference_images": []string{"valid.png"}})
	if !result.IsError || calls.Load() != 1 || !strings.Contains(string(JSON(result.StructuredContent)), "unsupported_references") {
		t.Fatal("upstream rejection was lost or retried", result, calls.Load())
	}
	if _, e := os.Stat(filepath.Join(workspace, "artifacts", "images")); !os.IsNotExist(e) {
		t.Fatal("failed edit published artifacts", e)
	}
	if got, e := os.ReadFile(filepath.Join(workspace, "valid.png")); e != nil || !bytes.Equal(got, raw) {
		t.Fatal("reference file changed", e)
	}
}

func TestImagePlatformRuntimePath(t *testing.T) {
	f := newPlatformFixture(t, 5)
	f.enroll()
	for _, path := range []string{"//other/images", "/../images", "/%2e%2e/images", "/images\\generations", "/images?secret=1"} {
		binding := f.platform.Binding()
		snapshot := O(binding["snapshot"])
		resource := O(A(snapshot["imageGenerators"])[0])
		resource["runtimePath"] = path
		if e := snapshotClosure(snapshot); Code(e) != "invalid_platform_data" {
			t.Fatalf("accepted image runtime path %q: %v", path, e)
		}
		resource["source"] = "platform"
		if _, _, e := f.models.resourceAccess(context.Background(), f.models.Config(), binding, resource); Code(e) != "invalid_platform_data" {
			t.Fatalf("request accepted image runtime path %q: %v", path, e)
		}
	}
}

func TestImageCancellationDuringResponseBody(t *testing.T) {
	for _, status := range []int{200, 502} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			f := newPlatformFixture(t, 5)
			c := f.models.Config()
			p := localProvider(f.server.URL)
			p["id"] = "local"
			c["providers"] = []any{p}
			localImageConfig(c)
			started := make(chan struct{})
			f.change(func() {
				f.hook = func(w http.ResponseWriter, r *http.Request, _ M) bool {
					w.WriteHeader(status)
					_, _ = w.Write([]byte(`{"data":`))
					w.(http.Flusher).Flush()
					close(started)
					<-r.Context().Done()
					return true
				}
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			result := make(chan error, 1)
			go func() {
				_, e := f.models.GenerateImage(ctx, c, nil, ImageGenerationRequest{Prompt: "cancel body", Count: 1})
				result <- e
			}()
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("response body not started")
			}
			cancel()
			select {
			case e := <-result:
				if Code(e) != "operation_canceled" {
					t.Fatalf("cancellation reported as response failure: %v", e)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("response body cancellation did not finish")
			}
		})
	}
}

func TestImageGenerationNativeContext(t *testing.T) {
	for _, protocol := range []string{"OPENAI_CHAT_COMPLETIONS", "OPENAI_RESPONSES"} {
		for _, referenceCount := range []int{0, 1, 4} {
			t.Run(fmt.Sprintf("%s/references%d", protocol, referenceCount), func(t *testing.T) {
				f := newPlatformFixture(t, 5)
				c := f.models.Config()
				p := localProvider(f.server.URL)
				p["id"] = "local"
				p["protocol"] = protocol
				O(A(p["models"])[0])["inputModalities"] = []string{"TEXT", "IMAGE"}
				c["providers"] = []any{p}
				c["defaultModelId"] = "proof-model"
				localImageConfig(c)
				f.save(c)
				raw := testPNG(t, 64, 40, false)
				modelImage, _, err := PrepareModelImage(raw)
				if err != nil {
					t.Fatal(err)
				}
				count := 1
				if referenceCount == 4 {
					count = 4
				}
				var references []any
				var referenceBytes [][]byte
				for i := 0; i < referenceCount; i++ {
					references = append(references, fmt.Sprintf("reference%d.png", i))
					referenceBytes = append(referenceBytes, testPNG(t, 20+i, 12, false))
				}
				args := M{"prompt": "create poster", "count": nil, "reference_images": nil}
				if referenceCount > 0 {
					args["reference_images"] = references
					args["count"] = count
				}
				var sawImage atomic.Bool
				var generationCalls atomic.Int32
				f.change(func() {
					f.hook = func(w http.ResponseWriter, r *http.Request, body M) bool {
						if strings.HasSuffix(r.URL.Path, "/images/generations") || strings.HasSuffix(r.URL.Path, "/images/edits") {
							generationCalls.Add(1)
							if referenceCount > 0 {
								if !strings.HasSuffix(r.URL.Path, "/images/edits") {
									t.Error("reference call used generations")
								}
								if err := r.ParseMultipartForm(1 << 20); err != nil {
									t.Error(err)
									WriteJSON(w, 400, M{})
									return true
								}
								defer r.MultipartForm.RemoveAll()
								if r.FormValue("n") != fmt.Sprint(count) || r.FormValue("model") != "image-model" || r.FormValue("prompt") != "create poster" || len(r.MultipartForm.Value) != 3 {
									t.Error("unexpected edit fields", r.MultipartForm.Value)
								}
								files := r.MultipartForm.File["image[]"]
								if len(files) != referenceCount {
									t.Error("reference count", len(files))
								}
								for i, file := range files {
									handle, err := file.Open()
									if err != nil {
										t.Error(err)
										continue
									}
									data, err := io.ReadAll(handle)
									handle.Close()
									if err != nil || i >= len(referenceBytes) || !bytes.Equal(data, referenceBytes[i]) || file.Header.Get("Content-Type") != "image/png" {
										t.Error("reference bytes/order changed", i, err)
									}
								}
							} else if len(body) != 3 || Has(body, "size") || I(body["n"]) != 1 {
								t.Error("unexpected generation fields", body)
							}
							images := []any{}
							for i := 0; i < count; i++ {
								images = append(images, M{"b64_json": base64.StdEncoding.EncodeToString(raw)})
							}
							WriteJSON(w, 200, M{"data": images})
							return true
						}
						if !strings.HasSuffix(r.URL.Path, "/responses") && !strings.HasSuffix(r.URL.Path, "/chat/completions") {
							return false
						}
						encoded := string(JSON(body))
						done := strings.Contains(encoded, "data:image/")
						if done {
							matches := regexp.MustCompile(`data:image/[^;]+;base64,([A-Za-z0-9+/=]+)`).FindAllStringSubmatch(encoded, -1)
							if len(matches) < count {
								t.Error("missing generated images in context", len(matches), count)
							}
							for _, match := range matches {
								if len(match) != 2 {
									t.Error("missing encoded pixels")
								} else if b, err := base64.StdEncoding.DecodeString(match[1]); err != nil || !bytes.Equal(b, modelImage) {
									t.Error("model received different pixels", err)
								}
							}
							sawImage.Store(true)
						}
						if protocol == "OPENAI_CHAT_COMPLETIONS" {
							if done {
								nativeDelta(w, M{"content": "Image inspected in context."})
							} else {
								nativeDelta(w, nativeCall("generate_images", args, "image-call"))
							}
						} else {
							item := M{"type": "function_call", "id": "fc_image", "call_id": "image-call", "name": "generate_images", "arguments": string(JSON(args)), "status": "completed"}
							if done {
								item = M{"type": "message", "id": "msg_image", "role": "assistant", "status": "completed", "content": []any{M{"type": "output_text", "text": "Image inspected in context.", "annotations": []any{}}}}
							}
							result := M{"id": "resp_image", "object": "response", "status": "completed", "output": []any{item}, "usage": M{"input_tokens": 100, "output_tokens": 20, "total_tokens": 120}}
							w.Header().Set("Content-Type", "text/event-stream")
							for _, event := range []M{{"type": "response.created", "response": Merge(result, M{"status": "in_progress", "output": []any{}})}, {"type": "response.output_item.added", "output_index": 0, "item": item}, {"type": "response.output_item.done", "output_index": 0, "item": item}, {"type": "response.completed", "response": result}} {
								fmt.Fprintf(w, "event: %s\ndata: %s\n\n", S(event["type"]), JSON(event))
							}
						}
						return true
					}
				})
				app := f.startApp()
				task := newParityTask(t, app, M{})
				for i, ref := range references {
					if err := os.WriteFile(filepath.Join(S(task["workspace"]), S(ref)), referenceBytes[i], 0600); err != nil {
						t.Fatal(err)
					}
				}
				submitTask(t, app.Manager, S(task["id"]), "Create a poster and inspect it.")
				finished := waitIdle(t, app.Manager, S(task["id"]))
				if !sawImage.Load() || generationCalls.Load() != 1 {
					t.Fatalf("native image path failed: calls=%d task=%s messages=%s", generationCalls.Load(), JSON(finished), JSON(taskMessages(t, app.Store, S(task["id"]))))
				}
				artifacts := A(finished["artifacts"])
				if len(artifacts) != 2*count+referenceCount {
					t.Fatal("original missing", artifacts)
				}
				var imagePath string
				for _, artifact := range artifacts {
					if strings.HasPrefix(S(O(artifact)["path"]), "artifacts/images/") && strings.HasSuffix(S(O(artifact)["path"]), ".png") {
						imagePath = S(O(artifact)["path"])
					}
				}
				original, e := os.ReadFile(filepath.Join(S(task["workspace"]), imagePath))
				if e != nil || !bytes.Equal(original, raw) {
					t.Fatal("original bytes", e)
				}
				media := false
				for _, msg := range taskMessages(t, app.Store, S(task["id"])) {
					for _, b := range A(msg["blocks"]) {
						if O(b)["type"] == "attachments" {
							media = true
							if S(O(O(O(b)["data"])["provenance"])["prompt"]) != "create poster" {
								t.Fatal("missing persisted generation prompt", b)
							}
							public := WorkbenchMessage(S(task["id"]), msg)
							if !strings.Contains(string(JSON(public)), "message-media") {
								t.Fatal("media projection")
							}
							for _, block := range A(public["blocks"]) {
								for _, item := range A(O(O(block)["data"])["items"]) {
									resp, err := http.Get(app.URL + S(O(item)["src"]))
									if err != nil {
										t.Fatal(err)
									}
									got, err := io.ReadAll(resp.Body)
									resp.Body.Close()
									if err != nil || resp.StatusCode != 200 || !bytes.Equal(got, modelImage) {
										t.Fatal("media HTTP bytes", resp.StatusCode, err)
									}
								}
							}
						}
					}
				}
				if !media {
					t.Fatal("tool media not normalized")
				}
				sawImage.Store(false)
				submitTask(t, app.Manager, S(task["id"]), "Describe the image from the preceding turn.")
				waitIdle(t, app.Manager, S(task["id"]))
				if !sawImage.Load() || generationCalls.Load() != 1 {
					t.Fatal("image was not retained for the following turn")
				}
				options, e := app.Manager.ForkOptions(context.Background(), S(task["id"]))
				if e != nil || len(options) == 0 {
					t.Fatal("fork options", e)
				}
				fork, e := app.Manager.Fork(context.Background(), S(task["id"]), I(options[len(options)-1]["index"]))
				if e != nil {
					t.Fatal("image fork", e)
				}
				child := O(fork["task"])
				mediaCount := 0
				for _, stored := range taskMessages(t, app.Store, S(child["id"])) {
					message := WorkbenchMessage(S(child["id"]), stored)
					for _, block := range A(message["blocks"]) {
						if O(block)["type"] != "attachments" {
							continue
						}
						data := O(O(block)["data"])
						if S(O(data["provenance"])["prompt"]) != "create poster" {
							t.Fatal("fork lost generation prompt", data)
						}
						mediaCount += len(A(data["items"]))
						for _, item := range A(data["items"]) {
							resp, err := http.Get(app.URL + S(O(item)["src"]))
							if err != nil {
								t.Fatal(err)
							}
							got, err := io.ReadAll(resp.Body)
							resp.Body.Close()
							if err != nil || resp.StatusCode != 200 || !bytes.Equal(got, modelImage) {
								t.Fatal("fork media HTTP bytes", resp.StatusCode, err)
							}
						}
					}
				}
				if mediaCount != count {
					t.Fatalf("fork lost images: %d expected %d", mediaCount, count)
				}
				sawImage.Store(false)
				submitTask(t, app.Manager, S(child["id"]), "Describe the image retained in this fork.")
				waitIdle(t, app.Manager, S(child["id"]))
				if !sawImage.Load() || generationCalls.Load() != 1 {
					t.Fatal("fork did not retain image context")
				}
			})
		}
	}
}

func TestImageHistoryRepeatedCallIDs(t *testing.T) {
	m, store, _, id := taskFixture(t)
	src := "data:image/png;base64," + base64.StdEncoding.EncodeToString(testPNG(t, 20, 12, false))
	history := []any{}
	for _, prompt := range []string{"first prompt", "second prompt"} {
		history = append(history,
			M{"message": M{"role": "assistant", "tool_calls": []any{M{"id": "reused", "function": M{"name": "generate_images", "arguments": string(JSON(M{"prompt": prompt}))}}}}},
			M{"message": M{"role": "tool", "tool_call_id": "reused", "multi_content": []any{M{"type": "text", "text": "generated"}, M{"type": "image_url", "image_url": M{"url": src}}}}},
		)
	}
	m.mu.Lock()
	err := m.normalizer.importHistory(m.tasks[id], history)
	m.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	var prompts []string
	for _, message := range taskMessages(t, store, id) {
		for _, b := range A(message["blocks"]) {
			block := O(b)
			if S(block["type"]) != "attachments" {
				continue
			}
			data := O(block["data"])
			prompts = append(prompts, S(O(data["provenance"])["prompt"]))
			items := A(data["items"])
			if len(items) != 1 || S(O(items[0])["src"]) != src || S(O(items[0])["mimeType"]) != "image/png" {
				t.Fatal("history media changed", data)
			}
		}
	}
	if strings.Join(prompts, "|") != "first prompt|second prompt" {
		t.Fatal("history prompt association", prompts)
	}
}

func TestImageGenerationWorkspaceModes(t *testing.T) {
	f := newPlatformFixture(t, 5)
	c := f.models.Config()
	p := localProvider(f.server.URL)
	p["id"] = "local"
	c["providers"] = []any{p}
	localImageConfig(c)
	f.save(c)
	raw := testPNG(t, 30, 20, false)
	var calls atomic.Int32
	f.change(func() {
		f.hook = func(w http.ResponseWriter, r *http.Request, body M) bool {
			if !strings.HasSuffix(r.URL.Path, "/images/generations") {
				return false
			}
			calls.Add(1)
			WriteJSON(w, 200, M{"data": []any{M{"b64_json": base64.StdEncoding.EncodeToString(raw)}}})
			return true
		}
	})
	app := f.startApp()
	shared := t.TempDir()
	paths := map[string]bool{}
	for _, mode := range []string{"auto", "specified", "specified", "none"} {
		in := M{"workspaceMode": mode}
		if mode == "specified" {
			in["workspace"] = shared
		}
		task := newParityTask(t, app, in)
		task = mustUpdate(t, app.Manager, S(task["id"]), M{"turnId": UUID()})
		headers, finish, e := app.Local.Begin(context.Background(), task)
		if e != nil {
			t.Fatal(e)
		}
		turn := app.Local.turns[headers["X-Tuck-Turn"]]
		before := calls.Load()
		result := app.Local.generateImage(turn.ctx, turn, M{"prompt": "workspace proof"})
		finish()
		if mode == "none" {
			// Managed engines have a real default directory even in none mode.
			if turn.Workspace == "" || app.Local.imageAllowed(turn) == nil || !result.IsError || calls.Load() != before {
				t.Fatal("none mode used the native directory", task, result)
			}
			if _, e = os.Stat(filepath.Join(turn.Workspace, TaskArtifactsDirectory)); !os.IsNotExist(e) {
				t.Fatal("none mode wrote generated files", e)
			}
			continue
		}
		if result.IsError || calls.Load() != before+1 {
			t.Fatal(mode, result)
		}
		root := shared
		if mode == "auto" {
			root = filepath.Join(app.Config.WorkspaceRoot(), S(task["id"]))
		}
		if S(task["workspace"]) != root {
			t.Fatal("wrong workspace", task)
		}
		file := O(A(O(result.StructuredContent)["files"])[0])
		path := filepath.Join(root, S(file["path"]))
		got, e := os.ReadFile(path)
		if e != nil || !bytes.Equal(got, raw) || paths[path] {
			t.Fatal("missing or overwritten original", mode, path, e)
		}
		paths[path] = true
		prompt, e := os.ReadFile(filepath.Join(root, S(file["prompt_path"])))
		if e != nil || string(prompt) != "workspace proof" || !regexp.MustCompile(`^artifacts/images/img_[A-Za-z0-9]{16}\.png$`).MatchString(S(file["path"])) {
			t.Fatal("prompt or filename contract", file, e)
		}
	}
}

func TestImageToolCancellationAndPermission(t *testing.T) {
	f := newWorkspaceFixture(t)
	l, turn := f.turn(t)
	var calls atomic.Int32
	started, canceled := make(chan struct{}), make(chan struct{})
	f.p.change(func() {
		f.p.hook = func(w http.ResponseWriter, r *http.Request, b M) bool {
			if !strings.HasSuffix(r.URL.Path, "/images/generations") {
				return false
			}
			calls.Add(1)
			close(started)
			<-r.Context().Done()
			close(canceled)
			return true
		}
	})
	ctx, cancel := context.WithCancel(turn.ctx)
	result := make(chan bool, 1)
	go func() { result <- l.generateImage(ctx, turn, M{"prompt": "cancel image"}).IsError }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("provider not reached")
	}
	cancel()
	select {
	case failed := <-result:
		if !failed {
			t.Fatal("canceled call succeeded")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancel not propagated")
	}
	select {
	case <-canceled:
	case <-time.After(5 * time.Second):
		t.Fatal("upstream not canceled")
	}
	files, e := l.Files.List(turn.Task)
	if e != nil || len(files) != 0 {
		t.Fatal("canceled output persisted", files, e)
	}
	c := l.Models.Config()
	O(O(c["agents"])[S(turn.Task["agent"])])["disabledTuckTools"] = []string{"generate_images"}
	if e = l.Models.SetConfig(c); e != nil {
		t.Fatal(e)
	}
	if !l.generateImage(context.Background(), turn, M{"prompt": "denied"}).IsError || calls.Load() != 1 {
		t.Fatal("tool permission bypass")
	}
}

func TestGeneratedFilesPublicationFailure(t *testing.T) {
	f, m, s, id, _ := fileFixture(t)
	task := mustTask(t, m, id)
	raw := testPNG(t, 30, 20, false)
	images := []GeneratedImage{{Bytes: raw, MIME: "image/png"}}
	root, e := openArtifactRoot(task, "images")
	if e != nil {
		t.Fatal(e)
	}
	keep, e := root.OpenFile("keep.png", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = keep.Write(raw); e != nil {
		t.Fatal(e)
	}
	keep.Close()
	root.Close()
	if _, e := s.DB.Exec("CREATE TRIGGER image_write_failure BEFORE UPDATE ON tasks BEGIN SELECT RAISE(FAIL,'fixture failure'); END"); e != nil {
		t.Fatal(e)
	}
	if _, e := f.SaveGeneratedImages(context.Background(), task, images, "saved prompt"); e == nil {
		t.Fatal("expected transaction failure")
	}
	files, e := f.List(task)
	if e != nil || len(files) != 1 || S(O(files[0])["path"]) != "artifacts/images/keep.png" {
		t.Fatal("unpublished image leaked", files, e)
	}
	if _, e = s.DB.Exec("DROP TRIGGER image_write_failure"); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = f.SaveGeneratedImages(ctx, task, images, "saved prompt"); e == nil {
		t.Fatal("cancellation ignored")
	}
	preserved, e := os.ReadFile(filepath.Join(S(task["workspace"]), "artifacts/images/keep.png"))
	if e != nil || !bytes.Equal(preserved, raw) {
		t.Fatal("rollback changed another file", e)
	}
}
