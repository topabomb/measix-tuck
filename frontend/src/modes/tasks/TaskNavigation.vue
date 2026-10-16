<script setup lang="ts">
import { computed } from 'vue'
import type { DockerTask } from '../../shared/workspace/runtime'
import { taskName, taskPath, tasksAssistantPath, type Assistant } from './task-view'
const props = defineProps<{trail: string[]; tasks: DockerTask[]; assistants: Assistant[]}>()
function label(path: string) {
  const clean = path.split('?')[0]
  if (clean === '/tasks') return '任务'
  for (const assistant of props.assistants) {
    if (clean === tasksAssistantPath(assistant.agent)) return `${assistant.displayName} · 全部任务`
    if (clean === `${tasksAssistantPath(assistant.agent)}/new`) return `${assistant.displayName} · 新建任务`
  }
  for (const task of props.tasks) {
    const assistant = props.assistants.find(item => item.agent === task.agent)?.displayName || task.assistantName || task.agent
    if (clean === taskPath(task.id)) return `${assistant} · ${taskName(task.title)}`
    if (clean === `${taskPath(task.id)}/supplement`) return `${assistant} · 补充任务`
    if (clean === `${taskPath(task.id)}/delivery`) return `${assistant} · ${path.includes('from=supplement') ? '上次交付' : '交付'}`
  }
  return '任务'
}
const crumbs = computed(() => props.trail.map(path => ({path,label:label(path)})))
const back = computed(() => crumbs.value.at(-2))
const backLabel = computed(() => {
  const path = back.value?.path.split('?')[0] || ''
  if (path === '/tasks') return '返回任务'
  if (path.startsWith('/tasks/assistants/')) return /^\/tasks\/assistants\/[^/]+\/new$/.test(path) ? '返回新建任务' : '返回全部任务'
  if (path.endsWith('/supplement')) return '返回补充任务'
  if (path.endsWith('/delivery')) return '返回交付'
  return '返回任务详情'
})
</script>
<template>
  <div v-if="crumbs.length > 1" class="task-navigation">
    <RouterLink v-if="back" class="back-link" :to="back.path"><span aria-hidden="true">←</span>{{ backLabel }}</RouterLink>
    <nav aria-label="任务路径"><ol><li v-for="(crumb,index) in crumbs" :key="crumb.path"><span v-if="index" aria-hidden="true" class="crumb-separator">/</span><span v-if="index === crumbs.length-1" aria-current="page">{{ crumb.label }}</span><RouterLink v-else :to="crumb.path">{{ crumb.label }}</RouterLink></li></ol></nav>
  </div>
</template>
