<script setup lang="ts">
import {computed,ref} from 'vue'
import {settingsAccess,changeSettingsAccess} from './settings-access'
import {router} from './router'
defineProps<{inline?:boolean}>()
const busy=ref(false),error=ref('')
const emit=defineEmits<{navigate:[]}>()
const label=computed(()=>settingsAccess.enabled?(settingsAccess.unlocked?'锁定配置':'解锁配置'):'配置保护')
async function click(){
  error.value=''
  if(!settingsAccess.enabled||!settingsAccess.unlocked){
    await router.push('/settings/protection')
    emit('navigate')
    return
  }
  busy.value=true
  try{await changeSettingsAccess('lock')}
  catch(cause){error.value=(cause as Error).message}
  finally{busy.value=false}
}
</script>
<template><div class="rail-protection" :class="{inline}"><button :aria-label="label" :title="label" :disabled="busy" @click="click"><svg viewBox="0 0 24 24" aria-hidden="true"><rect x="5" y="10" width="14" height="11" rx="2" /><path :d="settingsAccess.enabled&&!settingsAccess.unlocked?'M8 10V6a4 4 0 0 1 8 0v4':'M8 10V6a4 4 0 0 1 8 0'"/><path d="M12 14v3" /></svg><span>{{inline?label:settingsAccess.enabled?(settingsAccess.unlocked?'锁定':'解锁'):'保护'}}</span></button><p v-if="error" role="alert">{{error}}</p></div></template>
<style scoped>.rail-protection{position:relative}.rail-protection button{display:flex;align-items:center;justify-content:center;flex-direction:column;gap:4px;width:100%;min-height:48px;padding:4px 0;border:0;border-radius:8px;background:transparent;color:var(--tuck-muted);font:12px/16px var(--tuck-font-family);cursor:pointer}.rail-protection button:hover{background:var(--tuck-hover);color:var(--tuck-text)}.rail-protection svg{width:20px;height:20px;fill:none;stroke:currentColor;stroke-width:1.6;stroke-linecap:round}.rail-protection p{position:absolute;bottom:0;left:calc(100% + 8px);width:220px;background:var(--tuck-surface);color:var(--tuck-danger);padding:12px;border:1px solid var(--tuck-border);border-radius:8px;font-size:12px}.rail-protection.inline button{flex-direction:row;justify-content:flex-start;gap:8px;min-height:40px;padding:0 8px;font-size:13px}.rail-protection.inline svg{width:18px;height:18px;flex:none}.rail-protection.inline p{position:static;width:auto;margin:0;padding:8px;overflow-wrap:anywhere}</style>
