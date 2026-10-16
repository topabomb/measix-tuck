<script setup lang="ts">
import {ref,onMounted,onBeforeUnmount} from 'vue'
import {authFetch} from '../../../app/auth'
type Engine={managed:boolean;currentVersion:string|null;state:string;busy:string|null;latest:{version:string;publishedAt:string;releaseUrl:string}|null;downloadedVersion:string|null}
const emit=defineEmits<{changed:[]}>()
const engine=ref<Engine>(),busy=ref(''),error=ref(''),message=ref(''),confirm=ref(false)
let active=true
let timer:ReturnType<typeof setTimeout>|undefined
onBeforeUnmount(()=>{active=false;clearTimeout(timer)})
async function request(action='',body?:object){
 const r=await authFetch('/api/settings/engine'+(action?'/'+action:''),action?{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body||{})}:undefined)
 const data=await r.json()
 if(!r.ok)throw Object.assign(new Error(data.error||'引擎操作失败'),{code:data.code})
 if(active){engine.value=data;clearTimeout(timer);if(data.busy)timer=setTimeout(load,1500)}
 return data as Engine
}
async function load(){try{await request()}catch(e){if(active)error.value=(e as Error).message}}
async function run(action:'check'|'download'|'apply',interrupt=false){
 busy.value=action;error.value='';message.value='';confirm.value=false
 try {
  const version=action==='apply'?engine.value?.downloadedVersion:engine.value?.latest?.version
  await request(action,{version,interrupt})
  if(!active)return
  if(action==='download'){await run('apply');return}
  if(action==='apply'){message.value='更新已应用。';emit('changed')}
  if(action==='check'&&engine.value?.latest?.version===engine.value?.currentVersion)message.value='当前已是最新稳定版。'
 }catch(e){if(active){if((e as {code?:string}).code==='engine_interrupt_required')confirm.value=true;else error.value=(e as Error).message}}
 finally{if(active){busy.value='';await load()}}
}
onMounted(load)
</script>
<template>
 <section class="engine-settings" aria-label="Docker Agent 引擎">
  <div class="engine-summary"><div><h3>Docker Agent</h3><p>官方发布程序</p></div><strong>{{engine?.currentVersion||'版本未知'}}</strong></div>
  <p v-if="engine" class="hint">{{engine.state==='ready'?'运行中':engine.state==='starting'?'正在启动':'服务未就绪'}}</p>
  <p v-if="engine&&!engine.managed" class="hint">此引擎由外部管理，请在宿主更新。</p>
  <div v-if="engine?.latest" class="engine-release"><span>最新稳定版 <strong>{{engine.latest.version}}</strong></span><span>{{new Date(engine.latest.publishedAt).toLocaleDateString()}}</span><a :href="engine.latest.releaseUrl" target="_blank" rel="noopener noreferrer">发布说明 ↗</a></div>
  <p v-if="engine?.downloadedVersion" class="hint">{{engine.downloadedVersion}} 已下载，尚未应用。</p>
  <div class="engine-actions">
   <button v-if="!engine||engine.busy&&!busy" @click="load">刷新状态</button>
   <button :disabled="!!busy||!!engine?.busy||!engine" @click="run('check')">{{busy==='check'?'正在检查…':'检查更新'}}</button>
   <button v-if="engine?.managed&&engine.downloadedVersion" :disabled="!!busy||!!engine.busy" @click="run('apply')">{{busy==='apply'?'正在应用…':'应用已下载版本'}}</button>
   <button v-if="engine?.managed&&engine.latest&&engine.latest.version!==engine.currentVersion&&engine.latest.version!==engine.downloadedVersion" :disabled="!!busy||!!engine.busy" @click="run('download')">{{busy==='download'?'正在下载…':busy==='apply'?'正在应用…':`下载并应用 ${engine.latest.version}`}}</button>
  </div>
  <p v-if="engine?.managed" class="hint">下载期间可继续使用。应用时会重启引擎；如有任务正在执行，会先征求确认。</p>
  <div v-if="confirm" class="engine-confirm" role="alert"><p>有任务正在执行或等待交互。是否中断并应用更新？中断的任务不会自动恢复。</p><div class="engine-actions"><button :disabled="!!busy" @click="run('apply',true)">中断并应用</button><button :disabled="!!busy" @click="confirm=false;message='已保留下载，可稍后手动应用。'">稍后应用</button></div></div>
  <p v-if="message" role="status">{{message}}</p><p v-if="error" role="alert">{{error}}</p>
 </section>
</template>
<style scoped>
.engine-settings{display:grid;gap:14px}.engine-summary{display:flex;align-items:center;justify-content:space-between;gap:16px}.engine-summary p{margin:4px 0 0;color:var(--tuck-muted)}.engine-settings .hint,.engine-settings p{margin:0}.engine-release,.engine-actions{display:flex;gap:10px 16px;align-items:center;flex-wrap:wrap}.engine-release{padding:12px 0;border-block:1px solid var(--tuck-border)}.engine-release a{color:var(--tuck-accent)}.engine-confirm{padding:12px;border:1px solid var(--tuck-border);border-radius:8px;background:var(--tuck-surface)}.engine-confirm p{margin-bottom:10px}.engine-settings [role=alert]{overflow-wrap:anywhere}
</style>
