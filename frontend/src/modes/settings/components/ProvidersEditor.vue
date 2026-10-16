<script setup lang="ts">
import {computed,ref} from 'vue'
import {useRoute} from 'vue-router'
import {router} from '../../../app/router'
import ModelEditor from './ModelEditor.vue'
import PlatformModelDetails from './PlatformModelDetails.vue'
import {platformProviderPath,platformModelPath} from './catalog-paths'
import type {Settings,Provider} from './types'
const props=defineProps<{data:Settings}>(),route=useRoute(),idDraft=ref<string>()
const provider=computed(()=>props.data.config.providers.find(p=>p.id===route.params.provider))
const model=computed(()=>provider.value?.models.find(m=>m.modelId===route.params.model))
const managedProvider=computed(()=>props.data.managedProviders.find(p=>p.providerId===route.params.managedProvider))
const managedModels=computed(()=>props.data.managedModels.filter(m=>m.providerId===route.params.managedProvider))
const managedModel=computed(()=>managedModels.value.find(m=>m.modelId===route.params.managedModel))
const protocolLabel=(p:string)=>p==='OPENAI_RESPONSES'?'OpenAI Responses':p==='OPENAI_CHAT_COMPLETIONS'?'OpenAI Chat Completions':p
const defaultModels=computed(()=>[...props.data.managedModels.filter(m=>m.selectable).map(m=>({...m,sourceLabel:'平台下发'})),...props.data.config.providers.flatMap(p=>p.models.filter(m=>p.enabled&&m.enabled).map(m=>({...m,sourceLabel:'用户自定义'})))])
const path=(p:Provider)=>'/settings/providers/'+encodeURIComponent(p.id)
function addProvider(){const p:Provider={id:'provider_'+Date.now().toString(36),displayName:'新提供商',enabled:true,protocol:'OPENAI_CHAT_COMPLETIONS',baseUrl:'https://api.example.com/v1',models:[]};props.data.config.providers.push(p);void router.push(path(p))}
function addModel(p:Provider){const m={modelId:'model_'+Date.now().toString(36),upstreamModelKey:'',displayName:'新模型',enabled:true,...props.data.config.modelDefaults,inputModalities:['TEXT'],capabilities:['TOOL']};p.models.push(m);void router.push(path(p)+'/models/'+encodeURIComponent(m.modelId))}
function rename(p:Provider,event:Event){const value=(event.target as HTMLInputElement).value.trim();if(!/^[A-Za-z0-9_.-]{1,128}$/.test(value)||props.data.config.providers.some(item=>item!==p&&item.id===value)){idDraft.value=undefined;return}p.id=value;idDraft.value=undefined;void router.replace(path(p))}
function remove(p:Provider){props.data.config.providers.splice(props.data.config.providers.indexOf(p),1);void router.push('/settings/providers')}
</script>
<template>
 <section aria-label="提供商与模型配置">
  <template v-if="route.meta.managed">
   <PlatformModelDetails v-if="managedProvider&&managedModel" :model="managedModel" :data="data" />
   <template v-else-if="managedProvider&&!route.params.managedModel">
    <div class="detail-heading"><strong>{{managedProvider.displayName}}</strong><span class="source-tag">平台下发 · 只读</span></div>
    <p class="hint">提供商与模型由 Orchelm 管理，连接凭据由平台托管。</p>
    <dl class="settings-facts"><div><dt>提供商标识</dt><dd>{{managedProvider.providerId}}</dd></div><div><dt>协议</dt><dd>{{protocolLabel(managedProvider.protocol)}}</dd></div><div><dt>状态</dt><dd>{{managedProvider.enabled?'平台已启用':'平台已停用'}}</dd></div></dl>
    <div class="section-heading"><h3>模型</h3></div>
    <div class="settings-cards"><RouterLink v-for="m in managedModels" :key="m.modelId" class="settings-card" :to="platformModelPath(m)" :aria-label="`查看平台模型 ${m.displayName}`"><strong>{{m.displayName}}</strong><span>平台字段只读 · 可配置本地参数</span><small>{{m.modelId}}</small><span>{{m.selectable?'可用':m.reason||'不可用'}}</span></RouterLink></div>
    <p v-if="!managedModels.length" class="hint">此提供商暂无下发模型。</p>
   </template>
   <p v-else role="alert">此平台提供商或模型已不可用，请返回列表查看最新配置。</p>
  </template>
  <template v-else-if="!route.params.provider">
   <p v-if="data.mode==='platform'" class="hint">平台下发与用户自定义的模型可同时使用；平台字段只读，未下发的运行参数可在本机配置。</p>
   <div class="settings-toolbar"><label>默认模型（可选）<select v-model="data.config.defaultModelId"><option value="">未指定（沿用平台默认，如有）</option><option v-if="data.config.defaultModelId&&!defaultModels.some(m=>m.modelId===data.config.defaultModelId)" :value="data.config.defaultModelId" disabled>{{data.config.defaultModelId}}（不可用）</option><option v-for="m in defaultModels" :key="m.modelId" :value="m.modelId">{{ m.displayName }} · {{ m.sourceLabel }}</option></select></label><button class="primary" @click="addProvider">添加提供商</button></div>
   <div class="settings-cards">
    <RouterLink v-for="p in data.managedProviders" :key="'platform:'+p.providerId" class="settings-card" :to="platformProviderPath(p.providerId)" :aria-label="`查看平台提供商 ${p.displayName}`"><strong>{{p.displayName}}</strong><span>平台下发 · 只读</span><small>{{protocolLabel(p.protocol)}}</small><span>{{data.managedModels.filter(m=>m.providerId===p.providerId).length}} 个模型 · {{p.enabled?'平台已启用':'平台已停用'}}</span></RouterLink>
    <RouterLink v-for="p in data.config.providers" :key="'local:'+p.id" class="settings-card" :to="path(p)" :aria-label="`编辑提供商 ${p.id}`"><strong>{{ p.displayName }}</strong><span>用户自定义</span><small>{{protocolLabel(p.protocol)}}</small><span>{{ p.models.length }} 个模型 · {{ p.enabled?'已启用':'已停用' }}</span></RouterLink>
   </div>
   <p v-if="!data.config.providers.length&&!data.managedProviders.length" class="hint">尚未添加提供商。</p>
  </template>
  <template v-else-if="provider&&!route.params.model">
   <div class="detail-heading"><strong>{{ provider.displayName }}</strong><span class="source-tag">用户自定义</span></div>
   <div class="form-grid">
    <label>提供商标识<input :value="idDraft??provider.id" @input="idDraft=($event.target as HTMLInputElement).value" :readonly="provider.hasApiKey!==undefined" @blur="rename(provider,$event)" /></label><label>名称<input v-model.trim="provider.displayName" /></label>
    <label>协议<select v-model="provider.protocol"><option value="OPENAI_CHAT_COMPLETIONS">OpenAI Chat Completions</option><option value="OPENAI_RESPONSES">OpenAI Responses</option></select></label><label>API 基础地址<input v-model.trim="provider.baseUrl" placeholder="https://api.example.com/v1" /></label>
    <label>API Key<input :value="provider.apiKey" @input="provider.apiKey=($event.target as HTMLInputElement).value||undefined" type="password" autocomplete="new-password" :placeholder="provider.apiKey===''?'保存后清除密钥':provider.hasApiKey?'已设置；不输入则保留':'无需认证可留空'" /></label><label class="check"><input v-model="provider.enabled" type="checkbox" />启用提供商</label>
   </div>
   <div class="line"><small>密钥不回显；留空保留已有值。</small><button @click="provider.apiKey='';provider.hasApiKey=false">清除密钥</button><button @click="remove(provider)">删除提供商</button></div>
   <div class="section-heading"><h3>模型</h3><button @click="addModel(provider)">添加模型</button></div>
   <div class="settings-cards"><RouterLink v-for="m in provider.models" :key="m.modelId" class="settings-card" :to="path(provider)+'/models/'+encodeURIComponent(m.modelId)" :aria-label="`编辑模型 ${m.modelId}`"><strong>{{ m.displayName }}</strong><span>用户自定义</span><small>{{ m.modelId }}</small><span>{{ m.enabled?'已启用':'已停用' }} · 上下文 {{ m.contextWindow?.toLocaleString()??'未填写' }} · 输出 {{ m.maxOutputTokens?.toLocaleString()??'未填写' }}</span></RouterLink></div>
  </template>
  <ModelEditor v-else-if="provider&&model" :key="route.path" :data="data" :provider="provider" :model="model" />
  <template v-else><p role="alert">此提供商或模型不存在，请重新选择。</p></template>
 </section>
</template>
