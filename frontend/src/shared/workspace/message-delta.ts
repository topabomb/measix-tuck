import type { LogicalMessage } from '../../engine/model/conversation'
import type { SessionKernelContentPatch } from '../../engine/conversation/session-kernel'

export interface MessageDelta {
  messageId: string; index: number; blockId: string
  field: 'markdown' | 'text' | 'output'; text: string
  baseRevision: number; revision: number; blockRevision: number
}

/** A missing predecessor is recovered by the host from its authoritative snapshot. */
export function applyMessageDelta(previous: LogicalMessage, delta: MessageDelta): { message: LogicalMessage; patch: SessionKernelContentPatch } | null {
  if (previous.id !== delta.messageId || previous.index !== delta.index || (previous.revision ?? 0) !== delta.baseRevision || delta.revision !== delta.baseRevision + 1) return null
  const index = previous.blocks.findIndex(block => block.id === delta.blockId)
  const block = previous.blocks[index]
  const kinds = { markdown: ['markdown', 'append-markdown'], text: ['reasoning', 'append-reasoning'], output: ['terminal', 'append-terminal'] } as const
  const [type, kind] = kinds[delta.field]
  if (!block || block.type !== type || delta.blockRevision !== (block.revision ?? 0) + 1) return null
  const data = block.data as unknown as Record<string, unknown>
  if (typeof data[delta.field] !== 'string') return null
  const blocks = [...previous.blocks]
  blocks[index] = { ...block, revision: delta.blockRevision, data: { ...data, [delta.field]: data[delta.field] + delta.text } } as typeof block
  return { message: { ...previous, revision: delta.revision, blocks }, patch: { kind, blockId: delta.blockId, delta: delta.text } }
}
