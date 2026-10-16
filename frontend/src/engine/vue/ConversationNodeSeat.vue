<script setup lang="ts">
import { onBeforeUnmount, shallowRef, watch } from 'vue'
import type { ConversationSessionRuntime } from '../runtime/session-runtime'
import RenderUnitView from './RenderUnitView.vue'
import type { RendererResolver } from './renderers/registry'

const props = defineProps<{ runtime: ConversationSessionRuntime; nodeId: string; renderers?: RendererResolver; assistantName?: string }>()
const unit = shallowRef(props.runtime.projection.getNode(props.nodeId))
let unsubscribe: (() => void) | null = null

function bind(runtime: ConversationSessionRuntime, nodeId: string): void {
  unsubscribe?.()
  unit.value = runtime.projection.getNode(nodeId)
  unsubscribe = runtime.projection.subscribeNode(nodeId, () => {
    unit.value = runtime.projection.getNode(nodeId)
  })
}

watch(() => [props.runtime, props.nodeId] as const, ([runtime, nodeId]) => bind(runtime, nodeId), { immediate: true })
onBeforeUnmount(() => unsubscribe?.())
</script>

<template>
  <div v-if="unit" class="node-seat">
    <RenderUnitView :unit="unit" :renderers="renderers" :assistant-name="assistantName" />
    <slot name="actions" :unit="unit" />
  </div>
</template>
<style scoped>.node-seat { position:relative; }</style>
