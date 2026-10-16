<script setup lang="ts">
import {router} from '../../../app/router'
import {ref,watch} from 'vue'
import type {Settings} from './types'
const props=defineProps<{data:Settings}>()
const retentionDraft=ref<string>()
watch(()=>props.data,()=>{retentionDraft.value=undefined})
function setRetention(event:Event){const value=(event.target as HTMLInputElement).value;retentionDraft.value=value;if(value==='')delete props.data.config.workbench.deliveryRetentionCount;else props.data.config.workbench.deliveryRetentionCount=Number(value)}
const assistant=(name:string)=>props.data.config.workbench.assistants.find(item=>item.agent===name)
function create(){
 let number=1
 while(props.data.config.agents[`agent_${number}`])number++
 const name=`agent_${number}`
 props.data.config.agents[name]={description:'新 Agent',instruction:"Complete the task using available tools and report verified results in the user's language.",max_iterations:300,toolsets:[]}
 props.data.config.workbench.assistants.push({agent:name,displayName:'新 Agent',taskConcurrency:2,deliveryTool:true})
 void router.push('/settings/agents/'+name)
}
</script>
<template>
 <section aria-label="Agent 列表">
  <section class="agent-defaults" aria-label="全局配置">
   <h3>全局配置</h3>
   <div class="form-grid">
    <label>对话频道默认 Agent<select v-model="data.config.workbench.defaultAgent"><option value="">不指定（新建时选择）</option><option v-for="item in data.config.workbench.assistants" :key="item.agent" :value="item.agent">{{ item.displayName }} · {{ item.agent }}</option></select></label>
    <label>交付版本保留数量<input aria-label="交付版本保留数量" :value="retentionDraft??data.config.workbench.deliveryRetentionCount??10" type="number" min="0" max="100" step="1" placeholder="10" @input="setRetention" /><small>全局默认 10，范围 0–100；0 仅保留最新 1 个版本。Agent 可单独覆盖。</small></label>
   </div>
   <p class="hint">不指定时，新建对话需先选择 Agent；指定后自动选中它，并沿用其工作目录配置。任务从各 Agent 入口新建。</p>
   <p class="hint">每个对话在下一次成功交付后清理超出数量的旧版本及文件副本；修改设置不立即清理，交付失败不清理。</p>
  </section>
  <div class="agent-list-heading"><p class="hint">选择 Agent 编辑配置。</p><button type="button" class="primary" @click="create">新建 Agent</button></div>
  <div class="agent-cards">
   <RouterLink v-for="(agent,name) in data.config.agents" :key="name" class="agent-card" :to="'/settings/agents/'+encodeURIComponent(name)" :aria-label="`编辑 ${name}`">
    <div><strong>{{ assistant(name)?.displayName||agent.description||name }}</strong><span>{{ assistant(name)?'主代理':'子代理' }}</span></div>
    <small>{{ name }}{{ data.config.workbench.defaultAgent===name?' · 对话默认':'' }}</small>
    <p>{{ agent.description||'暂无说明' }}</p>
    <small>{{ agent.toolsets.length }} 个原生工具集 · {{ data.config.localMcp.filter(m=>m.agents.includes(name)).length }} 个 MCP{{ agent.readonly?' · 只读':'' }}</small>
   </RouterLink>
  </div>
 </section>
</template>
<style scoped>
.agent-defaults{border-bottom:1px solid var(--tuck-border);margin-bottom:16px;padding-bottom:10px}.agent-defaults h3{font-size:var(--tuck-font-ui);margin:0 0 12px}
.agent-list-heading{display:flex;justify-content:space-between;align-items:center;gap:12px;margin-bottom:14px}.agent-list-heading p{margin:0}.agent-cards{display:grid;grid-template-columns:repeat(auto-fit,minmax(min(100%,260px),1fr));gap:12px}.agent-card{display:block;border:1px solid var(--tuck-border);border-radius:8px;padding:14px;color:var(--tuck-text);background:var(--tuck-surface);text-decoration:none;min-width:0;overflow-wrap:anywhere}.agent-card:hover{background:var(--tuck-hover);border-color:var(--tuck-accent)}.agent-card>div{display:flex;align-items:center;justify-content:space-between;gap:8px;margin-bottom:5px}.agent-card span{font-size:var(--tuck-font-caption);color:var(--tuck-muted);white-space:nowrap}.agent-card p{font-size:var(--tuck-font-caption);color:var(--tuck-muted);margin:10px 0;line-height:1.5}.agent-card small{display:block}
</style>
