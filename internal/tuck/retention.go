package tuck

import "time"

const retentionDay = int64(24 * time.Hour / time.Millisecond)

func defaultRetention() M {
	return M{
		"conversations":     M{"autoArchive": true, "inactiveDays": 30, "autoDelete": false, "archivedDays": 90},
		"temporaryRuns":     M{"autoDelete": false, "archivedDays": 30},
		"managedWorkspaces": M{"autoCleanup": false},
	}
}

func validateRetention(value any) error {
	r := O(value)
	c, t, w := O(r["conversations"]), O(r["temporaryRuns"]), O(r["managedWorkspaces"])
	if len(r) != 3 || !noUnknown(r, "conversations", "temporaryRuns", "managedWorkspaces") ||
		len(c) != 4 || !noUnknown(c, "autoArchive", "inactiveDays", "autoDelete", "archivedDays") ||
		!isBool(c["autoArchive"]) || !isBool(c["autoDelete"]) || !Integer(c["inactiveDays"], 1, 36500) || !Integer(c["archivedDays"], 1, 36500) ||
		len(t) != 2 || !noUnknown(t, "autoDelete", "archivedDays") || !isBool(t["autoDelete"]) || !Integer(t["archivedDays"], 1, 36500) ||
		len(w) != 1 || !noUnknown(w, "autoCleanup") || !isBool(w["autoCleanup"]) {
		return Problem(422, "invalid_retention", "保留策略开关必须为布尔值，期限必须为 1–36500 的整数天")
	}
	return nil
}

// These facts are task-owned. Unknown legacy timestamps remain zero until
// the relevant policy first observes them; listing never starts a timer.
func lifecycle(task M) M {
	v := O(task["lifecycle"])
	if task["lifecycle"] == nil {
		v = M{"purpose": "conversation", "keep": false, "lastActivityAt": 0, "archivedAt": 0}
		task["lifecycle"] = v
	}
	return v
}

func activity(task M, now int64) {
	lifecycle(task)["lastActivityAt"] = now
}

func lifecycleError(err error) string {
	text := []rune(err.Error())
	if len(text) > 2048 {
		text = append(text[:2048], '…')
	}
	return string(text)
}

func validateLifecycle(task M) error {
	if value := task["managedWorkspaceId"]; value != nil && (!stringField(value, 1, 128) || !publicID.MatchString(S(value)) || S(task["workspaceMode"]) != "auto") {
		return Problem(409, "persistent_lifecycle", "托管目录引用必须属于自动工作目录")
	}
	if task["lifecycle"] == nil {
		return nil // Released tasks may have no lifecycle facts yet.
	}
	l, ok := task["lifecycle"].(map[string]any)
	if !ok || !noUnknown(l, "purpose", "keep", "lastActivityAt", "archivedAt", "activityObservedAt", "archiveObservedAt", "lastDeleteError") {
		return Problem(409, "persistent_lifecycle", "对话生命周期字段无效")
	}
	if l["purpose"] != nil && l["purpose"] != "conversation" && l["purpose"] != "temporary" ||
		l["keep"] != nil && !isBool(l["keep"]) || l["lastDeleteError"] != nil && !stringField(l["lastDeleteError"], 0, maxStoredJSON) {
		return Problem(409, "persistent_lifecycle", "对话用途、保留标记或删除错误类型无效")
	}
	for _, key := range []string{"lastActivityAt", "archivedAt", "activityObservedAt", "archiveObservedAt"} {
		if l[key] != nil && !Integer(l[key], 0, 1<<53-1) {
			return Problem(409, "persistent_lifecycle", "对话生命周期时间必须是非负整数毫秒")
		}
	}
	return nil
}

func retentionRequiresConfirmation(previous, next M) bool {
	for _, key := range []string{"conversations", "temporaryRuns"} {
		a, b := O(previous[key]), O(next[key])
		if B(b["autoDelete"]) && (!B(a["autoDelete"]) || I(b["archivedDays"]) < I(a["archivedDays"])) {
			return true
		}
	}
	return B(O(next["managedWorkspaces"])["autoCleanup"]) && !B(O(previous["managedWorkspaces"])["autoCleanup"])
}
