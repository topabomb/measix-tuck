import { block, type ContentBlock, type LogicalMessage } from './conversation'

export function cloneBlocks(blocks: readonly ContentBlock[]): ContentBlock[] {
  return blocks.map(contentBlock => ({ ...contentBlock, data: { ...contentBlock.data } })) as ContentBlock[]
}

export function appendReasoningContent(
  message: LogicalMessage,
  delta: string,
  durationMs: number,
  tokenCount?: number,
): { message: LogicalMessage; blockId: string } | null {
  const blocks = cloneBlocks(message.blocks)
  const targetIndex = blocks.findIndex(entry => entry.type === 'reasoning')
  if (targetIndex < 0) return null
  const current = blocks[targetIndex] as ContentBlock<'reasoning'>
  const text = `${current.data.text}${delta}`
  blocks[targetIndex] = block(current.id, 'reasoning', {
    ...current.data,
    text,
    ...(tokenCount === undefined ? {} : { tokenCount }),
    durationMs,
    status: 'streaming',
  }, (current.revision ?? 0) + 1)
  return { blockId: current.id, message: { ...message, blocks, revision: (message.revision ?? 0) + 1, live: true } }
}

export function appendMarkdownContent(message: LogicalMessage, delta: string): { message: LogicalMessage; blockId: string } {
  const blocks = cloneBlocks(message.blocks)
  let targetIndex = -1
  for (let index = blocks.length - 1; index >= 0; index -= 1) if (blocks[index]?.type === 'markdown') { targetIndex = index; break }
  if (targetIndex < 0) {
    const created = block(message.role === 'user' ? 'prompt' : 'answer', 'markdown', { markdown: delta }, 1)
    return { blockId: created.id, message: { ...message, blocks: [...blocks, created], revision: (message.revision ?? 0) + 1, live: true } }
  }
  const current = blocks[targetIndex] as ContentBlock<'markdown'>
  blocks[targetIndex] = block(current.id, 'markdown', { ...current.data, markdown: `${current.data.markdown}${delta}` }, (current.revision ?? 0) + 1)
  return { blockId: current.id, message: { ...message, blocks, revision: (message.revision ?? 0) + 1, live: true } }
}

/** Append-only terminal fast path. Tool execution policy remains outside the Engine. */
export function appendTerminalOutput(message: LogicalMessage, blockId: string, delta: string, durationMs?: number): { message: LogicalMessage; blockId: string } | null {
  const blocks = cloneBlocks(message.blocks)
  const targetIndex = blocks.findIndex(entry => entry.id === blockId && entry.type === 'terminal')
  if (targetIndex < 0) return null
  const current = blocks[targetIndex] as ContentBlock<'terminal'>
  blocks[targetIndex] = block(current.id, 'terminal', {
    ...current.data,
    output: `${current.data.output}${delta}`,
    ...(durationMs === undefined ? {} : { durationMs }),
    status: 'running',
  }, (current.revision ?? 0) + 1)
  return { blockId: current.id, message: { ...message, blocks, revision: (message.revision ?? 0) + 1, live: true } }
}

export function settleTerminal(message: LogicalMessage, blockId: string, status: 'success' | 'error' | 'interrupted', exitCode?: number, durationMs?: number): LogicalMessage {
  const blocks = cloneBlocks(message.blocks)
  const targetIndex = blocks.findIndex(entry => entry.id === blockId && entry.type === 'terminal')
  if (targetIndex < 0) return message
  const current = blocks[targetIndex] as ContentBlock<'terminal'>
  blocks[targetIndex] = block(current.id, 'terminal', {
    ...current.data,
    status,
    ...(exitCode === undefined ? {} : { exitCode }),
    ...(durationMs === undefined ? {} : { durationMs }),
  }, (current.revision ?? 0) + 1)
  return { ...message, blocks, revision: (message.revision ?? 0) + 1 }
}

export function replaceMarkdownContent(message: LogicalMessage, markdown: string, live: boolean): LogicalMessage {
  const blocks = cloneBlocks(message.blocks)
  let targetIndex = -1
  for (let index = blocks.length - 1; index >= 0; index -= 1) if (blocks[index]?.type === 'markdown') { targetIndex = index; break }
  const id = targetIndex >= 0 ? blocks[targetIndex]!.id : (message.role === 'user' ? 'prompt' : 'answer')
  const previousRevision = targetIndex >= 0 ? blocks[targetIndex]!.revision ?? 0 : 0
  const nextBlock = block(id, 'markdown', { markdown }, previousRevision + 1)
  if (targetIndex >= 0) blocks[targetIndex] = nextBlock
  else blocks.push(nextBlock)
  return { ...message, blocks, revision: (message.revision ?? 0) + 1, live }
}

export function settleReasoning(message: LogicalMessage, status: 'complete' | 'interrupted'): LogicalMessage {
  if (!message.blocks.some(entry => entry.type === 'reasoning')) return message
  const blocks = cloneBlocks(message.blocks).map(entry => entry.type === 'reasoning'
    ? block(entry.id, 'reasoning', { ...entry.data, status }, (entry.revision ?? 0) + 1)
    : entry)
  return { ...message, blocks, revision: (message.revision ?? 0) + 1 }
}

export function markdownText(message: LogicalMessage): string {
  for (let index = message.blocks.length - 1; index >= 0; index -= 1) {
    const contentBlock = message.blocks[index]
    if (contentBlock?.type === 'markdown') return contentBlock.data.markdown
  }
  return ''
}
