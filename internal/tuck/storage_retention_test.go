package tuck

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestEventRetentionPreservesFactsAndCursor(t *testing.T) {
	m, s, _, id := taskFixture(t)
	task := mustUpdate(t, m, id, M{"status": "waiting", "pendingInteraction": M{"id": "approval", "kind": "approval"}, "historyTiming": M{"previousMs": 3000, "previousTurns": 2, "incomplete": true}})
	msg := M{"id": id + ":m-0", "index": 0, "revision": 9, "role": "assistant", "blocks": []any{newBlock("image", "attachments", M{"items": []any{M{"src": "data:image/png;base64,AAAA"}}}), newBlock("tool", "tool-call", M{"status": "completed"})}}
	if _, err := s.Publish(task, msg, M{"type": "message", "message": msg}); err != nil {
		t.Fatal(err)
	}
	beforeStats, err := s.Statistics(task)
	if err != nil {
		t.Fatal(err)
	}
	// More than one batch, including zero timestamps from the old writer.
	err = s.Write(func(tx *sql.Tx) error {
		for i := 0; i < 1100; i++ {
			if _, e := tx.Exec("INSERT INTO events(task_id,type,created_at_ms,payload_json) VALUES(?,'state',0,'{\"type\":\"state\"}')", id); e != nil {
				return e
			}
		}
		_, e := tx.Exec("UPDATE events SET created_at_ms=0")
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	oldCursor, _ := s.Cursor()
	if _, err = s.Publish(task, nil, M{"type": "state", "task": task}); err != nil {
		t.Fatal(err)
	}
	if err = s.pruneEvents(context.Background(), NowMS(), 128<<20); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Events(0, 512); !errors.Is(err, errEventCursorExpired) {
		t.Fatal("expired replay was silently truncated", err)
	}
	events, err := s.Events(oldCursor, 512)
	if err != nil || len(events) != 1 {
		t.Fatal(events, err)
	}
	high, _ := s.Cursor()
	// Even a single event larger than the budget can be discarded safely.
	if err = s.pruneEvents(context.Background(), NowMS(), 0); err != nil {
		t.Fatal(err)
	}
	current, _ := s.Cursor()
	if current != high {
		t.Fatal("empty cache lost high-water mark", current, high)
	}
	if _, err = s.Events(high-1, 10); !errors.Is(err, errEventCursorExpired) {
		t.Fatal(err)
	}
	if events, err = s.Events(high, 10); err != nil || len(events) != 0 {
		t.Fatal(events, err)
	}
	storedTask, _ := s.Task(id)
	storedMessage, _ := s.Message(id, 0)
	stats, _ := s.Statistics(storedTask)
	if !reflect.DeepEqual(storedTask, Clone(task)) || !reflect.DeepEqual(storedMessage, Clone(msg)) || !reflect.DeepEqual(stats, beforeStats) {
		t.Fatal("retention changed durable facts", storedTask, storedMessage, stats)
	}
	next, err := s.Publish(task, nil, M{"type": "state", "task": task})
	if err != nil || int64(N(next["seq"])) != high+1 {
		t.Fatal(next, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = s.pruneEvents(ctx, NowMS(), 0); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if events, err = s.Events(high, 10); err != nil || len(events) != 1 {
		t.Fatal("cancelled maintenance changed cache", events, err)
	}
	if err = s.Check(); err != nil {
		t.Fatal(err)
	}
}

func TestNativeEventsOnlyPersistBusinessProjection(t *testing.T) {
	m, s, _, id := taskFixture(t)
	for i := 0; i < 100; i++ {
		mustEvent(t, m, id, M{"type": "partial_tool_call", "tool_call": M{"id": "call", "function": M{"arguments": strings.Repeat("x", 2048)}}})
		mustEvent(t, m, id, M{"type": "agent_choice", "content": "part", "message_id": "answer"})
	}
	raw, err := s.TaskEvents(id, []string{"native"})
	if err != nil || len(raw) != 0 {
		t.Fatal("native stream was duplicated", len(raw), err)
	}
	messages, _ := s.Messages(id, 0, 10)
	if len(messages) != 1 || !strings.Contains(string(JSON(messages)), strings.Repeat("part", 100)) {
		t.Fatal("normalized history lost streamed content", messages)
	}
}

func TestSteeringReceiptWriteFailureCanBeRetried(t *testing.T) {
	m, s, _, id := taskFixture(t)
	task := mustUpdate(t, m, id, M{"status": "working", "turnId": "turn"})
	if _, err := m.Steer(context.Background(), id, "confirmed", nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec("CREATE TRIGGER fail_receipt BEFORE UPDATE ON messages BEGIN SELECT RAISE(ABORT,'receipt failed'); END"); err != nil {
		t.Fatal(err)
	}
	event := M{"type": "user_message", "session_id": task["nativeSessionId"], "message": "confirmed", "message_id": "receipt"}
	if err := m.HandleEvent(id, event); err == nil {
		t.Fatal("expected receipt write failure")
	}
	msg, err := s.Message(id, 0)
	if err != nil || S(O(msg["steering"])["status"]) != "pending" || m.receipts[id]["receipt"] || m.steering[id][0].status != "pending" {
		t.Fatal("failed receipt was acknowledged in memory or storage", err)
	}
	if _, err = s.DB.Exec("DROP TRIGGER fail_receipt"); err != nil {
		t.Fatal(err)
	}
	mustEvent(t, m, id, event)
	msg, err = s.Message(id, 0)
	if err != nil || S(O(msg["steering"])["status"]) != "delivered" || S(O(msg["steering"])["messageId"]) != "receipt" {
		t.Fatal("receipt retry did not persist confirmation", err)
	}
	revision := I(msg["revision"])
	mustEvent(t, m, id, event)
	msg, err = s.Message(id, 0)
	if err != nil || I(msg["revision"]) != revision {
		t.Fatal("duplicate receipt was not idempotent", err)
	}
	if _, err = m.Steer(context.Background(), id, "unconfirmed", nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec("CREATE TRIGGER fail_settle BEFORE UPDATE ON messages BEGIN SELECT RAISE(ABORT,'settle failed'); END"); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	err = m.settleSteeringLocked(fixtureTaskLocked(m, id))
	m.mu.Unlock()
	if err == nil || m.steering[id][0].status != "pending" {
		t.Fatal("failed settlement was acknowledged in memory", err)
	}
	if _, err = s.DB.Exec("DROP TRIGGER fail_settle"); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	err = m.settleSteeringLocked(fixtureTaskLocked(m, id))
	m.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	msg, err = s.Message(id, 1)
	if err != nil || S(O(msg["steering"])["status"]) != "unknown" {
		t.Fatal("settlement retry did not persist uncertainty", err)
	}
}

func TestStoreAutomaticallyPrunesExpiredReplayOnOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tuck.db")
	s, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	s.cancelMaintenance()
	<-s.maintenanceDone
	event, err := s.Publish(nil, nil, M{"type": "connection"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec("UPDATE events SET created_at_ms=0"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	deadline := time.Now().Add(3 * time.Second)
	for {
		_, err = s.Events(0, 10)
		if errors.Is(err, errEventCursorExpired) {
			break
		}
		if err != nil || time.Now().After(deadline) {
			t.Fatal("automatic retention did not expire the old replay", err)
		}
		time.Sleep(time.Millisecond)
	}
	if cursor, err := s.Cursor(); err != nil || cursor != int64(N(event["seq"])) {
		t.Fatal("automatic maintenance lost cursor", cursor, err)
	}
}

func TestNativeProgressCheckpointSurvivesRestartWithoutReplay(t *testing.T) {
	m, s, _, id := taskFixture(t)
	task := fixtureTaskLocked(m, id)
	task["status"], task["turnId"] = "working", "turn"
	task["turnTiming"] = M{"turnId": "turn", "startedAt": 1, "observedAt": 1}
	task["activity"] = M{"phase": "processing"}
	if err := s.SaveTask(task); err != nil {
		t.Fatal(err)
	}
	high, _ := s.Cursor()
	progress := M{"type": "partial_tool_call", "tool_call": M{"id": "call", "function": M{"arguments": "unfinished"}}}
	// A failed checkpoint must not advance the in-memory recovery timestamp.
	if _, err := s.DB.Exec("CREATE TRIGGER fail_checkpoint BEFORE UPDATE ON tasks BEGIN SELECT RAISE(ABORT,'checkpoint failed'); END"); err != nil {
		t.Fatal(err)
	}
	if err := m.HandleEvent(id, progress); err == nil || N(O(task["turnTiming"])["observedAt"]) != 1 {
		t.Fatal("failed checkpoint changed timing", task, err)
	}
	s.DB.Exec("DROP TRIGGER fail_checkpoint")
	before := NowMS()
	mustEvent(t, m, id, progress)
	saved, _ := s.Task(id)
	observed := N(O(saved["turnTiming"])["observedAt"])
	if observed < float64(before) {
		t.Fatal("unprojected progress lost its last observation", saved)
	}
	if cursor, _ := s.Cursor(); cursor != high {
		t.Fatal("progress checkpoint created a redundant replay event", cursor, high)
	}
	restarted, err := NewManager(m.options)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close(context.Background())
	recovered, _ := restarted.Get(id)
	if !B(O(recovered["turnTiming"])["incomplete"]) || N(O(recovered["turnTiming"])["observedAt"]) != observed {
		t.Fatal("restart changed the last confirmed observation", recovered)
	}
}

func TestMigrationV2ExtractsFactsBeforeDroppingReplay(t *testing.T) {
	path, config, s := legacyMigrationFixture(t)
	if _, err := s.DB.Exec(schemaV2); err != nil {
		t.Fatal(err)
	}
	task := BaseTask(ID(), UUID(), "retained history", "", "root", "agent")
	id := S(task["id"])
	task["status"], task["turnId"] = "waiting", "current"
	task["turnTiming"] = M{"turnId": "current", "startedAt": 9000, "observedAt": 10000, "waitingSince": 9500}
	task["pendingInteraction"] = M{"id": "approval-original", "kind": "approval"}
	task["queue"] = []any{M{"id": "queued", "prompt": "keep", "source": M{"kind": "user"}}}
	task["queuedPrompts"] = 1
	task["usage"] = M{"inputTokens": 123, "outputTokens": 456}
	task["logicalCount"] = 3
	image := "data:image/png;base64," + strings.Repeat("A", 256<<10)
	messages := []M{
		{"index": 0, "revision": 19, "role": "assistant", "blocks": []any{newBlock("image", "attachments", M{"items": []any{M{"src": image}}}), newBlock("tool", "tool-call", M{"status": "completed"})}},
		{"index": 1, "revision": 4, "role": "user", "steering": M{"hash": Hash([]byte("confirmed")), "afterSeq": 0, "status": "pending"}, "blocks": []any{newBlock("steer:pending", "markdown", M{"markdown": "> pending\n\nconfirmed"})}},
		{"index": 2, "revision": 6, "role": "user", "blocks": []any{newBlock("steer:legacy", "markdown", M{"markdown": "> legacy\n\nlegacy"})}},
	}
	for _, msg := range messages {
		if _, err := s.Publish(task, msg, M{"type": "message", "message": msg}); err != nil {
			t.Fatal(err)
		}
	}
	for _, timing := range []M{
		{"turnId": "first", "startedAt": 1000, "observedAt": 1500},
		{"turnId": "first", "startedAt": 1000, "observedAt": 3000, "endedAt": 3000},
		{"turnId": "second", "startedAt": 4000, "observedAt": 5000, "incomplete": true},
		O(task["turnTiming"]),
	} {
		if _, err := s.Publish(task, nil, M{"type": "state", "task": Merge(task, M{"turnTiming": timing})}); err != nil {
			t.Fatal(err)
		}
	}
	for _, text := range []string{"confirmed", "legacy"} {
		if _, err := s.Publish(task, nil, M{"type": "native", "event": M{"type": "user_message", "session_id": task["nativeSessionId"], "message": text, "message_id": "receipt-" + text}}); err != nil {
			t.Fatal(err)
		}
	}
	// Model the former repeated image snapshots without accessing production.
	for i := 0; i < 64; i++ {
		if _, err := s.Publish(task, nil, M{"type": "message", "message": messages[0]}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.DB.Exec("UPDATE events SET created_at_ms=0"); err != nil {
		t.Fatal(err)
	}
	high, _ := s.Cursor()
	if err := MigrateCommand(path, []string{"--check"}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	before := filepath.Join(filepath.Dir(path), "before")
	if err := MigrateCommand(path, []string{"--backup", before}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if err := MigrateCommand(path, []string{"--check"}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if version, err := inspectDatabase(s.DB); err != nil || version != databaseVersion {
		t.Fatal(version, err)
	}
	got, _ := s.Task(id)
	history := O(got["historyTiming"])
	if N(history["previousMs"]) != 3000 || I(history["previousTurns"]) != 2 || !B(history["incomplete"]) {
		t.Fatal(history)
	}
	unchanged := Clone(got)
	delete(unchanged, "historyTiming")
	if !reflect.DeepEqual(unchanged, Clone(task)) {
		t.Fatal("migration changed task/approval/queue/usage/current turn")
	}
	media, _ := s.Message(id, 0)
	if !reflect.DeepEqual(media, Clone(messages[0])) {
		t.Fatal("media, tool call or revision changed")
	}
	for i, text := range []string{"confirmed", "legacy"} {
		msg, _ := s.Message(id, i+1)
		receipt := O(msg["steering"])
		if receipt["status"] != "delivered" || receipt["messageId"] != "receipt-"+text || I(msg["revision"]) != I(messages[i+1]["revision"]) {
			t.Fatal(msg)
		}
	}
	if cursor, _ := s.Cursor(); cursor != high {
		t.Fatal("migration reused cursor", cursor, high)
	}
	if _, err := s.Events(high-1, 10); !errors.Is(err, errEventCursorExpired) {
		t.Fatal("old browser was not reset", err)
	}
	stats, _ := s.Statistics(got)
	if N(stats["previousMs"]) != 3000 || I(stats["toolCalls"]) != 1 {
		t.Fatal(stats)
	}
	if err := migrateDatabase(s, config, databaseVersion); err != nil {
		t.Fatal(err)
	}
	again, _ := s.Task(id)
	if !reflect.DeepEqual(again, got) {
		t.Fatal("repeat migration aggregated twice")
	}
	var beforePages, afterPages int
	s.DB.QueryRow("PRAGMA page_count").Scan(&beforePages)
	if _, err := s.DB.Exec("VACUUM"); err != nil {
		t.Fatal(err)
	}
	s.DB.QueryRow("PRAGMA page_count").Scan(&afterPages)
	if afterPages >= beforePages/4 {
		t.Fatal("offline compaction did not reclaim discarded replay", beforePages, afterPages)
	}
	t.Logf("synthetic v2 migration: pages %d -> %d; cursor %d; image/revisions/statistics/approval/queue preserved", beforePages, afterPages, high)
	// Startup settles the existing approval and pauses the queue; no engine call.
	engine := &fakeNative{request: func(context.Context, string, string, any) (M, error) {
		t.Error("recovery executed a native request")
		return nil, fmt.Errorf("unexpected request")
	}}
	manager, err := NewManager(ManagerOptions{Store: s, Engine: engine, WorkspaceRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close(context.Background())
	recovered, _ := manager.Get(id)
	if recovered["pendingInteraction"] != nil || recovered["status"] != "idle" || !B(recovered["queuePaused"]) {
		t.Fatal("restart recovery changed", recovered)
	}
	if err := s.Check(); err != nil {
		t.Fatal(err)
	}
}

func TestExpiredSSERequestsSnapshotReset(t *testing.T) {
	f := newPlatformFixture(t, 5)
	app := f.startApp()
	if err := app.Manager.PublishGlobal(M{"type": "fixture"}); err != nil {
		t.Fatal(err)
	}
	high, _ := app.Store.Cursor()
	if err := app.Store.pruneEvents(context.Background(), NowMS(), 0); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", app.URL+"/api/events?since="+fmt.Sprint(high), nil)
	req.Header.Set("Last-Event-ID", "0")
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != 200 || !strings.Contains(string(body), "event: reset\ndata: {\"reason\":\"cursor_expired\"}") || strings.Contains(string(body), "id:") {
		t.Fatal(response.StatusCode, string(body), err)
	}
}

func TestMigrationV3FactExtractionRollsBack(t *testing.T) {
	_, config, s := legacyMigrationFixture(t)
	if _, err := s.DB.Exec(schemaV2); err != nil {
		t.Fatal(err)
	}
	task := BaseTask(ID(), UUID(), "rollback", "", "root", "agent")
	id := S(task["id"])
	if err := s.SaveTask(task); err != nil {
		t.Fatal(err)
	}
	// The public check rejects this divergent JSON position. If encountered
	// inside a migration, the failed receipt write must roll back prior facts.
	msg := M{"index": -1, "revision": 0, "role": "user", "blocks": []any{newBlock("steer:bad", "markdown", M{"markdown": "> pending\n\nbody"})}}
	if _, err := s.DB.Exec("INSERT INTO messages VALUES(?,0,0,?)", id, string(JSON(msg))); err != nil {
		t.Fatal(err)
	}
	if err := migrateDatabase(s, config, 2); err == nil {
		t.Fatal("invalid message unexpectedly migrated")
	}
	if version, err := inspectDatabase(s.DB); err != nil || version != 2 {
		t.Fatal("partially committed schema", version, err)
	}
	got, _ := s.Task(id)
	if !reflect.DeepEqual(got, Clone(task)) {
		t.Fatal("statistics escaped rolled-back transaction", got)
	}
}
