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
        <legend>普通对话</legend>
        <label><input v-model="policy.conversations.autoArchive" type="checkbox" />自动归档</label>
        <label>无活动天数<input v-model.number="policy.conversations.inactiveDays" type="number" min="1" max="36500" required /></label>
        <label><input v-model="policy.conversations.autoDelete" type="checkbox" />永久删除已过期归档</label>
        <label>归档后保留天数<input v-model.number="policy.conversations.archivedDays" type="number" min="1" max="36500" required /></label>
      </fieldset>
      <fieldset :disabled="busy || !!preview">
        <legend>临时执行记录</legend>
        <label><input v-model="policy.temporaryRuns.autoDelete" type="checkbox" />永久删除已过期临时记录</label>
        <label>归档后保留天数<input v-model.number="policy.temporaryRuns.archivedDays" type="number" min="1" max="36500" required /></label>
        <p>仅适用于明确登记为临时用途的执行，普通对话不受此项影响。</p>
      </fieldset>
      <fieldset :disabled="busy || !!preview">
        <legend>自动工作目录</legend>
        <label><input v-model="policy.managedWorkspaces.autoCleanup" type="checkbox" />纳管新建自动目录并回收无引用目录</label>
        <p>最后一个引用对话删除后，允许回收已纳管目录中的全部工作文件。历史未纳管目录、指定目录及远程文件保留。</p>
      </fieldset>
      <p>归档可以恢复；永久删除将移除历史、原生上下文、统计及交付。标记保留、执行中或等待交互的对话不会自动清理。</p>
      <div class="actions"><button type="submit" :disabled="busy || !!preview">保存保留设置</button><button type="button" :disabled="busy" @click="load">重新读取</button></div>
    </form>
    <button v-else type="button" :disabled="busy" @click="load">读取保留设置</button>
    <p v-if="saved" role="status">保留设置已保存并生效。</p>
    <p v-if="error" role="alert">{{ error }}</p>
    <details v-if="pending.length"><summary>尚未完成回收的目录（{{ pending.length }}）</summary><p v-for="item in pending" :key="item.id">{{ item.path }}：{{ item.error || (item.state === 'allocating' ? '创建尚未确认，保留目录待核对' : '等待按当前策略清理') }}</p></details>
    <WorkbenchDialog v-if="preview" label="确认自动清理" :busy="busy" :dismiss-on-backdrop="false" @close="preview = undefined">
      <h2>确认自动清理策略？</h2>
      <p>当前预览有 {{ preview.deleteCandidates }} 个对话符合删除时间条件。执行时会再次核对，后续到期对象也将自动处理。</p>
      <p v-if="preview.pendingDirectories">已登记 {{ preview.pendingDirectories }} 个待回收目录；实际清理仍须通过引用、在途使用和目录身份检查。</p>
      <p>永久删除无法撤销；开启目录回收后，已纳管且无引用的自动目录及全部内容也会清理。</p>
      <div class="actions"><button type="button" :disabled="busy" @click="preview = undefined">取消</button><button type="button" :disabled="busy" @click="save(preview.confirmation)">确认保存策略</button></div>
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
