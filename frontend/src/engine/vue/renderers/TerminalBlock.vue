<script setup lang="ts">
import { computed } from 'vue'
import type { RenderUnit } from '../../presentation/render-unit'
import { useFoldState } from './fold-state'

const props = defineProps<{ unit: RenderUnit }>()
const open = useFoldState(props.unit.id, props.unit.payload.defaultOpen !== false)
const command = computed(() => String(props.unit.payload.command ?? 'terminal'))
const output = computed(() => String(props.unit.payload.output ?? ''))
const status = computed(() => String(props.unit.payload.status ?? 'running'))
const callId = computed(() => String(props.unit.payload.callId ?? ''))
const exitCode = computed(() => props.unit.payload.exitCode === undefined ? null : Number(props.unit.payload.exitCode))
const cwd = computed(() => {
  const value = props.unit.payload.cwd as { label?: unknown; uri?: unknown } | undefined
  return value ? String(value.label ?? value.uri ?? '') : ''
})
</script>

<template>
  <article class="message-card terminal-card" :class="`terminal-${status}`" :data-call-id="callId" data-testid="terminal-block">
    <button class="terminal-head" type="button" :aria-expanded="open" @click="open = !open">
      <span class="terminal-glyph">&gt;_</span>
      <span class="terminal-title">
        <strong>{{ command }}</strong>
        <small><template v-if="cwd">{{ cwd }} · </template><template v-if="callId">{{ callId }}</template></small>
      </span>
      <span :class="['status', status]"><i />{{ status }}</span>
      <span v-if="exitCode !== null" class="terminal-exit">exit {{ exitCode }}</span>
      <span class="chevron" :class="{ open }">⌄</span>
    </button>
    <pre v-if="open" class="terminal-output"><code>{{ output }}</code></pre>
  </article>
</template>
