<script setup lang="ts">
import { useRoute } from 'vue-router'
import { conversationPath } from '../../../app/router'
import { taskName } from "../../../../shared/task-channel.mjs"
import { computed, nextTick, onBeforeUnmount, ref, useId, watch } from 'vue'
import type { DockerTask } from '../../../shared/workspace/runtime'
import { deriveSessionIndicator } from '../../../engine/conversation/session-semantics'
import { compactTokens as formatTokens, exactTokens } from '../../../shared/token-format'
import { sessionFamily, taskStatus, hasReportedUsage } from '../session-presentation'
import { activityLabel } from '../../../shared/workspace/activity-label'

const props = defineProps<{ tasks: readonly DockerTask[]; activeId: string }>()
const emit = defineEmits<{ navigate: [id: string] }>()
const route = useRoute()
const family = computed(() => sessionFamily(props.tasks, props.activeId))
const active = computed(() => props.tasks.find(task => task.id === props.activeId))
const parent = computed(() => family.value.ancestry.at(-2))
const children = computed(() => family.value.rows.filter(row => row.depth > 0))
const pendingRuns = computed(() => family.value.rows.flatMap(row => row.task.backgroundRuns || []))
const childCount = computed(() => children.value.length + pendingRuns.value.length)
type BackgroundRun = NonNullable<DockerTask['backgroundRuns']>[number]
function runState(task: DockerTask, run: BackgroundRun) {
  // Old running reports are not evidence of execution after the parent has stopped.
  return run.status === 'running' && (task.status === 'idle'||run.turnId!==task.turnId) ? 'unknown' : run.status || 'unknown'
}
const labels = { blocked:'待处理', working:'执行', completed:'完成', failed:'异常', interrupted:'停止', idle:'空闲', unknown:'待同步' }
const counts = computed(() => {
  const result = { blocked:0, working:0, completed:0, failed:0, interrupted:0, idle:0, unknown:0 }
  for (const {task} of children.value) {
    const state = deriveSessionIndicator(task)
    result[state === 'max-tokens' ? 'failed' : state]++
  }
  for (const {task} of family.value.rows) for (const run of task.backgroundRuns || []) {
    const state = runState(task,run)
    result[state === 'running' ? 'working' : state === 'stopped' ? 'interrupted' : state]++
  }
  return result
})
const summary = computed(() => Object.entries(counts.value).filter(([,count]) => count).map(([state,count]) => `${labels[state as keyof typeof labels]} ${count}`).join(' · '))
function backgroundLabel(task: DockerTask, run: BackgroundRun) {
  return { running:'执行中', completed:'已完成', failed:'失败', stopped:'已停止', unknown:'状态待同步' }[runState(task,run)]
}
const trigger = ref<HTMLButtonElement | null>(null)
const popup = ref<HTMLElement | null>(null)
const search = ref(''), page = ref(0), open = ref(false)
const popupId = useId()
const searchable = computed(() => family.value.rows.length + pendingRuns.value.length > 8)
const pageSize = 30
const matches = computed(() => {
  const query = search.value.trim().toLocaleLowerCase()
  const rows: { task: DockerTask; depth: number; run?: NonNullable<DockerTask['backgroundRuns']>[number] }[] = family.value.rows.flatMap(row => [row, ...(row.task.backgroundRuns || []).map(run => ({ ...row, run }))])
  return rows.filter(({ task, run }) => !query || (run ? `${run.title} ${run.agent} 后台 已启动 ${backgroundLabel(task,run)}` : `${taskName(task.title)} ${task.agent} ${taskStatus(task)}`).toLocaleLowerCase().includes(query))
})
const visible = computed(() => matches.value.slice(page.value * pageSize, (page.value + 1) * pageSize))
watch(search, () => { page.value = 0 })
watch(() => matches.value.length, size => { page.value = Math.min(page.value, Math.max(0, Math.ceil(size / pageSize) - 1)) })
watch(() => props.activeId, close)
function close() { popup.value?.hidePopover() }
function positionPopup() {
  if (!trigger.value || !popup.value) return
  const rect = trigger.value!.getBoundingClientRect()
  const width = Math.min(420, innerWidth - 24)
  popup.value!.style.left = `${Math.max(12, Math.min(rect.left, innerWidth - width - 12))}px`
  popup.value!.style.top = `${rect.bottom + 8}px`
}
function toggle() {
  if (open.value) { close(); return }
  positionPopup()
  search.value = ''; page.value = 0
  popup.value!.showPopover()
}
async function toggled(event: Event) {
  open.value = (event as ToggleEvent).newState === 'open'
  if (open.value) {
    await nextTick()
    popup.value?.querySelector<HTMLElement>('input, [aria-current="page"]')?.focus({ preventScroll: true })
  }
}
function navigate(id: string) { close(); if (id !== props.activeId) emit('navigate', id) }
function arrow(event: KeyboardEvent) {
  if (!['ArrowDown','ArrowUp'].includes(event.key)) return
  const buttons = [...(popup.value?.querySelectorAll<HTMLButtonElement>('[data-session-choice]') || [])]
  if (!buttons.length) return
  event.preventDefault()
  const index = buttons.indexOf(document.activeElement as HTMLButtonElement)
  const next = index < 0 ? (event.key === 'ArrowDown' ? 0 : buttons.length - 1) : (index + (event.key === 'ArrowDown' ? 1 : -1) + buttons.length) % buttons.length
  buttons[next]?.focus()
}
function tokenTotal(task: DockerTask) { return task.usage.inputTokens + task.usage.outputTokens + task.usage.cacheReadTokens + task.usage.cacheWriteTokens }
function tokens(task: DockerTask) { return formatTokens(tokenTotal(task)) }
window.addEventListener('resize', positionPopup)
onBeforeUnmount(() => { close(); window.removeEventListener('resize', positionPopup) })
</script>

<template>
  <nav class="session-navigation" :class="{ 'is-child': parent }" aria-label="会话层级" data-testid="session-navigation">
    <RouterLink v-if="parent" class="back-link parent-link" :to="{path:conversationPath(parent.id),query:route.query}" data-testid="parent-session-link" :title="`返回 ${taskName(parent.title)}`" :aria-label="`返回父会话：${taskName(parent.title)}`" @click="close">← <span>{{ taskName(parent.title) }}</span></RouterLink>
    <span v-if="parent" class="separator" aria-hidden="true">/</span>
    <strong v-if="!parent" class="root-title" :title="taskName(active?.title || '')">{{ taskName(active?.title || '') }}</strong>
    <button v-if="childCount || parent" ref="trigger" class="agent-switch" data-testid="agent-switch" :aria-label="`${parent ? `切换子代理：${taskName(active?.title || '')}` : `查看或切换 ${childCount} 个子代理`}，${summary}`" :aria-expanded="open" :aria-controls="popupId" aria-haspopup="dialog" :title="`${open ? '收起会话列表' : '查看或切换子代理会话'} · ${summary}`" @click="toggle">
      <span>{{ parent ? taskName(active?.title || '') : '子代理' }}</span><span v-if="!parent" class="agent-count">{{ childCount }}</span><span class="agent-count-summary" :class="{'needs-attention':counts.blocked || counts.failed}"><span class="counts-full">{{ summary }}</span><span class="counts-compact">{{ summary.split(' · ')[0] }}</span></span><svg class="switch-chevron" viewBox="0 0 16 16" aria-hidden="true"><path d="m4 6 4 4 4-4" /></svg>
    </button>
    <Teleport to="body">
      <section :id="popupId" ref="popup" popover="auto" class="agent-popover" role="dialog" aria-label="子代理会话" data-testid="agent-popover" @toggle="toggled" @keydown="arrow">
        <template v-if="open">
          <header><strong>会话切换</strong><span>{{ childCount }} 个子代理 · {{ summary }}</span><button aria-label="关闭子代理列表" @click="close">×</button></header>
          <input v-if="searchable" v-model="search" aria-label="搜索子代理" placeholder="搜索名称、Agent 或状态…" />
          <div class="agent-choices">
            <template v-for="{task, depth, run} in visible" :key="run?.id || task.id">
            <div v-if="run" class="pending-agent" data-testid="pending-background-agent">
              <div class="pending-heading"><strong>{{ run.agent }} · 后台</strong><span class="background-status" :data-status="runState(task,run)">{{ backgroundLabel(task,run) }}</span></div>
              <span :title="run.title">{{ run.title }}</span>
              <small v-if="run.summary" class="background-progress" aria-label="子代理最近输出，仅供进展参考" :title="run.summary">{{ run.summary }}</small>
              <small :title="run.observedAt ? `最近报告：${new Date(run.observedAt).toLocaleString()}` : undefined">{{ run.observedAt ? '最近报告 · 历史待同步' : '已启动 · 尚无进展报告' }}</small>
            </div>
            <button v-else data-session-choice :data-testid="`agent-choice-${task.id}`" :aria-current="task.id === activeId ? 'page' : undefined" :style="{ '--depth': Math.min(depth, 3) }" @click="navigate(task.id)">
              <i class="agent-status" :data-status="deriveSessionIndicator(task)" />
              <span class="agent-copy"><strong :title="taskName(task.title)">{{ depth ? taskName(task.title) : '主会话' }}</strong><small>{{ depth ? `${task.agent} · ${task.mode === 'background' ? '后台' : '前台'}` : taskName(task.title) }}</small><small :class="{ 'needs-attention': task.pendingInteraction, failure: deriveSessionIndicator(task) === 'failed' }">{{ taskStatus(task) }} · {{ task.logicalCount.toLocaleString() }} 条消息<span v-if="depth > 1"> · 第 {{ depth }} 层</span></small><small v-if="depth && task.status === 'working' && !task.pendingInteraction" class="child-progress" aria-label="子代理当前进展">{{ activityLabel(task) }}</small><small v-if="task.lastFailure" class="failure" :title="task.lastFailure.message">{{ task.lastFailure.message }}</small></span>
              <span class="agent-meta"><span class="choice-action" :class="{ current: task.id === activeId }">{{ task.id === activeId ? '✓ 当前会话' : '查看 →' }}</span><span :title="hasReportedUsage(task) ? `累计 ${exactTokens(tokenTotal(task))} tokens` : '用量未上报'">{{ hasReportedUsage(task) ? `累计 ${tokens(task)} tokens` : '用量未提供' }}</span></span>
            </button>
            </template>
            <p v-if="!visible.length" class="no-results">没有匹配的子代理</p>
          </div>
          <footer v-if="matches.length > pageSize"><button :disabled="!page" @click="page--">上一页</button><span>{{ page + 1 }} / {{ Math.ceil(matches.length / pageSize) }}</span><button :disabled="(page + 1) * pageSize >= matches.length" @click="page++">下一页</button></footer>
        </template>
      </section>
    </Teleport>
  </nav>
</template>

<style scoped>
.session-navigation { display:flex; align-items:center; gap:6px; min-width:0; width:100%; }
.session-navigation .root-title { flex:0 1 auto; min-width:0; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
.session-navigation > .agent-switch { border:0; background:transparent; color:var(--tuck-text); font-size:var(--tuck-font-ui); min-width:0; padding:5px 6px; display:flex; align-items:center; gap:6px; }
.session-navigation > .agent-switch { height:30px; padding:0 8px; border:1px solid var(--tuck-border); border-radius:7px; background:var(--tuck-surface); line-height:1; }
.session-navigation > .agent-switch[aria-expanded=true] { background:var(--tuck-hover); border-color:var(--tuck-muted); }
.agent-count { min-width:16px; height:18px; display:inline-flex; align-items:center; justify-content:center; color:var(--tuck-muted); font-size:var(--tuck-font-caption); font-variant-numeric:tabular-nums; }
.agent-count-summary { min-width:0; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; color:var(--tuck-muted); font-size:var(--tuck-font-caption); }
.agent-count-summary.needs-attention { color:var(--tuck-warning); }
.counts-compact { display:none; }
.switch-chevron { width:14px; height:14px; flex:none; display:block; fill:none; stroke:currentColor; stroke-width:1.6; stroke-linecap:round; stroke-linejoin:round; }
.agent-switch[aria-expanded=true] .switch-chevron { transform:rotate(180deg); }
.session-navigation > button:hover { background:var(--tuck-hover); }
.parent-link { flex:0 1 auto; min-width:0; max-width:40%; } .parent-link span, .agent-switch > span:first-child { overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
.agent-switch { flex:0 1 auto; max-width:60%; } .separator { color:var(--tuck-muted); }
.session-navigation:not(.is-child) > .agent-switch { flex:0 1 auto; max-width:75%; }
.session-navigation > .agent-switch > span:first-child { flex-shrink:0; }
.session-navigation.is-child > .agent-switch > span:first-child { flex-shrink:1; }
.agent-count { flex-shrink:0; }
.agent-switch i, .agent-status { width:6px; height:6px; flex-shrink:0; border-radius:50%; background:var(--tuck-muted); }
.agent-switch i.attention { background:var(--tuck-warning); } .agent-switch i.running { background:var(--tuck-success); }
.agent-popover { position:fixed; margin:0; width:min(420px,calc(100vw - 24px)); max-height:min(60dvh,460px,calc(100dvh - 76px)); padding:var(--tuck-dialog-padding); box-sizing:border-box; border:1px solid var(--tuck-border); border-radius:var(--tuck-dialog-radius); background:var(--tuck-surface); color:var(--tuck-text); box-shadow:0 8px 28px #0004; overflow:auto; font:var(--tuck-font-ui)/var(--tuck-line-ui) var(--tuck-font-family); color-scheme:var(--tuck-scheme); }
.agent-popover:popover-open { display:flex; flex-direction:column; gap:8px; }
.agent-popover * { box-sizing:border-box; }
.agent-popover button, .agent-popover input { font:inherit; color:inherit; border:1px solid var(--tuck-border); border-radius:6px; background:var(--tuck-background); padding:6px 8px; }
.agent-popover button { cursor:pointer; } .agent-popover button:disabled { opacity:.45; cursor:default; }
.agent-popover button:hover:not(:disabled) { background:var(--tuck-hover); }
.agent-popover header, .agent-popover footer { display:flex; gap:8px; align-items:center; flex-shrink:0; }
.agent-popover header > span { flex:1; color:var(--tuck-muted); font-size:var(--tuck-font-caption); }
.agent-popover header button { border:0; background:none; font-size:18px; line-height:1; }
.agent-popover input { width:100%; flex-shrink:0; }
.agent-choices { min-height:0; overflow:auto; overscroll-behavior:contain; }
.agent-choices > button { display:flex; width:100%; align-items:center; gap:9px; padding:9px 8px 9px calc(8px + var(--depth)*8px); text-align:left; border:1px solid transparent; background:transparent; }
.agent-choices > button[aria-current=page] { border-color:var(--tuck-border); background:var(--tuck-raised); }
.agent-copy { display:grid; flex:1; min-width:0; gap:2px; } .agent-copy strong,.agent-copy small { overflow:hidden; white-space:nowrap; text-overflow:ellipsis; }
.agent-copy small,.agent-meta { color:var(--tuck-muted); font-size:var(--tuck-font-caption); }
.pending-agent { display:grid; gap:4px; padding:9px 8px; border-bottom:1px solid var(--tuck-border); font-size:var(--tuck-font-ui); }
.pending-agent span { overflow:hidden; white-space:nowrap; text-overflow:ellipsis; }
.pending-agent small { color:var(--tuck-muted); font-size:var(--tuck-font-caption); }
.pending-heading { display:flex; align-items:center; gap:8px; justify-content:space-between; }
.pending-heading strong { min-width:0; overflow:hidden; white-space:nowrap; text-overflow:ellipsis; }
.background-status { flex:none; font-size:var(--tuck-font-caption); color:var(--tuck-muted); }
.background-status[data-status=running] { color:var(--tuck-accent); }
.background-status[data-status=completed] { color:var(--tuck-success); }
.background-status[data-status=failed] { color:var(--tuck-danger); }
.pending-agent .background-progress { overflow:hidden; white-space:nowrap; text-overflow:ellipsis; }
.agent-meta { display:grid; gap:4px; text-align:right; flex-shrink:0; }
.choice-action { color:var(--tuck-text); font-size:var(--tuck-font-caption); }.choice-action.current { color:var(--tuck-accent); }
.agent-copy .needs-attention { color:var(--tuck-warning); } .agent-copy .failure { color:var(--tuck-danger); }
.agent-status[data-status=working],.agent-status[data-status=completed] { background:var(--tuck-success); }
.agent-status[data-status=blocked] { background:var(--tuck-warning); }
.agent-status[data-status=failed],.agent-status[data-status=interrupted] { background:var(--tuck-danger); }
.agent-popover footer { justify-content:space-between; } .no-results { color:var(--tuck-muted); text-align:center; }
@container chat-column (max-width:760px) {
  .session-navigation .parent-link { flex:0 0 20px; }
  .parent-link span,.separator,.counts-full { display:none; }
  .counts-compact { display:inline; }
  .session-navigation:has(.agent-switch) .root-title { display:none; }
  .session-navigation > .agent-switch,.session-navigation:not(.is-child) > .agent-switch { max-width:100%; flex:0 1 auto; }
  .session-navigation.is-child .agent-count-summary { display:none; }
}
</style>
