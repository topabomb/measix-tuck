import type {
  ConversationDescriptor,
  SessionContextStats,
  TokenUsage,
} from './contracts'

/** Product-facing state derived from live execution + the last explicitly settled Turn. */
export type SessionIndicator =
  | 'working'
  | 'blocked'
  | 'failed'
  | 'interrupted'
  | 'max-tokens'
  | 'completed'
  | 'idle'

export function deriveSessionIndicator(
  session: Pick<ConversationDescriptor, 'status' | 'pendingInteraction' | 'lastTurnReason'>,
): SessionIndicator {
  if (session.status === 'working') return 'working'
  if (session.status === 'waiting' || session.pendingInteraction) return 'blocked'
  if (session.lastTurnReason === 'error') return 'failed'
  if (session.lastTurnReason === 'aborted' || session.lastTurnReason === 'interrupted') return 'interrupted'
  if (session.lastTurnReason === 'max-tokens') return 'max-tokens'
  if (session.lastTurnReason === 'completed') return 'completed'
  return 'idle'
}

export function sessionIndicatorLabel(indicator: SessionIndicator): string {
  switch (indicator) {
    case 'working': return 'Working'
    case 'blocked': return 'Blocked'
    case 'failed': return 'Failed'
    case 'interrupted': return 'Interrupted'
    case 'max-tokens': return 'Max tokens'
    case 'completed': return 'Completed'
    default: return 'Idle'
  }
}

export function sessionIndicatorGlyph(indicator: SessionIndicator): string {
  switch (indicator) {
    case 'working': return '●'
    case 'blocked': return '!'
    case 'failed': return '×'
    case 'interrupted': return '■'
    case 'max-tokens': return '↑'
    case 'completed': return '✓'
    default: return '○'
  }
}

/** DSH-compatible accounting: inputTokens is uncached input; cache buckets are disjoint. */
export function billedInputTokens(usage: TokenUsage): number {
  return usage.inputTokens + usage.cacheReadTokens + usage.cacheWriteTokens
}

export function cacheHitPercent(usage: TokenUsage): number | null {
  const billed = billedInputTokens(usage)
  if (billed <= 0) return null
  return Math.round(usage.cacheReadTokens / billed * 100)
}

export function contextOccupancyPercent(context: SessionContextStats): number | null {
  if (context.contextWindow <= 0) return null
  return Math.min(100, Math.round(context.projectedTokens / context.contextWindow * 100))
}

export function formatTokens(value: number): string {
  const n = Math.max(0, Math.round(value))
  if (n < 1_000) return String(n)
  const scaled = (v: number) => v >= 100 ? String(Math.round(v)) : String(Math.round(v * 10) / 10)
  if (n < 1_000_000) return `${scaled(n / 1_000)}K`
  return `${scaled(n / 1_000_000)}M`
}

export function emptyTokenUsage(): TokenUsage {
  return { inputTokens: 0, outputTokens: 0, cacheReadTokens: 0, cacheWriteTokens: 0, reasoningTokens: 0 }
}

export function normalizeTokenUsage(usage?: Partial<TokenUsage>): TokenUsage {
  return {
    inputTokens: nonNegative(usage?.inputTokens),
    outputTokens: nonNegative(usage?.outputTokens),
    cacheReadTokens: nonNegative(usage?.cacheReadTokens),
    cacheWriteTokens: nonNegative(usage?.cacheWriteTokens),
    reasoningTokens: nonNegative(usage?.reasoningTokens),
  }
}

function nonNegative(value: number | undefined): number {
  return Number.isFinite(value) ? Math.max(0, Math.round(value ?? 0)) : 0
}
