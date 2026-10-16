<script setup lang="ts">
import { computed } from 'vue'
import type { RenderUnit } from '../../presentation/render-unit'

const props = defineProps<{ unit: RenderUnit; assistantName?: string }>()
const role = computed(() => String(props.unit.payload.role ?? 'assistant'))
</script>

<template>
  <article class="message-card text-card" :class="`role-${role}`">
    <header class="message-author">
      <span class="author-mark">{{ role === 'user' ? 'U' : '✦' }}</span>
      <span>{{ role === 'user' ? 'You' : assistantName || 'Agent' }}</span>
      <small>message {{ unit.messageIndex.toLocaleString() }}</small>
    </header>
    <p>{{ unit.payload.text }}</p>
  </article>
</template>
