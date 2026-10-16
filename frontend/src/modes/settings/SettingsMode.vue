<script setup lang="ts">
import {computed} from 'vue'
import {useRoute} from 'vue-router'
import type {ConnectionInfo} from '../../shared/workspace/runtime'
import RuntimeConfiguration from './RuntimeConfiguration.vue'
import PlatformConnection from './components/PlatformConnection.vue'
import AppearanceSettings from './components/AppearanceSettings.vue'
import OrchestrationSettings from './components/OrchestrationSettings.vue'
import EngineSettings from './components/EngineSettings.vue'
import ProtectionSettings from './components/ProtectionSettings.vue'
import SettingsUnlock from './components/SettingsUnlock.vue'
import {settingsSections} from '../../../shared/settings-sections.mjs'
import {settingsAccess,settingsLocked} from '../../app/settings-access'
import './settings.css'
import {settingsReturnPath,router,modes} from '../../app/router'
import {platformProviderPath} from './components/catalog-paths'
defineProps<{connection?:ConnectionInfo;username?:string;canLogout:boolean}>()
const emit=defineEmits<{changed:[];logout:[]}>(),route=useRoute()
const sections=[...settingsSections,{key:'protection',label:'配置保护'}].map(s=>({id:s.key,label:s.label,path:'/settings/'+s.key}))
const current=computed(()=>sections.find(s=>s.id===route.meta.settingsSection))
const home=computed(()=>route.path==='/settings')
const parent=computed(()=>route.params.managedModel?platformProviderPath(String(route.params.managedProvider)):route.params.managedProvider?'/settings/providers':route.params.managedMcp?'/settings/mcp':route.params.tool?'/settings/agents/'+encodeURIComponent(String(route.params.agent)):route.params.agent?'/settings/agents':route.params.model?'/settings/providers/'+encodeURIComponent(String(route.params.provider)):route.params.provider?'/settings/providers':route.params.mcp?'/settings/mcp':home.value?settingsReturnPath():'/settings')
const backLabel=computed(()=>home.value?'返回'+(modes.find(m=>m.id===router.resolve(parent.value).meta.mode)?.label||'对话'):route.params.tool?'返回 Agent 配置':route.params.agent?'返回 Agent 列表':route.params.model||route.params.managedModel?'返回提供商':route.params.provider||route.params.managedProvider?'返回提供商列表':route.params.mcp||route.params.managedMcp?'返回 MCP 列表':'返回设置')
</script>
<template>
 <main class="settings-mode" :class="{'settings-home':home}">
  <aside class="settings-sidebar"><h1>设置</h1><nav aria-label="设置分类"><RouterLink v-for="s in sections" :key="s.id" :to="s.path" :aria-current="current?.id===s.id?'page':undefined">{{ s.label }}</RouterLink></nav></aside>
  <div class="settings-page"><header class="settings-heading"><RouterLink class="back-link settings-back" :to="parent" :aria-label="backLabel">← {{backLabel}}</RouterLink><h2>{{ home?'设置':current?.label || '设置页面不存在' }}</h2><p v-if="current?.id==='agents'">配置 Agent 的模型、指令、工具与协作关系。</p><p v-else-if="current?.id==='mcp'">查看平台 MCP，管理自定义连接；在 Agent 中选择启用。</p><p v-else-if="current?.id==='providers'">查看平台下发的模型，管理自定义提供商与模型。</p></header>
   <div v-if="home" class="settings-standalone"><nav class="settings-home-list" aria-label="设置首页"><RouterLink v-for="s in sections" :key="s.id" :to="s.path"><strong>{{s.label}}</strong><span aria-hidden="true">›</span></RouterLink></nav><div v-if="canLogout" class="settings-account"><small>{{ username }}</small><button @click="emit('logout')">退出登录</button></div></div>
   <div v-else-if="current&&settingsLocked(current.id)" class="settings-standalone"><SettingsUnlock /></div>
   <RuntimeConfiguration v-else-if="['providers','agents','mcp'].includes(String(current?.id))" :key="settingsAccess.generation" :connection="connection" @changed="emit('changed')" />
   <div v-else class="settings-standalone" :key="current?.id==='appearance'?'appearance':settingsAccess.generation"><PlatformConnection v-if="current?.id==='platform'" :connection="connection" @changed="emit('changed')" /><OrchestrationSettings v-else-if="current?.id==='orchestration'" /><EngineSettings v-else-if="current?.id==='engine'" @changed="emit('changed')" /><AppearanceSettings v-else-if="current?.id==='appearance'" /><ProtectionSettings v-else-if="current?.id==='protection'" /><p v-else>请从设置分类中选择页面。</p></div>
  </div>
 </main>
</template>
