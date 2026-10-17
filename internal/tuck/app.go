package tuck

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type App struct {
	Config            *Config
	Store             *Store
	Engine            *Engine
	Manager           *Manager
	Models            *Models
	Platform          *Platform
	Security          *Security
	Protection        *SettingsProtection
	Files             *Files
	Delivery          *Deliveries
	Local             *LocalTools
	Workspace         *EnterpriseWorkspace
	Updates           *EngineUpdates
	Server            *http.Server
	URL               string
	listener          net.Listener
	changeMu          sync.Mutex
	changing, closing atomic.Bool
	closeOnce         sync.Once
	closeErr          error
	instanceLocks     []*os.File
	ctx               context.Context
	cancel            context.CancelFunc
}

func NewApp(config *Config) (app *App, err error) {
	app = &App{Config: config}
	app.ctx, app.cancel = context.WithCancel(context.Background())
	building := app
	defer func() {
		if err != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			building.Close(ctx)
		}
	}()
	data := config.DataDir()
	configLock, err := acquireInstanceLock(config.Path + ".lock")
	if err != nil {
		return nil, err
	}
	app.instanceLocks = append(app.instanceLocks, configLock)
	if config.Pending() {
		return nil, Problem(409, "settings_changed", "配置在启动前已改变，请重新加载配置后启动")
	}
	if err = os.MkdirAll(data, 0700); err != nil {
		return nil, err
	}
	dataLock, err := acquireInstanceLock(filepath.Join(data, ".tuck.lock"))
	if err != nil {
		return nil, err
	}
	app.instanceLocks = append(app.instanceLocks, dataLock)
	if err = os.MkdirAll(config.WorkspaceRoot(), 0700); err != nil {
		return nil, err
	}
	app.Store, err = NewStore(filepath.Join(data, "tuck.db"))
	if err != nil {
		return nil, err
	}
	app.Security, err = NewSecurity(app.Store, config)
	if err != nil {
		return nil, err
	}
	app.Protection = NewSettingsProtection(config, app.Security)
	app.Platform, err = NewPlatform(app.Store, config)
	if err != nil {
		return nil, err
	}
	if app.Platform.Binding() != nil {
		_ = app.Platform.Sync(context.Background())
	}
	app.Models, err = NewModels(config, app.Store, app.Platform)
	if err != nil {
		return nil, err
	}
	server := O(config.Applied()["server"])
	app.listener, err = net.Listen("tcp", net.JoinHostPort(S(server["listenHost"]), fmt.Sprint(I(server["port"]))))
	if err != nil {
		return nil, err
	}
	port := app.listener.Addr().(*net.TCPAddr).Port
	app.URL = "http://127.0.0.1:" + strconv.Itoa(port)
	engine := O(config.Applied()["engine"])
	binary := S(engine["binary"])
	if binary != "" {
		binary = config.Resolve(binary)
	} else {
		binary = filepath.Join(data, "engines", S(engine["version"]), ExecutableName())
	}
	nativeDirectory := filepath.Join(data, "native")
	if err = os.MkdirAll(nativeDirectory, 0700); err != nil {
		return nil, err
	}
	app.Engine = NewEngine(EngineOptions{Binary: binary, DataDir: nativeDirectory, WorkspaceRoot: config.WorkspaceRoot(), URL: S(engine["url"]), ConfigDir: filepath.Join(data, "native-config"), External: S(engine["mode"]) == "external", DesktopProxy: B(engine["desktopProxy"])})
	app.Files = &Files{Models: app.Models}
	options := ManagerOptions{Store: app.Store, Engine: app.Engine, Config: app.Models.Config, Models: app.Models, WorkspaceRoot: config.WorkspaceRoot(), AgentRef: "agent", NativeDirectory: nativeDirectory, PreparePrompt: app.Files.Prepare, RefreshFiles: app.Files.List, ApprovalTools: app.Models.MCP.ApprovalTools}
	if S(engine["mode"]) == "external" {
		options.NativeDirectory = ""
	}
	options.BeginTurn = func(ctx context.Context, t M) (map[string]string, func(), error) { return app.Local.Begin(ctx, t) }
	options.InspectionPresentation = func(root, input M) M { return app.Local.inspectionPresentation(root, input) }
	options.ValidateAttachments = func(task M, ids []string) error { _, err := app.Files.attachments(task, ids); return err }
	options.CheckWorkspace = func(path string) error {
		if app.Delivery == nil {
			return nil
		}
		return app.Delivery.CheckWorkspace(path)
	}
	app.Manager, err = NewManager(options)
	if err != nil {
		return nil, err
	}
	app.Files.Manager = app.Manager
	app.Delivery, err = NewDeliveries(app.Manager, app.Store, app.Models, filepath.Join(data, "deliveries"))
	if err != nil {
		return nil, err
	}
	app.Local = NewLocalTools(app.Models, app.Manager, app.Files, app.Delivery)
	app.Delivery.Local = app.Local
	app.Models.Local = app.Local
	app.Workspace = NewEnterpriseWorkspace(app.Platform)
	app.Updates = NewEngineUpdates(app)
	app.Models.MCP.OnChange = func() {
		if !app.changing.Load() {
			_ = app.publishConnection()
		}
	}
	if err = app.configureEngine(); err != nil {
		return nil, err
	}
	app.Server = &http.Server{Handler: http.HandlerFunc(app.serve), ReadHeaderTimeout: 60 * time.Second}
	return app, nil
}
func (a *App) configureEngine() error {
	file, e := a.Models.WriteConfig()
	if e != nil {
		return e
	}
	options := a.Engine.Options()
	options.Config = file
	options.DesktopProxy = B(O(a.Models.Config()["engine"])["desktopProxy"])
	options.Env = a.Models.MCP.Environment()
	options.Env["TUCK_GATEWAY_TOKEN"] = a.Models.Secret
	return a.Engine.Configure(options)
}
func (a *App) Start(ctx context.Context) error {
	go func() { _ = a.Server.Serve(a.listener) }()
	if e := a.Engine.Start(ctx); e != nil {
		return e
	}
	a.Models.MCP.Prepare(ctx, false)
	if e := a.Manager.RestoreChildStatistics(ctx); e != nil {
		return e
	}
	return a.publishConnection()
}
func (a *App) publishConnection() error {
	return a.Manager.PublishGlobal(M{"type": "connection", "connection": a.Models.Summary(), "assistants": a.Manager.AssistantCatalog()})
}
func (a *App) Close(ctx context.Context) error {
	a.closeOnce.Do(func() {
		a.closing.Store(true)
		if a.cancel != nil {
			a.cancel()
		}
		if a.Workspace != nil {
			a.Workspace.Close()
		}
		if a.Platform != nil {
			a.Platform.Close()
		}
		if a.Updates != nil {
			a.Updates.Close()
		}
		if a.Models != nil {
			a.Models.cancel()
		}
		if a.Manager != nil {
			a.closeErr = a.Manager.Close(ctx)
		}
		if a.Local != nil {
			a.Local.Close()
		}
		if a.Engine != nil {
			_ = a.Engine.Close()
		}
		if a.Models != nil {
			_ = a.Models.Close(ctx)
		}
		if a.Server != nil {
			if e := a.Server.Close(); e != nil {
				if a.closeErr == nil {
					a.closeErr = e
				}
			}
			if a.listener != nil {
				_ = a.listener.Close()
			}
		} else if a.listener != nil {
			a.listener.Close()
		}
		if a.Store != nil {
			if e := a.Store.Close(); e != nil && a.closeErr == nil {
				a.closeErr = e
			}
		}
		for i := len(a.instanceLocks) - 1; i >= 0; i-- {
			_ = a.instanceLocks[i].Close()
		}
	})
	return a.closeErr
}
func (a *App) locked(r *http.Request) ([]string, error) {
	v, e := a.Protection.Info(r)
	return Strings(v["lockedCategories"]), e
}
func (a *App) connection(r *http.Request) (M, error) {
	locked, e := a.locked(r)
	if e != nil {
		return nil, e
	}
	return ConnectionView(a.Models.Summary(), locked), nil
}
func (a *App) runtimeSettings(r *http.Request) (M, error) {
	locked, e := a.locked(r)
	if e != nil {
		return nil, e
	}
	return RuntimeView(a.Config, a.Models, locked)
}
func (a *App) orchestration() M {
	wb := O(a.Models.Config()["workbench"])
	names := []string{}
	for _, v := range A(AssistantCatalog(a.Models.Config())["items"]) {
		if O(v)["deliveryTool"] != false {
			names = append(names, S(O(v)["displayName"]))
		}
	}
	return M{"requireDelivery": B(wb["requireDelivery"]), "updatedAt": N(wb["updatedAt"]), "available": len(names) > 0, "assistants": names}
}
func (a *App) serve(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithCancel(r.Context())
	stop := context.AfterFunc(a.ctx, cancel)
	defer stop()
	defer cancel()
	r = r.WithContext(ctx)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Cache-Control", "no-store")
	if a.closing.Load() {
		WriteError(w, Problem(503, "shutting_down", "Server is shutting down"))
		return
	}
	if e := a.route(w, r); e != nil {
		if errors.Is(e, os.ErrNotExist) {
			e = Problem(404, "not_found", "Resource not found")
		}
		WriteError(w, e)
	}
}
func (a *App) route(w http.ResponseWriter, r *http.Request) error {
	p := r.URL.Path
	if p == "/healthz" {
		state := a.Engine.State()
		status := 200
		if S(state["state"]) != "ready" {
			status = 503
		}
		WriteJSON(w, status, M{"status": map[bool]string{true: "ok", false: "unavailable"}[status == 200], "engine": state["state"]})
		return nil
	}
	if _, e := a.Security.Origin(r); e != nil {
		return e
	}
	if p == "/api/auth/session" && r.Method == "GET" {
		v, e := a.Security.Info(r)
		if e != nil {
			return e
		}
		WriteJSON(w, 200, v)
		return nil
	}
	if p == "/api/auth/login" && r.Method == "POST" {
		v, e := ReadJSON(r.Body, 4096)
		if e != nil {
			return e
		}
		v, e = a.Security.Login(r, w, v)
		if e != nil {
			return e
		}
		WriteJSON(w, 200, v)
		return nil
	}
	var session M
	var e error
	if strings.HasPrefix(p, "/api/") {
		session, e = a.Security.Authorize(r)
		if e != nil {
			return e
		}
	}
	if strings.HasPrefix(p, "/api/settings/") {
		return a.settingsRoute(w, r)
	}
	if p == "/api/auth/logout" && r.Method == "POST" {
		if e = a.Security.Logout(r, w); e != nil {
			return e
		}
		a.Protection.mu.Lock()
		delete(a.Protection.grants, Hash([]byte(cookieValue(r, "tuck_settings_access"))))
		a.Protection.mu.Unlock()
		WriteJSON(w, 200, M{"ok": true})
		return nil
	}
	if p == "/api/runtime" {
		connection, e := a.connection(r)
		if e != nil {
			return e
		}
		state := a.Engine.State()
		WriteJSON(w, 200, Merge(state, M{"version": state["version"], "workspaceRoot": a.Config.WorkspaceRoot(), "connection": connection, "assistants": a.Manager.AssistantCatalog(), "orchestration": a.orchestration(), "deliveryToolAvailable": true}))
		return nil
	}
	if p == "/api/connection" && r.Method == "GET" {
		v, e := a.connection(r)
		if e != nil {
			return e
		}
		WriteJSON(w, 200, v)
		return nil
	}
	if strings.HasPrefix(p, "/api/connection/") && r.Method == "POST" {
		return a.changeConnection(w, r)
	}
	if p == "/api/enterprise-workspace" || strings.HasPrefix(p, "/api/enterprise-workspace/") {
		if a.changing.Load() {
			return Problem(409, "workspace_connection_changing", "企业接入正在变更，请完成后重试")
		}
		ctx, cancel := a.Security.Watch(r.Context(), session)
		defer cancel()
		return a.Workspace.Handle(w, r.WithContext(ctx))
	}
	if p == "/api/tasks" {
		if r.Method == "GET" {
			cursor, e := a.Store.Cursor()
			if e != nil {
				return e
			}
			WriteJSON(w, 200, M{"cursor": cursor, "tasks": a.Manager.List()})
			return nil
		}
		if r.Method == "POST" {
			v, e := ReadJSON(r.Body, 24<<20)
			if e != nil {
				return e
			}
			task, e := a.Manager.Create(r.Context(), v)
			if e != nil {
				return e
			}
			WriteJSON(w, 201, task)
			return nil
		}
	}
	if p == "/api/events" && r.Method == "GET" {
		return a.events(w, r, session)
	}
	if strings.HasPrefix(p, "/api/tasks/") {
		return a.taskRoute(w, r)
	}
	if strings.HasPrefix(p, "/api/") {
		return Problem(404, "not_found", "Route not found")
	}
	return a.static(w, r)
}
func (a *App) taskRoute(w http.ResponseWriter, r *http.Request) error {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/tasks/"), "/")
	id := parts[0]
	action := strings.Join(parts[1:], "/")
	var cursor int64
	if r.Method == "GET" && (action == "" || action == "messages") {
		// Capture the replay boundary before reading the snapshot. A concurrent
		// publication may appear in both, but must never be skipped by both.
		var e error
		cursor, e = a.Store.Cursor()
		if e != nil {
			return e
		}
	}
	task, e := a.Manager.Get(id)
	if e != nil {
		return e
	}
	send := func(status int, v any) error { WriteJSON(w, status, v); return nil }
	if action == "" && r.Method == "GET" {
		return send(200, M{"task": task, "cursor": cursor})
	}
	if action == "messages" && r.Method == "GET" {
		start, count := 0, 2048
		if raw := r.URL.Query().Get("start"); raw != "" {
			n, e := strconv.Atoi(raw)
			if e != nil {
				return Problem(400, "invalid_range", "Invalid history range")
			}
			start = max(0, n)
		}
		if raw := r.URL.Query().Get("count"); raw != "" {
			n, e := strconv.Atoi(raw)
			if e != nil {
				return Problem(400, "invalid_range", "Invalid history range")
			}
			count = min(4096, max(1, n))
		}
		messages, e := a.Store.Messages(id, start, count)
		if e != nil {
			return e
		}
		if r.URL.Query().Get("view") == "workbench" {
			for index, m := range messages {
				messages[index] = WorkbenchMessage(id, m)
			}
		}
		return send(200, M{"messages": messages, "cursor": cursor})
	}
	if action == "message-media" && r.Method == "GET" {
		index, e := strconv.Atoi(r.URL.Query().Get("index"))
		if e != nil || index < 0 {
			return Problem(400, "invalid_position", "Invalid message position")
		}
		message, e := a.Store.Message(id, index)
		if e != nil {
			return e
		}
		media, e := InlineMessageMedia(message, r.URL.Query().Get("block"), r.URL.Query().Get("item"))
		if e != nil {
			return e
		}
		_ = media
		return a.serveMessageMedia(w, r, media)
	}
	if action == "result" && r.Method == "GET" {
		reply := ""
		if S(task["status"]) == "idle" && S(task["lastTurnReason"]) != "" && !TaskIsActive(task) {
			reply, e = a.Store.FinalReply(id)
			if e != nil {
				return e
			}
		}
		return send(200, M{"reply": reply})
	}
	if action == "statistics" && r.Method == "GET" {
		v, e := a.Store.Statistics(task)
		if e != nil {
			return e
		}
		return send(200, v)
	}
	if action == "assistant-summary" && r.Method == "GET" {
		return a.assistantSummary(w, r, task)
	}
	if action == "fork-options" && r.Method == "GET" {
		v, e := a.Manager.ForkOptions(r.Context(), id)
		if e != nil {
			return e
		}
		return send(200, v)
	}
	if action == "files" && (r.Method == "GET" || r.Method == "HEAD") {
		return a.Files.Serve(w, r, task)
	}
	if action == "deliveries" && r.Method == "GET" {
		before := int64(9007199254740991)
		limit := 20
		if raw := r.URL.Query().Get("beforeVersion"); raw != "" {
			before, e = strconv.ParseInt(raw, 10, 64)
			if e != nil || before < 1 {
				return Problem(400, "invalid_page", "交付分页参数无效")
			}
		}
		if raw := r.URL.Query().Get("limit"); raw != "" {
			limit, e = strconv.Atoi(raw)
			if e != nil || limit < 1 || limit > 100 {
				return Problem(400, "invalid_page", "交付分页参数无效")
			}
		}
		items, e := a.Store.Deliveries(id, before, limit+1)
		if e != nil {
			return e
		}
		return send(200, M{"items": items[:min(limit, len(items))], "hasMore": len(items) > limit})
	}
	if len(parts) >= 3 && parts[1] == "deliveries" && (r.Method == "GET" || r.Method == "HEAD") {
		version := parts[2]
		if version != "latest" {
			v, e := strconv.ParseInt(version, 10, 64)
			if e != nil || v < 1 || v > 9007199254740991 {
				return Problem(400, "invalid_version", "版本号无效")
			}
		}
		if len(parts) == 5 && parts[3] == "files" && publicID.MatchString(parts[4]) {
			return a.Delivery.ServeFile(w, r, id, version, parts[4])
		}
		if len(parts) == 3 {
			v, e := a.Store.Delivery(id, version)
			if e != nil {
				return e
			}
			if v == nil {
				return Problem(404, "delivery_not_found", "尚未提交此版本的交付")
			}
			return send(200, v)
		}
	}
	if r.Method != "POST" {
		return Problem(404, "not_found", "Route not found")
	}
	if a.changing.Load() && Contains([]string{"prompts", "steer", "queue", "approval-mode", "interaction", "model", "fork"}, action) {
		return Problem(409, "configuration_changing", "模型接入正在变更，请稍后执行")
	}
	v, e := ReadJSON(r.Body, 24<<20)
	if e != nil {
		return e
	}
	if action == "prompts" || action == "steer" {
		if Has(v, "attachmentIds") && !isArray(v["attachmentIds"]) {
			return Problem(400, "attachment_limit", "每条消息最多 8 个附件，且不能重复")
		}
		for _, id := range A(v["attachmentIds"]) {
			if _, ok := id.(string); !ok {
				return Problem(400, "attachment_limit", "每条消息最多 8 个附件，且不能重复")
			}
		}
	}
	var out any
	status := 200
	switch action {
	case "rename":
		out, e = a.Manager.Rename(r.Context(), id, S(v["title"]))
	case "fork":
		if !Integer(v["index"], 0, 9007199254740991) {
			return Problem(400, "invalid_fork", "无效的分支位置")
		}
		out, e = a.Manager.Fork(r.Context(), id, I(v["index"]))
		status = 201
	case "prompts":
		if Has(v, "expectedTurnCount") && !Integer(v["expectedTurnCount"], 0, 9007199254740991) {
			return Problem(400, "invalid_turn", "无效的对话轮数")
		}
		out, e = a.Manager.Submit(id, S(v["prompt"]), Strings(v["attachmentIds"]), v["expectedTurnCount"], nil)
		status = 202
	case "steer":
		_, e = a.Manager.Steer(r.Context(), id, S(v["prompt"]), Strings(v["attachmentIds"]), nil)
		out = M{"accepted": true}
		status = 202
	case "queue":
		out, e = a.Manager.UpdateQueue(r.Context(), id, v)
	case "approval-mode":
		out, e = a.Manager.SetApprovalMode(r.Context(), id, S(v["mode"]))
	case "model":
		out, e = a.Manager.SelectModel(id, v["modelId"], S(v["reasoningEffort"]))
	case "abort":
		e = a.Manager.Abort(r.Context(), id)
		out = M{"stopped": true}
	case "interaction":
		e = a.Manager.ResolveInteraction(r.Context(), id, v)
		out = M{"accepted": true}
	case "archive":
		out, e = a.Manager.Archive(id, B(v["archived"]))
	case "clear-queue":
		_, e = a.Manager.UpdateQueue(r.Context(), id, M{"action": "clear"})
		out = M{"cleared": true}
	case "upload":
		out, e = a.Files.Upload(task, v)
		status = 201
	case "save-file":
		e = a.Files.Save(task, v)
		out = M{"saved": true}
	default:
		return Problem(404, "not_found", "Route not found")
	}
	if e != nil {
		return e
	}
	return send(status, out)
}
func (a *App) events(w http.ResponseWriter, r *http.Request, session M) error {
	raw := r.Header.Get("Last-Event-ID")
	if raw == "" {
		raw = r.URL.Query().Get("since")
	}
	if raw == "" {
		raw = "0"
	}
	cursor, e := strconv.ParseInt(raw, 10, 64)
	if e != nil || cursor < 0 {
		return Problem(400, "invalid_cursor", "Invalid event cursor")
	}
	current, e := a.Store.Cursor()
	if e != nil {
		return e
	}
	if cursor > current {
		return Problem(409, "cursor_ahead", "Event cursor is ahead of this database; reload the workbench")
	}
	ctx, cancel := a.Security.Watch(r.Context(), session)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { _ = http.NewResponseController(w).SetWriteDeadline(time.Now()) })
	defer stop()
	notify, unsubscribe := a.Manager.Subscribe()
	defer unsubscribe()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(200)
	if _, e = fmt.Fprint(w, ": connected\n\n"); e != nil {
		return nil
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil
	}
	flusher.Flush()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		for {
			events, e := a.Store.Events(cursor, 512)
			if e != nil {
				panic(http.ErrAbortHandler)
			}
			if len(events) == 0 {
				break
			}
			for _, event := range events {
				if ctx.Err() != nil {
					return nil
				}
				if S(event["type"]) == "connection" {
					locked, e := a.locked(r)
					if e != nil {
						panic(http.ErrAbortHandler)
					}
					event["connection"] = ConnectionView(O(event["connection"]), locked)
				}
				if r.URL.Query().Get("view") == "workbench" {
					event = WorkbenchEvent(event)
				}
				seq := int64(N(event["seq"]))
				if _, e = fmt.Fprintf(w, "id: %d\ndata: %s\n\n", seq, JSON(event)); e != nil {
					return nil
				}
				flusher.Flush()
				cursor = seq
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-notify:
		case <-ticker.C:
			if _, e = fmt.Fprint(w, ": ping\n\n"); e != nil {
				return nil
			}
			flusher.Flush()
		}
	}
}
func (a *App) static(w http.ResponseWriter, r *http.Request) error {
	directory := a.Config.Resolve(S(O(a.Config.Applied()["server"])["frontendDir"]))
	root, e := os.OpenRoot(directory)
	if os.IsNotExist(e) {
		return Problem(503, "frontend_missing", "Build the frontend before starting Tuck")
	}
	if e != nil {
		return e
	}
	defer root.Close()
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}
	relative, e := relativeFile(path, false)
	if e != nil {
		return e
	}
	file, e := root.Open(relative)
	if e != nil {
		if strings.HasPrefix(r.URL.Path, "/assets/") || filepath.Ext(path) != "" {
			return Problem(404, "asset_missing", "Static asset not found")
		}
		relative = "index.html"
		file, e = root.Open(relative)
		if e != nil {
			return Problem(503, "frontend_missing", "Build the frontend before starting Tuck")
		}
	}
	defer file.Close()
	info, e := file.Stat()
	if e != nil {
		return e
	}
	if info.IsDir() {
		return Problem(404, "asset_missing", "Static asset not found")
	}
	w.Header().Set("Content-Type", MIMEType(relative))
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, filepath.Base(relative), info.ModTime(), file)
	return nil
}
func (a *App) changeConnection(w http.ResponseWriter, r *http.Request) error {
	if e := a.Protection.Require(r, "platform"); e != nil {
		return e
	}
	if a.Engine.Options().External {
		return Problem(409, "external_engine", "外部 Docker Agent 配置由宿主管理")
	}
	if !a.changeMu.TryLock() {
		return Problem(409, "configuration_changing", "配置正在应用，请稍后重试")
	}
	defer a.changeMu.Unlock()
	a.changing.Store(true)
	a.Manager.SetConfigurationChanging(true)
	defer func() { a.Workspace.Invalidate(); a.changing.Store(false); a.Manager.SetConfigurationChanging(false) }()
	if a.Manager.Active() {
		return Problem(409, "active_tasks", "请等待当前执行结束或停止后再更新资源配置")
	}
	a.Workspace.Close()
	data, e := ReadJSON(r.Body, 24<<20)
	if e != nil {
		return e
	}
	if e = a.Protection.Require(r, "platform"); e != nil {
		return e
	}
	switch r.URL.Path {
	case "/api/connection/enroll":
		e = a.Platform.Enroll(r.Context(), S(data["material"]))
	case "/api/connection/origin":
		e = a.Platform.ChangeOrigin(r.Context(), S(data["origin"]))
	case "/api/connection/sync":
		if a.Platform.Binding() != nil {
			e = a.Platform.Sync(r.Context())
		}
	case "/api/connection/logout":
		e = a.Platform.Logout(r.Context())
	default:
		return Problem(404, "not_found", "Route not found")
	}
	if e != nil {
		return e
	}
	if e = a.Models.Rebuild(); e != nil {
		return e
	}
	a.Models.MCP.Prepare(r.Context(), true)
	if e = a.Engine.Close(); e != nil {
		return e
	}
	if e = a.configureEngine(); e != nil {
		return e
	}
	if e = a.Engine.Start(r.Context()); e != nil {
		return e
	}
	if e = a.publishConnection(); e != nil {
		return e
	}
	view, e := a.connection(r)
	if e != nil {
		return e
	}
	WriteJSON(w, 200, view)
	return nil
}

var _ = url.PathEscape
