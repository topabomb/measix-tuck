<script setup lang="ts">
import { computed } from 'vue'
import DOMPurify from 'dompurify'
import type { RenderUnit } from '../../presentation/render-unit'

const props = defineProps<{ unit: RenderUnit }>()
const html = computed(() => DOMPurify.sanitize(String(props.unit.payload.html ?? ''), {
  USE_PROFILES: { html: true },
  FORBID_TAGS: ['script', 'style', 'iframe', 'object', 'embed'],
}))
</script>

<template>
  <article class="message-card html-card">
    <header class="compact-kind-head"><span class="kind-dot html-dot" />HTML artifact · sanitized</header>
    <div v-html="html" />
  </article>
</template>
