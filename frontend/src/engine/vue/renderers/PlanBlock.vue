<script setup lang="ts">
import { computed } from 'vue'
import type { RenderUnit } from '../../presentation/render-unit'

const props = defineProps<{ unit: RenderUnit }>()
const items = computed(() => Array.isArray(props.unit.payload.items) ? props.unit.payload.items as Array<{ id?: string; text?: string; status?: string }> : [])
const title = computed(() => String(props.unit.payload.title ?? 'Plan'))
const glyph = (status?: string) => ({
  pending: '○',
  'in-progress': '◐',
  completed: '✓',
  blocked: '!',
  cancelled: '×',
}[status ?? 'pending'] ?? '○')
</script>

<template>
  <article class="message-card plan-card" data-testid="plan-block">
    <header class="plan-head">
      <span class="plan-glyph">☷</span>
      <strong>{{ title }}</strong>
      <small>{{ items.filter(item => item.status === 'completed').length }}/{{ items.length }}</small>
    </header>
    <ol class="plan-items">
      <li v-for="item in items" :key="item.id ?? item.text" :class="`plan-${item.status ?? 'pending'}`" :data-plan-status="item.status ?? 'pending'">
        <span class="plan-status">{{ glyph(item.status) }}</span>
        <span>{{ item.text }}</span>
      </li>
    </ol>
  </article>
</template>
