package tuck

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// Store owns all Tuck DDL and short write transactions. Network/file IO stays outside.
type Store struct {
	DB      *sql.DB
	Path    string
	writeMu sync.Mutex
}

func NewStore(path string) (*Store, error) {
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return nil, err
		}
	}
	db, err := openDatabase(path, "rwc")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)
	s := &Store{DB: db, Path: path}
	failed := true
	defer func() {
		if failed {
			db.Close()
		}
	}()
	version, err := inspectDatabase(db)
	if err != nil {
		return nil, err
	}
	if version == 0 {
		if err = s.Write(func(tx *sql.Tx) error {
			if _, e := tx.Exec(schemaV1); e != nil {
				return e
			}
			_, e := tx.Exec(schemaV2)
			return e
		}); err != nil {
			return nil, err
		}
	} else if version != databaseVersion {
		return nil, Problem(409, "database_version", "数据库需要迁移；请停服后运行 tuck migrate --check")
	}
	if _, err = db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return nil, err
	}
	if path != ":memory:" {
		if err = os.Chmod(path, 0600); err != nil {
			return nil, err
		}
	}
	failed = false
	return s, nil
}
func (s *Store) Write(fn func(*sql.Tx) error) error {
	return s.WriteContext(context.Background(), fn)
}
func (s *Store) WriteContext(ctx context.Context, fn func(*sql.Tx) error) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = fn(tx); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) Close() error { return s.DB.Close() }

type executor interface {
	Exec(string, ...any) (sql.Result, error)
}

const maxStoredJSON = 128 << 20
const maxCatalogJSON = 4 << 20

func storedJSON(value any, limit int) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	if len(raw) > limit {
		return "", Problem(413, "persistent_json_too_large", "持久内容超过可读取上限，未保存该次变更")
	}
	return string(raw), nil
}

// Filenames are URI-escaped before adding SQLite options, so '?' and '#' in a
// configuration directory cannot truncate the filename or inject pragmas.
func openDatabase(path, mode string) (*sql.DB, error) {
	var dsn string
	if path == ":memory:" {
		dsn = "file:tuck-" + ID() + "?mode=memory&cache=shared"
	} else {
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		p := filepath.ToSlash(abs)
		if p[0] != '/' {
			p = "/" + p
		}
		u := url.URL{Scheme: "file", Path: p}
		dsn = u.String() + "?mode=" + mode
	}
	return sql.Open("sqlite", dsn+"&_pragma=foreign_keys(1)&_pragma=synchronous(FULL)&_pragma=busy_timeout(5000)")
}

func saveTask(q executor, t M) error {
	created, _ := time.Parse(time.RFC3339Nano, S(t["createdAt"]))
	archived := 0
	if B(t["archived"]) {
		archived = 1
	}
	raw, err := storedJSON(t, maxStoredJSON)
	if err != nil {
		return err
	}
	_, err = q.Exec("INSERT INTO tasks VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET status=excluded.status,archived=excluded.archived,native_session_id=excluded.native_session_id,payload_json=excluded.payload_json", S(t["id"]), created.UnixMilli(), S(t["status"]), archived, S(t["nativeSessionId"]), raw)
	return err
}
func saveMessage(q executor, id string, m M) error {
	raw, err := storedJSON(m, maxStoredJSON)
	if err != nil {
		return err
	}
	_, err = q.Exec("INSERT INTO messages VALUES(?,?,?,?) ON CONFLICT(task_id,position) DO UPDATE SET revision=excluded.revision,payload_json=excluded.payload_json", id, I(m["index"]), I(m["revision"]), raw)
	return err
}
func (s *Store) SaveTask(t M) error {
	return s.Write(func(tx *sql.Tx) error { return saveTask(tx, t) })
}
func (s *Store) SaveMessage(id string, m M) error {
	return s.Write(func(tx *sql.Tx) error { return saveMessage(tx, id, m) })
}
func (s *Store) Publish(task, message, event M) (M, error) {
	var result M
	err := s.Write(func(tx *sql.Tx) error {
		var id any
		if task != nil {
			id = S(task["id"])
			if err := saveTask(tx, task); err != nil {
				return err
			}
			if message != nil {
				if err := saveMessage(tx, S(task["id"]), message); err != nil {
					return err
				}
			}
		}
		raw := Clone(event)
		delete(raw, "seq")
		delete(raw, "taskId")
		payload, err := storedJSON(raw, maxStoredJSON)
		if err != nil {
			return err
		}
		r, err := tx.Exec("INSERT INTO events(task_id,type,created_at_ms,payload_json) VALUES(?,?,?,?)", id, S(event["type"]), NowMS(), payload)
		if err != nil {
			return err
		}
		seq, err := r.LastInsertId()
		if err != nil {
			return err
		}
		taskID := ""
		if id != nil {
			taskID = id.(string)
		}
		result = Merge(raw, M{"seq": seq, "taskId": taskID})
		return nil
	})
	return result, err
}
func decodeRow(row *sql.Row) (M, error) {
	var raw string
	err := row.Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	v, err := DecodeJSON([]byte(raw), maxStoredJSON)
	return O(v), err
}
func readObjects(rows *sql.Rows) ([]M, error) {
	defer rows.Close()
	result := []M{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		v, err := DecodeJSON([]byte(raw), maxStoredJSON)
		if err != nil {
			return nil, err
		}
		result = append(result, O(v))
	}
	return result, rows.Err()
}
func (s *Store) Tasks() ([]M, error) {
	r, e := s.DB.Query("SELECT payload_json FROM tasks ORDER BY rowid ASC")
	if e != nil {
		return nil, e
	}
	return readObjects(r)
}
func (s *Store) Task(id string) (M, error) {
	return decodeRow(s.DB.QueryRow("SELECT payload_json FROM tasks WHERE id=?", id))
}
func (s *Store) Messages(id string, start, count int) ([]M, error) {
	r, e := s.DB.Query("SELECT payload_json FROM messages WHERE task_id=? AND position>=? ORDER BY position LIMIT ?", id, start, count)
	if e != nil {
		return nil, e
	}
	return readObjects(r)
}
func (s *Store) Message(id string, index int) (M, error) {
	return decodeRow(s.DB.QueryRow("SELECT payload_json FROM messages WHERE task_id=? AND position=?", id, index))
}

// Fork presentation comes only from unambiguous parent facts. A reused call
// with missing or conflicting identity must never acquire another call's access.
func (s *Store) InspectionPresentations(id string) (map[string]M, error) {
	rows, err := s.DB.Query(`SELECT json_extract(block.value,'$.data') FROM messages,
	 json_each(messages.payload_json,'$.blocks') AS block WHERE task_id=?
	 AND json_extract(block.value,'$.type')='tool-call'
	 AND json_extract(block.value,'$.data.name')='inspect_attachments'`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result, invalid := map[string]M{}, map[string]bool{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		value, err := DecodeJSON([]byte(raw), maxStoredJSON)
		if err != nil {
			return nil, err
		}
		data := O(value)
		key, presentation := inspectionCallKey(data), O(data["presentation"])
		workspace := O(presentation["workspace"])
		if S(workspace["subject"]) == "" || S(workspace["space"]) == "" || S(workspace["resourceId"]) == "" || result[key] != nil && string(JSON(O(result[key]["workspace"]))) != string(JSON(workspace)) {
			invalid[key] = true
		}
		if invalid[key] {
			delete(result, key)
		} else {
			result[key] = presentation
		}
	}
	return result, rows.Err()
}
func (s *Store) LiveMessages(id string) ([]M, error) {
	rows, err := s.DB.Query("SELECT payload_json FROM messages WHERE task_id=? AND json_extract(payload_json,'$.live')=1 ORDER BY position", id)
	if err != nil {
		return nil, err
	}
	return readObjects(rows)
}
func (s *Store) SteeringMessages(id string, start, count int) ([]M, error) {
	rows, err := s.DB.Query("SELECT payload_json FROM messages WHERE task_id=? AND position>=? AND EXISTS(SELECT 1 FROM json_each(payload_json,'$.blocks') b WHERE json_extract(b.value,'$.id') LIKE 'steer:%') ORDER BY position LIMIT ?", id, start, count)
	if err != nil {
		return nil, err
	}
	return readObjects(rows)
}
func (s *Store) Cursor() (int64, error) {
	var n int64
	err := s.DB.QueryRow("SELECT COALESCE(MAX(seq),0) FROM events").Scan(&n)
	return n, err
}
func eventsRows(rows *sql.Rows) ([]M, error) {
	return eventsRowsLimit(rows, 0)
}
func eventsRowsLimit(rows *sql.Rows, budget int) ([]M, error) {
	defer rows.Close()
	r := []M{}
	bytes := 0
	for rows.Next() {
		var seq int64
		var task sql.NullString
		var raw string
		if e := rows.Scan(&seq, &task, &raw); e != nil {
			return nil, e
		}
		if budget > 0 && bytes+len(raw) > budget && len(r) > 0 {
			break
		}
		bytes += len(raw)
		v, e := DecodeJSON([]byte(raw), maxStoredJSON)
		if e != nil {
			return nil, e
		}
		r = append(r, Merge(O(v), M{"seq": seq, "taskId": task.String}))
	}
	return r, rows.Err()
}
func (s *Store) Events(after int64, count int) ([]M, error) {
	r, e := s.DB.Query("SELECT seq,task_id,payload_json FROM events WHERE seq>? ORDER BY seq LIMIT ?", after, count)
	if e != nil {
		return nil, e
	}
	return eventsRowsLimit(r, 4<<20)
}
func (s *Store) TaskEvents(id string, types []string) ([]M, error) {
	args := []any{id}
	marks := []string{}
	for _, t := range types {
		marks = append(marks, "?")
		args = append(args, t)
	}
	q := "SELECT seq,task_id,payload_json FROM events WHERE task_id=?"
	if len(marks) > 0 {
		q += " AND type IN(" + strings.Join(marks, ",") + ")"
	}
	q += " ORDER BY seq"
	r, e := s.DB.Query(q, args...)
	if e != nil {
		return nil, e
	}
	return eventsRows(r)
}
func (s *Store) SteeringReceipts(id string, after int64) ([]M, error) {
	rows, err := s.DB.Query("SELECT seq,task_id,payload_json FROM events WHERE task_id=? AND type='native' AND seq>? AND json_extract(payload_json,'$.event.type')='user_message' ORDER BY seq", id, after)
	if err != nil {
		return nil, err
	}
	return eventsRows(rows)
}
func (s *Store) HasDelivery(id, turn string) (bool, error) {
	var n int
	err := s.DB.QueryRow("SELECT COUNT(*) FROM task_deliveries WHERE task_id=? AND turn_id=?", id, turn).Scan(&n)
	return n > 0, err
}
func (s *Store) FinalReply(id string) (string, error) {
	rows, err := s.DB.Query("SELECT payload_json FROM messages WHERE task_id=? ORDER BY position DESC", id)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return "", err
		}
		v, e := DecodeJSON([]byte(raw), maxStoredJSON)
		if e != nil {
			return "", e
		}
		m := O(v)
		if S(m["role"]) == "user" {
			return "", nil
		}
		for _, b := range A(m["blocks"]) {
			if S(O(b)["type"]) == "tool-call" {
				return "", nil
			}
		}
		if S(m["role"]) != "assistant" || B(m["live"]) {
			continue
		}
		parts := []string{}
		for _, b := range A(m["blocks"]) {
			if S(O(b)["type"]) == "markdown" {
				parts = append(parts, S(O(O(b)["data"])["markdown"]))
			}
		}
		if text := strings.TrimSpace(strings.Join(parts, "\n\n")); text != "" {
			return text, nil
		}
	}
	return "", rows.Err()
}
func (s *Store) Statistics(task M) (M, error) {
	id := S(task["id"])
	current := S(O(task["turnTiming"])["turnId"])
	var elapsed float64
	var turns, incomplete int
	err := s.DB.QueryRow(`SELECT COALESCE(SUM(MAX(0,last-first)),0),COUNT(*),COALESCE(MAX(incomplete),0) FROM (
 SELECT MIN(turn_started_ms) AS first,
 MAX(turn_end_ms) AS last,
 MAX(turn_incomplete) AS incomplete
 FROM events WHERE task_id=? AND type IN ('created','state')
 AND turn_id IS NOT NULL AND turn_id NOT IN ('',?)
 GROUP BY turn_id)`, id, current).Scan(&elapsed, &turns, &incomplete)
	if err != nil {
		return nil, err
	}
	var tools int
	err = s.DB.QueryRow("SELECT COUNT(*) FROM messages,json_each(messages.payload_json,'$.blocks') b WHERE task_id=? AND json_extract(b.value,'$.type')='tool-call'", id).Scan(&tools)
	return M{"previousMs": elapsed, "previousTurns": turns, "incomplete": incomplete != 0, "toolCalls": tools}, err
}
func (s *Store) Catalog(key, fingerprint string) ([]M, int64, error) {
	var raw string
	var at int64
	err := s.DB.QueryRow("SELECT catalog_json,verified_at_ms FROM mcp_catalog WHERE resource_key=? AND fingerprint=?", key, fingerprint).Scan(&raw, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	v, e := DecodeJSON([]byte(raw), maxCatalogJSON)
	r := []M{}
	for _, x := range A(v) {
		r = append(r, O(x))
	}
	return r, at, e
}
func (s *Store) SaveCatalog(key, fingerprint string, tools []M) error {
	if tools == nil {
		tools = []M{}
	}
	raw, err := storedJSON(tools, maxCatalogJSON)
	if err != nil {
		return err
	}
	return s.Write(func(tx *sql.Tx) error {
		_, e := tx.Exec("INSERT INTO mcp_catalog VALUES(?,?,?,?) ON CONFLICT(resource_key) DO UPDATE SET fingerprint=excluded.fingerprint,catalog_json=excluded.catalog_json,verified_at_ms=excluded.verified_at_ms", key, fingerprint, raw, NowMS())
		return e
	})
}
func (s *Store) Check() error {
	var result string
	if err := s.DB.QueryRow("PRAGMA quick_check").Scan(&result); err != nil {
		return err
	}
	if result != "ok" {
		return fmt.Errorf("SQLite integrity: %s", result)
	}
	rows, err := s.DB.Query("PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	defer rows.Close()
	if rows.Next() {
		return fmt.Errorf("SQLite foreign key violation")
	}
	return rows.Err()
}
