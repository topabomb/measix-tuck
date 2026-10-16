<script setup lang="ts">
import { onMounted, onBeforeUnmount, ref } from 'vue'
const props = withDefaults(defineProps<{ label: string; busy?: boolean; wide?: boolean; contained?: boolean; preview?: boolean; dismissOnBackdrop?: boolean }>(), { dismissOnBackdrop: true })
const emit = defineEmits<{ close: [] }>()
const dialog = ref<HTMLDialogElement | null>(null)
let startedOutside = false
function pointerDown(event: PointerEvent) { startedOutside = outside(event) }
function outside(event: PointerEvent | MouseEvent): boolean {
  const box = dialog.value?.getBoundingClientRect()
  return !!box && event.target === dialog.value && (event.clientX < box.left || event.clientX > box.right || event.clientY < box.top || event.clientY > box.bottom)
}
function backdropClick(event: MouseEvent) {
  if (startedOutside && outside(event) && props.dismissOnBackdrop && !props.busy) emit('close')
  startedOutside = false
}
function containTab(event: KeyboardEvent) {
  if (event.key !== 'Tab') return
  const targets = [...(dialog.value?.querySelectorAll<HTMLElement>('button,input,select,textarea,a[href],summary,[tabindex]') || [])].filter(el => !el.matches(':disabled,[tabindex="-1"]') && el.getClientRects().length)
  const first = targets[0], last = targets.at(-1)
  if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus() }
  else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus() }
}
onMounted(() => dialog.value?.showModal())
onBeforeUnmount(() => dialog.value?.close())
</script>
<template>
  <Teleport to="body">
    <dialog ref="dialog" class="tuck-modal" :class="{ wide, contained, preview }" :aria-label="label" :aria-busy="!!busy" aria-modal="true" @cancel.prevent="!busy && emit('close')" @keydown="containTab" @pointerdown="pointerDown" @click="backdropClick">
      <slot />
    </dialog>
  </Teleport>
</template>
<style scoped>
.tuck-modal { box-sizing:border-box; width:min(560px,calc(100vw - 32px)); max-height:calc(100dvh - 32px); overflow:auto; overscroll-behavior:contain; padding:var(--tuck-dialog-padding); border:1px solid var(--tuck-border); border-radius:var(--tuck-dialog-radius); background:var(--tuck-surface); color:var(--tuck-text); color-scheme:var(--tuck-scheme); font:var(--tuck-font-ui)/var(--tuck-line-ui) var(--tuck-font-family); }
.tuck-modal::backdrop { background:#0008; } .tuck-modal.wide { width:min(1000px,calc(100vw - 32px)); }
.tuck-modal.contained[open] { display:flex; flex-direction:column; padding:0; overflow:hidden; }
.tuck-modal :deep(*) { box-sizing:border-box; }
.tuck-modal :deep(h2) { font-size:var(--tuck-font-title); line-height:var(--tuck-line-title); font-weight:600; margin:0 0 var(--tuck-dialog-gap); }
.tuck-modal :deep(label) { display:grid; gap:6px; margin-bottom:var(--tuck-dialog-gap); }
.tuck-modal :deep(input),.tuck-modal :deep(select),.tuck-modal :deep(textarea) { min-width:0; max-width:100%; padding:5px 8px; min-height:var(--tuck-dialog-control-height); border:1px solid var(--tuck-border); border-radius:6px; color:var(--tuck-text); background:var(--tuck-background); font:inherit; }
.tuck-modal :deep(input),.tuck-modal :deep(textarea) { width:100%; }
.tuck-modal :deep(button) { min-height:var(--tuck-dialog-control-height); padding:5px 10px; border:1px solid var(--tuck-border); border-radius:6px; background:var(--tuck-raised); color:var(--tuck-text); font:inherit; cursor:pointer; }
.tuck-modal :deep(button:not([type=submit]):hover:not(:disabled)) { background:var(--tuck-hover); }
.tuck-modal :deep(button:disabled) { opacity:.45; cursor:default; }
.tuck-modal :deep(button[type=submit]) { background:var(--tuck-accent); color:var(--tuck-accentText); border-color:transparent; }
.tuck-modal :deep(button[type=submit]:hover:not(:disabled)) { background:var(--tuck-accent); color:var(--tuck-accentText); filter:brightness(.94); }
.tuck-modal :deep(small) { color:var(--tuck-muted); overflow-wrap:anywhere; }
.tuck-modal :deep([role=alert]) { color:var(--tuck-danger); overflow-wrap:anywhere; white-space:pre-wrap; }
.tuck-modal :deep(.dialog-actions) { display:flex; justify-content:flex-end; gap:8px; margin-top:var(--tuck-dialog-gap); }
/* Preview chrome shares one compact header/footer; only the content scrolls. */
.tuck-modal.preview[open] { padding:0; overflow:hidden; }
.tuck-modal.preview :deep(.preview-heading) { display:flex; align-items:center; justify-content:space-between; gap:8px; min-height:var(--tuck-dialog-header-height); padding:4px 10px; margin:0; border-bottom:1px solid var(--tuck-border); }
.tuck-modal.preview :deep(.preview-heading > :first-child) { min-width:0; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; margin:0; font-size:var(--tuck-font-ui); font-weight:600; }
.tuck-modal.preview :deep(.preview-heading > button) { flex:none; width:30px; height:30px; min-height:30px; padding:0; border:0; background:transparent; font-size:20px; line-height:1; }
.tuck-modal.preview :deep(.preview-body) { display:block; margin:0 auto; max-width:100%; max-height:calc(100dvh - 140px); object-fit:contain; }
.tuck-modal.preview :deep(.preview-footer) { display:flex; align-items:center; justify-content:space-between; flex-wrap:wrap; gap:4px 12px; min-height:32px; padding:4px 10px; border-top:1px solid var(--tuck-border); font-size:var(--tuck-font-caption); }
.tuck-modal.preview :deep(.preview-footer > span) { min-width:0; overflow-wrap:anywhere; color:var(--tuck-muted); }
.tuck-modal.preview :deep(.preview-footer a) { flex:none; color:var(--tuck-accent); }
.tuck-modal.preview :deep(.preview-footer button) { min-height:30px; padding:4px 8px; }
</style>
