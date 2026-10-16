<script setup lang="ts">
import {computed,ref} from 'vue'
import {useRoute} from 'vue-router'
import {router} from '../../../app/router'
import ToolPicker from './ToolPicker.vue'
import {platformMcpPath,mcpStatus} from './catalog-paths'
import type {Settings,Mcp,McpState} from './types'
const props=defineProps<{data:Settings}>(),route=useRoute(),idDraft=ref<string>()
const m=computed(()=>props.data.config.localMcp.find(m=>m.name===route.params.mcp))
const managed=computed(()=>props.data.managedMcp.find(m=>m.mcpServerId===route.params.managedMcp))
const agentNames=computed(()=>props.data.agentNames??Object.keys(props.data.config.agents||{}))
const path=(m:Mcp)=>'/settings/mcp/'+encodeURIComponent(m.name)
function add(){const m:Mcp={name:'mcp_'+Date.now().toString(36),displayName:'新 MCP',enabled:true,url:'',agents:['root']};props.data.config.localMcp.push(m);void router.push(path(m))}
function rename(event:Event){const value=(event.target as HTMLInputElement).value.trim();if(!m.value||!/^[A-Za-z0-9_-]{1,32}$/.test(value)||props.data.config.localMcp.some(item=>item!==m.value&&item.name===value)){idDraft.value=undefined;return}if(value===m.value.name)return;m.value.name=value;void router.replace('/settings/mcp/'+encodeURIComponent(value))}
function remove(){if(m.value)props.data.config.localMcp.splice(props.data.config.localMcp.indexOf(m.value),1);void router.push('/settings/mcp')}
function selectManagedTools(value:string[]|undefined){if(!managed.value)return;const settings=props.data.config.platformMcpTools ||= {};if(value===undefined)delete settings[managed.value.settingsKey];else settings[managed.value.settingsKey]=value}
function localState(item:Mcp):McpState{const applied=props.data.localMcpState?.[item.name];return !item.enabled?{status:'disabled',validated:false,toolCount:0}:applied?.url===item.url&&applied.enabled===item.enabled&&item.authToken===undefined?applied:{status:'pending',validated:false,toolCount:0}}
function verified(state:McpState){if(m.value){const states=props.data.localMcpState ||= {};states[m.value.name]={name:m.value.name,url:m.value.url,enabled:m.value.enabled,...state}}else if(managed.value)Object.assign(managed.value,state)}

</script>
<template>
 <section aria-label="MCP 配置">
  <template v-if="route.meta.managed">
   <template v-if="managed">
    <div class="detail-heading"><strong>{{managed.displayName}}</strong><span class="source-tag">平台下发</span></div>
    <p class="hint">连接、认证与工具授权由 Orchelm 管理，删除登记请到平台操作。可在平台授权范围内选择本机使用的工具；各 Agent 可单独停用此连接。状态是最近验证结果，调用成败见对话工具记录。</p>
    <dl class="settings-facts"><div><dt>连接标识</dt><dd>{{managed.mcpServerId}}</dd></div><div><dt>状态</dt><dd>{{mcpStatus(managed.status)}}</dd></div><div><dt>已验证可用工具</dt><dd>{{managed.validated?managed.toolCount:'尚未验证'}}</dd></div></dl>
    <p v-if="managed.error" role="alert">{{managed.error}}</p>
    <ToolPicker allow-empty :model-value="data.config.platformMcpTools?.[managed.settingsKey]" :request="{kind:'platform_mcp',mcpServerId:managed.mcpServerId,settingsKey:managed.settingsKey}" @verified="verified" @update:model-value="selectManagedTools" />
    <div class="section-heading"><h3>Agent 使用情况</h3></div>
    <div class="settings-cards"><RouterLink v-for="name in agentNames" :key="name" class="settings-card" :to="'/settings/agents/'+encodeURIComponent(name)"><strong>{{name}}</strong><span>{{data.lockedSections?.includes('agents')?'Agent 配置已锁定':data.config.agents[name]?.disabledEnterpriseMcp?.includes(managed.mcpServerId)?'未启用':managed.enabled?'已启用':'已选择 · 平台已停用'}}</span></RouterLink></div>
   </template>
   <p v-else role="alert">此平台 MCP 已不可用，请返回列表查看最新配置。</p>
  </template>
  <template v-else-if="!route.params.mcp">
   <div class="section-heading"><p class="hint">平台下发与自定义 MCP 可同时使用。添加、删除或停用后请保存并生效；平台连接在平台删除。</p><button class="primary" @click="add">添加 MCP</button></div>
   <div class="settings-cards">
    <RouterLink v-for="item in data.managedMcp" :key="'platform:'+item.mcpServerId" class="settings-card" :to="platformMcpPath(item.mcpServerId)" :aria-label="`查看平台 MCP ${item.displayName}`"><strong>{{item.displayName}}</strong><span>平台下发</span><small>{{item.mcpServerId}}</small><span>{{mcpStatus(item.status)}}</span></RouterLink>
    <RouterLink v-for="item in data.config.localMcp" :key="'local:'+item.name" class="settings-card" :to="path(item)" :aria-label="`编辑 MCP ${item.name}`"><strong>{{ item.displayName||item.name }}</strong><span>用户自定义</span><small>{{ item.name }}</small><span>{{ mcpStatus(localState(item).status) }} · {{ item.agents.length }} 个 Agent</span></RouterLink>
   </div>
   <p v-if="!data.config.localMcp.length&&!data.managedMcp.length" class="hint">尚未配置 MCP。</p>
  </template>
  <template v-else-if="m">
   <div class="detail-heading"><strong>{{ m.displayName||m.name }}</strong><span class="source-tag">用户自定义</span></div>
   <dl class="settings-facts"><div><dt>状态</dt><dd>{{mcpStatus(localState(m).status)}}</dd></div><div><dt>已验证可用工具</dt><dd>{{localState(m).validated?localState(m).toolCount:'尚未验证'}}</dd></div></dl>
   <p v-if="localState(m).error" role="alert">{{localState(m).error}}</p>
   <p class="hint">保存后首次验证，成功后即可供已绑定 Agent 使用。这里展示最近验证结果；连接恢复由 Docker Agent 处理，部分连接错误会重连并重试一次，调用成败见对话中的工具结果。删除后请保存并生效。</p>
   <div class="form-grid"><label>连接标识<input :value="idDraft??m.name" @input="idDraft=($event.target as HTMLInputElement).value" :readonly="m.hasAuthToken!==undefined" @blur="rename" /></label><label>名称<input v-model.trim="m.displayName" /></label><label class="full">MCP 地址<input v-model.trim="m.url" placeholder="https://example.com/mcp" /></label><label>Bearer Token<input :value="m.authToken" @input="m.authToken=($event.target as HTMLInputElement).value||undefined" type="password" autocomplete="new-password" :placeholder="m.authToken===''?'保存后清除 Token':m.hasAuthToken?'已设置；不输入则保留':'无需认证可留空'" /></label><label class="check"><input v-model="m.enabled" type="checkbox" />启用 MCP</label></div>
   <div class="field"><span>适用 Agent</span><div class="checks"><label v-for="name in agentNames" :key="name" class="check"><input v-model="m.agents" type="checkbox" :value="name" />{{ name }}</label></div></div>
   <ToolPicker allow-empty :model-value="m.tools" :request="{kind:'mcp',mcp:{name:m.name,url:m.url,authToken:m.authToken}}" @verified="verified" @update:model-value="value=>{if(!m)return;if(value)m.tools=value;else delete m.tools}" />
   <div class="line end"><button @click="m.authToken='';m.hasAuthToken=false">清除认证</button><button @click="remove">删除 MCP</button></div>
  </template>
  <template v-else><p role="alert">此 MCP 不存在，请重新选择。</p></template>
 </section>
</template>
