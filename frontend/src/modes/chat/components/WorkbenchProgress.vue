<script setup lang="ts">
import { computed } from 'vue'
import type { AgentRunRef, PlanItem } from '../../../engine/model/conversation'
import type { RenderUnit } from '../../../engine/presentation/render-unit'
import { useFoldState } from '../../../engine/vue/renderers/fold-state'
import ActivityStatus from '../../../shared/ui/ActivityStatus.vue'
import DisclosureIcon from '../../../shared/ui/DisclosureIcon.vue'

const props = defineProps<{ unit: RenderUnit }>()
const open = useFoldState(props.unit.id, false)
const delegated = computed(() => props.unit.kind === 'delegation')
const items = computed(() => (props.unit.payload.items || []) as PlanItem[])
const runs = computed(() => (props.unit.payload.runs || []) as AgentRunRef[])
const completed = computed(() => (delegated.value ? runs.value : items.value).filter(item => item.status === 'completed').length)
const total = computed(() => delegated.value ? runs.value.length : items.value.length)
const statuses: Record<string,string> = { queued:'排队', pending:'待处理', running:'执行中', 'in-progress':'执行中', waiting:'等待处理', blocked:'等待处理', completed:'已完成', failed:'失败', interrupted:'已停止', cancelled:'已取消' }
const summary = computed(() => {
  const active = (delegated.value ? runs.value : items.value).filter(item => ['running','in-progress','waiting','blocked','failed'].includes(item.status))
  return active.length ? active.map(item => statuses[item.status]).filter((value,index,all) => all.indexOf(value) === index).join(' · ') : `${completed.value}/${total.value} 已完成`
})
const status = computed(() => {
  const all = (delegated.value ? runs.value : items.value).map(item => item.status)
  return all.find(value => ['failed','blocked','waiting','running','in-progress'].includes(value)) || (all.length && completed.value === total.value ? 'completed' : all[0] || '')
})
</script>

<template>
  <article class="progress-record" :data-open="open" :data-testid="delegated ? 'delegation-block' : 'plan-block'">
    <button class="progress-summary" :aria-expanded="open" @click="open = !open"><span>{{ delegated ? '↗' : '☷' }}</span><strong>{{ delegated ? '子代理' : '任务计划' }}</strong><span class="progress-status">{{ completed }}/{{ total }}</span><ActivityStatus :status="status" :label="summary" /><DisclosureIcon :open="open" /></button>
    <div v-if="open" class="progress-body">
      <template v-if="delegated">
        <button v-for="run in runs" :key="run.runId" class="child-record" :disabled="!run.childSessionId" :data-child-session-id="run.childSessionId" :data-run-id="run.runId" :data-mode="run.mode" :data-status="run.status" data-testid="delegation-run">
          <span><strong>{{ run.agent || run.title }}</strong><small>{{ run.mode === 'background' ? '后台' : '前台' }}</small><span v-if="run.summary" class="child-summary">{{ run.summary }}</span></span><ActivityStatus :status="run.status" :label="statuses[run.status] || run.status" /><span>↗</span>
        </button>
      </template>
      <ol v-else><li v-for="item in items" :key="item.id" :data-plan-status="item.status"><span :class="item.status">{{ item.status === 'completed' ? '✓' : item.status === 'in-progress' ? '◐' : item.status === 'blocked' ? '!' : '○' }}</span><span>{{ item.text }}</span></li></ol>
    </div>
  </article>
</template>

<style scoped>
.progress-record { border:1px solid transparent; border-radius:6px; color:var(--tuck-text); overflow:hidden; }
.progress-record[data-open=true] { border-color:var(--tuck-border); background:var(--tuck-surface); }
.progress-record .progress-summary { display:flex; width:100%; align-items:center; gap:8px; min-height:32px; padding:4px 8px; border:0; border-radius:0; background:transparent; color:var(--tuck-muted); font-size:var(--tuck-font-ui); line-height:1.5; }
.progress-summary strong { font-size:var(--tuck-font-ui); font-weight:550; color:var(--tuck-text); }
.progress-record .progress-summary:hover { background:var(--tuck-hover); }
.progress-status { flex:1; text-align:left; font-size:var(--tuck-font-caption); }
.progress-body { border-top:1px solid var(--tuck-border); max-height:min(40dvh,300px); overflow:auto; overscroll-behavior:contain; }
ol { list-style:none; padding:6px 9px; margin:0; } li { display:grid; grid-template-columns:16px minmax(0,1fr); gap:6px; padding:5px 0; font-size:var(--tuck-font-ui); line-height:1.6; overflow-wrap:anywhere; }
.completed { color:var(--tuck-success); } .in-progress { color:var(--tuck-accent); } .blocked { color:var(--tuck-warning); }
.progress-record .child-record { display:flex; align-items:center; justify-content:space-between; gap:8px; text-align:left; width:100%; padding:7px 10px; border:0; border-radius:0; color:var(--tuck-text); background:transparent; }
.child-record > span:first-child { display:grid; min-width:0; gap:3px; } .child-record strong { font-size:var(--tuck-font-ui); } .child-record small { color:var(--tuck-muted); font-size:var(--tuck-font-caption); }
.child-record:hover { background:var(--tuck-hover); } .child-summary { font-size:var(--tuck-font-caption); color:var(--tuck-muted); display:-webkit-box; -webkit-line-clamp:2; -webkit-box-orient:vertical; overflow:hidden; overflow-wrap:anywhere; }
.child-record > span:first-child { flex:1; }
</style>
