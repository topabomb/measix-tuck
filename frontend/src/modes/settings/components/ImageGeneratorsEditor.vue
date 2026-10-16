<script setup lang="ts">
import {computed,ref} from 'vue'
import type {Settings,ImageGenerator} from './types'
import {imageGeneratorOptions} from './image-generator-options'
const props=defineProps<{data:Settings}>()
const local=computed(()=>props.data.config.imageGenerators??[])
const managed=computed(()=>props.data.imageCatalog?.filter(r=>r.source==='platform')??[])
const choices=computed(()=>imageGeneratorOptions(props.data))
const idDrafts=ref(new Map<ImageGenerator,string>()),idErrors=ref(new Map<ImageGenerator,string>())
const selection=computed({get(){const r=props.data.config.defaultImageGenerator;return r?.source&&r.imageId?r.source+':'+r.imageId:''},set(value:string){const [source,imageId]=value.split(':');props.data.config.defaultImageGenerator=value?{source,imageId}:{}}})
function add(){const r:ImageGenerator={imageId:'image_'+Date.now().toString(36),providerId:props.data.config.providers[0]?.id,displayName:'新生图资源',enabled:true,protocol:'OPENAI_IMAGES_GENERATIONS',upstreamModelKey:'',responseFormat:''};(props.data.config.imageGenerators??=[]).push(r)}
function remove(r:ImageGenerator){props.data.config.imageGenerators=local.value.filter(v=>v!==r);idDrafts.value.delete(r);idErrors.value.delete(r)}
function editId(r:ImageGenerator,event:Event){idDrafts.value.set(r,(event.target as HTMLInputElement).value);idErrors.value.delete(r)}
function rename(r:ImageGenerator){
 const draft=idDrafts.value.get(r);idDrafts.value.delete(r)
 if(draft===undefined||props.data.lockedSections?.includes('agents'))return
 const value=draft.trim()
 if(!/^[A-Za-z0-9_.-]{1,128}$/.test(value)){idErrors.value.set(r,'标识需为 1–128 位字母、数字、点、下划线或连字符，已保留原标识。');return}
 if(local.value.some(other=>other!==r&&other.imageId===value)){idErrors.value.set(r,'此生图资源标识已存在，已保留原标识。');return}
 const selected=props.data.config.defaultImageGenerator
 if(selected?.source==='local'&&selected.imageId===r.imageId)selected.imageId=value
 for(const agent of Object.values(props.data.config.agents)){const ref=agent.imageGenerator;if(ref?.source==='local'&&ref.imageId===r.imageId)ref.imageId=value}
 r.imageId=value
}
</script>
<template>
 <section aria-label="文生图资源配置" class="image-generator-settings">
  <div class="section-heading"><h3>文生图工具</h3><button :disabled="!data.config.providers.length" @click="add">添加生图资源</button></div>
  <p class="hint">生图资源复用提供商的地址和密钥，独立选择模型与协议。助手通过工具生成图片，原图保存在任务文件中。</p>
  <label>默认生图资源<select v-model="selection" aria-label="默认生图资源"><option value="">未指定（沿用平台默认，如有）</option><option v-if="selection&&!choices.some(r=>r.selector===selection)" :value="selection" disabled>{{selection}}（不可用）</option><option v-for="r in choices" :key="r.selector" :value="r.selector">{{r.label}}</option></select></label>
  <p v-if="!local.length&&!managed.length" class="hint">尚未配置生图资源。先添加提供商，再添加生图资源；助手不会改用聊天模型生成图片。</p>
  <div v-for="(r,index) in local" :key="index" class="image-generator-card" :aria-label="`生图资源 ${r.displayName}`">
   <div class="detail-heading"><strong>{{r.displayName}}</strong><span class="source-tag">用户自定义</span><button @click="remove(r)">删除生图资源</button></div>
   <div class="form-grid">
    <label>生图资源名称<input v-model.trim="r.displayName" maxlength="80" /></label>
    <label>生图资源标识<input aria-label="生图资源标识" :aria-describedby="idErrors.has(r)?'image-id-error-'+index:undefined" :value="idDrafts.get(r)??r.imageId" :disabled="data.lockedSections?.includes('agents')" @input="editId(r,$event)" @blur="rename(r)" maxlength="128" /><small v-if="data.lockedSections?.includes('agents')">修改标识需先解锁 Agent 配置，以维护资源引用。</small><small v-if="idErrors.get(r)" :id="'image-id-error-'+index" role="alert">{{idErrors.get(r)}}</small></label>
    <label>生图提供商<select v-model="r.providerId"><option v-if="!data.config.providers.some(p=>p.id===r.providerId)" :value="r.providerId" disabled>{{r.providerId}}（不可用）</option><option v-for="p in data.config.providers" :key="p.id" :value="p.id">{{p.displayName}}</option></select></label>
    <label>生图协议<select v-model="r.protocol"><option value="OPENAI_IMAGES_GENERATIONS">OpenAI Images（生成 / 参考图编辑）</option></select></label>
    <label>生图上游模型<input v-model.trim="r.upstreamModelKey" placeholder="填写服务提供的模型名称" /></label>
    <label>图片返回格式<select v-model="r.responseFormat"><option value="">服务默认（GPT 图片模型）</option><option value="b64_json">Base64（兼容服务）</option><option value="url">URL（公开 HTTPS）</option></select></label>
    <label class="check"><input v-model="r.enabled" type="checkbox" />启用生图资源</label>
   </div>
   <p class="hint">默认生成 1 张，可选 1–4 张；尺寸由服务决定。参考图最多 4 张，需服务支持标准 OpenAI Images Edits 文件上传；非必要不使用。GPT 图片模型请选择服务默认返回格式。</p>
  </div>
  <div v-for="r in managed" :key="r.imageId" class="image-generator-card">
   <div class="detail-heading"><strong>{{r.displayName}}</strong><span class="source-tag">平台下发 · 只读</span></div>
   <dl class="settings-facts"><div><dt>资源标识</dt><dd>{{r.imageId}}</dd></div><div><dt>协议</dt><dd>{{r.protocol}}</dd></div><div><dt>状态</dt><dd>{{r.selectable?'已配置':r.reason}}</dd></div><div><dt>单次最多</dt><dd>{{Math.min(4,r.maxImagesPerRequest??4)}} 张</dd></div><div><dt>参考图</dt><dd>平台合同暂不支持</dd></div></dl>
  </div>
 </section>
</template>
<style scoped>
.image-generator-settings{margin-top:24px;min-width:0}
.image-generator-card{margin-top:16px;padding:16px;border:1px solid var(--tuck-border);border-radius:12px;min-width:0}
.image-generator-card .detail-heading{flex-wrap:wrap}
.image-generator-card input,.image-generator-card select{min-width:0;max-width:100%}
</style>
