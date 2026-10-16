import { describe, expect, it } from 'vitest'
import { block, type LogicalMessage } from '../src/engine/model/conversation'
import { SyntheticHistoryAdapter } from '../src/reference/history-adapter'
import {
  addFinalEvidence,
  agentMarkdownDelta,
  appendLiveTerminal,
  createLiveAssistantStep,
  createLiveToolCall,
  createLiveToolResult,
  liveToolForStep,
  settleLiveTerminal,
  updateLiveDelegations,
  updateLiveToolCall,
} from '../src/reference/live-run-script'
import { createScenarioTail } from '../src/reference/session-scenarios'

function liveMessage(step = 1): LogicalMessage {
  return {
    id: 'reference:m-9', index: 9, turnId: 'reference:turn', stepId: `reference:turn:step-${step}`, role: 'assistant', live: true,
    blocks: [
      block('reasoning', 'reasoning', { text: 'inspect', status: 'streaming', defaultOpen: false }),
      block('answer', 'markdown', { markdown: '' }),
    ],
  }
}

describe('public Reference scenarios', () => {
  it('materializes realistic canonical tails at global history coordinates', () => {
    const tail = createScenarioTail('transport', 180_000, 'transport-refactor')
    expect(tail.length).toBeGreaterThan(4)
    expect(tail[0]?.index).toBe(180_000 - tail.length)
    expect(tail.at(-1)?.index).toBe(179_999)
    const kinds = new Set(tail.flatMap(message => message.blocks.map(contentBlock => contentBlock.type)))
    expect([...kinds]).toEqual(expect.arrayContaining(['tool-call', 'tool-result', 'diff', 'code', 'markdown']))
  })

  it('keeps the seeded Agent-loop assistant live at the real global tail with visible plan intent', () => {
    const tail = createScenarioTail('agent-loop', 84_000, 'release-investigation')
    const live = tail.at(-1)!
    expect(live.index).toBe(83_999)
    expect(live.live).toBe(true)
    expect(live.stepId).toMatch(/:step-1$/)
    expect(live.blocks.map(contentBlock => contentBlock.type)).toEqual(['plan', 'reasoning', 'markdown'])
    const plan = live.blocks.find(contentBlock => contentBlock.type === 'plan')
    expect(plan?.type === 'plan' ? plan.data.items[0]?.status : null).toBe('in-progress')
  })

  it('overlays only the realistic tail while older stress history stays lazy', () => {
    const tail = createScenarioTail('transport', 100, 'transport-refactor')
    const adapter = new SyntheticHistoryAdapter('transport', 100, 3, false, tail)
    const old = adapter.loadRange(0, 1)[0]!
    const recent = adapter.loadRange(99, 1)[0]!
    expect(old.index).toBe(0)
    expect(old.id).toBe('transport:m-0')
    expect(recent).toBe(tail.at(-1))
  })

  it('models capability, presentation and resources independently across tool phases', () => {
    const specs = [1, 2, 3].map(step => liveToolForStep(step)!)
    expect(specs.map(spec => spec.category)).toEqual(['filesystem', 'search', 'shell'])
    expect(specs.map(spec => spec.presentation.kind)).toEqual(['resources', 'resources', 'terminal'])
    expect(specs.every(spec => (spec.resources?.length ?? 0) > 0)).toBe(true)
    expect(new Set(specs.map(spec => spec.callId)).size).toBe(3)

    const assistant = liveMessage(1)
    const callEntry = createLiveToolCall(assistant, specs[0]!)
    let callMessage: LogicalMessage = {
      id: 'reference:m-10', index: 10, turnId: callEntry.turnId, stepId: callEntry.stepId,
      role: callEntry.role, blocks: callEntry.blocks, live: callEntry.live,
    }
    callMessage = updateLiveToolCall(callMessage, specs[0]!, 'success', 100)
    const result = createLiveToolResult(callMessage, specs[0]!)
    const next = createLiveAssistantStep(assistant.turnId, 2)
    const call = callMessage.blocks.find(contentBlock => contentBlock.type === 'tool-call')
    expect(callEntry).toMatchObject({ turnId: assistant.turnId, stepId: assistant.stepId, role: 'assistant', live: true })
    expect(call?.type === 'tool-call' ? call.data.callId : null).toBe(specs[0]!.callId)
    expect(call?.type === 'tool-call' ? call.data.presentation?.kind : null).toBe('resources')
    expect(call?.type === 'tool-call' ? call.data.resources?.[0]?.kind : null).toBe('file')
    expect(result).toMatchObject({ turnId: assistant.turnId, stepId: assistant.stepId, role: 'tool' })
    expect(next).toMatchObject({ turnId: assistant.turnId, stepId: `${assistant.turnId}:step-2`, role: 'assistant', live: true })
  })

  it('keeps Plan separate from Step and represents sync plus parallel async children in one delegation block', () => {
    const first = createLiveAssistantStep('reference:turn', 1)
    let third: LogicalMessage = {
      id: 'reference:m-12', index: 12, ...createLiveAssistantStep('reference:turn', 3),
    }
    const plan = first.blocks.find(contentBlock => contentBlock.type === 'plan')
    const delegation = third.blocks.find(contentBlock => contentBlock.type === 'delegation')
    expect(plan?.type === 'plan' ? plan.data.items.map(item => item.status) : null).toEqual(['in-progress', 'pending', 'pending', 'pending'])
    expect(delegation?.type === 'delegation' ? delegation.data.runs.map(run => [run.mode, run.status]) : null).toEqual([
      ['foreground', 'completed'],
      ['background', 'running'],
      ['background', 'running'],
    ])
    expect(delegation?.type === 'delegation' ? delegation.data.runs.map(run => run.childSessionId) : null).toEqual([
      'child-review-contract', 'child-terminal-audit', 'child-resource-audit',
    ])
    third = updateLiveDelegations(third, 1)
    expect(third.blocks.find(contentBlock => contentBlock.type === 'delegation')?.data).toMatchObject({
      runs: [
        { runId: 'review-rendering-contract', status: 'completed' },
        { runId: 'audit-terminal-projection', status: 'completed' },
        { runId: 'audit-resource-semantics', status: 'running' },
      ],
    })
    third = updateLiveDelegations(third, 2)
    const settled = third.blocks.find(contentBlock => contentBlock.type === 'delegation')
    expect(settled?.type === 'delegation' ? settled.data.runs.every(run => run.status === 'completed') : false).toBe(true)
    expect(first.stepId).toBe('reference:turn:step-1')
    expect(third.stepId).toBe('reference:turn:step-3')
  })

  it('models shell evidence as a live terminal block and settles it without changing call identity', () => {
    const spec = liveToolForStep(3)!
    const callEntry = createLiveToolCall(liveMessage(3), spec)
    const callMessage: LogicalMessage = { id: 'reference:m-20', index: 20, turnId: callEntry.turnId, stepId: callEntry.stepId, role: 'assistant', live: true, blocks: callEntry.blocks }
    const resultEntry = createLiveToolResult(callMessage, spec, true)
    let result: LogicalMessage = { id: 'reference:m-21', index: 21, turnId: resultEntry.turnId, stepId: resultEntry.stepId, role: 'tool', live: true, blocks: resultEntry.blocks }
    expect(result.blocks.map(contentBlock => contentBlock.type)).toEqual(['tool-result', 'terminal'])
    const appended = appendLiveTerminal(result, spec, 0)!
    result = appended.message
    expect(appended.delta).toContain('$ pnpm test')
    const streamingTerminal = result.blocks.find(contentBlock => contentBlock.type === 'terminal')
    expect(streamingTerminal?.type === 'terminal' ? streamingTerminal.data.output : '').toContain('RUN')
    const settled = settleLiveTerminal(result, spec)
    const terminal = settled.blocks.find(contentBlock => contentBlock.type === 'terminal')
    expect(terminal?.type === 'terminal' ? terminal.data.status : null).toBe('success')
    expect(terminal?.type === 'terminal' ? terminal.data.exitCode : null).toBe(0)
  })

  it('settles an aborted shell stream as interrupted instead of a successful terminal', () => {
    const spec = liveToolForStep(3)!
    const callEntry = createLiveToolCall(liveMessage(3), spec)
    const callMessage: LogicalMessage = { id: 'reference:m-30', index: 30, turnId: callEntry.turnId, stepId: callEntry.stepId, role: 'assistant', live: true, blocks: callEntry.blocks }
    const resultEntry = createLiveToolResult(callMessage, spec, true)
    const result: LogicalMessage = { id: 'reference:m-31', index: 31, turnId: resultEntry.turnId, stepId: resultEntry.stepId, role: 'tool', live: true, blocks: resultEntry.blocks }
    const aborted = settleLiveTerminal(result, { ...spec, output: { ...spec.output, exitCode: 130 } })
    const terminal = aborted.blocks.find(contentBlock => contentBlock.type === 'terminal')
    const toolResult = aborted.blocks.find(contentBlock => contentBlock.type === 'tool-result')
    expect(terminal?.type === 'terminal' ? terminal.data.status : null).toBe('interrupted')
    expect(terminal?.type === 'terminal' ? terminal.data.exitCode : null).toBe(130)
    expect(toolResult?.type === 'tool-result' ? toolResult.data.status : null).toBe('error')
    expect(aborted.live).toBe(false)
  })

  it('streams complex GFM and adds resource-aware final evidence as canonical blocks', () => {
    const source = [1, 2, 3, 4].flatMap(step => Array.from({ length: 10 }, (_, index) => agentMarkdownDelta(step, index))).join('')
    expect(source).toContain('| Semantic layer | Owns | Must not own |')
    expect(source).toContain('```ts')
    expect(source).toContain('```text')
    expect(source).toContain('- [x]')
    expect(source).toContain('A resource can identify')
    expect(source).toContain('Plan items describe intended work')
    expect(source).toContain('child runs remain references')

    let final = liveMessage(4)
    final = addFinalEvidence(final, 'diff')
    final = addFinalEvidence(final, 'code')
    final = addFinalEvidence(final, 'artifacts')
    expect(final.blocks.map(contentBlock => contentBlock.type)).toEqual(['reasoning', 'diff', 'code', 'attachments', 'markdown'])
    const diff = final.blocks.find(contentBlock => contentBlock.type === 'diff')
    expect(diff?.type === 'diff' ? diff.data.resource.kind : null).toBe('file')
  })
})
