import type { Unsubscribe } from '../core/notifier'
import type { RenderUnit } from './render-unit'

/** Rebuildable keyed presentation store. Domain/session code never depends on it. */
export interface ConversationProjectionStore {
  readonly order: readonly string[]
  readonly size: number
  getNode(id: string): RenderUnit | undefined
  subscribeOrder(listener: () => void): Unsubscribe
  subscribeNode(id: string, listener: () => void): Unsubscribe
}
