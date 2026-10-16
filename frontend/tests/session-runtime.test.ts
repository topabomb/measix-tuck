import { describe, expect, it } from 'vitest'
import { ConversationSessionRuntime, SHIFT_MESSAGES, WINDOW_MESSAGES } from '../src/engine/runtime/session-runtime'
import { ConversationSessionKernel } from '../src/engine/conversation/session-kernel'
import { block } from '../src/engine/model/conversation'
import { appendMarkdownContent } from '../src/engine/model/message-mutations'
import { SyntheticHistoryAdapter } from '../src/reference/history-adapter'

function runtimeAt(position: number, status: 'idle' | 'working' = 'idle') {
  const descriptor = { id: 'identity-test', title: 'identity test', status, logicalCount: 1_000_000 } as const
  const kernel = new ConversationSessionKernel(descriptor, new SyntheticHistoryAdapter(descriptor.id, descriptor.logicalCount, 73, status === 'working'))
  const runtime = new ConversationSessionRuntime(kernel, {
    logicalPosition: position,
    anchorUnitId: null,
    anchorOffsetPx: 0,
    followTail: status === 'working',
    atVisualBottom: position === descriptor.logicalCount - 1,
    draftText: '',
  })
  return { kernel, runtime }
}

function appendTurn(kernel: ConversationSessionKernel, prompt: string): number {
  const turnId = `${kernel.id}:test-${kernel.count}`
  const indexes = kernel.appendCanonicalMessages([
    { turnId, role: 'user', blocks: [block('prompt', 'markdown', { markdown: prompt })] },
    { turnId, role: 'assistant', blocks: [block('answer', 'markdown', { markdown: '' })], live: true },
  ])
  return indexes[1]!
}

describe('ConversationSessionRuntime', () => {
  it('appends visible records without moving a paused reader in a short task conversation', async () => {
    const descriptor = {id:'task-attachment',title:'task attachment',status:'working',logicalCount:4} as const
    const kernel = new ConversationSessionKernel(descriptor,new SyntheticHistoryAdapter(descriptor.id,4,73,true))
    const runtime = new ConversationSessionRuntime(kernel,{logicalPosition:0,anchorUnitId:null,anchorOffsetPx:0,followTail:false,atVisualBottom:false,draftText:''})
    const index=appendTurn(kernel,'task continues after opening chat')
    await Promise.resolve()
    expect(runtime.range).toEqual({start:0,end:6})
    expect(runtime.activeUnits.some(unit=>unit.messageIndex===index-1)).toBe(true)
    expect(runtime.currentLogicalPosition).toBe(0)
    expect(runtime.followTail).toBe(false)
    const patched=appendMarkdownContent(kernel.getMessage(index),'new tool result')
    kernel.replaceCanonicalMessage(index,patched.message,{kind:'append-markdown',blockId:patched.blockId,delta:'new tool result'})
    await Promise.resolve()
    expect(runtime.activeUnits.some(unit=>String(unit.payload.markdown??'').includes('new tool result'))).toBe(true)
    runtime.dispose()
  })
  it('rehydrates a reading checkpoint whose visible anchor is the row after its semantic reader', () => {
    const {kernel,runtime}=runtimeAt(500_000)
    runtime.jump(500_000)
    const anchor=runtime.activeUnits.find(unit=>unit.messageIndex===500_001)!
    expect(anchor).toBeDefined()
    const snapshot=runtime.snapshot(anchor.id,35)
    runtime.dispose()
    const restored=new ConversationSessionRuntime(kernel,snapshot)
    expect(restored.range).toEqual({start:498_976,end:501_024})
    expect(restored.projection.order).toContain(anchor.id)
    expect(restored.currentLogicalPosition).toBe(500_000)
    expect(restored.rememberedSnapshot.anchorOffsetPx).toBe(35)
    restored.dispose()
  })

  it('materializes an extended tail even when a short window keeps the same start', () => {
    const kernel = new ConversationSessionKernel({id:'short',title:'short',status:'idle',logicalCount:57}, new SyntheticHistoryAdapter('short',57,73,false))
    const runtime = new ConversationSessionRuntime(kernel,{logicalPosition:56,anchorUnitId:null,anchorOffsetPx:0,followTail:false,atVisualBottom:false,draftText:''})
    appendTurn(kernel,'arrived while reading')
    expect(runtime.range).toEqual({start:0,end:59})
    expect(runtime.planShiftBackward()).toBeNull()
    expect(runtime.range).toEqual({start:0,end:59})
    expect(runtime.range).toEqual({start:0,end:59})
    expect(runtime.activeUnits.some(unit => unit.messageIndex === 58)).toBe(true)
    expect(runtime.planShiftForward()).toBeNull()
    runtime.dispose()
  })

  it('projects only the incoming slice and retains RenderUnit objects on prepend', () => {
    const { runtime } = runtimeAt(500_000)
    const old = [...runtime.activeUnits]
    const oldRange = { ...runtime.range }
    const plan = runtime.planShiftBackward()!
    expect(plan.next.end - plan.next.start).toBe(WINDOW_MESSAGES)
    expect(oldRange.start - plan.next.start).toBe(SHIFT_MESSAGES)
    const retained = old.find(unit => unit.messageIndex >= oldRange.start && unit.messageIndex < plan.next.end)!
    expect(plan.final.find(unit => unit.id === retained.id)).toBe(retained)
    runtime.dispose()
  })

  it('does not roll back streamed content when a measured window shift commits later', () => {
    const {kernel,runtime} = runtimeAt(999_999)
    appendTurn(kernel,'background turn')
    const plan = runtime.planShiftForward()!
    runtime.applyIntermediate(plan.intermediate,false)
    const index = kernel.count - 1
    const patched = appendMarkdownContent(kernel.getMessage(index),'result during layout')
    kernel.replaceCanonicalMessage(index,patched.message,{kind:'append-markdown',blockId:patched.blockId,delta:'result during layout'})
    runtime.commitShift(plan,true)
    expect(runtime.activeUnits.filter(unit => unit.messageIndex === index).some(unit => String(unit.payload.markdown ?? '').includes('result during layout'))).toBe(true)
    runtime.dispose()
  })

  it('keeps a requested jump separate from the committed semantic reader', () => {
    const { runtime } = runtimeAt(500_000)
    runtime.jump(100_000)
    expect(runtime.range.start).toBeLessThanOrEqual(100_000)
    expect(runtime.range.end).toBeGreaterThan(100_000)
    expect(runtime.currentLogicalPosition).toBe(500_000)

    runtime.setReaderPosition(100_000, false)
    expect(runtime.currentLogicalPosition).toBe(100_000)
    runtime.dispose()
  })

  it('keeps a history reader stable while the kernel grows off-screen', async () => {
    const { kernel, runtime } = runtimeAt(500_000)
    const before = runtime.currentLogicalPosition
    appendTurn(kernel, 'background work')
    await Promise.resolve()
    expect(runtime.logicalCount).toBe(1_000_002)
    expect(runtime.currentLogicalPosition).toBe(before)
    expect(runtime.messagesAfterCurrent).toBeGreaterThan(500_000)
    runtime.dispose()
  })

  it('rehydrates appended canonical turns from the lightweight kernel', async () => {
    const { kernel, runtime } = runtimeAt(999_999)
    const assistantIndex = appendTurn(kernel, 'continue the old session')
    const patched = appendMarkdownContent(kernel.getMessage(assistantIndex), 'new answer')
    kernel.replaceCanonicalMessage(assistantIndex, patched.message, { kind: 'append-markdown', blockId: patched.blockId, delta: 'new answer' })
    await Promise.resolve()
    runtime.jump(kernel.count - 1)
    const tailNodes = runtime.projection.order
      .map(id => runtime.projection.getNode(id))
      .filter(node => node?.messageIndex === kernel.count - 1)
    expect(tailNodes.some(node => String(node?.payload.markdown ?? '').includes('new answer'))).toBe(true)
    expect(runtime.logicalCount).toBe(1_000_002)
    runtime.dispose()
  })
})
