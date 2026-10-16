<script setup lang="ts">
import { useRoute } from 'vue-router'
import { conversationPath } from '../../../app/router'
import { computed, nextTick, onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'
import {modelReasoningOptions,reasoningLabels} from '../../../../shared/reasoning.mjs'
import type { ConversationSessionRuntime } from '../../../engine/runtime/session-runtime'
import { ActivePlanStrip } from '../../../engine/vue'
import type { ConnectionInfo, DockerTask, DockerWorkspaceRuntime } from '../../../shared/workspace/runtime'

import SessionSummary from './SessionSummary.vue'
import DisclosureIcon from '../../../shared/ui/DisclosureIcon.vue'
import QuestionForm from '../../../shared/conversation/QuestionForm.vue'
import { taskStatus } from '../session-presentation'
import { presentQuestion } from '../../../shared/conversation/question-presentation'
import ComposerAttachments from './ComposerAttachments.vue'
import WorkbenchRunStatus from './WorkbenchRunStatus.vue'
import SummaryPopover from '../../../shared/ui/SummaryPopover.vue'
import {useMobileLayout} from '../../../shared/use-mobile-layout'
const props = defineProps<{ task: DockerTask; runtime: ConversationSessionRuntime; workspace: DockerWorkspaceRuntime; connection?: ConnectionInfo; connected: boolean; error?: string | null }>()
const route = useRoute()
const emit = defineEmits<{ sending: []; sent: []; layout: [] }>()
const fileInput = ref<HTMLInputElement | null>(null)
const attachments = shallowRef(props.workspace.attachmentDrafts.list(props.task.id))
const unsubscribeAttachments = props.workspace.attachmentDrafts.subscribe(props.task.id, () => { emit('layout'); attachments.value = props.workspace.attachmentDrafts.list(props.task.id) })
onBeforeUnmount(unsubscribeAttachments)
const dragActive = ref(false)
let dragDepth = 0
const draft = ref(props.runtime.draftText)
const input = ref<HTMLTextAreaElement | null>(null)
const busy = ref(false)
const actionInFlight = ref('')
const sending = computed(() => ['prompts', 'steer'].includes(actionInFlight.value))
const editing = ref<string | null>(null)
const editText = ref('')
const mobile=useMobileLayout()
const showQueue = ref(!mobile.value)
watch(mobile,value=>{if(value){emit('layout');showQueue.value=false}})
const settingsOpen=ref(false),settingsTarget=ref<HTMLElement|null>(null),settingsTrigger=ref<HTMLButtonElement|null>(null)
const sendOptionsOpen=ref(false),sendTrigger=ref<HTMLButtonElement|null>(null),sendImmediately=ref(false)
watch([mobile,()=>props.task.id,()=>props.task.status,()=>props.task.pendingInteraction?.id],()=>{settingsOpen.value=false;sendOptionsOpen.value=false;sendImmediately.value=false})
const notice = ref('')
const planLabels = { complete:'任务已完成', plan:'任务计划', completed:'已完成' }
const pending = computed(() => presentQuestion(props.task.pendingInteraction))
const iterationApproval = computed(() => pending.value?.kind === 'approval' && pending.value.purpose === 'iterations')
const readOnly = computed(() => !!props.task.parentTaskId || props.task.archived)
const working = computed(() => props.task.status === 'working')
const selectedModel = computed(() => props.connection?.models.find(model => model.modelId === props.task.modelId && (!props.task.modelSource || !model.source || model.source===props.task.modelSource)))
const imageIssue = computed(() => attachments.value.some(item => item.uploaded?.kind === 'image' || item.previewUrl) && !selectedModel.value?.inputModalities.includes('IMAGE') ? '当前模型不支持图片或能力未确认，请切换模型或移除图片' : '')
const canSend = computed(() => !readOnly.value && props.connected && !busy.value && !imageIssue.value && attachments.value.every(item => item.status === 'ready') && (!!draft.value.trim() || !!attachments.value.length))
const keyboardHint = computed(() => mobile.value?'回车换行，使用按钮发送':working.value ? 'Enter 排队 · Ctrl / ⌘ + Enter 立即发送' : pending.value ? 'Enter 排队 · Shift + Enter 换行' : 'Enter 发送 · Shift + Enter 换行')

function resize() {
  if (!input.value) return
  emit('layout')
  input.value.style.height = 'auto'
  input.value.style.height = `${Math.min(input.value.scrollHeight, 144)}px`
}
function updateDraft() { props.runtime.setDraftText(draft.value); resize() }
const unsubscribeDraft = props.runtime.subscribeState(() => {
  const value = props.runtime.draftText
  if (value !== draft.value) { draft.value = value; void nextTick().then(resize) }
})
onBeforeUnmount(unsubscribeDraft)
onMounted(resize)
watch(imageIssue, () => emit('layout'), { flush: 'sync' })
function addFiles(files: readonly File[], items?: DataTransferItemList) {
  if (readOnly.value || busy.value || !props.connected) return
  emit('layout'); notice.value = ''
  if (items && Array.from(items).some(item => item.kind === 'file' && item.webkitGetAsEntry?.()?.isDirectory)) { notice.value = '暂不支持文件夹，请选择文件'; return }
  notice.value = props.workspace.attachmentDrafts.add(props.task.id, files) || ''
}
function pickFiles(event: Event) { const target = event.target as HTMLInputElement; addFiles(Array.from(target.files || [])); target.value = '' }
function pasteFiles(event: ClipboardEvent) {
  const files = Array.from(event.clipboardData?.items || []).filter(item => item.kind === 'file').map(item => item.getAsFile()).filter((file): file is File => !!file)
  if (!files.length) return
  event.preventDefault()
  addFiles(files, event.clipboardData?.items)
  const text = event.clipboardData?.getData('text/plain')
  if (text && input.value) { input.value.setRangeText(text, input.value.selectionStart, input.value.selectionEnd, 'end'); draft.value = input.value.value; updateDraft() }
}
const isFileDrag = (event: DragEvent) => event.dataTransfer?.types.includes('Files')
const isWorkspaceDrag = (event: DragEvent) => event.target instanceof Element && !!event.target.closest('#enterprise-workspace-panel')
function resetDrag() { dragDepth = 0; dragActive.value = false }
function dragEnter(event: DragEvent) { if (!isFileDrag(event)) return; if (isWorkspaceDrag(event)) { resetDrag(); return }; event.preventDefault(); dragDepth++; dragActive.value = !readOnly.value && !busy.value && props.connected && !document.querySelector('dialog[open]') }
function dragOver(event: DragEvent) { if (!isFileDrag(event)) return; if (isWorkspaceDrag(event)) { resetDrag(); return }; event.preventDefault(); if (event.dataTransfer) event.dataTransfer.dropEffect = dragActive.value ? 'copy' : 'none' }
function dragLeave(event: DragEvent) { if (!isFileDrag(event) || isWorkspaceDrag(event)) return; dragDepth = Math.max(0, dragDepth - 1); if (!dragDepth || !event.relatedTarget) resetDrag() }
function dropFiles(event: DragEvent) { if (!isFileDrag(event)) return; if (isWorkspaceDrag(event)) { resetDrag(); return }; event.preventDefault(); const accept = dragActive.value; resetDrag(); if (accept && event.dataTransfer) addFiles(Array.from(event.dataTransfer.files), event.dataTransfer.items) }
onMounted(() => { document.addEventListener('dragenter', dragEnter); document.addEventListener('dragover', dragOver); document.addEventListener('dragleave', dragLeave); document.addEventListener('drop', dropFiles); window.addEventListener('dragend', resetDrag); window.addEventListener('blur', resetDrag) })
onBeforeUnmount(() => { document.removeEventListener('dragenter', dragEnter); document.removeEventListener('dragover', dragOver); document.removeEventListener('dragleave', dragLeave); document.removeEventListener('drop', dropFiles); window.removeEventListener('dragend', resetDrag); window.removeEventListener('blur', resetDrag) })

async function command(action: string, body: unknown): Promise<boolean> {
  if (busy.value || !props.connected) return false
  emit('layout'); busy.value = true; actionInFlight.value = action; notice.value = ''
  try { return await props.workspace.command(props.task.id, action, body) }
  finally { busy.value = false; actionInFlight.value = '' }
}
async function send(immediate = false) {
  if (!canSend.value || immediate && pending.value) return
  const submitted = draft.value
  const submittedAttachments = [...attachments.value]
  const queued = working.value || !!pending.value
  if (!queued) emit('sending')
  if (await command(immediate ? 'steer' : 'prompts', { prompt: submitted, attachmentIds: submittedAttachments.map(item => item.uploaded!.id) })) {
    for (const item of submittedAttachments) props.workspace.attachmentDrafts.remove(props.task.id, item.id)
    if (draft.value === submitted) { draft.value = ''; props.runtime.setDraftText(''); await nextTick(); resize() }
    sendImmediately.value=false
    if (!queued) emit('sent')
  }
}
function keydown(event: KeyboardEvent) {
  if (event.key !== 'Enter' || event.shiftKey || event.isComposing || event.keyCode === 229) return
  if (mobile.value&&!event.ctrlKey&&!event.metaKey) return
  event.preventDefault(); void send(event.ctrlKey || event.metaKey)
}
async function queueAction(action: string, entryId?: string, prompt?: string) {
  if (await command('queue', { action, entryId, prompt })) {
    editing.value = null
  }
}
async function approvalMode(event: Event) {
  const select = event.target as HTMLSelectElement
  const mode = select.value
  select.value = props.task.approvalMode || 'manual'
  if (await command('approval-mode', { mode })) notice.value = pending.value ? '当前请求仍需单独处理' : ''
}
async function selectModel(event: Event) {
  const select = event.target as HTMLSelectElement
  const modelId = select.value; select.value = props.task.modelId || ''
  await command('model', { modelId })
}
async function selectReasoning(event: Event) {
  const select = event.target as HTMLSelectElement
  const reasoningEffort = select.value; select.value = props.task.reasoningEffort || 'default'
  await command('model', { modelId: props.task.modelId, reasoningEffort })
}
async function resolve(approved: boolean) {
  const interaction = pending.value
  if (!interaction) return
  await command('interaction', interaction.kind === 'approval'
    ? { interactionId: interaction.id, kind: 'approval', approved }
    : { interactionId: interaction.id, kind: 'question', answer: null })
}
</script>

<template>
  <div class="workbench-composer">
    <div class="composer-context" @click.capture="emit('layout')">
      <section v-if="pending" class="workbench-interaction" data-testid="pending-interaction" :data-kind="pending.kind" :data-interaction-id="pending.id" :data-call-id="pending.kind === 'approval' ? pending.callId : ''">
        <div class="interaction-heading"><strong>{{ pending.title }}</strong><span>{{ iterationApproval ? '等待继续确认' : pending.kind === 'approval' ? '等待审批' : '需要回答' }}</span></div>
        <p v-if="iterationApproval">{{ pending.detail }}</p>
        <details v-else-if="pending.kind === 'approval'"><summary>查看操作详情</summary><pre>{{ pending.detail }}</pre></details>
        <QuestionForm v-else :key="pending.id" :question="pending" :disabled="busy || !connected" @submit="command('interaction', { interactionId: pending.id, kind: 'question', answer: $event })" @skip="resolve(false)" />
        <div v-if="pending.kind === 'approval'" class="interaction-actions"><button data-testid="deny-interaction" :disabled="busy || !connected" @click="resolve(false)"><span class="control-face">{{ iterationApproval ? '结束本轮' : '拒绝' }}</span></button><button class="primary" data-testid="approve-interaction" :disabled="busy || !connected" @click="resolve(true)"><span class="control-face">{{ iterationApproval ? '继续 10 步' : '允许本次' }}</span></button></div>
      </section>
      <section v-if="task.queue.length" class="message-queue" data-testid="queue-banner" aria-label="待发送消息">
        <div class="queue-heading">
          <button type="button" :aria-expanded="showQueue" @click="emit('layout'); showQueue = !showQueue"><DisclosureIcon :open="showQueue" /> 待发送 <span>{{ task.queue.length }}</span></button>
          <button v-if="task.status === 'idle'" :disabled="busy || !connected" @click="queueAction('resume')">继续队列</button>
          <small v-else>{{ task.queuePaused ? '队列已暂停，请确认投递结果后继续' : '当前任务结束后依次发送' }}</small>
        </div>
        <ol v-if="showQueue" class="queue-items">
          <li v-for="(entry, index) in task.queue" :key="entry.id" :data-queue-id="entry.id">
            <span class="queue-number">{{ index + 1 }}</span>
            <template v-if="editing === entry.id">
              <div class="queue-edit"><textarea v-model="editText" aria-label="编辑排队消息" rows="2" /><div><button :disabled="busy" @click="editing = null">取消</button><button :disabled="busy || (!editText.trim() && !entry.attachmentIds?.length) || !connected" @click="queueAction('edit', entry.id, editText)">保存</button></div></div>
            </template>
            <template v-else>
              <p :title="entry.prompt">{{ entry.prompt }}<small v-if="entry.attachmentNames?.length" class="queued-attachments">附件：{{ entry.attachmentNames.join('、') }}</small></p>
              <div class="queue-actions">
                <button type="button" :disabled="busy || !!pending || !connected" title="运行时补充到当前轮；空闲时优先发送" @click="queueAction('send', entry.id)">立即发送</button>
                <button type="button" :disabled="busy" @click="editing = entry.id; editText = entry.prompt">编辑</button>
                <button type="button" :disabled="busy || !connected" aria-label="移除排队消息" title="移除" @click="queueAction('remove', entry.id)">×</button>
              </div>
            </template>
          </li>
        </ol>
      </section>
      <p v-if="task.lastFailure" class="composer-error" role="alert" data-testid="last-turn-failure">{{ task.lastFailure.message }}</p>
      <p v-if="error" class="composer-error" role="alert">{{ error }}</p>
      <div class="run-feedback-row">
        <WorkbenchRunStatus :task="task" :connected="connected" :sending="sending" :stopping="actionInFlight === 'abort'" @layout="emit('layout')" />
        <button v-if="task.status!=='idle'&&!readOnly" type="button" class="composer-stop" data-testid="composer-stop" title="停止执行，保留排队消息" :disabled="busy||!connected" @click="command('abort',{})"><span class="control-face"><svg viewBox="0 0 20 20" aria-hidden="true"><path d="M5 5h10v10H5Z" /></svg>{{actionInFlight==='abort'?'停止中…':'停止'}}</span></button>
      </div>
      <ActivePlanStrip :plan="task.activePlan || null" :labels="planLabels" @click.capture="emit('layout')" />
    </div>
    <div v-if="!readOnly" class="workbench-input-box">
      <ComposerAttachments :items="attachments" :disabled="busy" @remove="workspace.attachmentDrafts.remove(task.id, $event)" @retry="workspace.attachmentDrafts.retry(task.id, $event)" />
      <p v-if="imageIssue" class="attachment-issue" role="status">{{ imageIssue }}</p>
      <div v-if="dragActive" class="attachment-drop" role="status">松开添加附件</div>
      <input ref="fileInput" type="file" multiple hidden data-testid="attachment-input" @change="pickFiles" />
      <textarea ref="input" v-model="draft" aria-label="消息" :aria-description="keyboardHint" rows="1" data-testid="composer-input" :placeholder="pending ? '补充消息，先加入队列…' : working ? '补充要求，或排队下一条任务…' : '向 Tuck 发送消息…'" @paste="pasteFiles" @input="updateDraft" @keydown="keydown" />
      <div class="workbench-toolbar">
        <button type="button" class="attach-button" aria-label="添加文件" title="添加文件，也可粘贴或拖放" :disabled="busy || !connected" @click="fileInput?.click()"><span class="control-face"><svg viewBox="0 0 20 20" aria-hidden="true"><path d="M10 4v12M4 10h12" /></svg></span></button>
        <Teleport :to="settingsTarget||'body'" :disabled="!mobile||!settingsTarget">
        <div class="composer-settings" :class="{'in-panel':mobile}">
          <select v-if="connection?.models.length" aria-label="模型" data-testid="task-model" :value="selectedModel?.modelId || ''" :disabled="busy || task.status !== 'idle' || !connected" @change="selectModel"><option value="" disabled>选择模型</option><option v-for="model in connection.models" :key="model.modelId" :value="model.modelId" :disabled="!model.selectable">{{ model.displayName }}{{ connection.mode==='platform' ? (model.source==='local'?' · 本地':' · 平台') : '' }}</option></select>
          <select v-if="selectedModel" aria-label="推理强度" :value="task.reasoningEffort || 'default'" :disabled="(!selectedModel.capabilities.includes('REASONING')&&(!task.reasoningEffort||task.reasoningEffort==='default')) || busy || task.status !== 'idle' || !connected" title="自动：不指定推理等级。手动选项来自当前模型声明的能力。" @change="selectReasoning"><option v-if="task.reasoningEffort&&!modelReasoningOptions(selectedModel).includes(task.reasoningEffort)" :value="task.reasoningEffort" disabled>原推理等级已不可用</option><option v-for="effort in modelReasoningOptions(selectedModel)" :key="effort" :value="effort">{{ effort==='default'?'推理：自动':reasoningLabels[effort]||effort }}</option></select>
          <select aria-label="审批模式" data-testid="approval-mode" :value="task.approvalMode || 'manual'" :disabled="busy || !connected" :title="task.approvalMode === 'auto' ? '自动执行工具；显式禁止规则仍有效' : '安全读取自动执行；写入、命令等操作请求审批'" @change="approvalMode"><option value="manual">手动审批</option><option value="auto">自动审批</option></select>
        </div>
        </Teleport>
        <button ref="settingsTrigger" type="button" class="mobile-composer-settings" aria-label="对话设置" aria-haspopup="dialog" :aria-expanded="settingsOpen" @click="settingsOpen=!settingsOpen"><span class="control-face"><svg viewBox="0 0 20 20" aria-hidden="true"><path d="M3 6h14M3 14h14M7 3v6m6 2v6" /></svg>设置<span class="mobile-approval"> · {{task.approvalMode==='auto'?'自动':'手动'}}</span></span></button>
        <div class="composer-send-actions">
          <button v-if="working&&!mobile" type="button" data-testid="task-steer" :disabled="!canSend" title="补充到当前轮（Ctrl / ⌘ + Enter）" @click="send(true)">{{ actionInFlight === 'steer' ? '发送中…' : '立即发送' }}</button>
          <button type="button" class="primary" data-testid="send-message" :disabled="!canSend" @click="send(mobile&&sendImmediately)"><span class="control-face">{{sending?'发送中…':mobile&&sendImmediately?'立即补充':working||pending?'排队':'发送'}}<svg viewBox="0 0 20 20" aria-hidden="true"><path d="M10 16V4m-5 5 5-5 5 5" /></svg></span></button>
          <button v-if="mobile&&working&&!pending" ref="sendTrigger" type="button" class="send-mode" aria-label="发送方式" aria-haspopup="dialog" :aria-expanded="sendOptionsOpen" @click="sendOptionsOpen=!sendOptionsOpen"><span class="control-face"><DisclosureIcon open /></span></button>
        </div>
      </div>
    </div>
    <div v-else-if="task.parentTaskId" class="child-session-caption"><span>{{ task.mode === 'background' ? '后台' : '前台' }}子代理 · {{ taskStatus(task) }}</span><RouterLink class="back-link" :to="{path:conversationPath(task.parentTaskId),query:route.query}">← 返回父会话</RouterLink><small>由父 Agent 管理，此处只读</small></div>
    <p v-else class="composer-caption">此对话已归档</p>
    <SummaryPopover v-model:open="settingsOpen" :trigger="settingsTrigger" above label="对话设置"><div ref="settingsTarget" class="composer-settings-panel" /><p v-if="task.status!=='idle'" class="composer-caption">执行或等待交互时不可修改模型和推理；审批模式仍可调整，当前请求需单独处理。</p></SummaryPopover>
    <SummaryPopover v-model:open="sendOptionsOpen" :trigger="sendTrigger" above label="发送方式"><div class="send-mode-options"><button type="button" :aria-pressed="!sendImmediately" @click="sendImmediately=false;sendOptionsOpen=false">排队下一轮<small>本轮结束后发送</small></button><button type="button" :aria-pressed="sendImmediately" @click="sendImmediately=true;sendOptionsOpen=false">立即补充<small>补充到当前执行轮次</small></button></div></SummaryPopover>
    <div class="composer-footer">
      <SessionSummary :task="task" :connected="connected" />
      <span v-if="!readOnly" class="composer-shortcut" :title="keyboardHint">{{ keyboardHint }}</span>
    </div>
    <p v-if="!readOnly && (!connected || notice)" class="composer-caption" role="status">{{ !connected ? '连接已断开，重连后可发送；草稿已保留' : notice }}</p>
  </div>
</template>

<style scoped>
.workbench-composer { width:100%; max-width:var(--conversation-content-width,1100px); margin:0 auto; min-width:0; font-size:13px; container:composer / inline-size; }
.composer-context { display:flex; flex-direction:column; max-height:min(calc(var(--app-visible-height,100dvh) * .34),280px); overflow:auto; scrollbar-width:thin; }
.composer-context > * { flex-shrink:0; }
button, select, input, textarea { font:inherit; box-sizing:border-box; }
button { cursor:pointer; border:1px solid var(--tuck-border,#34414e); background:var(--tuck-surface,#19222b); color:var(--tuck-accent,#dce6ef); border-radius:8px; padding:7px 11px; white-space:nowrap; }
button:hover:not(:disabled) { background:var(--tuck-raised,#283745); }
button:disabled, select:disabled { opacity:.45; cursor:default; }
button.primary { background:var(--tuck-accent,#b8ddfb); color:var(--tuck-accentText,#102132); border-color:transparent; font-weight:650; }
.workbench-input-box { position:relative; padding:6px 10px 4px; border:1px solid var(--tuck-border,#354452); border-radius:14px; background:var(--tuck-surface,#131c25); }
.attachment-drop { position:absolute; inset:0; z-index:5; display:grid; place-items:center; border:2px dashed var(--tuck-accent); border-radius:14px; background:var(--tuck-surface); color:var(--tuck-accent); pointer-events:none; }
.attachment-issue { margin:0 0 6px; font-size:var(--tuck-font-caption); color:var(--tuck-warning); }
.queued-attachments { display:block; color:var(--tuck-muted); font-size:var(--tuck-font-caption); }
.workbench-input-box:focus-within { border-color:var(--tuck-accent); }
.workbench-composer .workbench-input-box > textarea { display:block; width:100%; min-height:28px; max-height:144px; overflow-y:auto; resize:none; border:0; outline:none; box-shadow:none; padding:2px 2px 4px; color:var(--tuck-text,#edf3f9); background:transparent; font-size:var(--tuck-font-body); line-height:var(--tuck-line-body); }
.workbench-composer .workbench-input-box > textarea:focus-visible { outline:none; }
.workbench-input-box > textarea::placeholder { color:var(--tuck-muted,#80909f); }
.workbench-toolbar { display:flex; align-items:center; justify-content:space-between; flex-wrap:wrap; gap:10px 8px; }
.mobile-composer-settings{display:none}
.run-feedback-row{display:flex;align-items:center;gap:8px;min-width:0;min-height:var(--chat-auxiliary-row-height,26px)}.run-feedback-row>:deep(.run-feedback){flex:1;min-width:0}.composer-stop{flex:none;border:0;background:transparent;height:var(--chat-auxiliary-row-height,26px);padding:3px 10px}
.run-feedback-row:not(:has(.run-feedback)){display:none}
.send-mode-options{display:grid;gap:var(--summary-row-gap)}.send-mode-options button{text-align:left;min-height:52px}.send-mode-options button[aria-pressed=true]{border-color:var(--tuck-accent)}.send-mode-options small{display:block;color:var(--tuck-muted);margin-top:4px}
.composer-settings, .composer-send-actions { display:flex; min-width:0; gap:6px; align-items:center; }
.composer-send-actions { margin-left:auto; }
.composer-settings { flex:1 1 260px; flex-wrap:wrap; }
.composer-settings select { min-width:0; max-width:190px; height:30px; padding:4px 3px; border:0; border-radius:6px; color:var(--tuck-text); background:var(--tuck-surface); font-size:var(--tuck-font-caption); text-overflow:ellipsis; }
.composer-settings.in-panel { display:grid; flex:none; gap:2px; }
.composer-settings.in-panel select { width:100%; max-width:none; height:44px; padding:8px; font-size:var(--tuck-font-ui); }
.composer-settings select[aria-label="模型"] { flex:0 1 auto; }
.composer-settings select[aria-label="审批模式"] { flex-shrink:0; }
.workbench-composer .attach-button { border:0; background:transparent; color:var(--tuck-muted); font-size:20px; width:30px; height:30px; padding:0;flex:none; }
.workbench-composer .composer-send-actions > button { min-height:30px; padding:5px 10px; border:0; background:transparent; color:var(--tuck-text); }
.composer-footer { display:flex; align-items:center; flex-wrap:wrap; gap:0 12px; margin-top:5px; min-width:0; }
.child-session-caption { display:flex; align-items:center; gap:8px 12px; flex-wrap:wrap; color:var(--tuck-text); font-size:12px; }
.child-session-caption small { color:var(--tuck-muted); }
.composer-shortcut { margin-left:auto; max-width:100%; box-sizing:border-box; padding:4px 2px; color:var(--tuck-muted); font-size:var(--tuck-font-caption); line-height:20px; white-space:nowrap; overflow:hidden; text-overflow:ellipsis; }
.composer-caption { min-height:18px; display:flex; gap:8px; justify-content:space-between; margin:7px 4px 0; color:var(--tuck-muted,#80909f); font-size:var(--tuck-font-caption); line-height:1.5; }
.message-queue { margin-bottom:2px; border:1px solid var(--tuck-raised,#2b3a47); border-radius:12px; background:var(--tuck-surface,#101821); overflow:hidden; }
.queue-heading { display:flex; align-items:center; justify-content:space-between; gap:8px; min-height:calc(var(--chat-auxiliary-row-height,26px) - 2px); padding:0 8px; }
.queue-heading button { display:inline-flex; align-items:center; gap:4px; min-height:24px; border:0; background:none; padding:2px 4px; }
.queue-heading small { color:var(--tuck-muted,#80909f); font-size:var(--tuck-font-caption); }
.queue-heading span { margin-left:5px; color:var(--tuck-accent,#91caff); }
.queue-items { list-style:none; margin:0; padding:0 10px 5px; max-height:160px; overflow:auto; }
.queue-items li { display:flex; align-items:center; gap:8px; border-top:1px solid var(--tuck-raised,#263440); padding:8px 0; }
.queue-number { color:var(--tuck-muted,#708395); font-size:var(--tuck-font-caption); }
.queue-items p { flex:1; min-width:0; margin:0; white-space:pre-wrap; overflow-wrap:anywhere; display:-webkit-box; -webkit-line-clamp:2; -webkit-box-orient:vertical; overflow:hidden; line-height:1.6; }
.queue-actions { display:flex; flex-shrink:0; gap:4px; }
.queue-actions button { border:0; background:none; padding:5px; font-size:12px; color:var(--tuck-accent,#a8cbe7); }
.queue-edit { flex:1; min-width:0; }
.queue-edit textarea { width:100%; resize:vertical; min-height:64px; max-height:120px; background:var(--tuck-background,#0c141b); color:var(--tuck-text,#e4eef8); border:1px solid var(--tuck-border,#40536a); border-radius:6px; padding:8px; }
.queue-edit > div { display:flex; justify-content:flex-end; gap:6px; margin-top:5px; }
.workbench-interaction { margin:0 0 10px; padding:12px; background:var(--tuck-surface,#272319); border:1px solid var(--tuck-border,#665233); border-radius:12px; }
.interaction-heading { display:flex; justify-content:space-between; gap:12px; }
.interaction-heading strong { overflow-wrap:anywhere; }
.interaction-heading span { font-size:var(--tuck-font-caption); color:var(--tuck-warning,#d6b77b); flex-shrink:0; }
.workbench-interaction summary { cursor:pointer; margin-top:8px; color:var(--tuck-text,#c3b79e); font-size:12px; }
.workbench-interaction pre { max-height:140px; overflow:auto; white-space:pre-wrap; overflow-wrap:anywhere; font-size:12px; line-height:1.5; }
.workbench-interaction p { white-space:pre-wrap; overflow-wrap:anywhere; }
.interaction-actions { display:flex; justify-content:flex-end; gap:8px; margin-top:6px; }
.interaction-actions button{display:inline-flex;align-items:center;justify-content:center;min-height:30px;padding:0;border:0;background:transparent}
.interaction-actions .control-face{min-height:30px;padding:5px 8px}
.interaction-actions button:not(.primary) .control-face{box-shadow:inset 0 0 0 1px var(--tuck-border)}
.workbench-interaction[data-kind=approval]{display:flex;flex-wrap:wrap;align-items:center;gap:4px 8px;padding:8px 10px;margin-bottom:4px}
.workbench-interaction[data-kind=approval]>.interaction-heading,.workbench-interaction[data-kind=approval]>p{flex-basis:100%}
.workbench-interaction[data-kind=approval]>details{flex:1 1 auto;min-width:0}
.workbench-interaction[data-kind=approval]>details[open]{flex-basis:100%}
.workbench-interaction[data-kind=approval] summary{margin:0;min-height:30px;align-content:center}
.workbench-interaction[data-kind=approval]>.interaction-actions{margin:0 0 0 auto;flex:none}
.workbench-interaction input { width:100%; padding:8px; background:var(--tuck-surface,#151d24); color:var(--tuck-text,#fff); border:1px solid var(--tuck-muted,#74623d); border-radius:6px; }
.composer-error { color:var(--tuck-danger,#ffada6); white-space:pre-wrap; overflow-wrap:anywhere; padding:6px 8px; margin:0 0 4px; border:1px solid var(--tuck-border,#693833); border-radius:8px; }
@container composer (max-width:480px) { .workbench-input-box { padding:6px 8px; } .queue-items li { flex-wrap:wrap; } .queue-items p { flex-basis:calc(100% - 28px); } .queue-actions { margin-left:auto; } .composer-settings { flex-basis:auto; } .composer-settings:not(.in-panel) select { max-width:160px; } .composer-caption { flex-wrap:wrap; } }
@media(max-width:760px){
  .workbench-composer .workbench-input-box>textarea{font-size:16px;min-height:30px}
  .workbench-toolbar{flex-wrap:nowrap;gap:4px}
  .mobile-composer-settings{display:block;height:44px;padding:0 8px;border:0;background:transparent;font-size:12px;flex:none}
  .workbench-composer .attach-button{width:44px;height:44px}
  .workbench-composer .composer-send-actions>button{height:44px;min-width:44px;padding:5px 10px}
  .composer-send-actions{gap:0;flex:none}.composer-send-actions>.send-mode{padding-inline:0}
  .composer-shortcut{display:none}.composer-footer{margin-top:0}
  .run-feedback-row{min-height:32px;margin-bottom:2px}.composer-stop{height:32px;min-width:54px}
  .queue-heading small{display:none}.queue-actions button,.interaction-actions button{min-height:44px}
}
@media(max-width:350px){.mobile-composer-settings .mobile-approval{display:none}}

/* Compact painted faces preserve the enclosing touch target. */
.control-face{display:inline-flex;align-items:center;justify-content:center;gap:4px;min-height:26px;padding:3px 6px;box-sizing:border-box;border-radius:6px;line-height:20px}
.control-face svg{width:14px;height:14px;flex:none;fill:none;stroke:currentColor;stroke-width:1.7;stroke-linecap:round;stroke-linejoin:round}
.workbench-composer .attach-button,.workbench-composer .composer-stop,.workbench-composer .mobile-composer-settings,.workbench-composer .composer-send-actions>button:has(.control-face){padding:0;background:transparent}
button:has(.control-face){align-items:center;justify-content:center;vertical-align:middle}
.workbench-composer .attach-button,.workbench-composer .composer-stop,.workbench-composer .composer-send-actions>button:has(.control-face){display:inline-flex}
.workbench-composer button:has(.control-face):hover:not(:disabled){background:transparent;filter:none}
button:hover:not(:disabled) > .control-face{background:var(--tuck-hover)}
.workbench-composer button.primary .control-face{background:var(--tuck-accent);color:var(--tuck-accentText);min-height:30px;padding:5px 8px}
.workbench-composer button.primary:hover:not(:disabled) .control-face{filter:brightness(.95)}
@media(max-width:760px){.workbench-composer .composer-send-actions>button.primary:focus-visible{outline-color:var(--tuck-accent)}}
@media(forced-colors:active){.workbench-composer .composer-send-actions>button.primary:focus-visible{outline-color:Highlight}}
/* Product controls and Engine plan slot presentation. */
.workbench-input-box > textarea { font-family:var(--tuck-font-family); }
button, select { font-size:var(--tuck-font-ui); }
select, option { color-scheme:var(--tuck-scheme); background:var(--tuck-surface); color:var(--tuck-text); }
:deep(.active-plan-strip) { position:relative; z-index:auto; top:auto; left:auto; transform:none; width:100%; margin-bottom:2px; }
:deep(.active-plan-strip > summary) { height:var(--chat-auxiliary-row-height,26px); border:1px solid var(--tuck-border); border-radius:6px; background:var(--tuck-surface); box-shadow:none; }
:deep(.active-plan-popover) { position:static; margin-top:4px; max-height:160px; box-shadow:none; border-radius:6px; }
:deep(.active-plan-strip:not([open]):hover .active-plan-popover) { display:none; }
</style>
