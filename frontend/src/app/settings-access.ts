import {reactive,readonly} from 'vue'
import {authFetch} from './auth'
export interface SettingsAccess {enabled:boolean;unlocked:boolean;categories:string[];lockedCategories:string[];expiresAt?:number}
const state=reactive({ready:false,error:'',generation:0,enabled:false,unlocked:false,categories:[] as string[],lockedCategories:[] as string[],expiresAt:undefined as number|undefined})
export const settingsAccess=readonly(state)
let request=0, timer:ReturnType<typeof setTimeout>|undefined, channel:BroadcastChannel|undefined
function fingerprint(){return JSON.stringify([state.enabled,state.unlocked,state.categories,state.lockedCategories])}
function suspend(){request++;state.ready=false;state.generation++;clearTimeout(timer);window.dispatchEvent(new Event('tuck-settings-access-changed'))}
function accept(value:SettingsAccess){
 const previous=fingerprint(),wasReady=state.ready;Object.assign(state,value,{ready:true,error:'',expiresAt:value.expiresAt})
 if(!wasReady||previous!==fingerprint()){state.generation++;window.dispatchEvent(new Event('tuck-settings-access-changed'))}
 clearTimeout(timer)
 if(value.unlocked&&value.expiresAt)timer=setTimeout(()=>{suspend();void refreshSettingsAccess()},Math.max(0,value.expiresAt-Date.now()))
}
export function settingsLocked(section:string){return section!=='appearance'&&(!state.ready||(section==='protection'?state.enabled&&!state.unlocked:state.lockedCategories.includes(section)))}
export async function refreshSettingsAccess(){
 const id=++request
 try{const r=await authFetch('/api/settings/access');if(!r.ok)throw new Error('配置权限读取失败');const value=await r.json();if(id===request)accept(value)}
 catch(e){if(id===request){state.error=(e as Error).message;if(state.ready)suspend();state.ready=false}}
}
export async function changeSettingsAccess(action:'unlock'|'lock'|'protection',body?:unknown){
 if(action==='lock')suspend()
 const id=++request
 try{
  const r=await authFetch('/api/settings/'+action,{method:action==='protection'?'PUT':'POST',headers:{'Content-Type':'application/json'},body:body===undefined?undefined:JSON.stringify(body)})
  const value=await r.json();if(!r.ok)throw new Error(value.error||'配置权限更新失败')
  if(id===request)accept(value);else await refreshSettingsAccess()
  channel?.postMessage('changed')
 }catch(e){await refreshSettingsAccess();throw e}
}
export function startSettingsAccess(){
 const refresh=()=>{void refreshSettingsAccess()}, denied=()=>{suspend();refresh()}
 if(typeof BroadcastChannel!=='undefined'){channel=new BroadcastChannel('tuck-settings-access');channel.onmessage=denied}
 window.addEventListener('focus',refresh);window.addEventListener('tuck-settings-access-denied',denied)
 refresh()
 return()=>{request++;clearTimeout(timer);channel?.close();channel=undefined;window.removeEventListener('focus',refresh);window.removeEventListener('tuck-settings-access-denied',denied);state.ready=false;state.generation++}
}
