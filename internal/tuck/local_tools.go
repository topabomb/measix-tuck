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
	server       *mcp.Server
	task, key    string
	agent        string
	requests     int
	listed       chan struct{}
	catalogMu    sync.Mutex
	catalog      string
	instructions string
}
type toolPeerContext struct{}
type toolPeerBinding struct {
	peer             *toolPeer
	setCatalogCookie func(*toolPeer)
}

// LocalTools uses the official MCP SDK; the turn binding fills a public header gap.
type LocalTools struct {
	Models   *Models
	Manager  *Manager
	Files    *Files
	Delivery *Deliveries
	mu       sync.RWMutex
	turns    map[string]*toolTurn
	peers    []*toolPeer
	handler  *mcp.StreamableHTTPHandler
}

const tuckToolsPath = "/tuck-tools"
const tuckTurnHeader = "X-Tuck-Turn"

func NewLocalTools(models *Models, manager *Manager, files *Files, delivery *Deliveries) *LocalTools {
	l := &LocalTools{Models: models, Manager: manager, Files: files, Delivery: delivery, turns: map[string]*toolTurn{}}
	l.handler = mcp.NewStreamableHTTPHandler(l.newPeer, &mcp.StreamableHTTPOptions{Stateless: true, PropagateRequestCancellation: true})
	return l
}

// configuredTuckTools is the single configuration permission filter, shared by
// native assembly and the MCP catalog. Runtime resources further narrow it.
func configuredTuckTools(config M, agent string, main bool) []string {
	a := O(O(config["agents"])[agent])
	if !Has(O(config["agents"]), agent) {
		return nil
	}
	var assistant M
	if main {
		for _, v := range A(O(config["workbench"])["assistants"]) {
			if S(O(v)["agent"]) == agent {
				assistant = O(v)
				break
			}
		}
	}
	names := []string{}
	for _, name := range modelContent.ToolNames() {
		if Contains(a["disabledTuckTools"], name) {
			continue
		}
		switch name {
		case "list_conversations", "send_conversation_message", "generate_images":
			if assistant == nil {
				continue
			}
		case "deliver_result":
			if assistant == nil || assistant["deliveryTool"] == false {
				continue
			}
		}
		if B(a["readonly"]) && name != "list_conversations" && name != inspectionTool {
			continue
		}
		names = append(names, name)
	}
	return names
}
func (l *LocalTools) definitions(turn *toolTurn, agent string) []M {
	if l.actorAllowed(turn.Task, agent) != nil {
		return nil
	}
	config := l.Models.Config()
	selected := configuredTuckTools(config, agent, agent == S(turn.Task["agent"]) && S(turn.Task["parentTaskId"]) == "")
	names := []string{}
	var resource M
	for _, name := range selected {
		switch name {
		case "deliver_result":
			if !l.Delivery.Available(turn.Task) {
				continue
			}
		case "upload_runcove_file":
			if turn.Target == nil || turn.Workspace == "" {
				continue
			}
		case "download_runcove_files":
			if turn.Target == nil || writableTaskWorkspace(turn.Task) != nil {
				continue
			}
		case "generate_images":
			if l.imageAllowed(turn) != nil {
				continue
			}
			resource, _ = resolveImage(config, l.Models.Platform.Binding(), agentImageSelector(config, agent))
		}
		names = append(names, name)
	}
	return modelContent.Tools(names, resource)
}
func (l *LocalTools) newPeer(r *http.Request) *mcp.Server {
	agent := r.URL.Query().Get("agent")
	if !agentID.MatchString(agent) {
		return nil
	}
	key := r.Header.Get(tuckTurnHeader)
	if r.Header.Get("Mcp-Method") != "server/discover" {
		if cookie, e := r.Cookie(toolCatalogCookie(agent)); e == nil {
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
	// Only catalog requests may resolve an expired connection identity. Calls
	// always require the exact live turn header and current actor permission.
	if task == "" {
		for _, p := range l.peers {
			if p.key == key && p.agent == agent {
				task = p.task
				break
			}
		}
	}
	if task == "" {
		return nil
	}
	var peer *toolPeer
	for _, p := range l.peers {
		if p.task == task && p.agent == agent {
			peer = p
			break
		}
	}
	if peer == nil {
		if turn == nil || l.actorAllowed(turn.Task, agent) != nil {
			return nil
		}
		defs := l.definitions(turn, agent)
		peer = &toolPeer{server: mcp.NewServer(&mcp.Implementation{Name: "Measix Tuck", Version: "1.0.0"}, &mcp.ServerOptions{Capabilities: &mcp.ServerCapabilities{Tools: &mcp.ToolCapabilities{ListChanged: true}}}), task: task, key: key, agent: agent}
		peer.server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
			return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
				result, err := next(ctx, method, req)
				if err == nil && (method == "initialize" || method == "server/discover") {
					// A retained catalog subscription may span turns. Each new
					// initialization describes the current installed definitions;
					// existing clients keep their own initialization snapshot.
					peer.catalogMu.Lock()
					instructions := peer.instructions
					peer.catalogMu.Unlock()
					switch initialized := result.(type) {
					case *mcp.InitializeResult:
						initialized.Instructions = instructions
					case *mcp.DiscoverResult:
						initialized.Instructions = instructions
					}
				}
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
		l.installDefinitions(peer, defs)
		l.peers = append(l.peers, peer)
	}
	peer.requests++
	if owner, ok := r.Context().Value(toolPeerContext{}).(*toolPeerBinding); ok {
		owner.peer = peer
		if r.Header.Get("Mcp-Method") == "server/discover" && turn != nil && turn.ctx.Err() == nil && l.actorAllowed(turn.Task, agent) == nil {
			owner.setCatalogCookie(peer)
		}
	}
	return peer.server
}
func (l *LocalTools) installTools(peer *toolPeer, turn *toolTurn) bool {
	return l.installDefinitions(peer, l.definitions(turn, peer.agent))
}
func (l *LocalTools) installDefinitions(peer *toolPeer, defs []M) bool {
	peer.catalogMu.Lock()
	defer peer.catalogMu.Unlock()
	encoded := string(JSON(defs))
	if encoded == peer.catalog {
		return false
	}
	peer.catalog = encoded
	peer.instructions = modelContent.Instructions(defs)
	wanted := map[string]bool{}
	for _, d := range defs {
		wanted[S(d["name"])] = true
	}
	for _, name := range modelContent.ToolNames() {
		if !wanted[name] {
			peer.server.RemoveTools(name)
		}
	}
	for _, d := range defs {
		name := S(d["name"])
		tool := &mcp.Tool{Name: name, Description: S(d["description"]), InputSchema: d["inputSchema"]}
		if annotations := O(d["annotations"]); len(annotations) > 0 {
			tool.Annotations = &mcp.ToolAnnotations{ReadOnlyHint: B(annotations["readOnlyHint"])}
		}
		peer.server.AddTool(tool, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			key := ""
			if req.Extra != nil {
				key = req.Extra.Header.Get(tuckTurnHeader)
			}
			l.mu.RLock()
			turn := l.turns[key]
			l.mu.RUnlock()
			if turn == nil || turn.TaskID != peer.task || turn.ctx.Err() != nil {
				return modelContent.MCPResult(modelContent.Failure("tool_not_permitted")), nil
			}
			if e := l.current(turn); e != nil {
				return modelContent.MCPResult(modelContent.Error(e)), nil
			}
			permitted := false
			for _, def := range l.definitions(turn, peer.agent) {
				if S(def["name"]) == name {
					permitted = true
					break
				}
			}
			if !permitted {
				return modelContent.MCPResult(modelContent.Failure("tool_not_permitted")), nil
			}
			value, e := DecodeJSON(req.Params.Arguments, 512<<10)
			if e != nil {
				return modelContent.MCPResult(modelContent.Failure("invalid_arguments")), nil
			}
			args, ok := value.(map[string]any)
			if !ok {
				return modelContent.MCPResult(modelContent.Failure("invalid_arguments")), nil
			}
			combined, cancel := context.WithCancel(ctx)
			stop := context.AfterFunc(turn.ctx, cancel)
			defer stop()
			defer cancel()
			var result M
			switch name {
			case "list_conversations":
				result, e = l.Manager.ListConversations(O(args))
			case "send_conversation_message":
				result, e = l.Manager.SendConversationMessage(combined, O(args))
			case "deliver_result":
				result = l.Delivery.Submit(combined, turn, O(args))
			case "inspect_attachments":
				result = l.inspectAttachments(combined, turn, peer.agent, O(args))
			case "generate_images":
				return l.generateImage(combined, turn, O(args)), nil
			default:
				result = l.Transfer(combined, turn, name, O(args))
			}
			if e != nil {
				result = modelContent.Error(e)
			}
			return modelContent.MCPResult(modelContent.ToolResult(name, result)), nil
		})
	}
	return true
}
func (l *LocalTools) imageAllowed(turn *toolTurn) error {
	config := l.Models.Config()
	if !Contains(configuredTuckTools(config, S(turn.Task["agent"]), S(turn.Task["parentTaskId"]) == ""), "generate_images") || turn.Workspace == "" || writableTaskWorkspace(turn.Task) != nil {
		return Problem(403, "tool_not_permitted", "生图需要可写的主任务目录及助手工具许可")
	}
	_, e := resolveImage(config, l.Models.Platform.Binding(), agentImageSelector(config, S(turn.Task["agent"])))
	return e
}

func (l *LocalTools) generateImage(ctx context.Context, turn *toolTurn, args M) *mcp.CallToolResult {
	fail := func(e error) *mcp.CallToolResult { return modelContent.MCPResult(modelContent.Error(e)) }
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
		value["textOnlyModel"] = true
		value["canInspect"] = l.Files.canInspect(turn.Task)
	}
	if len(result.Usage) > 0 {
		value["providerUsage"] = result.Usage
	}
	return modelContent.MCPResult(modelContent.ToolResult("generate_images", value), result.Images...)
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

	headers := map[string]string{tuckTurnHeader: turn.Key}
	finish := func() { cancel(); l.mu.Lock(); delete(l.turns, turn.Key); l.mu.Unlock() }
	return headers, finish, nil
}
func (l *LocalTools) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !Equal(r.Header.Get("Authorization"), "Bearer "+l.Models.Secret) {
		WriteError(w, Problem(403, "tool_not_permitted", "Internal tool authentication required"))
		return
	}
	if r.URL.Path != tuckToolsPath {
		WriteError(w, Problem(404, "not_found", "Tool endpoint not found"))
		return
	}
	// The current MCP protocol uses ephemeral requests. Retain peers only
	// while the SDK owns a live session (including legacy notification streams).
	owner := &toolPeerBinding{setCatalogCookie: func(peer *toolPeer) {
		// Reconnecting clients must retain the selected peer's stable catalog
		// identity, even when discovery carries a later turn's execution key.
		// This cookie locates a catalog and never authorizes execution.
		http.SetCookie(w, &http.Cookie{Name: toolCatalogCookie(peer.agent), Value: peer.key, Path: tuckToolsPath, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	}}
	r = r.WithContext(context.WithValue(r.Context(), toolPeerContext{}, owner))
	defer func() {
		peer := owner.peer
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
	l.handler.ServeHTTP(w, r)
}
func toolCatalogCookie(agent string) string { return "tuck-catalog-" + Hash([]byte(agent))[:16] }

func (l *LocalTools) actorAllowed(task M, agent string) error {
	config := l.Models.Config()
	agents := O(config["agents"])
	seen := map[string]bool{}
	var visit func(string) bool
	visit = func(name string) bool {
		if seen[name] || !Has(agents, name) {
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
	if !visit(S(task["agent"])) {
		return Problem(403, "tool_not_permitted", "此 Agent 不属于当前任务的代理集合")
	}

	return nil
}

func (l *LocalTools) inspectionAllowed(task M, agent string) error {
	if e := l.actorAllowed(task, agent); e != nil {
		return e
	}
	if Contains(O(O(l.Models.Config()["agents"])[agent])["disabledTuckTools"], inspectionTool) {
		return Problem(403, "tool_not_permitted", "附件识别已停用")
	}
	return nil
}

func (l *LocalTools) inspectAttachments(ctx context.Context, turn *toolTurn, agent string, args M) (result M) {
	fail := modelContent.Error
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

func ctxError(ctx context.Context) error {
	if e := ctx.Err(); e != nil {
		return Problem(409, "operation_canceled", fmt.Sprintf("Operation canceled: %v", e))
	}
	return nil
}
