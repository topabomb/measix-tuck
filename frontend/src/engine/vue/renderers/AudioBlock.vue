<script setup lang="ts">
import { computed } from 'vue'
import type { RenderUnit } from '../../presentation/render-unit'

const props = defineProps<{ unit: RenderUnit }>()
const title = computed(() => String(props.unit.payload.title ?? 'Audio'))
const purpose = computed(() => String(props.unit.payload.purpose ?? 'recording'))
const durationMs = computed(() => Math.max(0, Number(props.unit.payload.durationMs ?? 0)))
const duration = computed(() => `${Math.floor(durationMs.value / 60_000)}:${String(Math.floor(durationMs.value / 1000) % 60).padStart(2, '0')}`)
const transcript = computed(() => String(props.unit.payload.transcript ?? ''))
const model = computed(() => String(props.unit.payload.model ?? ''))
const status = computed(() => String(props.unit.payload.status ?? 'ready'))
const src = computed(() => typeof props.unit.payload.src === 'string' ? props.unit.payload.src : '')
const waveform = computed(() => {
  const supplied = props.unit.payload.waveform
  if (Array.isArray(supplied) && supplied.length) return supplied.slice(0, 48).map(value => Math.max(0.08, Math.min(1, Number(value) || 0)))
  return Array.from({ length: 36 }, (_, i) => 0.22 + ((i * 17 + props.unit.messageIndex * 7) % 68) / 100)
})
</script>

<template>
  <article class="message-card audio-card" :class="`audio-${status}`" data-testid="audio-block">
    <header class="audio-head">
      <span class="audio-glyph">{{ purpose === 'tts' ? '◖)))' : '◉' }}</span>
      <span><strong>{{ title }}</strong><small>{{ purpose.toUpperCase() }}<template v-if="model"> · {{ model }}</template> · {{ duration }}</small></span>
      <span class="audio-status">{{ status }}</span>
    </header>

    <audio v-if="src" :src="src" controls preload="metadata" />
    <div v-else class="audio-waveform" aria-label="Synthetic waveform" data-testid="audio-waveform">
      <i v-for="(level, index) in waveform" :key="index" :style="{ height: `${Math.round(level * 100)}%` }" />
    </div>

    <div v-if="transcript" class="audio-transcript" data-testid="audio-transcript">
      <span>{{ purpose === 'asr-input' ? 'Transcript / ASR result' : 'Spoken text' }}</span>
      <p>{{ transcript }}</p>
    </div>
  </article>
</template>
