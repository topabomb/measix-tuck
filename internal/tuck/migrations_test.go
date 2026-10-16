package tuck

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"encoding/base64"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Frozen released Go v1 template, with no user credentials.
// Do not derive old fixtures from future defaults by changing a version number.
//
//go:embed testdata/config-v1.json
var releasedConfigV1 []byte

func legacyMigrationFixture(t *testing.T) (string, M, *Store) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "tuck.config.json")
	value, err := DecodeJSON(releasedConfigV1, 4<<20)
	if err != nil {
		t.Fatal(err)
	}
	v := O(value)
	O(v["security"])["credentialKey"] = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{37}, 32))
	O(v["storage"])["dataDir"] = "data"
	O(v["storage"])["workspaceRoot"] = "data/workspaces"
	O(O(v["agents"])["root"])["add_date"] = true // Native extension survives.
	if err := os.WriteFile(path, JSON(v), 0600); err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(dir, "data")
	if err := os.Mkdir(data, 0700); err != nil {
		t.Fatal(err)
	}
	db, err := openDatabase(filepath.Join(data, "tuck.db"), "rwc")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(schemaV1); err != nil {
		t.Fatal(err)
	}
	s := &Store{DB: db, Path: filepath.Join(data, "tuck.db")}
	t.Cleanup(func() { s.Close() })
	return path, v, s
}

func migrationRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestMigrationV1PreservesAllOwnedData(t *testing.T) {
	path, config, s := legacyMigrationFixture(t)
	task := BaseTask(ID(), UUID(), "persistent", filepath.Join(filepath.Dir(path), "workspace"), "root", "agent")
	message := M{"index": 0, "revision": 7, "live": false, "blocks": []any{M{"id": "steer:original", "type": "text", "text": "delivered"}}}
	id := S(task["id"])
	if _, err := s.Publish(task, message, M{"type": "message", "message": message}); err != nil {
		t.Fatal(err)
	}
	// A deleted highest event must never cause AUTOINCREMENT cursor reuse.
	if _, err := s.Publish(task, nil, M{"type": "native", "event": M{"type": "user_message"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec("DELETE FROM events WHERE seq=2"); err != nil {
		t.Fatal(err)
	}
	state := M{"installationId": "ins_" + UUID(), "binding": M{"origin": "https://example.test", "credentials": M{"accessToken": "secret-access", "refreshToken": "secret-refresh"}, "snapshot": M{"schemaVersion": 5, "snapshotHash": "unchanged"}, "pendingRefresh": M{"key": "same-idempotency-key", "token": "secret-refresh"}, "appliedReported": true}}
	cipher, err := platformCrypt(config, S(state["installationId"]), 1, JSON(state), false)
	if err != nil {
		t.Fatal(err)
	}
	deliveryID, fileID := ID(), ID()
	deliveryBytes := []byte("immutable delivery")
	deliveryFiles := []any{M{"id": fileID, "path": "result.txt", "sizeBytes": len(deliveryBytes), "sha256": Hash(deliveryBytes)}}
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{"INSERT INTO platform_session VALUES(1,?,?)", []any{state["installationId"], cipher}},
		{"INSERT INTO web_sessions VALUES('token-hash','identity','csrf',9999999999999)", nil},
		{"INSERT INTO login_limits VALUES('ip:key',3,900,9999999999999)", nil},
		{"INSERT INTO mcp_catalog VALUES('valid','fingerprint','[]',12)", nil},
		{"INSERT INTO mcp_catalog VALUES('invalid-cache','fingerprint','{}',12)", nil},
		{"INSERT INTO task_deliveries VALUES(?,?,?,?,?,?,?,?,?)", []any{deliveryID, id, 4, "turn-original", 100, "summary", 1, len(deliveryBytes), string(JSON(deliveryFiles))}},
	} {
		if _, err = s.DB.Exec(q.sql, q.args...); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = s.DB.Exec("INSERT INTO mcp_catalog VALUES('oversized-cache','fingerprint',?,12)", string(JSON([]any{strings.Repeat("x", maxCatalogJSON)}))); err != nil {
		t.Fatal(err)
	}
	assets := []string{"data/deliveries/snapshots/keep-original", "data/native/docker-sessions.db", "workspace/uploads/original.bin"}
	snapshot := filepath.Join(filepath.Dir(path), "data", "deliveries", "snapshots", id, deliveryID, "files", fileID)
	if err = os.MkdirAll(filepath.Dir(snapshot), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(snapshot, deliveryBytes, 0600); err != nil {
		t.Fatal(err)
	}
	for _, name := range assets {
		full := filepath.Join(filepath.Dir(path), name)
		if err = os.MkdirAll(filepath.Dir(full), 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(full, []byte("opaque:"+name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()
	raw, dbRaw := migrationRead(t, path), migrationRead(t, s.Path)
	var output bytes.Buffer
	if err = Run([]string{"migrate", "--check", "--config", path}, nil, &output, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, migrationRead(t, path)) || !bytes.Equal(dbRaw, migrationRead(t, s.Path)) {
		t.Fatal("check changed source")
	}
	backup := filepath.Join(t.TempDir(), "before-migration")
	if err = MigrateCommand(path, []string{"--backup", backup}, &output); err != nil {
		t.Fatal(err, output.String())
	}
	if !bytes.Equal(raw, migrationRead(t, filepath.Join(backup, "config.json"))) {
		t.Fatal("backup lost exact configuration bytes")
	}
	if strings.Contains(output.String(), "secret-access") || strings.Contains(output.String(), "secret-refresh") {
		t.Fatal("secrets leaked")
	}
	current, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	want := Clone(config)
	want["schemaVersion"] = configVersion
	if !reflect.DeepEqual(current.Applied(), Clone(want)) {
		t.Fatal("configuration values changed")
	}
	upgraded, err := NewStore(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	gotTask, _ := upgraded.Task(id)
	gotMessage, _ := upgraded.Message(id, 0)
	if !reflect.DeepEqual(gotTask, Clone(task)) || !reflect.DeepEqual(gotMessage, Clone(message)) {
		t.Fatal("task/message payload changed")
	}
	for table, wantCount := range map[string]int{"events": 1, "web_sessions": 1, "login_limits": 1, "task_deliveries": 1, "mcp_catalog": 1} {
		var count int
		if err = upgraded.DB.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil || count != wantCount {
			t.Fatal(table, count, err)
		}
	}
	platform, err := NewPlatform(upgraded, current)
	if err != nil {
		t.Fatal(err)
	}
	defer platform.Close()
	if !reflect.DeepEqual(platform.Data(), Clone(state)) {
		t.Fatal("platform identity/credentials/refresh receipt changed")
	}
	var nextCipher []byte
	var format int
	if err = upgraded.DB.QueryRow("SELECT encrypted_payload,format_version FROM platform_session").Scan(&nextCipher, &format); err != nil || bytes.Equal(cipher, nextCipher) || format != platformFormatVersion {
		t.Fatal("cipher format was not migrated", err)
	}
	if _, err = platformCrypt(want, "ins_other", format, nextCipher, true); err == nil {
		t.Fatal("cipher accepted a different installation")
	}
	event, err := upgraded.Publish(task, nil, M{"type": "state"})
	if err != nil || I(event["seq"]) != 3 {
		t.Fatal("cursor highwater lost", event, err)
	}
	old, err := openDatabase(filepath.Join(backup, "tuck.db"), "ro")
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	oldVersion, err := inspectDatabase(old)
	var oldCipher []byte
	if err != nil || oldVersion != 1 {
		t.Fatal("backup is not restorable v1", oldVersion, err)
	}
	if err = old.QueryRow("SELECT encrypted_payload FROM platform_session").Scan(&oldCipher); err != nil || !bytes.Equal(cipher, oldCipher) {
		t.Fatal("backup ciphertext changed", err)
	}
	for _, name := range assets {
		if string(migrationRead(t, filepath.Join(filepath.Dir(path), name))) != "opaque:"+name {
			t.Fatal("file asset changed", name)
		}
	}
	if !bytes.Equal(deliveryBytes, migrationRead(t, snapshot)) {
		t.Fatal("committed delivery bytes changed")
	}
}

func TestMigrationAuditsJSONAndCommittedFilesBeforeWriting(t *testing.T) {
	for _, kind := range []string{"task-projection", "message-projection", "event-projection", "missing-snapshot", "damaged-snapshot"} {
		t.Run(kind, func(t *testing.T) {
			path, _, s := legacyMigrationFixture(t)
			task := BaseTask(ID(), UUID(), "title", "", "root", "agent")
			if _, err := s.Publish(task, M{"index": 0, "revision": 0}, M{"type": "state"}); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "task-projection":
				s.DB.Exec("UPDATE tasks SET status='running'")
			case "message-projection":
				s.DB.Exec("UPDATE messages SET revision=99")
			case "event-projection":
				s.DB.Exec("UPDATE events SET type='native'")
			default:
				id, fileID := ID(), ID()
				files := []any{M{"id": fileID, "sizeBytes": 4, "sha256": Hash([]byte("good"))}}
				if _, err := s.DB.Exec("INSERT INTO task_deliveries VALUES(?,?,?,?,?,?,?,?,?)", id, task["id"], 1, "turn", 1, "summary", 1, 4, string(JSON(files))); err != nil {
					t.Fatal(err)
				}
				if kind == "damaged-snapshot" {
					file := filepath.Join(filepath.Dir(s.Path), "deliveries", "snapshots", S(task["id"]), id, "files", fileID)
					if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(file, []byte("evil"), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			s.Close()
			before := migrationRead(t, s.Path)
			backup := filepath.Join(t.TempDir(), "unused")
			if err := MigrateCommand(path, []string{"--backup", backup}, io.Discard); err == nil {
				t.Fatal("accepted inconsistent data", kind)
			}
			if _, err := os.Stat(backup); !os.IsNotExist(err) || !bytes.Equal(before, migrationRead(t, s.Path)) {
				t.Fatal("audit failure changed input")
			}
		})
	}
}

func TestMigrationIndependentVersionsAndRestart(t *testing.T) {
	for _, configV := range []int{1, 2} {
		for _, dbV := range []int{0, 1, 2} {
			t.Run(string(rune('0'+configV))+"/"+string(rune('0'+dbV)), func(t *testing.T) {
				path, v, s := legacyMigrationFixture(t)
				if dbV == 2 {
					if err := migrateDatabase(s, v, 1); err != nil {
						t.Fatal(err)
					}
				}
				s.Close()
				if dbV == 0 {
					if err := os.Remove(s.Path); err != nil {
						t.Fatal(err)
					}
				}
				v["schemaVersion"] = configV
				if err := AtomicWrite(path, JSON(v), 0600); err != nil {
					t.Fatal(err)
				}
				backup := filepath.Join(t.TempDir(), "backup")
				if err := MigrateCommand(path, []string{"--backup", backup}, io.Discard); err != nil {
					t.Fatal(err)
				}
				if _, err := LoadConfig(path); err != nil {
					t.Fatal(err)
				}
				store, err := NewStore(s.Path)
				if err != nil {
					t.Fatal(err)
				}
				store.Close()
				raw := migrationRead(t, path)
				unused := filepath.Join(t.TempDir(), "must-not-create")
				if err = MigrateCommand(path, []string{"--backup", unused}, io.Discard); err != nil {
					t.Fatal(err)
				}
				if _, err = os.Stat(unused); !os.IsNotExist(err) || !bytes.Equal(raw, migrationRead(t, path)) {
					t.Fatal("repeat migration was not a no-op")
				}
			})
		}
	}
}

func TestMigrationRejectsUnknownOrInvalidInputsWithoutChanges(t *testing.T) {
	for _, kind := range []string{"future-config", "unversioned-config", "missing-key", "wrong-key", "future-db", "foreign-db", "spoofed-current-db", "extra-index"} {
		t.Run(kind, func(t *testing.T) {
			path, v, s := legacyMigrationFixture(t)
			switch kind {
			case "future-config":
				v["schemaVersion"] = 99
			case "unversioned-config":
				delete(v, "schemaVersion")
			case "missing-key":
				delete(O(v["security"]), "credentialKey")
			case "wrong-key":
				state := M{"installationId": "ins_original", "binding": nil}
				cipher, err := platformCrypt(freshConfig(), "ins_original", 1, JSON(state), false)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = s.DB.Exec("INSERT INTO platform_session VALUES(1,?,?)", "ins_original", cipher); err != nil {
					t.Fatal(err)
				}
			case "future-db":
				s.DB.Exec("PRAGMA user_version=99")
			case "foreign-db":
				s.DB.Exec("PRAGMA application_id=42")
			case "spoofed-current-db":
				s.DB.Exec("PRAGMA user_version=2")
			case "extra-index":
				s.DB.Exec("CREATE INDEX unexpected ON tasks(status)")
			}
			if err := AtomicWrite(path, JSON(v), 0600); err != nil {
				t.Fatal(err)
			}
			s.Close()
			raw, dbRaw := migrationRead(t, path), migrationRead(t, s.Path)
			backup := filepath.Join(t.TempDir(), "unused")
			if err := MigrateCommand(path, []string{"--backup", backup}, io.Discard); err == nil {
				t.Fatal("accepted", kind)
			}
			if _, err := os.Stat(backup); !os.IsNotExist(err) || !bytes.Equal(raw, migrationRead(t, path)) || !bytes.Equal(dbRaw, migrationRead(t, s.Path)) {
				t.Fatal("rejected migration changed inputs", kind)
			}
		})
	}
}

func TestMigrationDDLFailureRollsBackEverything(t *testing.T) {
	_, v, s := legacyMigrationFixture(t)
	// Simulate an on-disk violation: the second step fails after ALTER TABLE.
	// The public command rejects it at quick_check; the step must also be atomic.
	if _, err := s.DB.Exec("PRAGMA ignore_check_constraints=ON; INSERT INTO mcp_catalog VALUES('broken','fp','not-json',0)"); err != nil {
		t.Fatal(err)
	}
	if err := migrateDatabase(s, v, 1); err == nil {
		t.Fatal("migration unexpectedly succeeded")
	}
	version, err := inspectDatabase(s.DB)
	if err != nil || version != 1 {
		t.Fatal("partial DDL/version committed", version, err)
	}
	var raw string
	if err = s.DB.QueryRow("SELECT catalog_json FROM mcp_catalog WHERE resource_key='broken'").Scan(&raw); err != nil || raw != "not-json" {
		t.Fatal("failed migration lost data", raw, err)
	}
}

func TestMigrationBackupFailureAndConfigurationCAS(t *testing.T) {
	for _, kind := range []string{"existing", "inside-data", "external-edit"} {
		t.Run(kind, func(t *testing.T) {
			path, v, s := legacyMigrationFixture(t)
			s.Close()
			before := migrationRead(t, s.Path)
			backup := t.TempDir()
			var output io.Writer = io.Discard
			if kind == "inside-data" {
				backup = filepath.Join(filepath.Dir(s.Path), "backup")
			} else if kind == "external-edit" {
				backup = filepath.Join(t.TempDir(), "backup")
				output = migrationEditWriter{func() {
					O(v["server"])["port"] = 1234
					if err := AtomicWrite(path, JSON(v), 0600); err != nil {
						t.Fatal(err)
					}
				}}
			}
			if err := MigrateCommand(path, []string{"--backup", backup}, output); err == nil {
				t.Fatal("migration did not fail", kind)
			}
			if !bytes.Equal(before, migrationRead(t, s.Path)) {
				t.Fatal("failed preflight changed database")
			}
		})
	}
}

type migrationEditWriter struct{ edit func() }

func (w migrationEditWriter) Write(p []byte) (int, error) { w.edit(); return len(p), nil }

func TestStoreEscapesFilenameAndRejectsForeignKeys(t *testing.T) {
	name := "含空格 # & 数据.db"
	if os.PathSeparator != '\\' {
		name = "含空格 ?mode=memory#data.db"
	}
	path := filepath.Join(t.TempDir(), name)
	s, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = os.Stat(path); err != nil {
		t.Fatal("wrong URI filename", err)
	}
	if _, err = s.DB.Exec("PRAGMA foreign_keys=OFF; INSERT INTO messages VALUES('missing',0,0,'{}')"); err != nil {
		t.Fatal(err)
	}
	if err = s.Check(); err == nil {
		t.Fatal("foreign key violation accepted")
	}
}

func TestMaintenanceLockProcessAndCrashRelease(t *testing.T) {
	if path := os.Getenv("TUCK_LOCK_TEST"); path != "" {
		lock, err := acquireInstanceLock(path)
		if err != nil {
			t.Fatal(err)
		}
		defer lock.Close()
		os.Stdout.WriteString("locked\n")
		io.Copy(io.Discard, os.Stdin)
		return
	}
	path := filepath.Join(t.TempDir(), "instance.lock")
	cmd := exec.Command(os.Args[0], "-test.run=^TestMaintenanceLockProcessAndCrashRelease$")
	cmd.Env = append(os.Environ(), "TUCK_LOCK_TEST="+path)
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil || line != "locked\n" {
		t.Fatal("child did not acquire lock", line, err)
	}
	if lock, err := acquireInstanceLock(path); err == nil {
		lock.Close()
		t.Fatal("second process acquired occupied lock")
	}
	cmd.Process.Kill()
	cmd.Wait()
	lock, err := acquireInstanceLock(path)
	if err != nil {
		t.Fatal("crashed owner stranded lock", err)
	}
	lock.Close()
}

func TestPasswordMaintenanceHonorsVersionValidationAndLocks(t *testing.T) {
	path, _, s := legacyMigrationFixture(t)
	s.Close()
	before := migrationRead(t, path)
	if err := PasswordCommand(path, []string{"settings", "--reset"}, nil, io.Discard); err == nil || !bytes.Equal(before, migrationRead(t, path)) {
		t.Fatal("password command edited a legacy format", err)
	}
	if err := MigrateCommand(path, []string{"--backup", filepath.Join(t.TempDir(), "backup")}, io.Discard); err != nil {
		t.Fatal(err)
	}
	lock, err := acquireInstanceLock(path + ".lock")
	if err != nil {
		t.Fatal(err)
	}
	if err = PasswordCommand(path, []string{"settings", "--reset"}, nil, io.Discard); Status(err) != 409 {
		t.Fatal("password command bypassed service lock", err)
	}
	if err = MigrateCommand(path, []string{"--check"}, io.Discard); Status(err) != 409 {
		t.Fatal("migration bypassed service lock", err)
	}
	lock.Close()
	c, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	v, revision, _ := c.Read()
	hash, err := PasswordHash(testPassword, 12)
	if err != nil {
		t.Fatal(err)
	}
	O(v["server"])["listenHost"] = "0.0.0.0"
	O(O(v["security"])["auth"])["enabled"] = true
	O(O(v["security"])["auth"])["passwordHash"] = hash
	if _, err = c.Save(revision, v); err != nil {
		t.Fatal(err)
	}
	before = migrationRead(t, path)
	if err = PasswordCommand(path, []string{"login"}, strings.NewReader("\n"), io.Discard); err == nil || !bytes.Equal(before, migrationRead(t, path)) {
		t.Fatal("password command disabled external listener protection", err)
	}
}

func TestApplicationOwnsConfigurationAndDataUntilClosed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tuck.config.json")
	if err := InitConfig(path); err != nil {
		t.Fatal(err)
	}
	c, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	v, revision, _ := c.Read()
	O(v["server"])["port"] = 0
	if _, err = c.Save(revision, v); err != nil {
		t.Fatal(err)
	}
	c.MarkApplied()
	app, err := NewApp(c)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close(context.Background())
	if other, err := NewApp(c); Code(err) != "instance_in_use" {
		if other != nil {
			other.Close(context.Background())
		}
		t.Fatal("second app bypassed config ownership", err)
	}
	otherPath := filepath.Join(dir, "other.json")
	if err = AtomicWrite(otherPath, JSON(v), 0600); err != nil {
		t.Fatal(err)
	}
	otherConfig, err := LoadConfig(otherPath)
	if err != nil {
		t.Fatal(err)
	}
	if other, err := NewApp(otherConfig); Code(err) != "instance_in_use" {
		if other != nil {
			other.Close(context.Background())
		}
		t.Fatal("second config bypassed data ownership", err)
	}
	if err = MigrateCommand(path, []string{"--check"}, io.Discard); Code(err) != "instance_in_use" {
		t.Fatal("maintenance raced a live app", err)
	}
	if err = app.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = MigrateCommand(path, []string{"--check"}, io.Discard); err != nil {
		t.Fatal("close did not release ownership", err)
	}
}

func TestPersistenceNeverWritesContentItCannotReadBack(t *testing.T) {
	c, s, _, _ := securityFixture(t, false, nil)
	before := migrationRead(t, c.Path)
	v, revision, _ := c.Read()
	for _, name := range []string{"root", "worker", "reviewer", "extra_one", "extra_two"} {
		a := Clone(O(O(v["agents"])["root"]))
		a["instruction"] = strings.Repeat("x", 1000000)
		delete(a, "sub_agents")
		O(v["agents"])[name] = a
	}
	if err := ValidateConfig(v, false); err != nil {
		t.Fatal("fixture should be structurally valid", err)
	}
	if _, err := c.Save(revision, v); err == nil || !bytes.Equal(before, migrationRead(t, c.Path)) {
		t.Fatal("oversized configuration persisted", err)
	}
	platform, err := NewPlatform(s, c)
	if err != nil {
		t.Fatal(err)
	}
	defer platform.Close()
	var cipher []byte
	if err = s.DB.QueryRow("SELECT encrypted_payload FROM platform_session").Scan(&cipher); err != nil {
		t.Fatal(err)
	}
	state := platform.Data()
	state["binding"] = M{"oversized": strings.Repeat("x", 4<<20)}
	if err = platform.persist(state); err == nil {
		t.Fatal("unreadable platform state persisted")
	}
	var after []byte
	if err = s.DB.QueryRow("SELECT encrypted_payload FROM platform_session").Scan(&after); err != nil || !bytes.Equal(cipher, after) {
		t.Fatal("failed platform write changed ciphertext", err)
	}
	if err = s.SaveCatalog("empty", "fp", nil); err != nil {
		t.Fatal("empty catalog must be an array", err)
	}
	tooLarge := []M{{"name": "large", "inputSchema": M{"type": "object", "description": strings.Repeat("x", maxCatalogJSON)}}}
	if err = s.SaveCatalog("empty", "fp", tooLarge); Code(err) != "persistent_json_too_large" {
		t.Fatal("unreadable catalog persisted", err)
	}
	if catalog, _, err := s.Catalog("empty", "fp"); err != nil || catalog == nil || len(catalog) != 0 {
		t.Fatal("failed catalog write lost previous validation", catalog, err)
	}
}

func TestStoreRejectsUnreadablePublicationAtomically(t *testing.T) {
	s, err := NewStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	task := BaseTask(ID(), UUID(), "original", "", "root", "agent")
	if err = s.SaveTask(task); err != nil {
		t.Fatal(err)
	}
	large := strings.Repeat("x", maxStoredJSON)
	changed := Clone(task)
	changed["title"] = "must rollback"
	if _, err = s.Publish(changed, M{"index": 0, "revision": 0}, M{"type": "native", "data": large}); Code(err) != "persistent_json_too_large" {
		t.Fatal("oversized event persisted", err)
	}
	if messages, err := s.Messages(S(task["id"]), 0, 10); err != nil || len(messages) != 0 {
		t.Fatal("event failure partially committed message", err)
	}
	if cursor, err := s.Cursor(); err != nil || cursor != 0 {
		t.Fatal("event failure changed cursor", cursor, err)
	}
	if err = s.SaveMessage(S(task["id"]), M{"index": 0, "revision": 0, "text": large}); Code(err) != "persistent_json_too_large" {
		t.Fatal("oversized message persisted", err)
	}
	changed["large"] = large
	if err = s.SaveTask(changed); Code(err) != "persistent_json_too_large" {
		t.Fatal("oversized task persisted", err)
	}
	if got, err := s.Task(S(task["id"])); err != nil || got["title"] != "original" {
		t.Fatal("failed publication changed task", err)
	}
}

func TestMigrationHistoryIsReadableThroughApplication(t *testing.T) {
	path, v, s := legacyMigrationFixture(t)
	O(v["server"])["port"] = 0
	if err := AtomicWrite(path, JSON(v), 0600); err != nil {
		t.Fatal(err)
	}
	task := BaseTask(ID(), UUID(), "migrated history", "", "root", "agent")
	if _, err := s.Publish(task, M{"id": S(task["id"]) + ":m-0", "index": 0, "revision": 4, "blocks": []any{}}, M{"type": "created", "task": task}); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if err := MigrateCommand(path, []string{"--backup", filepath.Join(t.TempDir(), "backup")}, io.Discard); err != nil {
		t.Fatal(err)
	}
	c, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	app, err := NewApp(c)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close(context.Background())
	go func() { _ = app.Server.Serve(app.listener) }()
	status, body, _ := requestApp(t, app, http.MethodGet, "/api/tasks/"+S(task["id"]), nil, nil)
	if status != 200 || S(O(body["task"])["title"]) != "migrated history" || O(body["task"])["nativeSessionId"] != task["nativeSessionId"] {
		t.Fatal("migrated task unavailable", status, body)
	}
	status, body, _ = requestApp(t, app, http.MethodGet, "/api/tasks/"+S(task["id"])+"/messages", nil, nil)
	if status != 200 || len(A(body["messages"])) != 1 || I(O(A(body["messages"])[0])["revision"]) != 4 {
		t.Fatal("migrated message unavailable", status, body)
	}
}

func TestApplicationRejectsConfigurationChangedBeforeOwnership(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tuck.config.json")
	if err := InitConfig(path); err != nil {
		t.Fatal(err)
	}
	c, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = PasswordCommand(path, []string{"settings"}, strings.NewReader("six-ok\n"), io.Discard); err != nil {
		t.Fatal(err)
	}
	app, err := NewApp(c)
	if app != nil {
		app.Close(context.Background())
	}
	if Code(err) != "settings_changed" {
		t.Fatal("stale configuration started", err)
	}
}

func TestMigrationBackupIncludesWALAndCanRestoreReleasedV1(t *testing.T) {
	path, _, s := legacyMigrationFixture(t)
	if _, err := s.DB.Exec("PRAGMA journal_mode=WAL"); err != nil {
		t.Fatal(err)
	}
	task := BaseTask(ID(), UUID(), "WAL history", "", "root", "agent")
	if _, err := s.Publish(task, M{"index": 0, "revision": 5}, M{"type": "state"}); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(s.Path + "-wal"); err != nil || info.Size() == 0 {
		t.Fatal("fixture did not leave committed WAL data", err)
	}
	backup := filepath.Join(t.TempDir(), "backup")
	if err := MigrateCommand(path, []string{"--backup", backup}, io.Discard); err != nil {
		t.Fatal(err)
	}
	s.Close()
	// Restore the migration inputs into another directory. There are no file
	// deliveries or workspace assets in this fixture, so these are all inputs.
	restored := t.TempDir()
	restoredConfig := filepath.Join(restored, "tuck.config.json")
	if err := os.WriteFile(restoredConfig, migrationRead(t, filepath.Join(backup, "config.json")), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(restored, "data"), 0700); err != nil {
		t.Fatal(err)
	}
	restoredDB := filepath.Join(restored, "data", "tuck.db")
	if err := os.WriteFile(restoredDB, migrationRead(t, filepath.Join(backup, "tuck.db")), 0600); err != nil {
		t.Fatal(err)
	}
	db, err := openDatabase(restoredDB, "ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	version, err := inspectDatabase(db)
	if err != nil || version != 1 {
		t.Fatal("released v1 backup was not restored", version, err)
	}
	old := &Store{DB: db}
	got, err := old.Message(S(task["id"]), 0)
	if err != nil || I(got["revision"]) != 5 {
		t.Fatal("backup missed WAL message", got, err)
	}
	if err = old.Check(); err != nil {
		t.Fatal(err)
	}
	if err = MigrateCommand(restoredConfig, []string{"--check"}, io.Discard); err != nil {
		t.Fatal("restored inputs cannot pass preflight", err)
	}
}

func TestMigrationLeavesEmptyDatabaseForInitialization(t *testing.T) {
	path, _, s := legacyMigrationFixture(t)
	s.Close()
	if err := os.Remove(s.Path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.Path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := MigrateCommand(path, []string{"--backup", filepath.Join(t.TempDir(), "backup")}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if len(migrationRead(t, s.Path)) != 0 {
		t.Fatal("migration initialized a genuinely empty database")
	}
	if _, err := LoadConfig(path); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
}

func TestRuntimeApplyCannotChangeStartupResourceOwnership(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tuck.config.json")
	if err := InitConfig(path); err != nil {
		t.Fatal(err)
	}
	c, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	base, revision, _ := c.Read()
	O(base["server"])["port"] = 0
	if _, err = c.Save(revision, base); err != nil {
		t.Fatal(err)
	}
	c.MarkApplied()
	app, err := NewApp(c)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close(context.Background())
	original := migrationRead(t, path)
	var cipher []byte
	if err = app.Store.DB.QueryRow("SELECT encrypted_payload FROM platform_session").Scan(&cipher); err != nil {
		t.Fatal(err)
	}
	task := BaseTask(ID(), UUID(), "active", "", "root", "agent")
	task["status"] = "running"
	runCtx, cancel := context.WithCancel(context.Background())
	run := &taskRun{cancel: cancel, done: make(chan struct{})}
	app.Manager.mu.Lock()
	app.Manager.tasks[S(task["id"])] = task
	app.Manager.runs[S(task["id"])] = run
	app.Manager.mu.Unlock()
	defer func() {
		app.Manager.mu.Lock()
		delete(app.Manager.runs, S(task["id"]))
		task["status"] = "idle"
		close(run.done)
		app.Manager.mu.Unlock()
		cancel()
		if err := AtomicWrite(path, original, 0600); err != nil {
			t.Error(err)
		}
	}()
	for _, kind := range []string{"server", "storage", "auth", "credentialKey", "engine-mode", "engine-version", "engine-binary", "engine-url"} {
		t.Run(kind, func(t *testing.T) {
			next := Clone(base)
			switch kind {
			case "server":
				O(next["server"])["port"] = 1234
			case "storage":
				O(next["storage"])["dataDir"] = "another-data"
			case "auth":
				O(O(next["security"])["auth"])["username"] = "another-user"
			case "credentialKey":
				O(next["security"])["credentialKey"] = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
			default:
				field := strings.TrimPrefix(kind, "engine-")
				O(next["engine"])[field] = map[string]string{"mode": "external", "version": "v1.151.0", "binary": "other-engine", "url": "http://127.0.0.1:12345"}[field]
			}
			raw := JSON(next)
			if err := AtomicWrite(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			_, revision, err := c.Read()
			if err != nil {
				t.Fatal(err)
			}
			ctx, stop := context.WithTimeout(context.Background(), time.Second)
			defer stop()
			request := browserRequest("PUT").WithContext(ctx)
			data := M{"config": M{}, "revision": revision, "interrupt": true}
			if err = app.saveRuntime(request, data, func() error { return nil }); Code(err) != "restart_required" {
				t.Fatal("startup resource edit did not require restart", err)
			}
			var after []byte
			if err = app.Store.DB.QueryRow("SELECT encrypted_payload FROM platform_session").Scan(&after); err != nil {
				t.Fatal(err)
			}
			if runCtx.Err() != nil || !app.Manager.Running(S(task["id"])) || !reflect.DeepEqual(c.Applied(), Clone(base)) || !bytes.Equal(cipher, after) || !bytes.Equal(raw, migrationRead(t, path)) || !c.Pending() {
				t.Fatal("rejected apply changed execution, applied resources, ciphertext or pending file")
			}
			// Saving for a later restart remains possible and cannot mark it applied.
			data["defer"] = true
			if err = app.saveRuntime(request, data, func() error { return nil }); err != nil || !c.Pending() || runCtx.Err() != nil {
				t.Fatal("deferred save applied startup resources", err)
			}
		})
	}
	hot := Clone(base)
	O(hot["engine"])["desktopProxy"] = !B(O(hot["engine"])["desktopProxy"])
	if err = c.RequireRuntimeCompatible(hot); err != nil {
		t.Fatal("rebuildable engine input incorrectly requires restart", err)
	}
}

func TestConfigurationDoesNotCoerceMalformedResourceSelections(t *testing.T) {
	for _, field := range []string{"platformModelSettings", "platformMcpTools", "engine.binary"} {
		for _, invalid := range []any{nil, true, []any{}, 42} {
			value := freshConfig()
			if field == "engine.binary" {
				O(value["engine"])["binary"] = invalid
			} else {
				value[field] = invalid
			}
			if err := ValidateConfig(value, false); err == nil {
				t.Fatalf("malformed %s was silently treated as an empty selection", field)
			}
		}
	}
	value := freshConfig()
	value["localMcp"] = make([]any, 101)
	if err := ValidateConfig(value, false); err == nil {
		t.Fatal("MCP limit bypassed by loading a file")
	}
}
