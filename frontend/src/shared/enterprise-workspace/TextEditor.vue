<script setup lang="ts">
import { onMounted, onBeforeUnmount, ref, watch } from 'vue'
import { basicSetup } from 'codemirror'
import { EditorView, keymap } from '@codemirror/view'
import { EditorState, Compartment } from '@codemirror/state'
import { indentWithTab } from '@codemirror/commands'
import { LanguageDescription } from '@codemirror/language'
import { languages } from '@codemirror/language-data'
import type {DocumentView} from './state'
const props = defineProps<{modelValue:string;path:string;readonly?:boolean;wrap?:boolean;position:DocumentView['source'];active:boolean}>()
const emit = defineEmits<{'update:modelValue':[value:string];save:[]}>()
const host = ref<HTMLElement>(), languageIssue=ref(''); let view:EditorView|undefined
const language = new Compartment(), editable = new Compartment(), wrapping = new Compartment()
function rememberScroll(){if(view&&props.active)Object.assign(props.position,{left:view.scrollDOM.scrollLeft,top:view.scrollDOM.scrollTop})}
function restoreScroll(){view?.requestMeasure({read:()=>undefined,write:()=>{if(view&&props.active)view.scrollDOM.scrollTo(props.position.left,props.position.top)}})}
onMounted(async () => {
  view = new EditorView({parent:host.value,state:EditorState.create({doc:props.modelValue,extensions:[basicSetup,keymap.of([{key:'Mod-s',run:()=>{emit('save');return true}},indentWithTab]),language.of([]),editable.of(EditorState.readOnly.of(!!props.readonly)),wrapping.of(props.wrap ? EditorView.lineWrapping : []),EditorView.updateListener.of(update=>{if(update.docChanged) emit('update:modelValue',update.state.doc.toString())}),EditorView.theme({'&':{height:'100%',fontSize:'13px',backgroundColor:'var(--tuck-background)',color:'var(--tuck-text)'},'.cm-scroller':{overflow:'auto',fontFamily:'Consolas,monospace'},'.cm-gutters':{backgroundColor:'var(--tuck-surface)',color:'var(--tuck-muted)',borderRight:'1px solid var(--tuck-border)'},'.cm-content':{caretColor:'var(--tuck-text)'},'.cm-activeLine':{backgroundColor:'var(--tuck-hover)'}})]})})
  const end=view.state.doc.length
  view.dispatch({selection:{anchor:Math.min(props.position.anchor,end),head:Math.min(props.position.head,end)}})
  view.scrollDOM.addEventListener('scroll',rememberScroll,{passive:true})
  restoreScroll()
  const description = LanguageDescription.matchFilename(languages,props.path)
  if (description) {
    try { const support = await description.load(); view?.dispatch({effects:language.reconfigure(support)}) }
    catch { if(view)languageIssue.value='语法高亮未加载，仍可编辑；请先保存修改，再刷新页面重试。' }
  }
})
watch(()=>props.modelValue,value=>{if(view && value !== view.state.doc.toString()) view.dispatch({changes:{from:0,to:view.state.doc.length,insert:value}})})
watch(()=>props.readonly,value=>view?.dispatch({effects:editable.reconfigure(EditorState.readOnly.of(!!value))}))
watch(()=>props.wrap,value=>view?.dispatch({effects:wrapping.reconfigure(value ? EditorView.lineWrapping : [])}))
watch(()=>props.active,active=>{if(active)restoreScroll()},{flush:'post'})
onBeforeUnmount(()=>{if(view){rememberScroll();Object.assign(props.position,{anchor:view.state.selection.main.anchor,head:view.state.selection.main.head});view.scrollDOM.removeEventListener('scroll',rememberScroll);view.destroy();view=undefined}})
</script>
<template><div class="workspace-code"><p v-if="languageIssue" role="status">{{languageIssue}}</p><div ref="host" class="workspace-code-editor" /></div></template>
<style scoped>.workspace-code{height:100%;min-height:120px;min-width:0;overflow:hidden;display:flex;flex-direction:column}.workspace-code-editor{flex:1;min-height:0;overflow:hidden}.workspace-code p{margin:0;padding:5px 9px;color:var(--tuck-muted);font-size:11px;flex-shrink:0}</style>
