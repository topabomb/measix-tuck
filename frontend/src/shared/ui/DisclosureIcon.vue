<script setup lang="ts">
defineProps<{ open?: boolean }>()
</script>
<template><span class="disclosure-icon" aria-hidden="true"><svg viewBox="0 0 20 20" :class="{ open }"><path d="m7.5 5 5 5-5 5" /></svg></span></template>
<style scoped>
.disclosure-icon { display:inline-flex; align-items:center; justify-content:center; width:16px; height:20px; flex:none; color:var(--tuck-muted); }
svg { display:block; width:14px; height:14px; fill:none; stroke:currentColor; stroke-width:1.6; stroke-linecap:round; stroke-linejoin:round; }
svg.open { transform:rotate(90deg); }
</style>
