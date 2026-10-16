import type { ConversationExecutionController, InteractionResolution, LlmFailure, SubmitDisposition, TokenUsage } from '../engine/conversation/contracts'
import type { ConversationSessionKernel } from '../engine/conversation/session-kernel'
import { block, type LogicalMessage } from '../engine/model/conversation'
import {
  appendMarkdownContent,
  appendReasoningContent,
  markdownText,
  replaceMarkdownContent,
  settleReasoning,
} from '../engine/model/message-mutations'
import {
  addFinalEvidence,
  AGENT_FINAL_ARTIFACT_PUBLISH,
  AGENT_FINAL_CODE_PUBLISH,
  AGENT_FINAL_DIFF_PUBLISH,
  AGENT_FINAL_STEP,
  AGENT_MAX_PUBLISHES,
  AGENT_REASONING_PUBLISHES,
  AGENT_TOOL_CALL_PUBLISH,
  AGENT_TOOL_RESULT_PUBLISH,
  agentMarkdownDelta,
  agentReasoningDelta,
  appendLiveTerminal,
  createLiveAssistantStep,
  createLiveToolCall,
  createLiveToolResult,
  liveToolForStep,
  parseStepOrdinal,
  settleLiveTerminal,
  settleReasoningBlock,
  STRESS_REASONING_PUBLISHES,
  stressMarkdownDelta,
  stressReasoningDelta,
  updateLiveDelegations,
  updateLivePlan,
  updateLiveToolCall,
} from './live-run-script'

export type ReferencePlaybackMode = 'standard' | 'stress' | 'agent-loop'
type AgentPlaybackPhase = 'model' | 'tool'

const STRESS_MAX_PUBLISHES = 600
const AGENT_FINAL_COMPLETE_PUBLISH = AGENT_FINAL_ARTIFACT_PUBLISH + 12

/** Reference-only playback driver. Synthetic timing/accounting and scripted scenario output never enter Engine policy. */
export class SyntheticStreamController implements ConversationExecutionController {
  #kernel: ConversationSessionKernel
  #mode: ReferencePlaybackMode
  #timer: ReturnType<typeof setInterval> | null = null
  #framePending = false
  #pendingDelta = ''
  #runPublishes = 0
  #stepPublishes = 0
  #markdownTicks = 0
  #agentPhase: AgentPlaybackPhase = 'model'
  #planMessageIndex: number | null = null
  #delegationMessageIndex: number | null = null
  #toolResultIndex: number | null = null

  rate = 20
  ingressTicks = 0
  publishTicks = 0

  constructor(kernel: ConversationSessionKernel, mode: ReferencePlaybackMode = 'standard') {
    this.#kernel = kernel
    this.#mode = mode
  }

  get running(): boolean { return this.#timer !== null && this.#kernel.status === 'working' }
  get mode(): ReferencePlaybackMode { return this.#mode }

  start(reset = true): void {
    if (this.#kernel.status !== 'working' || this.#kernel.currentAssistantIndex === null) return
    this.#stopTimer()
    if (reset) {
      this.#runPublishes = 0
      this.#stepPublishes = 0
      this.#markdownTicks = 0
      this.#agentPhase = 'model'
      this.#toolResultIndex = null
      this.#delegationMessageIndex = null
      if (this.#mode === 'agent-loop') {
        const current = this.#kernel.currentAssistantIndex
        if (current !== null && parseStepOrdinal(this.#kernel.getMessage(current)) === 1) this.#planMessageIndex = current
      }
    }
    this.#timer = setInterval(() => this.#ingest(), Math.max(16, Math.round(1000 / this.rate)))
  }

  pause(): void { this.#stopTimer() }

  abort(): void {
    this.#stopTimer()
    if (this.#toolResultIndex !== null) {
      const result = this.#kernel.getMessage(this.#toolResultIndex)
      const spec = liveToolForStep(parseStepOrdinal(result))
      if (spec?.terminalChunks) this.#kernel.replaceCanonicalMessage(this.#toolResultIndex, settleLiveTerminal(result, { ...spec, output: { ...spec.output, exitCode: 130 } }))
      this.#toolResultIndex = null
    }
    const index = this.#kernel.currentAssistantIndex
    if (index !== null) {
      const current = this.#kernel.getMessage(index)
      if (current.blocks.some(entry => entry.type === 'reasoning' || entry.type === 'markdown')) {
        const settled = settleReasoning(current, 'interrupted')
        const patched = appendMarkdownContent(settled, '\n\n_Stopped by user._')
        this.#kernel.replaceCanonicalMessage(index, { ...patched.message, live: false })
      } else {
        this.#kernel.replaceCanonicalMessage(index, { ...current, live: false })
      }
    }
    this.#kernel.clearQueue()
    this.#kernel.finishExecution('aborted')
  }

  fail(failure: LlmFailure): void {
    this.#stopTimer()
    const index = this.#kernel.currentAssistantIndex
    if (index !== null) {
      const current = this.#kernel.getMessage(index)
      const next = current.blocks.some(entry => entry.type === 'reasoning') ? settleReasoning(current, 'interrupted') : current
      this.#kernel.replaceCanonicalMessage(index, { ...next, live: false })
    }
    this.#kernel.finishExecution('error', failure)
  }

  submit(prompt: string): SubmitDisposition {
    const text = prompt.trim()
    if (!text || this.#kernel.pendingInteraction) return 'blocked'
    if (this.#kernel.status === 'working') return this.#kernel.enqueue(text) ? 'queued' : 'blocked'
    if (!this.#beginSyntheticTurn(text)) return 'blocked'
    this.resetTelemetry()
    this.start(true)
    return 'started'
  }

  resolveInteraction(resolution: InteractionResolution): void { this.#kernel.resolveInteraction(resolution) }

  setRate(rate: number): void {
    const wasRunning = this.running
    this.rate = Math.max(1, Math.floor(rate))
    if (wasRunning) this.start(false)
  }

  resetTelemetry(): void { this.ingressTicks = 0; this.publishTicks = 0 }
  dispose(): void { this.#stopTimer() }

  #beginSyntheticTurn(prompt: string): boolean {
    const userIndex = this.#kernel.count
    const turnId = `${this.#kernel.id}:runtime-turn-${userIndex}`
    const userStepId = `${turnId}:step-0`
    const assistant = this.#mode === 'agent-loop'
      ? createLiveAssistantStep(turnId, 1)
      : {
          turnId,
          stepId: userStepId,
          role: 'assistant' as const,
          blocks: [
            block('reasoning', 'reasoning', { text: '', tokenCount: 0, durationMs: 0, defaultOpen: false, status: 'streaming' }),
            block('answer', 'markdown', { markdown: '' }),
          ],
          live: true,
        }
    const indexes = this.#kernel.appendCanonicalMessages([
      { turnId, stepId: userStepId, role: 'user', blocks: [block('prompt', 'markdown', { markdown: prompt })] },
      assistant,
    ])
    const assistantIndex = indexes[1]
    if (assistantIndex === undefined || !this.#kernel.startExecution(assistantIndex)) return false
    this.#planMessageIndex = this.#mode === 'agent-loop' ? assistantIndex : null
    this.#delegationMessageIndex = null
    this.#toolResultIndex = null
    this.#accountPrompt(prompt)
    return true
  }

  #stopTimer(): void {
    if (this.#timer) clearInterval(this.#timer)
    this.#timer = null
    this.#framePending = false
    this.#pendingDelta = ''
  }

  #ingest(): void {
    this.ingressTicks += 1
    if (this.#mode === 'agent-loop' && this.#agentPhase === 'tool') this.#pendingDelta = 'tool-progress'
    else {
      const reasoningPublishes = this.#mode === 'agent-loop' ? AGENT_REASONING_PUBLISHES : STRESS_REASONING_PUBLISHES
      this.#pendingDelta = this.#stepPublishes < reasoningPublishes
        ? this.#mode === 'agent-loop'
          ? agentReasoningDelta(this.#currentStepOrdinal(), this.ingressTicks)
          : stressReasoningDelta(this.ingressTicks)
        : 'answer-ready'
    }
    if (this.#framePending) return
    this.#framePending = true
    scheduleFrame(() => { this.#framePending = false; this.#publish() })
  }

  #publish(): void {
    const index = this.#kernel.currentAssistantIndex
    if (!this.#pendingDelta || index === null) return
    if (this.#mode === 'agent-loop') this.#publishAgentLoop(index)
    else this.#publishStress(index)
  }

  #publishStress(index: number): void {
    const reasoningPhase = this.#stepPublishes < STRESS_REASONING_PUBLISHES
    const delta = reasoningPhase ? this.#pendingDelta : stressMarkdownDelta(this.#markdownTicks++)
    this.#pendingDelta = ''

    if (reasoningPhase) this.#appendReasoning(index, delta)
    else {
      if (this.#stepPublishes === STRESS_REASONING_PUBLISHES) this.#settleCurrentReasoning(index)
      this.#appendMarkdown(index, delta)
    }

    this.#recordPublish()
    if (this.#runPublishes < STRESS_MAX_PUBLISHES) return
    this.#completeCurrentTurn()
  }

  #publishAgentLoop(index: number): void {
    if (this.#agentPhase === 'tool') {
      this.#publishAgentTool(index)
      return
    }

    const stepOrdinal = this.#currentStepOrdinal()
    const reasoningPhase = this.#stepPublishes < AGENT_REASONING_PUBLISHES
    const delta = reasoningPhase ? this.#pendingDelta : agentMarkdownDelta(stepOrdinal, this.#markdownTicks++)
    this.#pendingDelta = ''

    if (reasoningPhase) this.#appendReasoning(index, delta)
    else {
      if (this.#stepPublishes === AGENT_REASONING_PUBLISHES) this.#settleCurrentReasoning(index)
      this.#appendMarkdown(index, delta)
    }

    if (stepOrdinal === AGENT_FINAL_STEP) {
      if (this.#stepPublishes === AGENT_FINAL_DIFF_PUBLISH) this.#kernel.replaceCanonicalMessage(index, addFinalEvidence(this.#kernel.getMessage(index), 'diff'))
      if (this.#stepPublishes === AGENT_FINAL_CODE_PUBLISH) this.#kernel.replaceCanonicalMessage(index, addFinalEvidence(this.#kernel.getMessage(index), 'code'))
      if (this.#stepPublishes === AGENT_FINAL_ARTIFACT_PUBLISH) this.#kernel.replaceCanonicalMessage(index, addFinalEvidence(this.#kernel.getMessage(index), 'artifacts'))
    }

    this.#recordPublish()

    if (stepOrdinal < AGENT_FINAL_STEP && this.#stepPublishes >= AGENT_TOOL_CALL_PUBLISH) {
      this.#beginAgentTool(index, stepOrdinal)
      return
    }
    if (stepOrdinal === AGENT_FINAL_STEP && (this.#stepPublishes > AGENT_FINAL_COMPLETE_PUBLISH || this.#runPublishes >= AGENT_MAX_PUBLISHES)) this.#completeCurrentTurn()
  }

  #beginAgentTool(index: number, stepOrdinal: number): void {
    const current = this.#kernel.getMessage(index)
    const spec = liveToolForStep(stepOrdinal)
    if (!spec) throw new Error(`Agent-loop tool spec missing for step ${stepOrdinal}`)
    const settled = settleReasoningBlock(current) ?? current
    this.#kernel.replaceCanonicalMessage(index, { ...settled, live: false })
    const [callIndex] = this.#kernel.appendCanonicalMessages([createLiveToolCall(current, spec)])
    if (callIndex === undefined) throw new Error('agent-loop tool call record missing')
    this.#kernel.continueExecutionAt(callIndex)
    this.#agentPhase = 'tool'
    this.#stepPublishes = 0
    this.#markdownTicks = 0
    this.#toolResultIndex = null
  }

  #publishAgentTool(index: number): void {
    const current = this.#kernel.getMessage(index)
    const stepOrdinal = parseStepOrdinal(current)
    const spec = liveToolForStep(stepOrdinal)
    if (!spec) throw new Error(`Agent-loop tool spec missing for step ${stepOrdinal}`)
    this.#pendingDelta = ''

    if (this.#stepPublishes === 0) this.#kernel.replaceCanonicalMessage(index, updateLiveToolCall(current, spec, 'running', 25))
    else if (this.#stepPublishes === 2) this.#kernel.replaceCanonicalMessage(index, updateLiveToolCall(this.#kernel.getMessage(index), spec, 'running', 70))

    if (spec.terminalChunks) this.#publishTerminalTool(index, spec)
    else if (this.#stepPublishes === 4) this.#kernel.replaceCanonicalMessage(index, updateLiveToolCall(this.#kernel.getMessage(index), spec, 'success', 100))

    this.#recordPublish()
    if (this.#stepPublishes < AGENT_TOOL_RESULT_PUBLISH) return

    const completedCall = this.#kernel.getMessage(index)
    this.#kernel.replaceCanonicalMessage(index, { ...completedCall, live: false })
    const records = []
    if (!spec.terminalChunks) records.push(createLiveToolResult(completedCall, spec))
    else if (this.#toolResultIndex !== null) {
      const result = this.#kernel.getMessage(this.#toolResultIndex)
      this.#kernel.replaceCanonicalMessage(this.#toolResultIndex, settleLiveTerminal(result, spec))
    }
    records.push(createLiveAssistantStep(completedCall.turnId, stepOrdinal + 1))
    const indexes = this.#kernel.appendCanonicalMessages(records)
    const nextAssistant = indexes[indexes.length - 1]
    if (nextAssistant === undefined) throw new Error('agent-loop assistant step missing')
    if (stepOrdinal + 1 === 3) this.#delegationMessageIndex = nextAssistant
    this.#updatePlan(stepOrdinal + 1)
    this.#kernel.continueExecutionAt(nextAssistant)
    this.#agentPhase = 'model'
    this.#stepPublishes = 0
    this.#markdownTicks = 0
    this.#toolResultIndex = null
  }

  #publishTerminalTool(callIndex: number, spec: NonNullable<ReturnType<typeof liveToolForStep>>): void {
    if (this.#stepPublishes === 1 && this.#toolResultIndex === null) {
      const [resultIndex] = this.#kernel.appendCanonicalMessages([createLiveToolResult(this.#kernel.getMessage(callIndex), spec, true)])
      if (resultIndex === undefined) throw new Error('terminal result record missing')
      this.#toolResultIndex = resultIndex
    }
    if (this.#stepPublishes === 3) this.#updateDelegations(1)
    if (this.#stepPublishes === 6) this.#updateDelegations(2)
    if (this.#toolResultIndex === null) return
    const chunkIndex = this.#stepPublishes - 2
    if (chunkIndex >= 0) {
      const currentResult = this.#kernel.getMessage(this.#toolResultIndex)
      const patched = appendLiveTerminal(currentResult, spec, chunkIndex)
      if (patched) this.#kernel.replaceCanonicalMessage(this.#toolResultIndex, patched.message, { kind: 'append-terminal', blockId: patched.blockId, delta: patched.delta })
    }
    if (this.#stepPublishes === AGENT_TOOL_RESULT_PUBLISH - 1) {
      this.#kernel.replaceCanonicalMessage(callIndex, updateLiveToolCall(this.#kernel.getMessage(callIndex), spec, 'success', 100))
    }
  }

  #updatePlan(activeStep: number): void {
    if (this.#planMessageIndex === null) return
    const current = this.#kernel.getMessage(this.#planMessageIndex)
    const next = updateLivePlan(current, activeStep)
    if (next !== current) this.#kernel.replaceCanonicalMessage(this.#planMessageIndex, next)
  }

  #updateDelegations(completedBackground: number): void {
    if (this.#delegationMessageIndex === null) return
    const current = this.#kernel.getMessage(this.#delegationMessageIndex)
    const next = updateLiveDelegations(current, completedBackground)
    if (next !== current) this.#kernel.replaceCanonicalMessage(this.#delegationMessageIndex, next)
  }

  #recordPublish(): void {
    this.#stepPublishes += 1
    this.#runPublishes += 1
    this.publishTicks += 1
  }

  #appendReasoning(index: number, delta: string): void {
    const current = this.#kernel.getMessage(index)
    const reasoningTokens = estimateTokens(reasoningText(current)) + estimateTokens(delta)
    const patched = appendReasoningContent(current, delta, this.#stepPublishes * Math.max(16, Math.round(1000 / this.rate)), reasoningTokens)
    if (!patched) return
    this.#kernel.replaceCanonicalMessage(index, patched.message, { kind: 'append-reasoning', blockId: patched.blockId, delta })
    this.#accountOutput(delta, true)
  }

  #appendMarkdown(index: number, delta: string): void {
    const patched = appendMarkdownContent(this.#kernel.getMessage(index), delta)
    this.#kernel.replaceCanonicalMessage(index, patched.message, { kind: 'append-markdown', blockId: patched.blockId, delta })
    this.#accountOutput(delta, false)
  }

  #settleCurrentReasoning(index: number): void {
    const current = this.#kernel.getMessage(index)
    const settled = settleReasoningBlock(current)
    if (settled) this.#kernel.replaceCanonicalMessage(index, settled)
  }

  #currentStepOrdinal(): number {
    const index = this.#kernel.currentAssistantIndex
    return index === null ? 0 : parseStepOrdinal(this.#kernel.getMessage(index))
  }

  #completeCurrentTurn(): void {
    this.#stopTimer()
    this.#updatePlan(5)
    this.#updateDelegations(2)
    const index = this.#kernel.currentAssistantIndex
    if (index !== null) {
      const current = this.#kernel.getMessage(index)
      if (current.blocks.some(entry => entry.type === 'markdown')) {
        const settled = settleReasoning(current, 'complete')
        const answer = markdownText(settled).trim()
        const next = answer ? { ...settled, live: false } : replaceMarkdownContent(settled, 'Completed.', false)
        this.#kernel.replaceCanonicalMessage(index, next)
      } else this.#kernel.replaceCanonicalMessage(index, { ...current, live: false })
    }
    this.#kernel.finishExecution('completed')
    const queued = this.#kernel.dequeue()
    if (queued !== null && this.#beginSyntheticTurn(queued)) {
      this.resetTelemetry()
      this.start(true)
    }
  }

  #accountPrompt(text: string): void {
    const usage = this.#kernel.usage
    const context = this.#kernel.context
    const promptTokens = estimateTokens(text)
    const reusable = Math.min(context.projectedTokens, Math.round(context.projectedTokens * 0.82))
    const uncached = Math.max(promptTokens + 64, Math.round(context.projectedTokens * 0.03))
    const cacheWrite = Math.max(0, Math.round(uncached * 0.12))
    this.#kernel.setAccounting({
      ...usage,
      inputTokens: usage.inputTokens + uncached,
      cacheReadTokens: usage.cacheReadTokens + reusable,
      cacheWriteTokens: usage.cacheWriteTokens + cacheWrite,
    }, {
      ...context,
      projectedTokens: Math.min(context.contextWindow, context.projectedTokens + promptTokens + 32),
    })
  }

  #accountOutput(text: string, reasoning: boolean): void {
    const usage = this.#kernel.usage
    const context = this.#kernel.context
    const tokens = estimateTokens(text)
    const next: TokenUsage = reasoning
      ? { ...usage, reasoningTokens: usage.reasoningTokens + tokens }
      : { ...usage, outputTokens: usage.outputTokens + tokens }
    this.#kernel.setAccounting(next, {
      ...context,
      projectedTokens: Math.min(context.contextWindow, context.projectedTokens + tokens),
    })
  }
}

function reasoningText(message: LogicalMessage): string {
  const contentBlock = message.blocks.find(entry => entry.type === 'reasoning')
  return contentBlock?.type === 'reasoning' ? contentBlock.data.text : ''
}

function estimateTokens(text: string): number { return Math.max(1, Math.ceil(text.length / 4)) }

function scheduleFrame(callback: () => void): void {
  if (typeof requestAnimationFrame === 'function') requestAnimationFrame(callback)
  else queueMicrotask(callback)
}
