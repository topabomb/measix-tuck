<script setup lang="ts">
import {computed} from 'vue'
import {useRoute} from 'vue-router'
import type {RenderUnit} from '../../../engine/presentation/render-unit'
import {conversationPath} from '../../../app/router'
import {renderMarkdownCached} from '../../../engine/vue/renderers/markdown-cache'
const props=defineProps<{unit:RenderUnit}>()
const route=useRoute()
function read(value:unknown,depth=0):Record<string,unknown>|null {
  if(depth>4)return null
  if(typeof value==='string'){try{return read(JSON.parse(value),depth+1)}catch{return null}}
  if(!value||typeof value!=='object')return null
  const data=value as Record<string,unknown>
  if(data.status==='completed'||data.status==='failed')return data
  for(const nested of [data.structuredContent,data.output]){const parsed=read(nested,depth+1);if(parsed)return parsed}
  if(Array.isArray(data.content))for(const item of data.content){const parsed=read(item?.text,depth+1);if(parsed)return parsed}
  return null
}
const output=computed(()=>read(props.unit.payload.output))
const saved=computed(()=>props.unit.payload.status!=='error'&&output.value?.status==='completed'&&Number.isSafeInteger(output.value.version)&&Number(output.value.version)>0)
const failed=computed(()=>props.unit.payload.status==='error'||output.value?.status==='failed')
const excerpt=computed(()=>{
  const raw=String(props.unit.payload.deliverySummary||output.value?.summary||'')
  const html=renderMarkdownCached('delivery-activity:'+props.unit.id,props.unit.revision,raw.slice(0,20000))
  const text=new DOMParser().parseFromString(html,'text/html').body.textContent?.replace(/\s+/g,' ').trim()||''
  return text.length>240?text.slice(0,240)+'…':text
})
const sourceId=computed(()=>{
  const value=output.value?.view_url
  const match=typeof value==='string'?/^#\/chat\/conversations\/([A-Za-z0-9_-]+)\/delivery\?version=([1-9]\d*)$/.exec(value):null
  return match&&Number(match[2])===output.value?.version?match[1]:''
})
const target=computed(()=>`${conversationPath(sourceId.value)}/delivery?version=${output.value?.version}`)
const failure=computed(()=>String(output.value?.message||(failed.value?'交付未成功，请查看工具返回的详情。':'无法确认交付结果，请查看工具返回的详情。')))
const rawOutput=computed(()=>typeof props.unit.payload.output==='string'?props.unit.payload.output:JSON.stringify(props.unit.payload.output??{},null,2))
</script>
<template>
  <article class="delivery-activity" :class="{failed:!saved}" data-testid="delivery-activity" :data-call-id="unit.payload.callId" aria-label="交付记录">
    <header><strong>{{ saved?'已提交交付':failed?'交付失败':'交付返回结果' }}</strong><span v-if="saved">V{{ output?.version }} · {{ Number(output?.file_count)||0 }} 个文件</span></header>
    <p v-if="saved&&excerpt">{{ excerpt }}</p><p v-else-if="!saved" role="status">{{ failure }}</p>
    <p v-if="output?.cleanup_pending" class="note">成果已保存，部分历史备份将在下次交付时重试清理。</p>
    <p v-if="saved&&sourceId&&sourceId!==route.params.id" class="note">来自原对话</p>
    <RouterLink v-if="saved&&sourceId" :to="target" :aria-label="`查看交付 V${output?.version}`">查看交付 <span aria-hidden="true">→</span></RouterLink>
    <details v-if="!saved"><summary>工具返回详情</summary><pre><code>{{ rawOutput }}</code></pre></details>
  </article>
</template>
<style scoped>
.delivery-activity{box-sizing:border-box;max-width:100%;border:1px solid var(--tuck-border);border-radius:8px;background:var(--tuck-surface);padding:8px 10px;color:var(--tuck-text);font-size:var(--tuck-font-ui);line-height:1.6}
.delivery-activity header{display:flex;align-items:baseline;gap:8px 12px;flex-wrap:wrap;padding:0;border:0;min-height:0;background:transparent}.delivery-activity header strong{font-weight:600}.delivery-activity header span,.delivery-activity .note{color:var(--tuck-muted);font-size:var(--tuck-font-caption)}.delivery-activity p{margin:4px 0;padding:0;overflow-wrap:anywhere}.delivery-activity a{display:inline-flex;gap:6px;margin-top:4px;color:var(--tuck-accent);text-decoration:none}.delivery-activity a:hover{text-decoration:underline}.delivery-activity.failed{border-style:dashed}
.delivery-activity details{margin-top:7px;min-width:0}.delivery-activity summary{cursor:pointer;color:var(--tuck-muted);font-size:var(--tuck-font-caption)}.delivery-activity pre{margin:7px 0 0;max-height:min(42dvh,360px);overflow:auto;padding:8px 10px;border-radius:5px;white-space:pre-wrap;overflow-wrap:anywhere;font:12px/1.6 ui-monospace,Consolas,monospace;background:var(--tuck-background);color:var(--tuck-text)}
</style>
