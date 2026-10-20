<script setup lang="ts">
import {ref} from 'vue'
import {taskName} from '../../../../shared/task-channel.mjs'
import type {DockerTask, DockerWorkspaceRuntime} from '../../../shared/workspace/runtime'
import WorkbenchDialog from '../../../shared/ui/WorkbenchDialog.vue'
const props = defineProps<{task:DockerTask;workspace:DockerWorkspaceRuntime}>()
const emit = defineEmits<{close:[];deleted:[]}>()
const busy=ref(false),error=ref('')
async function remove() {
  if(busy.value)return
  busy.value=true;error.value=''
  try { await props.workspace.deleteSession(props.task.id);emit('deleted');emit('close') }
  catch(cause) { error.value=(cause as Error).message }
  finally { busy.value=false }
}
</script>
<template>
  <WorkbenchDialog label="删除对话" :busy="busy" :dismiss-on-backdrop="false" @close="emit('close')">
    <form @submit.prevent="remove">
      <h2>删除对话？</h2>
      <p class="delete-title">{{ taskName(task.title) }}</p>
      <p>将永久删除此对话及其全部子会话的历史、统计和交付副本，无法撤销。</p>
      <p>独立分支、工作目录、上传源文件及共享文件会保留。</p>
      <p v-if="error" role="alert">{{ error }}</p>
      <div class="dialog-actions"><button type="button" autofocus :disabled="busy" @click="emit('close')">取消</button><button type="submit" class="delete-confirm" :disabled="busy">{{ busy ? '删除中…' : '确认删除' }}</button></div>
    </form>
  </WorkbenchDialog>
</template>
<style scoped>
.delete-title {font-weight:600;overflow-wrap:anywhere}button.delete-confirm[type=submit] {background:var(--tuck-danger,#c43e3e);color:#fff}
</style>
