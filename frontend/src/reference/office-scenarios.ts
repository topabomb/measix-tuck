import { block, type AppendCanonicalMessage, type ResourceRef } from '../engine/model/conversation'

const sourceRef = (id: string, path: string, label: string): ResourceRef => ({
  id,
  kind: 'url',
  uri: `https://workspace.example/${path}`,
  label,
})

const artifactRef = (id: string, name: string): ResourceRef => ({
  id,
  kind: 'artifact',
  uri: `artifact://${id}`,
  label: name,
})

/**
 * Reference-owned office/knowledge-work scenarios. They deliberately reuse the same
 * canonical Engine primitives as the coding scenario: Plan, ResourceRef, tools,
 * delegation, Markdown and artifacts. Connectors, scheduling, approvals and
 * external side effects remain producer/host responsibilities.
 */
export function executiveBriefingScenario(scope: string): readonly AppendCanonicalMessage[] {
  const turn = `${scope}:executive-briefing`
  const step = `${turn}:step-0`
  const launchMail = sourceRef('briefing-launch-mail', 'mail/threads/launch-risk', 'Launch risk email thread')
  const qbrMeeting = sourceRef('briefing-qbr-meeting', 'calendar/events/qbr-review', 'QBR review meeting')
  const metricsDoc = sourceRef('briefing-metrics-doc', 'drive/docs/q3-metrics', 'Q3 metrics workbook')
  const competitor = sourceRef('briefing-competitor', 'web/competitor-update', 'Competitor launch update')
  const sources = [launchMail, qbrMeeting, metricsDoc, competitor] as const
  const memo = artifactRef('executive-briefing-memo', 'Monday Executive Briefing.docx')
  const deck = artifactRef('executive-briefing-deck', 'QBR Decision Review.pptx')
  const workbook = artifactRef('executive-briefing-workbook', 'KPI Snapshot.xlsx')

  return [
    {
      turnId: turn, stepId: step, role: 'user',
      blocks: [block('request', 'markdown', { markdown: 'Prepare Monday’s executive briefing. Pull the latest launch risks from email, next-week commitments from the calendar, KPI evidence from our QBR files, and one external competitor update. Give me the decisions, not a generic summary, and produce a one-page memo, a short deck, and the KPI workbook snapshot.' })],
    },
    {
      turnId: turn, stepId: step, role: 'assistant',
      blocks: [block('plan', 'plan', { title: 'Executive briefing', items: [
        { id: 'collect', text: 'Collect current work context and external evidence', status: 'completed' },
        { id: 'cross-check', text: 'Cross-check launch risk and KPI signals', status: 'completed' },
        { id: 'specialists', text: 'Run customer-risk and metrics specialist reviews', status: 'completed' },
        { id: 'deliver', text: 'Produce decision brief and office artifacts', status: 'completed' },
      ] })],
    },
    {
      turnId: turn, stepId: step, role: 'assistant',
      blocks: [block('collect-call', 'tool-call', { name: 'collect_work_context', callId: 'office-context-11', category: 'search', presentation: { kind: 'resources', resources: sources }, resources: sources, status: 'success', input: { sources: ['mail', 'calendar', 'documents', 'web'], horizon: 'next 7 days' }, durationMs: 740, defaultOpen: false })],
    },
    {
      turnId: turn, stepId: step, role: 'tool',
      blocks: [block('collect-result', 'tool-result', { name: 'collect_work_context', callId: 'office-context-11', category: 'search', presentation: { kind: 'resources', resources: sources }, resources: sources, status: 'success', output: { evidenceItems: 18, decisions: 3, risks: 2, calendarCommitments: 5, sourceCoverage: 'mail + calendar + documents + web' }, durationMs: 740, defaultOpen: false })],
    },
    {
      turnId: turn, stepId: `${turn}:step-1`, role: 'assistant',
      blocks: [block('delegation', 'delegation', { title: 'Parallel specialist checks', runs: [
        { runId: 'briefing-synthesis', title: 'Executive synthesis', agent: 'briefing-agent', mode: 'foreground', status: 'completed', childSessionId: 'child-office-synthesis', summary: 'Resolved three decision points and kept evidence tied to the source set.' },
        { runId: 'briefing-customer-risk', title: 'Customer risk scan', agent: 'customer-agent', mode: 'background', status: 'completed', childSessionId: 'child-office-customer-risk', summary: 'Two launch accounts need named owners before Thursday.' },
        { runId: 'briefing-metrics-check', title: 'KPI consistency check', agent: 'metrics-agent', mode: 'background', status: 'completed', childSessionId: 'child-office-metrics', summary: 'Pipeline is ahead of plan, but activation remains below the QBR target.' },
      ] })],
    },
    {
      turnId: turn, stepId: `${turn}:step-2`, role: 'assistant',
      blocks: [block('brief', 'markdown', { markdown: '## Monday executive briefing\n\n### Decisions needed\n\n1. **Launch risk:** assign executive owners to the two at-risk accounts before Thursday.\n2. **Activation gap:** keep the launch date, but move one growth experiment into the activation funnel this week.\n3. **Competitive response:** update the sales talk track before the Tuesday pipeline review.\n\n### Evidence\n\n| Signal | Current read | Source |\n| --- | --- | --- |\n| pipeline | ahead of plan | Q3 metrics workbook |\n| activation | below QBR target | Q3 metrics workbook |\n| launch accounts | 2 need owners | launch risk email thread |\n| competitor | packaging changed | external update |\n\nThe source-bearing tool record above keeps the final brief traceable without making connector semantics part of the Engine.' })],
    },
    {
      turnId: turn, stepId: `${turn}:step-2`, role: 'assistant',
      blocks: [block('artifacts', 'attachments', { title: 'Executive briefing deliverables', provenance: { origin: 'assistant' }, items: [
        { id: 'briefing-memo-file', name: memo.label ?? 'Monday Executive Briefing.docx', kind: 'file', mimeType: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document', sizeBytes: 184_000, resource: memo },
        { id: 'briefing-deck-file', name: deck.label ?? 'QBR Decision Review.pptx', kind: 'file', mimeType: 'application/vnd.openxmlformats-officedocument.presentationml.presentation', sizeBytes: 1_820_000, resource: deck },
        { id: 'briefing-workbook-file', name: workbook.label ?? 'KPI Snapshot.xlsx', kind: 'file', mimeType: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet', sizeBytes: 412_000, resource: workbook },
      ] })],
    },
  ]
}

export function meetingFollowupScenario(scope: string): readonly AppendCanonicalMessage[] {
  const turn = `${scope}:meeting-followup`
  const step = `${turn}:step-0`
  const transcript = sourceRef('followup-transcript', 'meetings/launch-readiness/transcript', 'Launch readiness transcript')
  const thread = sourceRef('followup-thread', 'mail/threads/launch-readiness', 'Launch readiness email thread')
  const brief = sourceRef('followup-brief', 'drive/docs/launch-brief', 'Launch brief')
  const resources = [transcript, thread, brief] as const

  return [
    {
      turnId: turn, stepId: step, role: 'user',
      blocks: [block('request', 'markdown', { markdown: 'Turn the launch-readiness meeting into a clean follow-up. Reconcile the transcript with the latest email thread and launch brief, extract owners and due dates, draft the message, and prepare the 30-minute review for Friday. Do not send or schedule anything until I approve the exact action.' })],
    },
    {
      turnId: turn, stepId: step, role: 'assistant',
      blocks: [block('plan', 'plan', { title: 'Meeting follow-up', items: [
        { id: 'reconcile', text: 'Reconcile transcript, email and launch brief', status: 'completed' },
        { id: 'actions', text: 'Extract owners, dates and unresolved decisions', status: 'completed' },
        { id: 'draft', text: 'Draft follow-up message and review meeting', status: 'completed' },
        { id: 'send', text: 'Send message and schedule review', status: 'blocked' },
      ] })],
    },
    {
      turnId: turn, stepId: step, role: 'assistant',
      blocks: [block('context-call', 'tool-call', { name: 'read_meeting_context', callId: 'meeting-context-24', category: 'search', presentation: { kind: 'resources', resources }, resources, status: 'success', input: { meeting: 'Launch readiness', include: ['transcript', 'mail', 'brief'] }, durationMs: 510, defaultOpen: false })],
    },
    {
      turnId: turn, stepId: step, role: 'tool',
      blocks: [block('context-result', 'tool-result', { name: 'read_meeting_context', callId: 'meeting-context-24', category: 'search', presentation: { kind: 'resources', resources }, resources, status: 'success', output: { decisions: 2, actionItems: 4, unresolved: 1, participants: 6 }, durationMs: 510, defaultOpen: false })],
    },
    {
      turnId: turn, stepId: `${turn}:step-1`, role: 'assistant',
      blocks: [block('draft', 'markdown', { markdown: '## Follow-up draft\n\n**Subject:** Launch readiness — owners and Friday review\n\n- Maya — confirm migration checklist by **Wednesday 3 PM**.\n- Daniel — publish the support escalation matrix by **Thursday noon**.\n- Priya — validate the two at-risk customer timelines before **Thursday EOD**.\n- Growth — bring the activation experiment choice to the **Friday review**.\n\nI also prepared a 30-minute Friday review with the six meeting participants. The combined send/schedule action is staged below and is intentionally blocked on approval.' })],
    },
    {
      turnId: turn, stepId: `${turn}:step-2`, role: 'assistant',
      blocks: [block('send-followup', 'tool-call', { name: 'send_meeting_followup', callId: 'meeting-followup-approval', category: 'productivity', presentation: { kind: 'resources', resources: [thread] }, resources: [thread], input: { recipients: 6, subject: 'Launch readiness — owners and Friday review', calendar: { durationMinutes: 30, when: 'Friday 10:00' }, requiresApproval: true }, defaultOpen: false })],
    },
    {
      turnId: turn, stepId: `${turn}:step-2`, role: 'assistant',
      blocks: [block('waiting', 'markdown', { markdown: 'The proposed message/calendar tool call is correlated to the pending approval but has not executed, so it intentionally carries no execution status. Approve or deny the blocker in the composer; the external adapter owns the actual side effect and any provider continuation.' })],
    },
  ]
}
