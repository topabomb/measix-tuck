<script setup lang="ts">
import { onMounted, onBeforeUnmount, ref } from 'vue'
import { authFetch } from '../../../app/auth'
import WorkbenchDialog from '../../../shared/ui/WorkbenchDialog.vue'

interface Retention {
  conversations: { autoArchive: boolean; inactiveDays: number; autoDelete: boolean; archivedDays: number }
  temporaryRuns: { autoDelete: boolean; archivedDays: number }
  managedWorkspaces: { autoCleanup: boolean }
}
interface Preview { confirmation: string; requiresConfirmation: boolean; archiveCandidates: number; deleteCandidates: number; pendingDirectories: number }
const controller = new AbortController()
const policy = ref<Retention>(), revision = ref(''), busy = ref(false), error = ref(''), saved = ref(false)
const preview = ref<Preview>()
const pending = ref<{ id: string; path: string; state: string; error: string }[]>([])
onBeforeUnmount(() => controller.abort())

async function request(method: string, body?: unknown) {
  const response = await authFetch('/api/settings/retention', {
    method, signal: controller.signal, headers: { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const value = await response.json()
  if (!response.ok) throw new Error(value.error?.message || value.message || '保留设置请求失败，请重新读取后核对')
  return value
}
async function load() {
  busy.value = true; error.value = ''; saved.value = false; preview.value = undefined
  try {
    const value = await request('GET')
    policy.value = value.retention; revision.value = value.revision; pending.value = value.pendingWorkspaces
  } catch (cause) { error.value = (cause as Error).message }
  finally { busy.value = false }
}
async function save(confirmation?: string) {
  busy.value = true; error.value = ''
  try {
    const value = await request('PUT', { retention: policy.value, revision: revision.value, confirmation })
    revision.value = value.revision; saved.value = true; preview.value = undefined
  } catch (cause) { error.value = (cause as Error).message; preview.value = undefined }
  finally { busy.value = false }
}
async function review() {
  busy.value = true; error.value = ''; saved.value = false
  try {
    const value: Preview = await request('POST', { retention: policy.value, revision: revision.value })
    if (value.requiresConfirmation) preview.value = value
    else await save()
  } catch (cause) { error.value = (cause as Error).message }
  finally { busy.value = false }
}
onMounted(load)
</script>

<template>
  <section class="retention-settings">
    <h3>归档与保留</h3>
    <form v-if="policy" @submit.prevent="review">
      <fieldset :disabled="busy || !!preview">
        <legend>对话记录</legend>
        <label><input v-model="policy.conversations.autoArchive" type="checkbox" />长时间未使用时自动归档</label>
        <label>多少天无活动后归档<input v-model.number="policy.conversations.inactiveDays" type="number" min="1" max="36500" required /></label>
        <p>从最后一次消息或执行活动开始计时，仅打开查看不会延长时间。归档会保留历史和文件，可以恢复后继续对话。</p>
        <label><input v-model="policy.conversations.autoDelete" type="checkbox" />归档到期后自动永久删除</label>
        <label>归档后保留天数<input v-model.number="policy.conversations.archivedDays" type="number" min="1" max="36500" required /></label>
        <p>永久删除会移除聊天历史、继续对话所需的上下文、统计和交付副本，无法恢复。</p>
      </fieldset>
      <fieldset :disabled="busy || !!preview">
        <legend>工作文件夹</legend>
        <label><input v-model="policy.managedWorkspaces.autoCleanup" type="checkbox" />删除对话时清理自动工作文件夹</label>
        <p>只适用于开启后新建、由 Tuck 自动创建的工作文件夹。删除对话后，会删除整个文件夹及其中全部文件；其他对话或分支仍在共用时保留。</p>
        <p>归档不会删除文件。此选项关闭时创建的文件夹、你自己指定的文件夹及远程工作区文件会保留。</p>
        <p>关闭此项会暂停清理；再次开启后，会继续处理此前尚未清理的文件夹。</p>
      </fieldset>
      <p>标记为“保留”的对话不会自动归档或删除；执行中、等待回复或审批、有排队消息时也会暂停自动处理。</p>
      <div class="actions"><button type="submit" :disabled="busy || !!preview">保存设置</button><button type="button" :disabled="busy" @click="load">重新读取</button></div>
    </form>
    <button v-else type="button" :disabled="busy" @click="load">读取设置</button>
    <p v-if="saved" role="status">归档与清理设置已生效。</p>
    <p v-if="error" role="alert">{{ error }}</p>
    <details v-if="pending.length"><summary>尚未清理的工作文件夹（{{ pending.length }}）</summary><p v-for="item in pending" :key="item.id">{{ item.path }}：{{ item.error || (item.state === 'allocating' ? '创建结果尚未确认，暂时保留，请核对' : '等待按当前设置清理') }}</p></details>
    <WorkbenchDialog v-if="preview" label="确认自动删除" :busy="busy" :dismiss-on-backdrop="false" @close="preview = undefined">
      <h2>允许按这些设置自动删除吗？</h2>
      <p v-if="policy?.conversations.autoDelete">当前有 {{ preview.deleteCandidates }} 个对话符合自动永久删除条件。保存后，后续到期的归档对话也会自动删除。</p>
      <p v-if="policy?.managedWorkspaces.autoCleanup">开启后新建的自动工作文件夹，会在最后一个共用对话删除后，连同其中全部文件一起删除。</p>
      <p v-if="preview.pendingDirectories">此前还有 {{ preview.pendingDirectories }} 个工作文件夹等待清理；确认后也会检查它们是否可以删除。</p>
      <p>永久删除无法撤销。执行前会再次检查保留标记、运行状态，以及文件夹是否仍被其他对话使用。</p>
      <div class="actions"><button type="button" :disabled="busy" @click="preview = undefined">取消</button><button type="button" :disabled="busy" @click="save(preview.confirmation)">确认保存</button></div>
    </WorkbenchDialog>
  </section>
</template>

<style scoped>
.retention-settings,.retention-settings form{display:grid;gap:12px}
h3,p{margin:0}p{color:var(--tuck-muted);overflow-wrap:anywhere}
.retention-settings fieldset{display:grid;gap:10px;min-width:0;border:1px solid var(--tuck-border);border-radius:8px;padding:12px;margin:0}
.retention-settings label{display:flex;align-items:center;gap:8px;margin:0}
.retention-settings input[type=checkbox]{width:16px;height:16px;flex:none;margin:0;accent-color:var(--tuck-accent)}
.retention-settings input[type=number]{width:100px;min-width:0;margin-left:auto}
.actions{display:flex;gap:8px;flex-wrap:wrap}
</style>
