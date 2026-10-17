package tuck

func imageDefinition(resource M) M {
	maxCount := 4
	description := modelText(textGenerate)
	if provider := S(resource["providerName"]); provider != "" {
		description += modelText(textGenerateProvider, map[string]string{"provider": provider})
	}
	description += modelText(textGenerateModel, map[string]string{"model": firstString(resource["upstreamModelKey"], resource["displayName"], "Unspecified")})
	if S(resource["source"]) == "platform" {
		maxCount = min(4, I(resource["maxImagesPerRequest"]))
		description += modelText(textGeneratePlatform)
	}
	return M{"name": "generate_images", "description": description, "inputSchema": M{"type": "object", "additionalProperties": false, "required": []string{"prompt"}, "properties": M{
		"prompt":           M{"type": "string", "minLength": 1, "maxLength": 32000, "description": modelText(textPrompt)},
		"count":            M{"type": []string{"integer", "null"}, "minimum": 1, "maximum": maxCount, "default": 1, "description": modelText(textCount)},
		"reference_images": M{"type": []string{"array", "null"}, "maxItems": 4, "uniqueItems": true, "items": M{"type": "string", "minLength": 1, "maxLength": 1024}, "description": modelText(textReferences)},
	}}}
}

func inspectionDefinition() M {
	return M{"name": inspectionTool, "description": modelText(textInspect), "inputSchema": M{"type": "object", "additionalProperties": false, "required": []string{"attachments", "request"}, "properties": M{
		"attachments": M{"type": "array", "minItems": 1, "maxItems": 4, "uniqueItems": true, "description": modelText(textAttachments), "items": M{"type": "object", "additionalProperties": false, "required": []string{"path"}, "properties": M{
			"source": M{"type": []string{"string", "null"}, "enum": []any{"local", "runcove", nil}, "default": "local", "description": modelText(textAttachmentSource)},
			"path":   M{"type": "string", "minLength": 1, "maxLength": 4096, "description": modelText(textAttachmentPath)},
		}}},
		"request": M{"type": "string", "minLength": 1, "maxLength": 32000, "description": modelText(textRequest)},
	}}}
}

func workspaceDefinitions() []M {
	return []M{
		{"name": "upload_runcove_file", "description": modelText(textUpload), "inputSchema": M{"type": "object", "additionalProperties": false, "required": []string{"file_path", "remote_path"}, "properties": M{
			"file_path": M{"type": "string", "description": modelText(textUploadPath)}, "remote_path": M{"type": "string", "description": modelText(textUploadRemote)}, "overwrite": M{"type": "boolean", "default": false, "description": modelText(textUploadOverwrite)},
		}}},
		{"name": "download_runcove_files", "description": modelText(textDownload), "inputSchema": M{"type": "object", "additionalProperties": false, "required": []string{"remote_paths", "local_directory"}, "properties": M{
			"remote_paths": M{"type": "array", "minItems": 1, "maxItems": 20, "description": modelText(textDownloadRemote), "items": M{"type": "string", "minLength": 1, "maxLength": 4096}}, "local_directory": M{"type": "string", "minLength": 1, "maxLength": 4096, "description": modelText(textDownloadDirectory)}, "overwrite": M{"type": "boolean", "default": false, "description": modelText(textDownloadOverwrite)},
		}}},
	}
}

func deliveryDefinition() M {
	return M{"name": "deliver_result", "description": modelText(textDelivery), "inputSchema": M{"type": "object", "additionalProperties": false, "required": []string{"summary"}, "properties": M{
		"summary": M{"type": "string", "minLength": 1, "maxLength": 1000, "description": modelText(textSummary)},
		"files": M{"type": []string{"array", "null"}, "maxItems": 10, "description": modelText(textDeliveryFiles), "items": M{"type": "object", "additionalProperties": false, "required": []string{"path"}, "properties": M{
			"source":      M{"type": []string{"string", "null"}, "enum": []any{"local", "runcove", nil}, "default": "local", "description": modelText(textDeliverySource)},
			"path":        M{"type": "string", "minLength": 1, "maxLength": 4096, "description": modelText(textDeliveryPath)},
			"description": M{"type": []string{"string", "null"}, "minLength": 1, "maxLength": 1000, "description": modelText(textFileDescription)},
		}}},
	}}}
}

func buildToolDefinitions(names []string, imageResource M) []M {
	definitions := []M{}
	for _, name := range modelContent.ToolNames() {
		if !Contains(names, name) {
			continue
		}
		var d M
		switch name {
		case "list_sessions":
			d = M{"name": name, "description": modelText(textList), "inputSchema": M{"type": "object", "additionalProperties": false, "properties": M{"limit": M{"type": []string{"integer", "null"}, "minimum": 1, "maximum": 100, "default": 10, "description": modelText(textLimit)}}}}
		case "send_session_message":
			d = M{"name": name, "description": modelText(textSend), "inputSchema": M{"type": "object", "additionalProperties": false, "required": []string{"target_task_id", "message"}, "properties": M{"target_task_id": M{"type": "string", "minLength": 1, "description": modelText(textTarget)}, "message": M{"type": "string", "minLength": 1, "maxLength": 100000, "description": modelText(textMessage)}}}}
		case "deliver_result":
			d = deliveryDefinition()
		case "upload_runcove_file":
			d = workspaceDefinitions()[0]
		case "download_runcove_files":
			d = workspaceDefinitions()[1]
		case "generate_images":
			d = imageDefinition(imageResource)
		case "inspect_attachments":
			d = inspectionDefinition()
		}
		if name == "list_sessions" || name == inspectionTool {
			d["annotations"] = M{"readOnlyHint": true}
		}
		definitions = append(definitions, d)
	}
	return definitions
}
