<script setup lang="ts">
import { computed } from 'vue'
import type { RenderUnit } from '../../presentation/render-unit'
import { useFoldState } from './fold-state'

const props = defineProps<{ unit: RenderUnit }>()
const open = useFoldState(props.unit.id, Boolean(props.unit.payload.defaultOpen))
const duration = computed(() => `${(Number(props.unit.payload.durationMs ?? 0) / 1000).toFixed(1)}s`)
const tokens = computed(() => Number(props.unit.payload.tokenCount ?? 0).toLocaleString())
const status = computed(() => String(props.unit.payload.status ?? (props.unit.payload.live ? 'streaming' : 'complete')))
</script>

<template>
  <article class="message-card thinking-card" :data-status="status" data-testid="thinking-block">
    <button class="disclosure-head" type="button" :aria-expanded="open" @click="open = !open">
      <span class="thinking-icon">✧</span>
      <span class="disclosure-copy">
        <strong>Thinking <em v-if="status === 'streaming'" class="reasoning-live">live</em></strong>
        <small>{{ duration }} · ~{{ tokens }} tokens · {{ status }}</small>
      </span>
      <span class="chevron" :class="{ open }">⌄</span>
    </button>
    <div v-if="open" class="thinking-body">
      <p v-for="(paragraph, index) in String(unit.payload.text ?? '').split('\n\n')" :key="index">{{ paragraph }}</p>
    </div>
  </article>
</template>
