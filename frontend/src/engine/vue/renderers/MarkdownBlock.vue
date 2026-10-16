<script setup lang="ts">
import { computed } from 'vue'
import type { RenderUnit } from '../../presentation/render-unit'
import { renderMarkdownCached } from './markdown-cache'

const props = defineProps<{ unit: RenderUnit; assistantName?: string }>()
const isFirst = computed(() => Number(props.unit.payload.partIndex ?? 0) === 0)
const isLast = computed(() => props.unit.payload.hasNextPart !== true)
const role = computed(() => String(props.unit.payload.role ?? 'assistant'))
const live = computed(() => props.unit.payload.live === true)
const html = computed(() => renderMarkdownCached(props.unit.id, props.unit.revision, String(props.unit.payload.markdown ?? '')))
</script>

<template>
  <article
    class="message-card markdown-card"
    :class="[`role-${role}`, { continuation: !isFirst, 'has-next-part': !isLast, live }]"
    :data-live-unit="live ? 'true' : undefined"
    data-testid="markdown-block"
  >
    <header v-if="isFirst" class="message-author">
      <span class="author-mark">{{ role === 'user' ? 'U' : '✦' }}</span>
      <span>{{ role === 'user' ? 'You' : assistantName || 'Agent' }}</span>
      <span v-if="live" class="streaming-label"><i /> streaming</span>
      <small>message {{ unit.messageIndex.toLocaleString() }}</small>
    </header>
    <div class="markdown-body" v-html="html" />
  </article>
</template>
