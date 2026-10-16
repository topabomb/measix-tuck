import { FenwickTree } from './fenwick'

export const DEFAULT_PAGE_SIZE = 512
export const DEFAULT_MESSAGE_ESTIMATE_PX = 420

export class PageHeightIndex {
  readonly pageCount: number
  private readonly heights: FenwickTree

  constructor(
    readonly messageCount: number,
    readonly pageSize = DEFAULT_PAGE_SIZE,
    defaultMessageHeight = DEFAULT_MESSAGE_ESTIMATE_PX,
  ) {
    this.pageCount = Math.max(1, Math.ceil(messageCount / pageSize))
    this.heights = new FenwickTree(this.pageCount)
    for (let page = 0; page < this.pageCount; page += 1) {
      const messages = Math.min(pageSize, messageCount - page * pageSize)
      this.heights.set(page, Math.max(1, messages) * defaultMessageHeight)
    }
  }

  updatePage(page: number, estimatedHeight: number): void {
    this.heights.set(page, Math.max(1, estimatedHeight))
  }

  estimatedOffsetBeforeMessage(messageIndex: number): number {
    const page = Math.floor(Math.max(0, messageIndex) / this.pageSize)
    return this.heights.prefix(Math.min(page, this.pageCount))
  }

  estimatedTotalHeight(): number { return this.heights.total() }

  pageAtEstimatedOffset(offset: number): number { return this.heights.lowerBound(offset) }
}
