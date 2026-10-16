<script setup lang="ts">
import { computed } from 'vue'
import type { RenderUnit } from '../../presentation/render-unit'
const props = defineProps<{ unit: RenderUnit }>()
const width = computed(() => Math.max(1, Number(props.unit.payload.width ?? 800)))
const height = computed(() => Math.max(1, Number(props.unit.payload.height ?? 450)))
const seed = computed(() => Number(props.unit.payload.seed ?? 1))
const alt = computed(() => String(props.unit.payload.alt ?? `Image output ${props.unit.messageIndex}`))
const src = computed(() => {
  const provided = props.unit.payload.src
  if (typeof provided === 'string' && provided) return provided
  const a = seed.value % 360
  const b = (a + 88) % 360
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="${width.value}" height="${height.value}" viewBox="0 0 ${width.value} ${height.value}"><defs><linearGradient id="g" x1="0" x2="1"><stop stop-color="hsl(${a} 70% 36%)"/><stop offset="1" stop-color="hsl(${b} 65% 18%)"/></linearGradient></defs><rect width="100%" height="100%" fill="url(#g)"/><circle cx="28%" cy="45%" r="18%" fill="rgba(255,255,255,.16)"/><text x="50%" y="52%" text-anchor="middle" font-size="42" fill="rgba(255,255,255,.82)" font-family="system-ui">responsive image ${props.unit.messageIndex}</text></svg>`
  return `data:image/svg+xml;charset=UTF-8,${encodeURIComponent(svg)}`
})
</script>
<template>
  <article class="message-card image-card" data-testid="image-block">
    <header><span class="kind-dot image-dot" />Image · {{ width }}×{{ height }}</header>
    <img :src="src" :width="width" :height="height" loading="lazy" :style="{ aspectRatio: `${width} / ${height}` }" :alt="alt" />
  </article>
</template>
