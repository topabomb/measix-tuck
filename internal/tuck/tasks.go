package tuck

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

type ModelRegistry interface {
	Summary() M
	Catalog() []M
	PickForAgent(string, any) (M, error)
	Pick(any, string) (M, error)
	Bind(M) (M, error)
}
type ManagerOptions struct {
	Store                                    *Store
	Engine                                   NativeEngine
	Config                                   func() M
	Models                                   ModelRegistry
	WorkspaceRoot, AgentRef, NativeDirectory string
	PreparePrompt                            func(M, string, []string) (M, []any, error)
	ValidateAttachments                      func(M, []string) error
	BeginTurn                                func(context.Context, M) (map[string]string, func(), error)
	RefreshFiles                             func(M) ([]any, error)
	CheckWorkspace                           func(string) error
	ApprovalTools                            func() []string
	InspectionPresentation                   func(M, M) M
}
type taskRun struct {
	cancel   context.CancelFunc
	done     chan struct{}
	stopping bool
}
type interaction struct{ pending, native M }
type steeringRecord struct {
	hash, status, messageID, turnID string
	after                           int64
	index                           int
}
type Manager struct {
	mu                                sync.Mutex
	options                           ManagerOptions
	tasks                             map[string]M
	taskOrder                         map[string]int
	runs                              map[string]*taskRun
	interactions                      map[string][]interaction
	resolving, policyChanges, sending map[string]bool
	sendWait                          map[string]chan struct{}
	steering                          map[string][]*steeringRecord
	receipts                          map[string]map[string]bool
	listeners                         map[chan struct{}]struct{}
	changing, closed                  bool
	configurationRevision             uint64
	normalizer                        normalizer
}

func NewManager(o ManagerOptions) (*Manager, error) {
	if o.AgentRef == "" {
		o.AgentRef = "tuck"
	}
	m := &Manager{options: o, tasks: map[string]M{}, runs: map[string]*taskRun{}, interactions: map[string][]interaction{}, resolving: map[string]bool{}, policyChanges: map[string]bool{}, sending: map[string]bool{}, steering: map[string][]*steeringRecord{}, receipts: map[string]map[string]bool{}, listeners: map[chan struct{}]struct{}{}}
	m.sendWait = map[string]chan struct{}{}
	m.taskOrder = map[string]int{}
	m.normalizer.init(m)
	tasks, err := o.Store.Tasks()
	if err != nil {
		return nil, err
	}
	for _, task := range tasks {
		m.taskOrder[S(task["id"])] = len(m.taskOrder)
		m.tasks[S(task["id"])] = task
		if S(task["workspaceMode"]) == "none" {
			task["workspace"] = o.NativeDirectory
		}
		queue := []any{}
		for _, v := range A(task["queue"]) {
			if S(O(O(v)["source"])["kind"]) != "delivery_required" {
				queue = append(queue, v)
			}
		}
		task["queue"] = queue
		task["queuedPrompts"] = len(queue)
		if len(queue) > 0 {
			task["queuePaused"] = true
		}
		if err = o.Store.SaveTask(task); err != nil {
			return nil, err
		}
	}
	if err = m.restoreSteeringLocked(); err != nil {
		return nil, err
	}
	recoveredParents := map[string]bool{}
	for _, task := range m.tasks {
		if S(task["status"]) != "idle" {
			// Only startup needs to inspect persisted live messages. Runtime
			// settlement uses the current turn's call records.
			messages, e := o.Store.LiveMessages(S(task["id"]))
			if e != nil {
				return nil, e
			}
			for _, message := range messages {
				if err = m.interruptMessageLocked(task, message); err != nil {
					return nil, err
				}
			}
			if err = m.settleLocked(task, "interrupted", M{"code": "service_restarted", "message": "服务已重启，原执行已中断。检查工作目录后可以继续任务。"}); err != nil {
				return nil, err
			}
			if parent := S(task["parentTaskId"]); parent != "" {
				recoveredParents[parent] = true
			}
		}
	}
	for id := range recoveredParents {
		if task := m.tasks[id]; task != nil && task["delegationIndex"] != nil {
			if err = m.normalizer.delegation(task); err != nil {
				return nil, err
			}
		}
	}
	return m, nil
}
func (m *Manager) Config() M {
	if m.options.Config != nil {
		return Clone(m.options.Config())
	}
	return M{"agents": M{"root": M{}}, "workbench": M{"defaultAgent": "root", "assistants": []any{M{"agent": "root", "displayName": "助手", "workspace": M{"mode": "auto"}, "taskConcurrency": 2}}}}
}

// RestoreChildStatistics uses only the public recovery response and preserves
// the readable Tuck history when an original native session is unavailable.
func (m *Manager) RestoreChildStatistics(ctx context.Context) error {
	for _, snapshot := range m.List() {
		if S(snapshot["parentTaskId"]) == "" || S(snapshot["mode"]) != "background" || S(snapshot["usageSource"]) == "live" {
			continue
		}
		recovery, err := m.options.Engine.Request(ctx, "GET", nativePath(snapshot, "/recovery"), nil)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			continue
		}
		m.mu.Lock()
		task := m.tasks[S(snapshot["id"])]
		if task != nil && S(task["usageSource"]) != "live" {
			err = m.normalizer.restoreToolMetadata(task, A(recovery["messages"]))
			if err == nil {
				for key, value := range RecoveredStatistics(recovery, S(task["agent"])) {
					task[key] = value
				}
				err = m.publishLocked(task, M{"type": "state", "task": task}, nil)
			}
		}
		m.mu.Unlock()
		if err != nil {
			return err
		}
	}
	return nil
}
func (m *Manager) AssistantCatalog() M {
	return AssistantCatalog(m.Config())
}
func (m *Manager) getLocked(id string) (M, error) {
	t := m.tasks[id]
	if t == nil {
		return nil, Problem(404, "task_not_found", "Task not found")
	}
	return t, nil
}
func (m *Manager) Get(id string) (M, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, e := m.getLocked(id)
	return Clone(t), e
}
func (m *Manager) List() []M {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := make([]M, 0, len(m.tasks))
	for _, t := range m.tasks {
		r = append(r, Clone(t))
	}
	sort.Slice(r, func(i, j int) bool {
		return m.taskOrder[S(r[i]["id"])] < m.taskOrder[S(r[j]["id"])]
	})
	return r
}
func (m *Manager) Update(id string, fn func(M) error) (M, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	task, err := m.getLocked(id)
	if err != nil {
		return nil, err
	}
	next := Clone(task)
	if err = fn(next); err != nil {
		return nil, err
	}
	if err = m.publishLocked(next, M{"type": "state", "task": next}, nil); err != nil {
		return nil, err
	}
	m.tasks[id] = next
	return Clone(next), nil
}
func (m *Manager) Subscribe() (<-chan struct{}, func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ch := make(chan struct{}, 1)
	m.listeners[ch] = struct{}{}
	return ch, func() { m.mu.Lock(); delete(m.listeners, ch); m.mu.Unlock() }
}
func (m *Manager) notifyLocked() {
	for ch := range m.listeners {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}
func (m *Manager) PublishGlobal(event M) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, err := m.options.Store.Publish(nil, nil, event)
	if err == nil {
		m.notifyLocked()
	}
	return err
}
func (m *Manager) publishLocked(task, event, message M) error {
	ObserveTurnTiming(task, NowMS())
	_, err := m.options.Store.Publish(task, message, event)
	if err != nil {
		if previous, e := m.options.Store.Task(S(task["id"])); e == nil {
			m.tasks[S(task["id"])] = previous
		}
		return err
	}
	if _, ok := m.taskOrder[S(task["id"])]; !ok {
		m.taskOrder[S(task["id"])] = len(m.taskOrder)
	}
	m.notifyLocked()
	return nil
}
func (m *Manager) stateLocked(task, patch M) error {
	for k, v := range patch {
		task[k] = v
	}
	return m.publishLocked(task, M{"type": "state", "task": task}, nil)
}
func (m *Manager) appendLocked(task, entry M) (M, error) {
	index := I(task["logicalCount"])
	task["logicalCount"] = index + 1
	message := Merge(M{"id": fmt.Sprintf("%s:m-%d", S(task["id"]), index), "index": index, "revision": 0}, entry)
	if err := m.publishLocked(task, M{"type": "message", "message": message}, message); err != nil {
		return nil, err
	}
	return message, nil
}
func (m *Manager) replaceLocked(task, message M) error {
	previous, err := m.options.Store.Message(S(task["id"]), I(message["index"]))
	if err != nil {
		return err
	}
	message["revision"] = I(message["revision"]) + 1
	for _, b := range A(message["blocks"]) {
		block := O(b)
		for _, p := range A(previous["blocks"]) {
			old := O(p)
			if S(old["id"]) == S(block["id"]) && !reflect.DeepEqual(old["data"], block["data"]) {
				block["revision"] = I(old["revision"]) + 1
			}
		}
	}
	return m.publishLocked(task, M{"type": "message", "message": message}, message)
}
func (m *Manager) appendTextLocked(task, message M, blockID, field, delta string) error {
	if delta == "" {
		return nil
	}
	for _, v := range A(message["blocks"]) {
		b := O(v)
		if S(b["id"]) != blockID {
			continue
		}
		data := O(b["data"])
		if field != "markdown" && field != "text" && field != "output" {
			return errors.New("invalid text append")
		}
		if _, ok := data[field].(string); !ok {
			return errors.New("invalid text block")
		}
		base := I(message["revision"])
		data[field] = S(data[field]) + delta
		b["revision"] = I(b["revision"]) + 1
		message["revision"] = base + 1
		return m.publishLocked(task, M{"type": "message-delta", "delta": M{"messageId": message["id"], "index": message["index"], "blockId": blockID, "field": field, "text": delta, "baseRevision": base, "revision": base + 1, "blockRevision": b["revision"]}}, message)
	}
	return errors.New("text block missing")
}
func BaseTask(id, nativeID, title, workspace, agent, ref string) M {
	return M{"id": id, "nativeSessionId": nativeID, "title": title, "workspace": workspace, "agent": agent, "agentRef": ref, "status": "idle", "logicalCount": 0, "queuedPrompts": 0, "queue": []any{}, "createdAt": NowISO(), "pendingInteraction": nil, "activePlan": nil, "lastTurnReason": nil, "lastFailure": nil, "modelCalls": 0, "usage": EmptyUsage(), "context": M{"projectedTokens": 0, "contextWindow": 0}, "turnCount": 0, "stepCount": 0, "activeAssistantIndex": nil, "artifacts": []any{}, "archived": false}
}

var channelTitle = regexp.MustCompile(`^T-([A-Za-z0-9_]{4}) · ([^\r\n]+)$`)

func ParseTaskTitle(title string) M {
	for _, r := range title {
		if r < 32 || r == 127 {
			return nil
		}
	}
	v := channelTitle.FindStringSubmatch(title)
	if v == nil || strings.TrimSpace(v[2]) == "" {
		return nil
	}
	return M{"code": v[1], "name": v[2]}
}
func TaskName(title string) string {
	if v := ParseTaskTitle(title); v != nil {
		return S(v["name"])
	}
	return title
}
func IsChannelTask(t M) bool {
	return S(t["parentTaskId"]) == "" && ParseTaskTitle(S(t["title"])) != nil
}
func TaskIsActive(t M) bool {
	return S(t["status"]) == "working" || S(t["status"]) == "waiting" || t["pendingInteraction"] != nil || !B(t["queuePaused"]) && (I(t["queuedPrompts"]) > 0 || len(A(t["queue"])) > 0)
}
func TurnHasEnded(t M) bool {
	return S(t["status"]) == "idle" && S(t["lastTurnReason"]) != "" && !TaskIsActive(t)
}
func (m *Manager) checkLocked(t M, allowStopping bool) error {
	if m.changing || m.closed {
		return Problem(409, "configuration_changing", "配置正在应用，请稍后执行")
	}
	if B(t["archived"]) || S(t["parentTaskId"]) != "" {
		return Problem(409, "task_readonly", "请选择未归档的主会话")
	}
	if r := m.runs[S(t["id"])]; r != nil && r.stopping && !allowStopping {
		return Problem(409, "task_stopping", "正在停止原执行，请稍后继续")
	}
	return nil
}
func (m *Manager) capacityLocked(t M) error {
	if !IsChannelTask(t) || m.runs[S(t["id"])] != nil || TaskIsActive(t) {
		return nil
	}
	limit := 2
	for _, v := range A(m.AssistantCatalog()["items"]) {
		a := O(v)
		if S(a["agent"]) == S(t["agent"]) {
			limit = I(a["taskConcurrency"])
		}
	}
	active := 0
	for id, x := range m.tasks {
		if id != S(t["id"]) && S(x["agent"]) == S(t["agent"]) && IsChannelTask(x) && (m.runs[id] != nil || TaskIsActive(x)) {
			active++
		}
	}
	if active >= limit {
		return Problem(409, "task_capacity", fmt.Sprintf("此助手已有 %d 个进行中任务（上限 %d）", active, limit))
	}
	return nil
}
func (m *Manager) bind(task M) error {
	if _, ok := O(m.Config()["agents"])[S(task["agent"])]; !ok {
		return Problem(409, "agent_missing", "此对话的助手已从本地配置移除，请恢复配置后继续。历史记录仍可查看。")
	}
	if m.options.Models != nil {
		patch, err := m.options.Models.Bind(Clone(task))
		if err != nil {
			return err
		}
		for k, v := range patch {
			task[k] = v
		}
		if patch["contextWindow"] != nil {
			task["context"] = Merge(O(task["context"]), M{"contextWindow": patch["contextWindow"]})
			delete(task, "contextWindow")
		}
	}
	return nil
}
func promptText(prompt string, attachments bool) (string, error) {
	if !attachments && strings.TrimSpace(prompt) == "" || utf8.RuneCountInString(prompt) > 100000 {
		return "", Problem(400, "invalid_prompt", "请填写消息或添加附件，文字最多 100000 字符")
	}
	return strings.TrimSpace(prompt), nil
}
func nativePath(t M, suffix string) string {
	return "/api/sessions/" + url.PathEscape(S(t["nativeSessionId"])) + suffix
}
func (m *Manager) Create(ctx context.Context, in M) (M, error) {
	m.mu.Lock()
	blocked := m.changing || m.closed
	revision := m.configurationRevision
	m.mu.Unlock()
	if blocked {
		return nil, Problem(409, "configuration_changing", "配置正在应用")
	}
	catalog := m.AssistantCatalog()
	agent := S(in["agent"])
	if agent == "" && S(in["channel"]) == "tasks" {
		return nil, Problem(422, "agent_required", "请从指定 Agent 新建任务")
	}
	if agent == "" {
		agent = S(catalog["defaultAgent"])
	}
	var assistant M
	for _, v := range A(catalog["items"]) {
		if S(O(v)["agent"]) == agent {
			assistant = O(v)
		}
	}
	if assistant == nil {
		return nil, Problem(400, "invalid_agent", "请选择已配置的可用助手")
	}
	approval := S(in["approvalMode"])
	if approval == "" {
		approval = "manual"
	}
	if approval != "manual" && approval != "auto" {
		return nil, Problem(400, "invalid_approval", "审批模式必须为 manual 或 auto")
	}
	selected := M{"model": in["model"]}
	var err error
	if m.options.Models != nil {
		selected, err = m.options.Models.PickForAgent(agent, in["modelId"])
		if err != nil {
			return nil, err
		}
		if S(in["channel"]) == "tasks" && S(selected["modelId"]) == "" {
			return nil, Problem(422, "model_required", "此助手未配置默认模型，请选择模型后创建任务")
		}
	}
	id := ID()
	policy := O(assistant["workspace"])
	mode := S(in["workspaceMode"])
	if mode == "" {
		if S(in["workspace"]) != "" {
			mode = "specified"
		} else {
			mode = S(policy["mode"])
			if mode == "" {
				mode = "auto"
			}
		}
	}
	if mode != "auto" && mode != "specified" && mode != "none" {
		return nil, Problem(422, "invalid_workspace_mode", "工作目录模式无效")
	}
	if mode != "specified" && S(in["workspace"]) != "" {
		return nil, Problem(422, "invalid_workspace", "只有指定目录模式可以填写路径")
	}
	directory := m.options.NativeDirectory
	if mode == "auto" {
		directory = filepath.Join(m.options.WorkspaceRoot, id)
		err = os.MkdirAll(directory, 0700)
	} else if mode == "specified" {
		directory = S(in["workspace"])
		if directory == "" {
			directory = S(policy["directory"])
		}
		directory, err = ValidTaskDirectory(directory)
	}
	if err != nil {
		return nil, err
	}
	if mode != "none" && m.options.CheckWorkspace != nil {
		if err = m.options.CheckWorkspace(directory); err != nil {
			return nil, err
		}
	}
	title := S(in["title"])
	if title == "" {
		title = "新任务"
	}
	body := M{"title": title, "safety_policy": "balanced"}
	if approval == "auto" {
		body["safety_policy"] = "autonomous"
	}
	if mode != "none" {
		body["working_dir"] = directory
	}
	native, err := m.options.Engine.Request(ctx, "POST", "/api/sessions", body)
	if err != nil {
		return nil, err
	}
	task := BaseTask(id, S(native["id"]), title, directory, agent, m.options.AgentRef)
	task = Merge(task, selected, M{"workspaceMode": mode, "assistantName": assistant["displayName"], "approvalMode": approval})
	m.setContext(task)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.changing || m.closed || m.configurationRevision != revision {
		return nil, Problem(409, "configuration_changing", "配置已变化，请重新新建任务")
	}
	if err = m.publishLocked(task, M{"type": "created", "task": task}, nil); err != nil {
		return nil, err
	}
	m.tasks[id] = task
	return Clone(task), nil
}
func ValidTaskDirectory(path string) (string, error) {
	if strings.TrimSpace(path) == "" || !filepath.IsAbs(path) {
		return "", Problem(422, "invalid_workspace", "请填写已有工作目录的绝对路径")
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", Problem(422, "invalid_workspace", "指定目录必须已存在且可访问")
	}
	info, err := os.Stat(real)
	if err != nil || !info.IsDir() {
		return "", Problem(422, "invalid_workspace", "指定目录必须已存在且可访问")
	}
	f, err := os.CreateTemp(real, ".tuck-access-*")
	if err != nil {
		return "", Problem(422, "invalid_workspace", "指定目录必须可读写")
	}
	_ = f.Close()
	_ = os.Remove(f.Name())
	return real, nil
}
func (m *Manager) setContext(task M) {
	if m.options.Models != nil {
		for _, model := range m.options.Models.Catalog() {
			if S(model["modelId"]) == S(task["modelId"]) {
				task["context"] = Merge(O(task["context"]), M{"contextWindow": model["contextWindow"]})
			}
		}
	}
}
func (m *Manager) Rename(ctx context.Context, id, title string) (M, error) {
	title = strings.TrimSpace(title)
	if title == "" || utf8.RuneCountInString(title) > 120 || strings.IndexFunc(title, func(r rune) bool { return r < 32 || r == 127 }) >= 0 {
		return nil, Problem(400, "invalid_title", "对话名称需为 1–120 个字符，且不能包含换行或控制字符")
	}
	t, e := m.Get(id)
	if e != nil {
		return nil, e
	}
	if v := ParseTaskTitle(S(t["title"])); v != nil {
		title = "T-" + S(v["code"]) + " · " + TaskName(title)
	}
	if utf8.RuneCountInString(title) > 120 {
		return nil, Problem(400, "invalid_title", "对话名称过长")
	}
	if _, e = m.options.Engine.Request(ctx, "PATCH", nativePath(t, "/title"), M{"title": title}); e != nil {
		return nil, e
	}
	return m.Update(id, func(t M) error { t["title"] = title; return nil })
}
func (m *Manager) SelectModel(id string, modelID any, reasoning string) (M, error) {
	if reasoning == "" {
		reasoning = "default"
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	t, e := m.getLocked(id)
	if e != nil {
		return nil, e
	}
	if e = m.checkLocked(t, false); e != nil {
		return nil, e
	}
	if m.options.Models == nil || S(t["status"]) != "idle" || m.runs[id] != nil {
		return nil, Problem(409, "task_active", "请在任务空闲时选择模型")
	}
	patch, e := m.options.Models.Pick(modelID, reasoning)
	if e != nil {
		return nil, e
	}
	for k, v := range patch {
		t[k] = v
	}
	t["agentRef"] = m.options.AgentRef
	m.setContext(t)
	e = m.publishLocked(t, M{"type": "state", "task": t}, nil)
	return Clone(t), e
}
func (m *Manager) prepare(task M, prompt string, ids []string) (M, []any, error) {
	if m.options.PreparePrompt != nil {
		return m.options.PreparePrompt(Clone(task), prompt, ids)
	}
	if len(ids) > 0 {
		return nil, nil, Problem(422, "attachments_unavailable", "附件处理不可用")
	}
	blocks := []any{}
	if prompt != "" {
		blocks = append(blocks, newBlock("prompt:"+S(task["turnId"]), "markdown", M{"markdown": prompt}))
	}
	return modelContent.UserMessage(prompt, nil, false, false), blocks, nil
}
func (m *Manager) Submit(id, prompt string, ids []string, expected any, source M) (M, error) {
	text, err := promptText(prompt, len(ids) > 0)
	if err != nil {
		return nil, err
	}
	if len(ids) > 0 && m.options.ValidateAttachments != nil {
		snapshot, e := m.Get(id)
		if e != nil {
			return nil, e
		}
		if e = m.options.ValidateAttachments(snapshot, ids); e != nil {
			return nil, e
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	task, err := m.getLocked(id)
	if err != nil {
		return nil, err
	}
	if err = m.checkLocked(task, false); err != nil {
		return nil, err
	}
	if expected != nil {
		if !Integer(expected, 0, 9007199254740991) {
			return nil, Problem(400, "invalid_turn", "无效的对话轮数")
		}
		if !IsChannelTask(task) || !TurnHasEnded(task) || I(task["turnCount"]) != I(expected) || m.runs[id] != nil || len(A(task["queue"])) > 0 {
			return nil, Problem(409, "turn_changed", "任务状态已变化或有待发送内容，请先查看任务")
		}
	}
	if err = m.bind(task); err != nil {
		return nil, err
	}
	if err = m.capacityLocked(task); err != nil {
		return nil, err
	}
	entry := M{"id": ID(), "prompt": text}
	if source != nil {
		entry["source"] = Clone(source)
	}
	if len(ids) > 0 {
		entry["attachmentIds"] = append([]string{}, ids...)
	}
	task["queue"] = append(A(task["queue"]), entry)
	task["queuedPrompts"] = len(A(task["queue"]))
	if err = m.publishLocked(task, M{"type": "state", "task": task}, nil); err != nil {
		return nil, err
	}
	result := M{"queued": len(A(task["queue"])), "status": task["status"]}
	if !B(task["queuePaused"]) {
		m.startLocked(id)
	}
	return result, nil
}
func (m *Manager) startLocked(id string) {
	if m.runs[id] != nil || m.closed {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &taskRun{cancel: cancel, done: make(chan struct{})}
	m.runs[id] = r
	go m.drain(ctx, id, r)
}
func (m *Manager) drain(ctx context.Context, id string, run *taskRun) {
	var runErr error
	awaitingDelivery := false
	defer func() {
		canContinue := runErr == nil
		if ctx.Err() != nil {
			confirmed := m.waitStopped(id)
			m.mu.Lock()
			if task := m.tasks[id]; task != nil {
				failure := OOrNil(task["lastFailure"])
				reason := "interrupted"
				// Delivery storage failures cancel the native turn to preserve queued
				// user work. Cancellation must not erase the originating failure.
				if len(failure) > 0 {
					reason = "error"
				} else if !confirmed {
					failure = M{"code": "stop_unconfirmed", "message": "执行连接已取消，但无法确认 Agent 已停止。检查运行服务后再继续。"}
				}
				_ = m.settleLocked(task, reason, failure)
				_ = m.settleChildrenLocked(task, failure)
			}
			m.mu.Unlock()
		} else if runErr != nil {
			// A native HTTP failure alone does not prove the session is missing.
			// Confirm only the official not-found payload; never recreate history.
			if Code(runErr) == "native_error" {
				if snapshot, err := m.Get(id); err == nil {
					_, lookup := m.options.Engine.Request(ctx, "GET", nativePath(snapshot, ""), nil)
					var native *nativeHTTPError
					if errors.As(lookup, &native) && Status(lookup) == 404 && S(native.Payload["message"]) == "session not found: session not found" {
						runErr = Problem(409, "native_session_missing", "Docker Agent 中已找不到此会话，当前无法续接原生历史。可恢复原生会话备份后重试，或新建对话；本地历史与文件仍保留。")
					}
				}
			}
			m.mu.Lock()
			if task := m.tasks[id]; task != nil {
				if len(A(task["queue"])) > 0 {
					task["queuePaused"] = true
				}
				code := Code(runErr)
				if code == "" {
					code = "execution_error"
				}
				message := runErr.Error()
				var network net.Error
				if errors.Is(runErr, io.ErrUnexpectedEOF) || errors.Is(runErr, io.EOF) || errors.As(runErr, &network) || strings.Contains(message, "Incomplete SSE frame") || code == "execution_disconnected" {
					code = "execution_disconnected"
					message = "与 Docker Agent 的执行连接中断。请核对已完成的操作后继续；任务未自动重试。"
					if S(O(task["pendingInteraction"])["purpose"]) == "iterations" {
						message = "已达本轮执行步数上限，等待继续确认时连接中断。" + message
					}
				}
				failure := M{"code": code, "message": message}
				canContinue = m.settleLocked(task, "error", failure) == nil
				if err := m.settleChildrenLocked(task, failure); err != nil {
					canContinue = false
				}
			}
			m.mu.Unlock()
		}
		m.refreshFiles(id)
		m.mu.Lock()
		delete(m.runs, id)
		for key, record := range m.normalizer.calls {
			owner := m.tasks[S(record["taskId"])]
			if S(record["taskId"]) == id || S(owner["parentTaskId"]) == id {
				delete(m.normalizer.calls, key)
			}
		}
		for key := range m.normalizer.assistants {
			if strings.HasPrefix(key, id+":") {
				delete(m.normalizer.assistants, key)
			}
		}
		close(run.done)
		// A submission can arrive after the last queue check while file
		// inventory is refreshing. Hand it to a new run under the same lock
		// that removes this one so accepted work cannot be stranded.
		if task := m.tasks[id]; task != nil && canContinue && ctx.Err() == nil && !m.changing && !m.closed && !B(task["queuePaused"]) && len(A(task["queue"])) > 0 {
			m.startLocked(id)
		}
		m.mu.Unlock()
	}()
	for ctx.Err() == nil {
		if !m.waitSending(ctx, id) {
			return
		}
		m.mu.Lock()
		task := m.tasks[id]
		if task == nil || len(A(task["queue"])) == 0 || B(task["queuePaused"]) {
			if task != nil && awaitingDelivery {
				runErr = m.settleLocked(task, "completed", nil)
			}
			m.mu.Unlock()
			return
		}
		entry := Clone(O(A(task["queue"])[0]))
		if S(O(entry["source"])["kind"]) == "delivery_required" && (S(O(entry["source"])["forTurnId"]) != S(task["turnId"]) || !B(O(m.Config()["workbench"])["requireDelivery"]) || !m.deliveryEnabled(task)) {
			task["queue"] = A(task["queue"])[1:]
			task["queuedPrompts"] = len(A(task["queue"]))
			runErr = m.publishLocked(task, M{"type": "state", "task": task}, nil)
			m.mu.Unlock()
			if runErr != nil {
				return
			}
			continue
		}
		if runErr = m.bind(task); runErr != nil {
			m.mu.Unlock()
			return
		}
		snapshot := Clone(task)
		snapshot["turnId"] = fmt.Sprintf("%s:turn-%d", id, I(task["turnCount"])+1)
		m.mu.Unlock()
		native, blocks, err := m.prepare(snapshot, S(entry["prompt"]), Strings(entry["attachmentIds"]))
		m.mu.Lock()
		task = m.tasks[id]
		queue := A(task["queue"])
		if ctx.Err() != nil || len(queue) == 0 || !reflect.DeepEqual(Clone(O(queue[0])), entry) || B(task["queuePaused"]) {
			m.mu.Unlock()
			continue
		}
		if err != nil {
			runErr = err
			m.mu.Unlock()
			return
		}
		task["queue"] = queue[1:]
		task["queuedPrompts"] = len(queue) - 1
		task["turnCount"] = I(task["turnCount"]) + 1
		task["turnId"] = snapshot["turnId"]
		task["stepId"] = S(task["turnId"]) + ":step-0"
		delete(task, "delegationIndex")
		task["nativeStopped"] = nil
		task["lastFailure"] = nil
		task["lastTurnReason"] = nil
		task["status"] = "working"
		task["activity"] = M{"phase": "preparing", "detail": "", "startedAt": NowMS()}
		StartTurnTiming(task, NowMS())
		user := M{"turnId": task["turnId"], "role": "user", "blocks": blocks}
		if entry["source"] != nil {
			user["source"] = entry["source"]
			if S(O(entry["source"])["kind"]) == "delivery_required" {
				for _, v := range blocks {
					block := O(v)
					if S(block["type"]) == "markdown" {
						O(block["data"])["provenance"] = M{"origin": "delivery-required"}
					}
				}
			}
		}
		awaitingDelivery = false
		_, runErr = m.appendLocked(task, user)
		if runErr == nil {
			runErr = m.publishLocked(task, M{"type": "state", "task": task}, nil)
		}
		snapshot = Clone(task)
		m.mu.Unlock()
		if runErr != nil {
			return
		}
		headers := map[string]string{}
		finish := func() {}
		if m.options.BeginTurn != nil {
			headers, finish, runErr = m.options.BeginTurn(ctx, snapshot)
			if runErr != nil {
				return
			}
			if finish == nil {
				finish = func() {}
			}
		}
		if m.options.ApprovalTools != nil {
			_, runErr = m.options.Engine.Request(ctx, "PATCH", nativePath(snapshot, "/permissions"), M{"permissions": M{"ask": m.options.ApprovalTools()}})
		}
		if runErr == nil && ctx.Err() == nil {
			runErr = m.options.Engine.Run(ctx, S(snapshot["nativeSessionId"]), S(snapshot["agentRef"]), S(snapshot["agent"]), []M{native}, S(snapshot["model"]), headers, func(event M) error { return m.HandleEvent(id, event) })
		}
		finish()
		if ctx.Err() != nil {
			return
		}
		if runErr != nil {
			return
		}
		if !m.waitSending(ctx, id) {
			return
		}
		m.mu.Lock()
		task = m.tasks[id]
		reason := S(task["nativeStopped"])
		if reason == "" {
			m.mu.Unlock()
			runErr = Problem(502, "execution_disconnected", "执行连接中断，未收到根会话的终止事件；任务未被自动重试。")
			return
		}
		if task["lastFailure"] != nil {
			reason = "error"
		}
		// A reminder is one follow-up to ordinary input, never a source of
		// another reminder. The native completion remains the execution fact.
		facts := deliveryFacts{Reason: reason, SourceKind: S(O(entry["source"])["kind"]), TurnID: S(task["turnId"]), Enabled: B(O(m.Config()["workbench"])["requireDelivery"]), Permitted: m.deliveryEnabled(task)}
		if facts.eligible() {
			has, e := m.options.Store.HasDelivery(id, S(task["turnId"]))
			if e != nil {
				runErr = e
				m.mu.Unlock()
				return
			}
			facts.Delivered = has
			if turnID := deliveryFollowup(facts); turnID != "" {
				entry := modelContent.DeliverySupplement(turnID)
				entry["id"] = ID()
				task["queue"] = append([]any{entry}, A(task["queue"])...)
				task["queuedPrompts"] = len(A(task["queue"]))
				task["deliveryContinuationForTurnId"] = task["turnId"]
				task["activity"] = M{"phase": "finishing", "detail": "正在补充交付", "startedAt": NowMS()}
				FinishTurnTiming(task, true, NowMS())
				awaitingDelivery = true
				runErr = m.publishLocked(task, M{"type": "state", "task": task}, nil)
				m.mu.Unlock()
				if runErr != nil {
					return
				}
				m.refreshFiles(id)
				continue
			}
		}
		runErr = m.settleLocked(task, reason, OOrNil(task["lastFailure"]))
		if reason != "completed" && len(A(task["queue"])) > 0 {
			task["queuePaused"] = true
			if runErr == nil {
				runErr = m.publishLocked(task, M{"type": "state", "task": task}, nil)
			}
		}
		m.mu.Unlock()
		if runErr != nil || reason != "completed" {
			return
		}
		m.refreshFiles(id)
	}
}
func OOrNil(v any) M {
	if v == nil {
		return nil
	}
	return O(v)
}
func (m *Manager) deliveryEnabled(t M) bool {
	return Contains(configuredTuckTools(m.Config(), S(t["agent"]), S(t["parentTaskId"]) == ""), "deliver_result")
}
func (m *Manager) waitStopped(id string) bool {
	t, err := m.Get(id)
	if err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for {
		probe, c := context.WithTimeout(ctx, time.Second)
		v, e := m.options.Engine.Request(probe, "GET", nativePath(t, "/status"), nil)
		c()
		if e != nil {
			return false
		}
		if !B(v["streaming"]) {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(100 * time.Millisecond):
		}
	}
}
func (m *Manager) Abort(ctx context.Context, id string) error {
	m.mu.Lock()
	t, err := m.getLocked(id)
	if err != nil {
		m.mu.Unlock()
		return err
	}
	queue := []any{}
	for _, v := range A(t["queue"]) {
		if S(O(O(v)["source"])["kind"]) != "delivery_required" {
			queue = append(queue, v)
		}
	}
	t["queue"] = queue
	t["queuedPrompts"] = len(queue)
	r := m.runs[id]
	if r == nil {
		err = m.publishLocked(t, M{"type": "state", "task": t}, nil)
		m.mu.Unlock()
		return err
	}
	r.stopping = true
	t["queuePaused"] = len(queue) > 0
	r.cancel()
	m.mu.Unlock()
	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (m *Manager) Active() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.runs) > 0 {
		return true
	}
	for _, t := range m.tasks {
		if TaskIsActive(t) {
			return true
		}
	}
	return false
}
func (m *Manager) Running(id string) bool { m.mu.Lock(); defer m.mu.Unlock(); return m.runs[id] != nil }
func (m *Manager) SetConfigurationChanging(v bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.changing = v
	if v {
		m.configurationRevision++
	}
}
func (m *Manager) Close(ctx context.Context) error {
	m.mu.Lock()
	m.closed = true
	ids := []string{}
	for id := range m.runs {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	var result error
	for _, id := range ids {
		if err := m.Abort(ctx, id); err != nil {
			result = errors.Join(result, err)
		}
	}
	return result
}
func (m *Manager) refreshFiles(id string) {
	if m.options.RefreshFiles == nil {
		return
	}
	t, err := m.Get(id)
	if err != nil {
		return
	}
	files, err := m.options.RefreshFiles(t)
	if err != nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	task := m.tasks[id]
	known := map[string]float64{}
	for _, v := range A(task["artifacts"]) {
		known[S(O(v)["path"])] = N(O(v)["modified"])
	}
	changed := []any{}
	for _, v := range files {
		f := O(v)
		old, ok := known[S(f["path"])]
		if !ok || old != N(f["modified"]) {
			path := S(f["path"])
			changed = append(changed, M{"id": id + ":file:" + path, "name": path, "kind": "file", "sizeBytes": f["size"], "src": "/api/tasks/" + id + "/files?path=" + url.QueryEscape(path), "resource": resource(task, path)})
		}
	}
	task["artifacts"] = files
	if m.publishLocked(task, M{"type": "state", "task": task}, nil) != nil {
		return
	}
	if len(changed) > 0 {
		_, _ = m.appendLocked(task, M{"turnId": task["turnId"], "role": "assistant", "blocks": []any{newBlock("artifacts:"+UUID(), "attachments", M{"title": "工作目录中的实际文件", "provenance": M{"origin": "tool-output"}, "items": changed})}})
	}
}
func (m *Manager) SetApprovalMode(ctx context.Context, id, mode string) (M, error) {
	if mode != "auto" && mode != "manual" {
		return nil, Problem(400, "invalid_approval", "审批模式必须为 manual 或 auto")
	}
	m.mu.Lock()
	task, e := m.getLocked(id)
	if e == nil {
		e = m.checkLocked(task, false)
	}
	if e == nil && m.policyChanges[id] {
		e = Problem(409, "policy_busy", "审批模式正在保存")
	}
	if e != nil {
		m.mu.Unlock()
		return nil, e
	}
	m.policyChanges[id] = true
	t := Clone(task)
	m.mu.Unlock()
	defer func() { m.mu.Lock(); delete(m.policyChanges, id); m.mu.Unlock() }()
	policy := "balanced"
	if mode == "auto" {
		policy = "autonomous"
	}
	if _, e = m.options.Engine.Request(ctx, "PATCH", nativePath(t, "/safety-policy"), M{"safety_policy": policy}); e != nil {
		return nil, e
	}
	return m.Update(id, func(t M) error { t["approvalMode"] = mode; return nil })
}
func (m *Manager) UpdateQueue(ctx context.Context, id string, in M) (M, error) {
	action := S(in["action"])
	m.mu.Lock()
	task, e := m.getLocked(id)
	if e == nil {
		e = m.checkLocked(task, action == "edit" || action == "remove" || action == "clear")
	}
	if e == nil && m.sending[id] {
		e = Problem(409, "queue_sending", "消息正在发送，请稍后操作")
	}
	if e != nil {
		m.mu.Unlock()
		return nil, e
	}
	queue := A(task["queue"])
	index := -1
	for i, v := range queue {
		if S(O(v)["id"]) == S(in["entryId"]) {
			index = i
		}
	}
	switch action {
	case "clear":
		queue = []any{}
		task["queuePaused"] = false
	case "resume":
		if task["pendingInteraction"] != nil || m.runs[id] != nil {
			e = Problem(409, "task_active", "请先处理当前执行或审批")
		} else if e = m.capacityLocked(task); e == nil {
			e = m.bind(task)
		}
		if e == nil {
			task["queuePaused"] = false
		}
	case "edit", "remove", "send":
		if index < 0 {
			e = Problem(409, "queue_changed", "这条消息已发送或移除，请刷新队列")
			break
		}
		entry := O(queue[index])
		if action == "edit" {
			var text string
			text, e = promptText(S(in["prompt"]), len(A(entry["attachmentIds"])) > 0)
			if e == nil {
				entry["prompt"] = text
			}
		} else if action == "remove" {
			queue = append(queue[:index], queue[index+1:]...)
		} else {
			if task["pendingInteraction"] != nil {
				e = Problem(409, "interaction_pending", "请先处理当前审批或问题")
				break
			}
			if e = m.bind(task); e != nil {
				break
			}
			if S(task["status"]) == "working" {
				m.sending[id] = true
				m.sendWait[id] = make(chan struct{})
				entry = Clone(entry)
				m.mu.Unlock()
				_, e = m.Steer(ctx, id, S(entry["prompt"]), Strings(entry["attachmentIds"]), nil)
				m.mu.Lock()
				task = m.tasks[id]
				delete(m.sending, id)
				close(m.sendWait[id])
				delete(m.sendWait, id)
				queue = A(task["queue"])
				if e != nil {
					task["queuePaused"] = true
					_ = m.publishLocked(task, M{"type": "state", "task": task}, nil)
				} else {
					next := []any{}
					for _, v := range queue {
						if S(O(v)["id"]) != S(entry["id"]) {
							next = append(next, v)
						}
					}
					queue = next
				}
			} else if m.runs[id] != nil {
				e = Problem(409, "task_finishing", "上一轮正在收尾，请稍后发送")
			} else if e = m.capacityLocked(task); e == nil {
				queue = append([]any{entry}, append(queue[:index], queue[index+1:]...)...)
				task["queuePaused"] = false
			}
		}
	default:
		e = Problem(400, "invalid_queue_action", "未知队列操作")
	}
	if e != nil {
		m.mu.Unlock()
		return nil, e
	}
	task["queue"] = queue
	task["queuedPrompts"] = len(queue)
	if len(queue) == 0 {
		task["queuePaused"] = false
	}
	e = m.publishLocked(task, M{"type": "state", "task": task}, nil)
	result := Clone(task)
	if e == nil && (action == "resume" || action == "send") && !B(task["queuePaused"]) {
		m.startLocked(id)
	}
	m.mu.Unlock()
	return result, e
}
func (m *Manager) ForkOptions(ctx context.Context, id string) ([]M, error) {
	m.mu.Lock()
	t, e := m.getLocked(id)
	if e == nil {
		e = m.checkLocked(t, false)
	}
	if e == nil && (S(t["status"]) != "idle" || m.runs[id] != nil) {
		e = Problem(409, "task_active", "请等待当前执行结束后创建分支")
	}
	task := Clone(t)
	m.mu.Unlock()
	if e != nil {
		return nil, e
	}
	native, e := m.options.Engine.Request(ctx, "GET", nativePath(task, ""), nil)
	if e != nil {
		return nil, e
	}
	result := []M{}
	ordinal := 0
	for _, v := range A(native["messages"]) {
		w := O(v)
		msg := w
		if w["message"] != nil {
			msg = O(w["message"])
		}
		if S(msg["role"]) != "user" {
			continue
		}
		index := ordinal
		ordinal++
		if S(w["agent_name"]) != "" && S(w["agent_name"]) != S(task["agent"]) {
			continue
		}
		result = append(result, M{"index": index, "prompt": msg["content"]})
	}
	return result, nil
}
func (m *Manager) Fork(ctx context.Context, id string, index int) (M, error) {
	if index < 0 {
		return nil, Problem(400, "invalid_fork", "无效的分支位置")
	}
	m.mu.Lock()
	revision := m.configurationRevision
	m.mu.Unlock()
	options, e := m.ForkOptions(ctx, id)
	if e != nil {
		return nil, e
	}
	var selected M
	for _, o := range options {
		if I(o["index"]) == index {
			selected = o
		}
	}
	if selected == nil {
		return nil, Problem(409, "fork_changed", "分支位置已变化，请重新选择")
	}
	parent, e := m.Get(id)
	if e != nil {
		return nil, e
	}
	native, e := m.options.Engine.Request(ctx, "POST", nativePath(parent, "/fork"), M{"user_message_index": index})
	if e != nil {
		return nil, e
	}
	title := TaskName(S(native["title"]))
	if title != S(native["title"]) {
		if _, e = m.options.Engine.Request(ctx, "PATCH", "/api/sessions/"+url.PathEscape(S(native["id"]))+"/title", M{"title": title}); e != nil {
			return nil, e
		}
	}
	task := BaseTask(ID(), S(native["id"]), title, S(parent["workspace"]), S(parent["agent"]), m.options.AgentRef)
	for _, k := range []string{"model", "modelId", "modelSource", "modelName", "modelProtocol", "reasoningEffort", "approvalMode", "agentRef", "assistantName", "workspaceMode"} {
		task[k] = parent[k]
	}
	task["forkedFrom"] = M{"id": id, "title": parent["title"], "userMessageIndex": index}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.configurationRevision != revision || m.changing || m.closed {
		return nil, Problem(409, "configuration_changing", "配置已变化，请重新创建分支")
	}
	current := m.tasks[id]
	if e = m.checkLocked(current, false); e != nil {
		return nil, e
	}
	if m.runs[id] != nil || S(current["status"]) != "idle" || I(current["logicalCount"]) != I(parent["logicalCount"]) || I(current["turnCount"]) != I(parent["turnCount"]) {
		return nil, Problem(409, "fork_changed", "原会话已变化，请重新选择分支位置")
	}
	m.tasks[S(task["id"])] = task
	if e = m.publishLocked(task, M{"type": "created", "task": task}, nil); e != nil {
		return nil, e
	}
	if e = m.normalizer.importHistory(task, A(native["messages"])); e != nil {
		return nil, e
	}
	count := 0
	for _, v := range A(native["messages"]) {
		w := O(v)
		if w["message"] != nil {
			w = O(w["message"])
		}
		if S(w["role"]) == "user" {
			count++
		}
	}
	task["turnCount"] = count
	if e = m.publishLocked(task, M{"type": "state", "task": task}, nil); e != nil {
		return nil, e
	}
	return M{"task": Clone(task), "draft": selected["prompt"]}, nil
}
func (m *Manager) Archive(id string, archived bool) (M, error) {
	return m.Update(id, func(t M) error {
		if S(t["status"]) != "idle" || m.runs[id] != nil {
			return Problem(409, "task_active", "请先等待任务结束、停止任务或处理当前交互")
		}
		t["archived"] = archived
		return nil
	})
}
func (m *Manager) createChildLocked(root M, nativeID, agent, mode string) (M, error) {
	for _, t := range m.tasks {
		if S(t["nativeSessionId"]) == nativeID {
			return t, nil
		}
	}
	task := BaseTask(ID(), nativeID, agent+" · 子任务", S(root["workspace"]), agent, m.options.AgentRef)
	task["parentTaskId"] = root["id"]
	task["mode"] = mode
	task["workspaceMode"] = root["workspaceMode"]
	task["turnId"] = S(task["id"]) + ":turn-1"
	m.tasks[S(task["id"])] = task
	return task, m.publishLocked(task, M{"type": "created", "task": task}, nil)
}
func (m *Manager) settleLocked(task M, reason string, failure M) error {
	if err := m.interruptToolsLocked(task); err != nil {
		return err
	}
	delete(m.interactions, S(task["id"]))
	if err := m.settleSteeringLocked(task); err != nil {
		return err
	}
	delete(m.normalizer.activityTools, S(task["id"]))
	confirmed := true
	switch S(failure["code"]) {
	case "service_restarted", "stop_unconfirmed", "execution_disconnected":
		confirmed = false
	}
	FinishTurnTiming(task, confirmed, NowMS())
	m.normalizer.timings.clear(S(task["id"]))
	if err := m.normalizer.endCompaction(task, reason); err != nil {
		return err
	}
	if err := m.normalizer.finishAssistant(task, reason); err != nil {
		return err
	}
	var lastFailure any
	if len(failure) > 0 {
		lastFailure = failure
	}
	return m.stateLocked(task, M{"status": "idle", "lastTurnReason": reason, "lastFailure": lastFailure, "pendingInteraction": nil, "nativeInteraction": nil, "activeAssistantIndex": nil, "activity": nil})
}
func (m *Manager) settleChildrenLocked(root M, failure M) error {
	changed := false
	for _, child := range m.tasks {
		if child["parentTaskId"] != root["id"] || S(child["status"]) == "idle" {
			continue
		}
		if err := m.settleLocked(child, "interrupted", failure); err != nil {
			return err
		}
		delete(m.normalizer.children, S(child["id"]))
		changed = true
	}
	if changed && root["delegationIndex"] != nil {
		return m.normalizer.delegation(root)
	}
	return nil
}
func (m *Manager) interruptToolsLocked(task M) error {
	for _, r := range m.normalizer.calls {
		if S(r["taskId"]) != S(task["id"]) {
			continue
		}
		msg, e := m.options.Store.Message(S(task["id"]), I(r["index"]))
		if e != nil {
			return e
		}
		if e = m.interruptMessageLocked(task, msg); e != nil {
			return e
		}
	}
	return nil
}
func (m *Manager) interruptMessageLocked(task, msg M) error {
	if msg == nil {
		return nil
	}
	changed := B(msg["live"])
	msg["live"] = false
	for _, b := range A(msg["blocks"]) {
		block := O(b)
		data := O(block["data"])
		if S(block["type"]) == "terminal" && S(data["status"]) == "running" || S(block["type"]) == "reasoning" && S(data["status"]) == "streaming" {
			data["status"] = "interrupted"
			changed = true
		}
	}
	if changed {
		return m.replaceLocked(task, msg)
	}
	return nil
}
func trimEnd(s string) string { return strings.TrimRightFunc(s, unicode.IsSpace) }

func (m *Manager) waitSending(ctx context.Context, id string) bool {
	m.mu.Lock()
	ch := m.sendWait[id]
	m.mu.Unlock()
	if ch == nil {
		return true
	}
	select {
	case <-ch:
		return true
	case <-ctx.Done():
		return false
	}
}
