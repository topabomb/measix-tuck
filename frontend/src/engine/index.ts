/**
 * Stable framework-neutral public surface.
 * Reference fixtures, Vue components and synthetic execution are intentionally excluded.
 */
export {
  block,
  type AgentRunMode,
  type AgentRunRef,
  type AgentRunStatus,
  type AppendCanonicalMessage,
  type ArtifactProvenance,
  type AttachmentItem,
  type AttachmentKind,
  type BuiltinToolCategory,
  type ContentBlock,
  type ContentBlockMap,
  type ContentBlockType,
  type LogicalMessage,
  type LogicalRole,
  type PlanItem,
  type PlanItemStatus,
  type ResourceKind,
  type ResourceRange,
  type ResourceRef,
  type ToolCategory,
  type ToolPresentationIntent,
} from './model/conversation'

export {
  type ConversationDescriptor,
  type ConversationExecutionController,
  type ConversationHistorySource,
  type InteractionResolution,
  type LlmFailure,
  type PendingApproval,
  type PendingInteraction,
  type PendingQuestion,
  type SessionContextStats,
  type SessionStatus,
  type SubmitDisposition,
  type TokenUsage,
  type TurnEndReasonKind,
  type WorkPlan,
} from './conversation/contracts'
export {
  ConversationSessionKernel,
  type SessionKernelContentPatch,
  type SessionKernelEvent,
  type SessionKernelEventKind,
} from './conversation/session-kernel'

export {
  ContentProjectorRegistry,
  createDefaultContentProjectors,
  projectMessage,
  projectMessages,
  type ContentProjectionContext,
  type ContentProjector,
} from './presentation/projector-registry'
export { ProjectionEngine, type ProjectionEngineStats } from './presentation/projection-engine'
export { KeyedConversationProjection } from './presentation/keyed-node-store'
export { type ConversationProjectionStore } from './presentation/contracts'
export { type BuiltinRenderKind, type RenderKind, type RenderUnit } from './presentation/render-unit'

export {
  VIEWPORT_POLICY,
  clampLogicalIndex,
  isMessageCommittedVisible,
  messagesAfter,
  remainingToBottom,
  selectCommittedAnchor,
  type CommittedViewportAnchor,
  type PhysicalListPort,
  type ViewportRowSample,
} from './viewport/contracts'
export { type SemanticViewportSnapshot, type SessionViewMemory } from './viewport/state'

/** Runtime tuning and adapter telemetry stay implementation details, not stable API. */
export { ConversationSessionRuntime } from './runtime/session-runtime'
