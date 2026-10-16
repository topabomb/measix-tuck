<script setup lang="ts">
defineProps<{ expanded?: boolean }>()
</script>
<template>
  <svg class="sidebar-icon" viewBox="0 0 20 20" aria-hidden="true">
    <path v-if="expanded" class="sidebar-fill" d="M4.5 3.5h3v13h-3a2 2 0 0 1-2-2v-9a2 2 0 0 1 2-2Z" />
    <rect x="2.5" y="3.5" width="15" height="13" rx="2" />
    <path d="M7.5 3.5v13" />
  </svg>
</template>
<style scoped>
.sidebar-icon { display:block; width:18px; height:18px; flex:none; fill:none; stroke:currentColor; stroke-width:1.5; stroke-linecap:round; stroke-linejoin:round; }
.sidebar-fill { fill:currentColor; fill-opacity:.14; stroke:none; }
</style>
