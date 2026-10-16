<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import type { AttachmentItem } from '../../engine/model/conversation'
import { copyText } from '../copy-text'
import WorkbenchDialog from './WorkbenchDialog.vue'

const props = defineProps<{ item: AttachmentItem; src: string; original?: boolean; prompt?: string; promptLoading?: boolean; promptError?: string }>()
const emit = defineEmits<{ close: [] }>()
const decodedSize = ref<{ width:number; height:number } | null>(null)
const dimensions = computed(() => decodedSize.value ?? (props.item.width && props.item.height ? {width:props.item.width,height:props.item.height} : null))
const size = computed(() => props.item.sizeBytes == null ? props.item.mimeType : props.item.sizeBytes < 1024 ? `${props.item.sizeBytes} B` : `${(props.item.sizeBytes/1024).toFixed(1)} KB`)
const closeButton = ref<HTMLButtonElement | null>(null), copied = ref(false), copyError = ref('')
const previousFocus = document.activeElement as HTMLElement | null
onMounted(() => closeButton.value?.focus())
onBeforeUnmount(() => { if (previousFocus?.isConnected) previousFocus.focus() })
function loaded(event:Event) { const image=event.target as HTMLImageElement; if(image.naturalWidth&&image.naturalHeight) decodedSize.value={width:image.naturalWidth,height:image.naturalHeight} }
async function copy() {
  copyError.value=''
  try { await copyText(props.prompt ?? ''); copied.value=true }
  catch { copyError.value='复制失败，请选择提示词后手动复制。' }
}
</script>
<template>
  <WorkbenchDialog :label="`图片预览 ${item.name}`" wide preview @close="emit('close')">
    <section class="media-preview-dialog">
      <header class="preview-heading"><strong :title="item.name">{{ item.name }}</strong><button ref="closeButton" aria-label="关闭图片预览" @click="emit('close')">×</button></header>
      <div class="image-preview-content">
        <img class="preview-body" :src="src" :alt="item.name" :style="prompt ? {maxHeight:'55dvh'} : undefined" @load="loaded" />
        <section v-if="prompt || promptLoading || promptError" class="generation-prompt" aria-label="生图提示词">
          <div><strong>生图提示词</strong><button v-if="prompt" @click="copy">{{ copied ? '已复制' : '复制提示词' }}</button></div>
          <p v-if="promptLoading" role="status">正在读取提示词…</p>
          <p v-else-if="promptError" role="alert">{{ promptError }}</p>
          <textarea v-if="prompt" :value="prompt" readonly aria-label="生图提示词内容" rows="4" />
          <p v-if="copyError" role="alert">{{ copyError }}</p>
        </section>
      </div>
      <footer class="preview-footer"><span><template v-if="dimensions">{{ dimensions.width }} × {{ dimensions.height }}<template v-if="size"> · </template></template>{{ size }}</span><a :href="src" target="_blank" rel="noopener noreferrer">{{ original ? '打开原图' : '打开图片' }} ↗</a></footer>
    </section>
  </WorkbenchDialog>
</template>
<style scoped>
.media-preview-dialog{display:flex;flex-direction:column;min-width:0;max-height:calc(100dvh - 34px);color:var(--tuck-text);background:var(--tuck-surface)}
.preview-heading,.preview-footer{flex:none}
.image-preview-content{min-height:0;overflow:auto;overscroll-behavior:contain}
.generation-prompt{padding:10px;border-top:1px solid var(--tuck-border)}
.generation-prompt>div{display:flex;align-items:center;justify-content:space-between;gap:8px;margin-bottom:6px}
.generation-prompt textarea{display:block;resize:none;max-height:24dvh;font:inherit;white-space:pre-wrap;overflow-wrap:anywhere}
.generation-prompt p{margin:4px 0;overflow-wrap:anywhere}
</style>
