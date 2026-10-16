import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createEnterpriseWorkspace } from '../src/shared/enterprise-workspace/state'

const { authFetch }=vi.hoisted(()=>({authFetch:vi.fn<(path:string,options?:RequestInit)=>Promise<Response>>()}))
vi.mock('../src/app/auth',()=>({authFetch,csrfHeader:()=>({}),expiredLogin:vi.fn()}))
const projection={agentSpaceId:'spc_11111111-1111-1111-1111-111111111111',subject:'cache-account',employeeName:'Tester',filesAvailable:true,filesReason:'',state:'CONNECTED'}
const json=(value:unknown)=>new Response(JSON.stringify(value),{headers:{'Content-Type':'application/json'}})
const paths=(owner:ReturnType<typeof createEnterpriseWorkspace>)=>owner.state.tabs.map(doc=>doc.path)
function deferred<T>(){let resolve!:(value:T)=>void;const promise=new Promise<T>(done=>{resolve=done});return{promise,resolve}}
function filesFixture(value='base') {
  const files=new Map<string,string>(),reads=new Map<string,number>()
  authFetch.mockImplementation(async(resource,options={})=>{
    const url=new URL(resource,'https://tuck.test'),path=url.searchParams.get('path')||''
    if(url.pathname==='/api/enterprise-workspace')return json(projection)
    if(url.pathname.endsWith('/files'))return json({entries:[]})
    if(options.method==='HEAD')return new Response(null,{headers:{etag:'"version"','Content-Length':String((files.get(path)||value).length)}})
    if(options.method==='PUT'){files.set(path,new TextDecoder().decode(options.body as Uint8Array));return json({outcome:'SUCCEEDED'})}
    reads.set(path,(reads.get(path)||0)+1)
    return new Response(files.get(path)||value,{headers:{etag:'"version"'}})
  })
  return{files,reads}
}
beforeEach(()=>{authFetch.mockReset();vi.stubGlobal('document',{baseURI:'https://tuck.test/'})})
afterEach(()=>vi.unstubAllGlobals())

describe('enterprise workspace tab cache',()=>{
  it('keeps twelve tabs and evicts the least recently used clean tab; revisits use cached text',async()=>{
    const {reads}=filesFixture(),owner=createEnterpriseWorkspace();await owner.refresh()
    for(let i=0;i<12;i++)await owner.open(`file-${i}.txt`)
    await owner.open('file-0.txt')
    expect(reads.get('file-0.txt')).toBe(1)
    await owner.open('file-12.txt')
    expect(owner.state.tabs).toHaveLength(12)
    expect(paths(owner)).toContain('file-0.txt');expect(paths(owner)).not.toContain('file-1.txt')
    expect(owner.state.active).toBe('file-12.txt')
    await owner.open('file-1.txt')
    expect(reads.get('file-1.txt')).toBe(2)
    expect(paths(owner)).not.toContain('file-2.txt')
  })

  it('refuses a new tab without changing active, tabs or requests when drafts and the current tab occupy all slots',async()=>{
    filesFixture();const owner=createEnterpriseWorkspace();await owner.refresh()
    for(let i=0;i<12;i++){await owner.open(`file-${i}.txt`);if(i<11)owner.state.tabs.at(-1)!.text=`draft-${i}`}
    const before=owner.state.tabs,ids=before.map(doc=>doc.id),calls=authFetch.mock.calls.length
    await expect(owner.open('blocked.txt')).rejects.toThrow('请保存或关闭部分标签后重试')
    expect(owner.state.active).toBe('file-11.txt');expect(owner.state.tabs).toBe(before)
    expect(owner.state.tabs.map(doc=>doc.id)).toEqual(ids);expect(authFetch.mock.calls).toHaveLength(calls)
    expect(owner.state.tabs.slice(0,11).every(doc=>doc.text.startsWith('draft-'))).toBe(true)
  })

  it('reserves slots synchronously and never evicts a tab whose content is still loading',async()=>{
    filesFixture();const normal=authFetch.getMockImplementation()!,pending=new Map<string,ReturnType<typeof deferred<Response>>>()
    authFetch.mockImplementation((resource,options={})=>{
      const url=new URL(resource,'https://tuck.test'),path=url.searchParams.get('path')||''
      if(url.pathname.endsWith('/content')&&!options.method&&path.startsWith('slow-')){const read=deferred<Response>();pending.set(path,read);return read.promise}
      return normal(resource,options)
    })
    const owner=createEnterpriseWorkspace();await owner.refresh()
    const opening=Array.from({length:12},(_,i)=>owner.open(`slow-${i}.txt`))
    await vi.waitFor(()=>expect(pending.size).toBe(12))
    await expect(owner.open('blocked.txt')).rejects.toThrow('文件标签已满')
    expect(owner.state.active).toBe('slow-11.txt');expect(owner.state.tabs).toHaveLength(12)
    for(const read of pending.values())read.resolve(new Response('finished',{headers:{etag:'"version"'}}))
    await Promise.all(opening)
    await owner.open('next.txt')
    expect(paths(owner)).not.toContain('slow-0.txt');expect(paths(owner)).toContain('slow-11.txt')
  })

  it('bounds completed clean text by UTF-16 bytes and counts identical text/original only once',async()=>{
    filesFixture('x'.repeat(2*1024*1024));const owner=createEnterpriseWorkspace();await owner.refresh()
    for(let i=0;i<6;i++)await owner.open(`large-${i}.txt`)
    expect(owner.state.tabs).toHaveLength(6) // Six 2 Mi-character strings = 24 MiB.
    await owner.open('large-0.txt')
    await owner.open('large-6.txt')
    expect(owner.state.tabs).toHaveLength(6)
    expect(paths(owner)).not.toContain('large-1.txt');expect(paths(owner)).toContain('large-0.txt')
    expect(owner.state.active).toBe('large-6.txt')
  })

  it('counts different draft and original bodies separately but never evicts a draft',async()=>{
    const body='x'.repeat(2*1024*1024);filesFixture(body)
    const owner=createEnterpriseWorkspace();await owner.refresh()
    for(let i=0;i<5;i++)await owner.open(`large-${i}.txt`)
    const draft=owner.state.tabs[0]!
    draft.text='y'+body.slice(1) // Draft+original adds 4 MiB, reaching 24 MiB.
    await owner.open('large-5.txt')
    expect(owner.state.tabs).toHaveLength(5)
    expect(paths(owner)).toContain(draft.path);expect(paths(owner)).not.toContain('large-1.txt')
    expect(draft.text).toBe('y'+body.slice(1));expect(draft.original).toBe(body)
  })

  it('converges the text cache after saving while preserving another unsaved draft',async()=>{
    const body='x'.repeat(2*1024*1024);filesFixture(body)
    const owner=createEnterpriseWorkspace();await owner.refresh()
    for(let i=0;i<6;i++)await owner.open(`large-${i}.txt`)
    const saved=owner.state.tabs[0]!,draft=owner.state.tabs[1]!
    saved.text='s'+body.slice(1);draft.text='d'+body.slice(1) // Temporarily 32 MiB: drafts are protected.
    await owner.save(saved)
    expect(saved.original).toBe(saved.text);expect(draft.original).toBe(body)
    expect(paths(owner)).toContain(draft.path);expect(owner.state.active).toBe('large-5.txt')
    expect(owner.state.tabs.reduce((bytes,doc)=>bytes+2*(doc.text.length+(doc.text===doc.original?0:doc.original.length)),0)).toBeLessThanOrEqual(24*1024*1024)
  })

  it('protects a clean save-as in flight even after switching away and opening another file',async()=>{
    filesFixture();const normal=authFetch.getMockImplementation()!,written=deferred<Response>()
    authFetch.mockImplementation(async(resource,options={})=>{
      const response=await normal(resource,options)
      return options.method==='PUT'?written.promise:response
    })
    const owner=createEnterpriseWorkspace();await owner.refresh()
    for(let i=0;i<12;i++)await owner.open(`file-${i}.txt`)
    const original=owner.state.tabs[0]!,saving=owner.save(original,'copy.txt')
    await vi.waitFor(()=>expect(authFetch.mock.calls.some(([,options])=>options?.method==='PUT')).toBe(true))
    await owner.open('new.txt')
    expect(paths(owner)).toContain('file-0.txt');expect(paths(owner)).not.toContain('file-1.txt')
    written.resolve(json({outcome:'SUCCEEDED'}));await saving
    expect(paths(owner)).toContain('copy.txt');expect(owner.state.active).toBe('new.txt')
  })

  it('keeps protected drafts above the soft byte budget and clears cache ownership on identity reset',async()=>{
    filesFixture();const owner=createEnterpriseWorkspace();await owner.refresh()
    for(let i=0;i<8;i++){await owner.open(`draft-${i}.txt`);owner.state.tabs.at(-1)!.text='x'.repeat(2*1024*1024)}
    await owner.open('draft-0.txt')
    expect(owner.state.tabs).toHaveLength(8);expect(owner.dirty()).toBe(true)
    owner.reset();owner.state.projection={...projection,subject:'another-account'}
    await owner.open('fresh.txt')
    expect(paths(owner)).toEqual(['fresh.txt']);expect(owner.state.active).toBe('fresh.txt')
  })
})
