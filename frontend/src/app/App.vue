<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount, shallowRef, ref, reactive, watch, provide } from 'vue'
import { createEnterpriseWorkspace, workspaceKey } from '../shared/enterprise-workspace/state'
import WorkspaceDialog from '../shared/enterprise-workspace/WorkspaceDialog.vue'
import { cancelWorkspaceDialog, workspaceConfirm } from '../shared/enterprise-workspace/dialog'
import { useRoute } from 'vue-router'
import { DockerWorkspaceRuntime } from '../shared/workspace/runtime'
import ChatMode, { type ChatViewState } from '../modes/chat/ChatMode.vue'
import SettingsMode from '../modes/settings/SettingsMode.vue'
import TasksMode from '../modes/tasks/TasksMode.vue'
import WorkspaceMode from '../modes/workspace/WorkspaceMode.vue'
import { createTaskViewState } from '../modes/tasks/task-view'
import ModePlaceholder from '../modes/ModePlaceholder.vue'
import AppShell from './AppShell.vue'
import LoginPage from './LoginPage.vue'
import TuckLoading from '../shared/ui/TuckLoading.vue'
import TuckBrand from '../shared/ui/TuckBrand.vue'
import { loginSession, signOut, type LoginSession } from './auth'
import { initializeThemes } from './themes'
import { modes, router } from './router'
import { pageTitle } from './page-title'
import {settingsAccess} from './settings-access'
import './styles/focus.css'
import './styles/typography.css'
import '../shared/ui/dialog-tokens.css'
import './styles/select.css'
initializeThemes()
const route = useRoute()
const enterprise = createEnterpriseWorkspace()
provide(workspaceKey,enterprise)
const fileModes=new Set(['chat','workspace'])
const removeWorkspaceGuard = router.beforeEach((to,from) => fileModes.has(String(from.meta.mode)) && !fileModes.has(String(to.meta.mode)) && enterprise.dirty() ? workspaceConfirm('企业工作区有未保存修改，仍要离开当前频道？草稿将保留至本页面关闭。') : true)
function beforeUnload(event:BeforeUnloadEvent) { if(enterprise.dirty() || enterprise.state.uploading){event.preventDefault();event.returnValue=''} }
const emit = defineEmits<{ 'page-title': [value:{path:string;title:string}] }>()
const taskState = reactive(createTaskViewState())
const workspace = shallowRef<DockerWorkspaceRuntime | null>(null)
const error = ref(''), loading = ref(false), revision = ref(0)
const auth = ref<LoginSession | null>(null)
const chatState = reactive<ChatViewState>({ search:'', assistant:'', archivesOpen:false, diagnosticsOpen:new URLSearchParams(location.search).has('inspect'), listOffset:0, lastRoute:'' })
const mode = computed(() => modes.find(item => item.id === route.meta.mode))
const connection = computed(() => { void revision.value; return workspace.value?.runtimeInfo.connection })
// Configuration enables the channel; transient file-service availability only
// controls operations inside it. Older/disconnected projections omit the flag.
const workspaceEnabled=computed(()=>enterprise.state.projection?.serviceState==='ENABLED'||enterprise.state.projection?.serviceState===undefined&&enterprise.state.projection?.filesAvailable===true)
const workspaceReason=computed(()=>enterprise.state.projection?.filesReason||enterprise.state.error||'正在确认企业工作区状态')
const title = computed(() => { void revision.value; return pageTitle(route,workspace.value,auth.value?.authenticated) })
watch([() => route.fullPath,title], ([path,title]) => emit('page-title',{path,title}), {immediate:true})
let disposed = false, attempt = 0, unsubscribe = () => {}
async function connect(): Promise<void> {
  if (loading.value) return
  const current = ++attempt
  loading.value = true; error.value = ''
  try {
    const session = await loginSession()
    if (disposed || current !== attempt) return
    auth.value = session
    if (!session.authenticated) return
    const workspaceGeneration=enterprise.generation()
    if(route.meta.mode!=='workspace'&&!(route.meta.mode==='chat'&&route.query.ew==='1'))void enterprise.refresh().catch(cause=>{if(!disposed&&current===attempt&&workspaceGeneration===enterprise.generation()&&(cause as Error).name!=='AbortError')enterprise.state.error=(cause as Error).message})
    const owner = await DockerWorkspaceRuntime.connect(false)
    if (disposed || current !== attempt) owner.dispose()
    else { workspace.value = owner; unsubscribe = owner.subscribe(() => revision.value++) }
  } catch (reason) { if (current === attempt) error.value = (reason as Error).message }
  finally { if (current === attempt) loading.value = false }
}
function lock() { cancelWorkspaceDialog(); enterprise.reset(); attempt++; loading.value = false; unsubscribe(); workspace.value?.dispose(); workspace.value = null; auth.value = {enabled:true,authenticated:false}; error.value = ''; Object.assign(chatState,{search:'',assistant:'',lastRoute:'',listOffset:0}); Object.assign(taskState,createTaskViewState()) }
async function logout() { try { await signOut() } catch (cause) { window.alert((cause as Error).message) } }
async function connectionChanged() {
  cancelWorkspaceDialog(); enterprise.abort()
  const current=attempt,generation=enterprise.generation()
  try { await workspace.value?.refreshConnection() }
  catch(error) { if(!disposed&&current===attempt&&generation===enterprise.generation()&&(error as Error).name!=='AbortError')enterprise.unavailable((error as Error).message);return }
  if(disposed||current!==attempt||generation!==enterprise.generation())return
  // Navigation may change while the runtime reconnects. Validate the address
  // that is open now, and let a newer restore own its projection and errors.
  const files=route.meta.mode==='workspace'||route.meta.mode==='chat'&&route.query.ew==='1'
  try { await enterprise.refresh(files&&typeof route.query.space==='string'?route.query.space:undefined) }
  catch(error) {
    if(disposed||current!==attempt||(error as Error).name==='AbortError')return
    if((error as Error & {code?:string}).code==='workspace_link_changed'){
      enterprise.state.notice='工作区链接已更新，已返回当前工作区根目录'
      await router.replace({path:route.path,query:{...route.query,space:enterprise.state.projection?.agentSpaceId,dir:undefined,file:undefined,anchor:undefined,filter:undefined}})
    }else enterprise.unavailable((error as Error).message)
  }
}
function settingsAccessChanged(){if(workspace.value){workspace.value.runtimeInfo.connection=undefined;revision.value++;if(settingsAccess.ready)void workspace.value.refreshConnection().catch(()=>{})}}
onMounted(() => { window.addEventListener('tuck-settings-access-changed',settingsAccessChanged);window.addEventListener('beforeunload',beforeUnload); window.addEventListener('tuck-auth-expired',lock); void connect() })
onBeforeUnmount(() => { window.removeEventListener('tuck-settings-access-changed',settingsAccessChanged);enterprise.reset(); removeWorkspaceGuard(); window.removeEventListener('beforeunload',beforeUnload); disposed = true; attempt++; window.removeEventListener('tuck-auth-expired',lock); unsubscribe(); workspace.value?.dispose() })
</script>
<template>
  <LoginPage v-if="auth?.enabled && !auth.authenticated" @signed-in="connect" />
  <AppShell v-else-if="auth?.authenticated" :workspace-enabled="workspaceEnabled" :workspace-reason="workspaceReason">
    <ModePlaceholder v-if="!mode" title="页面不存在" error="请从左侧选择工作模式。" />
    <SettingsMode v-else-if="mode.id==='settings'" :connection="connection" :username="auth.username" :can-logout="auth.enabled" @changed="connectionChanged" @logout="logout" />
    <WorkspaceMode v-else-if="mode.id==='workspace'" />
    <ModePlaceholder v-else-if="!['chat','tasks'].includes(mode.id)" :title="mode.label" />
    <TasksMode v-else-if="workspace && mode.id === 'tasks'" :workspace="workspace" :state="taskState" />
    <ChatMode v-else-if="workspace" :workspace="workspace" :state="chatState" />
    <main v-else class="tuck-connect"><p v-if="error" role="alert">{{ error }}</p><TuckLoading v-else label="正在连接工作台…" /><button v-if="error" :disabled="loading" @click="connect">重新连接</button></main>
  </AppShell>
  <main v-else class="tuck-connect login-loading"><h1><TuckBrand /></h1><p v-if="error">{{ error }}</p><TuckLoading v-else label="正在连接工作台…" /><button v-if="error" :disabled="loading" @click="connect">重新连接</button></main>
  <WorkspaceDialog />
</template>
<style scoped>
.tuck-connect { height:100%; display:flex; flex-direction:column; align-items:center; justify-content:center; gap:16px; color:var(--tuck-text); background:var(--tuck-background); padding:20px; box-sizing:border-box; }.login-loading { height:100dvh; }button { padding:10px 20px; }.account-name { padding:8px; font-size:var(--tuck-font-caption); color:var(--tuck-muted); overflow:hidden; text-overflow:ellipsis; }
</style>
