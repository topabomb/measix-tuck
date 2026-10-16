package tuck

import (
	"fmt"
	"strings"
)

// ToolTextID names text resources owned by Tuck, not external MCP contracts.
type ToolTextID string

const (
	textList              ToolTextID = "tuck.list_sessions.description"
	textLimit             ToolTextID = "tuck.list_sessions.param.limit"
	textSend              ToolTextID = "tuck.send_session_message.description"
	textTarget            ToolTextID = "tuck.send_session_message.param.target_task_id"
	textMessage           ToolTextID = "tuck.send_session_message.param.message"
	textDelivery          ToolTextID = "tuck.deliver_result.description"
	textSummary           ToolTextID = "tuck.deliver_result.param.summary"
	textDeliveryFiles     ToolTextID = "tuck.deliver_result.param.files"
	textDeliverySource    ToolTextID = "tuck.deliver_result.param.files[].source"
	textDeliveryPath      ToolTextID = "tuck.deliver_result.param.files[].path"
	textFileDescription   ToolTextID = "tuck.deliver_result.param.files[].description"
	textUpload            ToolTextID = "tuck.upload_runcove_file.description"
	textUploadPath        ToolTextID = "tuck.upload_runcove_file.param.file_path"
	textUploadRemote      ToolTextID = "tuck.upload_runcove_file.param.remote_path"
	textUploadOverwrite   ToolTextID = "tuck.upload_runcove_file.param.overwrite"
	textDownload          ToolTextID = "tuck.download_runcove_files.description"
	textDownloadRemote    ToolTextID = "tuck.download_runcove_files.param.remote_paths"
	textDownloadDirectory ToolTextID = "tuck.download_runcove_files.param.local_directory"
	textDownloadOverwrite ToolTextID = "tuck.download_runcove_files.param.overwrite"
	textGenerate          ToolTextID = "tuck.generate_images.description"
	textGenerateModel     ToolTextID = "tuck.generate_images.model"
	textGenerateProvider  ToolTextID = "tuck.generate_images.provider"
	textGeneratePlatform  ToolTextID = "tuck.generate_images.platform_references"
	textPrompt            ToolTextID = "tuck.generate_images.param.prompt"
	textCount             ToolTextID = "tuck.generate_images.param.count"
	textReferences        ToolTextID = "tuck.generate_images.param.reference_images"
	textInspect           ToolTextID = "tuck.inspect_attachments.description"
	textAttachments       ToolTextID = "tuck.inspect_attachments.param.attachments"
	textAttachmentSource  ToolTextID = "tuck.inspect_attachments.param.attachments[].source"
	textAttachmentPath    ToolTextID = "tuck.inspect_attachments.param.attachments[].path"
	textRequest           ToolTextID = "tuck.inspect_attachments.param.request"
)

var toolTexts = map[ToolTextID]string{
	textList:              "List Tuck conversations and their task IDs to find a conversation to contact.",
	textLimit:             "Number of conversations to return, from 1 to 100; default 10.",
	textSend:              "Send a message to a Tuck conversation as requested by the user. When needed, ask for a reply in the message and explain what feedback is needed. Acceptance means the message was received, not that the task is complete.",
	textTarget:            "Target conversation task ID, available from list_sessions.",
	textMessage:           "Message to send; when a reply is needed, explain what feedback you want.",
	textDelivery:          "Submit the results as a new delivery version with a brief summary and, when needed, key files from the task working directory or the connected Runcove workspace.",
	textSummary:           "Briefly describe the completed work.",
	textDeliveryFiles:     "Up to 10 key files to deliver. Omit when no files are needed.",
	textDeliverySource:    "File source: local for the task working directory, or runcove for the connected Runcove workspace. Default local.",
	textDeliveryPath:      "File path relative to the selected source root, including the filename.",
	textFileDescription:   "Brief description of the file's content or purpose. May be omitted.",
	textUpload:            "Upload one file from the task working directory to the connected Runcove workspace. Existing files are not overwritten by default.",
	textUploadPath:        "Source file path relative to the task working directory, such as reports/result.pdf.",
	textUploadRemote:      "Destination file path relative to the Runcove workspace root, including the filename. Do not start with / or /workspace/.",
	textUploadOverwrite:   "Whether to overwrite an existing remote file at the same path. Default false.",
	textDownload:          "Download 1–20 files from the connected Runcove workspace to the task working directory, preserving filenames. Existing files are not overwritten by default.",
	textDownloadRemote:    "1–20 source file paths relative to the Runcove workspace root. Do not start with / or /workspace/.",
	textDownloadDirectory: "Destination directory relative to the task working directory. Use . for its root or specify a subdirectory; stay inside the working directory.",
	textDownloadOverwrite: "Whether to overwrite existing files with the same names in the destination directory. Default false.",
	textGenerate:          "Generate images from a prompt, one image by default. Provide references only when needed to preserve a subject or style, or edit existing images. Generated images and their prompts are saved in artifacts/images; returns images and file paths.",
	textGenerateModel:     " Model: {model}.",
	textGenerateProvider:  " Provider: {provider}.",
	textGeneratePlatform:  " This platform image resource does not accept reference images; omit reference_images.",
	textPrompt:            "Describe the desired image. With references, explain each image's role in order and what to preserve or change.",
	textCount:             "Number of images to generate, from 1 to 4; default 1, subject to resource limits.",
	textReferences:        "Optional 0–4 PNG/JPEG/WebP reference images. Use file paths relative to the task working directory; omit unless needed.",
	textInspect:           "Use when you need to understand attachment content, extract information, or compare differences. Reads files from the task working directory or the connected Runcove workspace and returns text. Supports images; PDF and other attachment types are not yet supported. Provide relevant background and specific questions.",
	textAttachments:       "1–4 attachments to inspect, in order. Supports PNG/JPEG/GIF/WebP images.",
	textAttachmentSource:  "Attachment source: local for the task working directory, or runcove for the connected Runcove workspace. Default local.",
	textAttachmentPath:    "For local files, a relative or absolute path inside the task working directory. For Runcove files, a path relative to the workspace root, without /workspace/.",
	textRequest:           "What to identify, extract, compare, or verify. Refer to attachments by array order and include relevant background and the desired answer format when needed.",
}

// ResolveToolText resolves only named placeholders in Tuck-owned text.
func ResolveToolText(id ToolTextID, values map[string]string) (string, error) {
	text, ok := toolTexts[id]
	if !ok {
		return "", fmt.Errorf("unknown tool text resource: %s", id)
	}
	var out strings.Builder
	for {
		start := strings.IndexByte(text, '{')
		if start < 0 {
			out.WriteString(text)
			return out.String(), nil
		}
		end := strings.IndexByte(text[start:], '}')
		if end < 0 {
			return "", fmt.Errorf("invalid tool text resource: %s", id)
		}
		end += start
		key := text[start+1 : end]
		value, ok := values[key]
		if !ok || strings.TrimSpace(value) == "" {
			return "", fmt.Errorf("missing %s for tool text %s", key, id)
		}
		out.WriteString(text[:start])
		out.WriteString(value)
		text = text[end+1:]
	}
}

// Static resource errors are programmer errors, covered by catalog tests.
func toolText(id ToolTextID, values ...map[string]string) string {
	var args map[string]string
	if len(values) > 0 {
		args = values[0]
	}
	text, err := ResolveToolText(id, args)
	if err != nil {
		panic(err)
	}
	return text
}
