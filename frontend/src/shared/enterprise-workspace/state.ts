import { reactive, type InjectionKey } from 'vue'
import { authFetch, csrfHeader, expiredLogin } from '../../app/auth'
import { appUrl } from '../../app/urls'
import { workspaceConflict } from './dialog'
import { basename, copyPath, decodeText, encodeText, join, kind, parent, validPath, type Entry, type Projection } from './files'
export interface ScrollPosition {left:number;top:number}
// Transient reader state belongs to the existing tab, never browser storage.
export interface DocumentView {
  mode:'preview'|'source';wrap:boolean;zoom:number;offset:{x:number;y:number}
  source:ScrollPosition&{anchor:number;head:number}
  preview:ScrollPosition&{containers?:{path:number[];left:number;top:number}[]};csv:ScrollPosition&{filter:string;page:number;row:number;col:number}
  pdf:ScrollPosition&{page:number;scale:number;query:string;showThumbs:boolean}
  media:{position:number;rate:number;volume:number;muted:boolean}
}
export interface Document { id:number; path:string; type:string; text:string; original:string; etag:string; bom:boolean; newline:string; readonly:boolean; reason:string; loaded:boolean; error:string;view?:DocumentView }
interface Transfer { path:string; progress:number; loaded:number; total:number; status:string; cancel?:()=>void }
export const workspaceKey:InjectionKey<ReturnType<typeof createEnterpriseWorkspace>> = Symbol('enterprise-workspace')
const maxTabs=12,maxCachedTextBytes=24*1024*1024
// Core requires a single strong condition. Restrict upload tokens to ASCII so
// browser header encoding and Core's byte-length validation agree.
const uploadETag=(value?:string)=>!!value&&value.length<=1024&&/^"[^\x00-\x20"<>\[\]()\x7f-\uffff]*"$/.test(value)
export function createEnterpriseWorkspace() {
  const state = reactive({projection:null as Projection|null,projectionReadAt:null as number|null,dir:'',directoryLoaded:false,entries:[] as Entry[],tabs:[] as Document[],active:'',error:'',notice:'',loading:false,busy:false,uploading:false,refreshing:false,refreshError:'',used:undefined as number|undefined,available:undefined as number|undefined,clipboard:null as {cut:boolean;entries:Entry[]}|null,transfers:[] as Transfer[],tree:{} as Record<string,Entry[]>})
  const controllers = new Set<AbortController>(),loads=new Map<number,Promise<void>>(); let epoch = 0, listing = 0,projectionSequence=0,documentId=0,refreshController:AbortController|null=null
  const lastUsed=new Map<number,number>(),saving=new Set<number>();let accessSequence=0
  const textBytes=(doc:Document)=>2*(doc.text.length+(doc.text===doc.original?0:doc.original.length))
  function trimTabs(reserve=0) {
    const ids=new Set(state.tabs.map(doc=>doc.id))
    for(const id of lastUsed.keys())if(!ids.has(id))lastUsed.delete(id)
    let count=state.tabs.length,bytes=state.tabs.reduce((total,doc)=>total+textBytes(doc),0)
    const removed=new Set<number>(),candidates=state.tabs.filter(doc=>doc.path!==state.active&&!loads.has(doc.id)&&!saving.has(doc.id)&&doc.text===doc.original).sort((a,b)=>(lastUsed.get(a.id)||0)-(lastUsed.get(b.id)||0))
    for(const doc of candidates){if(count+reserve<=maxTabs&&bytes<=maxCachedTextBytes)break;removed.add(doc.id);count--;bytes-=textBytes(doc)}
    // Decide before mutation so a rejected new open keeps the current tabs and
    // active document intact. Drafts and in-flight operations are never evicted.
    if(count+reserve>maxTabs)throw new Error('文件标签已满，请保存或关闭部分标签后重试')
    if(removed.size){state.tabs=state.tabs.filter(doc=>!removed.has(doc.id));for(const id of removed)lastUsed.delete(id)}
  }
  const generation=()=>epoch
  const assertGeneration=(expected:number)=>{if(expected!==epoch)throw new Error('企业工作区已变化，已停止后续操作')}
  function cancelRefresh() {refreshController?.abort();refreshController=null;state.refreshing=false}
  function abort() { cancelRefresh();epoch++;state.uploading=false; for (const c of controllers) c.abort(); controllers.clear(); for (const t of state.transfers) t.cancel?.() }
  function reset() { abort();projectionSequence++;loads.clear();lastUsed.clear();saving.clear(); Object.assign(state,{projection:null,projectionReadAt:null,entries:[],tabs:[],active:'',dir:'',directoryLoaded:false,clipboard:null,tree:{},transfers:[],error:'',notice:'',refreshError:''}) }
  function unavailable(message:string){abort();if(state.projection)state.projection.filesAvailable=false;state.error=message}
  function url(resource:string,path = '',extra:Record<string,string> = {}) {
    const p = state.projection
    return '/api/enterprise-workspace'+resource+(resource ? '?'+new URLSearchParams({agentSpaceId:p?.agentSpaceId || '',subject:p?.subject || '',path,...extra}) : '')
  }
  async function response(resource:string,path = '',options:RequestInit = {},extra:Record<string,string> = {},background=false) {
    options.signal?.throwIfAborted()
    if(!background)cancelRefresh()
    if(resource && !state.projection?.filesAvailable) throw new Error('企业工作区当前不可用，请刷新状态')
    const c = new AbortController(); controllers.add(c); const current = epoch
    const externalAbort = () => c.abort(); options.signal?.addEventListener('abort',externalAbort,{once:true})
    const release = () => { controllers.delete(c); options.signal?.removeEventListener('abort',externalAbort) }
    try {
      const res = await authFetch(url(resource,path,extra),{...options,signal:c.signal})
      if (current !== epoch) throw new Error('工作区身份已变化')
      if (!res.ok) { const data = await res.json().catch(()=>({})); throw Object.assign(new Error(data.error || `文件请求失败 (${res.status})`),{status:res.status,code:data.code}) }
      if(!res.body) {release();return res}
      const reader=res.body.getReader()
      return new Response(new ReadableStream<Uint8Array>({async pull(target){try{const {done,value}=await reader.read();if(current!==epoch)throw new Error('工作区身份已变化');if(done){release();target.close()}else target.enqueue(value)}catch(error){release();target.error(error)}},async cancel(){c.abort();release();await reader.cancel()}}),{status:res.status,statusText:res.statusText,headers:res.headers})
    } catch(error) { release();throw error }
  }
  async function refresh(expected?:string,options:{signal?:AbortSignal;background?:boolean;requireFiles?:boolean} = {}) {
    const sequence=++projectionSequence
    const assertCurrent=()=>{if(sequence!==projectionSequence)throw new DOMException('已取消过时的工作区状态读取','AbortError')}
    let projection:Projection
    try{projection=await (await response('','',{signal:options.signal},{},options.background)).json()}
    catch(error){assertCurrent();throw error}
    options.signal?.throwIfAborted()
    // A later navigation owns the projection, including a rejected deep link.
    // Startup probes and old background reads must never re-enable its writes.
    assertCurrent()
    if (state.projection && (state.projection.subject !== projection.subject || state.projection.agentSpaceId !== projection.agentSpaceId)) { reset(); state.error = '企业账号或工作区已变化，请重新打开目标' }
    state.projection = projection
    state.projectionReadAt = Date.now()
    if (!projection.filesAvailable && options.requireFiles!==false) throw new Error(projection.filesReason || `工作区暂不可用 (${projection.state})`)
    if (expected && expected !== projection.agentSpaceId) {state.projection.filesAvailable=false;throw Object.assign(new Error('此地址属于其他企业工作区，不能打开；请返回当前工作区根目录'),{code:'workspace_link_changed'})}
    if(!options.background)state.refreshError=''
  }
  async function list(path = state.dir,treeOnly = false,options:{signal?:AbortSignal;background?:boolean} = {}) {
    if (!validPath(path)) throw new Error('目录路径无效')
    const current = treeOnly || options.background ? listing : ++listing,generation=epoch
    if (!treeOnly && !options.background) { state.loading = true; state.error = '' }
    try {
      const value = await (await response('/files',path,{signal:options.signal},{},options.background)).json()
      options.signal?.throwIfAborted();assertGeneration(generation)
      if(!treeOnly && options.background && (current!==listing || path!==state.dir))return value.entries as Entry[]
      state.tree[path] = value.entries
      if (!treeOnly && current === listing) {
        const unchanged=path===state.dir && state.entries.length===value.entries.length && state.entries.every((entry,index)=>{const next=value.entries[index];return (entry.kind!=='FILE'||!!entry.etag||!!entry.modifiedAt)&&entry.path===next.path&&entry.kind===next.kind&&entry.etag===next.etag&&entry.size===next.size&&entry.modifiedAt===next.modifiedAt})
        if(!unchanged)state.entries=value.entries
        state.dir=path;state.directoryLoaded=true;state.used=value.usedBytes;state.available=value.availableBytes;state.refreshError=''
      }
      return value.entries as Entry[]
    } catch(error) {
      const failure=error as Error & {code?:string}
      if(treeOnly||failure.name==='AbortError'||options.signal?.aborted||generation!==epoch)throw error
      const detail=failure.code==='invalid_file_path'?'企业文件服务不允许读取此目录，请联系管理员核对访问范围':failure.code==='file_not_found'?'目录已不存在，请返回上一级或根目录':failure.message
      const directoryFailure=Object.assign(new Error(`无法读取目录“${path||'根目录'}”：${detail}`),{cause:error,code:failure.code})
      if(!treeOnly&&!options.background&&current===listing)state.error=directoryFailure.message
      throw directoryFailure
    } finally { if (!treeOnly && !options.background && current === listing) state.loading = false }
  }
  async function refreshDirectory(expected?:string) {
    if(state.busy||state.loading||refreshController)return false
    const controller=new AbortController(),path=state.dir,current=epoch,sequence=listing
    refreshController=controller;state.refreshing=true
    try {
      await refresh(expected,{signal:controller.signal,background:true})
      if(controller.signal.aborted||current!==epoch||sequence!==listing||path!==state.dir)return false
      await list(path,false,{signal:controller.signal,background:true})
      if(controller.signal.aborted||current!==epoch||sequence!==listing||path!==state.dir)return false
      state.refreshError='';return true
    } catch(error) {
      if(!controller.signal.aborted&&current===epoch&&(error as Error).name!=='AbortError')state.refreshError=`自动刷新失败：${(error as Error).message}`
      return false
    } finally {if(refreshController===controller){refreshController=null;state.refreshing=false}}
  }
  async function result(res:Response) { const value = await res.json(); if (value.outcome !== 'SUCCEEDED') throw new Error(`${value.outcome === 'UNKNOWN' ? '结果未知，请刷新核对' : '部分操作失败'}${value.failures?.map((f:{path:string;code:string})=>`：${f.path} (${f.code})`).join('') || ''}`) }
  async function mutate(action:string,path:string,extra:Record<string,unknown> = {}) { await result(await response('/files','',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({action,path,...extra})})); state.tree = {} }
  async function put(path:string,data:BodyInit,etag?:string) { await result(await response('/content',path,{method:'PUT',headers:{'Content-Type':'application/octet-stream',...(etag ? {'If-Match':etag} : {'If-None-Match':'*'})},body:data})); state.tree = {} }
  async function bounded(path:string,limit:number,headers:Record<string,string> = {},signal?:AbortSignal,background=false,onBytes?:(length:number)=>void) {
    const res = await response('/content',path,{headers,signal},{},background); const reader = res.body!.getReader(); const chunks:Uint8Array[] = []; let total = 0
    const stop = () => { void reader.cancel() }; signal?.addEventListener('abort',stop,{once:true})
    try { while (true) { const {done,value} = await reader.read(); if (done) break; total += value.length; onBytes?.(value.length); if (total > limit) throw new Error('预览超过大小限制，请下载查看'); chunks.push(value) } if (signal?.aborted) throw new DOMException('已取消','AbortError') }
    finally { signal?.removeEventListener('abort',stop); await reader.cancel() }
    const data = new Uint8Array(total); let offset = 0; for (const chunk of chunks) { data.set(chunk,offset); offset += chunk.length }
    return {data,etag:res.headers.get('etag') || ''}
  }
  async function open(path:string,asText = false) {
    if (!validPath(path,false)) throw new Error('文件路径无效')
    let doc = state.tabs.find(d => d.path === path)
    if (!doc) { trimTabs(1);state.tabs.push({id:++documentId,path,type:asText ? 'text' : kind(path),text:'',original:'',etag:'',bom:false,newline:'\n',readonly:false,reason:'',loaded:false,error:''}); doc = state.tabs[state.tabs.length-1]! }
    lastUsed.set(doc.id,++accessSequence)
    state.active = path
    if (doc.loaded) {trimTabs();return}
    if(loads.has(doc.id))return loads.get(doc.id)
    const target=doc;target.error=''
    const loading=(async()=>{const doc=target;try {
      const head = await response('/content',path,{method:'HEAD'}); doc.etag = head.headers.get('etag') || ''
      if(['audio','video','pdf'].includes(doc.type) && !/^"[^"\r\n]+"$/.test(doc.etag))throw new Error('文件服务未提供强版本号，请下载查看或重新读取')
      if (['text','markdown','csv','html'].includes(doc.type)) {
        const large = Number(head.headers.get('content-length') || 0) > 2*1024*1024
        const {data,etag} = await bounded(path,large ? 256*1024 : 2*1024*1024,{...(doc.etag ? {'If-Match':doc.etag} : {}),...(large ? {Range:'bytes=0-262143'} : {})})
        Object.assign(doc,decodeText(data,large),{etag,original:''}); doc.original = doc.text
        if (!/^"[^"\r\n]+"$/.test(etag)) { doc.readonly = true; doc.reason ||= '服务未提供可用于保存的强版本号' }
      }
      doc.loaded = true
    } catch (error) { doc.error = (error as Error).message }
    })();loads.set(target.id,loading)
    try{await loading}finally{if(loads.get(target.id)===loading){loads.delete(target.id);trimTabs()}}
  }
  async function save(doc:Document,destination = doc.path) {
    if (!doc.loaded) throw new Error('正文尚未读取完成，请重新读取；已有草稿仍可下载')
    if (doc.readonly) throw new Error(doc.reason)
    if(!validPath(destination,false))throw new Error('另存路径无效')
    if(destination===doc.path && !doc.etag)throw new Error('此内容已转换，请另存为新文件')
    if(destination!==doc.path && state.tabs.some(d=>d.path===destination))throw new Error('该路径已有打开的标签，请选择其他名称')
    const saved = doc.text
    const current=epoch,previousPath=doc.path
    const encoded=encodeText(saved,doc.bom,doc.newline)
    if(encoded.length>2*1024*1024)throw new Error('编辑内容超过 2 MiB，请下载草稿后在本机编辑并上传')
    lastUsed.set(doc.id,++accessSequence)
    saving.add(doc.id)
    try {
    await put(destination,encoded as BodyInit,destination === doc.path ? doc.etag : undefined)
    assertGeneration(current)
    // Do not associate a later HEAD's version with the old draft: reread bytes.
    const read = await bounded(destination,2*1024*1024)
    const actual = decodeText(read.data)
    if (actual.text !== saved || actual.bom !== doc.bom || actual.newline !== doc.newline && saved.includes('\n')) throw new Error('保存后文件再次变化，草稿已保留，请核对远端内容')
    assertGeneration(current)
    if(destination!==previousPath && state.tabs.some(d=>d.id!==doc.id && d.path===destination))throw new Error('文件已保存到目标，但该路径已有打开的标签；当前草稿已保留，请核对目标文件')
    doc.etag = read.etag; doc.original = saved; doc.path = destination; if(destination!==previousPath)doc.type = kind(destination);if(state.active===previousPath)state.active=destination; await list()
    } finally {saving.delete(doc.id);trimTabs()}
  }
  async function upload(files:File[],directories:string[] = []) {
    const base = state.dir, current = epoch; const created = new Set<string>(); let policy = '', failure = ''
    state.uploading=true
    try {
    async function ensureDirectory(path:string) {
      if(!validPath(path))throw new Error('上传目录路径无效')
      const segments=path.slice(base ? base.length+1 : 0).split('/').filter(Boolean);let dir=base
      for(const segment of segments){if(current!==epoch)throw new Error('上传已取消');const next=join(dir,segment);if(!created.has(next)){const entries=await list(dir,true);const existing=entries.find(e=>e.path===next);if(existing?.kind==='FILE')throw new Error('上传目录与文件同名');if(!existing)await mutate('MKCOL',next);created.add(next)}dir=next}
    }
    for(const directory of directories)await ensureDirectory(join(base,directory))
    for (const file of files) {
      if (current !== epoch) break
      let path = join(base,file.webkitRelativePath || file.name)
      if (!validPath(path,false)) { state.error = failure = `${file.name}：上传文件路径无效`; continue }
      const transfer = reactive<Transfer>({path,progress:0,loaded:0,total:file.size,status:'准备中'}); state.transfers.push(transfer)
      try {
        await ensureDirectory(parent(path))
        const siblings = await list(parent(path),true); const existing = siblings.find(e => e.path === path); let etag:string|undefined
        if (existing) {
          const replaceable=existing.kind==='FILE'&&uploadETag(existing.etag)
          if(!policy){
            const choice=await workspaceConflict(`存在同名项目“${basename(path)}”`,{preferred:'替换',allowReplace:replaceable,detail:(files.length>1?'选择适用于本次上传的所有同名文件。':'')+(replaceable?'覆盖将替换当前版本的文件。':'同名目录或无有效版本的文件不能覆盖。')})
            if(choice===null){transfer.status='已取消，未传输该文件';break}
            policy=choice
          }
          if (policy === '跳过') { transfer.status = '已跳过'; continue }
          if (policy === '替换') { if (!replaceable) throw new Error('同名目录或无有效版本的文件不能覆盖'); etag = existing.etag }
          else path=copyPath(path,candidate=>siblings.some(e=>e.path===candidate))
        }
        if (current !== epoch) break
        transfer.path=path
        await new Promise<void>((resolve,reject) => {
          const xhr = new XMLHttpRequest(); xhr.open('PUT',appUrl(url('/content',path))); xhr.withCredentials = true
          for (const [key,value] of Object.entries(csrfHeader())) xhr.setRequestHeader(key,value)
          xhr.setRequestHeader(etag ? 'If-Match' : 'If-None-Match',etag || '*'); xhr.setRequestHeader('Content-Type','application/octet-stream')
          transfer.status = '上传中'; transfer.cancel = () => xhr.abort()
          xhr.upload.onprogress = event => {transfer.loaded=Math.min(file.size,event.loaded);if(event.lengthComputable&&event.total>0)transfer.progress=Math.min(100,Math.round(event.loaded/event.total*100))}
          xhr.upload.onload = () => {transfer.loaded=file.size;transfer.progress=100;transfer.status='已发送，等待服务端确认'}
          xhr.onerror = () => reject(new Error('连接中断，远端结果未知，请刷新核对')); xhr.onabort = () => reject(new Error('已取消传输，远端结果未知，请刷新核对'));xhr.ontimeout=()=>reject(new Error('传输超时，远端结果未知，请刷新核对'))
          xhr.onload = () => {
            let value;try{value=JSON.parse(xhr.responseText)}catch{value={}}
            if(xhr.status===401){expiredLogin();reject(new Error('本地登录已过期，请重新登录后核对上传结果'));return}
            if(current!==epoch){reject(new Error('企业工作区已变化，上传结果请在原工作区核对'));return}
            if(xhr.status>=200&&xhr.status<300&&value.outcome==='SUCCEEDED'){resolve();return}
            const message=xhr.status===412?(etag?'同名文件已变化，未覆盖；请刷新核对后重新上传':'目标已存在，未覆盖；请刷新核对后重新上传'):value.error || (value.outcome==='PARTIAL'?'部分写入未完成':'上传未确认成功')
            reject(new Error(xhr.status>=500||value.outcome==='UNKNOWN'||xhr.status>=200&&xhr.status<300?`${message}；远端结果未知，请刷新核对`:message))
          }; xhr.send(file)
        }); transfer.status = '已完成'; transfer.progress = 100;transfer.loaded=file.size
        if(state.tabs.some(doc=>doc.path===path))state.notice='文件已上传；已打开的同名文件请重新读取，草稿已保留'
      } catch (error) { transfer.status = (error as Error).message; if(current===epoch)state.error=failure=`${file.name}：${transfer.status}` }
      finally { delete transfer.cancel }
    }
    if(current===epoch){try{await list()}catch(error){failure ||= (error as Error).message}if(failure)state.error=failure}
    } finally {if(current===epoch)state.uploading=false}
  }
  const dirty = () => state.tabs.some(d => d.text !== d.original)
  return {state,reset,abort,unavailable,cancelRefresh,refreshDirectory,generation,assertGeneration,url,refresh,list,mutate,put,bounded,response,open,save,upload,dirty,download:(paths:string[]) => appUrl(paths.length === 1 && state.entries.find(e => e.path === paths[0])?.kind !== 'DIRECTORY' ? url('/content',paths[0]) : url('/archive','',{paths:JSON.stringify(paths)}))}
}
