<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { conversationPath } from '../../app/router'
import type { DockerTask, DockerWorkspaceRuntime } from '../../shared/workspace/runtime'
import ActivityStatus from '../../shared/ui/ActivityStatus.vue'
import TaskActions from './TaskActions.vue'
import TaskProgress from './TaskProgress.vue'
import DeleteConversationDialog from '../chat/components/DeleteConversationDialog.vue'
import { taskName, taskState, taskIsActive, taskHasEnded, taskNeedsConversation } from './task-view'
const props = defineProps<{task: DockerTask; workspace: DockerWorkspaceRuntime; connected: boolean}>()
const observedRequest = ref(false)
const deleteOpen = ref(false)
watch(() => props.task.turnTiming?.turnId, () => { observedRequest.value = false })
watch(() => props.task.pendingInteraction, pending => { if (pending) observedRequest.value = true }, {immediate:true})
const notice = computed(() => {
  if (!props.connected) return '连接中断，正在同步任务状态。'
  if (taskNeedsConversation(props.task)) return '任务需要处理，请进入对话检查错误和待发送内容。'
  if (props.task.status === 'waiting') return '正在同步待处理请求…'
  if (props.task.status === 'working') return observedRequest.value ? '请求已处理，已恢复执行。' : '正在执行，无需操作。'
  if (taskIsActive(props.task)) return '等待执行，无需操作。'
  if (taskHasEnded(props.task)) return props.task.latestDelivery?.turnId===props.task.turnId ? '本轮已结束，可查看交付或补充任务。' : '本轮已结束，尚未提交本轮交付。'
  return '任务尚未开始。'
})
</script>
<template>
  <section class="task-page task-request-page" :aria-label="taskName(task.title)">
    <div class="task-title"><h1><RouterLink :to="conversationPath(task.id)" title="Chat details">{{ taskName(task.title) }}</RouterLink></h1><span class="task-status" role="status"><ActivityStatus :status="taskState(task).icon" aria-hidden="true" />{{ taskState(task).label }}</span></div>
    <TaskProgress :task="task" :workspace="workspace" :connected="connected" />
    <p v-if="!task.pendingInteraction" role="status">{{ notice }}</p>
    <TaskActions :task="task" :workspace="workspace" :connected="connected" expanded />
    <button type="button" class="task-button" data-testid="task-delete" @click="deleteOpen = true">删除对话</button>
    <DeleteConversationDialog v-if="deleteOpen" :task="task" :workspace="workspace" @close="deleteOpen = false" />
  </section>
</template>
