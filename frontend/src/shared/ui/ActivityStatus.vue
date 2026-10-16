<script setup lang="ts">
import { computed } from 'vue'
const props = defineProps<{ status: string; label?: string }>()
const category = computed(() => {
  if (['success','complete','completed'].includes(props.status)) return 'success'
  if (['error','failed'].includes(props.status)) return 'error'
  if (['working','running','streaming','in-progress'].includes(props.status)) return 'running'
  if (['interrupted','cancelled'].includes(props.status)) return 'stopped'
  if (['pending','queued','waiting','blocked','max-tokens'].includes(props.status)) return 'waiting'
  return 'idle'
})
const title = computed(() => props.label || ({ success:'完成', error:'失败', running:'执行中', stopped:'已停止', waiting:'等待处理', idle:'空闲' }[category.value]))
</script>
<template>
  <span v-if="status" class="activity-status" :class="category" role="img" :aria-label="title" :title="title">
    <svg viewBox="0 0 20 20" aria-hidden="true"><path v-if="category === 'success'" d="m4 10 4 4 8-8" /><template v-else-if="category === 'error'"><circle cx="10" cy="10" r="7" /><path d="m7.5 7.5 5 5m0-5-5 5" /></template><rect v-else-if="category === 'stopped'" x="5" y="5" width="10" height="10" rx="1" /><template v-else><circle cx="10" cy="10" r="7" /><path v-if="category !== 'idle'" d="M10 5.5V10l3 2" /></template></svg>
  </span>
</template>
<style scoped>
.activity-status { display:inline-flex; align-items:center; justify-content:center; flex:none; width:16px; height:20px; color:var(--tuck-muted); }
svg { display:block; width:14px; height:14px; fill:none; stroke:currentColor; stroke-width:1.6; stroke-linecap:round; stroke-linejoin:round; }
.success { color:var(--tuck-success); } .error { color:var(--tuck-danger); } .running { color:var(--tuck-accent); } .waiting { color:var(--tuck-warning); }
</style>
