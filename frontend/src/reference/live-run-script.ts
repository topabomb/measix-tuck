import {
  block,
  type AgentRunRef,
  type AppendCanonicalMessage,
  type ContentBlock,
  type LogicalMessage,
  type ResourceRef,
  type ToolCategory,
  type ToolPresentationIntent,
} from '../engine/model/conversation'
import { appendTerminalOutput, settleTerminal } from '../engine/model/message-mutations'

export const STRESS_REASONING_PUBLISHES = 18
export const AGENT_REASONING_PUBLISHES = 6
export const AGENT_TOOL_CALL_PUBLISH = 12
export const AGENT_TOOL_RESULT_PUBLISH = 8
export const AGENT_FINAL_STEP = 4
export const AGENT_FINAL_DIFF_PUBLISH = 14
export const AGENT_FINAL_CODE_PUBLISH = 20
export const AGENT_FINAL_ARTIFACT_PUBLISH = 26
export const AGENT_MAX_PUBLISHES = 170

export interface LiveToolSpec {
  name: string
  callId: string
  category: ToolCategory
  input: Record<string, unknown>
  output: Record<string, unknown>
  presentation: ToolPresentationIntent
  resources?: readonly ResourceRef[]
  terminalChunks?: readonly string[]
  model?: string
}

const resource = (id: string, uri: string, label = uri, startLine?: number): ResourceRef => ({
  id,
  kind: 'file',
  uri,
  label,
  ...(startLine === undefined ? {} : { range: { startLine } }),
})

const PROJECTION_FILE = resource('projection-engine', 'src/engine/presentation/projection-engine.ts', 'projection-engine.ts', 1)
const KERNEL_FILE = resource('session-kernel', 'src/engine/conversation/session-kernel.ts', 'session-kernel.ts', 1)
const RUNTIME_FILE = resource('session-runtime', 'src/engine/runtime/session-runtime.ts', 'session-runtime.ts', 1)
const REPO_ROOT = resource('repo-root', '/workspace/measix-tuck', '/workspace/measix-tuck')

const TOOL_STEPS: Readonly<Record<number, LiveToolSpec>> = {
  1: {
    name: 'read_file',
    callId: 'loop-read-renderer',
    category: 'filesystem',
    presentation: { kind: 'resources', resources: [PROJECTION_FILE] },
    resources: [PROJECTION_FILE],
    input: { path: PROJECTION_FILE.uri, range: '1:300' },
    output: { lines: 284, finding: 'Markdown, reasoning and terminal streams patch stable RenderUnits without scanning total history' },
  },
  2: {
    name: 'search_code',
    callId: 'loop-search-boundaries',
    category: 'search',
    presentation: { kind: 'resources', resources: [KERNEL_FILE, RUNTIME_FILE] },
    resources: [KERNEL_FILE, RUNTIME_FILE],
    input: { query: 'turnId stepId callId ResourceRef projection incrementalPatches', scope: 'src/engine' },
    output: { matches: 21, files: [KERNEL_FILE.uri, RUNTIME_FILE.uri], finding: 'execution identity stays producer-owned; resources are semantic references rather than host navigation commands' },
  },
  3: {
    name: 'run_tests',
    callId: 'loop-run-tests',
    category: 'shell',
    presentation: { kind: 'terminal', command: 'pnpm test && pnpm build && pnpm test:e2e', cwd: REPO_ROOT },
    resources: [REPO_ROOT],
    input: { command: 'pnpm test && pnpm build && pnpm test:e2e', cwd: REPO_ROOT.uri },
    output: { unit: 'passed', build: 'passed', chromium: 'passed', exitCode: 0 },
    terminalChunks: [
      '$ pnpm test\n\n RUN  v4.1.10 /workspace/measix-tuck\n',
      ' ✓ architecture boundaries\n ✓ projection engine\n ✓ session runtime\n',
      '\n Unit and architecture suites passed\n\n$ pnpm build\n',
      'vue-tsc --noEmit && vite build\n✓ built production bundle\n\n$ pnpm test:e2e\n',
      'Running Chromium workbench + stress scenarios\n······························\n',
      'Chromium suite passed\n',
    ],
  },
}

const PLAN_ITEMS = [
  { id: 'inspect', text: 'Inspect the projection and resource boundaries' },
  { id: 'correlate', text: 'Correlate tool and execution identity without DOM assumptions' },
  { id: 'verify', text: 'Run the full release gate and stream terminal evidence' },
  { id: 'synthesize', text: 'Summarize the smallest rendering-layer change' },
] as const

const DELEGATED_RUNS: readonly AgentRunRef[] = [
  {
    runId: 'review-rendering-contract',
    title: 'Review rendering contract',
    agent: 'architecture-reviewer',
    mode: 'foreground',
    status: 'completed',
    childSessionId: 'child-review-contract',
    summary: 'The parent waited for this focused review before advancing; no layout or orchestration policy leaked into Engine semantics.',
  },
  {
    runId: 'audit-terminal-projection',
    title: 'Audit terminal projection',
    agent: 'performance-reviewer',
    mode: 'background',
    status: 'running',
    childSessionId: 'child-terminal-audit',
  },
  {
    runId: 'audit-resource-semantics',
    title: 'Audit resource semantics',
    agent: 'contract-reviewer',
    mode: 'background',
    status: 'running',
    childSessionId: 'child-resource-audit',
  },
]

const BACKGROUND_SUMMARIES: Readonly<Record<string, string>> = {
  'audit-terminal-projection': 'Confirmed append-only terminal updates replace one stable RenderUnit and keep unrelated siblings reusable.',
  'audit-resource-semantics': 'Confirmed ResourceRef carries identity/range only; editor routing and panel placement remain host-owned.',
}

const STEP_MARKDOWN: Readonly<Record<number, readonly string[]>> = {
  1: [
    '### Step 1 · Inspect the projection path\n\nThe first release clue points at the rendering boundary. I am keeping canonical history independent from viewport state while checking how live content becomes keyed `RenderUnit`s. ',
    '\n\n| Semantic layer | Owns | Must not own |\n| --- | --- | --- |\n',
    '| canonical model | Message / Turn / Step / Block / ResourceRef | DOM, panels, editor actions |\n| projection | stable renderer-ready units | provider policy, workspace layout |\n',
    '| Vue reference adapter | physical measurement | business identity |\n\n',
    '- [x] preserve message identity\n- [x] keep resource identity host-neutral\n- [ ] inspect the exact projection code path\n\n',
    '> A resource can identify `src/engine/...` without telling the Engine whether a product opens VS Code, a browser, or a side panel.\n\n',
  ],
  2: [
    '### Step 2 · Correlate workbench semantics\n\nThe file read confirms the hot path, so I am following tool, resource and delegation references before changing anything. ',
    '\n\n1. `turnId` groups the complete user-level run.\n2. `stepId` identifies actual execution iterations.\n',
    '\n   - Plan items describe intended work, not execution Steps.\n   - Tool call/result correlate by producer-owned `callId`.\n   - `ResourceRef` identifies files/URLs/artifacts without defining navigation.\n   - Delegated child traces stay in child sessions; the parent renders stable run references and statuses.\n\n',
    '```ts\nconst location = { id: "kernel", kind: "file", uri: "src/engine/conversation/session-kernel.ts" }\n',
    'const tool = { category: "filesystem", presentation: { kind: "resources", resources: [location] } }\n```\n\n',
    '| Boundary | Provider policy? | Layout/style? |\n| --- | ---: | ---: |\n| canonical Engine | no | no |\n| Reference execution adapter | yes | no |\n| Vue reference renderer | no | physical visuals only |\n\n',
  ],
  3: [
    '### Step 3 · Verify under load\n\nOne foreground reviewer has completed, while two independent background reviewers continue beside the parent shell verification. Their child conversations remain separate; this parent only needs stable identity, mode, status and concise result. ',
    '\n\n| Verification | Expected | Live state |\n| --- | --- | --- |\n| unit + architecture | deterministic | passing |\n',
    '| strict build | no type drift | passing |\n| Chromium | no row overlap / no page overflow | running |\n\n',
    '```text\nlogical history      -> 1,000,000+\nhot projection       -> bounded window\nterminal delta       -> one stable RenderUnit\nchild delegation     -> stable run refs, not nested traces\n',
    'mounted DOM          -> visible rows only\n```\n\n',
    '> Foreground/background is a producer-reported relationship to parent flow. The Engine renders it; it never schedules the child.\n\n',
  ],
  4: [
    '## Final synthesis\n\nThe task now demonstrates a realistic coding-agent run: plan, filesystem/search activities, one foreground delegated review, two parallel background child runs, streaming terminal verification, code/diff/artifacts, and final synthesis — all through canonical rendering semantics. ',
    '\n\n### Boundary result\n\n- Engine owns reusable semantic primitives and bounded projection.\n- Reference owns scenario timing, fake provider output and workbench composition.\n- Child scheduling, child-session navigation, layout, editor routing, permission policy and Agent orchestration remain outside the Engine.\n\n',
    '| Property | Result |\n| --- | --- |\n| Plan vs execution Step | distinct |\n| ResourceRef vs host action | distinct |\n| tool category vs presentation intent | distinct |\n| terminal streaming | incremental |\n| delegated child batch | sync + async/parallel refs |\n\n',
    '### Release checklist\n\n- [x] canonical tool correlation\n- [x] workbench semantic blocks\n- [x] stable incremental terminal output\n- [x] foreground/background child status semantics\n- [x] resource-aware diff/tool evidence\n- [ ] deployed Chromium verification\n\n',
    '> The Engine stays smaller than the workbench: it knows what can be rendered and correlated, not how a product arranges panels or executes agents.\n\n',
  ],
}

export function liveToolForStep(stepOrdinal: number): LiveToolSpec | null { return TOOL_STEPS[stepOrdinal] ?? null }

export function parseStepOrdinal(message: LogicalMessage): number {
  const match = message.stepId?.match(/:step-(\d+)$/)
  return match ? Number(match[1]) : 0
}

export function agentReasoningDelta(stepOrdinal: number, tick: number): string {
  const phrases = [
    `Step ${stepOrdinal}: preserve canonical identity before touching presentation policy. `,
    'Separate execution semantics from viewport measurement and product layout. ',
    'Inspect only changed hot state; never scan total history for ordinary UI work. ',
    'Keep tool/resource/delegation correlation explicit so remounts cannot change business identity. ',
  ]
  const phrase = phrases[tick % phrases.length]!
  return tick % 3 === 0 ? `\n\n${phrase}` : phrase
}

export function agentMarkdownDelta(stepOrdinal: number, markdownTick: number): string {
  const scripted = STEP_MARKDOWN[stepOrdinal] ?? STEP_MARKDOWN[AGENT_FINAL_STEP]!
  if (markdownTick < scripted.length) return scripted[markdownTick]!
  const cycle = markdownTick - scripted.length
  const variants = [
    `### Ongoing verification ${cycle + 1}\n\nThe stream is still growing after structured blocks above. Only the active Markdown tail is reparsed; settled prefix units keep identity.\n\n`,
    `| live check | value |\n| --- | --- |\n| step | ${stepOrdinal} |\n| sample | ${cycle + 1} |\n| semantic reader | preserved |\n\n`,
    '```ts\nconst next = appendMarkdownDelta(current, delta)\n// projection reuses every settled prefix RenderUnit\n```\n\n',
    '- [x] stream continues\n- [x] tool/resource records stay correlated\n- [x] child runs remain references rather than recursive parent trace\n- [x] physical measurement stays bounded\n\n',
    '> Rich content can change physical height without changing Turn, Step or reader identity.\n\n',
  ]
  return variants[cycle % variants.length]!
}

export function stressReasoningDelta(tick: number): string {
  const phrases = [
    'Correlate the deployed failure with the renderer change before editing code. ',
    'Preserve stable Message, Block and RenderUnit identities while the live turn grows. ',
    'Keep semantic reader state independent from dynamic DOM measurement. ',
    'Prefer bounded hot-state work over total-history scans. ',
  ]
  const phrase = phrases[tick % phrases.length]!
  return tick % 5 === 0 ? `\n\n${phrase}` : phrase
}

export function stressMarkdownDelta(step: number): string {
  const variants = [
    `### Streaming stress sample ${step + 1}\n\nThe million-message session remains a pure projection/viewport stress stream. Workbench orchestration is demonstrated in the dedicated coding-agent session. ${'bounded hot state '.repeat(9)}\n\n`,
    '| check | result |\n| --- | --- |\n| settled prefix | reused |\n| changed tail | patched |\n| total history scan | none |\n\n',
    '```ts\nprojection.appendMarkdownDelta(message, blockId, delta)\n// changed + hot + visible, not total history\n```\n\n',
    '- [x] exact Latest\n- [x] background execution\n- [x] bounded projection cache\n- [x] bounded DOM\n\n',
    '> Stress and product scenarios are separated so each test has one clear responsibility.\n\n',
  ]
  return variants[step % variants.length]!
}

export function settleReasoningBlock(message: LogicalMessage): LogicalMessage | null {
  const index = message.blocks.findIndex(entry => entry.type === 'reasoning')
  if (index < 0) return null
  const current = message.blocks[index]
  if (!current || current.type !== 'reasoning' || current.data.status === 'complete') return null
  const blocks = [...message.blocks]
  blocks[index] = block(current.id, 'reasoning', { ...current.data, status: 'complete' }, (current.revision ?? 0) + 1)
  return { ...message, blocks }
}

export function createLiveToolCall(message: LogicalMessage, spec: LiveToolSpec): AppendCanonicalMessage {
  return {
    turnId: message.turnId,
    stepId: message.stepId,
    role: 'assistant',
    live: true,
    blocks: [block(`live-tool-call-${spec.callId}`, 'tool-call', {
      name: spec.name,
      callId: spec.callId,
      category: spec.category,
      presentation: spec.presentation,
      resources: spec.resources,
      model: spec.model,
      status: 'running',
      progress: 10,
      input: spec.input,
      durationMs: 0,
      defaultOpen: false,
    })],
  }
}

export function updateLiveToolCall(message: LogicalMessage, spec: LiveToolSpec, status: 'running' | 'success', progress: number): LogicalMessage {
  const id = `live-tool-call-${spec.callId}`
  const blocks = [...message.blocks]
  const index = blocks.findIndex(entry => entry.id === id && entry.type === 'tool-call')
  if (index < 0) throw new Error(`tool call ${spec.callId} missing from active assistant record`)
  blocks[index] = block(id, 'tool-call', {
    name: spec.name,
    callId: spec.callId,
    category: spec.category,
    presentation: spec.presentation,
    resources: spec.resources,
    model: spec.model,
    status,
    progress,
    input: spec.input,
    durationMs: status === 'success' ? 480 : Math.round(progress * 4.8),
    defaultOpen: false,
  }, (blocks[index]?.revision ?? 0) + 1)
  return { ...message, blocks }
}

export function createLiveToolResult(message: LogicalMessage, spec: LiveToolSpec, runningTerminal = false): AppendCanonicalMessage {
  const blocks: ContentBlock[] = [block(`live-tool-result-${spec.callId}`, 'tool-result', {
    name: spec.name,
    callId: spec.callId,
    category: spec.category,
    presentation: spec.presentation,
    resources: spec.resources,
    model: spec.model,
    status: runningTerminal ? 'running' : 'success',
    progress: runningTerminal ? 10 : 100,
    output: runningTerminal ? { state: 'streaming' } : spec.output,
    durationMs: runningTerminal ? 0 : 480,
    defaultOpen: false,
  })]
  if (runningTerminal) blocks.push(block(`terminal-${spec.callId}`, 'terminal', {
    callId: spec.callId,
    command: spec.presentation.kind === 'terminal' ? spec.presentation.command : spec.name,
    cwd: spec.presentation.kind === 'terminal' ? spec.presentation.cwd : undefined,
    output: '',
    status: 'running',
    durationMs: 0,
    defaultOpen: true,
  }))
  return { turnId: message.turnId, stepId: message.stepId, role: 'tool', live: runningTerminal, blocks }
}

export function appendLiveTerminal(message: LogicalMessage, spec: LiveToolSpec, chunkIndex: number): { message: LogicalMessage; blockId: string; delta: string } | null {
  const delta = spec.terminalChunks?.[chunkIndex]
  if (!delta) return null
  const blockId = `terminal-${spec.callId}`
  const patched = appendTerminalOutput(message, blockId, delta, (chunkIndex + 1) * 220)
  return patched ? { ...patched, delta } : null
}

export function settleLiveTerminal(message: LogicalMessage, spec: LiveToolSpec): LogicalMessage {
  const exitCode = Number(spec.output.exitCode ?? 0)
  const terminalStatus = exitCode === 130 ? 'interrupted' : exitCode === 0 ? 'success' : 'error'
  const terminalId = `terminal-${spec.callId}`
  let next = settleTerminal(message, terminalId, terminalStatus, exitCode, 1_320)
  const resultId = `live-tool-result-${spec.callId}`
  const blocks = [...next.blocks]
  const resultIndex = blocks.findIndex(entry => entry.id === resultId && entry.type === 'tool-result')
  if (resultIndex >= 0) {
    blocks[resultIndex] = block(resultId, 'tool-result', {
      name: spec.name,
      callId: spec.callId,
      category: spec.category,
      presentation: spec.presentation,
      resources: spec.resources,
      model: spec.model,
      status: terminalStatus === 'success' ? 'success' : 'error',
      progress: 100,
      output: spec.output,
      durationMs: 1_320,
      defaultOpen: false,
    }, (blocks[resultIndex]?.revision ?? 0) + 1)
    next = { ...next, blocks }
  }
  return { ...next, live: false }
}

export function createLiveAssistantStep(turnId: string, stepOrdinal: number): AppendCanonicalMessage {
  const extra: ContentBlock[] = []
  if (stepOrdinal === 1) extra.push(block('work-plan', 'plan', { title: 'Rendering-engine hardening plan', items: planItemsForStep(1) }))
  if (stepOrdinal === 3) extra.push(block('delegated-review', 'delegation', { title: 'Delegated verification', runs: DELEGATED_RUNS }))
  return {
    turnId,
    stepId: `${turnId}:step-${stepOrdinal}`,
    role: 'assistant',
    live: true,
    blocks: [
      ...extra,
      block('reasoning', 'reasoning', { text: '', tokenCount: 0, durationMs: 0, defaultOpen: false, status: 'streaming' }),
      block('answer', 'markdown', { markdown: '' }),
    ],
  }
}

export function updateLivePlan(message: LogicalMessage, activeStep: number): LogicalMessage {
  const blocks = [...message.blocks]
  const index = blocks.findIndex(entry => entry.id === 'work-plan' && entry.type === 'plan')
  if (index < 0) return message
  blocks[index] = block('work-plan', 'plan', { title: 'Rendering-engine hardening plan', items: planItemsForStep(activeStep) }, (blocks[index]?.revision ?? 0) + 1)
  return { ...message, blocks }
}

export function updateLiveDelegations(message: LogicalMessage, completedBackground: number): LogicalMessage {
  const blocks = [...message.blocks]
  const index = blocks.findIndex(entry => entry.id === 'delegated-review' && entry.type === 'delegation')
  if (index < 0) return message
  const current = blocks[index]
  if (!current || current.type !== 'delegation') return message
  let seenBackground = 0
  const runs = current.data.runs.map(run => {
    if (run.mode !== 'background') return run
    seenBackground += 1
    if (seenBackground > completedBackground) return run
    return { ...run, status: 'completed' as const, summary: BACKGROUND_SUMMARIES[run.runId] }
  })
  blocks[index] = block(current.id, 'delegation', { ...current.data, runs }, (current.revision ?? 0) + 1)
  return { ...message, blocks, revision: (message.revision ?? 0) + 1 }
}

function planItemsForStep(activeStep: number) {
  return PLAN_ITEMS.map((item, index) => {
    const step = index + 1
    const status = activeStep > 4 ? 'completed' : step < activeStep ? 'completed' : step === activeStep ? 'in-progress' : 'pending'
    return { ...item, status: status as 'pending' | 'in-progress' | 'completed' }
  })
}

export function addFinalEvidence(message: LogicalMessage, kind: 'diff' | 'code' | 'artifacts'): LogicalMessage {
  if (kind === 'diff') return addBlockBeforeAnswer(message, block('live-final-diff', 'diff', {
    resource: KERNEL_FILE,
    lines: [
      ' for (const entry of entries) {',
      '+  preserveStableTurnAndStepCoordinates(entry)',
      '+  publishSemanticContentWithoutLayoutMetadata(entry)',
      ' }',
      '+projection.appendTerminalDelta(message, blockId, delta)',
    ],
    defaultOpen: true,
  }))
  if (kind === 'code') return addBlockBeforeAnswer(message, block('live-final-code', 'code', {
    language: 'typescript',
    filename: 'tests/workbench-rendering.contract.ts',
    resource: resource('workbench-contract-test', 'tests/workbench-rendering.contract.ts', 'workbench-rendering.contract.ts'),
    defaultOpen: true,
    code: [
      'expect(planItems).toSeparateIntentFromExecutionSteps()',
      'expect(toolPresentation).not.toDefineLayout()',
      'expect(resourceRefs).toBeHostNeutral()',
      'expect(terminalAppend).toPatchOneStableRenderUnit()',
      'expect(delegation.runs).toKeepChildTracesOutOfParentHistory()',
    ].join('\n'),
  }))
  return addBlockBeforeAnswer(message, block('live-final-artifacts', 'attachments', {
    title: 'Workbench verification artifacts',
    provenance: { origin: 'tool-output', toolCallId: 'loop-run-tests', toolName: 'run_tests' },
    items: [
      { id: 'loop-desktop-proof', name: 'coding-workbench-desktop.png', kind: 'image', mimeType: 'image/png', width: 1440, height: 900, sizeBytes: 642_000, seed: 8801, resource: { id: 'desktop-proof', kind: 'artifact', uri: 'artifact://coding-workbench-desktop', label: 'coding-workbench-desktop.png' } },
      { id: 'loop-mobile-proof', name: 'coding-workbench-mobile.png', kind: 'image', mimeType: 'image/png', width: 780, height: 1380, sizeBytes: 514_000, seed: 8802, resource: { id: 'mobile-proof', kind: 'artifact', uri: 'artifact://coding-workbench-mobile', label: 'coding-workbench-mobile.png' } },
    ],
  }))
}

function addBlockBeforeAnswer(message: LogicalMessage, contentBlock: ContentBlock): LogicalMessage {
  if (message.blocks.some(entry => entry.id === contentBlock.id)) return message
  const blocks = [...message.blocks]
  const answerIndex = blocks.findIndex(entry => entry.id === 'answer' && entry.type === 'markdown')
  if (answerIndex < 0) blocks.push(contentBlock)
  else blocks.splice(answerIndex, 0, contentBlock)
  return { ...message, blocks }
}
