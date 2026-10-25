package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	cli "github.com/measix/measix-tuck/cli-tools/common"
)

func errorCode(err error) int { return cli.ExitCode(err) }

func testClient(t *testing.T, handler http.HandlerFunc) (*apiClient, *httptest.Server) {
	t.Helper()
	s := httptest.NewServer(handler)
	t.Cleanup(s.Close)
	c, err := newClient(s.URL+"/v1", "test-private-key", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return c, s
}

func TestAPIContractAndNoRetries(t *testing.T) {
	var calls atomic.Int32
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer test-private-key" {
			t.Error("missing auth")
		}
		if r.URL.Path != "/v1/videos/generations" || r.Method != "POST" {
			t.Error(r.URL)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["model"] != "grok-imagine-video-1.5-lite" {
			t.Error(body)
		}
		w.WriteHeader(429)
		fmt.Fprint(w, `{"error":"limit; test-private-key"}`)
	})
	_, err := c.request(context.Background(), "POST", "/videos/generations", map[string]any{"model": "grok-imagine-video-1.5-lite"})
	if errorCode(err) != 3 || calls.Load() != 1 || strings.Contains(err.Error(), "test-private-key") {
		t.Fatalf("%d calls; %v", calls.Load(), err)
	}
}

func TestAPIRejectsMalformedResponses(t *testing.T) {
	for _, body := range []string{`not json`, `null`, `[]`, `{} {}`, `{"error":{"message":"bad"}}`} {
		t.Run(body, func(t *testing.T) {
			c, _ := testClient(t, func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, body) })
			if _, err := c.request(context.Background(), "GET", "/models", nil); errorCode(err) != 3 {
				t.Fatalf("%v", err)
			}
		})
	}
}

func TestWaitTerminalStates(t *testing.T) {
	for _, state := range []string{"done", "failed", "expired", "unexpected"} {
		t.Run(state, func(t *testing.T) {
			var calls atomic.Int32
			c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/videos/job-1" {
					t.Error(r.URL.Path)
				}
				if calls.Add(1) == 1 {
					fmt.Fprint(w, `{"status":"pending","progress":12}`)
					return
				}
				fmt.Fprintf(w, `{"status":%q,"video":{"url":"https://example.com/a.mp4","respect_moderation":true},"error":{"code":"invalid_argument","message":"test"}}`, state)
			})
			var progress bytes.Buffer
			r, err := c.wait(context.Background(), "job-1", time.Millisecond, time.Second, &progress)
			want := 4
			if state == "done" {
				want = 0
			}
			if state == "unexpected" {
				want = 3
			}
			if errorCode(err) != want || calls.Load() != 2 || r["request_id"] != "job-1" || !strings.Contains(progress.String(), "12%") {
				t.Fatalf("%v, %v, %s", r, err, &progress)
			}
		})
	}
}

func TestWaitTimeoutCancellationAndModeration(t *testing.T) {
	c, _ := testClient(t, func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, `{"status":"pending"}`) })
	if _, err := c.wait(context.Background(), "id", time.Second, 15*time.Millisecond, io.Discard); errorCode(err) != 5 {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.wait(ctx, "id", time.Second, time.Second, io.Discard); err == nil {
		t.Fatal("cancellation lost")
	}
	filtered, _ := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"status":"done","video":{"respect_moderation":false}}`)
	})
	if _, err := filtered.wait(context.Background(), "id", time.Millisecond, time.Second, io.Discard); errorCode(err) != 4 {
		t.Fatal(err)
	}
	bad, _ := testClient(t, func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, `{"video":{}}`) })
	if _, err := bad.status(context.Background(), "id"); errorCode(err) != 3 {
		t.Fatal(err)
	}
	for _, id := range []string{"", "../secret", "a/b", "a?x", "a#x"} {
		if validateID(id) == nil {
			t.Errorf("accepted %q", id)
		}
	}
}

var fakeMP4 = append([]byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'}, bytes.Repeat([]byte{1}, 64)...)

func resultFor(address string) map[string]any {
	return map[string]any{"status": "done", "request_id": "id", "video": map[string]any{"url": address, "respect_moderation": true}}
}

func TestDownloadAtomicNoCredentialAndOverwrite(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("credential leaked to download")
		}
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/file", 302)
			return
		}
		w.Header().Set("Content-Type", "video/mp4")
		w.Write(fakeMP4)
	}))
	defer s.Close()
	c, _ := newClient("https://api.example.com/v1", "secret", time.Second)
	path := filepath.Join(t.TempDir(), "result.mp4")
	result := resultFor(s.URL + "/redirect")
	if err := c.download(context.Background(), result, path, false, 1024); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, fakeMP4) {
		t.Fatal("wrong content")
	}
	if result["download"].(map[string]any)["bytes"] != int64(len(fakeMP4)) {
		t.Fatal(result)
	}
	if err := c.download(context.Background(), result, path, false, 1024); errorCode(err) != 2 {
		t.Fatal(err)
	}
	os.WriteFile(path, []byte("old"), 0600)
	if err := c.download(context.Background(), result, path, true, 1024); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(path)
	if !bytes.Equal(got, fakeMP4) {
		t.Fatal("overwrite failed")
	}
	files, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".grok-video-*.part"))
	if len(files) != 0 {
		t.Fatal(files)
	}
}

func TestDownloadFailurePreservesDestination(t *testing.T) {
	cases := []struct {
		name    string
		handler http.HandlerFunc
		limit   int64
	}{
		{"html", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "<html>not a video</html>") }, 1024},
		{"http", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(403) }, 1024},
		{"known too large", func(w http.ResponseWriter, _ *http.Request) { w.Write(fakeMP4) }, 20},
		{"stream too large", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(200)
			w.(http.Flusher).Flush()
			w.Write(fakeMP4)
		}, 20},
		{"truncated", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Length", "200")
			w.Write(fakeMP4)
		}, 1024},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewServer(tc.handler)
			defer s.Close()
			c, _ := newClient("https://api.example.com/v1", "secret", time.Second)
			path := filepath.Join(t.TempDir(), "keep.mp4")
			os.WriteFile(path, []byte("existing"), 0600)
			if err := c.download(context.Background(), resultFor(s.URL), path, true, tc.limit); errorCode(err) != 3 {
				t.Fatal(err)
			}
			got, _ := os.ReadFile(path)
			if string(got) != "existing" {
				t.Fatal("destination changed")
			}
			files, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".grok-video-*.part"))
			if len(files) != 0 {
				t.Fatal(files)
			}
		})
	}
}

func TestAPIRedirectDoesNotForwardKey(t *testing.T) {
	var received atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { received.Store(true) }))
	defer target.Close()
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) })
	if _, err := c.request(context.Background(), "GET", "/models", nil); errorCode(err) != 3 || received.Load() {
		t.Fatalf("redirect followed: %v", err)
	}
}
