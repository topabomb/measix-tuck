<script setup lang="ts">
import { reactive, ref, computed, provide, onMounted, onBeforeUnmount, watch } from 'vue'
import {useMobileLayout} from '../shared/use-mobile-layout'
import { useRoute } from 'vue-router'
import { modes, modePaths, router, settingsPathKey, type Mode } from './router'
import SummaryPopover from '../shared/ui/SummaryPopover.vue'
import SettingsMenu from './SettingsMenu.vue'
import SettingsMenuContent from './SettingsMenuContent.vue'
import SettingsLockButton from './SettingsLockButton.vue'
import {startSettingsAccess} from './settings-access'
import TuckMark from '../shared/ui/TuckMark.vue'
import RuncoveMark from '../shared/ui/RuncoveMark.vue'
const route = useRoute()
const moreOpen=ref(false),moreTrigger=ref<HTMLElement|null>(null)
watch(()=>route.fullPath,()=>{moreOpen.value=false})
const mobile=useMobileLayout(),visibleHeight=ref('100dvh'),keyboardOpen=ref(false)
const secondaryModes=modes.filter(mode=>mode.id==='orchestration')
const navigationModes=computed(()=>modes.filter(mode=>mode.id!=='settings'&&(!mobile.value||['chat','tasks','goals','workspace'].includes(mode.id))))
watch(mobile,()=>{moreOpen.value=false})
function viewportChanged(){
  const viewport=window.visualViewport
  visibleHeight.value=mobile.value&&viewport?.scale===1?`${viewport.height}px`:'100dvh'
  keyboardOpen.value=mobile.value&&viewport?.scale===1&&innerHeight-viewport.height>120
}
onMounted(()=>{viewportChanged();window.addEventListener('resize',viewportChanged);window.visualViewport?.addEventListener('resize',viewportChanged)})
onBeforeUnmount(()=>{window.removeEventListener('resize',viewportChanged);window.visualViewport?.removeEventListener('resize',viewportChanged)})
defineProps<{workspaceEnabled:boolean;workspaceReason:string}>()
const last = reactive({ ...modePaths })
provide(settingsPathKey,computed(()=>last.settings))
const stop = router.afterEach((to,_from,failure) => { const mode = to.meta.mode as Mode | undefined; if (!failure && mode) last[mode] = to.fullPath })
onBeforeUnmount(stop)
onBeforeUnmount(startSettingsAccess())
const initial = route.meta.mode as Mode | undefined
if (initial) last[initial] = route.fullPath
</script>
<template>
  <div class="app-shell" :class="{'keyboard-open':keyboardOpen}" :style="{'--app-visible-height':visibleHeight}">
    <nav class="mode-rail" aria-label="工作模式" data-testid="mode-navigation">
      <div class="rail-brand" title="松子 Tuck"><TuckMark /></div>
      <div class="mode-links">
        <template v-for="mode in navigationModes" :key="mode.id">
          <button v-if="mode.id==='workspace'&&!workspaceEnabled" type="button" disabled aria-label="工作区" :title="workspaceReason" data-testid="mode-workspace"><RuncoveMark /><span>工作区</span></button>
          <RouterLink v-else :to="last[mode.id]" :aria-current="route.meta.mode === mode.id ? 'page' : undefined" :class="{ selected: route.meta.mode === mode.id }" :data-testid="`mode-${mode.id}`">
            <RuncoveMark v-if="mode.id==='workspace'" /><svg v-else viewBox="0 0 24 24" aria-hidden="true"><path :d="mode.icon" /></svg><span>{{ mode.label }}</span>
          </RouterLink>
        </template>
        <button v-if="mobile" ref="moreTrigger" class="more-trigger" :class="{selected:['orchestration','settings'].includes(String(route.meta.mode))}" type="button" aria-label="更多频道与设置" :aria-expanded="moreOpen" aria-haspopup="dialog" @click="moreOpen=!moreOpen">
          <svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="5" cy="12" r="1"/><circle cx="12" cy="12" r="1"/><circle cx="19" cy="12" r="1"/></svg><span>更多</span>
        </button>
      </div>
      <div v-if="!mobile" class="rail-settings"><SettingsLockButton /><SettingsMenu :settings-path="last.settings" /></div>
    </nav>
    <SummaryPopover v-if="mobile" v-model:open="moreOpen" :trigger="moreTrigger" label="更多频道与设置" above>
      <nav class="more-group more-channels" aria-label="其他频道">
        <RouterLink v-for="item in secondaryModes" :key="item.id" :to="last[item.id]" :aria-current="route.meta.mode===item.id?'page':undefined" @click="moreOpen=false"><svg viewBox="0 0 24 24" aria-hidden="true"><path :d="item.icon" /></svg><span>{{item.label}}</span></RouterLink>
      </nav>
      <div class="more-group">
        <SettingsLockButton inline @navigate="moreOpen=false" />
      </div>
      <div class="more-group">
        <SettingsMenuContent v-if="moreOpen" :settings-path="last.settings" @navigate="moreOpen=false" />
      </div>
    </SummaryPopover>
    <div class="mode-host"><slot /></div>
  </div>
</template>
<style scoped>
.app-shell { --mode-rail-width:56px; display:grid; grid-template-columns:var(--mode-rail-width) minmax(0,1fr); height:100dvh; overflow:hidden; color:var(--tuck-text); background:var(--tuck-background); }
.mode-rail { display:flex; flex-direction:column; align-items:stretch; gap:4px; padding:0 4px 6px; border-right:1px solid var(--tuck-border); background:var(--tuck-surface); min-height:0; z-index:90; }
.rail-brand { display:grid; place-items:center; height:48px; flex:none; color:var(--tuck-accent); }.rail-brand :deep(svg) { width:28px; height:28px; }
.mode-links { display:flex; flex-direction:column; gap:4px; min-height:0; overflow-y:auto; scrollbar-width:none; }.mode-links::-webkit-scrollbar { display:none; }
.mode-links > a,.mode-links > button { display:flex; flex:none; flex-direction:column; align-items:center; justify-content:center; gap:4px; min-height:52px; padding:4px 0; box-sizing:border-box; border-radius:8px; text-decoration:none; color:var(--tuck-muted); font-size:12px; line-height:16px; cursor:pointer; }
.mode-links > button:disabled{border:0;background:transparent;font-family:inherit;opacity:.5;cursor:default}
.mode-links > a svg,.mode-links > button svg { width:20px; height:20px; }
.mode-links > a svg:not([viewBox="0 0 64 64"]){fill:none;stroke:currentColor;stroke-width:1.6;stroke-linecap:round;stroke-linejoin:round}
.mode-links > a:hover,.more-trigger:hover { background:var(--tuck-hover); }.mode-links > .more-trigger.selected, .mode-links > a.selected { background:var(--tuck-raised); color:var(--tuck-accent); font-weight:600; }

.rail-settings{margin-top:auto;flex:none;display:grid;gap:4px}
.more-trigger{border:0;background:transparent;font-family:inherit}.more-trigger svg{fill:currentColor}
.more-group { display:grid; gap:2px; }
.more-group + .more-group { border-top:1px solid var(--tuck-border); margin-top:4px; padding-top:4px; }
.more-channels a{display:flex;align-items:center;gap:8px;min-height:40px;padding:0 8px;border-radius:6px;text-decoration:none;color:var(--tuck-text)}
.more-channels svg{width:18px;height:18px;flex:none;fill:none;stroke:currentColor;stroke-width:1.6;stroke-linecap:round;stroke-linejoin:round}
.more-channels a:hover{background:var(--tuck-hover)}
.mode-host { position:relative; min-width:0; min-height:0; overflow:hidden; container-type:inline-size; container-name:mode-layout; }
@media(max-width:760px) {
  .app-shell { grid-template-columns:minmax(0,1fr);grid-template-rows:minmax(0,1fr) auto;height:var(--app-visible-height,100dvh); }
  .mode-host { grid-row:1; }
  .mode-rail { grid-row:2;display:block;border-right:0;border-top:1px solid var(--tuck-border);padding:0 4px env(safe-area-inset-bottom); }
  .rail-brand { display:none; }
  .mode-links { flex-direction:row;gap:0;overflow:visible; }
  .mode-links>a,.mode-links>button { flex:1;min-width:0;min-height:54px;gap:2px;padding:5px 0;font-size:11px; }
  .keyboard-open .mode-rail,.app-shell:has(.enterprise-workspace.is-focused) .mode-rail { display:none; }
  .app-shell:has(.enterprise-workspace.is-focused) { grid-template-rows:minmax(0,1fr);padding-bottom:env(safe-area-inset-bottom);box-sizing:border-box; }
}
</style>
