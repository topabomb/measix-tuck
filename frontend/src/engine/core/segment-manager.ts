export interface SegmentRange {
  start: number
  end: number
}

export class SegmentManager {
  private _range: SegmentRange
  private _totalMessages: number

  constructor(
    totalMessages: number,
    readonly capacity = 2048,
    readonly shift = 512,
    initialReader = totalMessages - 1,
  ) {
    this._totalMessages = Math.max(0, Math.floor(totalMessages))
    this._range = this.endingAt(initialReader)
  }

  get totalMessages(): number { return this._totalMessages }
  get range(): SegmentRange { return { ...this._range } }

  extendEnd(): SegmentRange {
    this._range.end = Math.min(this._totalMessages, this._range.start + this.capacity)
    return this.range
  }

  setTotalMessages(total: number): SegmentRange {
    this._totalMessages = Math.max(0, Math.floor(total))
    if (this._totalMessages === 0) {
      this._range = { start: 0, end: 0 }
      return this.range
    }
    const width = Math.min(this.capacity, Math.max(1, this._range.end - this._range.start))
    const end = Math.min(this._totalMessages, Math.max(1, this._range.end))
    const start = Math.max(0, Math.min(end - width, this._totalMessages - width))
    this._range = { start, end: Math.min(this._totalMessages, start + width) }
    return this.range
  }

  endingAt(index: number): SegmentRange {
    if (this._totalMessages <= 0) return { start: 0, end: 0 }
    const reader = Math.max(0, Math.min(this._totalMessages - 1, Math.floor(index)))
    const end = reader + 1
    return { start: Math.max(0, end - this.capacity), end }
  }

  setEndingAt(index: number): SegmentRange {
    this._range = this.endingAt(index)
    return this.range
  }

  around(index: number): SegmentRange {
    if (this._totalMessages <= 0) return { start: 0, end: 0 }
    const center = Math.max(0, Math.min(this._totalMessages - 1, Math.floor(index)))
    const maxStart = Math.max(0, this._totalMessages - this.capacity)
    const start = Math.max(0, Math.min(maxStart, center - Math.floor(this.capacity / 2)))
    return { start, end: Math.min(this._totalMessages, start + this.capacity) }
  }

  jump(index: number): SegmentRange {
    this._range = this.around(index)
    return this.range
  }

  shiftBackward(): SegmentRange {
    const start = Math.max(0, this._range.start - this.shift)
    if (start === this._range.start) return this.range
    this._range = { start, end: Math.min(this._totalMessages, start + this.capacity) }
    return this.range
  }

  shiftForward(): SegmentRange {
    const maxStart = Math.max(0, this._totalMessages - this.capacity)
    const start = Math.min(maxStart, this._range.start + this.shift)
    this._range = { start, end: Math.min(this._totalMessages, start + this.capacity) }
    return this.range
  }
}
