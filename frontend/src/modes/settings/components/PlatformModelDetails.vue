<script setup lang="ts">
import {computed,ref} from 'vue'
import ModelParameterFields from './ModelParameterFields.vue'
import type {ManagedModel,ModelParameters,Settings} from './types'
const props=defineProps<{model:ManagedModel;data:Settings}>()
const resetVersion=ref(0)
const override=computed(()=>props.data.config.platformModelSettings?.[props.model.settingsKey])
const parameters=computed(()=>{
 const values:ModelParameters={...props.data.config.modelDefaults,...override.value}
 for(const key of props.model.platformDefinedFields||[])if(['contextWindow','maxOutputTokens','temperature','topP'].includes(key))values[key as keyof ModelParameters]=props.model[key as keyof ModelParameters]
 return values
})
function change(key:keyof ModelParameters,value:number|undefined){
 if(props.model.platformDefinedFields?.includes(key))return
 const entries=props.data.config.platformModelSettings??={}
 const next={...entries[props.model.settingsKey]}
 if(value===undefined)delete next[key];else next[key]=value
 if(Object.keys(next).length)entries[props.model.settingsKey]=next;else delete entries[props.model.settingsKey]
}
function reset(){if(props.data.config.platformModelSettings)delete props.data.config.platformModelSettings[props.model.settingsKey];resetVersion.value++}
</script>
<template>
 <section aria-label="平台模型详情">
  <div class="detail-heading"><strong>{{ model.displayName }}</strong><span class="source-tag">平台下发</span></div>
  <p class="hint">模型身份、状态与能力由 Orchelm 管理，以下平台字段只读；未下发的运行参数可在本机设置。</p>
  <dl class="settings-facts">
   <div><dt>模型标识</dt><dd>{{model.modelId}}</dd></div>
   <div><dt>状态</dt><dd>{{model.selectable?'可用':model.reason||'不可用'}}</dd></div>
   <div><dt>输入</dt><dd>{{model.inputModalities.includes('IMAGE')?'文字、图片':'文字'}}</dd></div>
   <div><dt>能力</dt><dd>{{model.capabilities.map(c=>({TOOL:'工具调用',REASONING:'推理'} as Record<string,string>)[c]||c).join('、')||'未提供'}}</dd></div>
  </dl>
  <div class="settings-group"><h3>本地运行参数</h3><p class="hint">上下文窗口与最大输出是 Tuck 的运行预算，不代表模型实际容量。平台下发的参数保持只读；本地修改随页面底部的配置一起保存。</p>
   <ModelParameterFields :key="data.revision+':'+resetVersion" :model-value="parameters" :locked="model.platformDefinedFields" @change="change" />
   <div class="line end"><button :disabled="!override" @click="reset">恢复客户端默认</button></div>
  </div>
 </section>
</template>
