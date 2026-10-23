<script setup lang="ts">
import { computed, inject, nextTick, onMounted, onBeforeUnmount, ref, toRefs, watch, defineAsyncComponent, h } from 'vue'
import { appUrl } from '../../app/urls'
import { workspaceKey, type Document } from './state'
import { workspacePrompt, workspaceConfirm } from './dialog'
import { basename, bytes, copyPath, parent, parseCSV, serializeCSV, encodeText } from './files'
import { useDocumentPreview } from './document-preview'
import MediaPreview from './MediaPreview.vue'
import TextEditor from './SourceEditor.vue'
import { formatJSONSource } from './json'
const loadingView=(name:string)=>()=>h('p',{class:'ew-empty',role:'status'},`${name}加载中…`)
const failedView=(name:string)=>()=>h('p',{class:'ew-error',role:'alert'},`${name}加载失败，请刷新页面重试`)
const PdfPreview = defineAsyncComponent({loader:()=>import('./PdfPreview.vue'),loadingComponent:loadingView('PDF 预览'),errorComponent:failedView('PDF 预览'),delay:200})
const props = defineProps<{doc:Document;active:boolean;fragment?:string}>()
const emit = defineEmits<{open:[path:string];text:[path:string];link:[path:string,fragment?:string];fragment:[value:string];saved:[path:string]}>()
const owner = inject(workspaceKey)!, pending = ref(false), error = ref(''), compare = ref('')
props.doc.view??= {mode:props.doc.type==='text'?'source':'preview',wrap:true,zoom:1,offset:{x:0,y:0},source:{left:0,top:0,anchor:0,head:0},preview:{left:0,top:0},csv:{left:0,top:0,filter:'',page:1,row:0,col:0},pdf:{left:0,top:0,page:1,scale:1,query:'',showThumbs:false},media:{position:0,rate:1,volume:1,muted:false}}
const view=props.doc.view
const {mode,wrap,zoom,offset}=toRefs(view)
const csvFilter=computed({get:()=>view.csv.filter,set:value=>{view.csv.filter=value}}),csvPage=computed({get:()=>view.csv.page,set:value=>{view.csv.page=value}})
const selection=computed({get:()=>({row:view.csv.row,col:view.csv.col}),set:value=>{Object.assign(view.csv,value)}}),host=ref<HTMLElement>()
function scrolled(event:Event){const node=event.target;if(props.active&&node instanceof HTMLElement&&node.classList.contains('csv-scroll'))Object.assign(view.csv,{left:node.scrollLeft,top:node.scrollTop})}
async function restoreScroll(){await nextTick();if(!props.active)return;const node=host.value?.querySelector('.csv-scroll');node?.scrollTo(view.csv.left,view.csv.top)}
watch([()=>props.active,mode],()=>{void restoreScroll()},{flush:'post'})
onMounted(()=>{void restoreScroll()})
const source = computed(()=>['text','markdown','csv','html'].includes(props.doc.type))
const saveDisabled=computed(()=>props.doc.readonly||pending.value||owner.state.busy||!owner.state.projection?.filesAvailable||!props.doc.loaded)
const saving=computed(()=>props.doc.saveStatus==='saving'||props.doc.saveStatus==='verifying')
const saveFeedback=computed(()=>{
  const doc=props.doc
  if(doc.saveStatus==='saving')return '正在保存，等待服务端确认'
  if(doc.saveStatus==='verifying')return '服务端已确认，正在核对文件内容'
  if(doc.saveStatus==='failed')return '保存未确认，请核对远端内容；当前草稿已保留'
  if(doc.text!==doc.original)return '有未保存修改'
  return doc.saveStatus==='saved'?'服务端已确认保存':''
})
const more=ref<HTMLDetailsElement>(), previewInfo=ref<HTMLDetailsElement>()
watch(()=>props.active,active=>{if(!active){if(more.value)more.value.open=false;if(previewInfo.value)previewInfo.value.open=false}})
watch(()=>props.doc.path,()=>{compare.value='';error.value=''})
const {frame,rendered,links,image,dimensions,loading:previewLoading,progress:previewProgress,notice:previewNotice,error:previewError,failures:imageFailures,scroll:scrollPreview,retry:retryPreviewImage}=useDocumentPreview({
  document:()=>props.doc,mode,active:()=>props.active,position:view.preview,fragment:()=>props.fragment,
  read:async(...args)=>(await owner.bounded(...args)).data,
  resource:owner.previewResource,
  open:previewLink,
  interact:()=>{if(more.value)more.value.open=false;if(previewInfo.value)previewInfo.value.open=false},
})
const previewIssues=computed(()=>imageFailures.value.length?`${imageFailures.value.length} 项失败`:'资源异常')
function closeOutside(event:PointerEvent){if(event.target instanceof Node)for(const details of [more.value,previewInfo.value])if(details?.open&&!details.contains(event.target))details.open=false}
window.addEventListener('pointerdown',closeOutside)
function dismissMore(){if(more.value){more.value.open=false;more.value.querySelector('summary')?.focus()}}
function dismissPreviewInfo(){if(previewInfo.value){previewInfo.value.open=false;previewInfo.value.querySelector('summary')?.focus()}}
function closeOtherMenu(current:'more'|'preview'){const other=current==='more'?previewInfo.value:more.value;if(other)other.open=false}
function closeMore(event:MouseEvent){if((event.target as HTMLElement).closest('button,a'))dismissMore()}
function previewLink(path:string,fragment=''){dismissPreviewInfo();if(path===props.doc.path)scrollToFragment(fragment);else emit('link',path,fragment)}
function originalImage(path:string){dismissPreviewInfo();emit('open',path)}
function retryImage(index:number){retryPreviewImage(index);previewInfo.value?.querySelector('summary')?.focus()}
function scrollToFragment(fragment:string){scrollPreview(fragment);emit('fragment',fragment)}
const delimiter = computed(()=>props.doc.path.toLowerCase().endsWith('.tsv') ? '\t' : ',')
const table = computed(()=>{try { const rows = props.doc.type==='csv'?parseCSV(props.doc.text,delimiter.value):[]; return {rows,error:rows.length > 10000 || rows.some(r=>r.length>200) ? '表格过大，请使用源码模式（最多 10000 行、200 列）' : ''} } catch(error) {return {rows:[],error:(error as Error).message}}})
const rows = computed(()=>table.value.rows.map((cells,index)=>({cells,index})).filter(row=>!csvFilter.value || row.cells.some(cell=>cell.toLocaleLowerCase().includes(csvFilter.value.toLocaleLowerCase()))))
const visibleRows=computed(()=>rows.value.slice((csvPage.value-1)*100,csvPage.value*100))
watch(csvFilter,()=>{csvPage.value=1})
watch(()=>rows.value.length,()=>{csvPage.value=Math.min(csvPage.value,Math.max(1,Math.ceil(rows.value.length/100)))})
const columns = computed(()=>table.value.rows.reduce((max,row)=>Math.max(max,row.length),1))
const mediaURL = computed(()=>appUrl(owner.url('/content',props.doc.path,{preview:'1',...(props.doc.etag ? {version:props.doc.etag} : {})})))
async function act(fn:()=>Promise<void>) {if(pending.value||owner.state.busy)return;owner.cancelRefresh();pending.value=true;owner.state.busy=true;error.value='';try{await fn()}catch(cause){error.value=(cause as Error).message}finally{pending.value=false;owner.state.busy=false}}
function save(as = false) {void act(async()=>{const doc=props.doc,generation=owner.generation();const destination = as ? await workspacePrompt('另存为（工作区相对路径）',copyPath(doc.path,candidate=>candidate===doc.path||owner.state.entries.some(entry=>entry.path===candidate))) : doc.path;if(destination){owner.assertGeneration(generation);await owner.save(doc,destination);emit('saved',destination)}})}
async function reload() {const generation=owner.generation();if(props.doc.text !== props.doc.original && !await workspaceConfirm('丢弃当前未保存修改并读取远端版本？'))return;owner.assertGeneration(generation);props.doc.loaded=false;props.doc.error='';await owner.open(props.doc.path)}
function csvUpdate(row:number,col:number,value:string) {if(props.doc.readonly||table.value.error)return;const data=table.value.rows.map(r=>[...r]);while(data[row]!.length<=col)data[row]!.push('');data[row]![col]=value;props.doc.text=serializeCSV(data,delimiter.value,props.doc.text.endsWith('\n'))}
function csvStructure(action:string) {if(props.doc.readonly||table.value.error)return;const data=table.value.rows.map(r=>[...r]);if(action==='row')data.push(Array(columns.value).fill(''));if(action==='column')data.forEach(r=>r.push(''));if(action==='delete-row')data.splice(selection.value.row,1);if(action==='delete-column')data.forEach(r=>r.splice(selection.value.col,1));props.doc.text=serializeCSV(data,delimiter.value,props.doc.text.endsWith('\n'))}
async function compareRemote() {compare.value=new TextDecoder().decode((await owner.bounded(props.doc.path,2*1024*1024)).data)}
function downloadDraft(){const url=URL.createObjectURL(new Blob([encodeText(props.doc.text,props.doc.bom,props.doc.newline) as BlobPart]));const a=document.createElement('a');a.href=url;a.download=basename(props.doc.path);a.click();setTimeout(()=>URL.revokeObjectURL(url),1000)}
function formatJSON() {try{props.doc.text=formatJSONSource(props.doc.text);error.value=''}catch(cause){error.value=(cause as Error).message}}
function drag(event:PointerEvent) {if(event.button!==0)return;const node=event.currentTarget as HTMLElement;node.setPointerCapture(event.pointerId);const start={x:event.clientX-offset.value.x,y:event.clientY-offset.value.y};const move=(e:PointerEvent)=>{offset.value={x:e.clientX-start.x,y:e.clientY-start.y}};const end=()=>{node.removeEventListener('pointermove',move);node.removeEventListener('pointerup',end);node.removeEventListener('pointercancel',end)};node.addEventListener('pointermove',move);node.addEventListener('pointerup',end);node.addEventListener('pointercancel',end)}
const adjacent = computed(()=>(owner.state.tree[parent(props.doc.path)]||[]).filter(e=>e.kind==='FILE' && /\.(png|jpe?g|gif|webp|bmp)$/i.test(e.path)).sort((a,b)=>basename(a.path).localeCompare(basename(b.path),undefined,{numeric:true})))
const adjacentLoading=ref(false),adjacentError=ref('')
let adjacentController:AbortController|undefined
async function loadImageDirectory(){
  adjacentController?.abort();adjacentError.value='';adjacentLoading.value=false
  if(props.doc.type!=='image'||Object.hasOwn(owner.state.tree,parent(props.doc.path)))return
  const request=new AbortController();adjacentController=request;adjacentLoading.value=true
  try{await owner.list(parent(props.doc.path),true,{signal:request.signal,background:true})}
  catch(cause){if(!request.signal.aborted)adjacentError.value=`同目录图片读取失败：${(cause as Error).message}`}
  finally{if(adjacentController===request)adjacentLoading.value=false}
}
watch([()=>props.doc.path,()=>props.active],()=>{if(props.active)void loadImageDirectory();else adjacentController?.abort()},{immediate:true})
const imageIndex = computed(()=>adjacent.value.findIndex(e=>e.path===props.doc.path))
function nextImage(delta:number) {if(imageIndex.value<0)return;const target=adjacent.value[imageIndex.value+delta];if(target)emit('open',target.path)}
onBeforeUnmount(()=>{adjacentController?.abort();window.removeEventListener('pointerdown',closeOutside)})
</script>
<template><section ref="host" class="ew-document" :aria-label="basename(doc.path)" @scroll.capture="scrolled">
  <div class="ew-tools ew-document-tools" :class="{'ew-picture-toolbar':doc.type==='image'}" role="toolbar" aria-label="文件查看操作">
    <template v-if="source">
      <button v-if="doc.type!=='text'" :aria-pressed="mode==='preview'" @click="mode='preview'">预览</button>
      <button :aria-pressed="mode==='source'" @click="mode='source'">源码</button>
      <button :disabled="saveDisabled || doc.text===doc.original" @click="save()">保存</button>
    </template>
    <div v-else-if="doc.type==='image'" class="ew-picture-controls" role="group" aria-label="图片查看">
      <button class="ew-doc-icon" aria-label="上一张" title="同目录上一张" :disabled="adjacentLoading || imageIndex<=0" @click="nextImage(-1)"><svg aria-hidden="true" viewBox="0 0 24 24"><path d="m15 5-7 7 7 7" /></svg></button>
      <button class="ew-doc-icon" aria-label="下一张" title="同目录下一张" :disabled="adjacentLoading || imageIndex<0 || imageIndex>=adjacent.length-1" @click="nextImage(1)"><svg aria-hidden="true" viewBox="0 0 24 24"><path d="m9 5 7 7-7 7" /></svg></button>
      <button class="ew-doc-icon" aria-label="缩小图片" title="缩小图片" :disabled="!image || zoom<=.1" @click="zoom=Math.max(.1,zoom-.25)"><svg aria-hidden="true" viewBox="0 0 24 24"><path d="M5 12h14" /></svg></button>
      <span class="ew-zoom-value">{{Math.round(zoom*100)}}%</span>
      <button class="ew-doc-icon" aria-label="放大图片" title="放大图片" :disabled="!image || zoom>=8" @click="zoom=Math.min(8,zoom+.25)"><svg aria-hidden="true" viewBox="0 0 24 24"><path d="M5 12h14m-7-7v14" /></svg></button>
      <button class="ew-doc-icon" aria-label="适应窗口" title="适应窗口" :disabled="!image" @click="zoom=1;offset={x:0,y:0}"><svg aria-hidden="true" viewBox="0 0 24 24"><path d="M4 9V4h5m6 0h5v5m0 6v5h-5M9 20H4v-5M9 9h6v6H9z" /></svg></button>
      <span v-if="dimensions" class="ew-picture-size">{{dimensions}}</span>
    </div>
    <span v-else class="ew-document-name" :title="doc.path">{{basename(doc.path)}}</span>
    <span v-if="pending && !saving || adjacentLoading" role="status" class="ew-document-pending" :title="pending?'处理中':'正在读取同目录图片'"><span class="ew-spinner" aria-hidden="true" /></span>
    <div v-if="['markdown','html'].includes(doc.type) && mode==='preview'" class="ew-preview-info">
      <details ref="previewInfo" class="ew-links" @keydown.esc.stop.prevent="dismissPreviewInfo">
        <summary role="button" :class="{'has-warning':!!previewNotice}" :title="previewNotice||'预览说明与链接'" aria-label="预览说明与链接" @click="closeOtherMenu('preview')">
          <svg v-if="previewNotice" aria-hidden="true" viewBox="0 0 24 24"><path d="m12 3 10 18H2zM12 9v5m0 3v.1" /></svg>
          <span class="ew-preview-kind">{{doc.type==='html'?'静态预览':'文件链接'}}</span>
          <span v-if="previewNotice" class="ew-preview-issue" role="status" aria-label="部分资源未加载">{{previewIssues}}</span>
        </summary>
        <div>
          <p class="ew-preview-policy">按需加载图片 · 脚本与外部资源已禁用</p>
          <p v-if="previewProgress">{{previewProgress}}</p>
          <p v-if="previewNotice" class="ew-preview-problems">{{previewNotice}}</p>
          <div v-for="failure in imageFailures" :key="failure.path" class="ew-resource-error">
            <span :title="failure.path">{{basename(failure.path)}}：{{failure.error}}</span>
            <button @click="retryImage(failure.index)">重试</button>
            <button @click="originalImage(failure.path)">打开原图</button>
          </div>
          <div v-if="links.length" class="ew-preview-links" role="group" aria-label="工作区文件链接"><button v-for="(link,i) in links" :key="i" :title="link.path" @click="previewLink(link.path,link.fragment)">{{link.text}}</button></div>
        </div>
      </details>
      <span v-if="previewLoading" role="status" :title="`正在加载预览资源${previewProgress?'，'+previewProgress:''}`" class="ew-preview-progress"><span class="ew-spinner" aria-hidden="true" /><span class="ew-progress-text">{{previewProgress||'加载中'}}</span></span>
    </div>
    <div class="ew-document-actions">
      <a class="ew-direct-download ew-doc-action" :href="mediaURL.replace('preview=1','preview=0')" download aria-label="下载" title="下载文件"><svg aria-hidden="true" viewBox="0 0 24 24"><path d="M12 3v12m-4-4 4 4 4-4M5 16v5h14v-5" /></svg><span>下载</span></a>
      <button class="ew-direct-reload ew-doc-action ew-doc-icon" :disabled="pending || owner.state.busy" aria-label="重新读取" title="重新读取文件" @click="act(reload)"><svg aria-hidden="true" viewBox="0 0 24 24"><path d="M20 7v5h-5m5 0a8 8 0 1 0-2 6" /></svg></button>
      <details ref="more" class="ew-more" @keydown.esc.stop.prevent="dismissMore">
        <summary role="button" class="ew-doc-icon" aria-label="文档操作" title="当前文件操作" @click="closeOtherMenu('more')"><svg aria-hidden="true" viewBox="0 0 24 24"><path d="M12 3H5v18h5M12 3v5h6M12 3l6 5v2M16 12h2l.5 1.6 1.4.8 1.6-.3 1 1.8-1.1 1.2v1.6l1.1 1.2-1 1.8-1.6-.3-1.4.8L18 22h-2l-.5-1.6-1.4-.8-1.6.3-1-1.8 1.1-1.2v-1.6l-1.1-1.2 1-1.8 1.6.3 1.4-.8Z" /><circle cx="17" cy="17" r="1.7" /></svg></summary>
        <div @click="closeMore">
          <button v-if="source" :disabled="saveDisabled" @click="save(true)">另存为</button>
          <a class="ew-menu-download" :href="mediaURL.replace('preview=1','preview=0')" download><svg aria-hidden="true" viewBox="0 0 24 24"><path d="M12 3v12m-4-4 4 4 4-4M5 16v5h14v-5" /></svg>下载</a>
          <button class="ew-menu-reload" :disabled="pending || owner.state.busy" @click="act(reload)">重新读取</button>
          <label v-if="source && mode==='source'"><input v-model="wrap" type="checkbox" />自动换行</label>
          <button v-if="/\.json$/i.test(doc.path)" :disabled="doc.readonly || !doc.loaded" @click="formatJSON">格式化 JSON</button>
          <span v-if="doc.type==='image' && dimensions" class="ew-picture-metadata">{{dimensions}}</span>
          <template v-if="adjacentError"><p class="ew-preview-problems">{{adjacentError}}</p><button @click="loadImageDirectory()">重试同目录</button></template>
        </div>
      </details>
    </div>
  </div>
  <p v-if="source && doc.loaded && saveFeedback" class="ew-save-status" role="status" aria-label="文件保存状态"><span v-if="saving" class="ew-spinner" aria-hidden="true" />{{saveFeedback}}</p>
  <p v-if="doc.reason" class="ew-notice">{{doc.reason}} <button v-if="!doc.reason.includes('截断') && !doc.reason.includes('版本')" @click="doc.readonly=false;doc.newline='\n';doc.reason='已转换为 UTF-8/LF，请另存';doc.etag=''">转换为 UTF-8/LF</button></p>
  <div v-if="error || previewError || doc.error" class="ew-error" role="alert">{{error || previewError || doc.error}}<template v-if="source"><button @click="act(compareRemote)">查看远端版本</button><button :disabled="doc.readonly || pending || owner.state.busy || !owner.state.projection?.filesAvailable || !doc.loaded" @click="save(true)">另存草稿</button><button @click="downloadDraft">下载草稿</button></template></div>
  <details v-if="compare"><summary>远端版本（当前草稿仍保留）</summary><pre>{{compare}}</pre></details>
  <p v-if="!doc.loaded && !doc.error">正在读取文件…</p>
  <template v-else-if="source && doc.loaded">
    <div v-if="doc.type==='csv' && mode==='preview'" class="csv-preview"><div class="ew-tools"><input v-model="csvFilter" aria-label="筛选 CSV" placeholder="查找 / 筛选单元格" /><template v-if="!doc.readonly"><button :disabled="!!table.error" @click="csvStructure('row')">加行</button><button :disabled="!!table.error" @click="csvStructure('column')">加列</button><button :disabled="!!table.error" @click="csvStructure('delete-row')">删当前行</button><button :disabled="!!table.error" @click="csvStructure('delete-column')">删当前列</button></template><span>{{table.rows.length}} 行 · {{columns}} 列</span><button :disabled="csvPage<=1" @click="csvPage--">上页</button><span>{{csvPage}} / {{Math.max(1,Math.ceil(rows.length/100))}}</span><button :disabled="csvPage*100>=rows.length" @click="csvPage++">下页</button></div><p v-if="table.error" role="alert">{{table.error}}</p><div v-else class="csv-scroll"><table><thead><tr><th>#</th><th v-for="col in columns" :key="col">{{col}}</th></tr></thead><tbody><tr v-for="row in visibleRows" :key="row.index"><th>{{row.index+1}}</th><td v-for="col in columns" :key="col"><textarea rows="1" :aria-label="`第 ${row.index+1} 行第 ${col} 列`" :readonly="doc.readonly || !!table.error" :value="row.cells[col-1] || ''" @focus="selection={row:row.index,col:col-1}" @input="csvUpdate(row.index,col-1,($event.target as HTMLTextAreaElement).value)" /></td></tr></tbody></table></div></div>
    <div v-else class="ew-editor-body"><TextEditor v-if="mode==='source'" :key="doc.path" v-model="doc.text" :path="doc.path" :readonly="doc.readonly" :wrap="wrap" :position="view.source" :active="active" @save="save()" /><div v-if="mode==='preview' && ['markdown','html'].includes(doc.type)" class="ew-frame"><p v-if="!rendered && previewLoading" class="ew-empty" role="status">正在准备预览…</p><iframe v-if="rendered" ref="frame" :srcdoc="rendered" sandbox="allow-scripts" referrerpolicy="no-referrer" title="文件静态预览" /></div></div>
  </template>
  <template v-else-if="doc.type==='image' && doc.loaded"><div class="image-stage" @pointerdown="drag"><p v-if="previewLoading" role="status">正在加载图片…</p><img v-if="image" :src="image" :alt="basename(doc.path)" @error="error='图片解码失败，请下载查看'" draggable="false" :style="{transform:`translate(${offset.x}px,${offset.y}px) scale(${zoom})`}" /></div></template>
  <PdfPreview v-else-if="doc.type==='pdf' && doc.loaded" :key="mediaURL" :url="mediaURL" :state="view.pdf" :active="active" />
  <MediaPreview v-else-if="doc.loaded && ['audio','video'].includes(doc.type)" :url="mediaURL" :kind="doc.type==='video'?'video':'audio'" :available="owner.state.projection?.filesAvailable===true" :state="view.media" :active="active" />
  <div v-else-if="doc.loaded" class="ew-empty"><strong>{{basename(doc.path)}}</strong><p>{{bytes(owner.state.entries.find(e=>e.path===doc.path)?.size)}}</p><p>此格式暂不提供在线预览</p><a :href="mediaURL" download>下载文件</a><button @click="emit('text',doc.path)">作为文本查看</button></div>
</section></template>
<style scoped>
.ew-document{container-type:inline-size}
.ew-document-tools{min-height:40px;padding:5px 7px;gap:4px}
.ew-document-tools>button,.ew-document-tools .ew-doc-action{height:28px;min-height:28px;box-sizing:border-box;line-height:18px;padding:4px 7px}
.ew-document-tools svg{width:16px;height:16px;flex:none}
.ew-save-status{display:flex;align-items:center;gap:7px;flex-shrink:0;margin:0;padding:6px 9px;border-bottom:1px solid var(--tuck-border);font-size:12px;color:var(--tuck-muted);overflow-wrap:anywhere}
.ew-document-tools .ew-doc-icon{display:inline-flex;align-items:center;justify-content:center;width:28px;height:28px;padding:4px;box-sizing:border-box}
.ew-picture-controls,.ew-document-actions{display:flex;align-items:center;gap:4px;flex:none}
.ew-picture-controls{min-width:0}
.ew-picture-controls button{flex:none}
.ew-zoom-value{width:38px;text-align:center;font-size:12px;color:var(--tuck-muted)}
.ew-picture-size{display:none;margin-left:6px;color:var(--tuck-muted);font-size:11px;white-space:nowrap}
.ew-document-actions{margin-left:auto}
.ew-preview-info+.ew-document-actions{margin-left:0}
.ew-document-tools .ew-doc-action{display:none;align-items:center;gap:4px;padding:5px 7px;border:1px solid var(--tuck-border);border-radius:5px;background:var(--tuck-surface);color:var(--tuck-text);font:inherit;text-decoration:none;white-space:nowrap}
.ew-doc-action:hover{background:var(--tuck-hover)}
.ew-doc-action:focus-visible{outline:2px solid var(--tuck-accent);outline-offset:2px}
.ew-document-actions .ew-more{margin-left:0}
.ew-links>summary{display:inline-flex;align-items:center;gap:4px;padding:4px 5px;border-radius:4px}
.ew-links>summary.has-warning{background:#ce55551a;color:#ce5555;font-weight:600}
.ew-preview-issue{font-size:11px;white-space:nowrap}
.ew-preview-problems{padding:6px 8px;color:#ce5555;background:#ce555512;border-left:2px solid #ce5555;white-space:pre-wrap;overflow-wrap:anywhere}
.ew-preview-policy{font-size:11px;color:var(--tuck-muted)}
.ew-resource-error{border:1px solid #ce555545;border-radius:4px;background:#ce555508;padding:6px}
.ew-resource-error>span{font-size:12px;color:#ce5555}
.ew-preview-links{display:flex;flex-wrap:wrap;gap:4px;border-top:1px solid var(--tuck-border);padding-top:6px}
.ew-preview-links button{margin:0}
.ew-menu-download{display:flex;align-items:center;gap:5px}
.ew-picture-metadata{padding:4px 8px;font-size:11px;color:var(--tuck-muted)}
.ew-progress-text{display:none}
@container(min-width:480px){.ew-document-tools .ew-direct-download,.ew-document-tools .ew-direct-reload{display:inline-flex}.ew-document-actions .ew-menu-download,.ew-document-actions .ew-menu-reload{display:none}}
@container(min-width:680px){.ew-progress-text{display:inline;font-size:11px}}
@container(min-width:900px){.ew-picture-size{display:inline}}
@container(max-width:479px){.has-warning .ew-preview-kind{display:none}}
</style>
<style scoped>.ew-document{display:flex;flex-direction:column;min-height:0;flex:1;overflow:hidden}.ew-editor-body{display:flex;flex:1;min-height:0}.ew-editor-body>*{flex:1;width:0}iframe{border:0;background:white;min-height:0}.image-stage{overflow:hidden;flex:1;min-height:0;display:flex;align-items:center;justify-content:center;touch-action:none;cursor:grab;background:repeating-conic-gradient(#bbc1c8 0% 25%,#e3e6eb 0% 50%) 50%/20px 20px}.image-stage img{max-width:100%;max-height:100%;object-fit:contain;user-select:none}.csv-preview{display:flex;flex:1;flex-direction:column;min-height:0}.csv-scroll{overflow:auto;flex:1}.csv-scroll table{border-collapse:collapse;font-size:13px}.csv-scroll th{background:var(--tuck-surface);position:sticky;top:0}.csv-scroll td,.csv-scroll th{border:1px solid var(--tuck-border);padding:0}.csv-scroll th{padding:5px}.csv-scroll textarea{border:0;border-radius:0;width:150px;background:transparent}.csv-scroll textarea:focus{outline:2px solid var(--tuck-accent);outline-offset:-2px}details pre{max-height:180px;overflow:auto;margin:8px;white-space:pre-wrap}</style>
