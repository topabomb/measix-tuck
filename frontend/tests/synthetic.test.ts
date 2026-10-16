import { describe, expect, it } from 'vitest'
import { SyntheticConversationSource } from '../src/reference/synthetic'
import { projectMessage } from '../src/engine/presentation/projector-registry'

describe('SyntheticConversationSource', () => {
  it('addresses one million messages without materializing them', () => {
    const source = new SyntheticConversationSource(1_000_000)
    expect(source.count).toBe(1_000_000)
    expect(source.getMessage(543_210)).toEqual(source.getMessage(543_210))
    expect(source.getRange(900_000, 32)).toHaveLength(32)
  })

  it('keeps historical tails idle unless the adapter explicitly models a live run', () => {
    expect(new SyntheticConversationSource(10).getMessage(9).live).not.toBe(true)
    expect(new SyntheticConversationSource(10, 'live', 0, true).getMessage(9).live).toBe(true)
  })

  it('generates canonical blocks covering every built-in renderer and bounded chunking', () => {
    const source = new SyntheticConversationSource(1000)
    const kinds = new Set(Array.from({ length: 1000 }, (_, index) => projectMessage(source.getMessage(index))).flat().map(unit => unit.kind))
    expect(kinds).toEqual(new Set(['text', 'markdown', 'thinking', 'code', 'image', 'html', 'tool', 'diff']))
    const candidate = Array.from({ length: 1000 }, (_, index) => source.getMessage(index)).find(message => projectMessage(message).length > 1)
    expect(candidate).toBeDefined()
    expect(candidate?.blocks.length).toBeGreaterThan(0)
  })

  it('uses one producer-owned callId for a synthetic tool call/result pair', () => {
    const source = new SyntheticConversationSource(24, 'tool-id')
    const call = projectMessage(source.getMessage(2))[0]!
    const result = projectMessage(source.getMessage(3))[0]!
    expect(call.payload.callId).toBe(result.payload.callId)
    expect(call.payload.phase).toBe('call')
    expect(result.payload.phase).toBe('result')
  })
})