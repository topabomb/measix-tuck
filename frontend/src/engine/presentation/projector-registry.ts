import type { AgentRunRef, ContentBlock, LogicalMessage, ResourceRef } from '../model/conversation'
import type { RenderKind, RenderUnit } from './render-unit'
import { splitMarkdown } from './markdown-chunks'

export interface ContentProjectionContext {
  message: LogicalMessage
  block: ContentBlock
  blockIndex: number
}

export type ContentProjector = (context: ContentProjectionContext) => readonly RenderUnit[]

/** Framework-free semantic ContentBlock -> bounded RenderUnit registry. */
export class ContentProjectorRegistry {
  #projectors = new Map<string, ContentProjector>()

  register(type: string, projector: ContentProjector): this {
    this.#projectors.set(type, projector)
    return this
  }

  clone(): ContentProjectorRegistry {
    const next = new ContentProjectorRegistry()
    for (const [type, projector] of this.#projectors) next.register(type, projector)
    return next
  }

  has(type: string): boolean { return this.#projectors.has(type) }
  get size(): number { return this.#projectors.size }

  project(message: LogicalMessage, contentBlock: ContentBlock, blockIndex: number): readonly RenderUnit[] {
    const projector = this.#projectors.get(contentBlock.type)
    if (!projector) return [makeRenderUnit(message, contentBlock, `unknown-${blockIndex}`, 'unknown', 96, {
      blockType: contentBlock.type,
      data: contentBlock.data,
    })]
    return projector({ message, block: contentBlock, blockIndex })
  }
}

export function createDefaultContentProjectors(): ContentProjectorRegistry {
  return new ContentProjectorRegistry()
    .register('text', ({ message, block: contentBlock }) => {
      const data = contentBlock.data as { text: string }
      return [makeRenderUnit(message, contentBlock, 'text', 'text', 74 + Math.min(320, data.text.length * 0.16), { text: data.text })]
    })
    .register('markdown', ({ message, block: contentBlock }) => {
      const data = contentBlock.data as { markdown: string; provenance?: {origin:string} }
      const chunks = splitMarkdown(data.markdown)
      return chunks.map(chunk => makeRenderUnit(message, contentBlock, `md-${chunk.index}`, 'markdown', estimateMarkdown(chunk.text), {
        markdown: chunk.text,
        provenance: data.provenance,
        markdownHash: chunk.hash,
        partIndex: chunk.index,
        hasNextPart: chunk.index < chunks.length - 1,
      }, chunk.hash))
    })
    .register('reasoning', ({ message, block: contentBlock }) => {
      const data = contentBlock.data as { text: string; tokenCount?: number; durationMs?: number; defaultOpen?: boolean; status?: string }
      const openEstimate = data.defaultOpen ? Math.min(720, 110 + data.text.length * 0.14) : 72
      return [makeRenderUnit(message, contentBlock, 'thinking', 'thinking', openEstimate, {
        text: data.text,
        tokenCount: data.tokenCount ?? Math.round(data.text.length / 3.8),
        durationMs: data.durationMs ?? 0,
        defaultOpen: data.defaultOpen ?? false,
        status: data.status ?? (message.live ? 'streaming' : 'complete'),
      })]
    })
    .register('plan', ({ message, block: contentBlock }) => {
      const data = contentBlock.data as { title?: string; items?: readonly Record<string, unknown>[] }
      const items = data.items ?? []
      return [makeRenderUnit(message, contentBlock, 'plan', 'plan', 74 + Math.min(420, items.length * 38), { title: data.title, items })]
    })
    .register('code', ({ message, block: contentBlock }) => {
      const data = contentBlock.data as { code: string; language?: string; filename?: string; resource?: ResourceRef; defaultOpen?: boolean }
      const lines = data.code.split('\n')
      return chunkArray(lines, 80).map((part, index) => makeRenderUnit(message, contentBlock, `code-${index}`, 'code', 110 + Math.min(30, part.length) * 20, {
        language: data.language ?? 'text',
        code: part.join('\n'),
        filename: data.filename,
        resource: data.resource,
        defaultOpen: data.defaultOpen ?? part.length <= 34,
      }))
    })
    .register('image', ({ message, block: contentBlock }) => {
      const data = contentBlock.data as { src?: string; width: number; height: number; alt?: string; seed?: number }
      return [makeRenderUnit(message, contentBlock, 'image', 'image', 110 + Math.min(620, (data.height / Math.max(1, data.width)) * 820), data)]
    })
    .register('attachments', ({ message, block: contentBlock }) => {
      const data = contentBlock.data as { items?: readonly Record<string, unknown>[]; title?: string; provenance?: Record<string, unknown> }
      const items = data.items ?? []
      const images = items.filter(item => item.kind === 'image').length
      const nonImages = items.length - images
      const rows = images === 0 ? 0 : Math.ceil(images / Math.min(2, images))
      const estimate = 96 + Math.min(560, rows * 220) + Math.min(260, nonImages * 58)
      return [makeRenderUnit(message, contentBlock, 'attachments', 'attachments', estimate, { ...data, items })]
    })
    .register('audio', ({ message, block: contentBlock }) => {
      const data = contentBlock.data as { transcript?: string; durationMs?: number }
      const estimate = 140 + Math.min(260, String(data.transcript ?? '').length * 0.08)
      return [makeRenderUnit(message, contentBlock, 'audio', 'audio', estimate, contentBlock.data as Record<string, unknown>)]
    })
    .register('html', ({ message, block: contentBlock }) => {
      const data = contentBlock.data as { html: string }
      return [makeRenderUnit(message, contentBlock, 'html', 'html', 240 + Math.min(620, data.html.length * 0.08), data)]
    })
    .register('tool-call', ({ message, block: contentBlock }) => {
      const data = contentBlock.data as Record<string, unknown>
      return [makeRenderUnit(message, contentBlock, 'tool-call', 'tool', 76, { ...data, phase: 'call' })]
    })
    .register('tool-result', ({ message, block: contentBlock }) => {
      const data = contentBlock.data as Record<string, unknown>
      return [makeRenderUnit(message, contentBlock, 'tool-result', 'tool', 76, { ...data, phase: 'result' })]
    })
    .register('terminal', ({ message, block: contentBlock }) => {
      const data = contentBlock.data as { output: string; command?: string; callId?: string; cwd?: ResourceRef; status?: string; exitCode?: number; durationMs?: number; defaultOpen?: boolean }
      const lines = Math.max(1, data.output.split('\n').length)
      const estimate = data.defaultOpen === false ? 76 : 112 + Math.min(560, lines * 18)
      return [makeRenderUnit(message, contentBlock, 'terminal', 'terminal', estimate, { ...data })]
    })
    .register('delegation', ({ message, block: contentBlock }) => {
      const data = contentBlock.data as { title?: string; runs: readonly AgentRunRef[] }
      const runs = data.runs ?? []
      const estimate = 76 + Math.min(500, runs.length * 58 + runs.filter(run => run.summary).length * 34)
      return [makeRenderUnit(message, contentBlock, 'delegation', 'delegation', estimate, { title: data.title, runs })]
    })
    .register('diff', ({ message, block: contentBlock }) => {
      const data = contentBlock.data as { resource: ResourceRef; lines: readonly string[]; defaultOpen?: boolean }
      return chunkArray([...data.lines], 72).map((lines, index) => makeRenderUnit(message, contentBlock, `diff-${index}`, 'diff', 110 + Math.min(28, lines.length) * 20, {
        resource: data.resource,
        file: data.resource.label ?? data.resource.uri,
        lines,
        defaultOpen: data.defaultOpen ?? lines.length <= 32,
      }))
    })
}

/** Compatibility singleton. Production composition may inject its own registry instance. */
export const defaultContentProjectors = createDefaultContentProjectors()

export function projectMessage(message: LogicalMessage, registry = defaultContentProjectors): RenderUnit[] {
  return message.blocks.flatMap((contentBlock, blockIndex) => [...registry.project(message, contentBlock, blockIndex)])
}

export function projectMessages(messages: readonly LogicalMessage[], registry = defaultContentProjectors): RenderUnit[] {
  return messages.flatMap(message => projectMessage(message, registry))
}

export function makeRenderUnit(
  message: LogicalMessage,
  contentBlock: ContentBlock,
  suffix: string,
  kind: RenderKind,
  estimatePx: number,
  payload: Record<string, unknown>,
  revision = contentBlock.revision ?? message.revision ?? 0,
): RenderUnit {
  return {
    id: `${message.id}:${contentBlock.id}:${suffix}`,
    messageId: message.id,
    messageIndex: message.index,
    turnId: message.turnId,
    stepId: message.stepId,
    blockId: contentBlock.id,
    kind,
    revision,
    estimatePx,
    payload: {
      role: message.role,
      turnId: message.turnId,
      stepId: message.stepId,
      live: message.live === true,
      blockId: contentBlock.id,
      blockType: contentBlock.type,
      ...payload,
    },
  }
}

export function estimateMarkdown(markdown: string): number {
  const lines = Math.max(1, markdown.split('\n').length)
  return Math.min(1800, 100 + markdown.length * 0.11 + lines * 10)
}

function chunkArray<T>(items: T[], size: number): T[][] {
  const result: T[][] = []
  for (let index = 0; index < items.length; index += size) result.push(items.slice(index, index + size))
  return result.length ? result : [[]]
}
