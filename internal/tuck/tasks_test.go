package tuck

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

type fakeNative struct {
	request func(context.Context, string, string, any) (M, error)
	run     func(context.Context, string, string, string, []M, string, map[string]string, func(M) error) error
}

func (f *fakeNative) Request(c context.Context, method, path string, b any) (M, error) {
	if f.request != nil {
		return f.request(c, method, path, b)
	}
	if method == "POST" && path == "/api/sessions" {
		return M{"id": UUID()}, nil
	}
	return M{"streaming": false}, nil
}
func (f *fakeNative) Run(c context.Context, s, r, a string, msg []M, model string, h map[string]string, fn func(M) error) error {
	if f.run != nil {
		return f.run(c, s, r, a, msg, model, h, fn)
	}
	return fn(M{"type": "stream_stopped", "session_id": s, "reason": "normal"})
}
func taskFixture(t *testing.T) (*Manager, *Store, *fakeNative, string) {
	t.Helper()
	s, err := NewStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	engine := &fakeNative{}
	m, err := NewManager(ManagerOptions{Store: s, Engine: engine, WorkspaceRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		_ = m.Close(ctx)
		_ = s.Close()
	})
	task, err := m.Create(context.Background(), M{})
	if err != nil {
		t.Fatal(err)
	}
	return m, s, engine, S(task["id"])
}
func mustTask(t *testing.T, m *Manager, id string) M {
	t.Helper()
	v, e := m.Get(id)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func mustEvent(t *testing.T, m *Manager, id string, event M) {
	t.Helper()
	if e := m.HandleEvent(id, event); e != nil {
		t.Fatal(e)
	}
}
func mustUpdate(t *testing.T, m *Manager, id string, patch M) M {
	t.Helper()
	v, e := m.Update(id, func(task M) error {
		for k, x := range patch {
			task[k] = x
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func waitTask(t *testing.T, m *Manager, id string, predicate func(M) bool) M {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		task := mustTask(t, m, id)
		if predicate(task) {
			return task
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("task wait expired: %s", JSON(mustTask(t, m, id)))
	return nil
}
func waitIdle(t *testing.T, m *Manager, id string) M {
	return waitTask(t, m, id, func(task M) bool { return !m.Running(id) && S(task["lastTurnReason"]) != "" })
}
func submitTask(t *testing.T, m *Manager, id, text string) {
	t.Helper()
	if _, e := m.Submit(id, text, nil, nil, nil); e != nil {
		t.Fatal(e)
	}
}
func assertStatus(t *testing.T, err error, want int) {
	t.Helper()
	if err == nil || Status(err) != want {
		t.Fatalf("status got %v want %d", err, want)
	}
}

func TestNativeRecoveryAndMissingSession(t *testing.T) {
	for _, confirmed := range []bool{false, true} {
		t.Run(fmt.Sprint(confirmed), func(t *testing.T) {
			m, _, engine, id := taskFixture(t)
			engine.run = func(context.Context, string, string, string, []M, string, map[string]string, func(M) error) error {
				return nativeError(500, []byte(`{"message":"permission update failed"}`))
			}
			engine.request = func(_ context.Context, method, path string, _ any) (M, error) {
				if method == "GET" {
					if confirmed {
						return nil, nativeError(404, []byte(`{"message":"session not found: session not found"}`))
					}
					return nil, nativeError(404, []byte(`{"message":"database unavailable"}`))
				}
				return M{}, nil
			}
			submitTask(t, m, id, "continue")
			task := waitIdle(t, m, id)
			got := S(O(task["lastFailure"])["code"])
			if (got == "native_session_missing") != confirmed {
				t.Fatalf("incorrect missing classification: %s", JSON(task))
			}
			if I(task["logicalCount"]) < 1 {
				t.Fatal("accepted history lost")
			}
		})
	}
	m, store, engine, root := taskFixture(t)
	m.mu.Lock()
	child, err := m.createChildLocked(fixtureTaskLocked(m, root), UUID(), "worker", "background")
	if err != nil {
		m.mu.Unlock()
		t.Fatal(err)
	}
	id := S(child["id"])
	_, err = m.appendLocked(child, M{"role": "tool", "blocks": []any{newBlock("result:x", "tool-result", M{"callId": "x", "name": "tool", "output": "ok"})}})
	m.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	engine.request = func(_ context.Context, method, path string, _ any) (M, error) {
		if method != "GET" || !strings.HasSuffix(path, "/recovery") {
			t.Errorf("unexpected native request: %s %s", method, path)
		}
		return M{"messages": []any{M{"agent_name": "worker", "message": M{"role": "assistant", "message_id": "one", "usage": M{"input_tokens": 2, "output_tokens": 3}, "tool_calls": []any{M{"id": "x", "function": M{"name": "deliver_result", "arguments": `{"summary":"delivered"}`}}}}}, M{"message": M{"role": "tool", "tool_call_id": "x", "content": "ok"}}}}, nil
	}
	if err = m.RestoreChildStatistics(context.Background()); err != nil {
		t.Fatal(err)
	}
	task := mustTask(t, m, id)
	if I(task["modelCalls"]) != 1 || N(O(task["usage"])["inputTokens"]) != 2 {
		t.Fatalf("recovery statistics: %s", JSON(task))
	}
	message, err := store.Message(id, 0)
	if err != nil {
		t.Fatal(err)
	}
	data := O(O(A(message["blocks"])[0])["data"])
	if S(data["name"]) != "deliver_result" || S(data["deliverySummary"]) != "delivered" || S(data["status"]) != "success" {
		t.Fatalf("metadata not recovered: %s", JSON(message))
	}
	if err = m.RestoreChildStatistics(context.Background()); err != nil {
		t.Fatal(err)
	}
	if I(mustTask(t, m, id)["modelCalls"]) != 1 {
		t.Fatal("recovery accumulated twice")
	}
}

func TestTaskIdentifiersRestart(t *testing.T) {
	m, s, _, id := taskFixture(t)
	task := mustTask(t, m, id)
	if len(id) != 16 || len(S(task["nativeSessionId"])) != 36 {
		t.Fatal(task)
	}
	m.mu.Lock()
	child, e := m.createChildLocked(fixtureTaskLocked(m, id), "unchanged-native-child", "worker", "foreground")
	m.mu.Unlock()
	if e != nil || len(S(child["id"])) != 16 || child["nativeSessionId"] != "unchanged-native-child" {
		t.Fatal(child, e)
	}
	mustUpdate(t, m, S(child["id"]), M{"createdAt": "2025-01-01T00:00:00Z"})
	if m.List()[0]["id"] != id {
		t.Fatal("task listing lost insertion order")
	}
	mustUpdate(t, m, id, M{"status": "working", "modelCalls": 7, "usage": M{"inputTokens": 42, "outputTokens": 19}, "queue": []any{M{"id": "user", "prompt": "retained"}, M{"id": "auto", "prompt": "automatic", "source": M{"kind": "delivery_required"}}}})
	restored, e := NewManager(m.options)
	if e != nil {
		t.Fatal(e)
	}
	if restored.List()[0]["id"] != id {
		t.Fatal("restored listing lost insertion order")
	}
	got := mustTask(t, restored, id)
	if I(got["modelCalls"]) != 7 || N(O(got["usage"])["inputTokens"]) != 42 || S(got["status"]) != "idle" || S(got["lastTurnReason"]) != "interrupted" || S(O(got["lastFailure"])["code"]) != "service_restarted" || !B(got["queuePaused"]) || len(A(got["queue"])) != 1 {
		t.Fatal(got)
	}
	stored, e := s.Task(id)
	if e != nil || I(stored["queuedPrompts"]) != 1 {
		t.Fatal(stored, e)
	}
	if _, e = restored.Submit(id, "new queued", nil, nil, nil); e != nil {
		t.Fatal(e)
	}
	got = mustTask(t, restored, id)
	if len(S(O(A(got["queue"])[1])["id"])) != 16 {
		t.Fatal(got)
	}
}
func TestTaskTitleAdmissionAndSupplements(t *testing.T) {
	for _, title := range []string{"[任务] 海报", "T-123 · 海报", "T-12345 · 海报", "T-a7Q_ · ", "T-a7Q_ · 海报\n", "T-a7Q_ · \t海报"} {
		if ParseTaskTitle(title) != nil {
			t.Fatal(title)
		}
	}
	if S(ParseTaskTitle("T-a7Q_ · 海报")["code"]) != "a7Q_" || IsChannelTask(M{"title": "T-a7Q_ · 海报", "parentTaskId": "parent"}) {
		t.Fatal("title identity")
	}
	m, _, f, id := taskFixture(t)
	f.run = func(ctx context.Context, _, _, _ string, _ []M, _ string, _ map[string]string, _ func(M) error) error {
		<-ctx.Done()
		return ctx.Err()
	}
	mustUpdate(t, m, id, M{"title": "T-aaaa · A"})
	second, e := m.Create(context.Background(), M{"title": "T-bbbb · B"})
	if e != nil {
		t.Fatal(e)
	}
	third, e := m.Create(context.Background(), M{"title": "T-cccc · C"})
	if e != nil {
		t.Fatal(e)
	}
	submitTask(t, m, id, "A")
	submitTask(t, m, S(second["id"]), "B")
	_, e = m.Submit(S(third["id"]), "C", nil, nil, nil)
	assertStatus(t, e, 409)
	if len(A(mustTask(t, m, S(third["id"]))["queue"])) != 0 {
		t.Fatal("rejected request queued")
	}
	ordinary, e := m.Create(context.Background(), M{"title": "chat"})
	if e != nil {
		t.Fatal(e)
	}
	submitTask(t, m, S(ordinary["id"]), "unlimited chat")
	_, e = m.Submit(S(third["id"]), "supplement", nil, 0, nil)
	assertStatus(t, e, 409)
	_, e = m.Submit(S(third["id"]), "supplement", nil, -1, nil)
	assertStatus(t, e, 400)
	mustUpdate(t, m, S(third["id"]), M{"lastTurnReason": "completed", "turnCount": 1, "queue": []any{M{"id": "old", "prompt": "old"}}, "queuePaused": true})
	_, e = m.Submit(S(third["id"]), "supplement", nil, 1, nil)
	assertStatus(t, e, 409)
}
func TestTaskQueueFailureAndResume(t *testing.T) {
	m, s, f, id := taskFixture(t)
	var mu sync.Mutex
	sent := []string{}
	f.run = func(ctx context.Context, session, ref, agent string, msg []M, model string, h map[string]string, fn func(M) error) error {
		mu.Lock()
		sent = append(sent, S(msg[0]["content"]))
		n := len(sent)
		mu.Unlock()
		if n == 1 {
			if e := fn(M{"type": "error", "session_id": session, "message": "resource unavailable"}); e != nil {
				return e
			}
			return fn(M{"type": "stream_stopped", "session_id": session, "reason": "error"})
		}
		return fn(M{"type": "stream_stopped", "session_id": session, "reason": "normal"})
	}
	mustUpdate(t, m, id, M{"queuePaused": true})
	submitTask(t, m, id, "first")
	submitTask(t, m, id, "retained")
	if _, e := m.UpdateQueue(context.Background(), id, M{"action": "resume"}); e != nil {
		t.Fatal(e)
	}
	got := waitIdle(t, m, id)
	if !B(got["queuePaused"]) || S(got["lastTurnReason"]) != "error" {
		t.Fatal(got)
	}
	stored, _ := s.Task(id)
	if !B(stored["queuePaused"]) {
		t.Fatal("pause was not committed")
	}
	submitTask(t, m, id, "new draft")
	mu.Lock()
	count := len(sent)
	mu.Unlock()
	if count != 1 {
		t.Fatal("unasked replay")
	}
	if _, e := m.UpdateQueue(context.Background(), id, M{"action": "resume"}); e != nil {
		t.Fatal(e)
	}
	got = waitIdle(t, m, id)
	mu.Lock()
	defer mu.Unlock()
	if !reflect.DeepEqual(sent, []string{"first", "retained", "new draft"}) || I(got["turnCount"]) != 3 {
		t.Fatal(sent, got)
	}
}
func TestStoppingQueueEdits(t *testing.T) {
	m, s, _, id := taskFixture(t)
	mustUpdate(t, m, id, M{"queue": []any{M{"id": "one", "prompt": "later"}, M{"id": "two", "prompt": "another"}}, "queuedPrompts": 2, "queuePaused": true})
	m.mu.Lock()
	m.runs[id] = &taskRun{stopping: true}
	m.mu.Unlock()
	defer func() { m.mu.Lock(); delete(m.runs, id); m.mu.Unlock() }()
	for _, in := range []M{{"action": "send", "entryId": "one"}, {"action": "resume"}} {
		_, e := m.UpdateQueue(context.Background(), id, in)
		assertStatus(t, e, 409)
	}
	_, e := m.Submit(id, "new", nil, nil, nil)
	assertStatus(t, e, 409)
	if _, e = m.UpdateQueue(context.Background(), id, M{"action": "edit", "entryId": "one", "prompt": "edited"}); e != nil {
		t.Fatal(e)
	}
	task, _ := s.Task(id)
	if S(O(A(task["queue"])[0])["prompt"]) != "edited" {
		t.Fatal(task)
	}
	if _, e = m.UpdateQueue(context.Background(), id, M{"action": "remove", "entryId": "one"}); e != nil {
		t.Fatal(e)
	}
	if _, e = m.UpdateQueue(context.Background(), id, M{"action": "clear"}); e != nil {
		t.Fatal(e)
	}
	if I(mustTask(t, m, id)["queuedPrompts"]) != 0 {
		t.Fatal("not cleared")
	}
}
func TestQueueSendFailureRetainsAndSerializes(t *testing.T) {
	m, _, f, id := taskFixture(t)
	mustUpdate(t, m, id, M{"status": "working", "turnId": "turn", "queue": []any{M{"id": "queued", "prompt": "retain me"}}, "queuedPrompts": 1})
	entered, release := make(chan struct{}), make(chan struct{})
	f.request = func(ctx context.Context, method, path string, b any) (M, error) {
		if strings.HasSuffix(path, "/steer") {
			close(entered)
			<-release
			return nil, errors.New("native delivery failed")
		}
		return M{}, nil
	}
	done := make(chan error, 1)
	go func() {
		_, e := m.UpdateQueue(context.Background(), id, M{"action": "send", "entryId": "queued"})
		done <- e
	}()
	<-entered
	_, e := m.UpdateQueue(context.Background(), id, M{"action": "remove", "entryId": "queued"})
	assertStatus(t, e, 409)
	close(release)
	if e = <-done; e == nil {
		t.Fatal("send accepted")
	}
	task := mustTask(t, m, id)
	if len(A(task["queue"])) != 1 || !B(task["queuePaused"]) {
		t.Fatal(task)
	}
}
func TestSteeringReceipts(t *testing.T) {
	m, s, f, id := taskFixture(t)
	task := mustUpdate(t, m, id, M{"status": "working", "turnId": "turn"})
	send := func(text string) {
		t.Helper()
		if _, e := m.Steer(context.Background(), id, text, nil, nil); e != nil {
			t.Fatal(e)
		}
	}
	receipt := func(text, receipt, session string) {
		mustEvent(t, m, id, M{"type": "user_message", "session_id": session, "message_id": receipt, "message": text})
	}
	state := func(index int) string {
		msg, e := s.Message(id, index)
		if e != nil {
			t.Fatal(e)
		}
		return S(O(msg["steering"])["status"])
	}
	send("same")
	send("same")
	receipt("same", "child", "native-child")
	if state(0) != "pending" {
		t.Fatal("child attributed")
	}
	receipt("same\n", "one", S(task["nativeSessionId"]))
	receipt("same", "one", S(task["nativeSessionId"]))
	if state(0) != "delivered" || state(1) != "pending" {
		t.Fatal("duplicate receipt reused")
	}
	receipt("same", "two", S(task["nativeSessionId"]))
	if state(1) != "delivered" {
		t.Fatal("distinct receipt lost")
	}
	f.request = func(ctx context.Context, method, path string, b any) (M, error) {
		receipt("early", "early", S(task["nativeSessionId"]))
		return nil, errors.New("HTTP response lost")
	}
	send("early")
	if state(2) != "delivered" {
		t.Fatal("early receipt lost")
	}
	f.request = func(context.Context, string, string, any) (M, error) { return nil, errors.New("rejected") }
	if _, e := m.Steer(context.Background(), id, "rejected", nil, nil); e == nil {
		t.Fatal("rejection swallowed")
	}
	msgs, _ := s.Messages(id, 0, 100)
	if len(msgs) != 3 {
		t.Fatal(msgs)
	}
}
func TestSessionMessageUnicodeLength(t *testing.T) {
	budget := 100000 - utf8.RuneCountInString(modelContent.Collaboration(""))
	for _, tc := range []struct {
		name, message string
		accepted      bool
	}{
		{"Chinese", strings.Repeat("中", 40000), true},
		{"emoji_at_limit", strings.Repeat("😀", budget), true},
		{"wrapped_over_limit", strings.Repeat("中", budget+1), false},
		{"input_over_limit", strings.Repeat("中", 100001), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, _, _, id := taskFixture(t)
			mustUpdate(t, m, id, M{"queuePaused": true})
			value, err := m.SendConversationMessage(context.Background(), M{"target_conversation_id": id, "message": tc.message})
			queue := A(mustTask(t, m, id)["queue"])
			if tc.accepted {
				if err != nil || value["status"] != "accepted" || len(queue) != 1 {
					t.Fatalf("Unicode message was not queued: result=%v err=%v queued=%d", value, err, len(queue))
				}
				if O(queue[0])["prompt"] != modelContent.Collaboration(tc.message) || S(O(O(queue[0])["source"])["kind"]) != "collaboration" {
					t.Fatal("queued message or source changed")
				}
			} else if Code(err) != "invalid_message" || len(queue) != 0 {
				t.Fatalf("oversized message was accepted: err=%v queued=%d", err, len(queue))
			}
		})
	}
}

func TestCollaborationSubmissionUnconfirmed(t *testing.T) {
	for _, failure := range []string{"timeout", "recording"} {
		t.Run(failure, func(t *testing.T) {
			m, store, engine, id := taskFixture(t)
			mustUpdate(t, m, id, M{"status": "working", "turnId": "turn"})
			calls := 0
			engine.request = func(_ context.Context, method, path string, body any) (M, error) {
				calls++
				if method != "POST" || !strings.HasSuffix(path, "/steer") || !strings.Contains(string(JSON(body)), "TUCK_COLLABORATION_MESSAGE") {
					t.Fatal("unexpected submission", method, path, body)
				}
				if failure == "timeout" {
					return nil, context.DeadlineExceeded
				}
				if err := store.Close(); err != nil {
					t.Fatal(err)
				}
				return M{}, nil
			}
			_, err := m.SendConversationMessage(context.Background(), M{"target_conversation_id": id, "message": "Check the result"})
			if err == nil || Code(err) != "conversation_message_unconfirmed" || calls != 1 {
				t.Fatal("submission outcome or retry", err, calls)
			}
			value := modelContent.Error(err)
			if value["status"] != "unknown" || !strings.Contains(S(value["message"]), "Check the target conversation") || strings.Contains(S(value["message"]), "model service") || !modelContent.MCPResult(value).IsError {
				t.Fatal("uncertain submission misrepresented", value)
			}
		})
	}
	t.Run("confirmed_before_response_loss", func(t *testing.T) {
		m, _, engine, id := taskFixture(t)
		task := mustUpdate(t, m, id, M{"status": "working", "turnId": "turn"})
		calls := 0
		engine.request = func(context.Context, string, string, any) (M, error) {
			calls++
			mustEvent(t, m, id, M{"type": "user_message", "session_id": task["nativeSessionId"], "message_id": "receipt", "message": modelContent.Collaboration("Confirmed message")})
			return nil, context.DeadlineExceeded
		}
		value, err := m.SendConversationMessage(context.Background(), M{"target_conversation_id": id, "message": "Confirmed message"})
		if err != nil || value["status"] != "accepted" || calls != 1 {
			t.Fatal("confirmed submission lost", value, err, calls)
		}
	})
	m, _, engine, id := taskFixture(t)
	mustUpdate(t, m, id, M{"status": "working", "pendingInteraction": M{"kind": "approval"}})
	engine.request = func(context.Context, string, string, any) (M, error) {
		t.Fatal("precondition failure submitted a message")
		return nil, nil
	}
	_, err := m.SendConversationMessage(context.Background(), M{"target_conversation_id": id, "message": "Wait for approval"})
	if Code(err) != "interaction_pending" || modelContent.Error(err)["status"] != "failed" {
		t.Fatal("precondition failure became uncertain", err)
	}
}
func TestSteeringRestart(t *testing.T) {
	m, s, _, id := taskFixture(t)
	task := mustUpdate(t, m, id, M{"status": "working", "turnId": "turn"})
	for _, text := range []string{"same", "same", "unconfirmed"} {
		if _, e := m.Steer(context.Background(), id, text, nil, nil); e != nil {
			t.Fatal(e)
		}
	}
	mustEvent(t, m, id, M{"type": "user_message", "session_id": task["nativeSessionId"], "message": "same", "message_id": "one"})
	if _, e := NewManager(m.options); e != nil {
		t.Fatal(e)
	}
	msgs, _ := s.Messages(id, 0, 100)
	for i, want := range []string{"delivered", "unknown", "unknown"} {
		if S(O(msgs[i]["steering"])["status"]) != want {
			t.Fatal(msgs)
		}
	}
	revision := I(msgs[2]["revision"])
	if _, e := NewManager(m.options); e != nil {
		t.Fatal(e)
	}
	again, _ := s.Message(id, 2)
	if I(again["revision"]) != revision {
		t.Fatal("restart keeps rewriting settled receipts")
	}
}
func TestSteeringRecoveryPersistedReceipt(t *testing.T) {
	m, s, _, id := taskFixture(t)
	task := mustUpdate(t, m, id, M{"status": "working", "turnId": "turn"})
	if _, err := m.Steer(context.Background(), id, "confirmed", nil, nil); err != nil {
		t.Fatal(err)
	}
	mustEvent(t, m, id, M{"type": "user_message", "session_id": task["nativeSessionId"], "message": "confirmed", "message_id": "one"})
	if _, err := m.Steer(context.Background(), id, "unconfirmed", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.pruneEvents(context.Background(), NowMS(), 0); err != nil {
		t.Fatal(err)
	}
	if _, err := NewManager(m.options); err != nil {
		t.Fatal(err)
	}
	msgs, _ := s.Messages(id, 0, 100)
	if len(msgs) != 2 || S(O(msgs[0]["steering"])["status"]) != "delivered" || S(O(msgs[0]["steering"])["messageId"]) != "one" || S(O(msgs[1]["steering"])["status"]) != "unknown" {
		t.Fatal(msgs)
	}
}
func TestQuestionTypesAndInteractionQueue(t *testing.T) {
	schema := M{"type": "object", "properties": M{"style": M{"type": "string", "enum": []any{"简洁", "详细"}}, "confirm": M{"type": "boolean"}, "count": M{"type": "integer", "minimum": 1, "maximum": 5}, "email": M{"type": "string", "format": "email"}}, "required": []any{"style", "confirm", "count"}}
	answer, e := QuestionContent(schema, `{"style":"简洁","confirm":false,"count":2}`)
	if e != nil || answer["confirm"] != false || N(answer["count"]) != 2 {
		t.Fatal(answer, e)
	}
	for _, text := range []string{`{}`, `{"style":"x","confirm":false,"count":2}`, `{"style":"简洁","confirm":false,"count":0}`, `{"style":"简洁","confirm":"false","count":2}`, `{"style":"简洁","confirm":false,"count":2,"email":"invalid"}`} {
		_, e = QuestionContent(schema, text)
		assertStatus(t, e, 422)
	}
	if a, e := QuestionContent(M{"type": "string", "title": "title"}, "raw"); e != nil || a["title"] != "raw" {
		t.Fatal(a, e)
	}
	m, _, f, id := taskFixture(t)
	task := mustUpdate(t, m, id, M{"status": "working"})
	calls := []M{}
	f.request = func(c context.Context, method, path string, b any) (M, error) {
		calls = append(calls, M{"path": path, "body": b})
		return M{}, nil
	}
	for _, qid := range []string{"q1", "q2"} {
		mustEvent(t, m, id, M{"type": "elicitation_request", "session_id": "child-session", "elicitation_id": qid, "schema": schema})
	}
	pending := O(mustTask(t, m, id)["pendingInteraction"])
	if pending["id"] != "q1" {
		t.Fatal(pending)
	}
	assertStatus(t, m.ResolveInteraction(context.Background(), id, M{"kind": "question", "interactionId": "stale", "answer": nil}), 409)
	assertStatus(t, m.ResolveInteraction(context.Background(), id, M{"kind": "question", "interactionId": "q1", "answer": "{}"}), 422)
	if e = m.ResolveInteraction(context.Background(), id, M{"kind": "question", "interactionId": "q1", "answer": `{"style":"简洁","confirm":false,"count":2}`}); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(S(calls[0]["path"]), S(task["nativeSessionId"])) || O(calls[0]["body"])["elicitation_id"] != "q1" {
		t.Fatal(calls)
	}
	if O(mustTask(t, m, id)["pendingInteraction"])["id"] != "q2" {
		t.Fatal("lost parallel question")
	}
	if e = m.ResolveInteraction(context.Background(), id, M{"kind": "question", "interactionId": "q2", "answer": nil}); e != nil {
		t.Fatal(e)
	}
	if S(mustTask(t, m, id)["status"]) != "working" || O(calls[1]["body"])["action"] != "decline" {
		t.Fatal(calls)
	}
}
func TestMessageDeltasAndProjection(t *testing.T) {
	m, s, _, id := taskFixture(t)
	mustUpdate(t, m, id, M{"turnId": "turn", "status": "working"})
	for _, text := range []string{"hello", "世界"} {
		mustEvent(t, m, id, M{"type": "agent_choice", "message_id": "answer", "content": text})
	}
	messages, e := s.Messages(id, 0, 100)
	if e != nil || len(messages) != 1 {
		t.Fatal(messages, e)
	}
	message := messages[0]
	if S(O(findBlock(message, "markdown")["data"])["markdown"]) != "hello世界" || I(message["revision"]) != 2 {
		t.Fatal(message)
	}
	events, e := s.Events(0, 512)
	if e != nil {
		t.Fatal(e)
	}
	deltas := []M{}
	for _, event := range events {
		if S(event["type"]) == "message-delta" {
			deltas = append(deltas, O(event["delta"]))
		}
	}
	if len(deltas) != 2 || I(deltas[1]["baseRevision"]) != 1 || I(deltas[1]["revision"]) != 2 || deltas[1]["text"] != "世界" {
		t.Fatal(deltas)
	}
	src := "data:image/png;base64,dGVzdCBpbWFnZSBieXRlcw=="
	canonical := M{"index": 3, "revision": 4, "blocks": []any{newBlock("block", "attachments", M{"items": []any{M{"id": "item", "name": "image.png", "src": src}}})}}
	view := WorkbenchMessage(id, canonical)
	if strings.Contains(string(JSON(view)), "base64") || !strings.Contains(string(JSON(canonical)), "base64") {
		t.Fatal(view)
	}
	newer := Clone(canonical)
	newer["revision"] = 5
	if reflect.DeepEqual(WorkbenchMessage(id, newer), view) {
		t.Fatal("revision missing from media URL")
	}
	native := M{"seq": 42, "taskId": id, "type": "native", "event": M{"hidden": src}}
	if len(WorkbenchEvent(native)) != 3 || len(native) != 4 {
		t.Fatal(native)
	}
	media, e := InlineMessageMedia(canonical, "block", "item")
	if e != nil || string(media["bytes"].([]byte)) != "test image bytes" {
		t.Fatal(media, e)
	}
}
func TestCompactionOwnershipAndActivity(t *testing.T) {
	m, s, _, id := taskFixture(t)
	root := mustUpdate(t, m, id, M{"status": "working", "context": M{"reported": true, "projectedTokens": 214668}})
	m.mu.Lock()
	child, e := m.createChildLocked(fixtureTaskLocked(m, id), "child", "worker", "foreground")
	m.mu.Unlock()
	if e != nil {
		t.Fatal(e)
	}
	mustUpdate(t, m, S(child["id"]), M{"status": "working", "context": M{"reported": true, "projectedTokens": 214668}})
	mustEvent(t, m, id, M{"type": "session_compaction", "session_id": "child", "status": "started"})
	if mustTask(t, m, id)["compaction"] != nil || S(O(mustTask(t, m, S(child["id"]))["compaction"])["status"]) != "running" {
		t.Fatal("child owner")
	}
	mustEvent(t, m, id, M{"type": "session_compaction", "session_id": "unknown", "status": "started"})
	if mustTask(t, m, id)["compaction"] != nil {
		t.Fatal("unknown owner")
	}
	for _, outcome := range []string{"skipped", "failed", "applied", ""} {
		mustEvent(t, m, id, M{"type": "session_compaction", "session_id": root["nativeSessionId"], "status": "started"})
		mustEvent(t, m, id, M{"type": "session_compaction", "session_id": root["nativeSessionId"], "status": "completed", "outcome": outcome})
		want := outcome
		if want == "" {
			want = "unknown"
		}
		if S(O(mustTask(t, m, id)["compaction"])["status"]) != want {
			t.Fatal(outcome)
		}
	}
	mustEvent(t, m, id, M{"type": "session_compaction", "session_id": root["nativeSessionId"], "status": "started"})
	mustEvent(t, m, id, M{"type": "session_summary", "session_id": root["nativeSessionId"]})
	mustEvent(t, m, id, M{"type": "session_compaction", "session_id": root["nativeSessionId"], "status": "completed"})
	if S(O(mustTask(t, m, id)["compaction"])["status"]) != "applied" {
		t.Fatal("summary outcome")
	}
	mustEvent(t, m, id, M{"type": "agent_choice", "content": "first"})
	before, _ := s.TaskEvents(id, []string{"state"})
	mustEvent(t, m, id, M{"type": "agent_choice", "content": "second"})
	after, _ := s.TaskEvents(id, []string{"state"})
	if len(before) != len(after) {
		t.Fatal("per-token state churn")
	}
	for _, call := range []string{"one", "two"} {
		mustEvent(t, m, id, M{"type": "tool_call", "tool_call": M{"id": call, "function": M{"name": "shell", "arguments": "{}"}}})
	}
	if S(O(mustTask(t, m, id)["activity"])["detail"]) != "2 个工具" {
		t.Fatal("parallel tool activity")
	}
	mustEvent(t, m, id, M{"type": "tool_call_response", "tool_call_id": "one", "response": "ok"})
	if S(O(mustTask(t, m, id)["activity"])["phase"]) != "tool" {
		t.Fatal("remaining tool disappeared")
	}
	mustEvent(t, m, id, M{"type": "session_compaction", "status": "started"})
	if _, e := NewManager(m.options); e != nil {
		t.Fatal(e)
	}
	stored, _ := s.Task(id)
	if S(O(stored["compaction"])["status"]) != "interrupted" {
		t.Fatal(stored)
	}
}
func TestTurnTimingsAndRecovery(t *testing.T) {
	task := M{"turnId": "one"}
	StartTurnTiming(task, 100)
	ObserveTurnTiming(task, 200)
	task["pendingInteraction"] = M{"id": "q"}
	ObserveTurnTiming(task, 300)
	ObserveTurnTiming(task, 400)
	task["pendingInteraction"] = nil
	ObserveTurnTiming(task, 500)
	task["pendingInteraction"] = M{"id": "p"}
	ObserveTurnTiming(task, 600)
	FinishTurnTiming(task, true, 800)
	FinishTurnTiming(task, true, 900)
	if N(O(task["turnTiming"])["waitingMs"]) != 400 || N(O(task["turnTiming"])["endedAt"]) != 800 {
		t.Fatal(task)
	}
	task["turnId"] = "two"
	StartTurnTiming(task, 1000)
	ObserveTurnTiming(task, 1100)
	FinishTurnTiming(task, false, 99999)
	if !B(O(task["turnTiming"])["incomplete"]) || N(O(task["turnTiming"])["observedAt"]) != 1100 || O(task["turnTiming"])["endedAt"] != nil {
		t.Fatal(task)
	}
	legacy := M{}
	FinishTurnTiming(legacy, false, 123)
	if legacy["turnTiming"] != nil {
		t.Fatal(legacy)
	}
	timings := runtimeTimings{}
	root, child := M{"id": "root"}, M{"id": "child"}
	send := func(task M, ms int64, kind string, data M) {
		patch := timings.observe(task, Merge(data, M{"type": kind, "timestamp": time.UnixMilli(1700000000000 + ms).UTC().Format(time.RFC3339Nano)}))
		if patch != nil {
			task["timings"] = patch
		}
	}
	send(root, 0, "tool_call_confirmation", M{"tool_call": M{"id": "one"}})
	send(root, 10000, "tool_call", M{"tool_call": M{"id": "one"}})
	send(root, 10010, "tool_call", M{"tool_call": M{"id": "one"}})
	send(root, 10100, "tool_call_response", M{"tool_call_id": "one"})
	send(root, 11000, "tool_call_response", M{"tool_call_id": "one"})
	if N(O(root["timings"])["toolMs"]) != 100 || I(O(root["timings"])["toolSamples"]) != 1 {
		t.Fatal(root)
	}
	send(root, 12000, "agent_choice_reasoning", M{"content": "think"})
	send(child, 12500, "agent_choice", M{"content": "child"})
	send(root, 13000, "agent_choice", M{"content": "answer"})
	send(root, 13500, "token_usage", M{"usage": M{"snapshot_only": true, "last_message": M{"output_tokens": 999}}})
	send(root, 14000, "token_usage", M{"usage": M{"last_message": M{"output_tokens": 200}}})
	send(root, 14001, "token_usage", M{"usage": M{"last_message": M{"output_tokens": 200}}})
	if N(O(root["timings"])["streamMs"]) != 2000 || I(O(root["timings"])["streamSamples"]) != 1 {
		t.Fatal(root)
	}
	send(child, 15000, "stream_stopped", nil)
	send(child, 16000, "token_usage", M{"usage": M{"last_message": M{"output_tokens": 100}}})
	if child["timings"] != nil {
		t.Fatal(child)
	}
	send(root, 17000, "agent_choice", M{"content": "buffered"})
	send(root, 18000, "token_usage", M{"usage": M{"last_message": M{"output_tokens": 1000}}})
	if I(O(root["timings"])["streamSamples"]) != 1 {
		t.Fatal("buffered throughput fabricated")
	}
	first := M{"agent_name": "worker", "message": M{"role": "assistant", "message_id": "one", "usage": M{"input_tokens": 10, "output_tokens": 20, "cached_input_tokens": 30, "cached_write_tokens": 4, "reasoning_tokens": 5}}}
	recovery := M{"input_tokens": 100, "output_tokens": 50, "messages": []any{first, first, M{"agent_name": "worker", "message": M{"role": "assistant", "message_id": "two", "usage": M{"input_tokens": 2, "output_tokens": 3}}}, M{"agent_name": "root", "message": M{"role": "assistant", "usage": M{"input_tokens": 999}}}}}
	got := RecoveredStatistics(recovery, "worker")
	if N(O(got["usage"])["inputTokens"]) != 12 || N(O(got["usage"])["outputTokens"]) != 23 || I(got["modelCalls"]) != 2 || N(O(got["context"])["projectedTokens"]) != 150 {
		t.Fatal(got)
	}
	if !reflect.DeepEqual(got, RecoveredStatistics(recovery, "worker")) {
		t.Fatal("recovery non-idempotent")
	}
	if RecoveredStatistics(M{}, "worker")["modelCalls"] != nil {
		t.Fatal("unknown fabricated")
	}
}
func TestEngineSSEFramingCancellationNoRetry(t *testing.T) {
	var events []M
	reader := &oneByteReader{text: "id: 42\r\ndata: {\"type\":\"text\",\r\ndata: \"value\":\"中文\"}\r\n\r\n: keepalive\n\ndata:\n\ndata: [DONE]\n\n"}
	if err := ReadNativeSSE(reader, func(e M) error { events = append(events, e); return nil }); err != nil || len(events) != 1 || events[0]["value"] != "中文" {
		t.Fatal(events, err)
	}
	if ReadNativeSSE(strings.NewReader("data: {}"), func(M) error { return nil }) == nil {
		t.Fatal("partial frame accepted")
	}
	large := "data: {\"large\":\"" + strings.Repeat("x", 2*1024*1024) + "\"}\n\n"
	if e := ReadNativeSSE(strings.NewReader(large), func(e M) error {
		if len(S(e["large"])) != 2*1024*1024 {
			t.Fatal("large truncated")
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	requests := 0
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests++
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"type\":\"max_iterations_reached\"}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	engine := NewEngine(EngineOptions{URL: server.URL, External: true})
	defer engine.Close()
	ctx, cancel := context.WithCancel(context.Background())
	received := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- engine.Run(ctx, "session", "fixture", "root", nil, "", nil, func(M) error { close(received); return nil })
	}()
	<-received
	cancel()
	if e := <-done; e == nil {
		t.Fatal("abort ignored")
	}
	mu.Lock()
	defer mu.Unlock()
	if requests != 1 {
		t.Fatal("task-creating POST retried")
	}
	if engine.stream.Timeout != 0 {
		t.Fatal("execution fixed lifetime")
	}
}

type oneByteReader struct{ text string }

func (r *oneByteReader) Read(p []byte) (int, error) {
	if r.text == "" {
		return 0, io.EOF
	}
	p[0] = r.text[0]
	r.text = r.text[1:]
	return 1, nil
}
func TestDroppedIterationWait(t *testing.T) {
	m, _, f, id := taskFixture(t)
	f.run = func(c context.Context, s, r, a string, msg []M, model string, h map[string]string, fn func(M) error) error {
		if e := fn(M{"type": "max_iterations_reached", "max_iterations": 40}); e != nil {
			return e
		}
		return io.ErrUnexpectedEOF
	}
	submitTask(t, m, id, "synthetic")
	task := waitIdle(t, m, id)
	failure := O(task["lastFailure"])
	if S(failure["code"]) != "execution_disconnected" || !strings.Contains(S(failure["message"]), "执行步数上限") || strings.Contains(S(failure["message"]), "EOF") || task["pendingInteraction"] != nil {
		t.Fatal(task)
	}
}
func TestTurnStartsBeforePermissionAndQueues(t *testing.T) {
	m, _, f, id := taskFixture(t)
	var mu sync.Mutex
	starts := []float64{}
	m.options.ApprovalTools = func() []string { return []string{"ask_tool"} }
	f.request = func(c context.Context, method, path string, b any) (M, error) {
		if strings.HasSuffix(path, "/permissions") {
			task := mustTask(t, m, id)
			mu.Lock()
			starts = append(starts, N(O(task["turnTiming"])["startedAt"]))
			mu.Unlock()
			if starts[len(starts)-1] <= 0 {
				t.Error("missing turn timer")
			}
		}
		return M{}, nil
	}
	mustUpdate(t, m, id, M{"queuePaused": true})
	submitTask(t, m, id, "one")
	submitTask(t, m, id, "two")
	if _, e := m.UpdateQueue(context.Background(), id, M{"action": "resume"}); e != nil {
		t.Fatal(e)
	}
	task := waitIdle(t, m, id)
	mu.Lock()
	defer mu.Unlock()
	if len(starts) != 2 || I(task["turnCount"]) != 2 || N(O(task["turnTiming"])["endedAt"]) < starts[1] {
		t.Fatal(starts, task)
	}
}

func TestTaskResultsAndStatistics(t *testing.T) {
	m, s, _, id := taskFixture(t)
	index := 0
	appendMessage := func(role string, blocks []any, live bool) {
		t.Helper()
		msg := M{"id": fmt.Sprintf("%s:m-%d", id, index), "index": index, "revision": 0, "role": role, "blocks": blocks, "live": live}
		index++
		if e := s.SaveMessage(id, msg); e != nil {
			t.Fatal(e)
		}
	}
	text := func(v string) []any { return []any{newBlock("text", "markdown", M{"markdown": v})} }
	check := func(want string) {
		t.Helper()
		got, e := s.FinalReply(id)
		if e != nil || got != want {
			t.Fatal(got, e, "want", want)
		}
	}
	appendMessage("user", text("old request"), false)
	appendMessage("assistant", text("old answer"), false)
	appendMessage("user", text("new request"), false)
	check("")
	appendMessage("assistant", text("proposal"), false)
	appendMessage("assistant", []any{newBlock("call", "tool-call", M{"callId": "one"})}, false)
	appendMessage("tool", text("tool output"), false)
	check("")
	appendMessage("assistant", text("final answer"), false)
	appendMessage("assistant", []any{newBlock("files", "attachments", M{"items": []any{}})}, false)
	check("final answer")
	appendMessage("assistant", text("incomplete tail"), true)
	check("final answer")
	appendMessage("user", text("next turn"), false)
	appendMessage("assistant", text("streaming"), true)
	check("")
	task := mustTask(t, m, id)
	task["logicalCount"] = index
	task["turnCount"] = 2
	task["turnId"] = "one"
	StartTurnTiming(task, 1000)
	ObserveTurnTiming(task, 1500)
	FinishTurnTiming(task, true, 3000)
	task["turnId"] = "two"
	StartTurnTiming(task, 100000)
	StartTurnTiming(task, 100000) // Same turn must not be counted twice.

	stats, e := s.Statistics(task)
	if e != nil || N(stats["previousMs"]) != 2000 || I(stats["toolCalls"]) != 1 || I(stats["previousTurns"]) != 1 || B(stats["incomplete"]) {
		t.Fatal(stats, e)
	}
	task["turnTiming"] = M{"turnId": "two", "startedAt": 100000, "observedAt": 102000, "incomplete": true}
	if _, e = s.Publish(task, nil, M{"type": "state", "task": task}); e != nil {
		t.Fatal(e)
	}
	task["turnId"] = "three"
	StartTurnTiming(task, 200000)
	stats, e = s.Statistics(task)
	if e != nil || N(stats["previousMs"]) != 4000 || !B(stats["incomplete"]) {
		t.Fatal(stats, e)
	}
}

func TestDeliveryContinuationOwnership(t *testing.T) {
	for _, queuedUser := range []bool{false, true} {
		t.Run(fmt.Sprint(queuedUser), func(t *testing.T) {
			m, s, f, id := taskFixture(t)
			var configMu sync.Mutex
			config := m.Config()
			O(config["workbench"])["requireDelivery"] = true
			m.options.Config = func() M { configMu.Lock(); defer configMu.Unlock(); return Clone(config) }
			sent := []string{}
			f.run = func(c context.Context, session, ref, agent string, msg []M, model string, h map[string]string, fn func(M) error) error {
				sent = append(sent, S(msg[0]["content"]))
				return fn(M{"type": "stream_stopped", "session_id": session, "reason": "normal"})
			}
			once := sync.Once{}
			m.options.RefreshFiles = func(task M) ([]any, error) {
				once.Do(func() {
					if len(A(task["queue"])) == 0 || S(O(O(A(task["queue"])[0])["source"])["kind"]) != "delivery_required" {
						t.Error("continuation missing before cleanup", task)
					}
					if S(task["status"]) != "working" {
						t.Error("transient terminal status", task)
					}
					configMu.Lock()
					O(config["workbench"])["requireDelivery"] = false
					configMu.Unlock()
					_, e := m.Update(id, func(task M) error {
						queue := []any{}
						for _, v := range A(task["queue"]) {
							if S(O(O(v)["source"])["kind"]) != "delivery_required" {
								queue = append(queue, v)
							}
						}
						task["queue"] = queue
						task["queuedPrompts"] = len(queue)
						return nil
					})
					if e != nil {
						t.Error(e)
					}
				})
				return []any{}, nil
			}
			mustUpdate(t, m, id, M{"queuePaused": true})
			submitTask(t, m, id, "original")
			if queuedUser {
				submitTask(t, m, id, "queued user")
			}
			if _, e := m.UpdateQueue(context.Background(), id, M{"action": "resume"}); e != nil {
				t.Fatal(e)
			}
			task := waitIdle(t, m, id)
			expected := 1
			if queuedUser {
				expected = 2
			}
			if len(sent) != expected || I(task["turnCount"]) != expected || task["activity"] != nil || task["pendingInteraction"] != nil || I(task["queuedPrompts"]) != 0 || S(task["lastTurnReason"]) != "completed" {
				t.Fatal(sent, task)
			}
			messages, _ := s.Messages(id, 0, 100)
			for _, msg := range messages {
				if S(O(msg["source"])["kind"]) == "delivery_required" {
					t.Fatal("disabled continuation ran")
				}
			}
		})
	}
	m, s, f, id := taskFixture(t)
	var configMu sync.Mutex
	config := m.Config()
	O(config["workbench"])["requireDelivery"] = true
	m.options.Config = func() M { configMu.Lock(); defer configMu.Unlock(); return Clone(config) }
	count := 0
	f.run = func(c context.Context, session, ref, agent string, msg []M, model string, h map[string]string, fn func(M) error) error {
		count++
		if count == 2 {
			configMu.Lock()
			O(config["workbench"])["requireDelivery"] = false
			configMu.Unlock()
		}
		if S(msg[0]["content"]) != modelContent.DeliveryReminder() {
			t.Error("reminder mismatch")
		}
		return fn(M{"type": "stream_stopped", "session_id": session, "reason": "normal"})
	}
	submitTask(t, m, id, modelContent.DeliveryReminder())
	waitIdle(t, m, id)
	messages, _ := s.Messages(id, 0, 100)
	users := []M{}
	for _, msg := range messages {
		if msg["role"] == "user" {
			users = append(users, msg)
		}
	}
	if len(users) != 2 || users[0]["source"] != nil || S(O(users[1]["source"])["kind"]) != "delivery_required" || I(O(users[1]["source"])["templateVersion"]) != 2 || S(O(O(O(A(users[1]["blocks"])[0])["data"])["provenance"])["origin"]) != "delivery-required" {
		t.Fatal(users)
	}
}

func TestDeliveryRequiresExplicitRootCompletion(t *testing.T) {
	for _, tc := range []struct {
		name, session, reason, finish, failure string
		wantTurns                              int
	}{
		{"normal", "root", "normal", "stop", "", 2},
		{"enabled_during_turn", "root", "normal", "stop", "", 2},
		{"disabled_during_turn", "root", "normal", "stop", "", 1},
		{"unknown_reason", "root", "future_reason", "stop", "", 1},
		{"missing_reason", "root", "", "stop", "", 1},
		{"unregistered_child", "other", "normal", "stop", "", 1},
		{"other_root", "other-root", "normal", "stop", "", 1},
		{"missing_session", "", "normal", "stop", "", 1},
		{"canceled", "root", "canceled", "stop", "", 1},
		{"iterations", "root", "max_iterations", "stop", "", 1},
		{"truncated", "root", "normal", "length", "", 1},
		{"failed", "root", "normal", "stop", "model failed", 1},
		{"tool_only", "root", "", "", "", 1},
		{"child_only", "child", "normal", "stop", "", 1},
		{"disconnected", "root", "", "", "", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, _, f, id := taskFixture(t)
			config := m.Config()
			O(config["workbench"])["requireDelivery"] = true
			if tc.name == "enabled_during_turn" {
				O(config["workbench"])["requireDelivery"] = false
			}
			m.options.Config = func() M { return Clone(config) }
			other, err := m.Create(context.Background(), M{})
			if err != nil {
				t.Fatal(err)
			}
			other = mustUpdate(t, m, S(other["id"]), M{"nativeSessionId": "unrelated-native-session"})
			f.run = func(ctx context.Context, session, ref, agent string, messages []M, model string, headers map[string]string, emit func(M) error) error {
				if tc.name == "enabled_during_turn" {
					O(config["workbench"])["requireDelivery"] = true
				}
				if tc.name == "disabled_during_turn" {
					O(config["workbench"])["requireDelivery"] = false
				}
				if tc.name == "disconnected" {
					return nil
				}
				if tc.name == "tool_only" {
					return emit(M{"type": "tool_call_response", "session_id": session, "agent_name": agent, "tool_call_id": "tool", "response": "done"})
				}
				target := tc.session
				if target == "root" {
					target = session
				}
				if target == "other-root" {
					target = S(other["nativeSessionId"])
				}
				if target == "child" {
					if err := emit(M{"type": "stream_started", "session_id": target, "agent_name": "worker"}); err != nil {
						return err
					}
				}
				if tc.failure != "" {
					if err := emit(M{"type": "error", "session_id": session, "error": tc.failure}); err != nil {
						return err
					}
				}
				return emit(M{"type": "stream_stopped", "session_id": target, "reason": tc.reason, "finish_reason": tc.finish})
			}
			submitTask(t, m, id, "work")
			done := waitIdle(t, m, id)
			if I(done["turnCount"]) != tc.wantTurns || len(A(done["queue"])) != 0 {
				t.Fatalf("unexpected automatic injection: %s", JSON(done))
			}
			unchanged := mustTask(t, m, S(other["id"]))
			if unchanged["nativeStopped"] != nil || unchanged["lastTurnReason"] != nil || unchanged["turnTiming"] != nil {
				t.Fatal("another root session was modified")
			}
		})
	}
}

func TestAcceptedPromptSurvivesPreparationFailure(t *testing.T) {
	m, s, f, id := taskFixture(t)
	executed := false
	f.run = func(context.Context, string, string, string, []M, string, map[string]string, func(M) error) error {
		executed = true
		return nil
	}
	m.options.BeginTurn = func(context.Context, M) (map[string]string, func(), error) {
		return nil, nil, errors.New("workspace unavailable")
	}
	submitTask(t, m, id, "preserve request")
	task := waitIdle(t, m, id)
	messages, e := s.Messages(id, 0, 100)
	if e != nil || executed || I(task["turnCount"]) != 1 || S(task["lastTurnReason"]) != "error" || len(messages) != 1 || S(O(O(A(messages[0]["blocks"])[0])["data"])["markdown"]) != "preserve request" {
		t.Fatal(task, messages, e)
	}
}
