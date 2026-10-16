import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { createLifecycleScenarioTail, isLifecycleScenario } from '../src/reference/lifecycle-scenarios'

describe('Reference lifecycle scenarios', () => {
  it('models partial delegated failure as recoverable parent evidence, not parent failure', () => {
    const tail = createLifecycleScenarioTail('resilience-fallback', 44_000, 'partial-failure-recovery')
    const delegation = tail.flatMap(message => message.blocks).find(block => block.type === 'delegation')
    expect(delegation?.type).toBe('delegation')
    if (delegation?.type !== 'delegation') return

    expect(delegation.data.runs.filter(run => run.status === 'failed')).toHaveLength(1)
    expect(delegation.data.runs.filter(run => run.status === 'completed')).toHaveLength(2)
    expect(delegation.data.runs.find(run => run.status === 'failed')?.childSessionId).toBeUndefined()

    const fallback = tail.flatMap(message => message.blocks).find(block => block.type === 'tool-result' && block.data.callId === 'fallback-crm-export')
    expect(fallback?.type).toBe('tool-result')
    expect(tail.flatMap(message => message.blocks).some(block => block.type === 'markdown' && block.data.markdown.includes('guarded rollout'))).toBe(true)
  })

  it('keeps an interrupted Turn as history while a later steered Turn completes independently', () => {
    const tail = createLifecycleScenarioTail('steered-migration', 28_000, 'steered-interruption')
    const turns = new Set(tail.map(message => message.turnId))
    expect(turns.size).toBe(2)

    const terminal = tail.flatMap(message => message.blocks).find(block => block.type === 'terminal')
    expect(terminal?.type).toBe('terminal')
    if (terminal?.type === 'terminal') {
      expect(terminal.data.status).toBe('interrupted')
      expect(terminal.data.exitCode).toBe(130)
    }

    const plan = tail.flatMap(message => message.blocks).find(block => block.type === 'plan')
    expect(plan?.type).toBe('plan')
    if (plan?.type === 'plan') expect(plan.data.items.every(item => item.status === 'completed')).toBe(true)
    expect(tail.at(-1)?.blocks.some(block => block.type === 'markdown' && block.data.markdown.includes('Read-only impact report'))).toBe(true)
  })

  it('keeps lifecycle scenario names and product policy outside framework-neutral Engine files', () => {
    expect(isLifecycleScenario('partial-failure-recovery')).toBe(true)
    expect(isLifecycleScenario('steered-interruption')).toBe(true)
    const core = [
      readFileSync('src/engine/model/conversation.ts', 'utf8'),
      readFileSync('src/engine/conversation/contracts.ts', 'utf8'),
    ].join('\n')
    expect(core).not.toContain('partial-failure-recovery')
    expect(core).not.toContain('steered-interruption')
    expect(core).not.toContain('fallbackPolicy')
    expect(core).not.toContain('retryPolicy')
  })
})
