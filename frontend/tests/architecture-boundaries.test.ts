import { describe, expect, it } from 'vitest'
import { readFileSync, readdirSync, statSync } from 'node:fs'
import { dirname, join, relative, resolve, sep } from 'node:path'

const srcRoot = resolve(process.cwd(), 'src')
const engineRoot = join(srcRoot, 'engine')
const referenceRoot = join(srcRoot, 'reference')
const engineLayers = ['core', 'model', 'conversation', 'presentation', 'viewport', 'runtime', 'vue', 'workers'] as const
const forbidden: Record<(typeof engineLayers)[number], ReadonlySet<string>> = {
  core: new Set(['model', 'conversation', 'presentation', 'viewport', 'runtime', 'vue', 'workers']),
  model: new Set(['conversation', 'presentation', 'viewport', 'runtime', 'vue', 'workers']),
  conversation: new Set(['presentation', 'viewport', 'runtime', 'vue', 'workers']),
  presentation: new Set(['conversation', 'viewport', 'runtime', 'vue', 'workers']),
  viewport: new Set(['conversation', 'presentation', 'runtime', 'vue', 'workers']),
  runtime: new Set(['vue', 'workers']),
  vue: new Set(['workers']),
  workers: new Set(['model', 'conversation', 'presentation', 'viewport', 'runtime', 'vue']),
}

function sourceFiles(dir: string): string[] {
  return readdirSync(dir).flatMap(name => {
    const file = join(dir, name)
    if (statSync(file).isDirectory()) return sourceFiles(file)
    return /\.(?:ts|vue)$/.test(name) ? [file] : []
  })
}

function importsOf(file: string): string[] {
  const source = readFileSync(file, 'utf8')
  const result: string[] = []
  const pattern = /(?:from\s*|import\s*)['"]([^'"]+)['"]/g
  for (const match of source.matchAll(pattern)) if (match[1]?.startsWith('.')) result.push(match[1])
  return result
}

function targetLayer(file: string, specifier: string): string | null {
  const target = resolve(dirname(file), specifier)
  const rel = relative(engineRoot, target)
  if (rel.startsWith('..')) return null
  return rel.split(sep)[0] ?? null
}

describe('engine architecture boundaries', () => {
  it('physically separates reusable Engine and executable Reference ownership', () => {
    expect(statSync(engineRoot).isDirectory()).toBe(true)
    expect(statSync(referenceRoot).isDirectory()).toBe(true)
    const legacyRoots = ['core', 'model', 'conversation', 'presentation', 'viewport', 'runtime', 'vue', 'components', 'styles', 'workers']
    for (const name of legacyRoots) expect(() => statSync(join(srcRoot, name))).toThrow()
  })

  it('keeps every Engine relative import inside Engine and preserves dependency direction', () => {
    const violations: string[] = []
    for (const layer of engineLayers) {
      const root = join(engineRoot, layer)
      if (!statSync(root).isDirectory()) continue
      for (const file of sourceFiles(root)) {
        for (const specifier of importsOf(file)) {
          const target = resolve(dirname(file), specifier)
          if (relative(engineRoot, target).startsWith('..')) {
            violations.push(`${relative(srcRoot, file)} escapes engine -> ${specifier}`)
            continue
          }
          const targetName = targetLayer(file, specifier)
          if (targetName && forbidden[layer].has(targetName)) violations.push(`${relative(srcRoot, file)} -> ${specifier}`)
        }
      }
    }
    expect(violations, violations.join('\n')).toEqual([])
  })

  it('allows Reference to consume Engine while Engine never consumes Reference', () => {
    const engineSource = sourceFiles(engineRoot).map(file => readFileSync(file, 'utf8')).join('\n')
    expect(engineSource).not.toMatch(/(?:^|['"])\.\.\/(?:\.\.\/)*reference\//m)
    const referenceSource = sourceFiles(referenceRoot).map(file => readFileSync(file, 'utf8')).join('\n')
    expect(referenceSource).toMatch(/\.\.\/(?:engine|\.\.\/engine)\//)
  })

  it('keeps the framework-neutral public entry small and free of adapter tuning details', () => {
    const source = readFileSync(join(engineRoot, 'index.ts'), 'utf8')
    expect(source).not.toMatch(/(?:reference|\.\/vue\/)/)
    expect(source).toContain('ConversationHistorySource')
    expect(source).toContain('InteractionResolution')
    expect(source).toContain('ResourceRef')
    expect(source).toContain('PlanItem')
    expect(source).toContain('ToolPresentationIntent')
    expect(source).toContain('AgentRunRef')
    expect(source).toContain('AgentRunMode')
    expect(source).not.toMatch(/ConversationBackend|ConversationHistoryAdapter/)
    expect(source).not.toMatch(/SessionUiSnapshot|ShiftPlan|WINDOW_MESSAGES|SHIFT_MESSAGES/)
    expect(source).not.toContain('PresentationSurface')
  })

  it('keeps workbench rendering semantics layout-, style- and orchestration-agnostic', () => {
    const model = readFileSync(join(engineRoot, 'model/conversation.ts'), 'utf8')
    const renderUnit = readFileSync(join(engineRoot, 'presentation/render-unit.ts'), 'utf8')
    const projector = readFileSync(join(engineRoot, 'presentation/projector-registry.ts'), 'utf8')
    const combined = `${model}\n${renderUnit}`

    expect(model).toContain("export type ResourceKind = 'file' | 'url' | 'artifact'")
    expect(model).toContain('kind: ResourceKind')
    expect(model).toContain("export type AgentRunMode = 'foreground' | 'background'")
    expect(model).toContain("export type AgentRunStatus = 'queued' | 'running' | 'waiting' | 'completed' | 'failed' | 'interrupted'")
    expect(model).toContain("delegation: { title?: string; runs: readonly AgentRunRef[] }")
    expect(model).toContain("| { kind: 'terminal'; command?: string; cwd?: ResourceRef }")
    expect(model).toContain("plan: { title?: string; items: readonly PlanItem[] }")
    expect(model).toContain("terminal: { callId?: string")
    expect(model).toContain('diff: { resource: ResourceRef;')
    expect(model).not.toMatch(/diff:\s*\{[^}]*file:/s)
    expect(model).not.toMatch(/run_in_background|maxParallel|worktree|permissionMode|spawnChild|scheduleAgent/)
    expect(combined).not.toMatch(/PresentationSurface|panelPlacement|panelSide|sidebar|drawer|cssClass|className|color:/)
    expect(projector).toContain(".register('plan'")
    expect(projector).toContain(".register('terminal'")
    expect(projector).toContain(".register('delegation'")
    expect(projector).not.toContain(".register('agent-run'")
  })

  it('keeps child traces separate while exposing stable parent-facing run references', () => {
    const model = readFileSync(join(engineRoot, 'model/conversation.ts'), 'utf8')
    const projector = readFileSync(join(engineRoot, 'presentation/projector-registry.ts'), 'utf8')
    expect(model).toContain('childSessionId?: string')
    expect(model).toContain('Child trace remains in the child session')
    expect(model).not.toMatch(/messages:\s*readonly LogicalMessage\[\]|childMessages|childHistory/)
    expect(projector).toContain("'delegation', 'delegation'")
  })

  it('keeps terminal streaming incremental without turning Engine into a process manager', () => {
    const mutations = readFileSync(join(engineRoot, 'model/message-mutations.ts'), 'utf8')
    const kernel = readFileSync(join(engineRoot, 'conversation/session-kernel.ts'), 'utf8')
    const projection = readFileSync(join(engineRoot, 'presentation/projection-engine.ts'), 'utf8')
    const runtime = readFileSync(join(engineRoot, 'runtime/session-runtime.ts'), 'utf8')

    expect(mutations).toContain('appendTerminalOutput')
    expect(mutations).toContain('settleTerminal')
    expect(kernel).toContain("kind: 'append-terminal'")
    expect(projection).toContain('appendTerminalDelta')
    expect(runtime).toContain("event.contentPatch?.kind === 'append-terminal'")
    expect(`${kernel}\n${projection}\n${runtime}`).not.toMatch(/spawn\(|child_process|exec\(|pty|processManager|killProcess/)
  })

  it('keeps the synchronous history contract honest about async IO ownership', () => {
    const contracts = readFileSync(join(engineRoot, 'conversation/contracts.ts'), 'utf8')
    const adapter = readFileSync(join(referenceRoot, 'history-adapter.ts'), 'utf8')
    expect(contracts).toContain('export interface ConversationHistorySource')
    expect(contracts).toContain('async fetching and caching outside this')
    expect(contracts).not.toContain('interface ConversationBackend')
    expect(adapter).toContain('implements ConversationHistorySource')
    expect(adapter).toContain('async DB/network fetching stays')
  })

  it('owns blocker identity and session invariants without owning post-resolution workflow policy', () => {
    const contracts = readFileSync(join(engineRoot, 'conversation/contracts.ts'), 'utf8')
    const kernel = readFileSync(join(engineRoot, 'conversation/session-kernel.ts'), 'utf8')
    const runtime = readFileSync(join(engineRoot, 'runtime/session-runtime.ts'), 'utf8')
    const referenceStream = readFileSync(join(referenceRoot, 'stream-controller.ts'), 'utf8')
    const workspace = readFileSync(join(referenceRoot, 'workspace-runtime.ts'), 'utf8')
    const turnReasonDeclaration = contracts.slice(contracts.indexOf('export type TurnEndReasonKind'), contracts.indexOf('export interface LlmFailure'))

    expect(contracts).toContain("{ interactionId: string; kind: 'question'; answer: string | null }")
    expect(contracts).toContain("{ interactionId: string; kind: 'approval'; approved: boolean }")
    expect(contracts).toContain('callId?: string')
    expect(contracts).toContain('activeAssistantIndex?: number | null')
    expect(turnReasonDeclaration).not.toContain("'blocked'")
    expect(kernel).toContain('descriptor.activeAssistantIndex')
    expect(kernel).not.toMatch(/(?:history|backend)\.count\s*-\s*1/)
    expect(kernel).toContain('requestInteraction(interaction: PendingInteraction)')
    expect(kernel).toContain('resolveInteraction(resolution: InteractionResolution)')
    expect(kernel).toContain('pending.id !== resolution.interactionId')
    expect(kernel).toContain('cannot finish execution while interaction')
    expect(kernel).toContain("this.#pendingInteraction || this.#status === 'working'")
    expect(kernel).not.toContain('resolution.approved')
    expect(kernel).not.toContain('resolution.answer === null')
    expect(kernel).not.toContain('defaultTurnReason')
    expect(kernel).not.toContain('agent-loop')
    expect(kernel).not.toContain('estimateTokens')
    expect(runtime).not.toMatch(/streamTarget|liveChunkCount|mountedRows|jumpInput/)
    expect(referenceStream).toContain('resolveInteraction(resolution: InteractionResolution)')
    expect(workspace).toContain('activeAssistantIndex: descriptor.logicalCount - 1')
  })

  it('keeps tool execution facts producer-owned instead of inventing defaults in projection or Vue', () => {
    const projector = readFileSync(join(engineRoot, 'presentation/projector-registry.ts'), 'utf8')
    const toolRenderer = readFileSync(join(engineRoot, 'vue/renderers/ToolBlock.vue'), 'utf8')
    const office = readFileSync(join(referenceRoot, 'office-scenarios.ts'), 'utf8')
    const scenarios = readFileSync(join(referenceRoot, 'session-scenarios.ts'), 'utf8')
    const fixtures = readFileSync(join(referenceRoot, 'workspace-fixtures.ts'), 'utf8')

    expect(projector).not.toMatch(/status:\s*data\.status\s*\?\?\s*['"](?:running|success)['"]/)
    expect(toolRenderer).not.toMatch(/payload\.status\s*\?\?\s*['"]running['"]/)
    expect(toolRenderer).toContain('v-if="status"')
    expect(office).toContain("callId: 'meeting-followup-approval'")
    expect(office).not.toMatch(/callId: 'meeting-followup-approval'[^\n]*status: 'running'/)
    expect(scenarios).not.toMatch(/callId: 'config-edit-approval'[^\n]*status: 'running'/)
    expect(fixtures).toContain("callId: 'meeting-followup-approval'")
    expect(fixtures).toContain("callId: 'config-edit-approval'")
  })

  it('keeps synthetic playback, Agent-loop, child timing and delegation lifecycle in Reference', () => {
    const referenceStream = readFileSync(join(referenceRoot, 'stream-controller.ts'), 'utf8')
    const liveScript = readFileSync(join(referenceRoot, 'live-run-script.ts'), 'utf8')
    expect(referenceStream).toMatch(/\brate\s*=\s*20/)
    expect(referenceStream).toMatch(/\bingressTicks\s*=\s*0/)
    expect(referenceStream).toMatch(/\bpublishTicks\s*=\s*0/)
    expect(referenceStream).toContain('estimateTokens')
    expect(referenceStream).toContain("ReferencePlaybackMode = 'standard' | 'stress' | 'agent-loop'")
    expect(referenceStream).toContain('continueExecutionAt')
    expect(referenceStream).toContain('#updatePlan')
    expect(referenceStream).toContain('#updateDelegations')
    expect(liveScript).toContain('review-rendering-contract')
    expect(liveScript).toContain("mode: 'foreground'")
    expect(liveScript).toContain("mode: 'background'")
    expect(liveScript).toContain('childSessionId')
    expect(liveScript).toContain('terminalChunks')
    expect(liveScript).toContain("category: 'filesystem'")
    expect(liveScript).toContain("category: 'search'")
    expect(liveScript).toContain("category: 'shell'")
    expect(liveScript).toContain("presentation: { kind: 'terminal'")
  })

  it('keeps Vue adapter generic while Reference diagnostics owns playback controls', () => {
    const workspace = readFileSync(join(referenceRoot, 'components/AgentWorkspace.vue'), 'utf8')
    const diagnostics = readFileSync(join(referenceRoot, 'components/ReferenceDiagnosticsPanel.vue'), 'utf8')
    const viewport = readFileSync(join(engineRoot, 'vue/ConversationViewport.vue'), 'utf8')

    expect(workspace).toContain(':execution="activeExecution"')
    expect(workspace).toContain(':stream="activeExecution"')
    expect(workspace).toContain('uiState.activeMessageId')
    expect(viewport).toContain('execution: ConversationExecutionController')
    expect(viewport).not.toContain('stream: ConversationExecutionController')
    expect(viewport).toContain('data-testid="question-answer"')
    expect(viewport).toContain("interactionId: pending.id, kind: 'question', answer: normalized")
    expect(viewport).toContain('data-interaction-id')
    expect(diagnostics).toContain('Reference observability')
    expect(diagnostics).toContain('Reference playback')
    expect(diagnostics).toContain('uiState.activeRenderUnitCount')
    expect(diagnostics).toContain('last turn outcome')
    expect(diagnostics).toContain("lastTurnReason ?? 'none'")
  })

  it('keeps the public workspace focused and diagnostics explicit', () => {
    const workspace = readFileSync(join(referenceRoot, 'components/AgentWorkspace.vue'), 'utf8')
    const viewport = readFileSync(join(engineRoot, 'vue/ConversationViewport.vue'), 'utf8')
    expect(workspace).toContain("from '../../engine/vue'")
    expect(workspace).not.toContain('../../engine/vue/ConversationViewport.vue')
    expect(workspace).not.toContain('data-testid="session-search"')
    expect(workspace).not.toContain('data-testid="scenario-launch"')
    expect(workspace).not.toContain('Synthetic playback')
    expect(workspace).toContain('class="session-section-label"')
    expect(workspace).toContain('data-testid="diagnostics-open"')
    expect(workspace).toContain('navigator.webdriver')
    expect(viewport).toContain('data-conversation-engine="vue"')
    expect(viewport).not.toContain('conversation / {{ runtime.id }}')
    expect(viewport).not.toMatch(/Synthetic Agent|Search conversation|title="Attach"|>Agent ▾<|>Model ▾</)
  })

  it('keeps the Vue public adapter surface instance-oriented rather than globally mutable', () => {
    const publicVue = readFileSync(join(engineRoot, 'vue/index.ts'), 'utf8')
    const registry = readFileSync(join(engineRoot, 'vue/renderers/registry.ts'), 'utf8')
    const viewport = readFileSync(join(engineRoot, 'vue/ConversationViewport.vue'), 'utf8')
    expect(publicVue).toContain('ConversationViewport')
    expect(publicVue).toContain('RenderUnitView')
    expect(publicVue).toContain('RendererRegistry')
    expect(publicVue).toContain('createDefaultRendererRegistry')
    expect(publicVue).not.toMatch(/ConversationNodeSeat|defaultRendererRegistry|registerRenderer|resolveRenderer|registeredRendererIds/)
    expect(registry).toContain('export class RendererRegistry')
    expect(registry).toContain('clone(): RendererRegistry')
    expect(registry).toContain("['plan', PlanBlock]")
    expect(registry).toContain("['terminal', TerminalBlock]")
    expect(registry).toContain("['delegation', DelegationBlock]")
    expect(registry).not.toContain('AgentRunBlock')
    expect(viewport).toContain('renderers?: RendererResolver')
  })

  it('uses one GFM parser contract for chunk boundaries and HTML rendering', () => {
    const chunks = readFileSync(join(engineRoot, 'presentation/markdown-chunks.ts'), 'utf8')
    const renderer = readFileSync(join(engineRoot, 'vue/renderers/markdown-cache.ts'), 'utf8')
    expect(chunks).toContain("import { marked } from 'marked'")
    expect(chunks).toContain('marked.lexer(source, MARKDOWN_OPTIONS)')
    expect(chunks).not.toContain('cleanBoundary')
    expect(renderer).toContain('MARKDOWN_OPTIONS')
    expect(renderer).toContain("from '../../presentation/markdown-chunks'")
  })

  it('keeps realistic public scenarios canonical and renderer-neutral', () => {
    const scenarios = readFileSync(join(referenceRoot, 'session-scenarios.ts'), 'utf8')
    const adapter = readFileSync(join(referenceRoot, 'history-adapter.ts'), 'utf8')
    expect(scenarios).toContain("block('request', 'markdown'")
    expect(scenarios).toContain("'tool-call'")
    expect(scenarios).toContain("'tool-result'")
    expect(scenarios).toContain("'plan'")
    expect(scenarios).toContain("'code'")
    expect(scenarios).toContain("'diff'")
    expect(scenarios).toContain("'attachments'")
    expect(scenarios).toContain("kind: 'file'")
    expect(adapter).toContain('ReadonlyMap<number, LogicalMessage>')
    expect(adapter).toContain('this.#source.getRange')
  })

  it('keeps Reference and Engine Vue CSS ownership compact, scoped and separate', () => {
    const main = readFileSync(join(referenceRoot, '../main.ts'), 'utf8')
    const referenceCss = readFileSync(join(referenceRoot, 'styles/reference.css'), 'utf8')
    expect(main).toContain("./reference/styles/reference.css")
    expect(main).toContain("./engine/vue/engine.css")
    expect(main).toContain("./engine/vue/workbench-renderers.css")
    expect(main).toContain("./reference/styles/architecture.css")
    expect(referenceCss).not.toMatch(/\.session-search|\.scenario-button|\.workspace-context|\.reference-context-(?:chip|copy)|\.sidebar-version|\.session-empty/)
    for (const name of ['engine.css', 'renderers.css', 'workbench-renderers.css']) {
      const css = readFileSync(join(engineRoot, `vue/${name}`), 'utf8')
      expect(css).toContain('[data-conversation-engine].conversation-shell')
      expect(css).not.toMatch(/(^|\n)\s*:root\s*\{/)
      expect(css).not.toMatch(/(^|\n)\s*(?:html|body|#app)(?:\s|,|\{)/)
    }
  })

  it('keeps the architecture page aligned with current repository boundaries', () => {
    const page = readFileSync(join(referenceRoot, 'components/ArchitectureOverview.vue'), 'utf8')
    expect(page).toContain('docs/architecture.md')
    expect(page).toContain('Agent Workbench Rendering Engine')
    expect(page).toContain('Semantic renderability is Engine responsibility')
    expect(page).toContain('delegated child')
    expect(page).toContain('no core PresentationSurface')
    expect(page).toContain('package publishing disabled')
    expect(page).not.toContain('Private Vite application')
    expect(page).not.toContain('Session + Workspace Kernel')
  })

})
