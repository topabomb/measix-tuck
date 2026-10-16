import { onBeforeUnmount, ref, shallowRef, watch, type Ref } from 'vue'
import type { Document, DocumentView } from './state'
import { embeddedImageDataURL, imageURL, staticPreview, type PreviewLink } from './preview'
import { previewBridge } from './preview-bridge'
import { previewImages, type ImageDemand, type ImageProgress, type ImageUpdate } from './preview-images'

interface PreviewOptions {
  document:()=>Document
  mode:Ref<'preview'|'source'>
  active:()=>boolean
  position:DocumentView['preview']
  fragment:()=>string|undefined
  read:(path:string,limit:number,headers:Record<string,string>,signal:AbortSignal,background:boolean)=>Promise<Uint8Array>
  open:(path:string,fragment:string)=>void
  interact:()=>void
}

// One mounted document owns its readers, frame session and decoded resources.
// The workspace owner still owns authorization, versions and all file requests.
export function useDocumentPreview(options:PreviewOptions) {
  const frame=shallowRef<HTMLIFrameElement>(),rendered=ref(''),links=shallowRef<PreviewLink[]>([])
  const image=ref(''),dimensions=ref(''),loading=ref(false),progress=ref(''),notice=ref(''),error=ref('')
  const failures=shallowRef<ImageProgress['failures']>([])
  let controller:AbortController|undefined,objectURL='',revision=0,timer:ReturnType<typeof setTimeout>|undefined
  let token='',ready=false,targets=new Set<string>(),paths=new Map<number,string>()
  let loader:ReturnType<typeof previewImages>|undefined
  const updates=new Map<number,ImageUpdate>()
  const enabled=()=>options.active()&&(options.document().type==='image'||options.mode.value==='preview')

  function clear() {
    revision++;controller?.abort();controller=undefined;loader?.dispose();loader=undefined
    if(objectURL)URL.revokeObjectURL(objectURL)
    objectURL='';image.value='';dimensions.value='';rendered.value='';links.value=[]
    token='';ready=false;targets.clear();paths.clear();updates.clear();failures.value=[]
    loading.value=false;progress.value='';notice.value='';error.value=''
  }
  function send(data:Record<string,unknown>) {
    frame.value?.contentWindow?.postMessage({...data,workspacePreview:token},'*')
  }
  function restore(fragment='') {
    const {left,top,containers}=options.position
    // Vue wraps tab state in proxies; send only plain, cloneable coordinates.
    send({type:'restore',left,top,fragment,containers:containers?.map(item=>({path:[...item.path],left:item.left,top:item.top}))})
  }
  function scroll(fragment:string) {send({type:'scroll',fragment})}
  function retry(index:number) {loader?.retry(index)}
  function message(event:MessageEvent) {
    if(event.source!==frame.value?.contentWindow||!event.data||event.data.workspacePreview!==token||!token)return
    const data=event.data
    if(data.type==='interact')options.interact()
    else if(data.type==='ready') {
      ready=true;for(const update of updates.values())send({type:'image',...update})
      restore(options.fragment())
      send({type:'active',active:enabled()})
    }else if(data.type==='position'&&enabled()&&Number.isFinite(data.left)&&Number.isFinite(data.top)&&data.left>=0&&data.top>=0){
      Object.assign(options.position,{left:data.left,top:data.top})
      if(Array.isArray(data.containers)&&data.containers.length<=16)options.position.containers=data.containers.filter((item:NonNullable<DocumentView['preview']['containers']>[number])=>item&&Array.isArray(item.path)&&item.path.length<=32&&item.path.every(index=>Number.isInteger(index)&&index>=0)&&Number.isFinite(item.left)&&Number.isFinite(item.top)&&item.left>=0&&item.top>=0)
    }else if(data.type==='images'&&enabled()&&Array.isArray(data.demands)&&data.demands.length<=paths.size) {
      const demands=data.demands.filter((value:ImageDemand)=>value&&Number.isInteger(value.index)&&paths.has(value.index)&&typeof value.visible==='boolean'&&Number.isFinite(value.edge)&&value.edge>0)
      loader?.update(demands)
    }else if(data.type==='retryImage'&&enabled()&&Number.isInteger(data.index))retry(data.index)
    else if(data.type==='imageError'&&Number.isInteger(data.index))loader?.reject(data.index,'图片无法解码，可重试或打开原图')
    else if(data.type==='open'&&enabled()&&typeof data.path==='string'&&targets.has(data.path)) {
      const fragment=typeof data.fragment==='string'?data.fragment:''
      if(fragment.length<=512&&!/[\x00-\x1f\x7f]/.test(fragment))options.open(data.path,fragment)
    }
  }
  window.addEventListener('message',message)
  watch(options.fragment,fragment=>{if(ready&&enabled()&&fragment)scroll(fragment)})
  watch(frame,()=>{ready=false},{flush:'sync'})

  async function prepare() {
    clear()
    const current=revision,doc=options.document(),{path,type,text,etag}=doc
    if(!enabled()||!doc.loaded||type!=='image'&&!['markdown','html'].includes(type))return
    controller=new AbortController();const signal=controller.signal
    const valid=()=>current===revision&&!signal.aborted
    loading.value=true
    async function resource(target:string,limit:number,background:boolean,label:string,readSignal=signal) {
      signal.throwIfAborted();readSignal.throwIfAborted()
      const request=new AbortController(),abort=()=>request.abort();let timedOut=false
      signal.addEventListener('abort',abort,{once:true})
      if(readSignal!==signal)readSignal.addEventListener('abort',abort,{once:true})
      const timeout=setTimeout(()=>{timedOut=true;request.abort()},30_000)
      try {return await options.read(target,limit,target===path&&etag?{'If-Match':etag}:{},request.signal,background)}
      catch(cause){if(timedOut&&!signal.aborted&&!readSignal.aborted)throw new Error(`${label}读取超时，请重试`);throw cause}
      finally{clearTimeout(timeout);signal.removeEventListener('abort',abort);if(readSignal!==signal)readSignal.removeEventListener('abort',abort)}
    }
    try {
      if(type==='image') {
        const result=await imageURL(await resource(path,24*1024*1024,false,'图片'))
        if(valid()){objectURL=result.url;image.value=result.url;dimensions.value=`${result.width} × ${result.height}`}
        else URL.revokeObjectURL(result.url)
      }else {
        const prepared=await staticPreview(text,path,type==='markdown',{
          signal,
          stylesheet:async target=>{const data=await resource(target,512*1024,true,'样式');signal.throwIfAborted();return new TextDecoder('utf-8',{fatal:true}).decode(data)},
        })
        if(!valid())return
        // All consumers use the same prepared descriptors, never re-discover
        // paths by parsing the serialized HTML or accepting frame-owned paths.
        links.value=prepared.links.slice(0,100)
        targets=new Set(prepared.links.map(link=>link.path))
        for(const resource of prepared.resources)if(resource.kind!=='stylesheet'&&resource.path){paths.set(resource.index,resource.path);targets.add(resource.path)}
        const warnings=prepared.warnings.slice(0,4)
        notice.value=warnings.join('\n')
        token=Array.from(crypto.getRandomValues(new Uint8Array(16)),byte=>byte.toString(16).padStart(2,'0')).join('')
        loader=previewImages(paths,async(target,readSignal,edge)=>embeddedImageDataURL(await resource(target,24*1024*1024,true,'图片',readSignal),readSignal,edge),
        update=>{if(valid()){updates.set(update.index,update);if(ready)send({type:'image',...update})}},status=>{
          if(valid()) {
            loading.value=status.loading;progress.value=status.total?`已加载 ${status.loaded} / ${status.total} 张图片`:''
            failures.value=status.failures
            notice.value=[...warnings,...(status.failures.length?[`${status.failures.length} 个图片资源加载失败，可重试或打开原图。`]:[])].join('\n')
          }
        })
        rendered.value=previewBridge(prepared,token,options.fragment())
        loading.value=false
      }
    }catch(cause){if(valid()){loader?.dispose();loader=undefined;error.value=cause instanceof Error?cause.message:'预览失败'}}
    finally{if(valid()&&!loader)loading.value=false}
  }

  watch(()=>{
    const doc=options.document()
    return [doc.id,doc.path,doc.type,doc.text,doc.loaded,doc.etag] as const
  },(value,previous)=>{
    clearTimeout(timer)
    const changed=!previous||value[0]!==previous[0]||value[1]!==previous[1]||value[2]!==previous[2]
    if(changed)void prepare()
    else {
      clear()
      if(enabled()&&value[4]&&(value[2]==='image'||['html','markdown'].includes(value[2])))timer=setTimeout(()=>{void prepare()},250)
    }
  },{immediate:true})
  watch(enabled,active=>{
    if(!active){clearTimeout(timer);send({type:'active',active:false});loader?.update([]);if(!rendered.value&&!image.value)controller?.abort()}
    else if(!rendered.value&&!image.value)void prepare()
    else if(ready){restore();send({type:'active',active:true})}
  },{flush:'post'})
  onBeforeUnmount(()=>{clearTimeout(timer);clear();window.removeEventListener('message',message)})
  return {frame,rendered,links,image,dimensions,loading,progress,notice,error,failures,scroll,retry}
}
