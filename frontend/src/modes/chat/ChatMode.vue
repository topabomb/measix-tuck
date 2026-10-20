<script setup lang="ts">
import { computed, inject, ref, watch, onBeforeUnmount, onUnmounted, nextTick } from 'vue'
import { useRoute } from 'vue-router'
import { router, conversationPath, settingsPathKey } from '../../app/router'
import type { DockerWorkspaceRuntime } from '../../shared/workspace/runtime'
import ChatPage from './ChatPage.vue'
import ChatDelivery from './ChatDelivery.vue'
import TuckLoading from '../../shared/ui/TuckLoading.vue'
import NewConversationDialog from './components/NewConversationDialog.vue'
import SessionList from './components/SessionList.vue'
export interface ChatViewState { search: string; assistant: string; archivesOpen: boolean; diagnosticsOpen: boolean; listOffset: number; lastRoute: string }
const props = defineProps<{ workspace: DockerWorkspaceRuntime; state: ChatViewState }>()
const route = useRoute()
const settingsPath=inject(settingsPathKey,computed(()=>'/settings'))
const page = ref<InstanceType<typeof ChatPage> | null>(null)
const ready = ref(false), pending = ref(false), error = ref(''), creating = ref(false), epoch = ref(0)
let request = 0
const catalogRevision = ref(0)
const unsubscribe = props.workspace.subscribe(() => {
  catalogRevision.value++
  if (route.meta.mode === 'chat' && (ready.value || deliveryId.value) && typeof route.params.id === 'string' && !props.workspace.hasSession(route.params.id)) {
    ready.value = false; deliveryId.value = ''
    const next = props.workspace.descriptors[0]?.id
    void router.replace(next ? conversationPath(next) : '/chat')
  }
})
const deliveryId=ref('')
const deliveryTask=computed(()=>{void catalogRevision.value;return props.workspace.tasks.get(deliveryId.value)})
function save() { page.value?.saveView() }
async function open() {
  const revision = ++request
  if (route.meta.mode !== 'chat') return
  const path=route.path
  const current=()=>revision===request && route.path===path && route.meta.mode==='chat'
  save(); deliveryId.value=''; creating.value = false; error.value = ''; pending.value = true
  try {
    let id = typeof route.params.id === 'string' ? route.params.id : props.workspace.activeSessionId
    if (!id) id = await props.workspace.ensureDefaultSession()
    if (!current()) return
    if (!id) { ready.value=false; await nextTick(); if(current())props.workspace.suspendView(); return }
    if (!props.workspace.hasSession(id)) {
      try { await props.workspace.refreshTask(id) }
      catch(cause) {if((cause as {status?:number}).status===404)throw new Error('指定对话不存在');throw cause}
      if(!current())return
    }
    if(route.name==='chat-delivery') {
      const task=props.workspace.tasks.get(id)
      if(!task || task.parentTaskId)throw new Error('此对话没有独立交付')
      ready.value=false;deliveryId.value=id
      await nextTick()
      if(current())props.workspace.suspendView()
      return
    }
    const raw = route.params.message
    const message = typeof raw === 'string' && /^\d+$/.test(raw) ? Number(raw) : undefined
    if (raw !== undefined && message === undefined) throw new Error('消息地址无效')
    // A remembered mode entry restores its reader; a new deep link owns the target.
    const target = message !== undefined && props.state.lastRoute !== route.path
    if (target) await props.workspace.prepareMessage(id, message)
    else await props.workspace.prepareSession(id)
    if (!current()) return
    if (target) {
      ready.value = false
      await nextTick()
      if (!current()) return
      props.workspace.targetMessage(id, message)
      epoch.value++
    }
    props.workspace.activate(id)
    if (props.state.assistant && props.workspace.tasks.get(id)?.agent !== props.state.assistant) props.state.assistant = ''
    ready.value = true
    props.state.lastRoute = path
    // Native hash navigation updates the address before Vue Router commits it.
    // Keep the loaded view if leaving is cancelled, but never replace a newer destination.
    const address=router.resolve(location.hash.slice(1)||'/')
    if (!route.params.id && current() && address.path===path) await router.replace({path:conversationPath(id),query:address.query})
  } catch (cause) {
    if (current()) {
      ready.value = false; error.value = (cause as Error).message
      // Release foreground ownership after the outgoing reader saves/unmounts.
      await nextTick()
      if (current()) props.workspace.suspendView()
    }
  }
  finally { if (revision === request) pending.value = false }
}
watch(() => route.path, open, { immediate:true })
onBeforeUnmount(() => { request++; unsubscribe(); save() })
onUnmounted(() => props.workspace.suspendView())
const choices = computed(() => {
  void catalogRevision.value
  return props.workspace.descriptors
})
</script>
<template>
  <ChatDelivery v-if="deliveryTask" :task="deliveryTask" />
  <ChatPage v-else-if="ready" :key="epoch" ref="page" :workspace="workspace" :view-state="state" :pending="pending" />
  <section v-else class="chat-entry">
    <TuckLoading v-if="pending" label="正在加载对话…" />
    <template v-else-if="error"><h1>无法打开</h1><p role="alert">{{ error }}</p><button v-if="!route.params.id" @click="creating = true">新对话</button><RouterLink class="back-link" to="/chat">← 返回对话</RouterLink></template>
    <template v-else>
      <h1>与 Tuck 对话</h1>
      <button @click="creating = true">新对话</button>
      <div v-if="choices.length" class="assistant-conversations"><SessionList :sessions="choices" active-id="" query="" @select="id => router.push(conversationPath(id))" /></div>
      <p v-else>还没有对话</p>
    </template>
    <RouterLink v-if="!pending" class="back-link" :to="settingsPath">设置</RouterLink>
  </section>
  <NewConversationDialog v-if="creating" :workspace="workspace" :connection="workspace.runtimeInfo.connection" @close="creating = false" @created="id => router.push(conversationPath(id))" />
</template>
<style scoped>
.chat-entry { height:100%; box-sizing:border-box; padding:32px; overflow:auto; display:flex; flex-direction:column; align-items:center; justify-content:center; gap:12px; }h1,p { margin:0; }h1 { font-size:22px; }p { color:var(--tuck-muted); }.assistant-conversations { display:flex; height:min(50%,420px); width:min(100%,460px); min-height:0; }a,button { color:var(--tuck-accent); }button { padding:8px 16px; background:var(--tuck-raised); border:1px solid var(--tuck-border); border-radius:6px; font:inherit; cursor:pointer; }
</style>
