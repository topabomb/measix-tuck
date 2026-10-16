package tuck

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	stdimage "image"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// These tests exercise archived assertions at their original owner boundary.
// Provider scripts control responses; the engine remains the official executable.
func parityApp(t *testing.T) (*platformFixture, *App) {
	t.Helper()
	f := newPlatformFixture(t, 5)
	c := f.models.Config()
	p := localProvider(f.server.URL)
	O(A(p["models"])[0])["inputModalities"] = []any{"TEXT", "IMAGE"}
	p["models"] = append(A(p["models"]), Merge(O(A(p["models"])[0]), M{"modelId": "text-only", "inputModalities": []any{"TEXT"}}))
	c["providers"] = []any{p}
	c["defaultModelId"] = "proof-model"
	f.save(c)
	return f, f.startApp()
}
func nativeDelta(w http.ResponseWriter, delta M) {
	delta["role"] = "assistant"
	reason := "stop"
	if len(A(delta["tool_calls"])) > 0 {
		reason = "tool_calls"
	}
	w.Header().Set("Content-Type", "text/event-stream")
	fmt.Fprintf(w, "data: %s\n\n", JSON(M{"id": "parity", "object": "chat.completion.chunk", "choices": []any{M{"index": 0, "delta": delta, "finish_reason": nil}}}))
	fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", JSON(M{"id": "parity", "object": "chat.completion.chunk", "choices": []any{M{"index": 0, "delta": M{}, "finish_reason": reason}}, "usage": M{"prompt_tokens": 100, "completion_tokens": 20}}))
}
func nativeCall(name string, args M, id string) M {
	if id == "" {
		id = UUID()
	}
	return M{"tool_calls": []any{M{"index": 0, "id": id, "type": "function", "function": M{"name": name, "arguments": string(JSON(args))}}}}
}
func nativeScript(f *platformFixture, fn func(http.ResponseWriter, *http.Request, M)) {
	f.change(func() {
		f.hook = func(w http.ResponseWriter, r *http.Request, b M) bool {
			if strings.HasSuffix(r.URL.Path, "/chat/completions") {
				fn(w, r, b)
				return true
			}
			return false
		}
	})
}
func recentNative(b M) (string, []M) {
	messages := A(b["messages"])
	last := -1
	for i, v := range messages {
		if O(v)["role"] == "user" {
			last = i
		}
	}
	var user string
	if last >= 0 {
		if s, ok := O(messages[last])["content"].(string); ok {
			user = s
		} else {
			user = string(JSON(O(messages[last])["content"]))
		}
	}
	var results []M
	for _, v := range messages[last+1:] {
		if O(v)["role"] == "tool" {
			results = append(results, O(v))
		}
	}
	return user, results
}
func newParityTask(t *testing.T, app *App, in M) M {
	t.Helper()
	v, e := app.Manager.Create(context.Background(), Merge(M{"approvalMode": "auto"}, in))
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func taskMessages(t *testing.T, s *Store, id string) []M {
	t.Helper()
	v, e := s.Messages(id, 0, 10000)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func nativeNames(b M) []string {
	var v []string
	for _, x := range A(b["tools"]) {
		v = append(v, S(O(O(x)["function"])["name"]))
	}
	return v
}

func TestParityHistoricalToolRepair(t *testing.T) {
	for _, delivery := range []bool{false, true} {
		t.Run(fmt.Sprint(delivery), func(t *testing.T) {
			m, s, _, id := taskFixture(t)
			name, args := "shell", M{}
			if delivery {
				name = "deliver_result"
				args = M{"summary": "已完成历史成果", "files": []any{}}
			}
			calls := []any{M{"id": "call", "function": M{"name": name, "arguments": string(JSON(args))}}}
			if !delivery {
				calls = append(calls, M{"id": "unfinished", "function": M{"name": "write_file", "arguments": "{}"}})
			}
			history := []any{M{"message": M{"role": "assistant", "tool_calls": calls}}, M{"message": M{"role": "tool", "tool_call_id": "call", "content": "failed", "is_error": !delivery}}}
			m.mu.Lock()
			e := m.normalizer.importHistory(m.tasks[id], history)
			m.mu.Unlock()
			if e != nil {
				t.Fatal(e)
			}
			messages := taskMessages(t, s, id)
			call := O(O(A(messages[0]["blocks"])[0])["data"])
			result := O(O(A(messages[1]["blocks"])[0])["data"])
			want := "error"
			if delivery {
				want = "success"
			}
			if call["status"] != want || result["name"] != name {
				t.Fatal(messages)
			}
			if !delivery && O(O(A(messages[0]["blocks"])[1])["data"])["status"] != nil {
				t.Fatal("unfinished tool invented outcome")
			}
			delete(call, "status")
			result["name"] = "tool"
			delete(result, "deliverySummary")
			for _, v := range messages {
				if e = s.SaveMessage(id, v); e != nil {
					t.Fatal(e)
				}
			}
			m.mu.Lock()
			e = m.normalizer.restoreToolMetadata(m.tasks[id], history)
			m.mu.Unlock()
			if e != nil {
				t.Fatal(e)
			}
			repaired := taskMessages(t, s, id)
			if O(O(A(repaired[0]["blocks"])[0])["data"])["status"] != want || O(O(A(repaired[1]["blocks"])[0])["data"])["name"] != name {
				t.Fatal(repaired)
			}
			if delivery && O(O(A(repaired[1]["blocks"])[0])["data"])["deliverySummary"] != "已完成历史成果" {
				t.Fatal(repaired)
			}
			m.mu.Lock()
			e = m.normalizer.restoreToolMetadata(m.tasks[id], history)
			m.mu.Unlock()
			if e != nil || !bytes.Equal(JSON(taskMessages(t, s, id)), JSON(repaired)) || I(mustTask(t, m, id)["logicalCount"]) != 2 {
				t.Fatal("repair was not idempotent", e)
			}
		})
	}
}

func TestParityChildFailureAndRestart(t *testing.T) {
	m, s, _, id := taskFixture(t)
	root := mustUpdate(t, m, id, M{"status": "working", "turnId": "turn-1"})
	mustEvent(t, m, id, M{"type": "stream_started", "session_id": "child-native", "agent_name": "worker"})
	mustEvent(t, m, id, M{"type": "error", "session_id": "child-native", "error": "child failed", "code": "model_error"})
	mustEvent(t, m, id, M{"type": "stream_stopped", "session_id": "child-native", "reason": "error"})
	var child M
	for _, v := range m.List() {
		if v["parentTaskId"] == id {
			child = v
		}
	}
	if mustTask(t, m, id)["lastFailure"] != nil || child["lastTurnReason"] != "error" {
		t.Fatal(root, child)
	}
	delegation := func() M {
		for _, msg := range taskMessages(t, s, id) {
			if b := findBlock(msg, "delegation"); b != nil {
				return O(A(O(b["data"])["runs"])[0])
			}
		}
		t.Fatal("missing delegation")
		return nil
	}
	if r := delegation(); r["status"] != "failed" || r["summary"] != "child failed" {
		t.Fatal(r)
	}
	m.mu.Lock()
	e := m.settleLocked(m.tasks[S(child["id"])], "max-tokens", nil)
	if e == nil {
		e = m.normalizer.delegation(m.tasks[id])
	}
	m.mu.Unlock()
	if e != nil {
		t.Fatal(e)
	}
	if r := delegation(); r["status"] != "failed" || r["summary"] != "已达输出上限" {
		t.Fatal(r)
	}
	mustEvent(t, m, id, M{"type": "tool_call_confirmation", "agent_name": "root", "tool_call": M{"id": "pending-shell", "function": M{"name": "shell", "arguments": `{"cmd":"long command"}`}}})
	for _, msg := range taskMessages(t, s, id) {
		if findBlock(msg, "terminal") != nil {
			t.Fatal("unapproved terminal evidence")
		}
		for _, v := range A(msg["blocks"]) {
			b := O(v)
			if O(b["data"])["callId"] == "pending-shell" && O(b["data"])["status"] != nil {
				t.Fatal("unapproved success", b)
			}
		}
	}
	restored, e := NewManager(m.options)
	if e != nil {
		t.Fatal(e)
	}
	defer restored.Close(context.Background())
	got := mustTask(t, restored, id)
	if got["status"] != "idle" || got["pendingInteraction"] != nil || O(got["lastFailure"])["code"] != "service_restarted" {
		t.Fatal(got)
	}
}

func TestParityBackgroundReports(t *testing.T) {
	m, s, _, id := taskFixture(t)
	mustUpdate(t, m, id, M{"turnId": "turn-1"})
	for _, key := range []string{"a", "b", "c"} {
		m.mu.Lock()
		e := m.normalizer.backgroundRun(m.tasks[id], M{"agent": "worker", "task": key}, "Background agent task started with ID: "+key)
		m.mu.Unlock()
		if e != nil {
			t.Fatal(e)
		}
	}
	report := func(name, text string, failed bool) {
		mustEvent(t, m, id, M{"type": "tool_call_response", "session_id": mustTask(t, m, id)["nativeSessionId"], "tool_definition": M{"name": name}, "response": text, "result": M{"isError": failed}})
	}
	runs := func() []any { return A(mustTask(t, m, id)["backgroundRuns"]) }
	report("list_background_agents", "Background Agent Tasks:\n\nID: a\n  Agent:   worker\n  Status:  running\n  Runtime: 3s\n\nID: b\n  Agent:   worker\n  Status:  completed\n  Runtime: 2s\n", false)
	if O(runs()[0])["status"] != "running" || O(runs()[1])["status"] != "completed" || O(runs()[2])["status"] != nil {
		t.Fatal(runs())
	}
	report("view_background_agent", "Task ID: a\nAgent:   worker\nStatus:  running\nRuntime: 4s\n\n--- Output ---\nLatest output\n\nID: c\n  Agent:   worker\n  Status:  completed\n", false)
	if O(runs()[2])["status"] != nil || !strings.Contains(S(O(runs()[0])["summary"]), "Latest output") {
		t.Fatal("output interpreted as metadata", runs())
	}
	report("wait_background_agents", string(JSON(M{"tasks": []any{M{"task_id": "a", "status": "failed", "error": "network failed"}, M{"task_id": "missing", "status": "not_found"}, M{"task_id": "b", "status": "completed", "output": strings.Repeat("x", 300) + "FINAL"}}})), true)
	if O(runs()[0])["status"] != "failed" || O(runs()[0])["summary"] != "network failed" || len(S(O(runs()[1])["summary"])) != 240 || !strings.HasSuffix(S(O(runs()[1])["summary"]), "FINAL") {
		t.Fatal(runs())
	}
	report("list_background_agents", "ID: a\n  Agent: worker\n  Status: running\n", false)
	if O(runs()[0])["status"] != "failed" {
		t.Fatal("terminal regressed")
	}
	report("stop_background_agent", "Background agent task c stopped.", true)
	if O(runs()[2])["status"] != nil {
		t.Fatal("failed stop succeeded")
	}
	report("stop_background_agent", "Background agent task c stopped.", false)
	saved, e := s.Task(id)
	if e != nil || O(A(saved["backgroundRuns"])[2])["status"] != "stopped" {
		t.Fatal(saved, e)
	}
}

func TestParityNativeAttachments(t *testing.T) {
	f, app := parityApp(t)
	task := newParityTask(t, app, nil)
	id := S(task["id"])
	image, e := app.Files.Upload(task, uploadData("evidence.png", testPNG(t, 32, 24, false)))
	if e != nil {
		t.Fatal(e)
	}
	file, e := app.Files.Upload(mustTask(t, app.Manager, id), uploadData("notes.txt", []byte("LOCAL_FILE_PROOF")))
	if e != nil {
		t.Fatal(e)
	}
	if I(mustTask(t, app.Manager, id)["logicalCount"]) != 0 || len(taskMessages(t, app.Store, id)) != 0 {
		t.Fatal("upload created history")
	}
	saved, _ := app.Store.Task(id)
	if len(A(saved["uploads"])) != 2 {
		t.Fatal(saved)
	}
	if _, e = app.Manager.Submit(id, "ATTACHMENT_PROOF", []string{S(image["id"]), S(file["id"])}, nil, nil); e != nil {
		t.Fatal(e)
	}
	waitIdle(t, app.Manager, id)
	found := false
	for _, req := range f.requestList() {
		for _, v := range A(O(req["body"])["messages"]) {
			u := O(v)
			if u["role"] != "user" || !strings.Contains(string(JSON(u)), "ATTACHMENT_PROOF") {
				continue
			}
			found = true
			var text, data string
			for _, part := range A(u["content"]) {
				p := O(part)
				if p["type"] == "text" {
					text += S(p["text"])
				}
				if p["type"] == "image_url" {
					data = S(O(p["image_url"])["url"])
				}
			}
			for _, fragment := range []string{S(file["path"]), S(image["path"]), "already saved in this task's local working directory", "paths below are relative to that directory", "read_file for one file or read_multiple_files for multiple files", "No remote download is needed"} {
				if !strings.Contains(text, fragment) {
					t.Fatal("attachment guidance missing", fragment, text)
				}
			}
			if strings.Count(text, "User attachments") != 1 || strings.Contains(string(JSON(u)), "LOCAL_FILE_PROOF") || !strings.HasPrefix(data, "data:image/") {
				t.Fatal(u)
			}
			raw, e := base64.StdEncoding.DecodeString(strings.SplitN(data, ",", 2)[1])
			if e != nil || len(raw) == 0 {
				t.Fatal(e)
			}
			decoded, _, err := stdimage.DecodeConfig(bytes.NewReader(raw))
			if err != nil || decoded.Width != 32 || decoded.Height != 24 {
				t.Fatal("image pixels lost", decoded, err)
			}
		}
	}
	if !found {
		t.Fatal("native image request missing")
	}
	if len(A(O(findBlock(taskMessages(t, app.Store, id)[0], "attachments")["data"])["items"])) != 2 {
		t.Fatal("attachment projection missing")
	}
	other := newParityTask(t, app, nil)
	if _, e = app.Manager.Submit(S(other["id"]), "", []string{S(image["id"])}, nil, nil); e == nil {
		t.Fatal("cross-task image accepted")
	}
	if _, e = app.Manager.SelectModel(id, "text-only", "default"); e != nil {
		t.Fatal(e)
	}
	if _, e = app.Manager.Submit(id, "", []string{S(image["id"])}, nil, nil); e == nil {
		t.Fatal("text-only image accepted")
	}
	app.Manager.SelectModel(id, "proof-model", "default")
	mustUpdate(t, app.Manager, id, M{"queuePaused": true})
	if _, e = app.Manager.Submit(id, "", []string{S(image["id"])}, nil, nil); e != nil {
		t.Fatal(e)
	}
	app.Manager.SelectModel(id, "text-only", "default")
	if _, e = app.Manager.UpdateQueue(context.Background(), id, M{"action": "resume"}); e != nil {
		t.Fatal(e)
	}
	queued := waitTask(t, app.Manager, id, func(v M) bool { return !app.Manager.Running(id) && B(v["queuePaused"]) })
	if len(A(queued["queue"])) != 1 {
		t.Fatal("failed capability lost queue", queued)
	}
	app.Manager.SelectModel(id, "proof-model", "default")
	app.Manager.UpdateQueue(context.Background(), id, M{"action": "resume"})
	waitTask(t, app.Manager, id, func(v M) bool { return !app.Manager.Running(id) && len(A(v["queue"])) == 0 })
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	nativeScript(f, func(w http.ResponseWriter, r *http.Request, b M) {
		prompt, _ := recentNative(b)
		if strings.Contains(prompt, "STEER_TARGET") {
			once.Do(func() { close(entered) })
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		nativeDelta(w, M{"content": "done"})
	})
	submitTask(t, app.Manager, id, "STEER_TARGET")
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		close(release)
		t.Fatal("steering provider not reached", mustTask(t, app.Manager, id))
	}
	if _, e = app.Manager.Submit(id, "IMAGE_STEER", []string{S(image["id"])}, nil, nil); e != nil {
		t.Fatal(e)
	}
	entry := O(A(mustTask(t, app.Manager, id)["queue"])[0])
	_, e = app.Manager.UpdateQueue(context.Background(), id, M{"action": "send", "entryId": entry["id"]})
	close(release)
	if e != nil {
		t.Fatal(e)
	}
	waitIdle(t, app.Manager, id)
	found = false
	for _, req := range f.requestList() {
		for _, v := range A(O(req["body"])["messages"]) {
			u := O(v)
			if u["role"] == "user" && strings.Contains(string(JSON(u)), "IMAGE_STEER") && strings.Contains(string(JSON(u)), "image_url") && strings.Contains(string(JSON(u)), "No remote download is needed") {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("queued image steering did not reach native provider")
	}
	if e = os.WriteFile(filepath.Join(S(task["workspace"]), S(image["path"])), []byte("changed"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, _, e = app.Files.Prepare(mustTask(t, app.Manager, id), "", []string{S(image["id"])}); e == nil {
		t.Fatal("tampered attachment accepted")
	}
}

func TestParityNativeBackgroundBeforeParentEnds(t *testing.T) {
	f, app := parityApp(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	nativeScript(f, func(w http.ResponseWriter, r *http.Request, b M) {
		_, results := recentNative(b)
		if Contains(nativeNames(b), "run_background_agent") {
			if len(results) == 0 {
				nativeDelta(w, nativeCall("run_background_agent", M{"agent": "worker", "task": "BACKGROUND_CHILD"}, ""))
				return
			}
			once.Do(func() { close(entered) })
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		nativeDelta(w, M{"content": "child finished"})
	})
	task := newParityTask(t, app, nil)
	id := S(task["id"])
	submitTask(t, app.Manager, id, "BACKGROUND_PARENT")
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		close(release)
		t.Fatal("parent did not reach held response")
	}
	root := waitTask(t, app.Manager, id, func(v M) bool { return len(A(v["backgroundRuns"])) == 1 })
	if root["status"] != "working" || len(A(root["backgroundRuns"])) != 1 || O(A(root["backgroundRuns"])[0])["turnId"] != root["turnId"] {
		close(release)
		t.Fatal(root)
	}
	close(release)
	waitIdle(t, app.Manager, id)
	for _, v := range app.Manager.List() {
		if v["parentTaskId"] == id {
			t.Fatal("private background session identity inferred", v)
		}
	}
}

func TestParityNativeConcurrentDeliveries(t *testing.T) {
	f, app := parityApp(t)
	ready := make(chan struct{})
	var entered atomic.Int32
	nativeScript(f, func(w http.ResponseWriter, r *http.Request, b M) {
		prompt, results := recentNative(b)
		if len(results) == 0 {
			if entered.Add(1) == 2 {
				close(ready)
			}
			select {
			case <-ready:
			case <-r.Context().Done():
				return
			}
			nativeDelta(w, nativeCall("deliver_result", M{"summary": prompt, "files": []any{M{"path": "report.txt", "description": nil}}}, "same-provider-id"))
			return
		}
		nativeDelta(w, M{"content": "delivered"})
	})
	var tasks []M
	for _, marker := range []string{"FIRST_PRIVATE", "SECOND_PRIVATE"} {
		task := newParityTask(t, app, nil)
		tasks = append(tasks, task)
		if e := os.WriteFile(filepath.Join(S(task["workspace"]), "report.txt"), []byte(marker), 0600); e != nil {
			t.Fatal(e)
		}
		submitTask(t, app.Manager, S(task["id"]), marker)
	}
	for i, task := range tasks {
		id := S(task["id"])
		done := waitIdle(t, app.Manager, id)
		if I(O(done["latestDelivery"])["version"]) != 1 {
			t.Fatal(done, taskMessages(t, app.Store, id))
		}
		d, e := app.Store.Delivery(id, "1")
		if e != nil {
			t.Fatal(e)
		}
		file := O(A(d["files"])[0])
		url := app.URL + "/api/tasks/" + id + "/deliveries/1/files/" + S(file["id"])
		r, e := http.Get(url)
		if e != nil {
			t.Fatal(e)
		}
		raw, _ := io.ReadAll(r.Body)
		r.Body.Close()
		want := []string{"FIRST_PRIVATE", "SECOND_PRIVATE"}[i]
		if string(raw) != want || d["summary"] != want || d["taskId"] != id {
			t.Fatal(d, string(raw))
		}
	}
}

func TestParityNativeFilesystemPolicies(t *testing.T) {
	f, app := parityApp(t)
	work, outside := t.TempDir(), t.TempDir()
	for _, dir := range []string{filepath.Join(work, "allowed"), filepath.Join(work, "allowed-other"), filepath.Join(work, "private"), filepath.Join(outside, "blocked")} {
		if e := os.MkdirAll(dir, 0700); e != nil {
			t.Fatal(e)
		}
	}
	for path, text := range map[string]string{filepath.Join(work, "inside.txt"): "POLICY_INSIDE", filepath.Join(work, "allowed/read.txt"): "POLICY_RELATIVE", filepath.Join(work, "allowed-other/read.txt"): "POLICY_SIBLING", filepath.Join(work, "private/read.txt"): "POLICY_PRIVATE", filepath.Join(outside, "read.txt"): "POLICY_OUTSIDE", filepath.Join(outside, "blocked/read.txt"): "POLICY_BLOCKED"} {
		if e := os.WriteFile(path, []byte(text), 0600); e != nil {
			t.Fatal(e)
		}
	}
	read := func(path string) M { return M{"name": "read_file", "arguments": M{"path": path}} }
	run := func(toolset M, calls []M, mode string) []string {
		t.Helper()
		saveApp(t, app, func(c M) { O(O(c["agents"])["root"])["toolsets"] = []any{toolset} })
		nativeScript(f, func(w http.ResponseWriter, r *http.Request, b M) {
			_, results := recentNative(b)
			if len(results) < len(calls) {
				call := calls[len(results)]
				nativeDelta(w, nativeCall(S(call["name"]), O(call["arguments"]), ""))
				return
			}
			nativeDelta(w, M{"content": "policy verified"})
		})
		in := M{"workspaceMode": mode}
		if mode == "specified" {
			in["workspace"] = work
		}
		task := newParityTask(t, app, in)
		runAppTask(t, app, S(task["id"]), "Verify file policy")
		var outputs []string
		for _, req := range f.requestList() {
			body := O(req["body"])
			prompt, results := recentNative(body)
			if prompt == "Verify file policy" && len(results) == len(calls) {
				outputs = nil
				for _, v := range results {
					outputs = append(outputs, S(v["content"]))
				}
			}
		}
		if len(outputs) != len(calls) {
			t.Fatal("not all controlled native calls executed", outputs)
		}
		return outputs
	}
	for _, kind := range []string{"filesystem", "file"} {
		out := run(M{"type": kind, "allow_list": []any{"."}, "deny_list": []any{"private"}}, []M{read("inside.txt"), read(filepath.Join(outside, "read.txt")), read("private/read.txt"), {"name": "write_file", "arguments": M{"path": kind + "-new.txt", "content": "POLICY_WRITE"}}, {"name": "write_file", "arguments": M{"path": "private/read.txt", "content": "MUST_NOT_WRITE"}}}, "specified")
		for index, marker := range map[int]string{0: "POLICY_INSIDE", 1: "outside the allowed directories", 2: "denied directory", 4: "denied directory"} {
			if !strings.Contains(out[index], marker) {
				t.Fatal(kind, out)
			}
		}
		for path, want := range map[string]string{filepath.Join(work, kind+"-new.txt"): "POLICY_WRITE", filepath.Join(work, "private/read.txt"): "POLICY_PRIVATE"} {
			raw, e := os.ReadFile(path)
			if e != nil || string(raw) != want {
				t.Fatal(e, string(raw))
			}
		}
		out = run(M{"type": kind, "allow_list": []any{}, "deny_list": []any{"private"}}, []M{read(filepath.Join(outside, "read.txt")), read("private/read.txt")}, "specified")
		if !strings.Contains(out[0], "POLICY_OUTSIDE") || !strings.Contains(out[1], "denied directory") {
			t.Fatal(out)
		}
		out = run(M{"type": kind, "allow_list": []any{"allowed", outside}, "deny_list": []any{filepath.Join(outside, "blocked")}}, []M{read("allowed/read.txt"), read(filepath.Join(outside, "read.txt")), read("allowed-other/read.txt"), read(filepath.Join(outside, "blocked/read.txt"))}, "specified")
		for i, marker := range []string{"POLICY_RELATIVE", "POLICY_OUTSIDE", "outside the allowed directories", "denied directory"} {
			if !strings.Contains(out[i], marker) {
				t.Fatal(out)
			}
		}
	}
	if e := os.WriteFile(filepath.Join(app.Engine.Options().DataDir, "native-cwd.txt"), []byte("POLICY_NATIVE_CWD"), 0600); e != nil {
		t.Fatal(e)
	}
	out := run(M{"type": "file", "allow_list": []any{"."}}, []M{read("native-cwd.txt")}, "none")
	if !strings.Contains(out[0], "POLICY_NATIVE_CWD") {
		t.Fatal(out)
	}
}

func TestParityNativeSharedTodosAndReadonly(t *testing.T) {
	f, app := parityApp(t)
	saveApp(t, app, func(c M) { O(O(c["agents"])["root"])["toolsets"] = []any{M{"type": "todo", "shared": true}} })
	nativeScript(f, func(w http.ResponseWriter, r *http.Request, b M) {
		prompt, results := recentNative(b)
		if len(results) == 0 {
			if prompt == "CREATE_SHARED" {
				nativeDelta(w, nativeCall("create_todo", M{"description": "NATIVE_PROCESS_SHARED_PROOF"}, ""))
			} else {
				nativeDelta(w, nativeCall("list_todos", M{}, ""))
			}
			return
		}
		nativeDelta(w, M{"content": "done"})
	})
	first, second := newParityTask(t, app, nil), newParityTask(t, app, nil)
	runAppTask(t, app, S(first["id"]), "CREATE_SHARED")
	runAppTask(t, app, S(second["id"]), "LIST_SHARED")
	found := false
	for _, req := range f.requestList() {
		prompt, results := recentNative(O(req["body"]))
		if prompt == "LIST_SHARED" && strings.Contains(string(JSON(results)), "NATIVE_PROCESS_SHARED_PROOF") {
			found = true
		}
	}
	if !found {
		t.Fatal("shared native todos were isolated by conversation")
	}
	saveApp(t, app, func(c M) {
		O(O(c["agents"])["root"])["toolsets"] = []any{M{"type": "filesystem", "tools": []any{"read_file", "write_file"}, "readonly": true}}
	})
	nativeScript(f, func(w http.ResponseWriter, r *http.Request, b M) { nativeDelta(w, M{"content": "catalog"}) })
	task := newParityTask(t, app, nil)
	runAppTask(t, app, S(task["id"]), "READONLY_CATALOG")
	for _, req := range f.requestList() {
		b := O(req["body"])
		prompt, _ := recentNative(b)
		if prompt == "READONLY_CATALOG" {
			names := nativeNames(b)
			if !Contains(names, "read_file") || Contains(names, "write_file") || Contains(names, "list_directory") {
				t.Fatal(names)
			}
		}
	}
}

func TestParityAssistantConcurrencyConfig(t *testing.T) {
	f := newPlatformFixture(t, 5)
	base := f.models.Config()
	for _, v := range []any{0, -1, 1.5, "2", true, nil} {
		c := Clone(base)
		O(A(O(c["workbench"])["assistants"])[0])["taskConcurrency"] = v
		if e := ValidateConfig(c, false); e == nil {
			t.Fatal("invalid concurrency accepted", v)
		}
	}
	c := Clone(base)
	O(A(O(c["workbench"])["assistants"])[0])["taskConcurrency"] = 3
	f.save(c)
	if I(O(A(AssistantCatalog(c)["items"])[0])["taskConcurrency"]) != 3 {
		t.Fatal(AssistantCatalog(c))
	}
	y := generatedConfig(t, f.models)
	for _, v := range O(y["agents"]) {
		if Has(O(v), "taskConcurrency") {
			t.Fatal("workbench scheduling leaked to native config")
		}
	}
}

func TestParityNativeWorkspaceModes(t *testing.T) {
	f, app := parityApp(t)
	shared := t.TempDir()
	saveApp(t, app, func(c M) {
		for _, v := range A(O(c["workbench"])["assistants"]) {
			a := O(v)
			if a["agent"] == "root" {
				a["workspace"] = M{"mode": "none", "directory": shared}
			}
			if a["agent"] == "video" {
				a["workspace"] = M{"mode": "specified", "directory": shared}
			}
		}
	})
	before, _ := os.ReadDir(app.Config.WorkspaceRoot())
	task := newParityTask(t, app, nil)
	id := S(task["id"])
	after, _ := os.ReadDir(app.Config.WorkspaceRoot())
	if task["workspaceMode"] != "none" || S(task["workspace"]) != app.Engine.Options().DataDir || len(after) != len(before) {
		t.Fatal(task, before, after)
	}
	native := func(task M) M {
		t.Helper()
		v, e := app.Engine.Request(context.Background(), "GET", "/api/sessions/"+S(task["nativeSessionId"]), nil)
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	if v := native(task); S(v["working_dir"]) != "" || O(v["attributes"])["tuck.no_workspace"] != nil {
		t.Fatal(v)
	}
	if e := os.WriteFile(filepath.Join(S(task["workspace"]), "native-default-proof.txt"), []byte("NATIVE_CWD_VERIFIED"), 0600); e != nil {
		t.Fatal(e)
	}
	if status, v := appRequest(t, app, "PUT", "/api/settings/orchestration", M{"requireDelivery": true}); status != 200 {
		t.Fatal(v)
	}
	nativeScript(f, func(w http.ResponseWriter, r *http.Request, b M) {
		_, results := recentNative(b)
		switch len(results) {
		case 0:
			nativeDelta(w, nativeCall("read_file", M{"path": "native-default-proof.txt"}, ""))
		case 1:
			nativeDelta(w, nativeCall("deliver_result", M{"summary": "Native directory verified", "files": []any{M{"path": "native-default-proof.txt"}}}, ""))
		default:
			nativeDelta(w, M{"content": "done"})
		}
	})
	runAppTask(t, app, id, "WORKSPACE_NATIVE_PROOF")
	task = mustTask(t, app.Manager, id)
	if I(task["turnCount"]) != 1 || I(O(task["latestDelivery"])["fileCount"]) != 1 || len(A(task["artifacts"])) != 0 {
		t.Fatal(task)
	}
	for _, req := range f.requestList() {
		b := O(req["body"])
		prompt, _ := recentNative(b)
		if prompt == "WORKSPACE_NATIVE_PROOF" {
			for _, name := range []string{"read_file", "write_file", "deliver_result", "list_sessions", "send_session_message"} {
				if !Contains(nativeNames(b), name) {
					t.Fatal("none mode removed tools", name)
				}
			}
		}
	}
	fork, e := app.Manager.Fork(context.Background(), id, 0)
	if e != nil {
		t.Fatal(e)
	}
	child := O(fork["task"])
	if child["workspaceMode"] != "none" || child["workspace"] != task["workspace"] || S(native(child)["working_dir"]) != "" {
		t.Fatal(child)
	}
	if _, e = app.Manager.Create(context.Background(), M{"channel": "tasks"}); Status(e) != 422 {
		t.Fatal(e)
	}
	assigned := newParityTask(t, app, M{"channel": "tasks", "agent": "video"})
	if assigned["workspaceMode"] != "specified" || assigned["workspace"] != shared || native(assigned)["working_dir"] != shared {
		t.Fatal(assigned, native(assigned))
	}
	auto := newParityTask(t, app, M{"workspaceMode": "auto"})
	if auto["workspace"] != filepath.Join(app.Config.WorkspaceRoot(), S(auto["id"])) {
		t.Fatal(auto)
	}
	for _, in := range []M{{"workspaceMode": "specified", "workspace": "."}, {"workspaceMode": "none", "workspace": shared}, {"workspaceMode": "invalid"}} {
		if _, e = app.Manager.Create(context.Background(), in); Status(e) != 422 {
			t.Fatal(in, e)
		}
	}
	arbitrary := newParityTask(t, app, M{"workspaceMode": "specified", "workspace": t.TempDir()})
	if native(arbitrary)["working_dir"] != arbitrary["workspace"] {
		t.Fatal(arbitrary)
	}
	saveApp(t, app, func(c M) {
		for _, v := range A(O(c["workbench"])["assistants"]) {
			if O(v)["agent"] == "root" {
				O(v)["workspace"] = M{"mode": "auto", "directory": filepath.Join(shared, "missing")}
			}
		}
	})
	if mustTask(t, app.Manager, id)["workspaceMode"] != "none" || S(native(task)["working_dir"]) != "" {
		t.Fatal("default edit mutated existing task")
	}
	o := app.Manager.options
	o.Engine = &fakeNative{}
	o.NativeDirectory = ""
	external, e := NewManager(o)
	if e != nil {
		t.Fatal(e)
	}
	if mustTask(t, external, id)["workspace"] != "" {
		t.Fatal("external runtime fabricated native directory")
	}
	external.Close(context.Background())
	o.NativeDirectory = shared
	moved, e := NewManager(o)
	if e != nil {
		t.Fatal(e)
	}
	defer moved.Close(context.Background())
	if mustTask(t, moved, id)["workspace"] != shared || mustTask(t, moved, S(auto["id"]))["workspace"] != auto["workspace"] {
		t.Fatal("restart changed owned workspace")
	}
}

func TestParityWorkspaceSaveValidation(t *testing.T) {
	f, app := parityApp(t)
	before, e := os.ReadFile(app.Engine.Options().Config)
	if e != nil {
		t.Fatal(e)
	}
	for _, workspace := range []M{{"mode": "invalid"}, {"mode": "specified", "directory": ""}, {"mode": "specified", "directory": filepath.Join(t.TempDir(), "missing")}, {"mode": "specified", "directory": f.config.DataDir()}} {
		_, data := appRequest(t, app, "GET", "/api/settings/runtime", nil)
		for _, v := range A(O(O(data["config"])["workbench"])["assistants"]) {
			if O(v)["agent"] == "root" {
				O(v)["workspace"] = workspace
			}
		}
		if status, v := appRequest(t, app, "PUT", "/api/settings/runtime", data); status != 422 {
			t.Fatal(workspace, status, v)
		}
		raw, e := os.ReadFile(app.Engine.Options().Config)
		if e != nil || !bytes.Equal(raw, before) {
			t.Fatal("failed save changed native config", e)
		}
	}
	saveApp(t, app, func(c M) {
		for _, v := range A(O(c["workbench"])["assistants"]) {
			if O(v)["agent"] == "root" {
				O(v)["workspace"] = M{"mode": "auto", "directory": filepath.Join(t.TempDir(), "missing")}
			}
		}
	})
}

func TestParityNativeDeliveryContinuation(t *testing.T) {
	f, app := parityApp(t)
	saveApp(t, app, func(c M) { O(c["workbench"])["requireDelivery"] = true })
	nativeScript(f, func(w http.ResponseWriter, r *http.Request, b M) {
		prompt, results := recentNative(b)
		if strings.Contains(prompt, "tuck_automation") && len(results) == 0 {
			nativeDelta(w, nativeCall("deliver_result", M{"summary": "automatic result", "files": []any{M{"path": "report.txt"}}}, ""))
			return
		}
		nativeDelta(w, M{"content": "finished work"})
	})
	task := newParityTask(t, app, nil)
	id := S(task["id"])
	original := []byte("immutable report\n")
	os.WriteFile(filepath.Join(S(task["workspace"]), "report.txt"), original, 0600)
	done := runAppTask(t, app, id, "original user task")
	if I(done["turnCount"]) != 2 || I(O(done["latestDelivery"])["version"]) != 1 {
		t.Fatal(done)
	}
	events, err := app.Store.TaskEvents(id, []string{"native"})
	if err != nil {
		t.Fatal(err)
	}
	stops := 0
	for _, row := range events {
		event := O(row["event"])
		if event["type"] == "stream_stopped" {
			if event["session_id"] != done["nativeSessionId"] || event["reason"] != "normal" {
				t.Fatal("unexpected official completion contract", event)
			}
			stops++
		}
	}
	if stops != 2 {
		t.Fatal("expected one explicit root stop per native turn", stops)
	}
	messages := taskMessages(t, app.Store, id)
	found := false
	for _, msg := range messages {
		if strings.Contains(string(JSON(msg)), "tuck_automation") {
			found = true
			if !strings.Contains(string(JSON(msg)), "delivery-required") {
				t.Fatal("reminder provenance missing", msg)
			}
		}
	}
	if !found || !strings.Contains(string(JSON(messages[0])), "original user task") {
		t.Fatal(messages)
	}
	delivery, _ := app.Store.Delivery(id, "1")
	file := O(A(delivery["files"])[0])
	url := app.URL + "/api/tasks/" + id + "/deliveries/1/files/" + S(file["id"])
	os.WriteFile(filepath.Join(S(task["workspace"]), "report.txt"), []byte("changed"), 0600)
	get := func(method, ran string, want int) []byte {
		t.Helper()
		req, _ := http.NewRequest(method, url, nil)
		req.Header.Set("Range", ran)
		r, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer r.Body.Close()
		raw, _ := io.ReadAll(r.Body)
		if r.StatusCode != want {
			t.Fatal(r.StatusCode, string(raw))
		}
		if method == "HEAD" && r.ContentLength != int64(len(original)) {
			t.Fatal(r.ContentLength)
		}
		return raw
	}
	if !bytes.Equal(get("GET", "", 200), original) || string(get("GET", "bytes=0-7", 206)) != "immutabl" {
		t.Fatal("snapshot changed")
	}
	get("HEAD", "", 200)
	get("GET", "bytes=999-", 416)
	snapshot := filepath.Join(app.Delivery.Root, "snapshots", id, S(delivery["id"]), "files", S(file["id"]))
	os.WriteFile(snapshot, []byte("tampered"), 0600)
	get("GET", "", 409)
	os.Remove(snapshot)
	get("GET", "", 410)
	os.WriteFile(snapshot, original, 0600)
	saveApp(t, app, func(c M) { O(c["workbench"])["requireDelivery"] = false })
	done = runAppTask(t, app, id, "disabled followup")
	if I(done["turnCount"]) != 3 || I(O(done["latestDelivery"])["version"]) != 1 {
		t.Fatal(done)
	}
	saveApp(t, app, func(c M) { O(c["workbench"])["requireDelivery"] = true })
	done = runAppTask(t, app, id, "enabled followup")
	if I(done["turnCount"]) != 5 || I(O(done["latestDelivery"])["version"]) != 2 {
		t.Fatal(done)
	}
	nativeScript(f, func(w http.ResponseWriter, r *http.Request, b M) { nativeDelta(w, M{"content": "no delivery"}) })
	loop := newParityTask(t, app, nil)
	lid := S(loop["id"])
	submitTask(t, app.Manager, lid, "keep working")
	stopped := waitIdle(t, app.Manager, lid)
	count := I(stopped["turnCount"])
	time.Sleep(100 * time.Millisecond)
	if count != 2 || I(mustTask(t, app.Manager, lid)["turnCount"]) != count || len(A(stopped["queue"])) != 0 || stopped["latestDelivery"] != nil {
		t.Fatal(stopped)
	}
	// Genuine user input can independently receive one reminder, even when
	// its text happens to equal Tuck's reminder template.
	done = runAppTask(t, app, lid, DeliveryReminder())
	if I(done["turnCount"]) != 4 || len(A(done["queue"])) != 0 {
		t.Fatal(done)
	}
	nativeScript(f, func(w http.ResponseWriter, r *http.Request, b M) {
		WriteJSON(w, 400, M{"error": M{"message": "fixture model rejected", "type": "invalid_request_error"}})
	})
	failed := newParityTask(t, app, nil)
	submitTask(t, app.Manager, S(failed["id"]), "fail provider")
	done = waitIdle(t, app.Manager, S(failed["id"]))
	if done["lastTurnReason"] != "error" || I(done["turnCount"]) != 1 || len(A(done["queue"])) != 0 {
		t.Fatal(done)
	}
}

func TestParityNativeDeliveryPersistencePause(t *testing.T) {
	f, app := parityApp(t)
	saveApp(t, app, func(c M) { O(c["workbench"])["requireDelivery"] = true })
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	nativeScript(f, func(w http.ResponseWriter, r *http.Request, b M) {
		_, results := recentNative(b)
		if len(results) == 0 {
			once.Do(func() { close(entered) })
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			nativeDelta(w, nativeCall("deliver_result", M{"summary": "cannot commit"}, ""))
			return
		}
		nativeDelta(w, M{"content": "done"})
	})
	task := newParityTask(t, app, nil)
	id := S(task["id"])
	if _, e := app.Store.DB.Exec("CREATE TRIGGER reject_delivery BEFORE INSERT ON task_deliveries BEGIN SELECT RAISE(FAIL,'fixture storage failure'); END"); e != nil {
		t.Fatal(e)
	}
	defer app.Store.DB.Exec("DROP TRIGGER reject_delivery")
	submitTask(t, app.Manager, id, "first")
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		close(release)
		t.Fatal("provider not entered")
	}
	submitTask(t, app.Manager, id, "retained user request")
	close(release)
	done := waitIdle(t, app.Manager, id)
	if done["lastTurnReason"] != "error" || O(done["lastFailure"])["code"] != "persistence_failed" || !B(done["queuePaused"]) || len(A(done["queue"])) != 1 || O(A(done["queue"])[0])["prompt"] != "retained user request" {
		t.Fatal(done)
	}
}

func TestParityNativeDeliveryChildrenAndApproval(t *testing.T) {
	f, app := parityApp(t)
	var childSeen atomic.Bool
	nativeScript(f, func(w http.ResponseWriter, r *http.Request, b M) {
		_, results := recentNative(b)
		names := nativeNames(b)
		if !Contains(names, "transfer_task") {
			childSeen.Store(true)
			if Contains(names, "deliver_result") {
				t.Error("child received delivery tool")
			}
			nativeDelta(w, M{"content": "child result"})
			return
		}
		if len(results) == 0 {
			nativeDelta(w, nativeCall("transfer_task", M{"agent": "worker", "task": "work child"}, ""))
			return
		}
		if len(results) == 1 {
			nativeDelta(w, nativeCall("deliver_result", M{"summary": "parent result"}, ""))
			return
		}
		nativeDelta(w, M{"content": "parent done"})
	})
	task := newParityTask(t, app, nil)
	done := runAppTask(t, app, S(task["id"]), "delegate then deliver")
	if !childSeen.Load() || I(O(done["latestDelivery"])["version"]) != 1 {
		t.Fatal(done)
	}
	saveApp(t, app, func(c M) { O(c["workbench"])["requireDelivery"] = true })
	nativeScript(f, func(w http.ResponseWriter, r *http.Request, b M) {
		_, results := recentNative(b)
		if len(results) == 0 && Contains(nativeNames(b), "deliver_result") {
			nativeDelta(w, nativeCall("deliver_result", M{"summary": "approval result"}, ""))
			return
		}
		nativeDelta(w, M{"content": "done"})
	})
	manual := newParityTask(t, app, M{"approvalMode": "manual"})
	id := S(manual["id"])
	submitTask(t, app.Manager, id, "ask approval")
	pending := waitTask(t, app.Manager, id, func(v M) bool { return v["pendingInteraction"] != nil || !app.Manager.Running(id) })
	time.Sleep(150 * time.Millisecond)
	pending = mustTask(t, app.Manager, id)
	if O(pending["pendingInteraction"])["kind"] != "approval" || I(pending["turnCount"]) != 1 || pending["latestDelivery"] != nil || len(A(pending["queue"])) != 0 {
		t.Fatal(pending)
	}
	app.Manager.Abort(context.Background(), id)
	waitIdle(t, app.Manager, id)
	saveApp(t, app, func(c M) {
		for _, v := range A(O(c["workbench"])["assistants"]) {
			O(v)["deliveryTool"] = false
		}
	})
	disabled := newParityTask(t, app, nil)
	done = runAppTask(t, app, S(disabled["id"]), "tool disabled")
	if done["lastTurnReason"] != "completed" || I(done["turnCount"]) != 1 || done["latestDelivery"] != nil {
		t.Fatal(done)
	}
}

func TestParityNativeDeliveryNullableAndLimit(t *testing.T) {
	f, app := parityApp(t)
	var mu sync.Mutex
	var outputs []M
	schema := M{}
	nativeScript(f, func(w http.ResponseWriter, r *http.Request, b M) {
		_, results := recentNative(b)
		mu.Lock()
		outputs = make([]M, len(results))
		for i, v := range results {
			outputs[i] = Clone(v)
		}
		for _, v := range A(b["tools"]) {
			fn := O(O(v)["function"])
			if fn["name"] == "deliver_result" {
				schema = Clone(O(fn["parameters"]))
			}
		}
		mu.Unlock()
		switch len(results) {
		case 0:
			files := []any{}
			for i := 0; i < 49; i++ {
				files = append(files, M{"path": fmt.Sprintf("file%d.txt", i)})
			}
			nativeDelta(w, nativeCall("deliver_result", M{"summary": "too many", "files": files}, ""))
		case 1:
			nativeDelta(w, nativeCall("deliver_result", M{"summary": "nullable", "files": nil}, ""))
		case 2:
			nativeDelta(w, nativeCall("deliver_result", M{"summary": "omitted"}, ""))
		default:
			nativeDelta(w, M{"content": "done"})
		}
	})
	task := newParityTask(t, app, nil)
	id := S(task["id"])
	done := runAppTask(t, app, id, "delivery shape")
	mu.Lock()
	defer mu.Unlock()
	if len(outputs) != 3 || !strings.Contains(S(outputs[0]["content"]), "49") || !strings.Contains(strings.ToLower(S(outputs[0]["content"])), "archive") {
		t.Fatal(outputs)
	}
	files := O(O(schema["properties"])["files"])
	if I(files["maxItems"]) != 10 || !strings.Contains(string(JSON(files["type"])), "null") {
		t.Fatal(schema)
	}
	if I(O(done["latestDelivery"])["version"]) != 2 {
		t.Fatal(done)
	}
	list, _ := app.Store.Deliveries(id, 0, 20)
	if len(list) != 2 || list[0]["summary"] != "omitted" || list[1]["summary"] != "nullable" || len(A(list[0]["files"])) != 0 || len(A(list[1]["files"])) != 0 {
		t.Fatal(list)
	}
}

func TestParityNativeShutdownPendingMCP(t *testing.T) {
	f := newPlatformFixture(t, 5)
	f.change(func() {
		O(A(O(A(f.snapshot["mcp"])[0])["allowedTools"])[0])["approvalPolicy"] = "AUTO"
		f.controls["chatTool"] = ToolAlias("search_records")
	})
	f.enroll()
	app := f.startApp()
	entered, disconnected := make(chan struct{}), make(chan struct{})
	var start, end sync.Once
	f.change(func() {
		f.hook = func(w http.ResponseWriter, r *http.Request, b M) bool {
			if strings.Contains(r.URL.Path, "/mcp_") && b["method"] == "tools/call" {
				start.Do(func() { close(entered) })
				<-r.Context().Done()
				end.Do(func() { close(disconnected) })
				return true
			}
			return false
		}
	})
	task := newParityTask(t, app, M{"modelId": app.Models.Catalog()[0]["modelId"]})
	id := S(task["id"])
	submitTask(t, app.Manager, id, "pending enterprise call")
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("native tool did not enter upstream", mustTask(t, app.Manager, id))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if e := app.Close(ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case <-disconnected:
	case <-time.After(time.Second):
		t.Fatal("upstream call leaked")
	}
	done := mustTask(t, app.Manager, id)
	if done["lastTurnReason"] != "interrupted" || done["status"] != "idle" || app.Manager.Running(id) {
		t.Fatal(done)
	}
	if app.Store.DB.Ping() == nil {
		t.Fatal("database remained open")
	}
	if app.Models.ctx.Err() == nil {
		t.Fatal("model requests owner alive")
	}
	app.Engine.mu.Lock()
	process := app.Engine.process
	app.Engine.mu.Unlock()
	if process != nil {
		t.Fatal("native process retained", app.Engine.State())
	}
	if r, e := http.Get(app.URL + "/api/health"); e == nil {
		r.Body.Close()
		t.Fatal("HTTP owner remained listening")
	}
}

func TestParityNativeWorkspaceMultiFile(t *testing.T) {
	f := newWorkspaceFixture(t)
	imageData := testPNG(t, 40, 40, false)
	f.mu.Lock()
	f.files["picture.png"] = imageData
	f.files["notes.txt"] = []byte("Batch transfer verified")
	f.mu.Unlock()
	app := f.p.startApp()
	task := newParityTask(t, app, M{"modelId": app.Models.Catalog()[0]["modelId"]})
	id := S(task["id"])
	setScript := func(directory string, overwrite bool, expected int) {
		f.p.change(func() {
			f.p.hook = func(w http.ResponseWriter, r *http.Request, b M) bool {
				if strings.HasPrefix(r.URL.Path, f.davRoot) || strings.Contains(r.URL.Path, "/workspace") {
					f.serve(w, r, b)
					return true
				}
				if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
					return false
				}
				_, results := recentNative(b)
				names := nativeNames(b)
				if len(results) == 0 {
					count := 0
					for _, name := range names {
						if strings.HasSuffix(name, "upload_runcove_file") || strings.HasSuffix(name, "download_runcove_files") {
							count++
						}
					}
					if count != expected {
						t.Errorf("workspace tools %d expected %d: %v", count, expected, names)
					}
					if expected == 0 {
						nativeDelta(w, M{"content": "chat without files"})
						return true
					}
					for _, v := range A(b["tools"]) {
						fn := O(O(v)["function"])
						if fn["name"] == "download_runcove_files" {
							properties := O(O(fn["parameters"])["properties"])
							if I(O(properties["remote_paths"])["maxItems"]) != 20 || !strings.Contains(S(fn["description"]), "1–20") || !strings.Contains(string(JSON(O(properties["overwrite"])["type"])), "boolean") {
								t.Error(fn)
							}
						}
					}
					if Contains(names, "workspace_webdav_download") {
						t.Error("single download tool exposed")
					}
					nativeDelta(w, nativeCall("download_runcove_files", M{"remote_paths": []any{"picture.png", "notes.txt"}, "local_directory": directory, "overwrite": overwrite}, ""))
					return true
				}
				result := string(JSON(results))
				if !strings.Contains(result, "files") || !strings.Contains(result, "notes.txt") || strings.Contains(result, "completed_count") || strings.Contains(result, "Batch transfer verified") || strings.Contains(result, "Replaced through native MCP") || strings.Contains(result, "image_url") || strings.Contains(result, "data:image") {
					t.Error("tool result not metadata-only", result)
				}
				nativeDelta(w, M{"content": "files received"})
				return true
			}
		})
	}
	for i := 1; i <= 2; i++ {
		directory := fmt.Sprintf("round-%d", i)
		setScript(directory, false, 2)
		runAppTask(t, app, id, "Download both files")
		raw, e := os.ReadFile(filepath.Join(S(task["workspace"]), directory, "notes.txt"))
		img, ie := os.ReadFile(filepath.Join(S(task["workspace"]), directory, "picture.png"))
		if e != nil || ie != nil || string(raw) != "Batch transfer verified" || !bytes.Equal(img, imageData) {
			t.Fatal(e, ie, string(raw))
		}
	}
	getCount := func() int {
		f.mu.Lock()
		defer f.mu.Unlock()
		n := 0
		for _, c := range f.calls {
			if c["method"] == "GET" && strings.HasSuffix(S(c["path"]), ".txt") || c["method"] == "GET" && strings.HasSuffix(S(c["path"]), ".png") {
				n++
			}
		}
		return n
	}
	if getCount() != 4 {
		t.Fatal(getCount())
	}
	f.mu.Lock()
	f.files["notes.txt"] = []byte("Replaced through native MCP")
	f.mu.Unlock()
	setScript("round-2", true, 2)
	runAppTask(t, app, id, "Download both files")
	raw, _ := os.ReadFile(filepath.Join(S(task["workspace"]), "round-2/notes.txt"))
	if string(raw) != "Replaced through native MCP" || getCount() != 6 {
		t.Fatal(string(raw), getCount())
	}
	f.mu.Lock()
	O(f.projection["webdav"])["available"] = false
	f.mu.Unlock()
	setScript("unavailable", false, 0)
	runAppTask(t, app, id, "Files unavailable")
	f.mu.Lock()
	O(f.projection["webdav"])["available"] = true
	f.mu.Unlock()
	setScript("recovered", false, 2)
	runAppTask(t, app, id, "Files available again")
	f.mu.Lock()
	f.projection["filesAvailable"] = false
	f.mu.Unlock()
	setScript("files-disabled", false, 0)
	runAppTask(t, app, id, "Projection file availability off")
	f.mu.Lock()
	f.projection["filesAvailable"] = true
	f.mu.Unlock()
	setScript("files-enabled", false, 2)
	runAppTask(t, app, id, "Projection file availability recovered")
}
