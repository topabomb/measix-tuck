package tuck

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

func WorkspacePath(value string, root bool) (string, error) {
	if len(value) > 4096 || strings.ContainsAny(value, "\\\x00\r\n\t") || regexp.MustCompile(`[\x00-\x1f\x7f]`).MatchString(value) || !root && value == "" {
		return "", Problem(400, "invalid_workspace_path", "工作区路径无效")
	}
	if value != "" {
		for _, part := range strings.Split(value, "/") {
			if part == "" || part == "." || part == ".." {
				return "", Problem(400, "invalid_workspace_path", "工作区路径无效")
			}
		}
	}
	return value, nil
}
func strongETag(value string) bool {
	return regexp.MustCompile(`^"[^"\x00-\x20\x7f]+"$`).MatchString(value)
}
func (l *LocalTools) projection(ctx context.Context) (M, error) {
	p := l.Models.Platform
	token, e := p.Access(ctx)
	if e != nil {
		return nil, e
	}
	b := p.Binding()
	r, e := p.HTTP(ctx, S(b["origin"]), strings.TrimSuffix(S(O(b["discovery"])["clientApiBase"]), "/")+"/workspace", "GET", nil, token, nil)
	if e != nil {
		return nil, e
	}
	return responseJSON(r, "WorkspaceProjection")
}
func (l *LocalTools) workspaceGate(ctx context.Context, turn *toolTurn) error {
	if e := l.current(turn); e != nil {
		return e
	}
	if turn.Workspace == "" {
		return Problem(400, "invalid_arguments", "The external runtime working directory is unknown.")
	}
	if e := checkTurnWorkspace(turn); e != nil {
		return e
	}
	actual, e := filepath.EvalSymlinks(S(turn.Task["workspace"]))
	if e != nil || actual != turn.Workspace || turn.Target == nil || platformIdentity(l.Models.Platform.Binding()) != turn.Identity {
		return Problem(403, "tool_not_permitted", "The working directory or enterprise identity changed.")
	}
	return l.remoteGate(ctx, turn)
}
func (l *LocalTools) remoteGate(ctx context.Context, turn *toolTurn) error {
	if turn.Target == nil || platformIdentity(l.Models.Platform.Binding()) != turn.Identity {
		return Problem(403, "tool_not_permitted", "Runcove workspace access is unavailable or changed.")
	}
	p, e := l.projection(ctx)
	if e != nil {
		return e
	}
	d := O(p["webdav"])
	t := turn.Target
	if platformIdentity(l.Models.Platform.Binding()) != turn.Identity || !B(p["filesAvailable"]) || p["agentSpaceId"] != t["space"] || p["fileExchangeWebdavResourceId"] != t["resourceId"] || !B(d["available"]) || d["resourceId"] != t["resourceId"] || !reflectNumberEqual(d["bindingRevision"], t["revision"]) || d["accessMode"] != t["mode"] {
		return Problem(403, "tool_not_permitted", "Enterprise WebDAV access changed during this turn.")
	}
	return ctxError(ctx)
}
func reflectNumberEqual(a, b any) bool {
	if S(a) != "" || S(b) != "" {
		return S(a) == S(b)
	}
	return N(a) == N(b)
}
func encodeRemote(path string) string {
	parts := strings.Split(path, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}
func (l *LocalTools) remote(ctx context.Context, turn *toolTurn, path, method string, body io.Reader, headers http.Header) (*http.Response, error) {
	if platformIdentity(l.Models.Platform.Binding()) != turn.Identity {
		return nil, Problem(403, "tool_not_permitted", "The enterprise identity changed.")
	}
	token, e := l.Models.Platform.Access(ctx)
	if e != nil {
		return nil, e
	}
	if platformIdentity(l.Models.Platform.Binding()) != turn.Identity {
		return nil, Problem(403, "tool_not_permitted", "The enterprise identity changed.")
	}
	operation, cancel := context.WithCancel(ctx)
	if body != nil {
		body = watchRequestIdle(io.NopCloser(body), fileTransferIdleTimeout, cancel)
	}
	request, e := http.NewRequestWithContext(operation, method, S(turn.Target["root"])+encodeRemote(path), body)
	if e != nil {
		cancel()
		return nil, e
	}
	request.Header = headers.Clone()
	if request.Header == nil {
		request.Header = http.Header{}
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept-Encoding", "identity")
	if n, e := strconv.ParseInt(headers.Get("Content-Length"), 10, 64); e == nil {
		request.ContentLength = n
	}
	response, e := l.Models.client.Do(request)
	if e != nil {
		cancel()
		return nil, e
	}
	response.Body = watchIdle(response.Body, fileTransferIdleTimeout, cancel)
	return response, nil
}

type davMetadata struct {
	Kind, ETag string
	Bytes      int64
}

func (l *LocalTools) metadata(ctx context.Context, turn *toolTurn, path string, missing bool) (*davMetadata, error) {
	r, e := l.remote(ctx, turn, path, "PROPFIND", strings.NewReader(`<?xml version="1.0" encoding="utf-8"?><d:propfind xmlns:d="DAV:"><d:prop><d:resourcetype/><d:getetag/><d:getcontentlength/><d:getcontenttype/></d:prop></d:propfind>`), http.Header{"Depth": []string{"0"}, "Content-Type": []string{"application/xml; charset=utf-8"}})
	if e != nil {
		return nil, e
	}
	if r.StatusCode == 404 {
		r.Body.Close()
		if missing {
			if path != "" {
				if _, e = l.metadata(ctx, turn, "", false); e != nil {
					return nil, e
				}
			}
			return nil, nil
		}
		return nil, Problem(404, "webdav_file_not_found", "The remote file does not exist or the file service is unavailable.")
	}
	defer r.Body.Close()
	if r.StatusCode != 207 {
		code := "webdav_unavailable"
		if r.StatusCode == 401 || r.StatusCode == 403 {
			code = "tool_not_permitted"
		}
		return nil, Problem(502, code, "Enterprise WebDAV metadata could not be read.")
	}
	raw, e := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
	if e != nil || len(raw) > 1<<20 || bytes.Contains(bytes.ToUpper(raw), []byte("<!DOCTYPE")) || bytes.Contains(bytes.ToUpper(raw), []byte("<!ENTITY")) {
		return nil, Problem(502, "webdav_unavailable", "The remote metadata response is invalid.")
	}
	var parsed struct {
		Responses []struct {
			Href  string `xml:"href"`
			Props []struct {
				Status string `xml:"status"`
				Prop   struct {
					ResourceType struct {
						Collection *struct{} `xml:"collection"`
					} `xml:"resourcetype"`
					ETag  string `xml:"getetag"`
					Bytes int64  `xml:"getcontentlength"`
				} `xml:"prop"`
			} `xml:"propstat"`
		} `xml:"response"`
	}
	if e = xml.Unmarshal(raw, &parsed); e != nil || len(parsed.Responses) != 1 {
		return nil, Problem(502, "webdav_unavailable", "The remote metadata response is invalid.")
	}
	expected, _ := url.Parse(S(turn.Target["root"]) + encodeRemote(path))
	actual, e := expected.Parse(parsed.Responses[0].Href)
	if e != nil || actual.Scheme != expected.Scheme || actual.Host != expected.Host || strings.TrimSuffix(actual.Path, "/") != strings.TrimSuffix(expected.Path, "/") {
		return nil, Problem(502, "webdav_unavailable", "The remote metadata refers to a different location.")
	}
	var meta *davMetadata
	for _, p := range parsed.Responses[0].Props {
		if !strings.Contains(p.Status, " 200 ") {
			continue
		}
		meta = &davMetadata{Kind: "file", ETag: p.Prop.ETag, Bytes: p.Prop.Bytes}
		if p.Prop.ResourceType.Collection != nil {
			meta.Kind = "directory"
		}
		break
	}
	if meta == nil {
		return nil, Problem(502, "webdav_unavailable", "The remote metadata response has no successful properties.")
	}
	if meta.Kind == "file" && !strongETag(meta.ETag) {
		head, e := l.remote(ctx, turn, path, "HEAD", nil, nil)
		if e != nil {
			return nil, e
		}
		head.Body.Close()
		if head.StatusCode == 401 || head.StatusCode == 403 {
			return nil, Problem(403, "tool_not_permitted", "Enterprise file access is no longer authorized.")
		}
		meta.ETag = ""
		if head.StatusCode == 200 && strongETag(head.Header.Get("ETag")) {
			meta.ETag = head.Header.Get("ETag")
		}
	}
	return meta, nil
}
func overwriteFlag(value any) (bool, error) {
	if value == nil {
		return false, nil
	}
	if text, ok := value.(string); ok {
		switch strings.ToLower(strings.TrimSpace(text)) {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
	}
	flag, ok := value.(bool)
	if !ok {
		return false, Problem(400, "invalid_arguments", "overwrite must be a boolean.")
	}
	return flag, nil
}
func (l *LocalTools) Transfer(ctx context.Context, turn *toolTurn, name string, args M) M {
	if name == "download_runcove_files" {
		return l.downloadFiles(ctx, turn, args)
	}
	if name != "upload_runcove_file" {
		return toolFailure("invalid_arguments", "Choose an available enterprise WebDAV tool.")
	}
	overwrite, e := overwriteFlag(args["overwrite"])
	if e != nil {
		return toolFailure("invalid_arguments", e.Error())
	}
	remote, e := WorkspacePath(S(args["remote_path"]), false)
	if e != nil {
		return toolFailure("invalid_arguments", e.Error())
	}
	if reservedRemote(remote) {
		return toolFailure("webdav_path_not_permitted", "Enterprise WebDAV cannot access reserved .agent-space* directories. Copy the selected artifact into an ordinary project directory first.")
	}
	if e = l.workspaceGate(ctx, turn); e != nil {
		return transferFailure(e, false)
	}
	if S(turn.Target["mode"]) != "FULL" {
		return toolFailure("webdav_read_only", "The current enterprise WebDAV permits reading only.")
	}
	local, e := relativeFile(S(args["file_path"]), false)
	if e != nil {
		return toolFailure("invalid_arguments", e.Error())
	}
	root, e := os.OpenRoot(turn.Workspace)
	if e != nil {
		return transferFailure(e, false)
	}
	defer root.Close()
	file, e := root.Open(local)
	if e != nil {
		return transferFailure(e, false)
	}
	defer file.Close()
	info, e := file.Stat()
	if e != nil || !info.Mode().IsRegular() {
		return toolFailure("local_file_not_found", "The upload source must be a readable regular file.")
	}
	parent := ""
	for _, part := range strings.Split(remote, "/")[:len(strings.Split(remote, "/"))-1] {
		if parent != "" {
			parent += "/"
		}
		parent += part
		existing, e := l.metadata(ctx, turn, parent, true)
		if e != nil {
			return transferFailure(e, false)
		}
		if existing != nil && existing.Kind != "directory" {
			return toolFailure("webdav_parent_not_directory", "The remote parent path is not a directory.")
		}
		if existing == nil {
			created, e := l.remote(ctx, turn, parent, "MKCOL", nil, nil)
			if e != nil {
				return transferFailure(e, true)
			}
			created.Body.Close()
			if created.StatusCode != 201 {
				checked, e := l.metadata(ctx, turn, parent, true)
				if e != nil || checked == nil || checked.Kind != "directory" {
					return toolFailure("webdav_parent_not_directory", "The remote parent directory could not be created.")
				}
			}
		}
	}
	target, e := l.metadata(ctx, turn, remote, true)
	if e != nil {
		return transferFailure(e, false)
	}
	if target != nil && target.Kind == "directory" {
		return toolFailure("webdav_not_file", "The remote destination is a directory.")
	}
	if target != nil && !overwrite {
		return toolFailure("webdav_file_exists", "The remote file already exists. Set overwrite=true only if replacement is intended.")
	}
	headers := http.Header{"Content-Length": []string{fmt.Sprint(info.Size())}, "Content-Type": []string{MIMEType(local)}}
	if target == nil {
		headers.Set("If-None-Match", "*")
	} else if strongETag(target.ETag) {
		headers.Set("If-Match", target.ETag)
	} else {
		return toolFailure("webdav_version_unavailable", "The remote file has no strong ETag required for safe replacement.")
	}
	if e = l.workspaceGate(ctx, turn); e != nil {
		return transferFailure(e, false)
	}
	r, e := l.remote(ctx, turn, remote, "PUT", file, headers)
	if e != nil {
		return transferFailure(e, true)
	}
	r.Body.Close()
	if r.StatusCode == 412 {
		return toolFailure("webdav_file_exists", "The remote destination changed. Inspect it before deciding whether to replace it.")
	}
	if r.StatusCode != 200 && r.StatusCode != 201 && r.StatusCode != 204 {
		if r.StatusCode == 401 || r.StatusCode == 403 {
			return toolFailure("tool_not_permitted", "The enterprise session or file authorization is no longer valid.")
		}
		unknown := r.StatusCode >= 500 || r.StatusCode < 300
		if unknown {
			return transferFailure(fmt.Errorf("upload success was not confirmed"), true)
		}
		return toolFailure("webdav_upload_failed", "Upload success was not confirmed. Check the remote file before retrying.")
	}
	return M{"status": "completed", "remote_path": remote, "bytes": info.Size(), "overwritten": target != nil}
}
func reservedRemote(remote string) bool {
	for _, part := range strings.Split(remote, "/") {
		if strings.HasPrefix(part, ".agent-space") {
			return true
		}
	}
	return false
}
func transferFailure(e error, writing bool) M {
	if writing {
		return M{"status": "unknown", "code": "outcome_unknown", "message": "The upload was interrupted and its remote outcome is unknown. Check the remote file before retrying."}
	}
	code := Code(e)
	if errors.Is(e, context.Canceled) || errors.Is(e, context.DeadlineExceeded) {
		code = "operation_canceled"
	}
	if code == "" {
		code = "webdav_unavailable"
		if os.IsNotExist(e) || os.IsPermission(e) {
			code = "local_file_not_found"
		}
		if os.IsExist(e) {
			code = "local_file_exists"
		}
	}
	if code == "invalid_workspace_path" {
		code = "invalid_arguments"
	}
	message := e.Error()
	if Status(e) == 500 {
		message = "The enterprise file operation failed. Check the current workspace before retrying."
	}
	return toolFailure(code, message)
}
func checkDestination(root *os.Root, name string, overwrite bool) error {
	info, e := root.Lstat(name)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return Problem(409, "local_file_exists", "Only existing regular files can be overwritten; choose an unused destination.")
	}
	if !overwrite {
		return Problem(409, "local_file_exists", "The destination file already exists. Set overwrite=true to replace it, or choose another directory.")
	}
	return nil
}
func (l *LocalTools) downloadFiles(ctx context.Context, turn *toolTurn, args M) M {
	if e := writableTaskWorkspace(turn.Task); e != nil {
		return transferFailure(e, false)
	}
	overwrite, e := overwriteFlag(args["overwrite"])
	if e != nil {
		return transferFailure(e, false)
	}
	paths := Strings(args["remote_paths"])
	if !isArray(args["remote_paths"]) || len(paths) < 1 || len(paths) > 20 {
		return toolFailure("invalid_arguments", "Provide remote_paths with 1–20 files and a relative local_directory.")
	}
	directoryText, ok := args["local_directory"].(string)
	if !ok || directoryText == "" {
		return toolFailure("invalid_arguments", "Provide a relative local_directory.")
	}
	directory, e := relativeFile(strings.ReplaceAll(directoryText, "\\", "/"), false)
	if e != nil {
		return transferFailure(e, false)
	}
	if e = l.workspaceGate(ctx, turn); e != nil {
		return transferFailure(e, false)
	}
	root, e := taskRoot(turn.Task)
	if e != nil {
		return transferFailure(e, false)
	}
	defer root.Close()
	seen := map[string]bool{}
	for _, remote := range paths {
		if _, e = WorkspacePath(remote, false); e != nil {
			return transferFailure(e, false)
		}
		if reservedRemote(remote) {
			return toolFailure("webdav_path_not_permitted", "Reserved .agent-space* directories are unavailable. Copy the artifact into an ordinary project directory first.")
		}
		name := path.Base(remote)
		if _, e = relativeFile(name, true); e != nil {
			return transferFailure(e, false)
		}
		key := name
		if runtime.GOOS == "windows" {
			key = strings.ToLower(key)
		}
		if seen[key] {
			return toolFailure("local_name_conflict", "Duplicate destination names are not allowed.")
		}
		seen[key] = true
		if e = checkDestination(root, filepath.Join(directory, name), overwrite); e != nil {
			return transferFailure(e, false)
		}
	}
	if e = root.MkdirAll(directory, 0700); e != nil {
		return transferFailure(e, false)
	}
	parent, e := root.OpenRoot(directory)
	if e != nil {
		return transferFailure(e, false)
	}
	defer parent.Close()
	parentInfo, e := parent.Stat(".")
	if e != nil {
		return transferFailure(e, false)
	}
	results := []any{}
	completed := 0
	stop := false
	for _, remote := range paths {
		item := M{"remote_path": remote}
		if stop {
			item = Merge(item, toolFailure("not_attempted", "The batch stopped after cancellation or authorization changed."))
			results = append(results, item)
			continue
		}
		name := path.Base(remote)
		operation := func() M {
			if ctx.Err() != nil {
				return transferFailure(ctx.Err(), false)
			}
			if e = l.workspaceGate(ctx, turn); e != nil {
				return transferFailure(e, false)
			}
			if e = checkDestination(parent, name, overwrite); e != nil {
				return transferFailure(e, false)
			}
			meta, err := l.metadata(ctx, turn, remote, true)
			if err != nil {
				return transferFailure(err, false)
			}
			if meta == nil {
				return toolFailure("webdav_file_not_found", "The remote file does not exist.")
			}
			if meta.Kind != "file" {
				return toolFailure("webdav_not_file", "The download source is a directory. Specify individual files.")
			}
			if meta.Bytes < 0 {
				return toolFailure("webdav_unavailable", "The remote file metadata has an invalid size.")
			}
			tmp := ".download_" + ID() + ".part"
			defer parent.Remove(tmp)
			file, e := parent.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if e != nil {
				return transferFailure(e, false)
			}
			headers := http.Header{}
			if strongETag(meta.ETag) {
				headers.Set("If-Match", meta.ETag)
			}
			r, e := l.remote(ctx, turn, remote, "GET", nil, headers)
			if e != nil {
				file.Close()
				return transferFailure(e, false)
			}
			if r.StatusCode != 200 {
				file.Close()
				r.Body.Close()
				code := "webdav_download_failed"
				if r.StatusCode == 412 {
					code = "webdav_version_changed"
				}
				if r.StatusCode == 401 || r.StatusCode == 403 {
					code = "tool_not_permitted"
				}
				return toolFailure(code, "The remote download failed or the file version changed.")
			}
			if strongETag(meta.ETag) && r.Header.Get("ETag") != "" && r.Header.Get("ETag") != meta.ETag {
				file.Close()
				r.Body.Close()
				return toolFailure("webdav_version_changed", "The remote file version changed before download.")
			}
			n, e := io.CopyBuffer(file, contextReader{ctx, r.Body}, make([]byte, 128<<10))
			r.Body.Close()
			if e == nil {
				e = file.Sync()
			}
			file.Close()
			if e != nil {
				return transferFailure(e, false)
			}
			if ctx.Err() != nil {
				return transferFailure(ctx.Err(), false)
			}
			if n != meta.Bytes {
				return toolFailure("webdav_version_changed", "The downloaded file size does not match its metadata. The destination was not replaced.")
			}
			if e = l.workspaceGate(ctx, turn); e != nil {
				return transferFailure(e, false)
			}
			if e = checkTaskRoot(turn.Task, root); e != nil {
				return transferFailure(e, false)
			}
			current, e := root.Stat(directory)
			if e != nil || !os.SameFile(current, parentInfo) {
				return toolFailure("invalid_arguments", "The destination directory changed during download.")
			}
			if e = checkDestination(parent, name, overwrite); e != nil {
				return transferFailure(e, false)
			}
			if ctx.Err() != nil {
				return transferFailure(ctx.Err(), false)
			}
			if overwrite {
				e = parent.Rename(tmp, name)
			} else {
				e = parent.Link(tmp, name)
			}
			if e != nil {
				return transferFailure(e, false)
			}
			return M{"status": "completed", "file": M{"path": filepath.ToSlash(filepath.Join(directory, name)), "name": name, "mime_type": MIMEType(remote), "bytes": n}}
		}
		outcome := operation()
		stop = S(outcome["code"]) == "operation_canceled" || S(outcome["code"]) == "tool_not_permitted"
		if S(outcome["status"]) == "completed" {
			completed++
		}
		results = append(results, Merge(item, outcome))
	}
	status := "completed"
	if completed < len(paths) {
		status = "failed"
		if completed > 0 {
			status = "partial"
		}
	}
	return M{"status": status, "local_directory": filepath.ToSlash(directory), "completed_count": completed, "failed_count": len(paths) - completed, "files": results}
}
