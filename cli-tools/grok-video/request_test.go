package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func baseOptions() createOptions {
	return createOptions{model: "grok-imagine-video-1.5", prompt: "A red ball rolls.", duration: 8, audio: "auto"}
}

func TestGenerationWireContract(t *testing.T) {
	o := baseOptions()
	o.image = "file-id:first"
	o.lastFrame = "https://example.com/last.png"
	o.images = stringsFlag{"https://example.com/character.png"}
	o.voices = stringsFlag{"eve", "leo"}
	o.audios = stringsFlag{"https://example.com/voice.wav"}
	o.keyframes = stringsFlag{"2=file-id:middle", "4=https://example.com/middle.png"}
	o.resolution, o.aspectRatio, o.audio, o.user = "720p", "9:16", "off", "user-1"
	o.uploadURL = "https://store.example.com/result?signature=test"
	o.storeFilename, o.storeExpires, o.storePublic, o.publicExpires = "result.mp4", 7200, true, 3600
	p, err := buildRequest("generate", o, strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(p)
	var wire map[string]any
	json.Unmarshal(b, &wire)
	if wire["generate_audio"] != false || wire["duration"] != float64(8) || wire["user"] != "user-1" {
		t.Fatalf("wrong scalar fields: %s", b)
	}
	if wire["image"].(map[string]any)["file_id"] != "first" || wire["last_frame"].(map[string]any)["url"] != o.lastFrame {
		t.Fatalf("wrong frame contract: %s", b)
	}
	voices := wire["reference_audios"].([]any)
	if len(voices) != 3 || voices[0].(map[string]any)["voice_id"] != "eve" || voices[2].(map[string]any)["url"] != o.audios[0] {
		t.Fatalf("audio order/contract: %s", b)
	}
	frames := wire["keyframes"].([]any)
	if frames[0].(map[string]any)["timestamp_s"] != float64(2) {
		t.Fatalf("keyframe timestamp: %s", b)
	}
	storage := wire["storage_options"].(map[string]any)
	if storage["public_url"].(map[string]any)["expires_after"] != float64(3600) || wire["output"].(map[string]any)["upload_url"] != o.uploadURL {
		t.Fatalf("storage contract: %s", b)
	}
}

func TestRequestValidation(t *testing.T) {
	cases := []struct {
		name string
		edit func(*createOptions)
	}{
		{"empty model", func(o *createOptions) { o.model = " " }},
		{"missing prompt", func(o *createOptions) { o.prompt = " " }},
		{"conflicting prompts", func(o *createOptions) { o.promptFile = "-" }},
		{"zero duration", func(o *createOptions) { o.duration = 0 }},
		{"long duration", func(o *createOptions) { o.duration = 16 }},
		{"bad ratio", func(o *createOptions) { o.aspectRatio = "12:7" }},
		{"bad size", func(o *createOptions) { o.resolution = "4k" }},
		{"bad audio", func(o *createOptions) { o.audio = "false" }},
		{"classic HD", func(o *createOptions) { o.model, o.resolution = "grok-imagine-video", "1080p" }},
		{"classic tail", func(o *createOptions) { o.model, o.lastFrame = "grok-imagine-video", "file-id:tail" }},
		{"classic voices", func(o *createOptions) { o.model = "grok-imagine-video"; o.voices = stringsFlag{"eve"} }},
		{"classic long reference", func(o *createOptions) {
			o.model, o.duration = "grok-imagine-video", 11
			o.images = stringsFlag{"file-id:a"}
		}},
		{"classic first plus reference", func(o *createOptions) {
			o.model, o.image = "grok-imagine-video", "file-id:first"
			o.images = stringsFlag{"file-id:a"}
		}},
		{"reference HD", func(o *createOptions) { o.resolution = "1080p"; o.images = stringsFlag{"file-id:a"} }},
		{"lite references", func(o *createOptions) { o.model = "grok-imagine-video-1.5-lite"; o.images = stringsFlag{"file-id:a"} }},
		{"too many references", func(o *createOptions) {
			for range 15 {
				o.images = append(o.images, "file-id:a")
			}
		}},
		{"too many classic references", func(o *createOptions) {
			o.model = "grok-imagine-video"
			for range 8 {
				o.images = append(o.images, "file-id:a")
			}
		}},
		{"too many voices", func(o *createOptions) { o.voices = stringsFlag{"a", "b", "c", "d"} }},
		{"same frame slot", func(o *createOptions) { o.keyframes = stringsFlag{"1=file-id:a", "1.01=file-id:b"} }},
		{"frame zero", func(o *createOptions) { o.keyframes = stringsFlag{"0=file-id:a"} }},
		{"frame at end", func(o *createOptions) { o.keyframes = stringsFlag{"8=file-id:a"} }},
		{"frame nan", func(o *createOptions) { o.keyframes = stringsFlag{"NaN=file-id:a"} }},
		{"frame format", func(o *createOptions) { o.keyframes = stringsFlag{"1"} }},
		{"storage no name", func(o *createOptions) { o.storePublic = true }},
		{"storage short ttl", func(o *createOptions) { o.storeFilename = "a"; o.storeExpires = 10 }},
		{"public without enable", func(o *createOptions) { o.storeFilename = "a"; o.publicExpires = 3600 }},
		{"public outlives file", func(o *createOptions) {
			o.storeFilename = "a"
			o.storePublic = true
			o.storeExpires, o.publicExpires = 3600, 7200
		}},
		{"bad upload url", func(o *createOptions) { o.uploadURL = "file:///secret" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := baseOptions()
			tc.edit(&o)
			if _, err := buildRequest("generate", o, strings.NewReader("prompt")); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestEditExtendAndPromptInputs(t *testing.T) {
	for _, command := range []string{"edit", "extend"} {
		o := createOptions{model: "grok-imagine-video", video: "file-id:source", promptFile: "-", duration: 3}
		p, err := buildRequest(command, o, strings.NewReader("\ufeff继续镜头"))
		if err != nil {
			t.Fatal(err)
		}
		if p["prompt"] != "继续镜头" || p["video"].(map[string]any)["file_id"] != "source" {
			t.Fatal(p)
		}
		_, hasDuration := p["duration"]
		if hasDuration != (command == "extend") {
			t.Fatal(p)
		}
		o.model = "grok-imagine-video-1.5"
		if _, err := buildRequest(command, o, strings.NewReader("test")); err == nil {
			t.Fatal("1.5 edit/extend allowed")
		}
		o.model, o.video = "grok-imagine-video", ""
		if _, err := buildRequest(command, o, strings.NewReader("test")); err == nil {
			t.Fatal("missing video allowed")
		}
	}
	o := baseOptions()
	o.prompt = ""
	o.image = "file-id:first"
	if _, err := buildRequest("generate", o, nil); err != nil {
		t.Fatal(err)
	}
	o = baseOptions()
	o.prompt, o.model = "", "future-video-model"
	o.promptFile = filepath.Join(t.TempDir(), "prompt.txt")
	os.WriteFile(o.promptFile, []byte("camera moves\n"), 0600)
	p, err := buildRequest("generate", o, nil)
	if err != nil || p["model"] != o.model || p["prompt"] != "camera moves\n" {
		t.Fatalf("%v %v", p, err)
	}
}

func TestMediaSources(t *testing.T) {
	png := []byte{137, 80, 78, 71, 13, 10, 26, 10, 0, 0, 0, 13}
	path := filepath.Join(t.TempDir(), "input.png")
	os.WriteFile(path, png, 0600)
	v, err := mediaSource(path, "image")
	if err != nil {
		t.Fatal(err)
	}
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
	if v["url"] != dataURL {
		t.Fatal(v)
	}
	if _, err := mediaSource(dataURL, "image"); err != nil {
		t.Fatal(err)
	}
	for _, src := range []string{"data:image/png;base64,###", "data:audio/wav;base64,AAAA", "file-id:", "https://name:secret@example.com/a.png", "https://example.com/a.png#part", filepath.Join(t.TempDir(), "missing")} {
		if _, err := mediaSource(src, "image"); err == nil {
			t.Errorf("accepted %q", src)
		}
	}
	if _, err := mediaSource("file-id:audio", "audio"); err == nil {
		t.Fatal("audio file_id accepted")
	}
	var repeat stringsFlag
	if repeat.Set("") == nil {
		t.Fatal("empty repeated source accepted")
	}
}
