<script setup lang="ts">
import { ref, computed } from 'vue'
import { isChannelTask, parseTaskTitle } from '../../../../shared/task-channel.mjs'
import type { ConversationSessionRuntime, SessionUiSnapshot } from '../../../engine/runtime/session-runtime'
import type { DockerExecutionController, DockerTask, RuntimeInfo } from '../../../shared/workspace/runtime'
const props = defineProps<{ runtime: ConversationSessionRuntime; uiState: SessionUiSnapshot; stream: DockerExecutionController; task?: DockerTask; runtimeInfo: RuntimeInfo; connected: boolean; mountedRows: number; hotSessionCount: number; runningSessionCount: number; blockedSessionCount: number; failedSessionCount: number; fps: number; frameP95: number; longTasks: number; heapMb: number | null }>()
const emit = defineEmits<{ close: []; jump: [index: number]; shiftBackward: []; shiftForward: [] }>()
const target = ref(0)
const channel = computed(() => props.task ? isChannelTask(props.task) ? '任务频道' : props.task.parentTaskId ? '子代理会话' : '对话频道' : '—')
const assistant = computed(() => props.runtimeInfo.assistants?.items.find(item => item.agent === props.task?.agent)?.displayName || props.task?.assistantName || props.task?.agent || '—')
const created = computed(() => {
  const time = Date.parse(props.task?.createdAt || '')
  return Number.isFinite(time) ? new Date(time).toLocaleString('zh-CN',{hour12:false}) : '—'
})
</script>
<template>
  <aside class="diagnostics-panel product-diagnostics" aria-label="运行详情">
    <header class="diagnostics-head"><strong>运行详情</strong><button class="icon-button" aria-label="Close diagnostics" @click="emit('close')">×</button></header>
    <div class="diagnostics-body">
    <section v-if="task" aria-label="对话信息"><h3>对话信息</h3><dl>
      <dt>所属频道</dt><dd>{{ channel }}</dd>
      <dt>助手</dt><dd>{{ assistant }}</dd>
      <dt>创建时间</dt><dd>{{ created }}</dd>
      <dt v-if="isChannelTask(task)">任务标识</dt><dd v-if="isChannelTask(task)" class="identifier">T-{{ parseTaskTitle(task.title)?.code }}</dd>
      <dt class="full-row">原始标题</dt><dd class="original-title full-row">{{ task.title }}</dd>
    </dl></section>
    <section><h3>Docker Agent</h3><dl><dt>运行版本</dt><dd>{{ runtimeInfo.version }}</dd><dt>执行引擎</dt><dd data-testid="playback-mode">{{ stream.mode }}</dd><dt>事件连接</dt><dd>{{ connected ? '已连接' : '正在重连' }}</dd><dt>Agent</dt><dd>{{ task?.agent }}</dd><dt>模型</dt><dd>{{ task?.modelName || '原生配置' }}</dd><dt>推理请求</dt><dd>{{ task?.reasoningEffort || 'default' }}</dd><dt>审批</dt><dd>{{ task?.approvalMode === 'auto' ? '自动' : '手动' }}</dd><dt>原生会话 ID</dt><dd class="identifier">{{ task?.nativeSessionId }}</dd><dt>工作台会话 ID</dt><dd class="identifier" data-testid="active-session-id">{{ runtime.id }}</dd></dl><p class="directory">{{ task?.workspace }}</p></section>
    <section><h3>执行状态</h3><dl><dt>最近结果</dt><dd data-testid="last-turn-reason">{{ runtime.kernel.lastTurnReason || 'none' }}</dd><dt>失败代码</dt><dd data-testid="last-failure-code">{{ runtime.kernel.lastFailure?.code || '—' }}</dd><dt>排队消息</dt><dd data-testid="queued-prompts">{{ uiState.queuedPrompts }}</dd><dt>工作中 / 等待处理</dt><dd>{{ runningSessionCount }} / {{ blockedSessionCount }}</dd><dt>本次连接收到的事件</dt><dd data-testid="stream-ingress">{{ stream.ingressTicks }}</dd><dt>消息更新</dt><dd data-testid="stream-ticks">{{ stream.publishTicks }}</dd></dl><p v-if="runtime.kernel.lastFailure" class="failure">{{ runtime.kernel.lastFailure.message }}</p><p v-if="task?.parentTaskId">由父 Agent 管理 · {{ task.mode === 'background' ? '后台委派' : '前台委派' }}</p><p v-if="task?.forkedFrom">分支来源：{{ task.forkedFrom.title }}，共享工作目录。</p></section>
    <section v-if="task?.timings"><h3>计时采样</h3><dl><dt>输出样本</dt><dd>{{ task.timings.streamSamples || 0 }} 次响应</dd><dt>工具样本</dt><dd>{{ task.timings.toolSamples || 0 }} 次工具</dd></dl></section>
    <section><h3>历史定位</h3><form class="inline-control" @submit.prevent="emit('jump', target)"><input v-model.number="target" aria-label="消息序号" data-testid="jump-input" type="number" min="0" :max="Math.max(0,runtime.logicalCount-1)" /><button data-testid="jump-button" :disabled="!runtime.logicalCount">跳转</button></form><div class="inline-control"><button data-testid="prepend-button" @click="emit('shiftBackward')">更早</button><button @click="emit('shiftForward')">更晚</button></div></section>
    <section><h3>渲染诊断</h3><dl><dt>历史消息</dt><dd data-testid="logical-count">{{ runtime.logicalCount.toLocaleString() }}</dd><dt>当前窗口</dt><dd>{{ uiState.rangeStart }}–{{ Math.max(0,uiState.rangeEnd-1) }}</dd><dt>挂载行 / 上限</dt><dd><span data-testid="mounted-rows">{{ mountedRows }}</span> / 180</dd><dt>投影缓存 / 上限</dt><dd><span data-testid="projection-cache">{{ uiState.projectionCacheSize }}</span> / 4096</dd><dt>热视口 / 上限</dt><dd><span data-testid="hot-sessions">{{ hotSessionCount }}</span> / 3</dd><dt>完整投影</dt><dd data-testid="projection-full-projects">{{ uiState.projectionFullProjects }}</dd><dt>增量补丁</dt><dd data-testid="projection-incremental">{{ uiState.projectionIncrementalPatches }}</dd><dt>帧耗时 p95</dt><dd>{{ frameP95 }} ms</dd><dt>长任务</dt><dd>{{ longTasks }}</dd><dt>JS 内存</dt><dd>{{ heapMb === null ? '浏览器未提供' : `${heapMb} MB` }}</dd></dl><p>性能采样仅在此面板打开期间运行。</p></section>
    </div>
  </aside>
</template>
<style scoped>
.diagnostics-panel.product-diagnostics { display:flex; flex-direction:column; padding:0; overflow:hidden; color:var(--tuck-text); background:var(--tuck-surface); font-size:var(--tuck-font-ui); }
.diagnostics-panel { min-width:0; min-height:0; border-left:1px solid var(--tuck-border); }
.diagnostics-head { display:flex; align-items:center; justify-content:space-between; gap:10px; }
.diagnostics-head strong { font-size:var(--tuck-font-ui); }
.diagnostics-head .icon-button { display:grid; place-items:center; width:28px; height:28px; padding:0; border:0; background:transparent; color:var(--tuck-muted); cursor:pointer; }
.diagnostics-head .icon-button:hover { background:var(--tuck-hover); }
.inline-control { display:flex; flex-wrap:wrap; gap:6px; margin-bottom:6px; }
.inline-control input { min-width:0; width:100%; flex:1 1 120px; }
.product-diagnostics .diagnostics-head { position:static; flex:none; align-items:center; margin:0; height:var(--workbench-header-height,48px); box-sizing:border-box; padding:0 12px; border-bottom:1px solid var(--tuck-border); background:var(--tuck-surface); }
.diagnostics-body { flex:1; min-height:0; overflow:auto; overscroll-behavior:contain; }
.product-diagnostics section { padding:12px; border-bottom:1px solid var(--tuck-border); }
h3 { margin:0 0 10px; font-size:var(--tuck-font-ui); font-weight:650; } dl { margin:0; display:grid; grid-template-columns:minmax(0,1fr) minmax(0,1.1fr); gap:8px; } dt,p { color:var(--tuck-muted); } dd { margin:0; text-align:right; overflow-wrap:anywhere; } .full-row { grid-column:1/-1; } .original-title { text-align:left; white-space:pre-wrap; } .identifier { font:12px/1.5 ui-monospace,monospace; } p { font-size:var(--tuck-font-caption); line-height:1.6; margin:10px 0 0; overflow-wrap:anywhere; }.failure { color:var(--tuck-danger); }
input,button { color:var(--tuck-text); background:var(--tuck-raised); border:1px solid var(--tuck-border); border-radius:6px; padding:6px 9px; font:inherit; }
</style>
