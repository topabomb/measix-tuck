import type { VListHandle } from 'virtua/vue'
import type { ConversationSessionRuntime } from '../runtime/session-runtime'
import {
  VIEWPORT_POLICY,
  isMessageCommittedVisible,
  remainingToBottom as remainingToBottomForPort,
  selectCommittedAnchor,
  type CommittedViewportAnchor,
  type ViewportRowSample,
} from '../viewport/contracts'

export interface ViewportNavigationDependencies {
  runtime: ConversationSessionRuntime
  getOrder(): readonly string[]
  getList(): VListHandle | null
  getStage(): HTMLElement | null
  getVirtualEpoch(): number
  settleFrames(count?: number): Promise<void>
  onNavigationSettled(): void
}

/**
 * Vue/Virtua physical-navigation transaction owner.
 *
 * Semantic reader/follow state remains in ConversationSessionRuntime. This
 * controller owns only mounted geometry, user-scroll intent and latest-wins
 * navigation transactions so the SFC can stay a rendering/composer adapter.
 */
export class ViewportNavigationController {
  #revision = 0
  #running = false
  #userIntentUntil = 0
  #scrollDirection: -1 | 0 | 1 = 0
  #lastScrollOffset = 0
  #committedAnchor: CommittedViewportAnchor | null = null

  constructor(private readonly deps: ViewportNavigationDependencies) {}

  get running(): boolean { return this.#running }
  get scrollDirection(): -1 | 0 | 1 { return this.#scrollDirection }
  get committedAnchor(): CommittedViewportAnchor | null { return this.#committedAnchor }
  set committedAnchor(anchor: CommittedViewportAnchor | null) { this.#committedAnchor = anchor }

  begin(): number {
    this.#revision += 1
    this.#running = true
    return this.#revision
  }

  isCurrent(revision?: number): boolean { return revision === undefined || revision === this.#revision }

  finish(revision: number): void {
    if (!this.isCurrent(revision)) return
    this.#running = false
    this.deps.onNavigationSettled()
  }

  invalidate(): void {
    this.#revision += 1
    this.#running = false
  }

  markUserIntent(direction: -1 | 0 | 1): void {
    this.#userIntentUntil = performance.now() + VIEWPORT_POLICY.userIntentMs
    this.#scrollDirection = direction
  }

  clearUserIntent(): void {
    this.#userIntentUntil = 0
    this.#scrollDirection = 0
  }

  hasUserIntent(): boolean { return performance.now() < this.#userIntentUntil }

  recordScrollOffset(offset: number): void {
    const inferred: -1 | 0 | 1 = offset < this.#lastScrollOffset ? -1 : offset > this.#lastScrollOffset ? 1 : 0
    this.#lastScrollOffset = offset
    if (this.hasUserIntent() && this.#scrollDirection === 0 && inferred !== 0) this.#scrollDirection = inferred
  }

  setScrollOffset(offset: number): void { this.#lastScrollOffset = offset }

  // Own every imperative write here. Virtua's public scroll methods keep an
  // asynchronous measurement-driven scroll alive after returning; invalidating
  // our transaction cannot cancel that hidden writer. Its measurements remain
  // authoritative, but our writes are synchronous and revision-scoped.
  scrollToOffset(offset: number): void {
    const element = this.deps.getStage()?.querySelector<HTMLElement>('.conversation-vlist')
    if (element) element.scrollTop = offset
  }

  scrollByOffset(delta: number): void {
    const element = this.deps.getStage()?.querySelector<HTMLElement>('.conversation-vlist')
    if (element) this.scrollToOffset(element.scrollTop + delta)
  }

  scrollToIndex(index: number, align: 'start' | 'center' | 'end', offset = 0): void {
    const list = this.deps.getList()
    const element = this.deps.getStage()?.querySelector<HTMLElement>('.conversation-vlist')
    if (!list || !element) return
    const adjustment = align === 'start' ? 0 : (list.getItemSize(index) - element.clientHeight) / (align === 'center' ? 2 : 1)
    this.scrollToOffset(list.getItemOffset(index) + adjustment + offset)
  }

  renderedRow(id: string): HTMLElement | null {
    const stage = this.deps.getStage()
    if (!stage) return null
    for (const row of stage.querySelectorAll<HTMLElement>('[data-render-unit]')) if (row.dataset.renderUnit === id) return row
    return null
  }

  sampleRows(): ViewportRowSample[] {
    const stage = this.deps.getStage()
    if (!stage) return []
    return [...stage.querySelectorAll<HTMLElement>('[data-virtual-item="true"]')].flatMap(row => {
      const id = row.dataset.renderUnit ?? ''
      const node = this.deps.runtime.projection.getNode(id)
      if (!node) return []
      const rect = row.getBoundingClientRect()
      return [{ id, messageIndex: node.messageIndex, top: rect.top, bottom: rect.bottom }]
    })
  }

  captureCommittedAnchor(): CommittedViewportAnchor | null {
    const stage = this.deps.getStage()
    if (!stage) return null
    const viewport = stage.getBoundingClientRect()
    return selectCommittedAnchor(this.sampleRows(), viewport.top, viewport.bottom, this.deps.runtime.currentLogicalPosition)
  }

  rememberCommittedAnchor(): void {
    const runtime = this.deps.runtime
    if (runtime.atVisualBottom || runtime.followTail) {
      this.#committedAnchor = null
      return
    }
    const anchor = this.captureCommittedAnchor()
    if (anchor) this.#committedAnchor = anchor
  }

  remainingToBottom(): number {
    const element = this.deps.getStage()?.querySelector<HTMLElement>('.conversation-vlist')
    if (element) return Math.max(0, element.scrollHeight - element.scrollTop - element.clientHeight)
    const list = this.deps.getList()
    if (!list) return Number.POSITIVE_INFINITY
    return remainingToBottomForPort(list)
  }

  updateReaderFromUserScroll(offset = this.deps.getList()?.scrollOffset ?? 0): void {
    const runtime = this.deps.runtime
    const list = this.deps.getList()
    const order = this.deps.getOrder()
    if (!list || order.length === 0 || runtime.logicalCount <= 0) return
    const atBottom = this.remainingToBottom() < VIEWPORT_POLICY.bottomTolerancePx && runtime.range.end === runtime.logicalCount
    if (atBottom) {
      runtime.setReaderPosition(runtime.logicalCount - 1, true)
      this.#committedAnchor = null
      return
    }
    const bottomProbe = Math.max(0, Math.min(list.scrollSize - 1, offset + Math.max(1, list.viewportSize - 2)))
    const index = Math.max(0, Math.min(order.length - 1, list.findItemIndex(bottomProbe)))
    const node = runtime.projection.getNode(order[index]!)
    if (node) runtime.setReaderPosition(node.messageIndex, false)
  }

  async restoreListAnchor(anchor: CommittedViewportAnchor, revision?: number): Promise<boolean> {
    if (!this.isCurrent(revision)) return false
    const list = this.deps.getList()
    const order = this.deps.getOrder()
    if (!list) return false
    const index = order.indexOf(anchor.id)
    if (index < 0) return false
    let stableFrames = 0
    this.scrollToIndex(index, 'start', -anchor.offsetPx)
    for (let attempt = 0; attempt < VIEWPORT_POLICY.restoreAttempts; attempt += 1) {
      await this.deps.settleFrames(1)
      if (!this.isCurrent(revision)) return false
      const currentList = this.deps.getList()
      const stage = this.deps.getStage()
      const row = this.renderedRow(anchor.id)
      if (!currentList || !stage || !row) {
        stableFrames = 0
        this.scrollToIndex(index, 'start', -anchor.offsetPx)
        continue
      }
      const currentTop = row.getBoundingClientRect().top - stage.getBoundingClientRect().top
      const delta = currentTop - anchor.viewportTopPx
      if (Math.abs(delta) < VIEWPORT_POLICY.anchorTolerancePx) {
        stableFrames += 1
        if (stableFrames >= VIEWPORT_POLICY.stableLayoutFrames) break
        continue
      }
      stableFrames = 0
      this.scrollByOffset(delta)
    }
    await this.deps.settleFrames(1)
    if (!this.isCurrent(revision)) return false
    const stage = this.deps.getStage()
    const row = this.renderedRow(anchor.id)
    if (!stage || !row) return false
    const drift = Math.abs((row.getBoundingClientRect().top - stage.getBoundingClientRect().top) - anchor.viewportTopPx)
    if (drift >= VIEWPORT_POLICY.anchorTolerancePx) return false
    this.#committedAnchor = anchor
    return true
  }

  async pinMeasuredEnd(maxFrames = VIEWPORT_POLICY.restoreAttempts, revision?: number): Promise<boolean> {
    const runtime = this.deps.runtime
    if (runtime.logicalCount <= 0 || !this.isCurrent(revision)) return false
    let stableFrames = 0
    let previousScrollSize = -1
    let previousViewportSize = -1
    for (let attempt = 0; attempt < maxFrames; attempt += 1) {
      await this.deps.settleFrames(1)
      if (!this.isCurrent(revision)) return false
      const list = this.deps.getList()
      if (!list) return false
      this.scrollToOffset(list.scrollSize)
      await this.deps.settleFrames(1)
      if (!this.isCurrent(revision)) return false
      const current = this.deps.getList()
      if (!current) return false
      const physical = this.deps.getStage()?.querySelector<HTMLElement>('.conversation-vlist')
      const currentViewportSize = physical?.clientHeight ?? current.viewportSize
      const geometryStable = Math.abs(current.scrollSize - previousScrollSize) < 0.5
        && Math.abs(currentViewportSize - previousViewportSize) < 0.5
      const pinned = this.remainingToBottom() < 1
      stableFrames = pinned && geometryStable ? stableFrames + 1 : 0
      previousScrollSize = current.scrollSize
      previousViewportSize = currentViewportSize
      if (stableFrames >= VIEWPORT_POLICY.stableLayoutFrames) break
    }
    if (!this.isCurrent(revision)) return false
    this.#lastScrollOffset = this.deps.getList()?.scrollOffset ?? 0
    const physicallyAtBottom = this.remainingToBottom() < VIEWPORT_POLICY.bottomTolerancePx
    if (physicallyAtBottom) {
      runtime.setReaderPosition(runtime.logicalCount - 1, true)
      this.#committedAnchor = null
    }
    return physicallyAtBottom
  }

  findMessageUnitIndex(target: number, preferLast = false): number {
    const runtime = this.deps.runtime
    const order = this.deps.getOrder()
    let found = -1
    for (let i = 0; i < order.length; i += 1) {
      if (runtime.projection.getNode(order[i]!)?.messageIndex !== target) continue
      found = i
      if (!preferLast) break
    }
    return found
  }

  targetIsCommittedVisible(target: number): boolean {
    const stage = this.deps.getStage()
    if (!stage) return false
    const viewport = stage.getBoundingClientRect()
    return isMessageCommittedVisible(this.sampleRows(), target, viewport.top, viewport.bottom)
  }

  async scrollToLogical(target: number, align: 'start' | 'center' | 'end', revision: number): Promise<boolean> {
    const runtime = this.deps.runtime
    if (runtime.logicalCount <= 0 || !this.isCurrent(revision)) return false
    const semanticTailNavigation = align === 'end' && target === runtime.logicalCount - 1
    for (let attempt = 0; attempt < VIEWPORT_POLICY.jumpAttempts; attempt += 1) {
      await this.deps.settleFrames(1)
      if (!this.isCurrent(revision)) return false
      const list = this.deps.getList()
      if (!list) continue
      // Producers may append an empty assistant placeholder before the first
      // token. It has no visible row. Tail navigation targets the rendered end,
      // while the committed reader still includes that logical placeholder.
      const order = this.deps.getOrder()
      const index = semanticTailNavigation ? order.length - 1 : this.findMessageUnitIndex(target)
      if (index < 0) continue
      const visibleTarget = semanticTailNavigation ? runtime.projection.getNode(order[index]!)!.messageIndex : target
      this.scrollToIndex(index, align)
      await this.deps.settleFrames(2)
      if (!this.isCurrent(revision) || !this.targetIsCommittedVisible(visibleTarget)) continue
      await this.deps.settleFrames(VIEWPORT_POLICY.stableLayoutFrames)
      if (!this.isCurrent(revision) || !this.targetIsCommittedVisible(visibleTarget)) continue
      const current = this.deps.getList()
      if (!current) continue
      this.#lastScrollOffset = current.scrollOffset
      runtime.setReaderPosition(target, semanticTailNavigation && this.remainingToBottom() < VIEWPORT_POLICY.bottomTolerancePx)
      if (semanticTailNavigation) this.#committedAnchor = null
      else {
        const anchor = this.captureCommittedAnchor()
        if (anchor) this.#committedAnchor = anchor
      }
      return true
    }
    return false
  }

  async waitForJumpEpoch(previousEpoch: number, previousHandle: VListHandle | null, target: number, revision: number, tail = false): Promise<boolean> {
    for (let attempt = 0; attempt < VIEWPORT_POLICY.jumpAttempts + 2; attempt += 1) {
      await this.deps.settleFrames(1)
      if (!this.isCurrent(revision)) return false
      const epochReady = this.deps.getVirtualEpoch() > previousEpoch && this.deps.getVirtualEpoch() === this.deps.runtime.virtualEpoch
      const orderReady = tail ? this.deps.runtime.range.end === this.deps.runtime.logicalCount : this.findMessageUnitIndex(target) >= 0
      const current = this.deps.getList()
      const handleReady = tail && this.deps.getOrder().length === 0 || Boolean(current) && (current !== previousHandle || attempt >= 4)
      if (epochReady && orderReady && handleReady) return true
    }
    return false
  }
}
