package tuck

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"unicode/utf8"
)

const MaxDeliveryFile int64 = 512 << 20
const MaxDeliveryBytes int64 = 2 << 30

var publicID = regexp.MustCompile(`^[0-9A-Za-z]{16}$`)

type Deliveries struct {
	Manager        *Manager
	Store          *Store
	Models         *Models
	Root           string
	Local          *LocalTools
	removeSnapshot func(*os.Root, string) error
}

func NewDeliveries(manager *Manager, store *Store, models *Models, root string) (*Deliveries, error) {
	if e := os.MkdirAll(root, 0700); e != nil {
		return nil, e
	}
	actual, e := filepath.EvalSymlinks(root)
	if e != nil {
		return nil, e
	}
	d := &Deliveries{Manager: manager, Store: store, Models: models, Root: actual}
	for _, name := range []string{".staging", "snapshots"} {
		path := filepath.Join(actual, name)
		if e = os.MkdirAll(path, 0700); e != nil {
			return nil, e
		}
		info, e := os.Lstat(path)
		if e != nil || info.Mode()&os.ModeSymlink != 0 {
			return nil, Problem(409, "snapshot_failed", "Delivery storage must not contain symbolic links.")
		}
	}
	if e = d.recover(); e != nil {
		return nil, e
	}
	return d, nil
}
func (d *Deliveries) CheckWorkspace(path string) error {
	if path == "" {
		return nil
	}
	actual, e := filepath.EvalSymlinks(path)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	if inside(actual, d.Root) || inside(d.Root, actual) {
		return Problem(422, "invalid_workspace", "The working directory must be separate from delivery storage.")
	}
	return nil
}
func (d *Deliveries) Available(task M) bool {
	if S(task["parentTaskId"]) != "" || d.Models == nil {
		return false
	}
	for _, v := range A(AssistantCatalog(d.Models.Config())["items"]) {
		a := O(v)
		if a["agent"] == task["agent"] {
			return a["deliveryTool"] != false
		}
	}
	return false
}
func (d *Deliveries) recover() error {
	root, e := os.OpenRoot(d.Root)
	if e != nil {
		return e
	}
	defer root.Close()
	entries, e := os.ReadDir(filepath.Join(d.Root, ".staging"))
	if e != nil {
		return e
	}
	for _, entry := range entries {
		if publicID.MatchString(entry.Name()) && entry.IsDir() && entry.Type()&os.ModeSymlink == 0 {
			if e = root.RemoveAll(filepath.Join(".staging", entry.Name())); e != nil {
				return e
			}
		}
	}
	tasks, e := os.ReadDir(filepath.Join(d.Root, "snapshots"))
	if e != nil {
		return e
	}
	for _, task := range tasks {
		if publicID.MatchString(task.Name()) && task.IsDir() && task.Type()&os.ModeSymlink == 0 {
			_ = d.cleanup(task.Name())
		}
	}
	for _, task := range d.Manager.List() {
		if S(task["workspaceMode"]) != "none" {
			if e = d.CheckWorkspace(S(task["workspace"])); e != nil {
				return e
			}
		}
		latest, e := d.Store.Delivery(S(task["id"]), "latest")
		if e != nil {
			return e
		}
		if latest != nil {
			if _, e = d.Manager.Update(S(task["id"]), func(t M) error { t["latestDelivery"] = deliveryProjection(latest); return nil }); e != nil {
				return e
			}
		}
	}
	return nil
}
func deliveryProjection(d M) M {
	summary := []rune(S(d["summary"]))
	if len(summary) > 800 {
		summary = summary[:800]
	}
	return M{"version": d["version"], "turnId": d["turnId"], "createdAt": d["createdAt"], "summary": string(summary), "fileCount": d["fileCount"], "totalBytes": d["totalBytes"]}
}
func deliveryScan(row interface{ Scan(...any) error }, files bool) (M, error) {
	var id, task, turn, summary, raw string
	var version, created, count, total int64
	e := row.Scan(&id, &task, &version, &turn, &created, &summary, &count, &total, &raw)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	d := M{"id": id, "taskId": task, "version": version, "turnId": turn, "createdAt": created, "summary": summary, "fileCount": count, "totalBytes": total}
	if files {
		v, e := DecodeJSON([]byte(raw), 1<<20)
		if e != nil {
			return nil, e
		}
		d["files"] = v
	}
	return d, nil
}
func (s *Store) Delivery(task, version string) (M, error) {
	query := "SELECT * FROM task_deliveries WHERE task_id=? AND version=?"
	if version == "latest" {
		return deliveryScan(s.DB.QueryRow("SELECT * FROM task_deliveries WHERE task_id=? ORDER BY version DESC LIMIT 1", task), true)
	}
	return deliveryScan(s.DB.QueryRow(query, task, version), true)
}
func (s *Store) Deliveries(task string, before int64, limit int) ([]M, error) {
	if before <= 0 {
		before = 9007199254740991
	}
	rows, e := s.DB.Query("SELECT * FROM task_deliveries WHERE task_id=? AND version<? ORDER BY version DESC LIMIT ?", task, before, limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	result := []M{}
	for rows.Next() {
		d, e := deliveryScan(rows, false)
		if e != nil {
			return nil, e
		}
		result = append(result, d)
	}
	return result, rows.Err()
}
func (d *Deliveries) cleanup(task string) error {
	if !publicID.MatchString(task) {
		return nil
	}
	directory := filepath.Join(d.Root, "snapshots", task)
	entries, e := os.ReadDir(directory)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	root, e := os.OpenRoot(d.Root)
	if e != nil {
		return e
	}
	defer root.Close()
	for _, entry := range entries {
		if !publicID.MatchString(entry.Name()) || !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		var count int
		if e = d.Store.DB.QueryRow("SELECT COUNT(*) FROM task_deliveries WHERE id=?", entry.Name()).Scan(&count); e != nil {
			return e
		}
		if count == 0 {
			remove := d.removeSnapshot
			if remove == nil {
				remove = (*os.Root).RemoveAll
			}
			if e = remove(root, filepath.Join("snapshots", task, entry.Name())); e != nil {
				return e
			}
		}
	}
	return nil
}

type deliverySource struct {
	file     *os.File
	info     os.FileInfo
	path     string
	metadata M
	remote   *davMetadata
}
type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(b []byte) (int, error) {
	if e := r.ctx.Err(); e != nil {
		return 0, e
	}
	return r.reader.Read(b)
}
func (d *Deliveries) Submit(ctx context.Context, turn *toolTurn, args M) (result M) {
	turn.deliveryMu.Lock()
	defer turn.deliveryMu.Unlock()
	sources := []deliverySource{}
	var staging, final string
	activeField := ""
	committed := false
	defer func() {
		for _, source := range sources {
			if source.file != nil {
				source.file.Close()
			}
		}
		root, e := os.OpenRoot(d.Root)
		if e == nil {
			defer root.Close()
			if staging != "" {
				root.RemoveAll(staging)
			}
			if final != "" && !committed {
				root.RemoveAll(final)
			}
		}
	}()
	fail := func(code, message string) M {
		if code == "insufficient_storage" || code == "persistence_failed" {
			d.Manager.mu.Lock()
			if task := d.Manager.tasks[turn.TaskID]; task != nil && S(task["turnId"]) == turn.TurnID {
				task["queuePaused"] = true
				task["lastFailure"] = M{"code": code, "message": "交付存储不可用，请检查磁盘与数据库后继续。"}
				_ = d.Manager.publishLocked(task, M{"type": "state", "task": task}, nil)
				if run := d.Manager.runs[turn.TaskID]; run != nil {
					run.cancel()
				}
			}
			d.Manager.mu.Unlock()
		}
		result := toolFailure(code, message)
		if activeField != "" {
			result["field"] = activeField
		}
		return result
	}
	if e := d.Local.current(turn); e != nil {
		return fail("tool_not_permitted", e.Error())
	}
	summary, ok := args["summary"].(string)
	if !ok || strings.TrimSpace(summary) == "" || utf8.RuneCountInString(summary) > 1000 || !noUnknown(args, "summary", "files") || args["files"] != nil && !isArray(args["files"]) {
		return fail("invalid_arguments", "Provide a nonempty summary of at most 1,000 characters. Optional files must be a list.")
	}
	if len(A(args["files"])) > 10 {
		return fail("invalid_arguments", fmt.Sprintf("Received %d files; maximum 10. Select the key files or package the necessary files into an archive.", len(A(args["files"]))))
	}
	var root *os.Root
	var e error
	hasLocal := false
	for _, v := range A(args["files"]) {
		ref, e := parseToolFile(v, true)
		if e != nil {
			return fail(Code(e), e.Error())
		}
		hasLocal = hasLocal || ref.Source == "local"
	}
	if hasLocal {
		if e := checkTurnWorkspace(turn); e != nil {
			return fail(Code(e), e.Error())
		}
		if turn.Workspace == "" {
			return fail("invalid_workspace", "The external runtime working directory is unknown.")
		}
		root, e = os.OpenRoot(turn.Workspace)
		if e != nil {
			return fail("file_not_readable", "The working directory is unavailable.")
		}
		defer root.Close()
	}
	seen := map[string]bool{}
	var total int64
	for index, v := range A(args["files"]) {
		entry := O(v)
		activeField = fmt.Sprintf("files[%d].path", index)
		if !noUnknown(entry, "source", "path", "description") || !stringField(entry["path"], 1, 4096) || entry["description"] != nil && !stringField(entry["description"], 1, 1000) {
			return fail("invalid_arguments", "Each file needs a relative path and optional nonempty description.")
		}
		ref, e := parseToolFile(entry, true)
		if e != nil {
			return fail(Code(e), e.Error())
		}
		if ref.Source == "runcove" {
			if e = d.Local.remoteGate(ctx, turn); e != nil {
				return fail(Code(e), e.Error())
			}
			meta, e := d.Local.metadata(ctx, turn, ref.Path, false)
			if e != nil {
				return fail(Code(e), e.Error())
			}
			if meta.Kind != "file" || meta.Bytes < 0 || !strongETag(meta.ETag) {
				return fail("file_not_readable", "A remote delivery file is not readable or has no verifiable version.")
			}
			if meta.Bytes > MaxDeliveryFile || total+meta.Bytes > MaxDeliveryBytes {
				return fail("delivery_too_large", "Remote files exceed the delivery size limits.")
			}
			key := "runcove:" + ref.Path
			if seen[key] {
				return fail("invalid_arguments", "List each file once.")
			}
			seen[key] = true
			total += meta.Bytes
			metadata := M{"id": ID(), "path": ref.Path, "sizeBytes": meta.Bytes, "mimeType": MIMEType(ref.Path)}
			if entry["description"] != nil {
				metadata["description"] = strings.TrimSpace(S(entry["description"]))
			}
			sources = append(sources, deliverySource{path: ref.Path, metadata: metadata, remote: meta})
			continue
		}
		path, e := relativeFile(S(entry["path"]), true)
		if e != nil {
			return fail("invalid_arguments", e.Error())
		}
		parent := ""
		for _, part := range strings.Split(filepath.ToSlash(path), "/") {
			parent = filepath.Join(parent, part)
			info, e := root.Lstat(parent)
			if e != nil {
				return fail("file_not_found", "A listed file does not exist.")
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return fail("path_outside_workspace", "Delivery paths must not contain symbolic links or directory junctions.")
			}
		}
		handle, e := root.Open(path)
		if e != nil {
			return fail("file_not_readable", "A listed file cannot be read.")
		}
		info, e := handle.Stat()
		if e != nil || !info.Mode().IsRegular() {
			handle.Close()
			return fail("invalid_arguments", "Delivery entries must be readable regular files.")
		}
		if info.Size() > MaxDeliveryFile {
			handle.Close()
			return fail("file_too_large", "Each delivery file must be at most 512 MiB.")
		}
		total += info.Size()
		if total > MaxDeliveryBytes {
			handle.Close()
			return fail("delivery_too_large", "The delivery files must total at most 2 GiB.")
		}
		key := path
		if runtime.GOOS == "windows" {
			key = strings.ToLower(key)
		}
		if seen[key] {
			handle.Close()
			return fail("invalid_arguments", "List each file once.")
		}
		seen[key] = true
		for _, other := range sources {
			if os.SameFile(info, other.info) {
				handle.Close()
				return fail("invalid_arguments", "List each underlying file once.")
			}
		}
		metadata := M{"id": ID(), "path": filepath.ToSlash(path), "sizeBytes": info.Size(), "mimeType": MIMEType(path)}
		if entry["description"] != nil {
			metadata["description"] = strings.TrimSpace(S(entry["description"]))
		}
		sources = append(sources, deliverySource{file: handle, info: info, path: path, metadata: metadata})
	}
	free, e := availableBytes(d.Root)
	activeField = ""
	if e != nil || free < uint64(total)+(256<<20) {
		return fail("insufficient_storage", "Not enough free space to preserve this delivery.")
	}
	id := ID()
	staging = filepath.Join(".staging", id)
	storage, e := os.OpenRoot(d.Root)
	if e != nil {
		return fail("snapshot_failed", "Delivery storage unavailable.")
	}
	defer storage.Close()
	if e = storage.MkdirAll(filepath.Join(staging, "files"), 0700); e != nil {
		return fail("snapshot_failed", "Cannot create delivery snapshot.")
	}
	files := []any{}
	buffer := make([]byte, 128<<10)
	for index, source := range sources {
		activeField = fmt.Sprintf("files[%d].path", index)
		if ctx.Err() != nil {
			return fail("delivery_canceled", "The delivery was canceled before it was committed.")
		}
		target, e := storage.OpenFile(filepath.Join(staging, "files", S(source.metadata["id"])), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return fail("snapshot_failed", "Cannot create delivery file.")
		}
		hash := sha256.New()
		var reader io.Reader = source.file
		size := int64(N(source.metadata["sizeBytes"]))
		var remote io.ReadCloser
		if source.remote != nil {
			body, meta, err := d.Local.openRemoteFile(ctx, turn, source.path, MaxDeliveryFile)
			if err != nil {
				target.Close()
				return fail(Code(err), err.Error())
			}
			remote = body
			if meta.ETag != source.remote.ETag || meta.Bytes != size {
				body.Close()
				target.Close()
				return fail("file_changed", "A remote file changed before copying.")
			}
			reader = body
		}
		n, e := io.CopyBuffer(io.MultiWriter(target, hash), io.LimitReader(contextReader{ctx, reader}, size+1), buffer)
		if remote != nil {
			remote.Close()
		}
		if e == nil {
			e = target.Sync()
		}
		target.Close()
		if e != nil {
			if ctx.Err() != nil {
				return fail("delivery_canceled", "The delivery was canceled before it was committed.")
			}
			return fail("snapshot_failed", "Delivery file copy failed.")
		}
		if n != size {
			return fail("file_changed", "A file changed while copying.")
		}
		if source.remote != nil {
			if e = d.Local.remoteGate(ctx, turn); e != nil {
				return fail(Code(e), e.Error())
			}
		} else {
			after, statErr := source.file.Stat()
			disk, sourceErr := root.Lstat(source.path)
			if statErr != nil || sourceErr != nil || after.Size() != size || !after.ModTime().Equal(source.info.ModTime()) || !os.SameFile(source.info, disk) || disk.Mode()&os.ModeSymlink != 0 {
				return fail("file_changed", "A file changed while copying. Wait for its writer to finish.")
			}
		}

		source.metadata["sha256"] = hex.EncodeToString(hash.Sum(nil))
		files = append(files, source.metadata)
	}
	if e = d.Local.current(turn); e != nil {
		return fail("tool_not_permitted", e.Error())
	}
	activeField = ""
	if ctx.Err() != nil {
		return fail("delivery_canceled", "The delivery was canceled before it was committed.")
	}
	if e = syncDirectory(filepath.Join(d.Root, staging, "files")); e != nil {
		return fail("snapshot_failed", e.Error())
	}
	if e = syncDirectory(filepath.Join(d.Root, staging)); e != nil {
		return fail("snapshot_failed", e.Error())
	}
	final = filepath.Join("snapshots", turn.TaskID, id)
	if e = storage.MkdirAll(filepath.Dir(final), 0700); e != nil {
		return fail("snapshot_failed", e.Error())
	}
	if e = storage.Rename(staging, final); e != nil {
		return fail("snapshot_failed", e.Error())
	}
	staging = ""
	if e = syncDirectory(filepath.Join(d.Root, filepath.Dir(final))); e != nil {
		return fail("snapshot_failed", e.Error())
	}
	for _, parent := range []string{"snapshots", ".staging"} {
		if e = syncDirectory(filepath.Join(d.Root, parent)); e != nil {
			return fail("snapshot_failed", e.Error())
		}
	}
	value := M{"id": id, "taskId": turn.TaskID, "turnId": turn.TurnID, "createdAt": NowMS(), "summary": strings.TrimSpace(summary), "fileCount": len(files), "totalBytes": total, "files": files}
	saved, e := d.commit(ctx, turn, value)
	if e != nil {
		if errors.Is(e, context.Canceled) || errors.Is(e, context.DeadlineExceeded) || Code(e) == "delivery_canceled" {
			return fail("delivery_canceled", "The delivery was canceled before it was committed.")
		}
		return fail("persistence_failed", "Delivery could not be saved. Check files and storage before continuing.")
	}
	committed = true
	result = M{"status": "completed", "delivery_id": saved["id"], "version": saved["version"], "turn_id": saved["turnId"], "file_count": saved["fileCount"], "total_bytes": saved["totalBytes"], "view_url": "#/chat/conversations/" + turn.TaskID + "/delivery?version=" + fmt.Sprint(I(saved["version"]))}
	if e = d.cleanup(turn.TaskID); e != nil {
		result["cleanup_pending"] = true
	}
	return result
}
func (d *Deliveries) commit(ctx context.Context, turn *toolTurn, value M) (M, error) {
	manager := d.Manager
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	task := Clone(manager.tasks[turn.TaskID])
	if task == nil || S(task["turnId"]) != turn.TurnID || turn.ctx.Err() != nil {
		return nil, Problem(409, "delivery_canceled", "Delivery context expired")
	}
	keep := I(O(d.Models.Config()["workbench"])["deliveryRetentionCount"])
	if !Has(O(d.Models.Config()["workbench"]), "deliveryRetentionCount") {
		keep = 10
	}
	for _, v := range A(O(d.Models.Config()["workbench"])["assistants"]) {
		a := O(v)
		if a["agent"] == task["agent"] && Has(a, "deliveryRetentionCount") {
			keep = I(a["deliveryRetentionCount"])
		}
	}
	e := d.Store.WriteContext(ctx, func(tx *sql.Tx) error {
		var version int64
		if e := tx.QueryRow("SELECT COALESCE(MAX(version),0)+1 FROM task_deliveries WHERE task_id=?", turn.TaskID).Scan(&version); e != nil {
			return e
		}
		value["version"] = version
		if _, e := tx.Exec("INSERT INTO task_deliveries VALUES(?,?,?,?,?,?,?,?,?)", S(value["id"]), turn.TaskID, version, turn.TurnID, value["createdAt"], S(value["summary"]), I(value["fileCount"]), value["totalBytes"], string(JSON(value["files"]))); e != nil {
			return e
		}
		if _, e := tx.Exec("DELETE FROM task_deliveries WHERE task_id=? AND id NOT IN(SELECT id FROM task_deliveries WHERE task_id=? ORDER BY version DESC LIMIT ?)", turn.TaskID, turn.TaskID, max(1, keep)); e != nil {
			return e
		}
		task["latestDelivery"] = deliveryProjection(value)
		if e := saveTask(tx, task); e != nil {
			return e
		}
		event := M{"type": "state", "task": task}
		raw, e := storedJSON(event, maxStoredJSON)
		if e != nil {
			return e
		}
		_, e = tx.Exec("INSERT INTO events(task_id,type,created_at_ms,payload_json) VALUES(?,?,?,?)", turn.TaskID, "state", NowMS(), raw)
		return e
	})
	if e != nil {
		return nil, e
	}
	manager.tasks[turn.TaskID] = task
	manager.notifyLocked()
	return Clone(value), nil
}
func (d *Deliveries) ServeFile(w http.ResponseWriter, r *http.Request, taskID, version, fileID string) error {
	delivery, e := d.Store.Delivery(taskID, version)
	if e != nil {
		return e
	}
	if delivery == nil {
		return Problem(404, "delivery_not_found", "尚未提交此版本的交付")
	}
	var file M
	for _, v := range A(delivery["files"]) {
		if S(O(v)["id"]) == fileID {
			file = O(v)
		}
	}
	if file == nil {
		return Problem(404, "delivery_file_not_found", "交付文件不存在")
	}
	root, e := os.OpenRoot(d.Root)
	if e != nil {
		return e
	}
	defer root.Close()
	handle, e := root.Open(filepath.Join("snapshots", taskID, S(delivery["id"]), "files", fileID))
	if os.IsNotExist(e) {
		return Problem(410, "snapshot_missing", "交付文件副本缺失，摘要和清单仍保留")
	}
	if e != nil {
		return e
	}
	defer handle.Close()
	info, e := handle.Stat()
	if e != nil {
		return e
	}
	if info.Size() != int64(N(file["sizeBytes"])) {
		return Problem(409, "snapshot_damaged", "交付文件已损坏")
	}
	hash := sha256.New()
	if _, e = io.Copy(hash, contextReader{r.Context(), handle}); e != nil {
		return e
	}
	if hex.EncodeToString(hash.Sum(nil)) != S(file["sha256"]) {
		return Problem(409, "snapshot_damaged", "交付文件校验失败")
	}
	if _, e = handle.Seek(0, 0); e != nil {
		return e
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(filepath.Base(S(file["path"]))))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, filepath.Base(S(file["path"])), info.ModTime(), handle)
	return nil
}
