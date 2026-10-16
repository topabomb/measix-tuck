<script setup lang="ts">
import {computed} from 'vue'
import {workspaceModes,workspaceModeLabels,workspacePolicy,type WorkspaceMode} from '../../../../shared/workspace-mode.mjs'
import type {Assistant} from './types'
const props=defineProps<{assistant:Assistant}>()
const policy=computed(()=>workspacePolicy(props.assistant))
function edit(){return props.assistant.workspace ||= structuredClone(policy.value)}
</script>
<template>
 <section class="agent-workspace" aria-label="工作目录配置">
  <h3>工作目录</h3>
  <div class="form-grid">
   <label>工作目录模式<select :value="policy.mode" @change="edit().mode=($event.target as HTMLSelectElement).value as WorkspaceMode"><option v-for="mode in workspaceModes" :key="mode" :value="mode">{{ workspaceModeLabels[mode] }}</option></select></label>
  </div>
  <label v-if="policy.mode==='specified'">指定目录<input aria-label="指定目录" :value="policy.directory||''" placeholder="已有目录的绝对路径" @input="edit().directory=($event.target as HTMLInputElement).value" /><small>本机已有目录；Docker 部署填写容器内路径。使用此目录的会话共享文件。</small></label>
  <p class="hint">自动目录为每个新会话创建独立目录。未指定目录沿用 Docker Agent 的默认目录行为，工具与审批配置保持不变。设置仅影响新建会话，子代理继承所在会话目录。</p>
 </section>
</template>
<style scoped>.agent-workspace{border-top:1px solid var(--tuck-border);padding-top:12px;margin-top:12px}.agent-workspace h3{font-size:14px;margin:0 0 10px}</style>
