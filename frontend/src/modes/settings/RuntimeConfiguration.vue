<script setup lang="ts">
import {computed,onMounted,onBeforeUnmount,ref,watch,nextTick} from 'vue'
import {useRoute} from 'vue-router'
import {router} from '../../app/router'
import {authFetch} from '../../app/auth'
import {settingsAccess,settingsLocked} from '../../app/settings-access'
import {normalizeSettings,editableConfig} from './runtime-config-access'
import {workspaceConfirm} from '../../shared/enterprise-workspace/dialog'
import ProvidersEditor from './components/ProvidersEditor.vue'
import AgentToolsEditor from './components/AgentToolsEditor.vue'
import AgentList from './components/AgentList.vue'
import AgentEditor from './components/AgentEditor.vue'
import McpEditor from './components/McpEditor.vue'
import type {Settings} from './components/types'
import type {ConnectionInfo} from '../../shared/workspace/runtime'
const props=defineProps<{connection?:ConnectionInfo}>()
const emit=defineEmits<{changed:[]}>(),route=useRoute()
const data=ref<Settings>(),baseline=ref(''),busy=ref(false),error=ref(''),notice=ref(''),confirmInterrupt=ref(false)
const dirty=computed(()=>!!data.value&&JSON.stringify(data.value.config)!==baseline.value)
const runtimeSections=['providers','agents','mcp']
const body=ref<HTMLElement>()
watch(()=>route.path,async()=>{await nextTick();if(body.value)body.value.scrollTop=0})
const controller=new AbortController(),generation=settingsAccess.generation
const active=()=>!controller.signal.aborted&&generation===settingsAccess.generation
watch(()=>props.connection,connection=>{if(!data.value||!connection||data.value.lockedSections?.includes('mcp'))return;data.value.localMcpState=Object.fromEntries((connection.localMcp||[]).map(m=>[m.name,m]));for(const managed of data.value.managedMcp){const current=connection.mcp?.find(m=>m.mcpServerId===managed.mcpServerId);if(current)Object.assign(managed,current)}})
async function load(){busy.value=true;error.value='';try{const r=await authFetch('/api/settings/runtime?section='+route.meta.settingsSection,{signal:controller.signal});const value=await r.json();if(!active())return;if(!r.ok)throw new Error(value.error||'配置读取失败');data.value=normalizeSettings(value);baseline.value=JSON.stringify(data.value.config)}catch(e){if(active())error.value=(e as Error).message}finally{if(active())busy.value=false}}
async function save(action:'apply'|'interrupt'|'defer'='apply'){
 if(busy.value||!data.value)return
 busy.value=true;error.value='';notice.value=''
 try{const r=await authFetch('/api/settings/runtime',{method:'PUT',signal:controller.signal,headers:{'Content-Type':'application/json'},body:JSON.stringify({revision:data.value.revision,config:editableConfig(data.value),interrupt:action==='interrupt',defer:action==='defer'})});const value=await r.json();if(!active())return;if(!r.ok){if(value.code==='settings_interrupt_required'){confirmInterrupt.value=true;return}throw new Error(value.error||'配置保存失败')}
 data.value=normalizeSettings(value);baseline.value=JSON.stringify(data.value.config);confirmInterrupt.value=false;notice.value=value.pending?'已保存，尚未生效。可在下一次保存或重启后应用。':'已保存并生效';if(!value.pending)emit('changed')
 }catch(e){if(active())error.value=(e as Error).message}finally{if(active())busy.value=false}
}
const removeGuard=router.beforeEach(async(to,from)=>{
 if(!active()||to.fullPath===from.fullPath)return true
 if(busy.value)return false
 if(to.meta.mode==='settings'&&runtimeSections.includes(String(to.meta.settingsSection))&&!settingsLocked(String(to.meta.settingsSection)))return true
 return !dirty.value||await workspaceConfirm('配置尚未保存，离开将丢弃本次修改。仍要离开？')
})
function beforeUnload(e:BeforeUnloadEvent){if(dirty.value||busy.value){e.preventDefault();e.returnValue=''}}
onMounted(()=>{window.addEventListener('beforeunload',beforeUnload);void load()})
onBeforeUnmount(()=>{controller.abort();removeGuard();window.removeEventListener('beforeunload',beforeUnload)})
</script>
<template>
 <section class="runtime-config" aria-label="运行配置" :aria-busy="busy">
  <div ref="body" class="config-body">
   <p v-if="!data&&!error">正在读取配置…</p>
   <fieldset v-if="data" :disabled="busy" @input="notice='';confirmInterrupt=false" @change="notice='';confirmInterrupt=false" @click="notice=''">
    <p v-if="data.pending" class="hint">已保存的配置尚未生效；当前任务继续使用原配置。再次保存或重启后应用。</p>
    <ProvidersEditor v-if="route.meta.settingsSection==='providers'" :key="route.path" :data="data" />
    <AgentList v-else-if="route.meta.settingsSection==='agents'&&!route.params.agent" :data="data" />
    <AgentToolsEditor v-else-if="route.meta.settingsSection==='agents'&&route.params.tool" :key="route.fullPath" :data="data" :name="String(route.params.agent)" />
    <AgentEditor v-else-if="route.meta.settingsSection==='agents'" :data="data" />
    <McpEditor v-else :key="route.path" :data="data" />
   </fieldset>
  </div>
  <footer v-if="!route.meta.managed || route.params.managedModel || route.params.managedMcp || dirty || data?.pending || error || notice || !data" class="config-footer">
   <p v-if="error" role="alert">{{ error }}</p><p v-if="notice" role="status">{{ notice }}</p>
   <div v-if="confirmInterrupt" class="interrupt-confirm"><p>当前有任务进行中。中断后应用新配置，任务不会自动恢复；也可以仅保存，暂不生效。</p><div class="line end"><button :disabled="busy" @click="confirmInterrupt=false">取消</button><button :disabled="busy" @click="save('defer')">保存，暂不生效</button><button :disabled="busy" @click="save('interrupt')">中断任务并保存</button></div></div>
   <div v-else class="line"><small>{{ data?.lockedSections?.length?(dirty?'有未保存修改 · 当前可编辑的配置一起保存':'保存当前可编辑的配置，不修改已锁定分类'):(dirty?'有未保存修改 · 提供商、Agent 和 MCP 一起保存':'提供商、Agent 和 MCP 共用现有配置文件') }}</small><button v-if="!data" :disabled="busy" @click="load">重试</button><button v-else class="primary" :disabled="busy" @click="save()">{{ busy?'正在保存…':'保存并生效' }}</button></div>
  </footer>
 </section>
</template>
