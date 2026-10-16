<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, shallowRef, useSlots, watch } from 'vue'
import { VList, type VListHandle } from 'virtua/vue'
import type { ConversationExecutionController } from '../conversation/contracts'
import {
  billedInputTokens,
  cacheHitPercent,
  contextOccupancyPercent,
  deriveSessionIndicator,
  formatTokens,
  sessionIndicatorLabel,
} from '../conversation/session-semantics'
import type { ConversationSessionRuntime, SessionUiSnapshot, ShiftPlan } from '../runtime/session-runtime'
import type { SessionViewMemory } from '../viewport/state'
import { VIEWPORT_POLICY, type CommittedViewportAnchor } from '../viewport/contracts'
import ConversationNodeSeat from './ConversationNodeSeat.vue'
import type { RendererResolver } from './renderers/registry'
import { ViewportNavigationController } from './viewport-navigation-controller'
import { rememberMeasurements, restoredMeasurements } from './viewport-measurement-cache'

const props = defineProps<{ runtime: ConversationSessionRuntime; execution: ConversationExecutionController; uiState: SessionUiSnapshot; renderers?: RendererResolver; assistantName?: string; latestLabel?: string; readOnly?: boolean; placeholder?: string; highlightMessage?: number; prepareHistory?: (start: number, count: number) => Promise<void> }>()
const emit = defineEmits<{ viewportMetrics: [metrics: { mountedRows: number }] }>()
const slots = useSlots()

const VIRTUAL_BUFFER_PX = 900

const listRef = ref<VListHandle | null>(null)
const scrollStageRef = ref<HTMLElement | null>(null)
const composerInputRef = ref<HTMLTextAreaElement | null>(null)
const composerShellRef = ref<HTMLElement | null>(null)
const scrollbarWidth = ref(0)
const order = shallowRef<string[]>([...props.runtime.projection.order])
const composerText = ref(props.runtime.draftText)
const interactionAnswer = ref('')
const mountedRows = ref(0)
const physicalBottom = ref(true)
const restoring = ref(true)
const listReady = ref(false)
const initialCache = shallowRef<VListHandle['cache']>()
const initialEpoch = props.uiState.virtualEpoch
const shifting = ref(false)
const historyError = ref('')
const historyRequests = ref(0)
let retryHistoryAction: (() => Promise<void>) | undefined
const uiState = computed(() => props.uiState)
let metricsFrame = 0
let viewportResizeFrame = 0
let viewportResizeRunning = false
let viewportResizeQueued = false
let viewportObserver: ResizeObserver | null = null
let unsubscribeOrder: (() => void) | null = null
let pendingComposerAnchor: CommittedViewportAnchor | null = null
let pendingComposerPinned = false
let disposed = false
let userNavigationRevision = 0
let historyNavigationRevision = 0
let tailFrame = 0
let contentObserver: ResizeObserver | null = null

const messagesAfter = computed(() => uiState.value.messagesAfter)
const showLatest = computed(() => !restoring.value && props.runtime.logicalCount > 0 && !uiState.value.followTail && (messagesAfter.value > 0 || !physicalBottom.value || uiState.value.rangeEnd < props.runtime.logicalCount))
const followLabel = computed(() => uiState.value.followTail ? 'following tail' : 'tail paused')
const sessionIndicator = computed(() => {
  void uiState.value.eventRevision
  return deriveSessionIndicator(props.runtime.kernel.summary)
})
const statusLabel = computed(() => sessionIndicatorLabel(sessionIndicator.value))
const canSend = computed(() => !props.readOnly && Boolean(composerText.value.trim()) && !uiState.value.pendingInteraction)
const canAnswerQuestion = computed(() => Boolean(interactionAnswer.value.trim()))
const sendLabel = computed(() => uiState.value.sessionStatus === 'working' ? 'Queue prompt' : 'Send prompt')
const composerPlaceholder = computed(() => uiState.value.pendingInteraction
  ? 'Resolve the pending request before continuing…'
  : uiState.value.sessionStatus === 'working'
    ? 'Queue a follow-up while the agent is working…'
    : 'Ask the agent anything…')
const tokenUsage = computed(() => {
  void uiState.value.eventRevision
  return props.runtime.kernel.usage
})
const billedInput = computed(() => billedInputTokens(tokenUsage.value))
const cacheHit = computed(() => cacheHitPercent(tokenUsage.value))
const contextPercent = computed(() => contextOccupancyPercent(props.runtime.kernel.context))

function frame(): Promise<void> { return new Promise(resolve => requestAnimationFrame(() => resolve())) }
async function settleFrames(count = 2): Promise<void> { for (let i = 0; i < count; i += 1) { await nextTick(); await frame() } }
function nodeFor(id: string) { return props.runtime.projection.getNode(id) }
function itemProps({ item, index }: { item: string; index: number }) {
  const node = nodeFor(item)
  return {
    class: node?.messageIndex === props.highlightMessage ? 'virtua-row linked-message' : 'virtua-row',
    'data-virtual-item': 'true',
    'data-index': index,
    'data-message-index': node?.messageIndex ?? -1,
    'data-render-unit': item,
    'data-session-id': props.runtime.id,
  }
}

const navigation = new ViewportNavigationController({
  runtime: props.runtime,
  getOrder: () => order.value,
  getList: () => listRef.value,
  getStage: () => scrollStageRef.value,
  getVirtualEpoch: () => uiState.value.virtualEpoch,
  settleFrames,
  onNavigationSettled: () => { if (viewportResizeQueued) scheduleViewportResizeReconcile(); scheduleTailPin() },
})

function refreshMountedRows(): void {
  if (disposed || metricsFrame) return
  metricsFrame = requestAnimationFrame(() => {
    metricsFrame = 0
    const next = scrollStageRef.value?.querySelectorAll('[data-virtual-item="true"]').length ?? 0
    if (mountedRows.value === next) return
    mountedRows.value = next
    emit('viewportMetrics', { mountedRows: next })
  })
}

function takeUserScrollControl(direction: -1 | 0 | 1): void {
  // The not-yet-visible list has no reader position to take over. Gestures on
  // the loading surface must not cancel restoration and reveal estimates.
  if (restoring.value) return
  userNavigationRevision++
  navigation.invalidate()
  pendingComposerPinned = false; pendingComposerAnchor = null
  if (tailFrame) { cancelAnimationFrame(tailFrame); tailFrame = 0 }
  navigation.markUserIntent(direction)
  // Intent changes before the browser applies the native movement. Geometry
  // may still say "bottom" for this frame; upward intent must stay paused.
  props.runtime.setFollowTail(false)
  // At the physical end a downward gesture produces no scroll event. Commit
  // that explicit intent here, including short/just-collapsed conversations.
  if (direction > 0) resumeFollowAtEnd()
}
function resumeFollowAtEnd(): void {
  if (props.runtime.range.end !== props.runtime.logicalCount || navigation.remainingToBottom() >= VIEWPORT_POLICY.bottomTolerancePx) return
  props.runtime.setReaderPosition(props.runtime.logicalCount - 1, true)
  props.runtime.setFollowTail(true)
  navigation.rememberCommittedAnchor()
  scheduleTailPin()
}
function onUserWheel(event: WheelEvent): void {
  if (restoring.value) return
  const direction: -1 | 0 | 1 = event.deltaY < 0 ? -1 : event.deltaY > 0 ? 1 : 0
  // Scrolling an expanded tool result is not leaving the conversation tail.
  let element = event.target instanceof Element ? event.target : null
  while (element && element !== scrollStageRef.value && !element.classList.contains('conversation-vlist')) {
    if (element instanceof HTMLElement && /(auto|scroll)/.test(getComputedStyle(element).overflowY) && element.scrollHeight > element.clientHeight + 1 && (direction < 0 ? element.scrollTop > 0 : element.scrollTop + element.clientHeight < element.scrollHeight - 1)) return
    element = element.parentElement
  }
  if (!direction) return
  takeUserScrollControl(direction)
  // Latest is an overlay sibling of the scrollport. Native wheel chaining
  // over it cannot reach the list, so forward only this overlay's movement.
  if (event.target instanceof Element && event.target.closest('.jump-latest')) {
    event.preventDefault()
    const scale = event.deltaMode === 1 ? 16 : event.deltaMode === 2 ? (listRef.value?.viewportSize ?? 1) : 1
    navigation.scrollByOffset(event.deltaY * scale)
  }
  // At a loaded-window edge the native scroll offset may not change, so no
  // scroll-end event will arrive to load messages beyond that edge.
  if (direction > 0 && props.runtime.range.end < props.runtime.logicalCount && navigation.remainingToBottom() < VIEWPORT_POLICY.edgeThresholdPx) void shiftForward()
  else if (direction < 0 && props.runtime.range.start > 0 && (listRef.value?.scrollOffset ?? Infinity) < VIEWPORT_POLICY.edgeThresholdPx) void shiftBackward()
}
function onUserPointerDown(event: PointerEvent): void {
  // Clicking text, expanding a tool or selecting content is not a scroll
  // gesture. Only the native scroll surface itself can start a thumb drag.
  if (!(event.target instanceof HTMLElement) || !event.target.classList.contains('conversation-vlist')) return
  takeUserScrollControl(0)
}
function onUserTouchMove(): void { takeUserScrollControl(0) }
function onUserKeydown(event: KeyboardEvent): void {
  if (event.target instanceof Element && event.target.closest('input,textarea,select,button,[contenteditable="true"]')) return
  const direction = ['ArrowUp', 'PageUp', 'Home'].includes(event.key) || event.key === ' ' && event.shiftKey ? -1 : ['ArrowDown', 'PageDown', 'End', ' '].includes(event.key) ? 1 : 0
  if (!direction) return
  takeUserScrollControl(direction)
}
function captureNavigationIntent(): number { return userNavigationRevision }
async function jumpToLatestIfUnchanged(revision: number): Promise<void> { if (revision === userNavigationRevision) await jumpToLatest() }
function reconcileMeasuredBottom(): void {
  physicalBottom.value = navigation.remainingToBottom() < VIEWPORT_POLICY.bottomTolerancePx
  // Measurements describe position, never user intent. Only a user scroll to
  // the end or explicit Latest can enable following.
  if (restoring.value || navigation.running) return
  const atEnd = physicalBottom.value && props.runtime.range.end === props.runtime.logicalCount
  if (props.runtime.atVisualBottom !== atEnd) props.runtime.setReaderPosition(props.runtime.currentLogicalPosition, atEnd)
}
function onVirtualScroll(offset: number): void {
  navigation.recordScrollOffset(offset)
  reconcileMeasuredBottom()
  const hasIntent = !navigation.running && navigation.hasUserIntent()
  if (hasIntent) { pendingComposerPinned = false; pendingComposerAnchor = null }
  if (hasIntent) {
    navigation.updateReaderFromUserScroll(offset)
    navigation.rememberCommittedAnchor()
  }
  refreshMountedRows()
  if (!hasIntent) return
  if (navigation.scrollDirection > 0) resumeFollowAtEnd()
}
function onVirtualScrollEnd(): void {
  refreshMountedRows()
  const hasUserIntent = navigation.hasUserIntent()
  if (hasUserIntent && navigation.scrollDirection > 0 && !navigation.running) resumeFollowAtEnd()
  if (hasUserIntent) navigation.rememberCommittedAnchor()
  if (shifting.value || !hasUserIntent) return
  if (navigation.scrollDirection < 0 && (listRef.value?.scrollOffset ?? Infinity) < VIEWPORT_POLICY.edgeThresholdPx) void shiftBackward()
  else if (navigation.scrollDirection > 0 && navigation.remainingToBottom() < VIEWPORT_POLICY.edgeThresholdPx) void shiftForward()
}

async function applyShift(plan: ShiftPlan): Promise<void> {
  const revision = navigation.begin()
  const readerBefore = props.runtime.currentLogicalPosition
  const anchor = navigation.committedAnchor ?? navigation.captureCommittedAnchor()
  try {
    props.runtime.applyIntermediate(plan.intermediate, plan.direction === 'backward')
    await settleFrames(plan.direction === 'backward' ? 3 : 2)
    if (disposed) return
    props.runtime.commitShift(plan, navigation.isCurrent(revision) && plan.direction === 'forward' && plan.next.start > plan.previous.start)
    await settleFrames(3)
    if (!navigation.isCurrent(revision) || disposed) return
    if (anchor) await navigation.restoreListAnchor(anchor, revision)
    if (navigation.isCurrent(revision)) props.runtime.setReaderPosition(readerBefore, false)
    refreshMountedRows()
  } finally {
    props.runtime.finishShift()
    navigation.finish(revision)
  }
}
async function prepareHistoryRange(start: number, count: number): Promise<void> {
  if (!props.prepareHistory) return
  historyRequests.value++
  try { await props.prepareHistory(start, count) }
  finally { historyRequests.value-- }
}
async function shiftWindow(direction: 'backward' | 'forward'): Promise<void> {
  if (shifting.value) return
  const runtime = props.runtime, previous = runtime.range, revision = historyNavigationRevision
  const start = direction === 'backward' ? Math.max(0, previous.start - runtime.segment.shift) : Math.min(Math.max(0, runtime.logicalCount - runtime.segment.capacity), previous.start + runtime.segment.shift)
  const end = Math.min(runtime.logicalCount, start + runtime.segment.capacity)
  if (start === previous.start && end === previous.end) return
  shifting.value = true; historyError.value = ''; retryHistoryAction = () => shiftWindow(direction)
  try {
    const first = Math.min(previous.start, start), last = Math.max(previous.end, end)
    await prepareHistoryRange(first, last - first)
    if (disposed || revision !== historyNavigationRevision || runtime.range.start !== previous.start || runtime.range.end !== previous.end) return
    if (navigation.hasUserIntent() && navigation.scrollDirection !== (direction === 'backward' ? -1 : 1)) return
    const plan = direction === 'backward' ? runtime.planShiftBackward() : runtime.planShiftForward()
    if (!plan) return
    navigation.clearUserIntent()
    if (direction === 'backward') runtime.setFollowTail(false)
    await applyShift(plan)
  } catch (error) {
    if (!disposed && revision === historyNavigationRevision) historyError.value = (error as Error).message
  } finally { shifting.value = false }
}
async function shiftBackward(): Promise<void> { await shiftWindow('backward') }
async function shiftForward(): Promise<void> { await shiftWindow('forward') }
function retryHistory(): void { void retryHistoryAction?.() }

async function jumpToMessage(raw = props.runtime.currentLogicalPosition): Promise<void> {
  if (props.runtime.logicalCount <= 0) return
  userNavigationRevision++
  historyNavigationRevision++
  historyError.value = ''
  const revision = navigation.begin()
  const target = Math.max(0, Math.min(props.runtime.logicalCount - 1, Math.floor(Number(raw) || 0)))
  retryHistoryAction = () => jumpToMessage(target)
  // A centered jump is reading intent, including the last (possibly tall)
  // message. Only Latest commits a navigation to the physical bottom.
  props.runtime.setFollowTail(false)
  props.runtime.setReaderPosition(props.runtime.currentLogicalPosition, false)
  pendingComposerPinned = false; pendingComposerAnchor = null
  try {
    const range = props.runtime.segment.around(target)
    await prepareHistoryRange(range.start, range.end - range.start)
    if (!navigation.isCurrent(revision)) return
    navigation.clearUserIntent()
    const previousEpoch = uiState.value.virtualEpoch
    const previousHandle = listRef.value
    props.runtime.jump(target)
    if (!await navigation.waitForJumpEpoch(previousEpoch, previousHandle, target, revision)) return
    await navigation.scrollToLogical(target, 'center', revision)
  } catch (error) {
    if (!disposed && navigation.isCurrent(revision)) historyError.value = (error as Error).message
  } finally {
    navigation.finish(revision)
  }
}
async function jumpToLatest(): Promise<void> {
  if (props.runtime.logicalCount <= 0) return
  userNavigationRevision++
  historyNavigationRevision++
  historyError.value = ''
  const revision = navigation.begin()
  retryHistoryAction = jumpToLatest
  // Keep the old readable window until its replacement is prepared. Otherwise
  // a live append could materialize a still-missing tail during this HTTP read.
  props.runtime.setFollowTail(false)
  try {
    navigation.clearUserIntent()
    let last = props.runtime.logicalCount - 1
    await prepareHistoryRange(Math.max(0, last + 1 - props.runtime.segment.capacity), props.runtime.segment.capacity)
    if (!navigation.isCurrent(revision)) return
    props.runtime.setFollowTail(true)
    last = props.runtime.logicalCount - 1
    if (props.runtime.range.end !== props.runtime.logicalCount) {
      const previousEpoch = uiState.value.virtualEpoch
      const previousHandle = listRef.value
      props.runtime.jump(last)
      if (!await navigation.waitForJumpEpoch(previousEpoch, previousHandle, last, revision, true)) return
    } else {
      props.runtime.refreshProjection()
      await settleFrames(1)
      if (!navigation.isCurrent(revision)) return
    }
    if (order.value.length === 0) {
      props.runtime.setReaderPosition(last, true); props.runtime.setFollowTail(true); physicalBottom.value = true
      return
    }
    if (!await navigation.scrollToLogical(last, 'end', revision)) return
    await navigation.pinMeasuredEnd(VIEWPORT_POLICY.restoreAttempts, revision)
    // A growing stream may not settle within this transaction. The explicit
    // follow intent survives; subsequent measurements continue the same intent.
  } catch (error) {
    if (!disposed && navigation.isCurrent(revision)) { props.runtime.setFollowTail(false); historyError.value = (error as Error).message }
  } finally {
    navigation.finish(revision)
  }
}

function abortRun(): void { props.execution.abort() }
function resolveApproval(approved: boolean): void {
  const pending = uiState.value.pendingInteraction
  if (!pending || pending.kind !== 'approval') return
  props.execution.resolveInteraction({ interactionId: pending.id, kind: 'approval', approved })
}
function resolveQuestion(answer: string | null): void {
  const pending = uiState.value.pendingInteraction
  if (!pending || pending.kind !== 'question') return
  const normalized = answer === null ? null : answer.trim()
  if (normalized !== null && !normalized) return
  props.execution.resolveInteraction({ interactionId: pending.id, kind: 'question', answer: normalized })
  interactionAnswer.value = ''
}

function resizeComposer(): void {
  const input = composerInputRef.value
  if (!input) return
  input.style.height = '0px'
  input.style.height = `${Math.max(1, input.scrollHeight)}px`
  input.style.overflowY = input.scrollHeight > input.clientHeight ? 'auto' : 'hidden'
}
function capturePendingLayoutIntent(): void {
  // A navigation transaction owns its destination. Layout changes can queue
  // reconciliation, but must not capture its intermediate DOM as a new anchor.
  // After it settles, reconciliation uses the committed destination instead.
  if (navigation.running) return
  const shouldPin = pendingComposerPinned
    || (props.runtime.followTail && props.runtime.range.end === props.runtime.logicalCount)
  if (shouldPin) {
    pendingComposerPinned = true
    pendingComposerAnchor = null
    return
  }
  // A preceding width change may already have reflowed the DOM before input
  // delivers this callback. Keep the last settled reader anchor authoritative.
  if (!pendingComposerAnchor) pendingComposerAnchor = navigation.committedAnchor ?? navigation.captureCommittedAnchor()
  if (!navigation.committedAnchor && pendingComposerAnchor) navigation.committedAnchor = pendingComposerAnchor
}
function onComposerInput(): void {
  props.runtime.setDraftText(composerText.value)
  capturePendingLayoutIntent()
  void nextTick().then(resizeComposer)
}
async function sendComposer(): Promise<void> {
  const prompt = composerText.value.trim()
  if (props.readOnly || !prompt || uiState.value.pendingInteraction) return
  const disposition = props.execution.submit(prompt)
  if (disposition === 'blocked') return
  composerText.value = ''; props.runtime.setDraftText('')
  capturePendingLayoutIntent()
  await nextTick(); resizeComposer()
  if (disposition === 'started') { await settleFrames(2); await jumpToLatest() }
}
function scheduleViewportResizeReconcile(): void {
  if (disposed) return
  viewportResizeQueued = true
  if (navigation.running || viewportResizeFrame || viewportResizeRunning) return
  viewportResizeFrame = requestAnimationFrame(() => {
    viewportResizeFrame = 0
    void reconcileViewportResize()
  })
}
async function reconcileViewportResize(): Promise<void> {
  if (navigation.running || viewportResizeRunning) return
  viewportResizeRunning = true
  try {
    while (!disposed && viewportResizeQueued && !navigation.running) {
      viewportResizeQueued = false
      const userRevision = userNavigationRevision
      await settleFrames(2)
      if (disposed) return
      if (userRevision !== userNavigationRevision) continue
      if (navigation.running) { viewportResizeQueued = true; break }
      // Scroll delivery can complete during layout settling without a new
      // gesture. Preserve that current reader, not the pre-scroll snapshot.
      const readerBefore = props.runtime.currentLogicalPosition
      const pin = pendingComposerPinned || (pendingComposerAnchor === null && props.runtime.followTail)
      const anchor = pendingComposerAnchor ?? navigation.committedAnchor
      pendingComposerPinned = false; pendingComposerAnchor = null
      const revision = navigation.begin()
      try {
        if (pin && props.runtime.range.end === props.runtime.logicalCount) await navigation.pinMeasuredEnd(VIEWPORT_POLICY.restoreAttempts, revision)
        else if (anchor) {
          const restored = await navigation.restoreListAnchor(anchor, revision)
          if (restored && navigation.isCurrent(revision)) props.runtime.setReaderPosition(readerBefore, false)
        }
      } finally { navigation.finish(revision) }
      refreshMountedRows()
    }
  } finally {
    viewportResizeRunning = false
    if (viewportResizeQueued && !navigation.running) scheduleViewportResizeReconcile()
    scheduleTailPin()
  }
}
function captureSnapshot(): SessionViewMemory {
  // A view switched away before restoration completes has no new checkpoint.
  if (restoring.value) return props.runtime.rememberedSnapshot
  if (listRef.value && scrollStageRef.value) rememberMeasurements(props.runtime, order.value, listRef.value, scrollStageRef.value)
  const anchor = navigation.captureCommittedAnchor() ?? navigation.committedAnchor
  if (anchor) navigation.committedAnchor = anchor
  const snapshot = anchor ? props.runtime.snapshot(anchor.id, anchor.offsetPx) : props.runtime.snapshot()
  props.runtime.rememberSnapshot(snapshot)
  return snapshot
}
async function restoreSnapshot(): Promise<void> {
  const revision = navigation.begin()
  try {
    const snapshot = props.runtime.rememberedSnapshot
    await settleFrames(3)
    if (!navigation.isCurrent(revision)) return
    const list = listRef.value
    if (!list || order.value.length === 0) { refreshMountedRows(); return }
    if (snapshot.atVisualBottom && props.runtime.range.end === props.runtime.logicalCount) {
      props.runtime.refreshProjection()
      await settleFrames(1)
      if (!navigation.isCurrent(revision)) return
      if (!await navigation.scrollToLogical(props.runtime.logicalCount - 1, 'end', revision)) return
      await navigation.pinMeasuredEnd(VIEWPORT_POLICY.restoreAttempts, revision)
    } else if (snapshot.anchorUnitId && order.value.includes(snapshot.anchorUnitId)) {
      const restored = await navigation.restoreListAnchor({ id: snapshot.anchorUnitId, offsetPx: snapshot.anchorOffsetPx, viewportTopPx: snapshot.anchorOffsetPx }, revision)
      if (restored && navigation.isCurrent(revision)) props.runtime.setReaderPosition(snapshot.logicalPosition, false)
    } else {
      await navigation.scrollToLogical(snapshot.logicalPosition, 'center', revision)
    }
    if (!navigation.isCurrent(revision)) return
    await settleFrames(2)
    navigation.setScrollOffset(listRef.value?.scrollOffset ?? 0)
    if (!snapshot.atVisualBottom && !navigation.committedAnchor) navigation.rememberCommittedAnchor()
    refreshMountedRows()
  } finally {
    navigation.finish(revision)
    if (!disposed) { restoring.value = false; reconcileMeasuredBottom() }
  }
}
function formatAfter(count: number): string { return count.toLocaleString('en-US') }
function attachViewportObserver(): void {
  if (disposed) return
  viewportObserver?.disconnect(); viewportObserver = new ResizeObserver(() => {
    const list = scrollStageRef.value?.querySelector<HTMLElement>('.conversation-vlist')
    // Keep the composer in the same content box as the scrollport, including
    // classic scrollbars. Overlay scrollbars naturally contribute zero.
    scrollbarWidth.value = list ? list.offsetWidth - list.clientWidth : 0
    scheduleViewportResizeReconcile()
  })
  if (scrollStageRef.value) viewportObserver.observe(scrollStageRef.value)
  if (composerShellRef.value) viewportObserver.observe(composerShellRef.value)
  const list = scrollStageRef.value?.querySelector('.conversation-vlist')
  if (list) viewportObserver.observe(list)
  navigation.rememberCommittedAnchor()
}

function scheduleTailPin(): void {
  reconcileMeasuredBottom()
  if (disposed || restoring.value || tailFrame || navigation.running || viewportResizeRunning || !props.runtime.followTail || props.runtime.range.end !== props.runtime.logicalCount) return
  tailFrame = requestAnimationFrame(() => {
    tailFrame = 0
    if (disposed || restoring.value || navigation.running || viewportResizeRunning || !props.runtime.followTail) return
    const list = listRef.value
    if (!list) return
    navigation.clearUserIntent()
    navigation.scrollToOffset(list.scrollSize)
    physicalBottom.value = navigation.remainingToBottom() < VIEWPORT_POLICY.bottomTolerancePx
    if (physicalBottom.value) props.runtime.setReaderPosition(props.runtime.logicalCount - 1, true)
  })
}
function observeContentHeight(): void {
  contentObserver?.disconnect()
  // Virtua's first child owns total measured content height. One observer,
  // no per-row listeners, catches late Markdown/image measurements as well.
  const content = scrollStageRef.value?.querySelector('.conversation-vlist')?.firstElementChild
  if (content) { contentObserver ||= new ResizeObserver(scheduleTailPin); contentObserver.observe(content) }
  scheduleTailPin()
}
watch(listRef, () => { void nextTick().then(observeContentHeight) }, { flush: 'post' })

watch(() => props.uiState.eventRevision, (next, previous) => {
  if (next === previous) return
  void nextTick().then(scheduleTailPin)
})
watch(() => props.uiState.pendingInteraction?.id, () => { interactionAnswer.value = '' })

onMounted(() => {
  unsubscribeOrder = props.runtime.projection.subscribeOrder(() => { order.value = [...props.runtime.projection.order] })
  if (scrollStageRef.value) initialCache.value = restoredMeasurements(props.runtime, order.value, scrollStageRef.value)
  listReady.value = true
  resizeComposer(); void restoreSnapshot().then(attachViewportObserver)
})

onBeforeUnmount(() => {
  disposed = true
  viewportResizeQueued = false
  navigation.invalidate()
  if (!slots.composer) props.runtime.setDraftText(composerText.value)
  props.runtime.rememberSnapshot(captureSnapshot())
  unsubscribeOrder?.(); viewportObserver?.disconnect(); contentObserver?.disconnect()
  if (tailFrame) cancelAnimationFrame(tailFrame)
  if (metricsFrame) cancelAnimationFrame(metricsFrame)
  if (viewportResizeFrame) cancelAnimationFrame(viewportResizeFrame)
})

defineExpose({ captureSnapshot, jumpToMessage, jumpToLatest, shiftBackward, shiftForward, capturePendingLayoutIntent, captureNavigationIntent, jumpToLatestIfUnchanged })
</script>

<template>
  <main class="conversation-shell" data-conversation-engine="vue" :data-session-id="runtime.id" :style="{'--conversation-scrollbar-width': `${scrollbarWidth}px`}">
    <header class="conversation-header">
      <slot name="header-leading" />
      <div class="conversation-heading">
        <div class="conversation-title"><slot name="title"><strong>{{ runtime.title }}</strong></slot></div>
        <div class="conversation-header-context"><slot name="header-context" /></div>
      </div>
      <div class="conversation-header-actions">
        <slot name="header-actions" />
        <slot name="status"><span class="run-status" :class="`indicator-${sessionIndicator}`" :title="runtime.kernel.lastFailure?.message"><i /> {{ statusLabel }}</span></slot>
        <button v-if="!readOnly && uiState.sessionStatus === 'working'" class="header-stop" data-testid="abort-run" type="button" title="Stop run" @click="abortRun">■</button>
      </div>
    </header>

    <div ref="scrollStageRef" class="scroll-stage" data-testid="scrollport" @wheel.capture="onUserWheel" @pointerdown.capture="onUserPointerDown" @touchmove.capture="onUserTouchMove" @keydown.capture="onUserKeydown">
      <slot name="viewport-overlay" :mounted-rows="mountedRows" :follow-label="followLabel" :ui-state="uiState" :restoring="restoring" :history-pending="shifting || historyRequests > 0" :history-error="historyError" :retry-history="retryHistory" />

      <div v-if="order.length === 0" class="empty-conversation" data-testid="empty-conversation"><slot name="empty-state"><div class="empty-agent-mark">✦</div><h2>Start a conversation</h2><p>Send a prompt to begin. Session state is independent from the mounted viewport.</p></slot></div>

      <VList v-else-if="listReady" :key="`${runtime.id}:${uiState.virtualEpoch}`" ref="listRef" class="conversation-vlist" :style="{ visibility: restoring ? 'hidden' : undefined }" :cache="uiState.virtualEpoch === initialEpoch ? initialCache : undefined" :data="order" :buffer-size="VIRTUAL_BUFFER_PX" :shift="runtime.shiftMode" :item-props="itemProps" @scroll="onVirtualScroll" @scroll-end="onVirtualScrollEnd">
        <template #default="{ item }"><ConversationNodeSeat :runtime="runtime" :node-id="item" :renderers="renderers" :assistant-name="assistantName"><template v-if="slots['node-actions']" #actions="scope"><slot name="node-actions" v-bind="scope" /></template></ConversationNodeSeat></template>
      </VList>

      <button v-if="showLatest" class="jump-latest" data-testid="jump-latest" type="button" @click="jumpToLatest"><span>↓</span><strong>{{ latestLabel || 'Latest' }}</strong><em v-if="messagesAfter > 0" data-testid="messages-after">{{ formatAfter(messagesAfter) }}</em></button>
    </div>

    <footer ref="composerShellRef" class="composer-shell" data-testid="composer-shell">
      <slot name="composer">
      <div
        v-if="uiState.pendingInteraction"
        class="pending-interaction"
        data-testid="pending-interaction"
        :data-kind="uiState.pendingInteraction.kind"
        :data-interaction-id="uiState.pendingInteraction.id"
        :data-call-id="uiState.pendingInteraction.kind === 'approval' ? (uiState.pendingInteraction.callId ?? '') : ''"
      >
        <div class="pending-icon">!</div>
        <div class="pending-copy"><strong>{{ uiState.pendingInteraction.title }}</strong><p>{{ uiState.pendingInteraction.detail }}</p></div>
        <template v-if="uiState.pendingInteraction.kind === 'approval'">
          <div class="pending-actions"><button class="secondary" data-testid="deny-interaction" type="button" @click="resolveApproval(false)">Deny</button><button data-testid="approve-interaction" type="button" @click="resolveApproval(true)">Approve</button></div>
        </template>
        <template v-else>
          <div class="pending-question-response">
            <input v-model="interactionAnswer" data-testid="question-answer" type="text" placeholder="Type an answer…" @keydown.enter.prevent="resolveQuestion(interactionAnswer)" />
            <div class="pending-actions"><button class="secondary" data-testid="deny-interaction" type="button" @click="resolveQuestion(null)">Skip</button><button data-testid="approve-interaction" type="button" :disabled="!canAnswerQuestion" @click="resolveQuestion(interactionAnswer)">Answer</button></div>
          </div>
        </template>
      </div>
      <div v-else-if="sessionIndicator === 'failed'" class="turn-outcome-banner failure" data-testid="last-turn-failure"><strong>Last turn failed</strong><span>{{ runtime.kernel.lastFailure?.code }} · {{ runtime.kernel.lastFailure?.message }}</span></div>
      <div v-else-if="sessionIndicator === 'max-tokens'" class="turn-outcome-banner warning"><strong>Last turn reached the output-token limit</strong><span>You can continue this session with another prompt.</span></div>
      <div v-if="uiState.queuedPrompts > 0" class="queue-banner" data-testid="queue-banner">{{ uiState.queuedPrompts }} follow-up{{ uiState.queuedPrompts === 1 ? '' : 's' }} queued for this session</div>
      <div class="composer-box">
        <textarea ref="composerInputRef" v-model="composerText" data-testid="composer-input" rows="1" :placeholder="placeholder || composerPlaceholder" :disabled="readOnly || Boolean(uiState.pendingInteraction)" @input="onComposerInput" @keydown.enter.exact.prevent="sendComposer" />
        <div class="composer-actions">
          <div class="composer-extension"><slot name="composer-tools" /></div>
          <div><span class="context-meter" data-testid="stats-context">{{ contextPercent ?? 0 }}% context</span><button v-if="!readOnly && uiState.sessionStatus === 'working'" class="stop-button" data-testid="composer-stop" type="button" title="Stop run" @click="abortRun">■</button><button class="send-button" :class="{ queued: uiState.sessionStatus === 'working' }" :disabled="!canSend" type="button" :title="sendLabel" @click="sendComposer">{{ uiState.sessionStatus === 'working' ? '↗' : '↑' }}</button></div>
        </div>
      </div>
      <div class="session-stats-line" data-testid="session-stats-line">
        <span>{{ runtime.kernel.turnCount.toLocaleString() }} turns · {{ runtime.kernel.stepCount.toLocaleString() }} steps</span>
        <span v-if="cacheHit !== null" data-testid="stats-cache-hit">cache {{ cacheHit }}%</span>
        <span data-testid="stats-input-tokens">in {{ formatTokens(billedInput) }}</span>
        <span data-testid="stats-output-tokens">out {{ formatTokens(tokenUsage.outputTokens) }}</span>
        <span v-if="tokenUsage.reasoningTokens > 0">reasoning {{ formatTokens(tokenUsage.reasoningTokens) }}</span>
      </div>
      <span class="composer-hint">Enter to send</span>
      </slot>
    </footer>
  </main>
</template>
