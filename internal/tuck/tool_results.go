package tuck

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"regexp"
	"strings"
)

// Tool results are a model-facing projection, not a storage or UI schema.
func toolOutput(name string, value M) M {
	status := S(value["status"])
	if name == "download_runcove_files" && len(A(value["files"])) > 0 {
		files, failures := []any{}, []any{}
		for i, v := range A(value["files"]) {
			item := O(v)
			if item["status"] == "completed" {
				files = append(files, M{"path": O(item["file"])["path"]})
			} else {
				failure := toolFailure(S(item["code"]), S(item["message"]))
				delete(failure, "status")
				failure["field"] = fmt.Sprintf("remote_paths[%d]", i)
				failures = append(failures, failure)
			}
		}
		if len(failures) == 0 {
			return M{"status": "completed", "files": files}
		}
		result := toolFailure("download_failed", "No files were downloaded.")
		if len(files) > 0 {
			result = toolFailure("partial_download", "Some files could not be downloaded. Completed files were kept.")
			result["status"], result["files"] = "partial", files
		}
		result["errors"] = failures
		return result
	}
	if status != "completed" && status != "accepted" {
		result := toolFailure(S(value["code"]), S(value["message"]))
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
		if S(value["notice"]) != "" {
			result["warning"] = "The chat model does not declare image input. Use inspect_attachments to check the generated files if needed."
		}
		return result
	case "inspect_attachments":
		return M{"status": "completed", "text": value["text"]}
	}
	return toolFailure("internal_error", "")
}

var toolErrorMessages = map[string]string{
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

func toolFailure(code, message string) M {
	if code == "snapshot_failed" || code == "persistence_failed" {
		message = toolErrorMessages[code]
	}
	if code == "" {
		code, message = "internal_error", ""
	}
	// Localized errors belong to the UI. Tool prose is English, without OS details.
	english := strings.TrimSpace(message) != ""
	for _, r := range message {
		if r > 127 {
			english = false
			break
		}
	}
	if !english {
		message = toolErrorMessages[code]
		if message == "" {
			if _, platformCode := platformMessages[code]; platformCode {
				message = "The platform denied the operation. Check the enterprise account, permissions and configuration."
			} else {
				code, message = "internal_error", toolErrorMessages["internal_error"]
			}
		}
	}
	return M{"status": "failed", "code": code, "message": message}
}

func toolError(err error) M {
	var upstream *toolUpstreamError
	if errors.As(err, &upstream) {
		result := toolFailure(upstream.code, "")
		result["upstream_status"] = upstream.status
		if upstream.upstreamCode != "" {
			result["upstream_code"] = upstream.upstreamCode
		}
		return result
	}
	if errors.Is(err, context.Canceled) {
		return toolFailure("operation_canceled", "")
	}
	var timeout net.Error
	if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &timeout) && timeout.Timeout() {
		return toolFailure("upstream_timeout", "")
	}
	if os.IsNotExist(err) {
		return toolFailure("local_file_not_found", "")
	}
	if os.IsPermission(err) {
		return toolFailure("file_not_readable", "Access to the source or destination file was denied.")
	}
	if os.IsExist(err) {
		return toolFailure("local_file_exists", "")
	}
	if Code(err) == "" {
		return toolFailure("internal_error", "")
	}
	return toolFailure(Code(err), err.Error())
}

type toolUpstreamError struct {
	status             int
	code, upstreamCode string
}

func (e *toolUpstreamError) Error() string { return toolErrorMessages[e.code] }
func (e *toolUpstreamError) Unwrap() error { return Problem(e.status, e.code, e.Error()) }

var upstreamCodePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)

func modelTransportError(err error, fallback string) error {
	code := S(toolError(err)["code"])
	if code != "upstream_timeout" && code != "operation_canceled" {
		code = fallback
	}
	return Problem(502, code, toolErrorMessages[code])
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
	return &toolUpstreamError{status: status, code: code, upstreamCode: upstream}
}
