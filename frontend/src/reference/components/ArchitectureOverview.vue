<script setup lang="ts">
const ownership = [
  ['External adapters', 'Provider · Agent runtime · connectors · persistence', 'Own protocol decoding, model/tool/delegated-child execution, enterprise connector auth, real external actions, permission policy, retries/backoff/fallback choice, post-interaction provider continuation, durable persistence and async IO.'],
  ['Framework-neutral Engine', 'src/engine core', 'Own canonical history/identity, exact interaction correlation, current WorkPlan and session invariants, producer-reported execution facts, bounded projection and semantic viewport policy. No product workflow or layout policy.'],
  ['Reference host', 'src/reference/**', 'Own multi-session composition, parent/child navigation, scripted coding/office/lifecycle tasks, fake sources/actions, synthetic playback/history, diagnostics shortcuts and this architecture page.'],
]

const contracts = [
  ['01', 'Canonical identity', 'Message · Turn · Step · Block · callId', 'Stable producer coordinates survive remounts and virtualization. DOM adjacency is never business identity.'],
  ['02', 'Workbench semantics', 'ResourceRef · Plan · Tool intent · Terminal · Delegation', 'Describe resources, progress, streams and delegated child evidence without defining panels, scheduling, connectors, retry or fallback policy.'],
  ['03', 'Current work state', 'WorkPlan = canonical Plan shape', 'SessionKernel stores explicit current WorkPlan. It never scans latest history or DOM to infer which historical Plan is current.'],
  ['04', 'Interaction state', 'PendingInteraction · interactionId · callId?', 'Engine owns the exact blocker identity and may correlate an approval to one tool call. It does not decide what approval/answer causes next.'],
  ['05', 'Delegated child refs', 'AgentRunRef · foreground/background · childSessionId', 'Parent keeps stable child identity/status/summary. Detailed child trace stays in an independently addressable conversation session.'],
  ['06', 'SessionKernel', 'renderable runtime truth + invariants', 'Execution state, WorkPlan, blockers, queue, explicit outcomes and accounting. It rejects stale blocker responses and illegal state combinations without becoming a workflow engine.'],
  ['07', 'History source', 'sync hot-read boundary', 'Async database/network/connector fetch and cache fill stop before ConversationHistorySource.'],
  ['08', 'Projection', 'bounded rebuildable presentation', 'Hot canonical content becomes stable RenderUnits. Projection preserves producer facts and never invents missing tool execution status.'],
  ['09', 'Semantic viewport', 'reader · Latest · anchor · follow', 'Conversation position remains semantic while current-plan disclosure, child navigation and rich rows change physical composition.'],
  ['10', 'Vue reference adapter', 'optional physical integration', 'Virtua, DOM measurement, components and CSS demonstrate one implementation without entering core semantics or inventing provider truth.'],
]

const distinctions = [
  ['Plan ≠ Step', 'Plan reports intended/progress work; Step reports execution that actually happened.'],
  ['Plan snapshot ≠ current WorkPlan', 'Conversation Plan blocks are replayable snapshots. Session activePlan is explicit current state using the same semantic shape.'],
  ['Tool category ≠ presentation intent', 'Capability such as filesystem/search/shell/productivity is independent from generic/resources/changes/terminal interpretation.'],
  ['ResourceRef ≠ host action', 'File/URL/artifact identity does not say whether the host opens an editor, browser, mail app, document or nothing.'],
  ['Interaction id ≠ tool call id', 'interactionId identifies one session blocker. callId identifies one canonical tool call. PendingApproval may correlate them without collapsing the identities.'],
  ['Approval request ≠ tool execution', 'A proposed tool call can exist before approval with no execution status. Clearing approval does not itself execute the tool.'],
  ['Missing tool status ≠ running', 'Tool status/duration/progress are producer-reported facts. Projection and Vue never fabricate running or success when the producer reported none.'],
  ['Delegation ≠ subagent runtime', 'Engine renders producer-reported run identity, mode and status; it never schedules, resumes, stops or selects a child Agent.'],
  ['Child status ≠ parent status', 'A child may fail or remain running while its parent advances or completes; child state never determines parent outcome.'],
  ['Child reference ≠ child trace', 'Parent history keeps runId/childSessionId/summary only. Child messages/tools/nested delegation remain in the child session.'],
  ['Child address ≠ session tree', 'childSessionId is a semantic address. Parent/child topology, visibility, activation and return navigation are Host/Reference state.'],
  ['Failure evidence ≠ retry/fallback policy', 'Failed tool/child evidence is renderable truth. Retry budget, backoff, fallback source and acceptable confidence belong to runtime/host policy.'],
  ['Interrupted Turn ≠ continuation policy', 'Interrupted terminal/Turn evidence remains history. A later instruction may retry, branch or start a new Turn without Engine choosing that policy.'],
  ['Semantic content ≠ layout surface', 'There is intentionally no core PresentationSurface, composer-strip placement, panel, sidebar, tab or preview-placement contract.'],
]

const hotPaths = [
  ['Streaming reasoning', 'O(delta)', 'Replace one stable thinking RenderUnit.'],
  ['Streaming Markdown', 'O(delta + mutable tail)', 'Reparse only the parser-aligned mutable tail; settled prefix units retain identity.'],
  ['Streaming terminal', 'O(delta)', 'Append output into one stable terminal RenderUnit while unrelated siblings retain identity.'],
  ['Delegation status update', 'O(changed block)', 'Reproject one parent-visible delegation block; never project child history recursively.'],
  ['Current WorkPlan update', 'O(plan items)', 'Update explicit session state; never scan total history to find the latest Plan.'],
  ['Interaction resolution', 'O(1)', 'Validate exact interaction identity and clear one blocker; no history scan or provider workflow is inferred.'],
  ['Neighbor history shift', 'O(incoming slice)', 'Project only the incoming 512-message slice.'],
  ['Far jump', 'O(hot window)', 'Rebase one bounded hot window without scanning total history.'],
  ['Visible render', 'O(visible + overscan)', 'The physical adapter mounts only visible/overscan rows.'],
  ['Latest', 'O(1)', 'logicalCount - 1 - committed reader.'],
]

const referenceEvidence = [
  ['Coding-agent task', 'Current WorkPlan + history Plan → resource-aware tools → foreground/background children → parent terminal → final diff/code/artifacts.'],
  ['Inspectable child conversation', 'Click a parent delegation row to activate a real independent child transcript; return through Host navigation without recursive parent history.'],
  ['Executive briefing', 'Synthetic mail/calendar/doc/web ResourceRefs → source-bearing tool evidence → parallel specialists → decision brief → DOCX/PPTX/XLSX artifacts.'],
  ['Meeting follow-up approval', 'Draft + blocked WorkPlan → proposed productivity call with no execution status → PendingApproval with its own interaction id correlated to the exact callId.'],
  ['Production config approval', 'Exact diff → proposed edit_file call without a running claim → session-owned approval correlated to that callId.'],
  ['Clarify and continue', 'Typed PendingQuestion → exact answer resolution → blocker clears → same session can later start execution. Engine does not choose provider continuation.'],
  ['Partial failure recovery', 'One background specialist failed while two completed → Reference/runtime uses a cached fallback → parent completes while failed evidence stays visible.'],
  ['Interrupt and steer', 'Earlier terminal remains interrupted with exit 130 → later user direction starts a separate read-only Turn without rewriting old evidence.'],
  ['Diagnostics shortcuts', 'Restart / Plan / Delegation / Terminal / Final / office/lifecycle buttons are Reference-owned jumps, not Engine workflow or navigation APIs.'],
  ['Million-message stress', 'Dedicated 1M scenario proves bounded projection/cache/DOM independently from workbench workflow transitions.'],
]

const nonGoals = [
  'Project / repository / worktree lifecycle',
  'Agent/model routing or delegated-child scheduling/concurrency',
  'child provider selection, permissions, resume / interrupt / disposal',
  'retry budgets, retryability, backoff, fallback-source selection or automatic recovery policy',
  'post-approval / post-answer provider continuation and actual tool execution',
  'parent/child workspace tree, breadcrumbs, activation or return navigation',
  'Gmail / Outlook / Calendar / Drive / Teams / SharePoint connector and auth contracts',
  'mail sending, meeting creation or enterprise document editing',
  'scheduled / recurring workflow orchestration',
  'permission / allowlist evaluation',
  'editor/resource opening behavior',
  'composer/current-task strip placement or interaction design',
  'Changes / Artifacts / Preview panel layout',
  'tabs / sidebars / drawers / workspace navigation',
  'preview server, process or background-job lifecycle',
  'durable cloud sync and provider retry policy',
]

const invariants = [
  ['1,000,000+', 'addressable logical messages'],
  ['~2,048', 'hot messages/runtime'],
  ['≤ 4,096', 'projection cache entries'],
  ['≤ 3', 'Reference hot runtimes'],
  ['< 180', 'mounted rows'],
  ['< 4 px', 'normal anchor drift'],
]
</script>

<template>
  <main class="architecture-page" data-testid="architecture-page">
    <header class="architecture-nav">
      <a class="architecture-brand" href="#/architecture"><span>N</span> Agent Workbench Rendering Engine</a>
      <nav><a href="#ownership">Ownership</a><a href="#contracts">Semantics</a><a href="#efficiency">Efficiency</a><a href="#evidence">Evidence</a><a class="launch-lab" href="#/reference" data-testid="launch-lab">Open reference →</a></nav>
    </header>

    <section class="architecture-hero">
      <div class="hero-copy">
        <span class="architecture-kicker">stable semantic truth · explicit invariants · workflow-neutral rendering</span>
        <h1>Put reusable Agent semantics in the Engine without turning it into <em>the Agent runtime.</em></h1>
        <p>The core owns cross-product renderable truth: canonical identities, exact blocker correlation, current WorkPlan, producer-reported execution facts and semantic viewport state. Provider execution, recovery policy, child scheduling, connectors, external actions and product layout remain outside it.</p>
        <div class="hero-actions"><a class="primary-link" href="#lab">Exercise the reference</a><a class="secondary-link" href="https://github.com/topabomb/demo1/blob/34d103e496ddddeb0826130d6ef9f8a93d21a099/docs/architecture.md">Upstream rendering reference</a></div>
      </div>
      <div class="hero-proof"><span>Normal UI work</span><strong>O(changed + hot + visible)</strong><p>One million records stay addressable while session truth updates directly and only hot semantic history plus visible physical rows do work.</p><div><b>Identity-safe</b><b>Truthful-status</b><b>Policy-neutral</b><b>Layout-agnostic</b></div></div>
    </section>

    <section id="ownership" class="architecture-section">
      <header class="section-heading"><span>01 · Ownership</span><h2>External runtime → semantic Engine ← Reference host</h2><p>The reusable core owns facts and invariants that survive across Agent products, while execution and workflow choices stay at the boundary.</p></header>
      <div class="invariant-grid state-grid"><article v-for="entry in ownership" :key="entry[0]"><strong>{{ entry[0] }}</strong><span>{{ entry[1] }}</span><p>{{ entry[2] }}</p></article></div>
      <div class="workspace-band"><span>Core rule</span><strong>Semantic renderability is Engine responsibility · product layout and workspace navigation are not</strong><p>`src/engine/**` never imports Reference. Framework-neutral layers never import Vue/DOM.</p></div>
    </section>

    <section id="contracts" class="architecture-section">
      <header class="section-heading"><span>02 · Rendering contracts</span><h2>General semantics go inward; behavior policy stays outward</h2><p>Exact blocker identity and truthful execution status belong in Engine because every client must preserve them. Retry, approval consequences and provider continuation do not.</p></header>
      <div class="layer-stack">
        <article v-for="layer in contracts" :key="layer[0]" class="layer-card">
          <div class="layer-number">{{ layer[0] }}</div>
          <div class="layer-main"><div class="layer-title"><h3>{{ layer[1] }}</h3><span>{{ layer[2] }}</span></div><p>{{ layer[3] }}</p></div>
        </article>
      </div>
    </section>

    <section class="architecture-section split-section">
      <div>
        <header class="section-heading compact"><span>03 · Semantic uniqueness</span><h2>Concepts that must not collapse into each other</h2></header>
        <div class="scenario-table"><div v-for="row in distinctions" :key="row[0]" class="scenario-row"><strong>{{ row[0] }}</strong><span>{{ row[1] }}</span></div></div>
      </div>
      <div>
        <header class="section-heading compact"><span>04 · Data path</span><h2>Current truth is explicit; workflow policy stops before Engine</h2></header>
        <div class="code-diagram"><pre>Provider / Agent runtime / connectors / DB
        │ normalize history + current truth
        ▼
ConversationHistorySource + SessionKernel
  history     activePlan     PendingInteraction
                              id ── callId?
        │                         │
        ▼                         ▼
ProjectionEngine          exact blocker validation
  preserve explicit       no side effect / no resume policy
  tool execution facts
        │
        ▼
semantic viewport / optional Vue adapter

childSessionId = address only
failed/interrupted = evidence only
Reference/Host owns navigation + scenario composition
External runtime owns execution + recovery policy</pre></div>
      </div>
    </section>

    <section id="efficiency" class="architecture-section">
      <header class="section-heading"><span>05 · Hot-path budgets</span><h2>Correct semantics do not change the scaling law</h2><p>Interaction resolution is direct session-state validation; WorkPlan and delegation updates remain bounded; no workflow engine or history scan is introduced.</p></header>
      <div class="scenario-table"><div v-for="row in hotPaths" :key="row[0]" class="scenario-row"><strong>{{ row[0] }}<small style="display:block;margin-top:4px;opacity:.65">{{ row[1] }}</small></strong><span>{{ row[2] }}</span></div></div>
    </section>

    <section id="evidence" class="architecture-section">
      <header class="section-heading"><span>06 · Executable evidence</span><h2>The Reference tests semantic truth at real workflow boundaries</h2><p>Proposed actions, approvals, partial failure, interruption and delegated child work all use normal canonical/session/projector paths without connector- or recovery-specific Engine types.</p></header>
      <div class="scenario-table"><div v-for="row in referenceEvidence" :key="row[0]" class="scenario-row"><strong>{{ row[0] }}</strong><span>{{ row[1] }}</span></div></div>
      <div class="invariant-grid"><article v-for="entry in invariants" :key="entry[1]"><strong>{{ entry[0] }}</strong><span>{{ entry[1] }}</span></article></div>
    </section>

    <section class="architecture-section">
      <header class="section-heading"><span>07 · Explicit non-goals</span><h2>Useful Agent-product behavior that stays outside this Engine</h2><p>A host/runtime may use all of it. Its existence does not justify coupling behavior policy to neutral rendering truth.</p></header>
      <div class="scenario-table"><div v-for="item in nonGoals" :key="item" class="scenario-row"><strong>Host / external adapter</strong><span>{{ item }}</span></div></div>
    </section>

    <section class="architecture-section">
      <header class="section-heading"><span>08 · Distribution honesty</span><h2>Reusable source boundary today; package distribution remains separate</h2></header>
      <div class="workspace-band"><span>Current repository</span><strong>Vite Reference application · package publishing disabled</strong><p>`private: true` disables npm publication; it does not describe repository visibility. The Engine is source-level reusable and extraction-ready.</p></div>
    </section>
  </main>
</template>
