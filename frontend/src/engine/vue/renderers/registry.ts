import type { Component } from 'vue'
import TextBlock from './TextBlock.vue'
import MarkdownBlock from './MarkdownBlock.vue'
import ThinkingBlock from './ThinkingBlock.vue'
import PlanBlock from './PlanBlock.vue'
import CodeBlock from './CodeBlock.vue'
import ImageBlock from './ImageBlock.vue'
import AttachmentBlock from './AttachmentBlock.vue'
import AudioBlock from './AudioBlock.vue'
import HtmlBlock from './HtmlBlock.vue'
import ToolBlock from './ToolBlock.vue'
import TerminalBlock from './TerminalBlock.vue'
import DelegationBlock from './DelegationBlock.vue'
import DiffBlock from './DiffBlock.vue'
import UnknownBlock from './UnknownBlock.vue'

export interface RendererResolver {
  resolve(id: string): Component
}

const DEFAULT_ENTRIES: readonly (readonly [string, Component])[] = [
  ['text', TextBlock],
  ['markdown', MarkdownBlock],
  ['thinking', ThinkingBlock],
  ['plan', PlanBlock],
  ['code', CodeBlock],
  ['image', ImageBlock],
  ['attachments', AttachmentBlock],
  ['audio', AudioBlock],
  ['html', HtmlBlock],
  ['tool', ToolBlock],
  ['terminal', TerminalBlock],
  ['delegation', DelegationBlock],
  ['diff', DiffBlock],
  ['unknown', UnknownBlock],
]

/**
 * Per-viewport physical renderer registry. Semantic projection remains framework
 * neutral; products can clone/replace renderer IDs for one viewport without
 * mutating every Engine instance mounted in the same application.
 */
export class RendererRegistry implements RendererResolver {
  #renderers = new Map<string, Component>()
  readonly fallback: Component

  constructor(entries: Iterable<readonly [string, Component]> = DEFAULT_ENTRIES, fallback: Component = UnknownBlock) {
    this.fallback = fallback
    for (const [id, component] of entries) this.#renderers.set(id, component)
  }

  register(id: string, component: Component): this {
    this.#renderers.set(id, component)
    return this
  }

  resolve(id: string): Component {
    return this.#renderers.get(id) ?? this.fallback
  }

  ids(): readonly string[] {
    return Object.freeze([...this.#renderers.keys()])
  }

  clone(): RendererRegistry {
    return new RendererRegistry(this.#renderers, this.fallback)
  }
}

export function createDefaultRendererRegistry(): RendererRegistry {
  return new RendererRegistry()
}

/** Compatibility default for simple applications. Prefer a per-viewport clone when customizing. */
export const defaultRendererRegistry = createDefaultRendererRegistry()

export function registerRenderer(id: string, component: Component): void {
  defaultRendererRegistry.register(id, component)
}

export function resolveRenderer(id: string): Component {
  return defaultRendererRegistry.resolve(id)
}

export function registeredRendererIds(): readonly string[] {
  return defaultRendererRegistry.ids()
}
