import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createEnterpriseWorkspace } from '../src/shared/enterprise-workspace/state'

const { authFetch } = vi.hoisted(() => ({ authFetch: vi.fn<(path:string, options?:RequestInit)=>Promise<Response>>() }))
vi.mock('../src/app/auth', () => ({ authFetch, csrfHeader: () => ({}), expiredLogin: vi.fn() }))

const projection = { agentSpaceId:'spc_11111111-1111-1111-1111-111111111111', subject:'account-a', employeeName:'Tester', filesAvailable:true, filesReason:'', state:'CONNECTED' }
const json = (value:unknown, status = 200) => new Response(JSON.stringify(value), { status, headers:{'Content-Type':'application/json'} })
const text = (value:string, etag = '"v1"') => new Response(value, { headers:{etag} })
const head = (size = 4) => new Response(null, { headers:{etag:'"v1"', 'Content-Length':String(size)} })
function deferred<T>() {
  let resolve!:(value:T)=>void
  const promise = new Promise<T>(done => { resolve = done })
  return { promise, resolve }
}
function request(path:string, options:RequestInit = {}) {
  const url = new URL(path, 'https://tuck.test')
  return { resource:url.pathname.replace('/api/enterprise-workspace',''), path:url.searchParams.get('path'), method:options.method || 'GET' }
}

beforeEach(() => {
  authFetch.mockReset()
  vi.stubGlobal('document', { baseURI:'https://tuck.test/workbench/' })
})
afterEach(() => vi.unstubAllGlobals())

describe('enterprise workspace state ownership', () => {
  it('reads an unavailable projection for a status summary without enabling file access',async()=>{
    const unavailable={...projection,filesAvailable:false,filesReason:'not_provisioned',state:'UNPROVISIONED'}
    authFetch.mockResolvedValue(json(unavailable))
    const owner=createEnterpriseWorkspace()
    await expect(owner.refresh(undefined,{background:true,requireFiles:false})).resolves.toBeUndefined()
    expect(owner.state.projection).toEqual(unavailable)
    expect(owner.state.projectionReadAt).not.toBeNull()
    await expect(owner.response('/content','file.txt',{method:'HEAD'})).rejects.toThrow('工作区当前不可用')
    authFetch.mockResolvedValue(json(unavailable))
    await expect(owner.refresh()).rejects.toThrow('not_provisioned')
  })

  it('distinguishes a failed first directory read from a confirmed empty directory',async()=>{
    let fail=true
    authFetch.mockImplementation(async(path,options)=>request(path,options).resource ? fail?json({error:'文件路径无效',code:'invalid_file_path'},400):json({entries:[]}) : json(projection))
    const owner=createEnterpriseWorkspace();await owner.refresh()
    await expect(owner.list()).rejects.toThrow('无法读取目录“根目录”')
    expect(owner.state.directoryLoaded).toBe(false)
    expect(owner.state.error).toContain('企业文件服务不允许读取此目录')
    fail=false;await owner.list()
    expect(owner.state.directoryLoaded).toBe(true);expect(owner.state.entries).toEqual([]);expect(owner.state.error).toBe('')
  })

  it('preserves the last directory on failed navigation and clears stale refresh feedback after success',async()=>{
    const entries=[{path:'folder/file.txt',kind:'FILE',etag:'"v1"'}]
    authFetch.mockImplementation(async(path,options)=>{
      const call=request(path,options)
      if(!call.resource)return json(projection)
      return call.path==='missing'?json({error:'文件已不存在',code:'file_not_found'},404):json({entries})
    })
    const owner=createEnterpriseWorkspace();await owner.refresh();await owner.list('folder')
    const original=owner.state.entries
    await expect(owner.list('missing')).rejects.toThrow('无法读取目录“missing”')
    expect(owner.state.dir).toBe('folder');expect(owner.state.entries).toBe(original);expect(owner.state.directoryLoaded).toBe(true)
    owner.state.refreshError='自动刷新失败：旧目录'
    await owner.list('folder')
    expect(owner.state.refreshError).toBe('');expect(owner.state.error).toBe('')
  })

  it('ignores an obsolete startup failure after a successful newer projection',async()=>{
    const delayed=deferred<Response>();let reads=0
    authFetch.mockImplementation(async()=>++reads===1?delayed.promise:json(projection))
    const owner=createEnterpriseWorkspace(),old=owner.refresh()
    const obsolete=expect(old).rejects.toMatchObject({name:'AbortError'})
    await owner.refresh(projection.agentSpaceId)
    delayed.resolve(json({error:'old startup failure'},503));await obsolete
    expect(owner.state.projection?.filesAvailable).toBe(true)
    expect(owner.state.error).toBe('')
  })

  it.each([false,true])('ignores an older projection after newer navigation, wrong space = %s',async wrong=>{
    const delayed=deferred<Response>();let reads=0
    authFetch.mockImplementation(async()=>++reads===1?delayed.promise:json(projection))
    const owner=createEnterpriseWorkspace(),old=owner.refresh(wrong?undefined:'other-space')
    const obsolete=expect(old).rejects.toMatchObject({name:'AbortError'})
    if(wrong)await expect(owner.refresh('other-space')).rejects.toThrow('此地址属于其他企业工作区')
    else await owner.refresh(projection.agentSpaceId)
    delayed.resolve(json(projection));await obsolete
    expect(owner.state.projection?.filesAvailable).toBe(!wrong)
    expect(owner.state.projection?.agentSpaceId).toBe(projection.agentSpaceId)
    if(wrong)await expect(owner.response('/content','wrong-target.txt',{method:'HEAD'})).rejects.toThrow('当前不可用')
    expect(reads).toBe(2)
  })

  it('does not save an empty default draft after the first content read fails',async()=>{
    authFetch.mockImplementation(async(path,options)=>{
      const call=request(path,options)
      if(!call.resource)return json(projection)
      if(call.method==='HEAD')return head()
      if(call.method==='PUT')return json({outcome:'SUCCEEDED'})
      return json({error:'failed content read'},503)
    })
    const owner=createEnterpriseWorkspace();await owner.refresh();await owner.open('unread.txt')
    const doc=owner.state.tabs[0]!
    expect(doc.loaded).toBe(false);expect(doc.etag).toBe('"v1"');expect(doc.text).toBe('')
    await expect(owner.save(doc,'copy.txt')).rejects.toThrow('正文尚未读取完成')
    expect(authFetch.mock.calls.some(([,options])=>options?.method==='PUT')).toBe(false)
  })

  it('reports consumed chunks before rejecting an oversized bounded read',async()=>{
    const consumed:number[]=[];let cancelled=false
    authFetch.mockImplementation(async(path,options)=>request(path,options).resource?new Response(new ReadableStream<Uint8Array>({start(controller){controller.enqueue(new Uint8Array(4));controller.enqueue(new Uint8Array(4))},cancel(){cancelled=true}})):json(projection))
    const owner=createEnterpriseWorkspace();await owner.refresh()
    await expect(owner.bounded('large.png',6,{},undefined,false,length=>consumed.push(length))).rejects.toThrow('预览超过大小限制')
    expect(consumed).toEqual([4,4]);expect(cancelled).toBe(true)
  })

  it('reports bytes already consumed when a bounded stream later fails',async()=>{
    const consumed:number[]=[];let pulls=0
    authFetch.mockImplementation(async(path,options)=>request(path,options).resource?new Response(new ReadableStream<Uint8Array>({pull(controller){if(pulls++===0)controller.enqueue(new Uint8Array(3));else controller.error(new Error('truncated stream'))}})):json(projection))
    const owner=createEnterpriseWorkspace();await owner.refresh()
    await expect(owner.bounded('truncated.png',6,{},undefined,false,length=>consumed.push(length))).rejects.toThrow('truncated stream')
    expect(consumed).toEqual([3])
  })

  it('lets foreground reads cancel a slow automatic refresh without changing identity or file feedback', async () => {
    const delayed=deferred<Response>();let pending=false,signal:AbortSignal|undefined
    authFetch.mockImplementation(async(path,options={})=>{
      const call=request(path,options)
      if(!call.resource){if(pending){signal=options.signal as AbortSignal;return delayed.promise}return json(projection)}
      return head()
    })
    const owner=createEnterpriseWorkspace();await owner.refresh()
    owner.state.error='previous file error';owner.state.notice='file operation completed'
    const generation=owner.generation();pending=true
    const refreshing=owner.refreshDirectory()
    await vi.waitFor(()=>expect(signal).toBeDefined())
    expect(owner.state.refreshing).toBe(true);expect(owner.state.busy).toBe(false);expect(owner.state.loading).toBe(false)
    await owner.response('/content','file.txt',{method:'HEAD'})
    expect(signal!.aborted).toBe(true);expect(owner.state.refreshing).toBe(false)
    delayed.resolve(json({...projection,filesAvailable:false,filesReason:'obsolete failure'}))
    expect(await refreshing).toBe(false)
    expect(owner.state.projection?.filesAvailable).toBe(true)
    expect(owner.generation()).toBe(generation)
    expect(owner.state.error).toBe('previous file error');expect(owner.state.notice).toBe('file operation completed');expect(owner.state.refreshError).toBe('')
  })

  it('keeps unchanged directory entries and drafts while refreshing capacity separately', async () => {
    const entries=[{path:'file.txt',kind:'FILE',size:4,etag:'"v1"',modifiedAt:'2026-10-11T00:00:00Z'}];let used=1
    authFetch.mockImplementation(async(path,options)=>request(path,options).resource?json({entries,usedBytes:used,availableBytes:100-used}):json(projection))
    const owner=createEnterpriseWorkspace();await owner.refresh();await owner.list()
    const original=owner.state.entries
    owner.state.error='keep file error';owner.state.notice='keep notice';used=2
    expect(await owner.refreshDirectory()).toBe(true)
    expect(owner.state.entries).toBe(original);expect(owner.state.used).toBe(2);expect(owner.state.available).toBe(98)
    expect(owner.state.error).toBe('keep file error');expect(owner.state.notice).toBe('keep notice')
  })

  it('never lets a cancelled automatic list overwrite newer foreground navigation', async () => {
    const delayed=deferred<Response>();let pending=false,started=false
    authFetch.mockImplementation(async(path,options)=>{
      const call=request(path,options)
      if(!call.resource)return json(projection)
      if(pending&&call.path===''){started=true;return delayed.promise}
      return json({entries:[{path:'folder/new.txt',kind:'FILE',etag:'"new"'}]})
    })
    const owner=createEnterpriseWorkspace();await owner.refresh();pending=true
    const refreshing=owner.refreshDirectory()
    await vi.waitFor(()=>expect(started).toBe(true))
    await owner.list('folder')
    delayed.resolve(json({entries:[{path:'obsolete.txt',kind:'FILE'}]}))
    expect(await refreshing).toBe(false)
    expect(owner.state.dir).toBe('folder');expect(owner.state.entries.map(entry=>entry.path)).toEqual(['folder/new.txt'])
    expect(owner.state.refreshError).toBe('');expect(owner.state.loading).toBe(false)
  })

  it.each(['clip.mp4','sound.wav','document.pdf'])('requires a strong version before previewing %s', async path => {
    let etag='W/"weak"'
    authFetch.mockImplementation(async(resource,options)=>request(resource,options).resource?new Response(null,{headers:{etag}}):json(projection))
    const owner=createEnterpriseWorkspace();await owner.refresh();await owner.open(path)
    const doc=owner.state.tabs[0]!
    expect(doc.loaded).toBe(false);expect(doc.error).toContain('强版本号')
    expect(authFetch.mock.calls.filter(([resource,options])=>request(resource,options).resource==='/content').map(([,options])=>options?.method)).toEqual(['HEAD'])
    etag='"strong"';await owner.open(path)
    expect(doc.loaded).toBe(true);expect(doc.etag).toBe('"strong"');expect(doc.error).toBe('')
  })

  it('loads a reopened tab independently from the closed document still reading the same path', async () => {
    const oldContent = deferred<Response>()
    let reads = 0
    authFetch.mockImplementation(async (path, options) => {
      const call = request(path, options)
      if (!call.resource) return json(projection)
      if (call.method === 'HEAD') return head()
      return ++reads === 1 ? oldContent.promise : text('new content')
    })
    const owner = createEnterpriseWorkspace()
    await owner.refresh()
    const opening = owner.open('notes.txt')
    await vi.waitFor(() => expect(reads).toBe(1))
    const closed = owner.state.tabs[0]!
    owner.state.tabs = []
    owner.state.active = ''
    const reopening = owner.open('notes.txt')
    oldContent.resolve(text('old content'))
    await Promise.all([opening, reopening])
    expect(owner.state.tabs).toHaveLength(1)
    expect(owner.state.tabs[0]!.id).not.toBe(closed.id)
    expect(owner.state.tabs[0]!.loaded).toBe(true)
    expect(owner.state.tabs[0]!.text).toBe('new content')
    expect(reads).toBe(2)
  })

  it('does not start a request when its preview signal was already cancelled', async () => {
    authFetch.mockResolvedValue(json(projection))
    const owner = createEnterpriseWorkspace()
    await owner.refresh()
    authFetch.mockClear()
    const controller = new AbortController()
    controller.abort()
    await expect(owner.response('/content', 'sample.png', {signal:controller.signal})).rejects.toMatchObject({name:'AbortError'})
    await expect(owner.bounded('sample.png', 1024, {}, controller.signal)).rejects.toMatchObject({name:'AbortError'})
    expect(authFetch).not.toHaveBeenCalled()
  })

  it('shares concurrent reads of one file and preserves the draft on later opens', async () => {
    const content = deferred<Response>()
    authFetch.mockImplementation(async (path, options) => {
      const call = request(path, options)
      if (!call.resource) return json(projection)
      if (call.method === 'HEAD') return head()
      return content.promise
    })
    const owner = createEnterpriseWorkspace()
    await owner.refresh()
    const first = owner.open('notes.txt')
    const second = owner.open('notes.txt')
    await vi.waitFor(() => expect(authFetch.mock.calls.filter(([path, options]) => request(path, options).resource === '/content')).toHaveLength(2))
    content.resolve(text('base'))
    await Promise.all([first, second])
    const doc = owner.state.tabs[0]!
    doc.text = 'important draft'
    await owner.open('notes.txt')
    expect(owner.state.tabs).toHaveLength(1)
    expect(doc.text).toBe('important draft')
    expect(doc.original).toBe('base')
    expect(owner.dirty()).toBe(true)
    expect(authFetch.mock.calls.filter(([path, options]) => request(path, options).resource === '/content')).toHaveLength(2)
  })

  it('keeps a newly selected tab active when save-as finishes and retains later edits', async () => {
    const written = deferred<Response>()
    let saved = ''
    authFetch.mockImplementation(async (path, options = {}) => {
      const call = request(path, options)
      if (!call.resource) return json(projection)
      if (call.resource === '/files') return json({entries:[]})
      if (call.method === 'HEAD') return head()
      if (call.method === 'PUT') {
        saved = new TextDecoder().decode(options.body as Uint8Array)
        expect(new Headers(options.headers).get('If-None-Match')).toBe('*')
        return written.promise
      }
      return text(call.path === 'saved.txt' ? saved : 'base', call.path === 'saved.txt' ? '"v2"' : '"v1"')
    })
    const owner = createEnterpriseWorkspace()
    await owner.refresh()
    await owner.open('notes.txt')
    const doc = owner.state.tabs[0]!, id = doc.id
    doc.text = 'first revision'
    const saving = owner.save(doc, 'saved.txt')
    await vi.waitFor(() => expect(saved).toBe('first revision'))
    doc.text = 'a newer unsaved revision'
    await owner.open('other.txt')
    written.resolve(json({outcome:'SUCCEEDED'}))
    await saving
    expect(owner.state.active).toBe('other.txt')
    expect(doc.id).toBe(id)
    expect(doc.path).toBe('saved.txt')
    expect(doc.original).toBe('first revision')
    expect(doc.text).toBe('a newer unsaved revision')
    expect(doc.etag).toBe('"v2"')
    expect(owner.dirty()).toBe(true)
  })

  it('aborts an in-flight response body on identity reset without restoring its tab', async () => {
    let signal:AbortSignal|undefined
    let cancelled = false
    authFetch.mockImplementation(async (path, options = {}) => {
      const call = request(path, options)
      if (!call.resource) return json(projection)
      if (call.method === 'HEAD') return head(100)
      signal = options.signal as AbortSignal
      return new Response(new ReadableStream<Uint8Array>({
        start(controller) {
          controller.enqueue(new TextEncoder().encode('partial'))
          signal!.addEventListener('abort', () => {
            cancelled = true
            controller.error(new DOMException('Request aborted', 'AbortError'))
          }, {once:true})
        },
      }), {headers:{etag:'"v1"'}})
    })
    const owner = createEnterpriseWorkspace()
    await owner.refresh()
    const opening = owner.open('notes.txt')
    await vi.waitFor(() => expect(signal).toBeDefined())
    const doc = owner.state.tabs[0]!, generation = owner.generation()
    owner.reset()
    await opening
    expect(signal!.aborted).toBe(true)
    expect(cancelled).toBe(true)
    expect(owner.state.projection).toBeNull()
    expect(owner.state.tabs).toEqual([])
    expect(owner.state.active).toBe('')
    expect(doc.loaded).toBe(false)
    expect(doc.text).toBe('')
    expect(() => owner.assertGeneration(generation)).toThrow('已停止后续操作')
  })

  it('preserves both drafts if the save-as destination is opened while the write is pending', async () => {
    const written = deferred<Response>()
    let saved = ''
    authFetch.mockImplementation(async (path, options = {}) => {
      const call = request(path, options)
      if (!call.resource) return json(projection)
      if (call.method === 'HEAD') return head()
      if (call.method === 'PUT') {
        saved = new TextDecoder().decode(options.body as Uint8Array)
        return written.promise
      }
      return text(call.path === 'saved.txt' ? saved : 'base')
    })
    const owner = createEnterpriseWorkspace()
    await owner.refresh()
    await owner.open('notes.txt')
    const original = owner.state.tabs[0]!
    original.text = 'save-as draft'
    const saving = owner.save(original, 'saved.txt')
    await vi.waitFor(() => expect(saved).toBe('save-as draft'))
    await owner.open('saved.txt')
    const destination = owner.state.tabs[1]!
    destination.text = 'new destination draft'
    const result = expect(saving).rejects.toThrow('该路径已有打开的标签')
    written.resolve(json({outcome:'SUCCEEDED'}))
    await result
    expect(owner.state.tabs.map(doc=>doc.path)).toEqual(['notes.txt', 'saved.txt'])
    expect(original.original).toBe('base')
    expect(original.text).toBe('save-as draft')
    expect(destination.text).toBe('new destination draft')
    expect(owner.state.active).toBe('saved.txt')
  })

  it('retains drafts when a failed refresh marks the current workspace unavailable', async () => {
    let unavailable = false
    authFetch.mockImplementation(async (path, options) => {
      const call = request(path, options)
      if (!call.resource) return unavailable ? json({error:'企业连接暂不可用'}, 503) : json(projection)
      if (call.method === 'HEAD') return head()
      return text('base')
    })
    const owner = createEnterpriseWorkspace()
    await owner.refresh()
    await owner.open('notes.txt')
    const doc = owner.state.tabs[0]!
    doc.text = 'preserve this draft'
    unavailable = true
    // The connection-change handler reports refresh failure through this owner API.
    await owner.refresh().catch(error => owner.unavailable((error as Error).message))
    expect(owner.state.tabs[0]).toBe(doc)
    expect(doc.text).toBe('preserve this draft')
    expect(doc.original).toBe('base')
    expect(owner.state.active).toBe('notes.txt')
    expect(owner.state.error).toBe('企业连接暂不可用')
    expect(owner.state.projection?.filesAvailable).toBe(false)
    expect(owner.dirty()).toBe(true)
    await expect(owner.save(doc)).rejects.toThrow('当前不可用')
    expect(authFetch.mock.calls.some(([, options]) => options?.method === 'PUT')).toBe(false)
    unavailable = false
    await owner.refresh()
    expect(owner.state.projection?.filesAvailable).toBe(true)
    expect(owner.state.tabs[0]).toBe(doc)
    expect(doc.text).toBe('preserve this draft')
  })
})
