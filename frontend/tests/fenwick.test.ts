import { describe, expect, it } from 'vitest'
import { FenwickTree } from '../src/engine/core/fenwick'
import { PageHeightIndex } from '../src/engine/core/page-index'

describe('FenwickTree', () => {
  it('updates and resolves prefix positions in logarithmic index space', () => {
    const tree = new FenwickTree(5)
    ;[10, 20, 30, 40, 50].forEach((value, index) => tree.set(index, value))
    expect(tree.prefix(0)).toBe(0)
    expect(tree.prefix(3)).toBe(60)
    expect(tree.total()).toBe(150)
    expect(tree.lowerBound(0)).toBe(0)
    expect(tree.lowerBound(9)).toBe(0)
    expect(tree.lowerBound(10)).toBe(1)
    expect(tree.lowerBound(59)).toBe(2)
    tree.set(1, 100)
    expect(tree.prefix(3)).toBe(140)
  })
})

describe('PageHeightIndex', () => {
  it('keeps the million-message global height index page-sized', () => {
    const index = new PageHeightIndex(1_000_000, 512, 420)
    expect(index.pageCount).toBe(1954)
    expect(index.estimatedTotalHeight()).toBeGreaterThan(400_000_000)
    index.updatePage(1000, 123_456)
    expect(index.pageAtEstimatedOffset(index.estimatedOffsetBeforeMessage(512_000))).toBeGreaterThanOrEqual(999)
  })
})
