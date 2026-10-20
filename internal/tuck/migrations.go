package tuck

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Configuration and database versions evolve independently. No step guesses a
// legacy format or regenerates a missing encryption key. Database version owns
// SQL, product JSON and referenced file layout; platform format owns its cipher.
func migrateConfiguration(v M) (M, error) {
	if !Integer(v["schemaVersion"], 1, configVersion) {
		return nil, Problem(409, "config_version", "只支持正式 Go v1/v2/v3 配置；未知或封存格式不能迁移")
	}
	next := Clone(v)
	if I(next["schemaVersion"]) == 1 {
		// Released 1 -> 2 step: retain the key and every setting, and enforce
		// the key requirement below. Future steps must be explicit here.
		next["schemaVersion"] = 2
	}
	if I(next["schemaVersion"]) == 2 {
		renames := [][2]string{{"list_sessions", "list_conversations"}, {"send_session_message", "send_conversation_message"}}
		for _, value := range O(next["agents"]) {
			agent := O(value)
			for i, name := range A(agent["disabledTuckTools"]) {
				for _, rename := range renames {
					if name == rename[0] {
						A(agent["disabledTuckTools"])[i] = rename[1]
					}
				}
			}
		}
		// Retain the original patterns for native/external tools, and carry
		// their policy to the renamed Tuck tools without widening the glob.
		permissions := O(next["permissions"])
		for _, level := range []string{"allow", "ask", "deny"} {
			for _, value := range A(permissions[level]) {
				pattern := S(value)
				name, _, _ := strings.Cut(pattern, ":")
				for _, rename := range renames {
					matchesOld, _ := filepath.Match(strings.ToLower(name), rename[0])
					matchesNew, _ := filepath.Match(strings.ToLower(name), rename[1])
					replacement := rename[1] + pattern[len(name):]
					if rename[0] == "send_session_message" {
						replacement = strings.ReplaceAll(replacement, ":target_task_id=", ":target_conversation_id=")
					}
					if matchesOld && (!matchesNew || replacement != rename[1]+pattern[len(name):]) && !Contains(permissions[level], replacement) {
						permissions[level] = append(A(permissions[level]), replacement)
					}
				}
			}
		}
		next["schemaVersion"] = 3
	}
	if err := ValidateConfig(next, false); err != nil {
		return nil, err
	}
	if I(v["schemaVersion"]) != configVersion {
		if _, err := marshalConfig(next); err != nil {
			return nil, err
		}
	}
	return next, nil
}

func checkPlatformPersistence(db *sql.DB, config M, version int) error {
	if version == 0 {
		return nil
	}
	var installation string
	var encrypted []byte
	format := 1
	query := "SELECT installation_id,encrypted_payload FROM platform_session WHERE id=1"
	var err error
	if version == 1 {
		err = db.QueryRow(query).Scan(&installation, &encrypted)
	} else {
		err = db.QueryRow("SELECT installation_id,encrypted_payload,format_version FROM platform_session WHERE id=1").Scan(&installation, &encrypted, &format)
		if err == nil && format != platformFormatVersion {
			return Problem(409, "platform_version", "平台持久格式不受支持")
		}
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	_, _, err = decryptPlatformState(config, installation, format, encrypted)
	return err
}

// Transaction covers DDL, ciphertext and user_version. Product rows, event
// sequences (including sqlite_sequence), delivery IDs/versions and bytes stay
// untouched. Invalid/oversized reconstructable MCP catalogs are discarded.
func migrateDatabase(s *Store, config M, expected int) error {
	return s.Write(func(tx *sql.Tx) error {
		version, err := inspectDatabase(tx)
		if err != nil {
			return err
		}
		if version != expected {
			return Problem(409, "database_changed", "数据库在迁移检查后发生变化")
		}
		if version == databaseVersion {
			return nil
		}
		if version == 0 {
			if _, err = tx.Exec(schemaV1); err != nil {
				return err
			}
		}
		var installation string
		var encrypted []byte
		err = tx.QueryRow("SELECT installation_id,encrypted_payload FROM platform_session WHERE id=1").Scan(&installation, &encrypted)
		var next []byte
		if err == nil {
			plain, _, e := decryptPlatformState(config, installation, 1, encrypted)
			if e != nil {
				return Problem(409, "credential_key", "平台凭据无法解密，迁移已回滚")
			}
			next, err = platformCrypt(config, installation, platformFormatVersion, plain, false)
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if _, err = tx.Exec(schemaV2); err != nil {
			return err
		}
		if next != nil {
			if _, err = tx.Exec("UPDATE platform_session SET encrypted_payload=?,format_version=? WHERE id=1", next, platformFormatVersion); err != nil {
				return err
			}
		}
		return nil
	})
}

func MigrateCommand(path string, args []string, output io.Writer) error {
	check := len(args) == 1 && args[0] == "--check"
	backup := ""
	if len(args) == 2 && args[0] == "--backup" {
		backup = args[1]
	}
	if !check && backup == "" {
		return fmt.Errorf("usage: tuck migrate --check | tuck migrate --backup <new-directory> [--config path]; stop Tuck and its engine first")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	lock, err := acquireInstanceLock(abs + ".lock")
	if err != nil {
		return err
	}
	defer lock.Close()
	raw, err := os.ReadFile(abs)
	if err != nil {
		return err
	}
	v, err := DecodeJSON(raw, 4<<20)
	if err != nil {
		return fmt.Errorf("invalid configuration JSON")
	}
	next, err := migrateConfiguration(O(v))
	if err != nil {
		return err
	}
	config := &Config{Path: abs, applied: next}
	data := config.DataDir()
	dbPath := filepath.Join(data, "tuck.db")
	version := 0
	var db *sql.DB
	if _, err = os.Stat(data); err == nil {
		dataLock, e := acquireInstanceLock(filepath.Join(data, ".tuck.lock"))
		if e != nil {
			return e
		}
		defer dataLock.Close()
	} else if !os.IsNotExist(err) {
		return err
	}
	if _, err = os.Stat(dbPath); err == nil {
		db, err = openDatabase(dbPath, "ro")
		if err != nil {
			return err
		}
		defer db.Close()
		version, err = inspectDatabase(db)
		if err != nil {
			return err
		}
		if err = (&Store{DB: db}).Check(); err != nil {
			return err
		}
		if err = checkPlatformPersistence(db, next, version); err != nil {
			return err
		}
		if err = checkPersistentData(db, data, version); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	fmt.Fprintf(output, "Configuration v%d -> v%d; database v%d -> v%d; platform format -> v%d\n", I(O(v)["schemaVersion"]), configVersion, version, databaseVersion, platformFormatVersion)
	if check {
		fmt.Fprintln(output, "Check passed. No configuration, schema, ciphertext or workspace data changed. Apply with --backup <new-directory> after stopping Tuck and its engine.")
		return nil
	}
	if I(O(v)["schemaVersion"]) == configVersion && (version == 0 || version == databaseVersion) {
		fmt.Fprintln(output, "Already current; no backup or changes needed (an absent/empty database is initialized on first start).")
		return nil
	}
	backup, err = filepath.Abs(backup)
	if err != nil {
		return err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(backup))
	if err != nil {
		return err
	}
	backup = filepath.Join(parent, filepath.Base(backup))
	actualData := data
	if resolved, e := filepath.EvalSymlinks(data); e == nil {
		actualData = resolved
	}
	// The backup must survive replacement of the entire data directory.
	if inside(actualData, backup) || backup == abs {
		return fmt.Errorf("backup must be a new directory outside storage.dataDir")
	}
	if err = os.Mkdir(backup, 0700); err != nil {
		return err
	}
	// A failed backup remains visibly incomplete; it is never used or overwritten.
	if err = AtomicWrite(filepath.Join(backup, "config.json"), raw, 0600); err != nil {
		return err
	}
	manifest := M{"formatVersion": 1, "scope": "migration-inputs", "createdAt": NowISO(), "configPath": abs, "configHash": Hash(raw), "databasePath": dbPath, "databasePresent": db != nil, "configVersion": I(O(v)["schemaVersion"]), "databaseVersion": version, "targetConfigVersion": configVersion, "targetDatabaseVersion": databaseVersion, "filesChanged": false}
	if db != nil {
		copyPath := filepath.Join(backup, "tuck.db")
		if _, err = db.Exec("VACUUM INTO ?", copyPath); err != nil {
			return err
		}
		if err = syncBackupDatabase(copyPath); err != nil {
			return err
		}
		digest, e := FileHash(copyPath)
		if e != nil {
			return e
		}
		manifest["databaseHash"] = digest
		copyDB, e := openDatabase(copyPath, "ro")
		if e != nil {
			return e
		}
		e = (&Store{DB: copyDB}).Check()
		if e == nil {
			_, e = inspectDatabase(copyDB)
		}
		copyDB.Close()
		if e != nil {
			return e
		}
	}
	if err = AtomicWrite(filepath.Join(backup, "manifest.json"), append(JSON(manifest), '\n'), 0600); err != nil {
		return err
	}
	if err = syncDirectory(filepath.Dir(backup)); err != nil {
		return err
	}
	before, err := os.ReadFile(abs)
	if err != nil || !bytes.Equal(before, raw) {
		return Problem(409, "settings_changed", "配置在检查后发生变化，未执行迁移")
	}
	if db != nil && version > 0 {
		if err = db.Close(); err != nil {
			return err
		}
		writable, e := openDatabase(dbPath, "rw")
		if e != nil {
			return e
		}
		s := &Store{DB: writable, Path: dbPath}
		err = migrateDatabase(s, next, version)
		if err == nil {
			err = s.Check()
		}
		closeErr := s.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	// Commit the database first. A crash here leaves a supported mixed version;
	// rerunning this command checks v2 and completes only the remaining config.
	before, err = os.ReadFile(abs)
	if err != nil || !bytes.Equal(before, raw) {
		return Problem(409, "settings_changed", "数据库已迁移，配置发生变化；保留备份并重新运行 migrate --check 后续迁")
	}
	if I(O(v)["schemaVersion"]) != configVersion {
		formatted, e := marshalConfig(next)
		if e != nil {
			return e
		}
		if err = AtomicWrite(abs, formatted, 0600); err != nil {
			return fmt.Errorf("database migration committed; preserve backup and rerun migrate to finish configuration: %w", err)
		}
	}
	fmt.Fprintln(output, "Migration complete. Backup:", backup)
	fmt.Fprintln(output, "This backup covers changed inputs only; workspaces, delivery files and opaque engine data are unchanged. After starting the new program, rollback requires the matching full cold backup.")
	return nil
}

func syncBackupDatabase(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err = f.Chmod(0600); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}
