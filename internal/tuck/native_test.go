package tuck

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// The fixture speaks a provider protocol to the unmodified official executable.
// It never emulates the engine or opens its private database.
type nativeFixture struct {
	manager  *Manager
	store    *Store
	engine   *Engine
	mu       sync.Mutex
	requests []M
	started  map[string]chan struct{}
	release  map[string]chan struct{}
}

func officialFixture(t *testing.T, iterations int) *nativeFixture {
	t.Helper()
	binary := os.Getenv("TUCK_TEST_ENGINE")
	if binary == "" {
		name := "docker-agent"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		binary = filepath.Join("..", "..", "tests", "runtime", name)
	}
	binary, _ = filepath.Abs(binary)
	if _, err := os.Stat(binary); err != nil {
		t.Fatalf("official Docker Agent required; run tuck engine install and set TUCK_TEST_ENGINE: %v", err)
	}
	f := &nativeFixture{started: map[string]chan struct{}{}, release: map[string]chan struct{}{}}
	provider := httptest.NewServer(http.HandlerFunc(f.respond))
	t.Cleanup(provider.Close)
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "tuck.db"))
	if err != nil {
		t.Fatal(err)
	}
	f.store = store
	reserve, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := reserve.Addr().String()
	_ = reserve.Close()
	configPath := filepath.Join(dir, "fixture.yaml")
	f.engine = NewEngine(EngineOptions{Binary: binary, Config: configPath, DataDir: dir, WorkspaceRoot: filepath.Join(dir, "workspaces"), URL: "http://" + address, ConfigDir: filepath.Join(dir, "engine-config"), Env: map[string]string{"TUCK_NATIVE_TEST_TOKEN": "fixture-only"}})
	f.manager, err = NewManager(ManagerOptions{Store: store, Engine: f.engine, WorkspaceRoot: filepath.Join(dir, "workspaces"), NativeDirectory: dir, AgentRef: "fixture", RefreshFiles: func(task M) ([]any, error) {
		result := []any{}
		err := filepath.WalkDir(S(task["workspace"]), func(path string, d os.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if !d.IsDir() && d.Type().IsRegular() {
				info, e := d.Info()
				if e != nil {
					return e
				}
				rel, _ := filepath.Rel(S(task["workspace"]), path)
				result = append(result, M{"path": rel, "size": info.Size(), "modified": info.ModTime().UnixMilli()})
			}
			return nil
		})
		return result, err
	}})
	if err != nil {
		t.Fatal(err)
	}
	tools := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := ReadJSON(r.Body, 1024*1024)
		if err != nil {
			WriteError(w, err)
			return
		}
		switch r.URL.Path {
		case "/list":
			items := []any{}
			for _, task := range f.manager.List() {
				if S(task["parentTaskId"]) == "" && !B(task["archived"]) {
					items = append(items, M{"task_id": task["id"], "title": task["title"], "status": task["status"]})
				}
			}
			WriteJSON(w, 200, M{"sessions": items})
		case "/send":
			id := strings.TrimSpace(S(body["target_task_id"]))
			task, err := f.manager.Get(id)
			if err != nil {
				WriteError(w, err)
				return
			}
			delivery := "next_turn"
			if S(task["status"]) == "working" {
				delivery = "steer"
			}
			text := "[TUCK_COLLABORATION_MESSAGE]\n" + S(body["message"]) + "\n[/TUCK_COLLABORATION_MESSAGE]"
			if _, err = f.manager.Steer(r.Context(), id, text, nil, M{"kind": "collaboration"}); err != nil {
				WriteError(w, err)
				return
			}
			WriteJSON(w, 200, M{"accepted": true, "delivery": delivery})
		}
	}))
	t.Cleanup(tools.Close)
	toolsList := []any{M{"type": "filesystem", "allow_list": []any{"."}}, M{"type": "todo"}, M{"type": "shell"}, M{"type": "user_prompt"}, M{"type": "background_agents"}, M{"type": "api", "allow_private_ips": true, "api_config": M{"name": "list_sessions", "instruction": "List user conversations", "method": "POST", "endpoint": tools.URL + "/list", "args": M{}}}, M{"type": "api", "allow_private_ips": true, "api_config": M{"name": "send_session_message", "instruction": "Send only to explicitly requested conversation", "method": "POST", "endpoint": tools.URL + "/send", "required": []any{"target_task_id", "message"}, "args": M{"target_task_id": M{"type": "string"}, "message": M{"type": "string"}}}}}
	config := M{"version": 15, "providers": M{"fixture": M{"api_type": "openai_chatcompletions", "base_url": provider.URL + "/v1", "token_key": "TUCK_NATIVE_TEST_TOKEN"}}, "models": M{"test": M{"provider": "fixture", "model": "gpt-4o-mini", "capabilities": M{"image": true}}}, "agents": M{"root": M{"model": "test", "instruction": "Follow the requested fixture task.", "max_iterations": iterations, "sub_agents": []any{"worker"}, "toolsets": toolsList}, "worker": M{"model": "test", "instruction": "CHILD_ONLY independent reviewer.", "toolsets": []any{M{"type": "user_prompt"}}}}, "permissions": M{"allow": []any{"create_todos", "update_todos", "user_prompt", "transfer_task", "*_background_agent*", "list_sessions", "send_session_message"}, "ask": []any{"write_file", "shell"}}}
	raw, err := yaml.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(configPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err = f.engine.Start(ctx); err != nil {
		t.Fatalf("%v: %v", err, f.engine.State())
	}
	t.Cleanup(func() {
		f.mu.Lock()
		for key, release := range f.release {
			close(release)
			delete(f.release, key)
		}
		f.mu.Unlock()
		ctx, c := context.WithTimeout(context.Background(), 15*time.Second)
		defer c()
		_ = f.manager.Close(ctx)
		_ = f.engine.Close()
		_ = store.Close()
	})
	return f
}
func (f *nativeFixture) create(t *testing.T, in M) string {
	t.Helper()
	in = Merge(M{"model": "test"}, in)
	task, err := f.manager.Create(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	return S(task["id"])
}
func (f *nativeFixture) requestList() []M {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := []M{}
	for _, v := range f.requests {
		r = append(r, Clone(v))
	}
	return r
}
func (f *nativeFixture) waitRequest(t *testing.T, marker string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		for _, request := range f.requestList() {
			if strings.Contains(string(JSON(request["messages"])), marker) {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("provider never received " + marker)
}
func (f *nativeFixture) respond(w http.ResponseWriter, r *http.Request) {
	body, err := ReadJSON(r.Body, 104857600)
	if err != nil {
		WriteError(w, err)
		return
	}
	f.mu.Lock()
	f.requests = append(f.requests, Clone(body))
	number := len(f.requests)
	f.mu.Unlock()
	history := A(body["messages"])
	last := -1
	for i, v := range history {
		if S(O(v)["role"]) == "user" {
			last = i
		}
	}
	prompt := ""
	if last >= 0 {
		prompt = string(JSON(history[last]))
	}
	f.mu.Lock()
	var release <-chan struct{}
	for marker, ch := range f.release {
		if strings.Contains(prompt, marker) {
			release = ch
			break
		}
	}
	f.mu.Unlock()
	if release != nil {
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
	}
	recent := history
	if last >= 0 {
		recent = history[last+1:]
	}
	done := map[string]bool{}
	outputs := []M{}
	for _, v := range recent {
		message := O(v)
		for _, c := range A(message["tool_calls"]) {
			done[S(O(O(c)["function"])["name"])] = true
		}
		if S(message["role"]) == "tool" {
			outputs = append(outputs, message)
		}
	}
	tool := func(name string, args M) M {
		return M{"id": fmt.Sprintf("call-%d-%s", number, name), "type": "function", "function": M{"name": name, "arguments": string(JSON(args))}}
	}
	calls := []any{}
	content := "测试端点执行完成：已观察真实工具结果。"
	child := true
	for _, v := range A(body["tools"]) {
		if S(O(O(v)["function"])["name"]) == "transfer_task" {
			child = false
		}
	}
	if strings.Contains(prompt, "STEER_TARGET") || strings.Contains(prompt, "QUEUE_DELAY") {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(700 * time.Millisecond):
		}
	}
	if strings.Contains(prompt, "SLOW") {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(10 * time.Second):
		}
	}
	switch {
	case strings.Contains(prompt, "FAIL_MODEL"):
		WriteJSON(w, 400, M{"error": M{"message": "Intentional test provider failure", "type": "invalid_request_error"}})
		return
	case strings.Contains(string(JSON(history)), "CHILD_QUESTION") && child:
		if !done["user_prompt"] {
			calls = append(calls, tool("user_prompt", M{"message": "子助手需要确认素材", "schema": M{"type": "object", "properties": M{"material": M{"type": "string"}}, "required": []any{"material"}}}))
		}
	case strings.Contains(string(JSON(history)), "CHILD_ONLY") && child:
		content = "子 Agent 已独立完成检查。"
	case strings.Contains(prompt, "MULTI_QUESTION"):
		if !done["user_prompt"] {
			for i, title := range []string{"素材", "尺寸"} {
				c := tool("user_prompt", M{"message": "请提供" + title, "schema": M{"type": "object", "properties": M{"value": M{"type": "string", "title": title}}, "required": []any{"value"}}})
				c["id"] = fmt.Sprintf("question-%d-%d", number, i)
				calls = append(calls, c)
			}
			if strings.Contains(prompt, "WITH_APPROVAL") {
				calls = append(calls, tool("write_file", M{"path": "confirmed.txt", "content": "confirmed"}))
			}
		}
	case strings.Contains(prompt, "CHILD_QUESTION"):
		if !done["transfer_task"] {
			calls = append(calls, tool("transfer_task", M{"agent": "worker", "task": "CHILD_QUESTION 确认素材", "expected_output": "已确认的素材"}))
		}
	case strings.Contains(prompt, "REJECT_ONLY"):
		if !done["write_file"] {
			calls = append(calls, tool("write_file", M{"path": "rejected.txt", "content": "must not be written"}))
		}
	case strings.Contains(prompt, "BACKGROUND_LOOP"):
		if !done["run_background_agent"] {
			calls = append(calls, tool("run_background_agent", M{"agent": "worker", "task": "CHILD_ONLY 后台检查"}))
		} else if !done["wait_background_agents"] {
			id := ""
			for _, o := range outputs {
				match := backgroundStarted.FindStringSubmatch(S(o["content"]))
				if match != nil {
					id = match[1]
				}
			}
			calls = append(calls, tool("wait_background_agents", M{"task_ids": []any{id}, "timeout": 5}))
		}
	case strings.Contains(prompt, "CROSS_SESSION_"):
		if !done["list_sessions"] {
			calls = append(calls, tool("list_sessions", M{}))
		} else if !done["send_session_message"] {
			id := ""
			for _, o := range outputs {
				for _, v := range A(O(parseValue(o["content"]))["sessions"]) {
					if S(O(v)["title"]) == "消息接收方" {
						id = S(O(v)["task_id"])
					}
				}
			}
			text := "执行中协作测试消息"
			if strings.Contains(prompt, "IDLE") {
				text = "空闲协作测试消息"
			}
			calls = append(calls, tool("send_session_message", M{"target_task_id": id, "message": text}))
		}
	case strings.Contains(prompt, "FULL_LOOP"):
		switch {
		case !done["create_todos"]:
			calls = append(calls, tool("create_todos", M{"descriptions": []any{"生成报告", "验证报告"}}))
		case !done["user_prompt"]:
			calls = append(calls, tool("user_prompt", M{"message": "报告标题是什么？", "schema": M{"type": "object", "properties": M{"title": M{"type": "string"}}, "required": []any{"title"}}}))
		case !done["write_file"]:
			calls = append(calls, tool("write_file", M{"path": "report.md", "content": "# 真实工具报告\nDocker Agent native output\n"}))
		case !done["shell"]:
			cmd := "cat report.md"
			if runtime.GOOS == "windows" {
				cmd = "powershell -NoProfile -Command \"Get-Content report.md\""
			}
			calls = append(calls, tool("shell", M{"cmd": cmd}))
		case !done["transfer_task"]:
			calls = append(calls, tool("transfer_task", M{"agent": "worker", "task": "CHILD_ONLY 检查报告", "expected_output": "检查结果"}))
		case !done["update_todos"]:
			updates := []any{}
			for _, o := range outputs {
				for _, v := range A(O(parseValue(o["content"]))["all_todos"]) {
					updates = append(updates, M{"id": O(v)["id"], "status": "completed"})
				}
			}
			calls = append(calls, tool("update_todos", M{"updates": updates}))
		default:
			content = "已生成 [report.md](report.md)，终端和子任务验证完成。"
		}
	}
	if !B(body["stream"]) {
		WriteJSON(w, 200, M{"id": "test", "choices": []any{M{"index": 0, "message": M{"role": "assistant", "content": content, "tool_calls": calls}, "finish_reason": "stop"}}})
		return
	}
	delta := M{"role": "assistant", "content": content}
	if len(calls) > 0 {
		delta = M{"role": "assistant"}
		for i, v := range calls {
			O(v)["index"] = i
		}
		delta["tool_calls"] = calls
	}
	w.Header().Set("Content-Type", "text/event-stream")
	frame := func(delta M, finish any, usage bool) {
		packet := M{"id": fmt.Sprintf("completion-%d", number), "object": "chat.completion.chunk", "created": 1, "model": "gpt-4o-mini", "choices": []any{M{"index": 0, "delta": delta, "finish_reason": finish}}}
		if usage {
			packet["usage"] = M{"prompt_tokens": 120, "completion_tokens": 30, "total_tokens": 150}
		}
		fmt.Fprintf(w, "data: %s\n\n", JSON(packet))
		w.(http.Flusher).Flush()
	}
	frame(delta, nil, false)
	if strings.Contains(prompt, "METRICS_STREAM") && len(calls) == 0 {
		time.Sleep(200 * time.Millisecond)
		frame(M{"content": "流式计时验收。"}, nil, false)
	}
	finish := "stop"
	if len(calls) > 0 {
		finish = "tool_calls"
	}
	frame(M{}, finish, true)
	fmt.Fprint(w, "data: [DONE]\n\n")
}
func resolveAllNative(t *testing.T, f *nativeFixture, id string) M {
	t.Helper()
	deadline := time.Now().Add(50 * time.Second)
	seen := map[string]bool{}
	for time.Now().Before(deadline) {
		task := mustTask(t, f.manager, id)
		pending := O(task["pendingInteraction"])
		if len(pending) > 0 && !seen[S(pending["id"])] {
			seen[S(pending["id"])] = true
			if S(pending["kind"]) == "question" {
				assertStatus(t, f.manager.ResolveInteraction(context.Background(), id, M{"interactionId": pending["id"], "kind": "question", "answer": "{}"}), 422)
				if O(mustTask(t, f.manager, id)["pendingInteraction"])["id"] != pending["id"] {
					t.Fatal("invalid answer discarded waiter")
				}
			}
			response := M{"interactionId": pending["id"], "kind": pending["kind"], "approved": true, "answer": "验收报告"}
			if _, exists := O(O(pending["schema"])["properties"])["value"]; exists {
				response["answer"] = `{"value":"valid"}`
			}
			if _, exists := O(O(pending["schema"])["properties"])["material"]; exists {
				response["answer"] = `{"material":"sample.png"}`
			}
			if e := f.manager.ResolveInteraction(context.Background(), id, response); e != nil {
				t.Fatal(e)
			}
		}
		if !f.manager.Running(id) && S(task["lastTurnReason"]) != "" {
			return task
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("native loop timed out: %s", JSON(mustTask(t, f.manager, id)))
	return nil
}

func TestOfficialNativeFullLoop(t *testing.T) {
	f := officialFixture(t, 16)
	if S(f.engine.State()["version"]) != "v1.150.0" {
		t.Fatal("test contract requires official v1.150.0", f.engine.State())
	}
	id := f.create(t, nil)
	submitTask(t, f.manager, id, "FULL_LOOP")
	task := resolveAllNative(t, f, id)
	if S(task["lastTurnReason"]) != "completed" {
		t.Fatal(task)
	}
	raw, e := os.ReadFile(filepath.Join(S(task["workspace"]), "report.md"))
	if e != nil || !strings.Contains(string(raw), "Docker Agent") {
		t.Fatal(string(raw), e)
	}
	for _, v := range A(O(task["activePlan"])["items"]) {
		if O(v)["status"] != "completed" {
			t.Fatal(task["activePlan"])
		}
	}
	messages, _ := f.store.Messages(id, 0, 4096)
	terminal := false
	for _, msg := range messages {
		if block := findBlock(msg, "terminal"); block != nil && S(O(block["data"])["status"]) == "success" && strings.Contains(S(O(block["data"])["output"]), "Docker Agent") {
			terminal = true
		}
	}
	if !terminal || I(task["logicalCount"]) != len(messages) {
		t.Fatal("history/terminal mismatch", task)
	}
	children := 0
	for _, child := range f.manager.List() {
		if S(child["parentTaskId"]) == id {
			children++
			if S(child["lastTurnReason"]) != "completed" || I(child["logicalCount"]) == 0 {
				t.Fatal(child)
			}
		}
	}
	if children != 1 {
		t.Fatal("child identity", children)
	}
	rejected := f.create(t, nil)
	submitTask(t, f.manager, rejected, "REJECT_ONLY")
	waiting := waitTask(t, f.manager, rejected, func(t M) bool { return t["pendingInteraction"] != nil })
	p := O(waiting["pendingInteraction"])
	if e = f.manager.ResolveInteraction(context.Background(), rejected, M{"interactionId": p["id"], "kind": "approval", "approved": false}); e != nil {
		t.Fatal(e)
	}
	waitIdle(t, f.manager, rejected)
	if _, e = os.Stat(filepath.Join(S(waiting["workspace"]), "rejected.txt")); !os.IsNotExist(e) {
		t.Fatal("rejected file exists", e)
	}
	background := f.create(t, nil)
	submitTask(t, f.manager, background, "BACKGROUND_LOOP")
	b := waitIdle(t, f.manager, background)
	if S(b["lastTurnReason"]) != "completed" || len(A(b["backgroundRuns"])) != 1 || S(O(A(b["backgroundRuns"])[0])["status"]) != "completed" {
		t.Fatal(b)
	}
	for _, child := range f.manager.List() {
		if S(child["parentTaskId"]) == background {
			t.Fatal("background private identity inferred")
		}
	}
	failed := f.create(t, nil)
	submitTask(t, f.manager, failed, "FAIL_MODEL")
	failure := waitIdle(t, f.manager, failed)
	if S(failure["lastTurnReason"]) != "error" || !strings.Contains(S(O(failure["lastFailure"])["message"]), "Intentional test") {
		t.Fatal(failure)
	}
}
func TestOfficialNativeIsolationForkAndSteering(t *testing.T) {
	f := officialFixture(t, 16)
	first := f.create(t, nil)
	submitTask(t, f.manager, first, "PRIVATE_PROMPT")
	waitIdle(t, f.manager, first)
	second := f.create(t, nil)
	submitTask(t, f.manager, second, "FRESH_PROMPT")
	waitIdle(t, f.manager, second)
	for _, req := range f.requestList() {
		text := string(JSON(req["messages"]))
		if strings.Contains(text, "FRESH_PROMPT") && strings.Contains(text, "PRIVATE_PROMPT") {
			t.Fatal("history leaked", req)
		}
	}
	submitTask(t, f.manager, first, "CONTINUE_FIRST")
	waitIdle(t, f.manager, first)
	found := false
	for _, req := range f.requestList() {
		text := string(JSON(req["messages"]))
		if strings.Contains(text, "CONTINUE_FIRST") && strings.Contains(text, "PRIVATE_PROMPT") {
			found = true
		}
	}
	if !found {
		t.Fatal("native history not continued")
	}
	branch, e := f.manager.Fork(context.Background(), first, 1)
	if e != nil {
		t.Fatal(e)
	}
	if branch["draft"] != "CONTINUE_FIRST" {
		t.Fatal(branch)
	}
	branchID := S(O(branch["task"])["id"])
	native, e := f.engine.Request(context.Background(), "GET", nativePath(O(branch["task"]), ""), nil)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(JSON(native["messages"])), "CONTINUE_FIRST") {
		t.Fatal("fork included selected prompt")
	}
	submitTask(t, f.manager, branchID, "FORK_NEW")
	waitIdle(t, f.manager, branchID)
	steer := f.create(t, nil)
	submitTask(t, f.manager, steer, "STEER_TARGET")
	f.waitRequest(t, "STEER_TARGET")
	if _, e = f.manager.Steer(context.Background(), steer, "TUCK_STEER_MARKER", nil, nil); e != nil {
		t.Fatal(e)
	}
	task := waitIdle(t, f.manager, steer)
	if I(task["turnCount"]) != 1 {
		t.Fatal(task)
	}
	found = false
	for _, req := range f.requestList() {
		count := 0
		for _, v := range A(req["messages"]) {
			msg := O(v)
			if msg["role"] == "user" && msg["content"] == "TUCK_STEER_MARKER" {
				count++
			}
		}
		if count > 1 {
			t.Fatal("steer duplicated")
		}
		found = found || count == 1
	}
	if !found {
		t.Fatal("steer absent at actual provider")
	}
	messages, _ := f.store.Messages(steer, 0, 100)
	delivered := false
	for _, msg := range messages {
		if S(O(msg["steering"])["status"]) == "delivered" {
			delivered = true
		}
	}
	if !delivered {
		t.Fatal("native receipt not applied", messages)
	}
}
func TestOfficialNativeParallelQuestions(t *testing.T) {
	f := officialFixture(t, 16)
	for _, prompt := range []string{"MULTI_QUESTION WITH_APPROVAL", "CHILD_QUESTION"} {
		id := f.create(t, nil)
		submitTask(t, f.manager, id, prompt)
		task := resolveAllNative(t, f, id)
		if S(task["lastTurnReason"]) != "completed" || task["pendingInteraction"] != nil {
			t.Fatal(task)
		}
	}
	stopped := f.create(t, nil)
	submitTask(t, f.manager, stopped, "MULTI_QUESTION WITH_APPROVAL")
	waiting := waitTask(t, f.manager, stopped, func(t M) bool { return t["pendingInteraction"] != nil })
	pending := O(waiting["pendingInteraction"])
	if e := f.manager.Abort(context.Background(), stopped); e != nil {
		t.Fatal(e)
	}
	task := mustTask(t, f.manager, stopped)
	if task["pendingInteraction"] != nil || S(task["lastTurnReason"]) != "interrupted" {
		t.Fatal(task)
	}
	assertStatus(t, f.manager.ResolveInteraction(context.Background(), stopped, M{"interactionId": pending["id"], "kind": pending["kind"], "answer": nil, "approved": true}), 409)
}
func TestOfficialNativeIterationDecision(t *testing.T) {
	f := officialFixture(t, 1)
	for _, approved := range []bool{true, false} {
		id := f.create(t, M{"approvalMode": "auto"})
		submitTask(t, f.manager, id, "REJECT_ONLY")
		task := waitTask(t, f.manager, id, func(t M) bool { return S(O(t["pendingInteraction"])["purpose"]) == "iterations" })
		pending := O(task["pendingInteraction"])
		started := N(O(task["turnTiming"])["startedAt"])
		requests := len(f.requestList())
		if e := f.manager.ResolveInteraction(context.Background(), id, M{"interactionId": pending["id"], "kind": "approval", "approved": approved}); e != nil {
			t.Fatal(e)
		}
		task = waitIdle(t, f.manager, id)
		want := "interrupted"
		additional := 0
		if approved {
			want = "completed"
			additional = 1
		}
		if S(task["lastTurnReason"]) != want || len(f.requestList()) != requests+additional || N(O(task["turnTiming"])["startedAt"]) != started || O(task["turnTiming"])["waitingSince"] != nil {
			t.Fatal(task)
		}
		assertStatus(t, f.manager.ResolveInteraction(context.Background(), id, M{"interactionId": pending["id"], "kind": "approval", "approved": true}), 409)
	}
}
func TestOfficialNativeMetricsPolicyQueueAndStop(t *testing.T) {
	f := officialFixture(t, 16)
	id := f.create(t, nil)
	submitTask(t, f.manager, id, "REJECT_ONLY METRICS_STREAM")
	waiting := waitTask(t, f.manager, id, func(t M) bool { return t["pendingInteraction"] != nil })
	pending := O(waiting["pendingInteraction"])
	if I(O(waiting["timings"])["toolSamples"]) != 0 {
		t.Fatal("approval counted as tool execution")
	}
	if _, e := f.manager.SetApprovalMode(context.Background(), id, "auto"); e != nil {
		t.Fatal(e)
	}
	if O(mustTask(t, f.manager, id)["pendingInteraction"])["id"] != pending["id"] {
		t.Fatal("policy change resolved pending approval")
	}
	if e := f.manager.ResolveInteraction(context.Background(), id, M{"interactionId": pending["id"], "kind": "approval", "approved": true}); e != nil {
		t.Fatal(e)
	}
	task := waitIdle(t, f.manager, id)
	if I(O(task["timings"])["toolSamples"]) != 1 || I(O(task["timings"])["streamSamples"]) != 1 || N(O(task["timings"])["streamMs"]) < 150 || !B(O(task["context"])["reported"]) {
		t.Fatal(task)
	}
	queue := f.create(t, nil)
	submitTask(t, f.manager, queue, "STEER_TARGET")
	f.waitRequest(t, "STEER_TARGET")
	submitTask(t, f.manager, queue, "later one")
	submitTask(t, f.manager, queue, "later two")
	queued := A(mustTask(t, f.manager, queue)["queue"])
	entry := S(O(queued[1])["id"])
	if _, e := f.manager.UpdateQueue(context.Background(), queue, M{"action": "edit", "entryId": entry, "prompt": "edited followup"}); e != nil {
		t.Fatal(e)
	}
	if _, e := f.manager.UpdateQueue(context.Background(), queue, M{"action": "send", "entryId": entry}); e != nil {
		t.Fatal(e)
	}
	task = waitIdle(t, f.manager, queue)
	if I(task["turnCount"]) != 2 {
		t.Fatal(task)
	}
	native, e := f.engine.Request(context.Background(), "GET", nativePath(task, ""), nil)
	if e != nil {
		t.Fatal(e)
	}
	text := string(JSON(native["messages"]))
	if strings.Count(text, "edited followup") != 1 || strings.Count(text, "later one") != 1 || strings.Contains(text, "later two") {
		t.Fatal(text)
	}
	submitTask(t, f.manager, queue, "SLOW")
	f.waitRequest(t, "SLOW")
	submitTask(t, f.manager, queue, "preserved after stop")
	if e = f.manager.Abort(context.Background(), queue); e != nil {
		t.Fatal(e)
	}
	task = mustTask(t, f.manager, queue)
	if !B(task["queuePaused"]) || len(A(task["queue"])) != 1 {
		t.Fatal(task)
	}
	if _, e = f.manager.UpdateQueue(context.Background(), queue, M{"action": "resume"}); e != nil {
		t.Fatal(e)
	}
	task = waitIdle(t, f.manager, queue)
	if len(A(task["queue"])) != 0 || S(task["lastTurnReason"]) != "completed" {
		t.Fatal(task)
	}
}
func TestOfficialNativeCollaboration(t *testing.T) {
	f := officialFixture(t, 16)
	target := f.create(t, M{"title": "消息接收方"})
	sender := f.create(t, nil)
	submitTask(t, f.manager, target, "STEER_TARGET")
	f.waitRequest(t, "STEER_TARGET")
	submitTask(t, f.manager, sender, "CROSS_SESSION_WORKING")
	task := waitIdle(t, f.manager, sender)
	if S(task["lastTurnReason"]) != "completed" {
		t.Fatal(task)
	}
	waitIdle(t, f.manager, target)
	submitTask(t, f.manager, sender, "CROSS_SESSION_IDLE")
	waitIdle(t, f.manager, sender)
	waitIdle(t, f.manager, target)
	for _, marker := range []string{"执行中协作测试消息", "空闲协作测试消息"} {
		found := false
		for _, r := range f.requestList() {
			for _, v := range A(r["messages"]) {
				msg := O(v)
				if S(msg["role"]) == "user" && strings.Contains(S(msg["content"]), marker) {
					found = true
				}
			}
		}
		if !found {
			t.Fatal("collaboration missing at actual provider", marker)
		}
	}
}

func TestOfficialNativeActivityAndChannel(t *testing.T) {
	f := officialFixture(t, 16)
	gate := make(chan struct{})
	f.mu.Lock()
	f.release["ACTIVITY_PROBE"] = gate
	f.mu.Unlock()
	id := f.create(t, nil)
	submitTask(t, f.manager, id, "ACTIVITY_PROBE")
	f.waitRequest(t, "ACTIVITY_PROBE")
	task := mustTask(t, f.manager, id)
	phase := S(O(task["activity"])["phase"])
	if S(task["status"]) != "working" || (phase != "processing" && phase != "preparing") {
		t.Fatal(task)
	}
	messages, _ := f.store.Messages(id, 0, 100)
	for _, msg := range messages {
		if msg["role"] == "assistant" {
			t.Fatal("synthetic assistant before native output")
		}
	}
	f.mu.Lock()
	close(gate)
	delete(f.release, "ACTIVITY_PROBE")
	f.mu.Unlock()
	task = waitIdle(t, f.manager, id)
	if task["activity"] != nil {
		t.Fatal(task)
	}
	config := f.manager.Config()
	O(A(O(config["workbench"])["assistants"])[0])["taskConcurrency"] = 1
	f.manager.options.Config = func() M { return Clone(config) }
	first := f.create(t, M{"title": "T-ab12 · 原任务"})
	next := f.create(t, M{"title": "T-ab13 · 下个任务"})
	submitTask(t, f.manager, first, "MULTI_QUESTION")
	waitTask(t, f.manager, first, func(t M) bool { return t["pendingInteraction"] != nil })
	_, err := f.manager.Submit(next, "next", nil, nil, nil)
	assertStatus(t, err, 409)
	renamed, err := f.manager.Rename(context.Background(), first, "改名")
	if err != nil || renamed["title"] != "T-ab12 · 改名" {
		t.Fatal(renamed, err)
	}
	ordinary := f.create(t, nil)
	submitTask(t, f.manager, ordinary, "ordinary chat")
	if S(waitIdle(t, f.manager, ordinary)["lastTurnReason"]) != "completed" {
		t.Fatal("ordinary chat blocked")
	}
	resolveAllNative(t, f, first)
	branch, err := f.manager.Fork(context.Background(), first, 0)
	if err != nil || ParseTaskTitle(S(O(branch["task"])["title"])) != nil {
		t.Fatal(branch, err)
	}
	submitTask(t, f.manager, next, "next")
	waitIdle(t, f.manager, next)
}

// Avoid unused codec drift: the fixture's JSON is ordinary provider JSON.
var _ = json.Valid
