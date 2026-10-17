package tuck

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const MaxUploadBytes = 16 << 20

const TaskArtifactsDirectory = "artifacts"

// Each fixed artifact directory is opened beneath its verified parent. Reject
// links and substitutions so a type directory cannot redirect writes elsewhere.
func openArtifactRoot(task M, kind string) (*os.Root, error) {
	if e := writableTaskWorkspace(task); e != nil {
		return nil, e
	}
	root, e := taskRoot(task)
	if e != nil {
		return nil, e
	}
	for _, name := range []string{TaskArtifactsDirectory, kind} {
		if e = root.Mkdir(name, 0700); e != nil && !os.IsExist(e) {
			root.Close()
			return nil, e
		}
		info, err := root.Lstat(name)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			root.Close()
			return nil, Problem(409, "artifact_directory_changed", "产物目录必须是任务内的普通目录，不能使用链接")
		}
		next, err := root.OpenRoot(name)
		root.Close()
		if err != nil {
			return nil, err
		}
		opened, err := next.Stat(".")
		if err != nil || !os.SameFile(info, opened) {
			next.Close()
			return nil, Problem(409, "artifact_directory_changed", "产物目录已改变")
		}
		root = next
	}
	return root, nil
}

func checkArtifactRoot(task M, kind string, root *os.Root) error {
	workspace, e := filepath.EvalSymlinks(S(task["workspace"]))
	if e != nil {
		return e
	}
	expected := filepath.Join(workspace, TaskArtifactsDirectory, kind)
	actual, e := filepath.EvalSymlinks(expected)
	if e != nil || actual != expected {
		return Problem(409, "artifact_directory_changed", "产物目录已改变")
	}
	current, e := os.Stat(expected)
	opened, readErr := root.Stat(".")
	if e != nil || readErr != nil || !os.SameFile(current, opened) {
		return Problem(409, "artifact_directory_changed", "产物目录已改变")
	}
	return nil
}

// none may retain the engine's default directory for native reads, but Tuck
// must never treat it as an explicitly selected writable task workspace.
func writableTaskWorkspace(task M) error {
	if S(task["workspaceMode"]) == "none" || S(task["workspace"]) == "" {
		return Problem(409, "workspace_required", "此操作需要自动或指定的任务工作目录；无目录模式不允许 Tuck 写入引擎默认目录")
	}
	return nil
}

func MIMEType(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".mjs" || ext == ".js" {
		return "text/javascript"
	}
	v := mime.TypeByExtension(ext)
	if v == "" {
		v = "application/octet-stream"
	}
	return v
}
func AttachmentKind(path string) string {
	extension := strings.ToLower(filepath.Ext(path))
	switch extension {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		return "image"
	case ".mp3", ".wav", ".ogg", ".m4a":
		return "audio"
	case ".mp4", ".webm":
		return "video"
	}
	return "file"
}
func ImageType(data []byte) string {
	switch {
	case len(data) >= 8 && bytes.Equal(data[:8], []byte{137, 80, 78, 71, 13, 10, 26, 10}):
		return "image/png"
	case len(data) >= 3 && data[0] == 255 && data[1] == 216 && data[2] == 255:
		return "image/jpeg"
	case len(data) >= 6 && (string(data[:6]) == "GIF87a" || string(data[:6]) == "GIF89a"):
		return "image/gif"
	case len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		return "image/webp"
	}
	return ""
}
func inside(root, path string) bool {
	rel, e := filepath.Rel(root, path)
	return e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel)
}
func taskRoot(task M) (*os.Root, error) {
	if S(task["workspace"]) == "" {
		return nil, Problem(409, "workspace_unavailable", "外部运行时的默认目录未知，请指定目录后访问本地文件")
	}
	root, e := os.OpenRoot(S(task["workspace"]))
	if e != nil {
		return nil, e
	}
	return root, nil
}
func relativeFile(path string, strict bool) (string, error) {
	if path == "" || path == "." {
		return ".", nil
	}
	if filepath.IsAbs(path) || strings.ContainsRune(path, 0) {
		return "", Problem(403, "path_outside_workspace", "Resource is outside this task workspace")
	}
	if strict && (strings.Contains(path, "\\") || strings.ContainsAny(path, "<>:\"|?*\r\n")) {
		return "", Problem(400, "invalid_arguments", "Use a regular relative file path with forward slashes.")
	}
	clean := filepath.Clean(filepath.FromSlash(path))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", Problem(403, "path_outside_workspace", "Resource is outside this task workspace")
	}
	if strict {
		for _, part := range strings.Split(path, "/") {
			if part == "" || part == "." || part == ".." || strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") || regexp.MustCompile(`(?i)^(con|prn|aux|nul|com[1-9]|lpt[1-9])(?:\.|$)`).MatchString(part) {
				return "", Problem(400, "invalid_arguments", "Use a regular relative file path with forward slashes.")
			}
		}
	}
	return clean, nil
}

type Files struct {
	Manager *Manager
	Models  *Models
}

type InspectedAttachment struct {
	Path, MIME string
	Bytes      []byte
	RawBytes   int64
}

// Inspection is read-only and accepts any file location inside the task root,
// not just Tuck's managed artifact folders. No external URL or cache is used.
func (f *Files) ReadInspectionAttachments(ctx context.Context, task M, paths []any) ([]InspectedAttachment, error) {
	if len(paths) < 1 || len(paths) > 4 {
		return nil, Problem(422, "invalid_arguments", "附件识别每次需要 1–4 个文件")
	}
	root, e := taskRoot(task)
	if e != nil {
		return nil, e
	}
	defer root.Close()
	result := []InspectedAttachment{}
	identities := []os.FileInfo{}
	rawTotal, preparedTotal := 0, 0
	for _, value := range paths {
		if e := ctxError(ctx); e != nil {
			return nil, e
		}
		path := S(value)
		if path == "" || strings.Contains(path, "://") || strings.HasPrefix(path, "data:") {
			return nil, Problem(422, "invalid_arguments", "请提供工作目录内的文件路径，不支持 URL 或 base64")
		}
		if filepath.IsAbs(path) {
			path, e = filepath.Rel(S(task["workspace"]), path)
			if e != nil {
				return nil, Problem(403, "path_outside_workspace", "附件不在任务工作目录内")
			}
		}
		path, e = relativeFile(path, false)
		if e != nil {
			return nil, e
		}
		info, err := root.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			return nil, Problem(422, "attachment_unavailable", "附件必须是工作目录内可读的普通文件；不能通过链接逃出目录")
		}
		if info.Size() > MaxUploadBytes || int64(rawTotal)+info.Size() > 48<<20 {
			return nil, Problem(413, "attachment_limit", "识别附件每件最多 16 MiB，合计最多 48 MiB")
		}
		handle, err := root.Open(path)
		if err != nil {
			return nil, Problem(422, "attachment_unavailable", "无法读取附件")
		}
		opened, err := handle.Stat()
		if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
			handle.Close()
			return nil, Problem(409, "attachment_changed", "附件在读取前已改变")
		}
		for _, previous := range identities {
			if os.SameFile(previous, opened) {
				handle.Close()
				return nil, Problem(422, "invalid_arguments", "不能重复识别同一文件")
			}
		}
		raw, err := io.ReadAll(io.LimitReader(handle, MaxUploadBytes+1))
		handle.Close()
		if e := ctxError(ctx); e != nil {
			return nil, e
		}
		rawTotal += len(raw)
		if err != nil || len(raw) > MaxUploadBytes || rawTotal > 48<<20 {
			return nil, Problem(413, "attachment_limit", "附件读取失败或超过大小限制")
		}
		if ImageType(raw) == "" {
			return nil, Problem(422, "attachment_type_unsupported", "附件识别目前只支持 PNG、JPEG、GIF、WebP 图片；其他文件请使用对应读取工具")
		}
		prepared, mime, err := PrepareModelImage(raw)
		if err != nil {
			return nil, Problem(422, "attachment_invalid_image", "附件图片损坏或超过像素限制，请使用可正常解码且不超过 4000 万像素的图片")
		}
		preparedTotal += len(prepared)
		if preparedTotal > MaxUploadBytes {
			return nil, Problem(413, "attachment_limit", "模型图片表示合计超过 16 MiB，请减少或缩小图片")
		}
		identities = append(identities, opened)
		result = append(result, InspectedAttachment{Path: filepath.ToSlash(path), MIME: mime, Bytes: prepared, RawBytes: int64(len(raw))})
	}
	current, err := os.Stat(S(task["workspace"]))
	opened, openErr := root.Stat(".")
	if err != nil || openErr != nil || !os.SameFile(current, opened) {
		return nil, Problem(409, "workspace_changed", "任务目录已改变")
	}
	return result, nil
}

// Read references through the task root, preserving original pixels and order.
// No network input or temporary reference files are created.
func (f *Files) ReadImageReferences(ctx context.Context, task M, paths []any) ([]ImageReference, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	if len(paths) > 4 {
		return nil, Problem(422, "invalid_arguments", "参考图最多 4 张")
	}
	root, e := taskRoot(task)
	if e != nil {
		return nil, e
	}
	defer root.Close()
	var result []ImageReference
	var identities []os.FileInfo
	total := 0
	for _, value := range paths {
		if e = ctxError(ctx); e != nil {
			return nil, e
		}
		path, err := relativeFile(S(value), true)
		if err != nil {
			return nil, err
		}
		info, err := root.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			return nil, Problem(422, "invalid_image_reference", "参考图必须是任务目录内可读的普通图片文件")
		}
		if info.Size() > MaxUploadBytes || int64(total)+info.Size() > 48<<20 {
			return nil, Problem(413, "image_reference_limit", "参考图每张最多 16 MiB，合计最多 48 MiB")
		}
		handle, err := root.Open(path)
		if err != nil {
			return nil, Problem(422, "invalid_image_reference", "无法读取任务目录内的参考图")
		}
		opened, err := handle.Stat()
		if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
			handle.Close()
			return nil, Problem(409, "image_reference_changed", "参考图文件已改变")
		}
		for _, previous := range identities {
			if os.SameFile(previous, opened) {
				handle.Close()
				return nil, Problem(422, "invalid_image_reference", "参考图不能重复引用同一文件")
			}
		}
		raw, err := io.ReadAll(io.LimitReader(handle, MaxUploadBytes+1))
		handle.Close()
		if e = ctxError(ctx); e != nil {
			return nil, e
		}
		total += len(raw)
		if err != nil || len(raw) > MaxUploadBytes || total > 48<<20 {
			return nil, Problem(413, "image_reference_limit", "参考图读取失败或超过大小限制")
		}
		mimeType := ImageType(raw)
		if !Contains([]string{"image/png", "image/jpeg", "image/webp"}, mimeType) {
			return nil, Problem(422, "invalid_image_reference", "参考图只支持 PNG、JPEG 或 WebP")
		}
		if _, _, err = decodeImage(raw); err != nil {
			return nil, Problem(422, "invalid_image_reference", "参考图无法解码或像素超过限制")
		}
		identities = append(identities, opened)
		result = append(result, ImageReference{Bytes: raw, MIME: mimeType})
	}
	return result, nil
}

func (f *Files) List(task M) ([]any, error) {
	result := []any{}
	if S(task["workspace"]) == "" || S(task["workspaceMode"]) == "none" {
		return result, nil
	}
	root, e := taskRoot(task)
	if e != nil {
		return nil, e
	}
	defer root.Close()
	var visit func(string, int) error
	visit = func(dir string, depth int) error {
		if depth > 8 || len(result) >= 500 {
			return nil
		}
		handle, e := root.Open(dir)
		if e != nil {
			return e
		}
		entries, e := handle.ReadDir(-1)
		handle.Close()
		if e != nil {
			return e
		}
		for _, entry := range entries {
			if len(result) >= 500 {
				break
			}
			if entry.Name() == "node_modules" || entry.Name() == ".git" || entry.Name() == ".venv" || entry.Type()&os.ModeSymlink != 0 {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			if entry.IsDir() {
				if e = visit(path, depth+1); e != nil {
					return e
				}
			} else if info, e := root.Stat(path); e == nil && info.Mode().IsRegular() {
				result = append(result, M{"path": filepath.ToSlash(path), "size": info.Size(), "modified": info.ModTime().UnixMilli()})
			}
		}
		return nil
	}
	e = visit(".", 0)
	return result, e
}
func (f *Files) editable(task M) error {
	if S(task["parentTaskId"]) != "" || B(task["archived"]) {
		return Problem(409, "readonly_task", "子会话是只读历史")
	}
	return writableTaskWorkspace(task)
}
func (f *Files) Upload(task M, data M) (M, error) {
	if e := f.editable(task); e != nil {
		return nil, e
	}
	name := filepath.Base(strings.ReplaceAll(S(data["name"]), "\\", "/"))
	name = regexp.MustCompile(`[\x00-\x1f]`).ReplaceAllString(name, "")
	if utf8.RuneCountInString(name) > 180 {
		name = string([]rune(name)[:180])
	}
	if name == "" || name == "." || name == ".." {
		return nil, Problem(400, "invalid_name", "无效的文件名")
	}
	encoded, ok := data["base64"].(string)
	if !ok {
		return nil, Problem(400, "invalid_file", "无效的文件内容")
	}
	if len(encoded) > ((MaxUploadBytes+2)/3)*4 {
		return nil, Problem(413, "upload_limit", "单个文件最多 16 MiB")
	}
	raw, e := base64.StdEncoding.Strict().DecodeString(encoded)
	if e != nil {
		return nil, Problem(400, "invalid_file", "无效的文件内容")
	}
	if len(raw) > MaxUploadBytes {
		return nil, Problem(413, "upload_limit", "单个文件最多 16 MiB")
	}
	mimeType := ImageType(raw)
	if mimeType == "" && (strings.HasPrefix(S(data["mime"]), "image/") || regexp.MustCompile(`(?i)\.(png|jpe?g|gif|webp|bmp|avif|heic|tiff?)$`).MatchString(name)) && !strings.HasSuffix(strings.ToLower(name), ".svg") {
		return nil, Problem(400, "invalid_image", "图片无效或格式不支持，请使用 PNG、JPEG、WebP 或 GIF")
	}
	if mimeType != "" {
		if _, _, e = decodeImage(raw); e != nil {
			return nil, Problem(400, "invalid_image", "图片无法读取或像素过大")
		}
	}
	root, e := openArtifactRoot(task, "uploads")
	if e != nil {
		return nil, e
	}
	defer root.Close()
	id := ID()
	fileName := "upload_" + id + "-" + name
	path := TaskArtifactsDirectory + "/uploads/" + fileName
	handle, e := root.OpenFile(fileName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return nil, e
	}
	published := false
	defer func() {
		if !published {
			_ = root.Remove(fileName)
		}
	}()
	_, e = handle.Write(raw)
	if e == nil {
		e = handle.Sync()
	}
	closeErr := handle.Close()
	if e != nil || closeErr != nil {
		root.Remove(fileName)
		if e != nil {
			return nil, e
		}
		return nil, closeErr
	}
	kind := "file"
	if mimeType != "" {
		kind = "image"
	} else {
		mimeType = "application/octet-stream"
	}
	attachment := M{"id": id, "name": name, "path": path, "mime": mimeType, "size": len(raw), "sha256": Hash(raw), "kind": kind}
	if e = checkArtifactRoot(task, "uploads", root); e != nil {
		return nil, e
	}
	files, e := f.List(task)
	if e != nil {
		return nil, e
	}
	_, e = f.Manager.Update(S(task["id"]), func(current M) error {
		if err := f.editable(current); err != nil {
			return err
		}
		current["uploads"] = append(A(current["uploads"]), attachment)
		current["artifacts"] = files
		return nil
	})
	if e != nil {
		return nil, e
	}
	published = true
	return attachment, nil
}
func (f *Files) attachments(task M, ids []string) ([]M, error) {
	if len(ids) > 8 {
		return nil, Problem(400, "attachment_limit", "每条消息最多 8 个附件，且不能重复")
	}
	seen := map[string]bool{}
	images := 0
	result := []M{}
	for _, id := range ids {
		if seen[id] {
			return nil, Problem(400, "attachment_limit", "每条消息最多 8 个附件，且不能重复")
		}
		seen[id] = true
		var item M
		for _, v := range A(task["uploads"]) {
			if S(O(v)["id"]) == id {
				item = O(v)
				break
			}
		}
		if item == nil {
			return nil, Problem(400, "attachment_not_found", "附件不存在或不属于当前会话")
		}
		if S(item["kind"]) == "image" {
			if !Contains(f.Models.find(S(task["modelId"]))["inputModalities"], "IMAGE") && !f.canInspect(task) {
				return nil, Problem(409, "image_unsupported", "当前聊天模型不支持图片；请启用附件识别工具并配置识图模型，或切换到支持图片的聊天模型")
			}
			images += I(item["size"])
		}
		result = append(result, item)
	}
	if images > MaxUploadBytes {
		return nil, Problem(413, "attachment_limit", "每条消息的图片合计最多 16 MiB")
	}
	return result, nil
}
func (f *Files) Prepare(task M, prompt string, ids []string) (M, []any, error) {
	attachments, e := f.attachments(task, ids)
	if e != nil {
		return nil, nil, e
	}
	blocks := []any{}
	if prompt != "" {
		blocks = append(blocks, M{"id": "prompt:" + ID(), "type": "markdown", "data": M{"markdown": prompt}})
	}
	prepared := []modelAttachment{}
	items := []any{}
	directImages := Contains(f.Models.find(S(task["modelId"]))["inputModalities"], "IMAGE")
	for _, item := range attachments {
		path, e := relativeFile(S(item["path"]), false)
		if e != nil {
			return nil, nil, e
		}
		root, e := taskRoot(task)
		if e != nil {
			return nil, nil, e
		}
		handle, e := root.Open(path)
		root.Close()
		if e != nil {
			return nil, nil, e
		}
		info, e := handle.Stat()
		if e != nil || !info.Mode().IsRegular() || info.Size() > MaxUploadBytes {
			handle.Close()
			return nil, nil, Problem(400, "attachment_unavailable", "附件不可用")
		}
		media := modelAttachment{Name: S(item["name"]), Path: S(item["path"]), MIME: S(item["mime"])}
		if S(item["kind"]) == "image" {
			raw, e := io.ReadAll(io.LimitReader(handle, MaxUploadBytes+1))
			handle.Close()
			if e != nil {
				return nil, nil, e
			}
			if Hash(raw) != S(item["sha256"]) {
				return nil, nil, Problem(400, "attachment_changed", "图片已被修改，请重新添加")
			}
			encoded, mime, e := PrepareModelImage(raw)
			if e != nil {
				return nil, nil, e
			}
			if directImages {
				media.Bytes, media.MIME = encoded, mime
			}
		} else {
			handle.Close()
		}
		prepared = append(prepared, media)
		items = append(items, M{"id": item["id"], "name": item["name"], "kind": item["kind"], "mimeType": item["mime"], "sizeBytes": item["size"], "src": "/api/tasks/" + url.PathEscape(S(task["id"])) + "/files?path=" + url.QueryEscape(S(item["path"]))})
	}
	if len(items) > 0 {
		blocks = append(blocks, M{"id": "attachments:" + ID(), "type": "attachments", "data": M{"title": "附件", "provenance": M{"origin": "user-upload"}, "items": items}})
	}
	native := modelContent.UserMessage(prompt, prepared, directImages, f.canInspect(task))
	if len(JSON(native)) > AgentRequestBytes {
		return nil, nil, Problem(413, "request_too_large", "图片消息超过 100 MB，请减少图片或缩小尺寸")
	}
	return native, blocks, nil
}

func (f *Files) canInspect(task M) bool {
	if f.Models.Local == nil || f.Models.Local.inspectionAllowed(task, S(task["agent"])) != nil {
		return false
	}
	_, e := f.Models.InspectionModel(S(task["agent"]))
	return e == nil
}
func (f *Files) Serve(w http.ResponseWriter, r *http.Request, task M) error {
	if r.URL.Query().Has("inspection") {
		var images []InspectedAttachment
		var e error
		if r.URL.Query().Get("source") == "runcove" {
			images, e = f.remoteInspectionPreview(r, task)
		} else {
			images, e = f.ReadInspectionAttachments(r.Context(), task, []any{r.URL.Query().Get("path")})
		}
		if e != nil {
			return e
		}
		w.Header().Set("Content-Type", images[0].MIME)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		http.ServeContent(w, r, filepath.Base(images[0].Path), time.Time{}, bytes.NewReader(images[0].Bytes))
		return nil
	}
	path, e := relativeFile(r.URL.Query().Get("path"), false)
	if e != nil {
		return e
	}
	root, e := taskRoot(task)
	if e != nil {
		return e
	}
	defer root.Close()
	handle, e := root.Open(path)
	if e != nil {
		return e
	}
	defer handle.Close()
	info, e := handle.Stat()
	if e != nil {
		return e
	}
	if info.IsDir() {
		files, e := f.List(task)
		if e != nil {
			return e
		}
		WriteJSON(w, 200, files)
		return nil
	}
	if r.URL.Query().Has("preview") && info.Size() > 2<<20 {
		return Problem(413, "preview_limit", "文本预览最多 2 MB，请下载后查看")
	}
	if info.Size() > 64<<20 {
		return Problem(413, "file_limit", "文件预览或下载最多 64 MiB")
	}
	ext := strings.ToLower(filepath.Ext(path))
	uploadedMIME := ""
	for _, value := range A(task["uploads"]) {
		item := O(value)
		uploadedPath, err := relativeFile(S(item["path"]), false)
		if err == nil && uploadedPath == path && S(item["kind"]) == "image" && Contains([]string{"image/png", "image/jpeg", "image/gif", "image/webp"}, S(item["mime"])) {
			uploadedMIME = S(item["mime"])
			break
		}
	}
	inline := !r.URL.Query().Has("download") && (uploadedMIME != "" || Contains([]string{".png", ".jpg", ".jpeg", ".gif", ".webp", ".mp3", ".m4a", ".wav", ".ogg", ".mp4", ".webm", ".pdf"}, ext))
	disposition := "inline"
	if r.URL.Query().Has("download") || !inline && Contains([]string{".docx", ".xlsx", ".pptx"}, ext) {
		disposition = "attachment"
	}
	mimeType := "text/plain; charset=utf-8"
	if inline || Contains([]string{".docx", ".xlsx", ".pptx"}, ext) {
		mimeType = MIMEType(path)
	}
	if inline && uploadedMIME != "" {
		mimeType = uploadedMIME
	}
	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Content-Disposition", disposition+"; filename*=UTF-8''"+url.PathEscape(filepath.Base(path)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, filepath.Base(path), info.ModTime(), handle)
	return nil
}
func (f *Files) Save(task M, data M) error {
	if e := f.editable(task); e != nil {
		return e
	}
	text := fmt.Sprint(data["content"])
	if len(text) > 2<<20 {
		return Problem(413, "editor_limit", "Text editor limit is 2 MiB")
	}
	path, e := relativeFile(S(data["path"]), false)
	if e != nil {
		return e
	}
	root, e := taskRoot(task)
	if e != nil {
		return e
	}
	defer root.Close()
	handle, e := root.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0600)
	if e != nil {
		return e
	}
	_, e = io.WriteString(handle, text)
	if e == nil {
		e = handle.Sync()
	}
	ce := handle.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	files, e := f.List(task)
	if e != nil {
		return e
	}
	_, e = f.Manager.Update(S(task["id"]), func(t M) error { t["artifacts"] = files; return nil })
	return e
}

// SaveGeneratedImages publishes originals and exact submitted prompts together.
// Rollback removes only files exclusively created by this call.
func (f *Files) SaveGeneratedImages(ctx context.Context, task M, images []GeneratedImage, prompt string) ([]M, error) {
	if e := f.editable(task); e != nil {
		return nil, e
	}
	root, e := openArtifactRoot(task, "images")
	if e != nil {
		return nil, e
	}
	defer root.Close()
	dir := TaskArtifactsDirectory + "/images"
	published := false
	created := []string{}
	defer func() {
		if !published {
			for _, path := range created {
				_ = root.Remove(path)
			}
		}
	}()
	write := func(path string, content []byte) error {
		if e := ctxError(ctx); e != nil {
			return e
		}
		handle, e := root.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return e
		}
		created = append(created, path)
		_, e = handle.Write(content)
		if e == nil {
			e = handle.Sync()
		}
		closeErr := handle.Close()
		if e != nil {
			return e
		}
		return closeErr
	}
	files := []M{}
	for _, image := range images {
		ext := map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/webp": ".webp", "image/gif": ".gif"}[image.MIME]
		if ext == "" {
			return nil, Problem(422, "invalid_image", "不支持的图片格式")
		}
		base := "img_" + ID()
		path, promptPath := base+ext, base+".prompt.txt"
		if e = write(path, image.Bytes); e != nil {
			return nil, e
		}
		if e = write(promptPath, []byte(prompt)); e != nil {
			return nil, e
		}
		files = append(files, M{"path": dir + "/" + path, "promptPath": dir + "/" + promptPath, "mime": image.MIME, "size": len(image.Bytes), "sha256": Hash(image.Bytes)})
	}
	if e = checkArtifactRoot(task, "images", root); e != nil {
		return nil, e
	}
	artifacts, e := f.List(task)
	if e != nil {
		return nil, e
	}
	_, e = f.Manager.Update(S(task["id"]), func(current M) error {
		if e := ctxError(ctx); e != nil {
			return e
		}
		if e := f.editable(current); e != nil {
			return e
		}
		if current["turnId"] != task["turnId"] || current["workspace"] != task["workspace"] {
			return Problem(409, "task_changed", "生成期间任务已改变")
		}
		current["artifacts"] = artifacts
		return nil
	})
	if e != nil {
		return nil, e
	}
	published = true
	return files, nil
}

func decodeImage(raw []byte) (image.Image, bool, error) {
	mime := ImageType(raw)
	if mime == "" {
		return nil, false, fmt.Errorf("unsupported image")
	}
	if mime == "image/webp" && len(raw) >= 21 && string(raw[12:16]) == "VP8X" && raw[20]&2 != 0 {
		return decodeAnimatedWebP(raw)
	}
	config, _, e := image.DecodeConfig(bytes.NewReader(raw))
	if e != nil {
		return nil, false, e
	}
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 40000000 {
		return nil, false, fmt.Errorf("image dimensions exceed limit")
	}
	animated := false
	if mime == "image/gif" {
		animated = gifFrames(raw) > 1
		if int64(config.Width)*int64(config.Height)*int64(max(1, gifFrames(raw))) > 40000000 {
			return nil, false, fmt.Errorf("animation dimensions exceed limit")
		}
		all, err := gif.DecodeAll(bytes.NewReader(raw))
		if err != nil || len(all.Image) == 0 {
			return nil, false, fmt.Errorf("invalid GIF animation")
		}
		return all.Image[0], animated, nil
	}
	if mime == "image/webp" && len(raw) >= 21 && string(raw[12:16]) == "VP8X" && raw[20]&2 != 0 {
		animated = true
	}
	img, _, e := image.Decode(bytes.NewReader(raw))
	if e != nil {
		return nil, false, e
	}
	return img, animated, nil
}
func gifFrames(raw []byte) int {
	if len(raw) < 13 {
		return 0
	}
	pos := 13
	if raw[10]&128 != 0 {
		pos += 3 * (1 << ((raw[10] & 7) + 1))
	}
	frames := 0
	skip := func() bool {
		for pos < len(raw) {
			n := int(raw[pos])
			pos++
			if n == 0 {
				return true
			}
			pos += n
			if pos > len(raw) {
				return false
			}
		}
		return false
	}
	for pos < len(raw) {
		b := raw[pos]
		pos++
		switch b {
		case 0x3b:
			return frames
		case 0x21:
			pos++
			if !skip() {
				return frames
			}
		case 0x2c:
			if pos+9 > len(raw) {
				return frames
			}
			flags := raw[pos+8]
			pos += 9
			if flags&128 != 0 {
				pos += 3 * (1 << ((flags & 7) + 1))
			}
			pos++
			if !skip() {
				return frames
			}
			frames++
		default:
			return frames
		}
	}
	return frames
}
func PrepareModelImage(raw []byte) ([]byte, string, error) {
	img, animated, e := decodeImage(raw)
	if e != nil {
		return nil, "", e
	}
	if animated {
		return raw, ImageType(raw), nil
	}
	if orientation := jpegOrientation(raw); orientation >= 2 && orientation <= 8 {
		img = orientImage(img, orientation)
	}
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if max(width, height) > 2048 {
		scale := 2048 / float64(max(width, height))
		width = max(1, int(float64(width)*scale))
		height = max(1, int(float64(height)*scale))
		resized := image.NewNRGBA(image.Rect(0, 0, width, height))
		draw.CatmullRom.Scale(resized, resized.Bounds(), img, bounds, draw.Over, nil)
		img = resized
	}
	alpha := encodedAlpha(raw)
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y && !alpha; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			_, _, _, a := img.At(x, y).RGBA()
			if a != 65535 {
				alpha = true
				break
			}
		}
	}
	var out bytes.Buffer
	if alpha {
		e = png.Encode(&out, img)
		return out.Bytes(), "image/png", e
	}
	e = jpeg.Encode(&out, img, &jpeg.Options{Quality: 85})
	return out.Bytes(), "image/jpeg", e
}

func encodedAlpha(raw []byte) bool {
	if ImageType(raw) == "image/png" && len(raw) > 25 {
		if raw[25] == 4 || raw[25] == 6 {
			return true
		}
		for p := 8; p+12 <= len(raw); {
			n := int(binary.BigEndian.Uint32(raw[p : p+4]))
			if n < 0 || n > len(raw)-p-12 {
				return false
			}
			if string(raw[p+4:p+8]) == "tRNS" {
				return true
			}
			p += n + 12
		}
	}
	return ImageType(raw) == "image/webp" && len(raw) >= 21 && string(raw[12:16]) == "VP8X" && raw[20]&16 != 0
}

// The standard decoder handles static WebP. Decode each ANMF frame as an
// ordinary RIFF image to validate animations without losing the original bytes.
func decodeAnimatedWebP(raw []byte) (image.Image, bool, error) {
	var first image.Image
	frames := 0
	var pixels int64
	if int64(binary.LittleEndian.Uint32(raw[4:8]))+8 != int64(len(raw)) {
		return nil, false, fmt.Errorf("truncated WebP")
	}
	for p := 12; p+8 <= len(raw); {
		n := int(binary.LittleEndian.Uint32(raw[p+4 : p+8]))
		if n < 0 || n > len(raw)-p-8 {
			return nil, false, fmt.Errorf("truncated WebP chunk")
		}
		chunk := raw[p+8 : p+8+n]
		if string(raw[p:p+4]) == "ANMF" {
			if len(chunk) < 24 {
				return nil, false, fmt.Errorf("invalid WebP frame")
			}
			w := 1 + int(chunk[6]) + int(chunk[7])<<8 + int(chunk[8])<<16
			h := 1 + int(chunk[9]) + int(chunk[10])<<8 + int(chunk[11])<<16
			pixels += int64(w) * int64(h)
			if w <= 0 || h <= 0 || pixels > 40000000 {
				return nil, false, fmt.Errorf("animation dimensions exceed limit")
			}
			payload := chunk[16:]
			var frame bytes.Buffer
			frame.WriteString("RIFF")
			size := make([]byte, 4)
			binary.LittleEndian.PutUint32(size, uint32(4+18+len(payload)))
			frame.Write(size)
			frame.WriteString("WEBPVP8X")
			frame.Write([]byte{10, 0, 0, 0, 16, 0, 0, 0, byte(w - 1), byte((w - 1) >> 8), byte((w - 1) >> 16), byte(h - 1), byte((h - 1) >> 8), byte((h - 1) >> 16)})
			frame.Write(payload)
			img, _, err := image.Decode(bytes.NewReader(frame.Bytes()))
			if err != nil {
				return nil, false, err
			}
			if first == nil {
				first = img
			}
			frames++
		}
		p += 8 + n + n%2
	}
	if first == nil {
		return nil, false, fmt.Errorf("WebP animation has no frames")
	}
	return first, frames > 1, nil
}
func jpegOrientation(raw []byte) int {
	if ImageType(raw) != "image/jpeg" {
		return 1
	}
	for pos := 2; pos+4 < len(raw); {
		if raw[pos] != 255 {
			return 1
		}
		marker := raw[pos+1]
		if marker == 0xda || marker == 0xd9 {
			return 1
		}
		size := int(binary.BigEndian.Uint16(raw[pos+2 : pos+4]))
		if size < 2 || pos+2+size > len(raw) {
			return 1
		}
		segment := raw[pos+4 : pos+2+size]
		if marker == 0xe1 && len(segment) >= 14 && string(segment[:6]) == "Exif\x00\x00" {
			t := segment[6:]
			var order binary.ByteOrder = binary.LittleEndian
			if string(t[:2]) == "MM" {
				order = binary.BigEndian
			} else if string(t[:2]) != "II" {
				return 1
			}
			offset := int(order.Uint32(t[4:8]))
			if offset < 0 || offset+2 > len(t) {
				return 1
			}
			count := int(order.Uint16(t[offset : offset+2]))
			for i := 0; i < count; i++ {
				p := offset + 2 + i*12
				if p+12 > len(t) {
					return 1
				}
				if order.Uint16(t[p:p+2]) == 0x112 && order.Uint16(t[p+2:p+4]) == 3 && order.Uint32(t[p+4:p+8]) == 1 {
					return int(order.Uint16(t[p+8 : p+10]))
				}
			}
		}
		pos += 2 + size
	}
	return 1
}
func orientImage(src image.Image, orientation int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	outW, outH := w, h
	if orientation >= 5 {
		outW, outH = h, w
	}
	out := image.NewNRGBA(image.Rect(0, 0, outW, outH))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dx, dy := x, y
			switch orientation {
			case 2:
				dx = w - 1 - x
			case 3:
				dx, dy = w-1-x, h-1-y
			case 4:
				dy = h - 1 - y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = h-1-y, x
			case 7:
				dx, dy = h-1-y, w-1-x
			case 8:
				dx, dy = y, w-1-x
			}
			out.Set(dx, dy, color.NRGBAModel.Convert(src.At(x+b.Min.X, y+b.Min.Y)))
		}
	}
	return out
}
