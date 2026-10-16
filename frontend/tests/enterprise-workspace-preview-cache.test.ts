import {describe,it,expect,vi} from 'vitest'
import {previewImages,previewCacheLimits,type ImageProgress,type ImageUpdate} from '../src/shared/enterprise-workspace/preview-images'

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
