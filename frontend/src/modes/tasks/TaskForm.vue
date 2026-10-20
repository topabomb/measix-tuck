<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { api, type DockerWorkspaceRuntime } from '../../shared/workspace/runtime'
import TuckLoading from '../../shared/ui/TuckLoading.vue'
import TaskResult from './TaskResult.vue'
import RetentionStatus from '../chat/components/RetentionStatus.vue'
import { makeTaskTitle } from '../../../shared/task-channel.mjs'
import { channelLimit, taskIsActive, taskHasEnded, taskName, tasksAssistantPath, createTaskDraft, type TaskDraft, type Assistant } from './task-view'
import {workspacePolicy} from '../../../shared/workspace-mode.mjs'
import type { DockerTask } from '../../shared/workspace/runtime'
const props = defineProps<{workspace: DockerWorkspaceRuntime; assistant: Assistant; tasks: DockerTask[]; draft: TaskDraft; connected: boolean; task?: DockerTask}>()
const emit = defineEmits<{submitted:[id:string]}>()
const route = useRoute(), fileInput = ref<HTMLInputElement>()
const version = ref(0)
const off = props.workspace.subscribe(() => version.value++)
let unwatchUploads = () => {}
watch(() => props.draft.taskId, id => {
  unwatchUploads()
  unwatchUploads = props.workspace.attachmentDrafts.subscribe(id, () => version.value++)
  version.value++
}, {immediate:true,flush:'sync'})
onBeforeUnmount(() => {off();unwatchUploads()})
const policy = computed(()=>{
  void version.value
  const existing=props.draft.taskId?props.workspace.tasks.get(props.draft.taskId):undefined
  return existing?{mode:existing.workspaceMode,directory:existing.workspace}:workspacePolicy(props.assistant)
})
const uploaded = computed(() => { void version.value; return props.workspace.attachmentDrafts.list(props.draft.taskId) })
const connection = computed(() => { void version.value; return props.workspace.runtimeInfo.connection })
const resolvedModelId = computed(() => {
  const existing = props.task || props.tasks.find(task => task.id === props.draft.taskId)
  const modelId = existing ? existing.modelId : props.assistant.defaultModelId || connection.value?.defaultModelId
  return modelId
})
const resolvedModel = computed(() => connection.value?.models.find(model => model.modelId === resolvedModelId.value))
const needsModelChoice = computed(() => !!connection.value && !resolvedModelId.value)
const unavailableModel = computed(() => resolvedModelId.value && !resolvedModel.value?.selectable ? `模型 ${resolvedModel.value?.displayName || resolvedModelId.value} 已被移除或当前不可用，请检查模型配置。` : '')
const selected = computed(() => needsModelChoice.value ? connection.value?.models.find(model => model.modelId === props.draft.modelId) : resolvedModel.value)
const modelIssue = computed(() => !!unavailableModel.value || needsModelChoice.value && !selected.value?.selectable)
const full = computed(() => props.tasks.filter(task => task.agent === props.assistant.agent && taskIsActive(task)).length >= channelLimit(props.assistant))
const continuationIssue = computed(() => !props.task ? '' : !taskHasEnded(props.task) ? '任务尚未结束，请返回任务列表查看进展' : props.task.queue.length ? '此任务有待发送内容，请在全部任务中查看对话并处理' : '')
const hasFiles = computed(() => props.draft.files.length + uploaded.value.length > 0)
const imageIssue = computed(() => [...props.draft.files,...uploaded.value.map(item => item.file)].some(file => /^image\//.test(file.type)) && selected.value && !selected.value.inputModalities.includes('IMAGE') ? '此助手当前不支持图片附件，请移除图片或提供其他资料' : '')
function filesSelected(event: Event) {
  const input = event.target as HTMLInputElement
  const files = Array.from(input.files || [])
  input.value = ''
  if (props.draft.files.length + uploaded.value.length + files.length > 8) { props.draft.error = '每条消息最多 8 个附件'; return }
  if (files.some(file => file.size > 16*1024*1024)) { props.draft.error = '单个文件最多 16 MiB'; return }
  props.draft.error = ''; props.draft.files.push(...files)
}
function uploadsReady(id: string): Promise<void> {
  return new Promise((resolve,reject) => {
    let stop = () => {}
    const check = () => {
      if (props.workspace.attachmentDrafts.closed) { stop(); reject(new Error('工作台已关闭')); return }
      const items = props.workspace.attachmentDrafts.list(id)
      const error = items.find(item => item.status === 'error')
      if (error) { stop(); reject(new Error(error.error || '附件上传失败')); return }
      if (items.every(item => item.status === 'ready')) { stop(); resolve() }
    }
    stop = props.workspace.attachmentDrafts.subscribe(id,check); check()
  })
}
async function submit() {
  const draft = props.draft
  if (draft.busy || draft.uncertain || full.value || !props.connected || imageIssue.value || continuationIssue.value || modelIssue.value) return
  if (!props.task && (!draft.title.trim() || !draft.prompt.trim())) { draft.error = '请填写标题和任务要求'; return }
  if (props.task && !draft.prompt.trim() && !hasFiles.value) { draft.error = '请填写补充要求或上传资料'; return }
  draft.busy = true; draft.error = ''
  const origin = route.fullPath
  const expectedTurnCount = props.task?.turnCount
  let mutation = ''
  try {
    if (props.task) draft.taskId = props.task.id
    if (draft.taskId && needsModelChoice.value && draft.modelId) await props.workspace.selectModel(draft.taskId,draft.modelId)
    if (!props.task && !draft.taskId) {
      const title = makeTaskTitle(draft.title,[...props.workspace.tasks.values()].map(task => task.title))
      mutation = 'create'
      draft.taskId = await props.workspace.createSession({title,agent:props.assistant.agent,approvalMode:'auto',channel:'tasks',...(needsModelChoice.value ? {modelId:draft.modelId} : {})})
    } else if (!props.task) await props.workspace.renameSession(draft.taskId,draft.title)
    mutation = ''
    if (draft.files.length) {
      const error = props.workspace.attachmentDrafts.add(draft.taskId,[...draft.files])
      if (error) throw new Error(error)
      draft.files = []
    }
    await uploadsReady(draft.taskId)
    mutation = 'prompts'
    await api(`/api/tasks/${draft.taskId}/prompts`,{prompt:draft.prompt,attachmentIds:props.workspace.attachmentDrafts.list(draft.taskId).map(item => item.uploaded!.id),...(props.task ? {expectedTurnCount} : {})})
    mutation = ''
    const id = draft.taskId
    for (const item of props.workspace.attachmentDrafts.list(id)) props.workspace.attachmentDrafts.remove(id,item.id)
    // Refresh only this task; navigation does not depend on the first model token.
    await props.workspace.refreshTask(id).catch(() => {})
    Object.assign(draft,createTaskDraft(),props.task ? {taskId:id} : {})
    if (route.fullPath === origin) emit('submitted',id)
  } catch (cause) {
    const error = cause as Error & {status?: number}
    if (mutation && (!error.status || error.status >= 500)) {
      draft.uncertain = true
      draft.error = '提交结果尚未确认，请在全部任务中核对，避免重复发送'
    } else draft.error = error.message
  } finally { draft.busy = false }
}
const fileSize = (bytes: number) => bytes < 1024*1024 ? `${Math.ceil(bytes/1024)} KB` : `${(bytes/1024/1024).toFixed(1)} MB`
</script>
<template>
  <section class="task-page task-create" aria-labelledby="task-create-title">
    <h1 id="task-create-title">{{ task ? '补充任务' : '新建任务' }}</h1>
    <p v-if="task" class="task-source">{{ taskName(task.title) }}</p>
    <section v-if="task && taskHasEnded(task)" class="task-previous" aria-label="上次结果"><h2>上次结果</h2><TaskResult :task="task" compact /></section>
    <p id="task-form-guide" class="task-form-guide">{{ task ? '在原任务上补充要求或资料，助手会保留已有上下文继续执行。独立目标请新建任务。' : '交给此助手一项明确的任务，说明目标、必要条件和期望交付。提交后可在任务页查看进展、回答问题。' }}</p>
    <form aria-labelledby="task-create-title" @submit.prevent="submit" :aria-busy="draft.busy">
      <fieldset :disabled="draft.busy || draft.uncertain">
        <div v-if="needsModelChoice" class="task-field"><label for="task-model-choice">模型</label><select id="task-model-choice" v-model="draft.modelId" required aria-describedby="task-model-guide"><option :value="undefined" disabled>选择模型</option><option v-for="model in connection?.models" :key="model.modelId" :value="model.modelId" :disabled="!model.selectable">{{ model.displayName }}{{ model.selectable ? '' : '（不可用）' }}</option></select><small id="task-model-guide">此助手未配置默认模型，请为此任务选择模型。</small></div>
        <label v-if="!task" class="task-field">标题<input v-model="draft.title" required maxlength="111" autocomplete="off" placeholder="用一句话概括要完成的事" /></label>
        <div v-if="!task && policy.mode==='specified'" class="task-field"><span>工作目录</span><code class="task-fixed-directory" aria-label="指定工作目录">{{policy.directory}}</code></div>
        <label class="task-field">{{ task ? '补充要求' : '任务要求' }}<textarea v-model="draft.prompt" :required="!task || !hasFiles" aria-describedby="task-form-guide" :placeholder="task ? '说明要修改或继续的部分，以及新增资料的用途' : '说明做什么、使用哪些资料或网站、限制条件，以及需要交付什么'" maxlength="100000" rows="5" /></label>
        <div class="task-attachment-heading"><h2>附件</h2><button type="button" aria-describedby="task-attachment-guide" @click="fileInput?.click()">添加附件</button><input ref="fileInput" type="file" multiple hidden @change="filesSelected" /></div>
        <p id="task-attachment-guide" class="task-form-guide">提交时上传，最多 8 个，单个不超过 16 MiB。</p>
        <ul v-if="hasFiles" class="task-attachments" aria-label="附件">
          <li v-for="(file,index) in draft.files" :key="index" :aria-label="file.name"><span>{{ file.name }}</span><small>{{ fileSize(file.size) }} · 待上传</small><button type="button" :aria-label="`移除 ${file.name}`" @click="draft.files.splice(index,1)">移除</button></li>
          <li v-for="file in uploaded" :key="file.id" :aria-label="file.file.name"><span>{{ file.file.name }}</span><small :role="file.status === 'error' ? 'alert' : undefined">{{ file.status === 'ready' ? '已上传' : file.status === 'error' ? file.error : '上传中…' }}</small><button v-if="file.status === 'error'" type="button" :aria-label="`重试 ${file.file.name}`" @click="workspace.attachmentDrafts.retry(draft.taskId,file.id)">重试</button><button type="button" :aria-label="`移除 ${file.file.name}`" @click="workspace.attachmentDrafts.remove(draft.taskId,file.id)">移除</button></li>
        </ul>
      </fieldset>
      <p v-if="unavailableModel || imageIssue || draft.error" class="task-error" role="alert">{{ unavailableModel || imageIssue || draft.error }}</p>
      <p v-if="continuationIssue && !draft.busy" class="task-error" role="status">{{ continuationIssue }}</p>
      <p v-if="full" role="status">进行中任务已满，请等待任务结束后再提交</p>
      <RetentionStatus v-if="task" :task="task" :workspace="workspace" />
      <div class="task-form-actions"><RouterLink v-if="draft.taskId && !task || draft.uncertain || continuationIssue" :to="tasksAssistantPath(assistant.agent)">全部任务</RouterLink><button class="primary" type="submit" :aria-label="draft.busy ? '正在提交…' : task ? '提交补充' : '提交任务'" :disabled="draft.busy || draft.uncertain || full || !connected || !!imageIssue || !!continuationIssue || modelIssue"><TuckLoading v-if="draft.busy" label="正在提交…" /><template v-else>{{ task ? '提交补充' : '提交任务' }}</template></button></div>
    </form>
  </section>
</template>

<style scoped>
.task-fixed-directory{display:block;white-space:pre-wrap;overflow-wrap:anywhere;color:var(--tuck-muted);font:inherit}
</style>
