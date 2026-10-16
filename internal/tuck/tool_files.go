package tuck

import (
	"context"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// Resolve the original call's Tuck presentation, never the current account's
// similarly named file. No preview cache or engine state is consulted.
func (f *Files) remoteInspectionPreview(r *http.Request, task M) ([]InspectedAttachment, error) {
	index, e := strconv.Atoi(r.URL.Query().Get("message"))
	item, er := strconv.Atoi(r.URL.Query().Get("item"))
	if e != nil || er != nil || index < 0 || item < 0 || item > 3 {
		return nil, Problem(400, "invalid_arguments", "Invalid attachment preview.")
	}
	message, e := f.Manager.options.Store.Message(S(task["id"]), index)
	if e != nil {
		return nil, e
	}
	var call M
	for _, v := range A(message["blocks"]) {
		b := O(v)
		if b["id"] == r.URL.Query().Get("block") && b["type"] == "tool-call" {
			call = O(b["data"])
			break
		}
	}
	refs := A(O(call["input"])["attachments"])
	if S(call["name"]) != inspectionTool || item >= len(refs) {
		return nil, Problem(404, "attachment_unavailable", "Attachment call not found.")
	}
	ref, e := parseToolFile(refs[item], false)
	if e != nil || ref.Source != "runcove" || ref.Path != r.URL.Query().Get("path") {
		return nil, Problem(400, "invalid_arguments", "Attachment does not match the call.")
	}
	workspace := O(O(call["presentation"])["workspace"])
	l := f.Models.Local
	if S(workspace["subject"]) == "" || l == nil {
		return nil, Problem(409, "attachment_unavailable", "The original inspection workspace is unavailable.")
	}
	binding := l.Models.Platform.Binding()
	if S(workspace["subject"]) != workspaceSubject(binding) {
		return nil, Problem(403, "workspace_identity_changed", "The inspection belongs to a different workspace identity.")
	}
	projection, e := l.projection(r.Context())
	if e != nil {
		return nil, e
	}
	target := workspaceTarget(binding, projection)
	if target == nil || target["space"] != workspace["space"] || target["resourceId"] != workspace["resourceId"] || !reflectNumberEqual(target["revision"], workspace["revision"]) {
		return nil, Problem(409, "workspace_identity_changed", "The original Runcove workspace binding has changed.")
	}
	turn := &toolTurn{Task: task, Target: target, Identity: platformIdentity(binding)}
	return l.readToolAttachments(r.Context(), turn, []any{M{"source": "runcove", "path": ref.Path}})
}

func (l *LocalTools) inspectionPresentation(root, input M) M {
	presentation := M{"kind": "generic"}
	remote := false
	for _, value := range A(input["attachments"]) {
		remote = remote || O(value)["source"] == "runcove"
	}
	if !remote {
		return presentation
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	for _, turn := range l.turns {
		if turn.TaskID == S(root["id"]) && turn.TurnID == S(root["turnId"]) && turn.ctx.Err() == nil && turn.previewWorkspace != nil {
			presentation["workspace"] = Clone(turn.previewWorkspace)
			break
		}
	}
	return presentation
}

func inspectionCallKey(data M) string {
	return string(JSON(M{"name": data["name"], "callId": data["callId"], "input": data["input"]}))
}

type toolFileRef struct{ Source, Path string }

// Windows directory junctions may retain their input spelling after
// EvalSymlinks. Compare the directory identity captured when the turn began.
func checkTurnWorkspace(turn *toolTurn) error {
	info, e := os.Stat(S(turn.Task["workspace"]))
	if e != nil || turn.workspaceInfo == nil || !os.SameFile(turn.workspaceInfo, info) {
		return Problem(409, "workspace_changed", "任务目录已改变")
	}
	return nil
}

func parseToolFile(value any, delivery bool) (toolFileRef, error) {
	v := O(value)
	allowed := []string{"source", "path"}
	if delivery {
		allowed = append(allowed, "description")
	}
	if !noUnknown(v, allowed...) || !stringField(v["path"], 1, 4096) || v["source"] != nil && !Contains([]string{"local", "runcove"}, S(v["source"])) {
		return toolFileRef{}, Problem(422, "invalid_arguments", "Each file needs path and optional source (local or runcove).")
	}
	ref := toolFileRef{Source: firstString(v["source"], "local"), Path: S(v["path"])}
	if ref.Source == "runcove" {
		if _, e := WorkspacePath(ref.Path, false); e != nil {
			return ref, e
		}
	}
	return ref, nil
}

func checkTaskRoot(task M, root *os.Root) error {
	opened, e := root.Stat(".")
	actual, err := os.Stat(S(task["workspace"]))
	if e != nil || err != nil || !os.SameFile(opened, actual) {
		return Problem(409, "workspace_changed", "The task working directory changed.")
	}
	return nil
}

func workspaceTarget(binding, projection M) M {
	for _, value := range A(O(binding["snapshot"])["webdav"]) {
		d := O(value)
		id := S(d["resourceId"])
		dav := O(projection["webdav"])
		if id == S(projection["fileExchangeWebdavResourceId"]) && B(d["enabled"]) && S(d["purpose"]) == "WORKSPACE_FILE_EXCHANGE" && S(d["clientProtocol"]) == "WEBDAV" && S(d["authOwnership"]) == "ENTERPRISE_MANAGED" && S(dav["resourceId"]) == id && B(projection["filesAvailable"]) && B(dav["available"]) && Contains([]string{"READ_ONLY", "FULL"}, S(dav["accessMode"])) && strings.HasPrefix(S(projection["agentSpaceId"]), "spc_") && S(d["rootPathTemplate"]) == "/runtime/v1/resources/"+id+"/spaces/{agentSpaceId}/dav/" {
			return M{"resourceId": id, "space": projection["agentSpaceId"], "revision": dav["bindingRevision"], "mode": dav["accessMode"], "name": d["displayName"], "root": S(binding["origin"]) + strings.ReplaceAll(S(d["rootPathTemplate"]), "{agentSpaceId}", S(projection["agentSpaceId"]))}
		}
	}
	return nil
}

func inspectionWorkspace(binding, target M) M {
	if target == nil {
		return nil
	}
	return M{"subject": workspaceSubject(binding), "space": target["space"], "resourceId": target["resourceId"], "revision": target["revision"]}
}

// Remote reads reuse the turn's enterprise target and existing DAV transport.
// Callers stream into delivery staging or bounded memory; never a task cache.
func (l *LocalTools) openRemoteFile(ctx context.Context, turn *toolTurn, path string, limit int64) (io.ReadCloser, *davMetadata, error) {
	if _, e := WorkspacePath(path, false); e != nil {
		return nil, nil, e
	}
	if e := l.remoteGate(ctx, turn); e != nil {
		return nil, nil, e
	}
	meta, e := l.metadata(ctx, turn, path, false)
	if e != nil {
		return nil, nil, e
	}
	if meta.Kind != "file" || meta.Bytes < 0 {
		return nil, nil, Problem(422, "attachment_unavailable", "Specify a readable Runcove file, not a directory.")
	}
	if meta.Bytes > limit {
		return nil, nil, Problem(413, "file_too_large", "The remote file exceeds the allowed size.")
	}
	if !strongETag(meta.ETag) {
		return nil, nil, Problem(409, "file_version_unavailable", "The remote file has no verifiable version.")
	}
	response, e := l.remote(ctx, turn, path, "GET", nil, http.Header{"If-Match": []string{meta.ETag}})
	if e != nil {
		return nil, nil, e
	}
	if response.StatusCode != 200 || response.Header.Get("ETag") != "" && response.Header.Get("ETag") != meta.ETag {
		response.Body.Close()
		return nil, nil, Problem(409, "remote_file_unavailable", "The remote file changed or could not be read.")
	}
	return response.Body, meta, nil
}

func (l *LocalTools) readToolAttachments(ctx context.Context, turn *toolTurn, values []any) ([]InspectedAttachment, error) {
	result := []InspectedAttachment{}
	seen := map[string]bool{}
	localPaths := []any{}
	refs := []toolFileRef{}
	for _, v := range values {
		ref, e := parseToolFile(v, false)
		if e != nil {
			return nil, e
		}
		key := ref.Source + ":" + ref.Path
		if seen[key] {
			return nil, Problem(422, "invalid_arguments", "List each attachment once.")
		}
		seen[key] = true
		refs = append(refs, ref)
		if ref.Source == "local" {
			localPaths = append(localPaths, ref.Path)
		}
	}
	var locals []InspectedAttachment
	var e error
	if len(localPaths) > 0 {
		locals, e = l.Files.ReadInspectionAttachments(ctx, turn.Task, localPaths)
		if e != nil {
			return nil, e
		}
	}
	localIndex, rawTotal, preparedTotal := 0, int64(0), 0
	for _, ref := range refs {
		var item InspectedAttachment
		if ref.Source == "local" {
			item = locals[localIndex]
			localIndex++
		} else {
			body, meta, e := l.openRemoteFile(ctx, turn, ref.Path, MaxUploadBytes)
			if e != nil {
				return nil, e
			}
			raw, e := io.ReadAll(io.LimitReader(body, MaxUploadBytes+1))
			body.Close()
			if e != nil {
				return nil, e
			}
			if int64(len(raw)) != meta.Bytes || int64(len(raw)) > MaxUploadBytes {
				return nil, Problem(413, "attachment_limit", "Remote attachment changed or exceeded its size limit.")
			}
			if ImageType(raw) == "" {
				return nil, Problem(422, "attachment_type_unsupported", "Only PNG/JPEG/GIF/WebP images are supported; PDF is not yet supported.")
			}
			prepared, mime, e := PrepareModelImage(raw)
			if e != nil {
				return nil, Problem(422, "attachment_invalid_image", "附件图片损坏或超过像素限制，请使用可正常解码且不超过 4000 万像素的图片")
			}
			item = InspectedAttachment{Path: ref.Path, MIME: mime, Bytes: prepared, RawBytes: int64(len(raw))}
			if e = l.remoteGate(ctx, turn); e != nil {
				return nil, e
			}
		}
		rawTotal += item.RawBytes
		preparedTotal += len(item.Bytes)
		if rawTotal > 48<<20 || preparedTotal > int(MaxUploadBytes) {
			return nil, Problem(413, "attachment_limit", "Attachments exceed the combined size limit.")
		}
		result = append(result, item)
	}
	return result, nil
}
