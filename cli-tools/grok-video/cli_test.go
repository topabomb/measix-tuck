package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
)

func noEnv(string) string { return "" }

func invoke(args []string, env map[string]string) (string, string, error) {
	var out, log bytes.Buffer
	err := run(context.Background(), args, strings.NewReader("stdin prompt"), &out, &log, func(k string) string { return env[k] })
	return out.String(), log.String(), err
}

func TestCompleteOfflineHelp(t *testing.T) {
	for _, args := range [][]string{nil, {"--help"}, {"help"}, {"version"}, {"--version"}} {
		out, _, err := invoke(args, nil)
		if err != nil || out == "" {
			t.Fatalf("%v: %v", args, err)
		}
	}
	for command := range commandDetails {
		for _, args := range [][]string{{"help", command}, {command, "--help"}, {command, "-h"}} {
			out, _, err := invoke(args, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, term := range []string{"Usage:", "--base-url", "--api-key-env", "--api-key-file", "--timeout", "Exit:", "Example"} {
				if !strings.Contains(out, term) {
					t.Errorf("%s help missing %s", command, term)
				}
			}
		}
	}
	for _, command := range []string{"generate", "edit", "extend"} {
		out, _, _ := invoke([]string{command, "--help"}, nil)
		for _, term := range []string{"--dry-run", "--wait", "--output", "--store-filename", "--upload-url"} {
			if !strings.Contains(out, term) {
				t.Error(command, term)
			}
		}
	}
}

func TestEmbeddedMarkdownDocumentation(t *testing.T) {
	source, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	// The executable must not look for source files relative to its working directory.
	t.Chdir(t.TempDir())
	out, log, err := invoke([]string{"docs"}, map[string]string{"GROK_BASE_URL": "invalid-url", "GROK_API_KEY": "private-key"})
	if err != nil {
		t.Fatal(err)
	}
	if out != string(source) || log != "" {
		t.Fatal("docs must output exactly README.md bytes with no log")
	}
	for _, term := range []string{"# grok-video 完整手册", "grok-imagine-video-1.5-lite", "兼容服务", "## 协议调研", "--reference-audio", "--store-public-expires", "--api-key-file"} {
		if !strings.Contains(out, term) {
			t.Errorf("embedded documentation missing %s", term)
		}
	}
	if strings.Contains(out, "private-key") || strings.Contains(out, "invalid-url") {
		t.Fatal("docs depends on private environment")
	}
	for _, command := range []string{"generate", "edit", "extend", "status", "wait", "download", "models", "docs", "version", "help"} {
		if !strings.Contains(out, "### 命令 "+command+"\n") {
			t.Errorf("manual is missing command %s", command)
		}
	}
	// Check the authored manual against actual registered options without generating it.
	options := regexp.MustCompile(`(?m)^  (--[a-z][a-z0-9-]*|-h)(?: |\n)`)
	for command := range commandDetails {
		help, _, err := invoke([]string{command, "--help"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range options.FindAllStringSubmatch(help, -1) {
			if !strings.Contains(out, "`"+match[1]+"`") {
				t.Errorf("manual is missing %s option %s", command, match[1])
			}
		}
	}
	for _, args := range [][]string{{"docs", "--help"}, {"help", "docs"}, {"version", "--help"}, {"help", "version"}} {
		out, _, err := invoke(args, nil)
		if err != nil || !strings.Contains(out, "Usage:") {
			t.Fatal(out, err)
		}
	}
	if _, _, err := invoke([]string{"docs", "unexpected"}, nil); errorCode(err) != 2 {
		t.Fatal(err)
	}
}

type documentationErrorWriter struct{}

func (documentationErrorWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestDocsWriteFailure(t *testing.T) {
	err := run(context.Background(), []string{"docs"}, strings.NewReader(""), documentationErrorWriter{}, io.Discard, noEnv)
	if !errors.Is(err, io.ErrClosedPipe) || errorCode(err) != 1 {
		t.Fatal(err)
	}
}

func TestCLIUsageErrors(t *testing.T) {
	cases := [][]string{{"unknown"}, {"help", "x", "y"}, {"version", "x"}, {"generate", "--wat"}, {"generate", "--model"}, {"status"}, {"status", "a", "b"}, {"status", "../x"}, {"generate", "extra"}, {"download", "id"}, {"wait", "id", "--overwrite"}, {"wait", "id", "--poll-interval", "0s"}, {"models", "--timeout", "0s"}, {"models", "--base-url", "ftp://wrong", "--api-key-env", "KEY"}}
	for _, args := range cases {
		_, _, err := invoke(args, map[string]string{"KEY": "secret"})
		if errorCode(err) != 2 {
			t.Errorf("%v: %v", args, err)
		}
	}
}

func TestDryRunAllThreeModels(t *testing.T) {
	for _, model := range []string{"grok-imagine-video", "grok-imagine-video-1.5", "grok-imagine-video-1.5-preview", "grok-imagine-video-1.5-lite"} {
		out, _, err := invoke([]string{"generate", "--model", model, "--prompt-file", "-", "--dry-run"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		var result map[string]any
		json.Unmarshal([]byte(out), &result)
		body := result["body"].(map[string]any)
		if body["model"] != model || body["prompt"] != "stdin prompt" || result["path"] != "/videos/generations" {
			t.Fatal(out)
		}
	}
}

func TestCreateWaitAndSeparateDownload(t *testing.T) {
	var posted, polled atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/videos/generations":
			posted.Add(1)
			var p map[string]any
			json.NewDecoder(r.Body).Decode(&p)
			if p["model"] != "grok-imagine-video-1.5-lite" || p["duration"] != float64(2) {
				t.Error(p)
			}
			fmt.Fprint(w, `{"request_id":"job-1"}`)
		case "/v1/videos/job-1":
			if polled.Add(1) == 1 {
				fmt.Fprint(w, `{"status":"pending"}`)
				return
			}
			fmt.Fprintf(w, `{"status":"done","video":{"url":%q,"respect_moderation":true}}`, server.URL+"/file")
		case "/file":
			if r.Header.Get("Authorization") != "" {
				t.Error("download auth leak")
			}
			w.Write(fakeMP4)
		default:
			t.Error(r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	env := map[string]string{"GROK_BASE_URL": server.URL + "/v1", "GROK_API_KEY": "secret-key"}
	path := filepath.Join(t.TempDir(), "clip.mp4")
	out, log, err := invoke([]string{"generate", "--model", "grok-imagine-video-1.5-lite", "--prompt", "ball", "--duration", "2", "--output", path, "--poll-interval", "1ms"}, env)
	if err != nil || posted.Load() != 1 || polled.Load() != 2 || !strings.Contains(log, "submitted job-1") || !strings.Contains(out, `"download"`) {
		t.Fatalf("%s %s %v", out, log, err)
	}
	newPath := filepath.Join(t.TempDir(), "again.mp4")
	_, _, err = invoke([]string{"download", "job-1", "--output", newPath}, env)
	if err != nil || posted.Load() != 1 {
		t.Fatal(err)
	}
	_, _, err = invoke([]string{"generate", "--prompt", "ball", "--output", path}, env)
	if errorCode(err) != 2 || posted.Load() != 1 {
		t.Fatal("existing output should prevent POST", err)
	}
}

func TestStatusFailureAndModels(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			fmt.Fprint(w, `{"object":"list","data":[{"id":"grok-imagine-video"},{"id":"grok-imagine-video-1.5-lite"},{"id":"grok-tts"}]}`)
			return
		}
		fmt.Fprint(w, `{"status":"failed","error":{"message":"bad input"}}`)
	}))
	defer s.Close()
	env := map[string]string{"GROK_BASE_URL": s.URL + "/v1", "XAI_API_KEY": "secret"}
	out, _, err := invoke([]string{"status", "job-1"}, env)
	if err != nil || !strings.Contains(out, `"failed"`) {
		t.Fatal(out, err)
	}
	out, _, err = invoke([]string{"wait", "job-1"}, env)
	if errorCode(err) != 4 || !strings.Contains(out, `"failed"`) {
		t.Fatal(out, err)
	}
	out, _, err = invoke([]string{"models"}, env)
	if err != nil || strings.Contains(out, "grok-tts") || !strings.Contains(out, "lite") {
		t.Fatal(out, err)
	}
	out, _, err = invoke([]string{"models", "--prefix="}, env)
	if err != nil || !strings.Contains(out, "grok-tts") {
		t.Fatal(out, err)
	}
}

func TestCredentialRulesAndRedaction(t *testing.T) {
	file := filepath.Join(t.TempDir(), "key.txt")
	os.WriteFile(file, []byte("\ufeffprivate-key\n"), 0600)
	key, err := readKey(commonOptions{keyFile: file}, false, noEnv)
	if err != nil || key != "private-key" {
		t.Fatal(key, err)
	}
	if _, err := readKey(commonOptions{keyFile: file, keyEnv: "KEY"}, true, noEnv); errorCode(err) != 2 {
		t.Fatal(err)
	}
	env := func(k string) string {
		if k == "XAI_API_KEY" {
			return "fallback"
		}
		return ""
	}
	if _, err := readKey(commonOptions{keyEnv: "CUSTOM"}, true, env); errorCode(err) != 2 {
		t.Fatal("explicit env used fallback", err)
	}
	if key, err := readKey(commonOptions{keyEnv: "GROK_API_KEY"}, false, env); err != nil || key != "fallback" {
		t.Fatal(key, err)
	}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"data":[{"id":"private-key"}]}`) }))
	defer s.Close()
	out, _, err := invoke([]string{"models", "--prefix=", "--base-url", s.URL + "/v1", "--api-key-file", file}, nil)
	if err != nil || strings.Contains(out, "private-key") || !strings.Contains(out, "REDACTED") {
		t.Fatal(out, err)
	}
}
