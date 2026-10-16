import { describe, expect, it } from 'vitest'
import { ConversationSessionKernel } from '../src/engine/conversation/session-kernel'
import { SyntheticHistoryAdapter } from '../src/reference/history-adapter'
import { createMarkdownGalleryTurn, createMixedReferenceTurns, MARKDOWN_COMPATIBILITY_FIXTURES } from '../src/reference/scenarios'
import { projectMessage } from '../src/engine/presentation/projector-registry'

describe('runtime heterogeneous fixture path', () => {
  it('appends canonical ContentBlock messages and re-projects them through the normal pipeline', () => {
    const descriptor = { id: 'fixture', title: 'Fixture', age: 'now', status: 'idle' as const, logicalCount: 20 }
    const kernel = new ConversationSessionKernel(descriptor, new SyntheticHistoryAdapter('fixture', 20, 11))
    const before = kernel.count
    const indexes = kernel.appendCanonicalMessages(createMixedReferenceTurns('fixture', 1, 5))
    expect(indexes.length).toBe(25)
    expect(kernel.count).toBe(before + 25)
    const kinds = new Set(indexes.flatMap(index => projectMessage(kernel.getMessage(index)).map(unit => unit.kind)))
    expect(kinds).toEqual(new Set(['markdown', 'thinking', 'code', 'tool', 'diff', 'image', 'html']))
  })

  it('appends the full Markdown compatibility gallery as normal addressable canonical history', () => {
    const descriptor = { id: 'markdown', title: 'Markdown', age: 'now', status: 'idle' as const, logicalCount: 0 }
    const kernel = new ConversationSessionKernel(descriptor, new SyntheticHistoryAdapter('markdown', 0, 1))
    const indexes = kernel.appendCanonicalMessages(createMarkdownGalleryTurn('markdown', 1))
    expect(indexes).toHaveLength(MARKDOWN_COMPATIBILITY_FIXTURES.length)
    expect(indexes.every(index => projectMessage(kernel.getMessage(index)).every(unit => unit.kind === 'markdown'))).toBe(true)
  })
})