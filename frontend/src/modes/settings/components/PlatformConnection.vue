<script setup lang="ts">
import TuckMark from '../../../shared/ui/TuckMark.vue'
import OrchelmMark from '../../../shared/ui/OrchelmMark.vue'
import { computed, ref, onBeforeUnmount } from 'vue'
import {platformModelPath,platformMcpPath,mcpStatus} from './catalog-paths'
import { type ConnectionInfo } from '../../../shared/workspace/runtime'
import {authFetch} from '../../../app/auth'
const controller=new AbortController()
onBeforeUnmount(()=>controller.abort())
async function api<T>(path:string,body:unknown):Promise<T>{const response=await authFetch(path,{method:'POST',signal:controller.signal,headers:{'Content-Type':'application/json'},body:JSON.stringify(body)});const value=await response.json();if(controller.signal.aborted)throw new DOMException('Aborted','AbortError');if(!response.ok)throw new Error(value.error||'平台操作失败');return value}
const props = defineProps<{ connection?: ConnectionInfo }>()
const platformModels=computed(()=>props.connection?.mode==='platform'?props.connection.models.filter(m=>m.source!=='local'):[])
const emit = defineEmits<{ changed: [connection: ConnectionInfo] }>()
const material = ref('')
const busy = ref(false)
const pendingAction = ref(''), notice = ref('')
const error = ref('')
const editingOrigin = ref(false), origin = ref('')
function editOrigin() { origin.value = props.connection?.origin || ''; editingOrigin.value = true; error.value = ''; notice.value = '' }
async function saveOrigin() {
  if (busy.value) return
  busy.value = true; pendingAction.value = 'origin'; error.value = ''; notice.value = ''
  try {
    const value = await api<ConnectionInfo>('/api/connection/origin', { origin: origin.value.trim() })
    emit('changed', value); editingOrigin.value = false
    if (value.error) error.value = value.error
    else notice.value = '连接地址已更新，配置已同步'
  } catch (cause) { error.value = (cause as Error).message }
  finally { busy.value = false; pendingAction.value = '' }
}
async function command(action: 'enroll' | 'sync' | 'logout'): Promise<void> {
  if (busy.value) return
  busy.value = true; pendingAction.value = action; error.value = ''; notice.value = ''
  try { const value = await api<ConnectionInfo>(`/api/connection/${action}`, action === 'enroll' ? { material: material.value } : {}); material.value = ''; emit('changed', value); if (value.error) error.value = value.error; else notice.value = {enroll:'企业平台已接入',sync:'配置已同步',logout:'已退出企业平台'}[action] } catch (cause) { error.value = (cause as Error).message } finally { busy.value = false; pendingAction.value = '' }
}
</script>
<template>
    <section class="tuck-platform-modal" :aria-busy="busy">
      <header class="platform-heading"><div class="platform-brand"><OrchelmMark /><div><h2>企业平台</h2><small>由 Orchelm 提供</small></div></div></header>
      <div class="platform-body">
      <div v-if="connection?.mode === 'platform'" class="platform-identity"><div><strong>{{ connection.deploymentName }}</strong><small>{{ connection.origin }}</small><span class="employee-name" data-testid="platform-employee">员工 · {{ connection.employeeName || '暂未获取姓名' }}</span></div><span class="connection-state" :class="connection.status === 'ready' ? 'ready' : 'attention'">{{ connection.status === 'ready' ? '已接入' : '需处理' }}</span></div>
      <p v-else class="platform-hint">{{ connection?.mode === 'direct' ? '当前使用本机模型，可粘贴 JSON 接入企业平台。' : '粘贴管理员提供的接入 JSON。' }}</p>
      <form v-if="connection?.mode === 'platform' && editingOrigin" class="origin-editor" @submit.prevent="saveOrigin">
        <label for="platform-origin">企业平台连接地址</label>
        <input id="platform-origin" v-model="origin" type="url" required :disabled="busy" placeholder="https://企业平台地址:端口" autocomplete="off" spellcheck="false" aria-describedby="origin-hint" />
        <p id="origin-hint" class="platform-hint">用于同一企业平台换网络或更换域名，保留当前员工身份，无需重新申请接入码。</p>
        <div class="tuck-platform-actions"><button :disabled="busy || !origin.trim()" type="submit">{{ pendingAction === 'origin' ? '连接中…' : '保存并连接' }}</button><button type="button" :disabled="busy" @click="editingOrigin = false">取消</button></div>
      </form>
      <div v-if="connection?.mode === 'platform' && !editingOrigin" class="tuck-platform-actions"><button :disabled="busy" @click="editOrigin">修改连接地址</button><button :disabled="busy" @click="command('sync')">{{ pendingAction === 'sync' ? '同步中…' : '同步配置' }}</button><button :disabled="busy" @click="command('logout')">{{ pendingAction === 'logout' ? '退出中…' : '退出企业平台' }}</button></div>
      <details :open="connection?.mode !== 'platform'" class="enrollment"><summary>{{ connection?.mode === 'platform' ? '更换企业平台接入' : '粘贴接入 JSON' }}</summary><label>接入 JSON<textarea v-model="material" :disabled="busy" data-testid="platform-json" rows="5" autocomplete="off" spellcheck="false" placeholder='{"formatVersion":1,"kind":"PLATFORM_ENROLLMENT","platformUrl":"https://…","code":"…","expiresAt":"…"}' /></label><button :disabled="busy || !material.trim()" data-testid="platform-enroll" @click="command('enroll')">{{ pendingAction === 'enroll' ? '接入中…' : '接入并同步' }}</button></details>
      <p v-if="error || connection?.error" class="tuck-error" role="alert">{{ error || connection?.error }}</p>
      <p v-if="busy || notice" class="connection-notice" role="status"><TuckMark v-if="busy" animated class="connection-spinner" />{{ busy ? ({origin:'正在验证地址并同步配置…',enroll:'正在接入企业平台…',sync:'正在同步配置…',logout:'正在退出企业平台…'} as Record<string,string>)[pendingAction] : notice }}</p>
      <section v-if="platformModels.length" class="model-section"><h3>平台模型 <small>{{ platformModels.length }}</small></h3>
       <ul class="tuck-model-catalog"><li v-for="model in platformModels" :key="model.modelId"><RouterLink class="catalog-link" :to="model.providerId?platformModelPath({providerId:model.providerId,modelId:model.modelId}):'/settings/providers'" :aria-label="`查看平台模型 ${model.displayName}`"><div><strong>{{model.displayName}}</strong><small>平台下发 · 可配置本地参数 · {{model.selectable?'可用':model.reason||'不可用'}}</small></div><span aria-hidden="true">›</span></RouterLink></li></ul>
      </section>
      <section v-if="connection?.mode === 'platform'" class="mcp-catalog"><h3>企业 MCP <small>{{ connection.mcp?.length || 0 }}</small></h3>
       <p v-if="!connection.mcp?.length" class="platform-hint">暂无服务</p>
       <ul class="tuck-model-catalog"><li v-for="server in connection.mcp" :key="server.mcpServerId"><RouterLink class="catalog-link" :to="platformMcpPath(server.mcpServerId)" :aria-label="`查看平台 MCP ${server.displayName}`"><div><strong>{{server.displayName}}</strong><small>平台下发 · 可配置工具选择 · {{mcpStatus(server.status)}}</small></div><span aria-hidden="true">›</span></RouterLink></li></ul>
      </section>
      </div>
    </section>
</template>
<style scoped>
.tuck-platform-modal { display:flex; flex-direction:column; min-height:0; max-height:inherit; }
.platform-heading { display:flex; align-items:center; justify-content:space-between; gap:12px; flex:none; padding:12px 16px; border-bottom:1px solid var(--tuck-border); }
.platform-heading h2 { margin:0; }
.platform-brand { display:flex; align-items:center; gap:8px; min-width:0; }
.platform-brand > svg { width:32px; height:32px; flex:none; color:var(--tuck-accent); }
.platform-brand small { color:var(--tuck-muted); }
.employee-name { margin-top:4px; overflow-wrap:anywhere; font-size:var(--tuck-font-ui); }
.platform-body { min-height:0; overflow:auto; overscroll-behavior:contain; padding:12px 16px 16px; display:flex; flex-direction:column; gap:12px; }
.platform-identity { display:flex; align-items:flex-start; justify-content:space-between; gap:12px; }
.platform-identity > div { min-width:0; display:grid; gap:3px; }
.platform-identity small { overflow-wrap:anywhere; }
.connection-state { flex:none; font-size:var(--tuck-font-caption); }.ready { color:var(--tuck-success); }.attention { color:var(--tuck-warning); }
.platform-hint { color:var(--tuck-muted); font-size:var(--tuck-font-caption); }
h3 { margin:0; font-size:var(--tuck-font-ui); font-weight:600; } h3 small { margin-left:4px; font-weight:400; }
.model-section { display:grid; gap:8px; }
.connection-notice { display:flex; align-items:center; gap:8px; color:var(--tuck-muted); font-size:var(--tuck-font-ui); }
.connection-spinner { width:16px; height:16px; color:var(--tuck-accent); }


h2,p { margin:0; }
.tuck-error { color: var(--tuck-danger,#ff9b94); white-space: pre-wrap; font-size:var(--tuck-font-ui); }
textarea { width: 100%; resize: vertical; font: 12px/1.5 monospace; padding: 10px; background: var(--tuck-background,#0d141c); color: var(--tuck-text,#e3eaf1); border: 1px solid var(--tuck-border,#30404f); border-radius: 8px; box-sizing: border-box; }
.tuck-platform-actions { display: flex; flex-wrap: wrap; gap: 8px; }
.origin-editor { display:grid; gap:8px; padding:12px; border:1px solid var(--tuck-border); border-radius:8px; }
.origin-editor label { margin:0; }
.origin-editor input { width:100%; min-width:0; box-sizing:border-box; }
.tuck-model-catalog { list-style: none; padding: 0; display: grid; gap: 8px; }
.tuck-model-catalog li { display: grid; gap: 8px; padding: 10px; border: 1px solid var(--tuck-border,#354152); border-radius: 8px; }
.tuck-model-catalog small { overflow-wrap: anywhere; }
.tuck-model-catalog { margin:0; }
.mcp-catalog { display:grid; gap:10px; }
.enrollment summary { cursor:pointer; color:var(--tuck-muted); font-size:var(--tuck-font-caption); }
.enrollment[open] summary { margin-bottom:8px; }
.catalog-link{display:flex;align-items:center;justify-content:space-between;gap:12px;color:var(--tuck-text);text-decoration:none}.catalog-link>div{display:grid;gap:5px;min-width:0;overflow-wrap:anywhere}.catalog-link:hover{color:var(--tuck-accent)}
</style>
