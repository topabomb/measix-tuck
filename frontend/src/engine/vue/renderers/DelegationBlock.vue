<script setup lang="ts">
import { computed } from 'vue'
import type { AgentRunRef } from '../../model/conversation'
import type { RenderUnit } from '../../presentation/render-unit'

const props = defineProps<{ unit: RenderUnit }>()
const runs = computed(() => Array.isArray(props.unit.payload.runs) ? props.unit.payload.runs as AgentRunRef[] : [])
const title = computed(() => typeof props.unit.payload.title === 'string' ? props.unit.payload.title : 'Delegated agents')
const aggregate = computed(() => {
  const counts = new Map<string, number>()
  for (const run of runs.value) counts.set(run.status, (counts.get(run.status) ?? 0) + 1)
  return [...counts.entries()].map(([status, count]) => `${count} ${status}`).join(' · ')
})
</script>

<template>
  <article class="message-card delegation-card" data-testid="delegation-block">
    <div class="delegation-head">
      <span class="delegation-glyph">↗</span>
      <span class="delegation-title">
        <strong>{{ title }}</strong>
        <small>{{ runs.length }} run<template v-if="runs.length !== 1">s</template><template v-if="aggregate"> · {{ aggregate }}</template></small>
      </span>
    </div>
    <div class="delegation-runs">
      <section
        v-for="run in runs"
        :key="run.runId"
        class="delegation-run"
        :class="`delegation-run-${run.status}`"
        :data-run-id="run.runId"
        :data-mode="run.mode"
        :data-status="run.status"
        :data-child-session-id="run.childSessionId"
        data-testid="delegation-run"
      >
        <span class="delegation-run-main">
          <strong>{{ run.title }}</strong>
          <small>{{ run.agent ?? 'agent' }} · {{ run.mode }}<template v-if="run.childSessionId"> · {{ run.childSessionId }}</template></small>
        </span>
        <span :class="['status', run.status]"><i />{{ run.status }}</span>
        <p v-if="run.summary" class="delegation-summary">{{ run.summary }}</p>
      </section>
    </div>
  </article>
</template>
