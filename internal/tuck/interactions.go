package tuck

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func QuestionSchema(raw any) M {
	schema := Clone(O(raw))
	if len(O(schema["properties"])) > 0 {
		schema["type"] = "object"
		return schema
	}
	if S(schema["type"]) != "" && S(schema["type"]) != "object" {
		key := S(schema["title"])
		if key == "" {
			key = "value"
		}
		return M{"type": "object", "properties": M{key: schema}, "required": []any{key}}
	}
	return M{"type": "object", "properties": M{"response": M{"type": "string", "title": "回答问题"}}, "required": []any{"response"}}
}
func QuestionContent(raw, answer any) (M, error) {
	if answer == nil {
		return nil, nil
	}
	text, ok := answer.(string)
	if !ok {
		return nil, Problem(400, "invalid_answer", "Invalid interaction response")
	}
	schema := QuestionSchema(raw)
	var content M
	_ = json.Unmarshal([]byte(text), &content)
	if content == nil {
		props := O(schema["properties"])
		if len(props) == 1 {
			for key, v := range props {
				kind := S(O(v)["type"])
				if kind == "" || kind == "string" {
					content = M{key: text}
				}
			}
		}
		if content == nil {
			return nil, Problem(422, "invalid_answer", "请分别填写问题中的各个字段")
		}
	}
	compiler := jsonschema.NewCompiler()
	compiler.UseLoader(jsonschema.SchemeURLLoader{})
	compiler.AssertFormat()
	if err := compiler.AddResource("urn:tuck:question", schema); err != nil {
		return nil, Problem(422, "invalid_schema", "问题格式无效，请跳过或让 Agent 重新提问")
	}
	compiled, err := compiler.Compile("urn:tuck:question")
	if err != nil {
		return nil, Problem(422, "invalid_schema", "问题格式无效，请跳过或让 Agent 重新提问")
	}
	if err = compiled.Validate(content); err != nil {
		return nil, Problem(422, "invalid_answer", "请检查回答的必填项、类型或取值范围")
	}
	return content, nil
}
func (m *Manager) requestInteractionLocked(task, pending, native M) error {
	id := S(task["id"])
	for _, r := range m.interactions[id] {
		if S(r.pending["id"]) == S(pending["id"]) {
			return nil
		}
	}
	m.interactions[id] = append(m.interactions[id], interaction{pending, native})
	if len(m.interactions[id]) == 1 {
		return m.stateLocked(task, M{"status": "waiting", "pendingInteraction": pending, "nativeInteraction": native})
	}
	return nil
}
func (m *Manager) ResolveInteraction(ctx context.Context, id string, response M) error {
	m.mu.Lock()
	task, err := m.getLocked(id)
	if err == nil && m.changing {
		err = Problem(409, "configuration_changing", "配置正在应用，请稍后执行")
	}
	pending := O(task["pendingInteraction"])
	if err == nil && (len(pending) == 0 || pending["id"] != response["interactionId"] || pending["kind"] != response["kind"]) {
		err = Problem(409, "interaction_expired", "此交互已失效，请使用当前问题或审批")
	}
	if err == nil && m.resolving[id] {
		err = Problem(409, "interaction_busy", "当前交互正在处理")
	}
	kind := S(response["kind"])
	if err == nil {
		if kind == "approval" {
			if _, ok := response["approved"].(bool); !ok {
				err = Problem(400, "invalid_interaction", "Invalid interaction response")
			}
		} else if kind == "question" && response["answer"] != nil {
			if _, ok := response["answer"].(string); !ok {
				err = Problem(400, "invalid_interaction", "Invalid interaction response")
			}
		}
	}
	if err != nil {
		m.mu.Unlock()
		return err
	}
	snapshot := Clone(task)
	pending = Clone(pending)
	m.resolving[id] = true
	m.mu.Unlock()
	defer func() { m.mu.Lock(); delete(m.resolving, id); m.mu.Unlock() }()
	path := nativePath(snapshot, "/resume")
	body := M{"confirmation": "reject", "reason": "User rejected the proposed action"}
	if kind == "approval" {
		if B(response["approved"]) {
			body = M{"confirmation": "approve", "reason": ""}
		}
	} else {
		native := O(snapshot["nativeInteraction"])
		content, e := QuestionContent(native["schema"], response["answer"])
		if e != nil {
			return e
		}
		target := Clone(snapshot)
		if S(native["targetId"]) != "" {
			target["nativeSessionId"] = native["targetId"]
		}
		path = nativePath(target, "/elicitation")
		action := "accept"
		if response["answer"] == nil {
			action = "decline"
		}
		body = M{"action": action, "elicitation_id": pending["id"]}
		if content != nil {
			body["content"] = content
		}
	}
	if _, err = m.options.Engine.Request(ctx, "POST", path, body); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	task = m.tasks[id]
	if O(task["pendingInteraction"])["id"] != pending["id"] {
		return nil
	}
	remaining := []interaction{}
	for _, r := range m.interactions[id] {
		if r.pending["id"] != pending["id"] {
			remaining = append(remaining, r)
		}
	}
	if len(remaining) > 0 {
		m.interactions[id] = remaining
		return m.stateLocked(task, M{"status": "waiting", "pendingInteraction": remaining[0].pending, "nativeInteraction": remaining[0].native})
	}
	delete(m.interactions, id)
	task["activity"] = nil
	if err = m.normalizer.activity(task, "processing", "", false); err != nil {
		return err
	}
	return m.stateLocked(task, M{"status": "working", "pendingInteraction": nil, "nativeInteraction": nil})
}
func (m *Manager) Steer(ctx context.Context, id, prompt string, ids []string, source M) (M, error) {
	text, err := promptText(prompt, len(ids) > 0)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	task, err := m.getLocked(id)
	if err == nil {
		err = m.checkLocked(task, false)
	}
	if err == nil {
		err = m.bind(task)
	}
	if err == nil && task["pendingInteraction"] != nil {
		err = Problem(409, "interaction_pending", "请先处理当前审批或问题，消息可先加入队列")
	}
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}
	if S(task["status"]) != "working" {
		m.mu.Unlock()
		return m.Submit(id, text, ids, nil, source)
	}
	snapshot := Clone(task)
	m.mu.Unlock()
	native, blocks, err := m.prepare(snapshot, text, ids)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	task = m.tasks[id]
	if err = m.checkLocked(task, false); err == nil {
		err = m.bind(task)
	}
	if err == nil && (task["pendingInteraction"] != nil || S(task["status"]) != "working" || task["turnId"] != snapshot["turnId"]) {
		err = Problem(409, "turn_changed", "执行状态已变化，请重新发送")
	}
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}
	cursor, err := m.options.Store.Cursor()
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}
	record := &steeringRecord{hash: Hash([]byte(trimEnd(S(native["content"])))), after: cursor, status: "pending", index: -1, turnID: S(snapshot["turnId"])}
	m.steering[id] = append(m.steering[id], record)
	m.mu.Unlock()
	_, requestErr := m.options.Engine.Request(ctx, "POST", nativePath(snapshot, "/steer"), M{"messages": []M{native}})
	m.mu.Lock()
	defer m.mu.Unlock()
	task = m.tasks[id]
	if requestErr != nil && record.status != "delivered" {
		m.forgetSteeringLocked(id, record)
		return nil, requestErr
	}
	label := "用户立即补充"
	if S(source["kind"]) == "collaboration" {
		label = "跨会话消息"
	}
	display := []any{newBlock("steer:"+UUID(), "markdown", M{"markdown": "> **" + label + "** · 等待加入上下文\n\n" + text})}
	for _, b := range blocks {
		if S(O(b)["type"]) != "markdown" {
			display = append(display, b)
		}
	}
	entry := M{"turnId": snapshot["turnId"], "role": "user", "blocks": display, "steering": M{"hash": record.hash, "afterSeq": record.after, "status": record.status}}
	if source != nil {
		entry["source"] = Clone(source)
	}
	msg, err := m.appendLocked(task, entry)
	if err != nil {
		return nil, err
	}
	record.index = I(msg["index"])
	if err = m.updateSteeringLocked(task, record); err != nil {
		return nil, err
	}
	if record.status != "pending" {
		m.forgetSteeringLocked(id, record)
	}
	return M{"accepted": true}, nil
}
func (m *Manager) forgetSteeringLocked(id string, record *steeringRecord) {
	list := m.steering[id]
	for i, r := range list {
		if r == record {
			list = append(list[:i], list[i+1:]...)
			break
		}
	}
	if len(list) == 0 {
		delete(m.steering, id)
	} else {
		m.steering[id] = list
	}
}
func (m *Manager) updateSteeringLocked(task M, record *steeringRecord) error {
	if record.index < 0 {
		return nil
	}
	msg, err := m.options.Store.Message(S(task["id"]), record.index)
	if err != nil {
		return err
	}
	labels := map[string]string{"pending": "等待加入上下文", "delivered": "已加入上下文", "unknown": "未确认是否加入上下文"}
	for _, v := range A(msg["blocks"]) {
		b := O(v)
		if strings.HasPrefix(S(b["id"]), "steer:") {
			data := O(b["data"])
			body := S(data["markdown"])
			if i := strings.Index(body, "\n\n"); i >= 0 {
				body = body[i:]
			} else {
				body = "\n\n" + body
			}
			source := "立即补充"
			if S(O(msg["source"])["kind"]) == "collaboration" || msg["source"] == nil && strings.HasPrefix(strings.TrimSpace(body), "[协作消息]") {
				source = "跨会话消息"
			}
			data["markdown"] = "> **" + source + "** · " + labels[record.status] + body
			msg["steering"] = M{"hash": record.hash, "afterSeq": record.after, "status": record.status}
			if record.messageID != "" {
				O(msg["steering"])["messageId"] = record.messageID
			}
			return m.replaceLocked(task, msg)
		}
	}
	return nil
}
func (m *Manager) confirmSteeringLocked(task, event M) error {
	id := S(task["id"])
	if S(event["session_id"]) != S(task["nativeSessionId"]) {
		return nil
	}
	receipt := S(event["message_id"])
	if receipt != "" && m.receipts[id][receipt] {
		return nil
	}
	hash := Hash([]byte(trimEnd(S(event["message"]))))
	for _, r := range m.steering[id] {
		if r.status == "pending" && r.hash == hash {
			if receipt != "" {
				if m.receipts[id] == nil {
					m.receipts[id] = map[string]bool{}
				}
				m.receipts[id][receipt] = true
			}
			r.status = "delivered"
			r.messageID = receipt
			if err := m.updateSteeringLocked(task, r); err != nil {
				return err
			}
			if r.index >= 0 {
				m.forgetSteeringLocked(id, r)
			}
			break
		}
	}
	return nil
}
func (m *Manager) settleSteeringLocked(task M) error {
	id := S(task["id"])
	for _, r := range m.steering[id] {
		if r.status == "pending" {
			r.status = "unknown"
			if err := m.updateSteeringLocked(task, r); err != nil {
				return err
			}
		}
	}
	delete(m.steering, id)
	delete(m.receipts, id)
	return nil
}
func (m *Manager) restoreSteeringLocked() error {
	for id, task := range m.tasks {
		messages := []M{}
		used := map[string]bool{}
		for start := 0; ; {
			batch, err := m.options.Store.SteeringMessages(id, start, 512)
			if err != nil {
				return err
			}
			messages = append(messages, batch...)
			if len(batch) < 512 {
				break
			}
			start = I(batch[len(batch)-1]["index"]) + 1
		}
		pending := false
		var after int64 = 9007199254740991
		for _, msg := range messages {
			steering := O(msg["steering"])
			if status := S(steering["status"]); status != "delivered" && status != "unknown" {
				pending = true
				after = min(after, int64(N(steering["afterSeq"])))
			}
			if receipt := S(O(msg["steering"])["messageId"]); receipt != "" {
				used[receipt] = true
			}
		}
		if !pending {
			continue
		}
		events, err := m.options.Store.SteeringReceipts(id, after)
		if err != nil {
			return err
		}
		for _, msg := range messages {
			steering := O(msg["steering"])
			if S(steering["status"]) == "delivered" || S(steering["status"]) == "unknown" {
				continue
			}
			var content string
			found := false
			for _, v := range A(msg["blocks"]) {
				b := O(v)
				if strings.HasPrefix(S(b["id"]), "steer:") {
					text := S(O(b["data"])["markdown"])
					if i := strings.Index(text, "\n\n"); i >= 0 {
						content = text[i+2:]
					} else {
						content = text
					}
					found = true
					break
				}
			}
			if !found {
				continue
			}
			hash := S(steering["hash"])
			if hash == "" {
				hash = Hash([]byte(trimEnd(content)))
			}
			candidates := []M{}
			for _, row := range events {
				event := O(row["event"])
				if S(event["type"]) == "user_message" && S(event["session_id"]) == S(task["nativeSessionId"]) && !used[S(event["message_id"])] && N(row["seq"]) > N(steering["afterSeq"]) && Hash([]byte(trimEnd(S(event["message"])))) == hash {
					candidates = append(candidates, event)
				}
			}
			unique := len(steering) > 0
			if !unique {
				count := 0
				for _, other := range messages {
					for _, v := range A(other["blocks"]) {
						b := O(v)
						text := S(O(b["data"])["markdown"])
						if strings.HasPrefix(S(b["id"]), "steer:") {
							i := strings.Index(text, "\n\n")
							if i >= 0 && Hash([]byte(trimEnd(text[i+2:]))) == hash {
								count++
							}
						}
					}
				}
				unique = count == 1
			}
			r := &steeringRecord{hash: hash, after: int64(N(steering["afterSeq"])), index: I(msg["index"]), status: "unknown"}
			if len(candidates) == 1 && unique {
				r.status = "delivered"
				r.messageID = S(candidates[0]["message_id"])
				used[r.messageID] = true
			}
			if err = m.updateSteeringLocked(task, r); err != nil {
				return err
			}
		}
	}
	return nil
}
