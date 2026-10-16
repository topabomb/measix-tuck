<script setup lang="ts">
import {computed} from 'vue'
import {useRoute} from 'vue-router'
import {router} from '../../../app/router'
import AgentWorkspaceEditor from './AgentWorkspaceEditor.vue'
import AgentToolsEditor from './AgentToolsEditor.vue'
import type {Settings} from './types'
const props=defineProps<{data:Settings}>()
const data=computed(()=>props.data),route=useRoute()
const selectedAgent=computed(()=>typeof route.params.agent==='string'?route.params.agent:'root')
const agent=computed(()=>props.data.config.agents[selectedAgent.value])
const assistant=computed(()=>props.data.config.workbench.assistants.find(a=>a.agent===selectedAgent.value))
const referenced=computed(()=>data.value.config.workbench.defaultAgent===selectedAgent.value||Object.values(data.value.config.agents).some(a=>a.sub_agents?.includes(selectedAgent.value))||data.value.config.localMcp.some(m=>m.agents.includes(selectedAgent.value)))
const modelOptions=computed(()=>props.data.lockedSections?.includes('providers')?(props.data.modelCatalog||[]).map(m=>({...m,sourceLabel:m.source==='platform'?'平台下发':'用户自定义'})):[...props.data.managedModels.filter(m=>m.selectable).map(m=>({...m,sourceLabel:'平台下发'})),...props.data.config.providers.flatMap(p=>p.models.filter(m=>p.enabled&&m.enabled&&m.capabilities.includes('TOOL')).map(m=>({...m,sourceLabel:'用户自定义'})))])
function expose(value:boolean){const c=data.value!.config;if(value)c.workbench.assistants.push({agent:selectedAgent.value,displayName:agent.value?.description||selectedAgent.value,taskConcurrency:2,deliveryTool:true});else c.workbench.assistants=c.workbench.assistants.filter(a=>a.agent!==selectedAgent.value)}
function deleteAgent(){if(referenced.value)return;const c=data.value!.config;delete c.agents[selectedAgent.value];c.workbench.assistants=c.workbench.assistants.filter(a=>a.agent!==selectedAgent.value);void router.push('/settings/agents')}
</script>
<template>
<section aria-label="Agent 配置">
          <div class="agent-edit-heading"><strong>{{ assistant?.displayName||agent?.description||selectedAgent }}</strong><small>{{ selectedAgent }}</small></div>
          <p v-if="!agent" role="alert">此 Agent 不存在，请返回列表选择。</p><template v-if="agent"><div class="form-grid"><label>说明<input v-model="agent.description" /></label><label>默认模型<select v-model="agent.modelId"><option :value="undefined">沿用默认模型</option><option v-for="m in modelOptions" :key="m.modelId" :value="m.modelId">{{ m.displayName }} · {{m.sourceLabel}}</option></select></label>
          <label>最大执行步数<input v-model.number="agent.max_iterations" aria-label="最大执行步数" type="number" min="0" step="1" /><small>0 表示不限；达到上限后需确认继续。</small></label>
          <label>重复调用阈值<input :value="agent.max_consecutive_tool_calls ?? 5" aria-label="重复调用阈值" type="number" min="0" step="1" @input="agent.max_consecutive_tool_calls=($event.target as HTMLInputElement).valueAsNumber" /><small>连续相同工具和参数的调用次数。默认 5；0 也使用默认值，不能关闭保护。</small></label>
          <label class="check"><input v-model="agent.readonly" type="checkbox" />只读 Agent</label></div>
          <label>指令<textarea v-model="agent.instruction" rows="6" spellcheck="false" /></label>
          <div class="field"><span>子代理</span><div class="checks"><label v-for="(_,name) in data.config.agents" v-show="name!==selectedAgent" :key="name" class="check"><input type="checkbox" :checked="agent.sub_agents?.includes(String(name))" @change="agent.sub_agents=($event.target as HTMLInputElement).checked?[...(agent.sub_agents||[]),String(name)]:(agent.sub_agents||[]).filter(n=>n!==name)" />{{ name }}</label></div></div>
          <AgentToolsEditor :key="selectedAgent" :data="data" :name="selectedAgent" />
<label class="check"><input type="checkbox" :checked="!!assistant" :disabled="data.config.workbench.defaultAgent===selectedAgent" @change="expose(($event.target as HTMLInputElement).checked)" />主代理</label>
          <p class="hint">主代理可在对话和任务频道中创建会话；关闭后仅供其他 Agent 调用。新建 Agent 默认启用。</p>
          <div v-if="assistant" class="form-grid"><label>助手名称<input v-model="assistant.displayName" /></label><label>助手简介<input v-model="assistant.description" /></label><label>任务频道并发数<input v-model.number="assistant.taskConcurrency" type="number" min="1" /></label></div>
          <AgentWorkspaceEditor v-if="assistant" :assistant="assistant" />
          <div v-if="!['root','worker','reviewer'].includes(selectedAgent)" class="line end"><small v-if="referenced">删除前请先解除频道默认、子代理或 MCP 引用。</small><button :disabled="referenced" @click="deleteAgent">删除 Agent</button></div></template>
        </section>
</template>

<style scoped>.agent-edit-heading{display:flex;align-items:baseline;gap:12px;flex-wrap:wrap;margin-bottom:16px}.agent-edit-heading a{font-size:var(--tuck-font-caption);color:var(--tuck-accent);text-decoration:none}.agent-edit-heading small{margin-left:auto}</style>
