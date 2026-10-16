<script setup lang="ts">
import {reactive} from 'vue'
import type {ModelParameters} from './types'
defineProps<{modelValue:ModelParameters;locked?:string[]}>()
const emit=defineEmits<{change:[key:keyof ModelParameters,value:number|undefined]}>()
const drafts=reactive<Partial<Record<keyof ModelParameters,string>>>({})
function update(key:keyof ModelParameters,event:Event){const value=(event.target as HTMLInputElement).value;drafts[key]=value;emit('change',key,value===''?undefined:Number(value))}
</script>
<template>
 <div class="form-grid">
  <label>上下文窗口<input :value="drafts.contextWindow??modelValue.contextWindow" type="number" min="1" max="10000000" :readonly="locked?.includes('contextWindow')" @input="update('contextWindow',$event)" /></label>
  <label>最大输出<input :value="drafts.maxOutputTokens??modelValue.maxOutputTokens" type="number" min="1" max="10000000" :readonly="locked?.includes('maxOutputTokens')" @input="update('maxOutputTokens',$event)" /></label>
  <label>Temperature（可选）<input :value="drafts.temperature??modelValue.temperature" type="number" min="0" max="2" step="0.1" :readonly="locked?.includes('temperature')" placeholder="提供商默认" @input="update('temperature',$event)" /></label>
  <label>Top P（可选）<input :value="drafts.topP??modelValue.topP" type="number" min="0" max="1" step="0.05" :readonly="locked?.includes('topP')" placeholder="提供商默认" @input="update('topP',$event)" /></label>
 </div>
</template>
