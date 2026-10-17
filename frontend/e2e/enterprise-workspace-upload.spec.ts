import { test, expect, type Page } from '@playwright/test'
import { createHash } from 'node:crypto'
import { fixture } from '../../tests/fixtures/fixture.mjs'
import { workspaceFixture } from '../../tests/fixtures/workspace-fixture.mjs'

let app:Awaited<ReturnType<typeof fixture>>,core:Awaited<ReturnType<typeof workspaceFixture>>
let heldPath='',releaseUpload=()=>{},confirmation=Promise.resolve()
test.beforeAll(async()=>{core=await workspaceFixture({beforeUploadResponse:async path=>{if(path===heldPath)await confirmation}});app=await fixture({enterprisePlatform:core.platform})})
test.afterEach(()=>{releaseUpload();heldPath=''})
test.afterAll(async()=>{releaseUpload();await app?.close();await core?.close()})
function holdConfirmation(path:string){heldPath=path;confirmation=new Promise<void>(resolve=>{releaseUpload=resolve})}
async function workspace(page:Page){
  const task=await app.manager.create({title:'上传进度验收'})
  await page.goto(app.url+`/#/chat/conversations/${task.id}?ew=1`)
  const panel=page.getByRole('complementary',{name:'企业工作区'})
  await expect(panel.getByRole('option',{name:/hello\.txt/})).toBeVisible()
  return panel
}

test('returning from the file picker cannot let focus refresh discard the selected upload',async({page})=>{
  const panel=await workspace(page)
  let releaseFocus=()=>{},focusRequests=0
  const focusGate=new Promise<void>(resolve=>{releaseFocus=resolve})
  await page.route('**/api/enterprise-workspace',async route=>{focusRequests++;await focusGate;await route.continue()})
  try{
    const chooserReady=page.waitForEvent('filechooser')
    await panel.getByRole('button',{name:'上传',exact:true}).click()
    const chooser=await chooserReady
    // Native pickers can dispatch window focus before the input change event.
    await page.evaluate(()=>window.dispatchEvent(new Event('focus')))
    await chooser.setFiles({name:'picker-return.txt',mimeType:'text/plain',buffer:Buffer.from('selected after focus')})
    await expect.poll(()=>core.files.get('picker-return.txt')?.toString()).toBe('selected after focus')
    await expect(panel.getByRole('region',{name:'上传进度'})).toBeVisible()
    expect(focusRequests).toBe(0)
  }finally{releaseFocus()}
})

test('a real large upload shows bytes and progress, then waits for server confirmation',async({page},info)=>{
  test.setTimeout(90000)
  const panel=await workspace(page),payload=Buffer.alloc(32*1024*1024,0x63)
  holdConfirmation('large-upload.bin')
  const cdp=await page.context().newCDPSession(page)
  await cdp.send('Network.enable')
  await cdp.send('Network.emulateNetworkConditions',{offline:false,latency:0,downloadThroughput:32*1024*1024,uploadThroughput:4*1024*1024})
  await panel.locator('input[type=file]').first().setInputFiles({name:'large-upload.bin',mimeType:'application/octet-stream',buffer:payload})
  const transfers=panel.getByRole('region',{name:'上传进度'}),progress=transfers.getByRole('progressbar',{name:'large-upload.bin 上传进度'})
  await expect(transfers).toBeVisible()
  await expect.poll(()=>progress.evaluate((element:HTMLProgressElement)=>element.value>0&&element.value<100),{timeout:20000}).toBe(true)
  await expect(transfers.getByText(/MiB \/ 32\.0 MiB/)).toBeVisible()
  await page.screenshot({path:info.outputPath('workspace-upload-progress.png')})
  await expect(transfers.getByRole('status')).toHaveText('已发送，等待服务端确认',{timeout:45000})
  await expect(progress).toHaveAttribute('value','100')
  expect(core.files.get('large-upload.bin')?.length).toBe(payload.length)
  expect(createHash('sha256').update(core.files.get('large-upload.bin')!).digest('hex')).toBe(createHash('sha256').update(payload).digest('hex'))
  await page.screenshot({path:info.outputPath('workspace-upload-confirming.png')})
  releaseUpload()
  await expect(transfers.getByRole('status')).toHaveText('服务端已确认保存')
  await expect(transfers.getByRole('button',{name:'取消上传 large-upload.bin'})).toHaveCount(0)
})

test('cancelling after bytes were sent retains an unknown outcome instead of claiming rollback',async({page})=>{
  const panel=await workspace(page)
  holdConfirmation('cancelled-upload.bin')
  await panel.locator('input[type=file]').first().setInputFiles({name:'cancelled-upload.bin',mimeType:'application/octet-stream',buffer:Buffer.alloc(1024*1024,0x64)})
  const transfers=panel.getByRole('region',{name:'上传进度'})
  await expect(transfers.getByRole('status')).toHaveText('已发送，等待服务端确认')
  await expect.poll(()=>core.files.has('cancelled-upload.bin')).toBe(true)
  await transfers.getByRole('button',{name:'取消上传 cancelled-upload.bin'}).click()
  await expect(transfers.getByRole('status')).toContainText('远端结果未知')
  await expect(panel.getByRole('alert')).toContainText('远端结果未知')
  releaseUpload()
  expect(core.files.get('cancelled-upload.bin')?.length).toBe(1024*1024)
})
