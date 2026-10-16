<script setup lang="ts">
import {computed} from 'vue'
import type {RenderUnit} from '../../../engine/presentation/render-unit'
import MarkdownBlock from '../../../engine/vue/renderers/MarkdownBlock.vue'
const props=defineProps<{unit:RenderUnit;assistantName?:string}>()
const origin=computed(()=>(props.unit.payload.provenance as {origin?:string}|undefined)?.origin)
</script>
<template>
 <details v-if="origin==='delivery-required'" class="orchestration-note" data-testid="orchestration-note"><summary><strong>Tuck 自动要求补交付</strong><span>上一轮已完成，尚未提交成果</span></summary><pre>{{unit.payload.markdown}}</pre></details>
 <MarkdownBlock v-else :unit="unit" :assistant-name="assistantName" />
</template>
<style scoped>
.orchestration-note{margin:6px 0;padding:8px 12px;border-left:2px solid var(--tuck-border);color:var(--tuck-muted);font-size:var(--tuck-font-caption);line-height:1.6;overflow-wrap:anywhere}.orchestration-note summary{cursor:pointer}.orchestration-note strong{font-weight:500}.orchestration-note span{margin-left:8px}.orchestration-note pre{white-space:pre-wrap;font:inherit;margin:10px 0 0}
</style>
