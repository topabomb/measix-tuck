import {test,expect} from '@playwright/test'
import {fixture} from '../../tests/fixtures/fixture.mjs'
import {platformFixture} from '../../tests/fixtures/platform-fixture.mjs'
import {mkdir} from 'node:fs/promises'
import {resolve} from 'node:path'

test('remote inspection inputs and immutable delivery work without a local directory',async({page})=>{
 test.setTimeout(120000)
 const f=await platformFixture()
 let app:Awaited<ReturnType<typeof fixture>>|undefined
 try{
  const image=await page.evaluate(()=>{const c=document.createElement('canvas');c.width=400;c.height=250;const x=c.getContext('2d')!;x.fillStyle='#15375d';x.fillRect(0,0,400,250);x.fillStyle='#e5b65b';x.fillRect(120,65,160,120);return c.toDataURL('image/png').split(',')[1]!})
  let bytes=Buffer.from(image,'base64')
  const space='spc_22222222-2222-4222-8222-222222222222',resource='dav_11111111-1111-4111-8111-111111111111'
  const root=`/runtime/v1/resources/${resource}/spaces/${space}/dav/`
  f.snapshot.webdav=[{resourceId:resource,displayName:'Runcove files',enabled:true,purpose:'WORKSPACE_FILE_EXCHANGE',authOwnership:'ENTERPRISE_MANAGED',clientProtocol:'WEBDAV',rootPathTemplate:root.replace(space,'{agentSpaceId}')}]
  f.controls.workspace={schemaVersion:1,serviceState:'ENABLED',state:'CONNECTED',bindingRevision:1,agentSpaceId:space,filesAvailable:true,filesReason:'available',mcpAvailable:false,mcpReason:'disabled',fileExchangeWebdavResourceId:resource,webdav:{resourceId:resource,available:true,reason:'available',accessMode:'FULL',bindingRevision:1}}
  f.controls.dav=(req:any,res:any)=>{
   if(req.url!==root+'picture.png'){res.writeHead(404);res.end();return}
   if(req.method==='PROPFIND'){res.writeHead(207,{'Content-Type':'application/xml'});res.end(`<d:multistatus xmlns:d="DAV:"><d:response><d:href>${root}picture.png</d:href><d:propstat><d:prop><d:resourcetype/><d:getetag>"picture"</d:getetag><d:getcontentlength>${bytes.length}</d:getcontentlength></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response></d:multistatus>`);return}
   res.writeHead(200,{'Content-Type':'image/png','Content-Length':bytes.length,'ETag':'"picture"'});res.end(bytes)
  }
  await f.models.platform.enroll(f.material)
  app=await fixture({attachmentInspection:true,delivery:true,inspectionAttachments:[{source:'runcove',path:'picture.png'}],enterprisePlatform:{binding:f.models.platform.binding,access:async()=>f.controls.token}})
  await page.goto(app.url+'/#/settings/providers')
  await page.getByRole('combobox',{name:'默认识图模型',exact:true}).selectOption('vision')
  await page.getByRole('button',{name:'保存并生效',exact:true}).click()
  await expect(page.getByRole('status')).toContainText('已保存并生效')
  const task=await app.manager.create({title:'Runcove 远程附件',workspaceMode:'none',approvalMode:'auto'})
  await page.goto(app.url+`/#/chat/conversations/${task.id}`)
  await page.getByTestId('composer-input').fill('INSPECT_TOOL 请识别远程图片。');await page.getByTestId('send-message').click()
  await expect(page.getByText('已根据识图结果确认：蓝色背景，金色方块。',{exact:true})).toBeVisible({timeout:60000})
  const call=page.getByTestId('tool-block').filter({has:page.locator('.activity-summary strong',{hasText:'inspect_attachments'})}).first()
  await call.locator('.activity-summary').click()
  await expect.poll(()=>call.locator('img').evaluate((img:HTMLImageElement)=>img.naturalWidth)).toBeGreaterThan(0)
  await mkdir(resolve('../.local/runcove-tools'),{recursive:true})
  for(const width of [1365,390,320]){
   await page.setViewportSize({width,height:900});await call.scrollIntoViewIfNeeded()
   await page.screenshot({path:resolve(`../.local/runcove-tools/input-${width}.png`)})
   await call.getByTestId('attachment-image').click();await expect(page.locator('.media-preview-dialog img')).toBeVisible()
   await page.screenshot({path:resolve(`../.local/runcove-tools/preview-${width}.png`)})
   await page.getByRole('button',{name:'关闭图片预览'}).click()
   expect(await page.evaluate(()=>document.documentElement.scrollWidth-innerWidth)).toBeLessThanOrEqual(1)
  }
  await page.reload()
  if(await call.locator('.activity-summary').getAttribute('aria-expanded')==='false')await call.locator('.activity-summary').click()
  await expect.poll(()=>call.locator('img').evaluate((img:HTMLImageElement)=>img.naturalWidth)).toBeGreaterThan(0)
  await page.getByTestId('composer-input').fill('DELIVER_REMOTE_TOOL 请交付远程图片。');await page.getByTestId('send-message').click()
  await expect(page.getByRole('link',{name:'查看交付 V1'})).toBeVisible({timeout:60000})
  bytes=Buffer.from('changed remote file')
  await page.getByRole('link',{name:'查看交付 V1'}).click()
  const download=page.getByRole('link',{name:'下载 picture.png'})
  await expect(download).toBeVisible()
  const saved=await page.request.get(app.url+(await download.getAttribute('href'))!)
  expect(saved.status()).toBe(200);expect(await saved.body()).toEqual(Buffer.from(image,'base64'))
  await page.screenshot({path:resolve('../.local/runcove-tools/delivery-320.png')})
  expect(app.requests.filter(r=>r.stream===false)).toHaveLength(1)
  expect(app.requests.filter(r=>r.stream===true).every(r=>!JSON.stringify(r).includes('data:image/'))).toBe(true)
 }finally{await app?.close();await f.close()}
})
