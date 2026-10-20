import {afterEach,beforeEach,expect,it,vi} from 'vitest'
import {DockerWorkspaceRuntime} from '../src/shared/workspace/runtime'
const {authFetch}=vi.hoisted(()=>({authFetch:vi.fn()}))
vi.mock('../src/app/auth',()=>({authFetch,loginSession:vi.fn(),expiredLogin:vi.fn()}))
const root={id:'root',title:'Root',status:'idle',logicalCount:0,createdAt:'2026-01-01',queue:[],archived:false}
const child={...root,id:'child',parentTaskId:'root'}
const kept={...root,id:'kept',title:'Keep',status:'waiting',pendingInteraction:{id:'approval',kind:'approval',title:'Keep approval'}}
const json=(value:unknown)=>new Response(JSON.stringify(value),{headers:{'Content-Type':'application/json'}})
let tasks:object[],streams:Stream[],cursor:number
class Stream extends EventTarget {
  onmessage?: (event:{data:string})=>void
  constructor(readonly url:string){super();streams.push(this)}
  close(){}
  send(value:object){this.onmessage?.({data:JSON.stringify(value)})}
}
beforeEach(()=>{
  tasks=[root,child,kept];streams=[];cursor=1
  vi.stubGlobal('EventSource',Stream)
  authFetch.mockImplementation(async(path:string)=>{
    if(path==='/api/runtime')return json({state:'ready'})
    if(path==='/api/tasks')return json({tasks,cursor})
    if(path.endsWith('/delete'))return json({deletedTaskIds:['root','child'],seq:10})
    if(path.endsWith('/root'))return json({task:root,cursor})
    throw new Error(path)
  })
})
afterEach(()=>vi.unstubAllGlobals())
it('removes descendants and caches across SSE while retaining another conversation draft',async()=>{
  const owner=await DockerWorkspaceRuntime.connect(false)
  try {
    owner.ensureRuntime('root').setDraftText('discard')
    owner.ensureRuntime('kept').setDraftText('keep draft')
    streams[0]!.send({seq:10,type:'deleted',deletedTaskIds:['root','child']})
    await vi.waitFor(()=>expect(owner.hasSession('root')).toBe(false))
    expect(owner.hasSession('child')).toBe(false)
    expect(owner.hotSessionIds).toEqual(['kept'])
    expect(owner.ensureRuntime('kept').draftText).toBe('keep draft')
    streams[0]!.send({seq:11,type:'created',taskId:'root',task:root})
    await Promise.resolve();await Promise.resolve()
    expect(owner.hasSession('root')).toBe(false)
  }finally{owner.dispose()}
})
it('does not resurrect a deleted conversation from a late HTTP snapshot',async()=>{
  const owner=await DockerWorkspaceRuntime.connect(false)
  try {
    let resolve!:(value:Response)=>void
    authFetch.mockImplementationOnce(()=>new Promise<Response>(done=>{resolve=done}))
    const refresh=owner.refreshTask('root').catch(error=>error)
    await owner.deleteSession('root')
    resolve(json({task:root,cursor:1}))
    expect((await refresh).status).toBe(404)
    expect(owner.hasSession('root')).toBe(false)
    expect(authFetch).toHaveBeenCalledWith('/api/tasks/root/delete',expect.objectContaining({body:'{"confirmed":true}'}))
  }finally{owner.dispose()}
})
it('reconciles deleted tasks when their deletion event has expired',async()=>{
  const owner=await DockerWorkspaceRuntime.connect(false)
  try {
    owner.ensureRuntime('root')
    owner.ensureRuntime('kept').setDraftText('keep draft')
    tasks=[kept];cursor=100
    streams[0]!.dispatchEvent(new Event('reset'))
    await vi.waitFor(()=>expect(owner.hasSession('root')).toBe(false))
    expect(owner.hasSession('child')).toBe(false)
    expect(owner.ensureRuntime('kept').draftText).toBe('keep draft')
  }finally{owner.dispose()}
})
it('retains history and drafts after a failed delete request',async()=>{
  const owner=await DockerWorkspaceRuntime.connect(false)
  try {
    owner.ensureRuntime('root').setDraftText('keep')
    authFetch.mockRejectedValueOnce(new Error('native unavailable'))
    await expect(owner.deleteSession('root')).rejects.toThrow('native unavailable')
    expect(owner.tasks.size).toBe(3)
    expect(owner.ensureRuntime('root').draftText).toBe('keep')
  }finally{owner.dispose()}
})
it('does not mistake a new conversation created during reset for a deletion',async()=>{
  const owner=await DockerWorkspaceRuntime.connect(false)
  try {
    let resolve!:(value:Response)=>void
    authFetch.mockImplementationOnce(()=>new Promise<Response>(done=>{resolve=done}))
    streams[0]!.dispatchEvent(new Event('reset'))
    await Promise.resolve()
    authFetch.mockResolvedValueOnce(json({...root,id:'new'}))
    await owner.createSession()
    resolve(json({tasks:[root,child,kept],cursor:100}))
    await vi.waitFor(()=>expect(streams[1]?.url).toContain('since=100'),{timeout:2000})
    expect(owner.hasSession('new')).toBe(true)
  }finally{owner.dispose()}
})
it('resynchronizes when queued events need history already deleted by another client',async()=>{
  tasks=[{...root,logicalCount:1},kept]
  const owner=await DockerWorkspaceRuntime.connect(false)
  try {
    tasks=[kept];cursor=10
    const original=authFetch.getMockImplementation()!
    authFetch.mockImplementation((path:string,...args:unknown[])=>path.startsWith('/api/tasks/root/messages')
      ? Promise.resolve(new Response(JSON.stringify({error:'Task not found'}),{status:404}))
      : original(path,...args))
    streams[0]!.send({seq:2,type:'message',taskId:'root',message:{id:'old',index:0,role:'user',blocks:[],revision:1}})
    await vi.waitFor(()=>expect(streams[1]?.url).toContain('since=10'),{timeout:2000})
    expect(owner.hasSession('root')).toBe(false)
    expect(owner.hasSession('kept')).toBe(true)
    expect(owner.error).toBeNull()
  }finally{owner.dispose()}
})
it('does not reuse a deleted default conversation that never became active',async()=>{
  tasks=[]
  const owner=await DockerWorkspaceRuntime.connect(false)
  try {
    authFetch.mockResolvedValueOnce(json(root))
    expect(await owner.ensureDefaultSession()).toBe('root')
    streams[0]!.send({seq:2,type:'deleted',deletedTaskIds:['root']})
    await vi.waitFor(()=>expect(owner.hasSession('root')).toBe(false))
    authFetch.mockResolvedValueOnce(json({...root,id:'replacement'}))
    expect(await Promise.all([owner.ensureDefaultSession(),owner.ensureDefaultSession()])).toEqual(['replacement','replacement'])
    expect(owner.tasks.size).toBe(1)
  }finally{owner.dispose()}
})
