import type { VListHandle } from 'virtua/vue'
import type { ConversationSessionRuntime } from '../runtime/session-runtime'

interface MeasuredView {
  layout: string
  units: { id: string; revision: number }[]
  cache: VListHandle['cache']
}

// Adapter-only, one bounded-window cache per hot runtime. Runtime eviction also
// releases these measurements; no DOM handles or session-global archive.
const measurements = new WeakMap<ConversationSessionRuntime, MeasuredView>()

function layoutKey(stage: HTMLElement): string {
  const style = getComputedStyle(stage)
  return [stage.clientWidth, style.fontFamily, style.fontSize, style.lineHeight,
    style.getPropertyValue('--tuck-font-body'), style.getPropertyValue('--tuck-font-ui'),
    style.getPropertyValue('--tuck-font-caption'), style.getPropertyValue('--tuck-line-body'),
    style.getPropertyValue('--conversation-content-width')].join('|')
}

export function rememberMeasurements(runtime: ConversationSessionRuntime, order: readonly string[], list: VListHandle, stage: HTMLElement): void {
  measurements.set(runtime, {
    layout: layoutKey(stage),
    units: order.map(id => ({ id, revision: runtime.projection.getNode(id)?.revision ?? -1 })),
    cache: list.cache,
  })
}

export function restoredMeasurements(runtime: ConversationSessionRuntime, order: readonly string[], stage: HTMLElement): VListHandle['cache'] | undefined {
  const saved = measurements.get(runtime)
  if (!saved || saved.layout !== layoutKey(stage) || saved.units.length !== order.length) return
  if (saved.units.some((unit, index) => unit.id !== order[index] || unit.revision !== runtime.projection.getNode(unit.id)?.revision)) return
  return saved.cache
}
