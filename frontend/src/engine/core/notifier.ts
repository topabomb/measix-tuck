export type Unsubscribe = () => void

/** Framework-free microtask publication primitive for bounded engine state. */
export class BatchedNotifier {
  #listeners = new Set<() => void>()
  #scheduled = false

  subscribe(listener: () => void): Unsubscribe {
    this.#listeners.add(listener)
    return () => this.#listeners.delete(listener)
  }

  markDirty(): void {
    if (this.#scheduled) return
    this.#scheduled = true
    queueMicrotask(() => {
      this.#scheduled = false
      for (const listener of this.#listeners) listener()
    })
  }

  notifyNow(): void {
    this.#scheduled = false
    for (const listener of this.#listeners) listener()
  }

  get listenerCount(): number { return this.#listeners.size }
}
