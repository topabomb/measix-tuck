<script setup lang="ts">
import type { RenderUnit } from '../../presentation/render-unit'
defineProps<{ unit: RenderUnit }>()
</script>

<template>
  <article class="message-card unknown-card" data-testid="unknown-block">
    <header class="compact-kind-head"><span class="kind-dot" />Unsupported renderer · {{ unit.kind }}</header>
    <pre>{{ JSON.stringify(unit.payload, null, 2) }}</pre>
  </article>
</template>
