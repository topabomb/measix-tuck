import { describe, expect, it } from 'vitest'
import { SegmentManager } from '../src/engine/core/segment-manager'

describe('SegmentManager semantic restore', () => {
  it('ends an initial/cold-rehydrated window at the last visible logical reader', () => {
    const segment = new SegmentManager(180_000, 2048, 512, 90_000)

    expect(segment.range).toEqual({ start: 87_953, end: 90_001 })
    expect(segment.range.end - 1).toBe(90_000)
  })

  it('keeps explicit far jumps centered because they start a fresh navigation epoch', () => {
    const segment = new SegmentManager(180_000, 2048, 512, 90_000)

    expect(segment.jump(90_000)).toEqual({ start: 88_976, end: 91_024 })
  })

  it('handles the true tail and an empty source without inventing logical rows', () => {
    expect(new SegmentManager(180_000, 2048, 512).range).toEqual({ start: 177_952, end: 180_000 })
    expect(new SegmentManager(0, 2048, 512).range).toEqual({ start: 0, end: 0 })
  })
})