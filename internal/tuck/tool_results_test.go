package tuck

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"
)

type toolTimeoutBody struct{}

func (toolTimeoutBody) Read([]byte) (int, error) { return 0, toolReadTimeout{} }
func (toolTimeoutBody) Close() error             { return nil }

type toolReadTimeout struct{}

func (toolReadTimeout) Error() string   { return "fixture response body timeout" }
func (toolReadTimeout) Timeout() bool   { return true }
func (toolReadTimeout) Temporary() bool { return true }

func TestModelToolResponseBodyTimeout(t *testing.T) {
	f := newPlatformFixture(t, 5)
	c := inspectionConfig(f, "OPENAI_CHAT_COMPLETIONS", "OPENAI_CHAT_COMPLETIONS")
	localImageConfig(c)
	O(A(c["imageGenerators"])[0])["providerId"] = "configured"
	f.save(c)
	f.models.client = &http.Client{Transport: testTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: toolTimeoutBody{}, Request: r}, nil
	})}
	model, e := f.models.InspectionModel("root")
	if e != nil {
		t.Fatal(e)
	}
	_, e = f.models.InspectAttachments(context.Background(), c, nil, model, "Inspect", []InspectedAttachment{{Path: "image.png", MIME: "image/png", Bytes: testPNG(t, 20, 12, false)}})
	assertToolError(t, modelContent.Error(e), "upstream_timeout")
	_, e = f.models.GenerateImage(context.Background(), c, nil, ImageGenerationRequest{Prompt: "Generate", Count: 1})
	assertToolError(t, modelContent.Error(e), "upstream_timeout")
	// The same classification is used after a generated-image download.
	_, e = io.ReadAll(toolTimeoutBody{})
	assertToolError(t, modelContent.Error(modelContent.TransportError(e, "invalid_image_response")), "upstream_timeout")
}

func assertToolError(t *testing.T, value M, code string) {
	t.Helper()
	if value["status"] != "failed" || value["code"] != code || S(value["message"]) == "" {
		t.Fatal(value)
	}
	for _, r := range S(value["message"]) {
		if r > 127 {
			t.Fatal("non-English error", value)
		}
	}
	for _, key := range []string{"reason", "detail", "issues", "modelId", "attachments", "workspace"} {
		if Has(value, key) {
			t.Fatal("redundant error data", value)
		}
	}
	for key, v := range value {
		if v == nil || v == "" {
			t.Fatal("empty optional field", key, value)
		}
	}
}

func TestToolResultErrorsAndOptionalFields(t *testing.T) {
	for _, tt := range []struct {
		err  error
		code string
	}{
		{errors.New("SQL failure C:/private/database.db 密钥"), "internal_error"},
		{&os.PathError{Op: "open", Path: "C:/private/file", Err: os.ErrPermission}, "file_not_readable"},
		{&os.PathError{Op: "open", Path: "C:/private/file", Err: os.ErrNotExist}, "local_file_not_found"},
		{context.Canceled, "operation_canceled"},
		{context.DeadlineExceeded, "upstream_timeout"},
		{Problem(422, "attachment_invalid_image", "图片损坏"), "attachment_invalid_image"},
		{Problem(409, "workspace_changed", "目录改变"), "workspace_changed"},
	} {
		value := modelContent.Error(tt.err)
		assertToolError(t, value, tt.code)
		if len(value) != 3 || strings.Contains(string(JSON(value)), "private") {
			t.Fatal(value)
		}
	}
	for status, code := range map[int]string{401: "upstream_authentication_failed", 403: "upstream_forbidden", 429: "upstream_rate_limited", 504: "upstream_timeout", 400: "upstream_rejected", 503: "upstream_unavailable"} {
		for _, providerCode := range []string{"", "unsupported_image", "secret-key", "非法错误", "Bearer token", strings.Repeat("a", 65)} {
			err := modelContent.UpstreamError(status, M{"error": M{"code": providerCode, "message": "secret-key 图片 data:image/png;base64,PRIVATE"}}, "secret-key")
			value := modelContent.Error(err)
			assertToolError(t, value, code)
			if I(value["upstream_status"]) != status || Has(value, "upstream_code") != (providerCode == "unsupported_image") || strings.Contains(string(JSON(value)), "PRIVATE") || strings.Contains(string(JSON(value)), "secret-key") {
				t.Fatal(value)
			}
		}
	}
}

func TestToolResultBusinessProjection(t *testing.T) {
	for _, tt := range []struct {
		name  string
		value M
		want  string
	}{
		{"inspect_attachments", M{"status": "completed", "text": "中文识别正文", "modelId": "internal", "attachments": []any{"input.png"}, "workspace": M{"subject": "internal"}}, `{"status":"completed","text":"中文识别正文"}`},
		{"send_conversation_message", M{"status": "accepted", "target_conversation_id": "internal", "delivery": "steer"}, `{"status":"accepted"}`},
		{"upload_runcove_file", M{"status": "completed", "remote_path": "input.txt", "bytes": 20, "overwritten": false}, `{"overwritten":false,"status":"completed"}`},
		{"deliver_result", M{"status": "completed", "delivery_id": "internal", "turn_id": "internal", "total_bytes": 20, "cleanup_pending": true, "version": 2, "file_count": 0, "view_url": "#/delivery"}, `{"file_count":0,"status":"completed","version":2,"view_url":"#/delivery"}`},
		{"generate_images", M{"status": "completed", "resource": "internal", "providerUsage": M{"tokens": 30}, "files": []any{M{"path": "artifacts/images/img_x.png", "promptPath": "artifacts/images/img_x.prompt.txt", "mime": "image/png", "sha256": "internal", "size": 100}}}, `{"files":[{"path":"artifacts/images/img_x.png","prompt_path":"artifacts/images/img_x.prompt.txt"}],"status":"completed"}`},
		{"list_conversations", M{"status": "completed", "conversations": []any{M{"conversation_id": "conversation", "title": "title", "assistant_name": "assistant", "status": "idle", "queued_prompts": 0, "agent": "root", "pending_interaction": nil}}}, `{"conversations":[{"assistant_name":"assistant","conversation_id":"conversation","status":"idle","title":"title"}],"status":"completed"}`},
	} {
		if got := string(JSON(modelContent.ToolResult(tt.name, tt.value))); got != tt.want {
			t.Fatal(tt.name, got)
		}
	}
	f := newWorkspaceFixture(t)
	l, turn := f.turn(t)
	value := modelContent.ToolResult("download_runcove_files", l.Transfer(context.Background(), turn, "download_runcove_files", M{"remote_paths": []any{"hello.txt", "missing.txt"}, "local_directory": "results"}))
	if value["status"] != "partial" || value["code"] != "partial_download" || len(A(value["files"])) != 1 || O(A(value["files"])[0])["path"] != "results/hello.txt" || O(A(value["errors"])[0])["field"] != "remote_paths[1]" {
		t.Fatal(value)
	}
	value = modelContent.ToolResult("download_runcove_files", l.Transfer(context.Background(), turn, "download_runcove_files", M{"remote_paths": []any{"missing.txt"}, "local_directory": "results"}))
	if Has(value, "files") || value["code"] != "download_failed" {
		t.Fatal(value)
	}
	value = modelContent.ToolResult("upload_runcove_file", transferFailure(context.Canceled, true))
	if len(value) != 3 || value["status"] != "unknown" || value["code"] != "outcome_unknown" {
		t.Fatal(value)
	}
}

func TestModelImageProcessingDetailsStayInternal(t *testing.T) {
	details := M{"width": 2560, "height": 1440, "original_width": 3840, "alpha": true,
		"transparency": true, "resized": true, "quality": 90, "method": 4, "pixel_limit": 40000000}
	technical := regexp.MustCompile("(?i)(^|[^a-z])(width|height|dimensions?|resolution|aspect_ratio|alpha|transparency|transparent|resized?|pixels?|quality|method)([^a-z]|$)")
	// Check the actual model projection, including unknown business-owner facts.
	result := modelContent.ToolResult("generate_images", Merge(details, M{
		"status": "completed", "files": []any{Merge(details, M{"path": "artifacts/images/image.png", "promptPath": "artifacts/images/image.prompt.txt"})},
	}))
	if technical.Match(JSON(result)) {
		t.Fatal("processing details leaked through the tool result", result)
	}
	if technical.Match(JSON(modelContent.Tools(modelContent.ToolNames(), M{}))) {
		t.Fatal("processing details leaked through tool parameters or descriptions")
	}
	for _, code := range []string{"attachment_limit", "attachment_invalid_image", "image_limit"} {
		if result := modelContent.Failure(code); technical.Match(JSON(result)) {
			t.Fatal("processing details leaked through an error", result)
		}
	}
}

func TestConversationToolResultMCP(t *testing.T) {
	f := newPlatformFixture(t, 5)
	f.save(inspectionConfig(f, "OPENAI_CHAT_COMPLETIONS", "OPENAI_CHAT_COMPLETIONS"))
	app := f.startApp()
	sender := newParityTask(t, app, nil)
	target := newParityTask(t, app, nil)
	session := connectTuckTools(t, app, sender, "root")
	value := callTuckTool(t, session, "list_conversations", M{"limit": 1}, false)
	if len(A(value["conversations"])) != 1 || Has(O(A(value["conversations"])[0]), "pending_interaction") {
		t.Fatal(value)
	}
	callTuckTool(t, session, "list_conversations", M{"limit": 101}, true)
	value = callTuckTool(t, session, "list_conversations", M{}, false)
	if Has(value, "sessions") {
		t.Fatal("obsolete result field", value)
	}
	conversationID := ""
	for _, v := range A(value["conversations"]) {
		item := O(v)
		if Has(item, "task_id") {
			t.Fatal("internal task field exposed", item)
		}
		if item["conversation_id"] == target["id"] {
			conversationID = S(item["conversation_id"])
		}
	}
	if conversationID == "" {
		t.Fatal("target missing from conversation list", value)
	}
	callTuckTool(t, session, "send_conversation_message", M{"target_task_id": conversationID, "message": "Reject the old parameter"}, true)
	value = callTuckTool(t, session, "send_conversation_message", M{"target_conversation_id": "missing-conversation", "message": "Check missing target"}, true)
	assertToolError(t, value, "conversation_not_found")
	value = callTuckTool(t, session, "send_conversation_message", M{"target_conversation_id": conversationID, "message": "Message result contract"}, false)
	if string(JSON(value)) != `{"status":"accepted"}` {
		t.Fatal(value)
	}
	waitIdle(t, app.Manager, S(target["id"]))
}

func TestConversationToolUnavailableTargetModelMCP(t *testing.T) {
	f := newPlatformFixture(t, 5)
	f.save(inspectionConfig(f, "OPENAI_CHAT_COMPLETIONS", "OPENAI_CHAT_COMPLETIONS"))
	app := f.startApp()
	sender := newParityTask(t, app, nil)
	target := newParityTask(t, app, nil)
	session := connectTuckTools(t, app, sender, "root")
	// Historical conversations can retain a model removed from the catalog.
	target = mustUpdate(t, app.Manager, S(target["id"]), M{"modelId": "removed-model", "modelSource": "local"})
	value := callTuckTool(t, session, "send_conversation_message", M{"target_conversation_id": target["id"], "message": "Do not queue an unavailable target"}, true)
	assertToolError(t, value, "model_unavailable")
	current, e := app.Manager.Get(S(target["id"]))
	if e != nil || len(A(current["queue"])) != 0 || I(current["turnCount"]) != 0 {
		t.Fatal("unavailable target accepted work", current, e)
	}
}

func TestInspectionPresentationForkAmbiguity(t *testing.T) {
	_, manager, store, id, _ := fileFixture(t)
	workspace := M{"subject": "subject", "space": "space", "resourceId": "resource", "revision": 1}
	input := M{"attachments": []any{M{"source": "runcove", "path": "image.png"}}, "request": "Inspect"}
	call := M{"name": inspectionTool, "callId": "reused", "input": input, "presentation": M{"kind": "generic", "workspace": workspace}}
	task, _ := manager.Get(id)
	appendCall := func(data M) {
		manager.mu.Lock()
		defer manager.mu.Unlock()
		_, e := manager.appendLocked(fixtureTaskLocked(manager, id), M{"role": "assistant", "blocks": []any{newBlock("call:"+ID(), "tool-call", data)}})
		if e != nil {
			t.Fatal(e)
		}
	}
	appendCall(call)
	index, e := store.InspectionPresentations(S(task["id"]))
	if e != nil || index[inspectionCallKey(call)] == nil {
		t.Fatal(index, e)
	}
	// A matching record without identity makes inheritance ambiguous.
	appendCall(Merge(call, M{"presentation": M{"kind": "generic"}}))
	index, e = store.InspectionPresentations(id)
	if e != nil || len(index) != 0 {
		t.Fatal(index, e)
	}
	appendCall(Merge(call, M{"presentation": M{"kind": "generic", "workspace": Merge(workspace, M{"space": "other"})}}))
	index, e = store.InspectionPresentations(id)
	if e != nil || len(index) != 0 {
		t.Fatal(index, e)
	}
}
