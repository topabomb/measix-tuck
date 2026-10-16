<script setup lang="ts">
import { taskName } from "../../../shared/task-channel.mjs"
import { computed, ref, toRef, watch, nextTick, onMounted, onBeforeUnmount } from 'vue'
import TuckLoading from '../../shared/ui/TuckLoading.vue'
import { usePerformanceMetrics } from '../../shared/use-performance-metrics'
import type { SessionViewMemory } from '../../engine/viewport/state'
import { deriveSessionIndicator } from '../../engine/conversation/session-semantics'
import { useWorkspaceRuntime } from './use-workspace-runtime'
import { ConversationViewport } from '../../engine/vue'
import DiagnosticsPanel from './components/WorkbenchDiagnostics.vue'
import { DockerWorkspaceRuntime, defaultAssistants } from '../../shared/workspace/runtime'
import WorkbenchComposer from './components/WorkbenchComposer.vue'
import SessionNavigation from './components/SessionNavigation.vue'
import NewConversationDialog from './components/NewConversationDialog.vue'
import ForkConversationDialog from './components/ForkConversationDialog.vue'
import RenameConversationDialog from './components/RenameConversationDialog.vue'
import { useLightDismiss } from '../../shared/use-light-dismiss'
import TaskFiles from './components/TaskFiles.vue'
import EnterpriseWorkspace from '../../shared/enterprise-workspace/EnterpriseWorkspace.vue'
import { indicatorLabel, taskStatus } from './session-presentation'

import SessionList from './components/SessionList.vue'
import SidebarIcon from '../../shared/ui/SidebarIcon.vue'
import AgentActivity from './components/AgentActivity.vue'
import ActivityStatus from '../../shared/ui/ActivityStatus.vue'
import AssistantIcon from '../../shared/ui/AssistantIcon.vue'
import WorkbenchAttachments from './components/WorkbenchAttachments.vue'
import WorkbenchProgress from './components/WorkbenchProgress.vue'
import ChatMarkdown from './components/ChatMarkdown.vue'
import { createDefaultRendererRegistry } from '../../engine/vue/renderers/registry'
const renderers = createDefaultRendererRegistry().register('markdown',ChatMarkdown).register('tool', AgentActivity).register('thinking', AgentActivity).register('terminal', AgentActivity).register('attachments', WorkbenchAttachments).register('plan', WorkbenchProgress).register('delegation', WorkbenchProgress)
import type { ChatViewState } from './ChatMode.vue'
import { router, conversationPath, linkUrl } from '../../app/router'
import { copyText } from '../../shared/copy-text'
import DeliverySummary from './components/DeliverySummary.vue'
import AssistantSummary from './components/AssistantSummary.vue'
import RuncoveMark from '../../shared/ui/RuncoveMark.vue'
import { useRoute } from 'vue-router'
const props = defineProps<{ workspace: DockerWorkspaceRuntime; viewState: ChatViewState; pending?: boolean }>()
const route = useRoute()
const linkedMessage = computed(() => typeof route.params.message === 'string' ? Number(route.params.message) : undefined)
const copyNotice = ref('')
let copyTimer: ReturnType<typeof setTimeout> | undefined

interface ViewportHandle {
  capturePendingLayoutIntent(): void
  captureNavigationIntent(): number
  jumpToLatestIfUnchanged(revision: number): Promise<void>
  captureSnapshot(): SessionViewMemory
  jumpToMessage(index?: number): Promise<void>
  jumpToLatest(): Promise<void>
  shiftBackward(): Promise<void>
  shiftForward(): Promise<void>
}

const { workspace, activeSession, activeUiState, workspaceRevision } = useWorkspaceRuntime(props.workspace)
const liveWorkspace = props.workspace
const forkTaskId = ref<string | null>(null)
const newTaskOpen = ref(false)
const renameOpen = ref(false)
const deliverySummary=ref<InstanceType<typeof DeliverySummary>|null>(null)
const navigationTasks = computed<readonly import('../../shared/workspace/runtime').DockerTask[]>((previous) => {
  void workspaceRevision.value
  const tasks = [...props.workspace.tasks.values()]
  return previous?.length === tasks.length && tasks.every((task,index) => previous[index] === task) ? previous : tasks
})
const sessionSearch = toRef(props.viewState, 'search')
const assistantFilter = toRef(props.viewState, 'assistant')
const assistants = computed(() => { void workspaceRevision.value; return liveWorkspace.runtimeInfo.assistants || defaultAssistants })
const assistantOptions = computed(() => {
  const items = [...assistants.value.items]
  for (const task of navigationTasks.value) if (!task.parentTaskId && !items.some(item => item.agent === task.agent)) items.push({ agent: task.agent, displayName: task.assistantName || task.agent })
  return items
})
const assistantNames = computed(() => Object.fromEntries(assistantOptions.value.map(item => [item.agent,item.displayName])))
const connection = computed(() => { void workspaceRevision.value; return liveWorkspace?.runtimeInfo.connection })
const filesOpen = ref(false), filesTrigger = ref<HTMLElement|null>(null)
const localError = ref('')
const taskInfo = computed(() => { void workspaceRevision.value; return liveWorkspace?.tasks.get(activeSession.value.id) })
const deliveryToolEnabled=computed(()=>{void workspaceRevision.value;const info=liveWorkspace.runtimeInfo;const available=info.deliveryToolAvailable===true;return !taskInfo.value?.parentTaskId && available && assistants.value.items.some(a=>a.agent===taskInfo.value?.agent&&a.deliveryTool!==false)})
const showDelivery=computed(()=>!!taskInfo.value&&!taskInfo.value.parentTaskId&&(deliveryToolEnabled.value||!!taskInfo.value.latestDelivery))
const deliveryRequirement=computed(()=>{void workspaceRevision.value;return liveWorkspace.runtimeInfo.orchestration?.requireDelivery})
const messageAssistantName = computed(() => assistantNames.value[taskInfo.value?.agent || ''] || taskInfo.value?.assistantName || taskInfo.value?.agent || '助手')
const artifactTask = computed(() => {
  let task = taskInfo.value
  const seen = new Set<string>()
  while (task?.parentTaskId && !seen.has(task.id)) { seen.add(task.id); const parent = liveWorkspace.tasks.get(task.parentTaskId); if (!parent) break; task = parent }
  return task
})
const activeAssistant = computed(() => assistants.value.items.find(item => item.agent === artifactTask.value?.agent))
const archivedTasks = computed(() => navigationTasks.value.filter(task => task.archived && !task.parentTaskId && (!assistantFilter.value || task.agent === assistantFilter.value)))
const archivesOpen = toRef(props.viewState, 'archivesOpen')
const restoringArchive = ref<string | null>(null)
async function restoreArchive(id: string): Promise<void> {
  if (restoringArchive.value) return
  restoringArchive.value = id
  try { await liveWorkspace.command(id, 'archive', { archived: false }) }
  finally { restoringArchive.value = null }
}
const runtimeError = computed(() => { void workspaceRevision.value; return liveWorkspace?.error || localError.value })
const sessionMenu = ref<HTMLDetailsElement | null>(null)
useLightDismiss(() => sessionMenu.value, () => !!sessionMenu.value?.open, () => { if (sessionMenu.value) sessionMenu.value.open = false })
const viewportRef = ref<ViewportHandle | null>(null)
// Keep the public workspace clean by default, but diagnostics are always one click away.
// The optional inspect URL opens this observability surface for local diagnostics.
const diagnosticsOpen = toRef(props.viewState, 'diagnosticsOpen')
const enterpriseOpen = computed(()=>route.query.ew === '1')
const enterpriseWidth = ref(420)
try {enterpriseWidth.value=Math.max(340,Math.min(900,Number(localStorage.getItem('tuck-enterprise-width'))||420))}catch{/* optional preference */}
function resizeEnterprise(width:number) {enterpriseWidth.value=Math.max(340,Math.min(900,width));try{localStorage.setItem('tuck-enterprise-width',String(enterpriseWidth.value))}catch{/* optional preference */}}
const sidebarWidth=ref(220)
try{sidebarWidth.value=Math.max(220,Math.min(400,Number(localStorage.getItem('tuck.sidebar.width'))||220))}catch{/* optional preference */}
const layoutWidth=ref(0)
const sidebarReservedWidth=computed(()=>enterpriseOpen.value&&route.query.wide!=='1'?340:diagnosticsOpen.value?318:0)
const actualSidebarWidth=computed(()=>layoutWidth.value?Math.min(sidebarWidth.value,Math.max(220,layoutWidth.value-480-sidebarReservedWidth.value)):sidebarWidth.value)
const maximumEnterpriseWidth=computed(()=>layoutWidth.value?Math.min(900,Math.max(340,layoutWidth.value-(sidebarCollapsed.value?0:actualSidebarWidth.value)-480)):900)
const actualEnterpriseWidth=computed(()=>Math.min(enterpriseWidth.value,maximumEnterpriseWidth.value))
function resizeSidebar(width:number){sidebarWidth.value=Math.max(220,Math.min(400,width));try{localStorage.setItem('tuck.sidebar.width',String(sidebarWidth.value))}catch{/* optional preference */}}
function dragSidebar(event:PointerEvent){
  if(event.button!==0)return
  event.preventDefault()
  const target=event.currentTarget as HTMLElement
  target.setPointerCapture(event.pointerId)
  const start=event.clientX,initial=sidebarRef.value?.getBoundingClientRect().width||220
  const move=(current:PointerEvent)=>resizeSidebar(initial+current.clientX-start)
  const end=()=>{target.removeEventListener('pointermove',move);target.removeEventListener('pointerup',end);target.removeEventListener('pointercancel',end)}
  target.addEventListener('pointermove',move);target.addEventListener('pointerup',end);target.addEventListener('pointercancel',end)
}
const enterpriseToggle=ref<HTMLButtonElement>()
async function closeEnterprise(restoreFocus=true) {
  const path=route.path
  await router.replace({path,query:{...route.query,ew:undefined}})
  await nextTick()
  if(restoreFocus && route.path===path && !enterpriseOpen.value)enterpriseToggle.value?.focus()
}
function toggleEnterprise() {if(enterpriseOpen.value)void closeEnterprise();else void router.replace({path:route.path,query:{...route.query,ew:'1'}})}
watch(enterpriseOpen,open=>{if(open)diagnosticsOpen.value=false},{immediate:true})
watch(diagnosticsOpen,open=>{if(open && enterpriseOpen.value)void closeEnterprise(false)})
const mountedRows = ref(0)
const mobileSessionsOpen = ref(false)
// Transient controls belong to the route that opened them, not the reused page.
watch(() => route.fullPath, () => {
  mobileSessionsOpen.value = false
  if (sessionMenu.value) sessionMenu.value.open = false
  filesOpen.value = false
  renameOpen.value = false
  forkTaskId.value = null
  newTaskOpen.value = false
  localError.value = ''
  copyNotice.value = ''
  clearTimeout(copyTimer)
}, { flush: 'sync' })
const host = ref<HTMLElement | null>(null)
const listRef = ref<InstanceType<typeof SessionList> | null>(null)
const narrow = ref(false)
let sizeObserver: ResizeObserver | undefined
const sidebarCollapsed = ref(false)
try { sidebarCollapsed.value = localStorage.getItem('tuck.sidebar.collapsed') === 'true' } catch { /* optional preference */ }
const sidebarVisible = computed(() => narrow.value ? mobileSessionsOpen.value : !sidebarCollapsed.value)
const sidebarRef = ref<HTMLElement | null>(null)
const sidebarToggle = ref<HTMLButtonElement | null>(null)
const pendingSession = computed(() => props.pending)
function closeSidebar(): void {
  if (narrow.value) mobileSessionsOpen.value = false
  else { sidebarCollapsed.value = true; try { localStorage.setItem('tuck.sidebar.collapsed','true') } catch { /* optional preference */ } }
  void nextTick(() => sidebarToggle.value?.focus())
}
function openSidebar(): void {
  if (narrow.value) mobileSessionsOpen.value = true
  else { sidebarCollapsed.value = false; try { localStorage.setItem('tuck.sidebar.collapsed','false') } catch { /* optional preference */ } }
  void nextTick(() => sidebarRef.value?.querySelector<HTMLButtonElement>('button')?.focus())
}
function drawerKeys(event: KeyboardEvent): void {
  if (!narrow.value || !mobileSessionsOpen.value || document.querySelector('dialog[open]')) return
  if (event.key === 'Escape') { event.preventDefault(); closeSidebar(); return }
  if (event.key !== 'Tab') return
  const items = [...(sidebarRef.value?.querySelectorAll<HTMLElement>('button:not(:disabled),input,select,summary,a[href]') || [])].filter(el => el.getClientRects().length)
  const first = items[0], last = items.at(-1)
  if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus() }
  else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus() }
}
onMounted(() => {
  sizeObserver = new ResizeObserver(entries => {
    layoutWidth.value=entries[0]!.contentRect.width
    const next = entries[0]!.contentRect.width <= 760
    if (next !== narrow.value) { narrow.value = next; mobileSessionsOpen.value = false }
  })
  if (host.value) sizeObserver.observe(host.value)
  document.addEventListener('keydown',drawerKeys)
})
onBeforeUnmount(() => { saveView(); clearTimeout(copyTimer); sizeObserver?.disconnect(); document.removeEventListener('keydown',drawerKeys) })
function saveView(): void {
  const snapshot = viewportRef.value?.captureSnapshot()
  if (snapshot) liveWorkspace.saveSnapshot(activeSession.value.id, snapshot)
  props.viewState.listOffset = listRef.value?.offset() ?? props.viewState.listOffset
}
defineExpose({ saveView })
async function copyLink(message?: number): Promise<void> {
  try { await copyText(linkUrl(conversationPath(activeSession.value.id, message))); copyNotice.value = '链接已复制'; clearTimeout(copyTimer); copyTimer = setTimeout(() => copyNotice.value = '', 1800) }
  catch (error) { localError.value = (error as Error).message }
}


const { fps, frameP95, longTasks, heapMb } = usePerformanceMetrics(() => diagnosticsOpen.value)

const activeExecution = computed(() => workspace.executionFor(activeSession.value.id))
const activeIndicator = computed(() => { void activeUiState.value.eventRevision; return deriveSessionIndicator(activeSession.value.kernel.summary) })
const allDescriptors = computed(() => { void workspaceRevision.value; return workspace.descriptors })
const sessionDescriptors = computed(() => {
  const query = sessionSearch.value.trim().toLocaleLowerCase()
  return !query && !assistantFilter.value ? allDescriptors.value : allDescriptors.value.filter(task => (!query || task.title.toLocaleLowerCase().includes(query)) && (!assistantFilter.value || liveWorkspace.tasks.get(task.id)?.agent === assistantFilter.value))
})
const hotSessionCount = computed(() => { void workspaceRevision.value; return workspace.hotSessionCount })
const runningSessionCount = computed(() => { void workspaceRevision.value; return workspace.runningSessionCount })
const blockedSessionCount = computed(() => { void workspaceRevision.value; return workspace.blockedSessionCount })
const failedSessionCount = computed(() => { void workspaceRevision.value; return workspace.failedSessionCount })
let sendNavigation: { sessionId: string; revision: number } | null = null
function sendingMessage(): void { sendNavigation = { sessionId: activeSession.value.id, revision: viewportRef.value?.captureNavigationIntent() ?? -1 } }
function messageSent(): void {
  const intent = sendNavigation; sendNavigation = null
  if (intent?.sessionId === activeSession.value.id) void viewportRef.value?.jumpToLatestIfUnchanged(intent.revision)
}

async function switchSession(id: string): Promise<void> {
  mobileSessionsOpen.value = false
  await router.push({path:conversationPath(id),query:route.query})
}

function newSession(): void { mobileSessionsOpen.value = false; newTaskOpen.value = true }
async function sessionCreated(id: string): Promise<void> {
  if (assistantFilter.value) assistantFilter.value = liveWorkspace.tasks.get(id)?.agent || ''
  sessionSearch.value = ''
  await switchSession(id)
}

function jump(target: number): void { void viewportRef.value?.jumpToMessage(target) }
function onConversationClick(event: MouseEvent): void {
  const target = event.target instanceof Element ? event.target.closest<HTMLElement>('[data-child-session-id]') : null
  const childSessionId = target?.dataset.childSessionId
  if (!childSessionId || childSessionId === activeSession.value.id) return
  if (workspace.hasSession(childSessionId)) void switchSession(childSessionId)
}

async function prepareHistory(start: number, count: number): Promise<void> { await liveWorkspace?.prepareHistory(activeSession.value.id, start, count) }
async function archiveTask(): Promise<void> { if (!liveWorkspace) return; try { const next=await liveWorkspace.archiveSession(activeSession.value.id); if(next)await switchSession(next);else await router.push('/chat') } catch (error) { localError.value = (error as Error).message } }
</script>

<template>
  <section ref="host" class="agent-app" :style="{'--preferred-session-sidebar-width':sidebarWidth+'px','--sidebar-reserved-width':sidebarReservedWidth+'px',...(enterpriseOpen && route.query.wide!=='1'?{'--diagnostics-width':`min(${enterpriseWidth}px, calc(100cqw - var(--workspace-sidebar-width) - 480px))`}:{})}" :class="{ 'live-workspace': true, 'diagnostics-closed': !diagnosticsOpen && (!enterpriseOpen || route.query.wide==='1'), 'mobile-sessions-open': mobileSessionsOpen, 'sidebar-collapsed': sidebarCollapsed }">
    <button v-if="narrow && mobileSessionsOpen" class="mobile-session-backdrop" type="button" aria-label="Close conversations" @click="closeSidebar" />

    <aside id="conversation-sidebar" ref="sidebarRef" class="session-sidebar" data-testid="session-sidebar" :inert="!sidebarVisible || enterpriseOpen && route.query.wide==='1'" :aria-hidden="!sidebarVisible">
      <div class="sidebar-head">
        <div>
          <div class="product-name">与 Tuck 对话</div>
        </div>
        <div class="sidebar-head-actions"><button class="new-session" data-testid="new-session" @click="newSession"><svg viewBox="0 0 16 16" aria-hidden="true"><path d="M8 3v10M3 8h10" /></svg><span>新对话</span></button>
          <button class="sidebar-control" type="button" :aria-label="narrow ? '关闭对话列表' : '收起对话列表'" :title="narrow ? '关闭对话列表' : '收起对话列表'" @click="closeSidebar"><SidebarIcon expanded /></button>
        </div>
      </div>

      <label v-if="assistantOptions.length > 1" class="assistant-filter"><AssistantIcon /><select v-model="assistantFilter" aria-label="筛选助手"><option value="">全部助手</option><option v-for="item in assistantOptions" :key="item.agent" :value="item.agent">{{ item.displayName }}</option></select><svg class="filter-chevron" aria-hidden="true" viewBox="0 0 20 20"><path d="m5 7.5 5 5 5-5" /></svg></label>
      <input v-model="sessionSearch" class="session-search" aria-label="搜索对话" placeholder="搜索对话…" />
      <SessionList ref="listRef" :initial-offset="viewState.listOffset" :key="assistantFilter" :sessions="sessionDescriptors" :active-id="activeSession.id" :query="sessionSearch" :assistant-names="assistantNames" :show-assistant="!assistantFilter" @select="switchSession" />
      <details v-if="archivedTasks.length" :open="archivesOpen" class="archived-sessions" @toggle="archivesOpen = ($event.target as HTMLDetailsElement).open">
        <summary><svg aria-hidden="true" viewBox="0 0 20 20"><path d="m7 5 5 5-5 5" /></svg><span>已归档</span><span class="archive-count">{{ archivedTasks.length }}</span></summary>
        <div v-if="archivesOpen" class="archive-list">
          <button v-for="task in archivedTasks" :key="task.id" type="button" :aria-label="`恢复 ${taskName(task.title)}`" :title="taskName(task.title)" :disabled="!!restoringArchive" @click="restoreArchive(task.id)">
            <span class="archive-title">{{ taskName(task.title) }}</span><span class="archive-action"><svg aria-hidden="true" viewBox="0 0 20 20"><path d="M4 8a6 6 0 1 1 0 5M4 3v5h5" /></svg>{{ restoringArchive === task.id ? '恢复中…' : '恢复' }}</span>
          </button>
        </div>
      </details>
      <div v-if="liveWorkspace" class="sidebar-footer"><span class="status-led" :class="{ disconnected: !liveWorkspace.connected }" /><span>{{ liveWorkspace.connected ? '已连接' : '正在重连…' }}</span><span v-if="runningSessionCount">· {{ runningSessionCount }} 个任务执行中</span></div>
    </aside>
    <div v-if="!sidebarCollapsed && !(enterpriseOpen && route.query.wide==='1')" class="sidebar-resize" role="separator" aria-label="调整对话列表宽度" aria-orientation="vertical" :aria-valuenow="Math.round(actualSidebarWidth)" aria-valuemin="220" aria-valuemax="400" tabindex="0" title="拖动调整对话列表宽度，双击恢复默认" @pointerdown="dragSidebar" @dblclick="resizeSidebar(220)" @keydown.left.prevent="resizeSidebar((sidebarRef?.getBoundingClientRect().width||220)-20)" @keydown.right.prevent="resizeSidebar((sidebarRef?.getBoundingClientRect().width||220)+20)" />

    <ConversationViewport
      data-summary-boundary
      :key="activeSession.id"
      ref="viewportRef"
      :runtime="activeSession"
      :highlight-message="linkedMessage"
      :renderers="renderers"
      :assistant-name="messageAssistantName"
      latest-label="回到最新"
      :execution="activeExecution"
      :ui-state="activeUiState"
      :prepare-history="prepareHistory"

      :read-only="!!taskInfo?.parentTaskId"
      :inert="narrow && mobileSessionsOpen || enterpriseOpen && route.query.wide==='1'"
      @click.capture="onConversationClick"
      @viewport-metrics="mountedRows = $event.mountedRows"
    >
      <template #header-leading><button v-if="!sidebarVisible" ref="sidebarToggle" class="sidebar-control" data-testid="mobile-session-toggle" type="button" aria-label="展开对话列表" title="展开对话列表" aria-controls="conversation-sidebar" :aria-expanded="sidebarVisible" @click="openSidebar"><SidebarIcon /></button></template>
      <template #empty-state><div class="empty-agent-mark">✦</div><h2>开始新对话</h2></template>
      <template v-if="liveWorkspace && taskInfo" #composer>
        <WorkbenchComposer :inert="!!pendingSession" :key="taskInfo.id" :task="taskInfo" :runtime="activeSession" :workspace="liveWorkspace" :connection="connection" :connected="liveWorkspace.connected" :error="runtimeError" @sending="sendingMessage" @sent="messageSent" @layout="viewportRef?.capturePendingLayoutIntent()" />
      </template>
      <template #status><span class="run-status" :class="`indicator-${activeIndicator}`" :title="taskInfo ? taskStatus(taskInfo) : indicatorLabel(activeIndicator)"><ActivityStatus :status="activeIndicator" aria-hidden="true" /><span class="run-status-label">{{ taskInfo ? taskStatus(taskInfo) : indicatorLabel(activeIndicator) }}</span></span></template>
      <template #title><SessionNavigation :tasks="navigationTasks" :active-id="activeSession.id" @navigate="switchSession" /></template>
      <template #header-context>
        <AssistantSummary v-if="artifactTask" :task="artifactTask" :assistant="activeAssistant" :tool-available="liveWorkspace.runtimeInfo.deliveryToolAvailable" :require-delivery="deliveryRequirement" :parent-context="!!taskInfo?.parentTaskId" />
      </template>
      <template #node-actions="{ unit }"><button class="message-link" type="button" aria-label="复制消息链接" title="复制消息链接" :data-message-link="unit.messageIndex" @click.stop="copyLink(unit.messageIndex)"><svg viewBox="0 0 20 20" aria-hidden="true"><path d="m8 12 4-4M7 10l-2 2a3 3 0 0 0 4 4l2-2m-2-8 2-2a3 3 0 0 1 4 4l-2 2" /></svg></button></template>
      <template #header-actions>
        <DeliverySummary v-if="showDelivery && taskInfo" ref="deliverySummary" :task="taskInfo" :tool-enabled="deliveryToolEnabled" :require-delivery="deliveryRequirement" />
        <button v-if="taskInfo?.forkedFrom" class="reference-header-action" type="button" aria-label="来源" :title="taskName(taskInfo.forkedFrom.title)" @click="switchSession(taskInfo.forkedFrom.id)"><svg aria-hidden="true" viewBox="0 0 20 20"><path d="M6 3v8a4 4 0 0 0 4 4h6m-4-4 4 4-4 4" /></svg><span>来源</span></button>
        <button class="reference-header-action" type="button" data-testid="task-files" aria-label="产物" title="产物" :aria-expanded="filesOpen" @click="filesTrigger=$event.currentTarget as HTMLElement;filesOpen = !filesOpen"><svg aria-hidden="true" viewBox="0 0 20 20"><path d="M5 2h7l4 4v12H5Z M12 2v5h4 M8 11h5 M8 14h5" /></svg><span>产物</span></button>
        <button ref="enterpriseToggle" class="reference-header-action workspace-toggle" type="button" aria-label="工作区" :title="enterpriseOpen?'收起工作区':'打开工作区'" aria-controls="enterprise-workspace-panel" :aria-expanded="enterpriseOpen" :aria-pressed="enterpriseOpen" @click="toggleEnterprise"><RuncoveMark /><span>工作区</span></button>
        <details ref="sessionMenu" class="tuck-session-menu">
          <summary data-testid="session-menu" aria-label="更多操作" title="更多操作">⋯</summary>
          <div class="tuck-session-menu-items">
            <button v-if="showDelivery" class="mobile-header-item" type="button" @click="deliverySummary?.open(sessionMenu?.querySelector('summary')||undefined);sessionMenu&&(sessionMenu.open=false)"><svg viewBox="0 0 20 20" aria-hidden="true"><path d="M6 3H4v15h12V3h-2 M7 2h6v4H7Z M6 11l3 3 5-5" /></svg><span>交付摘要</span></button>
            <button class="mobile-header-item" type="button" @click="filesTrigger=sessionMenu?.querySelector('summary')||null;filesOpen=true;sessionMenu&&(sessionMenu.open=false)"><svg viewBox="0 0 20 20" aria-hidden="true"><path d="M5 2h7l4 4v12H5Z M12 2v5h4 M8 11h5 M8 14h5" /></svg><span>产物</span></button>
            <button class="mobile-header-item" type="button" @click="toggleEnterprise();sessionMenu&&(sessionMenu.open=false)"><RuncoveMark /><span>工作区</span></button>
            <button type="button" @click="copyLink(); sessionMenu && (sessionMenu.open = false)"><svg viewBox="0 0 20 20" aria-hidden="true"><path d="m8 12 4-4 M7 13l-1 1a3 3 0 0 1-4-4l4-4a3 3 0 0 1 4 0 M13 7l1-1a3 3 0 0 1 4 4l-4 4a3 3 0 0 1-4 0" /></svg><span>复制对话链接</span></button><button v-if="taskInfo" type="button" @click="renameOpen = true; sessionMenu && (sessionMenu.open = false)"><svg viewBox="0 0 20 20" aria-hidden="true"><path d="m12 3 5 5 M3 17l4-1L17 6a2 2 0 0 0-3-3L4 13Z" /></svg><span>重命名</span></button><button v-if="!taskInfo?.parentTaskId && taskInfo?.status === 'idle' && taskInfo.logicalCount" @click="forkTaskId = activeSession.id; sessionMenu && (sessionMenu.open = false)"><svg viewBox="0 0 20 20" aria-hidden="true"><path d="M6 5v10 M6 10h5a3 3 0 0 0 3-3V5 M4 3h4v3H4Z M12 2h4v3h-4Z M4 15h4v3H4Z" /></svg><span>创建分支</span></button>
            <button v-if="!taskInfo?.parentTaskId && activeUiState.sessionStatus === 'idle'" type="button" data-testid="task-archive" @click="archiveTask"><svg viewBox="0 0 20 20" aria-hidden="true"><path d="M3 3h14v4H3Z M4 7v10h12V7 M8 10h4" /></svg><span>归档对话</span></button>
            <button v-if="taskInfo?.queue.length" type="button" @click="liveWorkspace?.command(activeSession.id, 'clear-queue', {}); sessionMenu && (sessionMenu.open = false)"><svg viewBox="0 0 20 20" aria-hidden="true"><path d="M4 6h12 M8 3h4 M6 6l1 11h6l1-11" /></svg><span>清空队列</span></button>
            <button data-testid="diagnostics-open" type="button" :aria-pressed="diagnosticsOpen" @click="diagnosticsOpen = !diagnosticsOpen; sessionMenu && (sessionMenu.open = false); sessionMenu?.querySelector('summary')?.focus()"><svg viewBox="0 0 20 20" aria-hidden="true"><path d="M3 16V9 M8 16V4 M13 16v-6 M18 16V7" /></svg><span>运行详情</span></button>
          </div>
        </details>
      </template>
      <template #viewport-overlay="{ mountedRows: visibleRows, followLabel, uiState, restoring, historyPending, historyError, retryHistory }">
        <div v-if="pendingSession || restoring" class="conversation-loading" data-testid="conversation-loading"><TuckLoading label="正在加载对话…" /></div>
        <div v-else-if="historyPending || historyError" class="history-fetch-status" :role="historyError ? 'alert' : 'status'">
          <button v-if="historyError" type="button" :title="historyError" @click="retryHistory">历史加载失败，重试</button>
          <TuckLoading v-else label="正在加载历史…" />
        </div>
        <div v-show="diagnosticsOpen" class="conversation-meta-strip">
          <span>Loaded <strong data-testid="segment-range">{{ uiState.rangeStart.toLocaleString() }} – {{ Math.max(uiState.rangeStart, uiState.rangeEnd - 1).toLocaleString() }}</strong></span>
          <span>Reader <strong data-testid="reader-position">#{{ uiState.reader.toLocaleString() }}</strong></span>
          <span data-testid="mounted-label">{{ visibleRows }} DOM rows</span><span data-testid="follow-state">{{ followLabel }}</span>
        </div>
      </template>
    </ConversationViewport>

    <EnterpriseWorkspace v-if="enterpriseOpen" :display-width="actualEnterpriseWidth" :maximum-width="maximumEnterpriseWidth" @close="closeEnterprise()" @width="resizeEnterprise" />
    <DiagnosticsPanel
      v-if="diagnosticsOpen"
      :runtime="activeSession"
      :task="taskInfo"
      :runtime-info="props.workspace.runtimeInfo"
      :connected="props.workspace.connected"
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
      @close="diagnosticsOpen = false"
      @jump="jump"
      @shift-backward="viewportRef?.shiftBackward()"
      @shift-forward="viewportRef?.shiftForward()"
    />
    <TaskFiles v-if="filesOpen && artifactTask" :key="activeSession.id" :task="artifactTask" :trigger="filesTrigger" :shared="!!taskInfo?.parentTaskId" @close="filesOpen = false" />
    <ForkConversationDialog v-if="forkTaskId" :workspace="liveWorkspace" :task-id="forkTaskId" @close="forkTaskId = null" @created="switchSession" />
    <RenameConversationDialog v-if="renameOpen && taskInfo" :task="taskInfo" :workspace="liveWorkspace" @close="renameOpen = false" />
    <NewConversationDialog v-if="newTaskOpen" :workspace="liveWorkspace" :connection="connection" :initial-agent="assistantFilter || assistants.defaultAgent" @close="newTaskOpen = false" @created="sessionCreated" />
    <div v-if="copyNotice" class="copy-feedback" role="status">{{ copyNotice }}</div>
  </section>
</template>

<style scoped src="./styles/workbench.css"></style>
<style scoped src="./styles/conversation.css"></style>
<style scoped>
.agent-app {position:relative}
.assistant-filter { position:relative; display:flex; align-items:center; margin:0 8px 8px; color:var(--tuck-muted); font-size:var(--tuck-font-ui); }
.assistant-filter select { appearance:none; box-sizing:border-box; width:100%; min-width:0; height:32px; padding:0 30px; font:inherit; color:var(--tuck-text); background:var(--tuck-surface); border:1px solid var(--tuck-border); border-radius:8px; cursor:pointer; }
.assistant-filter > .assistant-icon { position:absolute; left:10px; pointer-events:none; }
.assistant-filter select::picker-icon { display:none; }
.filter-chevron { position:absolute; right:10px; width:12px; height:12px; fill:none; stroke:currentColor; stroke-width:1.5; stroke-linecap:round; stroke-linejoin:round; pointer-events:none; }
.workspace-toggle :deep(svg) { fill:currentColor; stroke:none; }
@container chat-column (max-width:760px) {
  .reference-header-action > span { display:none; }
  .reference-header-action { width:30px; padding:0; }
}
</style>
