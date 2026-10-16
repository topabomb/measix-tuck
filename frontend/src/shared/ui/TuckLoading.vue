<script setup lang="ts">
import TuckMark from './TuckMark.vue'
withDefaults(defineProps<{ label?: string }>(), { label:'加载中…' })
</script>
<template><span class="tuck-loading" role="status"><TuckMark animated /><span>{{ label }}</span></span></template>
<style scoped>
.tuck-loading { display:inline-flex; align-items:center; gap:6px; min-height:20px; font-size:var(--tuck-font-caption,12px); line-height:20px; color:var(--tuck-muted); }
.tuck-mark { width:16px; height:16px; color:var(--tuck-accent); }
</style>
