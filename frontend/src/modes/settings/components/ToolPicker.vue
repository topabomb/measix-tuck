<script setup lang="ts">
import {computed,ref,watch} from 'vue'
import {authFetch} from '../../../app/auth'
import type {McpState} from './types'
const props=defineProps<{modelValue?:string[];request:Record<string,unknown>;allowEmpty?:boolean}>()
const emit=defineEmits<{'update:modelValue':[value:string[]|undefined];verified:[state:McpState]}>()
const tools=ref<{name:string;description:string;readonly:boolean}[]>(),busy=ref(false),error=ref(''),search=ref('')
let generation=0
watch(()=>JSON.stringify(props.request),()=>{generation++;tools.value=undefined;error.value=''})
const choices=computed(()=>[...(tools.value||[]),...(props.modelValue||[]).filter(name=>!tools.value?.some(t=>t.name===name)).map(name=>({name,description:'已配置；尚未从当前服务确认',readonly:false}))].filter(t=>(t.name+' '+t.description).toLowerCase().includes(search.value.toLowerCase())))
async function load(){if(busy.value)return;const current=++generation;busy.value=true;error.value='';try{const r=await authFetch('/api/settings/tools',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(props.request)});const value=await r.json();if(current!==generation)return;if(!r.ok)throw new Error(value.error||'工具读取失败');tools.value=value.tools;if(value.state)emit('verified',value.state)}catch(e){if(current===generation)error.value=(e as Error).message}finally{busy.value=false}}
function toggle(name:string,checked:boolean){const selected=new Set(props.modelValue||tools.value?.map(t=>t.name)||[]);checked?selected.add(name):selected.delete(name);if(selected.size||props.allowEmpty)emit('update:modelValue',[...selected])}
</script>
<template>
 <section class="tool-picker" aria-label="可用工具">
  <div class="section-heading"><h3>可用工具</h3><button type="button" :disabled="busy" @click="load">{{busy?'正在读取…':tools?'刷新工具':'读取工具列表'}}</button></div>
  <p class="hint">读取工具名称和说明，不执行工具。默认使用全部工具，也可选择部分{{allowEmpty?'或全部停用':''}}；调用参数由 Agent 根据任务填写。</p>
  <label class="check"><input type="checkbox" :checked="!modelValue" :disabled="!tools?.length&&!modelValue" @change="emit('update:modelValue',($event.target as HTMLInputElement).checked?undefined:tools?.map(t=>t.name))" />使用全部工具</label>
  <p v-if="error" role="alert">{{error}}</p>
  <template v-if="tools||modelValue">
   <label v-if="choices.length>6||search">搜索工具<input v-model="search" type="search" placeholder="名称或说明" /></label>
   <p v-if="tools?.length===0" class="hint">服务未返回可用工具。</p>
   <p v-if="allowEmpty&&modelValue?.length===0" class="hint">此连接的工具已全部停用；保存后生效。</p>
   <div class="tool-picker-list"><label v-for="item in choices" :key="item.name" class="check tool-choice"><input type="checkbox" :aria-label="`启用 ${item.name}`" :checked="!modelValue||modelValue.includes(item.name)" :disabled="!modelValue||(!allowEmpty&&modelValue.length===1&&modelValue.includes(item.name))" @change="toggle(item.name,($event.target as HTMLInputElement).checked)" /><span><strong>{{item.name}}</strong><small>{{item.description}}</small></span></label></div>
  </template>
 </section>
</template>
<style scoped>.tool-picker{margin:16px 0;border-top:1px solid var(--tuck-border);padding-top:14px}.tool-picker-list{display:grid;gap:8px;margin-top:12px;max-height:360px;overflow:auto}.tool-choice{align-items:flex-start!important;background:var(--tuck-surface);border:1px solid var(--tuck-border);border-radius:6px;padding:10px}.tool-choice input{margin-top:2px!important}.tool-choice span{min-width:0;overflow-wrap:anywhere}.tool-choice small{display:block;margin-top:4px}.tool-choice strong{font-weight:500}</style>
