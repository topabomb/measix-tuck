import type { ConversationDescriptor, PendingInteraction } from '../engine/conversation/contracts'
import type { LifecycleScenarioKey } from './lifecycle-scenarios'
import type { ReferenceScenarioKey } from './session-scenarios'
import type { ReferencePlaybackMode } from './stream-controller'

export interface ReferenceSessionDescriptor extends ConversationDescriptor {
  age: string
}

export type ChildReferenceScenarioKey = 'child-rendering-review' | 'child-terminal-audit' | 'child-resource-audit'

export interface ReferenceSessionSeed extends ReferenceSessionDescriptor {
  seedOffset: number
  liveTail?: boolean
  scenario?: ReferenceScenarioKey | ChildReferenceScenarioKey | LifecycleScenarioKey
  playbackMode?: ReferencePlaybackMode
  /** Reference workspace relationship only; Engine does not own a session tree. */
  parentSessionId?: string
  /** Hidden child sessions remain directly addressable without cluttering Recent. */
  listed?: boolean
}

const approval: PendingInteraction = {
  id: 'approval-edit-config',
  kind: 'approval',
  title: 'Approve production config edit',
  detail: 'The agent prepared an exact change to src/runtime/config.ts and is waiting before applying it. This blocker belongs to the session and survives navigation or viewport eviction.',
  toolName: 'edit_file',
  callId: 'config-edit-approval',
}

const meetingFollowupApproval: PendingInteraction = {
  id: 'approval-meeting-followup',
  kind: 'approval',
  title: 'Approve follow-up and Friday review',
  detail: 'The agent has staged the exact follow-up message and 30-minute Friday review. Approving lets the external productivity adapter perform the send/schedule action; denying leaves both untouched.',
  toolName: 'send_meeting_followup',
  callId: 'meeting-followup-approval',
}

const question: PendingInteraction = {
  id: 'question-android-target',
  kind: 'question',
  title: 'Choose Android fallback behavior',
  detail: 'For API 35 clients that cannot refresh managed configuration, should the client fail closed or keep the last accepted configuration?',
}

const agentInitialPlan = {
  title: 'Release regression investigation',
  items: [
    { id: 'inspect', text: 'Inspect projection and resource boundaries', status: 'in-progress' as const },
    { id: 'correlate', text: 'Correlate tool and execution identity', status: 'pending' as const },
    { id: 'verify', text: 'Run the full release gate with terminal evidence', status: 'pending' as const },
    { id: 'synthesize', text: 'Summarize the smallest rendering-layer patch', status: 'pending' as const },
  ],
}

const briefingPlan = {
  title: 'Executive briefing',
  items: [
    { id: 'collect', text: 'Collect current work context and external evidence', status: 'completed' as const },
    { id: 'cross-check', text: 'Cross-check launch risk and KPI signals', status: 'completed' as const },
    { id: 'specialists', text: 'Run customer-risk and metrics specialist reviews', status: 'completed' as const },
    { id: 'deliver', text: 'Produce decision brief and office artifacts', status: 'completed' as const },
  ],
}

const followupPlan = {
  title: 'Meeting follow-up',
  items: [
    { id: 'reconcile', text: 'Reconcile transcript, email and launch brief', status: 'completed' as const },
    { id: 'actions', text: 'Extract owners, dates and unresolved decisions', status: 'completed' as const },
    { id: 'draft', text: 'Draft follow-up message and review meeting', status: 'completed' as const },
    { id: 'send', text: 'Send message and schedule review', status: 'blocked' as const },
  ],
}

const resiliencePlan = {
  title: 'Launch-risk synthesis',
  items: [
    { id: 'fanout', text: 'Run customer, metrics and policy specialist checks', status: 'completed' as const },
    { id: 'recover', text: 'Recover any missing evidence through an allowed fallback', status: 'completed' as const },
    { id: 'synthesize', text: 'Produce a decision brief with explicit confidence', status: 'completed' as const },
  ],
}

const steeringPlan = {
  title: 'Read-only migration review',
  items: [
    { id: 'inspect', text: 'Inspect migration configuration without writes', status: 'completed' as const },
    { id: 'impact', text: 'Summarize affected targets and rollback boundary', status: 'completed' as const },
    { id: 'recommend', text: 'Recommend the smallest reversible future change', status: 'completed' as const },
  ],
}

function usage(inputTokens: number, outputTokens: number, cacheReadTokens: number, cacheWriteTokens: number, reasoningTokens = 0) {
  return { inputTokens, outputTokens, cacheReadTokens, cacheWriteTokens, reasoningTokens }
}

/** Public Reference conversations: realistic recent tails over large lazy histories. */
export const RECENT_SESSIONS: readonly ReferenceSessionSeed[] = [
  {
    id: 'agent-loop', title: 'Agent loop investigation', age: 'now', status: 'working', logicalCount: 84_000,
    seedOffset: 17, liveTail: true, scenario: 'release-investigation', playbackMode: 'agent-loop', activePlan: agentInitialPlan,
    usage: usage(52_000, 7_900, 218_000, 11_000, 2_400), context: { projectedTokens: 68_200, contextWindow: 128_000 }, turnCount: 3_800, stepCount: 7_600,
  },
  {
    id: 'child-review-contract', title: 'Review rendering contract', age: 'now', status: 'idle', logicalCount: 8,
    seedOffset: 811, scenario: 'child-rendering-review', parentSessionId: 'agent-loop', listed: false, lastTurnReason: 'completed',
    usage: usage(8_200, 1_300, 21_000, 800, 420), context: { projectedTokens: 12_600, contextWindow: 128_000 }, turnCount: 1, stepCount: 3,
  },
  {
    id: 'child-terminal-audit', title: 'Audit terminal projection', age: 'now', status: 'idle', logicalCount: 8,
    seedOffset: 821, scenario: 'child-terminal-audit', parentSessionId: 'agent-loop', listed: false, lastTurnReason: 'completed',
    usage: usage(7_600, 1_100, 19_500, 720, 360), context: { projectedTokens: 11_900, contextWindow: 128_000 }, turnCount: 1, stepCount: 3,
  },
  {
    id: 'child-resource-audit', title: 'Audit resource semantics', age: 'now', status: 'idle', logicalCount: 8,
    seedOffset: 831, scenario: 'child-resource-audit', parentSessionId: 'agent-loop', listed: false, lastTurnReason: 'completed',
    usage: usage(7_900, 1_180, 20_200, 760, 390), context: { projectedTokens: 12_100, contextWindow: 128_000 }, turnCount: 1, stepCount: 3,
  },
  {
    id: 'office-briefing', title: 'Monday executive briefing', age: '3m', status: 'idle', logicalCount: 62_000,
    seedOffset: 41, scenario: 'executive-briefing', lastTurnReason: 'completed', activePlan: briefingPlan,
    usage: usage(47_000, 6_800, 192_000, 9_400, 1_900), context: { projectedTokens: 61_300, contextWindow: 128_000 }, turnCount: 2_900, stepCount: 5_600,
  },
  {
    id: 'office-followup', title: 'Launch meeting follow-up', age: '8m', status: 'waiting', logicalCount: 36_000,
    seedOffset: 67, scenario: 'meeting-followup', pendingInteraction: meetingFollowupApproval, activePlan: followupPlan,
    usage: usage(29_000, 4_300, 116_000, 6_100, 1_100), context: { projectedTokens: 44_800, contextWindow: 128_000 }, turnCount: 1_700, stepCount: 3_200,
  },
  {
    id: 'resilience-fallback', title: 'Launch risk fallback', age: '14m', status: 'idle', logicalCount: 44_000,
    seedOffset: 73, scenario: 'partial-failure-recovery', lastTurnReason: 'completed', activePlan: resiliencePlan,
    usage: usage(38_000, 5_200, 141_000, 7_400, 1_500), context: { projectedTokens: 51_600, contextWindow: 128_000 }, turnCount: 2_100, stepCount: 4_000,
  },
  {
    id: 'steered-migration', title: 'Interrupted migration steering', age: '18m', status: 'idle', logicalCount: 28_000,
    seedOffset: 79, scenario: 'steered-interruption', lastTurnReason: 'completed', activePlan: steeringPlan,
    usage: usage(25_000, 3_900, 97_000, 4_900, 980), context: { projectedTokens: 39_200, contextWindow: 128_000 }, turnCount: 1_400, stepCount: 2_900,
  },
  {
    id: 'million', title: 'Million-message streaming stress', age: '22m', status: 'working', logicalCount: 1_000_000,
    seedOffset: 0, liveTail: true, scenario: 'release-investigation', playbackMode: 'stress',
    usage: usage(184_000, 12_600, 936_000, 31_000, 3_900), context: { projectedTokens: 103_400, contextWindow: 128_000 }, turnCount: 42_100, stepCount: 81_900,
  },
  {
    id: 'dsh-transport', title: 'Agent transport refactor', age: '24m', status: 'idle', logicalCount: 180_000,
    seedOffset: 101, scenario: 'transport-refactor', lastTurnReason: 'completed',
    usage: usage(62_000, 8_400, 281_000, 14_000, 2_100), context: { projectedTokens: 74_200, contextWindow: 128_000 }, turnCount: 8_200, stepCount: 15_400,
  },
  {
    id: 'tool-rendering', title: 'Production config migration', age: '1h', status: 'waiting', logicalCount: 420_000,
    seedOffset: 211, scenario: 'config-approval', pendingInteraction: approval,
    usage: usage(91_000, 13_100, 452_000, 18_000, 2_800), context: { projectedTokens: 91_700, contextWindow: 128_000 }, turnCount: 18_900, stepCount: 39_200,
  },
  {
    id: 'event-normalization', title: 'Provider event normalization', age: '2h', status: 'idle', logicalCount: 95_000,
    seedOffset: 307, scenario: 'event-normalization', lastTurnReason: 'completed',
    usage: usage(34_000, 5_900, 149_000, 7_100, 1_300), context: { projectedTokens: 52_400, contextWindow: 128_000 }, turnCount: 4_300, stepCount: 8_700,
  },
  {
    id: 'dynamic-heights', title: 'Responsive artifact review', age: '4h', status: 'idle', logicalCount: 260_000,
    seedOffset: 401, scenario: 'responsive-artifacts', lastTurnReason: 'interrupted',
    usage: usage(78_000, 10_700, 321_000, 11_800, 2_000), context: { projectedTokens: 86_900, contextWindow: 128_000 }, turnCount: 11_800, stepCount: 22_700,
  },
  {
    id: 'workspace-files', title: 'Multimodal handoff review', age: '1d', status: 'idle', logicalCount: 48_000,
    seedOffset: 503, scenario: 'multimodal-handoff', lastTurnReason: 'completed',
    usage: usage(21_000, 3_500, 88_000, 5_200, 800), context: { projectedTokens: 39_800, contextWindow: 128_000 }, turnCount: 2_200, stepCount: 4_100,
  },
  {
    id: 'android-protocol', title: 'Android protocol rollout', age: '2d', status: 'waiting', logicalCount: 24_000,
    seedOffset: 601, scenario: 'android-rollout', pendingInteraction: question,
    usage: usage(18_000, 2_800, 64_000, 3_900, 600), context: { projectedTokens: 31_600, contextWindow: 128_000 }, turnCount: 1_100, stepCount: 2_300,
  },
  {
    id: 'context-cache', title: 'Long-context incident recovery', age: '3d', status: 'idle', logicalCount: 700_000,
    seedOffset: 701, scenario: 'context-recovery', lastTurnReason: 'error',
    lastFailure: { code: 'PROVIDER_TIMEOUT', message: 'Provider request timed out after the retry budget was exhausted.', status: 504, requestId: 'req-reference-cache-timeout' },
    usage: usage(128_000, 17_400, 812_000, 28_000, 4_800), context: { projectedTokens: 118_500, contextWindow: 128_000 }, turnCount: 31_500, stepCount: 63_100,
  },
]

export function newSessionSeed(counter: number): ReferenceSessionSeed {
  return {
    id: `new-${counter}`,
    title: `New agent session ${counter}`,
    age: 'now',
    status: 'idle',
    logicalCount: 0,
    seedOffset: 900 + counter,
    playbackMode: 'standard',
    lastTurnReason: null,
    usage: usage(0, 0, 0, 0, 0),
    context: { projectedTokens: 0, contextWindow: 128_000 },
    turnCount: 0,
    stepCount: 0,
  }
}
