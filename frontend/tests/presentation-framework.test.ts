import { describe, expect, it } from 'vitest'
import type { LogicalMessage } from '../src/engine/model/conversation'
import { block } from '../src/engine/model/conversation'
import { ContentProjectorRegistry, defaultContentProjectors, projectMessage } from '../src/engine/presentation/projector-registry'
import { splitMarkdown } from '../src/engine/presentation/markdown-chunks'
import { AGENT_SCENARIO_MESSAGE_COUNT, createAgentScenarioPack, createMixedReferenceTurn, MARKDOWN_COMPATIBILITY_FIXTURES } from '../src/reference/scenarios'

function message(blocks: LogicalMessage['blocks'], revision = 0): LogicalMessage {
  return { id: 's:m-1', index: 1, turnId: 's:t-1', stepId: 's:t-1:step-0', role: 'assistant', blocks, revision }
}

describe('Agent presentation framework', () => {
  it('projects one canonical message containing heterogeneous content through bounded stable units', () => {
    const fixture = createMixedReferenceTurn('s', 3)[1]!
    const units = projectMessage(message(fixture.blocks))
    expect(new Set(units.map(unit => unit.kind))).toEqual(new Set(['thinking', 'markdown', 'html']))
    expect(units.every(unit => unit.id.startsWith('s:m-1:'))).toBe(true)
    expect(units.every(unit => unit.estimatePx <= 1800)).toBe(true)
    expect(units.every(unit => unit.turnId === 's:t-1' && unit.stepId === 's:t-1:step-0')).toBe(true)
  })

  it('keeps settled Markdown prefix chunks stable while only the streaming tail changes', () => {
    const prefix = Array.from({ length: 80 }, (_, i) => `### section ${i}\n\n${'stable paragraph '.repeat(20)}`).join('\n\n')
    const before = projectMessage(message([block('answer', 'markdown', { markdown: prefix })]))
    const after = projectMessage(message([block('answer', 'markdown', { markdown: `${prefix}\n\n### appended\n\nnew streaming tail` })], 1))
    expect(before.length).toBeGreaterThan(1)
    for (let i = 0; i < before.length - 1; i += 1) {
      expect(after[i]?.id).toBe(before[i]?.id)
      expect(after[i]?.revision).toBe(before[i]?.revision)
    }
  })

  it('emits explicit Markdown continuation metadata for multi-unit messages', () => {
    const source = Array.from({ length: 90 }, (_, i) => `### section ${i}\n\n${'paragraph '.repeat(30)}`).join('\n\n')
    const units = projectMessage(message([block('answer', 'markdown', { markdown: source })]))
    expect(units.length).toBeGreaterThan(1)
    expect(units.map(unit => unit.payload.partIndex)).toEqual(units.map((_, index) => index))
    expect(units.map(unit => unit.payload.hasNextPart)).toEqual(units.map((_, index) => index < units.length - 1))
  })

  it('never splits an open fenced code block', () => {
    const source = `intro\n\n\`\`\`ts\n${Array.from({ length: 600 }, (_, i) => `const line_${i} = ${i}`).join('\n')}\n\`\`\`\n\nafter`
    const chunks = splitMarkdown(source, 900)
    expect(chunks.length).toBeGreaterThanOrEqual(2)
    for (const chunk of chunks) {
      const fences = chunk.text.match(/```/g)?.length ?? 0
      expect(fences % 2).toBe(0)
    }
    expect(chunks.map(chunk => chunk.text).join('')).toBe(source)
  })

  it('keeps tight GFM tables intact instead of splitting at ordinary line breaks', () => {
    const source = [
      '| key | value |',
      '| --- | --- |',
      ...Array.from({ length: 120 }, (_, index) => `| row-${index} | ${'value '.repeat(8)} |`),
    ].join('\n')
    const chunks = splitMarkdown(source, 180)
    expect(chunks).toHaveLength(1)
    expect(chunks[0]?.text).toBe(source)
  })

  it('keeps loose GFM lists atomic across internal blank lines', () => {
    const source = [
      '- first item',
      '',
      `  ${'first continuation '.repeat(24)}`,
      '',
      '- second item',
      '',
      `  ${'second continuation '.repeat(24)}`,
    ].join('\n')
    const chunks = splitMarkdown(source, 120)
    expect(chunks).toHaveLength(1)
    expect(chunks[0]?.text).toBe(source)
  })

  it('splits only between parser-recognized top-level blocks and preserves source bytes', () => {
    const source = [
      '## before',
      '',
      'A paragraph before the quote.',
      '',
      '> quoted paragraph',
      '>',
      '> - nested one',
      '> - nested two',
      '',
      '## after',
      '',
      'Trailing paragraph.',
    ].join('\n')
    const chunks = splitMarkdown(source, 80)
    expect(chunks.length).toBeGreaterThan(1)
    expect(chunks.map(chunk => chunk.text).join('')).toBe(source)
    expect(chunks.filter(chunk => chunk.text.includes('> quoted paragraph'))).toHaveLength(1)
  })

  it('supports semantic projector extensions without modifying the default registry', () => {
    const registry = new ContentProjectorRegistry()
    registry.register('citation', ({ message: owner, block: contentBlock }) => [{
      id: `${owner.id}:${contentBlock.id}:citation`, messageId: owner.id, messageIndex: owner.index,
      turnId: owner.turnId, stepId: owner.stepId, blockId: contentBlock.id,
      kind: 'citation', revision: 0, estimatePx: 64, payload: { href: 'https://example.com' },
    }])
    const customBlock = { id: 'cite-1', type: 'citation', data: { href: 'https://example.com' } } as never
    expect(registry.project(message([]), customBlock, 0)[0]?.kind).toBe('citation')
    expect(defaultContentProjectors.has('citation')).toBe(false)
  })

  it('projects common Agent media workflows without tool-specific renderer coupling', () => {
    const pack = createAgentScenarioPack('scenario', 2)
    expect(pack).toHaveLength(AGENT_SCENARIO_MESSAGE_COUNT)
    const projected = pack.flatMap((entry, index) => projectMessage({
      id: `scenario:m-${index}`,
      index,
      turnId: entry.turnId,
      stepId: entry.stepId,
      role: entry.role,
      blocks: entry.blocks,
      revision: 0,
    }))
    expect(new Set(projected.map(unit => unit.kind))).toEqual(new Set(['attachments', 'thinking', 'markdown', 'tool', 'audio']))
    expect(projected.filter(unit => unit.kind === 'attachments')).toHaveLength(3)
    expect(projected.filter(unit => unit.kind === 'audio')).toHaveLength(2)

    const imageCall = pack.find(entry => entry.fixtureId === 'scenario-image-gen-call')?.blocks[0]
    const imageResult = pack.find(entry => entry.fixtureId === 'scenario-image-gen-result')?.blocks[0]
    const generated = pack.find(entry => entry.fixtureId === 'scenario-image-gen-artifacts')?.blocks[0]
    expect(imageCall?.type).toBe('tool-call')
    expect(imageResult?.type).toBe('tool-result')
    expect(generated?.type).toBe('attachments')
    if (imageCall?.type === 'tool-call' && imageResult?.type === 'tool-result' && generated?.type === 'attachments') {
      expect(imageResult.data.callId).toBe(imageCall.data.callId)
      expect(generated.data.provenance?.toolCallId).toBe(imageCall.data.callId)
      expect(generated.data.items).toHaveLength(4)
    }
  })

  it('maintains a deterministic Markdown compatibility gallery', () => {
    expect(MARKDOWN_COMPATIBILITY_FIXTURES.map(item => item.id)).toEqual([
      'gfm-basics', 'lists-tasks', 'wide-table', 'fences', 'raw-html', 'long-document',
    ])
  })
})
