import type { ConversationHistorySource } from '../../engine/conversation/contracts'
import type { LogicalMessage } from '../../engine/model/conversation'
import type { SegmentRange } from '../../engine/core/segment-manager'
import { CHAT_HISTORY } from './history-policy'

type ReadHistory = (start: number, count: number) => Promise<readonly LogicalMessage[]>
interface RetainedHistory { range?: SegmentRange; activeIndex?: number | null }

/** Read-through cache of Tuck's persisted messages, including SSE revisions. */
export class CachedHistory implements ConversationHistorySource {
  #count: number
  #cache = new Map<number, { message: LogicalMessage; bytes: number }>()
  #loading = new Map<number, Promise<void>>()
  #preparedRange: SegmentRange | null = null
  #bytes = 0

  constructor(readonly sessionId: string, count: number, private readonly read: ReadHistory, private readonly retained: () => RetainedHistory) { this.#count = count }
  get count(): number { return this.#count }
  get cachedMessages(): number { return this.#cache.size }
  get estimatedBytes(): number { return this.#bytes }

  loadRange(start: number, count: number): readonly LogicalMessage[] {
    return Array.from({ length: Math.max(0, Math.min(count, this.count - start)) }, (_, offset) => {
      const index = start + offset, entry = this.#cache.get(index)
      if (!entry) throw new Error(`History range ${index} has not been prepared`)
      this.#cache.delete(index); this.#cache.set(index, entry)
      return entry.message
    })
  }

  storeCanonicalMessage(message: LogicalMessage, appendedText?: string): void {
    if (message.index > this.#count) throw new Error('Canonical history append has a gap')
    if (message.index === this.#count) this.#count++
    this.#put(message, appendedText)
    this.#trim()
  }

  /** Preserve the pending view while its asynchronous read/navigation settles. */
  prepareView(start: number, count: number): Promise<void> {
    this.#preparedRange = { start, end: Math.min(this.count, start + count) }
    return this.prepare(start, count, false)
  }

  release(): void { this.#preparedRange = null; this.#trim() }

  async prepare(start: number, count: number, transientRead = true): Promise<void> {
    const end = Math.min(this.count, start + count), waits = new Set<Promise<void>>()
    for (let index = start; index < end;) {
      if (this.#cache.has(index)) { index++; continue }
      const existing = this.#loading.get(index)
      if (existing) { waits.add(existing); index++; continue }
      const first = index
      while (index < end && index - first < CHAT_HISTORY.requestMessages && !this.#cache.has(index) && !this.#loading.has(index)) index++
      const last = index
      const request = this.read(first, last - first).then(messages => {
        for (const message of messages) if (message.index >= first && message.index < last) this.#put(message)
        for (let position = first; position < last; position++) if (!this.#cache.has(position)) throw new Error(`History message ${position} was not returned`)
      }).finally(() => {
        for (let position = first; position < last; position++) if (this.#loading.get(position) === request) this.#loading.delete(position)
      })
      for (let position = first; position < last; position++) this.#loading.set(position, request)
      waits.add(request)
    }
    const results = await Promise.allSettled(waits)
    this.#trim(transientRead ? { start, end } : undefined)
    const failure = results.find(result => result.status === 'rejected')
    if (failure?.status === 'rejected') throw failure.reason
  }

  #put(message: LogicalMessage, appendedText?: string): void {
    const previous = this.#cache.get(message.index)
    if (previous && (previous.message.revision ?? 0) > (message.revision ?? 0)) return
    // UTF-16 serialized size is a conservative budget, not a JS heap measurement.
    // Token appends account only their increment rather than serializing the prefix.
    const bytes = previous && appendedText !== undefined ? previous.bytes + (JSON.stringify(appendedText).length - 2) * 2 : JSON.stringify(message).length * 2
    this.#bytes += bytes - (previous?.bytes ?? 0)
    this.#cache.delete(message.index); this.#cache.set(message.index, { message, bytes })
  }

  #trim(requested?: SegmentRange): void {
    const { range, activeIndex } = this.retained()
    const warm = Boolean(range || this.#preparedRange)
    const contains = (candidate: SegmentRange | null | undefined, index: number) => candidate && index >= candidate.start && index < candidate.end
    for (const [index, entry] of this.#cache) {
      if (warm && this.#cache.size <= CHAT_HISTORY.cacheMessages && this.#bytes <= CHAT_HISTORY.cacheBytes) break
      if (index === this.count - 1 || warm && index >= this.count - CHAT_HISTORY.windowMessages || index === activeIndex || this.#loading.has(index) || contains(range, index) || contains(this.#preparedRange, index) || contains(requested, index)) continue
      this.#cache.delete(index); this.#bytes -= entry.bytes
    }
  }
}
