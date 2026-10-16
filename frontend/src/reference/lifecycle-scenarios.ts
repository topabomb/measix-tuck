import { block, type AppendCanonicalMessage, type LogicalMessage, type ResourceRef } from '../engine/model/conversation'

export type LifecycleScenarioKey = 'partial-failure-recovery' | 'steered-interruption'

const fileRef = (id: string, uri: string, label = uri): ResourceRef => ({ id, kind: 'file', uri, label })
const artifactRef = (id: string, uri: string, label: string): ResourceRef => ({ id, kind: 'artifact', uri, label })

/** Reference-only routing for lifecycle scenarios; Engine knows none of these scenario names. */
export function isLifecycleScenario(value: string): value is LifecycleScenarioKey {
  return value === 'partial-failure-recovery' || value === 'steered-interruption'
}

export function createLifecycleScenarioTail(
  sessionId: string,
  logicalCount: number,
  scenario: LifecycleScenarioKey,
): readonly LogicalMessage[] {
  if (logicalCount <= 0) return []
  const entries = scenario === 'partial-failure-recovery'
    ? partialFailureRecoveryScenario(sessionId)
    : steeredInterruptionScenario(sessionId)
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

/**
 * Reference-only Agent lifecycle fixtures. They exercise existing canonical semantics
 * without moving retry/fallback/interrupt policy into the framework-neutral Engine.
 */
export function partialFailureRecoveryScenario(scope: string): readonly AppendCanonicalMessage[] {
  const turn = `${scope}:partial-failure-recovery`
  const step0 = `${turn}:step-0`
  const cachedExport = artifactRef('crm-export-cache', 'artifact://crm/customer-risk-export-2026-08-21.csv', 'customer-risk-export.csv')
  const launchBrief = fileRef('launch-brief', 'docs/launch-brief.md', 'launch-brief.md')
  return [
    {
      turnId: turn,
      stepId: step0,
      role: 'user',
      blocks: [block('request', 'markdown', { markdown: 'Prepare a launch-risk brief from customer, metrics, and policy evidence. Run the specialist checks in parallel. If one source fails, recover with an explicit fallback and call out the evidence gap instead of failing the whole task.' })],
    },
    {
      turnId: turn,
      stepId: `${turn}:step-1`,
      role: 'assistant',
      blocks: [
        block('plan', 'plan', { title: 'Launch-risk synthesis', items: [
          { id: 'fanout', text: 'Run customer, metrics and policy specialist checks', status: 'completed' },
          { id: 'recover', text: 'Recover any missing evidence through an allowed fallback', status: 'completed' },
          { id: 'synthesize', text: 'Produce a decision brief with explicit confidence', status: 'completed' },
        ] }),
        block('delegation', 'delegation', { title: 'Parallel specialist review', runs: [
          { runId: 'customer-risk-specialist', title: 'Customer risk review', agent: 'customer-risk', mode: 'background', status: 'failed', summary: 'CRM live query returned 503 after the adapter retry budget; no child session address was provided by this runtime.' },
          { runId: 'metrics-specialist', title: 'Metrics review', agent: 'metrics-analyst', mode: 'background', status: 'completed', summary: 'Activation is on target; support volume is 14% above the launch threshold.' },
          { runId: 'policy-specialist', title: 'Policy review', agent: 'policy-reviewer', mode: 'background', status: 'completed', summary: 'No blocking policy issue; rollout notice copy needs one clarification.' },
        ] }),
      ],
    },
    {
      turnId: turn,
      stepId: `${turn}:step-2`,
      role: 'assistant',
      blocks: [block('fallback-call', 'tool-call', {
        name: 'read_cached_customer_export',
        callId: 'fallback-crm-export',
        category: 'filesystem',
        presentation: { kind: 'resources', resources: [cachedExport] },
        resources: [cachedExport],
        status: 'success',
        input: { resource: cachedExport.uri, freshness: '24h' },
        durationMs: 94,
        defaultOpen: false,
      })],
    },
    {
      turnId: turn,
      stepId: `${turn}:step-2`,
      role: 'tool',
      blocks: [block('fallback-result', 'tool-result', {
        name: 'read_cached_customer_export',
        callId: 'fallback-crm-export',
        category: 'filesystem',
        presentation: { kind: 'resources', resources: [cachedExport] },
        resources: [cachedExport],
        status: 'success',
        output: { rows: 1842, newestRecord: '2026-08-21T22:10:00Z', finding: 'Enterprise churn risk is concentrated in two accounts; evidence is one day stale because the live CRM source failed.' },
        durationMs: 126,
        defaultOpen: false,
      })],
    },
    {
      turnId: turn,
      stepId: `${turn}:step-3`,
      role: 'assistant',
      blocks: [block('brief', 'markdown', { markdown: '## Launch-risk brief\n\n**Recommendation:** proceed with a guarded rollout. Metrics and policy checks completed normally. The customer specialist failed because the live CRM source was unavailable, so I used the most recent allowed cached export instead of treating one child failure as a parent failure.\n\n| Signal | Result | Confidence |\n| --- | --- | --- |\n| activation | on target | high |\n| support load | +14% vs threshold | high |\n| customer risk | two enterprise accounts need outreach | medium — cached source |\n| policy | no blocker | high |\n\nThe evidence gap is explicit and should be refreshed when CRM service returns.' })],
    },
    {
      turnId: turn,
      stepId: `${turn}:step-3`,
      role: 'assistant',
      blocks: [block('brief-resource', 'code', { filename: launchBrief.label, resource: launchBrief, language: 'markdown', defaultOpen: false, code: '# Launch risk\n\nProceed with guarded rollout. Refresh CRM evidence before broadening the cohort.' })],
    },
  ]
}

export function steeredInterruptionScenario(scope: string): readonly AppendCanonicalMessage[] {
  const firstTurn = `${scope}:migration-run`
  const secondTurn = `${scope}:steered-readonly-review`
  const repo = fileRef('migration-config', 'src/runtime/migration-config.ts', 'migration-config.ts')
  return [
    {
      turnId: firstTurn,
      stepId: `${firstTurn}:step-0`,
      role: 'user',
      blocks: [block('request', 'markdown', { markdown: 'Run the migration dry-run and prepare the write path if the verification is clean.' })],
    },
    {
      turnId: firstTurn,
      stepId: `${firstTurn}:step-1`,
      role: 'assistant',
      blocks: [block('shell-call', 'tool-call', { name: 'run_shell', callId: 'migration-dry-run', category: 'shell', presentation: { kind: 'terminal', command: 'pnpm migration:dry-run' }, status: 'error', input: { command: 'pnpm migration:dry-run' }, durationMs: 1480, defaultOpen: false })],
    },
    {
      turnId: firstTurn,
      stepId: `${firstTurn}:step-1`,
      role: 'tool',
      blocks: [block('shell-result', 'terminal', { callId: 'migration-dry-run', command: 'pnpm migration:dry-run', output: '> checking 24 migration targets\n> validating write preconditions\n^C\nStopped by user before any write phase.\n', status: 'interrupted', exitCode: 130, durationMs: 1480, defaultOpen: true })],
    },
    {
      turnId: firstTurn,
      stepId: `${firstTurn}:step-2`,
      role: 'assistant',
      blocks: [block('interrupted', 'markdown', { markdown: 'The dry-run was interrupted before the write phase. No migration change was applied. I will treat this Turn as stopped rather than silently continuing the previous plan.' })],
    },
    {
      turnId: secondTurn,
      stepId: `${secondTurn}:step-0`,
      role: 'user',
      blocks: [block('steer', 'markdown', { markdown: 'Change direction: do not run the migration. Give me a read-only impact report and the smallest reversible change we would make later.' })],
    },
    {
      turnId: secondTurn,
      stepId: `${secondTurn}:step-1`,
      role: 'assistant',
      blocks: [block('plan', 'plan', { title: 'Read-only migration review', items: [
        { id: 'inspect', text: 'Inspect migration configuration without writes', status: 'completed' },
        { id: 'impact', text: 'Summarize affected targets and rollback boundary', status: 'completed' },
        { id: 'recommend', text: 'Recommend the smallest reversible future change', status: 'completed' },
      ] })],
    },
    {
      turnId: secondTurn,
      stepId: `${secondTurn}:step-1`,
      role: 'assistant',
      blocks: [block('inspect-call', 'tool-call', { name: 'read_file', callId: 'readonly-migration-inspect', category: 'filesystem', presentation: { kind: 'resources', resources: [repo] }, resources: [repo], status: 'success', input: { path: repo.uri }, durationMs: 22, defaultOpen: false })],
    },
    {
      turnId: secondTurn,
      stepId: `${secondTurn}:step-1`,
      role: 'tool',
      blocks: [block('inspect-result', 'tool-result', { name: 'read_file', callId: 'readonly-migration-inspect', category: 'filesystem', presentation: { kind: 'resources', resources: [repo] }, resources: [repo], status: 'success', output: { targets: 24, writesPerformed: 0, reversibleBoundary: 'feature flag + one config value' }, durationMs: 22, defaultOpen: false })],
    },
    {
      turnId: secondTurn,
      stepId: `${secondTurn}:step-2`,
      role: 'assistant',
      blocks: [block('summary', 'markdown', { markdown: '## Read-only impact report\n\nThe interrupted Turn remains historical evidence; the new instruction starts a separate Turn and becomes authoritative. No writes were performed. The future change can be limited to one feature flag plus one config value, with rollback at the same boundary.' })],
    },
  ]
}
