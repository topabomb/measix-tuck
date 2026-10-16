package tuck

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestQueueEditDuringPreparationUsesCurrentEntry(t *testing.T) {
	m, _, engine, id := taskFixture(t)
	preparing, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	var preparations int
	m.options.PreparePrompt = func(_ M, prompt string, _ []string) (M, []any, error) {
		preparations++
		if preparations == 1 {
			close(preparing)
			<-release
		}
		return M{"role": "user", "content": prompt}, nil, nil
	}
	sent := make(chan string, 2)
	engine.run = func(_ context.Context, session, _, _ string, messages []M, _ string, _ map[string]string, fn func(M) error) error {
		sent <- S(messages[0]["content"])
		return fn(M{"type": "stream_stopped", "session_id": session, "reason": "completed"})
	}
	if _, err := m.Submit(id, "before edit", []string{"attachment"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	<-preparing
	entry := O(A(mustTask(t, m, id)["queue"])[0])
	if _, err := m.UpdateQueue(context.Background(), id, M{"action": "edit", "entryId": entry["id"], "prompt": "after edit"}); err != nil {
		t.Fatal(err)
	}
	unblock()
	waitIdle(t, m, id)
	if got := <-sent; got != "after edit" {
		t.Fatalf("sent obsolete queue content: %q", got)
	}
}

func TestQueueSubmittedDuringFinalRefreshIsStarted(t *testing.T) {
	m, _, engine, id := taskFixture(t)
	finishing, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	var refreshes atomic.Int32
	m.options.RefreshFiles = func(M) ([]any, error) {
		if refreshes.Add(1) == 2 {
			close(finishing)
			<-release
		}
		return nil, nil
	}
	sent := make(chan string, 2)
	engine.run = func(_ context.Context, session, _, _ string, messages []M, _ string, _ map[string]string, fn func(M) error) error {
		sent <- S(messages[0]["content"])
		return fn(M{"type": "stream_stopped", "session_id": session, "reason": "completed"})
	}
	submitTask(t, m, id, "first")
	<-finishing
	if got := <-sent; got != "first" {
		t.Fatal(got)
	}
	submitTask(t, m, id, "during final refresh")
	unblock()
	select {
	case got := <-sent:
		if got != "during final refresh" {
			t.Fatal(got)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("accepted queue stranded: %s", JSON(mustTask(t, m, id)))
	}
	waitTask(t, m, id, func(task M) bool { return !m.Running(id) && I(task["turnCount"]) == 2 })
}

func TestUnfinishedToolMessagesSettleAfterDisconnectAndRestart(t *testing.T) {
	for _, restart := range []bool{false, true} {
		t.Run(fmt.Sprint(restart), func(t *testing.T) {
			m, store, engine, id := taskFixture(t)
			createTools := func() {
				for _, call := range []string{"first", "second"} {
					mustEvent(t, m, id, M{"type": "tool_call", "tool_call": M{"id": call, "function": M{"name": "shell", "arguments": `{"cmd":"echo hello"}`}}})
					mustEvent(t, m, id, M{"type": "tool_call_output", "tool_call_id": call, "output": "hello"})
				}
			}
			if restart {
				mustUpdate(t, m, id, M{"status": "working"})
				createTools()
				if _, err := NewManager(m.options); err != nil {
					t.Fatal(err)
				}
			} else {
				engine.run = func(context.Context, string, string, string, []M, string, map[string]string, func(M) error) error {
					createTools()
					return io.ErrUnexpectedEOF
				}
				submitTask(t, m, id, "execute")
				waitIdle(t, m, id)
			}
			messages, err := store.Messages(id, 0, 100)
			if err != nil {
				t.Fatal(err)
			}
			for _, msg := range messages {
				if findBlock(msg, "tool-call") == nil {
					continue
				}
				if B(msg["live"]) || S(O(findBlock(msg, "terminal")["data"])["status"]) != "interrupted" {
					t.Errorf("unfinished tool still presented as running: %s", JSON(msg))
				}
			}
			before, _ := store.Messages(id, 0, 100)
			if _, err = NewManager(m.options); err != nil {
				t.Fatal(err)
			}
			after, _ := store.Messages(id, 0, 100)
			if !reflect.DeepEqual(before, after) {
				t.Fatal("settled tool history changed on another restart")
			}
		})
	}
}

func TestToolCallIdentityIsScopedToExecutionAndAgent(t *testing.T) {
	m, store, _, first := taskFixture(t)
	second, err := m.Create(context.Background(), M{})
	if err != nil {
		t.Fatal(err)
	}
	childNative := UUID()
	mustEvent(t, m, first, M{"type": "stream_started", "session_id": childNative, "agent_name": "worker"})
	child := ""
	for _, task := range m.List() {
		if task["nativeSessionId"] == childNative {
			child = S(task["id"])
		}
	}
	if child == "" {
		t.Fatal("missing child session")
	}
	cases := []struct{ root, owner, agent, name string }{
		{first, first, "root", "shell"},
		{S(second["id"]), S(second["id"]), "root", "read_file"},
		{first, child, "worker", "search_files"},
	}
	for _, test := range cases {
		mustEvent(t, m, test.root, M{"type": "tool_call", "agent_name": test.agent, "tool_call": M{"id": "call_0", "function": M{"name": test.name, "arguments": `{}`}}})
	}
	for _, test := range cases {
		mustEvent(t, m, test.root, M{"type": "tool_call_response", "agent_name": test.agent, "tool_call_id": "call_0", "response": test.name + " result"})
	}
	for _, test := range cases {
		messages, err := store.Messages(test.owner, 0, 100)
		if err != nil {
			t.Fatal(err)
		}
		calls, results := 0, 0
		for _, message := range messages {
			for _, value := range A(message["blocks"]) {
				block := O(value)
				if S(block["type"]) != "tool-call" && S(block["type"]) != "tool-result" {
					continue
				}
				data := O(block["data"])
				if data["name"] != test.name || data["status"] != "success" {
					t.Errorf("tool result attributed to another execution: owner=%s block=%s", test.owner, JSON(block))
				}
				if block["type"] == "tool-call" {
					calls++
				} else {
					results++
					if data["output"] != test.name+" result" {
						t.Error("tool output crossed conversation boundary")
					}
				}
			}
		}
		if calls != 1 || results != 1 {
			t.Errorf("owner %s has %d calls and %d results", test.owner, calls, results)
		}
	}
}

func TestToolTimingsWithRepeatedIDsAcrossTasks(t *testing.T) {
	var timings runtimeTimings
	first, second := M{"id": "first"}, M{"id": "second"}
	start := time.Now().UTC()
	call := func(task M, delta time.Duration) {
		timings.observe(task, M{"type": "tool_call", "timestamp": start.Add(delta).Format(time.RFC3339Nano), "tool_call": M{"id": "call_0"}})
	}
	finish := func(task M, delta time.Duration, want float64) {
		got := timings.observe(task, M{"type": "tool_call_response", "timestamp": start.Add(delta).Format(time.RFC3339Nano), "tool_call_id": "call_0"})
		if N(got["toolMs"]) != want || N(got["toolSamples"]) != 1 {
			t.Fatalf("incorrect task timing: %s", JSON(got))
		}
	}
	call(first, 0)
	call(second, 10*time.Millisecond)
	finish(second, 30*time.Millisecond, 20)
	finish(first, 40*time.Millisecond, 40)
}

func TestToolCallIDMayBeReusedAfterAResult(t *testing.T) {
	m, store, _, id := taskFixture(t)
	for _, name := range []string{"read_file", "search_files"} {
		mustEvent(t, m, id, M{"type": "tool_call", "agent_name": "root", "tool_call": M{"id": "call_0", "function": M{"name": name, "arguments": `{}`}}})
		mustEvent(t, m, id, M{"type": "tool_call_response", "agent_name": "root", "tool_call_id": "call_0", "response": name})
	}
	messages, err := store.Messages(id, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	calls := []string{}
	for _, msg := range messages {
		if block := findBlock(msg, "tool-call"); block != nil {
			calls = append(calls, S(O(block["data"])["name"]))
		}
	}
	if !reflect.DeepEqual(calls, []string{"read_file", "search_files"}) {
		t.Fatalf("second call reused first call's record: %v", calls)
	}
}

func TestDisconnectedParentSettlesUnfinishedChildren(t *testing.T) {
	m, store, engine, id := taskFixture(t)
	childNative := UUID()
	engine.run = func(context.Context, string, string, string, []M, string, map[string]string, func(M) error) error {
		mustEvent(t, m, id, M{"type": "stream_started", "session_id": childNative, "agent_name": "worker"})
		mustEvent(t, m, id, M{"type": "tool_call", "agent_name": "worker", "tool_call": M{"id": "child-call", "function": M{"name": "shell", "arguments": `{}`}}})
		mustEvent(t, m, id, M{"type": "tool_call_output", "agent_name": "worker", "tool_call_id": "child-call", "output": "partial"})
		return io.ErrUnexpectedEOF
	}
	submitTask(t, m, id, "delegate")
	waitIdle(t, m, id)
	for _, child := range m.List() {
		if child["nativeSessionId"] != childNative {
			continue
		}
		if child["status"] != "idle" || S(O(child["lastFailure"])["code"]) != "execution_disconnected" {
			t.Fatalf("child remains active after losing its execution stream: %s", JSON(child))
		}
		messages, _ := store.Messages(S(child["id"]), 0, 100)
		for _, msg := range messages {
			if B(msg["live"]) || S(O(findBlock(msg, "terminal")["data"])["status"]) == "running" {
				t.Fatal("child tool left running")
			}
		}
	}
	if m.Active() {
		t.Fatal("disconnected child still blocks configuration changes")
	}
}

func TestRestartReadsNativeReceiptsOnlyForUnresolvedSteering(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "tuck.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	manager, err := NewManager(ManagerOptions{Store: store, Engine: &fakeNative{}, WorkspaceRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	task, err := manager.Create(context.Background(), M{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Publish(task, nil, M{"type": "native", "event": M{"type": "tool_call_output", "output": strings.Repeat("x", 1<<20)}}); err != nil {
		t.Fatal(err)
	}
	if err = store.SaveMessage(S(task["id"]), M{"index": 0, "revision": 0, "blocks": []any{newBlock("ordinary", "markdown", M{"markdown": strings.Repeat("x", 1<<20)})}}); err != nil {
		t.Fatal(err)
	}
	underlying := store.DB
	var eventRows, messageRows atomic.Int32
	name := "backend-review-" + ID()
	sql.Register(name, completionAuditDriver{Driver: underlying.Driver(), after: func(query string) {
		if strings.Contains(query, "FROM events") {
			eventRows.Add(1)
		}
		if strings.Contains(query, "FROM messages") {
			messageRows.Add(1)
		}
	}})
	store.DB, err = sql.Open(name, filepath.ToSlash(store.Path)+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	defer store.DB.Close()
	defer underlying.Close()
	if _, err = NewManager(manager.options); err != nil {
		t.Fatal(err)
	}
	if eventRows.Load() != 0 {
		t.Fatalf("startup decoded %d unrelated native event rows without pending steering", eventRows.Load())
	}
	if messageRows.Load() != 0 {
		t.Fatalf("steering recovery decoded %d unrelated transcript rows", messageRows.Load())
	}
}

func TestCreationCannotPublishAcrossConfigurationSwitch(t *testing.T) {
	m, _, engine, _ := taskFixture(t)
	creating, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	engine.request = func(_ context.Context, method, path string, _ any) (M, error) {
		if method == "POST" && path == "/api/sessions" {
			close(creating)
			<-release
			return M{"id": "session-from-previous-engine"}, nil
		}
		return M{}, nil
	}
	done := make(chan error, 1)
	go func() {
		_, err := m.Create(context.Background(), M{})
		done <- err
	}()
	<-creating
	m.SetConfigurationChanging(true)
	m.SetConfigurationChanging(false)
	unblock()
	if err := <-done; Code(err) != "configuration_changing" {
		t.Fatalf("stale native response became a current task: %v", err)
	}
	if len(m.List()) != 1 {
		t.Fatal("published task from the previous configuration")
	}
}

func TestForkCannotPublishAcrossConfigurationSwitch(t *testing.T) {
	m, _, engine, id := taskFixture(t)
	forking, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	engine.request = func(_ context.Context, method, path string, _ any) (M, error) {
		messages := []any{M{"message": M{"role": "user", "content": "fork here"}}}
		if method == "POST" && strings.HasSuffix(path, "/fork") {
			close(forking)
			<-release
			return M{"id": UUID(), "title": "fork", "messages": messages}, nil
		}
		return M{"messages": messages}, nil
	}
	done := make(chan error, 1)
	go func() {
		_, err := m.Fork(context.Background(), id, 0)
		done <- err
	}()
	<-forking
	m.SetConfigurationChanging(true)
	m.SetConfigurationChanging(false)
	unblock()
	if err := <-done; Code(err) != "configuration_changing" {
		t.Fatalf("stale fork became a current task: %v", err)
	}
	if len(m.List()) != 1 {
		t.Fatal("published fork from the previous configuration")
	}
}

func TestQueueDoesNotRetryWhenFailureCannotBePersisted(t *testing.T) {
	m, store, _, id := taskFixture(t)
	attempts := make(chan struct{}, 8)
	m.options.PreparePrompt = func(M, string, []string) (M, []any, error) {
		select {
		case attempts <- struct{}{}:
		default:
		}
		if _, err := store.DB.Exec("CREATE TRIGGER IF NOT EXISTS reject_terminal_state BEFORE INSERT ON events BEGIN SELECT RAISE(ABORT,'storage failure'); END"); err != nil {
			return nil, nil, err
		}
		return nil, nil, fmt.Errorf("attachment cannot be read")
	}
	submitTask(t, m, id, "execute once")
	<-attempts
	select {
	case <-attempts:
		t.Fatal("failed terminal write silently retried the queued operation")
	case <-time.After(100 * time.Millisecond):
	}
}
