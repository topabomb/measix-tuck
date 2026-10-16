<script setup lang="ts">
import type { DockerTask } from '../workspace/runtime'
import {computed,onBeforeUnmount,ref,watch} from 'vue'
import {authFetch} from '../../app/auth'
import {appUrl} from '../../app/urls'
import {renderMarkdownCached} from '../../engine/vue/renderers/markdown-cache'
import { turnHasEnded } from '../../../shared/task-channel.mjs'
const props=defineProps<{task: DockerTask}>()
const reply=ref(''),error=ref(''),loading=ref(false)
let controller:AbortController|undefined
const html=computed(()=>{
  const doc=new DOMParser().parseFromString(renderMarkdownCached(`task-reply:${props.task.id}:${reply.value}`,0,reply.value),'text/html')
  for(const el of doc.querySelectorAll('[href],[src]'))for(const attr of ['href','src']){
    const value=el.getAttribute(attr)
    if(value?.startsWith('/api/'))el.setAttribute(attr,appUrl(value))
  }
  for(const table of doc.querySelectorAll('table')){const scroll=doc.createElement('div');scroll.className='task-table-scroll';scroll.tabIndex=0;table.replaceWith(scroll);scroll.append(table)}
  return doc.body.innerHTML
})
async function refreshReply(){
  controller?.abort();reply.value='';error.value='';loading.value=false
  if(!turnHasEnded(props.task))return
  const request=controller=new AbortController();loading.value=true
  try {
    const response=await authFetch(`/api/tasks/${encodeURIComponent(props.task.id)}/result`,{signal:request.signal})
    if(!response.ok)throw new Error('回答读取失败')
    const data=await response.json();if(!request.signal.aborted)reply.value=data.reply||''
  }catch(reason){if(!request.signal.aborted)error.value=(reason as Error).message}
  finally{if(!request.signal.aborted)loading.value=false}
}
watch([()=>props.task.id,()=>props.task.turnCount,()=>props.task.lastTurnReason,()=>props.task.status],refreshReply,{immediate:true})
onBeforeUnmount(()=>controller?.abort())
</script>
<template>
<div class="task-answer">
      <p v-if="loading" class="task-empty">正在读取回答…</p>
      <p v-else-if="error" class="task-error" role="alert">{{error}} <button @click="refreshReply">重试</button></p>
      <article v-else-if="reply" class="markdown-body task-final-reply" aria-label="任务回答" v-html="html" />
      <p v-else class="task-empty">{{turnHasEnded(task)?'本轮暂无最终回复。':'本轮尚未结束。'}}</p>
    </div>
</template>
<style scoped>
.task-answer{margin-top:20px;min-width:0}.task-final-reply{overflow-wrap:anywhere}.task-final-reply :deep(.task-table-scroll){overflow-x:auto;max-width:100%}.task-final-reply :deep(pre){overflow-x:auto}
</style>
