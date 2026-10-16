<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import { usePerformanceMetrics } from '../use-performance-metrics'
import type { SessionViewMemory } from '../../engine/viewport/state'
import {
  deriveSessionIndicator,
  sessionIndicatorGlyph,
  sessionIndicatorLabel,
  type SessionIndicator,
} from '../../engine/conversation/session-semantics'
import { createAgentScenarioPack, createMarkdownGalleryTurn, createMixedReferenceTurns } from '../scenarios'
import type { ReferenceSessionDescriptor } from '../workspace-fixtures'
import { useWorkspaceRuntime } from '../vue/use-workspace-runtime'
import { ActivePlanStrip, ConversationViewport } from '../../engine/vue'
import ReferenceDiagnosticsPanel from './ReferenceDiagnosticsPanel.vue'
import { DockerWorkspaceRuntime, api } from '../../shared/workspace/runtime'
import WorkbenchComposer from '../../modes/chat/components/WorkbenchComposer.vue'

const props = defineProps<{ workspace?: DockerWorkspaceRuntime }>()
const live = !!props.workspace

interface ViewportHandle {
  capturePendingLayoutIntent(): void
  captureSnapshot(): SessionViewMemory
  jumpToMessage(index?: number): Promise<void>
  jumpToLatest(): Promise<void>
  shiftBackward(): Promise<void>
  shiftForward(): Promise<void>
}

type ReferenceNavigationTarget =
  | 'restart-agent'
  | 'agent-plan'
  | 'agent-delegation'
  | 'agent-terminal'
  | 'agent-final'
  | 'office-briefing'
  | 'office-followup'
  | 'lifecycle-clarify'
  | 'lifecycle-fallback'
  | 'lifecycle-steer'

type DirectReferenceTarget =
  | 'office-briefing'
  | 'office-followup'
  | 'lifecycle-clarify'
  | 'lifecycle-fallback'
  | 'lifecycle-steer'

const AGENT_REFERENCE_MESSAGE = {
  'agent-plan': 83_999,
  'agent-delegation': 84_005,
  'agent-terminal': 84_007,
  'agent-final': 84_008,
} as const

const DIRECT_REFERENCE_SESSION: Record<DirectReferenceTarget, string> = {
  'office-briefing': 'office-briefing',
  'office-followup': 'office-followup',
  'lifecycle-clarify': 'android-protocol',
  'lifecycle-fallback': 'resilience-fallback',
  'lifecycle-steer': 'steered-migration',
}

function isDirectReferenceTarget(target: ReferenceNavigationTarget): target is DirectReferenceTarget {
  return target === 'office-briefing'
    || target === 'office-followup'
    || target === 'lifecycle-clarify'
    || target === 'lifecycle-fallback'
    || target === 'lifecycle-steer'
}

function isAgentJumpTarget(target: ReferenceNavigationTarget): target is keyof typeof AGENT_REFERENCE_MESSAGE {
  return target === 'agent-plan'
    || target === 'agent-delegation'
    || target === 'agent-terminal'
    || target === 'agent-final'
}

const { workspace, activeSession, activeUiState, workspaceRevision } = useWorkspaceRuntime(props.workspace)
const liveWorkspace = props.workspace
const newTaskOpen = ref(false)
const taskTitle = ref('')
const taskDirectory = ref('')
const taskModel = ref('')
const taskApprovalMode = ref<'manual' | 'auto'>('manual')
const sessionSearch = ref('')
const connection = computed(() => { void workspaceRevision.value; return liveWorkspace?.runtimeInfo.connection })
const creating = ref(false)
const filesOpen = ref(false)
const preview = ref<{ taskId: string; path: string; content: string } | null>(null)
const localError = ref('')
const taskInfo = computed(() => { void workspaceRevision.value; return liveWorkspace?.tasks.get(activeSession.value.id) })
const archivedTasks = computed(() => { void workspaceRevision.value; return [...(liveWorkspace?.tasks.values() || [])].filter(task => task.archived && !task.parentTaskId) })
const runtimeError = computed(() => { void workspaceRevision.value; return liveWorkspace?.error || localError.value })
const uploadInput = ref<HTMLInputElement | null>(null)
const sessionMenu = ref<HTMLDetailsElement | null>(null)
const viewportRef = ref<ViewportHandle | null>(null)
// Keep the public workspace clean by default, but diagnostics are always one click away.
// Playwright opens the panel automatically because the E2E suite consumes its observability surface.
const diagnosticsOpen = ref(live ? new URLSearchParams(window.location.search).has('inspect') : typeof navigator !== 'undefined' && navigator.webdriver)
const mountedRows = ref(0)
const mobileSessionsOpen = ref(false)
const fixtureOrdinal = ref(1)
const { fps, frameP95, longTasks, heapMb } = usePerformanceMetrics(() => diagnosticsOpen.value)

const activeExecution = computed(() => workspace.executionFor(activeSession.value.id))
const activeIndicator = computed(() => { void activeUiState.value.eventRevision; return deriveSessionIndicator(activeSession.value.kernel.summary) })
const sessionDescriptors = computed(() => { void workspaceRevision.value; return workspace.descriptors.filter(task => !sessionSearch.value || task.title.toLowerCase().includes(sessionSearch.value.toLowerCase())) })
const hotSessionCount = computed(() => { void workspaceRevision.value; return workspace.hotSessionCount })
const runningSessionCount = computed(() => { void workspaceRevision.value; return workspace.runningSessionCount })
const blockedSessionCount = computed(() => { void workspaceRevision.value; return workspace.blockedSessionCount })
const failedSessionCount = computed(() => { void workspaceRevision.value; return workspace.failedSessionCount })
const canInjectFixtures = computed(() => !live && activeUiState.value.sessionStatus !== 'working' && !activeUiState.value.pendingInteraction)
const activePlan = computed(() => { void activeUiState.value.eventRevision; return activeSession.value.kernel.activePlan })
const activeParentSessionId = computed(() => workspace.parentSessionId(activeSession.value.id))
const activeParentTitle = computed(() => {
  const id = activeParentSessionId.value
  return id ? workspace.kernelFor(id).title : ''
})

async function switchSession(id: string): Promise<void> {
  if (sessionMenu.value) sessionMenu.value.open = false
  if (id !== activeSession.value.id) {
    if (liveWorkspace) await liveWorkspace.prepareSession(id)
    const snapshot = viewportRef.value?.captureSnapshot()
    workspace.activate(id, snapshot)
    mountedRows.value = 0
  }
  mobileSessionsOpen.value = false
}

async function newSession(): Promise<void> {
  if (live) { mobileSessionsOpen.value = false; taskModel.value = connection.value?.defaultModelId || ''; newTaskOpen.value = true; return }
  const snapshot = viewportRef.value?.captureSnapshot()
  if (snapshot) workspace.saveSnapshot(activeSession.value.id, snapshot)
  const id = await workspace.createSession()
  workspace.activate(id)
  mountedRows.value = 0
  mobileSessionsOpen.value = false
}

function jump(target: number): void { void viewportRef.value?.jumpToMessage(target) }
function indicator(descriptor: ReferenceSessionDescriptor): SessionIndicator { return deriveSessionIndicator(descriptor) }
function indicatorLabel(state: SessionIndicator): string {
  if (!live) return sessionIndicatorLabel(state)
  return ({ idle: '空闲', working: '执行中', blocked: '等待处理', failed: '失败', interrupted: '已停止', 'max-tokens': '已达输出上限', completed: '已完成' } as Record<string, string>)[state] || sessionIndicatorLabel(state)
}
function indicatorDetail(descriptor: ReferenceSessionDescriptor): string {
  const state = indicator(descriptor)
  if (state === 'blocked') return descriptor.pendingInteraction?.kind === 'question' ? 'Needs answer' : 'Needs approval'
  if (state === 'failed') return descriptor.lastFailure?.code ?? 'Failed'
  if (descriptor.queuedPrompts) return `${descriptor.queuedPrompts} queued`
  return `${descriptor.logicalCount.toLocaleString()} messages`
}

async function openChildSession(childSessionId: string): Promise<void> {
  if (!workspace.hasSession(childSessionId)) return
  await switchSession(childSessionId)
  await nextTick()
  await viewportRef.value?.jumpToLatest()
}

function onConversationClick(event: MouseEvent): void {
  const target = event.target instanceof Element ? event.target.closest<HTMLElement>('[data-child-session-id]') : null
  const childSessionId = target?.dataset.childSessionId
  if (!childSessionId || childSessionId === activeSession.value.id) return
  void openChildSession(childSessionId)
}

async function returnToParent(): Promise<void> {
  const parent = activeParentSessionId.value
  if (!parent) return
  // switchSession captures the child snapshot and ReferenceWorkspaceRuntime restores the
  // parent's previously committed viewport snapshot. Do not override that restoration
  // with a jump-to-latest: returning from a child should land back on the delegation
  // context that opened it.
  await switchSession(parent)
  await nextTick()
}

async function navigateReference(target: ReferenceNavigationTarget): Promise<void> {
  if (target === 'restart-agent') {
    // Full reload is intentionally Reference-only: it recreates the synthetic producer and
    // all seeded sessions without pretending the Engine owns replay/reset policy.
    window.location.reload()
    return
  }

  if (isDirectReferenceTarget(target)) {
    await switchSession(DIRECT_REFERENCE_SESSION[target])
    await nextTick()
    await viewportRef.value?.jumpToLatest()
    return
  }

  if (!isAgentJumpTarget(target)) return
  await switchSession('agent-loop')
  await nextTick()
  const messageIndex = AGENT_REFERENCE_MESSAGE[target]
  if (activeSession.value.logicalCount > messageIndex) await viewportRef.value?.jumpToMessage(messageIndex)
  else await viewportRef.value?.jumpToLatest()
}

// Verification fixtures stay in Reference ownership. They exercise the normal canonical
// model/projector/renderer path and are exposed only inside Session diagnostics.
async function injectMixed(turns: number): Promise<void> {
  if (!canInjectFixtures.value) return
  const start = fixtureOrdinal.value
  activeSession.value.kernel.appendCanonicalMessages(createMixedReferenceTurns(activeSession.value.id, start, turns))
  fixtureOrdinal.value += turns
  await Promise.resolve()
  await viewportRef.value?.jumpToLatest()
}

async function injectMarkdownGallery(): Promise<void> {
  if (!canInjectFixtures.value) return
  activeSession.value.kernel.appendCanonicalMessages(createMarkdownGalleryTurn(activeSession.value.id, fixtureOrdinal.value++))
  await Promise.resolve()
  await viewportRef.value?.jumpToLatest()
}

async function injectAgentScenarios(): Promise<void> {
  if (!canInjectFixtures.value) return
  activeSession.value.kernel.appendCanonicalMessages(createAgentScenarioPack(activeSession.value.id, fixtureOrdinal.value++))
  await Promise.resolve()
  await viewportRef.value?.jumpToLatest()
}

async function createTask(): Promise<void> {
  if (!liveWorkspace || creating.value) return
  creating.value = true; localError.value = ''
  try { const id = await liveWorkspace.createSession({ title: taskTitle.value || '新对话', workspace: taskDirectory.value || undefined, modelId: taskModel.value || undefined, approvalMode: taskApprovalMode.value }); await switchSession(id); newTaskOpen.value = false; taskTitle.value = ''; taskDirectory.value = '' } catch (error) { localError.value = (error as Error).message } finally { creating.value = false }
}
async function connectionChanged(): Promise<void> { await liveWorkspace?.refreshConnection(); localError.value = '' }
async function prepareHistory(start: number, count: number): Promise<void> { await liveWorkspace?.prepareHistory(activeSession.value.id, start, count) }
async function uploadFiles(event: Event): Promise<void> {
  const files = (event.target as HTMLInputElement).files
  if (!files) return
  const target = activeSession.value
  for (const file of files) {
    try {
      if (file.size > 16 * 1024 * 1024) throw new Error('单个上传文件最多 16 MiB')
      const data = await new Promise<string>((resolve, reject) => { const reader = new FileReader(); reader.onload = () => resolve(String(reader.result).split(',')[1]!); reader.onerror = reject; reader.readAsDataURL(file) })
      const result = await api<{ path: string }>(`/api/tasks/${target.id}/upload`, { name: file.name, base64: data })
      target.setDraftText(`${target.draftText}\n文件：${result.path}`.trim())
    } catch (error) { localError.value = (error as Error).message }
  }
  if (uploadInput.value) uploadInput.value.value = ''
}
async function openFile(path: string): Promise<void> {
  const taskId = activeSession.value.id
  try {
    const url = `/api/tasks/${taskId}/files?path=${encodeURIComponent(path)}`
    if (/\.(png|jpe?g|gif|webp|mp[34]|wav|ogg|webm|pdf|docx|xlsx|pptx)$/i.test(path)) { window.open(url, '_blank', 'noopener'); return }
    const response = await fetch(url); if (!response.ok) throw new Error(await response.text()); preview.value = { taskId, path, content: await response.text() }
  } catch (error) { localError.value = (error as Error).message }
}
async function saveFile(): Promise<void> {
  if (!preview.value) return
  try { await api(`/api/tasks/${preview.value.taskId}/save-file`, preview.value); preview.value = null } catch (error) { localError.value = (error as Error).message }
}
async function archiveTask(): Promise<void> { if (!liveWorkspace) return; try { await switchSession(await liveWorkspace.archiveSession(activeSession.value.id)) } catch (error) { localError.value = (error as Error).message } }
</script>

<template>
  <section class="agent-app lab-workspace" :class="{ 'live-workspace': live, 'diagnostics-closed': !diagnosticsOpen, 'mobile-sessions-open': mobileSessionsOpen }">
    <button class="mobile-session-toggle" data-testid="mobile-session-toggle" type="button" aria-label="Open conversations" @click="mobileSessionsOpen = true">☰</button>
    <button v-if="mobileSessionsOpen" class="mobile-session-backdrop" type="button" aria-label="Close conversations" @click="mobileSessionsOpen = false" />

    <aside class="session-sidebar" data-testid="session-sidebar">
      <div class="sidebar-head">
        <div>
          <div class="product-name">{{ live ? 'Tuck' : 'Agent Workspace' }}</div>
          <small v-if="!live" class="product-kicker">Conversation engine reference</small>
        </div>
        <div class="sidebar-head-actions">
          <a v-if="!live" class="architecture-link" href="#/architecture" data-testid="architecture-link" title="Engine architecture">⌘</a>
          <button class="mobile-session-close" type="button" aria-label="Close conversations" @click="mobileSessionsOpen = false">×</button>
        </div>
      </div>

      <div class="primary-actions">
        <button class="new-session" data-testid="new-session" @click="newSession">{{ live ? '＋ 新对话' : '＋ New session' }}</button>
      </div>

      <div class="session-section-label">{{ live ? '对话' : 'Recent' }}</div>
      <input v-if="live" v-model="sessionSearch" class="session-search" aria-label="搜索对话" placeholder="搜索对话…" />
      <div class="session-list" data-testid="recent-sessions">
        <button
          v-for="descriptor in sessionDescriptors"
          :key="descriptor.id"
          class="session-row"
          :class="[`indicator-${indicator(descriptor)}`, { active: descriptor.id === activeSession.id, unread: descriptor.unread }]"
          :data-testid="`session-${descriptor.id}`"
          @click="switchSession(descriptor.id)"
        >
          <span class="session-glyph" :class="`indicator-${indicator(descriptor)}`">{{ sessionIndicatorGlyph(indicator(descriptor)) }}</span>
          <span class="session-copy">
            <strong>{{ descriptor.title }}</strong>
            <small><span>{{ indicatorLabel(indicator(descriptor)) }}</span><span v-if="!live"> · {{ indicatorDetail(descriptor) }}</span><span v-else-if="descriptor.queuedPrompts"> · {{ descriptor.queuedPrompts }} 条排队</span></small>
          </span>
          <span class="session-row-meta"><i v-if="descriptor.unread" class="unread-dot" /><time>{{ descriptor.age }}</time></span>
        </button>
      </div>
      <details v-if="archivedTasks.length" class="tuck-runtime-info"><summary>已归档 {{ archivedTasks.length }}</summary><button v-for="task in archivedTasks" :key="task.id" @click="liveWorkspace?.command(task.id, 'archive', { archived: false })">恢复 {{ task.title }}</button></details>
      <div v-if="liveWorkspace" class="tuck-sidebar-settings"><RouterLink to="/settings/platform">平台设置</RouterLink></div>

      <div v-if="liveWorkspace" class="sidebar-footer"><span class="status-led" :class="{ disconnected: !liveWorkspace.connected }" /><span>{{ liveWorkspace.connected ? '已连接' : '正在重连…' }}</span><span v-if="runningSessionCount">· {{ runningSessionCount }} 个任务执行中</span></div>
      <div v-else class="sidebar-footer">
        <span class="status-led" />
        <span>{{ runningSessionCount }} working</span>
        <span v-if="blockedSessionCount">· {{ blockedSessionCount }} waiting</span>
        <span v-if="failedSessionCount">· {{ failedSessionCount }} failed</span>
      </div>
    </aside>

    <ConversationViewport
      :key="activeSession.id"
      ref="viewportRef"
      :runtime="activeSession"
      :execution="activeExecution"
      :ui-state="activeUiState"
      :prepare-history="live ? prepareHistory : undefined"
      :placeholder="live ? activeUiState.pendingInteraction ? '请先处理上方请求' : activeUiState.sessionStatus === 'working' ? '补充消息，发送后排队执行…' : '输入消息…' : undefined"
      :read-only="!!taskInfo?.parentTaskId"
      @click.capture="onConversationClick"
      @viewport-metrics="mountedRows = $event.mountedRows"
    >
      <template v-if="live" #empty-state><div class="empty-agent-mark">✦</div><h2>开始新对话</h2></template>
      <template v-if="liveWorkspace && taskInfo" #composer>
        <WorkbenchComposer :key="taskInfo.id" :task="taskInfo" :runtime="activeSession" :workspace="liveWorkspace" :connection="connection" :connected="liveWorkspace.connected" :error="runtimeError" @upload="uploadInput?.click()" @sent="viewportRef?.jumpToLatest()" @layout="viewportRef?.capturePendingLayoutIntent()" />
        <input ref="uploadInput" type="file" multiple hidden @change="uploadFiles" />
      </template>
      <template v-if="live" #status><span class="run-status" :class="`indicator-${activeIndicator}`"><i />{{ indicatorLabel(activeIndicator) }}</span></template>
      <template #header-context>
        <button
          v-if="activeParentSessionId"
          class="reference-parent-session"
          data-testid="parent-session-link"
          type="button"
          :title="`Return to ${activeParentTitle}`"
          @click.stop="returnToParent"
        >← Parent · {{ activeParentTitle }}</button>
      </template>
      <template #header-actions>
        <button v-if="live" class="reference-header-action" type="button" data-testid="task-files" @click="filesOpen = !filesOpen">文件</button>
        <details v-if="live" ref="sessionMenu" class="tuck-session-menu" @keydown.esc="sessionMenu && (sessionMenu.open = false)">
          <summary data-testid="session-menu" aria-label="更多操作" title="更多操作">⋯</summary>
          <div class="tuck-session-menu-items">
            <button v-if="!taskInfo?.parentTaskId && activeUiState.sessionStatus === 'idle'" type="button" data-testid="task-archive" @click="archiveTask">归档对话</button>
            <button v-if="taskInfo?.queue.length" type="button" @click="liveWorkspace?.command(activeSession.id, 'clear-queue', {}); sessionMenu && (sessionMenu.open = false)">清空队列</button>
            <button data-testid="diagnostics-open" type="button" :aria-pressed="diagnosticsOpen" @click="diagnosticsOpen = !diagnosticsOpen; sessionMenu && (sessionMenu.open = false)">运行详情</button>
          </div>
        </details>
        <button v-else class="reference-header-action" data-testid="diagnostics-open" type="button" :aria-pressed="diagnosticsOpen" title="Session diagnostics" aria-label="Session diagnostics" @click="diagnosticsOpen = !diagnosticsOpen">◫</button>
      </template>
      <template #viewport-overlay="{ mountedRows: visibleRows, followLabel, uiState }">
        <div v-show="diagnosticsOpen" class="conversation-meta-strip">
          <span>Loaded <strong data-testid="segment-range">{{ uiState.rangeStart.toLocaleString() }} – {{ Math.max(uiState.rangeStart, uiState.rangeEnd - 1).toLocaleString() }}</strong></span>
          <span>Reader <strong data-testid="reader-position">#{{ uiState.reader.toLocaleString() }}</strong></span>
          <span data-testid="mounted-label">{{ visibleRows }} DOM rows</span><span v-if="uiState.activeMessageId" data-testid="follow-state">{{ followLabel }}</span>
        </div>
      </template>
      <template #composer-tools><ActivePlanStrip :plan="activePlan" /></template>
    </ConversationViewport>

    <ReferenceDiagnosticsPanel
      v-if="diagnosticsOpen"
      :runtime="activeSession"
      :ui-state="activeUiState"
      :stream="activeExecution"
      :mounted-rows="mountedRows"
      :hot-session-count="hotSessionCount"
      :running-session-count="runningSessionCount"
      :blocked-session-count="blockedSessionCount"
      :failed-session-count="failedSessionCount"
      :fps="fps"
      :frame-p95="frameP95"
      :long-tasks="longTasks"
      :heap-mb="heapMb"
      :can-inject-fixtures="canInjectFixtures"
      :live="live"
      @close="diagnosticsOpen = false"
      @jump="jump"
      @shift-backward="viewportRef?.shiftBackward()"
      @shift-forward="viewportRef?.shiftForward()"
      @inject-mixed="injectMixed"
      @inject-markdown="injectMarkdownGallery"
      @inject-agent="injectAgentScenarios"
      @reference-navigate="navigateReference"
    />
    <aside v-if="filesOpen && taskInfo" class="tuck-files-panel"><header><strong>任务文件</strong><button aria-label="关闭文件" @click="filesOpen = false">×</button></header><p class="tuck-file-directory">{{ taskInfo.workspace }}</p><p v-if="!taskInfo.artifacts.length">暂无文件</p><button v-for="file in taskInfo.artifacts" :key="file.path" class="tuck-file-row" @click="openFile(file.path)"><span>{{ file.path }}</span><small>{{ file.size.toLocaleString() }} B</small></button></aside>
    <div v-if="newTaskOpen" class="tuck-modal-backdrop"><form class="tuck-modal" @submit.prevent="createTask"><h2>新对话</h2><label>名称<input v-model="taskTitle" placeholder="新对话" /></label><label v-if="connection?.models.length">模型<select v-model="taskModel" data-testid="new-task-model"><option value="" disabled>请选择平台模型</option><option v-for="model in connection.models" :key="model.modelId" :value="model.modelId" :disabled="!model.selectable">{{ model.displayName }}{{ model.reason ? ' · ' + model.reason : '' }}</option></select></label><label>审批模式<select v-model="taskApprovalMode" aria-label="新对话审批模式"><option value="manual">手动审批</option><option value="auto">自动审批</option></select><small>手动：安全读取自动执行，写入和命令等操作请求审批。自动：允许工具自动执行。</small></label><details class="tuck-directory-options"><summary>工作目录（可选）</summary><label>目录<input v-model="taskDirectory" placeholder="留空则创建独立工作目录" /></label><small>{{ liveWorkspace?.runtimeInfo.workspaceRoot }}</small></details><p v-if="localError" class="tuck-error">{{ localError }}</p><div><button type="button" @click="newTaskOpen = false">取消</button><button :disabled="creating || !!connection?.models.length && !taskModel" type="submit">{{ creating ? '创建中…' : '创建' }}</button></div></form></div>
    <div v-if="preview" class="tuck-modal-backdrop"><section class="tuck-modal tuck-editor"><header><strong>{{ preview.path }}</strong><button @click="preview = null">×</button></header><textarea v-model="preview.content" /><div><button @click="preview = null">关闭</button><button @click="saveFile">保存文件</button></div></section></div>
  </section>
</template>

<style scoped>
.live-workspace { --session-sidebar-width: 240px; }
.session-search { margin:0 12px 10px; min-width:0; padding:9px 10px; color:#dce6ef; border:1px solid #2b3a47; background:#101820; border-radius:8px; font:inherit; font-size:12px; }
.live-workspace .session-row { min-height:62px; }
.live-workspace .session-row time { font-size:10px; }
.live-workspace .product-name { font-size: 17px; }
.live-workspace .new-session { font-size: 12px; padding: 10px 12px; }
.live-workspace .session-copy strong { font-size: 12px; }
.live-workspace .session-copy small, .live-workspace .sidebar-footer { font-size: 10px; }
.live-workspace .reference-header-action { width: auto; min-width: 34px; padding: 6px 10px; flex-shrink: 0; white-space: nowrap; }
.live-workspace :deep(.conversation-shell) { position: relative; z-index: 0; }
.live-workspace :deep(.conversation-header) { position: relative; z-index: 30; gap: 12px; }
.live-workspace :deep(.conversation-heading) { flex: 1; overflow: hidden; }
.live-workspace :deep(.conversation-title) { flex: 1; }
.live-workspace :deep(.conversation-header-actions) { flex-shrink: 0; }
.agent-app.live-workspace :deep(.session-stats-line), .agent-app.live-workspace :deep(.composer-hint), .agent-app.live-workspace :deep(.context-meter), .agent-app.live-workspace :deep(.header-stop) { display: none; }
.live-workspace :deep(.composer-shell) { padding: 12px 20px 12px; max-height:72dvh; overflow:auto; }
.live-workspace :deep(.conversation-title strong) { font-size:15px; }
.live-workspace :deep(.composer-box textarea) { font-size: 14px; }
.live-workspace :deep(.pending-copy p) { max-height: 110px; overflow: auto; overflow-wrap: anywhere; }
.live-workspace :deep(.turn-outcome-banner) { max-height: 100px; overflow: auto; overflow-wrap: anywhere; }
.live-workspace :deep(.composer-extension) { flex: 1; }
.live-workspace .tuck-composer-tools { gap: 8px; min-width: 0; }
.live-workspace .tuck-composer-tools > button { white-space: nowrap; flex-shrink: 0; }
.live-workspace .tuck-composer-tools > button:first-child { font-size: 20px; width: 30px; height: 30px; padding: 0; }
.live-workspace .tuck-composer-tools > button:disabled { opacity: .4; cursor: default; }
.live-workspace .tuck-task-model { min-width: 0; max-width: 180px; width: auto; border: 0; font-size: 11px; }
.tuck-sidebar-settings { padding: 8px 12px; }
.status-led.disconnected { background: #e9bd5b; box-shadow: none; }
.tuck-session-menu { position: relative; flex-shrink: 0; }
.tuck-session-menu > summary { display: grid; place-items: center; width: 32px; height: 32px; border-radius: 7px; cursor: pointer; list-style: none; font-size: 20px; color: #aab9c7; }
.tuck-session-menu > summary::-webkit-details-marker { display: none; }
.tuck-session-menu > summary:hover, .tuck-session-menu[open] > summary { background: #17222d; }
.tuck-session-menu-items { position: absolute; top: 38px; right: 0; width: 150px; display: grid; gap: 4px; padding: 6px; border: 1px solid #30404f; border-radius: 9px; background: #131c25; box-shadow: 0 8px 25px #0006; }
.tuck-session-menu-items > button { width: 100%; border: 0; text-align: left; white-space: nowrap; padding: 9px 12px; }
.tuck-file-directory { font-size: 11px; overflow-wrap: anywhere; color: #8a9aaa; }
.tuck-directory-options { font-size: 12px; }
.tuck-directory-options summary { cursor: pointer; }
.tuck-directory-options label { margin: 12px 0 8px; }
.tuck-directory-options small { display: block; }
.live-workspace .tuck-runtime-info { display: block; max-height: 22dvh; overflow: auto; padding: 8px 16px; }
.tuck-runtime-info summary { cursor: pointer; }
.tuck-runtime-info > button { display: block; max-width: 100%; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; margin-top: 6px; }
.live-workspace .tuck-files-panel { background: #10171f; color: #e3eaf1; }
.live-workspace .tuck-files-panel button { background: #17222d; color: inherit; border: 1px solid #30404f; border-radius: 6px; padding: 6px 10px; cursor: pointer; }
.live-workspace .tuck-modal { max-height: calc(100dvh - 40px); overflow: auto; box-sizing: border-box; background: #10171f; color: #e3eaf1; border: 1px solid #30404f; }
.live-workspace .tuck-modal input, .live-workspace .tuck-modal select, .live-workspace .tuck-modal textarea { background: #0d141c; color: inherit; border: 1px solid #30404f; }
.live-workspace .tuck-modal button { background: #17222d; color: inherit; border: 1px solid #30404f; border-radius: 7px; }
.live-workspace .tuck-modal button[type="submit"] { background: #79c0ff; color: #0b1722; border-color: #79c0ff; }
@media (max-width: 760px) {
  .live-workspace :deep(.conversation-shell) { --conversation-header-leading-space: 52px; }
  .live-workspace :deep(.conversation-header) { padding-right: 10px; gap: 8px; }
  .live-workspace :deep(.conversation-header-actions) { gap: 4px; }
  .live-workspace :deep(.composer-shell) { padding: 10px; }
  .live-workspace .tuck-task-model { max-width: 145px; }
}
.tuck-task-model { display: flex; align-items: center; gap: 6px; font-size: 12px; }
.tuck-task-model select, .tuck-modal select { padding: 6px 8px; border: 1px solid #aaa6; border-radius: 6px; background: #fafbfc; color: #252832; max-width: 240px; }
.tuck-runtime-info { display: flex; flex-wrap: wrap; gap: 8px; padding: 12px 16px; font-size: 12px; } .tuck-runtime-info small { opacity: .6; } .tuck-runtime-info a { width: 100%; color: #8498d7; } .tuck-workspace-path { max-width: 36vw; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; opacity: .6; } .tuck-error { color: #c64f59; white-space: pre-wrap; font-size: 13px; } .tuck-composer-tools { display: flex; gap: 12px; align-items: center; font-size: 12px; padding: 4px 0; } .tuck-composer-tools button { background: none; border: 0; color: inherit; cursor: pointer; } .tuck-files-panel { position: fixed; right: 12px; top: 12px; bottom: 12px; width: min(380px, 90vw); background: var(--color-surface, #f7f8fb); color: var(--color-text, #252832); padding: 20px; z-index: 80; box-shadow: 0 4px 30px #0003; overflow: auto; border-radius: 12px; } .tuck-files-panel header, .tuck-editor header { display: flex; justify-content: space-between; margin-bottom: 16px; } .tuck-file-row { display: flex; text-align: left; justify-content: space-between; width: 100%; margin: 8px 0; padding: 10px; border: 1px solid #8883; border-radius: 6px; } .tuck-modal-backdrop { position: fixed; inset: 0; z-index: 100; background: #0007; display: grid; place-items: center; padding: 20px; } .tuck-modal { background: #fafbfc; color: #252832; padding: 24px; border-radius: 12px; width: min(560px, 100%); display: flex; flex-direction: column; gap: 16px; } .tuck-modal h2 { margin: 0; } .tuck-modal label { display: flex; flex-direction: column; gap: 8px; } .tuck-modal input { width: 100%; padding: 10px; border: 1px solid #aaa6; border-radius: 6px; box-sizing: border-box; } .tuck-modal button { padding: 8px 16px; cursor: pointer; } .tuck-modal > div { display: flex; justify-content: flex-end; gap: 8px; } .tuck-modal small { overflow-wrap: anywhere; opacity: .7; } .tuck-editor { width: min(1000px, 95vw); } .tuck-editor textarea { min-height: 55vh; font-family: monospace; resize: vertical; }
</style>