import { ProjectionEngine } from '../../engine/presentation/projection-engine'
import { createDefaultContentProjectors } from '../../engine/presentation/projector-registry'
import { CHAT_HISTORY } from './history-policy'

/** Keep empty producer placeholders in history, but out of the product's visible rows. */
export function createWorkbenchProjection(): ProjectionEngine {
  const defaults = createDefaultContentProjectors()
  const registry = defaults.clone()
  for (const type of ['markdown', 'reasoning'] as const) registry.register(type, context => {
    const data = context.block.data as { markdown?: string; text?: string }
    if (!(data.markdown ?? data.text ?? '').trim()) return []
    return defaults.project(context.message, context.block, context.blockIndex)
  })
  registry.register('diff', context => {
    const {lines} = context.block.data as {lines:readonly string[]}
    // These are observed whole-file snapshots, not a native patch. An unchanged
    // observation must not be presented as a removal and re-addition of the file.
    if (lines[2] === '@@ observed file snapshots @@') {
      const body = lines.slice(3)
      const before = body.filter(line => line.startsWith('-')).map(line => line.slice(1))
      const after = body.filter(line => line.startsWith('+')).map(line => line.slice(1))
      if (body.every(line => /^[+-]/.test(line)) && before.length === after.length && before.every((line,index) => line === after[index])) return []
    }
    return defaults.project(context.message, context.block, context.blockIndex)
  })
  return new ProjectionEngine(registry, CHAT_HISTORY.windowMessages)
}
