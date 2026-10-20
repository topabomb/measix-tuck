package tuck

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type normalizer struct {
	m             *Manager
	calls         map[string]M
	assistants    map[string]int
	children      map[string]M
	todos         map[string][]any
	activityTools map[string]map[string]string
	timings       runtimeTimings
}

func (n *normalizer) init(m *Manager) {
	n.m = m
	n.calls = map[string]M{}
	n.assistants = map[string]int{}
	n.children = map[string]M{}
	n.todos = map[string][]any{}
	n.activityTools = map[string]map[string]string{}
}
func newBlock(id, kind string, data M) M {
	return M{"id": id, "type": kind, "data": data, "revision": 0}
}
func parseValue(v any) any {
	if text, ok := v.(string); ok {
		var r any
		if json.Unmarshal([]byte(text), &r) != nil {
			return nil
		}
		return r
	}
	return v
}
func category(name string) string {
	for _, s := range []string{"shell", "terminal", "background_job"} {
		if strings.Contains(name, s) {
			return "shell"
		}
	}
	for _, s := range []string{"file", "directory", "search", "grep"} {
		if strings.Contains(name, s) {
			return "filesystem"
		}
	}
	if strings.Contains(name, "todo") {
		return "productivity"
	}
	return "generic"
}
func resource(task M, path string) M {
	root := S(task["workspace"])
	if root == "" {
		return nil
	}
	absolute := path
	if !filepath.IsAbs(path) {
		absolute = filepath.Join(root, path)
	}
	rel, err := filepath.Rel(root, absolute)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return nil
	}
	return M{"id": S(task["id"]) + ":file:" + rel, "kind": "file", "label": rel, "uri": "/api/tasks/" + S(task["id"]) + "/files?path=" + url.QueryEscape(rel)}
}
func (n *normalizer) append(task M, role string, blocks []any, live bool) (M, error) {
	turn := S(task["turnId"])
	if turn == "" {
		turn = S(task["id"]) + ":turn-0"
	}
	step := S(task["stepId"])
	if step == "" {
		step = S(task["id"]) + ":step-0"
	}
	return n.m.appendLocked(task, M{"turnId": turn, "stepId": step, "role": role, "blocks": blocks, "live": live})
}
func nativeCallKey(root M, event M) string {
	id := firstString(event["tool_call_id"], O(event["tool_call"])["id"])
	// Native tool events carry agent_name but need not carry session_id.
	// Provider call IDs are only unique within their execution, not globally.
	return S(root["id"]) + "\x00" + S(event["agent_name"]) + "\x00" + id
}
func (n *normalizer) owner(root, event M) M {
	session := S(event["session_id"])
	if session != "" {
		for _, task := range n.m.tasks {
			if S(task["nativeSessionId"]) == session {
				return task
			}
		}
		return root
	}
	if r := n.calls[nativeCallKey(root, event)]; r != nil {
		if task := n.m.tasks[S(r["taskId"])]; task != nil {
			return task
		}
	}
	var candidate M
	for _, child := range n.children {
		if child["rootId"] == root["id"] && child["agent"] == event["agent_name"] {
			if candidate != nil {
				return root
			}
			candidate = n.m.tasks[S(child["id"])]
		}
	}
	if candidate != nil {
		return candidate
	}
	return root
}
func (n *normalizer) assistant(task, event M) (M, error) {
	messageID := S(event["message_id"])
	if messageID == "" {
		messageID = S(task["activeMessageKey"])
	}
	if messageID == "" {
		messageID = UUID()
	}
	task["activeMessageKey"] = messageID
	key := S(task["id"]) + ":" + messageID
	index, ok := n.assistants[key]
	if !ok {
		task["stepCount"] = I(task["stepCount"]) + 1
		task["stepId"] = fmt.Sprintf("%s:step-%d", S(task["turnId"]), I(task["stepCount"]))
		msg, err := n.append(task, "assistant", []any{newBlock(key+":reasoning", "reasoning", M{"text": "", "status": "streaming"}), newBlock(key+":answer", "markdown", M{"markdown": ""})}, true)
		if err != nil {
			return nil, err
		}
		index = I(msg["index"])
		n.assistants[key] = index
	}
	if task["activeAssistantIndex"] == nil || I(task["activeAssistantIndex"]) != index {
		if err := n.m.stateLocked(task, M{"activeAssistantIndex": index}); err != nil {
			return nil, err
		}
	}
	return n.m.options.Store.Message(S(task["id"]), index)
}

// File observations happen outside the manager lock. Engine identity/events remain authoritative.
func (m *Manager) HandleEvent(rootID string, event M) error {
	event = Clone(event)
	var observation *string
	kind := S(event["type"])
	if kind == "tool_call" || kind == "tool_call_confirmation" || kind == "tool_call_response" {
		m.mu.Lock()
		root := m.tasks[rootID]
		if root == nil {
			m.mu.Unlock()
			return Problem(404, "task_not_found", "Task not found")
		}
		task := Clone(m.normalizer.owner(root, event))
		call := O(event["tool_call"])
		name := S(O(call["function"])["name"])
		input := O(parseValue(O(call["function"])["arguments"]))
		if kind == "tool_call_response" {
			if record := m.normalizer.calls[nativeCallKey(root, event)]; record != nil {
				name = S(record["name"])
				input = Clone(O(record["input"]))
			}
		}
		m.mu.Unlock()
		if (strings.Contains(name, "write_file") || strings.Contains(name, "edit_file")) && S(input["path"]) != "" {
			observation = readObservedText(task, S(input["path"]))
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	root, err := m.getLocked(rootID)
	if err != nil {
		return err
	}
	return m.normalizer.handle(root, event, observation)
}
func readObservedText(task M, path string) *string {
	root, err := filepath.EvalSymlinks(S(task["workspace"]))
	if err != nil {
		return nil
	}
	target := path
	if !filepath.IsAbs(target) {
		target = filepath.Join(root, target)
	}
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil
	}
	handle, err := os.OpenRoot(root)
	if err != nil {
		return nil
	}
	defer handle.Close()
	f, err := handle.Open(rel)
	if err != nil {
		return nil
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 256*1024 {
		return nil
	}
	raw, err := io.ReadAll(io.LimitReader(f, 256*1024+1))
	if err != nil || len(raw) > 256*1024 {
		return nil
	}
	if strings.ContainsRune(string(raw), 0) {
		return nil
	}
	text := string(raw)
	return &text
}
func (n *normalizer) handle(root, event M, observed *string) error {
	m := n.m
	kind := S(event["type"])
	if kind == "stream_started" && S(event["session_id"]) != "" && event["session_id"] != root["nativeSessionId"] {
		child, err := m.createChildLocked(root, S(event["session_id"]), firstString(event["agent_name"], "worker"), "foreground")
		if err != nil {
			return err
		}
		n.children[S(child["id"])] = M{"id": child["id"], "rootId": root["id"], "agent": child["agent"]}
		if err = m.stateLocked(child, M{"status": "working", "lastTurnReason": nil}); err != nil {
			return err
		}
		if err = n.delegation(root); err != nil {
			return err
		}
	}
	task := n.owner(root, event)
	if kind == "user_message" {
		if err := m.confirmSteeringLocked(task, event); err != nil {
			return err
		}
	}
	// Parameter fragments and other progress may have no visible projection.
	// Keep the last observed turn time for crash recovery, without retaining the
	// raw stream. Clone only timing, and expose it in memory after the commit.
	timing, at := O(root["turnTiming"]), NowMS()
	if len(timing) > 0 && timing["endedAt"] == nil && !B(timing["incomplete"]) && float64(at) > N(timing["observedAt"]) {
		checkpoint := Merge(root, M{"turnTiming": Clone(timing)})
		ObserveTurnTiming(checkpoint, at)
		if err := m.options.Store.SaveTask(checkpoint); err != nil {
			return err
		}
		root["turnTiming"] = checkpoint["turnTiming"]
	}
	// A terminal event must identify its actual session. Unknown or missing
	// identities cannot establish that the root Agent turn has ended.
	if kind == "stream_stopped" && (S(event["session_id"]) == "" || event["session_id"] != task["nativeSessionId"] || task["id"] != root["id"] && task["parentTaskId"] != root["id"]) {
		return nil
	}
	if err := n.observeActivity(task, event); err != nil {
		return err
	}
	if timings := n.timings.observe(task, event); timings != nil {
		if err := m.stateLocked(task, M{"timings": timings}); err != nil {
			return err
		}
	}
	switch kind {
	case "session_compaction":
		if S(event["session_id"]) != "" && event["session_id"] != task["nativeSessionId"] {
			break
		}
		at := S(event["timestamp"])
		if _, err := time.Parse(time.RFC3339Nano, at); err != nil {
			at = NowISO()
		}
		if S(event["status"]) == "started" {
			var before any
			if B(O(task["context"])["reported"]) {
				before = O(task["context"])["projectedTokens"]
			}
			return m.stateLocked(task, M{"compaction": M{"status": "running", "startedAt": at, "beforeTokens": before}})
		}
		if S(event["status"]) == "completed" {
			outcome := S(event["outcome"])
			if outcome != "applied" && outcome != "skipped" && outcome != "failed" {
				outcome = "unknown"
				if B(O(task["compaction"])["summaryReceived"]) {
					outcome = "applied"
				}
			}
			return m.stateLocked(task, M{"compaction": Merge(O(task["compaction"]), M{"status": outcome, "completedAt": at})})
		}
	case "session_summary":
		if (S(event["session_id"]) == "" || event["session_id"] == task["nativeSessionId"]) && S(O(task["compaction"])["status"]) == "running" {
			return m.stateLocked(task, M{"compaction": Merge(O(task["compaction"]), M{"summaryReceived": true})})
		}
	case "stream_started":
		return m.stateLocked(task, M{"status": "working", "lastTurnReason": nil})
	case "user_message":
		if S(event["message"]) != "" && task["id"] != root["id"] {
			_, err := n.append(task, "user", []any{newBlock("user:"+UUID(), "markdown", M{"markdown": event["message"]})}, false)
			return err
		}
	case "agent_choice", "agent_choice_reasoning":
		msg, err := n.assistant(task, event)
		if err != nil {
			return err
		}
		blockType, field := "markdown", "markdown"
		if kind == "agent_choice_reasoning" {
			blockType = "reasoning"
			field = "text"
		}
		for _, v := range A(msg["blocks"]) {
			b := O(v)
			if S(b["type"]) == blockType {
				return m.appendTextLocked(task, msg, S(b["id"]), field, S(event["content"]))
			}
		}
	case "message_added":
		return n.finishAssistant(task, "completed")
	case "tool_call", "tool_call_confirmation":
		call := O(event["tool_call"])
		id := S(call["id"])
		if id == "" {
			break
		}
		name := firstString(O(call["function"])["name"], call["name"], O(event["tool_definition"])["name"], "tool")
		args := O(call["function"])["arguments"]
		if args == nil {
			args = call["arguments"]
		}
		input := O(parseValue(args))
		key := nativeCallKey(root, event)
		record := n.calls[key]
		if record == nil {
			resources := []any{}
			paths := []any{input["path"], input["file_path"]}
			paths = append(paths, A(input["paths"])...)
			for _, p := range paths {
				if _, ok := p.(string); ok {
					if r := resource(task, S(p)); r != nil {
						resources = append(resources, r)
					}
				}
			}
			presentation := M{"kind": "generic"}
			if name == inspectionTool && m.options.InspectionPresentation != nil {
				presentation = m.options.InspectionPresentation(root, input)
			}
			if category(name) == "shell" {
				presentation = M{"kind": "terminal", "command": firstString(input["cmd"], input["command"]), "cwd": resource(task, ".")}
			} else if len(resources) > 0 {
				p := "resources"
				if strings.Contains(name, "write") || strings.Contains(name, "edit") {
					p = "changes"
				}
				presentation = M{"kind": p, "resources": resources}
			}
			data := M{"name": name, "callId": id, "input": input, "category": category(name), "presentation": presentation, "resources": resources}
			msg, err := n.append(task, "assistant", []any{newBlock("call:"+id, "tool-call", data)}, true)
			if err != nil {
				return err
			}
			record = M{"taskId": task["id"], "index": msg["index"], "name": name, "input": input, "startedAt": NowMS()}
			if observed != nil {
				record["before"] = *observed
			}
			n.calls[key] = record
			if err = m.stateLocked(task, M{"activeAssistantIndex": msg["index"]}); err != nil {
				return err
			}
		}
		if kind == "tool_call_confirmation" {
			return m.requestInteractionLocked(root, M{"id": ID(), "kind": "approval", "title": "允许 " + name + "？", "detail": string(JSON(input)), "toolName": name, "callId": id}, M{"kind": "approval", "targetId": root["nativeSessionId"]})
		}
		record["startedAt"] = NowMS()
	case "tool_call_output":
		id := S(event["tool_call_id"])
		r := n.calls[nativeCallKey(root, event)]
		if r != nil && category(S(r["name"])) == "shell" {
			owner := m.tasks[S(r["taskId"])]
			msg, err := m.options.Store.Message(S(owner["id"]), I(r["index"]))
			if err != nil {
				return err
			}
			terminal := findBlock(msg, "terminal")
			if terminal == nil {
				terminal = newBlock("terminal:"+id, "terminal", M{"callId": id, "command": firstString(O(r["input"])["cmd"], O(r["input"])["command"]), "cwd": resource(owner, "."), "output": "", "status": "running"})
				msg["blocks"] = append(A(msg["blocks"]), terminal)
				if err = m.replaceLocked(owner, msg); err != nil {
					return err
				}
			}
			return m.appendTextLocked(owner, msg, S(terminal["id"]), "output", S(event["output"]))
		}
	case "tool_call_response":
		return n.toolResponse(root, task, event, observed)
	case "max_iterations_reached":
		detail := "继续可再执行最多 10 步。"
		if I(event["max_iterations"]) > 0 {
			detail = fmt.Sprintf("已执行 %d 步。", I(event["max_iterations"])) + detail
		}
		return m.requestInteractionLocked(root, M{"id": ID(), "kind": "approval", "purpose": "iterations", "title": "已达本轮执行步数上限", "detail": detail}, M{"kind": "approval", "targetId": root["nativeSessionId"]})
	case "elicitation_request":
		return m.requestInteractionLocked(root, M{"id": event["elicitation_id"], "kind": "question", "title": firstString(O(event["meta"])["cagent/title"], "需要你回答"), "detail": firstString(event["message"]), "schema": QuestionSchema(event["schema"])}, M{"kind": "question", "targetId": root["nativeSessionId"], "schema": event["schema"]})
	case "token_usage":
		usage := event
		if event["usage"] != nil {
			usage = O(event["usage"])
		}
		patch := M{"usage": AccumulateUsage(O(task["usage"]), usage), "modelCalls": I(task["modelCalls"]), "context": M{"projectedTokens": N(usage["context_length"]), "contextWindow": N(usage["context_limit"]), "reported": Has(usage, "context_length")}, "cost": N(usage["cost"])}
		if usage["last_message"] != nil && !B(usage["snapshot_only"]) {
			patch["usageSource"] = "live"
			patch["modelCalls"] = I(task["modelCalls"]) + 1
		}
		return m.stateLocked(task, patch)
	case "error":
		return m.stateLocked(task, M{"lastFailure": M{"code": firstString(event["code"], "runtime_error"), "message": firstString(event["error"], event["message"], "Agent execution failed")}})
	case "stream_stopped":
		FinishTurnTiming(task, true, NowMS())
		reason := "interrupted"
		switch S(event["reason"]) {
		case "normal":
			reason = "completed"
		case "error":
			reason = "error"
		}
		if reason == "completed" && task["lastFailure"] != nil {
			reason = "error"
		}
		if reason == "completed" && S(event["finish_reason"]) == "length" {
			reason = "max-tokens"
		}
		if err := n.endCompaction(task, reason); err != nil {
			return err
		}
		if err := n.finishAssistant(task, reason); err != nil {
			return err
		}
		if task["id"] != root["id"] {
			if err := m.stateLocked(task, M{"status": "idle", "lastTurnReason": reason, "activeAssistantIndex": nil, "activity": nil}); err != nil {
				return err
			}
			delete(n.children, S(task["id"]))
			return n.delegation(root)
		}
		root["nativeStopped"] = reason
	case "sub_session_completed":
		native := O(event["sub_session"])
		if S(native["id"]) == "" {
			break
		}
		child, err := m.createChildLocked(root, S(native["id"]), firstString(event["agent_name"], "worker"), "foreground")
		if err != nil {
			return err
		}
		reason := "completed"
		if child["lastFailure"] != nil {
			reason = "error"
		}
		if err = n.endCompaction(child, reason); err != nil {
			return err
		}
		delete(n.activityTools, S(child["id"]))
		if I(child["logicalCount"]) == 0 {
			if err = n.importHistory(child, A(native["messages"])); err != nil {
				return err
			}
		}
		if S(child["status"]) != "idle" {
			if err = m.stateLocked(child, M{"status": "idle", "lastTurnReason": reason, "activeAssistantIndex": nil, "activity": nil}); err != nil {
				return err
			}
		}
		delete(n.children, S(child["id"]))
		return n.delegation(root)
	}
	return nil
}
func firstString(values ...any) string {
	for _, v := range values {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return ""
}
func findBlock(msg M, kind string) M {
	for _, v := range A(msg["blocks"]) {
		if S(O(v)["type"]) == kind {
			return O(v)
		}
	}
	return nil
}
func (n *normalizer) toolResponse(root, task, event M, observed *string) error {
	m := n.m
	id := S(event["tool_call_id"])
	record := n.calls[nativeCallKey(root, event)]
	name := firstString(record["name"], O(event["tool_definition"])["name"], "tool")
	result := O(event["result"])
	failed := B(result["isError"]) || B(result["is_error"])
	status := "success"
	if failed {
		status = "error"
	}
	output := event["response"]
	if output == nil {
		output = event["result"]
	}
	text := S(output)
	if text == "" {
		text = S(O(output)["output"])
	}
	if !failed && name == "run_background_agent" {
		if err := n.backgroundRun(root, O(record["input"]), text); err != nil {
			return err
		}
	}
	if !failed || name == "wait_background_agents" {
		if err := n.backgroundStatus(root, name, text); err != nil {
			return err
		}
	}
	if record == nil {
		_, err := n.append(task, "tool", []any{newBlock("result:"+id, "tool-result", M{"name": name, "callId": id, "output": output, "status": status})}, false)
		return err
	}
	// A later provider response may reuse this ID. Keep the record only
	// while its call is in flight, including confirmation and output events.
	defer delete(n.calls, nativeCallKey(root, event))
	owner := m.tasks[S(record["taskId"])]
	msg, err := m.options.Store.Message(S(owner["id"]), I(record["index"]))
	if err != nil {
		return err
	}
	msg["live"] = false
	call := findBlock(msg, "tool-call")
	data := O(call["data"])
	data["status"] = status
	data["durationMs"] = float64(NowMS()) - N(record["startedAt"])
	terminal := findBlock(msg, "terminal")
	if terminal == nil && category(name) == "shell" {
		terminal = newBlock("terminal:"+id, "terminal", M{"callId": id, "command": firstString(O(record["input"])["cmd"], O(record["input"])["command"]), "cwd": resource(owner, "."), "output": "", "status": status})
		msg["blocks"] = append(A(msg["blocks"]), terminal)
	}
	if terminal != nil {
		d := O(terminal["data"])
		if S(d["output"]) == "" {
			if s, ok := output.(string); ok {
				d["output"] = s
			} else {
				d["output"] = string(JSON(output))
			}
		}
		d["status"] = status
	}
	if err = m.replaceLocked(owner, msg); err != nil {
		return err
	}
	out := M{"name": name, "callId": id, "output": output, "category": category(name), "resources": data["resources"], "presentation": data["presentation"], "status": status}
	if name == "deliver_result" {
		out["deliverySummary"] = O(record["input"])["summary"]
	}
	if _, err = n.append(owner, "tool", []any{newBlock("result:"+id, "tool-result", out)}, false); err != nil {
		return err
	}
	if !failed && strings.Contains(name, "todo") {
		if err = n.plan(owner, name, O(record["input"]), output); err != nil {
			return err
		}
	}
	media := []any{}
	for _, spec := range []struct{ field, kind string }{{"images", "image"}, {"audios", "audio"}, {"documents", "file"}} {
		for i, v := range A(result[spec.field]) {
			item := O(v)
			mime := S(item["mimeType"])
			src := S(item["uri"])
			if S(item["data"]) != "" {
				src = "data:" + mime + ";base64," + S(item["data"])
			} else if S(item["text"]) != "" {
				src = "data:text/plain;charset=utf-8," + url.PathEscape(S(item["text"]))
			}
			media = append(media, M{"id": fmt.Sprintf("%s:media-%d", id, len(media)), "name": firstString(item["name"], fmt.Sprintf("%s-%d", spec.kind, i+1)), "kind": spec.kind, "mimeType": mime, "src": src})
		}
	}
	if len(media) > 0 {
		provenance := M{"origin": "tool-output", "toolName": name, "toolCallId": id}
		if name == "generate_images" && !failed {
			provenance["prompt"] = O(record["input"])["prompt"]
		}
		if _, err = n.append(owner, "tool", []any{newBlock("media:"+id, "attachments", M{"title": "工具返回的实际产物", "provenance": provenance, "items": media})}, false); err != nil {
			return err
		}
	}
	if !failed && observed != nil && (strings.Contains(name, "write_file") || strings.Contains(name, "edit_file")) {
		path := S(O(record["input"])["path"])
		r := resource(owner, path)
		if r != nil {
			lines := []any{"--- " + path + " (snapshot on proposal)", "+++ " + path + " (observed after tool result)", "@@ observed file snapshots @@"}
			if before, ok := record["before"].(string); ok {
				for _, l := range strings.Split(before, "\n") {
					lines = append(lines, "-"+l)
				}
			}
			for _, l := range strings.Split(*observed, "\n") {
				lines = append(lines, "+"+l)
			}
			blocks := []any{newBlock("diff:"+id, "diff", M{"resource": r, "lines": lines}), newBlock("code:"+id, "code", M{"filename": path, "code": *observed, "language": strings.TrimPrefix(filepath.Ext(path), "."), "resource": r})}
			if strings.EqualFold(filepath.Ext(path), ".html") || strings.EqualFold(filepath.Ext(path), ".htm") {
				blocks = append(blocks, newBlock("html:"+id, "html", M{"html": *observed}))
			}
			_, err = n.append(owner, "assistant", blocks, false)
			return err
		}
	}
	return nil
}
func (n *normalizer) activity(task M, phase, detail string, after bool) error {
	old := O(task["activity"])
	if S(old["phase"]) == phase && S(old["detail"]) == detail && B(old["afterCompaction"]) == after {
		return nil
	}
	return n.m.stateLocked(task, M{"activity": M{"phase": phase, "detail": detail, "afterCompaction": after, "startedAt": NowMS()}})
}
func (n *normalizer) processing(task M, after bool) error {
	tools := n.activityTools[S(task["id"])]
	if len(tools) > 0 {
		detail := fmt.Sprintf("%d 个工具", len(tools))
		if len(tools) == 1 {
			for _, v := range tools {
				detail = v
			}
		}
		return n.activity(task, "tool", detail, false)
	}
	return n.activity(task, "processing", "", after)
}
func (n *normalizer) observeActivity(task, event M) error {
	if S(event["session_id"]) != "" && event["session_id"] != task["nativeSessionId"] {
		return nil
	}
	switch S(event["type"]) {
	case "stream_started":
		return n.activity(task, "processing", "", false)
	case "mcp_init_started":
		return n.activity(task, "preparing", "正在连接工具", false)
	case "mcp_init_finished":
		return n.processing(task, false)
	case "session_compaction":
		if S(event["status"]) == "started" {
			return n.activity(task, "compacting", "", false)
		}
		if S(event["status"]) == "completed" {
			return n.processing(task, S(event["outcome"]) == "applied" || S(event["outcome"]) == "" && B(O(task["compaction"])["summaryReceived"]))
		}
	case "agent_choice":
		if S(event["content"]) != "" {
			return n.activity(task, "responding", "", false)
		}
	case "agent_choice_reasoning":
		if S(event["content"]) != "" {
			return n.activity(task, "reasoning", "", false)
		}
	case "partial_tool_call":
		return n.processing(task, false)
	case "message_added":
		if phase := S(O(task["activity"])["phase"]); phase == "responding" || phase == "reasoning" {
			return n.processing(task, false)
		}
	case "tool_call":
		call := O(event["tool_call"])
		if S(call["id"]) != "" {
			id := S(task["id"])
			if n.activityTools[id] == nil {
				n.activityTools[id] = map[string]string{}
			}
			n.activityTools[id][S(call["id"])] = firstString(O(call["function"])["name"], call["name"], O(event["tool_definition"])["name"], "工具")
			return n.processing(task, false)
		}
	case "tool_call_response":
		delete(n.activityTools[S(task["id"])], S(event["tool_call_id"]))
		return n.processing(task, false)
	case "stream_stopped":
		delete(n.activityTools, S(task["id"]))
		return n.activity(task, "finishing", "", false)
	}
	return nil
}
func (n *normalizer) endCompaction(task M, reason string) error {
	if S(O(task["compaction"])["status"]) != "running" {
		return nil
	}
	status := "interrupted"
	if reason == "error" {
		status = "failed"
	} else if reason == "completed" {
		status = "unknown"
	}
	return n.m.stateLocked(task, M{"compaction": Merge(O(task["compaction"]), M{"status": status, "completedAt": NowISO()})})
}

var markdownLink = regexp.MustCompile(`\]\(([^)]+)\)`)

func (n *normalizer) finishAssistant(task M, reason string) error {
	if task["activeAssistantIndex"] == nil {
		return nil
	}
	msg, err := n.m.options.Store.Message(S(task["id"]), I(task["activeAssistantIndex"]))
	if err != nil {
		return err
	}
	if msg != nil {
		msg["live"] = false
		for _, v := range A(msg["blocks"]) {
			b := O(v)
			data := O(b["data"])
			if S(b["type"]) == "reasoning" {
				data["status"] = "interrupted"
				if reason == "completed" {
					data["status"] = "complete"
				}
			}
			if S(b["type"]) == "markdown" {
				data["markdown"] = markdownLink.ReplaceAllStringFunc(S(data["markdown"]), func(s string) string {
					link := s[2 : len(s)-1]
					for _, prefix := range []string{"http:", "https:", "data:", "/", "#", "mailto:"} {
						if strings.HasPrefix(strings.ToLower(link), prefix) {
							return s
						}
					}
					if r := resource(task, link); r != nil {
						return "](" + S(r["uri"]) + ")"
					}
					return s
				})
			}
		}
		if err = n.m.replaceLocked(task, msg); err != nil {
			return err
		}
	}
	return n.m.stateLocked(task, M{"activeAssistantIndex": nil, "activeMessageKey": nil})
}
func (n *normalizer) plan(task M, name string, input M, output any) error {
	todos := n.todos[S(task["id"])]
	decoded := parseValue(output)
	object := O(decoded)
	switch {
	case object["all_todos"] != nil:
		todos = A(object["all_todos"])
	case object["todos"] != nil:
		todos = A(object["todos"])
	case strings.Contains(name, "list_todos"):
		if list, ok := decoded.([]any); ok {
			todos = list
		}
	case strings.Contains(name, "create_todos"):
		if list, ok := decoded.([]any); ok {
			todos = append(todos, list...)
		} else {
			todos = append(todos, A(input["todos"])...)
		}
	case strings.Contains(name, "create_todo"):
		if len(object) > 0 {
			todos = append(todos, object)
		} else {
			todos = append(todos, Merge(M{"id": UUID()}, input))
		}
	case strings.Contains(name, "update_todos"):
		updates := A(input["todos"])
		if input["todos"] == nil {
			updates = A(input["updates"])
		}
		for _, v := range updates {
			update := O(v)
			for _, t := range todos {
				todo := O(t)
				if todo["id"] == update["id"] {
					for k, x := range update {
						todo[k] = x
					}
				}
			}
		}
	}
	n.todos[S(task["id"])] = todos
	items := []any{}
	for i, v := range todos {
		todo := O(v)
		status := S(todo["status"])
		if status != "pending" && status != "in-progress" && status != "completed" && status != "blocked" && status != "cancelled" {
			status = "pending"
		}
		id := S(todo["id"])
		if id == "" {
			id = fmt.Sprint(i)
		}
		items = append(items, M{"id": id, "text": firstString(todo["description"], todo["title"], todo["text"]), "status": status})
	}
	plan := M{"title": "任务计划", "items": items}
	if err := n.m.stateLocked(task, M{"activePlan": plan}); err != nil {
		return err
	}
	_, err := n.append(task, "assistant", []any{newBlock("plan:"+UUID(), "plan", plan)}, false)
	return err
}
func (n *normalizer) delegation(root M) error {
	runs := []any{}
	for _, child := range n.m.tasks {
		if child["parentTaskId"] != root["id"] {
			continue
		}
		status := "queued"
		switch S(child["status"]) {
		case "working":
			status = "running"
		case "waiting":
			status = "waiting"
		default:
			switch S(child["lastTurnReason"]) {
			case "error", "max-tokens":
				status = "failed"
			case "interrupted":
				status = "interrupted"
			case "completed":
				status = "completed"
			}
		}
		run := M{"runId": child["id"], "childSessionId": child["id"], "title": child["title"], "agent": child["agent"], "mode": firstString(child["mode"], "foreground"), "status": status}
		if child["lastFailure"] != nil {
			run["summary"] = O(child["lastFailure"])["message"]
		} else if S(child["lastTurnReason"]) == "max-tokens" {
			run["summary"] = "已达输出上限"
		}
		runs = append(runs, run)
	}
	if root["delegationIndex"] == nil {
		msg, err := n.append(root, "assistant", []any{newBlock("delegation:"+S(root["turnId"]), "delegation", M{"title": "委派任务", "runs": runs})}, false)
		if err == nil {
			root["delegationIndex"] = msg["index"]
		}
		return err
	}
	msg, err := n.m.options.Store.Message(S(root["id"]), I(root["delegationIndex"]))
	if err != nil {
		return err
	}
	O(O(A(msg["blocks"])[0])["data"])["runs"] = runs
	return n.m.replaceLocked(root, msg)
}
func historyTools(messages []any) map[string]M {
	tools := map[string]M{}
	for _, v := range messages {
		w := O(v)
		if w["message"] != nil {
			w = O(w["message"])
		}
		if w["message"] != nil {
			w = O(w["message"])
		}
		for _, x := range A(w["tool_calls"]) {
			call := O(x)
			id := S(call["id"])
			meta := Merge(tools[id], M{"name": O(call["function"])["name"]})
			if S(meta["name"]) == "deliver_result" {
				meta["input"] = parseValue(O(call["function"])["arguments"])
			}
			tools[id] = meta
		}
		if S(w["role"]) == "tool" {
			status := "success"
			if B(w["is_error"]) {
				status = "error"
			}
			id := S(w["tool_call_id"])
			tools[id] = Merge(tools[id], M{"status": status})
		}
	}
	return tools
}
func (n *normalizer) restoreToolMetadata(task M, nativeMessages []any) error {
	tools := historyTools(nativeMessages)
	messages, err := n.m.options.Store.Messages(S(task["id"]), 0, I(task["logicalCount"]))
	if err != nil {
		return err
	}
	for _, message := range messages {
		changed := false
		for _, value := range A(message["blocks"]) {
			block := O(value)
			if S(block["type"]) != "tool-call" && S(block["type"]) != "tool-result" {
				continue
			}
			data := O(block["data"])
			meta := tools[S(data["callId"])]
			if meta == nil {
				continue
			}
			if S(block["type"]) == "tool-result" && S(meta["name"]) == "deliver_result" {
				if _, ok := data["deliverySummary"].(string); !ok {
					if summary, ok := O(meta["input"])["summary"].(string); ok {
						data["deliverySummary"] = summary
						changed = true
					}
				}
			}
			if S(data["status"]) == "" && S(meta["status"]) != "" {
				data["status"] = meta["status"]
				changed = true
			}
			if S(data["name"]) == "tool" && S(meta["name"]) != "" {
				data["name"] = meta["name"]
				changed = true
			}
		}
		if changed {
			if err := n.m.replaceLocked(task, message); err != nil {
				return err
			}
		}
	}
	return nil
}

func (n *normalizer) importHistory(task M, messages []any) error {
	tools := historyTools(messages)
	presentations := map[string]M{}
	if parent := S(O(task["forkedFrom"])["id"]); parent != "" {
		var err error
		presentations, err = n.m.options.Store.InspectionPresentations(parent)
		if err != nil {
			return err
		}
	}
	// Call IDs may be reused in later rounds. Associate media with the
	// preceding call's input, rather than the last occurrence in the history.
	calls := map[string]M{}
	for _, v := range messages {
		w := O(v)
		if w["message"] != nil {
			w = O(w["message"])
		}
		msg := w
		if w["message"] != nil {
			msg = O(w["message"])
		}
		role := S(msg["role"])
		if (role != "user" && role != "assistant" && role != "tool") || B(w["implicit"]) {
			continue
		}
		blocks := []any{}
		if S(msg["reasoning_content"]) != "" {
			blocks = append(blocks, newBlock("import:"+UUID(), "reasoning", M{"text": msg["reasoning_content"], "status": "complete"}))
		}
		if S(msg["content"]) != "" && role != "tool" {
			blocks = append(blocks, newBlock("import:"+UUID(), "markdown", M{"markdown": msg["content"]}))
		}
		for _, v := range A(msg["tool_calls"]) {
			c := O(v)
			f := O(c["function"])
			calls[S(c["id"])] = M{"name": f["name"], "input": parseValue(f["arguments"])}
			data := M{"name": f["name"], "callId": c["id"], "input": parseValue(f["arguments"]), "category": category(S(f["name"])), "status": tools[S(c["id"])]["status"]}
			if presentation := presentations[inspectionCallKey(data)]; presentation != nil {
				data["presentation"] = Clone(presentation)
			}
			blocks = append(blocks, newBlock("call:"+S(c["id"]), "tool-call", data))
		}
		if role == "tool" {
			id := S(msg["tool_call_id"])
			meta := tools[id]
			status := "success"
			if B(msg["is_error"]) {
				status = "error"
			}
			data := M{"name": firstString(msg["name"], meta["name"], "tool"), "callId": id, "output": msg["content"], "status": status}
			if S(meta["name"]) == "deliver_result" {
				data["deliverySummary"] = O(meta["input"])["summary"]
			}
			blocks = append(blocks, newBlock("result:"+id, "tool-result", data))
			// The public session API retains MCP images in multi_content.
			// Import them without consulting engine storage or fetching URLs.
			media := []any{}
			for _, part := range A(msg["multi_content"]) {
				p := O(part)
				src := S(O(p["image_url"])["url"])
				if S(p["type"]) != "image_url" || src == "" {
					continue
				}
				mime := ""
				if strings.HasPrefix(src, "data:") {
					mime = strings.SplitN(strings.SplitN(src[5:], ",", 2)[0], ";", 2)[0]
				}
				media = append(media, M{"id": fmt.Sprintf("%s:media-%d", id, len(media)), "name": fmt.Sprintf("image-%d", len(media)+1), "kind": "image", "mimeType": mime, "src": src})
			}
			if len(media) > 0 {
				call := calls[id]
				name := firstString(call["name"], data["name"])
				provenance := M{"origin": "tool-output", "toolName": name, "toolCallId": id}
				if name == "generate_images" && status == "success" {
					provenance["prompt"] = O(call["input"])["prompt"]
				}
				blocks = append(blocks, newBlock("media:"+id, "attachments", M{"title": "工具返回的实际产物", "provenance": provenance, "items": media}))
			}
			delete(calls, id)
		}
		if len(blocks) > 0 {
			if _, err := n.append(task, role, blocks, false); err != nil {
				return err
			}
		}
	}
	return nil
}

var backgroundStarted = regexp.MustCompile(`^Background agent task started with ID: (\S+)`)
var backgroundMetadata = regexp.MustCompile(`^(?:Task )?ID: (\S+)\n\s*Agent: +[^\n]*\n\s*Status: +(running|completed|failed|stopped)\s*(?:\n|$)`)
var backgroundStopped = regexp.MustCompile(`^Background agent task (\S+) stopped\.$`)

func (n *normalizer) backgroundRun(root, input M, output string) error {
	match := backgroundStarted.FindStringSubmatch(output)
	if match == nil {
		return nil
	}
	for _, v := range A(root["backgroundRuns"]) {
		if S(O(v)["id"]) == match[1] {
			return nil
		}
	}
	title := []rune(S(input["task"]))
	if len(title) > 160 {
		title = title[:160]
	}
	root["backgroundRuns"] = append(A(root["backgroundRuns"]), M{"id": match[1], "turnId": root["turnId"], "agent": firstString(input["agent"], "worker"), "title": string(title)})
	return n.m.publishLocked(root, M{"type": "state", "task": root}, nil)
}
func (n *normalizer) backgroundStatus(root M, name, output string) error {
	reports := []any{}
	switch name {
	case "wait_background_agents":
		reports = A(O(parseValue(output))["tasks"])
	case "list_background_agents", "view_background_agent":
		parts := strings.Split(output, "\n--- Output ---\n")
		for _, section := range regexp.MustCompile(`\n\s*\n`).Split(parts[0], -1) {
			match := backgroundMetadata.FindStringSubmatch(section)
			if match == nil {
				continue
			}
			report := M{"task_id": match[1], "status": match[2]}
			if name == "view_background_agent" && len(parts) > 1 {
				report["output"] = strings.Join(parts[1:], "\n--- Output ---\n")
			}
			reports = append(reports, report)
		}
	case "stop_background_agent":
		if match := backgroundStopped.FindStringSubmatch(output); match != nil {
			reports = append(reports, M{"task_id": match[1], "status": "stopped"})
		}
	}
	changed := false
	for _, v := range reports {
		report := O(v)
		status := S(report["status"])
		if status != "running" && status != "completed" && status != "failed" && status != "stopped" {
			continue
		}
		for _, v := range A(root["backgroundRuns"]) {
			run := O(v)
			if run["id"] != report["task_id"] {
				continue
			}
			if old := S(run["status"]); old != "" && old != "running" && status == "running" {
				continue
			}
			run["turnId"] = root["turnId"]
			run["status"] = status
			run["observedAt"] = NowISO()
			text := strings.Join(strings.Fields(firstString(report["error"], report["output"])), " ")
			if text != "" {
				r := []rune(text)
				if len(r) > 240 {
					r = r[len(r)-240:]
				}
				run["summary"] = string(r)
			}
			changed = true
		}
	}
	if changed {
		return n.m.publishLocked(root, M{"type": "state", "task": root}, nil)
	}
	return nil
}

// WorkbenchMessage removes inline media bytes from the browser's hot history.
func WorkbenchMessage(taskID string, message M) M {
	out := Clone(message)
	for _, v := range A(out["blocks"]) {
		b := O(v)
		data := O(b["data"])
		if S(b["type"]) == "tool-call" && S(data["name"]) == inspectionTool {
			items := []any{}
			for i, value := range A(O(data["input"])["attachments"]) {
				if i >= 4 {
					break
				}
				ref, e := parseToolFile(value, false)
				if e == nil {
					src := fmt.Sprintf("/api/tasks/%s/files?inspection=1&path=%s", url.PathEscape(taskID), url.QueryEscape(ref.Path))
					if ref.Source == "runcove" {
						if !Contains([]string{"success", "error"}, S(data["status"])) {
							continue
						}
						src += fmt.Sprintf("&source=runcove&message=%d&item=%d&revision=%d&block=%s", I(message["index"]), i, I(message["revision"]), url.QueryEscape(S(b["id"])))
					}
					items = append(items, M{"id": fmt.Sprint(i), "name": filepath.Base(ref.Path), "kind": "image", "mimeType": "", "src": src})
				}
			}
			data["inputAttachments"] = items
		}
		if S(b["type"]) != "attachments" {
			continue
		}
		for _, v := range A(O(b["data"])["items"]) {
			item := O(v)
			if strings.HasPrefix(S(item["src"]), "data:") {
				item["src"] = fmt.Sprintf("/api/tasks/%s/message-media?index=%d&block=%s&item=%s&revision=%d", url.PathEscape(taskID), I(out["index"]), url.QueryEscape(S(b["id"])), url.QueryEscape(S(item["id"])), I(out["revision"]))
			}
		}
	}
	return out
}
func WorkbenchEvent(event M) M {
	out := Clone(event)
	if S(out["type"]) == "native" {
		delete(out, "event")
	}
	if out["message"] != nil {
		out["message"] = WorkbenchMessage(S(out["taskId"]), O(out["message"]))
	}
	return out
}
func InlineMessageMedia(message M, blockID, itemID string) (M, error) {
	for _, v := range A(message["blocks"]) {
		b := O(v)
		if S(b["id"]) != blockID {
			continue
		}
		for _, v := range A(O(b["data"])["items"]) {
			item := O(v)
			if S(item["id"]) != itemID {
				continue
			}
			src := S(item["src"])
			if !strings.HasPrefix(src, "data:") {
				break
			}
			comma := strings.Index(src, ",")
			if comma < 0 {
				break
			}
			meta := src[5:comma]
			mime := strings.Split(meta, ";")[0]
			if mime == "" {
				mime = "application/octet-stream"
			}
			if !regexp.MustCompile(`^[\w.+-]+/[\w.+-]+$`).MatchString(mime) {
				return nil, Problem(400, "invalid_media_type", "Invalid media type")
			}
			var bytes []byte
			var err error
			if strings.Contains(meta, ";base64") {
				bytes, err = base64.StdEncoding.DecodeString(src[comma+1:])
			} else {
				var text string
				text, err = url.PathUnescape(src[comma+1:])
				bytes = []byte(text)
			}
			if err != nil {
				break
			}
			return M{"bytes": bytes, "mimeType": mime, "name": firstString(item["name"], "media"), "etag": "\"" + Hash(bytes) + "\""}, nil
		}
	}
	return nil, Problem(404, "media_not_found", "Message media not found")
}
