<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import type { DockerWorkspaceRuntime, DockerTask } from '../../shared/workspace/runtime'
import { defaultAssistants } from '../../shared/workspace/runtime'
import TuckLoading from '../../shared/ui/TuckLoading.vue'
import TaskOverview from './TaskOverview.vue'
import TaskForm from './TaskForm.vue'
import TaskDetail from './TaskDetail.vue'
import TaskDelivery from './TaskDelivery.vue'
import TaskNavigation from './TaskNavigation.vue'
import { conversationPath } from '../../app/router'
import { sortedTasks, isChannelTask, taskPath, tasksAssistantPath, taskListParams, taskReturnTo, taskLink, taskNeedsConversation, createTaskDraft, type TaskViewState, type Assistant } from './task-view'
import './tasks.css'
const props = defineProps<{workspace: DockerWorkspaceRuntime; state: TaskViewState}>()
const route = useRoute(), router = useRouter()
const host = ref<HTMLElement>()
const tasks = shallowRef<DockerTask[]>([]), assistants = shallowRef<Assistant[]>([]), connected = ref(false)
let revision = -1
function update() {
  connected.value = props.workspace.connected
  if (revision === props.workspace.taskRevision) return
  revision = props.workspace.taskRevision
  tasks.value = sortedTasks([...props.workspace.tasks.values()])
  const catalog = props.workspace.runtimeInfo.assistants || defaultAssistants
  assistants.value = [...catalog.items]
  for (const task of tasks.value) if (!assistants.value.some(item => item.agent === task.agent)) assistants.value.push({agent:task.agent,displayName:task.assistantName || task.agent,unavailable:true})
}
const unsubscribe = props.workspace.subscribe(update)
update()
const agent = computed(() => typeof route.params.agent === 'string' ? route.params.agent : '')
const id = computed(() => typeof route.params.id === 'string' ? route.params.id : '')
const assistant = computed(() => assistants.value.find(item => item.agent === agent.value))
const task = computed(() => tasks.value.find(item => item.id === id.value))
const isNew = computed(() => route.name === 'task-new')
const isSupplement = computed(() => route.name === 'task-supplement')
const isDelivery = computed(() => route.name === 'task-delivery')
const taskAssistant = computed(() => assistants.value.find(item => item.agent === task.value?.agent))
const loading = ref(false), error = ref('')
let navigation = 0
watch(() => route.fullPath, async (path, previous) => {
  const index = props.state.trail.indexOf(path)
  if (index >= 0) props.state.trail.splice(index + 1)
  else if (previous && previous.split('?')[0] === route.path && props.state.trail.at(-1) === previous) props.state.trail.splice(-1,1,path)
  else if (previous?.startsWith('/tasks') && props.state.trail.at(-1) === previous) props.state.trail.push(path)
  else {
    // Direct links have a deterministic parent; normal navigation retains the
    // actual origin, without browser-history guesses or persisted routing data.
    props.state.trail = ['/tasks']
    const returnTo = taskReturnTo(route.query.returnTo)
    if (returnTo) props.state.trail.push(returnTo)
    else if (agent.value && isNew.value) props.state.trail.push(tasksAssistantPath(agent.value))
    if (id.value && isDelivery.value && route.query.from === 'supplement') {
      const viaDetail = route.query.via === 'detail'
      if (viaDetail) props.state.trail.push(router.resolve(taskLink(taskPath(id.value),route)).fullPath)
      props.state.trail.push(router.resolve(taskLink(`${taskPath(id.value)}/supplement`,route,viaDetail ? 'detail' : undefined)).fullPath)
    }
    else if (id.value && route.query.from === 'detail' && route.path !== taskPath(id.value)) props.state.trail.push(router.resolve(taskLink(taskPath(id.value),route)).fullPath)
    if (path !== '/tasks') props.state.trail.push(path)
  }
  const token = ++navigation
  error.value = ''; loading.value = false
  if (id.value && !props.workspace.hasSession(id.value)) {
    loading.value = true
    try { await props.workspace.refreshTask(id.value) } catch (cause) { if (token === navigation) error.value = (cause as Error).message }
    finally { if (token === navigation) { loading.value = false; update() } }
  }
  if (token !== navigation) return
  if (id.value && !loading.value && (!task.value || !isChannelTask(task.value))) error.value ||= '任务不存在或不属于任务频道'
  if (agent.value && (!assistant.value || isNew.value && assistant.value.unavailable)) error.value = '此助手不可用于新建任务'
  await nextTick()
  if (token === navigation && host.value && !(previous?.split('?')[0] === route.path && isDelivery.value)) host.value.scrollTop = props.state.offsets[route.fullPath] || 0
}, {immediate:true})
const draft = computed(() => {
  const key = isSupplement.value && task.value ? `task:${task.value.id}` : `new:${agent.value}`
  if (!props.state.drafts[key]) props.state.drafts[key] = {...createTaskDraft(),...(isSupplement.value && task.value ? {taskId:task.value.id} : {})}
  // Read back through the reactive owner. Returning the assignment's raw RHS
  // makes the first visit's attachments and busy/error state non-reactive.
  return props.state.drafts[key]!
})
function remember() { if (host.value) props.state.offsets[route.fullPath] = host.value.scrollTop }
async function submitted(id: string) {
  await router.push('/tasks')
  await nextTick()
  const row = host.value?.querySelector(`[data-task-id="${CSS.escape(id)}"]`)
  if (row) row.scrollIntoView({block:'nearest'})
  else {
    // A quickly completed older conversation may fall outside the latest two.
    // Return to its list page so the submitted task remains immediately visible.
    const submittedTask = tasks.value.find(item => item.id === id)
    if (!submittedTask) return
    const agent = submittedTask.agent
    const page = Math.floor(tasks.value.filter(item => item.agent === agent).findIndex(item => item.id === id)/30) + 1
    await router.replace({path:tasksAssistantPath(agent),query:taskListParams('all',page)})
    await nextTick()
    host.value?.querySelector(`[data-task-id="${CSS.escape(id)}"]`)?.scrollIntoView({block:'nearest'})
  }
}
onMounted(() => { if (host.value) host.value.scrollTop = props.state.offsets[route.fullPath] || 0 })
onBeforeUnmount(() => { navigation++; unsubscribe() })
</script>
<template>
  <div class="tasks-mode">
  <header v-if="route.path === '/tasks'" class="tasks-home-header"><div><h1>让 Tuck 执行任务</h1><p>为 Agent 浏览器自动化优化</p></div></header>
  <main ref="host" class="task-channel" aria-label="任务频道" @scroll.passive="remember">
    <TaskNavigation v-if="!error" :class="{'task-navigation-wide': !!agent && !isNew}" :trail="state.trail" :tasks="tasks" :assistants="assistants" />
    <p v-if="!connected" class="channel-notice" role="status">连接中，暂不可操作</p>
    <div v-if="loading" class="channel-loading"><TuckLoading label="正在加载任务…" /></div>
    <section v-else-if="error" class="task-page"><RouterLink class="back-link task-back" to="/tasks"><span aria-hidden="true">←</span>返回任务</RouterLink><p role="alert">{{ error }}</p></section>
    <section v-else-if="task && (isSupplement || isDelivery) && taskNeedsConversation(task)" class="task-page" aria-label="任务需要处理"><h1>任务需要处理</h1><p role="status">请进入对话检查错误和待发送内容，再继续任务。</p><RouterLink class="task-button primary" :to="conversationPath(task.id)">进入对话处理</RouterLink></section>
    <TaskForm v-else-if="isNew && assistant" :key="`new:${agent}`" :workspace="workspace" :assistant="assistant" :tasks="tasks" :draft="draft" :connected="connected" @submitted="submitted" />
    <TaskForm v-else-if="isSupplement && task && taskAssistant" :key="`task:${task.id}`" :task="task" :workspace="workspace" :assistant="taskAssistant" :tasks="tasks" :draft="draft" :connected="connected" @submitted="submitted" />
    <TaskDelivery v-else-if="task && isDelivery" :key="task.id" :task="task" />
    <TaskDetail v-else-if="task" :key="task.id" :task="task" :workspace="workspace" :connected="connected" />
    <TaskOverview v-else :workspace="workspace" :tasks="tasks" :assistants="assistants" :agent="agent" :connected="connected" />
  </main>
  </div>
</template>
