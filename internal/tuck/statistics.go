package tuck

import "time"

func EmptyUsage() M {
	return M{"inputTokens": 0, "outputTokens": 0, "cacheReadTokens": 0, "cacheWriteTokens": 0, "reasoningTokens": 0}
}
func StartTurnTiming(task M, at int64) {
	task["turnTiming"] = M{"turnId": task["turnId"], "startedAt": at, "observedAt": at, "waitingMs": 0}
}
func ObserveTurnTiming(task M, at int64) {
	t := O(task["turnTiming"])
	if len(t) == 0 || t["endedAt"] != nil || B(t["incomplete"]) {
		return
	}
	n := max(float64(at), N(t["observedAt"]))
	if task["pendingInteraction"] != nil && t["waitingSince"] == nil {
		t["waitingSince"] = n
	}
	if task["pendingInteraction"] == nil && t["waitingSince"] != nil {
		t["waitingMs"] = N(t["waitingMs"]) + max(0, n-N(t["waitingSince"]))
		delete(t, "waitingSince")
	}
	t["observedAt"] = n
}
func FinishTurnTiming(task M, confirmed bool, at int64) {
	t := O(task["turnTiming"])
	if len(t) == 0 || t["endedAt"] != nil || B(t["incomplete"]) {
		return
	}
	end := N(t["observedAt"])
	if confirmed {
		end = max(end, float64(at))
	}
	if t["waitingSince"] != nil {
		t["waitingMs"] = N(t["waitingMs"]) + max(0, end-N(t["waitingSince"]))
	}
	delete(t, "waitingSince")
	t["observedAt"] = end
	if confirmed {
		t["endedAt"] = end
	} else {
		t["incomplete"] = true
	}
}
func AccumulateUsage(totals, usage M) M {
	next := Merge(EmptyUsage(), totals)
	last := O(usage["last_message"])
	if len(last) == 0 || B(usage["snapshot_only"]) {
		return next
	}
	for key, native := range map[string]string{"inputTokens": "input_tokens", "outputTokens": "output_tokens", "cacheReadTokens": "cached_input_tokens", "cacheWriteTokens": "cached_write_tokens", "reasoningTokens": "reasoning_tokens"} {
		next[key] = N(next[key]) + max(0, N(last[native]))
	}
	return next
}
func RecoveredStatistics(recovery M, agent string) M {
	usage := EmptyUsage()
	calls := 0
	seen := map[string]bool{}
	for _, v := range A(recovery["messages"]) {
		w := O(v)
		msg := w
		if w["message"] != nil {
			msg = O(w["message"])
		}
		if S(w["agent_name"]) != "" && S(w["agent_name"]) != agent || S(msg["role"]) != "assistant" || msg["usage"] == nil {
			continue
		}
		id := S(msg["message_id"])
		if id != "" && seen[id] {
			continue
		}
		seen[id] = true
		usage = AccumulateUsage(usage, M{"last_message": msg["usage"]})
		calls++
	}
	var count any
	source := "unavailable"
	if calls > 0 {
		count = calls
		source = "recovery"
	}
	return M{"usage": usage, "modelCalls": count, "usageSource": source, "context": M{"projectedTokens": max(0, N(recovery["input_tokens"])) + max(0, N(recovery["output_tokens"])), "contextWindow": 0}}
}

type runtimeTimings struct {
	streams map[string][2]int64
	tools   map[string]M
}

func (t *runtimeTimings) clear(id string) {
	delete(t.streams, id)
	for key, v := range t.tools {
		if S(v["taskId"]) == id {
			delete(t.tools, key)
		}
	}
}
func (t *runtimeTimings) observe(task, event M) M {
	at, err := time.Parse(time.RFC3339Nano, S(event["timestamp"]))
	if err != nil {
		return nil
	}
	if t.streams == nil {
		t.streams = map[string][2]int64{}
		t.tools = map[string]M{}
	}
	n := at.UnixMilli()
	id, kind := S(task["id"]), S(event["type"])
	f := O(O(event["tool_call"])["function"])
	if ((kind == "agent_choice" || kind == "agent_choice_reasoning") && S(event["content"]) != "") || (kind == "partial_tool_call" && (S(f["arguments"]) != "" || S(f["name"]) != "")) {
		v, ok := t.streams[id]
		if !ok {
			v = [2]int64{n, n}
		} else {
			v[1] = max(v[1], n)
		}
		t.streams[id] = v
	}
	patch := M{}
	usage := O(event["usage"])
	if kind == "token_usage" && usage["last_message"] != nil && !B(usage["snapshot_only"]) {
		v, ok := t.streams[id]
		delete(t.streams, id)
		tokens := N(O(usage["last_message"])["output_tokens"])
		if ok && v[1] > v[0] && n > v[0] && tokens > 0 {
			patch = M{"streamMs": n - v[0], "streamTokens": tokens, "streamSamples": 1}
		}
	}
	call := S(O(event["tool_call"])["id"])
	key := id + "\x00" + call
	if kind == "tool_call" && call != "" && t.tools[key] == nil {
		t.tools[key] = M{"taskId": id, "at": n}
	}
	if kind == "tool_call_response" {
		key := id + "\x00" + S(event["tool_call_id"])
		v := t.tools[key]
		delete(t.tools, key)
		if v != nil && S(v["taskId"]) == id && float64(n) >= N(v["at"]) {
			patch = M{"toolMs": float64(n) - N(v["at"]), "toolSamples": 1}
		}
	}
	if kind == "stream_stopped" || kind == "error" {
		t.clear(id)
	}
	if len(patch) == 0 {
		return nil
	}
	next := Merge(O(task["timings"]))
	for k, v := range patch {
		next[k] = N(next[k]) + N(v)
	}
	return next
}
