<script setup lang="ts">
import type { DockerTask } from '../../shared/workspace/runtime'
import { useDelivery } from '../../shared/delivery/use-delivery'
import { conversationPath } from '../../app/router'
import { taskName } from '../../../shared/task-channel.mjs'
import DeliveryAnswer from '../../shared/delivery/DeliveryAnswer.vue'
import TuckLoading from '../../shared/ui/TuckLoading.vue'
const props=defineProps<{task:DockerTask}>()
const {delivery,versions,hasMoreVersions,loading,error,missing,isPreviousTurn,isHistorical,requestedVersion,files,fileUrl,html,refresh,moreVersions,choose,size}=useDelivery(()=>props.task)
</script>
<template>
  <main class="chat-delivery" aria-label="对话交付">
    <div class="chat-delivery-content">
      <header><RouterLink class="back-link delivery-back" :to="conversationPath(task.id)">← 返回对话</RouterLink><h1>交付</h1><span class="conversation-title" data-testid="conversation-title">对话：{{ taskName(task.title) }}</span></header>
      <DeliveryAnswer :task="task" />
      <h2 class="summary-heading">交付结果</h2>
      <div class="delivery-controls">
        <label v-if="delivery">版本 <select aria-label="交付版本" :value="requestedVersion==='latest'?'':requestedVersion" @change="choose"><option value="">最新版本</option><option v-if="!versions.some(v=>v.version===delivery?.version)" :value="String(delivery.version)">V{{ delivery.version }}</option><option v-for="version in versions" :key="version.version" :value="String(version.version)">V{{ version.version }}{{ version.version===task.latestDelivery?.version?'（最新）':'' }}</option></select></label>
        <button v-if="hasMoreVersions" :disabled="loading" @click="moreVersions">加载更早版本</button><button :disabled="loading" @click="refresh">刷新交付</button>
      </div>
      <TuckLoading v-if="loading" label="正在读取交付…" />
      <p v-if="error" role="alert">{{ error }}</p>
      <p v-if="missing">{{ requestedVersion==='latest'?'此对话尚未提交交付。':'此交付版本不存在或已按保留规则清理。' }}</p>
      <template v-if="delivery">
        <div class="delivery-version"><strong>V{{ delivery.version }} · {{ isHistorical?'历史版本':isPreviousTurn?'上次交付':'本轮交付' }}</strong><time :datetime="new Date(delivery.createdAt).toISOString()">{{ new Date(delivery.createdAt).toLocaleString() }}</time></div>
        <p v-if="isPreviousTurn&&!isHistorical" class="delivery-notice" role="status">本轮尚未提交交付，以下为上次交付。</p>
        <article class="delivery-summary markdown-body" aria-label="交付摘要" v-html="html" />
        <section class="delivery-attachments"><h2>交付文件 <small>{{ files.length }}</small></h2>
          <ul v-if="files.length" class="delivery-files" aria-label="交付文件"><li v-for="file in files" :key="file.id"><div class="file-info"><div class="file-name"><strong>{{ file.path }}</strong><small>{{ size(file.sizeBytes) }}</small></div><p v-if="file.description">{{ file.description }}</p></div><a :href="fileUrl(file)" :download="file.path" :aria-label="`下载 ${file.path}`">下载</a></li></ul>
          <p v-else>无交付文件。</p>
        </section>
      </template>
    </div>
  </main>
</template>
<style scoped>
.chat-delivery{height:100%;overflow:auto;box-sizing:border-box;padding:16px 20px;background:var(--tuck-background);color:var(--tuck-text);font:var(--tuck-font-ui)/var(--tuck-line-ui) var(--tuck-font-family)}
.chat-delivery-content{max-width:920px;margin:0 auto;min-width:0}header{display:flex;align-items:baseline;flex-wrap:wrap;gap:8px 16px;margin-bottom:12px}.delivery-back{flex:none}h1{min-width:0;margin:0;font-size:var(--tuck-font-title);line-height:var(--tuck-line-title);font-weight:600;overflow-wrap:anywhere}.conversation-title{min-width:0;font-size:var(--tuck-font-ui);color:var(--tuck-muted);overflow-wrap:anywhere}h2{margin:0 0 4px;font-size:var(--tuck-font-ui);line-height:var(--tuck-line-ui);font-weight:600}h2 small{font-size:var(--tuck-font-caption);font-weight:400;color:var(--tuck-muted);margin-left:6px}
.delivery-controls,.delivery-version{display:flex;align-items:center;flex-wrap:wrap;gap:6px 12px;margin-bottom:8px}.delivery-controls label{display:flex;align-items:center;gap:6px}.delivery-version{color:var(--tuck-muted);font-size:var(--tuck-font-caption)}.delivery-version strong{font-weight:500}.delivery-notice{margin:6px 0;color:var(--tuck-muted);font-size:var(--tuck-font-caption)}
a{color:var(--tuck-accent);text-decoration:none}a:hover{text-decoration:underline}button,select,.delivery-files a{box-sizing:border-box;border:1px solid var(--tuck-border);border-radius:6px;padding:3px 8px;min-height:28px;background:var(--tuck-surface);color:var(--tuck-text);font:inherit}button{cursor:pointer}button:disabled{opacity:.5}button:hover:not(:disabled),.delivery-files a:hover{background:var(--tuck-hover)}.delivery-summary{font:var(--tuck-font-body)/var(--tuck-line-body) var(--tuck-font-family);overflow-wrap:anywhere;margin:12px 0}.delivery-summary :deep(p){margin:6px 0}.delivery-summary :deep(h1),.delivery-summary :deep(h2),.delivery-summary :deep(h3){font-size:var(--tuck-font-title);line-height:var(--tuck-line-title);margin:12px 0 6px}.delivery-summary :deep(pre){overflow:auto;max-width:100%}.delivery-summary :deep(img){max-width:100%}.delivery-summary :deep(.delivery-table-scroll){max-width:100%;overflow:auto}
.delivery-attachments{border-top:1px solid var(--tuck-border);padding-top:10px}.delivery-files{list-style:none;padding:0;margin:0}.delivery-files li{display:flex;align-items:center;gap:12px;padding:8px 0;border-bottom:1px solid var(--tuck-border)}.file-info{flex:1;min-width:0;overflow-wrap:anywhere}.file-name{display:flex;align-items:baseline;gap:8px;flex-wrap:wrap}.file-info strong{font-weight:500}.file-info p{color:var(--tuck-muted);margin:2px 0 0;font-size:var(--tuck-font-caption);line-height:var(--tuck-line-caption)}.file-info small{color:var(--tuck-muted);font-size:var(--tuck-font-caption)}.delivery-files a{flex:none}[role=alert]{color:var(--tuck-danger)}
@media(max-width:600px){.chat-delivery{padding:12px}h1{font-size:var(--tuck-font-body)}}
.summary-heading{margin-top:16px;padding-top:12px;border-top:1px solid var(--tuck-border)}
</style>
