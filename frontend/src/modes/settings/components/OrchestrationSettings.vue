<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount } from 'vue'
import { authFetch } from '../../../app/auth'
import RetentionSettings from './RetentionSettings.vue'
const controller=new AbortController()
onBeforeUnmount(()=>controller.abort())
const busy=ref(false),loaded=ref(false),error=ref(''),enabled=ref(false),available=ref(false),assistants=ref<string[]>([])
async function load(){busy.value=true;loaded.value=false;error.value='';try{const r=await authFetch('/api/settings/orchestration',{signal:controller.signal});if(!r.ok)throw new Error('编排设置读取失败');const d=await r.json();enabled.value=d.requireDelivery;available.value=d.available;assistants.value=d.assistants;loaded.value=true}catch(e){error.value=(e as Error).message}finally{busy.value=false}}
async function change(event:Event){const value=(event.target as HTMLInputElement).checked;busy.value=true;error.value='';try{const r=await authFetch('/api/settings/orchestration',{method:'PUT',signal:controller.signal,headers:{'Content-Type':'application/json'},body:JSON.stringify({requireDelivery:value})});if(!r.ok)throw new Error('设置保存失败');enabled.value=(await r.json()).requireDelivery}catch(e){error.value=(e as Error).message;(event.target as HTMLInputElement).checked=enabled.value}finally{busy.value=false}}
onMounted(load)
</script>
<template>
    <section class="orchestration-settings">
      <label class="require-delivery"><input type="checkbox" :checked="enabled" :disabled="busy||!loaded" aria-describedby="delivery-policy-description" @change="change" /><span>启用交付要求</span></label>
      <div id="delivery-policy-description"><p>对已启用交付工具的主助手，每次普通输入正常完成但未交付时，自动补充一次交付要求。补充后仍未交付就结束，不会反复续轮。你可以随时停止。</p><p>在主助手一轮正常结束后检查此开关；关闭后不再自动补交付。子代理不适用。</p></div>
      <p v-if="loaded&&available" class="delivery-assistants">适用助手：{{ assistants.join('、') }}</p><p v-else-if="loaded">当前没有可使用交付工具的主助手，此设置暂不触发自动续轮。</p>
      <p v-if="error" role="alert">{{ error }}</p>
      <RetentionSettings />
    </section>
</template>
<style scoped>
.orchestration-trigger{display:flex;align-items:center;gap:8px;width:100%;margin-top:6px;text-align:left}
.orchestration-trigger>svg{width:18px;height:18px;flex:none;fill:none;stroke:currentColor;stroke-width:1.5;color:var(--tuck-accent)}
.orchestration-settings{display:grid;gap:12px;font:var(--tuck-font-ui)/var(--tuck-line-ui) var(--tuck-font-family)}
.orchestration-settings header{display:flex;justify-content:space-between;align-items:center;gap:12px}.orchestration-settings header h2{margin:0}
.orchestration-settings header button{width:28px;height:28px;padding:0;flex:none}
.orchestration-settings label.require-delivery{display:flex;gap:8px;align-items:center;justify-content:flex-start;margin:0;font-weight:600}
.orchestration-settings .require-delivery input{width:16px;height:16px;min-width:16px;flex:0 0 16px;margin:0;padding:0;accent-color:var(--tuck-accent)}
.orchestration-settings p{margin:0;color:var(--tuck-muted)}.orchestration-settings p+p{margin-top:6px}.delivery-assistants{font-size:var(--tuck-font-caption)}
</style>
