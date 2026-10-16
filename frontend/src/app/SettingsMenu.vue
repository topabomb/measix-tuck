<script setup lang="ts">
import {nextTick,ref,watch} from 'vue'
import {useRoute} from 'vue-router'
import {useLightDismiss} from '../shared/use-light-dismiss'
import SettingsMenuContent from './SettingsMenuContent.vue'
defineProps<{settingsPath:string}>()
const root=ref<HTMLElement|null>(null),open=ref(false),route=useRoute()
useLightDismiss(()=>root.value,()=>open.value,()=>{open.value=false})
watch(()=>route.fullPath,()=>{open.value=false})
async function toggle() {
  open.value = !open.value
  if (!open.value) return
  await nextTick()
  root.value?.querySelector<HTMLElement>('.settings-popover a')?.focus()
}
</script>
<template>
 <div ref="root" class="settings-menu">
  <button class="settings-trigger" type="button" aria-label="设置菜单" data-testid="mode-settings" :aria-expanded="open" aria-controls="settings-quick-panel" :class="{selected:route.meta.mode==='settings'}" @click="toggle"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 6h16M4 12h16M4 18h16M8 3v6m8 0v6m-7 0v6" /></svg><span>设置</span></button>
  <SettingsMenuContent v-if="open" id="settings-quick-panel" class="settings-popover" :settings-path="settingsPath" @navigate="open=false" />
 </div>
</template>
<style scoped>
.settings-menu{margin-top:auto;position:relative;font:var(--tuck-font-ui)/var(--tuck-line-ui) var(--tuck-font-family)}
.settings-trigger{display:flex;flex-direction:column;align-items:center;justify-content:center;gap:4px;width:100%;min-height:52px;padding:4px 0;border:0;border-radius:8px;background:transparent;color:var(--tuck-muted);font:inherit;font-size:12px;line-height:16px;cursor:pointer}
.settings-trigger svg{width:20px;height:20px;fill:none;stroke:currentColor;stroke-width:1.6;stroke-linecap:round;stroke-linejoin:round}
.settings-trigger:hover,.settings-trigger.selected{background:var(--tuck-raised);color:var(--tuck-accent)}
.settings-popover {
  position:absolute;
  left:calc(100% + 10px);
  bottom:0;
  width:min(224px,calc(100vw - 72px));
  max-height:calc(100dvh - 20px);
  box-sizing:border-box;
  overflow:auto;
  padding:6px;
  border:1px solid var(--tuck-border);
  border-radius:var(--tuck-dialog-radius);
  background:var(--tuck-surface);
  box-shadow:0 8px 28px #0003;
}
</style>
