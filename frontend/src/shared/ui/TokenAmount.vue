<script setup lang="ts">
import { exactTokens } from '../token-format'
defineProps<{ value: number | null; missing?: string }>()
</script>
<template>
  <span class="token-amount">{{ value === null ? missing || '未上报' : exactTokens(value) }}<small v-if="value !== null"> tokens</small></span>
</template>
<style scoped>
.token-amount { font-variant-numeric:tabular-nums; white-space:nowrap; }
.token-amount small { color:var(--tuck-muted); font-size:var(--tuck-font-caption); }
</style>
