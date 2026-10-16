import { describe, expect, it } from 'vitest'
import { CachedHistory } from '../src/shared/workspace/history-cache'
import { CHAT_HISTORY } from '../src/shared/workspace/history-policy'
import { ConversationSessionKernel } from '../src/engine/conversation/session-kernel'
import type { LogicalMessage } from '../src/engine/model/conversation'

const message = (index: number, revision = 0, text = `Message ${index}`): LogicalMessage => ({ id:`cache:m-${index}`, index, role:'assistant', turnId:'turn', revision, blocks:[{id:`p-${index}`,type:'markdown',data:{markdown:text}}] })
function history(total = 6000) {
  const requests: { start:number; count:number }[] = []
  const retained: { range?:{start:number;end:number}; activeIndex?:number } = {}
  const cache = new CachedHistory('cache', total, async (start,count) => {
    requests.push({start,count}); await Promise.resolve()
    return Array.from({length:count},(_,offset)=>message(start+offset))
  },()=>retained)
  return {cache,requests,retained}
}

describe('persisted workbench history cache',()=>{
  it('deduplicates overlapping reads and fetches only newly exposed records',async()=>{
    const {cache,requests}=history()
    await Promise.all([cache.prepareView(5872,128),cache.prepareView(5872,128)])
    expect(requests).toEqual([{start:5872,count:64},{start:5936,count:64}])
    await cache.prepareView(5808,192)
    expect(requests.at(-1)).toEqual({start:5808,count:64})
    expect(cache.loadRange(5808,192)).toHaveLength(192)
  })

  it('keeps a later canonical revision when an earlier HTTP read finishes',async()=>{
    let complete!:(messages:LogicalMessage[])=>void
    const cache = new CachedHistory('cache',1,()=>new Promise(resolve=>complete=resolve),()=>({}))
    const read=cache.prepare(0,1)
    cache.storeCanonicalMessage(message(0,3,'newer SSE content'))
    complete([message(0,1,'old HTTP content')]); await read
    expect(cache.loadRange(0,1)[0]?.revision).toBe(3)
  })

  it('retains a partially successful read and retries only missing records after failure',async()=>{
    let fail=true
    const requests:{start:number;count:number}[]=[]
    const cache=new CachedHistory('cache',128,async(start,count)=>{
      requests.push({start,count})
      if(start===64&&fail)throw new Error('offline')
      return Array.from({length:count},(_,offset)=>message(start+offset))
    },()=>({}))
    await expect(cache.prepareView(0,128)).rejects.toThrow('offline')
    fail=false; await cache.prepareView(0,128)
    expect(requests).toEqual([{start:0,count:64},{start:64,count:64},{start:64,count:64}])
    expect(cache.loadRange(0,128)).toHaveLength(128)
  })

  it('bounds fetched and live bodies together and releases cold bodies without losing logical count',async()=>{
    const {cache,retained}=history(128)
    await cache.prepareView(0,128)
    const kernel=new ConversationSessionKernel({id:'cache',title:'Cache',status:'idle',logicalCount:128},cache)
    for(let index=128;index<1500;index++)kernel.appendCanonicalMessages([message(index)])
    expect(kernel.count).toBe(1500)
    expect(cache.cachedMessages).toBeLessThanOrEqual(CHAT_HISTORY.cacheMessages)
    retained.activeIndex=1498; cache.release()
    expect(cache.cachedMessages).toBeLessThanOrEqual(2)
    await cache.prepareView(0,128)
    expect(kernel.getMessage(0).id).toBe('cache:m-0')
    expect(kernel.count).toBe(1500)
  })

  it('accounts a growing stream incrementally and permits required oversized content',async()=>{
    const {cache}=history(0)
    const oversized='x'.repeat(CHAT_HISTORY.cacheBytes)
    cache.storeCanonicalMessage(message(0,0,oversized))
    const before=cache.estimatedBytes
    cache.storeCanonicalMessage(message(0,1,oversized+'more'),'more')
    expect(cache.estimatedBytes-before).toBe(8)
    expect(cache.loadRange(0,1)[0]?.blocks[0]?.data).toEqual({markdown:oversized+'more'})
  })

  it('does not resurrect a released view cache when its HTTP response arrives',async()=>{
    let complete!:(messages:LogicalMessage[])=>void
    const cache=new CachedHistory('cache',500,()=>new Promise(resolve=>complete=resolve),()=>({}))
    const read=cache.prepareView(100,64)
    cache.release()
    complete(Array.from({length:64},(_,offset)=>message(100+offset)));await read
    expect(cache.cachedMessages).toBe(0)
    expect(cache.count).toBe(500)
  })

  it('evicts old unprotected heavy bodies under the byte budget',async()=>{
    const text='x'.repeat(1024*1024)
    const cache=new CachedHistory('cache',1000,async(start,count)=>Array.from({length:count},(_,offset)=>message(start+offset,0,text)),()=>({}))
    for(let index=0;index<20;index++)await cache.prepareView(index,1)
    expect(cache.estimatedBytes).toBeLessThanOrEqual(CHAT_HISTORY.cacheBytes)
    expect(cache.cachedMessages).toBeLessThan(10)
    expect(cache.loadRange(19,1)[0]?.index).toBe(19)
  })
})
