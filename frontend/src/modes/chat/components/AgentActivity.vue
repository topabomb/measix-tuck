<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import type { RenderUnit } from '../../../engine/presentation/render-unit'
import { useFoldState } from '../../../engine/vue/renderers/fold-state'
import ActivityStatus from '../../../shared/ui/ActivityStatus.vue'
import DisclosureIcon from '../../../shared/ui/DisclosureIcon.vue'
import DeliveryActivity from './DeliveryActivity.vue'
import WorkbenchAttachments from './WorkbenchAttachments.vue'
import {readTuckToolResult} from './tool-result'
import {renderMarkdownCached} from '../../../engine/vue/renderers/markdown-cache'
const props = defineProps<{ unit: RenderUnit }>()
const presentation = computed(() => (props.unit.payload.presentation as { kind?: string } | undefined)?.kind)
const open = useFoldState(props.unit.id, false)
const kind = computed(() => props.unit.kind)
const thinking = computed(() => kind.value === 'thinking')
const terminal = computed(() => kind.value === 'terminal')
const result = computed(() => props.unit.payload.phase === 'result')
const toolResult=computed(()=>result.value?readTuckToolResult(String(props.unit.payload.name),props.unit.payload.output):null)
const inspectionText=computed(()=>toolResult.value?.status==='completed'&&props.unit.payload.name==='inspect_attachments'&&typeof toolResult.value.text==='string'?toolResult.value.text:'')
const inspectionHTML=computed(()=>open.value&&inspectionText.value?renderMarkdownCached(props.unit.id+':inspection',props.unit.revision,inspectionText.value):'')
const failureMessage=computed(()=>['failed','partial','unknown'].includes(String(toolResult.value?.status))&&typeof toolResult.value?.message==='string'?toolResult.value.message:'')
const inputMedia = computed<RenderUnit | null>(() => {
  const items = props.unit.payload.inputAttachments
  if (result.value || props.unit.payload.name !== 'inspect_attachments' || !Array.isArray(items) || !items.length) return null
  return { ...props.unit, id: props.unit.id + ':input-images', kind: 'attachments', payload: { items, title: '识别输入 · 当前文件预览' } }
})
const title = computed(() => thinking.value ? '思考' : terminal.value ? String(props.unit.payload.command || '终端') : String(props.unit.payload.name || '工具'))
const state = computed(() => ['partial','unknown'].includes(String(toolResult.value?.status))?String(toolResult.value?.status):String(props.unit.payload.status || ''))
const statusLabel = computed(() => ({ success: '完成', complete: '完成', completed: '完成', partial:'部分完成',unknown:'结果未确认', error: '失败', streaming: '思考中', running: '执行中', interrupted: '已停止' }[state.value] || state.value))
const preview = ref(''), previewBox = ref<HTMLElement | null>(null), previewText = ref<HTMLElement | null>(null), clipped = ref(false)
let previewTimer: ReturnType<typeof setTimeout> | undefined, observer: ResizeObserver | undefined
function updatePreview() {
  // Bound work and DOM text even when the canonical reasoning grows very large.
  const tail = String(props.unit.payload.text || '').slice(-2048)
  preview.value = tail.replace(/^[\uDC00-\uDFFF]/, '').replace(/\s+/g, ' ').trim()
}
watch(() => [props.unit.payload.text, state.value], () => {
  if (!thinking.value) return
  if (state.value !== 'streaming' || !preview.value) { clearTimeout(previewTimer); previewTimer = undefined; updatePreview() }
  else if (!previewTimer) previewTimer = setTimeout(() => { previewTimer = undefined; updatePreview() }, 180)
}, { immediate: true })
onMounted(() => {
  observer = new ResizeObserver(() => { clipped.value = !!previewText.value && !!previewBox.value && previewText.value.scrollWidth > previewBox.value.clientWidth + 1 })
  if (previewBox.value) observer.observe(previewBox.value)
  if (previewText.value) observer.observe(previewText.value)
})
onBeforeUnmount(() => { clearTimeout(previewTimer); observer?.disconnect() })
const brief = computed(() => {
  if (thinking.value) return preview.value
  if (terminal.value) return ''
  if (result.value) return ({accepted:'已接收',partial:'部分完成',unknown:'结果未确认',failed:'调用失败'}[String(toolResult.value?.status)]||'返回结果')
  const input = props.unit.payload.input as Record<string, unknown> | undefined
  return String(input?.description || input?.path || input?.command || input?.agent || '').slice(0, 100)
})
const body = computed(() => {
  if (!open.value) return ''
  if (thinking.value) return String(props.unit.payload.text || '')
  if(toolResult.value)return JSON.stringify(toolResult.value,null,2)
  const content = terminal.value || result.value ? props.unit.payload.output : props.unit.payload.input
  return typeof content === 'string' ? content : JSON.stringify(content ?? {}, null, 2)
})
const detail = ref<HTMLElement | null>(null)
let followDetail = false
function detailScrolled() { const el = detail.value; if (el) followDetail = el.scrollHeight - el.scrollTop - el.clientHeight < 24 }
watch(open, value => { followDetail = value && ['streaming','running'].includes(state.value) })
watch(body, async () => { await nextTick(); if (followDetail && detail.value) detail.value.scrollTop = detail.value.scrollHeight }, { flush:'post' })
</script>
<template>
  <DeliveryActivity v-if="result && unit.payload.name === 'deliver_result'" :unit="unit" />
  <article v-else class="agent-activity" :data-open="open" :data-testid="thinking ? 'thinking-block' : terminal ? 'terminal-block' : 'tool-block'" :data-call-id="unit.payload.callId" :data-status="state" :data-category="unit.payload.category" :data-presentation-kind="presentation">
    <button class="activity-summary" :aria-expanded="open" @click="open = !open"><span class="activity-icon" aria-hidden="true">{{ thinking ? '✧' : terminal ? '›_' : result ? '↳' : '⚙' }}</span><strong :title="title">{{ title }}</strong><span ref="previewBox" class="activity-brief" :class="{ 'tail-preview': thinking && !open, clipped: thinking && !open && clipped }"><span v-show="!thinking || !open" ref="previewText">{{ brief }}</span></span><ActivityStatus :status="state" :label="statusLabel" /><DisclosureIcon :open="open" /></button>
    <div v-if="open" class="activity-detail">
      <div v-if="unit.payload.callId" class="activity-metadata"><span>{{ unit.payload.callId }}</span><span v-if="unit.payload.durationMs != null">{{ Math.round(Number(unit.payload.durationMs)) }} ms</span></div>
      <WorkbenchAttachments v-if="inputMedia" :unit="inputMedia" />
      <div v-if="inspectionText" class="inspection-result markdown-body" data-testid="inspection-result-text" v-html="inspectionHTML" />
      <div v-else-if="failureMessage" class="tool-failure" data-testid="tool-failure-message"><p>{{failureMessage}}</p><code>{{toolResult?.code}}</code><code v-if="toolResult?.field">{{toolResult.field}}</code></div>
      <details v-if="inspectionText||failureMessage" class="result-details"><summary>工具返回详情</summary><pre><code>{{body}}</code></pre></details>
      <pre v-else ref="detail" @scroll.passive="detailScrolled" :class="{ 'activity-terminal': terminal }"><code>{{ body }}</code></pre>
    </div>
  </article>
</template>
<style scoped>
.agent-activity { min-width:0; max-width:100%; box-sizing:border-box; border-radius:6px; background:transparent; overflow:hidden; }
.agent-activity[data-open=true] { outline:1px solid var(--tuck-border); outline-offset:-1px; background:var(--tuck-surface); }
.agent-activity .activity-summary:hover { background:var(--tuck-hover); }
.agent-activity .activity-summary { display:flex; align-items:center; gap:4px; min-height:var(--chat-auxiliary-row-height,26px); width:100%; padding:3px 6px; text-align:left; border:0; border-radius:0; background:transparent; color:var(--tuck-text); font-family:inherit; font-size:var(--tuck-font-ui); line-height:20px; }
.activity-summary strong { font-size:var(--tuck-font-ui); font-weight:500; flex:0 1 auto; max-width:70%; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
.activity-icon { display:inline-flex;align-items:center;justify-content:center;flex-shrink:0; width:14px; color:var(--tuck-muted); }
.activity-brief { min-width:0; flex:1; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; font-size:var(--tuck-font-caption); color:var(--tuck-muted); }
.tail-preview { display:flex; justify-content:flex-end; text-overflow:clip; }
.tail-preview > span { flex:0 0 auto; min-width:100%; }
.tail-preview.clipped { mask-image:linear-gradient(to right,transparent,#000 16px); }
.activity-detail { border-top:1px solid var(--tuck-border); }.activity-metadata { display:flex; gap:10px; padding:4px 10px; overflow-wrap:anywhere; color:var(--tuck-muted); font-size:var(--tuck-font-caption); }
.activity-detail pre { margin:0; max-height:min(42dvh,360px); overflow:auto; padding:6px 10px; white-space:pre-wrap; overflow-wrap:anywhere; font:12px/1.6 ui-monospace,Consolas,monospace; background:var(--tuck-background); color:var(--tuck-text); }
.activity-detail .activity-terminal { white-space:pre; overscroll-behavior-x:contain; }
.activity-terminal code { white-space:inherit; overflow-wrap:normal; word-break:normal; }
.inspection-result{max-height:min(50dvh,480px);overflow:auto;overflow-wrap:anywhere;padding:8px 10px;font-size:var(--tuck-font-body);line-height:1.65}.inspection-result :deep(p){margin:6px 0}.inspection-result :deep(pre){overflow:auto}.inspection-result :deep(img){max-width:100%}.tool-failure{padding:6px 10px;overflow-wrap:anywhere}.tool-failure p{margin:4px 0}.tool-failure code{display:inline-block;margin-right:8px;font-size:var(--tuck-font-caption);color:var(--tuck-muted)}.result-details>summary{padding:6px 10px;cursor:pointer;color:var(--tuck-muted);font-size:var(--tuck-font-caption)}
</style>
