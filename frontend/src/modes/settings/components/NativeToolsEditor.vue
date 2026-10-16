<script setup lang="ts">
import {computed,onBeforeUnmount,onMounted,reactive,ref,watch} from 'vue'
import {authFetch} from '../../../app/auth'
import type {Agent} from './types'
type Tool={name:string;description:string;readonly:boolean}
type Toolset=Record<string,unknown>
type Group={type:string;tools:Tool[];config?:Toolset;key:string;error?:string}
const props=defineProps<{agent:Agent;name:string}>()
const catalog=ref<{type:string;tools:Tool[];error?:string}[]>([]),busy=ref(false),error=ref(''),search=ref('')
const controller=new AbortController(),disabledDrafts=new Map<string,Toolset>()
const listDrafts=reactive(new WeakMap<Toolset,Record<string,string>>())
watch(()=>props.agent,()=>{disabledDrafts.clear()})
const titles:Record<string,string>={filesystem:'文件系统',file:'文件操作',shell:'终端',todo:'待办',user_prompt:'用户交互',background_agents:'后台子代理',background_jobs:'后台进程',fetch:'网页请求',git:'Git',think:'思考',plan:'计划',environment:'环境信息',session_context:'会话上下文'}
const names=(value:unknown):string[]=>Array.isArray(value)?value.filter((x):x is string=>typeof x==='string'):[]
const groups=computed<Group[]>(()=>[...catalog.value,...[...new Set(props.agent.toolsets.map(t=>String(t.type)))].filter(type=>!catalog.value.some(g=>g.type===type)).map(type=>({type,tools:[],error:'已保存的专有配置保留；工具是否可用由原生引擎决定。'}))].flatMap(group=>{
 const configured=props.agent.toolsets.filter(t=>t.type===group.type)
 return (configured.length?configured:[undefined]).map((config,index)=>{
  const extra=names(config?.tools).filter(name=>!group.tools.some(t=>t.name===name)).map(name=>({name,description:'已配置，当前运行时未发现此工具。',readonly:false}))
  return {...group,tools:[...group.tools,...extra],config,key:group.type+'-'+index}
 })
}))
const matches=(group:Group,tool:Tool)=>(tool.name+' '+tool.description+' '+titles[group.type]).toLowerCase().includes(search.value.trim().toLowerCase())
const visibleGroups=computed(()=>groups.value.filter(group=>!search.value.trim()||group.tools.some(tool=>matches(group,tool))))
function restricted(group:Group,tool:Tool){return !tool.readonly&&(props.agent.readonly||(group.config||disabledDrafts.get(group.key))?.readonly===true)}
function checked(group:Group,tool:Tool){return !!group.config&&!restricted(group,tool)&&(!names(group.config.tools).length||names(group.config.tools).includes(tool.name))}
const enabledCount=computed(()=>groups.value.reduce((sum,g)=>sum+g.tools.filter(t=>checked(g,t)).length,0))
function toggleGroup(group:Group,enabled:boolean){
 if(!enabled){if(group.config){disabledDrafts.set(group.key,group.config);props.agent.toolsets.splice(props.agent.toolsets.indexOf(group.config),1)}return}
 if(group.config)return
 const saved=disabledDrafts.get(group.key)
 const config=saved||{type:group.type,...(['filesystem','file'].includes(group.type)?{allow_list:['.']}:{})}
 if(!names(config.tools).length)config.tools=group.tools.filter(tool=>!restricted(group,tool)).map(tool=>tool.name)
 if(!names(config.tools).length)return
 props.agent.toolsets.push(config);disabledDrafts.delete(group.key)
}
function toggle(group:Group,tool:Tool,enabled:boolean){
 if(restricted(group,tool))return
 let config=group.config
 if(!config){
  if(!enabled)return
  config=disabledDrafts.get(group.key)||{type:group.type,...(['filesystem','file'].includes(group.type)?{allow_list:['.']}:{})}
  config.tools=[tool.name];props.agent.toolsets.push(config);disabledDrafts.delete(group.key);return
 }
 const selected=new Set(names(config.tools).length?names(config.tools):group.tools.map(t=>t.name))
 enabled?selected.add(tool.name):selected.delete(tool.name)
 if(selected.size)config.tools=[...selected]
 else{disabledDrafts.set(group.key,config);props.agent.toolsets.splice(props.agent.toolsets.indexOf(config),1)}
}
function setList(config:Toolset,field:string,event:Event){
 const value=(event.target as HTMLTextAreaElement).value
 listDrafts.set(config,{...listDrafts.get(config),[field]:value})
 const entries=[...new Set(value.split(/\r?\n/).map(v=>v.trim()).filter(Boolean))]
 if(entries.length)config[field]=entries;else delete config[field]
}
async function load(){
 if(busy.value)return;busy.value=true;error.value=''
 try{const r=await authFetch('/api/settings/tools',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({kind:'builtin'}),signal:controller.signal});const data=await r.json();if(!r.ok)throw new Error(data.error||'工具读取失败');catalog.value=data.groups
  // Native omitted/empty tools means all tools. Persist the visible selection
  // so saving keeps an explicit whitelist across later engine upgrades.
  for(const config of props.agent.toolsets){const group=catalog.value.find(g=>g.type===config.type);if(group?.tools.length&&!names(config.tools).length)config.tools=group.tools.map(t=>t.name)}
 }
 catch(e){if(!controller.signal.aborted)error.value=(e as Error).message}finally{busy.value=false}
}
onMounted(load);onBeforeUnmount(()=>controller.abort())
</script>
<template>
 <section class="native-tools" aria-label="Docker Agent 原生工具选择">
  <div class="native-heading"><h3>Docker Agent 原生工具</h3><small v-if="catalog.length">已启用 {{enabledCount}} 项</small></div>
  <p class="hint">{{name}} · 勾选允许使用的工具，保存后生效。</p>
  <p v-if="agent.readonly" class="hint">当前 Agent 为只读，未标注只读的工具不可用；原有选择保留。</p>
  <p v-if="busy&&!catalog.length" role="status">正在读取原生工具…</p>
  <div v-if="error" class="native-error"><p role="alert">{{error}}</p><button type="button" @click="load">重新读取</button></div>
  <template v-if="!busy&&(catalog.length||groups.length)">
   <label class="native-search">搜索工具<input v-model="search" type="search" placeholder="名称、说明或类别" /></label>
   <section v-for="group in visibleGroups" :key="group.key" class="native-group" :aria-label="titles[group.type]||group.type">
    <div class="native-group-heading"><label class="check"><input type="checkbox" :aria-label="`启用${titles[group.type]||group.type}分组`" :checked="!!group.config" :disabled="!group.config&&!group.tools.some(tool=>!restricted(group,tool))" @change="toggleGroup(group,($event.target as HTMLInputElement).checked)" /><strong>{{titles[group.type]||group.type}}</strong></label><small>{{group.tools.filter(tool=>checked(group,tool)).length}} / {{group.tools.length}} 项</small></div>
    <div class="native-rows">
    <label v-for="tool in group.tools.filter(tool=>matches(group,tool))" :key="tool.name" class="check native-row" :class="{'native-disabled':restricted(group,tool)}">
     <input type="checkbox" :aria-label="`启用 ${group.type} / ${tool.name}${group.key.endsWith('-0')?'':' / '+group.key}`" :checked="checked(group,tool)" :disabled="!!restricted(group,tool)" @change="toggle(group,tool,($event.target as HTMLInputElement).checked)" />
     <span class="native-description"><span class="native-name"><strong>{{tool.name}}</strong><small v-if="restricted(group,tool)">只读限制</small></span><span class="native-help" :title="tool.description">{{tool.description}}</span></span>
    </label>
   </div>
    <div v-if="group.config&&(['filesystem','file','todo'].includes(group.type)||group.config.readonly===true)" class="native-option">
     <template v-if="group.config">
      <p v-if="group.config.readonly===true" class="hint">此组配置为只读，仅标注为只读的工具可用。</p>
      <div v-if="['filesystem','file'].includes(group.type)" class="form-grid">
       <label>允许访问的目录<textarea :value="listDrafts.get(group.config)?.allow_list??names(group.config.allow_list).join('\n')" rows="2" :aria-label="group.key+' 允许访问的目录'" placeholder="每行一个目录，例如 ." @input="setList(group.config,'allow_list',$event)" /></label>
       <label>禁止访问的目录<textarea :value="listDrafts.get(group.config)?.deny_list??names(group.config.deny_list).join('\n')" rows="2" :aria-label="group.key+' 禁止访问的目录'" placeholder="每行一个目录" @input="setList(group.config,'deny_list',$event)" /></label>
      </div>
      <p v-if="['filesystem','file'].includes(group.type)" class="hint">“.” 表示运行工作目录；允许列表留空时不限制目录，禁止列表优先。仅作用于此类文件工具，不限制终端或 MCP。</p>
      <label v-if="group.type==='todo'" class="check"><input v-model="group.config.shared" type="checkbox" />在运行进程内共享待办（可能跨会话）</label>
     </template>
    </div>
    <p v-if="group.error" class="hint">{{group.error}}</p>
   </section>
   <p v-if="!visibleGroups.length" class="hint">没有匹配的工具。</p>
  </template>
 </section>
</template>
<style scoped>
.native-group{border:1px solid var(--tuck-border);border-radius:8px;margin-top:16px;padding:12px 16px}.native-group-heading{display:flex;align-items:center;justify-content:space-between;gap:12px;margin-bottom:8px}.native-group-heading label.check{margin:0}.native-group-heading small{color:var(--tuck-muted);white-space:nowrap}.native-rows .native-row:last-child{border-bottom:0}.native-option{margin-top:8px}.native-option .hint:last-child{margin-bottom:0}@media(max-width:600px){.native-group{padding:10px 12px}}
.native-tools{max-width:960px}.native-heading{display:flex;align-items:baseline;gap:12px;margin-bottom:8px}.native-heading small,.native-name small{color:var(--tuck-muted);font-size:var(--tuck-font-caption)}.native-search{max-width:460px}.native-rows{border-top:1px solid var(--tuck-border)}.native-rows label.native-row{margin:0;padding:10px 0;align-items:flex-start;border-bottom:1px solid var(--tuck-border);gap:10px}.native-row input{margin-top:3px!important}.native-description{min-width:0;flex:1}.native-name{display:flex;align-items:baseline;gap:10px;flex-wrap:wrap}.native-name strong{font-size:var(--tuck-font-caption);font-weight:600;overflow-wrap:anywhere}.native-help{display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden;overflow-wrap:anywhere;color:var(--tuck-muted);font-size:var(--tuck-font-caption);line-height:1.5;margin-top:3px}.native-disabled{opacity:.65}.native-options{margin-top:18px}.native-options summary{cursor:pointer;margin-bottom:12px}.native-option{border-top:1px solid var(--tuck-border);padding:12px 0}.native-option h4{font:inherit;font-weight:600;margin:0 0 8px}.native-error{color:var(--tuck-danger)}
</style>
