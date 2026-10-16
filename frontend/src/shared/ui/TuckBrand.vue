<script setup lang="ts">
import TuckMark from './TuckMark.vue'
</script>

<template>
  <span class="tuck-brand" aria-label="松子 Tuck"><TuckMark /><strong>Tuck</strong></span>
</template>
<style scoped>
.tuck-brand { display:inline-flex; align-items:center; gap:6px; vertical-align:middle; color:var(--tuck-accent); line-height:1; white-space:nowrap; }
.tuck-brand > .tuck-mark { width:1.65em; height:1.65em; } strong { font-weight:650; letter-spacing:-.025em; }
</style>
