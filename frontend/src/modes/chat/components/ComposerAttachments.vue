<script setup lang="ts">
import { ref, watch } from 'vue'
import type { DraftAttachment } from '../../../shared/workspace/attachment-drafts'
import WorkbenchDialog from '../../../shared/ui/WorkbenchDialog.vue'
const props = defineProps<{ items: readonly DraftAttachment[]; disabled: boolean }>()
defineEmits<{ remove: [id: string]; retry: [id: string] }>()
const preview = ref<DraftAttachment | null>(null)
watch(() => props.items, items => { if (preview.value && !items.some(item => item.id === preview.value?.id)) preview.value = null })
</script>
<template>
  <div v-if="items.length" class="draft-attachments" role="group" aria-label="待发送附件">
    <div v-for="item in items" :key="item.id" class="draft-attachment" :data-status="item.status">
      <button v-if="item.previewUrl" class="thumbnail" :aria-label="`预览 ${item.file.name}`" @click="preview = item"><img :src="item.previewUrl" :alt="item.file.name" /></button>
      <span v-else class="file-icon" aria-hidden="true">▤</span>
      <div class="attachment-label"><span :title="item.file.name">{{ item.file.name }}</span><small role="status" :title="item.error">{{ item.status === 'uploading' ? '上传中…' : item.status === 'error' ? item.error : item.uploaded?.kind === 'image' ? '图片' : '文件' }}</small></div>
      <button v-if="item.status === 'error'" :disabled="disabled" :aria-label="`重试 ${item.file.name}`" @click="$emit('retry', item.id)">重试</button>
      <button :disabled="disabled" :aria-label="`移除 ${item.file.name}`" @click="$emit('remove', item.id)">×</button>
    </div>
  </div>
  <WorkbenchDialog v-if="preview" :label="`图片预览 ${preview.file.name}`" wide preview @close="preview = null">
    <div class="preview-heading"><strong :title="preview.file.name">{{ preview.file.name }}</strong><button aria-label="关闭图片预览" @click="preview = null">×</button></div>
    <img class="preview-body" :src="preview.previewUrl" :alt="preview.file.name" />
  </WorkbenchDialog>
</template>
<style scoped>
.draft-attachments { display:flex; gap:6px; overflow-x:auto; padding:0 0 7px; max-width:100%; scrollbar-width:thin; }
.draft-attachment { display:flex; flex:0 0 210px; align-items:center; gap:6px; height:52px; padding:4px 6px; box-sizing:border-box; border:1px solid var(--tuck-border); border-radius:7px; background:var(--tuck-background); color:var(--tuck-text); }
.draft-attachment[data-status=error] { border-color:var(--tuck-danger); }
.attachment-label { display:grid; gap:2px; flex:1; min-width:0; font-size:var(--tuck-font-caption); }
.attachment-label span,.attachment-label small { overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
.attachment-label small { font-size:inherit; color:var(--tuck-muted); }
.draft-attachment[data-status=error] small { color:var(--tuck-danger); }
button { border:0; border-radius:4px; background:transparent; color:var(--tuck-text); cursor:pointer; font:inherit; padding:4px; flex-shrink:0; }
button:hover { background:var(--tuck-hover); } button:disabled { opacity:.45; cursor:default; }
.thumbnail { padding:0; width:40px; height:40px; cursor:zoom-in; overflow:hidden; } .thumbnail img { width:100%; height:100%; object-fit:contain; }
.file-icon { font-size:24px; color:var(--tuck-muted); }
</style>
