<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { answerWorkspaceDialog, cancelWorkspaceDialog, workspaceDialogState } from './dialog'

const dialog = ref<HTMLDialogElement>()
const input = ref<HTMLInputElement>(), select = ref<HTMLSelectElement>(), confirm = ref<HTMLButtonElement>()
let disposed = false

watch(() => workspaceDialogState.current?.id, async id => {
  await nextTick()
  if (disposed || workspaceDialogState.current?.id !== id) return
  if (id === undefined) {
    dialog.value?.close()
    return
  }
  if (!dialog.value?.open) dialog.value?.showModal()
  const current = workspaceDialogState.current!
  if (current.kind === 'prompt') { input.value?.focus(); input.value?.select() }
  else if (current.kind === 'conflict') select.value?.focus()
  else confirm.value?.focus()
}, {immediate:true, flush:'post'})

function submit() {
  const current = workspaceDialogState.current
  if (current) answerWorkspaceDialog(current.id, current.value)
}

onBeforeUnmount(() => { disposed = true; cancelWorkspaceDialog(); dialog.value?.close() })
</script>

<template>
  <Teleport to="body">
    <dialog ref="dialog" class="workspace-dialog" aria-labelledby="workspace-dialog-title" @cancel.prevent="cancelWorkspaceDialog">
      <form v-if="workspaceDialogState.current" @submit.prevent="submit">
        <h2 id="workspace-dialog-title">{{workspaceDialogState.current.message}}</h2>
        <p v-if="workspaceDialogState.current.detail" id="workspace-dialog-detail">{{workspaceDialogState.current.detail}}</p>
        <input v-if="workspaceDialogState.current.kind === 'prompt'" ref="input" v-model="workspaceDialogState.current.value" :aria-label="workspaceDialogState.current.message" autocomplete="off" />
        <label v-else-if="workspaceDialogState.current.kind === 'conflict'">同名处理方式
          <select ref="select" v-model="workspaceDialogState.current.value" aria-label="同名处理方式" :aria-describedby="workspaceDialogState.current.detail?'workspace-dialog-detail':undefined">
            <option value="副本">保留两份（副本）</option>
            <option value="跳过">跳过</option>
            <option value="替换" :disabled="!workspaceDialogState.current.allowReplace">覆盖文件</option>
          </select>
        </label>
        <footer><button type="button" @click="cancelWorkspaceDialog">取消</button><button ref="confirm" type="submit" class="primary">确定</button></footer>
      </form>
    </dialog>
  </Teleport>
</template>

<style scoped>
.workspace-dialog{width:min(440px,calc(100vw - 32px));box-sizing:border-box;max-height:calc(100dvh - 32px);overflow:auto;padding:var(--tuck-dialog-padding);border:1px solid var(--tuck-border);border-radius:var(--tuck-dialog-radius);background:var(--tuck-surface);color:var(--tuck-text);box-shadow:0 16px 64px #0004}
.workspace-dialog::backdrop{background:#0008}
form>p{margin:0;font-size:13px;line-height:1.6;color:var(--tuck-muted);overflow-wrap:anywhere}
form{display:flex;flex-direction:column;gap:var(--tuck-dialog-gap)}h2{margin:0;font-size:var(--tuck-font-title);line-height:var(--tuck-line-title);overflow-wrap:anywhere}label{display:flex;flex-direction:column;gap:8px;font-size:14px}input,select,button{font:inherit;color:inherit}input,select{width:100%;box-sizing:border-box;min-height:var(--tuck-dialog-control-height);padding:5px 8px;border:1px solid var(--tuck-border);border-radius:6px;background:var(--tuck-background)}footer{display:flex;justify-content:flex-end;gap:10px}button{min-height:var(--tuck-dialog-control-height);padding:5px 10px;border:1px solid var(--tuck-border);border-radius:6px;background:var(--tuck-raised);cursor:pointer}.primary{color:var(--tuck-accent);border-color:var(--tuck-accent)}
</style>
