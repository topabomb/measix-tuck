export interface ImageDemand {index:number;visible:boolean;edge:number}
export interface ImageUpdate {index:number;state:'deferred'|'loading'|'loaded'|'failed';url?:string;error?:string}
export interface ImageProgress {loading:boolean;loaded:number;total:number;failures:{index:number;path:string;error:string}[]}

// Only the current frame retains decoded images. Its offscreen reuse cache is
// bounded by both entries and estimated UTF-16 data-URL bytes, and dies with the
// frame. Visible resources are working data, never denied by a lifetime quota.
export const previewCacheLimits={entries:24,bytes:32*1024*1024}
export const previewConcurrency=4
// One workspace shares these slots across previews and directory thumbnails.
// A cancelled running task releases its slot only when it actually exits.
export function previewResourcePool() {
  let active=0
  const queue:{start:()=>void;cancel:()=>void}[]=[]
  function pump(){while(active<previewConcurrency&&queue.length)queue.shift()!.start()}
  return function run<T>(signal:AbortSignal,work:()=>Promise<T>):Promise<T> {
    if(signal.aborted)return Promise.reject(new DOMException('已取消','AbortError'))
    return new Promise<T>((resolve,reject)=>{
      const item={start:()=>{
        signal.removeEventListener('abort',item.cancel);active++
        void Promise.resolve().then(()=>{signal.throwIfAborted();return work()}).then(resolve,reject).finally(()=>{active--;pump()})
      },cancel:()=>{const index=queue.indexOf(item);if(index>=0)queue.splice(index,1);signal.removeEventListener('abort',item.cancel);reject(new DOMException('已取消','AbortError'))}}
      signal.addEventListener('abort',item.cancel,{once:true});queue.push(item);pump()
    })
  }
}
const resolution=(edge:number)=>[128,256,512,1024,2048].find(size=>size>=edge)||2048
export function previewImages(
  paths:Map<number,string>,
  read:(path:string,signal:AbortSignal,edge:number)=>Promise<string>,
  deliver:(update:ImageUpdate)=>void,
  progress:(value:ImageProgress)=>void,
) {
  let wanted=new Map<number,ImageDemand>(),disposed=false
  const active=new Map<string,AbortController>()
  const cache=new Map<string,{url:string;edge:number}>(),failures=new Map<string,string>(),loaded=new Set<number>()
  const sent=new Map<number,ImageUpdate>()
  function send(update:ImageUpdate) {
    const previous=sent.get(update.index)
    if(previous?.state===update.state&&previous.url===update.url&&previous.error===update.error)return
    sent.set(update.index,update);deliver(update)
  }
  function report() {
    if(disposed)return
    const items:{index:number;path:string;error:string}[]=[],seen=new Set<string>()
    for(const [index,path] of paths)if(failures.has(path)&&!seen.has(path)){seen.add(path);items.push({index,path,error:failures.get(path)!})}
    progress({loading:active.size>0,loaded:loaded.size,total:paths.size,failures:items})
  }
  function desiredPaths(){return new Set([...wanted.keys()].map(index=>paths.get(index)!))}
  function requestedEdge(path:string){return Math.max(...[...wanted.values()].filter(item=>paths.get(item.index)===path).map(item=>item.edge))}
  function needsRead(path:string){return !failures.has(path)&&(!cache.has(path)||cache.get(path)!.edge<requestedEdge(path))}
  function touch(path:string){const entry=cache.get(path);if(entry){cache.delete(path);cache.set(path,entry)}return entry}
  function trim(){
    const desired=desiredPaths(),idle=[...cache].filter(([path])=>!desired.has(path))
    let size=idle.reduce((total,[,entry])=>total+entry.url.length*2,0),count=idle.length
    for(const [path,entry] of idle){if(count<=previewCacheLimits.entries&&size<=previewCacheLimits.bytes)break;cache.delete(path);size-=entry.url.length*2;count--}
  }
  function show(path:string,state:ImageUpdate['state'],url?:string,error?:string) {
    for(const index of wanted.keys())if(paths.get(index)===path){if(state==='loaded')loaded.add(index);send({index,state,url,error})}
  }
  function pump() {
      while(!disposed&&active.size<previewConcurrency) {
        const candidates=[...wanted.values()].sort((a,b)=>Number(b.visible)-Number(a.visible))
        const next=candidates.find(item=>!active.has(paths.get(item.index)!)&&needsRead(paths.get(item.index)!))
        if(!next)break
        const path=paths.get(next.index)!,controller=new AbortController()
        active.set(path,controller);if(!cache.has(path))show(path,'loading');report()
        void (async()=>{
        try {
          const edge=requestedEdge(path)
          const url=await read(path,controller.signal,edge)
          if(disposed||controller.signal.aborted||!desiredPaths().has(path))return
          cache.delete(path);cache.set(path,{url,edge});show(path,'loaded',url);trim()
        } catch(cause) {
          if(!disposed&&!controller.signal.aborted&&desiredPaths().has(path)){
            const error=cause instanceof Error?cause.message:'图片读取失败'
            failures.set(path,error);const previous=cache.get(path);if(previous)show(path,'loaded',previous.url);else show(path,'failed',undefined,error)
          }
        } finally {active.delete(path);pump();report()}
        })()
      }
  }
  function update(demands:ImageDemand[]) {
    if(disposed)return
    const previous=wanted
    // Keep every visible image, but only a small nearby preload window. A dense
    // hidden gallery must not occupy the reader ahead of actual content.
    const nearby=new Set<string>()
    wanted=new Map(demands.filter(item=>paths.has(item.index)).sort((a,b)=>Number(b.visible)-Number(a.visible)).filter(item=>{
      if(item.visible)return true
      const path=paths.get(item.index)!
      if(!nearby.has(path)&&nearby.size>=6)return false
      nearby.add(path);return true
    }).map(item=>[item.index,{...item,edge:resolution(item.edge)}]))
    const desired=desiredPaths()
    const visible=new Set([...wanted.values()].filter(item=>item.visible).map(item=>paths.get(item.index)!))
    const pendingVisible=[...visible].filter(path=>!active.has(path)&&needsRead(path)).length
    let available=previewConcurrency-active.size
    for(const [path,controller] of active){
      if(!desired.has(path)){controller.abort();available++}
      else if(!visible.has(path)&&pendingVisible>available){controller.abort();available++}
    }
    for(const index of previous.keys())if(!wanted.has(index))send({index,state:'deferred'})
    for(const index of wanted.keys()) {
      const path=paths.get(index)!,entry=touch(path),error=failures.get(path)
      if(entry){loaded.add(index);send({index,state:'loaded',url:entry.url})}
      else if(error)send({index,state:'failed',error})
    }
    trim();report();void pump()
  }
  function retry(index:number) {
    if(disposed||!paths.has(index))return
    const path=paths.get(index)!
    failures.delete(path)
    wanted.set(index,{index,visible:true,edge:wanted.get(index)?.edge||1024})
    report();void pump()
  }
  function reject(index:number,error:string) {
    if(disposed||!paths.has(index))return
    const path=paths.get(index)!
    cache.delete(path);failures.set(path,error);show(path,'failed',undefined,error);report()
  }
  function dispose() {
    disposed=true;for(const controller of active.values())controller.abort()
    wanted.clear();cache.clear();failures.clear();loaded.clear();sent.clear()
  }
  return {update,retry,reject,dispose}
}
