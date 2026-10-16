<script setup lang="ts">
import { onMounted, onBeforeUnmount, ref, toRefs, watch, nextTick } from 'vue'
import type {DocumentView} from './state'
import { getDocument, GlobalWorkerOptions, TextLayer, type PDFDocumentProxy, type RenderTask } from 'pdfjs-dist'
import worker from 'pdfjs-dist/build/pdf.worker.min.mjs?url'
GlobalWorkerOptions.workerSrc = worker
const props = defineProps<{url:string;state:DocumentView['pdf'];active:boolean}>()
const {page,scale,query,showThumbs}=toRefs(props.state)
const canvas = ref<HTMLCanvasElement>(), layer = ref<HTMLElement>(), scroller=ref<HTMLElement>(), count = ref(0), matches = ref<number[]>([]), status = ref('正在加载 PDF…'), thumbs = ref<{page:number;url:string}[]>([])
let drawn=false
function rememberScroll(){if(props.active&&scroller.value)Object.assign(props.state,{left:scroller.value.scrollLeft,top:scroller.value.scrollTop})}
function restoreScroll(){scroller.value?.scrollTo(props.state.left,props.state.top)}
let pdf:PDFDocumentProxy|undefined, loading:ReturnType<typeof getDocument>|undefined, render:RenderTask|undefined, thumbnailRender:RenderTask|undefined, text:TextLayer|undefined, disposed = false, revision = 0, search = 0,thumbnailRevision=0
async function draw() {
  const current = ++revision; render?.cancel(); text?.cancel()
  if (!pdf||!props.active) return
  drawn=false
  try {
    const number = Math.max(1,Math.min(count.value,Math.floor(Number(page.value)||1)))
    if(page.value!==number){page.value=number;return}
    const item = await pdf.getPage(number)
    if (current !== revision || disposed) return
    await nextTick(); if (!canvas.value || !layer.value) return
    const nominal=item.getViewport({scale:scale.value}),ratio=Math.min(devicePixelRatio||1,2)
    const reduction=Math.min(1,8192/(nominal.width*ratio),8192/(nominal.height*ratio),Math.sqrt(16_000_000/(nominal.width*nominal.height*ratio*ratio)))
    const effectiveScale=scale.value*reduction,viewport=item.getViewport({scale:effectiveScale})
    canvas.value.width = viewport.width*ratio; canvas.value.height = viewport.height*ratio
    canvas.value.style.width = `${viewport.width}px`; canvas.value.style.height = `${viewport.height}px`
    layer.value.replaceChildren(); layer.value.style.setProperty('--total-scale-factor',String(effectiveScale)); layer.value.style.width = `${viewport.width}px`; layer.value.style.height = `${viewport.height}px`
    render = item.render({canvas:canvas.value,viewport,transform:ratio === 1 ? undefined : [ratio,0,0,ratio,0,0]})
    await render.promise; if (current !== revision) return
    text = new TextLayer({textContentSource:item.streamTextContent(),container:layer.value,viewport}); await text.render(); if(current!==revision||disposed)return;drawn=true;restoreScroll();status.value = reduction<1?'此页尺寸较大，已自动降低渲染比例':''
  } catch (error) { if ((error as Error).name !== 'RenderingCancelledException' && !disposed && current === revision) status.value = (error as Error).message }
}
async function find() {
  const current = ++search; matches.value = []; if (!pdf || !query.value.trim()) return
  const document=pdf,active=()=>!disposed&&current===search&&pdf===document,results:number[]=[]
  const needle = query.value.toLocaleLowerCase(); status.value = '正在查找…'
  try{
  for (let i=1;i<=document.numPages;i++) {
    if(!active())return
    const item=await document.getPage(i);if(!active())return
    const content=await item.getTextContent();if(!active())return
    if(content.items.map(item=>'str' in item ? item.str : '').join(' ').toLocaleLowerCase().includes(needle))results.push(i)
    if(i%10===0){await new Promise(resolve=>setTimeout(resolve,0));if(!active())return}
  }
  if(!active())return
  matches.value=results;status.value = results.length ? `在 ${results.length} 页找到结果` : '没有匹配内容'; if (results[0]) page.value = results[0]
  }catch(error){if(active())status.value=`查找失败：${(error as Error).message}`}
}
async function thumbnails() {
  const current=++thumbnailRevision;thumbnailRender?.cancel()
  for (const thumb of thumbs.value) URL.revokeObjectURL(thumb.url); thumbs.value = []
  if (!pdf || !showThumbs.value || !props.active) return
  const start = Math.max(1,Math.floor(Number(page.value)||1)-4), end = Math.min(count.value,start+11)
  try{
  for (let i=start;i<=end;i++) {
    if (disposed || current!==thumbnailRevision || !showThumbs.value) return
    const item = await pdf.getPage(i);if(disposed || current!==thumbnailRevision)return
    const original=item.getViewport({scale:1}),viewport=item.getViewport({scale:Math.min(.16,160/original.width,200/original.height)}), target = document.createElement('canvas'); target.width = Math.max(1,viewport.width); target.height = Math.max(1,viewport.height)
    thumbnailRender=item.render({canvas:target,viewport});await thumbnailRender.promise
    const blob = await new Promise<Blob|null>(resolve=>target.toBlob(resolve)); if (blob && !disposed && current===thumbnailRevision) thumbs.value.push({page:i,url:URL.createObjectURL(blob)})
  }
  }catch(error){if(!disposed&&current===thumbnailRevision&&(error as Error).name!=='RenderingCancelledException')status.value=`缩略图无法读取：${(error as Error).message}`}
}
async function load(){
  if(loading||!props.active||disposed)return
  const resources=new URL('pdfjs/',document.baseURI).href
  const task=getDocument({url:props.url,withCredentials:true,enableXfa:false,disableAutoFetch:true,disableStream:true,cMapUrl:resources+'cmaps/',cMapPacked:true,standardFontDataUrl:resources+'standard_fonts/',wasmUrl:resources+'wasm/'})
  loading=task
  try{const result=await task.promise;if(disposed||loading!==task)return;pdf=result;count.value=pdf.numPages;await draw();if(showThumbs.value&&props.active)void thumbnails()}
  catch(error){if(!disposed&&loading===task&&props.active)status.value=`PDF 无法预览：${(error as Error).message}，可下载查看`}
}
onMounted(()=>{void load()})
watch([page,scale],()=>{void draw()}); watch(showThumbs,()=>{void thumbnails()})
watch(()=>props.active,async active=>{
  if(!active){revision++;search++;thumbnailRevision++;render?.cancel();thumbnailRender?.cancel();text?.cancel();if(!pdf){const task=loading;loading=undefined;void task?.destroy().catch(()=>{})}if(status.value==='正在查找…')status.value='查找已暂停，可重新查找'}
  else {await nextTick();if(!pdf)await load();else if(!drawn)await draw();restoreScroll()}
},{flush:'post'})
onBeforeUnmount(()=>{disposed = true;revision++;search++;thumbnailRevision++;render?.cancel();thumbnailRender?.cancel();text?.cancel();void loading?.destroy().catch(()=>{});for(const thumb of thumbs.value)URL.revokeObjectURL(thumb.url)})
</script>
<template><section class="ew-pdf"><div class="ew-tools"><button @click="showThumbs=!showThumbs">缩略图</button><button :disabled="page<=1" @click="page--">上一页</button><input v-model.number="page" aria-label="PDF 页码" type="number" :min="1" :max="count" /><span>/ {{ count }}</span><button :disabled="page>=count" @click="page++">下一页</button><select v-model="scale" aria-label="PDF 缩放"><option :value="0.5">50%</option><option :value="0.75">75%</option><option :value="1">100%</option><option :value="1.5">150%</option><option :value="2">200%</option></select></div><form class="ew-tools" @submit.prevent="find"><input v-model="query" placeholder="查找 PDF 文字" aria-label="查找 PDF 文字" /><button>查找</button><select v-if="matches.length" v-model="page" aria-label="匹配页面"><option v-for="n in matches" :key="n" :value="n">第 {{ n }} 页</option></select></form><p v-if="status" role="status">{{ status }}</p><div class="pdf-body"><aside v-if="showThumbs"><button @click="thumbnails">更新附近页面</button><button v-for="thumb in thumbs" :key="thumb.page" @click="page=thumb.page"><img :src="thumb.url" :alt="`第 ${thumb.page} 页`" />{{ thumb.page }}</button></aside><div ref="scroller" class="pdf-scroll" @scroll="rememberScroll"><div class="pdf-sheet"><canvas ref="canvas" /><div ref="layer" class="textLayer" /></div></div></div></section></template>
<style scoped>.ew-pdf{display:flex;flex-direction:column;min-height:0;flex:1}.pdf-body{display:flex;flex:1;min-height:0}.pdf-scroll{overflow:auto;flex:1;padding:12px;background:#737980}.pdf-sheet{position:relative;width:max-content;line-height:0;background:white}.pdf-sheet canvas{display:block}.pdf-sheet .textLayer{inset:0}.pdf-body aside{width:120px;overflow:auto;flex-shrink:0}.pdf-body aside button{display:block;width:100%}.pdf-body img{width:100%}input[type=number]{width:50px}p{padding:8px}</style>

<style scoped>
.textLayer{position:absolute;text-align:initial;overflow:clip;line-height:1;letter-spacing:normal;word-spacing:normal;text-size-adjust:none;forced-color-adjust:none;transform-origin:0 0;caret-color:CanvasText;--min-font-size:1;--text-scale-factor:calc(var(--total-scale-factor) * var(--min-font-size));--min-font-size-inv:calc(1 / var(--min-font-size))}.textLayer :deep(span),.textLayer :deep(br){color:transparent;position:absolute;white-space:pre;cursor:text;transform-origin:0 0;user-select:text}.textLayer :deep(> :not(.markedContent)),.textLayer :deep(.markedContent span:not(.markedContent)){z-index:1;--font-height:0;font-size:calc(var(--text-scale-factor) * var(--font-height));--scale-x:1;--rotate:0deg;transform:rotate(var(--rotate)) scaleX(var(--scale-x)) scale(var(--min-font-size-inv))}.textLayer :deep(.markedContent){display:contents}.textLayer :deep(::selection){background:#287be766}
</style>
