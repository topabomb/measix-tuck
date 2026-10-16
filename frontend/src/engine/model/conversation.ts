export type BuiltinToolCategory = 'generic' | 'search' | 'filesystem' | 'shell' | 'image-generation' | 'tts' | 'asr'
/** Tool categories describe capability families, not visual treatment. */
export type ToolCategory = BuiltinToolCategory | (string & {})
export type AttachmentKind = 'image' | 'audio' | 'video' | 'file'

/** ResourceRef.kind is exactly: file, url or artifact. The alias is shared by public contracts. */
export type ResourceKind = 'file' | 'url' | 'artifact'
export interface ResourceRange {
  startLine: number
  startColumn?: number
  endLine?: number
  endColumn?: number
}

/** Stable semantic resource identity. Opening/routing the resource belongs to the host. */
export interface ResourceRef {
  id: string
  kind: ResourceKind
  uri: string
  label?: string
  range?: ResourceRange
}

/** Renderer-neutral hint for how a tool activity is best understood. It never controls layout or policy. */
export type ToolPresentationIntent =
  | { kind: 'generic' }
  | { kind: 'terminal'; command?: string; cwd?: ResourceRef }
  | { kind: 'changes'; resources?: readonly ResourceRef[] }
  | { kind: 'resources'; resources: readonly ResourceRef[] }

export type PlanItemStatus = 'pending' | 'in-progress' | 'completed' | 'blocked' | 'cancelled'
export interface PlanItem {
  id: string
  text: string
  status: PlanItemStatus
}

/**
 * Parent-facing execution relationship for delegated child work.
 * foreground means the producer reports that parent flow waited for this run;
 * background means parent flow may continue while the child remains active.
 * This is observation, never scheduling policy.
 */
export type AgentRunMode = 'foreground' | 'background'
export type AgentRunStatus = 'queued' | 'running' | 'waiting' | 'completed' | 'failed' | 'interrupted'
export interface AgentRunRef {
  runId: string
  title: string
  agent?: string
  mode: AgentRunMode
  status: AgentRunStatus
  /** Optional stable child conversation/session address. Navigation remains host-owned. */
  childSessionId?: string
  /** Final or producer-reported concise result. Child trace remains in the child session. */
  summary?: string
}

export interface AttachmentItem {
  id: string
  name: string
  kind: AttachmentKind
  mimeType: string
  resource?: ResourceRef
  sizeBytes?: number
  src?: string
  width?: number
  height?: number
  durationMs?: number
  seed?: number
}

export interface ArtifactProvenance {
  origin: 'user-upload' | 'tool-output' | 'assistant'
  toolCallId?: string
  toolName?: string
  model?: string
  prompt?: string
}

export interface ContentBlockMap {
  text: { text: string }
  markdown: { markdown: string; flavor?: 'gfm'; provenance?: {origin:string} }
  reasoning: { text: string; tokenCount?: number; durationMs?: number; defaultOpen?: boolean; status?: 'streaming' | 'complete' | 'interrupted' }
  plan: { title?: string; items: readonly PlanItem[] }
  code: { code: string; language?: string; filename?: string; resource?: ResourceRef; defaultOpen?: boolean }
  /** Stress/reference image primitive. User/tool media normally uses `attachments`. */
  image: { src?: string; width: number; height: number; alt?: string; seed?: number }
  attachments: { items: readonly AttachmentItem[]; title?: string; provenance?: ArtifactProvenance }
  audio: { title: string; purpose: 'tts' | 'asr-input' | 'recording'; durationMs: number; src?: string; transcript?: string; model?: string; waveform?: readonly number[]; status?: 'processing' | 'ready' | 'error' }
  html: { html: string }
  'tool-call': { name: string; callId: string; input: unknown; category?: ToolCategory; presentation?: ToolPresentationIntent; resources?: readonly ResourceRef[]; model?: string; progress?: number; durationMs?: number; status?: 'running' | 'success' | 'error'; defaultOpen?: boolean }
  'tool-result': { name: string; callId: string; output: unknown; category?: ToolCategory; presentation?: ToolPresentationIntent; resources?: readonly ResourceRef[]; model?: string; progress?: number; durationMs?: number; status?: 'running' | 'success' | 'error'; defaultOpen?: boolean }
  terminal: { callId?: string; command?: string; cwd?: ResourceRef; output: string; status: 'running' | 'success' | 'error' | 'interrupted'; exitCode?: number; durationMs?: number; defaultOpen?: boolean }
  /** One parent-visible delegation batch. One run covers sync; many runs cover parallel/background children. */
  delegation: { title?: string; runs: readonly AgentRunRef[] }
  diff: { resource: ResourceRef; lines: readonly string[]; defaultOpen?: boolean }
}

export type ContentBlockType = keyof ContentBlockMap
export type ContentBlock<K extends ContentBlockType = ContentBlockType> = {
  [P in K]: { id: string; type: P; data: ContentBlockMap[P]; revision?: number }
}[K]

export type LogicalRole = 'user' | 'assistant' | 'tool' | 'system'

/** Provider-neutral canonical history record. No reference, Vue, DOM or virtualizer fields belong here. */
export interface LogicalMessage {
  id: string
  index: number
  turnId: string
  /** Stable model-request coordinate inside a Turn when the producer has one. */
  stepId?: string
  role: LogicalRole
  blocks: readonly ContentBlock[]
  revision?: number
  live?: boolean
}

/** Append form used by normalized backend/runtime adapters before global message indexes are assigned. */
export interface AppendCanonicalMessage {
  turnId: string
  stepId?: string
  role: LogicalRole
  blocks: readonly ContentBlock[]
  live?: boolean
}

export function block<K extends ContentBlockType>(id: string, type: K, data: ContentBlockMap[K], revision = 0): ContentBlock<K> {
  return { id, type, data, revision } as ContentBlock<K>
}
