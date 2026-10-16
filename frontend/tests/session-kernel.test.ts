import { describe, expect, it } from 'vitest'
import { ConversationSessionKernel } from '../src/engine/conversation/session-kernel'
import { block } from '../src/engine/model/conversation'
import { appendMarkdownContent, appendReasoningContent, settleReasoning } from '../src/engine/model/message-mutations'
import { SyntheticHistoryAdapter } from '../src/reference/history-adapter'
import { SyntheticStreamController } from '../src/reference/stream-controller'
import { contextOccupancyPercent } from '../src/engine/conversation/session-semantics'

function appendTurn(kernel: ConversationSessionKernel, prompt: string): number {
  const userIndex = kernel.count
  const turnId = `${kernel.id}:test-turn-${userIndex}`
  const stepId = `${turnId}:step-0`
  const indexes = kernel.appendCanonicalMessages([
    { turnId, stepId, role: 'user', blocks: [block('prompt', 'markdown', { markdown: prompt })] },
    { turnId, stepId, role: 'assistant', blocks: [block('reasoning', 'reasoning', { text: '', tokenCount: 0, durationMs: 0, status: 'streaming' }), block('answer', 'markdown', { markdown: '' })], live: true },
  ])
  return indexes[1]!
}

describe('ConversationSessionKernel', () => {
  it('keeps unknown provider context limits unknown instead of showing 100 percent', () => {
    const kernel = new ConversationSessionKernel({ id: 'unknown', title: 'Unknown', status: 'idle', logicalCount: 0 }, new SyntheticHistoryAdapter('unknown', 0, 1))
    kernel.setAccounting({ inputTokens: 100 }, { projectedTokens: 100, contextWindow: 0 })
    expect(contextOccupancyPercent(kernel.context)).toBeNull()
  })
  it('stores canonical content/lifecycle/accounting without inventing provider behavior', () => {
    const descriptor = {
      id: 'resume', title: 'Resume', status: 'idle' as const, logicalCount: 100,
      usage: { inputTokens: 20, outputTokens: 10, cacheReadTokens: 40, cacheWriteTokens: 5, reasoningTokens: 2 },
      context: { projectedTokens: 2_000, contextWindow: 128_000 },
      lastTurnReason: 'completed' as const,
    }
    const kernel = new ConversationSessionKernel(descriptor, new SyntheticHistoryAdapter('resume', 100, 9))
    const assistantIndex = appendTurn(kernel, 'continue this task')
    expect(kernel.startExecution(assistantIndex)).toBe(true)
    expect(kernel.startExecution(assistantIndex)).toBe(false)
    expect(kernel.currentAssistantIndex).toBe(assistantIndex)
    expect(kernel.count).toBe(102)
    expect(kernel.getMessage(100)).toMatchObject({ role: 'user', blocks: [{ id: 'prompt', type: 'markdown' }] })
    expect(kernel.getMessage(101)).toMatchObject({ live: true, blocks: [{ type: 'reasoning' }, { type: 'markdown' }] })

    const reasoning = appendReasoningContent(kernel.getMessage(assistantIndex), 'Inspect stable identities before rendering. ', 20, 11)!
    kernel.replaceCanonicalMessage(assistantIndex, reasoning.message, { kind: 'append-reasoning', blockId: reasoning.blockId, delta: 'Inspect stable identities before rendering. ' })
    expect(kernel.lastEvent.contentPatch).toMatchObject({ kind: 'append-reasoning', blockId: 'reasoning' })

    const answer = appendMarkdownContent(kernel.getMessage(assistantIndex), 'A provider-normalized streamed answer. ')
    kernel.replaceCanonicalMessage(assistantIndex, answer.message, { kind: 'append-markdown', blockId: answer.blockId, delta: 'A provider-normalized streamed answer. ' })
    expect(kernel.lastEvent.contentPatch).toMatchObject({ kind: 'append-markdown', blockId: 'answer' })

    kernel.setAccounting({ inputTokens: 25, outputTokens: 19, cacheReadTokens: 44, cacheWriteTokens: 6, reasoningTokens: 13 }, { projectedTokens: 2_100, contextWindow: 128_000 })
    expect(kernel.usage).toEqual({ inputTokens: 25, outputTokens: 19, cacheReadTokens: 44, cacheWriteTokens: 6, reasoningTokens: 13 })

    const settled = settleReasoning(kernel.getMessage(assistantIndex), 'complete')
    kernel.replaceCanonicalMessage(assistantIndex, { ...settled, live: false })
    kernel.finishExecution('completed')
    expect(kernel.status).toBe('idle')
    expect(kernel.lastTurnReason).toBe('completed')
    expect(kernel.getMessage(assistantIndex)).toMatchObject({ live: false, blocks: [{ type: 'reasoning', data: { status: 'complete' } }, { type: 'markdown' }] })
  })

  it('counts one Turn across separately appended Agent-loop records and advances the execution target without resetting lifecycle', () => {
    const descriptor = { id: 'loop', title: 'Loop', status: 'idle' as const, logicalCount: 0, turnCount: 0, stepCount: 0 }
    const kernel = new ConversationSessionKernel(descriptor, new SyntheticHistoryAdapter('loop', 0, 4))
    const turnId = 'loop:turn-1'
    const step0 = `${turnId}:step-0`
    const [user, assistant0] = kernel.appendCanonicalMessages([
      { turnId, stepId: step0, role: 'user', blocks: [block('prompt', 'markdown', { markdown: 'inspect and verify' })] },
      { turnId, stepId: step0, role: 'assistant', blocks: [block('answer', 'markdown', { markdown: 'checking' })], live: true },
    ])
    expect(user).toBe(0)
    expect(kernel.turnCount).toBe(1)
    expect(kernel.stepCount).toBe(1)
    expect(kernel.startExecution(assistant0!)).toBe(true)

    const [toolResult] = kernel.appendCanonicalMessages([{
      turnId,
      stepId: step0,
      role: 'tool',
      blocks: [block('tool-result', 'tool-result', { name: 'read_file', callId: 'call-1', category: 'filesystem', status: 'success', output: { lines: 12 } })],
    }])
    expect(toolResult).toBe(2)
    expect(kernel.turnCount).toBe(1)
    expect(kernel.stepCount).toBe(1)

    const step1 = `${turnId}:step-1`
    const [assistant1] = kernel.appendCanonicalMessages([{
      turnId,
      stepId: step1,
      role: 'assistant',
      blocks: [block('answer', 'markdown', { markdown: 'next model step' })],
      live: true,
    }])
    expect(kernel.turnCount).toBe(1)
    expect(kernel.stepCount).toBe(2)
    kernel.continueExecutionAt(assistant1!)
    expect(kernel.currentAssistantIndex).toBe(assistant1)
    expect(kernel.status).toBe('working')

    expect(() => kernel.continueExecutionAt(toolResult!)).toThrow(/assistant message/)
    kernel.finishExecution('completed')
    expect(() => kernel.continueExecutionAt(assistant1!)).toThrow(/not working/)
  })

  it('never infers a restored execution target or Turn outcome from live status', () => {
    const history = new SyntheticHistoryAdapter('restore', 12, 5)
    const kernel = new ConversationSessionKernel({ id: 'restore', title: 'Restore', status: 'working', logicalCount: 12 }, history)
    expect(kernel.status).toBe('working')
    expect(kernel.currentAssistantIndex).toBeNull()
    expect(kernel.lastTurnReason).toBeNull()

    const waiting = new ConversationSessionKernel({
      id: 'waiting', title: 'Waiting', status: 'waiting', logicalCount: 0,
      pendingInteraction: { id: 'q0', kind: 'question', title: 'Need input', detail: 'Choose one.' },
    }, new SyntheticHistoryAdapter('waiting', 0, 1))
    expect(waiting.status).toBe('waiting')
    expect(waiting.lastTurnReason).toBeNull()

    const tail = history.loadRange(11, 1)[0]!
    const explicitHistory = new SyntheticHistoryAdapter('explicit', 1, 1, false, [{
      ...tail,
      id: 'explicit:m-0',
      index: 0,
      role: 'assistant',
      live: true,
    }])
    const explicit = new ConversationSessionKernel({ id: 'explicit', title: 'Explicit', status: 'working', logicalCount: 1, activeAssistantIndex: 0 }, explicitHistory)
    expect(explicit.currentAssistantIndex).toBe(0)
    expect(explicit.summary.activeAssistantIndex).toBe(0)
  })

  it('correlates typed interaction resolutions by exact blocker identity without settling the Turn implicitly', () => {
    const kernel = new ConversationSessionKernel({ id: 'interaction', title: 'Interaction', status: 'idle', logicalCount: 0 }, new SyntheticHistoryAdapter('interaction', 0, 1))
    const assistantIndex = appendTurn(kernel, 'inspect before asking')
    expect(kernel.startExecution(assistantIndex)).toBe(true)

    kernel.requestInteraction({ id: 'q1', kind: 'question', title: 'Choose behavior', detail: 'Which fallback?' })
    expect(kernel.status).toBe('waiting')
    expect(kernel.pendingInteraction?.kind).toBe('question')
    expect(kernel.currentAssistantIndex).toBeNull()
    expect(kernel.lastTurnReason).toBeNull()
    expect(() => kernel.requestInteraction({ id: 'a2', kind: 'approval', title: 'Other', detail: 'Other?' })).toThrow(/already pending/)
    expect(() => kernel.resolveInteraction({ interactionId: 'stale-q', kind: 'question', answer: 'old answer' })).toThrow(/stale/)
    expect(() => kernel.resolveInteraction({ interactionId: 'q1', kind: 'approval', approved: true })).toThrow(/expects question/)
    expect(() => kernel.finishExecution('completed')).toThrow(/cannot finish execution/)

    const exposed = kernel.pendingInteraction
    expect(exposed).not.toBe(kernel.pendingInteraction)
    if (exposed) exposed.title = 'tampered outside the kernel'
    expect(kernel.pendingInteraction?.title).toBe('Choose behavior')
    expect(kernel.summary.pendingInteraction).not.toBe(kernel.pendingInteraction)

    kernel.resolveInteraction({ interactionId: 'q1', kind: 'question', answer: 'Keep the last accepted configuration.' })
    expect(kernel.pendingInteraction).toBeNull()
    expect(kernel.status).toBe('idle')
    expect(kernel.lastTurnReason).toBeNull()

    expect(kernel.startExecution(assistantIndex)).toBe(true)
    kernel.finishExecution('completed')
    expect(kernel.lastTurnReason).toBe('completed')
  })

  it('leaves denial and no-answer outcomes to the execution adapter', () => {
    const skippedQuestion = new ConversationSessionKernel({
      id: 'skip-question', title: 'Skip question', status: 'waiting', logicalCount: 0,
      pendingInteraction: { id: 'q2', kind: 'question', title: 'Optional detail', detail: 'Provide extra context?' },
    }, new SyntheticHistoryAdapter('skip-question', 0, 1))
    skippedQuestion.resolveInteraction({ interactionId: 'q2', kind: 'question', answer: null })
    expect(skippedQuestion.status).toBe('idle')
    expect(skippedQuestion.lastTurnReason).toBeNull()

    const approvalKernel = new ConversationSessionKernel({
      id: 'approval', title: 'Approval', status: 'waiting', logicalCount: 0,
      pendingInteraction: { id: 'a1', kind: 'approval', title: 'Edit config', detail: 'Apply patch?', toolName: 'edit_file', callId: 'call-edit-1' },
    }, new SyntheticHistoryAdapter('approval', 0, 1))
    expect(approvalKernel.pendingInteraction).toMatchObject({ id: 'a1', callId: 'call-edit-1' })
    approvalKernel.resolveInteraction({ interactionId: 'a1', kind: 'approval', approved: false })
    expect(approvalKernel.status).toBe('idle')
    expect(approvalKernel.lastTurnReason).toBeNull()

    approvalKernel.finishExecution('aborted')
    expect(approvalKernel.status).toBe('idle')
    expect(approvalKernel.lastTurnReason).toBe('aborted')
  })

  it('owns a monotonic message revision for every canonical replacement', () => {
    const descriptor = { id: 'revision', title: 'Revision', status: 'idle' as const, logicalCount: 0 }
    const kernel = new ConversationSessionKernel(descriptor, new SyntheticHistoryAdapter('revision', 0, 4))
    const [index] = kernel.appendCanonicalMessages([{
      turnId: 'revision:turn-1',
      stepId: 'revision:turn-1:step-0',
      role: 'assistant',
      blocks: [block('answer', 'markdown', { markdown: 'first' })],
    }])
    const target = index!
    const initial = kernel.getMessage(target)
    expect(initial.revision).toBe(0)

    kernel.replaceCanonicalMessage(target, {
      ...initial,
      revision: 0,
      blocks: [block('answer', 'markdown', { markdown: 'second' })],
    })
    const second = kernel.getMessage(target)
    expect(second.revision).toBe(1)

    kernel.replaceCanonicalMessage(target, {
      ...second,
      revision: 0,
      blocks: [block('answer', 'markdown', { markdown: 'third' })],
    })
    expect(kernel.getMessage(target).revision).toBe(2)
    expect(kernel.getMessage(target).blocks[0]).toMatchObject({ type: 'markdown', data: { markdown: 'third' } })
  })

  it('delivers every semantic mutation in producer order while summary notification stays coalesced', async () => {
    const descriptor = { id: 'events', title: 'Events', status: 'idle' as const, logicalCount: 0 }
    const kernel = new ConversationSessionKernel(descriptor, new SyntheticHistoryAdapter('events', 0, 4))
    const events: string[] = []
    let summaries = 0
    const unsubscribeEvents = kernel.subscribeEvents(event => events.push(event.contentPatch?.kind ?? event.kind))
    const unsubscribeSummary = kernel.subscribe(() => { summaries += 1 })

    const index = appendTurn(kernel, 'first')
    kernel.startExecution(index)
    const reasoning = appendReasoningContent(kernel.getMessage(index), 'r', 1, 1)!
    kernel.replaceCanonicalMessage(index, reasoning.message, { kind: 'append-reasoning', blockId: reasoning.blockId, delta: 'r' })
    let answer = appendMarkdownContent(kernel.getMessage(index), 'a')
    kernel.replaceCanonicalMessage(index, answer.message, { kind: 'append-markdown', blockId: answer.blockId, delta: 'a' })
    answer = appendMarkdownContent(kernel.getMessage(index), 'b')
    kernel.replaceCanonicalMessage(index, answer.message, { kind: 'append-markdown', blockId: answer.blockId, delta: 'b' })

    expect(events).toEqual(['append', 'status', 'append-reasoning', 'append-markdown', 'append-markdown'])
    expect(summaries).toBe(0)
    await Promise.resolve()
    expect(summaries).toBe(1)
    unsubscribeEvents()
    unsubscribeSummary()
  })

  it('queues follow-ups while working instead of coupling them to the viewport', () => {
    const descriptor = { id: 'queue', title: 'Queue', status: 'idle' as const, logicalCount: 0 }
    const kernel = new ConversationSessionKernel(descriptor, new SyntheticHistoryAdapter('queue', 0, 4))
    const execution = new SyntheticStreamController(kernel)
    expect(execution.submit('first')).toBe('started')
    expect(execution.submit('second')).toBe('queued')
    expect(kernel.queuedPrompts).toBe(1)
    execution.dispose()
  })

  it('records last-turn failure without making the historical session non-resumable', () => {
    const descriptor = { id: 'failed', title: 'Failed', status: 'idle' as const, logicalCount: 20, lastTurnReason: 'error' as const }
    const kernel = new ConversationSessionKernel(descriptor, new SyntheticHistoryAdapter('failed', 20, 4))
    const execution = new SyntheticStreamController(kernel)
    expect(execution.submit('retry with a new turn')).toBe('started')
    execution.fail({ code: 'PROVIDER_TIMEOUT', message: 'timeout', status: 504 })
    expect(kernel.status).toBe('idle')
    expect(kernel.lastTurnReason).toBe('error')
    expect(kernel.lastFailure?.code).toBe('PROVIDER_TIMEOUT')
    expect(execution.submit('continue after failure')).toBe('started')
    expect(kernel.lastFailure).toBeNull()
    execution.dispose()
  })
})
