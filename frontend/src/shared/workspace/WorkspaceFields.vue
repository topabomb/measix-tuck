<script setup lang="ts">
import {workspaceModes,workspaceModeLabels,type WorkspaceMode} from '../../../shared/workspace-mode.mjs'
const mode=defineModel<WorkspaceMode>('mode',{required:true})
const directory=defineModel<string>('directory',{required:true})
</script>
<template>
 <div class="workspace-fields">
  <label>工作目录模式<select v-model="mode"><option v-for="item in workspaceModes" :key="item" :value="item">{{ workspaceModeLabels[item] }}</option></select></label>
  <label v-if="mode==='specified'">工作目录<input v-model="directory" aria-label="工作目录" required placeholder="本机已有目录的绝对路径" /><small>可指定任意本机已有且可读写的目录；Docker 部署填写容器内路径。相同目录的会话共享文件。</small></label>
  <small v-else-if="mode==='none'">沿用 Docker Agent 的默认目录行为；多个会话可能共享目录。工具与审批配置保持不变。</small>
  <small v-else>为此会话创建独立工作目录。</small>
 </div>
</template>
<style scoped>.workspace-fields{display:grid;gap:6px;margin:10px 0}.workspace-fields label{display:grid;gap:5px}.workspace-fields select,.workspace-fields input{width:100%;min-width:0;box-sizing:border-box}.workspace-fields small{font-size:12px;color:var(--tuck-muted);overflow-wrap:anywhere;line-height:1.5}</style>
