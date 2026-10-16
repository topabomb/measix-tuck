<script setup lang="ts">
import { computed } from 'vue'
import type { RenderUnit } from '../../presentation/render-unit'
import { useFoldState } from './fold-state'

const props = defineProps<{ unit: RenderUnit }>()
const open = useFoldState(props.unit.id, Boolean(props.unit.payload.defaultOpen))
const phase = computed(() => String(props.unit.payload.phase ?? 'call'))
const status = computed(() => typeof props.unit.payload.status === 'string' ? props.unit.payload.status : '')
const category = computed(() => String(props.unit.payload.category ?? 'generic'))
const callId = computed(() => String(props.unit.payload.callId ?? ''))
const presentation = computed(() => {
  const value = props.unit.payload.presentation as { kind?: unknown } | undefined
  return String(value?.kind ?? 'generic')
})
const categoryGlyph = computed(() => ({
  filesystem: '▣',
  search: '⌕',
  shell: '>_',
  'image-generation': '◇',
  tts: '◖',
  asr: '◗',
  generic: '⚙',
}[category.value] ?? '◆'))
const model = computed(() => typeof props.unit.payload.model === 'string' ? props.unit.payload.model : '')
const progress = computed(() => {
  const value = Number(props.unit.payload.progress)
  return Number.isFinite(value) ? Math.max(0, Math.min(100, value)) : null
})
const input = computed(() => JSON.stringify(props.unit.payload.input ?? {}, null, 2))
const output = computed(() => JSON.stringify(props.unit.payload.output ?? {}, null, 2))
const duration = computed(() => typeof props.unit.payload.durationMs === 'number' ? props.unit.payload.durationMs : null)
const resources = computed(() => Array.isArray(props.unit.payload.resources)
  ? props.unit.payload.resources as Array<{ id?: unknown; label?: unknown; uri?: unknown }>
  : [])
</script>

<template>
  <article
    class="message-card tool-card"
    :class="[`tool-${phase}`, status ? `tool-${status}` : '', `tool-category-${category}`]"
    :data-category="category"
    :data-presentation-kind="presentation"
    :data-call-id="callId"
    :data-status="status || undefined"
    data-testid="tool-block"
  >
    <button class="tool-summary" type="button" :aria-expanded="open" @click="open = !open">
      <span class="tool-icon" :title="category">{{ phase === 'result' ? '↳' : categoryGlyph }}</span>
      <span class="tool-title">
        <strong>{{ phase === 'result' ? 'Tool result' : 'Tool call' }} · {{ unit.payload.name }}</strong>
        <small><span class="tool-category-label">{{ categoryGlyph }} {{ category }}</span> · {{ presentation }}<template v-if="model"> · {{ model }}</template> · {{ callId }}<template v-if="duration !== null"> · {{ duration.toLocaleString() }} ms</template></small>
      </span>
      <span v-if="status" :class="['status', status]"><i />{{ status }}</span>
      <span class="chevron" :class="{ open }">⌄</span>
    </button>

    <div v-if="progress !== null" class="tool-progress" :aria-label="`Tool progress ${progress}%`"><i :style="{ width: `${progress}%` }" /></div>

    <div v-if="open" class="tool-detail">
      <div v-if="resources.length" class="tool-resources" data-testid="tool-resources">
        <span v-for="resource in resources" :key="String(resource.id ?? resource.uri)" class="tool-resource">{{ resource.label ?? resource.uri }}</span>
      </div>
      <div v-if="phase !== 'result'" class="tool-pane">
        <span class="tool-pane-label">input</span>
        <pre><code>{{ input }}</code></pre>
      </div>
      <div v-else class="tool-pane">
        <span class="tool-pane-label">output</span>
        <pre><code>{{ output }}</code></pre>
      </div>
    </div>
  </article>
</template>
