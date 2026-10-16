import { block, type AppendCanonicalMessage, type LogicalMessage, type ResourceRef } from '../engine/model/conversation'

const fileRef = (id: string, uri: string, label = uri): ResourceRef => ({ id, kind: 'file', uri, label })

/**
 * Detailed child-session transcripts for the Reference. The parent keeps only AgentRunRef
 * metadata; these messages live in independently addressable conversation sessions.
 */
export function createChildScenarioTail(sessionId: string, logicalCount: number): readonly LogicalMessage[] {
  const entries = childEntries(sessionId)
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

function childEntries(sessionId: string): readonly AppendCanonicalMessage[] {
  switch (sessionId) {
    case 'child-review-contract': return renderingContractReview(sessionId)
    case 'child-terminal-audit': return terminalProjectionAudit(sessionId)
    case 'child-resource-audit': return resourceSemanticsAudit(sessionId)
    default: return []
  }
}

function renderingContractReview(scope: string): readonly AppendCanonicalMessage[] {
  const turn = `${scope}:review`
  const projection = fileRef('child-projection', 'src/engine/presentation/projection-engine.ts', 'projection-engine.ts')
  const architecture = fileRef('child-architecture', 'docs/architecture.md', 'architecture.md')
  return [
    { turnId: turn, stepId: `${turn}:step-0`, role: 'user', blocks: [block('task', 'markdown', { markdown: 'Review the rendering contract independently. Verify that the new workbench semantics do not couple canonical state to layout, orchestration, or DOM identity. Return a concise finding to the parent.' })] },
    { turnId: turn, stepId: `${turn}:step-1`, role: 'assistant', blocks: [block('reasoning', 'reasoning', { text: 'I am checking the projection boundary and architecture contract rather than trusting the parent summary. The key question is whether semantic identity survives remounts and whether product actions stay outside core state.', status: 'complete', tokenCount: 74, durationMs: 1_120, defaultOpen: true })] },
    { turnId: turn, stepId: `${turn}:step-1`, role: 'assistant', blocks: [block('read-contract', 'tool-call', { name: 'read_files', callId: 'child-contract-read', category: 'filesystem', presentation: { kind: 'resources', resources: [projection, architecture] }, resources: [projection, architecture], input: { paths: [projection.uri, architecture.uri] }, status: 'success', durationMs: 360, defaultOpen: false })] },
    { turnId: turn, stepId: `${turn}:step-1`, role: 'tool', blocks: [block('read-contract-result', 'tool-result', { name: 'read_files', callId: 'child-contract-read', category: 'filesystem', presentation: { kind: 'resources', resources: [projection, architecture] }, resources: [projection, architecture], output: { finding: 'Canonical Message/Turn/Step/Block identities and ResourceRef/Plan/Delegation semantics contain no panel placement, CSS, scheduler, permission, or child-trace payload.' }, status: 'success', durationMs: 360, defaultOpen: false })] },
    { turnId: turn, stepId: `${turn}:step-2`, role: 'assistant', blocks: [block('result', 'markdown', { markdown: '## Child review result\n\nThe rendering contract is clean. Projection consumes semantic records only; Vue owns physical rendering; the Reference/host owns workspace navigation and orchestration. **No layout or subagent scheduler policy leaked into the framework-neutral Engine.**\n\nThis is the detailed child transcript. The parent receives only the short summary attached to `child-review-contract`.' })] },
  ]
}

function terminalProjectionAudit(scope: string): readonly AppendCanonicalMessage[] {
  const turn = `${scope}:audit`
  const runtime = fileRef('child-runtime', 'src/engine/runtime/session-runtime.ts', 'session-runtime.ts')
  return [
    { turnId: turn, stepId: `${turn}:step-0`, role: 'user', blocks: [block('task', 'markdown', { markdown: 'Audit terminal streaming independently. Confirm that append-only output patches one stable render unit and does not force a full history rebuild.' })] },
    { turnId: turn, stepId: `${turn}:step-1`, role: 'assistant', blocks: [block('reasoning', 'reasoning', { text: 'I will follow the terminal patch from the canonical mutation through SessionRuntime into ProjectionEngine and verify identity preservation.', status: 'complete', tokenCount: 51, durationMs: 860, defaultOpen: true })] },
    { turnId: turn, stepId: `${turn}:step-1`, role: 'assistant', blocks: [block('search', 'tool-call', { name: 'search_code', callId: 'child-terminal-search', category: 'search', presentation: { kind: 'resources', resources: [runtime] }, resources: [runtime], input: { query: 'append-terminal appendTerminalDelta' }, status: 'success', durationMs: 290, defaultOpen: false })] },
    { turnId: turn, stepId: `${turn}:step-1`, role: 'tool', blocks: [block('terminal-evidence', 'terminal', { callId: 'child-terminal-search', command: 'pnpm test -- projection-engine session-runtime', output: '✓ append-terminal routes to ProjectionEngine.appendTerminalDelta\n✓ terminal RenderUnit id remains stable\n✓ unrelated sibling units retain identity\n', status: 'success', exitCode: 0, durationMs: 1_040, defaultOpen: true })] },
    { turnId: turn, stepId: `${turn}:step-2`, role: 'assistant', blocks: [block('result', 'markdown', { markdown: '## Terminal audit result\n\nThe append path is incremental and bounded. A terminal delta replaces one keyed unit; it does not recursively project the parent history or scan total history. The parent can safely summarize this as **stable RenderUnit confirmed**.' })] },
  ]
}

function resourceSemanticsAudit(scope: string): readonly AppendCanonicalMessage[] {
  const turn = `${scope}:audit`
  const model = fileRef('child-model', 'src/engine/model/conversation.ts', 'conversation.ts')
  return [
    { turnId: turn, stepId: `${turn}:step-0`, role: 'user', blocks: [block('task', 'markdown', { markdown: 'Audit ResourceRef semantics. Verify that file/URL/artifact identity does not encode editor routing, panel placement, downloads, or other host actions.' })] },
    { turnId: turn, stepId: `${turn}:step-1`, role: 'assistant', blocks: [block('reasoning', 'reasoning', { text: 'I am separating semantic addressability from product navigation. The resource contract should answer what and where, never what the host must do on activation.', status: 'complete', tokenCount: 55, durationMs: 910, defaultOpen: true })] },
    { turnId: turn, stepId: `${turn}:step-1`, role: 'assistant', blocks: [block('read', 'tool-call', { name: 'read_file', callId: 'child-resource-read', category: 'filesystem', presentation: { kind: 'resources', resources: [model] }, resources: [model], input: { path: model.uri }, status: 'success', durationMs: 240, defaultOpen: false })] },
    { turnId: turn, stepId: `${turn}:step-1`, role: 'tool', blocks: [block('read-result', 'tool-result', { name: 'read_file', callId: 'child-resource-read', category: 'filesystem', presentation: { kind: 'resources', resources: [model] }, resources: [model], output: { fields: ['id', 'kind', 'uri', 'label', 'range'], forbidden: ['openInEditor', 'panel', 'download', 'route'] }, status: 'success', durationMs: 240, defaultOpen: false })] },
    { turnId: turn, stepId: `${turn}:step-2`, role: 'assistant', blocks: [block('result', 'markdown', { markdown: '## Resource audit result\n\n`ResourceRef` remains host-neutral: stable identity, kind, URI, label, and optional range only. Opening an editor, browser, preview, download, or side panel is entirely **host-owned**. The parent summary is therefore sufficient while this detailed evidence remains in the child session.' })] },
  ]
}
