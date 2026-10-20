package tuck

import (
	"database/sql"
	"strings"
)

func migrateEventFacts(tx *sql.Tx) error {
	rows, err := tx.Query("SELECT payload_json FROM tasks ORDER BY rowid")
	if err != nil {
		return err
	}
	tasks, err := readObjects(rows)
	if err != nil {
		return err
	}
	byID := map[string]M{}
	for _, task := range tasks {
		id := S(task["id"])
		var elapsed float64
		var turns, incomplete int
		// This is exactly the v2 statistics boundary: the current timing is
		// still live in tasks and must not be counted twice.
		err = tx.QueryRow(`SELECT COALESCE(SUM(MAX(0,last-first)),0),COUNT(*),COALESCE(MAX(incomplete),0) FROM (
 SELECT MIN(turn_started_ms) AS first, MAX(turn_end_ms) AS last, MAX(turn_incomplete) AS incomplete
 FROM events WHERE task_id=? AND type IN ('created','state')
 AND turn_id IS NOT NULL AND turn_id NOT IN ('',?)
 GROUP BY turn_id)`, id, S(O(task["turnTiming"])["turnId"])).Scan(&elapsed, &turns, &incomplete)
		if err != nil {
			return err
		}
		task["historyTiming"] = M{"previousMs": elapsed, "previousTurns": turns, "incomplete": incomplete != 0}
		if err = saveTask(tx, task); err != nil {
			return err
		}
		byID[id] = task
	}
	return migrateSteeringReceipts(tx, byID, func(task, msg M) error {
		return saveMessage(tx, S(task["id"]), msg)
	})
}

// Legacy receipt extraction runs only before v3 drops the replay cache.
func migrateSteeringReceipts(q schemaReader, tasks map[string]M, save func(M, M) error) error {
	for id, task := range tasks {
		messages := []M{}
		used := map[string]bool{}
		for start := 0; ; {
			batch, err := steeringMessages(q, id, start, 512)
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
		rows, err := q.Query("SELECT seq,task_id,payload_json FROM events WHERE task_id=? AND type='native' AND seq>? AND json_extract(payload_json,'$.event.type')='user_message' ORDER BY seq", id, after)
		if err != nil {
			return err
		}
		events, err := eventsRows(rows)
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
			r := &steeringRecord{status: "unknown"}
			if len(candidates) == 1 && unique {
				r.status = "delivered"
				r.messageID = S(candidates[0]["message_id"])
				used[r.messageID] = true
			}
			if updateSteeringMessage(msg, r) {
				if err := save(task, msg); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
