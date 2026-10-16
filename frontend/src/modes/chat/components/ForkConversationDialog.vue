<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api, type DockerWorkspaceRuntime } from '../../../shared/workspace/runtime'
import WorkbenchDialog from '../../../shared/ui/WorkbenchDialog.vue'
const props = defineProps<{ workspace: DockerWorkspaceRuntime; taskId: string }>()
const emit = defineEmits<{ close: []; created: [id: string] }>()
const options = ref<{ index: number; prompt: string }[]>([]), index = ref(0), busy = ref(true), error = ref('')
const creating = ref(false)
onMounted(async () => {
  try { options.value = await api(`/api/tasks/${props.taskId}/fork-options`); index.value = options.value.at(-1)?.index ?? 0 }
  catch (cause) { error.value = (cause as Error).message }
  finally { busy.value = false }
})
async function fork() {
  if (busy.value || !options.value.length) return
  busy.value = true; creating.value = true; error.value = ''
  try { emit('created', await props.workspace.forkSession(props.taskId,index.value)); emit('close') }
  catch (cause) { error.value = (cause as Error).message }
  finally { busy.value = false; creating.value = false }
}
</script>
<template>
  <WorkbenchDialog label="创建分支" :busy="busy" :dismiss-on-backdrop="false" @close="emit('close')"><form @submit.prevent="fork">
    <h2>从用户消息处分支</h2><p>保留所选消息之前的上下文，把该消息放入新对话草稿。分支共享工作目录，文件修改会彼此可见。</p>
    <select v-model="index" aria-label="分支位置" :disabled="busy"><option v-for="option in options" :key="option.index" :value="option.index">{{ option.index + 1 }} · {{ option.prompt.slice(0,100) }}</option></select>
    <p v-if="error" role="alert">{{ error }}</p><p v-if="busy" role="status">{{ creating ? '正在创建分支…' : '正在读取分支位置…' }}</p><div class="dialog-actions"><button type="button" :disabled="busy" @click="emit('close')">取消</button><button type="submit" :disabled="busy || !options.length">{{ creating ? '创建中…' : '创建分支' }}</button></div>
  </form></WorkbenchDialog>
</template>
