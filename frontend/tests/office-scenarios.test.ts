import { describe, expect, it } from 'vitest'
import { createScenarioTail } from '../src/reference/session-scenarios'
import { RECENT_SESSIONS } from '../src/reference/workspace-fixtures'

describe('office Agent Reference scenarios', () => {
  it('renders an evidence-backed executive briefing with delegation and office artifacts', () => {
    const tail = createScenarioTail('office-briefing', 62_000, 'executive-briefing')
    expect(tail).toHaveLength(7)
    expect(tail.at(-1)?.index).toBe(61_999)

    const blocks = tail.flatMap(message => message.blocks)
    expect(blocks.map(block => block.type)).toEqual(expect.arrayContaining(['plan', 'tool-call', 'tool-result', 'delegation', 'markdown', 'attachments']))

    const plan = blocks.find(block => block.type === 'plan')
    expect(plan?.type === 'plan' ? plan.data.items.every(item => item.status === 'completed') : false).toBe(true)

    const sourceTool = blocks.find(block => block.type === 'tool-call')
    expect(sourceTool?.type === 'tool-call' ? sourceTool.data.resources?.length : 0).toBe(4)
    expect(sourceTool?.type === 'tool-call' ? sourceTool.data.presentation?.kind : null).toBe('resources')

    const delegation = blocks.find(block => block.type === 'delegation')
    expect(delegation?.type === 'delegation' ? delegation.data.runs.map(run => [run.mode, run.status]) : []).toEqual([
      ['foreground', 'completed'],
      ['background', 'completed'],
      ['background', 'completed'],
    ])

    const artifacts = blocks.find(block => block.type === 'attachments')
    expect(artifacts?.type === 'attachments' ? artifacts.data.items.map(item => item.name) : []).toEqual([
      'Monday Executive Briefing.docx',
      'QBR Decision Review.pptx',
      'KPI Snapshot.xlsx',
    ])
  })

  it('correlates a proposed meeting action to approval without claiming it executed', () => {
    const tail = createScenarioTail('office-followup', 36_000, 'meeting-followup')
    expect(tail).toHaveLength(7)
    const blocks = tail.flatMap(message => message.blocks)

    const plan = blocks.find(block => block.type === 'plan')
    expect(plan?.type === 'plan' ? plan.data.items.at(-1)?.status : null).toBe('blocked')

    const action = blocks.find(block => block.type === 'tool-call' && block.data.name === 'send_meeting_followup')
    expect(action?.type === 'tool-call' ? action.data.category : null).toBe('productivity')
    expect(action?.type === 'tool-call' ? action.data.callId : null).toBe('meeting-followup-approval')
    expect(action?.type === 'tool-call' ? action.data.status : undefined).toBeUndefined()
    expect(action?.type === 'tool-call' ? action.data.durationMs : undefined).toBeUndefined()

    const seed = RECENT_SESSIONS.find(session => session.id === 'office-followup')
    expect(seed).toMatchObject({
      status: 'waiting',
      pendingInteraction: {
        id: 'approval-meeting-followup',
        kind: 'approval',
        toolName: 'send_meeting_followup',
        callId: 'meeting-followup-approval',
      },
    })
  })
})
