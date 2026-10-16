<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import type { DockerTask } from '../../../shared/workspace/runtime'
import { compactTokens as formatTokens, exactTokens } from '../../../shared/token-format'
import TokenAmount from '../../../shared/ui/TokenAmount.vue'
import { hasReportedUsage } from '../session-presentation'
import SummaryPopover from '../../../shared/ui/SummaryPopover.vue'
const props = defineProps<{ task: DockerTask; connected?: boolean }>()
const open = ref<'activity' | 'tokens' | 'context' | null>(null)
const root = ref<HTMLElement | null>(null)
const trigger=ref<HTMLElement|null>(null)
watch(() => props.task.id, () => { open.value = null })
const total = computed(() => props.task.usage.inputTokens + props.task.usage.cacheReadTokens + props.task.usage.cacheWriteTokens + props.task.usage.outputTokens)
const usageKnown = computed(() => hasReportedUsage(props.task))
const contextKnown = computed(() => props.task.context.projectedTokens > 0 || props.task.context.reported === true)
const contextRatio = computed(() => contextKnown.value && props.task.context.contextWindow > 0 ? props.task.context.projectedTokens / props.task.context.contextWindow * 100 : null)
const contextPercent = computed(() => contextRatio.value === null ? null : Math.round(contextRatio.value))
const contextLabel = computed(() => contextKnown.value ? formatTokens(props.task.context.projectedTokens) : '待上报')
const compacting = computed(() => props.task.compaction?.status === 'running' && props.task.status === 'working')
const compactionLabel = computed(() => compacting.value && props.connected === false ? '压缩状态待同步' : ({ running:'正在压缩上下文', applied:'压缩完成', skipped:'已跳过压缩', failed:'压缩失败', interrupted:'压缩已中断', unknown:'压缩结果未确认' }[props.task.compaction?.status || 'unknown']))
const now = ref(Date.now())
const visible = ref(!document.hidden)
const turnActive = computed(() => !!props.task.turnTiming && props.task.turnTiming.endedAt == null && !props.task.turnTiming.incomplete && props.task.status !== 'idle')
const turnEnd = computed(() => {
  const timing = props.task.turnTiming
  return timing ? timing.endedAt ?? (turnActive.value && props.connected !== false ? now.value : timing.observedAt) : undefined
})
const turnMs = computed(() => props.task.turnTiming && turnEnd.value != null ? Math.max(0, turnEnd.value - props.task.turnTiming.startedAt) : undefined)
const turnWaitingMs = computed(() => {
  const timing = props.task.turnTiming
  return timing ? timing.waitingMs + (timing.waitingSince != null && turnEnd.value != null ? Math.max(0, turnEnd.value - timing.waitingSince) : 0) : 0
})
const turnLabel = computed(() => turnActive.value ? '本轮' : '上一轮')
const turnSync = computed(() => turnActive.value && props.connected === false)
const turnTimeText = computed(() => turnMs.value == null ? '未记录' : shortDuration(turnMs.value))
let clock: ReturnType<typeof setInterval> | undefined
watch(() => [compacting.value, turnActive.value, props.task.id, props.task.turnTiming?.turnId, props.connected, visible.value], () => {
  clearInterval(clock); clock = undefined; now.value = Date.now()
  if ((compacting.value || turnActive.value) && props.connected !== false && visible.value) clock = setInterval(() => { now.value = Date.now() }, 1000)
}, { immediate:true })
function visibilityChanged() { visible.value = !document.hidden }
onMounted(() => document.addEventListener('visibilitychange', visibilityChanged))
onBeforeUnmount(() => { clearInterval(clock); document.removeEventListener('visibilitychange', visibilityChanged) })
const compactionMs = computed(() => {
  const start = Date.parse(props.task.compaction?.startedAt || '')
  const end = compacting.value ? now.value : Date.parse(props.task.compaction?.completedAt || '')
  return Number.isFinite(start) && Number.isFinite(end) ? Math.max(0, end - start) : undefined
})
const elapsed = computed(() => {
  if (compactionMs.value === undefined) return ''
  const seconds = Math.floor(compactionMs.value / 1000)
  return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, '0')}`
})
const compactionNotice = computed(() => props.task.compaction && props.task.compaction.status !== 'applied' ? compactionLabel.value : '')
const inputTotal = computed(() => props.task.usage.inputTokens + props.task.usage.cacheReadTokens + props.task.usage.cacheWriteTokens)
const tps = computed(() => props.task.timings?.streamSamples && props.task.timings.streamMs! > 0 ? props.task.timings.streamTokens! * 1000 / props.task.timings.streamMs! : null)
const cacheRatio = computed(() => usageKnown.value && inputTotal.value > 0 ? `${Math.round(props.task.usage.cacheReadTokens / inputTotal.value * 1000) / 10}%` : '无法计算')
const activityLabel = computed(() => `${props.task.parentTaskId ? '' : `${props.task.turnCount} 轮 · `}${props.task.parentTaskId && !usageKnown.value ? '运行详情' : props.task.modelCalls == null ? '调用未上报' : `${props.task.modelCalls} 次调用`}${tps.value === null ? '' : ` · ≈${Math.round(tps.value)} tok/s`}`)
function duration(ms: number | undefined): string {
  if (ms == null) return '未采集'
  if (ms < 1000) return `${Math.round(ms)} ms`
  if (ms < 60000) return `${Math.round(ms / 100) / 10} 秒`
  return `${Math.floor(ms / 60000)} 分 ${Math.floor(ms % 60000 / 1000)} 秒`
}
function shortDuration(ms: number): string {
  const seconds = Math.floor(ms / 1000), minutes = Math.floor(seconds / 60)
  return minutes < 60 ? `${minutes}:${String(seconds % 60).padStart(2, '0')}` : `${Math.floor(minutes / 60)}:${String(minutes % 60).padStart(2, '0')}:${String(seconds % 60).padStart(2, '0')}`
}
function toggle(value: typeof open.value,event:MouseEvent) { trigger.value=event.currentTarget as HTMLElement;open.value = open.value === value ? null : value }
</script>
<template>
  <div ref="root" class="session-summary">
    <SummaryPopover :open="!!open" :trigger="trigger" above close-label="关闭摘要" :label="open === 'activity' ? '会话统计' : open === 'tokens' ? '累计用量' : '上下文详情'" @update:open="value=>{if(!value)open=null}">
      <template v-if="open === 'activity'">
        <dl class="turn-details"><dt>{{ turnLabel }}耗时</dt><dd data-testid="turn-duration">{{ turnMs == null ? '未记录' : duration(turnMs) }}<span v-if="turnSync" class="turn-note">待同步</span><span v-else-if="task.turnTiming?.incomplete" class="turn-note">中断 · 截至最后记录</span></dd><template v-if="turnWaitingMs > 0"><dt>其中等待确认</dt><dd data-testid="turn-waiting">{{ duration(turnWaitingMs) }}</dd></template></dl>
        <dl><dt v-if="!task.parentTaskId">对话轮次</dt><dd v-if="!task.parentTaskId">{{ task.turnCount }}</dd><dt>模型调用</dt><dd>{{ task.parentTaskId && !usageKnown ? '未上报' : task.modelCalls ?? '未上报' }}</dd>
          <dt>累计输出流耗时</dt><dd>{{ duration(task.timings?.streamMs) }}</dd><dt>流式 TPS（估算）</dt><dd>{{ tps === null ? '未采集' : `${Math.round(tps * 10) / 10} tok/s` }}</dd>
          <dt>工具累计耗时</dt><dd>{{ duration(task.timings?.toolMs) }}</dd>
        </dl>
      </template>
      <template v-else-if="open === 'tokens'">
        <dl><dt class="total">总计</dt><dd class="total"><TokenAmount :value="usageKnown ? total : null" /></dd>
          <dt>输入合计</dt><dd><TokenAmount :value="usageKnown ? inputTotal : null" /></dd>
          <dt class="sub-item">未缓存输入</dt><dd><TokenAmount :value="usageKnown ? task.usage.inputTokens : null" /></dd>
          <dt class="sub-item">缓存读取</dt><dd><TokenAmount :value="usageKnown ? task.usage.cacheReadTokens : null" /></dd>
          <dt class="sub-item">缓存写入</dt><dd><TokenAmount :value="usageKnown ? task.usage.cacheWriteTokens : null" /></dd>
          <dt class="section-start">输出</dt><dd class="section-start"><TokenAmount :value="usageKnown ? task.usage.outputTokens : null" /></dd>
          <template v-if="task.usage.reasoningTokens > 0"><dt class="sub-item">其中推理</dt><dd><TokenAmount :value="task.usage.reasoningTokens" /></dd></template>
          <dt>缓存读取占比</dt><dd>{{ cacheRatio }}</dd>
        </dl>
      </template>
      <template v-else><dl><dt>当前占用</dt><dd><TokenAmount :value="contextKnown ? task.context.projectedTokens : null" missing="未上报" /></dd><dt>配置窗口</dt><dd><TokenAmount :value="task.context.contextWindow > 0 ? task.context.contextWindow : null" missing="未配置" /></dd><dt>占用比例</dt><dd>{{ contextRatio === null ? '无法计算' : `${Math.round(contextRatio * 10) / 10}%` }}</dd><template v-if="task.compaction"><dt>最近压缩</dt><dd data-testid="compaction-result">{{ compactionLabel }}</dd><dt>{{ compacting ? '已用时' : '耗时' }}</dt><dd>{{ duration(compactionMs) }}</dd><template v-if="task.compaction.beforeTokens != null"><dt>压缩前占用</dt><dd><TokenAmount :value="task.compaction.beforeTokens" /></dd></template></template></dl></template>
    </SummaryPopover>
    <button :aria-label="activityLabel" :aria-expanded="open === 'activity'" @click="toggle('activity',$event)"><span v-if="task.turnTiming" class="turn-clock" data-testid="turn-clock" :title="turnSync ? '执行状态待同步' : task.turnTiming.incomplete ? '中断，截至最后记录' : `${turnLabel}总耗时`">{{ turnSync ? '待同步' : turnLabel }} <span class="turn-digits">{{ turnTimeText }}</span><span v-if="task.turnTiming.incomplete">*</span></span><span :class="{ 'summary-wide': task.turnTiming }"><template v-if="task.turnTiming"> · </template><template v-if="task.parentTaskId && !usageKnown">运行详情</template><template v-else><span class="summary-wide"><template v-if="!task.parentTaskId">{{ task.turnCount }} 轮 · </template>{{ task.modelCalls == null ? '调用未上报' : `${task.modelCalls} 次调用` }}</span><span class="summary-narrow">{{ task.modelCalls == null ? '统计' : `${task.modelCalls} 次` }}</span></template><span v-if="tps !== null" title="流式输出速度估算"> · ≈{{ Math.round(tps) }} tok/s</span></span></button>
    <button :aria-label="usageKnown ? `累计 ${formatTokens(total)} tokens` : '用量 待上报'" v-if="usageKnown || !task.parentTaskId" :aria-expanded="open === 'tokens'" @click="toggle('tokens',$event)"><span class="summary-wide">{{ usageKnown ? `累计 ${formatTokens(total)} tokens` : '用量 待上报' }}</span><span class="summary-narrow">{{ usageKnown ? `${formatTokens(total)} tok` : '用量待报' }}</span></button>
    <button v-if="task.compaction || !task.parentTaskId || task.context.projectedTokens > 0 || task.context.contextWindow > 0" :aria-label="compactionNotice || `上下文 ${contextPercent === null ? contextLabel : `${contextPercent}%`}`" class="context-summary" :class="{ compacting, 'compaction-warning': compactionNotice && !compacting }" :aria-expanded="open === 'context'" :title="compactionNotice || (contextPercent === null ? '查看上下文详情' : `${exactTokens(task.context.projectedTokens)} / ${exactTokens(task.context.contextWindow)} tokens`)" @click="toggle('context',$event)">
      <span v-if="compacting" class="compaction-spinner" :class="{ paused: connected === false }" aria-hidden="true" />
      <svg v-else-if="contextPercent !== null" class="context-ring" :class="{ warning: contextPercent >= 80, danger: contextPercent >= 95 }" viewBox="0 0 20 20" aria-hidden="true"><circle class="ring-track" cx="10" cy="10" r="8" /><circle class="ring-value" cx="10" cy="10" r="8" pathLength="100" :stroke-dasharray="`${Math.max(0, Math.min(100, contextPercent))} 100`" /></svg>
      <span v-if="compacting" data-testid="compaction-status">{{ connected === false ? '压缩待同步' : '压缩中' }} <span class="compaction-time" aria-hidden="true">{{ elapsed }}</span></span>
      <span v-else-if="compactionNotice" data-testid="compaction-status">{{ compactionNotice }}</span>
      <span v-else><span class="summary-wide">上下文 </span>{{ contextPercent === null ? contextLabel : `${contextPercent}%` }}</span>
    </button>
    <span v-if="task.compaction" class="status-announcement" role="status">{{ compactionLabel }}</span>
  </div>
</template>
<style scoped>
.session-summary { position:relative; display:flex; flex-wrap:nowrap; align-items:center; gap:8px; min-width:0; max-width:100%; font-size:var(--tuck-font-caption); }
.session-summary > button { min-width:0; border:0; background:transparent; padding:4px 6px; color:var(--tuck-muted); font-size:var(--tuck-font-caption); line-height:20px; white-space:nowrap; overflow:hidden; text-overflow:ellipsis; }
.session-summary > button[aria-expanded=true] { background:var(--tuck-hover); color:var(--tuck-text); }
.context-summary { display:inline-flex; align-items:center; gap:5px; }
.context-ring { width:16px; height:16px; flex:none; fill:none; stroke-width:2; color:var(--tuck-accent); }
.context-ring.warning { color:var(--tuck-warning); }.context-ring.danger { color:var(--tuck-danger); }
.ring-track { stroke:var(--tuck-border); }.ring-value { stroke:currentColor; transform:rotate(-90deg); transform-origin:10px 10px; }
dl { display:grid; align-items:baseline; grid-template-columns:minmax(0,1fr) auto; gap:var(--summary-row-gap) 12px; margin:0; }
dt { color:var(--tuck-muted); } dd { margin:0; text-align:right; font-variant-numeric:tabular-nums; white-space:nowrap; }
.sub-item { padding-left:12px; font-size:12px; }
.turn-details { padding-bottom:var(--summary-section-gap); margin-bottom:var(--summary-section-gap); border-bottom:1px solid var(--tuck-border); }
.turn-note { display:block; color:var(--tuck-muted); font-size:var(--tuck-font-caption); }
.turn-digits { display:inline-block; min-width:7ch; font-variant-numeric:tabular-nums; text-align:left; }
.total { border-bottom:1px solid var(--tuck-border); padding-bottom:10px; color:var(--tuck-text); font-weight:600; } dd.total { font-size:var(--tuck-font-title); }
.section-start { border-top:1px solid var(--tuck-border); padding-top:8px; }
.summary-narrow { display:none; }
@container composer (max-width:900px) {
  .summary-wide { display:none; }.summary-narrow { display:inline; }
  .session-summary { justify-content:flex-start; gap:6px; }
  .session-summary > button { padding:3px 2px; }
}
.context-summary.compacting { color:var(--tuck-accent); }
.context-summary.compaction-warning { color:var(--tuck-warning); }
.compaction-time { display:inline-block; min-width:3.2em; font-variant-numeric:tabular-nums; }
.compaction-spinner { box-sizing:border-box; width:14px; height:14px; flex:none; border:2px solid var(--tuck-border); border-top-color:var(--tuck-accent); border-radius:50%; animation:compaction-spin 1s linear infinite; }
.compaction-spinner.paused { animation:none; }
.status-announcement { position:absolute; width:1px; height:1px; overflow:hidden; clip-path:inset(50%); white-space:nowrap; }
@keyframes compaction-spin { to { transform:rotate(360deg); } }
@media(prefers-reduced-motion:reduce) { .compaction-spinner { animation:none; } }
</style>
