package tuck

import (
	"context"
	"log/slog"
	"time"
)

func (m *Manager) taskTreeLocked(root M) ([]M, error) {
	rows, err := m.options.Store.DB.Query(`WITH RECURSIVE tree(id) AS (SELECT ? UNION SELECT tasks.id FROM tasks JOIN tree ON tasks.parent_task_id=tree.id) SELECT id FROM tree`, S(root["id"]))
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			break
		}
		ids = append(ids, id)
	}
	readErr := rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if readErr != nil {
		return nil, readErr
	}
	tasks := []M{}
	for _, id := range ids {
		t, e := m.getLocked(id)
		if e != nil {
			return nil, e
		}
		tasks = append(tasks, t)
	}
	return tasks, nil
}

func (m *Manager) taskBusyLocked(t M) bool {
	id := S(t["id"])
	busy := S(t["status"]) != "idle" || t["pendingInteraction"] != nil || len(A(t["queue"])) > 0 || I(t["queuedPrompts"]) > 0 ||
		m.runs[id] != nil || m.taskUses[id] > 0 || len(m.interactions[id]) > 0 || m.resolving[id] || m.policyChanges[id] || m.sending[id] || m.deleting[id] || m.nativeChanges[id] > 0
	for _, run := range A(t["backgroundRuns"]) {
		busy = busy || S(O(run)["status"]) == "running"
	}
	return busy
}

func (m *Manager) retentionLocked(root M, policy M, now int64) M {
	state := M{"action": "", "dueAt": nil, "blockedReason": ""}
	if S(root["parentTaskId"]) != "" {
		state["blockedReason"] = "随主对话保留"
		return state
	}
	if m.closed || m.changing {
		state["blockedReason"] = "服务正在关闭或应用配置"
		return state
	}
	temporary := S(O(root["lifecycle"])["purpose"]) == "temporary"
	var latest int64
	tree, err := m.taskTreeLocked(root)
	if err != nil {
		state["blockedReason"] = "无法读取子会话，请稍后重试"
		return state
	}
	for _, task := range tree {
		l := O(task["lifecycle"])
		if temporary && task["lastFailure"] != nil {
			state["blockedReason"] = "失败尚未处理"
			return state
		}
		if B(l["keep"]) {
			state["blockedReason"] = "已标记保留"
			return state
		}
		if m.taskBusyLocked(task) {
			state["blockedReason"] = "存在执行、交互或待处理队列"
			return state
		}
		latest = max(latest, int64(N(l["lastActivityAt"])))
	}
	l := O(root["lifecycle"])
	p := O(policy["conversations"])
	if B(root["archived"]) {
		if temporary {
			p = O(policy["temporaryRuns"])
		}
		if !B(p["autoDelete"]) {
			state["blockedReason"] = "自动删除未启用"
			return state
		}
		since := int64(N(l["archivedAt"]))
		if since == 0 {
			since = int64(N(l["archiveObservedAt"]))
		}
		if since == 0 {
			state["blockedReason"] = "等待建立归档观察起点"
			return state
		}
		due := since + int64(I(p["archivedDays"]))*retentionDay
		state["dueAt"] = due
		if now >= due {
			state["action"] = "delete"
		}
	} else {
		if !temporary && !B(p["autoArchive"]) {
			state["blockedReason"] = "自动归档未启用"
			return state
		}
		latest = max(latest, int64(N(l["activityObservedAt"])))
		if latest == 0 {
			state["blockedReason"] = "等待建立活动观察起点"
			return state
		}
		due := latest + int64(I(p["inactiveDays"]))*retentionDay
		if temporary {
			due = latest
		}
		state["dueAt"] = due
		if now >= due {
			state["action"] = "archive"
		}
	}
	return state
}

func (m *Manager) RetentionState(id string, policy M, now int64) (M, error) {
	m.mu.Lock()
	defer m.unlock()
	task, err := m.getLocked(id)
	if err != nil {
		return nil, err
	}
	state := m.retentionLocked(task, policy, now)
	state["archivedAt"] = O(task["lifecycle"])["archivedAt"]
	state["keep"] = B(O(task["lifecycle"])["keep"])
	state["lastDeleteError"] = O(task["lifecycle"])["lastDeleteError"]
	return state, nil
}

func (m *Manager) setArchived(id string, archived, automatic bool, now int64) (M, error) {
	m.mu.Lock()
	defer m.unlock()
	task, err := m.getLocked(id)
	if err != nil {
		return nil, err
	}
	if m.closed || m.changing || S(task["parentTaskId"]) != "" {
		return nil, Problem(409, "task_readonly", "只能修改未关闭的主对话")
	}
	tree, err := m.taskTreeLocked(task)
	if err != nil {
		return nil, err
	}
	for _, t := range tree {
		if m.taskBusyLocked(t) {
			return nil, Problem(409, "task_active", "请先处理主对话及子会话的执行、交互和队列")
		}
	}
	if automatic && S(m.retentionLocked(task, O(m.Config()["retention"]), now)["action"]) != "archive" {
		return nil, Problem(409, "retention_changed", "归档资格已变化")
	}
	next := Clone(task)
	if B(next["archived"]) != archived {
		next["archived"] = archived
		l := lifecycle(next)
		l["archiveObservedAt"] = 0
		if archived {
			l["archivedAt"] = now
		} else {
			l["archivedAt"] = 0
			activity(next, now)
			delete(l, "lastDeleteError")
		}
		if err = m.publishLocked(next, M{"type": "state", "task": next}, nil); err != nil {
			return nil, err
		}
		m.tasks[id] = next
	}
	return Clone(next), nil
}

func (m *Manager) Keep(id string, keep bool) (M, error) {
	return m.Update(id, func(t M) error {
		if m.closed || m.changing || S(t["parentTaskId"]) != "" {
			return Problem(409, "task_readonly", "只能修改主对话")
		}
		lifecycle(t)["keep"] = keep
		return nil
	})
}

// Existing unknown timestamps acquire an observation anchor only when their
// policy is enabled. Reading/previews and offline migrations do not mutate it.
func (m *Manager) observeRetention(id string, now int64) error {
	m.mu.Lock()
	defer m.unlock()
	task, err := m.getLocked(id)
	if err != nil {
		return err
	}
	policy := O(m.Config()["retention"])
	p := O(policy["conversations"])
	next := Clone(task)
	l := lifecycle(next)
	changed := false
	if B(next["archived"]) {
		if S(l["purpose"]) == "temporary" {
			p = O(policy["temporaryRuns"])
		}
		if B(p["autoDelete"]) && N(l["archivedAt"]) == 0 && N(l["archiveObservedAt"]) == 0 {
			l["archiveObservedAt"] = now
			changed = true
		}
	} else if B(p["autoArchive"]) && N(l["activityObservedAt"]) == 0 {
		tree, err := m.taskTreeLocked(task)
		if err != nil {
			return err
		}
		for _, t := range tree {
			if N(O(t["lifecycle"])["lastActivityAt"]) == 0 {
				l["activityObservedAt"] = now
				changed = true
				break
			}
		}
	}
	if changed {
		if err = m.publishLocked(next, M{"type": "state", "task": next}, nil); err != nil {
			return err
		}
		m.tasks[id] = next
	}
	return nil
}

func (a *App) lifecycleOperation(ctx context.Context) (context.Context, func(), error) {
	a.lifecycleMu.Lock()
	defer a.lifecycleMu.Unlock()
	if a.closing.Load() {
		return nil, nil, Problem(409, "service_closing", "服务正在关闭")
	}
	a.lifecycleWG.Add(1)
	op, cancel := context.WithCancel(ctx)
	stop := func() bool { return false }
	if a.ctx != nil {
		stop = context.AfterFunc(a.ctx, cancel)
	}
	return op, func() { stop(); cancel(); a.lifecycleWG.Done() }, nil
}

func (a *App) deleteConversation(ctx context.Context, id string, automatic bool, now int64) (M, error) {
	ctx, finish, err := a.lifecycleOperation(ctx)
	if err != nil {
		return nil, err
	}
	defer finish()
	deleted, err := a.Manager.deleteConversation(ctx, id, automatic, now)
	if err != nil {
		return nil, err
	}
	for _, key := range Strings(deleted["deletedTaskIds"]) {
		if err = a.Delivery.cleanup(key); err != nil {
			deleted["cleanupPending"] = true
		}
	}
	// Maintenance sweeps directories once after the candidate batch, so an
	// unrelated failed directory is not retried for every deleted conversation.
	if !automatic {
		pending, err := a.Files.cleanupWorkspaces(ctx)
		if pending || err != nil {
			deleted["cleanupPending"] = true
		}
	}
	return deleted, nil
}

func (a *App) maintainLifecycle(ctx context.Context, now int64) error {
	// Keyset pagination avoids skipping records after deletion and bounds each
	// candidate batch. Failures advance the cursor rather than spinning.
	var after int64
	for {
		rows, err := a.Store.DB.QueryContext(ctx, "SELECT rowid,id FROM tasks WHERE rowid>? AND (parent_task_id IS NULL OR parent_task_id='') ORDER BY rowid LIMIT 32", after)
		if err != nil {
			return err
		}
		type candidate struct {
			row int64
			id  string
		}
		batch := []candidate{}
		for rows.Next() {
			var c candidate
			if err = rows.Scan(&c.row, &c.id); err != nil {
				break
			}
			if c.row > after {
				batch = append(batch, c)
			}
			if len(batch) == 32 {
				break
			}
		}
		readErr := rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if readErr != nil {
			return readErr
		}
		if len(batch) == 0 {
			break
		}
		for _, c := range batch {
			if err = ctx.Err(); err != nil {
				return err
			}
			after = c.row
			if err = a.Manager.observeRetention(c.id, now); err != nil {
				continue
			}
			state, e := a.Manager.RetentionState(c.id, O(a.Models.Config()["retention"]), now)
			if e != nil {
				continue
			}
			switch S(state["action"]) {
			case "archive":
				_, e = a.Manager.setArchived(c.id, true, true, now)
			case "delete":
				_, e = a.deleteConversation(ctx, c.id, true, now)
			}
			if e != nil && ctx.Err() == nil {
				slog.Warn("task retention", "task", c.id, "error", e)
			}
		}
	}
	_, err := a.Files.cleanupWorkspaces(ctx)
	return err
}

func (a *App) startLifecycle() {
	a.lifecycleMu.Lock()
	if a.closing.Load() || a.maintenanceStarted {
		a.lifecycleMu.Unlock()
		return
	}
	a.maintenanceStarted = true
	a.lifecycleWG.Add(1)
	a.lifecycleMu.Unlock()
	go func() {
		defer a.lifecycleWG.Done()
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			if err := a.maintainLifecycle(a.ctx, NowMS()); err != nil && a.ctx.Err() == nil {
				slog.Error("lifecycle maintenance", "error", err)
			}
			select {
			case <-a.ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}
