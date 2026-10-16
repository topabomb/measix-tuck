import { describe, expect, it } from 'vitest'
import { messagesAfter, remainingToBottom, selectCommittedAnchor } from '../src/engine/viewport/contracts'

describe('semantic viewport contract', () => {
  it('does not treat a mounted measurement probe as the semantic anchor', () => {
    const anchor = selectCommittedAnchor([
      { id: 'probe', messageIndex: 91_020, top: 100, bottom: 180 },
      { id: 'real', messageIndex: 90_000, top: 120, bottom: 210 },
      { id: 'next', messageIndex: 90_001, top: 210, bottom: 290 },
    ], 100, 600, 90_001)
    expect(anchor?.id).toBe('real')
    expect(anchor?.offsetPx).toBe(20)
  })

  it('anchors to the committed row edge nearest viewport top instead of a giant partially visible row', () => {
    const anchor = selectCommittedAnchor([
      { id: 'giant-above', messageIndex: 100, top: -575, bottom: 140 },
      { id: 'nearest', messageIndex: 101, top: 18, bottom: 118 },
      { id: 'later', messageIndex: 102, top: 118, bottom: 220 },
    ], 0, 800, 102)
    expect(anchor?.id).toBe('nearest')
    expect(anchor?.offsetPx).toBe(18)
  })

  it('computes logical messages-after independently of physical scroll size', () => {
    expect(messagesAfter(500_000, 1_000_000)).toBe(499_999)
    expect(messagesAfter(999_999, 1_000_000)).toBe(0)
  })

  it('defines physical end using virtualizer measurements only at the adapter boundary', () => {
    expect(remainingToBottom({ scrollSize: 12_000, scrollOffset: 10_800, viewportSize: 1_000 })).toBe(200)
    expect(remainingToBottom({ scrollSize: 12_000, scrollOffset: 11_000, viewportSize: 1_000 })).toBe(0)
  })
})