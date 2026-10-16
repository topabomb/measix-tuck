import { BatchedNotifier, type Unsubscribe } from '../core/notifier'
import type { AppendCanonicalMessage, LogicalMessage } from '../model/conversation'
import { cloneBlocks } from '../model/message-mutations'
import type {
  ConversationDescriptor,
  ConversationHistorySource,
  InteractionResolution,
  LlmFailure,
  PendingInteraction,
  SessionContextStats,
  SessionStatus,
  TokenUsage,
  TurnEndReasonKind,
  WorkPlan,
} from './contracts'
import { normalizeTokenUsage } from './session-semantics'

export type SessionKernelEventKind = 'content' | 'append' | 'status' | 'queue' | 'interaction' | 'foreground' | 'usage' | 'plan' | 'title'
export type SessionKernelContentPatch =
  | { kind: 'append-markdown'; blockId: string; delta: string }
  | { kind: 'append-reasoning'; blockId: string; delta: string }
  | { kind: 'append-terminal'; blockId: string; delta: string }
export interface SessionKernelEvent {
  kind: SessionKernelEventKind
  messageIndex?: number
  contentPatch?: SessionKernelContentPatch
}

/**
 * Canonical session truth.
 *
 * This kernel stores normalized messages and lifecycle/accounting facts but never
 * invents provider output, execution targets, default assistant blocks, token estimates
 * or product copy. Execution adapters own those decisions and publish them through
 * these semantic APIs.
 */
export class ConversationSessionKernel {
  readonly id: string
  #title: string
  readonly history: ConversationHistorySource

  #appended: LogicalMessage[] = []
  #overrides = new Map<number, LogicalMessage>()
  #status: SessionStatus
  #pendingInteraction: PendingInteraction | null
  #activePlan: WorkPlan | null
  #queuedPrompts: string[] = []
  #foreground = false
  #unread = false
  #notifier = new BatchedNotifier()
  #eventListeners = new Set<(event: SessionKernelEvent) => void>()
  #lastEvent: SessionKernelEvent = { kind: 'status' }
  #currentAssistantIndex: number | null = null
  #lastTurnReason: TurnEndReasonKind | null
  #lastFailure: LlmFailure | null
  #usage: TokenUsage
  #context: SessionContextStats
  #turnCount: number
  #stepCount: number
  #runStartedAt = 0
  #firstOutputAt = 0
  #lastTurnDurationMs = 0
  #lastTtftMs = 0

  constructor(descriptor: ConversationDescriptor, history: ConversationHistorySource) {
    this.id = descriptor.id
    this.#title = descriptor.title
    this.history = history
    this.#status = descriptor.status
    this.#pendingInteraction = clonePendingInteraction(descriptor.pendingInteraction ?? null)
    this.#activePlan = cloneWorkPlan(descriptor.activePlan ?? null)
    if ((this.#status === 'waiting') !== (this.#pendingInteraction !== null)) {
      throw new Error('waiting session state requires exactly one pending interaction')
    }
    this.#lastTurnReason = descriptor.lastTurnReason ?? null
    this.#lastFailure = descriptor.lastFailure ? { ...descriptor.lastFailure } : null
    this.#usage = normalizeTokenUsage(descriptor.usage)
    this.#context = descriptor.context
      ? { ...descriptor.context }
      : { projectedTokens: 0, contextWindow: 128_000 }
    this.#turnCount = Math.max(0, Math.floor(descriptor.turnCount ?? 0))
    this.#stepCount = Math.max(this.#turnCount, Math.floor(descriptor.stepCount ?? this.#turnCount))

    if (descriptor.status === 'working') {
      this.#lastTurnReason = null
      this.#runStartedAt = now()
      if (descriptor.activeAssistantIndex !== undefined && descriptor.activeAssistantIndex !== null) {
        this.#assertAssistantMessage(descriptor.activeAssistantIndex)
        this.#currentAssistantIndex = descriptor.activeAssistantIndex
      }
    }
  }

  subscribe(listener: () => void): Unsubscribe { return this.#notifier.subscribe(listener) }
  subscribeEvents(listener: (event: SessionKernelEvent) => void): Unsubscribe {
    this.#eventListeners.add(listener)
    return () => this.#eventListeners.delete(listener)
  }

  get lastEvent(): SessionKernelEvent { return this.#lastEvent }
  get title(): string { return this.#title }
  setTitle(title: string): void {
    if (this.#title === title) return
    this.#title = title
    this.#emit({ kind: 'title' }, false)
  }
  get status(): SessionStatus { return this.#status }
  get pendingInteraction(): PendingInteraction | null { return clonePendingInteraction(this.#pendingInteraction) }
  get activePlan(): WorkPlan | null { return cloneWorkPlan(this.#activePlan) }
  get queuedPrompts(): number { return this.#queuedPrompts.length }
  get currentAssistantIndex(): number | null { return this.#currentAssistantIndex }
  get count(): number { return this.history.count + this.#appended.length }
  get unread(): boolean { return this.#unread }
  get foreground(): boolean { return this.#foreground }
  get lastTurnReason(): TurnEndReasonKind | null { return this.#lastTurnReason }
  get lastFailure(): LlmFailure | null { return this.#lastFailure ? { ...this.#lastFailure } : null }
  get usage(): TokenUsage { return { ...this.#usage } }
  get context(): SessionContextStats { return { ...this.#context } }
  get turnCount(): number { return this.#turnCount }
  get stepCount(): number { return this.#stepCount }
  get lastTurnDurationMs(): number { return this.#lastTurnDurationMs }
  get lastTtftMs(): number { return this.#lastTtftMs }

  get summary(): ConversationDescriptor {
    return {
      id: this.id,
      title: this.title,
      status: this.#status,
      logicalCount: this.count,
      unread: this.#unread,
      queuedPrompts: this.queuedPrompts,
      pendingInteraction: clonePendingInteraction(this.#pendingInteraction),
      activePlan: this.activePlan,
      lastTurnReason: this.#lastTurnReason,
      lastFailure: this.lastFailure,
      usage: this.usage,
      context: this.context,
      turnCount: this.#turnCount,
      stepCount: this.#stepCount,
      activeAssistantIndex: this.#currentAssistantIndex,
    }
  }

  setForeground(value: boolean): void {
    const changed = this.#foreground !== value || (value && this.#unread)
    this.#foreground = value
    if (value) this.#unread = false
    if (changed) this.#emit({ kind: 'foreground' }, false)
  }

  /**
   * Explicit producer/session state. Historical plan blocks are snapshots and this
   * value is never inferred by scanning message order or rendered DOM.
   */
  setActivePlan(plan: WorkPlan | null): void {
    const next = cloneWorkPlan(plan)
    if (sameWorkPlan(this.#activePlan, next)) return
    this.#activePlan = next
    this.#emit({ kind: 'plan' })
  }

  getMessage(index: number): LogicalMessage {
    if (!Number.isInteger(index) || index < 0 || index >= this.count) throw new RangeError(`message index ${index} outside 0..${this.count - 1}`)
    const override = this.#overrides.get(index)
    if (override) return override
    if (index < this.history.count) {
      const found = this.history.loadRange(index, 1)[0]
      if (!found) throw new RangeError(`history message ${index} missing`)
      return found
    }
    const message = this.#appended[index - this.history.count]
    if (!message) throw new RangeError(`appended message ${index} missing`)
    return message
  }

  loadRange(start: number, count: number): readonly LogicalMessage[] {
    const safeStart = Math.max(0, Math.min(this.count, Math.floor(start)))
    const safeEnd = Math.max(safeStart, Math.min(this.count, safeStart + Math.max(0, Math.floor(count))))
    const out = new Array<LogicalMessage>(safeEnd - safeStart)
    for (let index = safeStart; index < safeEnd; index += 1) out[index - safeStart] = this.getMessage(index)
    return out
  }

  appendCanonicalMessages(entries: readonly AppendCanonicalMessage[]): readonly number[] {
    if (entries.length === 0) return []
    const indexes: number[] = []
    const previous = this.count > 0 ? this.getMessage(this.count - 1) : null
    let previousTurnId = previous?.turnId ?? null
    let previousStepId = previous?.stepId ?? null

    for (const entry of entries) {
      const index = this.count
      indexes.push(index)

      if (entry.turnId !== previousTurnId) this.#turnCount += 1
      if (entry.stepId) {
        if (entry.stepId !== previousStepId) this.#stepCount += 1
      } else {
        this.#stepCount += 1
      }

      this.#writeMessage(index, {
        id: `${this.id}:m-${index}`,
        index,
        turnId: entry.turnId,
        stepId: entry.stepId,
        role: entry.role,
        blocks: cloneBlocks(entry.blocks),
        revision: 0,
        live: entry.live,
      })
      previousTurnId = entry.turnId
      previousStepId = entry.stepId ?? null
    }
    this.#emit({ kind: 'append', messageIndex: indexes[indexes.length - 1] })
    return indexes
  }

  replaceCanonicalMessage(index: number, message: LogicalMessage, contentPatch?: SessionKernelContentPatch): void {
    const current = this.getMessage(index)
    if (
      message.index !== index ||
      message.id !== current.id ||
      message.turnId !== current.turnId ||
      message.stepId !== current.stepId ||
      message.role !== current.role
    ) throw new Error(`canonical identity changed for message ${index}`)

    const revision = Math.max((current.revision ?? 0) + 1, message.revision ?? 0)
    this.#writeMessage(index, { ...message, revision, blocks: cloneBlocks(message.blocks) }, contentPatch?.delta)
    if (contentPatch) this.#recordFirstOutput()
    this.#emit({ kind: 'content', messageIndex: index, contentPatch })
  }

  startExecution(currentAssistantIndex: number | null = null): boolean {
    if (this.#pendingInteraction || this.#status === 'working') return false
    if (currentAssistantIndex !== null) this.#assertAssistantMessage(currentAssistantIndex)
    this.#currentAssistantIndex = currentAssistantIndex
    this.#status = 'working'
    this.#lastTurnReason = null
    this.#lastFailure = null
    this.#runStartedAt = now()
    this.#firstOutputAt = 0
    this.#emit({ kind: 'status', messageIndex: currentAssistantIndex ?? undefined })
    return true
  }

  continueExecutionAt(currentAssistantIndex: number): void {
    if (this.#status !== 'working') throw new Error('cannot continue an execution that is not working')
    this.#assertAssistantMessage(currentAssistantIndex)
    if (this.#currentAssistantIndex === currentAssistantIndex) return
    this.#currentAssistantIndex = currentAssistantIndex
    this.#emit({ kind: 'status', messageIndex: currentAssistantIndex })
  }

  requestInteraction(interaction: PendingInteraction): void {
    if (this.#pendingInteraction) throw new Error(`interaction ${this.#pendingInteraction.id} is already pending`)
    if (this.#status !== 'working') throw new Error('cannot request an interaction when execution is not working')
    const index = this.#currentAssistantIndex
    this.#pendingInteraction = clonePendingInteraction(interaction)
    this.#status = 'waiting'
    this.#lastTurnReason = null
    this.#lastFailure = null
    this.#currentAssistantIndex = null
    if (this.#runStartedAt > 0) this.#lastTurnDurationMs = Math.max(0, now() - this.#runStartedAt)
    this.#runStartedAt = 0
    this.#firstOutputAt = 0
    this.#emit({ kind: 'interaction', messageIndex: index ?? undefined })
  }

  finishExecution(reason: TurnEndReasonKind, failure: LlmFailure | null = null): void {
    if (this.#pendingInteraction) throw new Error(`cannot finish execution while interaction ${this.#pendingInteraction.id} is pending`)
    const index = this.#currentAssistantIndex
    this.#status = 'idle'
    this.#lastTurnReason = reason
    this.#lastFailure = failure ? { ...failure } : null
    this.#currentAssistantIndex = null
    if (this.#runStartedAt > 0) this.#lastTurnDurationMs = Math.max(0, now() - this.#runStartedAt)
    this.#runStartedAt = 0
    this.#firstOutputAt = 0
    this.#emit({ kind: 'status', messageIndex: index ?? undefined })
  }

  setAccounting(usage: Partial<TokenUsage>, context: SessionContextStats = this.#context): void {
    this.#usage = normalizeTokenUsage(usage)
    this.#context = {
      projectedTokens: Math.max(0, Math.floor(context.projectedTokens)),
      contextWindow: Math.max(0, Math.floor(context.contextWindow)),
    }
    this.#emit({ kind: 'usage' })
  }

  enqueue(prompt: string): boolean {
    const text = prompt.trim()
    if (!text || this.#pendingInteraction) return false
    this.#queuedPrompts.push(text)
    this.#emit({ kind: 'queue' })
    return true
  }

  dequeue(): string | null {
    const next = this.#queuedPrompts.shift() ?? null
    if (next !== null) this.#emit({ kind: 'queue' })
    return next
  }

  clearQueue(): void {
    if (this.#queuedPrompts.length === 0) return
    this.#queuedPrompts = []
    this.#emit({ kind: 'queue' })
  }

  resolveInteraction(resolution: InteractionResolution): void {
    const pending = this.#pendingInteraction
    if (!pending) return
    if (pending.id !== resolution.interactionId) {
      throw new Error(`interaction ${resolution.interactionId} is stale; current interaction is ${pending.id}`)
    }
    if (pending.kind !== resolution.kind) throw new Error(`interaction ${pending.id} expects ${pending.kind} resolution`)

    this.#pendingInteraction = null
    this.#status = 'idle'
    this.#lastTurnReason = null
    this.#lastFailure = null
    this.#emit({ kind: 'interaction' })
  }

  #assertAssistantMessage(index: number): void {
    const message = this.getMessage(index)
    if (message.role !== 'assistant') throw new Error(`execution target ${index} must be an assistant message`)
  }

  #writeMessage(index: number, message: LogicalMessage, appendedText?: string): void {
    if (this.history.storeCanonicalMessage) this.history.storeCanonicalMessage(message, appendedText)
    else if (index < this.history.count) this.#overrides.set(index, message)
    else this.#appended[index - this.history.count] = message
  }

  #recordFirstOutput(): void {
    if (this.#firstOutputAt !== 0 || this.#runStartedAt === 0) return
    this.#firstOutputAt = now()
    this.#lastTtftMs = Math.max(0, this.#firstOutputAt - this.#runStartedAt)
  }

  #touchUnread(): void { if (!this.#foreground) this.#unread = true }
  #emit(event: SessionKernelEvent, markUnread = true): void {
    this.#lastEvent = event
    if (markUnread) this.#touchUnread()
    for (const listener of this.#eventListeners) listener(event)
    this.#notifier.markDirty()
  }
}

function clonePendingInteraction(interaction: PendingInteraction | null): PendingInteraction | null {
  return interaction ? { ...interaction } : null
}

function cloneWorkPlan(plan: WorkPlan | null): WorkPlan | null {
  return plan ? { title: plan.title, items: plan.items.map(item => ({ ...item })) } : null
}

function sameWorkPlan(left: WorkPlan | null, right: WorkPlan | null): boolean {
  if (left === right) return true
  if (!left || !right || left.title !== right.title || left.items.length !== right.items.length) return false
  return left.items.every((item, index) => {
    const other = right.items[index]
    return other !== undefined && item.id === other.id && item.text === other.text && item.status === other.status
  })
}

function now(): number { return typeof performance !== 'undefined' ? performance.now() : Date.now() }
