<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { ConnectionInfo, DockerWorkspaceRuntime } from '../../../shared/workspace/runtime'
import { defaultAssistants } from '../../../shared/workspace/runtime'
import {workspacePolicy,type WorkspaceMode} from '../../../../shared/workspace-mode.mjs'
import WorkspaceFields from '../../../shared/workspace/WorkspaceFields.vue'
import WorkbenchDialog from '../../../shared/ui/WorkbenchDialog.vue'
const props = defineProps<{ workspace: DockerWorkspaceRuntime; connection?: ConnectionInfo; initialAgent?: string }>()
const emit = defineEmits<{ close: []; created: [id: string] }>()
const title = ref(''), directory = ref(''), model = ref('')
const approval = ref<'manual' | 'auto'>('manual'), busy = ref(false), error = ref('')
const catalog = props.workspace.runtimeInfo.assistants || defaultAssistants
const agent = ref(catalog.items.some(item => item.agent === props.initialAgent) ? props.initialAgent! : catalog.defaultAgent)
const assistant = computed(() => catalog.items.find(item => item.agent === agent.value))
const policy = computed(()=>workspacePolicy(assistant.value))
const mode = ref<WorkspaceMode>(policy.value.mode)
watch(assistant,()=>{mode.value=policy.value.mode;directory.value=policy.value.directory||''},{immediate:true})
const defaultModel = computed(() => {
  const id = assistant.value?.defaultModelId || props.connection?.defaultModelId
  const item = props.connection?.models.find(item => item.modelId === id)
  return item ? `${item.displayName}${item.selectable ? '' : '（不可用）'}` : (id ? '不可用' : '未配置')
})
async function create() {
  if (busy.value) return
  busy.value = true; error.value = ''
  try {
    const id = await props.workspace.createSession({title:title.value || '新对话',agent:agent.value,workspaceMode:mode.value,workspace:mode.value==='specified'?directory.value:undefined,modelId:model.value || undefined,approvalMode:approval.value})
    emit('created', id); emit('close')
  } catch (cause) { error.value = (cause as Error).message }
  finally { busy.value = false }
}
</script>
<template>
  <WorkbenchDialog label="新对话" :busy="busy" :dismiss-on-backdrop="false" @close="emit('close')">
    <form @submit.prevent="create">
      <h2>新对话</h2>
      <label>名称<input v-model="title" placeholder="新对话" autofocus /></label>
      <label>助手<select v-model="agent" aria-label="新对话助手"><option value="" disabled>请选择 Agent</option><option v-for="item in catalog.items" :key="item.agent" :value="item.agent">{{ item.displayName }}</option></select><small>{{ assistant?.description }}</small></label>
      <label v-if="connection?.models.length">模型<select v-model="model" data-testid="new-task-model"><option value="">助手默认 · {{ defaultModel }}</option><option v-for="item in connection.models" :key="item.modelId" :value="item.modelId" :disabled="!item.selectable">{{ item.displayName }}{{ item.reason ? ' · ' + item.reason : '' }}</option></select></label>
      <label>审批模式<select v-model="approval" aria-label="新对话审批模式"><option value="manual">手动审批</option><option value="auto">自动审批</option></select><small>手动：安全读取自动执行，写入和命令等操作请求审批。自动：允许工具自动执行。</small></label>
      <WorkspaceFields v-if="assistant" v-model:mode="mode" v-model:directory="directory" />
      <p v-if="error" role="alert">{{ error }}</p>
      <div class="dialog-actions"><button type="button" :disabled="busy" @click="emit('close')">取消</button><button type="submit" :disabled="busy || !assistant">{{ busy ? '创建中…' : '创建' }}</button></div>
    </form>
  </WorkbenchDialog>
</template>
<style scoped>summary { cursor:pointer; margin:10px 0; }</style>
