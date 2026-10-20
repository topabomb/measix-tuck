<script setup lang="ts">
import { ref, watch, onBeforeUnmount } from 'vue'
import { api, type DockerTask, type DockerWorkspaceRuntime } from '../../../shared/workspace/runtime'
const props = defineProps<{ task: DockerTask; workspace: DockerWorkspaceRuntime }>()
const state = ref<{ dueAt: number | null; blockedReason: string; archivedAt?: number; keep: boolean; lastDeleteError?: string }>()
const error = ref(''), busy = ref(false)
let controller: AbortController | undefined
async function load() {
  controller?.abort()
  const current = new AbortController(); controller = current; error.value = ''
  try {
    const next = await api<NonNullable<typeof state.value>>(`/api/tasks/${encodeURIComponent(props.task.id)}/retention`, undefined, current.signal)
    if (!current.signal.aborted) state.value = next
  } catch (cause) { if (!current.signal.aborted) error.value = (cause as Error).message }
}
async function toggleKeep() {
  busy.value = true; error.value = ''
  try {
    if (!await props.workspace.command(props.task.id, 'keep', { keep: !state.value?.keep })) throw new Error(props.workspace.error || '保留设置失败')
    await load()
  }
  catch (cause) { error.value = (cause as Error).message }
  finally { busy.value = false }
}
watch(() => JSON.stringify([
  props.task.id, props.task.archived, props.task.status,
  props.task.lifecycle?.keep, props.task.lifecycle?.archivedAt, props.task.lifecycle?.lastDeleteError,
  props.task.status === 'idle' ? props.task.lifecycle?.lastActivityAt : 0,
  props.task.queue?.length, props.task.pendingInteraction?.id,
  props.task.backgroundRuns?.map(run => [run.id, run.status]),
]), load, { immediate: true })
let revision = props.workspace.runtimeInfo.retentionRevision
const unsubscribe = props.workspace.subscribe(() => {
  const next = props.workspace.runtimeInfo.retentionRevision
  if (next !== revision) { revision = next; void load() }
})
onBeforeUnmount(() => { controller?.abort(); unsubscribe() })
</script>
<template>
  <div class="retention-status">
    <template v-if="state">
      <span v-if="state.archivedAt">归档于 {{ new Date(state.archivedAt).toLocaleDateString() }}</span>
      <span v-if="state.dueAt">预计{{ task.archived ? '删除' : '归档' }}：{{ new Date(state.dueAt).toLocaleDateString() }}</span>
      <span v-else>{{ state.blockedReason }}</span>
      <button type="button" :disabled="busy" @click.stop="toggleKeep">{{ state.keep ? '取消保留' : '保留对话' }}</button>
      <span v-if="state.lastDeleteError" role="alert">{{ state.lastDeleteError }}</span>
    </template>
    <span v-if="error" role="alert">{{ error }}</span>
  </div>
</template>
<style scoped>
.retention-status{display:flex;gap:6px;flex-wrap:wrap;font-size:var(--tuck-font-caption);color:var(--tuck-muted);overflow-wrap:anywhere}
.retention-status button{font:inherit;color:inherit;background:transparent;border:1px solid var(--tuck-border);border-radius:4px;padding:2px 6px;cursor:pointer}
.retention-status button:disabled{opacity:.5;cursor:wait}
</style>
