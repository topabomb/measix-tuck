<script setup lang="ts">
import TuckLoading from '../../../shared/ui/TuckLoading.vue'
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { api, type DockerTask } from '../../../shared/workspace/runtime'
import { authFetch } from '../../../app/auth'
import { appUrl } from '../../../app/urls'
import WorkbenchDialog from '../../../shared/ui/WorkbenchDialog.vue'
import SummaryPopover from '../../../shared/ui/SummaryPopover.vue'
const props = defineProps<{ task: DockerTask; shared?: boolean; trigger?:HTMLElement|null }>()
const emit = defineEmits<{ close: [] }>()
const files = ref(props.task.artifacts), loading = ref(false), search = ref(''), page = ref(0)
const matches = computed(() => files.value.filter(file => file.path.toLowerCase().includes(search.value.toLowerCase())))
const visible = computed(() => matches.value.slice(page.value * 40, (page.value + 1) * 40))
watch(search, () => { page.value = 0 })
watch(() => props.task.artifacts, value => { files.value = value })
watch(() => matches.value.length, count => { page.value = Math.min(page.value, Math.max(0, Math.ceil(count / 40) - 1)) })
const preview = ref<{ path:string; content:string } | null>(null), original = ref(''), editing = ref(false), discard = ref(false), busy = ref(false), error = ref('')
const previewText = ref<HTMLTextAreaElement | null>(null)
let controller: AbortController | null = null
let disposed = false
const dirty = computed(() => preview.value?.content !== original.value)
const url = (path: string) => appUrl(`/api/tasks/${props.task.id}/files?path=${encodeURIComponent(path)}`)
const size = (bytes: number) => bytes < 1024 ? `${bytes} B` : bytes < 1024*1024 ? `${(bytes/1024).toFixed(1)} KB` : `${(bytes/1024/1024).toFixed(1)} MB`
async function refresh() {
  if (loading.value) return
  loading.value = true; error.value = ''
  try { const result = await api<DockerTask['artifacts']>(`/api/tasks/${props.task.id}/files`); if (!disposed) files.value = result }
  catch (cause) { if (!disposed) error.value = (cause as Error).message }
  finally { loading.value = false }
}
onMounted(refresh)
onBeforeUnmount(() => { disposed = true; controller?.abort() })
async function openFile(file: DockerTask['artifacts'][number]) {
  controller?.abort(); controller = new AbortController(); const request = controller
  error.value = ''
  if (/\.(png|jpe?g|gif|webp|mp[34]|m4a|wav|ogg|webm|pdf)$/i.test(file.path)) { window.open(url(file.path),'_blank','noopener'); return }
  if (file.size > 2 * 1024 * 1024 || /\.(docx?|xlsx?|pptx?|zip|7z|gz|exe|dll|db|sqlite|bin)$/i.test(file.path)) { error.value = '此文件请下载后查看；文本预览最多 2 MB。'; return }
  try {
    const response = await authFetch(url(file.path) + '&preview=1', { signal: request.signal })
    if (!response.ok) throw new Error(await response.text())
    const content = await response.text()
    if (content.includes('\0')) throw new Error('二进制文件请下载后查看。')
    if (!disposed && !request.signal.aborted) {
      original.value = content; editing.value = false; discard.value = false; preview.value = {path:file.path,content}
      await nextTick()
      if (previewText.value) { previewText.value.setSelectionRange(0,0); previewText.value.scrollTop = 0 }
    }
  } catch (cause) { if (!request.signal.aborted && !disposed) error.value = (cause as Error).message }
}
function closePreview() { if (busy.value) return; if (dirty.value) discard.value = true; else preview.value = null }
async function save() {
  if (!preview.value || busy.value) return
  const submitted = { ...preview.value }
  busy.value = true; error.value = ''
  try { await api(`/api/tasks/${props.task.id}/save-file`,submitted); if (disposed) return; original.value = submitted.content; editing.value = dirty.value; discard.value = false; await refresh() }
  catch (cause) { error.value = (cause as Error).message }
  finally { busy.value = false }
}
</script>
<template>
  <SummaryPopover v-if="!preview" :open="true" :trigger="trigger" label="任务产物" close-label="关闭文件" @update:open="!$event && !preview && emit('close')">
    <section class="tuck-files-panel" aria-label="任务产物">
      <p v-if="shared" class="shared-note">父子会话共享工作目录，以下文件不代表由当前子代理独自产出。</p>
      <div class="artifact-toolbar"><details><summary>工作目录{{ task.workspaceMode==='none' ? ' · 原生默认' : '' }}</summary><p class="file-directory">{{ task.workspace || '外部运行时默认目录未知' }}</p></details><small class="artifact-count">{{ files.length }} 项</small><button :disabled="loading" @click="refresh"><TuckLoading v-if="loading" label="读取中…" /><template v-else>刷新</template></button></div>
      <p v-if="task.workspaceMode==='none'" class="shared-note">此会话未指定目录，不自动枚举运行时目录。已提交的成果请在“交付”中查看。</p>
      <input v-if="files.length > 8 || search" v-model="search" aria-label="筛选产物" placeholder="筛选文件名…" />
      <p v-if="error" role="alert">{{ error }}</p>
      <div class="artifact-list">
        <div v-for="file in visible" :key="file.path" class="artifact-item">
          <button class="tuck-file-row" :title="`查看 ${file.path}`" @click="openFile(file)"><span class="file-name">{{ file.path }}</span><small>{{ size(file.size) }} · {{ new Date(file.modified).toLocaleString() }}</small></button>
          <a :href="url(file.path)+'&download=1'" :download="file.path" :aria-label="`下载 ${file.path}`">下载</a>
        </div>
        <p v-if="!visible.length" class="empty-artifacts">{{ loading ? '正在读取产物…' : search ? '没有匹配的文件' : '暂无产物文件' }}</p>
      </div>
      <footer v-if="matches.length > 40"><button :disabled="page === 0" @click="page--">上一页</button><span>{{ page + 1 }} / {{ Math.ceil(matches.length / 40) }}</span><button :disabled="(page+1)*40 >= matches.length" @click="page++">下一页</button></footer>
    </section>
  </SummaryPopover>
  <WorkbenchDialog v-else label="查看任务产物" wide preview :busy="busy" :dismiss-on-backdrop="!dirty" @close="closePreview">
    <form class="tuck-editor" @submit.prevent="save"><header class="preview-heading"><h2 :title="preview.path">{{ preview.path }}</h2><button type="button" aria-label="关闭文件预览" :disabled="busy" @click="closePreview">×</button></header><textarea ref="previewText" v-model="preview.content" aria-label="文件内容" :readonly="!editing" :tabindex="editing ? 0 : -1" spellcheck="false" /><p v-if="error" role="alert">{{ error }}</p>
      <div v-if="discard" class="discard-prompt" role="alert">有未保存的修改。<button type="button" @click="discard = false">继续编辑</button><button type="button" @click="preview = null">放弃修改</button></div>
      <div class="preview-footer"><button v-if="!editing" type="button" @click="editing = true">编辑</button><button v-else type="submit" :disabled="busy || !dirty">保存文件</button></div>
    </form>
  </WorkbenchDialog>
</template>
<style scoped>
.tuck-files-panel { min-width:0; display:grid; gap:8px; }
.tuck-files-panel button{font:inherit;color:var(--tuck-text);cursor:pointer}.tuck-files-panel button:disabled{opacity:.45;cursor:default}.tuck-files-panel input{width:100%;min-width:0;box-sizing:border-box;padding:6px 8px;border:1px solid var(--tuck-border);border-radius:6px;background:var(--tuck-background);color:var(--tuck-text);font:inherit}.tuck-files-panel [role=alert]{margin:0;color:var(--tuck-danger);overflow-wrap:anywhere}
.artifact-toolbar,footer { display:flex; align-items:center; justify-content:space-between; gap:12px; }
.shared-note,.file-directory { margin:0; font-size:var(--tuck-font-ui); color:var(--tuck-muted); overflow-wrap:anywhere; }
.artifact-toolbar button,footer button{min-height:32px;padding:4px 8px;border:1px solid var(--tuck-border);border-radius:6px;background:var(--tuck-raised)}.artifact-count{color:var(--tuck-muted);white-space:nowrap}.artifact-toolbar details { min-width:0; flex:1; }.artifact-toolbar summary { cursor:pointer; font-size:var(--tuck-font-ui); color:var(--tuck-muted); }.file-directory { margin-top:8px; }
.artifact-list { max-height:min(50dvh,420px); overflow:auto; overscroll-behavior:contain; }
.artifact-item { display:flex; gap:10px; align-items:center; border-bottom:1px solid var(--tuck-border); padding:4px 0; }
.tuck-files-panel .tuck-file-row { display:grid; gap:4px; flex:1; min-width:0; padding:8px; border:0; text-align:left; background:transparent; }
.file-name { min-width:0; overflow-wrap:anywhere; font-size:14px; }.tuck-file-row small { font-size:var(--tuck-font-caption); color:var(--tuck-muted); }
.artifact-item a { color:var(--tuck-accent); text-decoration:none; padding:8px; font-size:var(--tuck-font-ui); flex-shrink:0; }.artifact-item a:hover { text-decoration:underline; }
.empty-artifacts { margin:8px 0; text-align:center; color:var(--tuck-muted); font-size:var(--tuck-font-ui); }
.tuck-editor { display:flex; flex-direction:column; min-height:0; max-height:calc(100dvh - 34px); overflow:hidden; }
.tuck-editor > :not(textarea) { flex:none; }
.tuck-editor textarea { display:block; flex:1 1 auto; min-height:0; height:48dvh; width:100%; font:12px/1.6 monospace; resize:none; }
.discard-prompt { display:flex; gap:8px; flex-wrap:wrap; align-items:center; margin-top:10px; }
</style>
