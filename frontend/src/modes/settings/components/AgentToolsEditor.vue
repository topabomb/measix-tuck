<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import {useRoute} from 'vue-router'
import {inspectionModelOptions} from './inspection-model-options'
import {imageGeneratorOptions} from './image-generator-options'
import NativeToolsEditor from './NativeToolsEditor.vue'
import type { Mcp, Settings } from './types'
import {platformMcpPath,mcpStatus} from './catalog-paths'

const props = defineProps<{ data: Settings; name: string }>()
const agent = computed(() => props.data.config.agents[props.name])
const assistant = computed(() => props.data.config.workbench.assistants.find(item => item.agent === props.name))
const retentionDraft=ref<string>()
watch(()=>props.data,()=>{retentionDraft.value=undefined})
const retainedVersions=computed(()=>Math.max(1,assistant.value?.deliveryRetentionCount??props.data.config.workbench.deliveryRetentionCount??10))
function setRetention(event:Event){const value=(event.target as HTMLInputElement).value;retentionDraft.value=value;if(!assistant.value)return;if(value==='')delete assistant.value.deliveryRetentionCount;else assistant.value.deliveryRetentionCount=Number(value)}
const route=useRoute()
const parentPath=computed(()=>'/settings/agents/'+encodeURIComponent(props.name))
function bind(mcp: Mcp, event: Event) {
  if ((event.target as HTMLInputElement).checked) mcp.agents = [...new Set([...mcp.agents, props.name])]
  else mcp.agents = mcp.agents.filter(name => name !== props.name)
}
function bindEnterprise(id:string,event:Event){
  if(!agent.value)return
  const disabled=new Set(agent.value.disabledEnterpriseMcp||[])
  if((event.target as HTMLInputElement).checked)disabled.delete(id);else disabled.add(id)
  if(disabled.size)agent.value.disabledEnterpriseMcp=[...disabled];else delete agent.value.disabledEnterpriseMcp
}
const inspectionOptions=computed(()=>inspectionModelOptions(props.data))
const inspectionModel=computed({get:()=>agent.value?.attachmentInspectionModelId??'',set:(value:string)=>{agent.value.attachmentInspectionModelId=value}})
const imageOptions=computed(()=>imageGeneratorOptions(props.data))
const imageResource=computed({get:()=>{const r=agent.value?.imageGenerator;return r?.source&&r.imageId?`${r.source}:${r.imageId}`:''},set:(value:string)=>{if(!value){delete agent.value.imageGenerator;return}const [source,imageId]=value.split(':');agent.value.imageGenerator={source,imageId}}})
const tuckTools=[
  {id:'inspect_attachments',label:'识别附件',description:'读取任务目录或已连接的 Runcove 工作区中的附件并返回文字；目前支持图片，暂不支持 PDF。',readonlyAllowed:true},
  {id:'generate_images',label:'生成图片',description:'使用已配置的生图资源，保存原图并将图片返回模型上下文；需可写的本地主任务目录。',main:true},
  {id:'list_sessions',label:'查看会话',description:'列出主会话及其任务 ID、助手和当前状态。',main:true},
  {id:'send_session_message',label:'发送会话消息',description:'按用户要求向指定主会话发送消息。',main:true},
  {id:'upload_runcove_file',label:'上传工作区文件',description:'将本地工作目录中的文件上传到 Runcove 工作区。'},
  {id:'download_runcove_files',label:'批量下载工作区文件',description:'下载 1–20 个文件到指定本地目录，保留文件名，默认不覆盖已有文件。'},
]
function setTuck(id:string,event:Event){
  const disabled=new Set(agent.value.disabledTuckTools||[])
  if((event.target as HTMLInputElement).checked)disabled.delete(id);else disabled.add(id)
  if(disabled.size)agent.value.disabledTuckTools=[...disabled];else delete agent.value.disabledTuckTools
}
</script>

<template>
  <div class="agent-tools" aria-label="Agent 工具配置" :class="{'tool-page':!!route.params.tool}">
    <template v-if="route.params.tool">

      <p v-if="!agent" role="alert">此 Agent 不存在，请返回列表选择。</p>
      <NativeToolsEditor v-else-if="route.params.tool==='native'" :agent="agent" :name="name" />
      <template v-else-if="route.params.tool==='tuck'">
        <div class="tools-heading"><h3>Tuck 工具</h3></div>
        <p class="hint">选择此 Agent 可使用的工具，保存并应用后生效。</p>
        <p v-if="agent.readonly" class="hint">只读 Agent 仍可识别附件；写入和会话协作工具不可用，取消只读后沿用已保存的开关。</p>
        <div class="tuck-list">
          <div v-for="item in tuckTools" :key="item.id" class="tuck-tool">
            <label class="check"><input type="checkbox" :aria-label="'启用'+item.label" :checked="(!agent.readonly||item.readonlyAllowed)&&(!item.main||!!assistant)&&!agent.disabledTuckTools?.includes(item.id)" :disabled="!!agent.readonly&&!item.readonlyAllowed||!!item.main&&!assistant" @change="setTuck(item.id,$event)" /><span><strong>{{item.label}}</strong><code>{{item.id}}</code></span></label>
            <p class="hint">{{item.description}}<span v-if="item.main&&!assistant"> 仅主代理可用。</span><span v-else-if="!item.main&&!item.readonlyAllowed"> 需本地工作目录及可用的Runcove 工作区。</span></p>
            <label v-if="item.id==='inspect_attachments'&&!agent.disabledTuckTools?.includes(item.id)" class="delivery-options">识图模型（覆盖）<select aria-label="识图模型（覆盖）" v-model="inspectionModel"><option value="">跟随全局默认</option><option v-if="agent.attachmentInspectionModelId&&!inspectionOptions.some(m=>m.modelId===agent.attachmentInspectionModelId)" :value="agent.attachmentInspectionModelId" disabled>{{agent.attachmentInspectionModelId}}（不可用）</option><option v-for="m in inspectionOptions" :key="m.modelId" :value="m.modelId">{{m.label}}</option></select></label>
            <label v-if="item.id==='generate_images'&&assistant&&!agent.readonly&&!agent.disabledTuckTools?.includes(item.id)" class="delivery-options">生图资源（覆盖）<select aria-label="生图资源（覆盖）" v-model="imageResource"><option value="">跟随全局默认</option><option v-if="imageResource&&!imageOptions.some(r=>r.selector===imageResource)" :value="imageResource" disabled>{{imageResource}}（不可用）</option><option v-for="r in imageOptions" :key="r.selector" :value="r.selector">{{r.label}}</option></select></label>
          </div>
          <div class="tuck-tool">
            <label class="check"><input type="checkbox" aria-label="启用交付工具" :checked="!!assistant&&!agent.readonly&&assistant.deliveryTool!==false" :disabled="!assistant||!!agent.readonly" @change="assistant&&(assistant.deliveryTool=($event.target as HTMLInputElement).checked)" /><span><strong>交付成果</strong><code>deliver_result</code></span></label>
            <p class="hint">提交摘要和文件清单，保存交付版本。<span v-if="!assistant">子代理不提供交付工具。</span></p>
            <div v-if="assistant" class="delivery-options">
              <label>交付版本保留数量（覆盖）<input aria-label="交付版本保留数量（覆盖）" :value="retentionDraft??assistant.deliveryRetentionCount" type="number" min="0" max="100" step="1" :placeholder="`跟随全局（${data.config.workbench.deliveryRetentionCount??10}）`" @input="setRetention" /></label>
              <p class="hint">留空跟随全局；0 仅保留最新版本，最多 100。每个对话保留最新 {{retainedVersions}} 个版本，在下一次成功交付时清理旧版本及副本。</p>
              <p class="hint">每轮必须交付：在<RouterLink to="/settings/orchestration">编排设置</RouterLink>中启用交付要求。</p>
            </div>
          </div>
        </div>
      </template>
    </template>
    <template v-else-if="agent">
      <div class="tools-heading"><h3>工具</h3></div>
      <div class="tool-cards tool-groups">
        <RouterLink class="tool-card" :to="parentPath+'/tools/native'" aria-label="Docker Agent 原生工具"><strong>Docker Agent 原生工具</strong><span>文件、终端、计划等工具，在同一页面勾选</span></RouterLink>
        <RouterLink class="tool-card" :to="parentPath+'/tools/tuck'" aria-label="Tuck 工具"><strong>Tuck 工具</strong><span>会话协作、交付成果、工作区文件传输</span></RouterLink>
      </div>
    <p v-if="data.lockedSections?.includes('mcp')" class="hint">MCP 配置已锁定，解锁后可管理连接。</p><template v-else><div class="tools-heading"><h3>MCP</h3><RouterLink to="/settings/mcp">管理连接</RouterLink></div>
    <p class="hint">选择供此 Agent 使用的连接；平台授权或连接开关关闭时不可用。</p>
    <div v-if="data.managedMcp.length" class="mcp-bindings">
      <div v-for="mcp in data.managedMcp" :key="mcp.mcpServerId" class="mcp-binding"><label class="check"><input type="checkbox" :aria-label="`启用平台 MCP ${mcp.displayName}`" :checked="!agent.disabledEnterpriseMcp?.includes(mcp.mcpServerId)" @change="bindEnterprise(mcp.mcpServerId,$event)" /><span>{{mcp.displayName}}<small>平台下发 · {{mcpStatus(mcp.status)}}</small></span></label><RouterLink :to="platformMcpPath(mcp.mcpServerId)" :aria-label="`查看平台 MCP ${mcp.displayName}`">详情</RouterLink></div>
    </div>
    <div v-if="data.config.localMcp.length" class="mcp-bindings">
      <div v-for="mcp in data.config.localMcp" :key="mcp.name" class="mcp-binding"><label class="check"><input type="checkbox" :aria-label="`启用自定义 MCP ${mcp.displayName||mcp.name}`" :checked="mcp.agents.includes(name)" @change="bind(mcp, $event)" /><span>{{ mcp.displayName || mcp.name }}<small>用户自定义 · {{ mcp.enabled ? '已启用' : '已停用' }}</small></span></label><RouterLink :to="'/settings/mcp/'+encodeURIComponent(mcp.name)" :aria-label="`编辑 MCP ${mcp.name}`">编辑</RouterLink></div>
    </div>
    <p v-if="!data.config.localMcp.length&&!data.managedMcp.length" class="hint">尚无 MCP 连接，可前往连接设置添加。</p></template>

    </template>
  </div>
</template>
<style scoped>
.tuck-tool{padding:12px 0;border-top:1px solid var(--tuck-border)}.tuck-tool .check{align-items:flex-start}.tuck-tool .check input{margin-top:3px}.tuck-tool .check span{min-width:0}.tuck-tool code{display:inline-block;color:var(--tuck-muted);font-size:12px;font-weight:400;overflow-wrap:anywhere;margin:0 0 0 10px}.tuck-tool .hint{margin:5px 0 0 21px}.delivery-options{margin:12px 0 0 21px}.delivery-options label{max-width:260px}.delivery-options .hint{margin:6px 0}.tool-names{font-size:var(--tuck-font-caption);overflow-wrap:anywhere;margin:8px 0}.tool-names code{font-size:inherit}.agent-tools{margin:16px 0;padding:12px 0;border-top:1px solid var(--tuck-border);border-bottom:1px solid var(--tuck-border);min-width:0}.agent-tools.tool-page{border:0;margin:0;padding:0}.tool-back{display:inline-block;margin-bottom:16px;color:var(--tuck-accent);font-size:var(--tuck-font-caption);text-decoration:none}.tools-heading{display:flex;align-items:baseline;justify-content:space-between;gap:8px;margin:0 0 10px}.tools-heading h3{margin:0;font-size:var(--tuck-font-ui);font-weight:600}.tools-heading a{color:var(--tuck-accent);font-size:var(--tuck-font-caption);text-decoration:none}.tool-cards{display:grid;grid-template-columns:repeat(auto-fit,minmax(min(100%,220px),1fr));gap:10px;margin-bottom:18px}.tool-card{display:grid;gap:5px;border:1px solid var(--tuck-border);border-radius:7px;padding:12px;color:var(--tuck-text);background:var(--tuck-surface);text-decoration:none;text-align:left;overflow-wrap:anywhere}.tool-card:hover{border-color:var(--tuck-accent);background:var(--tuck-hover)}.tool-card span,.tool-card small{font-size:var(--tuck-font-caption);color:var(--tuck-muted)}.tool-content fieldset{border:0;padding:0;margin:0;min-width:0}.tool-content textarea{resize:vertical}.tool-content [role=alert]{color:var(--tuck-danger);white-space:pre-wrap}.tool-actions{display:flex;justify-content:flex-end;flex-wrap:wrap;gap:8px}.inherited{color:var(--tuck-muted)}.mcp-bindings{display:grid;grid-template-columns:repeat(auto-fit,minmax(min(100%,260px),1fr));gap:8px;margin-bottom:10px}.mcp-binding{display:flex;align-items:center;justify-content:space-between;gap:12px;border:1px solid var(--tuck-border);border-radius:6px;padding:8px 10px;min-width:0}.mcp-binding>a{color:var(--tuck-accent);font-size:var(--tuck-font-caption);flex:none;text-decoration:none}.mcp-bindings label{min-width:0}.mcp-bindings span{overflow-wrap:anywhere}.mcp-bindings small{display:block;font-size:var(--tuck-font-caption);color:var(--tuck-muted)}.automatic-tools{color:var(--tuck-muted);font-size:var(--tuck-font-caption);line-height:var(--tuck-line-caption)}.automatic-tools p{margin:6px 0}.agent-tools small{color:var(--tuck-muted)}.tool-content .check{flex-wrap:wrap}
@media(max-width:620px){.tuck-tool code{display:block;margin:3px 0}}
</style>
