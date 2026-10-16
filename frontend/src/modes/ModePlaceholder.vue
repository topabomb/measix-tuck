<script setup lang="ts">
defineProps<{ title: string; error?: string }>()
</script>
<template><section class="mode-placeholder" data-testid="mode-placeholder"><header>{{ title }}</header><main><h1>{{ title }}</h1><p>{{ error || '此工作模式尚未开放。' }}</p><p v-if="!error">可以继续使用对话，已有执行会继续进行。</p></main></section></template>
<style scoped>
.mode-placeholder { height:100%; display:flex; flex-direction:column; min-width:0; }header { display:flex; align-items:center; height:48px; padding:0 16px; border-bottom:1px solid var(--tuck-border); font-weight:600; box-sizing:border-box; }main { flex:1; display:flex; flex-direction:column; justify-content:center; align-items:center; padding:24px; text-align:center; }h1 { font-size:20px; margin:0 0 12px; }p { color:var(--tuck-muted); margin:4px 0; font-size:var(--tuck-font-ui); }
</style>
