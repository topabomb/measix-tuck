<script setup lang="ts">
import {nextTick,ref} from 'vue'
import {applyTheme,builtInThemes,customThemes,selectedTheme} from './themes'
defineProps<{settingsPath:string}>()
const emit=defineEmits<{navigate:[]}>()
const root=ref<HTMLElement|null>(null),themes=ref(false)
async function showThemes(value:boolean) {
  themes.value = value
  await nextTick()
  root.value?.querySelector<HTMLElement>('button,a')?.focus()
}
</script>
<template>
 <nav ref="root" class="settings-menu-content" aria-label="设置快捷菜单">
  <template v-if="!themes">
   <RouterLink :to="settingsPath" @click="emit('navigate')"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 6h16M4 12h16M4 18h16M8 3v6m8 0v6m-7 0v6" /></svg><span>设置</span><span aria-hidden="true">›</span></RouterLink>
   <button type="button" aria-label="主题" @click="showThemes(true)"><svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="8"/><path d="M12 4a8 8 0 0 1 0 16Z" /></svg><span>主题</span><span aria-hidden="true">›</span></button>
  </template>
  <template v-else>
   <button type="button" class="back-link theme-back" aria-label="返回设置菜单" @click="showThemes(false)">← 主题 <small>立即生效</small></button>
   <div class="theme-list"><button v-for="theme in [...builtInThemes,...customThemes]" :key="theme.id" type="button" :aria-pressed="selectedTheme===theme.id" @click="applyTheme(theme.id)"><i aria-hidden="true" :style="{background:theme.colors.background,borderColor:theme.colors.accent}" /><span>{{theme.name}}</span><span v-if="selectedTheme===theme.id" aria-hidden="true">✓</span></button></div>
   <RouterLink class="theme-more" to="/settings/appearance" @click="emit('navigate')">更多外观设置</RouterLink>
  </template>
 </nav>
</template>
<style scoped>
.settings-menu-content { min-width:0; font:var(--tuck-font-ui)/var(--tuck-line-ui) var(--tuck-font-family); }
.settings-menu-content a, .settings-menu-content button {
  display:flex;
  align-items:center;
  gap:8px;
  width:100%;
  min-height:36px;
  box-sizing:border-box;
  padding:7px 8px;
  border:0;
  border-radius:5px;
  background:transparent;
  color:var(--tuck-text);
  font:inherit;
  font-size:var(--tuck-font-caption);
  text-decoration:none;
  text-align:left;
  cursor:pointer;
}
.settings-menu-content a>span:first-of-type, .settings-menu-content button>span:first-of-type { flex:1; min-width:0; overflow-wrap:anywhere; }
.settings-menu-content svg { width:18px; height:18px; flex:none; fill:none; stroke:currentColor; stroke-width:1.6; stroke-linecap:round; stroke-linejoin:round; }
.settings-menu-content a:hover, .settings-menu-content button:hover, .theme-list button[aria-pressed=true] { background:var(--tuck-hover); }
.settings-menu-content .theme-back { margin-bottom:4px; color:var(--tuck-accent); }
.theme-back small { margin-left:auto; color:var(--tuck-muted); font-size:11px; }
.theme-list i { width:15px; height:15px; border:2px solid; border-radius:50%; flex:none; }
.settings-menu-content .theme-more { border-top:1px solid var(--tuck-border); border-radius:0; margin-top:4px; color:var(--tuck-muted); }
@media(max-width:760px) {
  .settings-menu-content a, .settings-menu-content button { min-height:40px; font-size:var(--tuck-font-ui); }
}
</style>
