<script setup lang="ts">
import {computed,ref,watch,onBeforeUnmount} from 'vue'
import {router} from '../../../app/router'
import {reasoningEfforts,reasoningLabels,modelReasoningOptions} from '../../../../shared/reasoning.mjs'
import type {Settings,Model,Provider} from './types'
import {authFetch} from '../../../app/auth'
import ModelParameterFields from './ModelParameterFields.vue'
const props=defineProps<{data:Settings;provider:Provider;model:Model}>()
const controller=new AbortController()
onBeforeUnmount(()=>controller.abort())
const parent=computed(()=>'/settings/providers/'+encodeURIComponent(props.provider.id))
const idDraft=ref<string>()
const testing=ref(false),testResult=ref<{ok:boolean;message:string;reply?:string;elapsedMs?:number}>()
const testInput=computed(()=>JSON.stringify({provider:{id:props.provider.id,protocol:props.provider.protocol,baseUrl:props.provider.baseUrl,apiKey:props.provider.apiKey},model:{upstreamModelKey:props.model.upstreamModelKey}}))
watch(testInput,()=>{testResult.value=undefined})
async function verify(){
 if(testing.value)return
 const input=testInput.value
 testing.value=true;testResult.value=undefined
 try{
  const r=await authFetch('/api/settings/model-test',{method:'POST',signal:controller.signal,headers:{'Content-Type':'application/json'},body:input})
  const result=await r.json()
  if(!controller.signal.aborted&&input===testInput.value)testResult.value=r.ok?result:{ok:false,message:result.error||'验证失败'}
 }catch{if(!controller.signal.aborted&&input===testInput.value)testResult.value={ok:false,message:'验证请求失败，请检查连接'}}finally{testing.value=false}
}
function rename(event:Event){const next=(event.target as HTMLInputElement).value.trim(),old=props.model.modelId;if(!next||props.data.config.providers.some(p=>p.models.some(m=>m!==props.model&&m.modelId===next))){idDraft.value=undefined;return}props.model.modelId=next;if(props.data.config.defaultModelId===old)props.data.config.defaultModelId=next;if(props.data.config.defaultAttachmentInspectionModelId===old)props.data.config.defaultAttachmentInspectionModelId=next;for(const a of Object.values(props.data.config.agents)){if(a.modelId===old)a.modelId=next;if(a.attachmentInspectionModelId===old)a.attachmentInspectionModelId=next;}void router.replace(parent.value+'/models/'+encodeURIComponent(next))}
function toggleEffort(value:string,checked:boolean){const selected=new Set(modelReasoningOptions(props.model).slice(1));checked?selected.add(value):selected.delete(value);props.model.reasoningEfforts=reasoningEfforts.filter(v=>selected.has(v))}
function remove(){props.provider.models.splice(props.provider.models.indexOf(props.model),1);void router.push(parent.value)}
</script>
<template>
 <section aria-label="模型配置">
  <div class="detail-heading"><strong>{{ model.displayName||'模型' }}</strong><span class="source-tag">用户自定义</span></div>
  <div class="form-grid">
   <label>模型标识<input aria-label="模型标识" :value="idDraft??model.modelId" @input="idDraft=($event.target as HTMLInputElement).value" :readonly="data.savedModelIds.includes(model.modelId)" @blur="rename" /><small>Tuck 内唯一引用，保存后固定；修改上游名称不会改变已有会话的引用。</small></label>
   <label>显示名称<input v-model.trim="model.displayName" /></label>
   <label>上游模型名称<input aria-label="上游模型名称" v-model.trim="model.upstreamModelKey" placeholder="供应商 API 的 model 值" /><small>实际发送给供应商的 model，可与模型标识相同。</small></label>
   <label class="check"><input v-model="model.enabled" type="checkbox" />启用模型</label>
  </div>
  <ModelParameterFields :key="data.revision" :model-value="model" @change="(key,value)=>Object.assign(model,{[key]:value})" />
  <label class="check"><input v-model="model.capabilities" type="checkbox" value="TOOL" />支持工具调用（聊天 Agent 需要，专用识图不需要）</label>
  <label class="check"><input v-model="model.inputModalities" type="checkbox" value="IMAGE" />支持图片输入</label>
  <div class="settings-group"><h3>推理</h3><label class="check"><input v-model="model.capabilities" type="checkbox" value="REASONING" @change="!($event.target as HTMLInputElement).checked&&(delete model.reasoningEfforts)" />允许手动选择推理等级</label>
   <div v-if="model.capabilities.includes('REASONING')" class="checks"><label v-for="effort in reasoningEfforts" :key="effort" class="check"><input type="checkbox" :checked="modelReasoningOptions(model).includes(effort)" @change="toggleEffort(effort,($event.target as HTMLInputElement).checked)" />{{ reasoningLabels[effort] }}</label></div>
   <p class="hint">默认使用“自动”，不发送推理等级。仅勾选该模型与当前 API 支持的值；不会按模型名称猜测能力。</p>
  </div>
  <div class="settings-group"><div class="section-heading"><h3>连接验证</h3><button :disabled="testing||!model.upstreamModelKey||!provider.baseUrl" @click="verify">{{testing?'正在验证…':'验证连接'}}</button></div><p class="hint">验证一次短文本交互，不验证工具调用、图片或已有对话历史。可能产生少量费用，不保存配置。</p><p v-if="testResult" :role="testResult.ok?'status':'alert'">{{testResult.message}}{{testResult.elapsedMs!==undefined?` · ${testResult.elapsedMs} ms`:''}}<span v-if="testResult.reply"> · {{testResult.reply}}</span></p></div>
  <div class="line end"><button @click="remove">删除模型</button></div>
 </section>
</template>
