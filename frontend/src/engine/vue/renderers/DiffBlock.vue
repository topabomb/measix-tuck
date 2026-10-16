<script setup lang="ts">
import { computed } from 'vue'
import type { RenderUnit } from '../../presentation/render-unit'
import { useFoldState } from './fold-state'

const props = defineProps<{ unit: RenderUnit }>()
const lines = computed(() => (props.unit.payload.lines as string[] | undefined) ?? [])
const open = useFoldState(props.unit.id, Boolean(props.unit.payload.defaultOpen))
const visibleLines = computed(() => open.value || lines.value.length <= 30 ? lines.value : lines.value.slice(0, 8))
</script>

<template>
  <article class="message-card diff-card" data-testid="diff-block">
    <header class="compact-kind-head">
      <span><span class="kind-dot diff-dot" />Diff · {{ unit.payload.file }}</span>
      <span class="head-actions">
        <small>{{ lines.length }} lines</small>
        <button v-if="lines.length > 30" class="fold-toggle" type="button" :aria-expanded="open" @click="open = !open"><svg viewBox="0 0 16 16" aria-hidden="true"><path :d="open ? 'm4 10 4-4 4 4' : 'm4 6 4 4 4-4'" /></svg><span>{{ open ? '收起' : '展开' }}</span></button>
      </span>
    </header>
    <pre><span v-for="(line, i) in visibleLines" :key="i" :class="line.startsWith('+') ? 'add' : line.startsWith('-') ? 'del' : ''">{{ line + '\n' }}</span><span v-if="!open && lines.length > visibleLines.length" class="diff-ellipsis">… {{ lines.length - visibleLines.length }} 行已折叠</span></pre>
  </article>
</template>
