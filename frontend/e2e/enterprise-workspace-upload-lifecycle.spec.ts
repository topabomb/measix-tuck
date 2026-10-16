import {test,expect,type Page,type TestInfo} from '@playwright/test'
import {createHash} from 'node:crypto'
import {createReadStream} from 'node:fs'
import {mkdir,mkdtemp,open,rename,rm,stat,writeFile} from 'node:fs/promises'
import {tmpdir} from 'node:os'
import {join,relative,resolve} from 'node:path'
import {setTimeout as delay} from 'node:timers/promises'
import {fixture} from '../../tests/fixtures/fixture.mjs'
import {workspaceFixture} from '../../tests/fixtures/workspace-fixture.mjs'

// Use a real disk file and streaming receiver; never allocate its whole body.
// The controlled Core fixture commits only complete uploads. Production Core
// may have different write semantics, so cancellation must still say unknown.
test.use({trace:'off'})
const large=process.env.TUCK_TEST_LARGE_UPLOAD==='1',MiB=1024*1024
const size=(large?500:32)*MiB,rate=(large?32:4)*MiB,name='large-upload.bin'
const transferTimeout=large?120_000:30_000
let directory='',source='',expectedHash=''
test.beforeAll(async()=>{
  test.setTimeout(180_000)
  directory=await mkdtemp(join(tmpdir(),'tuck-upload-lifecycle-'));source=join(directory,name)
  const file=await open(source,'wx'),block=Buffer.alloc(MiB,0x67),hash=createHash('sha256')
  try{for(let offset=0;offset<size;offset+=block.length){expect((await file.write(block)).bytesWritten).toBe(block.length);hash.update(block)}}finally{await file.close()}
  expectedHash=hash.digest('hex');expect((await stat(source)).size).toBe(size)
})
test.afterAll(async()=>{if(directory){expect(relative(resolve(tmpdir()),resolve(directory))).toMatch(/^tuck-upload-lifecycle-[^\\/]+$/);await rm(directory,{recursive:true,force:true})}})

async function uploadFixture(page:Page,info:TestInfo){
  test.setTimeout(180_000)
  const destination=join(directory,`received-${info.testId.replace(/[^a-z0-9]/gi,'')}.bin`)
  const metrics={sourceBytes:size,receivedBytes:0,active:0,committed:false,sha256:'',diskSha256:'',requests:0,canceled:0,errors:[] as string[]}
  let release=()=>{}
  const confirmation=new Promise<void>(resolve=>{release=resolve})
  const core=await workspaceFixture({
    extraEntries:path=>!path&&metrics.committed?[{path:name,kind:'FILE',size,etag:'"upload-v1"'}]:[],
    content:async(req,res,path)=>{
      if(path!==name)return false
      if(req.method==='HEAD'){
        res.writeHead(metrics.committed?200:404,metrics.committed?{'Content-Length':String(size),ETag:'"upload-v1"'}:{})
        res.end();return true
      }
      if(req.method!=='PUT')return false
      metrics.requests++;metrics.active++
      const controller=new AbortController(),cancel=()=>controller.abort()
      res.once('close',cancel);req.once('aborted',cancel)
      const file=await open(destination+'.partial','w'),hash=createHash('sha256'),began=performance.now()
      let closed=false
      try{
        for await(const chunk of req){
          if(controller.signal.aborted)break
          const data=chunk as Buffer
          const result=await file.write(data);if(result.bytesWritten!==data.length)throw new Error('Incomplete fixture write')
          metrics.receivedBytes+=data.length;hash.update(data)
          const wait=metrics.receivedBytes/rate*1000-(performance.now()-began)
          if(wait>0)await delay(wait,undefined,{signal:controller.signal})
        }
        await file.close();closed=true
        if(req.complete&&!controller.signal.aborted&&metrics.receivedBytes===size){
          await rename(destination+'.partial',destination);metrics.sha256=hash.digest('hex');metrics.committed=true
          if(!controller.signal.aborted)await Promise.race([confirmation,new Promise<void>(resolve=>controller.signal.addEventListener('abort',()=>resolve(),{once:true}))])
          if(!controller.signal.aborted){res.writeHead(200,{'Content-Type':'application/json'});res.end(JSON.stringify({outcome:'SUCCEEDED',failures:[],truncated:false}))}
        }
      }catch(error){if(!controller.signal.aborted){metrics.errors.push((error as Error).message);res.destroy(error as Error)}}
      finally{if(!closed)await file.close();if(controller.signal.aborted)metrics.canceled++;controller.abort();metrics.active--;res.off('close',cancel);req.off('aborted',cancel)}
      return true
    },
  })
  const app=await fixture({enterprisePlatform:core.platform})
  await page.goto(app.url+'/#/workspace')
  const panel=page.getByRole('region',{name:'企业文件管理器'})
  await expect(panel.getByRole('option',{name:/hello\.txt/})).toBeVisible()
  const picker=page.waitForEvent('filechooser')
  await panel.getByRole('button',{name:'上传',exact:true}).click()
  await (await picker).setFiles(source)
  const transfers=panel.getByRole('region',{name:'上传进度'}),progress=transfers.getByRole('progressbar',{name:name+' 上传进度'})
  await expect(progress).toBeVisible()
  return {panel,transfers,progress,metrics,release,core,
    verifyComplete:async()=>{
      expect(metrics.receivedBytes).toBe(size);expect(metrics.sha256).toBe(expectedHash)
      expect((await stat(destination)).size).toBe(size)
      const hash=createHash('sha256');for await(const chunk of createReadStream(destination))hash.update(chunk)
      metrics.diskSha256=hash.digest('hex');expect(metrics.diskSha256).toBe(expectedHash)
    },
    evidence:async()=>{await writeFile(info.outputPath('upload-evidence.json'),JSON.stringify({...metrics,expectedHash},null,2))},
    close:async()=>{release();await app.close();await core.close();await writeFile(info.outputPath('upload-evidence.json'),JSON.stringify({...metrics,expectedHash},null,2))},
  }
}

async function guardedReload(page:Page,accept:boolean){
  const ready=page.waitForEvent('dialog',{timeout:10_000})
  const reload=page.reload({waitUntil:'domcontentloaded',timeout:15_000}).catch(error=>{if(accept)throw error})
  const dialog=await ready;expect(dialog.type()).toBe('beforeunload')
  if(accept)await dialog.accept();else await dialog.dismiss()
  // Dismissing a native beforeunload cancels navigation, so no new DOMContentLoaded
  // can finish that navigation wait. Verify the surviving upload instead.
  if(accept)await reload
}

test('directory preparation is guarded before any file transfer exists',async({page})=>{
  const core=await workspaceFixture(),app=await fixture({enterprisePlatform:core.platform})
  let release=()=>{},blocked=false
  const gate=new Promise<void>(resolve=>{release=resolve})
  try{
    await page.goto(app.url+'/#/workspace')
    const panel=page.getByRole('region',{name:'企业文件管理器'})
    await expect(panel.getByRole('option',{name:/hello\.txt/})).toBeVisible()
    await page.route('**/api/enterprise-workspace/files?*',async route=>{
      if(!blocked&&route.request().method()==='GET'){blocked=true;await gate}
      await route.continue()
    })
    const folder=join(directory,'upload-empty');await mkdir(folder)
    const body=(await panel.locator('.ew-body').boundingBox())!
    const session=await page.context().newCDPSession(page)
    try{for(const type of ['dragEnter','dragOver','drop'] as const)await session.send('Input.dispatchDragEvent',{type,x:body.x+20,y:body.y+20,data:{items:[],files:[folder],dragOperationsMask:1}})}finally{await session.detach()}
    await expect.poll(()=>blocked).toBe(true)
    await expect(panel.getByRole('button',{name:'刷新',exact:true})).toBeDisabled()
    await expect(panel.getByRole('region',{name:'上传进度'})).toHaveCount(0)
    await guardedReload(page,false)
    release();await expect(panel.getByRole('option',{name:/upload-empty/})).toBeVisible()
    await expect(panel.getByRole('button',{name:'刷新',exact:true})).toBeEnabled()
    await page.reload();await expect(panel.getByRole('option',{name:/upload-empty/})).toBeVisible()
    expect(core.requests.filter(r=>r.method==='PUT')).toHaveLength(0)
    expect(core.requests.filter(r=>r.method==='POST')).toHaveLength(1)
  }finally{release();await app.close();await core.close()}
})

test('disk upload verifies intermediate progress, refresh rejection, confirmation and complete SHA-256',async({page},info)=>{
  const f=await uploadFixture(page,info)
  try{
    await expect.poll(()=>f.progress.evaluate((el:HTMLProgressElement)=>el.value>0&&el.value<100)).toBe(true)
    await expect(f.panel.getByRole('button',{name:'刷新',exact:true})).toBeDisabled()
    await page.screenshot({path:info.outputPath('upload-progress.png')})
    await guardedReload(page,false)
    await expect(f.progress).toBeVisible();expect(f.metrics.requests).toBe(1)
    await expect(f.transfers.getByRole('status')).toHaveText('已发送，等待服务端确认',{timeout:transferTimeout})
    await expect.poll(()=>f.metrics.committed,{timeout:transferTimeout}).toBe(true)
    await f.verifyComplete()
    f.release();await expect(f.transfers.getByRole('status')).toHaveText('已完成')
    await expect(f.panel.getByRole('button',{name:'刷新',exact:true})).toBeEnabled()
    await expect.poll(()=>f.metrics.active).toBe(0);expect(f.metrics.errors).toEqual([])
    await page.reload();await expect(f.panel.getByRole('option',{name:new RegExp(name.replace('.','\\.'))})).toBeVisible()
    await expect(f.panel.getByRole('region',{name:'上传进度'})).toHaveCount(0);expect(f.metrics.requests).toBe(1)
    await f.evidence()
  }finally{await f.close()}
})

test('cancelling mid-upload closes upstream and refresh does not restart the file',async({page},info)=>{
  const f=await uploadFixture(page,info)
  try{
    await expect.poll(()=>f.metrics.receivedBytes).toBeGreaterThanOrEqual(MiB)
    await f.transfers.getByRole('button',{name:'取消上传 '+name}).click()
    await expect(f.transfers.getByRole('status')).toContainText('远端结果未知')
    await expect.poll(()=>f.metrics.active).toBe(0)
    expect(f.metrics.receivedBytes).toBeLessThan(size);expect(f.metrics.committed).toBe(false)
    await page.screenshot({path:info.outputPath('upload-cancelled.png')})
    await page.reload();await expect(f.panel.getByRole('option',{name:/hello\.txt/})).toBeVisible()
    await expect(f.panel.getByRole('region',{name:'上传进度'})).toHaveCount(0)
    await expect(f.panel.getByRole('option',{name:new RegExp(name.replace('.','\\.'))})).toHaveCount(0)
    expect(f.metrics.requests).toBe(1);expect(f.metrics.canceled).toBe(1);expect(f.metrics.errors).toEqual([])
    await f.evidence()
  }finally{await f.close()}
})

test('cancelling confirmation preserves the already committed file and refresh discovers it',async({page},info)=>{
  const f=await uploadFixture(page,info)
  try{
    await expect.poll(()=>f.metrics.committed,{timeout:transferTimeout}).toBe(true)
    await f.verifyComplete()
    await expect(f.transfers.getByRole('status')).toHaveText('已发送，等待服务端确认')
    await f.transfers.getByRole('button',{name:'取消上传 '+name}).click()
    await expect(f.transfers.getByRole('status')).toContainText('远端结果未知')
    await expect.poll(()=>f.metrics.active).toBe(0)
    await page.reload();await expect(f.panel.getByRole('option',{name:new RegExp(name.replace('.','\\.'))})).toBeVisible()
    expect(f.metrics.sha256).toBe(expectedHash);expect(f.metrics.requests).toBe(1)
    await expect(f.panel.getByRole('region',{name:'上传进度'})).toHaveCount(0)
    await f.evidence()
  }finally{await f.close()}
})

for(const confirming of [false,true])test(`accepting refresh ${confirming?'during confirmation':'mid-upload'} closes the stream and reads authoritative directory without retry`,async({page},info)=>{
  const f=await uploadFixture(page,info)
  try{
    if(confirming){await expect.poll(()=>f.metrics.committed,{timeout:transferTimeout}).toBe(true);await f.verifyComplete()}
    else await expect.poll(()=>f.metrics.receivedBytes).toBeGreaterThanOrEqual(MiB)
    await guardedReload(page,true)
    await expect(f.panel.getByRole('option',{name:/hello\.txt/})).toBeVisible()
    await expect.poll(()=>f.metrics.active).toBe(0)
    await expect(f.panel.getByRole('region',{name:'上传进度'})).toHaveCount(0)
    const uploaded=f.panel.getByRole('option',{name:new RegExp(name.replace('.','\\.'))})
    if(confirming){await expect(uploaded).toBeVisible();expect(f.metrics.sha256).toBe(expectedHash)}
    else{await expect(uploaded).toHaveCount(0);expect(f.metrics.receivedBytes).toBeLessThan(size)}
    expect(f.metrics.committed).toBe(confirming);expect(f.metrics.requests).toBe(1);expect(f.metrics.canceled).toBe(1);expect(f.metrics.errors).toEqual([])
    await f.evidence()
  }finally{await f.close()}
})
