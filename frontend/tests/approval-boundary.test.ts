import { describe, expect, it } from 'vitest'
import { ConversationSessionKernel } from '../src/engine/conversation/session-kernel'
import { createDefaultContentProjectors } from '../src/engine/presentation/projector-registry'
import { block, type LogicalMessage } from '../src/engine/model/conversation'
import { SyntheticHistoryAdapter } from '../src/reference/history-adapter'

describe('generic approval boundary', () => {
  it('keeps interaction identity distinct from correlated tool-call identity', () => {
    const kernel = new ConversationSessionKernel({
      id: 'approval-boundary', title: 'Approval boundary', status: 'waiting', logicalCount: 0,
      pendingInteraction: {
        id: 'approval-42', kind: 'approval', title: 'Approve action', detail: 'Review exact action.',
        toolName: 'external_action', callId: 'tool-call-17',
      },
    }, new SyntheticHistoryAdapter('approval-boundary', 0, 1))

    expect(kernel.pendingInteraction).toMatchObject({ id: 'approval-42', callId: 'tool-call-17' })
    expect(() => kernel.resolveInteraction({ interactionId: 'approval-old', kind: 'approval', approved: true })).toThrow(/stale/)
    expect(kernel.pendingInteraction?.id).toBe('approval-42')
    kernel.resolveInteraction({ interactionId: 'approval-42', kind: 'approval', approved: false })
    expect(kernel.pendingInteraction).toBeNull()
    expect(kernel.status).toBe('idle')
  })

  it('projects a proposed tool call without fabricating running or success', () => {
    const message: LogicalMessage = {
      id: 's:m-0', index: 0, turnId: 's:t-0', stepId: 's:t-0:step-0', role: 'assistant',
      blocks: [block('proposed', 'tool-call', {
        name: 'external_action', callId: 'tool-call-17', category: 'productivity',
        input: { requiresApproval: true },
      })],
    }
    const [unit] = createDefaultContentProjectors().project(message, message.blocks[0]!, 0)
    expect(unit?.payload).toMatchObject({ phase: 'call', callId: 'tool-call-17' })
    expect(unit?.payload.status).toBeUndefined()
    expect(unit?.payload.durationMs).toBeUndefined()
  })
})
