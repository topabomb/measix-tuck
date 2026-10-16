<script setup lang="ts">
import {ref} from 'vue'
import {settingsAccess,changeSettingsAccess,refreshSettingsAccess} from '../../../app/settings-access'
const password=ref(''),busy=ref(false),error=ref('')
async function unlock(){busy.value=true;error.value='';try{await changeSettingsAccess('unlock',{password:password.value})}catch(e){error.value=(e as Error).message}finally{password.value='';busy.value=false}}
</script>
<template><section class="settings-unlock">
 <template v-if="!settingsAccess.ready"><p>{{settingsAccess.error||'正在确认配置权限…'}}</p><button v-if="settingsAccess.error" @click="refreshSettingsAccess">重试</button></template>
 <form v-else @submit.prevent="unlock"><h3>此配置已锁定</h3><p class="hint">输入配置保护密码后可查看和修改。外观设置始终可用。</p><label>配置保护密码<input v-model="password" type="password" autocomplete="current-password" required :disabled="busy" /></label><p v-if="error" role="alert">{{error}}</p><button class="primary" :disabled="busy||!password">{{busy?'正在解锁…':'解锁配置'}}</button></form>
</section></template>
<style scoped>.settings-unlock{max-width:440px}.settings-unlock h3{margin-bottom:10px}</style>
