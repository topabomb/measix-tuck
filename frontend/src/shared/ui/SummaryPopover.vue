<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref, watch } from 'vue'
const props = defineProps<{ open:boolean; label:string; trigger?:HTMLElement|null; above?:boolean; closeLabel?:string }>()
const emit = defineEmits<{ 'update:open':[value:boolean] }>()
const panel = ref<HTMLElement|null>(null)
function position() {
  if (!panel.value || !props.open) return
  const viewport=window.visualViewport, width=document.documentElement.clientWidth
  const top=viewport?.offsetTop||0, height=viewport?.height||innerHeight
  const box=props.trigger?.getBoundingClientRect()
  const mobile=width<=760,bounds=props.trigger?.closest('[data-summary-boundary]')?.getBoundingClientRect()
  const left=mobile?12:Math.max(12,(bounds?.left||0)+12),right=mobile?width-12:Math.min(width-12,bounds?.right?bounds.right-12:width-12)
  const panelWidth=Math.min(360,right-left)
  Object.assign(panel.value.style,{
    width:`${mobile?width-24:panelWidth}px`,left:`${mobile?12:Math.max(left,Math.min((box?.right||right)-panelWidth,right-panelWidth))}px`,
    top:mobile||props.above?'auto':`${Math.max(top+12,box?.bottom?box.bottom+8:top+12)}px`,
    bottom:mobile?`${Math.max(0,innerHeight-top-height)+12}px`:props.above?`${Math.max(12,innerHeight-(box?.top||innerHeight-12)+8)}px`:'auto',
    maxHeight:`${mobile?Math.max(100,height*.75):Math.max(100,Math.min(520,props.above?(box?.top||height)-top-24:top+height-(box?.bottom||top)-24))}px`,
  })
}
function close(focus=false) { panel.value?.hidePopover();emit('update:open',false);if(focus)props.trigger?.focus() }
watch([()=>props.open,()=>props.label,()=>props.trigger,()=>props.above],async(_value,_previous,onCleanup)=>{
  let cancelled=false
  let observer:ResizeObserver|undefined
  const viewport=window.visualViewport
  onCleanup(()=>{
    cancelled=true
    observer?.disconnect()
    window.removeEventListener('resize',position)
    viewport?.removeEventListener('resize',position)
    viewport?.removeEventListener('scroll',position)
  })
  await nextTick()
  if(cancelled||!panel.value)return
  if(!props.open){panel.value.hidePopover();return}
  position()
  panel.value.showPopover()
  panel.value.focus({preventScroll:true})
  // Internal panel resizing does not dispatch window.resize.
  observer=new ResizeObserver(position)
  if(props.trigger)observer.observe(props.trigger)
  const boundary=props.trigger?.closest('[data-summary-boundary]')
  if(boundary)observer.observe(boundary)
  window.addEventListener('resize',position)
  viewport?.addEventListener('resize',position)
  viewport?.addEventListener('scroll',position)
},{flush:'post',immediate:true})
function toggled(event:Event){if((event as ToggleEvent).newState==='closed'&&!panel.value?.matches(':popover-open'))emit('update:open',false)}
onBeforeUnmount(()=>panel.value?.hidePopover())
</script>
<template>
  <section ref="panel" popover="auto" class="summary-popover" role="dialog" :aria-label="label" tabindex="-1" @toggle="toggled" @keydown.esc.prevent.stop="close(true)">
    <header><strong>{{label}}</strong><button type="button" :aria-label="closeLabel||`关闭${label}`" @click="close(true)">×</button></header>
    <div class="summary-body"><slot /></div>
  </section>
</template>
<style scoped src="./summary-popover.css"></style>
