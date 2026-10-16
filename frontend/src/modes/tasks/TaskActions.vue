<script setup lang="ts">
import { useRoute } from 'vue-router'
import { computed, ref, watch } from 'vue'
import { api, type DockerTask, type DockerWorkspaceRuntime } from '../../shared/workspace/runtime'
import QuestionForm from '../../shared/conversation/QuestionForm.vue'
import { presentQuestion } from '../../shared/conversation/question-presentation'
import { taskPath, taskHasEnded, taskLink, taskNeedsConversation } from './task-view'
import { conversationPath } from '../../app/router'
const route = useRoute()
const props = defineProps<{task: DockerTask; workspace: DockerWorkspaceRuntime; connected: boolean; expanded?: boolean}>()
const busy = ref(false), error = ref('')
watch(() => props.task.pendingInteraction?.id, () => { error.value = '' })
const pending = computed(() => presentQuestion(props.task.pendingInteraction))
const limit = computed(() => pending.value?.kind === 'approval' && pending.value.purpose === 'iterations')
async function command(action: string, body: unknown) {
  if (busy.value || !props.connected) return
  const interactionId = props.task.pendingInteraction?.id
  busy.value = true; error.value = ''
  try { await api(`/api/tasks/${props.task.id}/${action}`,body); await props.workspace.refreshTask(props.task.id) }
  catch (cause) {
    await props.workspace.refreshTask(props.task.id).catch(() => {})
    // Another browser or the native runtime may have moved to the next request
    // while this response was in flight. Do not attach the old error to it.
    if (action !== 'interaction' || props.task.pendingInteraction?.id === interactionId) error.value = (cause as Error).message
  }
  finally { busy.value = false }
}
function answer(value: string | null) {
  const current = pending.value
  if (current?.kind === 'question') void command('interaction',{interactionId:current.id,kind:'question',answer:value})
}
function approve(approved: boolean) {
  const current = pending.value
  if (current?.kind === 'approval') void command('interaction',{interactionId:current.id,kind:'approval',approved})
}
</script>
<template>
  <div v-if="expanded && pending || task.lastFailure || error" class="task-request">
    <section v-if="expanded && pending" class="task-interaction" aria-label="待处理请求" :aria-busy="busy">
      <h2>{{ pending.title }}</h2>
      <template v-if="pending.kind === 'approval'">
        <p v-if="limit">{{ pending.detail }}</p><pre v-else class="task-parameters">{{ pending.detail }}</pre>
        <div class="task-form-actions"><button :disabled="busy || !connected" @click="approve(false)">{{ limit ? '结束本轮' : '拒绝' }}</button><button class="primary" :disabled="busy || !connected" @click="approve(true)">{{ limit ? '继续 10 步' : '允许本次' }}</button></div>
      </template>
      <QuestionForm v-else :key="pending.id" :question="pending" :disabled="busy || !connected" full-page @submit="answer" @skip="answer(null)" />
    </section>
    <p v-if="task.lastFailure" class="task-error" role="alert">{{ task.lastFailure.message }}</p>
    <p v-if="error" class="task-error" role="alert">{{ error }}</p>
  </div>
  <div v-if="!expanded || taskHasEnded(task) || taskNeedsConversation(task) || task.status !== 'idle'" class="row-actions" role="group" aria-label="任务操作">
    <RouterLink v-if="taskNeedsConversation(task)" class="task-button primary" :to="conversationPath(task.id)">进入对话处理</RouterLink>
    <RouterLink v-else-if="pending && !expanded" class="task-button primary" :to="taskLink(taskPath(task.id),route)">处理请求</RouterLink>
    <template v-else-if="taskHasEnded(task)"><RouterLink class="task-button accent-action" :to="taskLink(`${taskPath(task.id)}/delivery`,route,expanded ? 'detail' : undefined)">查看交付</RouterLink><RouterLink class="task-button task-supplement" :to="taskLink(`${taskPath(task.id)}/supplement`,route,expanded ? 'detail' : undefined)">补充任务</RouterLink></template>
    <RouterLink v-else-if="!expanded" class="task-button accent-action" :to="taskLink(taskPath(task.id),route)">查看任务</RouterLink>
    <button v-else :disabled="busy || !connected" @click="command('abort',{})">停止任务</button>
    <RouterLink v-if="!taskNeedsConversation(task) && !taskHasEnded(task) && task.latestDelivery" class="task-button" :to="taskLink(`${taskPath(task.id)}/delivery`,route,expanded ? 'detail' : undefined)">查看交付</RouterLink>
  </div>
</template>
