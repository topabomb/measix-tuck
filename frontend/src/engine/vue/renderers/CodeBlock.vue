<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import type { RenderUnit } from '../../presentation/render-unit'
import { useFoldState } from './fold-state'
import { highlightCode } from './highlight-client'

const props = defineProps<{ unit: RenderUnit }>()
const open = useFoldState(props.unit.id, Boolean(props.unit.payload.defaultOpen))
const highlighted = ref('')
let generation = 0

const language = computed(() => String(props.unit.payload.language ?? 'typescript'))
const fullCode = computed(() => String(props.unit.payload.code ?? ''))
const lineCount = computed(() => fullCode.value.split('\n').length)
const displayCode = computed(() => {
  if (open.value || lineCount.value <= 28) return fullCode.value
  return `${fullCode.value.split('\n').slice(0, 8).join('\n')}\n// … ${lineCount.value - 8} 行已折叠`
})

watch(
  () => [props.unit.revision, displayCode.value, language.value] as const,
  async () => {
    const current = ++generation
    highlighted.value = ''
    try {
      const html = await highlightCode(displayCode.value, language.value)
      if (current === generation) highlighted.value = html
    } catch {
      if (current === generation) highlighted.value = ''
    }
  },
  { immediate: true },
)

onBeforeUnmount(() => { generation += 1 })
</script>

<template>
  <article class="message-card code-card" data-testid="code-block">
    <header class="compact-kind-head">
      <span><span class="kind-dot code-dot" />{{ unit.payload.filename ?? 'code' }} · {{ language }}</span>
      <span class="head-actions">
        <small>{{ lineCount }} lines</small>
        <button v-if="lineCount > 28" class="fold-toggle" type="button" :aria-expanded="open" @click="open = !open"><svg viewBox="0 0 16 16" aria-hidden="true"><path :d="open ? 'm4 10 4-4 4 4' : 'm4 6 4 4 4-4'" /></svg><span>{{ open ? '收起' : '展开' }}</span></button>
      </span>
    </header>
    <div v-if="highlighted" class="shiki-host" v-html="highlighted" />
    <pre v-else><code>{{ displayCode }}</code></pre>
  </article>
</template>
