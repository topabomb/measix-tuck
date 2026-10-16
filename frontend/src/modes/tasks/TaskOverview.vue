<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import type { DockerTask, DockerWorkspaceRuntime } from '../../shared/workspace/runtime'
import TaskActions from './TaskActions.vue'
import TaskProgress from './TaskProgress.vue'
import TaskResult from './TaskResult.vue'
import AssistantIcon from '../../shared/ui/AssistantIcon.vue'
import ActivityStatus from '../../shared/ui/ActivityStatus.vue'
import { conversationPath } from '../../app/router'
import { taskName, taskIsActive, taskHasEnded, taskState, tasksAssistantPath, channelLimit, taskListQuery, taskListParams, taskLink, type Assistant } from './task-view'
const props = defineProps<{tasks: DockerTask[]; assistants: Assistant[]; agent: string; connected: boolean; workspace: DockerWorkspaceRuntime}>()
const route = useRoute(), router = useRouter()
const selection = computed(() => taskListQuery(route.query))
function select(period: string, page = 1) { void router.push({path:route.path,query:taskListParams(period,page)}) }
const groups = computed(() => props.assistants.filter(a => !props.agent || a.agent === props.agent).map(assistant => {
  const all = props.tasks.filter(task => task.agent === assistant.agent)
  const active = all.filter(taskIsActive)
  const recent = all.filter(task => !taskIsActive(task) && !!task.lastTurnReason).slice(0,2)
  const period = selection.value.period
  const since = Date.now() - (period === '7d' ? 7 : 1) * 24 * 60 * 60 * 1000
  const matches = period === 'all' ? all : all.filter(task => Date.parse(task.createdAt) >= since)
  const page = Math.min(selection.value.page - 1, Math.max(0,Math.ceil(matches.length/30)-1))
  const rows = props.agent ? matches.slice(page*30,page*30+30) : [...active,...recent]
  const batches = props.agent ? [{label:'',rows}] : [{label:'进行中',rows:active},{label:'最近结束',rows:recent}].filter(batch => batch.rows.length)
  return {assistant,active,recent,rows,batches,page,total:matches.length}
}))
</script>
<template>
  <div class="task-overview">
    <section v-for="group in groups" :key="group.assistant.agent" class="assistant-section" :aria-label="group.assistant.displayName">
      <header class="assistant-heading">
        <div class="assistant-label"><component :is="agent ? 'h1' : 'h2'"><AssistantIcon />{{ group.assistant.displayName }}</component><span class="assistant-count">进行中 {{ group.active.length }} 个</span></div>
        <div class="assistant-actions">
          <RouterLink v-if="connected && !group.assistant.unavailable && group.active.length < channelLimit(group.assistant)" class="task-button primary" :to="taskLink(`${tasksAssistantPath(group.assistant.agent)}/new`,route)">新建任务</RouterLink>
          <button v-else disabled>新建任务</button>
          <RouterLink v-if="!agent" :to="tasksAssistantPath(group.assistant.agent)">全部任务</RouterLink>
        </div>
        <span v-if="group.active.length >= channelLimit(group.assistant)" class="task-capacity">进行中任务已满</span>
        <p v-if="!agent && group.assistant.description" class="task-assistant-description">{{ group.assistant.description }}</p>
      </header>
      <div v-if="agent" class="task-time-filter" role="group" aria-label="下达时间"><button v-for="period in [{value:'24h',label:'今天'},{value:'7d',label:'最近 7 天'},{value:'all',label:'全部'}]" :key="period.value" type="button" :aria-pressed="selection.period === period.value" @click="select(period.value)">{{ period.label }}</button></div>
      <section v-for="batch in group.batches" :key="batch.label" class="task-batch" :aria-label="batch.label || undefined">
      <h3 v-if="batch.label" class="task-group-label">{{ batch.label }}</h3>
      <ul class="task-list" aria-label="任务列表">
        <li v-for="task in batch.rows" :key="task.id" :data-task-id="task.id" :aria-label="taskName(task.title)">
          <div class="task-row" :class="{ 'task-waiting': !!task.pendingInteraction }">
            <div class="task-identity"><RouterLink v-if="agent" class="task-name" :to="conversationPath(task.id)" title="Chat details">{{ taskName(task.title) }}</RouterLink><span v-else class="task-name">{{ taskName(task.title) }}</span><span class="task-status" :data-state="taskState(task).icon" role="status"><ActivityStatus :status="taskState(task).icon" aria-hidden="true" /><span>{{ taskState(task).label }}</span></span></div>
            <TaskActions :task="task" :workspace="workspace" :connected="connected" />
            <div class="task-row-info"><TaskProgress :task="task" :workspace="workspace" :connected="connected" /><TaskResult v-if="taskHasEnded(task) || task.latestDelivery" :task="task" preview /></div>
          </div>
        </li>
      </ul>
      </section>
      <p v-if="!group.rows.length" class="task-empty">{{ agent ? '此时间范围内暂无任务' : '暂无任务' }}</p>
      <nav v-if="agent && group.total > 30" class="task-pagination" aria-label="任务分页"><button :disabled="group.page === 0" @click="select(selection.period,group.page)">上一页</button><span>{{ group.page+1 }} / {{ Math.ceil(group.total/30) }}</span><button :disabled="(group.page+1)*30 >= group.total" @click="select(selection.period,group.page+2)">下一页</button></nav>
    </section>
  </div>
</template>
