package tuck

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type toolTurn struct {
	Key, TaskID, TurnID, Workspace, Identity string
	Task, Target, previewWorkspace           M
	workspaceInfo                            os.FileInfo
	ctx                                      context.Context
	cancel                                   context.CancelFunc
	deliveryMu                               sync.Mutex
}
type toolPeer struct {
	server          *mcp.Server
	task, kind, key string
	agent           string
	requests        int
	listed          chan struct{}
	catalogMu       sync.Mutex
	catalog         string
}
type toolPeerContext struct{}

// LocalTools uses the official MCP SDK; the turn binding fills a public header gap.
type LocalTools struct {
	Models                          *Models
	Manager                         *Manager
	Files                           *Files
	Delivery                        *Deliveries
	mu                              sync.RWMutex
	turns                           map[string]*toolTurn
	peers                           []*toolPeer
	delivery, workspace, inspection *mcp.StreamableHTTPHandler
}

func NewLocalTools(models *Models, manager *Manager, files *Files, delivery *Deliveries) *LocalTools {
	l := &LocalTools{Models: models, Manager: manager, Files: files, Delivery: delivery, turns: map[string]*toolTurn{}}
	l.delivery = mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server { return l.newPeer(r, "delivery") }, &mcp.StreamableHTTPOptions{Stateless: true, PropagateRequestCancellation: true})
	l.workspace = mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server { return l.newPeer(r, "workspace") }, &mcp.StreamableHTTPOptions{Stateless: true, PropagateRequestCancellation: true})
	l.inspection = mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server { return l.newPeer(r, "inspection") }, &mcp.StreamableHTTPOptions{Stateless: true, PropagateRequestCancellation: true})
	return l
}
func (l *LocalTools) newPeer(r *http.Request, kind string) *mcp.Server {
	agent := ""
	if kind == "inspection" {
		agent = r.URL.Query().Get("agent")
		if !agentID.MatchString(agent) {
			return nil
		}
	}
	header := "X-Tuck-Workspace-Turn"
	if kind == "delivery" {
		header = "X-Tuck-Delivery-Turn"
	}
	key := r.Header.Get(header)
	if r.Header.Get("Mcp-Method") != "server/discover" {
		if cookie, e := r.Cookie(toolCatalogCookie(kind, agent)); e == nil {
			key = cookie.Value
		}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	turn := l.turns[key]
	task := ""
	if turn != nil {
		task = turn.TaskID
	}
	// Catalog refreshes use the connection's original headers. Only SDK peers
	// that still own a subscription/session may resolve that original identity;
	// tool calls below still require the exact live turn header.
	if task == "" {
		for _, peer := range l.peers {
			if peer.key == key && peer.kind == kind && peer.agent == agent {
				task = peer.task
				break
			}
		}
	}
	if task == "" {
		return nil
	}
	var peer *toolPeer
	for _, existing := range l.peers {
		if existing.task == task && existing.kind == kind && existing.agent == agent {
			peer = existing
			break
		}
	}
	if peer == nil {
		if turn == nil {
			return nil
		}
		peer = &toolPeer{server: mcp.NewServer(&mcp.Implementation{Name: "Measix Tuck " + kind, Version: "1.0.0"}, &mcp.ServerOptions{Capabilities: &mcp.ServerCapabilities{Tools: &mcp.ToolCapabilities{ListChanged: true}}}), task: task, kind: kind, key: key, agent: agent}
		peer.server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
			return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
				result, err := next(ctx, method, req)
				if method == "tools/list" && err == nil {
					l.mu.Lock()
					if peer.listed != nil {
						close(peer.listed)
						peer.listed = nil
					}
					l.mu.Unlock()
				}
				return result, err
			}
		})
		l.installTools(peer, turn)
		l.peers = append(l.peers, peer)
	}
	peer.requests++
	if owner, ok := r.Context().Value(toolPeerContext{}).(**toolPeer); ok {
		*owner = peer
	}
	return peer.server
}
func (l *LocalTools) installTools(peer *toolPeer, turn *toolTurn) bool {
	peer.catalogMu.Lock()
	defer peer.catalogMu.Unlock()
	definitions := []M{}
	if peer.kind == "inspection" {
		if l.inspectionAllowed(turn.Task, peer.agent) == nil {
			definitions = append(definitions, inspectionDefinition())
		}
	} else if peer.kind == "delivery" {
		if l.Delivery.Available(turn.Task) {
			definitions = append(definitions, deliveryDefinition())
		}
	} else if turn.Target != nil && !B(O(O(l.Models.Config()["agents"])[S(turn.Task["agent"])])["readonly"]) {
		definitions = workspaceDefinitions(turn.Target)
		if writableTaskWorkspace(turn.Task) != nil {
			kept := []M{}
			for _, definition := range definitions {
				if S(definition["name"]) != "download_runcove_files" {
					kept = append(kept, definition)
				}
			}
			definitions = kept
		}
	}
	if peer.kind == "workspace" && l.imageAllowed(turn) == nil {
		config := l.Models.Config()
		resource, _ := resolveImage(config, l.Models.Platform.Binding(), agentImageSelector(config, S(turn.Task["agent"])))
		definitions = append(definitions, imageDefinition(resource))
	}
	encoded := string(JSON(definitions))
	if encoded == peer.catalog {
		return false
	}
	peer.catalog = encoded
	wanted := map[string]bool{}
	for _, definition := range definitions {
		wanted[S(definition["name"])] = true
	}
	for _, name := range []string{"deliver_result", "upload_runcove_file", "download_runcove_files", "generate_images", "inspect_attachments"} {
		if !wanted[name] {
			peer.server.RemoveTools(name)
		}
	}
	for _, definition := range definitions {
		definition := definition
		name := S(definition["name"])
		tool := &mcp.Tool{Name: name, Description: S(definition["description"]), InputSchema: definition["inputSchema"]}
		if name == "inspect_attachments" {
			tool.Annotations = &mcp.ToolAnnotations{ReadOnlyHint: true}
		}
		peer.server.AddTool(tool, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			header := "X-Tuck-Workspace-Turn"
			if peer.kind == "delivery" {
				header = "X-Tuck-Delivery-Turn"
			}
			key := ""
			if req.Extra != nil {
				key = req.Extra.Header.Get(header)
			}
			l.mu.RLock()
			current := l.turns[key]
			l.mu.RUnlock()
			if current == nil || current.TaskID != peer.task || current.ctx.Err() != nil {
				return localResult(toolFailure("tool_not_permitted", "The task context for this turn has expired.")), nil
			}
			value, e := DecodeJSON(req.Params.Arguments, 512<<10)
			if e != nil {
				return localResult(toolFailure("invalid_arguments", "Provide a valid JSON object matching the tool parameters.")), nil
			}
			combined, cancel := context.WithCancel(ctx)
			stop := context.AfterFunc(current.ctx, cancel)
			defer stop()
			defer cancel()
			if peer.kind == "inspection" {
				return localResult(toolOutput(name, l.inspectAttachments(combined, current, peer.agent, O(value)))), nil
			}
			if peer.kind == "delivery" {
				return localResult(toolOutput(name, l.Delivery.Submit(combined, current, O(value)))), nil
			}
			if name == "generate_images" {
				return l.generateImage(combined, current, O(value)), nil
			}
			return localResult(toolOutput(name, l.Transfer(combined, current, name, O(value)))), nil
		})
	}
	return true
}
func localResult(value M) *mcp.CallToolResult {
	return &mcp.CallToolResult{IsError: S(value["status"]) != "completed", StructuredContent: value, Content: []mcp.Content{&mcp.TextContent{Text: string(JSON(value))}}}
}

func (l *LocalTools) imageAllowed(turn *toolTurn) error {
	agent := O(O(l.Models.Config()["agents"])[S(turn.Task["agent"])])
	if len(agent) == 0 || B(agent["readonly"]) || Contains(agent["disabledTuckTools"], "generate_images") || turn.Workspace == "" || writableTaskWorkspace(turn.Task) != nil || S(turn.Task["parentTaskId"]) != "" {
		return Problem(403, "tool_not_permitted", "生图需要可写的主任务目录及助手工具许可")
	}
	config := l.Models.Config()
	_, e := resolveImage(config, l.Models.Platform.Binding(), agentImageSelector(config, S(turn.Task["agent"])))
	return e
}

func imageDefinition(resource M) M {
	maxCount := 4
	description := toolText(textGenerate)
	if provider := S(resource["providerName"]); provider != "" {
		description += toolText(textGenerateProvider, map[string]string{"provider": provider})
	}
	description += toolText(textGenerateModel, map[string]string{"model": firstString(resource["upstreamModelKey"], resource["displayName"], "Unspecified")})
	if S(resource["source"]) == "platform" {
		maxCount = min(4, I(resource["maxImagesPerRequest"]))
		description += toolText(textGeneratePlatform)
	}
	return M{"name": "generate_images", "description": description, "inputSchema": M{"type": "object", "additionalProperties": false, "required": []string{"prompt"}, "properties": M{
		"prompt":           M{"type": "string", "minLength": 1, "maxLength": 32000, "description": toolText(textPrompt)},
		"count":            M{"type": []string{"integer", "null"}, "minimum": 1, "maximum": maxCount, "default": 1, "description": toolText(textCount)},
		"reference_images": M{"type": []string{"array", "null"}, "maxItems": 4, "uniqueItems": true, "items": M{"type": "string", "minLength": 1, "maxLength": 1024}, "description": toolText(textReferences)},
	}}}
}

func (l *LocalTools) generateImage(ctx context.Context, turn *toolTurn, args M) *mcp.CallToolResult {
	fail := func(e error) *mcp.CallToolResult { return localResult(toolError(e)) }
	// Native strict schemas may require nullable optional fields.
	for _, field := range []string{"reference_images", "count"} {
		if args[field] == nil {
			delete(args, field)
		}
	}
	check := func() error {
		if e := ctxError(ctx); e != nil {
			return e
		}
		if e := l.current(turn); e != nil {
			return e
		}
		if e := l.imageAllowed(turn); e != nil {
			return e
		}
		if e := checkTurnWorkspace(turn); e != nil {
			return e
		}
		resolved, e := filepath.EvalSymlinks(S(turn.Task["workspace"]))
		if e != nil || resolved != turn.Workspace {
			return Problem(409, "workspace_changed", "任务目录已改变")
		}
		return l.Delivery.CheckWorkspace(resolved)
	}
	if e := check(); e != nil {
		return fail(e)
	}
	if !noUnknown(args, "prompt", "reference_images", "count") || !stringField(args["prompt"], 1, 32000) || Has(args, "reference_images") && (!isArray(args["reference_images"]) || len(A(args["reference_images"])) > 4 || !uniqueStrings(args["reference_images"], func(s string) bool { return stringField(s, 1, 1024) })) || Has(args, "count") && !Integer(args["count"], 1, 4) {
		return fail(Problem(422, "invalid_arguments", "生图参数无效"))
	}
	count := 1
	if Has(args, "count") {
		count = I(args["count"])
	}
	config, binding := l.Models.Config(), l.Models.Platform.Binding()
	resource, e := resolveImage(config, binding, agentImageSelector(config, S(turn.Task["agent"])))
	if e != nil {
		return fail(e)
	}
	if S(resource["source"]) == "platform" && turn.Identity != platformIdentity(binding) {
		return fail(Problem(409, "resource_changed", "平台身份或资源已改变"))
	}
	if S(resource["source"]) == "platform" && len(A(args["reference_images"])) > 0 {
		return fail(Problem(422, "image_references_unsupported", "平台合同尚未提供参考图编辑路由，请使用支持 OpenAI Images Edits 的本地配置资源"))
	}
	references, e := l.Files.ReadImageReferences(ctx, turn.Task, A(args["reference_images"]))
	if e != nil {
		return fail(e)
	}
	if e = check(); e != nil {
		return fail(e)
	}
	result, e := l.Models.GenerateImage(ctx, config, binding, ImageGenerationRequest{Resource: S(resource["selector"]), Prompt: S(args["prompt"]), Count: count, References: references})
	if e != nil {
		return fail(e)
	}
	if e = check(); e != nil {
		return fail(e)
	}
	if S(resource["source"]) == "platform" && turn.Identity != platformIdentity(l.Models.Platform.Binding()) {
		return fail(Problem(409, "resource_changed", "平台身份或资源已改变"))
	}
	files, e := l.Files.SaveGeneratedImages(ctx, turn.Task, result.Images, S(args["prompt"]))
	if e != nil {
		return fail(e)
	}
	value := M{"status": "completed", "resource": result.Resource["selector"], "files": files}
	if !Contains(l.Models.find(S(turn.Task["modelId"]))["inputModalities"], "IMAGE") {
		value["notice"] = "当前聊天模型未声明图片输入：文件已生成，但不要声称已看见图片。需要检查时，请用 inspect_attachments 读取返回的 path；该工具需要已配置的识图模型及许可。"
	}
	if len(result.Usage) > 0 {
		value["providerUsage"] = result.Usage
	}
	response := localResult(toolOutput("generate_images", value))
	for _, img := range result.Images {
		response.Content = append(response.Content, &mcp.ImageContent{Data: img.ModelBytes, MIMEType: img.ModelMIME})
	}
	return response
}
func platformIdentity(b M) string {
	if b == nil {
		return ""
	}
	return strings.Join([]string{S(b["origin"]), S(b["deploymentId"]), S(b["userId"]), S(b["sessionId"]), S(O(b["snapshot"])["snapshotHash"])}, "\x00")
}
func (l *LocalTools) Begin(ctx context.Context, task M) (map[string]string, func(), error) {
	workspace := ""
	if S(task["workspace"]) != "" {
		var e error
		workspace, e = filepath.EvalSymlinks(S(task["workspace"]))
		if e != nil {
			return nil, nil, e
		}
	}
	turnCtx, cancel := context.WithCancel(ctx)
	turn := &toolTurn{Key: UUID(), TaskID: S(task["id"]), TurnID: S(task["turnId"]), Workspace: workspace, Task: Clone(task), Identity: platformIdentity(l.Models.Platform.Binding()), ctx: turnCtx, cancel: cancel}
	if workspace != "" {
		var e error
		turn.workspaceInfo, e = os.Stat(workspace)
		if e != nil {
			cancel()
			return nil, nil, e
		}
	}
	if e := l.Delivery.CheckWorkspace(workspace); e != nil && S(task["workspaceMode"]) != "none" {
		cancel()
		return nil, nil, e
	}
	if turn.Identity != "" {
		projection, e := l.projection(turnCtx)
		if e == nil {
			binding := l.Models.Platform.Binding()
			if platformIdentity(binding) == turn.Identity {
				turn.Target = workspaceTarget(binding, projection)
				turn.previewWorkspace = inspectionWorkspace(binding, turn.Target)
			}
		}
	}

	l.mu.Lock()
	l.turns[turn.Key] = turn
	peers := append([]*toolPeer{}, l.peers...)
	l.mu.Unlock()
	for _, peer := range peers {
		if peer.task != turn.TaskID {
			continue
		}
		// Await the official SDK's list-changed/list-tools exchange before the
		// engine reads its cached catalog for this turn. This is one bounded
		// catalog operation, not a connection timeout or a retry loop.
		listed := make(chan struct{})
		l.mu.Lock()
		peer.listed = listed
		l.mu.Unlock()
		if !l.installTools(peer, turn) {
			l.mu.Lock()
			peer.listed = nil
			l.mu.Unlock()
			continue
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-listed:
		case <-turnCtx.Done():
		case <-timer.C:
		}
		timer.Stop()
		l.mu.Lock()
		if peer.listed == listed {
			peer.listed = nil
		}
		l.mu.Unlock()
	}

	headers := map[string]string{"X-Tuck-Workspace-Turn": turn.Key, "X-Tuck-Delivery-Turn": turn.Key}
	finish := func() { cancel(); l.mu.Lock(); delete(l.turns, turn.Key); l.mu.Unlock() }
	return headers, finish, nil
}
func (l *LocalTools) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !Equal(r.Header.Get("Authorization"), "Bearer "+l.Models.Secret) {
		WriteError(w, Problem(403, "tool_not_permitted", "Internal tool authentication required"))
		return
	}
	// The SDK starts subscription requests from a background context, where
	// Docker Agent v1.150.0 cannot expand turn headers. Its official HTTP client
	// retains cookies. Bind catalog-only requests to that client's original
	// task; execution continues to require the live turn header in installTools.
	if r.Header.Get("Mcp-Method") == "server/discover" {
		kind, header := "workspace", "X-Tuck-Workspace-Turn"
		agent := ""
		if r.URL.Path == "/api/tools/delivery" {
			kind, header = "delivery", "X-Tuck-Delivery-Turn"
		} else if r.URL.Path == "/attachment-tools" {
			kind, agent = "inspection", r.URL.Query().Get("agent")
		}
		key := r.Header.Get(header)
		l.mu.RLock()
		valid := l.turns[key] != nil
		l.mu.RUnlock()
		if valid {
			http.SetCookie(w, &http.Cookie{Name: toolCatalogCookie(kind, agent), Value: key, Path: r.URL.Path, HttpOnly: true, SameSite: http.SameSiteStrictMode})
		}
	}
	// The current MCP protocol uses ephemeral requests. Retain peers only
	// while the SDK owns a live session (including legacy notification streams).
	var peer *toolPeer
	r = r.WithContext(context.WithValue(r.Context(), toolPeerContext{}, &peer))
	defer func() {
		if peer == nil {
			return
		}
		l.mu.Lock()
		defer l.mu.Unlock()
		peer.requests--
		if peer.requests > 0 {
			return
		}
		for range peer.server.Sessions() {
			return
		}
		for i, existing := range l.peers {
			if existing == peer {
				l.peers = append(l.peers[:i], l.peers[i+1:]...)
				break
			}
		}
	}()
	if r.URL.Path == "/api/tools/delivery" {
		l.delivery.ServeHTTP(w, r)
	} else if r.URL.Path == "/attachment-tools" {
		l.inspection.ServeHTTP(w, r)
	} else if r.URL.Path == "/workspace-tools" {
		l.workspace.ServeHTTP(w, r)
	} else {
		WriteError(w, Problem(404, "not_found", "Tool not found"))
	}
}

func toolCatalogCookie(kind, agent string) string {
	if kind == "inspection" {
		kind += "-" + Hash([]byte(agent))[:16]
	}
	return "tuck-" + kind + "-catalog"
}

func inspectionDefinition() M {
	return M{"name": inspectionTool, "description": toolText(textInspect), "inputSchema": M{"type": "object", "additionalProperties": false, "required": []string{"attachments", "request"}, "properties": M{
		"attachments": M{"type": "array", "minItems": 1, "maxItems": 4, "uniqueItems": true, "description": toolText(textAttachments), "items": M{"type": "object", "additionalProperties": false, "required": []string{"path"}, "properties": M{
			"source": M{"type": []string{"string", "null"}, "enum": []any{"local", "runcove", nil}, "default": "local", "description": toolText(textAttachmentSource)},
			"path":   M{"type": "string", "minLength": 1, "maxLength": 4096, "description": toolText(textAttachmentPath)},
		}}},
		"request": M{"type": "string", "minLength": 1, "maxLength": 32000, "description": toolText(textRequest)},
	}}}
}

func (l *LocalTools) inspectionAllowed(task M, agent string) error {
	config := l.Models.Config()
	agents := O(config["agents"])
	seen := map[string]bool{}
	var visit func(string) bool
	visit = func(name string) bool {
		if seen[name] || len(O(agents[name])) == 0 {
			return false
		}
		seen[name] = true
		if name == agent {
			return true
		}
		for _, child := range Strings(O(agents[name])["sub_agents"]) {
			if visit(child) {
				return true
			}
		}
		return false
	}
	if !visit(S(task["agent"])) || Contains(O(agents[agent])["disabledTuckTools"], inspectionTool) {
		return Problem(403, "tool_not_permitted", "此 Agent 未获附件识别工具许可")
	}

	return nil
}

func (l *LocalTools) inspectAttachments(ctx context.Context, turn *toolTurn, agent string, args M) (result M) {
	fail := toolError
	check := func() error {
		if e := ctxError(ctx); e != nil {
			return e
		}
		if e := l.current(turn); e != nil {
			return e
		}
		if e := l.inspectionAllowed(turn.Task, agent); e != nil {
			return e
		}
		remote := false
		for _, value := range A(args["attachments"]) {
			ref, e := parseToolFile(value, false)
			if e != nil {
				return e
			}
			if ref.Source == "local" {
				if e := checkTurnWorkspace(turn); e != nil {
					return e
				}
				workspace, e := filepath.EvalSymlinks(S(turn.Task["workspace"]))
				if e != nil || workspace != turn.Workspace {
					return Problem(409, "workspace_changed", "任务目录已改变")
				}
				if e = l.Delivery.CheckWorkspace(workspace); e != nil {
					return e
				}
			} else {
				remote = true
			}
		}
		if remote {
			return l.remoteGate(ctx, turn)
		}
		return nil
	}
	if e := check(); e != nil {
		return fail(e)
	}
	if !noUnknown(args, "attachments", "request") || !stringField(args["request"], 1, 32000) || strings.TrimSpace(S(args["request"])) == "" || !isArray(args["attachments"]) || len(A(args["attachments"])) < 1 || len(A(args["attachments"])) > 4 {
		return fail(Problem(422, "invalid_arguments", "请传入 1–4 个 attachments 文件路径和非空 request 识别要求"))
	}
	model, e := l.Models.InspectionModel(agent)
	if e != nil {
		return fail(e)
	}
	config, binding := l.Models.Config(), l.Models.Platform.Binding()
	if platformIdentity(binding) != turn.Identity {
		return fail(Problem(409, "resource_changed", "平台身份已改变"))
	}
	images, e := l.readToolAttachments(ctx, turn, A(args["attachments"]))
	if e != nil {
		return fail(e)
	}
	if e = check(); e != nil {
		return fail(e)
	}
	text, e := l.Models.InspectAttachments(ctx, config, binding, model, S(args["request"]), images)
	if e != nil {
		return fail(e)
	}
	if e = check(); e != nil {
		return fail(e)
	}
	if platformIdentity(l.Models.Platform.Binding()) != turn.Identity {
		return fail(Problem(409, "resource_changed", "平台身份已改变"))
	}
	return M{"status": "completed", "text": text}
}
func (l *LocalTools) current(turn *toolTurn) error {
	l.mu.RLock()
	current := l.turns[turn.Key] == turn
	l.mu.RUnlock()
	if !current || turn.ctx.Err() != nil {
		return Problem(403, "tool_not_permitted", "The task context for this turn has expired.")
	}
	task, e := l.Manager.Get(turn.TaskID)
	if e != nil {
		return e
	}
	if S(task["turnId"]) != turn.TurnID || S(task["parentTaskId"]) != "" {
		return Problem(403, "tool_not_permitted", "This call does not belong to the active main conversation.")
	}
	return nil
}
func (l *LocalTools) Close() {
	l.mu.Lock()
	for _, turn := range l.turns {
		turn.cancel()
	}
	peers := l.peers
	l.peers = nil
	l.mu.Unlock()
	for _, peer := range peers {
		for session := range peer.server.Sessions() {
			session.Close()
		}
	}
}
func workspaceDefinitions(target M) []M {
	return []M{
		{"name": "upload_runcove_file", "description": toolText(textUpload), "inputSchema": M{"type": "object", "additionalProperties": false, "required": []string{"file_path", "remote_path"}, "properties": M{
			"file_path": M{"type": "string", "description": toolText(textUploadPath)}, "remote_path": M{"type": "string", "description": toolText(textUploadRemote)}, "overwrite": M{"type": "boolean", "default": false, "description": toolText(textUploadOverwrite)},
		}}},
		{"name": "download_runcove_files", "description": toolText(textDownload), "inputSchema": M{"type": "object", "additionalProperties": false, "required": []string{"remote_paths", "local_directory"}, "properties": M{
			"remote_paths": M{"type": "array", "minItems": 1, "maxItems": 20, "description": toolText(textDownloadRemote), "items": M{"type": "string", "minLength": 1, "maxLength": 4096}}, "local_directory": M{"type": "string", "minLength": 1, "maxLength": 4096, "description": toolText(textDownloadDirectory)}, "overwrite": M{"type": "boolean", "default": false, "description": toolText(textDownloadOverwrite)},
		}}},
	}
}
func deliveryDefinition() M {
	return M{"name": "deliver_result", "description": toolText(textDelivery), "inputSchema": M{"type": "object", "additionalProperties": false, "required": []string{"summary"}, "properties": M{
		"summary": M{"type": "string", "minLength": 1, "maxLength": 1000, "description": toolText(textSummary)},
		"files": M{"type": []string{"array", "null"}, "maxItems": 10, "description": toolText(textDeliveryFiles), "items": M{"type": "object", "additionalProperties": false, "required": []string{"path"}, "properties": M{
			"source":      M{"type": []string{"string", "null"}, "enum": []any{"local", "runcove", nil}, "default": "local", "description": toolText(textDeliverySource)},
			"path":        M{"type": "string", "minLength": 1, "maxLength": 4096, "description": toolText(textDeliveryPath)},
			"description": M{"type": []string{"string", "null"}, "minLength": 1, "maxLength": 1000, "description": toolText(textFileDescription)},
		}}},
	}}}
}

func ctxError(ctx context.Context) error {
	if e := ctx.Err(); e != nil {
		return Problem(409, "operation_canceled", fmt.Sprintf("Operation canceled: %v", e))
	}
	return nil
}
