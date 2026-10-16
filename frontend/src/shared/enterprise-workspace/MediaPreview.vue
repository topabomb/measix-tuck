<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, toRefs, watch } from 'vue'
import type {DocumentView} from './state'

const props=defineProps<{url:string;kind:'audio'|'video';available:boolean;state:DocumentView['media'];active:boolean}>()
const media=ref<HTMLMediaElement>(),hidden=ref(document.hidden),attached=ref(props.available&&props.active&&!document.hidden)
const {position}=toRefs(props.state),error=ref(''),reason=ref(!props.available?'企业工作区当前不可用':document.hidden?'页面已转入后台，已停止读取':'')
let resumeRequested=false,disposed=false,operation=0

function release(message:string) {
  operation++;resumeRequested=false;attached.value=false;reason.value=message
  const element=media.value
  if(!element)return
  if(Number.isFinite(element.currentTime)&&element.readyState>=1){position.value=element.currentTime;Object.assign(props.state,{rate:element.playbackRate,volume:element.volume,muted:element.muted})}
  element.pause();element.removeAttribute('src');element.load()
}
function time(seconds:number) {const value=Math.floor(seconds);return `${Math.floor(value/60)}:${String(value%60).padStart(2,'0')}`}
function play() {
  const element=media.value,current=operation
  if(!element||!attached.value||hidden.value||!props.available||!props.active||disposed)return
  try{element.currentTime=Number.isFinite(element.duration)?Math.min(position.value,element.duration):position.value}catch{/* Metadata may not yet permit seeking; native controls remain available. */}
  element.playbackRate=props.state.rate;element.volume=props.state.volume;element.muted=props.state.muted
  if(!resumeRequested)return
  resumeRequested=false
  void element.play().catch(()=>{if(current===operation&&!disposed&&attached.value)error.value='未能开始播放，请使用播放器的播放按钮'})
}
async function resume() {
  if(!props.available||!props.active||hidden.value||disposed)return
  error.value='';reason.value='';resumeRequested=true;attached.value=true
  await nextTick()
  if(media.value&&media.value.readyState>=1)play()
}
function failed() {
  if(!attached.value||disposed)return
  error.value='浏览器无法播放此文件或文件版本已变化，请重新读取或下载查看'
  release('读取失败，已释放连接')
}
function visibilityChanged() {hidden.value=document.hidden;if(hidden.value)release('页面已转入后台，已停止读取')}
watch(()=>props.available,available=>{if(!available)release('企业工作区当前不可用，已停止读取')})
watch(()=>props.active,active=>{if(!active)release('已切换文件，已停止读取')})
watch(()=>props.url,()=>{release('');position.value=0;error.value='';attached.value=props.available&&props.active&&!hidden.value;if(!attached.value)reason.value=props.available?'文件未激活或页面处于后台，尚未读取':'企业工作区当前不可用'})
onMounted(()=>document.addEventListener('visibilitychange',visibilityChanged))
onBeforeUnmount(()=>{disposed=true;document.removeEventListener('visibilitychange',visibilityChanged);release('')})
</script>

<template>
  <div class="media-preview">
    <div class="ew-tools"><button v-if="attached" @click="release('已停止读取')">停止读取</button><button v-else :disabled="!available||hidden" @click="resume">继续播放</button><span>暂停可能继续缓冲；停止读取会释放连接。</span></div>
    <p v-if="!attached" class="ew-notice" role="status">{{reason}} · 保留位置 {{time(position)}}</p>
    <p v-if="error" class="ew-error" role="alert">{{error}}</p>
    <div class="media-stage"><component :is="kind==='video'?'video':'audio'" v-show="attached" ref="media" :src="attached?url:undefined" controls preload="metadata" @loadedmetadata="play" @error="failed" /></div>
  </div>
</template>

<style scoped>
.media-preview{display:flex;flex-direction:column;flex:1;min-height:0}.ew-tools span{color:var(--tuck-muted);font-size:12px}.media-stage{display:flex;align-items:center;justify-content:center;flex:1;background:#171a20;min-height:0}.media-stage video,.media-stage audio{max-width:100%;max-height:100%}
</style>
