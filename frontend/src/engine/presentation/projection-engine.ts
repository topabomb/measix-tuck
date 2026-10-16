import type { ContentBlock, LogicalMessage } from '../model/conversation'
import type { RenderUnit } from './render-unit'
import { splitMarkdown } from './markdown-chunks'
import {
  ContentProjectorRegistry,
  createDefaultContentProjectors,
  estimateMarkdown,
  makeRenderUnit,
  projectMessage as projectMessageWithRegistry,
} from './projector-registry'

interface ProjectionCacheEntry {
  revision: number
  units: readonly RenderUnit[]
}

export interface ProjectionEngineStats {
  cacheSize: number
  cacheHits: number
  fullProjects: number
  incrementalPatches: number
  evictions: number
}

/**
 * Owns rebuildable presentation work for one hot conversation runtime.
 *
 * The registry is semantic policy; this engine adds bounded message-level memoization
 * and incremental paths for high-frequency Markdown/reasoning/terminal output. Session
 * and domain state never depend on this cache.
 */
export class ProjectionEngine {
  readonly registry: ContentProjectorRegistry
  readonly maxMessages: number

  #cache = new Map<string, ProjectionCacheEntry>()
  #cacheHits = 0
  #fullProjects = 0
  #incrementalPatches = 0
  #evictions = 0

  constructor(registry = createDefaultContentProjectors(), maxMessages = 4096) {
    this.registry = registry
    this.maxMessages = Math.max(128, Math.floor(maxMessages))
  }

  get stats(): ProjectionEngineStats {
    return {
      cacheSize: this.#cache.size,
      cacheHits: this.#cacheHits,
      fullProjects: this.#fullProjects,
      incrementalPatches: this.#incrementalPatches,
      evictions: this.#evictions,
    }
  }

  projectMessage(message: LogicalMessage): readonly RenderUnit[] {
    const revision = message.revision ?? 0
    const cached = this.#cache.get(message.id)
    if (cached && cached.revision === revision) {
      this.#cacheHits += 1
      this.#touch(message.id, cached)
      return cached.units
    }

    this.#fullProjects += 1
    const units = projectMessageWithRegistry(message, this.registry)
    return this.#remember(message, units)
  }

  projectMessages(messages: readonly LogicalMessage[]): RenderUnit[] {
    const units: RenderUnit[] = []
    for (const message of messages) units.push(...this.projectMessage(message))
    return units
  }

  /** Append-only Markdown fast path; settled prefix RenderUnits remain referentially stable. */
  appendMarkdownDelta(message: LogicalMessage, blockId: string, delta: string): readonly RenderUnit[] {
    const cached = this.#cache.get(message.id)
    const contentBlock = message.blocks?.find(entry => entry.id === blockId && entry.type === 'markdown') as ContentBlock<'markdown'> | undefined
    if (!cached || !contentBlock || !delta) return this.projectMessage(message)

    const indexes = cached.units.flatMap((unit, index) => unit.blockId === blockId ? [index] : [])
    if (indexes.length === 0 || !isContiguous(indexes)) return this.projectMessage(message)
    const first = indexes[0]!
    const last = indexes[indexes.length - 1]!
    const blockUnits = cached.units.slice(first, last + 1)
    const previousSource = blockUnits.map(unit => String(unit.payload.markdown ?? '')).join('')
    const currentSource = contentBlock.data.markdown
    if (currentSource.length !== previousSource.length + delta.length || !currentSource.endsWith(delta)) return this.projectMessage(message)

    const oldTail = blockUnits[blockUnits.length - 1]!
    const oldTailText = String(oldTail.payload.markdown ?? '')
    const settledPrefix = blockUnits.slice(0, -1)
    const chunks = splitMarkdown(`${oldTailText}${delta}`)
    const baseIndex = settledPrefix.length
    const finalPartIndex = baseIndex + chunks.length - 1
    const tailUnits = chunks.map((chunk, localIndex) => {
      if (chunks.length === 1 && localIndex === 0 && chunk.text === oldTailText && chunk.hash === oldTail.revision) return oldTail
      const partIndex = baseIndex + localIndex
      return makeRenderUnit(
        message,
        contentBlock,
        `md-${partIndex}`,
        'markdown',
        estimateMarkdown(chunk.text),
        {
          markdown: chunk.text,
          markdownHash: chunk.hash,
          partIndex,
          hasNextPart: partIndex < finalPartIndex,
        },
        chunk.hash,
      )
    })

    const units = [...cached.units.slice(0, first), ...settledPrefix, ...tailUnits, ...cached.units.slice(last + 1)]
    this.#incrementalPatches += 1
    return this.#remember(message, units)
  }

  /** Reasoning text is one stable presentation node; append replaces only that node. */
  appendReasoningDelta(message: LogicalMessage, blockId: string, delta: string): readonly RenderUnit[] {
    const cached = this.#cache.get(message.id)
    const contentBlock = message.blocks?.find(entry => entry.id === blockId && entry.type === 'reasoning') as ContentBlock<'reasoning'> | undefined
    if (!cached || !contentBlock || !delta) return this.projectMessage(message)

    const index = cached.units.findIndex(unit => unit.blockId === blockId)
    if (index < 0 || cached.units.some((unit, candidate) => candidate !== index && unit.blockId === blockId)) return this.projectMessage(message)
    const previous = cached.units[index]!
    const previousText = String(previous.payload.text ?? '')
    const currentText = contentBlock.data.text
    if (currentText.length !== previousText.length + delta.length || !currentText.endsWith(delta)) return this.projectMessage(message)

    const data = contentBlock.data
    const estimate = data.defaultOpen ? Math.min(720, 110 + currentText.length * 0.14) : 72
    const next = makeRenderUnit(message, contentBlock, 'thinking', 'thinking', estimate, {
      text: currentText,
      tokenCount: data.tokenCount ?? Math.round(currentText.length / 3.8),
      durationMs: data.durationMs ?? 0,
      defaultOpen: data.defaultOpen ?? false,
      status: data.status ?? (message.live ? 'streaming' : 'complete'),
    })
    const units = [...cached.units]
    units[index] = next
    this.#incrementalPatches += 1
    return this.#remember(message, units)
  }

  /** Terminal output uses the same one-node append invariant as reasoning, so long logs do not re-project siblings. */
  appendTerminalDelta(message: LogicalMessage, blockId: string, delta: string): readonly RenderUnit[] {
    const cached = this.#cache.get(message.id)
    const contentBlock = message.blocks?.find(entry => entry.id === blockId && entry.type === 'terminal') as ContentBlock<'terminal'> | undefined
    if (!cached || !contentBlock || !delta) return this.projectMessage(message)

    const index = cached.units.findIndex(unit => unit.blockId === blockId)
    if (index < 0 || cached.units.some((unit, candidate) => candidate !== index && unit.blockId === blockId)) return this.projectMessage(message)
    const previous = cached.units[index]!
    const previousOutput = String(previous.payload.output ?? '')
    const currentOutput = contentBlock.data.output
    if (currentOutput.length !== previousOutput.length + delta.length || !currentOutput.endsWith(delta)) return this.projectMessage(message)

    const data = contentBlock.data
    const lines = Math.max(1, currentOutput.split('\n').length)
    const estimate = data.defaultOpen === false ? 76 : 112 + Math.min(560, lines * 18)
    const next = makeRenderUnit(message, contentBlock, 'terminal', 'terminal', estimate, { ...data })
    const units = [...cached.units]
    units[index] = next
    this.#incrementalPatches += 1
    return this.#remember(message, units)
  }

  clear(): void { this.#cache.clear() }

  #remember(message: LogicalMessage, units: readonly RenderUnit[]): readonly RenderUnit[] {
    const ownedUnits = Object.freeze([...units])
    const entry: ProjectionCacheEntry = { revision: message.revision ?? 0, units: ownedUnits }
    this.#touch(message.id, entry)
    while (this.#cache.size > this.maxMessages) {
      const oldest = this.#cache.keys().next().value as string | undefined
      if (!oldest) break
      this.#cache.delete(oldest)
      this.#evictions += 1
    }
    return ownedUnits
  }

  #touch(id: string, entry: ProjectionCacheEntry): void {
    this.#cache.delete(id)
    this.#cache.set(id, entry)
  }
}

function isContiguous(indexes: readonly number[]): boolean {
  for (let i = 1; i < indexes.length; i += 1) if (indexes[i] !== indexes[i - 1]! + 1) return false
  return true
}
