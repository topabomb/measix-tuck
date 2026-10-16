<script setup lang="ts">
import { taskName } from "../../../../shared/task-channel.mjs"
import { ref } from 'vue'
import type { DockerTask, DockerWorkspaceRuntime } from '../../../shared/workspace/runtime'
import WorkbenchDialog from '../../../shared/ui/WorkbenchDialog.vue'
const props = defineProps<{ task: DockerTask; workspace: DockerWorkspaceRuntime }>()
const emit = defineEmits<{ close: [] }>()
const title = ref(taskName(props.task.title)), busy = ref(false), error = ref('')
async function save() {
  if (busy.value || !title.value.trim()) return
  busy.value = true; error.value = ''
  try { await props.workspace.renameSession(props.task.id, title.value); emit('close') }
  catch (cause) { error.value = (cause as Error).message }
  finally { busy.value = false }
}
</script>
<template>
  <WorkbenchDialog label="重命名对话" :busy="busy" :dismiss-on-backdrop="title === taskName(task.title)" @close="emit('close')">
    <form @submit.prevent="save"><h2>重命名对话</h2><label>对话名称<input v-model="title" :maxlength="taskName(task.title) === task.title ? 120 : 111" :disabled="busy" required autofocus /></label><p v-if="error" role="alert">{{ error }}</p><div class="dialog-actions"><button type="button" :disabled="busy" @click="emit('close')">取消</button><button type="submit" :disabled="busy || !title.trim()">{{ busy ? '保存中…' : '保存' }}</button></div></form>
  </WorkbenchDialog>
</template>
