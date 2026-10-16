<script setup lang="ts">
import { markRaw, nextTick, onBeforeUnmount, onMounted, shallowRef, ref, watch, type Component } from 'vue'
import type {DocumentView} from './state'
const props=defineProps<{modelValue:string;path:string;readonly?:boolean;wrap?:boolean;position:DocumentView['source'];active:boolean}>()
const emit=defineEmits<{'update:modelValue':[value:string];save:[]}>()
const editor=shallowRef<Component>(),failed=ref(false),fallback=ref<HTMLTextAreaElement>();let disposed=false
function remember(){const node=fallback.value;if(node&&props.active)Object.assign(props.position,{left:node.scrollLeft,top:node.scrollTop,anchor:node.selectionStart,head:node.selectionEnd})}
async function restore(){await nextTick();const node=fallback.value;if(node&&props.active){node.setSelectionRange(props.position.anchor,props.position.head);node.scrollTo(props.position.left,props.position.top)}}
watch([failed,()=>props.active],()=>{void restore()},{flush:'post'})
onMounted(async()=>{try{const loaded=await import('./TextEditor.vue');if(!disposed)editor.value=markRaw(loaded.default)}catch{if(!disposed)failed.value=true}})
onBeforeUnmount(()=>{disposed=true})
function shortcut(event:KeyboardEvent){if((event.ctrlKey||event.metaKey)&&event.key.toLowerCase()==='s'){event.preventDefault();if(!props.readonly)emit('save')}}
</script>
<template>
  <component :is="editor" v-if="editor" :model-value="modelValue" :path="path" :readonly="readonly" :wrap="wrap" :position="position" :active="active" @update:model-value="emit('update:modelValue',$event)" @save="emit('save')" />
  <div v-else-if="failed" class="workspace-basic-code">
    <p role="status">编辑器加载失败，已切换为基础文本编辑。请先保存修改，再刷新页面重试。</p>
    <textarea ref="fallback" class="workspace-basic-editor" aria-label="文件源码" :value="modelValue" :readonly="readonly" :wrap="wrap?'soft':'off'" :style="{whiteSpace:wrap?'pre-wrap':'pre'}" spellcheck="false" @input="emit('update:modelValue',($event.target as HTMLTextAreaElement).value)" @scroll="remember" @select="remember" @keydown="shortcut" />
  </div>
  <p v-else class="ew-empty" role="status">编辑器加载中…</p>
</template>
<style scoped>.workspace-basic-code{display:flex;flex-direction:column;min-height:0;height:100%;min-width:0}.workspace-basic-code p{flex-shrink:0;margin:0;padding:8px;color:var(--tuck-muted);font-size:12px}.workspace-basic-editor{flex:1;min-height:120px;min-width:0;resize:none;box-sizing:border-box;border:0;padding:12px;background:var(--tuck-background);color:var(--tuck-text);font:13px/1.6 Consolas,monospace;overflow:auto}</style>
