<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import type { DockerTask } from '../../shared/workspace/runtime'
import { useDelivery } from '../../shared/delivery/use-delivery'
import TuckLoading from '../../shared/ui/TuckLoading.vue'
import {turnHasEnded} from '../../../shared/task-channel.mjs'
import { taskPath, taskLink } from './task-view'
const route=useRoute(),router=useRouter()
const props=defineProps<{task:DockerTask;compact?:boolean;preview?:boolean}>()
const {delivery,versions,hasMoreVersions,loading,error,missing,isPreviousTurn,isHistorical,requestedVersion,files,fileUrl,html,refresh,moreVersions,choose,size}=useDelivery(()=>props.task,()=>!!(props.compact||props.preview))
const search=computed({get:()=>!props.compact&&!props.preview&&typeof route.query.fileSearch==='string'?route.query.fileSearch:'',set:value=>{void router.replace({path:route.path,query:{...route.query,fileSearch:value||undefined}})}})
const matching=computed(()=>files.value.filter(f=>f.path.toLowerCase().includes(search.value.toLowerCase())))
const visible=computed(()=>props.compact?matching.value.slice(0,5):matching.value)
const excerpt=computed(()=>new DOMParser().parseFromString(html.value,'text/html').body.textContent?.replace(/\s+/g,' ')||'')
const deliveryState=computed(()=>isPreviousTurn.value?'previous':turnHasEnded(props.task)?'current-ended':'current-active')
const deliveryLabel=computed(()=>isPreviousTurn.value?'上次交付':'本轮交付')
</script>
<template>
  <div class="task-result" :class="{'task-result-preview':preview}" :role="preview?'group':undefined" :aria-label="preview?'交付概况':undefined" :data-delivery-state="delivery?deliveryState:undefined">
    <template v-if="preview"><strong v-if="delivery" class="task-file-count">{{ deliveryLabel }} · V{{ delivery.version }} · {{ delivery.fileCount }} 个文件</strong><span v-if="delivery" class="task-result-excerpt">{{ excerpt }}</span><span v-else-if="error">{{ error }}</span><span v-else-if="!loading">尚未提交交付</span></template>
    <template v-else>
      <div v-if="loading" class="channel-loading"><TuckLoading label="正在读取交付…" /></div>
      <div v-if="!compact" class="delivery-toolbar">
        <h2>交付结果</h2>
        <div class="delivery-controls"><label v-if="delivery">版本 <select aria-label="交付版本" :value="requestedVersion==='latest'?'':requestedVersion" @change="choose"><option value="">最新版本</option><option v-if="!versions.some(v=>v.version===delivery?.version)" :value="String(delivery.version)">V{{ delivery.version }}</option><option v-for="version in versions" :key="version.version" :value="String(version.version)">V{{ version.version }}{{ version.version===task.latestDelivery?.version?'（最新）':'' }}</option></select></label><button v-if="hasMoreVersions" :disabled="loading" @click="moreVersions">加载更早版本</button><button :disabled="loading" @click="refresh">刷新交付</button></div>
      </div>
      <p v-if="error" class="task-error" role="alert">{{ error }}</p>
      <p v-if="missing" class="task-empty">{{ requestedVersion==='latest'?'尚未提交交付，可从对话或工作目录查看已有内容。':'此交付版本不存在或已按保留规则清理。' }}</p>
      <template v-if="delivery">
        <div class="delivery-version"><strong>V{{ delivery.version }} · {{ isHistorical?'历史版本':isPreviousTurn?'上次交付':'本轮交付' }}</strong><time :datetime="new Date(delivery.createdAt).toISOString()">{{ new Date(delivery.createdAt).toLocaleString() }}</time><span v-if="compact">{{ files.length }} 个文件</span></div>
        <p v-if="!compact && isPreviousTurn && !isHistorical" class="delivery-notice" role="status">本轮尚未提交交付，以下为上次交付。</p>
        <p v-if="compact" class="task-previous-excerpt">{{ excerpt }}</p><article v-else class="task-conclusion markdown-body" aria-label="交付摘要" v-html="html" />
        <h3>交付文件 <small>{{ files.length }}</small></h3>
        <input v-if="!compact&&(files.length>8||search)" v-model="search" type="search" aria-label="搜索文件" placeholder="搜索文件" />
        <ul v-if="visible.length" class="delivery-files" aria-label="交付文件"><li v-for="file in visible" :key="file.id" :aria-label="file.path"><span>{{ file.path }}<small v-if="file.description" class="file-description">{{ file.description }}</small></span><small v-if="!compact">{{ size(file.sizeBytes) }}</small><a class="task-button" :href="fileUrl(file)" :download="file.path" :aria-label="`下载 ${file.path}`">下载</a></li></ul>
        <p v-else class="task-empty">{{ search?'没有匹配的文件':'无交付文件。' }}</p>
        <RouterLink v-if="compact" class="task-button task-previous-link" :to="taskLink(`${taskPath(task.id)}/delivery`,route,'supplement')">查看上次交付</RouterLink>
      </template>
    </template>
  </div>
</template>
<style scoped>
.delivery-toolbar,.delivery-controls,.delivery-version{display:flex;align-items:center;gap:8px 16px;flex-wrap:wrap}
.delivery-toolbar{justify-content:space-between;margin:16px 0 12px;padding-top:16px;border-top:1px solid var(--tuck-border)}
.delivery-controls{margin-left:auto;gap:8px}.delivery-controls label{display:flex;align-items:center;gap:8px;color:var(--tuck-muted);font-size:var(--tuck-font-ui)}
.delivery-version,.delivery-notice{color:var(--tuck-muted);font-size:var(--tuck-font-caption)}.delivery-version{margin-bottom:16px}.delivery-version strong{color:var(--tuck-text)}
.file-description{display:block;color:var(--tuck-muted);margin-top:4px}
</style>
