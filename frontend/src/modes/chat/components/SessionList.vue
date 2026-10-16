<script setup lang="ts">
import { computed, nextTick, ref, watch, onMounted } from 'vue'
import { conversationPath, router, modes } from '../../../app/router'
import { VList, type VListHandle } from 'virtua/vue'
import type { WorkSessionDescriptor } from '../../../shared/workspace/runtime'
import { deriveSessionIndicator } from '../../../engine/conversation/session-semantics'
import ActivityStatus from '../../../shared/ui/ActivityStatus.vue'
import AssistantIcon from '../../../shared/ui/AssistantIcon.vue'
import { indicatorLabel } from '../session-presentation'
const props = defineProps<{ sessions: readonly WorkSessionDescriptor[]; activeId: string; query: string; assistantNames?: Record<string,string>; showAssistant?: boolean; initialOffset?: number }>()
const emit = defineEmits<{ select: [id: string] }>()
const items = computed(() => [...props.sessions])
const list = ref<VListHandle | null>(null), host = ref<HTMLElement | null>(null)
const indicator = deriveSessionIndicator
const taskIcon = modes.find(mode => mode.id === 'tasks')!.icon
const detail = (task: WorkSessionDescriptor) => task.queuedPrompts ? `${indicatorLabel(indicator(task))} · ${task.queuedPrompts} 条排队` : ['blocked','failed','interrupted','max-tokens'].includes(indicator(task)) ? indicatorLabel(indicator(task)) : ''
watch(() => props.query, () => list.value?.scrollToIndex(0))
watch(() => props.activeId, async id => {
  await nextTick()
  const index = props.sessions.findIndex(task => task.id === id)
  if (index >= 0) list.value?.scrollToIndex(index, { align: 'nearest' })
})
onMounted(() => { if (props.initialOffset) list.value?.scrollTo(props.initialOffset) })
defineExpose({ offset: () => list.value?.scrollOffset ?? 0 })
let pendingFocus: string | null = null
function focusPending() {
  if (!pendingFocus) return
  const button = host.value?.querySelector<HTMLElement>(`[data-session-id="${CSS.escape(pendingFocus)}"]`)
  if (button) { button.focus({ preventScroll:true }); if (document.activeElement === button) pendingFocus = null }
}
function mountedRow() { void nextTick().then(focusPending) }
async function navigate(event: KeyboardEvent) {
  if (event.key === 'Tab') pendingFocus = null
  if (!['ArrowDown','ArrowUp','Home','End'].includes(event.key) || !props.sessions.length) return
  const button = (event.target as HTMLElement).closest<HTMLElement>('[data-session-id]')
  if (!button) return
  event.preventDefault()
  const current = props.sessions.findIndex(task => task.id === button.dataset.sessionId)
  const index = event.key === 'Home' ? 0 : event.key === 'End' ? props.sessions.length - 1 : Math.max(0, Math.min(props.sessions.length - 1, current + (event.key === 'ArrowDown' ? 1 : -1)))
  pendingFocus = props.sessions[index]!.id
  list.value?.scrollToIndex(index, { align: 'nearest' })
  await nextTick()
  focusPending()
}
</script>
<template>
  <nav ref="host" class="session-list-host" aria-label="对话列表" data-testid="recent-sessions" @keydown="navigate">
    <VList v-if="sessions.length" ref="list" class="session-list" :data="items" :item-size="showAssistant ? 42 : 38" :buffer-size="120" @scroll="mountedRow" @scroll-end="mountedRow">
      <template #default="{ item: task }">
        <a :href="router.resolve(conversationPath(task.id)).href" :ref="mountedRow" :key="task.id" class="session-row" :class="[`indicator-${indicator(task)}`, { active: task.id === activeId, unread: task.unread }]" :data-session-id="task.id" :data-testid="`session-${task.id}`" :aria-current="task.id === activeId ? 'page' : undefined" :aria-label="`${task.title} · ${indicatorLabel(indicator(task))}${task.queuedPrompts ? ` · ${task.queuedPrompts} 条排队` : ''}${task.taskChannel ? ' · 来自任务频道' : ''}`" :title="`${task.title} · ${indicatorLabel(indicator(task))} · ${task.age}`" @click="event => { if (!event.ctrlKey && !event.metaKey && !event.shiftKey && !event.altKey && event.button === 0) { event.preventDefault(); emit('select',task.id) } }">
          <ActivityStatus class="session-glyph" :status="indicator(task)" :label="indicatorLabel(indicator(task))" aria-hidden="true" />
          <span class="session-copy"><strong>{{ task.title }}</strong><small v-if="showAssistant || detail(task) || task.taskChannel" class="session-detail"><span v-if="showAssistant" class="session-assistant" :title="assistantNames?.[task.agent || 'root'] || task.agent"><AssistantIcon /><span>{{ assistantNames?.[task.agent || 'root'] || task.agent }}</span></span><span v-if="task.taskChannel" class="session-task-source" title="来自任务频道" role="img" aria-label="来自任务频道"><svg viewBox="0 0 24 24" aria-hidden="true"><path :d="taskIcon" /></svg></span><span v-if="detail(task)" class="session-state">{{ detail(task) }}</span></small></span>
          <i v-if="task.unread" class="unread-dot" aria-label="未读" />
        </a>
      </template>
    </VList>
    <p v-else class="session-empty">没有匹配的对话</p>
  </nav>
</template>
<style scoped>
.session-list-host { flex:1; min-height:0; overflow:hidden; }
.session-list { height:100%; padding:0 8px; }
.session-row { text-decoration:none; box-sizing:border-box; display:flex; width:100%; border:1px solid transparent; background:transparent; color:var(--tuck-text); text-align:left; font:inherit; cursor:pointer; }
.session-row:hover { background:var(--tuck-hover); }
.session-row.active { background:var(--tuck-raised); color:var(--tuck-accent); }
.session-copy { display:grid; }
.session-copy strong { display:block; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
.unread-dot { width:6px; height:6px; flex:none; border-radius:50%; background:var(--tuck-accent); }
.session-list .session-row { align-items:center; gap:8px; min-height:38px; padding:4px 8px; border-radius:6px; }
.session-copy { flex:1; min-width:0; gap:0; }
.session-detail { display:flex; align-items:center; gap:6px; min-width:0; line-height:16px; }
.session-assistant { display:inline-flex; align-items:center; gap:3px; max-width:104px; flex-shrink:0; padding:0 4px; border-radius:3px; background:var(--tuck-hover); color:var(--tuck-muted); white-space:nowrap; }
.session-assistant > span { min-width:0; overflow:hidden; text-overflow:ellipsis; }
.session-assistant > .assistant-icon { width:12px; height:12px; }
.session-task-source { display:inline-flex; flex:none; align-items:center; color:var(--tuck-muted); }
.session-task-source svg { width:13px; height:13px; fill:none; stroke:currentColor; stroke-width:1.7; stroke-linecap:round; stroke-linejoin:round; }
.session-state { overflow:hidden; text-overflow:ellipsis; white-space:nowrap; color:var(--tuck-muted); }
.session-copy strong { font:400 var(--tuck-font-ui)/var(--tuck-line-ui) var(--tuck-font-family); }
.session-row.active .session-copy strong { font-weight:600; }
.session-copy small { font:var(--tuck-font-caption)/var(--tuck-line-caption) var(--tuck-font-family); }
.session-glyph { flex:none; width:12px; margin:0; }
.session-empty { margin:12px; color:var(--tuck-muted); font-size:var(--tuck-font-caption); }
</style>
