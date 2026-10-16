<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, useId, watch } from 'vue'
import type { DockerTask, DockerWorkspaceRuntime } from '../../shared/workspace/runtime'
import { activityLabel } from '../../shared/workspace/activity-label'
import TuckMark from '../../shared/ui/TuckMark.vue'
import { authFetch } from '../../app/auth'
import { compactTokens, exactTokens, hasReportedUsage } from '../../shared/token-format'
import { taskIsActive, taskDuration } from './task-view'
const props = defineProps<{task: DockerTask; connected: boolean; workspace: DockerWorkspaceRuntime}>()
const previewNoteId = useId()
const now = ref(Date.now()), visible = ref(!document.hidden)
const active = computed(() => taskIsActive(props.task))
const ticking = computed(() => active.value && props.connected && visible.value && !!props.task.turnTiming && !props.task.turnTiming.endedAt && !props.task.turnTiming.incomplete)
const phaseStart = computed(() => props.task.activity?.startedAt ?? props.task.turnTiming?.startedAt)
const phaseTimed = computed(() => active.value && props.connected && !props.task.pendingInteraction && phaseStart.value != null)
const phaseElapsed = computed(() => taskDuration(now.value-(phaseStart.value ?? now.value)))
// Reuse the live canonical message and its existing SSE deltas. A cold visit
// prepares only the active record; no polling or tool-history transcript.
const previewRevision = ref(0)
const unsubscribe = props.workspace.subscribe(() => { if (active.value) previewRevision.value++ })
watch([() => props.task.id,() => props.task.activeAssistantIndex,active], async ([id,index,isActive]) => {
  if (isActive && index != null) {
    try { await props.workspace.prepareHistory(id,index,1); previewRevision.value++ } catch { /* Activity remains visible while history reconnects. */ }
  }
}, {immediate:true})
const livePreview = computed(() => {
  void previewRevision.value
  if (!active.value || props.task.pendingInteraction || props.task.activeAssistantIndex == null) return ''
  const phase = props.task.activity?.phase
  if (phase !== 'responding' && phase !== 'reasoning') return ''
  try {
    const message = props.workspace.kernelFor(props.task.id).getMessage(props.task.activeAssistantIndex)
    if (message.role !== 'assistant') return ''
    const block = [...message.blocks].reverse().find(block => block.type === (phase === 'reasoning' ? 'reasoning' : 'markdown'))
    const data = block?.data as {text?:string;markdown?:string} | undefined
    return (data?.text || data?.markdown || '').slice(-200).replace(/\s+/g,' ').trim()
  } catch { return '' }
})
const label = computed(() => !props.connected ? '连接中断，正在同步…' : props.task.pendingInteraction ? '' : activityLabel(props.task))
const stats = ref<{previousMs:number; previousTurns:number; incomplete:boolean; toolCalls:number}>()
const statsError = ref(false)
let request: AbortController | undefined
watch([() => props.task.id,() => props.task.turnTiming?.turnId,() => props.task.logicalCount,() => props.connected], async (value, previous) => {
  if (!previous || value[0] !== previous[0] || value[1] !== previous[1]) stats.value = undefined
  statsError.value = false
  request?.abort(); const current = request = new AbortController()
  try {
    const response = await authFetch(`/api/tasks/${props.task.id}/statistics`,{signal:current.signal})
    if (!response.ok) throw new Error('statistics unavailable')
    const value = await response.json()
    if (!current.signal.aborted) stats.value = value
  } catch { if (!current.signal.aborted) {stats.value = undefined; statsError.value = true} }
}, {immediate:true})
const date = computed(() => {
  const value = Date.parse(props.task.createdAt)
  return Number.isFinite(value) ? new Date(value).toLocaleString('zh-CN',{month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit',hour12:false}) : ''
})
const elapsed = computed(() => {
  const timing = props.task.turnTiming
  if (!stats.value) return statsError.value ? '读取失败' : '读取中…'
  if (!timing && !stats.value.previousTurns) return '未记录'
  const end = timing ? timing.endedAt ?? (ticking.value ? now.value : timing.observedAt) : 0
  const value = taskDuration(stats.value.previousMs + (timing ? Math.max(0,end-timing.startedAt) : 0))
  const partial = stats.value.incomplete || timing?.incomplete || stats.value.previousTurns + (timing ? 1 : 0) < (props.task.turnCount || 0)
  return partial ? `≥ ${value}` : value
})
const tokens = computed(() => hasReportedUsage(props.task) ? props.task.usage.inputTokens + props.task.usage.cacheReadTokens + props.task.usage.cacheWriteTokens + props.task.usage.outputTokens : null)
let timer: ReturnType<typeof setInterval> | undefined
watch([ticking,phaseTimed,visible,phaseStart], () => {
  clearInterval(timer); timer = undefined
  if (visible.value && (ticking.value || phaseTimed.value)) { now.value = Date.now(); timer = setInterval(() => now.value = Date.now(),1000) }
}, {immediate:true})
const visibilityChanged = () => {visible.value = !document.hidden}
onMounted(() => document.addEventListener('visibilitychange',visibilityChanged))
onBeforeUnmount(() => {unsubscribe();request?.abort();clearInterval(timer);document.removeEventListener('visibilitychange',visibilityChanged)})
</script>
<template>
  <div class="task-run-meta" aria-label="执行概况">
    <span v-if="active && label" class="task-run-activity"><TuckMark :animated="connected && visible && !task.pendingInteraction" /><span role="status">{{ label }}</span><span v-if="phaseTimed" class="task-phase-time" aria-label="当前阶段用时">{{ phaseElapsed }}</span><span v-if="livePreview" class="task-live-preview" role="note" :aria-label="task.activity?.phase === 'reasoning' ? '实时过程预览：思考' : '实时过程预览：回复'" :aria-describedby="previewNoteId"><span>{{ livePreview }}</span></span></span>
    <span v-if="livePreview" :id="previewNoteId" hidden>过程片段，非操作指令或最终结果。</span>
    <span v-if="date">开始 {{ date }}</span>
    <span v-if="!statsError" class="task-elapsed" title="各轮用时之和，含等待，不含轮间空闲；≥ 表示记录不完整">用时 {{ elapsed }}</span>
    <span v-if="task.turnCount">{{ task.turnCount }} 轮</span>
    <span v-if="!statsError">工具 {{ stats ? `${stats.toolCalls} 次` : '读取中…' }}</span>
    <span v-else title="用时和工具调用统计暂不可用">统计读取失败</span>
    <span :title="`总 Token ${exactTokens(tokens)}`">总 Token {{ compactTokens(tokens) }}</span>
  </div>
</template>
