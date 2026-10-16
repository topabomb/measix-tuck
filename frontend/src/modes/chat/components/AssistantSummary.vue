<script setup lang="ts">
import {computed,inject,onBeforeUnmount,ref,watch} from 'vue'
import {api,type AssistantCatalog,type AssistantSummaryInfo,type DockerTask} from '../../../shared/workspace/runtime'
import AssistantIcon from '../../../shared/ui/AssistantIcon.vue'
import SummaryPopover from '../../../shared/ui/SummaryPopover.vue'
import {copyText} from '../../../shared/copy-text'
import {workspaceKey} from '../../../shared/enterprise-workspace/state'
import {settingsAccess,settingsLocked} from '../../../app/settings-access'
import {workspaceModeLabels} from '../../../../shared/workspace-mode.mjs'
const props=defineProps<{task:DockerTask;assistant?:AssistantCatalog['items'][number];toolAvailable?:boolean;requireDelivery?:boolean;parentContext?:boolean}>()
const opened=ref(false),trigger=ref<HTMLButtonElement|null>(null),info=ref<AssistantSummaryInfo|null>(null),loading=ref(false),error=ref(''),workspaceError=ref('')
const enterprise=inject(workspaceKey)
const name=computed(()=>props.assistant?.displayName||props.task.assistantName||props.task.agent)
const deliveryEnabled=computed(()=>props.toolAvailable===true&&!!props.assistant&&props.assistant.deliveryTool!==false)
const directoryMode=computed(()=>props.task.workspaceMode?workspaceModeLabels[props.task.workspaceMode]:props.task.workspace?'工作目录':'未指定目录')
const settings=computed(()=>!settingsLocked('agents')?info.value?.settings:undefined)
const mcp=computed(()=>!settingsLocked('agents')&&!settingsLocked('mcp')?info.value?.mcp:undefined)
const enabledMcp=computed(()=>mcp.value?.filter(item=>item.enabled).length)
const workspaceStatus=computed(()=>({UNPROVISIONED:'未创建',CONNECTING:'连接中',CONNECTED:'已连接',DISCONNECTING:'正在断开',DISCONNECTED:'已断开',RESTORING:'恢复中',DELETING:'删除中',DELETED:'已删除',NEEDS_ATTENTION:'需要处理'} as Record<string,string>)[enterprise?.state.projection?.state||'']||'状态未知')
const copied=ref(false),copyError=ref(false)
let copyTimer:ReturnType<typeof setTimeout>|undefined
watch([opened,()=>props.task.id,()=>settingsAccess.generation],([open],_,cleanup)=>{
  info.value=null;error.value='';workspaceError.value='';loading.value=false
  if(!open)return
  const controller=new AbortController();cleanup(()=>controller.abort());loading.value=true
  const requests=[api<AssistantSummaryInfo>(`/api/tasks/${encodeURIComponent(props.task.id)}/assistant-summary`,undefined,controller.signal).then(value=>{if(!controller.signal.aborted)info.value=value}).catch(cause=>{if(!controller.signal.aborted)error.value=(cause as Error).message})]
  if(enterprise)requests.push(enterprise.refresh(undefined,{signal:controller.signal,background:true,requireFiles:false}).catch(cause=>{if(!controller.signal.aborted)workspaceError.value=(cause as Error).message}))
  void Promise.allSettled(requests).then(()=>{if(!controller.signal.aborted)loading.value=false})
})
watch(()=>props.task.id,()=>{opened.value=false;copied.value=false;copyError.value=false;clearTimeout(copyTimer)})
async function copyDirectory(){
  const path=props.task.workspace;if(!path)return;copyError.value=false
  try{await copyText(path);if(props.task.workspace!==path)return;copied.value=true;clearTimeout(copyTimer);copyTimer=setTimeout(()=>copied.value=false,1800)}catch{copyError.value=true}
}
function mcpStatus(status:string){return ({ready:'目录已验证',unavailable:'最近验证失败',pending:'尚未验证',disabled:'已停用'} as Record<string,string>)[status]||'未读取'}
onBeforeUnmount(()=>clearTimeout(copyTimer))
</script>
<template>
  <button ref="trigger" class="assistant-badge" data-testid="active-assistant" type="button" :title="name" :aria-label="parentContext?'所属主助手信息':'助手信息'" aria-haspopup="dialog" :aria-expanded="opened" @click="opened=!opened"><AssistantIcon /><span class="assistant-name">{{name}}</span><span class="assistant-mobile-label">助手</span></button>
  <SummaryPopover v-model:open="opened" :trigger="trigger" :label="parentContext?'所属主助手信息':'助手信息'" class="assistant-popover">
    <strong class="assistant-name-heading">{{name}}</strong>
    <p v-if="parentContext" class="summary-note">以下为所属主助手与主对话信息。</p>
    <section class="summary-section">
      <h3>当前对话</h3>
      <dl><dt>{{parentContext?'主对话模型':'当前模型'}}</dt><dd>{{task.modelName||task.modelId||'未指定'}}</dd><dt>工具审批</dt><dd>{{task.approvalMode==='auto'?'自动批准':'手动确认'}}</dd><dt>交付要求</dt><dd>{{!deliveryEnabled?'不适用':requireDelivery===undefined?'未读取':requireDelivery?'已开启':'已关闭'}}</dd></dl>
    </section>
    <section class="summary-section" aria-label="助手工具摘要">
      <h3>MCP 与工具</h3>
      <dl><dt>原生报告工具数</dt><dd data-testid="assistant-tool-count">{{info?.availableTools??(loading?'读取中…':'未读取')}}</dd><dt>MCP 绑定</dt><dd data-testid="assistant-mcp-count">{{mcp?`${mcp.length} 项 · ${enabledMcp} 项启用`:!settingsAccess.ready?'权限未读取':info?.mcpLocked||settingsLocked('agents')||settingsLocked('mcp')?'配置已锁定':loading?'读取中…':'未读取'}}</dd></dl>
      <p v-if="info?.toolsError" class="summary-note">{{info.toolsError}}</p>
      <details v-if="mcp?.length"><summary>查看 MCP</summary><ul class="mcp-list"><li v-for="item in mcp" :key="item.id"><strong>{{item.displayName}}</strong><span>{{item.source==='platform'?'平台':'自定义'}} · {{mcpStatus(item.status)}}<template v-if="item.toolCount!==null"> · {{item.toolCount}} 个已选工具</template></span></li></ul></details>
      <p v-if="error" class="summary-error" role="alert">{{error}}</p>
    </section>
    <section class="summary-section" aria-label="远程工作区状态">
      <h3>远程工作区</h3>
      <p v-if="workspaceError" class="summary-error" role="status">状态读取失败：{{workspaceError}}</p>
      <template v-else-if="enterprise?.state.projection"><dl><dt>文件服务</dt><dd>{{enterprise.state.projection.filesAvailable?'可用':'不可用'}}</dd><dt>工作区状态</dt><dd>{{workspaceStatus}}</dd></dl><p v-if="!enterprise.state.projection.filesAvailable" class="summary-note">{{enterprise.state.projection.filesReason==='not_provisioned'?'工作区尚未创建':enterprise.state.projection.filesReason}}</p></template>
      <p v-else class="summary-note">{{loading?'正在读取工作区状态…':'未读取工作区状态'}}</p>
      <small v-if="enterprise?.state.projectionReadAt" class="summary-note">最近读取 {{new Date(enterprise.state.projectionReadAt).toLocaleTimeString()}}</small>
      <RouterLink class="summary-link" to="/workspace">打开远程工作区 ›</RouterLink>
    </section>
    <section class="summary-section">
      <h3>助手特定设置</h3>
      <dl v-if="settings"><dt>默认模型</dt><dd>{{settings.defaultModel}}</dd><dt>执行步数上限</dt><dd>{{settings.maxIterations===0?'不限':settings.maxIterations??'原生默认'}}</dd><dt>只读助手</dt><dd>{{settings.readonly?'是':'否'}}</dd><dt>子代理</dt><dd>{{settings.subAgents.length?settings.subAgents.join('、'):'无'}}</dd></dl>
      <p v-else class="summary-note">{{!settingsAccess.ready?'配置权限未读取。':settingsLocked('agents')||info?.settingsLocked?'助手配置已锁定，解锁后可查看。':loading?'正在读取助手配置…':'未取得可展示的助手配置。'}}</p>
      <details v-if="settings?.instructionPreview"><summary>指令摘要</summary><p class="instruction-preview">{{settings.instructionPreview}}</p></details>
      <RouterLink class="summary-link" :to="`/settings/agents/${encodeURIComponent(task.agent)}`">查看助手配置 ›</RouterLink>
      <details class="directory-details"><summary>工作目录与其他信息</summary><dl><dt>Agent</dt><dd>{{task.agent}}</dd><dt>工作目录</dt><dd>{{directoryMode}}</dd><dd v-if="task.workspace" class="directory-path"><code>{{task.workspace}}</code><button type="button" aria-label="复制工作目录" @click="copyDirectory">{{copied?'已复制':'复制'}}</button><small v-if="copyError" role="alert">复制失败，请选中路径复制。</small></dd><dt>交付工具</dt><dd>{{deliveryEnabled?'已启用':'已停用'}}</dd></dl><p v-if="assistant?.description" class="summary-note">{{assistant.description}}</p></details>
    </section>
  </SummaryPopover>
</template>
<style scoped>
.assistant-badge{display:inline-flex;align-items:center;justify-content:center;gap:4px;max-width:140px;min-width:30px;height:30px;padding:0 6px;border:1px solid transparent;border-radius:6px;background:transparent;color:var(--tuck-muted);font:var(--tuck-font-caption)/1 var(--tuck-font-family);cursor:pointer}
.assistant-badge:hover,.assistant-badge[aria-expanded=true]{color:var(--tuck-text);background:var(--tuck-hover)}.assistant-badge[aria-expanded=true]{border-color:var(--tuck-border)}.assistant-name{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.assistant-mobile-label{display:none}
.assistant-name-heading{display:block;font-size:var(--tuck-font-title)}.summary-section{padding:var(--summary-section-gap) 0;border-bottom:1px solid var(--tuck-border)}.summary-section:last-child{border:0;padding-bottom:0}.summary-section h3{margin:0 0 6px;font-size:var(--tuck-font-ui)}dl{display:grid;grid-template-columns:auto minmax(0,1fr);gap:var(--summary-row-gap) 12px;margin:0}dt{color:var(--tuck-muted)}dd{margin:0;text-align:right;overflow-wrap:anywhere}
.summary-note{display:block;margin:6px 0;color:var(--tuck-muted);font-size:var(--tuck-font-caption)}.summary-error{margin:6px 0;color:var(--tuck-danger);font-size:var(--tuck-font-caption);overflow-wrap:anywhere}.summary-link{display:block;margin-top:6px;color:var(--tuck-accent);text-decoration:none}.summary-link:hover{text-decoration:underline}summary{cursor:pointer;color:var(--tuck-accent);padding:6px 0}.instruction-preview{white-space:pre-wrap;overflow-wrap:anywhere}.mcp-list{list-style:none;padding:0;margin:0}.mcp-list li{padding:6px 0}.mcp-list span{display:block;color:var(--tuck-muted);font-size:var(--tuck-font-caption)}
.directory-path{grid-column:1/-1;display:grid;grid-template-columns:minmax(0,1fr) auto;gap:4px 8px;align-items:start;text-align:left}.directory-path code{min-width:0;overflow-wrap:anywhere;white-space:pre-wrap;user-select:text;font:var(--tuck-font-caption)/var(--tuck-line-body) monospace;color:var(--tuck-muted)}.directory-path button{border:0;border-radius:4px;background:transparent;color:var(--tuck-accent);padding:0 4px;min-height:24px;cursor:pointer}.directory-path small{grid-column:1/-1;color:var(--tuck-danger)}
@container(max-width:760px){.assistant-name{display:none}.assistant-badge{width:30px;padding:0}}
@media(max-width:760px){.assistant-badge{width:50px;height:44px}.assistant-badge>svg{display:none}.assistant-mobile-label{display:inline}.summary-link,summary{min-height:28px}.directory-path button{min-height:44px}}
@media(max-width:360px){.assistant-badge{width:44px}.assistant-badge>svg{display:block}.assistant-mobile-label{display:none}}
</style>
