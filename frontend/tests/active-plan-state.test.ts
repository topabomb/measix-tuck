import { describe, expect, it } from 'vitest'
import { ConversationSessionKernel } from '../src/engine/conversation/session-kernel'
import { block } from '../src/engine/model/conversation'
import { SyntheticHistoryAdapter } from '../src/reference/history-adapter'

const plan = {
  title: 'Release gate',
  items: [
    { id: 'inspect', text: 'Inspect evidence', status: 'completed' as const },
    { id: 'verify', text: 'Run verification', status: 'in-progress' as const },
  ],
}

describe('session active work plan', () => {
  it('restores and clones explicit active plan state', () => {
    const kernel = new ConversationSessionKernel({
      id: 'plan-state', title: 'Plan state', status: 'idle', logicalCount: 0, activePlan: plan,
    }, new SyntheticHistoryAdapter('plan-state', 0, 1))

    const first = kernel.activePlan!
    const second = kernel.activePlan!
    expect(first).toEqual(plan)
    expect(kernel.summary.activePlan).toEqual(plan)
    expect(first).not.toBe(second)
    expect(first.items).not.toBe(second.items)
    expect(first.items[0]).not.toBe(second.items[0])
  })

  it('never infers current plan from historical plan messages', () => {
    const kernel = new ConversationSessionKernel({
      id: 'plan-history', title: 'Plan history', status: 'idle', logicalCount: 0,
    }, new SyntheticHistoryAdapter('plan-history', 0, 1))

    kernel.appendCanonicalMessages([{
      turnId: 'turn-1', stepId: 'turn-1:step-1', role: 'assistant',
      blocks: [block('plan', 'plan', plan)],
    }])

    expect(kernel.activePlan).toBeNull()
    expect(kernel.summary.activePlan).toBeNull()
  })

  it('publishes explicit plan state independently from message projection', () => {
    const kernel = new ConversationSessionKernel({
      id: 'plan-update', title: 'Plan update', status: 'idle', logicalCount: 0,
    }, new SyntheticHistoryAdapter('plan-update', 0, 1))
    const events: string[] = []
    kernel.subscribeEvents(event => events.push(event.kind))

    kernel.setActivePlan(plan)
    expect(kernel.activePlan).toEqual(plan)
    expect(kernel.lastEvent.kind).toBe('plan')
    expect(events).toEqual(['plan'])

    kernel.setActivePlan(plan)
    expect(events).toEqual(['plan'])

    kernel.setActivePlan(null)
    expect(kernel.activePlan).toBeNull()
    expect(events).toEqual(['plan', 'plan'])
  })
})
