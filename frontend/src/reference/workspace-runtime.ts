import type { ConversationDescriptor } from '../engine/conversation/contracts'
import { deriveSessionIndicator } from '../engine/conversation/session-semantics'
import { ConversationSessionKernel, type SessionKernelEvent } from '../engine/conversation/session-kernel'
import { BatchedNotifier, type Unsubscribe } from '../engine/core/notifier'
import { ConversationSessionRuntime } from '../engine/runtime/session-runtime'
import type { SessionViewMemory } from '../engine/viewport/state'
import { createChildScenarioTail } from './child-scenarios'
import { SyntheticHistoryAdapter } from './history-adapter'
import { createLifecycleScenarioTail, isLifecycleScenario } from './lifecycle-scenarios'
import { createScenarioTail, type ReferenceScenarioKey } from './session-scenarios'
import { SyntheticStreamController } from './stream-controller'
import {
  newSessionSeed,
  RECENT_SESSIONS,
  type ReferenceSessionDescriptor,
  type ReferenceSessionSeed,
} from './workspace-fixtures'

export const HOT_SESSION_LIMIT = 3

/**
 * Reference composition only. Realistic scenario tails, fake ages, parent/child navigation
 * metadata and synthetic playback stay here; reusable Kernel/Runtime behavior remains
 * under engine/**.
 */
export class ReferenceWorkspaceRuntime {
  #kernels = new Map<string, ConversationSessionKernel>()
  #executions = new Map<string, SyntheticStreamController>()
  #snapshots = new Map<string, SessionViewMemory>()
  #runtimes = new Map<string, ConversationSessionRuntime>()
  #ages = new Map<string, string>()
  #parentSessionIds = new Map<string, string>()
  #listedSessionIds = new Set<string>()
  #lastAccess = new Map<string, number>()
  #kernelUnsubs = new Map<string, Unsubscribe>()
  #summarySignatures = new Map<string, string>()
  #order: string[] = []
  #notifier = new BatchedNotifier()
  #clock = 0
  #newCounter = 0
  #activeSessionId = 'agent-loop'

  constructor() {
    for (const descriptor of RECENT_SESSIONS) this.#register(descriptor)
    this.#activeSessionId = RECENT_SESSIONS[0]!.id
    this.kernelFor(this.#activeSessionId).setForeground(true)
    this.ensureRuntime(this.#activeSessionId)
    // Seeded working sessions begin a fresh synthetic Reference playback here. This
    // initializes Reference-only Plan/delegation coordinates once; later Pause/Resume
    // uses start(false) and preserves the same run state.
    for (const kernel of this.#kernels.values()) if (kernel.status === 'working') this.executionFor(kernel.id).start(true)
  }

  subscribe(listener: () => void): Unsubscribe { return this.#notifier.subscribe(listener) }
  get descriptors(): readonly ReferenceSessionDescriptor[] {
    return this.#order
      .filter(id => this.#listedSessionIds.has(id))
      .map(id => ({ ...this.kernelFor(id).summary, age: this.#ages.get(id) ?? 'now' }))
  }
  get activeSessionId(): string { return this.#activeSessionId }
  get activeSession(): ConversationSessionRuntime { return this.ensureRuntime(this.#activeSessionId) }
  get activeKernel(): ConversationSessionKernel { return this.kernelFor(this.#activeSessionId) }
  get hotSessionCount(): number { return this.#runtimes.size }
  get hotSessionIds(): readonly string[] { return [...this.#runtimes.keys()] }
  get runningSessionCount(): number { return this.#countIndicator('working') }
  get blockedSessionCount(): number { return this.#countIndicator('blocked') }
  get failedSessionCount(): number { return this.#countIndicator('failed') }

  hasSession(id: string): boolean { return this.#kernels.has(id) }
  parentSessionId(id: string): string | null { return this.#parentSessionIds.get(id) ?? null }

  kernelFor(id: string): ConversationSessionKernel {
    const kernel = this.#kernels.get(id)
    if (!kernel) throw new Error(`Unknown conversation session: ${id}`)
    return kernel
  }

  executionFor(id: string): SyntheticStreamController {
    const execution = this.#executions.get(id)
    if (!execution) throw new Error(`Execution missing for session: ${id}`)
    return execution
  }

  ensureRuntime(id: string): ConversationSessionRuntime {
    const existing = this.#runtimes.get(id)
    if (existing) { this.#touch(id); return existing }
    const kernel = this.kernelFor(id)
    const snapshot = this.#snapshots.get(id) ?? defaultSnapshot(kernel.summary)
    const runtime = new ConversationSessionRuntime(kernel, snapshot)
    this.#runtimes.set(id, runtime)
    this.#touch(id)
    this.#prune()
    return runtime
  }

  activate(id: string, outgoingSnapshot?: SessionViewMemory): ConversationSessionRuntime {
    if (id === this.#activeSessionId) return this.ensureRuntime(id)
    const outgoing = this.#activeSessionId
    if (outgoingSnapshot) this.saveSnapshot(outgoing, outgoingSnapshot)
    else {
      const runtime = this.#runtimes.get(outgoing)
      if (runtime) this.saveSnapshot(outgoing, runtime.snapshot())
    }
    this.kernelFor(outgoing).setForeground(false)
    this.#activeSessionId = id
    this.kernelFor(id).setForeground(true)
    const runtime = this.ensureRuntime(id)
    this.#touch(id)
    this.#prune()
    this.#notifier.notifyNow()
    return runtime
  }

  saveSnapshot(id: string, snapshot: SessionViewMemory): void {
    const kernel = this.kernelFor(id)
    const normalized: SessionViewMemory = { ...snapshot, logicalPosition: clamp(snapshot.logicalPosition, kernel.count) }
    this.#snapshots.set(id, normalized)
    this.#runtimes.get(id)?.rememberSnapshot(normalized)
  }

  snapshotFor(id: string): SessionViewMemory {
    const kernel = this.kernelFor(id)
    return { ...(this.#snapshots.get(id) ?? defaultSnapshot(kernel.summary)) }
  }

  hasHotRuntime(id: string): boolean { return this.#runtimes.has(id) }

  createSession(): string {
    this.#newCounter += 1
    const descriptor = newSessionSeed(this.#newCounter)
    this.#register(descriptor, true)
    this.#notifier.notifyNow()
    return descriptor.id
  }

  dispose(): void {
    for (const execution of this.#executions.values()) execution.dispose()
    for (const runtime of this.#runtimes.values()) runtime.dispose()
    for (const unsubscribe of this.#kernelUnsubs.values()) unsubscribe()
    this.#executions.clear(); this.#runtimes.clear(); this.#kernels.clear(); this.#kernelUnsubs.clear(); this.#ages.clear(); this.#parentSessionIds.clear(); this.#listedSessionIds.clear()
  }

  #register(descriptor: ReferenceSessionSeed, prepend = false): void {
    const tail = descriptor.parentSessionId
      ? createChildScenarioTail(descriptor.id, descriptor.logicalCount)
      : descriptor.scenario && isLifecycleScenario(descriptor.scenario)
        ? createLifecycleScenarioTail(descriptor.id, descriptor.logicalCount, descriptor.scenario)
        : descriptor.scenario
          ? createScenarioTail(descriptor.id, descriptor.logicalCount, descriptor.scenario as ReferenceScenarioKey)
          : []
    const history = new SyntheticHistoryAdapter(descriptor.id, descriptor.logicalCount, descriptor.seedOffset, descriptor.liveTail === true, tail)
    const kernelDescriptor: ConversationDescriptor = descriptor.status === 'working' && descriptor.liveTail && descriptor.logicalCount > 0
      ? { ...descriptor, activeAssistantIndex: descriptor.logicalCount - 1 }
      : descriptor
    const kernel = new ConversationSessionKernel(kernelDescriptor, history)
    this.#kernels.set(descriptor.id, kernel)
    this.#ages.set(descriptor.id, descriptor.age)
    if (descriptor.parentSessionId) this.#parentSessionIds.set(descriptor.id, descriptor.parentSessionId)
    if (descriptor.listed !== false) this.#listedSessionIds.add(descriptor.id)
    this.#executions.set(descriptor.id, new SyntheticStreamController(kernel, descriptor.playbackMode ?? 'standard'))
    this.#snapshots.set(descriptor.id, defaultSnapshot(descriptor))
    if (prepend) this.#order.unshift(descriptor.id); else this.#order.push(descriptor.id)
    this.#summarySignatures.set(descriptor.id, summarySignature(kernel.summary))
    const unsubscribeSummary = kernel.subscribe(() => {
      const next = summarySignature(kernel.summary)
      if (next !== this.#summarySignatures.get(kernel.id)) {
        this.#summarySignatures.set(kernel.id, next)
        this.#notifier.markDirty()
      }
    })
    const unsubscribeEvents = kernel.subscribeEvents(event => this.#syncActivePlanFromProducerEvent(kernel, event))
    this.#kernelUnsubs.set(descriptor.id, () => { unsubscribeSummary(); unsubscribeEvents() })
  }

  /**
   * Reference producer policy: when the producer appends/replaces a canonical plan snapshot,
   * publish that same semantic value as session activePlan. No history scan or DOM
   * inference is involved, and real adapters can update activePlan directly.
   */
  #syncActivePlanFromProducerEvent(kernel: ConversationSessionKernel, event: SessionKernelEvent): void {
    if ((event.kind !== 'append' && event.kind !== 'content') || event.messageIndex === undefined) return
    const message = kernel.getMessage(event.messageIndex)
    const plan = message.blocks.find(entry => entry.type === 'plan')
    if (plan?.type === 'plan') kernel.setActivePlan(plan.data)
  }

  #touch(id: string): void { this.#clock += 1; this.#lastAccess.set(id, this.#clock) }
  #prune(): void {
    while (this.#runtimes.size > HOT_SESSION_LIMIT) {
      const victim = [...this.#runtimes.values()]
        .filter(runtime => runtime.id !== this.#activeSessionId)
        .sort((a, b) => (this.#lastAccess.get(a.id) ?? 0) - (this.#lastAccess.get(b.id) ?? 0))[0]
      if (!victim) break
      this.#snapshots.set(victim.id, victim.snapshot())
      victim.dispose()
      this.#runtimes.delete(victim.id)
      this.#lastAccess.delete(victim.id)
    }
  }

  #countIndicator(target: ReturnType<typeof deriveSessionIndicator>): number {
    let count = 0
    for (const kernel of this.#kernels.values()) if (this.#listedSessionIds.has(kernel.id) && deriveSessionIndicator(kernel.summary) === target) count += 1
    return count
  }
}

function defaultSnapshot(descriptor: ConversationDescriptor): SessionViewMemory {
  const last = Math.max(0, descriptor.logicalCount - 1)
  return { logicalPosition: last, anchorUnitId: null, anchorOffsetPx: 0, followTail: descriptor.status === 'working', atVisualBottom: true, draftText: '' }
}

function clamp(index: number, count: number): number {
  if (count <= 0) return 0
  return Math.max(0, Math.min(count - 1, Math.floor(Number(index) || 0)))
}

function summarySignature(summary: ConversationDescriptor): string {
  const usage = summary.usage
  const contextPercent = summary.context && summary.context.contextWindow > 0 ? Math.round(summary.context.projectedTokens / summary.context.contextWindow * 100) : 0
  const activePlan = summary.activePlan
  const planState = activePlan
    ? `${activePlan.title ?? ''}:${activePlan.items.map(item => `${item.id}=${item.status}`).join(',')}`
    : ''
  return [deriveSessionIndicator(summary), summary.logicalCount, summary.unread ? '1' : '0', summary.queuedPrompts ?? 0, summary.pendingInteraction?.id ?? '', summary.lastTurnReason ?? '', summary.lastFailure?.code ?? '', planState, Math.floor((usage?.outputTokens ?? 0) / 64), Math.floor((usage?.cacheReadTokens ?? 0) / 256), contextPercent].join(':')
}
