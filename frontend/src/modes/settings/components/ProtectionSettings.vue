<script setup lang="ts">
import {onMounted,onBeforeUnmount,ref} from 'vue'
import {authFetch} from '../../../app/auth'
import {changeSettingsAccess} from '../../../app/settings-access'
import {settingsSections} from '../../../../shared/settings-sections.mjs'
const enabled=ref(false),categories=ref<string[]>([]),revision=ref(''),password=ref(''),confirmPassword=ref(''),busy=ref(true),loaded=ref(false),error=ref('')
const controller=new AbortController()
async function load(){busy.value=true;error.value='';try{const r=await authFetch('/api/settings/protection',{signal:controller.signal});const d=await r.json();if(!r.ok)throw new Error(d.error||'保护设置读取失败');enabled.value=d.enabled;categories.value=d.categories;revision.value=d.revision;loaded.value=true}catch(e){if(!controller.signal.aborted)error.value=(e as Error).message}finally{busy.value=false}}
async function save(remove=false){
 if(!remove&&password.value!==confirmPassword.value){error.value='两次输入的密码不一致';return}
 if(!remove&&(!enabled.value||password.value)&&password.value.length<6){error.value='密码至少需要 6 个字符';return}
 busy.value=true;error.value=''
 try{await changeSettingsAccess('protection',{revision:revision.value,categories:categories.value,...(remove?{password:''}:password.value?{password:password.value}:{})})}catch(e){error.value=(e as Error).message}finally{password.value='';confirmPassword.value='';busy.value=false}
}
onMounted(load);onBeforeUnmount(()=>controller.abort())
</script>
<template><section><p class="hint">{{enabled?'已启用配置保护。修改密码或保护范围后会重新锁定。':'尚未启用配置保护。设置密码并选择需要保护的配置。'}} 外观始终可用。</p>
 <form v-if="loaded" @submit.prevent="save()"><fieldset :disabled="busy"><div class="form-grid"><label>{{enabled?'新密码（留空保持原密码）':'保护密码'}}<input v-model="password" type="password" autocomplete="new-password" :required="!enabled" minlength="6" /></label><label>确认新密码<input v-model="confirmPassword" type="password" autocomplete="new-password" :required="!!password" /></label></div><p class="hint">密码至少 6 个字符。解锁在当前浏览器中有效 1 小时，到期后需重新解锁。</p><h3>保护范围</h3><div class="protection-categories"><label v-for="section in settingsSections.filter(s=>s.protectable)" :key="section.key" class="check"><input v-model="categories" type="checkbox" :value="section.key" /><span>{{section.label}}<small>{{section.description}}</small></span></label></div><p class="hint">未选择分类时，保护以上全部配置；外观始终可用。配置保护页面始终需要解锁。</p><div class="line"><button class="primary">保存保护设置</button><button v-if="enabled" type="button" @click="save(true)">移除配置保护</button></div></fieldset></form><p v-else-if="busy">正在读取配置…</p><button v-else @click="load">重试</button><p v-if="error" role="alert">{{error}}</p>
</section></template>
<style scoped>.protection-categories{display:grid;gap:10px;margin:14px 0}.protection-categories small{display:block;margin-top:2px}.protection-categories .check{align-items:start}.protection-categories input{margin-top:3px}</style>
