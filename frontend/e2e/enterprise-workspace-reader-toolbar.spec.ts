import {test,expect,type Page} from '@playwright/test'
import {fixture} from '../../tests/fixtures/fixture.mjs'
import {workspaceFixture} from '../../tests/fixtures/workspace-fixture.mjs'

let app:Awaited<ReturnType<typeof fixture>>,core:Awaited<ReturnType<typeof workspaceFixture>>
let slow=false,release:(()=>void)|undefined
test.beforeEach(async()=>{
  slow=false;release=undefined
  core=await workspaceFixture({content:async(req,res,path)=>{
    if(slow&&req.method==='GET'&&path==='slow.png'){
      await new Promise<void>(resolve=>{release=resolve})
      return res.destroyed
    }
    return false
  }})
  app=await fixture({enterprisePlatform:core.platform})
})
test.afterEach(async()=>{release?.();try{await app?.close()}finally{await core?.close()}})
async function open(page:Page,path:string,width=1440){
  await page.setViewportSize({width,height:900})
  const task=await app.manager.create({title:'文件查看工具栏'})
  await page.goto(`${app.url}/#/chat/conversations/${task.id}?ew=1&wide=1&file=${encodeURIComponent(path)}`)
  return page.getByRole('complementary',{name:'企业工作区'})
}

for(const entry of ['deep link','HTML link'])test(`an image opened by ${entry} navigates its own directory without a matching browser directory`,async({page})=>{
  for(const path of ['folder/a.png','folder/b.png','unrelated.png'])core.put(path,core.files.get('sample.png')!)
  core.put('links.html','<h1>跨目录图片</h1><a href="folder/a.png">打开相册</a>')
  const panel=await open(page,entry==='deep link'?'folder/a.png':'links.html')
  if(entry==='HTML link')await panel.frameLocator('iframe').getByRole('link',{name:'打开相册',exact:true}).click()
  await expect(panel.getByRole('region',{name:'a.png',exact:true}).getByRole('img',{name:'a.png',exact:true})).toBeVisible()
  await expect(panel.getByRole('button',{name:'上一张',exact:true})).toBeDisabled()
  await expect(panel.getByRole('button',{name:'下一张',exact:true})).toBeEnabled()
  await panel.getByRole('button',{name:'下一张',exact:true}).click()
  await expect(panel.getByRole('tab',{name:'b.png',exact:true})).toHaveAttribute('aria-selected','true')
  await expect(panel.getByRole('button',{name:'下一张',exact:true})).toBeDisabled()
  await panel.getByRole('button',{name:'上一张',exact:true}).click()
  await expect(panel.getByRole('tab',{name:'a.png',exact:true})).toHaveAttribute('aria-selected','true')
  await expect(panel.getByRole('tab',{name:'unrelated.png',exact:true})).toHaveCount(0)
})

for(const width of [1440,560,390])test(`the ${width}px image viewer has one compact accessible toolbar and keeps room for the image`,async({page})=>{
  const panel=await open(page,'sample.png',width),toolbar=panel.getByRole('toolbar',{name:'文件查看操作',exact:true})
  await expect(panel.locator('.image-stage img')).toBeVisible()
  const geometry=await toolbar.evaluate(node=>({height:node.getBoundingClientRect().height,overflow:node.scrollWidth-node.clientWidth,icons:Array.from(node.querySelectorAll('svg')).filter(svg=>svg.getBoundingClientRect().width>0).map(svg=>({width:svg.getBoundingClientRect().width,height:svg.getBoundingClientRect().height}))}))
  expect(geometry.height).toBeLessThanOrEqual(42)
  expect(geometry.overflow).toBeLessThanOrEqual(1)
  expect(geometry.icons.length).toBeGreaterThanOrEqual(6)
  for(const icon of geometry.icons){expect(icon.width).toBe(16);expect(icon.height).toBe(16)}
  await expect(panel.locator('.ew-image-tools')).toHaveCount(0)
  expect((await panel.locator('.image-stage').boundingBox())!.height).toBeGreaterThan(500)
  await toolbar.getByRole('button',{name:'放大图片',exact:true}).click()
  await expect(toolbar.getByText('125%',{exact:true})).toBeVisible()
  await toolbar.getByRole('button',{name:'适应窗口',exact:true}).click()
  await expect(toolbar.getByText('100%',{exact:true})).toBeVisible()
  const more=toolbar.getByRole('button',{name:'文档操作',exact:true})
  if(width===390){
    await expect(toolbar.getByRole('link',{name:'下载',exact:true})).toHaveCount(0)
    await more.press('Enter')
    await expect(toolbar.getByRole('link',{name:'下载',exact:true})).toBeVisible()
    await expect(toolbar.getByRole('button',{name:'重新读取',exact:true})).toBeVisible()
    const popup=await toolbar.locator('details[open]>div').boundingBox(),bounds=(await panel.boundingBox())!
    expect(popup!.x).toBeGreaterThanOrEqual(bounds.x);expect(popup!.x+popup!.width).toBeLessThanOrEqual(bounds.x+bounds.width)
  }else{
    await expect(toolbar.getByRole('link',{name:'下载',exact:true})).toBeVisible()
    const reread=toolbar.getByRole('button',{name:'重新读取',exact:true})
    await expect(reread).toBeVisible()
    await expect(reread).toHaveText('')
    expect((await reread.boundingBox())!.width).toBe(28)
    expect((await reread.boundingBox())!.height).toBe(28)
    await more.press('Enter')
  }
  await more.press('Escape');await expect(more).toBeFocused()
  await expect(panel).toBeVisible()
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
})

test('preview failures stay prominent during another image read and keyboard retry keeps its menu usable',async({page})=>{
  slow=true
  core.put('retry.png',core.files.get('sample.png')!);core.put('slow.png',core.files.get('sample.png')!)
  core.put('failures.html','<h1>资源提示</h1><img id="retry" width="80" height="80" src="retry.png"><img id="slow" width="80" height="80" src="slow.png"><img src="https://blocked.invalid/track.png"><img src="../outside.png">')
  let fail=true;const external:string[]=[]
  page.on('request',request=>{if(request.url().includes('blocked.invalid'))external.push(request.url())})
  await page.route('**/api/enterprise-workspace/content?**',route=>{
    if(fail&&new URL(route.request().url()).searchParams.get('path')==='retry.png')return route.fulfill({status:503,json:{error:'图片暂时不可用'}})
    return route.continue()
  })
  const panel=await open(page,'failures.html',390),frame=panel.frameLocator('iframe'),info=panel.getByRole('button',{name:'预览说明与链接',exact:true})
  await expect(frame.locator('#retry')).toHaveAttribute('data-workspace-state','failed')
  await expect.poll(()=>!!release).toBe(true)
  await expect(frame.locator('#slow')).toHaveAttribute('data-workspace-state','loading')
  const badge=panel.getByRole('status',{name:'部分资源未加载',exact:true})
  await expect(badge).toBeVisible();await expect(badge).toHaveText('1 项失败')
  expect(await badge.evaluate(node=>parseFloat(getComputedStyle(node).fontSize))).toBeGreaterThanOrEqual(10)
  await info.press('Enter')
  await expect(panel.getByText('2 个外部或无效图片已阻止',{exact:false})).toBeVisible()
  const retry=panel.getByRole('button',{name:'重试',exact:true})
  await retry.focus();fail=false;await retry.press('Enter')
  await expect(panel.locator('.ew-links')).toHaveAttribute('open','')
  await expect.poll(()=>panel.locator('.ew-links').evaluate(node=>node.contains(document.activeElement))).toBe(true)
  release!()
  await expect(frame.locator('#retry')).toHaveAttribute('data-workspace-state','loaded')
  await expect(badge).toBeVisible();await expect(badge).toHaveText('资源异常')
  await page.keyboard.press('Escape');await expect(info).toBeFocused()
  await expect(panel).toBeVisible()
  expect(external).toEqual([])
  expect(core.requests.some(request=>request.path==='outside.png'||request.path.includes('blocked.invalid'))).toBe(false)
})
