package tuck

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
)

// This is an offline audit, not startup work. SQL detects divergent projections
// without decoding complete task/event histories into memory. Native event
// payloads and user workspace contents remain opaque.
func checkPersistentData(db *sql.DB, data string, version int) error {
	if version == 0 {
		return nil
	}
	for _, query := range []string{
		`SELECT 1 FROM tasks WHERE length(CAST(payload_json AS BLOB))>? OR json_extract(payload_json,'$.id') IS NOT id OR json_extract(payload_json,'$.status') IS NOT status OR json_extract(payload_json,'$.archived') IS NOT archived OR COALESCE(json_extract(payload_json,'$.nativeSessionId'),'') IS NOT COALESCE(native_session_id,'') LIMIT 1`,
		`SELECT 1 FROM messages WHERE length(CAST(payload_json AS BLOB))>? OR json_extract(payload_json,'$.index') IS NOT position OR json_extract(payload_json,'$.revision') IS NOT revision LIMIT 1`,
		`SELECT 1 FROM events WHERE length(CAST(payload_json AS BLOB))>? OR json_extract(payload_json,'$.type') IS NOT type LIMIT 1`,
	} {
		var found int
		err := db.QueryRow(query, maxStoredJSON).Scan(&found)
		if err == nil {
			return Problem(409, "persistent_data", "持久 JSON 超出读取上限或与类型列不一致；请保留原库并核对匹配备份")
		}
		if err != sql.ErrNoRows {
			return err
		}
	}
	rows, err := db.Query("SELECT task_id,id,file_count,total_bytes,files_json FROM task_deliveries ORDER BY task_id,version")
	if err != nil {
		return err
	}
	defer rows.Close()
	var root *os.Root
	defer func() {
		if root != nil {
			root.Close()
		}
	}()
	for rows.Next() {
		var task, delivery, raw string
		var count int
		var total int64
		if err = rows.Scan(&task, &delivery, &count, &total, &raw); err != nil {
			return err
		}
		value, err := DecodeJSON([]byte(raw), 1<<20)
		if err != nil || !publicID.MatchString(task) || !publicID.MatchString(delivery) || !isArray(value) || len(A(value)) != count {
			return Problem(409, "persistent_delivery", "交付持久标识或清单无效")
		}
		seen := map[string]bool{}
		var size int64
		for _, item := range A(value) {
			file := O(item)
			id := S(file["id"])
			digest, err := hex.DecodeString(S(file["sha256"]))
			if !publicID.MatchString(id) || seen[id] || !Integer(file["sizeBytes"], 0, int(MaxDeliveryFile)) || err != nil || len(digest) != sha256.Size {
				return Problem(409, "persistent_delivery", "交付文件元数据无效")
			}
			seen[id] = true
			if root == nil {
				root, err = os.OpenRoot(filepath.Join(data, "deliveries"))
				if err != nil {
					return Problem(409, "snapshot_missing", "交付快照目录缺失，请恢复匹配的文件备份")
				}
			}
			f, err := root.Open(filepath.Join("snapshots", task, delivery, "files", id))
			if err != nil {
				return Problem(409, "snapshot_missing", "已提交的交付文件缺失，请恢复匹配的文件备份")
			}
			info, err := f.Stat()
			if err != nil || !info.Mode().IsRegular() || info.Size() != int64(N(file["sizeBytes"])) {
				f.Close()
				return Problem(409, "snapshot_damaged", "交付文件类型或大小不匹配")
			}
			hash := sha256.New()
			n, err := io.Copy(hash, io.LimitReader(f, info.Size()+1))
			f.Close()
			if err != nil || n != info.Size() || hex.EncodeToString(hash.Sum(nil)) != S(file["sha256"]) {
				return Problem(409, "snapshot_damaged", "交付文件校验失败")
			}
			size += n
		}
		if size != total {
			return Problem(409, "persistent_delivery", "交付字节总数与清单不一致")
		}
	}
	return rows.Err()
}
