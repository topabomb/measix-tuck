<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import type { DockerTask } from '../../../shared/workspace/runtime'
import { activityLabel } from '../../../shared/workspace/activity-label'
import TuckMark from '../../../shared/ui/TuckMark.vue'

const props = defineProps<{ task: DockerTask; connected: boolean; sending: boolean; stopping: boolean }>()
const emit = defineEmits<{ layout: [] }>()
const active = computed(() => props.task.status !== 'idle')
const shown = computed(() => active.value || props.sending || props.stopping)
// Capture the viewport intent before changing footer height. The existing
// ResizeObserver reconciles the measured layout; ticks never trigger layout.
watch(shown, () => emit('layout'), { flush: 'sync' })
const label = computed(() => {
  if (!props.connected) return '连接中断，正在同步…'
  if (props.stopping) return '正在停止…'
  if (props.task.pendingInteraction) return props.task.pendingInteraction.kind === 'question' ? '等待回答' : '等待确认'
  if (!active.value) return '正在发送…'
  return activityLabel(props.task)
})
const running = computed(() => shown.value && props.connected && !props.task.pendingInteraction)
const visible = ref(!document.hidden)
const now = ref(Date.now())
const startedAt = computed(() => props.task.activity?.startedAt ?? props.task.turnTiming?.startedAt)
const timed = computed(() => active.value && running.value && !props.stopping && startedAt.value != null)
const elapsed = computed(() => {
  const seconds = Math.max(0, Math.floor((now.value - (startedAt.value || now.value)) / 1000))
  return seconds < 60 ? `${seconds} 秒` : `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, '0')}`
})
let timer: ReturnType<typeof setInterval> | undefined
watch([timed, visible, startedAt], () => {
  clearInterval(timer); timer = undefined
  if (timed.value && visible.value) { now.value = Date.now(); timer = setInterval(() => { now.value = Date.now() }, 1000) }
}, { immediate: true })
const visibilityChanged = () => { visible.value = !document.hidden }
onMounted(() => document.addEventListener('visibilitychange', visibilityChanged))
onBeforeUnmount(() => { clearInterval(timer); document.removeEventListener('visibilitychange', visibilityChanged) })
</script>

<template>
  <div v-if="shown" class="run-feedback" data-testid="run-feedback" :data-phase="task.activity?.phase || 'processing'">
    <TuckMark class="run-feedback-icon" :animated="running && visible" />
    <span class="run-feedback-label" role="status" :title="label">{{ label }}</span>
    <span v-if="timed && now - (startedAt || now) >= 1000" class="run-feedback-time" data-testid="run-feedback-time" aria-hidden="true">{{ elapsed }}</span>
  </div>
</template>

<style scoped>
.run-feedback { display:flex; align-items:center; justify-content:flex-start; gap:4px; height:20px; min-width:0; color:var(--tuck-muted); font-size:var(--tuck-font-caption); line-height:1; }
.run-feedback > .run-feedback-icon { width:16px; height:16px; color:var(--tuck-accent); }
.run-feedback-label { min-width:0; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
.run-feedback-time { flex:none; font-variant-numeric:tabular-nums; }
</style>
