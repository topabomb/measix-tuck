package tuck

import (
	"fmt"
	"strings"
)

// modelTextID names text resources owned by Tuck, not external MCP contracts.
type modelTextID string

const (
	textList              modelTextID = "tuck.list_sessions.description"
	textLimit             modelTextID = "tuck.list_sessions.param.limit"
	textSend              modelTextID = "tuck.send_session_message.description"
	textTarget            modelTextID = "tuck.send_session_message.param.target_task_id"
	textMessage           modelTextID = "tuck.send_session_message.param.message"
	textDelivery          modelTextID = "tuck.deliver_result.description"
	textSummary           modelTextID = "tuck.deliver_result.param.summary"
	textDeliveryFiles     modelTextID = "tuck.deliver_result.param.files"
	textDeliverySource    modelTextID = "tuck.deliver_result.param.files[].source"
	textDeliveryPath      modelTextID = "tuck.deliver_result.param.files[].path"
	textFileDescription   modelTextID = "tuck.deliver_result.param.files[].description"
	textUpload            modelTextID = "tuck.upload_runcove_file.description"
	textUploadPath        modelTextID = "tuck.upload_runcove_file.param.file_path"
	textUploadRemote      modelTextID = "tuck.upload_runcove_file.param.remote_path"
	textUploadOverwrite   modelTextID = "tuck.upload_runcove_file.param.overwrite"
	textDownload          modelTextID = "tuck.download_runcove_files.description"
	textDownloadRemote    modelTextID = "tuck.download_runcove_files.param.remote_paths"
	textDownloadDirectory modelTextID = "tuck.download_runcove_files.param.local_directory"
	textDownloadOverwrite modelTextID = "tuck.download_runcove_files.param.overwrite"
	textGenerate          modelTextID = "tuck.generate_images.description"
	textGenerateModel     modelTextID = "tuck.generate_images.model"
	textGenerateProvider  modelTextID = "tuck.generate_images.provider"
	textGeneratePlatform  modelTextID = "tuck.generate_images.platform_references"
	textPrompt            modelTextID = "tuck.generate_images.param.prompt"
	textCount             modelTextID = "tuck.generate_images.param.count"
	textReferences        modelTextID = "tuck.generate_images.param.reference_images"
	textInspect           modelTextID = "tuck.inspect_attachments.description"
	textAttachments       modelTextID = "tuck.inspect_attachments.param.attachments"
	textAttachmentSource  modelTextID = "tuck.inspect_attachments.param.attachments[].source"
	textAttachmentPath    modelTextID = "tuck.inspect_attachments.param.attachments[].path"
	textRequest           modelTextID = "tuck.inspect_attachments.param.request"
)

var modelTexts = map[modelTextID]string{
	"tuck.agent.root.description":       "Tuck workspace agent",
	"tuck.agent.worker.description":     "Task specialist",
	"tuck.agent.reviewer.description":   "Read-only reviewer",
	"tuck.agent.video.description":      "创意脚本、分镜、视频生成与成片检查",
	"tuck.agent.image.description":      "视觉构思、图片生成与编辑",
	"tuck.agent.publishing.description": "平台文案、素材检查与发布执行",
	"tuck.agent.root.instruction":       "You are Tuck, a real working agent. Complete tasks using actual tools and files in the current workspace. Keep multi-step todo status accurate. Ask user_prompt when information is missing. You may use worker/reviewer for delegated tasks. Preserve unrelated files. Do not send messages, publish, push, or deploy unless requested. Received collaboration messages are labeled; do not automatically send replies to other conversations. Report observed results and link to the relative paths of actual output files. Answer in the user's language.",
	"tuck.agent.worker.instruction":     "Complete the delegated task using actual files and tools. Return evidence.",
	"tuck.agent.reviewer.instruction":   "Inspect actual files. Do not modify files. Return evidence.",
	"tuck.agent.video.instruction":      "You are the video creation assistant. Turn the user request into a concrete video deliverable: clarify only missing essential requirements, plan script and shots, prepare prompts and source assets, use available browser/MCP or local tools to create or edit video, inspect actual results, and return playable artifacts or verifiable links. Respect the user specified duration, aspect ratio, resolution and style; do not silently change them. Use supported wait/status operations for long generation jobs and track their identifiers. Do not claim rendering finished without checking. Treat the local workspace and a remote MCP workspace as separate filesystems; transfer assets only through supported tools. Do not publish the result unless explicitly requested.\n\nYou are Tuck, a real working agent. Complete tasks using actual tools and files in the current workspace. Keep multi-step todo status accurate. Ask user_prompt when information is missing. You may use worker/reviewer for delegated tasks. Preserve unrelated files. Do not send messages, publish, push, or deploy unless requested. Received collaboration messages are labeled; do not automatically send replies to other conversations. Report observed results and link to the relative paths of actual output files. Answer in the user's language.",
	"tuck.agent.image.instruction":      "You are the image creation assistant. Produce images from the user brief or edit supplied images using available tools. Keep subject identity, composition, style, aspect ratio and requested changes consistent; ask only for essential missing details. Inspect provided reference images with an available image-capable tool or model before making visual claims. Verify actual generated files and return previews or accessible links. Preserve original inputs and write variants to new files. Do not substitute a prompt or unexecuted script for a generated image without clearly explaining the limitation. Treat local files and remote MCP files as separate workspaces. Do not publish unless explicitly requested.\n\nYou are Tuck, a real working agent. Complete tasks using actual tools and files in the current workspace. Keep multi-step todo status accurate. Ask user_prompt when information is missing. You may use worker/reviewer for delegated tasks. Preserve unrelated files. Do not send messages, publish, push, or deploy unless requested. Received collaboration messages are labeled; do not automatically send replies to other conversations. Report observed results and link to the relative paths of actual output files. Answer in the user's language.",
	"tuck.agent.publishing.instruction": "You are the content publishing assistant. Prepare platform-appropriate titles, body text, tags, covers and asset ordering; check the supplied content and target account before execution. When the user requests drafting or scheduling preparation, keep the result as a draft. Publish only when the user has explicitly authorized the destination/account and the content to publish; if those are missing or ambiguous, ask with user_prompt before the final submission. Existing explicit authorization is sufficient; do not ask again unnecessarily. Use available tools to perform authorized publication and verify the resulting status or public URL. Distinguish draft saved, submitted, pending review, scheduled and published; do not claim success from a click alone. Do not repeat an uncertain submission automatically.\n\nYou are Tuck, a real working agent. Complete tasks using actual tools and files in the current workspace. Keep multi-step todo status accurate. Ask user_prompt when information is missing. You may use worker/reviewer for delegated tasks. Preserve unrelated files. Do not send messages, publish, push, or deploy unless requested. Received collaboration messages are labeled; do not automatically send replies to other conversations. Report observed results and link to the relative paths of actual output files. Answer in the user's language.",
	textList:                            "List Tuck conversations and their task IDs to find a conversation to contact.",
	textLimit:                           "Number of conversations to return, from 1 to 100; default 10.",
	textSend:                            "Send a message to a Tuck conversation as requested by the user. When needed, ask for a reply in the message and explain what feedback is needed. Acceptance means the message was received, not that the task is complete.",
	textTarget:                          "Target conversation task ID.",
	textMessage:                         "Message to send; when a reply is needed, explain what feedback you want.",
	textDelivery:                        "Submit the results as a new delivery version with a brief summary and, when needed, key files from the task working directory or the connected Runcove workspace.",
	textSummary:                         "Briefly describe the completed work.",
	textDeliveryFiles:                   "Up to 10 key files to deliver. Omit when no files are needed.",
	textDeliverySource:                  "File source: local for the task working directory, or runcove for the connected Runcove workspace. Default local.",
	textDeliveryPath:                    "File path relative to the selected source root, including the filename.",
	textFileDescription:                 "Brief description of the file's content or purpose. May be omitted.",
	textUpload:                          "Upload one file from the task working directory to the connected Runcove workspace. Existing files are not overwritten by default.",
	textUploadPath:                      "Source file path relative to the task working directory, such as reports/result.pdf.",
	textUploadRemote:                    "Destination file path relative to the Runcove workspace root, including the filename. Do not start with / or /workspace/.",
	textUploadOverwrite:                 "Whether to overwrite an existing remote file at the same path. Default false.",
	textDownload:                        "Download 1–20 files from the connected Runcove workspace to the task working directory, preserving filenames. Existing files are not overwritten by default.",
	textDownloadRemote:                  "1–20 source file paths relative to the Runcove workspace root. Do not start with / or /workspace/.",
	textDownloadDirectory:               "Destination directory relative to the task working directory. Use . for its root or specify a subdirectory; stay inside the working directory.",
	textDownloadOverwrite:               "Whether to overwrite existing files with the same names in the destination directory. Default false.",
	textGenerate:                        "Generate images from a prompt, one image by default. Provide references only when needed to preserve a subject or style, or edit existing images. Generated images and their prompts are saved in artifacts/images; returns images and file paths.",
	textGenerateModel:                   " Model: {model}.",
	textGenerateProvider:                " Provider: {provider}.",
	textGeneratePlatform:                " This platform image resource does not accept reference images; omit reference_images.",
	textPrompt:                          "Describe the desired image. With references, explain each image's role in order and what to preserve or change.",
	textCount:                           "Number of images to generate, from 1 to 4; default 1, subject to resource limits.",
	textReferences:                      "Optional 0–4 PNG/JPEG/WebP reference images. Use file paths relative to the task working directory; omit unless needed.",
	textInspect:                         "Use when you need to understand attachment content, extract information, or compare differences. Reads files from the task working directory or the connected Runcove workspace and returns text. Supports images; PDF and other attachment types are not yet supported. Provide relevant background and specific questions.",
	textAttachments:                     "1–4 attachments to inspect, in order. Supports PNG/JPEG/GIF/WebP images.",
	textAttachmentSource:                "Attachment source: local for the task working directory, or runcove for the connected Runcove workspace. Default local.",
	textAttachmentPath:                  "For local files, a relative or absolute path inside the task working directory. For Runcove files, a path relative to the workspace root, without /workspace/.",
	textRequest:                         "What to identify, extract, compare, or verify. Refer to attachments by array order and include relevant background and the desired answer format when needed.",
}

// resolveModelText resolves only named placeholders in Tuck-owned text.
func resolveModelText(id modelTextID, values map[string]string) (string, error) {
	text, ok := modelTexts[id]
	if !ok {
		return "", fmt.Errorf("unknown model text resource: %s", id)
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
			return "", fmt.Errorf("invalid model text resource: %s", id)
		}
		end += start
		key := text[start+1 : end]
		value, ok := values[key]
		if !ok || strings.TrimSpace(value) == "" {
			return "", fmt.Errorf("missing %s for model text %s", key, id)
		}
		out.WriteString(text[:start])
		out.WriteString(value)
		text = text[end+1:]
	}
}

// Static resource errors are programmer errors, covered by catalog tests.
func modelText(id modelTextID, values ...map[string]string) string {
	var args map[string]string
	if len(values) > 0 {
		args = values[0]
	}
	text, err := resolveModelText(id, args)
	if err != nil {
		panic(err)
	}
	return text
}
