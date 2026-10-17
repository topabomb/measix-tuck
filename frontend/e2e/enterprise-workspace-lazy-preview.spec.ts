import {test,expect,type Page} from '@playwright/test'
import {fixture} from '../../tests/fixtures/fixture.mjs'
import {workspaceFixture} from '../../tests/fixtures/workspace-fixture.mjs'

let app:Awaited<ReturnType<typeof fixture>>,core:Awaited<ReturnType<typeof workspaceFixture>>
let slow=false,release:(()=>void)|undefined,closed=false
test.beforeEach(async()=>{
  slow=false;closed=false;release=undefined
  core=await workspaceFixture({content:async(req,res,path)=>{
    if(slow&&req.method==='GET'&&path==='photo-0.png'){
      res.on('close',()=>{closed=true})
      await new Promise<void>(resolve=>{release=resolve})
      return res.destroyed
    }
    return false
  }})
  app=await fixture({enterprisePlatform:core.platform})
})
test.afterEach(async()=>{release?.();try{await app?.close()}finally{await core?.close()}})
const gets=(path:string)=>core.requests.filter(request=>request.path===path&&request.method==='GET'&&request.url.includes('/content'))
async function open(page:Page,path:string,width=1440) {
  await page.setViewportSize({width,height:900})
  const task=await app.manager.create({title:'按需完整预览'})
  await page.goto(app.url+`/#/chat/conversations/${task.id}?ew=1&wide=1&focus=1&file=${encodeURIComponent(path)}`)
  const panel=page.getByRole('complementary',{name:'企业工作区'}),frame=panel.frameLocator('iframe[title="文件静态预览"]')
  await expect(frame.locator('h1')).toBeVisible()
  return {panel,frame}
}
function album() {
  for(let index=0;index<45;index++)core.put(`photo-${index}.png`,core.files.get('sample.png')!)
  core.put('album.html','<h1>完整长图集</h1>'+Array.from({length:45},(_,index)=>`<section style="height:400px"><img width="200" height="160" id="photo-${index}" src="photo-${index}.png" alt="图片 ${index}"><p>图 ${index}</p></section>`).join(''))
}

for(const width of [1440,390])test(`a ${width}px album loads its forty-second image on demand and reuses a cached first image`,async({page})=>{
  album()
  const {panel,frame}=await open(page,'album.html',width),iframe=await panel.locator('iframe').elementHandle()
  await expect(frame.locator('#photo-0')).toHaveAttribute('data-workspace-state','loaded')
  expect(gets('photo-41.png')).toHaveLength(0)
  await frame.locator('#photo-41').scrollIntoViewIfNeeded()
  await expect(frame.locator('#photo-41')).toHaveAttribute('data-workspace-state','loaded')
  expect(await frame.locator('#photo-41').evaluate(node=>(node as HTMLImageElement).naturalWidth)).toBe(2)
  await expect(frame.locator('#photo-0')).toHaveAttribute('data-workspace-state','deferred')
  const firstReads=gets('photo-0.png').length
  await frame.locator('#photo-0').scrollIntoViewIfNeeded()
  await expect(frame.locator('#photo-0')).toHaveAttribute('data-workspace-state','loaded')
  expect(gets('photo-0.png').length).toBe(firstReads)
  expect(await iframe!.evaluate(node=>node===document.querySelector('iframe[title="文件静态预览"]'))).toBe(true)
  await expect(frame.locator('#photo-0')).toBeInViewport()
  await expect(panel.getByRole('alert')).toHaveCount(0)
})

test('scrolling away cancels a slow image and does not block the newly visible image',async({page})=>{
  album();slow=true
  const {panel,frame}=await open(page,'album.html')
  await expect.poll(()=>gets('photo-0.png').length).toBe(1)
  await frame.locator('#photo-41').scrollIntoViewIfNeeded()
  await expect.poll(()=>closed).toBe(true)
  await expect(frame.locator('#photo-41')).toHaveAttribute('data-workspace-state','loaded')
  release!()
  await expect(frame.locator('#photo-0')).toHaveAttribute('data-workspace-state','deferred')
  await expect(panel.getByRole('status',{name:'部分资源未加载',exact:true})).toHaveCount(0)
})

test('a failed image can be retried inside the preview without blocking later images',async({page})=>{
  core.put('retry.png',core.files.get('sample.png')!)
  core.put('retry.html','<h1>可恢复的图片</h1><img id="retry" width="180" height="160" src="retry.png" alt="重试图片"><img id="good" width="180" height="160" src="sample.png" alt="可用图片">')
  let failed=true
  await page.route('**/api/enterprise-workspace/content?**',route=>{
    const url=new URL(route.request().url())
    if(failed&&url.searchParams.get('path')==='retry.png')return route.fulfill({status:503,json:{error:'图片服务暂时不可用'}})
    return route.continue()
  })
  const {panel,frame}=await open(page,'retry.html')
  await expect(frame.locator('#retry')).toHaveAttribute('data-workspace-state','failed')
  await expect(frame.locator('#good')).toHaveAttribute('data-workspace-state','loaded')
  await panel.getByRole('button',{name:'预览说明与链接',exact:true}).click()
  await expect(panel.locator('.ew-resource-error')).toContainText('图片服务暂时不可用')
  await expect(panel.getByRole('button',{name:'打开原图',exact:true})).toBeVisible()
  await panel.getByRole('button',{name:'预览说明与链接',exact:true}).press('Escape')
  failed=false
  await frame.locator('#retry').press('Enter')
  await expect(frame.locator('#retry')).toHaveAttribute('data-workspace-state','loaded')
  expect(await frame.locator('#retry').evaluate(node=>(node as HTMLImageElement).naturalWidth)).toBe(2)
  await expect(panel.getByRole('status',{name:'部分资源未加载',exact:true})).toHaveCount(0)
  await frame.locator('#retry').press('Enter')
  await expect(panel.getByRole('region',{name:'retry.png',exact:true}).getByRole('img',{name:'retry.png',exact:true})).toBeVisible()
})

test('foreign, stale-token and unlisted resource messages cannot make the parent read files',async({page})=>{
  album()
  const {panel,frame}=await open(page,'album.html')
  await expect(frame.locator('#photo-0')).toHaveAttribute('data-workspace-state','loaded')
  const token=await panel.locator('iframe').evaluate(node=>new DOMParser().parseFromString((node as HTMLIFrameElement).srcdoc,'text/html').querySelector('script')!.getAttribute('nonce'))
  await page.evaluate(token=>window.postMessage({workspacePreview:token,type:'images',demands:[{index:41,visible:true,edge:1000}]},'*'),token)
  await frame.locator('body').evaluate((node,token)=>{
    const target=node.ownerDocument.defaultView!.parent
    target.postMessage({workspacePreview:token+'-old',type:'retryImage',index:41},'*')
    target.postMessage({workspacePreview:token,type:'retryImage',index:99999,path:'unlisted.png'},'*')
  },token)
  // A real same-frame request is the round-trip barrier for earlier messages.
  await frame.locator('#photo-0').press('Enter')
  await expect(panel.getByRole('region',{name:'photo-0.png',exact:true}).getByRole('img',{name:'photo-0.png',exact:true})).toBeVisible()
  expect(gets('photo-41.png')).toHaveLength(0)
  expect(gets('unlisted.png')).toHaveLength(0)
})

test('one raster path upgrades from a thumbnail to the larger visible display size',async({page})=>{
  await page.goto(app.url)
  const png=await page.evaluate(()=>{const canvas=document.createElement('canvas');canvas.width=canvas.height=1600;const context=canvas.getContext('2d')!;context.fillStyle='#4e7092';context.fillRect(0,0,1600,1600);return canvas.toDataURL('image/png').split(',')[1]!})
  core.put('large.png',Buffer.from(png,'base64'))
  core.put('sizes.html','<h1>自适应分辨率</h1><img id="small" width="100" height="100" src="large.png"><section style="margin-top:1800px"><img id="large" width="900" height="900" src="large.png"></section>')
  const {frame}=await open(page,'sizes.html')
  await expect(frame.locator('#small')).toHaveAttribute('data-workspace-state','loaded')
  await expect.poll(()=>frame.locator('#small').evaluate(node=>(node as HTMLImageElement).naturalWidth)).toBe(128)
  await frame.locator('#large').scrollIntoViewIfNeeded()
  await expect(frame.locator('#large')).toHaveAttribute('data-workspace-state','loaded')
  await expect.poll(()=>frame.locator('#large').evaluate(node=>(node as HTMLImageElement).naturalWidth)).toBeGreaterThanOrEqual(900)
})

test('an optional stylesheet budget does not prevent later styles or HTML rendering',async({page})=>{
  core.put('slow.css','body{color:red}')
  core.put('later.css','h1{color:rgb(12,34,56)}')
  core.put('slow-style.html','<link rel="stylesheet" href="slow.css"><link rel="stylesheet" href="later.css"><h1>正文不能被样式卡住</h1><img src="sample.png">')
  let release:(()=>void)|undefined
  await page.clock.install()
  await page.route('**/api/enterprise-workspace/content?**',async route=>{
    if(route.request().method()==='GET'&&new URL(route.request().url()).searchParams.get('path')==='slow.css')await new Promise<void>(resolve=>{release=resolve})
    try{await route.continue()}catch{}
  })
  try{
    const task=await app.manager.create({title:'样式总超时'})
    await page.goto(app.url+`/#/chat/conversations/${task.id}?ew=1&wide=1&focus=1&file=slow-style.html`)
    await expect.poll(()=>!!release).toBe(true)
    await page.clock.fastForward(30_100)
    const panel=page.getByRole('complementary',{name:'企业工作区'}),frame=panel.frameLocator('iframe[title="文件静态预览"]')
    await expect(frame.locator('h1')).toHaveCSS('color','rgb(12, 34, 56)')
    await panel.getByRole('button',{name:'预览说明与链接',exact:true}).click()
    await expect(panel.locator('.ew-links')).toContainText('样式不可用：slow.css')
  }finally{release?.()}
})

test('a foreground image waits beyond the optional resource budget and can still finish',async({page})=>{
  await page.clock.install()
  let finish:(()=>void)|undefined
  await page.route('**/api/enterprise-workspace/content?**',async route=>{
    if(route.request().method()==='GET'&&new URL(route.request().url()).searchParams.get('path')==='sample.png')await new Promise<void>(resolve=>{finish=resolve})
    try{await route.continue()}catch{}
  })
  try{
    const task=await app.manager.create({title:'图片传输不按总时长中断'})
    await page.goto(app.url+`/#/chat/conversations/${task.id}?ew=1&wide=1&file=sample.png`)
    const panel=page.getByRole('complementary',{name:'企业工作区'})
    await expect.poll(()=>!!finish).toBe(true)
    await page.clock.fastForward(30_100)
    await expect(panel.getByRole('alert')).toHaveCount(0)
    finish!()
    await expect(panel.getByRole('img',{name:'sample.png',exact:true})).toBeVisible()
  }finally{finish?.()}
})

for(const dirty of [false,true])test(`file tab quota ${dirty?'protects drafts and reports full tabs':'evicts clean tabs without disturbing the file target'}`,async({page})=>{
  for(let index=0;index<13;index++)core.put(`tab-${index}.txt`,`content ${index}`)
  const task=await app.manager.create({title:'文件标签配额'})
  await page.goto(app.url+`/#/chat/conversations/${task.id}?ew=1&wide=1`)
  const panel=page.getByRole('complementary',{name:'企业工作区'})
  for(let index=0;index<12;index++){
    if(index)await panel.getByRole('tab',{name:'返回文件列表',exact:true}).click()
    await panel.getByRole('option',{name:new RegExp(`^tab-${index}\\.txt`)}).dblclick()
    await expect(panel.locator('.ew-document:visible .cm-content')).toContainText(`content ${index}`)
    if(dirty)await panel.locator('.ew-document:visible .cm-content').fill(`draft ${index}`)
  }
  await panel.getByRole('tab',{name:'返回文件列表',exact:true}).click()
  const previousURL=page.url()
  await panel.getByRole('option',{name:/^tab-12\.txt/}).dblclick()
  await expect(panel.locator('.ew-document-tab')).toHaveCount(12)
  if(dirty){
    await expect(panel.getByRole('alert')).toContainText('文件标签已满，请保存或关闭部分标签后重试')
    expect(page.url()).toBe(previousURL)
    expect(gets('tab-12.txt')).toHaveLength(0)
    await panel.getByRole('tab',{name:'tab-0.txt •',exact:true}).click()
    await expect(panel.locator('.ew-document:visible .cm-content')).toHaveText('draft 0')
  }else{
    await expect(panel.getByRole('tab',{name:'tab-12.txt',exact:true})).toHaveAttribute('aria-selected','true')
    await expect(panel.getByRole('tab',{name:'tab-0.txt',exact:true})).toHaveCount(0)
    expect(new URLSearchParams(page.url().split('?')[1]).get('file')).toBe('tab-12.txt')
  }
})
