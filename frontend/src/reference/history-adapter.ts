import type { ConversationHistorySource } from '../engine/conversation/contracts'
import type { LogicalMessage } from '../engine/model/conversation'
import { SyntheticConversationSource } from './synthetic'

/**
 * Reference-only synchronous history source.
 *
 * The large history is generated lazily for stress coverage, while a small canonical
 * scenario tail can replace the newest records so the public Reference lands on realistic
 * Agent work instead of lorem-like synthetic content. A real product can back the same
 * Engine contract with a local cache/read-through store; async DB/network fetching stays
 * outside the hot synchronous history-read path.
 */
export class SyntheticHistoryAdapter implements ConversationHistorySource {
  readonly #source: SyntheticConversationSource
  readonly #tail: ReadonlyMap<number, LogicalMessage>

  constructor(
    public readonly sessionId: string,
    public readonly count: number,
    seedOffset = 0,
    liveTail = false,
    tail: readonly LogicalMessage[] = [],
  ) {
    this.#source = new SyntheticConversationSource(count, sessionId, seedOffset, liveTail)
    this.#tail = new Map(tail.map(message => [message.index, message]))
  }

  loadRange(start: number, count: number): readonly LogicalMessage[] {
    const result = this.#source.getRange(start, count)
    for (let offset = 0; offset < result.length; offset += 1) {
      const index = start + offset
      const replacement = this.#tail.get(index)
      if (replacement) result[offset] = replacement
    }
    return result
  }
}
