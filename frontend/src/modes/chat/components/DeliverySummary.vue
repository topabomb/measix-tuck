<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { DockerTask } from '../../../shared/workspace/runtime'
import { conversationPath } from '../../../app/router'
import SummaryPopover from '../../../shared/ui/SummaryPopover.vue'
import { renderMarkdownCached } from '../../../engine/vue/renderers/markdown-cache'
const props=defineProps<{task:DockerTask;toolEnabled:boolean;requireDelivery?:boolean}>()
const opened=ref(false),trigger=ref<HTMLButtonElement|null>(null)
const menuTrigger=ref<HTMLElement|null>(null)
defineExpose({open:(source?:HTMLElement)=>{menuTrigger.value=source||null;opened.value=true}})
watch(()=>props.task.id,()=>{opened.value=false})
const delivery=computed(()=>props.task.latestDelivery)
const previous=computed(()=>!!delivery.value&&delivery.value.turnId!==props.task.turnId)
const excerpt=computed(()=>{
  if(!delivery.value)return ''
  const html=renderMarkdownCached(`delivery-excerpt:${props.task.id}:${delivery.value.version}`,0,delivery.value.summary)
  const text=new DOMParser().parseFromString(html,'text/html').body.textContent?.replace(/\s+/g,' ').trim()||''
  return text.length>240?text.slice(0,240)+'…':text
})
</script>
<template>
  <div class="delivery-summary-entry">
    <button ref="trigger" class="reference-header-action" type="button" aria-label="交付摘要" aria-haspopup="dialog" :aria-expanded="opened" @click="menuTrigger=null;opened=!opened"><svg aria-hidden="true" viewBox="0 0 20 20"><path d="M6 3H4v15h12V3h-2 M7 2h6v4H7Z M6 11l3 3 5-5" /></svg><span>{{ previous?'上次交付':'交付' }}{{ delivery?` V${delivery.version}`:'' }}</span></button>
    <SummaryPopover v-model:open="opened" :trigger="menuTrigger||trigger" label="交付摘要" class="delivery-popover">
        <dl><dt>交付工具</dt><dd>{{ toolEnabled?'已启用':'已停用' }}</dd><dt>交付要求</dt><dd>{{ requireDelivery===undefined?'未读取':requireDelivery?'已开启':'已关闭' }}<small v-if="!toolEnabled">当前助手不适用</small></dd>
          <template v-if="delivery"><dt>{{ previous?'上次交付':'本轮交付' }}</dt><dd>V{{ delivery.version }} · {{ delivery.fileCount }} 个文件</dd><dt>交付时间</dt><dd><time :datetime="new Date(delivery.createdAt).toISOString()">{{ new Date(delivery.createdAt).toLocaleString() }}</time></dd></template>
        </dl>
        <p v-if="!toolEnabled" class="delivery-note">交付工具已停用，仍可查看历史交付。</p>
        <p v-if="previous" class="delivery-note">本轮尚未提交交付。</p>
        <p v-if="delivery" class="delivery-excerpt">{{ excerpt }}</p><p v-else class="delivery-note">尚无交付。</p>
        <RouterLink class="delivery-open" :to="`${conversationPath(task.id)}/delivery${delivery?`?version=${delivery.version}`:''}`">查看交付</RouterLink>
    </SummaryPopover>
  </div>
</template>
<style scoped>
.delivery-summary-entry{position:relative;display:flex;flex:none}
.reference-header-action{display:inline-flex;align-items:center;justify-content:center;gap:5px;min-width:30px;height:30px;padding:0 8px;box-sizing:border-box;border:1px solid var(--tuck-border);border-radius:6px;background:var(--tuck-surface);color:var(--tuck-text);font:var(--tuck-font-ui)/1 var(--tuck-font-family);white-space:nowrap;cursor:pointer}.reference-header-action:hover,.reference-header-action[aria-expanded=true]{background:var(--tuck-hover)}.reference-header-action[aria-expanded=true]{border-color:var(--tuck-accent)}.reference-header-action svg{width:14px;height:14px;fill:none;stroke:currentColor;stroke-width:1.4;stroke-linecap:round;stroke-linejoin:round}
dl{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:var(--summary-row-gap) 12px;margin:0}dt{color:var(--tuck-muted)}dd{margin:0;text-align:right;font-variant-numeric:tabular-nums}dd small{display:block;color:var(--tuck-muted);font-size:var(--tuck-font-caption)}
.delivery-note{margin:6px 0 0;color:var(--tuck-muted);font-size:var(--tuck-font-caption)}.delivery-excerpt{margin:8px 0;padding-top:8px;border-top:1px solid var(--tuck-border);overflow-wrap:anywhere;line-height:var(--tuck-line-body)}.delivery-open{display:inline-block;color:var(--tuck-accent);text-decoration:none;margin-top:8px}.delivery-open:hover{text-decoration:underline}
@container(max-width:760px){.reference-header-action>span{display:none}.reference-header-action{width:30px;padding:0}}
</style>
