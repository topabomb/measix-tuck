import { block, type ContentBlock, type LogicalMessage, type LogicalRole } from '../engine/model/conversation'
import { hash32, intBetween } from './prng'

type SyntheticKind = 'markdown' | 'text' | 'reasoning' | 'tool' | 'code' | 'diff' | 'image' | 'html'
type ToolPhase = 'call' | 'result'

interface PatternEntry {
  role: LogicalRole
  kind: SyntheticKind
  turn: number
  step: number
  toolPhase?: ToolPhase
  toolName?: string
}

const TURNS_PER_CYCLE = 4
const TURN_PATTERN: readonly PatternEntry[] = [
  { role: 'user', kind: 'markdown', turn: 0, step: 0 },
  { role: 'assistant', kind: 'reasoning', turn: 0, step: 0 },
  { role: 'assistant', kind: 'tool', turn: 0, step: 0, toolPhase: 'call', toolName: 'read_file' },
  { role: 'tool', kind: 'tool', turn: 0, step: 0, toolPhase: 'result', toolName: 'read_file' },
  { role: 'assistant', kind: 'reasoning', turn: 0, step: 1 },
  { role: 'assistant', kind: 'tool', turn: 0, step: 1, toolPhase: 'call', toolName: 'search' },
  { role: 'tool', kind: 'tool', turn: 0, step: 1, toolPhase: 'result', toolName: 'search' },
  { role: 'assistant', kind: 'markdown', turn: 0, step: 2 },
  { role: 'user', kind: 'text', turn: 1, step: 0 },
  { role: 'assistant', kind: 'reasoning', turn: 1, step: 0 },
  { role: 'assistant', kind: 'code', turn: 1, step: 0 },
  { role: 'assistant', kind: 'markdown', turn: 1, step: 1 },
  { role: 'user', kind: 'markdown', turn: 2, step: 0 },
  { role: 'assistant', kind: 'reasoning', turn: 2, step: 0 },
  { role: 'assistant', kind: 'tool', turn: 2, step: 0, toolPhase: 'call', toolName: 'edit_file' },
  { role: 'tool', kind: 'tool', turn: 2, step: 0, toolPhase: 'result', toolName: 'edit_file' },
  { role: 'assistant', kind: 'tool', turn: 2, step: 1, toolPhase: 'call', toolName: 'test' },
  { role: 'tool', kind: 'tool', turn: 2, step: 1, toolPhase: 'result', toolName: 'test' },
  { role: 'assistant', kind: 'diff', turn: 2, step: 2 },
  { role: 'assistant', kind: 'markdown', turn: 2, step: 2 },
  { role: 'user', kind: 'text', turn: 3, step: 0 },
  { role: 'assistant', kind: 'image', turn: 3, step: 0 },
  { role: 'assistant', kind: 'html', turn: 3, step: 0 },
  { role: 'assistant', kind: 'markdown', turn: 3, step: 1 },
]

/** Reference-only deterministic source used to prove large-history behavior. */
export class SyntheticConversationSource {
  readonly count: number
  #scope: string
  #seedOffset: number
  #liveTail: boolean

  constructor(count: number, scope = '', seedOffset = 0, liveTail = false) {
    this.count = count
    this.#scope = scope
    this.#seedOffset = seedOffset
    this.#liveTail = liveTail
  }

  getMessage(index: number): LogicalMessage {
    if (!Number.isInteger(index) || index < 0 || index >= this.count) {
      throw new RangeError(`message index ${index} outside 0..${this.count - 1}`)
    }

    const seed = hash32(index + 0x51f15e + this.#seedOffset)
    const intensity = intBetween(seed + 31, 1, 10)
    const patternIndex = index % TURN_PATTERN.length
    const cycle = Math.floor(index / TURN_PATTERN.length)
    const pattern = TURN_PATTERN[patternIndex]!
    const messageId = this.#scope ? `${this.#scope}:m-${index}` : `m-${index}`
    const turnNumber = cycle * TURNS_PER_CYCLE + pattern.turn
    const turnId = this.#scope ? `${this.#scope}:turn-${turnNumber}` : `turn-${turnNumber}`
    const stepId = `${turnId}:step-${pattern.step}`

    if (this.#liveTail && index === this.count - 1) {
      return {
        id: messageId,
        index,
        turnId,
        stepId,
        role: 'assistant',
        blocks: [block('answer', 'markdown', { markdown: '### Working on it…\n\nThe latest assistant response is streaming. New model deltas are coalesced before UI publication.' })],
        live: true,
      }
    }

    return {
      id: messageId,
      index,
      turnId,
      stepId,
      role: pattern.role,
      blocks: blocksFor(index, seed, intensity, pattern),
    }
  }

  getRange(start: number, count: number): LogicalMessage[] {
    const safeStart = Math.max(0, Math.min(start, this.count))
    const safeEnd = Math.max(safeStart, Math.min(this.count, safeStart + Math.max(0, count)))
    const result = new Array<LogicalMessage>(safeEnd - safeStart)
    for (let index = safeStart; index < safeEnd; index += 1) result[index - safeStart] = this.getMessage(index)
    return result
  }
}

function blocksFor(index: number, seed: number, intensity: number, pattern: PatternEntry): ContentBlock[] {
  if (pattern.kind === 'markdown') {
    const sectionCount = pattern.role === 'user' ? intBetween(seed + 2, 1, 2) : intBetween(seed + 2, 1, Math.min(6, 2 + Math.ceil(intensity / 2)))
    return Array.from({ length: sectionCount }, (_, section) => {
      const paragraphs = intBetween(seed + section * 19, 1, pattern.role === 'user' ? 3 : 6)
      const title = pattern.role === 'user' ? `Request ${index.toLocaleString()}` : ['Implementation', 'Investigation', 'Result', 'Trade-offs', 'Verification', 'Next steps'][section % 6]
      const markdown = [`### ${title}`, ...Array.from({ length: paragraphs }, (__, paragraph) => sentence(seed + paragraph * 23 + section * 7, 22 + intensity * 5))].join('\n\n')
      return block(`md-${section}`, 'markdown', { markdown })
    })
  }

  if (pattern.kind === 'text') {
    const lines = intBetween(seed + 1, 1, 8 + intensity)
    return [block('text', 'text', { text: sentence(seed, lines * 9) })]
  }

  if (pattern.kind === 'reasoning') {
    const paragraphs = intBetween(seed + 41, 2, 6 + intensity)
    const text = Array.from({ length: paragraphs }, (_, paragraph) => {
      const prefix = paragraph === 0 ? 'I need to inspect the current state before changing anything.' : 'Then I should validate the next dependency and preserve the existing invariant.'
      return `${prefix} ${sentence(seed + paragraph * 31, 24 + intensity * 4)}`
    }).join('\n\n')
    return [block('reasoning', 'reasoning', { text, tokenCount: Math.round(text.length / 3.8), durationMs: intBetween(seed + 43, 900, 28_000), defaultOpen: false, status: 'complete' })]
  }

  if (pattern.kind === 'code') {
    const lines = intBetween(seed + 3, 18, 70 + intensity * 14)
    const code = Array.from({ length: lines }, (_, line) => {
      if (line % 11 === 0) return `// render unit ${line}: keep hot state independent from total history size`
      if (line % 7 === 0) return `await adapter.applyDelta({ id: 'm-${index}-${line}', revision: ${line} })`
      return `const value_${line} = transform(input[${line}], { cache: ${line % 2 === 0}, priority: ${line % 7} })`
    }).join('\n')
    return [block('code', 'code', { code, language: 'typescript', filename: `src/agent/turn-${index % 97}.ts` })]
  }

  if (pattern.kind === 'image') {
    const width = intBetween(seed + 4, 720, 1600)
    const height = intBetween(seed + 5, 320, 1100)
    return [block('image', 'image', { width, height, seed, alt: `Generated artifact preview ${index}` })]
  }

  if (pattern.kind === 'html') {
    const cards = intBetween(seed + 6, 2, 7)
    const html = `<section class="synthetic-html"><h3>Generated interactive artifact</h3><p>This HTML is intentionally passed through the renderer boundary and sanitized before mounting.</p>${Array.from({ length: cards }, (_, card) => `<div class="html-chip"><strong>Node ${card + 1}</strong><span>${sentence(seed + card, 12)}</span></div>`).join('')}<script>window.__unsafeSyntheticPayload = true</script></section>`
    return [block('html', 'html', { html })]
  }

  if (pattern.kind === 'tool') {
    const name = pattern.toolName ?? `tool_${seed % 17}`
    const phase = pattern.toolPhase ?? 'call'
    const callId = `call_${Math.floor(index / TURN_PATTERN.length).toString(36)}_${pattern.turn}_${pattern.step}_${name}`
    const rows = intBetween(seed + 7, 3, 8 + intensity)
    const path = `/workspace/src/${name}-${index % 31}.ts`
    const location = { id: `synthetic-resource-${index}`, kind: 'file' as const, uri: path, label: path }
    const input = { path, query: sentence(seed, 7), limit: intBetween(seed + 5, 10, 200), recursive: seed % 2 === 0 }
    const outputRows = Array.from({ length: rows }, (_, row) => ({ line: intBetween(seed + row * 17, 1, 4000), score: Number((((seed + row * 19) % 1000) / 1000).toFixed(3)), preview: sentence(seed + row * 29, 10 + (row % 8)) }))
    const status = phase === 'result' && seed % 17 === 0 ? 'error' : phase === 'result' ? 'success' : 'running'
    return phase === 'result'
      ? [block('tool-result', 'tool-result', { name, callId, category: name === 'test' ? 'shell' : 'filesystem', presentation: { kind: 'resources', resources: [location] }, resources: [location], durationMs: intBetween(seed, 5, 9000), status, output: { rows: outputRows, truncated: rows > 10, exitCode: status === 'error' ? 1 : 0 }, defaultOpen: false })]
      : [block('tool-call', 'tool-call', { name, callId, category: name === 'test' ? 'shell' : 'filesystem', presentation: { kind: 'resources', resources: [location] }, resources: [location], durationMs: intBetween(seed, 5, 9000), status, input, defaultOpen: false })]
  }

  if (pattern.kind === 'diff') {
    const lineCount = intBetween(seed + 8, 35, 100 + intensity * 24)
    const lines = Array.from({ length: lineCount }, (__, line) => `${line % 3 === 0 ? '+' : line % 5 === 0 ? '-' : ' '} ${String(line + 1).padStart(4, ' ')}  ${sentence(seed + line, 8 + (line % 11))}`)
    const uri = `src/generated-${index % 29}.ts`
    return [block('diff', 'diff', { resource: { id: `synthetic-diff-${index}`, kind: 'file', uri, label: uri }, lines })]
  }

  return [block('unknown', 'text', { text: '[unsupported synthetic content]' })]
}

const words = ['agent', 'context', 'runtime', 'stream', 'virtual', 'render', 'tool', 'model', 'workspace', 'cache', 'delta', 'anchor', 'message', 'token', 'layout', 'history', 'protocol', 'projection', 'session', 'gateway', 'artifact', 'reasoning', 'commit']
function sentence(seed: number, count: number): string {
  const out: string[] = []
  for (let index = 0; index < count; index += 1) out.push(words[(seed + index * 13) % words.length]!)
  return `${out.join(' ')}.`
}
