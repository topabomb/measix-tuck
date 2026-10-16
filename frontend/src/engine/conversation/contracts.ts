import type { ContentBlockMap, LogicalMessage } from '../model/conversation'

/** Current live session execution state. Settled outcomes such as interruption live in `lastTurnReason`. */
export type SessionStatus = 'idle' | 'working' | 'waiting'

/** Explicitly settled Turn outcomes. A waiting interaction is live state, not an end reason. */
export type TurnEndReasonKind =
  | 'completed'
  | 'aborted'
  | 'error'
  | 'max-tokens'
  | 'interrupted'

export interface LlmFailure {
  message: string
  code: string
  status?: number
  providerRetryAfterMs?: number
  requestId?: string
}

/** Provider-normalized token accounting. Prompt buckets are disjoint. */
export interface TokenUsage {
  inputTokens: number
  outputTokens: number
  cacheReadTokens: number
  cacheWriteTokens: number
  reasoningTokens: number
}

export interface SessionContextStats {
  projectedTokens: number
  contextWindow: number
}

/**
 * Current producer-owned work plan for a session. The shape is exactly the same
 * semantic value rendered by a canonical `plan` block; placement/popovers remain UI concerns.
 */
export type WorkPlan = ContentBlockMap['plan']

interface PendingInteractionBase {
  id: string
  title: string
  detail: string
}

export interface PendingApproval extends PendingInteractionBase {
  kind: 'approval'
  purpose?: 'iterations'
  toolName?: string
  /** Optional correlation to the exact canonical tool call being reviewed. */
  callId?: string
}

export interface PendingQuestion extends PendingInteractionBase {
  kind: 'question'
  /** Optional structured form supplied by the execution adapter. */
  schema?: Record<string, unknown>
}

export type PendingInteraction = PendingApproval | PendingQuestion

/**
 * Explicit response to one exact session-owned blocker. The stable interaction id
 * prevents a delayed UI/provider response from resolving a newer blocker of the same kind.
 * Execution adapters interpret the approval/answer value after Engine validation.
 */
export type InteractionResolution =
  | { interactionId: string; kind: 'approval'; approved: boolean }
  | { interactionId: string; kind: 'question'; answer: string | null }

/** Durable/session semantics only. Relative age, badges and other list chrome belong to products. */
export interface ConversationDescriptor {
  id: string
  title: string
  status: SessionStatus
  logicalCount: number
  unread?: boolean
  /** Summary count only; queued prompt payloads are live SessionKernel state, not descriptor persistence. */
  queuedPrompts?: number
  /** Required exactly when status is `waiting`; absent for all other live states. */
  pendingInteraction?: PendingInteraction | null
  /** Latest explicitly settled Turn outcome. Live waiting/working state never infers this value. */
  lastTurnReason?: TurnEndReasonKind | null
  lastFailure?: LlmFailure | null
  usage?: Partial<TokenUsage>
  context?: SessionContextStats
  turnCount?: number
  stepCount?: number
  /**
   * Explicit current work-plan state restored by the producer/host. Historical `plan`
   * messages remain snapshots and are never scanned to guess this value.
   */
  activePlan?: WorkPlan | null
  /** Explicit active assistant record for a rehydrated working session. Never inferred from history order. */
  activeAssistantIndex?: number | null
}

/**
 * Synchronous, globally addressable history source used by SessionKernel/Runtime.
 * Remote/database integrations should place async fetching and caching outside this
 * hot read contract, then expose the locally available range through this interface.
 */
export interface ConversationHistorySource {
  readonly sessionId: string
  readonly count: number
  loadRange(start: number, count: number): readonly LogicalMessage[]
  /**
   * Optional write-through cache for histories already persisted by the producer.
   * It owns live records as well as fetched records, so the kernel need not retain
   * a second unbounded copy. Appending at count must advance count synchronously.
   * The host prepares evicted records before synchronous reads. The optional text
   * append avoids serializing a growing message again just to account cache size.
   */
  storeCanonicalMessage?(message: LogicalMessage, appendedText?: string): void
}

export type SubmitDisposition = 'started' | 'queued' | 'blocked'

/** Runtime-neutral command port. Playback/debug controls belong to reference/provider adapters. */
export interface ConversationExecutionController {
  readonly running: boolean
  abort(): void
  submit(prompt: string): SubmitDisposition
  resolveInteraction(resolution: InteractionResolution): void
  dispose?(): void
}
