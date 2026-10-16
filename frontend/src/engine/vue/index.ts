/** Vue adapter surface. Framework-neutral Engine APIs live in `../index.ts`. */
export { default as ConversationViewport } from './ConversationViewport.vue'
export { default as RenderUnitView } from './RenderUnitView.vue'
export { default as ActivePlanStrip } from './ActivePlanStrip.vue'
export {
  RendererRegistry,
  createDefaultRendererRegistry,
  type RendererResolver,
} from './renderers/registry'
