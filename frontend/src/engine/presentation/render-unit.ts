export type BuiltinRenderKind =
  | 'text'
  | 'markdown'
  | 'thinking'
  | 'plan'
  | 'code'
  | 'image'
  | 'attachments'
  | 'audio'
  | 'html'
  | 'tool'
  | 'terminal'
  | 'delegation'
  | 'diff'
  | 'unknown'

/** Renderer IDs stay open so product packages can register additional presentation kinds. */
export type RenderKind = BuiltinRenderKind | (string & {})

/**
 * Rebuildable renderer-ready node. Stable semantic location is explicit so a
 * renderer never has to inspect Session state, DOM order, or an opaque payload to
 * discover which Turn / Step / Block it belongs to.
 */
export interface RenderUnit {
  id: string
  messageId: string
  messageIndex: number
  turnId: string
  stepId?: string
  blockId: string
  kind: RenderKind
  revision: number
  estimatePx: number
  payload: Record<string, unknown>
}
