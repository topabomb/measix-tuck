package tuck

import (
	"net/http"
	"reflect"
)

// This endpoint edits only Tuck retention policy. It never applies pending
// provider/engine edits or routes through the engine-restarting runtime path.
func (a *App) retentionSettings(w http.ResponseWriter, r *http.Request) error {
	ctx, finish, err := a.lifecycleOperation(r.Context())
	if err != nil {
		return err
	}
	defer finish()
	r = r.WithContext(ctx)
	if err := a.Protection.Require(r, "orchestration"); err != nil {
		return err
	}
	if r.Method == "GET" {
		current, revision, err := a.Config.Read()
		if err != nil {
			return err
		}
		workspaces, err := a.Store.managedWorkspaces()
		if err != nil {
			return err
		}
		pending := []any{}
		for _, v := range workspaces {
			if v.State != "attached" || v.LastError != "" {
				pending = append(pending, M{"id": v.ID, "path": v.Path, "state": v.State, "error": v.LastError, "cleanupAllowed": v.CleanupAllowed})
			}
		}
		WriteJSON(w, 200, M{"retention": current["retention"], "applied": a.Models.Config()["retention"], "revision": revision, "pendingWorkspaces": pending})
		return nil
	}
	if r.Method != "POST" && r.Method != "PUT" {
		return Problem(405, "method", "Method not allowed")
	}
	if err := a.Protection.sameOrigin(r); err != nil {
		return err
	}
	data, err := ReadJSON(r.Body, 16384)
	if err != nil {
		return err
	}
	if !noUnknown(data, "retention", "revision", "confirmation") {
		return Problem(400, "invalid_retention", "保留设置请求包含未知字段")
	}
	if err = validateRetention(data["retention"]); err != nil {
		return err
	}
	if !a.changeMu.TryLock() {
		return Problem(409, "configuration_changing", "配置正在应用")
	}
	defer a.changeMu.Unlock()
	current, revision, err := a.Config.Read()
	if err != nil {
		return err
	}
	if S(data["revision"]) != revision {
		return Problem(409, "settings_changed", "配置已改变，请重新读取")
	}
	next := Clone(O(data["retention"]))
	// Deterministic receipt binds the proposed policy and file revision; it is
	// not an authorization token and all permissions are checked again on PUT.
	receipt := Hash(JSON(M{"revision": revision, "retention": next}))
	requires := retentionRequiresConfirmation(O(a.Models.Config()["retention"]), next)
	if r.Method == "POST" {
		archive, remove := 0, 0
		tasks, err := a.Manager.Snapshots()
		if err != nil {
			return err
		}
		for _, task := range tasks {
			state, e := a.Manager.RetentionState(S(task["id"]), next, NowMS())
			if e != nil {
				continue
			}
			switch S(state["action"]) {
			case "archive":
				archive++
			case "delete":
				remove++
			}
		}
		directories := 0
		if B(O(next["managedWorkspaces"])["autoCleanup"]) {
			if err = a.Store.DB.QueryRow("SELECT COUNT(*) FROM managed_workspaces WHERE state='pending' AND cleanup_allowed=1").Scan(&directories); err != nil {
				return err
			}
		}
		WriteJSON(w, 200, M{"confirmation": receipt, "requiresConfirmation": requires, "archiveCandidates": archive, "deleteCandidates": remove, "pendingDirectories": directories})
		return nil
	}
	if requires && S(data["confirmation"]) != receipt {
		return Problem(409, "retention_confirmation_required", "请预览并确认永久删除与目录回收范围")
	}
	if err = a.Protection.Require(r, "orchestration"); err != nil {
		return err
	}
	a.Manager.mu.Lock()
	defer a.Manager.unlock()
	if a.closing.Load() || a.Manager.changing {
		return Problem(409, "configuration_changing", "服务正在关闭或应用配置")
	}
	applied := a.Config.Applied()
	current["retention"] = next
	ownRevision, err := a.Config.Save(revision, current)
	if err != nil {
		return err
	}
	applied["retention"] = Clone(next)
	// Retain the prior applied revision if other on-disk edits remain pending.
	a.Config.mu.RLock()
	appliedRevision := a.Config.appliedRevision
	a.Config.mu.RUnlock()
	if reflect.DeepEqual(applied, current) {
		appliedRevision = ownRevision
	}
	a.Config.MarkAppliedVersion(applied, appliedRevision)
	a.Models.mu.Lock()
	a.Models.config["retention"] = Clone(next)
	a.Models.mu.Unlock()
	// The loop rereads policy on each object. Retention-only edits do not
	// invalidate native creation/fork operations or change execution ownership.
	_, err = a.Store.Publish(nil, nil, M{"type": "retention", "retentionRevision": Hash(JSON(next))})
	if err != nil {
		return err
	}
	a.Manager.notifyLocked()
	WriteJSON(w, 200, M{"retention": next, "revision": ownRevision})
	return nil
}
