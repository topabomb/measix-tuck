package tuck

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type toolTurn struct {
	Key, TaskID, TurnID, Workspace, Identity string
	Task, Target                             M
	ctx                                      context.Context
	cancel                                   context.CancelFunc
	deliveryMu                               sync.Mutex
}
type toolPeer struct {
	server          *mcp.Server
	task, kind, key string
	requests        int
	listed          chan struct{}
	catalogMu       sync.Mutex
	catalog         string
}
type toolPeerContext struct{}

// LocalTools uses the official MCP SDK; the turn binding fills a public header gap.
type LocalTools struct {
	Models              *Models
	Manager             *Manager
	Files               *Files
	Delivery            *Deliveries
	mu                  sync.RWMutex
	turns               map[string]*toolTurn
	peers               []*toolPeer
	delivery, workspace *mcp.StreamableHTTPHandler
}

func NewLocalTools(models *Models, manager *Manager, files *Files, delivery *Deliveries) *LocalTools {
	l := &LocalTools{Models: models, Manager: manager, Files: files, Delivery: delivery, turns: map[string]*toolTurn{}}
	l.delivery = mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server { return l.newPeer(r, "delivery") }, &mcp.StreamableHTTPOptions{Stateless: true, PropagateRequestCancellation: true})
	l.workspace = mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server { return l.newPeer(r, "workspace") }, &mcp.StreamableHTTPOptions{Stateless: true, PropagateRequestCancellation: true})
	return l
}
func (l *LocalTools) newPeer(r *http.Request, kind string) *mcp.Server {
	header := "X-Tuck-Workspace-Turn"
	if kind == "delivery" {
		header = "X-Tuck-Delivery-Turn"
	}
	key := r.Header.Get(header)
	if r.Header.Get("Mcp-Method") != "server/discover" {
		if cookie, e := r.Cookie("tuck-" + kind + "-catalog"); e == nil {
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
			if peer.key == key && peer.kind == kind {
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
		if existing.task == task && existing.kind == kind {
			peer = existing
			break
		}
	}
	if peer == nil {
		if turn == nil {
			return nil
		}
		peer = &toolPeer{server: mcp.NewServer(&mcp.Implementation{Name: "Measix Tuck " + kind, Version: "1.0.0"}, &mcp.ServerOptions{Capabilities: &mcp.ServerCapabilities{Tools: &mcp.ToolCapabilities{ListChanged: true}}}), task: task, kind: kind, key: key}
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
	if peer.kind == "delivery" {
		if l.Delivery.Available(turn.Task) {
			definitions = append(definitions, deliveryDefinition())
		}
	} else if turn.Target != nil && !B(O(O(l.Models.Config()["agents"])[S(turn.Task["agent"])])["readonly"]) {
		definitions = workspaceDefinitions(turn.Target)
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
	for _, name := range []string{"deliver_result", "webdav_upload", "webdav_download_files"} {
		if !wanted[name] {
			peer.server.RemoveTools(name)
		}
	}
	for _, definition := range definitions {
		definition := definition
		name := S(definition["name"])
		peer.server.AddTool(&mcp.Tool{Name: name, Description: S(definition["description"]), InputSchema: definition["inputSchema"]}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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
				return localResult(M{"status": "failed", "reason": "tool_not_permitted", "code": "tool_not_permitted", "detail": "The task context for this turn has expired."}), nil
			}
			value, e := DecodeJSON(req.Params.Arguments, 512<<10)
			if e != nil {
				return localResult(M{"status": "failed", "code": "invalid_arguments", "message": e.Error()}), nil
			}
			combined, cancel := context.WithCancel(ctx)
			stop := context.AfterFunc(current.ctx, cancel)
			defer stop()
			defer cancel()
			if peer.kind == "delivery" {
				return localResult(l.Delivery.Submit(combined, current, O(value))), nil
			}
			return localResult(l.Transfer(combined, current, name, O(value))), nil
		})
	}
	return true
}
func localResult(value M) *mcp.CallToolResult {
	return &mcp.CallToolResult{IsError: S(value["status"]) != "completed", StructuredContent: value, Content: []mcp.Content{&mcp.TextContent{Text: string(JSON(value))}}}
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
	if e := l.Delivery.CheckWorkspace(workspace); e != nil && S(task["workspaceMode"]) != "none" {
		cancel()
		return nil, nil, e
	}
	if turn.Identity != "" {
		projection, e := l.projection(turnCtx)
		if e == nil {
			b := l.Models.Platform.Binding()
			for _, v := range A(O(b["snapshot"])["webdav"]) {
				d := O(v)
				id := S(d["resourceId"])
				dav := O(projection["webdav"])
				if id == S(projection["fileExchangeWebdavResourceId"]) && B(d["enabled"]) && S(d["purpose"]) == "WORKSPACE_FILE_EXCHANGE" && S(d["clientProtocol"]) == "WEBDAV" && S(d["authOwnership"]) == "ENTERPRISE_MANAGED" && S(dav["resourceId"]) == id && B(projection["filesAvailable"]) && B(dav["available"]) && Contains([]string{"READ_ONLY", "FULL"}, S(dav["accessMode"])) && strings.HasPrefix(S(projection["agentSpaceId"]), "spc_") && S(d["rootPathTemplate"]) == "/runtime/v1/resources/"+id+"/spaces/{agentSpaceId}/dav/" && platformIdentity(b) == turn.Identity {
					turn.Target = M{"resourceId": id, "space": projection["agentSpaceId"], "revision": dav["bindingRevision"], "mode": dav["accessMode"], "name": d["displayName"], "root": S(b["origin"]) + strings.ReplaceAll(S(d["rootPathTemplate"]), "{agentSpaceId}", S(projection["agentSpaceId"]))}
					break
				}
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
		if r.URL.Path == "/api/tools/delivery" {
			kind, header = "delivery", "X-Tuck-Delivery-Turn"
		}
		key := r.Header.Get(header)
		l.mu.RLock()
		valid := l.turns[key] != nil
		l.mu.RUnlock()
		if valid {
			http.SetCookie(w, &http.Cookie{Name: "tuck-" + kind + "-catalog", Value: key, Path: r.URL.Path, HttpOnly: true, SameSite: http.SameSiteStrictMode})
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
	} else if r.URL.Path == "/workspace-tools" {
		l.workspace.ServeHTTP(w, r)
	} else {
		WriteError(w, Problem(404, "not_found", "Tool not found"))
	}
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
func toolFailure(code, message string) M {
	return M{"status": "failed", "code": code, "reason": code, "message": message, "detail": message}
}
func workspaceDefinitions(target M) []M {
	description := " Current enterprise WebDAV: " + S(target["name"]) + ". Reserved .agent-space* paths are unavailable; copy a selected artifact into an ordinary project directory before transfer."
	return []M{{"name": "webdav_upload", "description": "Upload one existing task file. Both paths must be relative: file_path starts at this task's local working directory; remote_path starts at the enterprise Files root, without a leading slash or /workspace prefix. Create remote parents. Default no overwrite; overwrite=true requires a strong ETag. Check unknown outcomes before retrying." + description, "inputSchema": M{"type": "object", "additionalProperties": false, "required": []any{"file_path", "remote_path"}, "properties": M{"file_path": M{"type": "string", "description": "Relative task file path, e.g. report.txt; never an absolute drive path."}, "remote_path": M{"type": "string", "description": "Relative enterprise Files path, e.g. reports/report.txt; never /workspace/reports/report.txt."}, "overwrite": M{"type": "boolean", "default": false}}}}, {"name": "webdav_download_files", "description": "Download 1–20 enterprise files into a relative task directory. Remote paths start at the enterprise Files root, without a leading slash or /workspace prefix. Keep basenames, reject duplicates before any download, preserve completed files and old bytes on failure. Return metadata only." + description, "inputSchema": M{"type": "object", "additionalProperties": false, "required": []any{"remote_paths", "local_directory"}, "properties": M{"remote_paths": M{"type": "array", "minItems": 1, "maxItems": 20, "items": M{"type": "string", "minLength": 1, "maxLength": 4096, "description": "Relative enterprise Files path, e.g. reports/report.txt."}}, "local_directory": M{"type": "string", "minLength": 1, "maxLength": 4096, "description": "Relative directory under this task's local working directory, e.g. downloads."}, "overwrite": M{"type": "boolean", "default": false}}}}}
}
func deliveryDefinition() M {
	return M{"name": "deliver_result", "description": "Submit the results as a new delivery version. Provide a brief summary and only key files needed for delivery.", "inputSchema": M{"type": "object", "additionalProperties": false, "required": []any{"summary"}, "properties": M{"summary": M{"type": "string", "minLength": 1, "maxLength": 1000}, "files": M{"type": []any{"array", "null"}, "maxItems": 10, "items": M{"type": "object", "additionalProperties": false, "required": []any{"path"}, "properties": M{"path": M{"type": "string", "minLength": 1, "maxLength": 4096}, "description": M{"type": []any{"string", "null"}, "minLength": 1, "maxLength": 1000}}}}}}}
}
func ctxError(ctx context.Context) error {
	if e := ctx.Err(); e != nil {
		return Problem(409, "operation_canceled", fmt.Sprintf("Operation canceled: %v", e))
	}
	return nil
}
