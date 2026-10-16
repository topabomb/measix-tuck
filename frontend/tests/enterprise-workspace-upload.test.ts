import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createEnterpriseWorkspace } from '../src/shared/enterprise-workspace/state'

const {authFetch,expiredLogin,workspaceConflict}=vi.hoisted(()=>({authFetch:vi.fn(),expiredLogin:vi.fn(),workspaceConflict:vi.fn()}))
vi.mock('../src/app/auth',()=>({authFetch,expiredLogin,csrfHeader:()=>({'X-Tuck-CSRF':'local-csrf'})}))
vi.mock('../src/shared/enterprise-workspace/dialog',()=>({workspaceConflict}))
const projection={agentSpaceId:'spc_11111111-1111-1111-1111-111111111111',subject:'test-account',employeeName:'Tester',filesAvailable:true,filesReason:'',state:'CONNECTED'}
const json=(value:unknown,status=200)=>new Response(JSON.stringify(value),{status,headers:{'Content-Type':'application/json'}})
class UploadXHR {
  static instances:UploadXHR[]=[]
  headers:Record<string,string>={};url='';method='';body:unknown;withCredentials=false;status=0;responseText=''
  upload:{onprogress?:((event:{loaded:number;total:number;lengthComputable:boolean})=>void);onload?:()=>void}={}
  onload?:()=>void;onabort?:()=>void;onerror?:()=>void;ontimeout?:()=>void
  constructor(){UploadXHR.instances.push(this)}
  open(method:string,url:string){this.method=method;this.url=url}
  setRequestHeader(key:string,value:string){this.headers[key]=value}
  send(body:unknown){this.body=body}
  abort(){this.onabort?.()}
  respond(status:number,value:unknown){this.status=status;this.responseText=JSON.stringify(value);this.onload?.()}
}
beforeEach(()=>{
  vi.stubGlobal('document',{baseURI:'https://tuck.test/workbench/'})
  vi.stubGlobal('XMLHttpRequest',UploadXHR)
  UploadXHR.instances=[];authFetch.mockReset();expiredLogin.mockReset();workspaceConflict.mockReset()
  authFetch.mockImplementation(async(path:string)=>json(path.includes('/files')?{entries:[]}:projection))
})
afterEach(()=>vi.unstubAllGlobals())
async function start(name='upload.bin'){
  const owner=createEnterpriseWorkspace();await owner.refresh()
  const file=new File([new Uint8Array(1024*1024)],name),upload=owner.upload([file])
  await vi.waitFor(()=>expect(UploadXHR.instances).toHaveLength(1))
  return {owner,file,upload,xhr:UploadXHR.instances[0]!}
}
describe('enterprise upload progress and outcome',()=>{
  it('keeps a rejected upload path visible after refreshing the directory',async()=>{
    const owner=createEnterpriseWorkspace();await owner.refresh()
    await owner.upload([new File(['data'],'invalid\\name.txt')])
    expect(UploadXHR.instances).toHaveLength(0)
    expect(owner.state.error).toBe('invalid\\name.txt：上传文件路径无效')
  })
  it('shows sent bytes but waits for the server result before declaring success',async()=>{
    const {owner,file,upload,xhr}=await start()
    expect(xhr.method).toBe('PUT');expect(xhr.body).toBe(file)
    expect(xhr.headers).toMatchObject({'X-Tuck-CSRF':'local-csrf','If-None-Match':'*'})
    xhr.upload.onprogress?.({loaded:file.size/2,total:file.size,lengthComputable:true})
    expect(owner.state.transfers[0]).toMatchObject({progress:50,loaded:file.size/2,total:file.size,status:'上传中'})
    xhr.upload.onload?.()
    expect(owner.state.transfers[0]).toMatchObject({progress:100,loaded:file.size,status:'已发送，等待服务端确认'})
    expect(owner.state.transfers[0]!.cancel).toBeTypeOf('function')
    xhr.respond(200,{outcome:'SUCCEEDED'});await upload
    expect(owner.state.transfers[0]).toMatchObject({progress:100,status:'已完成'})
    expect(owner.state.transfers[0]!.cancel).toBeUndefined()
  })
  it('uses the actual duplicate destination in the visible transfer',async()=>{
    authFetch.mockImplementation(async(path:string)=>json(path.includes('/files')?{entries:[{path:'upload.bin',kind:'FILE',etag:'"v1"'}]}:projection))
    workspaceConflict.mockResolvedValue('副本')
    const {owner,upload,xhr}=await start()
    expect(new URL(xhr.url,'https://tuck.test').searchParams.get('path')).toBe('upload (1).bin')
    expect(owner.state.transfers[0]!.path).toBe('upload (1).bin')
    xhr.respond(200,{outcome:'SUCCEEDED'});await upload
  })
  it('prefers confirmed overwrite and sends only the listed strong version',async()=>{
    authFetch.mockImplementation(async(path:string)=>json(path.includes('/files')?{entries:[{path:'upload.bin',kind:'FILE',etag:'"v1"'}]}:projection))
    workspaceConflict.mockResolvedValue('替换')
    const {owner,upload,xhr}=await start()
    expect(workspaceConflict).toHaveBeenCalledWith(expect.stringContaining('upload.bin'),expect.objectContaining({preferred:'替换',allowReplace:true}))
    expect(xhr.headers['If-Match']).toBe('"v1"');expect(xhr.headers['If-None-Match']).toBeUndefined()
    xhr.respond(412,{error:'文件已变化'});await upload
    expect(owner.state.error).toContain('同名文件已变化，未覆盖');expect(UploadXHR.instances).toHaveLength(1)
  })
  it.each([undefined,'W/"v1"','*','"one", "two"','"bad(value)"','"'+ 'a'.repeat(1024)+'"','"'+ 'é'.repeat(600)+'"','"版本一"'])('does not offer or send overwrite with invalid version %s',async etag=>{
    authFetch.mockImplementation(async(path:string)=>json(path.includes('/files')?{entries:[{path:'upload.bin',kind:'FILE',etag}]}:projection))
    workspaceConflict.mockResolvedValue('替换')
    const owner=createEnterpriseWorkspace();await owner.refresh();await owner.upload([new File(['data'],'upload.bin')])
    expect(workspaceConflict).toHaveBeenCalledWith(expect.any(String),expect.objectContaining({allowReplace:false}))
    expect(UploadXHR.instances).toHaveLength(0);expect(owner.state.error).toContain('不能覆盖')
  })
  it('cancelling conflict confirmation stops the queued batch before any file is sent',async()=>{
    authFetch.mockImplementation(async(path:string)=>json(path.includes('/files')?{entries:[{path:'upload.bin',kind:'FILE',etag:'"v1"'}]}:projection))
    workspaceConflict.mockResolvedValue(null)
    const owner=createEnterpriseWorkspace();await owner.refresh();await owner.upload([new File(['data'],'upload.bin'),new File(['queued'],'new.txt')])
    expect(UploadXHR.instances).toHaveLength(0);expect(owner.state.transfers).toHaveLength(1)
    expect(owner.state.transfers[0]!.status).toBe('已取消，未传输该文件')
    expect(owner.state.uploading).toBe(false)
  })
  it('reports a create race without silently changing into overwrite',async()=>{
    const {owner,upload,xhr}=await start()
    xhr.respond(412,{error:'文件已变化'});await upload
    expect(owner.state.error).toContain('目标已存在，未覆盖');expect(UploadXHR.instances).toHaveLength(1)
    expect(xhr.headers['If-None-Match']).toBe('*');expect(xhr.headers['If-Match']).toBeUndefined()
  })
  it('retains an open draft and tells the user to reread after an upload',async()=>{
    const {owner,upload,xhr}=await start()
    owner.state.tabs.push({id:1,path:'upload.bin',type:'text',text:'local draft',original:'old',etag:'"old"',bom:false,newline:'\n',readonly:false,reason:'',loaded:true,error:''})
    xhr.respond(200,{outcome:'SUCCEEDED'});await upload
    expect(owner.state.tabs[0]!.text).toBe('local draft');expect(owner.state.notice).toContain('请重新读取')
  })
  it('holds upload ownership through directory preparation and releases it on failure',async()=>{
    const owner=createEnterpriseWorkspace();await owner.refresh()
    let reject!:(error:Error)=>void
    authFetch.mockImplementation(()=>new Promise<Response>((_,fail)=>{reject=fail}))
    const upload=owner.upload([],['empty-folder']),failure=expect(upload).rejects.toThrow('directory failed')
    expect(owner.state.uploading).toBe(true);expect(owner.state.transfers).toHaveLength(0)
    reject(new Error('directory failed'));await failure
    expect(owner.state.uploading).toBe(false)
  })
  it('an aborted older upload cannot release the newer upload owner',async()=>{
    const {owner,upload}=await start();owner.abort()
    const newer=owner.upload([new File(['new bytes'],'newer.txt')])
    await upload;expect(owner.state.uploading).toBe(true)
    await vi.waitFor(()=>expect(UploadXHR.instances).toHaveLength(2))
    UploadXHR.instances[1]!.respond(200,{outcome:'SUCCEEDED'});await newer
    expect(owner.state.uploading).toBe(false)
  })
  it.each(['abort','network','timeout','server'] as const)('retains an unknown outcome after %s failure, even if all bytes were sent',async(mode)=>{
    const {owner,upload,xhr}=await start();xhr.upload.onload?.()
    if(mode==='abort')owner.state.transfers[0]!.cancel?.()
    else if(mode==='network')xhr.onerror?.()
    else if(mode==='timeout')xhr.ontimeout?.()
    else xhr.respond(504,{error:'企业服务暂时不可用'})
    await upload
    expect(owner.state.transfers[0]!.status).toContain('远端结果未知')
    expect(owner.state.error).toContain('远端结果未知')
    expect(owner.state.transfers[0]!.status).not.toBe('已完成')
  })
  it('keeps enterprise authentication rejection visible without expiring the local login',async()=>{
    const {owner,upload,xhr}=await start()
    xhr.respond(409,{code:'platform_session_unavailable',error:'企业会话不可用，请重新接入企业账号'})
    await upload
    expect(owner.state.error).toContain('企业会话不可用')
    expect(owner.state.transfers[0]!.status).toContain('企业会话不可用')
    expect(expiredLogin).not.toHaveBeenCalled()
  })
  it('records a failed preparation instead of silently hiding it after directory refresh',async()=>{
    const owner=createEnterpriseWorkspace();await owner.refresh()
    authFetch.mockImplementation(async()=>json({error:'企业会话不可用'},409))
    await owner.upload([new File(['data'],'rejected.txt')])
    expect(UploadXHR.instances).toHaveLength(0)
    expect(owner.state.error).toContain('rejected.txt：企业会话不可用')
    expect(owner.state.transfers[0]!.status).toBe('企业会话不可用')
  })
})
