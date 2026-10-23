import {describe,it,expect,vi} from 'vitest'
import {previewImages,previewCacheLimits,previewResourcePool,type ImageProgress,type ImageUpdate} from '../src/shared/enterprise-workspace/preview-images'

describe('workspace resource concurrency',()=>{
  it('shares four slots, fills a vacancy immediately and counts cancellation until work exits',async()=>{
    const run=previewResourcePool(),old=new AbortController(),next=new AbortController()
    const started:number[]=[],release=new Map<number,()=>void>()
    const work=(index:number,signal:AbortSignal)=>run(signal,()=>new Promise<void>(resolve=>{started.push(index);release.set(index,resolve)}))
    const first=Array.from({length:4},(_,index)=>work(index,old.signal))
    await vi.waitFor(()=>expect(started).toEqual([0,1,2,3]))
    const queued=work(4,old.signal).catch(error=>error.name)
    old.abort()
    expect(await queued).toBe('AbortError')
    const fifth=work(5,next.signal),sixth=work(6,next.signal)
    await Promise.resolve();expect(started).toEqual([0,1,2,3])
    release.get(1)!()
    await vi.waitFor(()=>expect(started).toEqual([0,1,2,3,5]))
    release.get(5)!()
    await vi.waitFor(()=>expect(started).toEqual([0,1,2,3,5,6]))
    for(const finish of release.values())finish()
    await Promise.all([...first,fifth,sixth])
  })
})

describe('HTML image resource cache lifecycle',()=>{
  function setup(count=30,read=vi.fn(async(path:string,_signal:AbortSignal,edge:number)=>`${path}@${edge}`)){
    const updates:ImageUpdate[]=[];let status:ImageProgress|undefined
    const loader=previewImages(new Map(Array.from({length:count},(_,i)=>[i,`${i}.png`])),read,update=>updates.push(update),value=>{status=value})
    const demand=(index:number,edge=128)=>loader.update([{index,edge,visible:true}])
    const idle=()=>vi.waitFor(()=>expect(status?.loading).toBe(false))
    return {loader,read,updates,demand,idle}
  }

  it('keeps recent offscreen resources, evicts the oldest at the entry quota and reloads it',async()=>{
    const {loader,read,demand,idle}=setup()
    for(let index=0;index<27;index++){demand(index);await idle()}
    demand(2);await idle()
    expect(read.mock.calls.filter(([path])=>path==='2.png')).toHaveLength(1)
    demand(0);await idle()
    expect(read.mock.calls.filter(([path])=>path==='0.png')).toHaveLength(2)
    loader.dispose()
  })

  it('loads four distinct paths concurrently and a slow first image does not hold the next slot',async()=>{
    const release=new Map<string,(url:string)=>void>(),updates:ImageUpdate[]=[]
    const read=vi.fn((path:string)=>new Promise<string>(resolve=>release.set(path,resolve)))
    const loader=previewImages(new Map([[0,'a'],[1,'a'],[2,'b'],[3,'c'],[4,'d'],[5,'e']]),read,item=>updates.push(item),()=>{})
    loader.update(Array.from({length:6},(_,index)=>({index,visible:true,edge:128})))
    expect(read.mock.calls.map(([path])=>path)).toEqual(['a','b','c','d'])
    release.get('b')!('b-url')
    await vi.waitFor(()=>expect(read.mock.calls.map(([path])=>path)).toEqual(['a','b','c','d','e']))
    release.get('a')!('a-url')
    await vi.waitFor(()=>expect(updates.filter(item=>item.url==='a-url').map(item=>item.index)).toEqual([0,1]))
    loader.dispose();for(const finish of release.values())finish('late')
  })

  it('cancels only enough nearby reads for visible work and never releases their slots early',async()=>{
    const tasks=new Map<string,{signal:AbortSignal;finish:(url:string)=>void}>()
    const read=vi.fn((path:string,signal:AbortSignal)=>new Promise<string>(finish=>tasks.set(path,{signal,finish})))
    const {loader}=setup(6,read)
    loader.update(Array.from({length:4},(_,index)=>({index,visible:false,edge:128})))
    loader.update([...Array.from({length:4},(_,index)=>({index,visible:false,edge:128})),{index:4,visible:true,edge:128}])
    expect([...tasks.values()].filter(task=>task.signal.aborted)).toHaveLength(1)
    expect(read).toHaveBeenCalledTimes(4)
    tasks.get('0.png')!.finish('cancelled')
    await vi.waitFor(()=>expect(tasks.has('4.png')).toBe(true))
    loader.dispose();for(const task of tasks.values())task.finish('late')
  })

  it('also evicts offscreen resources by UTF-16 byte quota even below the entry quota',async()=>{
    const read=vi.fn(async(path:string,_signal:AbortSignal,_edge:number)=>path+':'+('a'.repeat(previewCacheLimits.bytes/4)))
    const {loader,demand,idle}=setup(5,read)
    for(let index=0;index<4;index++){demand(index);await idle()}
    demand(0);await idle()
    expect(read.mock.calls.filter(([path])=>path==='0.png')).toHaveLength(2)
    loader.dispose()
  })

  it('upgrades a cached thumbnail once the displayed resolution grows, without rereading on minor resizes',async()=>{
    const {loader,read,updates,demand,idle}=setup()
    demand(0,120);await idle()
    demand(0,900);await idle()
    expect(updates.at(-1)?.url).toBe('0.png@1024')
    demand(0,901);await idle()
    expect(read).toHaveBeenCalledTimes(2)
    loader.dispose()
  })

  it('finishes a requested larger resolution after a small read was already in flight',async()=>{
    let release:(url:string)=>void=()=>{}
    const read=vi.fn((path:string,_signal:AbortSignal,edge:number)=>edge===128?new Promise<string>(resolve=>{release=resolve}):Promise.resolve(`${path}@${edge}`))
    const {loader,demand,idle,updates}=setup(1,read)
    demand(0,100);demand(0,1000);release('0.png@128');await idle()
    expect(read).toHaveBeenCalledTimes(2)
    expect(updates.at(-1)?.url).toBe('0.png@1024')
    loader.dispose()
  })

  it('cancels disposal and cannot deliver or retain the old frame result',async()=>{
    let release:(url:string)=>void=()=>{}
    const read=vi.fn((_path:string,_signal:AbortSignal,_edge:number)=>new Promise<string>(resolve=>{release=resolve}))
    const {loader,demand,updates}=setup(1,read)
    demand(0);const signal=read.mock.calls[0]![1];loader.dispose();release('old')
    await Promise.resolve();await Promise.resolve()
    expect(signal.aborted).toBe(true)
    expect(updates.some(update=>update.state==='loaded')).toBe(false)
    demand(0);expect(read).toHaveBeenCalledTimes(1)
  })
})
