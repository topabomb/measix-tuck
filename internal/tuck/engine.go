package tuck

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// NativeEngine is exclusively the official public HTTP and execution stream API.
type NativeEngine interface {
	Request(context.Context, string, string, any) (M, error)
	Run(context.Context, string, string, string, []M, string, map[string]string, func(M) error) error
}
type EngineOptions struct {
	Binary, Config, DataDir, WorkspaceRoot, URL, ConfigDir string
	External, DesktopProxy                                 bool
	Env                                                    map[string]string
}
type Engine struct {
	mu                        sync.Mutex
	options                   EngineOptions
	process                   *exec.Cmd
	done                      chan struct{}
	state, version, lastError string
	client, stream            *http.Client
}

func NewEngine(o EngineOptions) *Engine {
	if o.URL == "" {
		o.URL = "http://127.0.0.1:8091"
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 5 * time.Minute
	return &Engine{options: o, state: "starting", client: &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, stream: &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (e *Engine) Options() EngineOptions {
	e.mu.Lock()
	defer e.mu.Unlock()
	o := e.options
	o.Env = map[string]string{}
	for k, v := range e.options.Env {
		o.Env[k] = v
	}
	return o
}
func (e *Engine) Configure(o EngineOptions) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.process != nil {
		return Problem(409, "engine_running", "引擎仍在运行")
	}
	if o.URL == "" {
		o.URL = "http://127.0.0.1:8091"
	}
	e.options = o
	return nil
}
func (e *Engine) State() M {
	e.mu.Lock()
	defer e.mu.Unlock()
	return M{"status": e.state, "state": e.state, "version": e.version, "error": e.lastError, "external": e.options.External}
}
func EngineVersion(ctx context.Context, binary string) (string, error) {
	out, err := exec.CommandContext(ctx, binary, "version").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("engine version: %w", err)
	}
	match := regexp.MustCompile(`\bv[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?\b`).FindString(string(out))
	if match == "" {
		return "", errors.New("official engine did not report a semantic version")
	}
	return match, nil
}
func (e *Engine) Start(ctx context.Context) error {
	e.mu.Lock()
	if e.process != nil {
		e.mu.Unlock()
		return Problem(409, "engine_running", "引擎已启动")
	}
	o := e.options
	e.state = "starting"
	e.lastError = ""
	e.mu.Unlock()
	if !o.External {
		v, err := EngineVersion(ctx, o.Binary)
		if err != nil {
			return e.fail(err)
		}
		for _, p := range []string{o.DataDir, o.WorkspaceRoot} {
			if p != "" {
				if err = os.MkdirAll(p, 0700); err != nil {
					return e.fail(err)
				}
			}
		}
		u, err := url.Parse(o.URL)
		if err != nil {
			return e.fail(err)
		}
		listener, err := net.Listen("tcp", u.Host)
		if err != nil {
			return e.fail(fmt.Errorf("Docker Agent port %s is occupied: %w", u.Host, err))
		}
		_ = listener.Close()
		args := []string{"serve", "api", o.Config, "--listen", u.Host, "--max-request-size", "104857600", "--session-db", filepath.Join(o.DataDir, "docker-sessions.db")}
		if o.ConfigDir != "" {
			args = append(args, "--config-dir", o.ConfigDir)
		}
		cmd := exec.Command(o.Binary, args...)
		cmd.Dir = o.DataDir
		cmd.Env = os.Environ()
		proxy := "1"
		if o.DesktopProxy {
			proxy = "0"
		}
		cmd.Env = append(cmd.Env, "DOCKER_AGENT_DISABLE_DESKTOP_PROXY="+proxy)
		for k, v := range o.Env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
		cmd.Stdout = io.Discard
		cmd.Stderr = engineLog{e}
		if err = cmd.Start(); err != nil {
			return e.fail(err)
		}
		e.mu.Lock()
		e.process = cmd
		e.done = make(chan struct{})
		done := e.done
		e.version = v
		e.mu.Unlock()
		go func() {
			err := cmd.Wait()
			e.mu.Lock()
			if e.process == cmd {
				e.state = "unavailable"
				if err != nil && e.lastError == "" {
					e.lastError = err.Error()
				}
			}
			close(done)
			e.mu.Unlock()
		}()
	}
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		probe, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		_, err := e.Request(probe, "GET", "/api/ping", nil)
		cancel()
		if err == nil {
			e.mu.Lock()
			e.state = "ready"
			e.mu.Unlock()
			return nil
		}
		select {
		case <-ctx.Done():
			_ = e.Close()
			return e.fail(ctx.Err())
		case <-deadline.C:
			_ = e.Close()
			return e.fail(errors.New("Docker Agent API did not become ready"))
		case <-tick.C:
		}
	}
}

type engineLog struct{ e *Engine }

func (w engineLog) Write(b []byte) (int, error) {
	w.e.mu.Lock()
	defer w.e.mu.Unlock()
	text := w.e.lastError + string(b)
	if len(text) > 1200 {
		text = text[len(text)-1200:]
	}
	w.e.lastError = text
	return len(b), nil
}
func (e *Engine) fail(err error) error {
	e.mu.Lock()
	e.state = "unavailable"
	e.lastError = err.Error()
	e.mu.Unlock()
	return err
}
func (e *Engine) Request(ctx context.Context, method, path string, body any) (M, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
	}
	e.mu.Lock()
	base := e.options.URL
	e.mu.Unlock()
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(JSON(body))
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, r)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 104857601))
	if err != nil {
		return nil, err
	}
	if len(raw) > 104857600 {
		return nil, Problem(502, "engine_response_too_large", "引擎响应超过上限")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, nativeError(resp.StatusCode, raw)
	}
	var value any
	if err = json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	if list, ok := value.([]any); ok {
		return M{"items": list}, nil
	}
	return O(value), nil
}

type nativeHTTPError struct {
	error
	Payload M
}

func (e *nativeHTTPError) Unwrap() error { return e.error }

func nativeError(status int, raw []byte) error {
	var payload M
	_ = json.Unmarshal(raw, &payload)
	if len(raw) > 1000 {
		raw = raw[:1000]
	}
	return &nativeHTTPError{Problem(status, "native_error", fmt.Sprintf("Docker Agent %d: %s", status, raw)), payload}
}
func (e *Engine) Run(ctx context.Context, session, ref, agent string, messages []M, model string, headers map[string]string, onEvent func(M) error) error {
	body := M{"messages": messages}
	if model != "" {
		body["model"] = model
	}
	e.mu.Lock()
	base := e.options.URL
	e.mu.Unlock()
	path := "/api/sessions/" + url.PathEscape(session) + "/agent/" + url.PathEscape(ref) + "/" + url.PathEscape(agent)
	req, err := http.NewRequestWithContext(ctx, "POST", base+path, bytes.NewReader(JSON(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := e.stream.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1000))
		return nativeError(resp.StatusCode, raw)
	}
	if !strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		return errors.New("Docker Agent did not return an execution stream")
	}
	return ReadNativeSSE(resp.Body, onEvent)
}

// Reader accepts arbitrary UTF-8/CRLF chunk boundaries and multi-line data.
// Limit is per frame, not scanner's default 64 KiB or an execution lifetime.
func ReadNativeSSE(r io.Reader, onEvent func(M) error) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 128*1024), 104857600)
	var data strings.Builder
	frameBytes := 0
	for scanner.Scan() {
		line := scanner.Text()
		frameBytes += len(line) + 1
		if frameBytes > 104857600 {
			return errors.New("native SSE frame exceeds 100 MiB")
		}
		if line == "" {
			if data.Len() > 0 {
				text := strings.TrimSuffix(data.String(), "\n")
				if text != "[DONE]" {
					var event M
					if err := json.Unmarshal([]byte(text), &event); err != nil {
						return err
					}
					if err := onEvent(event); err != nil {
						return err
					}
				}
			}
			data.Reset()
			frameBytes = 0
		} else if strings.HasPrefix(line, "data:") {
			value := strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " ")
			if value != "" {
				data.WriteString(value)
				data.WriteByte('\n')
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if data.Len() != 0 {
		return errors.New("Incomplete SSE frame")
	}
	return nil
}
func (e *Engine) Close() error {
	e.mu.Lock()
	cmd, done := e.process, e.done
	e.mu.Unlock()
	if cmd != nil {
		select {
		case <-done:
		default:
			_ = cmd.Process.Kill()
			<-done
		}
		e.mu.Lock()
		if e.process == cmd {
			e.process = nil
			e.state = "unavailable"
		}
		e.mu.Unlock()
	}
	e.client.CloseIdleConnections()
	return nil
}
