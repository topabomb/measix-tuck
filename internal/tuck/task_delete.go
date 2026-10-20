package tuck

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// Track in-flight public native mutations so deletion cannot race a rename/fork.
// This is an operation guard only; no deletion state is persisted.
func (m *Manager) beginNativeChange(id string) (M, func(), error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	task, err := m.getLocked(id)
	if err != nil {
		return nil, nil, err
	}
	m.nativeChanges[id]++
	return Clone(task), func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.nativeChanges[id]--
		if m.nativeChanges[id] == 0 {
			delete(m.nativeChanges, id)
		}
	}, nil
}

func nativeSessionMissing(err error) bool {
	var native *nativeHTTPError
	return errors.As(err, &native) && Status(err) == 404 &&
		S(native.Payload["message"]) == "session not found: session not found"
}

// Delete removes only an explicitly selected root and its owned descendants.
// Independent forks and all workspace/source files remain untouched.
func (m *Manager) Delete(ctx context.Context, id string) (M, error) {
	m.mu.Lock()
	root, err := m.getLocked(id)
	if err == nil && (m.changing || m.closed) {
		err = Problem(409, "configuration_changing", "配置正在应用，请稍后删除")
	}
	if err == nil && S(root["parentTaskId"]) != "" {
		err = Problem(409, "child_delete", "子会话随主对话一起删除，请从主对话操作")
	}
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}
	tasks := []M{Clone(root)}
	selected := map[string]bool{id: true}
	for i := 0; i < len(tasks); i++ {
		for childID, task := range m.tasks {
			if !selected[childID] && S(task["parentTaskId"]) == S(tasks[i]["id"]) {
				selected[childID] = true
				tasks = append(tasks, Clone(task))
			}
		}
	}
	for _, task := range tasks {
		key := S(task["id"])
		busy := S(task["status"]) != "idle" || task["pendingInteraction"] != nil ||
			len(A(task["queue"])) > 0 || I(task["queuedPrompts"]) > 0 ||
			m.runs[key] != nil || len(m.interactions[key]) > 0 || m.resolving[key] ||
			m.policyChanges[key] || m.sending[key] || m.deleting[key] || m.nativeChanges[key] > 0
		for _, run := range A(task["backgroundRuns"]) {
			busy = busy || S(O(run)["status"]) == "running"
		}
		if busy {
			m.mu.Unlock()
			return nil, Problem(409, "task_active", "请先结束主对话及子会话的执行、处理审批，并清空队列后再删除")
		}
	}
	for key := range selected {
		m.deleting[key] = true
	}
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		for key := range selected {
			delete(m.deleting, key)
		}
		m.mu.Unlock()
	}()

	// Children first also releases their separately registered native runtimes.
	// A failed attempt retains all local records, allowing an explicit retry.
	for i := len(tasks) - 1; i >= 0; i-- {
		task := tasks[i]
		if S(task["nativeSessionId"]) == "" {
			return nil, Problem(409, "native_session_missing_id", "缺少原生会话标识，未删除本地历史")
		}
		if _, err = m.options.Engine.Request(ctx, "GET", nativePath(task, ""), nil); err != nil {
			if nativeSessionMissing(err) {
				continue
			}
			return nil, Problem(502, "native_delete_failed", "引擎删除未完成，本地历史仍保留；部分原生会话可能已删除，请重试。原因："+err.Error())
		}
		result, e := m.options.Engine.Request(ctx, "DELETE", nativePath(task, "?wait=true"), nil)
		if e != nil {
			return nil, Problem(502, "native_delete_failed", "引擎删除未完成，本地历史仍保留；部分原生会话可能已删除，请重试。原因："+e.Error())
		}
		if S(result["message"]) != "session deleted" {
			return nil, Problem(409, "native_delete_unconfirmed", "引擎删除或停止尚未确认，本地历史仍保留，请稍后重试删除")
		}
	}

	ids := make([]string, 0, len(tasks))
	for _, task := range tasks {
		ids = append(ids, S(task["id"]))
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	result, err := m.options.Store.DeleteTasks(ctx, ids)
	if err != nil {
		return nil, Problem(500, "delete_commit_failed", "原生会话已删除，但本地提交失败；本地记录仍保留，请重试删除")
	}
	for key := range selected {
		delete(m.tasks, key)
		delete(m.taskOrder, key)
		delete(m.interactions, key)
		delete(m.steering, key)
		delete(m.receipts, key)
		delete(m.normalizer.children, key)
		delete(m.normalizer.todos, key)
		delete(m.normalizer.activityTools, key)
		m.normalizer.timings.clear(key)
	}
	for key, record := range m.normalizer.calls {
		if selected[S(record["taskId"])] {
			delete(m.normalizer.calls, key)
		}
	}
	for key := range m.normalizer.assistants {
		for id := range selected {
			if strings.HasPrefix(key, id+":") {
				delete(m.normalizer.assistants, key)
			}
		}
	}
	m.notifyLocked()
	return result, nil
}

func (s *Store) DeleteTasks(ctx context.Context, ids []string) (M, error) {
	event := M{"type": "deleted", "deletedTaskIds": ids}
	var seq int64
	err := s.WriteContext(ctx, func(tx *sql.Tx) error {
		for _, id := range ids {
			for _, table := range []string{"messages", "events", "task_deliveries"} {
				if _, e := tx.Exec("DELETE FROM "+table+" WHERE task_id=?", id); e != nil {
					return e
				}
			}
			if _, e := tx.Exec("DELETE FROM tasks WHERE id=?", id); e != nil {
				return e
			}
		}
		raw, e := storedJSON(event, maxStoredJSON)
		if e != nil {
			return e
		}
		result, e := tx.Exec("INSERT INTO events(task_id,type,created_at_ms,payload_json) VALUES(NULL,?,?,?)", "deleted", NowMS(), raw)
		if e != nil {
			return e
		}
		seq, e = result.LastInsertId()
		return e
	})
	return Merge(event, M{"seq": seq}), err
}
