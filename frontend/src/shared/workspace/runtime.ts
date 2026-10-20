import type { WorkspaceMode, WorkspacePolicy } from '../../../shared/workspace-mode.mjs'
import { applyMessageDelta, type MessageDelta } from './message-delta'
import { AttachmentDrafts } from './attachment-drafts'
import { appUrl, messageUrls } from '../../app/urls'
import { authFetch, loginSession, expiredLogin } from '../../app/auth'
import {settingsAccess} from '../../app/settings-access'
import { createWorkbenchProjection } from './projection'
import { BatchedNotifier } from '../../engine/core/notifier'
import { SegmentManager } from '../../engine/core/segment-manager'
import { CHAT_HISTORY } from './history-policy'
import { CachedHistory } from './history-cache'
import { ConversationSessionKernel } from '../../engine/conversation/session-kernel'
import type { ConversationDescriptor, ConversationExecutionController, InteractionResolution, SubmitDisposition } from '../../engine/conversation/contracts'
import type { LogicalMessage } from '../../engine/model/conversation'
import { ConversationSessionRuntime } from '../../engine/runtime/session-runtime'
import type { SessionViewMemory } from '../../engine/viewport/state'
import { deriveSessionIndicator } from '../../engine/conversation/session-semantics'
import { taskName, isChannelTask } from '../../../shared/task-channel.mjs'
export type WorkSessionDescriptor = ConversationDescriptor & { age: string; agent?: string; taskChannel?: boolean }

export interface DockerTask extends ConversationDescriptor {
  lifecycle?: { purpose: string; keep: boolean; lastActivityAt: number; archivedAt: number; lastDeleteError?: string }
  managedWorkspaceId?: string
  turnId?: string
  activity?: { phase: 'preparing' | 'processing' | 'reasoning' | 'responding' | 'tool' | 'compacting' | 'finishing'; detail?: string; startedAt: number; afterCompaction?: boolean } | null
  turnTiming?: { turnId: string; startedAt: number; observedAt: number; endedAt?: number; waitingMs: number; waitingSince?: number; incomplete?: boolean }
  usage: { inputTokens: number; outputTokens: number; cacheReadTokens: number; cacheWriteTokens: number; reasoningTokens: number }
  context: { projectedTokens: number; contextWindow: number; reported?: boolean }
  compaction?: { status: 'running' | 'applied' | 'skipped' | 'failed' | 'interrupted' | 'unknown'; startedAt?: string; completedAt?: string; beforeTokens?: number | null }
  timings?: { streamMs?: number; streamTokens?: number; streamSamples?: number; toolMs?: number; toolSamples?: number }
  nativeSessionId: string; workspace: string; workspaceMode?: WorkspaceMode; agent: string; assistantName?: string; parentTaskId?: string; createdAt: string; archived: boolean
  artifacts: { path: string; size: number; modified: number }[]
  latestDelivery?: {version:number;turnId:string;createdAt:number;summary:string;fileCount:number;totalBytes:number}
  queue: { id: string; prompt: string; attachmentIds?: string[]; attachmentNames?: string[] }[]
  approvalMode: 'manual' | 'auto'
  queuePaused?: boolean
  modelId?: string; modelName?: string; modelProtocol?: string; modelSource?: 'platform'|'local'
  reasoningEffort?: string
  modelCalls?: number | null
  usageSource?: 'live' | 'recovery' | 'unavailable'
  mode?: 'foreground' | 'background'
  backgroundRuns?: {turnId?:string; id: string; agent: string; title: string; status?: 'running' | 'completed' | 'failed' | 'stopped'; summary?: string; observedAt?: string }[]
  forkedFrom?: { id: string; title: string; userMessageIndex: number }
}
export interface ConnectionInfo {
  attachmentInspection?:Record<string,{available:boolean;modelId?:string;reason?:string}>
  localMcp?: {name:string;displayName:string;url:string;enabled:boolean;status:'pending'|'ready'|'unavailable'|'disabled';validated:boolean;toolCount:number;error?:string|null}[]
  mode: 'platform' | 'direct' | 'template'; status: string; defaultModelId?: string; deploymentName?: string; employeeName?: string; origin?: string; managedGeneration?: number; schemaVersion?: number; compatibilityPlatform?: string; error?: string | null
  models: { providerId?:string; source?:'platform'|'local'; modelId: string; displayName: string; protocol: string; selectable: boolean; reason?: string | null; lastError?: string | null; inputModalities: string[]; capabilities: string[]; reasoningEfforts?: string[]; contextWindow?:number; maxOutputTokens?:number; settingsSource?:string }[]
  mcp?: {mcpServerId:string;displayName:string;enabled:boolean;status:'pending'|'ready'|'unavailable'|'disabled';validated:boolean;toolCount:number;error?:string|null}[]
}
export interface AssistantCatalog { defaultAgent: string; items: { agent: string; displayName: string; description?: string; taskConcurrency?: number; deliveryTool?: boolean; workspace?: WorkspacePolicy; defaultModelId?: string }[] }
export interface AssistantSummaryInfo {
  settingsLocked:boolean; mcpLocked:boolean;
  settings?:{readonly:boolean;maxIterations:number|null;subAgents:string[];defaultModel:string;instructionPreview:string};
  mcp?:{id:string;displayName:string;source:'local'|'platform';enabled:boolean;status:string;validated:boolean;toolCount:number|null}[];
  availableTools:number|null;toolsError:string|null;observedAt:number;
}
export const defaultAssistants: AssistantCatalog = { defaultAgent: 'root', items: [{ agent: 'root', displayName: '通用助手' }] }
export interface OrchestrationInfo { requireDelivery:boolean; updatedAt:number }
export interface RuntimeInfo { retentionRevision?:string; deliveryToolAvailable?:boolean; orchestration?:OrchestrationInfo; state: string; version: string; workspaceRoot: string; error: string | null; connection?: ConnectionInfo; assistants?: AssistantCatalog }
type WorkbenchEvent = { seq: number; taskId: string; type: 'deleted' | 'created' | 'state' | 'message' | 'message-delta' | 'native' | 'connection' | 'orchestration' | 'retention'; retentionRevision?:string; deletedTaskIds?:string[]; orchestration?:OrchestrationInfo; task?: DockerTask; message?: LogicalMessage; delta?: MessageDelta; connection?: ConnectionInfo; assistants?: AssistantCatalog }

export async function api<T>(path: string, body?: unknown, signal?:AbortSignal): Promise<T> {
  const response = await authFetch(path, { method: body === undefined ? 'GET' : 'POST', headers: { 'Content-Type': 'application/json' }, body: body === undefined ? undefined : JSON.stringify(body),signal })
  const value = await response.json()
  if (!response.ok) throw Object.assign(new Error(value.error || `HTTP ${response.status}`), {status:response.status})
  return value as T
}

export class DockerExecutionController implements ConversationExecutionController {
  readonly mode = 'docker-agent'
  ingressTicks = 0
  publishTicks = 0
  constructor(readonly workspace: DockerWorkspaceRuntime, readonly id: string) {}
  get running(): boolean { return this.workspace.kernelFor(this.id).status === 'working' }
  abort(): void { void this.workspace.command(this.id, 'abort', {}) }
  submit(prompt: string): SubmitDisposition {
    const kernel = this.workspace.kernelFor(this.id)
    if (kernel.pendingInteraction) return 'blocked'
    void this.workspace.command(this.id, 'prompts', { prompt })
    return kernel.status === 'working' ? 'queued' : 'started'
  }
  resolveInteraction(response: InteractionResolution): void { void this.workspace.command(this.id, 'interaction', response) }
}

export class DockerWorkspaceRuntime {
  readonly attachmentDrafts = new AttachmentDrafts()
  readonly live = true
  readonly tasks = new Map<string, DockerTask>()
  readonly runtimeInfo: RuntimeInfo
  error: string | null = null
  connected = false
  taskRevision = 0
  #kernels = new Map<string, ConversationSessionKernel>()
  #sources = new Map<string, CachedHistory>()
  #executions = new Map<string, DockerExecutionController>()
  #runtimes = new Map<string, ConversationSessionRuntime>()
  #snapshots = new Map<string, SessionViewMemory>()
  #notifier = new BatchedNotifier()
  #events: EventSource | null = null
  #activeSessionId = ''
  #preparingSessionId = ''
  #defaultSession?: Promise<string>
  #cursor = 0
  #stateCursors = new Map<string, number>()
  #deleted = new Set<string>()
  #eventError: string | null = null
  #descriptorCache = new Map<string, WorkSessionDescriptor>()
  #descriptorList: readonly WorkSessionDescriptor[] | null = null
  #registrations = new Map<string, Promise<void>>()
  #disposed = false
  #reconnect: ReturnType<typeof setTimeout> | undefined
  #kernelSubscriptions = new Map<string, () => void>()

  private constructor(info: RuntimeInfo) { this.runtimeInfo = info }
  static async connect(eager = true): Promise<DockerWorkspaceRuntime> {
    const info = await api<RuntimeInfo>('/api/runtime')
    if (info.state !== 'ready') throw new Error(info.error || 'Docker Agent 服务正在启动，请稍后重试')
    const workspace = new DockerWorkspaceRuntime(info)
    try {
      const snapshot = await api<{ tasks: DockerTask[]; cursor: number }>('/api/tasks')
      for (const task of snapshot.tasks) { await workspace.#register(task); workspace.#stateCursors.set(task.id,snapshot.cursor) }
      const root = snapshot.tasks.find(task => !task.parentTaskId && !task.archived)
      workspace.#cursor = snapshot.cursor
      workspace.#activeSessionId = root?.id || (eager ? await workspace.ensureDefaultSession() : '')
      if (eager && workspace.#activeSessionId) {
        await workspace.prepareSession(workspace.#activeSessionId)
        workspace.kernelFor(workspace.#activeSessionId).setForeground(true)
        workspace.ensureRuntime(workspace.#activeSessionId)
      }
      workspace.#listen()
    } catch (error) { workspace.dispose(); throw error }
    return workspace
  }
  async #register(task: DockerTask): Promise<void> {
    if (this.#deleted.has(task.id)) return
    if (this.#kernels.has(task.id)) return
    const pending = this.#registrations.get(task.id)
    if (pending) return pending
    const registration = this.#registerNew(task)
    this.#registrations.set(task.id, registration)
    try { await registration } finally { this.#registrations.delete(task.id) }
  }
  async #registerNew(task: DockerTask): Promise<void> {
    this.tasks.set(task.id, task)
    this.taskRevision++
    const source = this.#historySource(task)
    if (task.activeAssistantIndex != null) await source.prepare(task.activeAssistantIndex, 1)
    if (this.#disposed || this.#deleted.has(task.id)) return
    const kernel = new ConversationSessionKernel(task, source)
    for (const entry of task.queue || []) kernel.enqueue(entry.prompt)
    this.#sources.set(task.id, source)
    this.#kernels.set(task.id, kernel)
    this.#executions.set(task.id, new DockerExecutionController(this, task.id))
    this.#refreshDescriptor(task.id)
    this.#kernelSubscriptions.set(task.id,kernel.subscribe(() => { this.#refreshDescriptor(task.id); this.#notifier.markDirty() }))
  }
  #historySource(task: DockerTask): CachedHistory {
    return new CachedHistory(task.id, task.logicalCount, async (start, count) => {
      const result = await api<{ messages: LogicalMessage[] }>(`/api/tasks/${task.id}/messages?start=${start}&count=${count}&view=workbench`)
      return result.messages.map(messageUrls)
    }, () => ({ range: this.#runtimes.get(task.id)?.range, activeIndex: this.tasks.get(task.id)?.activeAssistantIndex }))
  }
  async #resetReplay(): Promise<void> {
    const knownBefore = new Set(this.tasks.keys())
    const snapshot = await api<{ tasks: DockerTask[]; cursor: number }>('/api/tasks')
    const present = new Set(snapshot.tasks.map(task => task.id))
    this.#removeSessions([...knownBefore].filter(id => !present.has(id) && (this.#stateCursors.get(id) || 0) <= snapshot.cursor))
    for (const task of snapshot.tasks) {
      if (this.#disposed) return
      if (this.#deleted.has(task.id)) continue
      await this.#register(task)
      // A newer HTTP task response may have arrived during the snapshot read.
      let state = (this.#stateCursors.get(task.id) || 0) > snapshot.cursor ? this.tasks.get(task.id)! : task
      for (;;) {
        if (this.#deleted.has(task.id)) break
        const previous = this.tasks.get(task.id)
        const source = this.#historySource(state)
        const window = () => {
          const runtime = this.#runtimes.get(task.id)
          if (!runtime) return { start: 0, end: 0 }
          if (runtime.followTail) return { start: Math.max(0, state.logicalCount - CHAT_HISTORY.windowMessages), end: state.logicalCount }
          return { start: runtime.range.start, end: Math.min(state.logicalCount, runtime.range.end) }
        }
        // Navigation can change while a page is loading. Prepare the actual view
        // before swapping sources; failed reads leave the existing view intact.
        for (;;) {
          const range = window()
          await source.prepareView(range.start, range.end - range.start)
          if (state.activeAssistantIndex != null) await source.prepare(state.activeAssistantIndex, 1)
          if (this.#disposed) return
          if (this.#deleted.has(task.id)) break
          const current = window()
          if (range.start === current.start && range.end === current.end) break
        }
        if (this.#deleted.has(task.id)) break
        if (this.tasks.get(task.id) !== previous) { state = this.tasks.get(task.id)!; continue }
        this.#sources.set(task.id, source)
        this.kernelFor(task.id).replaceHistory(source, state)
        this.#applyState(state, Math.max(snapshot.cursor, this.#stateCursors.get(task.id) || 0))
        break
      }
    }
    if (this.#disposed) return
    this.#cursor = snapshot.cursor
    if (this.error === this.#eventError) this.error = null
    this.#eventError = null
    this.#notifier.notifyNow()
  }
  #listen(): void {
    if (this.#disposed) return
    const source = new EventSource(appUrl(`/api/events?since=${this.#cursor}&view=workbench`))
    this.#events = source
    let chain = Promise.resolve()
    let failed = false, reconnecting = false
    const reconnect = () => {
      if (reconnecting || this.#disposed) return
      reconnecting = true; source.close(); this.connected = false; this.#notifier.notifyNow()
      // EventSource's last received ID may be ahead of asynchronously applied
      // events. Reconnect from our committed cursor after the current queue drains.
      void chain.finally(() => {
        if (!this.#disposed) this.#reconnect = setTimeout(() => this.#listen(), 1000)
      })
    }
    source.onopen = () => { this.connected = true; void this.refreshConnection().catch(error => { this.error = error.message; this.#notifier.notifyNow() }); this.#notifier.notifyNow() }
    source.onerror = () => { void loginSession().then(session => { if (!session.authenticated) { source.close(); expiredLogin() } else reconnect() }).catch(reconnect) }
    source.addEventListener('reset', () => {
      source.close()
      chain = chain.then(() => this.#resetReplay()).catch(error => {
        this.#eventError = this.error = error.message
        this.#notifier.notifyNow()
      })
      reconnect()
    })
    source.onmessage = event => {
      chain = chain.then(async () => {
        if (failed || this.#disposed) return
        const update = JSON.parse(event.data) as WorkbenchEvent
        if (update.seq <= this.#cursor) return
        if (update.type === 'deleted') this.#removeSessions(update.deletedTaskIds || [])
        if (update.type === 'orchestration' && update.orchestration && (!this.runtimeInfo.orchestration || update.orchestration.updatedAt>=this.runtimeInfo.orchestration.updatedAt)) this.runtimeInfo.orchestration=update.orchestration
        if (update.type === 'connection' && update.connection) this.runtimeInfo.connection = update.connection
        if (update.type === 'connection' && update.assistants) this.runtimeInfo.assistants = update.assistants
        if (update.type === 'retention') this.runtimeInfo.retentionRevision=update.retentionRevision
        if (update.type === 'connection') this.taskRevision++
        if (update.type === 'created' && update.task && !this.#kernels.has(update.task.id)) await this.#register(update.task)
        const kernel = this.#kernels.get(update.taskId)
        if (kernel) {
          const execution = this.executionFor(update.taskId)
          execution.ingressTicks++
          if (update.type === 'message' && update.message) {
            const message = messageUrls(update.message)
            if (message.index === kernel.count) {
              if (kernel.count) await this.#sources.get(update.taskId)!.prepare(kernel.count - 1, 1)
              kernel.appendCanonicalMessages([message])
              if (message.revision) kernel.replaceCanonicalMessage(message.index, message)
            }
            else if (message.index < kernel.count) {
              await this.#sources.get(update.taskId)!.prepare(message.index, 1)
              const previous = kernel.getMessage(message.index)
              if ((message.revision ?? 0) > (previous.revision ?? 0)) kernel.replaceCanonicalMessage(message.index, message)
            }
            else throw new Error('History update has a gap; reload the workbench to restore it')
            execution.publishTicks++
          }
          if (update.type === 'message-delta' && update.delta) {
            const delta = update.delta
            await this.#sources.get(update.taskId)!.prepare(delta.index, 1)
            let previous: LogicalMessage | undefined
            try { previous = kernel.getMessage(delta.index) } catch { /* Recover a cold/gapped record below. */ }
            if (!previous || (previous.revision ?? 0) < delta.revision) {
              const applied = previous && applyMessageDelta(previous, delta)
              if (applied) kernel.replaceCanonicalMessage(delta.index, applied.message, applied.patch)
              else {
                const result = await api<{ messages: LogicalMessage[] }>(`/api/tasks/${update.taskId}/messages?start=${delta.index}&count=1&view=workbench`)
                const snapshot = result.messages[0] && messageUrls(result.messages[0])
                if (!snapshot || snapshot.id !== delta.messageId || (snapshot.revision ?? 0) < delta.revision) throw new Error('Message delta recovery failed')
                if (snapshot.index === kernel.count) {
                  if (kernel.count) await this.#sources.get(update.taskId)!.prepare(kernel.count - 1, 1)
                  kernel.appendCanonicalMessages([snapshot])
                  if (snapshot.revision) kernel.replaceCanonicalMessage(snapshot.index, snapshot)
                }
                else kernel.replaceCanonicalMessage(snapshot.index, snapshot)
              }
              execution.publishTicks++
            }
          }
          if (update.task) this.#applyState(update.task,update.seq)
        }
        this.#cursor = update.seq
        if (this.#eventError && this.error === this.#eventError) this.error = null
        this.#eventError = null
        this.#notifier.markDirty()
      }).catch(async error => {
        failed = true
        // A queued event can outlive its deleted conversation. Refresh the
        // authoritative list instead of retrying the same missing history.
        try {
          if (error.status !== 404) throw error
          await this.#resetReplay()
        } catch (cause) {
          this.#eventError = this.error = (cause as Error).message
          this.#notifier.notifyNow()
        }
        reconnect()
      })
    }
  }
  #applyState(task: DockerTask, cursor?: number): void {
    if (this.#deleted.has(task.id)) return
    // HTTP snapshots and SSE share server ordering. A late response must not
    // resurrect a resolved blocker; older replayed states must not undo a snapshot.
    if (cursor !== undefined) {
      if (cursor < (this.#stateCursors.get(task.id) || 0)) return
      this.#stateCursors.set(task.id,cursor)
    }
    this.tasks.set(task.id, task)
    this.taskRevision++
    const kernel = this.kernelFor(task.id)
    kernel.setTitle(task.title)
    const pending = kernel.pendingInteraction
    if (pending && pending.id !== task.pendingInteraction?.id) kernel.resolveInteraction(pending.kind === 'approval' ? { interactionId: pending.id, kind: 'approval', approved: false } : { interactionId: pending.id, kind: 'question', answer: null })
    if (task.status === 'working') {
      const active = task.activeAssistantIndex != null && task.activeAssistantIndex < kernel.count ? task.activeAssistantIndex : null
      if (kernel.status !== 'working') kernel.startExecution(active)
      else if (active !== null) kernel.continueExecutionAt(active)
    } else if (task.status === 'waiting' && task.pendingInteraction && !kernel.pendingInteraction) {
      if (kernel.status !== 'working') kernel.startExecution()
      kernel.requestInteraction(task.pendingInteraction)
    } else if (task.status === 'idle' && task.lastTurnReason) kernel.finishExecution(task.lastTurnReason, task.lastFailure || null)
    kernel.setActivePlan(task.activePlan || null)
    kernel.setAccounting(task.usage || {}, task.context)
    kernel.clearQueue()
    for (const entry of task.queue || []) kernel.enqueue(entry.prompt)
    this.#refreshDescriptor(task.id)
  }
  subscribe(listener: () => void): () => void { return this.#notifier.subscribe(listener) }
  #refreshDescriptor(id: string): void {
    const task = this.tasks.get(id)!
    const previous = this.#descriptorCache.get(id)
    if (task.parentTaskId || task.archived) {
      if (this.#descriptorCache.delete(id)) this.#descriptorList = null
      return
    }
    const summary = this.kernelFor(id).summary
    const next: WorkSessionDescriptor = { id, agent:task.agent, taskChannel:isChannelTask(task), title: taskName(summary.title), status: summary.status, unread: summary.unread,
      queuedPrompts: summary.queuedPrompts, pendingInteraction: summary.pendingInteraction, lastTurnReason: summary.lastTurnReason,
      logicalCount: summary.logicalCount, lastFailure: summary.lastFailure, age: previous?.age ?? new Date(task.createdAt).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) }
    if (previous && previous.title === next.title && previous.taskChannel === next.taskChannel && previous.status === next.status && previous.unread === next.unread && previous.queuedPrompts === next.queuedPrompts && previous.pendingInteraction?.id === next.pendingInteraction?.id && previous.lastTurnReason === next.lastTurnReason && previous.logicalCount === next.logicalCount && previous.lastFailure?.code === next.lastFailure?.code) return
    this.#descriptorCache.set(id, next)
    this.#descriptorList = null
  }
  get descriptors(): readonly WorkSessionDescriptor[] {
    return this.#descriptorList ??= [...this.#descriptorCache.values()].sort((a,b) => this.tasks.get(b.id)!.createdAt.localeCompare(this.tasks.get(a.id)!.createdAt))
  }
  get activeSessionId(): string { return this.#activeSessionId }
  ensureDefaultSession(): Promise<string> {
    if (this.#activeSessionId) return Promise.resolve(this.#activeSessionId)
    if (!(this.runtimeInfo.assistants || defaultAssistants).defaultAgent) return Promise.resolve('')
    // Mode navigation may outlive its page; bootstrap belongs to this owner.
    const pending = this.#defaultSession ??= this.createSession().catch(error => {
      this.#defaultSession = undefined
      throw error
    })
    return pending.then(id => {
      if (!this.#deleted.has(id)) return id
      // Bootstrap may finish after navigation left chat, without activation.
      if (this.#defaultSession === pending) this.#defaultSession = undefined
      return this.ensureDefaultSession()
    })
  }
  get activeSession(): ConversationSessionRuntime { return this.ensureRuntime(this.#activeSessionId) }
  get activeKernel(): ConversationSessionKernel { return this.kernelFor(this.#activeSessionId) }
  get hotSessionCount(): number { return this.#runtimes.size }
  get hotSessionIds(): readonly string[] { return [...this.#runtimes.keys()] }
  get runningSessionCount(): number { return this.descriptors.filter(task => deriveSessionIndicator(task) === 'working').length }
  get blockedSessionCount(): number { return this.descriptors.filter(task => deriveSessionIndicator(task) === 'blocked').length }
  get failedSessionCount(): number { return this.descriptors.filter(task => deriveSessionIndicator(task) === 'failed').length }
  hasSession(id: string): boolean { return this.#kernels.has(id) }
  hasHotRuntime(id: string): boolean { return this.#runtimes.has(id) }
  parentSessionId(id: string): string | null { return this.tasks.get(id)?.parentTaskId || null }
  kernelFor(id: string): ConversationSessionKernel { const value = this.#kernels.get(id); if (!value) throw new Error(`Unknown task ${id}`); return value }
  executionFor(id: string): DockerExecutionController { return this.#executions.get(id)! }
  snapshotFor(id: string): SessionViewMemory {
    const tail = Math.max(0, this.kernelFor(id).count - 1)
    const saved = this.#snapshots.get(id)
    if (saved && !saved.followTail) return saved
    return { logicalPosition:tail, anchorUnitId:null, anchorOffsetPx:0, followTail:true, atVisualBottom:true, draftText:saved?.draftText || '' }
  }
  saveSnapshot(id: string, snapshot: SessionViewMemory): void { if (!this.#deleted.has(id)) this.#snapshots.set(id, snapshot) }
  async prepareHistory(id: string, start: number, count: number): Promise<void> {
    for (;;) {
      const source = this.#sources.get(id)
      await source?.prepareView(start, count)
      if (this.#disposed || source === this.#sources.get(id)) return
      // A reset may replace the cache while navigation is awaiting its page.
      // Prepare that same destination in the current source before returning.
    }
  }
  async prepareSession(id: string): Promise<void> {
    // Restoring an evicted viewport uses the same centered window as its constructor.
    // A hot viewport may have shifted away from center; retain its exact range.
    const snapshot = this.snapshotFor(id)
    const segment = new SegmentManager(this.kernelFor(id).count, CHAT_HISTORY.windowMessages, CHAT_HISTORY.shiftMessages, snapshot.logicalPosition)
    const range = this.#runtimes.get(id)?.range || (snapshot.followTail ? segment.range : segment.around(snapshot.logicalPosition))
    await this.#prepareSessionRange(id, range)
  }
  async #prepareSessionRange(id: string, range: { start: number; end: number }): Promise<void> {
    this.#preparingSessionId = id
    for (const [key, source] of this.#sources) if (key !== id && !this.#runtimes.has(key)) source.release()
    try { await this.prepareHistory(id, range.start, range.end - range.start) }
    finally { if (this.#disposed || (this.#preparingSessionId !== id && !this.#runtimes.has(id))) this.#sources.get(id)?.release() }
  }
  ensureRuntime(id: string): ConversationSessionRuntime {
    let runtime = this.#runtimes.get(id)
    if (!runtime) {
      runtime = new ConversationSessionRuntime(this.kernelFor(id), this.snapshotFor(id), createWorkbenchProjection(), CHAT_HISTORY)
      this.#runtimes.set(id, runtime)
    }
    while (this.#runtimes.size > CHAT_HISTORY.hotSessions) {
      const victim = [...this.#runtimes.keys()].find(key => key !== this.#activeSessionId && key !== id)
      if (!victim) break
      const old = this.#runtimes.get(victim)!
      this.saveSnapshot(victim, old.snapshot())
      old.dispose()
      this.#runtimes.delete(victim)
      this.#sources.get(victim)?.release()
    }
    return runtime
  }
  activate(id: string, outgoing?: SessionViewMemory): ConversationSessionRuntime {
    if (outgoing) this.saveSnapshot(this.#activeSessionId, outgoing)
    if (this.hasSession(this.#activeSessionId)) this.kernelFor(this.#activeSessionId).setForeground(false)
    this.#activeSessionId = id
    this.kernelFor(id).setForeground(true)
    const runtime = this.ensureRuntime(id)
    this.#notifier.notifyNow()
    return runtime
  }
  suspendView(): void {
    this.#preparingSessionId = ''
    if (this.hasSession(this.#activeSessionId)) this.kernelFor(this.#activeSessionId).setForeground(false)
    for (const [id, runtime] of this.#runtimes) { this.saveSnapshot(id, runtime.rememberedSnapshot); runtime.dispose() }
    this.#runtimes.clear()
    for (const source of this.#sources.values()) source.release()
  }
  async prepareMessage(id: string, index: number): Promise<void> {
    const kernel = this.kernelFor(id)
    if (!Number.isSafeInteger(index) || index < 0 || index >= kernel.count) throw new Error('指定消息不存在')
    const range = new SegmentManager(kernel.count, CHAT_HISTORY.windowMessages, CHAT_HISTORY.shiftMessages, index).around(index)
    await this.#prepareSessionRange(id, range)
  }
  targetMessage(id: string, index: number): void {
    const existing = this.#runtimes.get(id)
    const snapshot = existing?.snapshot() || this.snapshotFor(id)
    existing?.dispose(); this.#runtimes.delete(id)
    this.saveSnapshot(id, { ...snapshot, logicalPosition:index, anchorUnitId:null, anchorOffsetPx:0, followTail:false, atVisualBottom:false })
  }
  async createSession(options: { title?: string; workspace?: string; workspaceMode?: WorkspaceMode; channel?: 'chat' | 'tasks'; agent?: string; modelId?: string; approvalMode?: 'manual' | 'auto' } = {}): Promise<string> { const task = await api<DockerTask>('/api/tasks', options); await this.#register(task); this.#notifier.markDirty(); return task.id }
  async forkSession(id: string, index: number): Promise<string> {
    const result = await api<{ task: DockerTask; draft: string }>(`/api/tasks/${id}/fork`, { index })
    await this.#register(result.task)
    this.#applyState(result.task)
    await this.prepareSession(result.task.id)
    this.ensureRuntime(result.task.id).setDraftText(result.draft)
    this.#notifier.markDirty()
    return result.task.id
  }
  async refreshConnection(): Promise<void> {
    const accessGeneration=settingsAccess.generation
    const info = await api<RuntimeInfo>('/api/runtime')
    if(this.#disposed||accessGeneration!==settingsAccess.generation)return
    this.runtimeInfo.retentionRevision = info.retentionRevision
    this.runtimeInfo.deliveryToolAvailable = info.deliveryToolAvailable
    if(info.orchestration && (!this.runtimeInfo.orchestration || info.orchestration.updatedAt>=this.runtimeInfo.orchestration.updatedAt))this.runtimeInfo.orchestration=info.orchestration
    this.runtimeInfo.connection = info.connection
    this.runtimeInfo.assistants = info.assistants
    this.taskRevision++
    this.#notifier.markDirty()
  }
  async selectModel(id: string, modelId: string): Promise<void> { this.#applyState(await api<DockerTask>(`/api/tasks/${id}/model`, { modelId })); this.#notifier.markDirty() }
  async refreshTask(id: string): Promise<DockerTask> {
    const { task, cursor } = await api<{task: DockerTask; cursor:number}>(`/api/tasks/${encodeURIComponent(id)}`)
    if (this.#disposed) throw new Error('工作台已关闭')
    await this.#register(task)
    if (this.#deleted.has(id)) throw Object.assign(new Error('此对话已删除'), {status:404})
    this.#applyState(task,cursor); this.#notifier.notifyNow()
    return this.tasks.get(id)!
  }
  async renameSession(id: string, title: string): Promise<void> { this.#applyState(await api<DockerTask>(`/api/tasks/${id}/rename`, { title })); this.#notifier.notifyNow() }
  async archiveSession(id: string): Promise<string> { const task = await api<DockerTask>(`/api/tasks/${id}/archive`, { archived: true }); this.#applyState(task); return [...this.tasks.values()].find(value => !value.parentTaskId && !value.archived)?.id || ((this.runtimeInfo.assistants || defaultAssistants).defaultAgent ? await this.createSession() : (this.#activeSessionId = '')) }
  #removeSessions(ids: readonly string[]): void {
    for (const id of ids) {
      this.#deleted.add(id)
      this.#kernelSubscriptions.get(id)?.()
      this.#kernelSubscriptions.delete(id)
      this.#runtimes.get(id)?.dispose()
      this.#sources.get(id)?.release()
      for (const item of this.attachmentDrafts.list(id)) this.attachmentDrafts.remove(id,item.id)
      this.tasks.delete(id); this.#kernels.delete(id); this.#executions.delete(id)
      this.#runtimes.delete(id); this.#sources.delete(id); this.#snapshots.delete(id)
      this.#descriptorCache.delete(id); this.#stateCursors.delete(id)
      if (this.#activeSessionId === id) { this.#activeSessionId = ''; this.#defaultSession = undefined }
      if (this.#preparingSessionId === id) this.#preparingSessionId = ''
    }
    if (ids.length) { this.taskRevision++; this.#descriptorList = null }
  }
  async deleteSession(id: string): Promise<void> {
    const result = await api<{deletedTaskIds:string[];cleanupPending?:boolean}>(`/api/tasks/${encodeURIComponent(id)}/delete`,{confirmed:true})
    this.#removeSessions(result.deletedTaskIds)
    if (result.cleanupPending) this.error = '对话已删除，部分文件尚未清理。交付副本将在下次启动时重试；自动工作文件夹会按当前清理设置继续处理。'
    this.#notifier.notifyNow()
  }
  async command(id: string, action: string, body: unknown): Promise<boolean> {
    try { this.error = null; await api(`/api/tasks/${id}/${action}`, body); return true }
    catch (error) { this.error = (error as Error).message; return false }
    finally { this.#notifier.notifyNow() }
  }
  dispose(): void {
    this.#disposed = true
    this.attachmentDrafts.dispose()
    clearTimeout(this.#reconnect)
    this.#events?.close()
    for (const off of this.#kernelSubscriptions.values()) off()
    this.#kernelSubscriptions.clear()
    this.suspendView()
  }
}
