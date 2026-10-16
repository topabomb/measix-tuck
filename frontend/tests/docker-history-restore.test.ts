import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { DockerWorkspaceRuntime } from '../src/shared/workspace/runtime'

const { authFetch } = vi.hoisted(() => ({ authFetch: vi.fn<(path:string)=>Promise<Response>>() }))
vi.mock('../src/app/auth', () => ({ authFetch, loginSession:vi.fn(), expiredLogin:vi.fn() }))

const task = {id:'history',title:'History',status:'idle',logicalCount:5000,createdAt:'2026-01-01',queue:[]}
const json = (value:unknown) => new Response(JSON.stringify(value),{headers:{'Content-Type':'application/json'}})
let ranges:{start:number;count:number}[]
beforeEach(() => {
  ranges=[]
  vi.stubGlobal('EventSource',class {close(){}})
  authFetch.mockImplementation(async path => {
    if(path==='/api/runtime')return json({state:'ready'})
    if(path==='/api/tasks')return json({tasks:[task],cursor:0})
    const url=new URL(path,'https://tuck.test')
    if(url.pathname===`/api/tasks/${task.id}/messages`){
      const start=Number(url.searchParams.get('start')),count=Number(url.searchParams.get('count'))
      ranges.push({start,count})
      return json({messages:Array.from({length:count},(_,offset)=>{
        const index=start+offset
        return {id:`history:m-${index}`,index,role:'assistant',turnId:'turn',blocks:[{id:`p-${index}`,type:'markdown',data:{markdown:`Message ${index}`}}]}
      })})
    }
    throw new Error(`Unexpected request ${path}`)
  })
})
afterEach(() => vi.unstubAllGlobals())

describe('Docker history preparation and cold materialization', () => {
  it('keeps the newest orchestration state across replayed events and delayed refreshes',async()=>{
    let stream:{onmessage?:(event:{data:string})=>void}|undefined
    vi.stubGlobal('EventSource',class {constructor(){stream=this}onmessage?: (event:{data:string})=>void;close(){}})
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
