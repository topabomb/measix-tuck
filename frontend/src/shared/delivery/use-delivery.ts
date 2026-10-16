import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import type { DockerTask } from '../workspace/runtime'
import { authFetch } from '../../app/auth'
import { appUrl } from '../../app/urls'
import { renderMarkdownCached } from '../../engine/vue/renderers/markdown-cache'
type File = {id:string;path:string;description?:string;sizeBytes:number;mimeType:string}
type Delivery = {version:number;turnId:string;createdAt:number;summary:string;fileCount:number;files:File[]}
export function useDelivery(task:()=>DockerTask,latestOnly:()=>boolean=()=>false){
const route=useRoute(),router=useRouter()
const delivery=ref<Delivery>(),versions=ref<Delivery[]>([]),loading=ref(false),error=ref(''),missing=ref(false),hasMoreVersions=ref(false)
let controller:AbortController|undefined
const isPreviousTurn=computed(()=>!!delivery.value && delivery.value.turnId!==task().turnId)
const isHistorical=computed(()=>!!delivery.value && delivery.value.version!==(task().latestDelivery?.version||versions.value[0]?.version||delivery.value.version))
const requestedVersion=computed(()=>latestOnly()?'latest':String(route.query.version||'latest'))
const files=computed(()=>delivery.value?.files||[])
const fileUrl=(file:File)=>appUrl(`/api/tasks/${task().id}/deliveries/${delivery.value?.version}/files/${file.id}?download=1`)
const html=computed(()=>{
  if(!delivery.value)return ''
  const doc=new DOMParser().parseFromString(renderMarkdownCached(`delivery:${task().id}:${delivery.value.version}`,0,delivery.value.summary),'text/html')
  for(const el of doc.querySelectorAll('[href],[src]'))for(const attr of ['href','src']){
    if(!el.hasAttribute(attr))continue
    const value=el.getAttribute(attr)||''
    let path=value
    try{
      const target=new URL(value,location.href),localFiles=new URL(appUrl(`/api/tasks/${task().id}/files`),location.origin)
      if(target.origin===location.origin&&/\/api\/tasks\/[^/]+\/files$/.test(target.pathname)){
        path=[localFiles.pathname,`/api/tasks/${task().id}/files`].includes(target.pathname)?target.searchParams.get('path')||'':''
      }else if(/^(https?:|mailto:|#)/i.test(value))continue
      else path=decodeURIComponent(path.replace(/^\.\//,''))
    }catch{path=''}
    const file=files.value.find(f=>f.path===path)
    if(file){
      if(attr==='href'){el.setAttribute(attr,fileUrl(file));el.setAttribute('download',file.path);el.setAttribute('title',`下载 ${file.path}`)}
      else {const link=doc.createElement('a');link.href=fileUrl(file);link.download=file.path;link.textContent=el.getAttribute('alt')||file.path;link.setAttribute('aria-label',`下载 ${file.path}`);el.replaceWith(link)}
    }else if(attr==='href')el.replaceWith(doc.createTextNode(el.textContent||value));else el.removeAttribute(attr)
  }
  for(const table of doc.querySelectorAll('table')){const scroll=doc.createElement('div');scroll.className='delivery-table-scroll';scroll.tabIndex=0;table.replaceWith(scroll);scroll.append(table)}
  return doc.body.innerHTML
})
async function refresh(){
  controller?.abort();const request=controller=new AbortController();loading.value=true;error.value='';missing.value=false;delivery.value=undefined;versions.value=[];hasMoreVersions.value=false
  try{
    if(requestedVersion.value!=='latest'&&!/^[1-9]\d*$/.test(requestedVersion.value))throw new Error('交付版本号无效')
    const response=await authFetch(`/api/tasks/${task().id}/deliveries/${requestedVersion.value}`,{signal:request.signal})
    if(response.status===404){missing.value=true;return}
    if(!response.ok)throw new Error('交付读取失败，请重试')
    const data=await response.json() as Delivery;if(request.signal.aborted)return;delivery.value=data
    if(!latestOnly()){const list=await authFetch(`/api/tasks/${task().id}/deliveries?limit=20`,{signal:request.signal});if(!list.ok)throw new Error('版本列表读取失败');const data=await list.json();if(!request.signal.aborted){versions.value=data.items;hasMoreVersions.value=data.hasMore}}
  }catch(reason){if(!request.signal.aborted)error.value=(reason as Error).message}finally{if(!request.signal.aborted)loading.value=false}
}
async function moreVersions(){
  const last=versions.value.at(-1),request=controller
  if(!last||!request||loading.value||!hasMoreVersions.value)return
  error.value=''
  loading.value=true
  try{
    const response=await authFetch(`/api/tasks/${task().id}/deliveries?beforeVersion=${last.version}&limit=20`,{signal:request.signal})
    if(!response.ok)throw new Error('版本列表读取失败')
    const data=await response.json()
    if(!request.signal.aborted){versions.value.push(...data.items);hasMoreVersions.value=data.hasMore}
  }catch(reason){if(!request.signal.aborted)error.value=(reason as Error).message}
  finally{if(!request.signal.aborted)loading.value=false}
}
function choose(event:Event){void router.push({path:route.path,query:{...route.query,version:(event.target as HTMLSelectElement).value||undefined,fileSearch:undefined}})}
const size=(bytes:number)=>bytes<1024?`${bytes} B`:bytes<1024*1024?`${Math.ceil(bytes/1024)} KiB`:`${(bytes/1024/1024).toFixed(1)} MiB`
watch([()=>task().id,()=>task().latestDelivery?.version,requestedVersion],()=>void refresh(),{immediate:true})
onBeforeUnmount(()=>controller?.abort())
return {delivery,versions,hasMoreVersions,loading,error,missing,isPreviousTurn,isHistorical,requestedVersion,files,fileUrl,html,refresh,moreVersions,choose,size}
}
