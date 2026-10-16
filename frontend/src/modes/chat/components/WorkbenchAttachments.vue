<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import type { AttachmentItem } from '../../../engine/model/conversation'
import type { RenderUnit } from '../../../engine/presentation/render-unit'
import { useFoldState } from '../../../engine/vue/renderers/fold-state'
import WorkbenchDialog from '../../../shared/ui/WorkbenchDialog.vue'

const props = defineProps<{ unit: RenderUnit }>()
const items = computed(() => (props.unit.payload.items ?? []) as readonly AttachmentItem[])
const expanded = useFoldState(props.unit.id, false)
const visibleItems = computed(() => expanded.value ? items.value : items.value.slice(0, 4))
const preview = ref<AttachmentItem | null>(null)
const decodedSize = ref<{ width: number; height: number } | null>(null)
const previewSize = computed(() => decodedSize.value ?? (preview.value?.width && preview.value?.height ? { width: preview.value.width, height: preview.value.height } : null))
const closePreview = ref<HTMLButtonElement | null>(null)
let previousFocus: HTMLElement | null = null
watch(preview, async value => {
  decodedSize.value = null
  if (value) { previousFocus = document.activeElement as HTMLElement; await nextTick(); closePreview.value?.focus() }
  else previousFocus?.focus()
})
function previewLoaded(event: Event) {
  const image = event.target as HTMLImageElement
  if (image.naturalWidth && image.naturalHeight) decodedSize.value = { width: image.naturalWidth, height: image.naturalHeight }
}
function safeUrl(item: AttachmentItem) {
  const value = item.src || item.resource?.uri || ''
  return /^(https?:\/\/|\/(?!\/)|data:image\/)/i.test(value) ? value : ''
}
function size(item: AttachmentItem) {
  return item.sizeBytes == null ? item.mimeType : item.sizeBytes < 1024 ? `${item.sizeBytes} B` : `${(item.sizeBytes / 1024).toFixed(1)} KB`
}
</script>

<template>
  <article class="workbench-attachments" data-testid="attachments-block">
    <header><span>{{ unit.payload.title || '附件' }}</span><small>{{ items.length }} 项</small></header>
    <div class="attachment-items" :class="{ expanded }">
      <template v-for="item in visibleItems" :key="item.id">
        <button v-if="item.kind === 'image' && safeUrl(item)" class="image-preview" data-testid="attachment-image" :aria-label="`预览 ${item.name}`" @click="preview = item">
          <img :src="safeUrl(item)" :alt="item.name" :width="item.width || 240" :height="item.height || 160" loading="lazy" />
          <span :title="item.name">{{ item.name }}</span>
        </button>
        <a v-else-if="safeUrl(item)" class="file-item" data-testid="attachment-file" :href="safeUrl(item)" target="_blank" rel="noopener noreferrer"><span>▤</span><span><strong :title="item.name">{{ item.name }}</strong><small>{{ size(item) }}</small></span><span>↗</span></a>
        <div v-else class="file-item" data-testid="attachment-file"><span>▤</span><span><strong :title="item.name">{{ item.name }}</strong><small>{{ size(item) }}</small></span></div>
      </template>
    </div>
    <button v-if="items.length > 4" class="expand-attachments" :aria-expanded="expanded" @click="expanded = !expanded">{{ expanded ? '收起附件' : `展开其余 ${items.length - 4} 项` }}</button>
    <WorkbenchDialog v-if="preview" :label="`图片预览 ${preview.name}`" wide preview @close="preview = null">
        <section class="media-preview-dialog">
          <header class="preview-heading"><strong :title="preview.name">{{ preview.name }}</strong><button ref="closePreview" aria-label="关闭图片预览" @click="preview = null">×</button></header>
          <img class="preview-body" :key="preview.id" :src="safeUrl(preview)" :alt="preview.name" @load="previewLoaded" />
          <footer class="preview-footer"><span><template v-if="previewSize">{{ previewSize.width }} × {{ previewSize.height }} · </template>{{ size(preview) }}</span><a :href="safeUrl(preview)" target="_blank" rel="noopener noreferrer">打开原图 ↗</a></footer>
        </section>
    </WorkbenchDialog>
  </article>
</template>

<style scoped>
.workbench-attachments { border:1px solid var(--tuck-border); border-radius:7px; overflow:hidden; color:var(--tuck-text); background:var(--tuck-surface); }
.workbench-attachments > header { display:flex; gap:8px; align-items:center; padding:0 8px; min-height:32px; box-sizing:border-box; font-size:var(--tuck-font-caption); }
.workbench-attachments small { color:var(--tuck-muted); font-size:var(--tuck-font-caption); }
.attachment-items { display:grid; grid-template-columns:repeat(auto-fit,minmax(150px,240px)); gap:6px; padding:0 6px 6px; }
.attachment-items.expanded { max-height:320px; overflow:auto; overscroll-behavior-x:contain; }
.workbench-attachments .image-preview { min-width:0; border:1px solid var(--tuck-border); border-radius:5px; background:var(--tuck-background); color:var(--tuck-text); padding:0; cursor:zoom-in; overflow:hidden; align-self:start; }
.image-preview img { display:block; width:100%; height:140px; object-fit:contain; background:var(--tuck-background); }
.image-preview > span { display:block; padding:4px 6px; overflow:hidden; white-space:nowrap; text-overflow:ellipsis; font-size:var(--tuck-font-caption); }
.file-item { display:flex; align-items:center; gap:8px; min-width:0; height:44px; box-sizing:border-box; padding:5px 8px; border:1px solid var(--tuck-border); border-radius:5px; color:var(--tuck-text); background:var(--tuck-background); text-decoration:none; }
.file-item > span:nth-child(2) { display:grid; flex:1; min-width:0; gap:2px; }
.file-item strong { font-size:var(--tuck-font-caption); font-weight:500; overflow:hidden; white-space:nowrap; text-overflow:ellipsis; }
.file-item small { overflow:hidden; white-space:nowrap; text-overflow:ellipsis; }
a.file-item:hover, .image-preview:hover { border-color:var(--tuck-accent); }
.expand-attachments { border:0; border-top:1px solid var(--tuck-border); border-radius:0; width:100%; padding:5px; background:var(--tuck-surface); color:var(--tuck-muted); cursor:pointer; font-size:var(--tuck-font-caption); }
.media-preview-dialog { min-width:0; color:var(--tuck-text); background:var(--tuck-surface); }
@media(max-width:480px) { .attachment-items { grid-template-columns:repeat(2,minmax(0,1fr)); } .image-preview img { height:100px; } }
</style>
