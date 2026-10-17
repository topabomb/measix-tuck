package tuck

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"regexp"
)

// Tool results are a model-facing projection, not a storage or UI schema.
func buildToolOutput(name string, value M) M {
	status := S(value["status"])
	if name == "download_runcove_files" && len(A(value["files"])) > 0 {
		files, failures := []any{}, []any{}
		for i, v := range A(value["files"]) {
			item := O(v)
			if item["status"] == "completed" {
				files = append(files, M{"path": O(item["file"])["path"]})
			} else {
				failure := buildToolFailure(S(item["code"]))
				delete(failure, "status")
				failure["field"] = fmt.Sprintf("remote_paths[%d]", i)
				failures = append(failures, failure)
			}
		}
		if len(failures) == 0 {
			return M{"status": "completed", "files": files}
		}
		result := buildToolFailure("download_failed")
		if len(files) > 0 {
			result = buildToolFailure("partial_download")
			result["status"], result["files"] = "partial", files
		}
		result["errors"] = failures
		return result
	}
	if status != "completed" && status != "accepted" {
		result := buildToolFailure(S(value["code"]))
		if status == "unknown" {
			result["status"] = "unknown"
		}
		for _, key := range []string{"field", "upstream_status", "upstream_code"} {
			if value[key] != nil && value[key] != "" {
				result[key] = value[key]
			}
		}
		return result
	}
	switch name {
	case "list_sessions":
		sessions := []any{}
		for _, v := range A(value["sessions"]) {
			s := O(v)
			item := M{"task_id": s["task_id"], "title": s["title"], "assistant_name": s["assistant_name"], "status": s["status"]}
			if S(s["pending_interaction"]) != "" {
				item["pending_interaction"] = s["pending_interaction"]
			}
			sessions = append(sessions, item)
		}
		return M{"status": "completed", "sessions": sessions}
	case "send_session_message":
		return M{"status": "accepted"}
	case "deliver_result":
		return M{"status": "completed", "version": value["version"], "file_count": value["file_count"], "view_url": value["view_url"]}
	case "upload_runcove_file":
		return M{"status": "completed", "overwritten": B(value["overwritten"])}
	case "generate_images":
		files := []any{}
		for _, v := range A(value["files"]) {
			f := O(v)
			files = append(files, M{"path": f["path"], "prompt_path": f["promptPath"]})
		}
		result := M{"status": "completed", "files": files}
		if B(value["textOnlyModel"]) {
			result["warning"] = "The chat model does not declare image input. Do not claim to have seen the generated images."
			if B(value["canInspect"]) {
				result["warning"] = S(result["warning"]) + " Use inspect_attachments to check the generated files if needed."
			}
		}
		return result
	case "inspect_attachments":
		return M{"status": "completed", "text": value["text"]}
	}
	return buildToolFailure("internal_error")
}

var modelErrorMessages = map[string]string{
	"session_message_unconfirmed":    "Message submission could not be confirmed. Check the target conversation before sending it again.",
	"file_version_unavailable":       "The remote file has no verifiable version.",
	"remote_file_unavailable":        "The remote file changed or could not be read.",
	"mcp_scope_changed":              "The MCP configuration or account changed. Apply the current configuration before continuing.",
	"mcp_tool_unavailable":           "This MCP tool is unavailable or no longer permitted by the current contract.",
	"mcp_request":                    "The MCP request does not match the supported protocol.",
	"mcp_method":                     "This MCP resource only supports permitted tool operations.",
	"mcp_response_size":              "The MCP response exceeds the size limit.",
	"mcp_catalog":                    "The MCP tool catalog is invalid or incomplete.",
	"mcp_unavailable":                "The MCP resource is unavailable.",
	"mcp_error":                      "The MCP request failed.",
	"mcp_protocol":                   "The MCP protocol or tool capability is unsupported.",
	"delivery_file_limit":            "A delivery accepts at most 10 files. Select key files or create an archive first.",
	"download_failed":                "No files were downloaded.",
	"partial_download":               "Some files could not be downloaded. Completed files were kept.",
	"webdav_path_not_permitted":      "Enterprise WebDAV cannot access reserved .agent-space* directories. Copy the selected artifact into an ordinary project directory first.",
	"webdav_read_only":               "The current enterprise WebDAV permits reading only.",
	"webdav_parent_not_directory":    "The remote parent path is not a directory.",
	"webdav_not_file":                "The remote path is not a regular file. Specify individual files.",
	"webdav_file_exists":             "The remote file already exists. Set overwrite=true only if replacement is intended.",
	"webdav_version_unavailable":     "The remote file has no strong ETag required for safe replacement.",
	"webdav_upload_failed":           "Upload success was not confirmed. Check the remote file before sending it again.",
	"not_attempted":                  "The batch stopped after cancellation or authorization changed.",
	"webdav_file_not_found":          "The remote file does not exist.",
	"webdav_unavailable":             "The remote file service or metadata is unavailable.",
	"webdav_version_changed":         "The remote file changed during download. The local destination was not replaced.",
	"outcome_unknown":                "The remote write may have completed. Check the remote file before sending it again.",
	"webdav_download_failed":         "The remote download failed. Completed files were kept.",
	"turn_changed":                   "The target turn changed. Check the conversation before sending again.",
	"message_too_large":              "The message exceeds the size limit.",
	"snapshot_failed":                "The delivery snapshot could not be saved. Check the delivery storage.",
	"persistence_failed":             "The delivery could not be committed. Check the delivery storage.",
	"file_not_found":                 "The source file does not exist.",
	"file_changed":                   "The source file changed while it was being read.",
	"delivery_too_large":             "The delivery exceeds the total size limit of 2 GiB.",
	"delivery_canceled":              "The delivery was canceled before it was committed.",
	"insufficient_storage":           "There is not enough free space to preserve this delivery.",
	"invalid_message":                "Provide a target conversation ID and a nonempty message within the length limit.",
	"invalid_target":                 "Choose an available main conversation.",
	"interaction_pending":            "The target conversation is waiting for user input or approval.",
	"configuration_changing":         "The configuration is being updated. Try again after it is applied.",
	"task_readonly":                  "Choose an available, unarchived main conversation.",
	"task_stopping":                  "The target conversation is stopping. Wait for it to stop before sending again.",
	"task_capacity":                  "The target assistant has reached its task concurrency limit. Wait for an active task to finish.",
	"agent_missing":                  "The target conversation's agent was removed. Restore its configuration before sending again.",
	"model_required":                 "Select a model for the target conversation before sending again.",
	"model_source_required":          "Select the model and its source for the target conversation before sending again.",
	"model_source_changed":           "The target conversation's model source changed. Select its model again before sending.",
	"model_unavailable":              "The target conversation's model is unavailable. Restore it or select an available model before sending again.",
	"platform_pending":               "The enterprise configuration is not ready. Synchronize it before sending again.",
	"reasoning_unavailable":          "The target model does not support the selected reasoning setting. Update the conversation's model settings.",
	"invalid_prompt":                 "Provide a nonempty message within the length limit.",
	"internal_error":                 "The operation failed because of an internal error.",
	"invalid_arguments":              "Check the parameter types, paths and limits in the tool definition.",
	"invalid_json":                   "Provide a valid JSON object matching the tool parameters.",
	"tool_not_permitted":             "This tool is not permitted for the active task or workspace.",
	"operation_canceled":             "The operation was canceled.",
	"workspace_required":             "Choose a writable task working directory before using this tool.",
	"workspace_unavailable":          "The working directory is unavailable.",
	"invalid_workspace":              "The working directory is invalid or unknown.",
	"workspace_changed":              "The task working directory changed. The operation was stopped.",
	"artifact_directory_changed":     "The artifact directory changed. The operation was stopped.",
	"path_outside_workspace":         "The path must stay inside the task working directory.",
	"task_changed":                   "The task changed. The operation was stopped.",
	"task_not_found":                 "The target task does not exist.",
	"not_found":                      "The requested resource does not exist.",
	"readonly_task":                  "This task does not permit writes.",
	"local_file_not_found":           "The local source file does not exist or is not a readable regular file.",
	"file_not_readable":              "The source file could not be read.",
	"file_too_large":                 "The file exceeds the allowed size.",
	"local_file_exists":              "The destination file already exists. Choose another path or explicitly allow replacement.",
	"local_name_conflict":            "Multiple source files have the same destination filename.",
	"attachment_unavailable":         "The attachment is unavailable.",
	"attachment_changed":             "The attachment changed. Read the intended file again.",
	"attachment_limit":               "The attachments exceed the size or image limits.",
	"attachment_type_unsupported":    "Only PNG, JPEG, GIF and WebP images are supported.",
	"attachment_invalid_image":       "The image cannot be decoded or exceeds the 40-million-pixel limit.",
	"image_resource_unavailable":     "Select an available image generation resource.",
	"image_references_unsupported":   "This platform image resource does not support references. Omit reference_images or select a compatible local resource.",
	"invalid_image_reference":        "Provide valid PNG, JPEG or WebP reference files inside the task working directory.",
	"image_reference_limit":          "Reference images exceed the allowed size or count.",
	"image_reference_changed":        "A reference image changed. Check the file before generating again.",
	"image_unreachable":              "The image service could not be reached. Check the connection.",
	"invalid_image_response":         "The image service returned an invalid or incomplete result.",
	"invalid_image":                  "The image service returned an invalid or unsupported image.",
	"image_limit":                    "Generated images exceed the allowed file, pixel or model-input size.",
	"inspection_model_required":      "Configure an attachment inspection model with TEXT and IMAGE input.",
	"inspection_model_unavailable":   "The selected attachment inspection model is unavailable. Check its configuration.",
	"inspection_request_invalid":     "The attachment inspection endpoint is invalid.",
	"inspection_unreachable":         "The attachment inspection service could not be reached. Check the connection.",
	"inspection_response_invalid":    "The attachment inspection service returned invalid or oversized text.",
	"inspection_incomplete":          "The attachment inspection did not finish normally. Check the model output limit or service status.",
	"resource_changed":               "The platform identity or resource configuration changed. Check the current configuration.",
	"resource_unavailable":           "The selected resource connection is unavailable.",
	"not_enrolled":                   "Connect an enterprise account before using this resource.",
	"managed_snapshot_required":      "Synchronize the enterprise configuration before continuing.",
	"invalid_platform_data":          "The platform returned invalid configuration data.",
	"platform_unreachable":           "The platform could not be reached. Check the connection.",
	"refresh_conflict":               "The enterprise session could not be renewed. Reconnect the account.",
	"invalid_workspace_path":         "Provide a relative path inside the selected workspace.",
	"workspace_identity_changed":     "The original workspace identity or binding changed.",
	"upstream_timeout":               "The model service timed out. Try again later.",
	"upstream_authentication_failed": "The model service rejected the credentials. Check the provider or platform connection.",
	"upstream_forbidden":             "The model service denied access to this resource.",
	"upstream_rate_limited":          "The model service rate limit was reached. Try again later.",
	"upstream_rejected":              "The model service rejected the request. Check the model capabilities and request limits.",
	"upstream_unavailable":           "The model service is unavailable. Try again later.",
}

func buildToolFailure(code string) M {
	message := modelErrorMessages[code]
	if message == "" {
		if _, ok := platformMessages[code]; ok {
			message = "The platform denied the operation. Check the enterprise account, permissions and configuration."
		} else {
			code = "internal_error"
			message = modelErrorMessages[code]
		}
	}
	status := "failed"
	if code == "session_message_unconfirmed" {
		status = "unknown"
	}
	return M{"status": status, "code": code, "message": message}
}

func buildToolError(err error) M {
	var upstream *modelUpstreamError
	if errors.As(err, &upstream) {
		result := buildToolFailure(upstream.code)
		result["upstream_status"] = upstream.status
		if upstream.upstreamCode != "" {
			result["upstream_code"] = upstream.upstreamCode
		}
		return result
	}
	if errors.Is(err, context.Canceled) {
		return buildToolFailure("operation_canceled")
	}
	var timeout net.Error
	if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &timeout) && timeout.Timeout() {
		return buildToolFailure("upstream_timeout")
	}
	if os.IsNotExist(err) {
		return buildToolFailure("local_file_not_found")
	}
	if os.IsPermission(err) {
		return buildToolFailure("file_not_readable")
	}
	if os.IsExist(err) {
		return buildToolFailure("local_file_exists")
	}
	if Code(err) == "" {
		return buildToolFailure("internal_error")
	}
	return buildToolFailure(Code(err))
}

type modelUpstreamError struct {
	status             int
	code, upstreamCode string
}

func (e *modelUpstreamError) Error() string { return modelErrorMessages[e.code] }
func (e *modelUpstreamError) Unwrap() error { return Problem(e.status, e.code, e.Error()) }

var upstreamCodePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)

func modelTransportError(err error, fallback string) error {
	code := S(buildToolError(err)["code"])
	if code != "upstream_timeout" && code != "operation_canceled" {
		code = fallback
	}
	return Problem(502, code, modelErrorMessages[code])
}

func modelToolError(status int, payload M, secrets ...string) error {
	code := "upstream_unavailable"
	switch {
	case status == 401:
		code = "upstream_authentication_failed"
	case status == 403:
		code = "upstream_forbidden"
	case status == 429:
		code = "upstream_rate_limited"
	case status == 408 || status == 504:
		code = "upstream_timeout"
	case status >= 400 && status < 500:
		code = "upstream_rejected"
	}
	upstream := firstString(O(payload["error"])["code"], payload["code"])
	if !upstreamCodePattern.MatchString(upstream) || cleanSecret(upstream, secrets...) != upstream {
		upstream = ""
	}
	return &modelUpstreamError{status: status, code: code, upstreamCode: upstream}
}
