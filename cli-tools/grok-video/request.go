package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"
)

const maxInputBytes = 32 << 20
const maxRequestBytes = 128 << 20

type stringsFlag []string

func (s *stringsFlag) String() string { return strings.Join(*s, ", ") }
func (s *stringsFlag) Set(v string) error {
	if strings.TrimSpace(v) == "" {
		return fmt.Errorf("value cannot be empty")
	}
	*s = append(*s, v)
	return nil
}

type createOptions struct {
	model, prompt, promptFile, image, lastFrame, video string
	duration                                           int
	aspectRatio, resolution, audio, user               string
	images, voices, audios, keyframes                  stringsFlag
	uploadURL, storeFilename                           string
	storeExpires, publicExpires                        int
	storePublic                                        bool
}

// buildRequest encodes only the documented wire fields. No provider-specific
// fallback or model substitution happens here.
func buildRequest(command string, o createOptions, stdin io.Reader) (map[string]any, error) {
	if strings.TrimSpace(o.model) == "" {
		return nil, fmt.Errorf("--model cannot be empty")
	}
	if o.prompt != "" && o.promptFile != "" {
		return nil, fmt.Errorf("--prompt and --prompt-file are mutually exclusive")
	}
	if o.promptFile != "" {
		var r io.Reader = stdin
		if o.promptFile != "-" {
			f, err := os.Open(o.promptFile)
			if err != nil {
				return nil, fmt.Errorf("read --prompt-file: %w", err)
			}
			defer f.Close()
			r = f
		}
		b, err := readBounded(r, 1<<20)
		if err != nil {
			return nil, fmt.Errorf("read prompt (maximum 1 MiB): %w", err)
		}
		o.prompt = strings.TrimPrefix(string(b), "\ufeff")
	}
	if !utf8.ValidString(o.prompt) {
		return nil, fmt.Errorf("prompt must be valid UTF-8")
	}
	if command != "generate" && strings.TrimSpace(o.prompt) == "" {
		return nil, fmt.Errorf("--prompt or --prompt-file is required for %s", command)
	}
	p := map[string]any{"model": o.model}
	if strings.TrimSpace(o.prompt) != "" {
		p["prompt"] = o.prompt
	}
	if o.user != "" {
		p["user"] = o.user
	}
	if command == "generate" {
		if err := generationFields(p, o); err != nil {
			return nil, err
		}
	} else {
		if o.video == "" {
			return nil, fmt.Errorf("--video is required for %s", command)
		}
		if modelFamily(o.model) == "1.5" || modelFamily(o.model) == "lite" {
			return nil, fmt.Errorf("%s does not support %s; use grok-imagine-video", o.model, command)
		}
		v, err := mediaSource(o.video, "video")
		if err != nil {
			return nil, fmt.Errorf("--video: %w", err)
		}
		p["video"] = v
		if command == "extend" {
			if o.duration < 2 || o.duration > 10 {
				return nil, fmt.Errorf("--duration for extend must be 2..10 seconds (new segment only)")
			}
			p["duration"] = o.duration
		}
	}
	if o.uploadURL != "" {
		if err := validateMediaURL(o.uploadURL); err != nil {
			return nil, fmt.Errorf("--upload-url: %w", err)
		}
		p["output"] = map[string]any{"upload_url": o.uploadURL}
	}
	if o.storeFilename == "" && (o.storeExpires != 0 || o.storePublic || o.publicExpires != 0) {
		return nil, fmt.Errorf("storage options require --store-filename")
	}
	if o.storeFilename != "" {
		s := map[string]any{"filename": o.storeFilename}
		for name, n := range map[string]int{"--store-expires": o.storeExpires, "--store-public-expires": o.publicExpires} {
			if n != 0 && (n < 3600 || n > 2592000) {
				return nil, fmt.Errorf("%s must be 3600..2592000 seconds, or omitted", name)
			}
		}
		if o.storeExpires != 0 {
			s["expires_after"] = o.storeExpires
		}
		if o.publicExpires != 0 {
			if !o.storePublic {
				return nil, fmt.Errorf("--store-public-expires requires --store-public")
			}
			if o.storeExpires != 0 && o.publicExpires > o.storeExpires {
				return nil, fmt.Errorf("public URL expiry cannot exceed file expiry")
			}
			s["public_url"] = map[string]any{"expires_after": o.publicExpires}
		} else if o.storePublic {
			s["public_url"] = true
		}
		p["storage_options"] = s
	}
	b, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	if len(b) > maxRequestBytes {
		return nil, fmt.Errorf("encoded request exceeds 128 MiB; use remote URLs or file-id: inputs")
	}
	return p, nil
}

func generationFields(p map[string]any, o createOptions) error {
	if o.duration < 1 || o.duration > 15 {
		return fmt.Errorf("--duration must be 1..15 seconds")
	}
	p["duration"] = o.duration
	if o.aspectRatio != "" {
		allowed := "|1:1|16:9|9:16|4:3|3:4|3:2|2:3|21:9|5:2|"
		if !strings.Contains(allowed, "|"+o.aspectRatio+"|") {
			return fmt.Errorf("unsupported --aspect-ratio %q", o.aspectRatio)
		}
		p["aspect_ratio"] = o.aspectRatio
	}
	if o.resolution != "" {
		if o.resolution != "480p" && o.resolution != "720p" && o.resolution != "1080p" {
			return fmt.Errorf("--resolution must be 480p, 720p or 1080p")
		}
		p["resolution"] = o.resolution
	}
	switch o.audio {
	case "auto":
	case "on", "off":
		p["generate_audio"] = o.audio == "on"
	default:
		return fmt.Errorf("--audio must be auto, on or off")
	}
	references := len(o.images)+len(o.audios)+len(o.voices) > 0
	pins := o.lastFrame != "" || len(o.keyframes) > 0
	if _, ok := p["prompt"]; !ok && o.image == "" && !pins {
		return fmt.Errorf("--prompt or --prompt-file is required unless --image, --last-frame or --keyframe is provided")
	}
	family := modelFamily(o.model)
	if len(o.images) > 14 || len(o.voices)+len(o.audios) > 3 || len(o.keyframes) > 4 {
		return fmt.Errorf("at most 14 reference images, 3 reference voices/audio clips and 4 keyframes are allowed")
	}
	if family == "classic" {
		if pins || len(o.voices)+len(o.audios) > 0 {
			return fmt.Errorf("grok-imagine-video does not support last frame, keyframes or reference audio")
		}
		if len(o.images) > 7 || (references && o.duration > 10) {
			return fmt.Errorf("grok-imagine-video references allow at most 7 images and 10 seconds")
		}
		if references && o.image != "" {
			return fmt.Errorf("grok-imagine-video cannot combine --image with reference images")
		}
		if o.resolution == "1080p" {
			return fmt.Errorf("grok-imagine-video supports at most 720p")
		}
	}
	if family == "lite" && (references || pins) {
		return fmt.Errorf("grok-imagine-video-1.5-lite supports text/image generation only")
	}
	if (references || pins) && o.resolution == "1080p" {
		return fmt.Errorf("reference/pinned-frame generation supports at most 720p")
	}
	for _, item := range []struct{ field, value string }{{"image", o.image}, {"last_frame", o.lastFrame}} {
		if item.value == "" {
			continue
		}
		v, err := mediaSource(item.value, "image")
		if err != nil {
			return fmt.Errorf("%s: %w", item.field, err)
		}
		p[item.field] = v
	}
	if len(o.images) > 0 {
		list := make([]map[string]any, 0, len(o.images))
		for _, src := range o.images {
			v, err := mediaSource(src, "image")
			if err != nil {
				return fmt.Errorf("--reference-image: %w", err)
			}
			list = append(list, v)
		}
		p["reference_images"] = list
	}
	if len(o.voices)+len(o.audios) > 0 {
		list := make([]map[string]any, 0, len(o.voices)+len(o.audios))
		// Voice entries precede clip entries; this stable order is documented in help.
		for _, voice := range o.voices {
			list = append(list, map[string]any{"voice_id": voice})
		}
		for _, src := range o.audios {
			v, err := mediaSource(src, "audio")
			if err != nil {
				return fmt.Errorf("--reference-audio: %w", err)
			}
			list = append(list, v)
		}
		p["reference_audios"] = list
	}
	if len(o.keyframes) > 0 {
		list := make([]map[string]any, 0, len(o.keyframes))
		slots := map[int]bool{}
		for _, value := range o.keyframes {
			at, src, ok := strings.Cut(value, "=")
			t, err := strconv.ParseFloat(at, 64)
			if !ok || err != nil || math.IsNaN(t) || math.IsInf(t, 0) || t <= 0 || t >= float64(o.duration) {
				return fmt.Errorf("--keyframe requires SECONDS=SOURCE with 0 < seconds < duration")
			}
			slot := int(math.Round(t * 3))
			if slot <= 0 || slot >= o.duration*3 || slots[slot] {
				return fmt.Errorf("keyframes must occupy distinct interior 1/3-second slots")
			}
			slots[slot] = true
			v, err := mediaSource(src, "image")
			if err != nil {
				return fmt.Errorf("--keyframe: %w", err)
			}
			list = append(list, map[string]any{"image": v, "timestamp_s": t})
		}
		p["keyframes"] = list
	}
	return nil
}

func modelFamily(model string) string {
	switch model {
	case "grok-imagine-video":
		return "classic"
	case "grok-imagine-video-1.5", "grok-imagine-video-1.5-preview", "grok-imagine-video-1.5-2026-05-30":
		return "1.5"
	case "grok-imagine-video-1.5-lite":
		return "lite"
	}
	return "" // Unknown aliases are validated by the server, never renamed.
}

func mediaSource(src, kind string) (map[string]any, error) {
	if id, ok := strings.CutPrefix(src, "file-id:"); ok {
		if kind == "audio" || strings.TrimSpace(id) == "" || strings.ContainsAny(id, "\r\n") {
			return nil, fmt.Errorf("file-id: requires a nonempty image/video file ID; audio uses URL or local file")
		}
		return map[string]any{"file_id": id}, nil
	}
	if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
		if err := validateMediaURL(src); err != nil {
			return nil, err
		}
		return map[string]any{"url": src}, nil
	}
	if strings.HasPrefix(src, "data:") {
		header, encoded, ok := strings.Cut(src, ",")
		if !ok || !strings.HasSuffix(header, ";base64") || !validMIME(strings.TrimSuffix(strings.TrimPrefix(header, "data:"), ";base64"), kind) {
			return nil, fmt.Errorf("expected a base64 %s data URL", kind)
		}
		if base64.StdEncoding.DecodedLen(len(encoded)) > maxInputBytes {
			return nil, fmt.Errorf("input exceeds 32 MiB")
		}
		if b, err := base64.StdEncoding.DecodeString(encoded); err != nil || len(b) == 0 {
			return nil, fmt.Errorf("empty or invalid base64 input")
		}
		return map[string]any{"url": src}, nil
	}
	f, err := os.Open(src)
	if err != nil {
		return nil, fmt.Errorf("open local %s: %w", kind, err)
	}
	defer f.Close()
	if st, err := f.Stat(); err != nil || !st.Mode().IsRegular() {
		return nil, fmt.Errorf("local %s source must be a regular file", kind)
	}
	b, err := readBounded(f, maxInputBytes)
	if err != nil || len(b) == 0 {
		return nil, fmt.Errorf("local %s must be nonempty and at most 32 MiB", kind)
	}
	contentType := http.DetectContentType(b)
	if !validMIME(contentType, kind) && kind == "audio" {
		contentType = strings.Split(mime.TypeByExtension(strings.ToLower(filepath.Ext(src))), ";")[0]
	}
	if !validMIME(contentType, kind) {
		return nil, fmt.Errorf("unsupported local %s format", kind)
	}
	return map[string]any{"url": "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(b)}, nil
}

func validMIME(m, kind string) bool {
	switch kind {
	case "image":
		return m == "image/jpeg" || m == "image/png" || m == "image/webp"
	case "video":
		return m == "video/mp4"
	case "audio":
		return strings.HasPrefix(m, "audio/")
	}
	return false
}

func validateMediaURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.Fragment != "" {
		return fmt.Errorf("expected an HTTP(S) URL without userinfo or fragment")
	}
	return nil
}

func readBounded(r io.Reader, limit int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err == nil && int64(len(b)) > limit {
		err = fmt.Errorf("content exceeds %d bytes", limit)
	}
	return b, err
}
