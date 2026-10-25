package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	cli "github.com/measix/measix-tuck/cli-tools/common"
)

type apiClient struct {
	base, key string
	http      *http.Client
	timeout   time.Duration
}

func newClient(base, key string, timeout time.Duration) (*apiClient, error) {
	if err := validateMediaURL(base); err != nil {
		return nil, cli.Errorf(2, "--base-url: %v", err)
	}
	u, _ := url.Parse(base)
	if u.RawQuery != "" {
		return nil, cli.Errorf(2, "--base-url must not contain a query")
	}
	if timeout <= 0 {
		return nil, cli.Errorf(2, "--timeout must be positive")
	}
	return &apiClient{strings.TrimRight(base, "/"), key, &http.Client{
		Timeout: timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return fmt.Errorf("API redirects are disabled; configure the final --base-url")
		},
	}, timeout}, nil
}

func (c *apiClient) request(ctx context.Context, method, path string, body any) (map[string]any, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, cli.Errorf(2, "encode request: %v", err)
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return nil, cli.Errorf(2, "build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "grok-video/"+version)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, cli.Errorf(3, "%s %s: %s", method, path, c.cleanError(err))
	}
	defer resp.Body.Close()
	b, err := readBounded(resp.Body, 4<<20)
	if err != nil {
		return nil, cli.Errorf(3, "read API response: %s", c.cleanError(err))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message := strings.TrimSpace(string(b))
		if len(message) > 2048 {
			message = message[:2048] + "..."
		}
		return nil, cli.Errorf(3, "%s %s: HTTP %d: %s", method, path, resp.StatusCode, c.redact(message))
	}
	var result map[string]any
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err := dec.Decode(&result); err != nil || result == nil {
		return nil, cli.Errorf(3, "API returned invalid JSON object for %s", path)
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return nil, cli.Errorf(3, "API returned trailing content for %s", path)
	}
	if e, ok := result["error"]; ok && e != nil && result["status"] == nil {
		return nil, cli.Errorf(3, "API returned an error: %s", c.redact(fmt.Sprint(e)))
	}
	return result, nil
}

func (c *apiClient) redact(s string) string {
	if c.key != "" {
		return strings.ReplaceAll(s, c.key, "[REDACTED]")
	}
	return s
}

func (c *apiClient) cleanError(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		return c.redact(ue.Err.Error()) // Do not echo signed URLs from network errors.
	}
	return c.redact(err.Error())
}

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,255}$`)

func validateID(id string) error {
	if !requestIDPattern.MatchString(id) {
		return cli.Errorf(2, "request ID must be 1..256 letters, digits, underscores or hyphens and start with a letter/digit")
	}
	return nil
}

func (c *apiClient) status(ctx context.Context, id string) (map[string]any, error) {
	if err := validateID(id); err != nil {
		return nil, err
	}
	r, err := c.request(ctx, http.MethodGet, "/videos/"+id, nil)
	if err != nil {
		return nil, err
	}
	s, ok := r["status"].(string)
	if !ok || s == "" {
		return nil, cli.Errorf(3, "video result is missing status (request %s)", id)
	}
	r["request_id"] = id
	return r, nil
}

func (c *apiClient) wait(ctx context.Context, id string, interval, timeout time.Duration, progress io.Writer) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	last := ""
	for {
		r, err := c.status(ctx, id)
		if ctx.Err() == context.DeadlineExceeded {
			return nil, cli.Errorf(5, "wait timed out for %s; the remote job may still be running; resume with: grok-video wait %s", id, id)
		}
		if err != nil {
			return nil, err
		}
		state := r["status"].(string)
		message := state
		if n, ok := r["progress"]; ok {
			message += " " + fmt.Sprint(n) + "%"
		}
		if message != last {
			fmt.Fprintf(progress, "%s: %s\n", id, message)
			last = message
		}
		switch state {
		case "done":
			video, ok := r["video"].(map[string]any)
			if !ok {
				return r, cli.Errorf(3, "completed result is missing video (request %s)", id)
			}
			if video["respect_moderation"] == false {
				return r, cli.Errorf(4, "video %s was filtered by moderation", id)
			}
			return r, nil
		case "failed", "expired":
			return r, cli.Errorf(4, "video %s %s: %s", id, state, c.redact(fmt.Sprint(r["error"])))
		case "pending":
		default:
			return r, cli.Errorf(3, "unknown video status %q (request %s)", state, id)
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			if ctx.Err() == context.DeadlineExceeded {
				return nil, cli.Errorf(5, "wait timed out for %s; remote generation is not cancelled", id)
			}
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func checkOutput(path string, overwrite bool) error {
	if strings.TrimSpace(path) == "" || path == "-" {
		return cli.Errorf(2, "--output must be a file path; stdout is reserved for JSON")
	}
	st, err := os.Lstat(path)
	if err == nil {
		if !st.Mode().IsRegular() {
			return cli.Errorf(2, "output must not be a directory, symlink or special file")
		}
		if !overwrite {
			return cli.Errorf(2, "output already exists; choose a new path or pass --overwrite")
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("check output: %w", err)
	}
	st, err = os.Stat(filepath.Dir(path))
	if err != nil || !st.IsDir() {
		return cli.Errorf(2, "output parent directory must already exist")
	}
	return nil
}

func (c *apiClient) download(ctx context.Context, result map[string]any, path string, overwrite bool, maxBytes int64) error {
	if result["status"] != "done" {
		return cli.Errorf(4, "video is %v; use wait or download --wait first", result["status"])
	}
	video, ok := result["video"].(map[string]any)
	if !ok || video["respect_moderation"] == false {
		return cli.Errorf(4, "video is missing or filtered by moderation")
	}
	raw, _ := video["url"].(string)
	if raw == "" {
		return cli.Errorf(3, "completed result has no video.url; inspect remote storage/upload result")
	}
	if err := validateMediaURL(raw); err != nil {
		return cli.Errorf(3, "invalid video download URL: %v", err)
	}
	if err := checkOutput(path, overwrite); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return cli.Errorf(3, "invalid download request")
	}
	// A separate, credential-free client also keeps auth out of redirect requests.
	downloader := &http.Client{Timeout: c.timeout, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("too many download redirects")
		}
		if err := validateMediaURL(req.URL.String()); err != nil {
			return err
		}
		if via[len(via)-1].URL.Scheme == "https" && req.URL.Scheme != "https" {
			return fmt.Errorf("download redirect would downgrade HTTPS")
		}
		return nil
	}}
	resp, err := downloader.Do(req)
	if err != nil {
		return cli.Errorf(3, "download: %s", c.cleanError(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return cli.Errorf(3, "download: HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > maxBytes {
		return cli.Errorf(3, "download exceeds --max-download-bytes (%d)", maxBytes)
	}
	prefix := make([]byte, 12)
	if _, err := io.ReadFull(resp.Body, prefix); err != nil || string(prefix[4:8]) != "ftyp" {
		return cli.Errorf(3, "download did not return an MP4 file (missing ftyp header)")
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".grok-video-*.part")
	if err != nil {
		return fmt.Errorf("create download file: %w", err)
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	n, err := io.Copy(tmp, io.LimitReader(io.MultiReader(bytes.NewReader(prefix), resp.Body), maxBytes+1))
	if err != nil {
		return cli.Errorf(3, "download interrupted: %s", c.cleanError(err))
	}
	if n > maxBytes {
		return cli.Errorf(3, "download exceeds --max-download-bytes (%d)", maxBytes)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("flush download: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if overwrite {
		err = os.Rename(tmp.Name(), path)
	} else {
		// Link publishes atomically without clobbering a concurrently created file.
		err = os.Link(tmp.Name(), path)
	}
	if err != nil {
		return fmt.Errorf("publish download (existing output preserved on failure): %w", err)
	}
	abs, _ := filepath.Abs(path)
	result["download"] = map[string]any{"path": abs, "bytes": n}
	return nil
}
