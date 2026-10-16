<script setup lang="ts">
import { computed } from 'vue'
import type { RenderUnit } from '../../presentation/render-unit'
import type { AttachmentItem, ArtifactProvenance } from '../../model/conversation'

const props = defineProps<{ unit: RenderUnit }>()
const items = computed(() => Array.isArray(props.unit.payload.items) ? props.unit.payload.items as unknown as AttachmentItem[] : [])
const images = computed(() => items.value.filter(item => item.kind === 'image'))
const files = computed(() => items.value.filter(item => item.kind !== 'image'))
const provenance = computed(() => (props.unit.payload.provenance ?? null) as ArtifactProvenance | null)
const title = computed(() => String(props.unit.payload.title ?? (provenance.value?.origin === 'tool-output' ? 'Generated media' : 'Attachments')))

function formatBytes(value?: number): string {
  if (!value || value <= 0) return 'size unknown'
  if (value < 1024) return `${value} B`
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB`
  return `${(value / 1024 / 1024).toFixed(1)} MB`
}

function imageSrc(item: AttachmentItem): string {
  if (item.src) return item.src
  const width = Math.max(1, item.width ?? 768)
  const height = Math.max(1, item.height ?? 768)
  const seed = item.seed ?? 1
  const a = seed % 360
  const b = (a + 72) % 360
  const label = item.name.replace(/[<>&]/g, '')
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="${width}" height="${height}" viewBox="0 0 ${width} ${height}"><defs><linearGradient id="g" x1="0" y1="0" x2="1" y2="1"><stop stop-color="hsl(${a} 68% 40%)"/><stop offset="1" stop-color="hsl(${b} 64% 18%)"/></linearGradient></defs><rect width="100%" height="100%" rx="24" fill="url(#g)"/><circle cx="72%" cy="28%" r="18%" fill="rgba(255,255,255,.13)"/><circle cx="28%" cy="70%" r="25%" fill="rgba(255,255,255,.08)"/><text x="50%" y="51%" text-anchor="middle" font-size="34" fill="rgba(255,255,255,.88)" font-family="system-ui">${label}</text></svg>`
  return `data:image/svg+xml;charset=UTF-8,${encodeURIComponent(svg)}`
}
</script>

<template>
  <article class="message-card attachment-card" data-testid="attachments-block">
    <header class="attachment-head">
      <div><strong>{{ title }}</strong><small>{{ items.length }} item{{ items.length === 1 ? '' : 's' }}</small></div>
      <span v-if="provenance?.model" class="media-model">{{ provenance.model }}</span>
    </header>

    <p v-if="provenance?.prompt" class="media-prompt" data-testid="media-prompt"><span>Prompt</span>{{ provenance.prompt }}</p>

    <div v-if="images.length" class="attachment-grid" :class="`count-${Math.min(images.length, 4)}`">
      <figure v-for="item in images" :key="item.id" class="attachment-image" data-testid="attachment-image">
        <img :src="imageSrc(item)" :alt="item.name" :width="item.width ?? 768" :height="item.height ?? 768" loading="lazy" :style="{ aspectRatio: `${item.width ?? 768} / ${item.height ?? 768}` }" />
        <figcaption><strong>{{ item.name }}</strong><span>{{ item.width ?? '?' }}×{{ item.height ?? '?' }} · {{ formatBytes(item.sizeBytes) }}</span></figcaption>
      </figure>
    </div>

    <div v-if="files.length" class="attachment-files">
      <div v-for="item in files" :key="item.id" class="attachment-file" data-testid="attachment-file">
        <span class="file-glyph">{{ item.kind === 'audio' ? '♫' : item.kind === 'video' ? '▶' : '▤' }}</span>
        <span><strong>{{ item.name }}</strong><small>{{ item.mimeType }} · {{ formatBytes(item.sizeBytes) }}<template v-if="item.durationMs"> · {{ (item.durationMs / 1000).toFixed(1) }}s</template></small></span>
      </div>
    </div>

    <footer v-if="provenance?.toolName || provenance?.toolCallId" class="attachment-provenance">
      {{ provenance.toolName ?? 'tool' }}<template v-if="provenance.toolCallId"> · {{ provenance.toolCallId }}</template>
    </footer>
  </article>
</template>
