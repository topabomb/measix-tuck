export interface ImageDemand {index:number;visible:boolean;edge:number}
export interface ImageUpdate {index:number;state:'deferred'|'loading'|'loaded'|'failed';url?:string;error?:string}
export interface ImageProgress {loading:boolean;loaded:number;total:number;failures:{index:number;path:string;error:string}[]}

// Only the current frame retains decoded images. Its offscreen reuse cache is
// bounded by both entries and estimated UTF-16 data-URL bytes, and dies with the
// frame. Visible resources are working data, never denied by a lifetime quota.
export const previewCacheLimits={entries:24,bytes:32*1024*1024}
const resolution=(edge:number)=>[128,256,512,1024,2048].find(size=>size>=edge)||2048
export function previewImages(
  paths:Map<number,string>,
  read:(path:string,signal:AbortSignal,edge:number)=>Promise<string>,
  deliver:(update:ImageUpdate)=>void,
  progress:(value:ImageProgress)=>void,
) {
  let wanted=new Map<number,ImageDemand>(),running=false,disposed=false
  let active:{path:string;controller:AbortController}|undefined
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
    progress({loading:!!active,loaded:loaded.size,total:paths.size,failures:items})
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
  async function pump() {
    if(running||disposed)return
    running=true
    try {
      while(!disposed) {
        const candidates=[...wanted.values()].sort((a,b)=>Number(b.visible)-Number(a.visible))
        const next=candidates.find(item=>needsRead(paths.get(item.index)!))
        if(!next)break
        const path=paths.get(next.index)!,controller=new AbortController()
        active={path,controller};if(!cache.has(path))show(path,'loading');report()
        try {
          const edge=requestedEdge(path)
          const url=await read(path,controller.signal,edge)
          if(disposed||controller.signal.aborted||!desiredPaths().has(path))continue
          cache.delete(path);cache.set(path,{url,edge});show(path,'loaded',url);trim()
        } catch(cause) {
          if(!disposed&&!controller.signal.aborted&&desiredPaths().has(path)){
            const error=cause instanceof Error?cause.message:'图片读取失败'
            failures.set(path,error);const previous=cache.get(path);if(previous)show(path,'loaded',previous.url);else show(path,'failed',undefined,error)
          }
        } finally {if(active?.controller===controller)active=undefined;report()}
      }
    } finally {running=false}
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
    if(active){
      const readingVisible=[...wanted.values()].some(item=>item.visible&&paths.get(item.index)===active!.path)
      const pendingVisible=[...wanted.values()].some(item=>item.visible&&paths.get(item.index)!==active!.path&&needsRead(paths.get(item.index)!))
      if(!desired.has(active.path)||!readingVisible&&pendingVisible)active.controller.abort()
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
    disposed=true;active?.controller.abort();active=undefined
    wanted.clear();cache.clear();failures.clear();loaded.clear();sent.clear()
  }
  return {update,retry,reject,dispose}
}
