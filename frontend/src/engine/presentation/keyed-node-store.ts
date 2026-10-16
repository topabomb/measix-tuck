import { BatchedNotifier, type Unsubscribe } from '../core/notifier'
import type { ConversationProjectionStore } from './contracts'
import type { RenderUnit } from './render-unit'

/**
 * Framework-free keyed presentation store. `order` changes only when nodes
 * enter/leave/move; a stable-node patch notifies only subscribers of that key.
 */
export class KeyedConversationProjection implements ConversationProjectionStore {
  #order: readonly string[] = Object.freeze([] as string[])
  #nodes = new Map<string, RenderUnit>()
  #orderNotifier = new BatchedNotifier()
  #nodeNotifiers = new Map<string, BatchedNotifier>()

  get order(): readonly string[] { return this.#order }
  get size(): number { return this.#order.length }
  getNode(id: string): RenderUnit | undefined { return this.#nodes.get(id) }
  subscribeOrder(listener: () => void): Unsubscribe { return this.#orderNotifier.subscribe(listener) }

  subscribeNode(id: string, listener: () => void): Unsubscribe {
    let notifier = this.#nodeNotifiers.get(id)
    if (!notifier) {
      notifier = new BatchedNotifier()
      this.#nodeNotifiers.set(id, notifier)
    }
    const unsubscribe = notifier.subscribe(listener)
    return () => {
      unsubscribe()
      if (notifier?.listenerCount === 0) this.#nodeNotifiers.delete(id)
    }
  }

  replace(units: readonly RenderUnit[]): void {
    const nextIds = units.map(unit => unit.id)
    const orderChanged = !sameOrder(this.#order, nextIds)
    const live = new Set(nextIds)

    for (const unit of units) {
      const previous = this.#nodes.get(unit.id)
      if (previous === unit) continue
      // Revision is a renderer/cache hint, not a complete equality contract.
      // Presentation metadata such as `live`, role or provenance may change while
      // a content-derived revision stays stable, so a new unit object must publish.
      this.#nodes.set(unit.id, unit)
      this.#nodeNotifiers.get(unit.id)?.markDirty()
    }

    for (const id of this.#nodes.keys()) if (!live.has(id)) this.#nodes.delete(id)

    if (orderChanged) {
      this.#order = Object.freeze(nextIds)
      this.#orderNotifier.markDirty()
    }
  }

  patch(unit: RenderUnit): void {
    if (!this.#nodes.has(unit.id)) return
    this.#nodes.set(unit.id, unit)
    this.#nodeNotifiers.get(unit.id)?.markDirty()
  }

  clear(): void {
    if (this.#order.length === 0) return
    this.#order = Object.freeze([] as string[])
    this.#nodes.clear()
    this.#orderNotifier.markDirty()
  }
}

function sameOrder(a: readonly string[], b: readonly string[]): boolean {
  if (a.length !== b.length) return false
  for (let i = 0; i < a.length; i += 1) if (a[i] !== b[i]) return false
  return true
}
