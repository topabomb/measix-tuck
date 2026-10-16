import { block, type AppendCanonicalMessage, type LogicalMessage, type ResourceRef } from '../engine/model/conversation'
import { executiveBriefingScenario, meetingFollowupScenario } from './office-scenarios'

export type ReferenceScenarioKey =
  | 'release-investigation'
  | 'transport-refactor'
  | 'config-approval'
  | 'event-normalization'
  | 'responsive-artifacts'
  | 'multimodal-handoff'
  | 'android-rollout'
  | 'context-recovery'
  | 'executive-briefing'
  | 'meeting-followup'

const fileRef = (id: string, uri: string, label = uri): ResourceRef => ({ id, kind: 'file', uri, label })

/**
 * Realistic recent-tail fixtures for the public Reference.
 *
 * Deep history remains lazily synthetic so stress scenarios can reach 1M+ messages,
 * while the part a user actually lands on reads like a real Agent workspace. Every
 * fixture is canonical conversation data; renderers never receive Reference-only nodes.
 */
export function createScenarioTail(
  sessionId: string,
  logicalCount: number,
  scenario: ReferenceScenarioKey,
): readonly LogicalMessage[] {
  if (logicalCount <= 0) return []
  const entries = scenarioEntries(sessionId, scenario)
  const kept = entries.slice(-Math.min(entries.length, logicalCount))
  const start = logicalCount - kept.length
  return kept.map((entry, offset) => ({
    id: `${sessionId}:m-${start + offset}`,
    index: start + offset,
    turnId: entry.turnId,
    stepId: entry.stepId,
    role: entry.role,
    blocks: entry.blocks,
    revision: 0,
    live: entry.live,
  }))
}

function scenarioEntries(scope: string, scenario: ReferenceScenarioKey): readonly AppendCanonicalMessage[] {
  switch (scenario) {
    case 'release-investigation': return releaseInvestigation(scope)
    case 'transport-refactor': return transportRefactor(scope)
    case 'config-approval': return configApproval(scope)
    case 'event-normalization': return eventNormalization(scope)
    case 'responsive-artifacts': return responsiveArtifacts(scope)
    case 'multimodal-handoff': return multimodalHandoff(scope)
    case 'android-rollout': return androidRollout(scope)
    case 'context-recovery': return contextRecovery(scope)
    case 'executive-briefing': return executiveBriefingScenario(scope)
    case 'meeting-followup': return meetingFollowupScenario(scope)
  }
}

function releaseInvestigation(scope: string): AppendCanonicalMessage[] {
  const turn = `${scope}:release-investigation`
  const step = `${turn}:step-0`
  const workflow = { id: 'release-workflow', kind: 'url' as const, uri: 'https://github.com/topabomb/demo1/actions', label: 'GitHub Actions' }
  return [
    {
      turnId: turn, stepId: step, role: 'user',
      blocks: [block('request', 'markdown', { markdown: 'The Pages deployment is intermittently failing after a renderer change. Trace the regression, inspect the changed files, propose the smallest safe patch, and keep verifying while I browse older history.' })],
    },
    {
      turnId: turn, stepId: step, role: 'assistant',
      blocks: [block('reasoning', 'reasoning', { text: 'Start from the release evidence, then inspect the renderer boundary and keep stable IDs intact while narrowing the regression.', tokenCount: 31, durationMs: 820, defaultOpen: false, status: 'complete' })],
    },
    {
      turnId: turn, stepId: step, role: 'assistant',
      blocks: [block('release-check', 'tool-call', { name: 'read_ci_run', callId: 'release-ci-42', category: 'search', presentation: { kind: 'resources', resources: [workflow] }, resources: [workflow], status: 'success', input: { run: 451, job: 'deploy-pages' }, durationMs: 214, defaultOpen: false })],
    },
    {
      turnId: turn, stepId: step, role: 'tool',
      blocks: [block('release-check-result', 'tool-result', { name: 'read_ci_run', callId: 'release-ci-42', category: 'search', presentation: { kind: 'resources', resources: [workflow] }, resources: [workflow], status: 'success', output: { failedStep: 'deployed-site chromium', symptom: 'row geometry changed after async content measurement', affected: 'Markdown + tool output' }, durationMs: 418, defaultOpen: false })],
    },
    {
      turnId: turn, stepId: `${turn}:step-1`, role: 'assistant', live: true,
      blocks: [
        block('work-plan', 'plan', { title: 'Release regression investigation', items: [
          { id: 'inspect', text: 'Inspect projection and resource boundaries', status: 'in-progress' },
          { id: 'correlate', text: 'Correlate tool and execution identity', status: 'pending' },
          { id: 'verify', text: 'Run the full release gate with terminal evidence', status: 'pending' },
          { id: 'synthesize', text: 'Summarize the smallest rendering-layer patch', status: 'pending' },
        ] }),
        block('reasoning', 'reasoning', { text: 'I have the failing release evidence. I am correlating it with the renderer update before changing code. ', tokenCount: 27, durationMs: 640, defaultOpen: false, status: 'streaming' }),
        block('answer', 'markdown', { markdown: '## Investigating the release regression\n\nI found a measurement-sensitive renderer path and am validating it against the deployed behavior.\n\n' }),
      ],
    },
  ]
}

function transportRefactor(scope: string): AppendCanonicalMessage[] {
  const turn = `${scope}:transport-refactor`
  const step = `${turn}:step-0`
  const callId = 'transport-read-17'
  const adapter = fileRef('transport-adapter', 'src/transport/event-adapter.ts')
  const normalized = fileRef('normalize-provider-event', 'src/transport/normalize-provider-event.ts')
  return [
    { turnId: turn, stepId: step, role: 'user', blocks: [block('request', 'markdown', { markdown: 'Refactor the agent transport adapter so provider events normalize into one stable conversation contract. Show the patch and verification, not a provider-specific UI.' })] },
    { turnId: turn, stepId: step, role: 'assistant', blocks: [block('thinking', 'reasoning', { text: 'Separate provider event decoding from conversation identity, then keep the renderer dependent only on canonical blocks.', tokenCount: 36, durationMs: 1260, defaultOpen: false, status: 'complete' })] },
    { turnId: turn, stepId: step, role: 'assistant', blocks: [block('read', 'tool-call', { name: 'read_file', callId, category: 'filesystem', presentation: { kind: 'resources', resources: [adapter] }, resources: [adapter], status: 'success', input: { path: adapter.uri }, durationMs: 18, defaultOpen: false })] },
    { turnId: turn, stepId: step, role: 'tool', blocks: [block('read-result', 'tool-result', { name: 'read_file', callId, category: 'filesystem', presentation: { kind: 'resources', resources: [adapter] }, resources: [adapter], status: 'success', output: { lines: 186, finding: 'provider event names leak into presentation mapping' }, durationMs: 29, defaultOpen: false })] },
    { turnId: turn, stepId: `${turn}:step-1`, role: 'assistant', blocks: [block('patch', 'diff', { resource: adapter, lines: ['- return { kind: providerEvent.type, payload: providerEvent }', '+ return normalizeProviderEvent(providerEvent)', '+ // presentation consumes canonical ContentBlock[] only', '+ return { turnId, stepId, role, blocks }'], defaultOpen: true })] },
    { turnId: turn, stepId: `${turn}:step-1`, role: 'assistant', blocks: [block('code', 'code', { filename: normalized.label, resource: normalized, language: 'typescript', defaultOpen: true, code: `export function normalizeProviderEvent(event: ProviderEvent): CanonicalMutation {\n  return {\n    turnId: stableTurnId(event),\n    stepId: stableStepId(event),\n    role: normalizeRole(event),\n    blocks: normalizeBlocks(event),\n  }\n}` })] },
    { turnId: turn, stepId: `${turn}:step-2`, role: 'assistant', blocks: [block('summary', 'markdown', { markdown: '### Transport refactor verified\n\n- Provider decoding stays in the adapter.\n- `SessionKernel` receives canonical identities and blocks.\n- Renderer registration remains provider-neutral.\n- Existing history can replay without a provider SDK.' })] },
  ]
}

function configApproval(scope: string): AppendCanonicalMessage[] {
  const turn = `${scope}:config-approval`
  const step = `${turn}:step-0`
  const config = fileRef('runtime-config', 'src/runtime/config.ts')
  return [
    { turnId: turn, stepId: step, role: 'user', blocks: [block('request', 'markdown', { markdown: 'Raise the production worker limit from 8 to 12, but do not apply the workspace edit until I approve the exact diff.' })] },
    { turnId: turn, stepId: step, role: 'assistant', blocks: [block('reasoning', 'reasoning', { text: 'The edit changes production concurrency. Prepare the exact diff and stop at the approval boundary.', tokenCount: 24, durationMs: 620, defaultOpen: false, status: 'complete' })] },
    { turnId: turn, stepId: step, role: 'assistant', blocks: [block('patch', 'diff', { resource: config, lines: [' export const runtimeConfig = {', '-  maxWorkers: 8,', '+  maxWorkers: 12,', '   queuePolicy: \'bounded\',', ' }'], defaultOpen: true })] },
    { turnId: turn, stepId: step, role: 'assistant', blocks: [block('edit', 'tool-call', { name: 'edit_file', callId: 'config-edit-approval', category: 'filesystem', presentation: { kind: 'changes', resources: [config] }, resources: [config], input: { path: config.uri, patch: 'maxWorkers: 8 -> 12', requiresApproval: true }, defaultOpen: false })] },
    { turnId: turn, stepId: step, role: 'assistant', blocks: [block('waiting', 'markdown', { markdown: 'The patch is ready but the tool has not executed. The session is paused at the workspace approval boundary; browsing another conversation will not discard this pending request.' })] },
  ]
}

function eventNormalization(scope: string): AppendCanonicalMessage[] {
  const turn = `${scope}:event-normalization`
  const step = `${turn}:step-0`
  return [
    { turnId: turn, stepId: step, role: 'user', blocks: [block('request', 'markdown', { markdown: 'Normalize this provider tool event and make sure reconnect/replay does not duplicate the result row.' })] },
    { turnId: turn, stepId: step, role: 'assistant', blocks: [block('payload', 'code', { language: 'json', filename: 'provider-event.json', defaultOpen: true, code: `{"type":"tool.result","call_id":"call_91","seq":4812,"payload":{"exitCode":0,"rows":3}}` })] },
    { turnId: turn, stepId: step, role: 'assistant', blocks: [block('reasoning', 'reasoning', { text: 'Correlation belongs to the producer call ID and sequence coordinate, not DOM adjacency. Replay should converge on the same canonical record.', tokenCount: 34, durationMs: 940, defaultOpen: false, status: 'complete' })] },
    { turnId: turn, stepId: `${turn}:step-1`, role: 'assistant', blocks: [block('normalized', 'code', { language: 'typescript', filename: 'normalized-event.ts', defaultOpen: true, code: `const mutation = {\n  turnId: 'turn-204',\n  stepId: 'turn-204:step-3',\n  role: 'tool',\n  callId: 'call_91',\n  revision: 4812,\n}\n` })] },
    { turnId: turn, stepId: `${turn}:step-2`, role: 'assistant', blocks: [block('summary', 'markdown', { markdown: '### Replay result\n\n| Check | Result |\n| --- | --- |\n| stable `callId` | pass |\n| duplicate tool row | none |\n| reconnect replay | deterministic |\n| viewport dependency | none |' })] },
  ]
}

function responsiveArtifacts(scope: string): AppendCanonicalMessage[] {
  const turn = `${scope}:responsive-artifacts`
  const step = `${turn}:step-0`
  return [
    { turnId: turn, stepId: step, role: 'user', blocks: [block('request', 'markdown', { markdown: 'Review these generated artifacts on desktop and mobile. Wide tables, code, images and HTML must stay contained while I expand details.' })] },
    { turnId: turn, stepId: step, role: 'assistant', blocks: [block('preview', 'attachments', { title: 'Generated review artifacts', provenance: { origin: 'tool-output', toolName: 'generate_preview', toolCallId: 'preview-28' }, items: [
      { id: 'preview-desktop', name: 'dashboard-desktop.png', kind: 'image', mimeType: 'image/png', width: 1600, height: 900, sizeBytes: 842_000, seed: 9281, resource: { id: 'preview-desktop-resource', kind: 'artifact', uri: 'artifact://dashboard-desktop', label: 'dashboard-desktop.png' } },
      { id: 'preview-mobile', name: 'dashboard-mobile.png', kind: 'image', mimeType: 'image/png', width: 780, height: 1380, sizeBytes: 624_000, seed: 9282, resource: { id: 'preview-mobile-resource', kind: 'artifact', uri: 'artifact://dashboard-mobile', label: 'dashboard-mobile.png' } },
    ] })] },
    { turnId: turn, stepId: `${turn}:step-1`, role: 'assistant', blocks: [block('layout-table', 'markdown', { markdown: '### Responsive checks\n\n| Surface | Desktop | Mobile | Behavior |\n| --- | --- | --- | --- |\n| image gallery | 2 columns | 1 column | contained |\n| wide table | internal scroll | internal scroll | no page overflow |\n| code | horizontal scroll | horizontal scroll | measured row remains stable |\n| disclosure | expands | expands | adjacent rows re-measure |' })] },
    { turnId: turn, stepId: `${turn}:step-1`, role: 'assistant', blocks: [block('artifact-html', 'html', { html: '<section class="synthetic-html"><h3>Build summary artifact</h3><p>12 checks passed · 0 overflow regressions</p><div class="html-chip">responsive container</div><script>window.__unsafeArtifact=true</script></section>' })] },
    { turnId: turn, stepId: `${turn}:step-2`, role: 'assistant', blocks: [block('summary', 'markdown', { markdown: 'The same canonical artifacts reflow without changing semantic reader position. Active HTML is sanitized before mounting.' })] },
  ]
}

function multimodalHandoff(scope: string): AppendCanonicalMessage[] {
  const turn = `${scope}:multimodal-handoff`
  const step = `${turn}:step-0`
  const asrCall = 'handoff-asr-7'
  return [
    { turnId: turn, stepId: step, role: 'user', blocks: [block('uploads', 'attachments', { title: 'Design handoff', provenance: { origin: 'user-upload' }, items: [
      { id: 'handoff-screen', name: 'agent-workspace.png', kind: 'image', mimeType: 'image/png', width: 1440, height: 960, sizeBytes: 731_000, seed: 6124 },
      { id: 'handoff-spec', name: 'interaction-spec.pdf', kind: 'file', mimeType: 'application/pdf', sizeBytes: 1_842_000 },
      { id: 'handoff-audio', name: 'review-note.m4a', kind: 'audio', mimeType: 'audio/mp4', durationMs: 24_800, sizeBytes: 2_920_000 },
    ] })] },
    { turnId: turn, stepId: step, role: 'assistant', blocks: [block('reasoning', 'reasoning', { text: 'Treat the screenshot, document and voice note as durable artifacts, then correlate transcription with the originating tool call.', tokenCount: 37, durationMs: 1180, defaultOpen: false, status: 'complete' })] },
    { turnId: turn, stepId: step, role: 'assistant', blocks: [block('asr-call', 'tool-call', { name: 'speech_to_text', callId: asrCall, category: 'asr', model: 'asr-reference-1', status: 'success', progress: 100, input: { artifactId: 'handoff-audio', language: 'auto' }, durationMs: 890, defaultOpen: false })] },
    { turnId: turn, stepId: step, role: 'tool', blocks: [block('asr-result', 'tool-result', { name: 'speech_to_text', callId: asrCall, category: 'asr', model: 'asr-reference-1', status: 'success', output: { transcript: 'Keep the session list compact and make the mixed streaming result the primary reference.', language: 'en', confidence: 0.96 }, durationMs: 890, defaultOpen: false })] },
    { turnId: turn, stepId: `${turn}:step-1`, role: 'assistant', blocks: [block('voice-summary', 'audio', { title: 'Review note', purpose: 'asr-input', durationMs: 24_800, transcript: 'Keep the session list compact and make the mixed streaming result the primary reference.', model: 'asr-reference-1', status: 'ready', waveform: Array.from({ length: 40 }, (_, i) => 0.18 + ((i * 29) % 70) / 100) })] },
    { turnId: turn, stepId: `${turn}:step-1`, role: 'assistant', blocks: [block('summary', 'markdown', { markdown: '### Handoff summary\n\nThe image, PDF and audio keep independent artifact identities. The ASR result remains correlated through `callId`, and media rendering is independent from tool execution UI.' })] },
  ]
}

function androidRollout(scope: string): AppendCanonicalMessage[] {
  const turn = `${scope}:android-rollout`
  const step = `${turn}:step-0`
  return [
    { turnId: turn, stepId: step, role: 'user', blocks: [block('request', 'markdown', { markdown: 'Prepare the Android rollout patch. Before choosing the fallback behavior, ask me whether API 35 clients should fail closed or keep the last accepted configuration.' })] },
    { turnId: turn, stepId: step, role: 'assistant', blocks: [block('protocol', 'code', { language: 'kotlin', filename: 'ManagedConfigGate.kt', defaultOpen: true, code: `fun accept(config: ManagedConfig): Decision {\n    if (config.generation < minimumAcceptedGeneration) return Decision.Reject\n    return Decision.Apply(config)\n}` })] },
    { turnId: turn, stepId: step, role: 'assistant', blocks: [block('reasoning', 'reasoning', { text: 'The protocol rule is clear, but the offline fallback is a product decision and must remain blocked on the user question.', tokenCount: 29, durationMs: 740, defaultOpen: false, status: 'complete' })] },
    { turnId: turn, stepId: step, role: 'assistant', blocks: [block('summary', 'markdown', { markdown: 'The patch is staged. I need the fallback decision before finalizing the client behavior; the pending question survives session switching.' })] },
  ]
}

function contextRecovery(scope: string): AppendCanonicalMessage[] {
  const turn = `${scope}:context-recovery`
  const step = `${turn}:step-0`
  const callId = 'context-search-88'
  return [
    { turnId: turn, stepId: step, role: 'user', blocks: [block('request', 'markdown', { markdown: 'Continue the long-context investigation, reuse the cached prefix, and recover cleanly if the provider times out.' })] },
    { turnId: turn, stepId: step, role: 'assistant', blocks: [block('search', 'tool-call', { name: 'search_history', callId, category: 'search', status: 'success', input: { query: 'cache prefix invalidation after tool schema change', limit: 40 }, durationMs: 180, defaultOpen: false })] },
    { turnId: turn, stepId: step, role: 'tool', blocks: [block('search-result', 'tool-result', { name: 'search_history', callId, category: 'search', status: 'success', output: { matches: 17, reusedPrefixTokens: 812000, newestRevision: 4431 }, durationMs: 612, defaultOpen: false })] },
    { turnId: turn, stepId: `${turn}:step-1`, role: 'assistant', blocks: [block('reasoning', 'reasoning', { text: 'The cached prefix is still reusable. The last provider request failed after retrieval, so preserve the durable history and expose the failure without making the session terminal.', tokenCount: 43, durationMs: 2120, defaultOpen: false, status: 'interrupted' })] },
    { turnId: turn, stepId: `${turn}:step-1`, role: 'assistant', blocks: [block('recovery', 'markdown', { markdown: '### Recovery point\n\nThe previous request timed out after the retry budget. Canonical history, cache accounting and the reader snapshot are intact, so a new prompt can resume from this point without reconstructing the UI tree.' })] },
  ]
}
