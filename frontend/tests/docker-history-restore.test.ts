import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { DockerWorkspaceRuntime } from '../src/shared/workspace/runtime'

const { authFetch } = vi.hoisted(() => ({ authFetch: vi.fn<(path:string)=>Promise<Response>>() }))
vi.mock('../src/app/auth', () => ({ authFetch, loginSession:vi.fn(), expiredLogin:vi.fn() }))

const task = {id:'history',title:'History',status:'idle',logicalCount:5000,createdAt:'2026-01-01',queue:[]}
const json = (value:unknown) => new Response(JSON.stringify(value),{headers:{'Content-Type':'application/json'}})
let ranges:{start:number;count:number}[]
let snapshotTask: Record<string, unknown>, cursor: number, revision: number
let streams: TestEventSource[]
class TestEventSource extends EventTarget {
  closed = false
  onmessage?: (event: {data:string}) => void
  constructor(readonly url:string) { super(); streams.push(this) }
  close() { this.closed = true }
}
beforeEach(() => {
  ranges=[]
  snapshotTask={...task}; cursor=0; revision=0; streams=[]
  vi.stubGlobal('EventSource',TestEventSource)
  authFetch.mockImplementation(async path => {
    if(path==='/api/runtime')return json({state:'ready'})
    if(path==='/api/tasks')return json({tasks:[snapshotTask],cursor})
    if(path===`/api/tasks/${task.id}`)return json({task:snapshotTask,cursor})
    const url=new URL(path,'https://tuck.test')
    if(url.pathname===`/api/tasks/${task.id}/messages`){
      const start=Number(url.searchParams.get('start')),count=Number(url.searchParams.get('count'))
      ranges.push({start,count})
      return json({messages:Array.from({length:count},(_,offset)=>{
        const index=start+offset
        return {id:`history:m-${index}`,index,revision,role:'assistant',turnId:'turn',blocks:[{id:`p-${index}`,revision,type:'markdown',data:{markdown:`Message ${index} revision ${revision}`}}]}
      })})
    }
    throw new Error(`Unexpected request ${path}`)
  })
})
afterEach(() => vi.unstubAllGlobals())

describe('Docker history preparation and cold materialization', () => {
  it.each([true,false])('resets expired replay while preserving the live view and draft (tail=%s)', async tail => {
    const workspace=await DockerWorkspaceRuntime.connect(true)
    try {
      if (!tail) {
        await workspace.prepareMessage(task.id,42)
        workspace.targetMessage(task.id,42)
        workspace.activate(task.id)
      }
      const runtime=workspace.activeSession, range=runtime.range
      runtime.setDraftText('unsent draft')
      const pending={id:'approval',kind:'approval',title:'Allow tool?',detail:'shell'}
      snapshotTask={...task,logicalCount:5002,status:'waiting',pendingInteraction:pending}
      cursor=1001; revision=7; ranges=[]
      streams[0]!.dispatchEvent(new Event('reset'))
      await vi.waitFor(()=>expect(workspace.activeKernel.count).toBe(5002))
      expect(workspace.activeSession).toBe(runtime)
      expect(runtime.draftText).toBe('unsent draft')
      expect(runtime.followTail).toBe(tail)
      expect(runtime.range).toEqual(tail?{start:4874,end:5002}:range)
      expect(runtime.currentLogicalPosition).toBe(tail?5001:42)
      expect(workspace.activeKernel.pendingInteraction?.id).toBe('approval')
      expect(workspace.activeKernel.getMessage(tail?5001:42).revision).toBe(7)
      expect(ranges.reduce((sum,r)=>sum+r.count,0)).toBeLessThanOrEqual(128)
      await vi.waitFor(()=>expect(streams[1]?.url).toContain('since=1001'),{timeout:2000})
      // An older replayed state cannot resurrect a resolved/changed blocker.
      streams[1]!.onmessage!({data:JSON.stringify({seq:1000,type:'state',taskId:task.id,task:{...task,status:'idle'}})})
      await Promise.resolve(); await Promise.resolve()
      expect(workspace.activeKernel.pendingInteraction?.id).toBe('approval')
    } finally { workspace.dispose() }
  })
  it('keeps the old projection and cursor when reset snapshot loading fails, then retries', async () => {
    const workspace=await DockerWorkspaceRuntime.connect(true)
    try {
      const runtime=workspace.activeSession
      runtime.setDraftText('keep me')
      authFetch.mockRejectedValueOnce(new Error('snapshot unavailable'))
      streams[0]!.dispatchEvent(new Event('reset'))
      await vi.waitFor(()=>expect(workspace.error).toBe('snapshot unavailable'))
      expect(workspace.activeSession).toBe(runtime)
      expect(runtime.draftText).toBe('keep me')
      await vi.waitFor(()=>expect(streams[1]?.url).toContain('since=0'),{timeout:2000})
      cursor=123
      streams[1]!.dispatchEvent(new Event('reset'))
      await vi.waitFor(()=>expect(workspace.error).toBeNull())
      expect(workspace.activeSession).toBe(runtime)
    } finally { workspace.dispose() }
  })
  it('does not overwrite a newer HTTP approval resolution or edits made during a reset read', async () => {
    const workspace=await DockerWorkspaceRuntime.connect(true)
    let release=()=>{}
    try {
      const original=authFetch.getMockImplementation()!
      const held=new Promise<void>(resolve=>{release=resolve})
      let reading=false,hold=true
      authFetch.mockImplementation(async path=>{
        if(hold&&path.includes('/messages?')){reading=true;await held}
        return original(path)
      })
      snapshotTask={...task,status:'waiting',pendingInteraction:{id:'old',kind:'approval',title:'Old approval'}}
      cursor=100
      streams[0]!.dispatchEvent(new Event('reset'))
      await vi.waitFor(()=>expect(reading).toBe(true))
      workspace.activeSession.setDraftText('typed while loading')
      snapshotTask={...task,logicalCount:5001,lastTurnReason:'completed',pendingInteraction:null}
      cursor=105
      await workspace.refreshTask(task.id)
      hold=false;release()
      await vi.waitFor(()=>expect(workspace.activeKernel.count).toBe(5001))
      expect(workspace.activeKernel.status).toBe('idle')
      expect(workspace.activeKernel.pendingInteraction).toBeNull()
      expect(workspace.activeSession.draftText).toBe('typed while loading')
      await vi.waitFor(()=>expect(streams[1]?.url).toContain('since=100'),{timeout:2000})
    } finally { release(); workspace.dispose() }
  })
  it('finishes in-flight history navigation against the replacement source', async () => {
    const workspace=await DockerWorkspaceRuntime.connect(true)
    let release=()=>{}
    try {
      const original=authFetch.getMockImplementation()!
      const held=new Promise<void>(resolve=>{release=resolve})
      let reading=false,hold=true
      authFetch.mockImplementation(async path=>{
        const url=new URL(path,'https://tuck.test')
        if(hold&&url.pathname.endsWith('/messages')&&Number(url.searchParams.get('start'))<2000){reading=true;await held}
        return original(path)
      })
      const navigating=workspace.prepareMessage(task.id,1500)
      await vi.waitFor(()=>expect(reading).toBe(true))
      snapshotTask={...task,logicalCount:5001};cursor=100
      streams[0]!.dispatchEvent(new Event('reset'))
      await vi.waitFor(()=>expect(workspace.activeKernel.count).toBe(5001))
      hold=false;release()
      await navigating
      workspace.targetMessage(task.id,1500)
      const runtime=workspace.activate(task.id)
      expect(runtime.currentLogicalPosition).toBe(1500)
      expect(runtime.activeUnits.some(unit=>unit.messageIndex===1500)).toBe(true)
    } finally { release(); workspace.dispose() }
  })
  it('keeps the newest orchestration state across replayed events and delayed refreshes',async()=>{
    let stream:{onmessage?:(event:{data:string})=>void}|undefined
    vi.stubGlobal('EventSource',class {constructor(){stream=this}onmessage?: (event:{data:string})=>void;close(){}addEventListener(){}})
    const workspace=await DockerWorkspaceRuntime.connect(false)
    try{
      workspace.runtimeInfo.orchestration={requireDelivery:false,updatedAt:20}
      stream!.onmessage!({data:JSON.stringify({seq:1,type:'orchestration',orchestration:{requireDelivery:true,updatedAt:10}})})
      await Promise.resolve();await Promise.resolve()
      expect(workspace.runtimeInfo.orchestration).toEqual({requireDelivery:false,updatedAt:20})
      authFetch.mockResolvedValueOnce(json({state:'ready',orchestration:{requireDelivery:true,updatedAt:15}}))
      await workspace.refreshConnection()
      expect(workspace.runtimeInfo.orchestration?.requireDelivery).toBe(false)
      stream!.onmessage!({data:JSON.stringify({seq:2,type:'orchestration',orchestration:{requireDelivery:true,updatedAt:21}})})
      await Promise.resolve();await Promise.resolve()
      expect(workspace.runtimeInfo.orchestration?.requireDelivery).toBe(true)
    }finally{workspace.dispose()}
  })
  it('keeps initial tail preparation bounded to the last window',async()=>{
    const workspace=await DockerWorkspaceRuntime.connect(true)
    try{
      expect(workspace.activeSession.followTail).toBe(true)
      expect(workspace.activeSession.range).toEqual({start:4872,end:5000})
      expect(ranges).toEqual([{start:4872,count:64},{start:4936,count:64}])
    }finally{workspace.dispose()}
  })

  it.each([0,42,2500,4999])('prepares the complete cold deep-link window at %s',async index=>{
    const workspace=await DockerWorkspaceRuntime.connect(false)
    try{
      await workspace.prepareMessage(task.id,index)
      workspace.targetMessage(task.id,index)
      const runtime=workspace.activate(task.id)
      expect(runtime.activeUnits.some(unit=>unit.messageIndex===index)).toBe(true)
      expect(runtime.activeUnits).toHaveLength(128)
      expect(ranges).toEqual([{start:runtime.range.start,count:64},{start:runtime.range.start+64,count:64}])
      expect(runtime.currentLogicalPosition).toBe(index)
      workspace.suspendView()
      await workspace.prepareSession(task.id)
      expect(workspace.activate(task.id).range).toEqual(runtime.range)
    }finally{workspace.dispose()}
  })
})
