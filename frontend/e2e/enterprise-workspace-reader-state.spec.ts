import {test,expect,type Page} from '@playwright/test'
import {fixture} from '../../tests/fixtures/fixture.mjs'
import {builtInThemes} from '../src/app/themes'
import {workspaceFixture} from '../../tests/fixtures/workspace-fixture.mjs'

let app:Awaited<ReturnType<typeof fixture>>,core:Awaited<ReturnType<typeof workspaceFixture>>
let browserErrors:string[]=[]
test.beforeEach(async({page})=>{browserErrors=[];page.on('pageerror',error=>browserErrors.push(error.message));core=await workspaceFixture();app=await fixture({enterprisePlatform:core.platform})})
test.afterEach(async()=>{try{await app?.close()}finally{await core?.close()}expect(browserErrors).toEqual([])})
const panel=(page:Page)=>page.locator('#enterprise-workspace-panel')
const viewer=(page:Page)=>panel(page).locator('.ew-document:visible')
const reads=(path:string)=>core.requests.filter(request=>request.method==='GET'&&request.path===path&&request.url.includes('/content')).length
async function start(page:Page,file:string,chat=false){
  const target=chat?`chat/conversations/${(await app.manager.create({title:'阅读状态'})).id}?ew=1&wide=1&`:'workspace?'
  await page.goto(`${app.url}/#/${target}file=${encodeURIComponent(file)}`)
  await expect(viewer(page)).toBeVisible()
}
async function tab(page:Page,name:string){await panel(page).getByRole('tab',{name,exact:true}).click();await expect(viewer(page)).toHaveAttribute('aria-label',name)}
async function open(page:Page,name:string){
  await panel(page).getByRole('tab',{name:'返回文件列表',exact:true}).click()
  await panel(page).getByRole('option',{name:new RegExp('^'+name.replaceAll('.','\\.')+'(?:\\s|$)')}).dblclick()
  await expect(viewer(page)).toHaveAttribute('aria-label',name)
}

for(const chat of [false,true])test(`focus reclaims the mobile navigation height and remains escapable (${chat?'chat panel':'workspace channel'})`,async({page},info)=>{
  await start(page,'page.html',chat)
  await expect(viewer(page).frameLocator('iframe').locator('h1')).toHaveText('Static HTML')
  const rail=page.getByRole('navigation',{name:'工作模式',exact:true})
  for(const width of [320,390,1365]){
    await page.setViewportSize({width,height:760})
    await expect(rail).toBeVisible()
    await expect(viewer(page).getByRole('toolbar',{name:'文件查看操作'})).toBeVisible()
    await expect(panel(page).getByRole('button',{name:'复制工作区路径',exact:true})).toBeVisible()
    await page.screenshot({path:info.outputPath(`normal-${width}.png`)})
    const before=await panel(page).boundingBox()
    await panel(page).getByRole('button',{name:'专注内容',exact:true}).click()
    await expect(viewer(page).getByRole('toolbar',{name:'文件查看操作'})).toBeHidden()
    await expect(panel(page).locator('footer')).toBeHidden()
    if(width<=760){
      await expect(rail).toBeHidden()
      expect((await panel(page).boundingBox())!.height-before!.height).toBeGreaterThanOrEqual(54)
    }else await expect(rail).toBeVisible()
    expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
    await page.screenshot({path:info.outputPath(`focus-${width}.png`)})
    await panel(page).getByRole('button',{name:'退出专注内容',exact:true}).click()
    await expect(rail).toBeVisible()
  }
  await page.setViewportSize({width:390,height:760})
  await panel(page).getByRole('button',{name:'专注内容',exact:true}).click()
  await panel(page).focus();await panel(page).press('Escape')
  await expect(rail).toBeVisible()
  await panel(page).getByRole('button',{name:'专注内容',exact:true}).click()
  if(chat){
    await panel(page).getByRole('button',{name:'收起企业工作区',exact:true}).click();await expect(rail).toBeVisible()
    await page.goto(app.url+'/#/chat/conversations/missing?ew=1&focus=1')
    await expect(panel(page)).toHaveCount(0);await expect(rail).toBeVisible()
  }
  else {await page.goto(app.url+'/#/workspace?focus=1');await expect(rail).toBeVisible()}
})

test('recent static previews reuse the frame and resources; eviction restores reading position without rereading text',async({page})=>{
  core.put('reader.css','h1 {color:rgb(12,34,56)}')
  for(const name of ['a','b','c','d','e'])core.put(`${name}.html`,`<link rel="stylesheet" href="reader.css"><h1>${name}</h1><img width="40" height="40" src="sample.png"><div style="height:4000px">long reader</div>`)
  await start(page,'a.html')
  const frame=()=>viewer(page).frameLocator('iframe')
  await expect(frame().locator('img')).toHaveAttribute('data-workspace-state','loaded')
  await frame().locator('body').evaluate(()=>scrollTo(0,700))
  await expect.poll(()=>frame().locator('body').evaluate(()=>scrollY)).toBe(700)
  const first=await viewer(page).locator('iframe').elementHandle(),cssReads=reads('reader.css'),imageReads=reads('sample.png'),textReads=reads('a.html')
  await open(page,'b.html');await expect(frame().locator('h1')).toHaveText('b');await tab(page,'a.html')
  expect(await first!.evaluate(node=>node===document.querySelector('.ew-document:not([style*="display: none"]) iframe'))).toBe(true)
  await expect.poll(()=>frame().locator('body').evaluate(()=>scrollY)).toBe(700)
  // Source/preview is lazy, but unchanged prepared HTML/styles/images survive.
  await viewer(page).getByRole('button',{name:'源码',exact:true}).click()
  await expect(viewer(page).locator('iframe')).toHaveCount(0)
  await viewer(page).getByRole('button',{name:'预览',exact:true}).click()
  await expect.poll(()=>frame().locator('body').evaluate(()=>scrollY)).toBe(700)
  await frame().locator('body').evaluate(()=>scrollTo(0,0))
  await expect(frame().locator('img')).toHaveAttribute('data-workspace-state','loaded')
  // b has its own resources; returning to a performs no extra CSS/image reads.
  expect(reads('reader.css')).toBe(cssReads+1);expect(reads('sample.png')).toBeLessThanOrEqual(imageReads+1)
  await frame().locator('body').evaluate(()=>scrollTo(0,900))
  const retained=await viewer(page).locator('iframe').elementHandle()
  await open(page,'b.html');await open(page,'c.html');await open(page,'d.html')
  await expect(panel(page).locator('.ew-document')).toHaveCount(4)
  expect(await retained!.evaluate(node=>node.isConnected)).toBe(true)
  await open(page,'e.html')
  await expect(panel(page).locator('.ew-document')).toHaveCount(4)
  expect(await retained!.evaluate(node=>node.isConnected)).toBe(false)
  await tab(page,'a.html')
  await expect.poll(()=>frame().locator('body').evaluate(()=>scrollY)).toBe(900)
  expect(reads('a.html')).toBe(textReads)
  await viewer(page).getByRole('button',{name:'源码',exact:true}).click()
  await viewer(page).locator('.cm-content').fill('<h1>new draft</h1><div style="height:4000px"></div>')
  await viewer(page).getByRole('button',{name:'预览',exact:true}).click()
  await expect(frame().locator('h1')).toHaveText('new draft')
  expect(core.files.get('a.html')!.toString()).toContain('<h1>a</h1>')
  await panel(page).getByRole('button',{name:'关闭 a.html',exact:true}).click()
  await page.getByRole('dialog').getByRole('button',{name:'确定',exact:true}).click()
  await expect(panel(page).locator('.ew-document[aria-label="a.html"]')).toHaveCount(0)
})

test('source position, selection and drafts survive renderer eviction',async({page})=>{
  core.put('long.txt',Array.from({length:500},(_,i)=>`line ${i}: reader position`).join('\n'))
  await start(page,'long.txt')
  const editor=()=>viewer(page).locator('.cm-content'),scroll=()=>viewer(page).locator('.cm-scroller')
  await expect(editor()).toContainText('line 0')
  const textReads=reads('long.txt')
  await editor().press('Control+End');await editor().press('End');await editor().press('Shift+Home')
  await scroll().evaluate(node=>node.scrollTo(0,900))
  await expect.poll(()=>scroll().evaluate(node=>node.scrollTop)).toBe(900)
  await open(page,'hello.txt');await open(page,'page.html');await open(page,'readme.md');await open(page,'shape.svg');await tab(page,'long.txt')
  expect(reads('long.txt'),'return after eviction').toBe(textReads)
  await expect.poll(()=>scroll().evaluate(node=>node.scrollTop)).toBe(900)
  await editor().focus()
  expect(await page.evaluate(()=>getSelection()?.toString())).toBe('line 499: reader position')
  await editor().press('Control+End');await editor().press('End');await editor().press('Enter');await editor().press('d')
  expect(reads('long.txt'),'after editing').toBe(textReads)
  await open(page,'hello.txt');await open(page,'page.html');await open(page,'readme.md');await open(page,'shape.svg')
  await panel(page).getByRole('tab',{name:'long.txt •',exact:true}).click()
  await expect(editor()).toContainText('d')
  expect(reads('long.txt')).toBe(textReads)
})

test('PDF and CSV controls and image transforms return with each tab, including after eviction',async({page})=>{
  core.put('rows.csv','name,value\n'+Array.from({length:220},(_,i)=>`row${i},${i}`).join('\n'))
  // Same-length edit preserves the fixture's PDF cross-reference offsets.
  core.put('document.pdf',core.files.get('document.pdf')!.toString().replace('/MediaBox [0 0 300 200]','/MediaBox [0 0 300 999]'))
  await start(page,'document.pdf')
  await expect(viewer(page).locator('.textLayer')).toContainText('Workspace PDF search')
  await viewer(page).getByLabel('PDF 缩放',{exact:true}).selectOption('1.5')
  await viewer(page).getByLabel('查找 PDF 文字',{exact:true}).fill('Workspace')
  await expect.poll(()=>viewer(page).locator('canvas').evaluate(node=>node.getBoundingClientRect().height)).toBe(1498.5)
  await viewer(page).locator('.pdf-scroll').evaluate(node=>node.scrollTo(0,500))
  await expect.poll(()=>viewer(page).locator('.pdf-scroll').evaluate(node=>node.scrollTop)).toBe(500)
  await open(page,'rows.csv')
  await viewer(page).getByRole('button',{name:'下页',exact:true}).click()
  await viewer(page).locator('.csv-scroll').evaluate(node=>node.scrollTo(0,400))
  await open(page,'sample.png')
  await expect(viewer(page).locator('img')).toBeVisible()
  await viewer(page).getByRole('button',{name:'放大图片',exact:true}).click()
  await open(page,'hello.txt');await open(page,'page.html')
  await tab(page,'document.pdf')
  await expect(viewer(page).getByLabel('PDF 缩放',{exact:true})).toHaveValue('1.5')
  await expect(viewer(page).getByLabel('查找 PDF 文字',{exact:true})).toHaveValue('Workspace')
  await expect(viewer(page).locator('.textLayer')).toContainText('Workspace PDF search')
  await expect.poll(()=>viewer(page).locator('.pdf-scroll').evaluate(node=>node.scrollTop)).toBe(500)
  await tab(page,'rows.csv')
  await expect(viewer(page).getByRole('textbox',{name:'第 101 行第 1 列',exact:true})).toBeVisible()
  await expect.poll(()=>viewer(page).locator('.csv-scroll').evaluate(node=>node.scrollTop)).toBe(400)
  await tab(page,'sample.png')
  await expect(viewer(page).locator('.ew-zoom-value')).toHaveText('125%')
  await expect(panel(page).locator('.ew-document')).toHaveCount(4)
})

test('nested HTML scrolling restores after eviction and Markdown retains its preview mode',async({page})=>{
  core.put('nested.html','<h1>Nested reader</h1><main style="height:300px;overflow:auto"><div style="height:3000px">Long nested content</div></main>')
  await start(page,'nested.html')
  const frame=()=>viewer(page).frameLocator('iframe')
  await frame().locator('main').evaluate(node=>node.scrollTo(0,750))
  await expect.poll(()=>frame().locator('main').evaluate(node=>node.scrollTop)).toBe(750)
  await open(page,'readme.md')
  await expect(frame().locator('h1')).toHaveText('Workspace')
  await viewer(page).getByRole('button',{name:'源码',exact:true}).click()
  await expect(viewer(page).locator('.cm-content')).toContainText('# Workspace')
  await open(page,'hello.txt');await open(page,'page.html');await open(page,'shape.svg');await tab(page,'nested.html')
  await expect.poll(()=>frame().locator('main').evaluate(node=>node.scrollTop)).toBe(750)
  await tab(page,'readme.md')
  await expect(viewer(page).getByRole('button',{name:'源码',exact:true})).toHaveAttribute('aria-pressed','true')
  await expect(viewer(page).locator('iframe')).toHaveCount(0)
})

test('media switching releases its source and retains playback position and controls through eviction',async({page})=>{
  await start(page,'sound.wav')
  const audio=()=>viewer(page).locator('audio')
  await expect.poll(()=>audio().evaluate(node=>(node as HTMLAudioElement).readyState)).toBeGreaterThanOrEqual(1)
  await audio().evaluate(node=>{const media=node as HTMLAudioElement;media.currentTime=.3;media.playbackRate=.75;media.volume=.4;media.muted=true})
  await open(page,'hello.txt')
  await expect(panel(page).locator('audio')).not.toHaveAttribute('src')
  await tab(page,'sound.wav')
  await expect(viewer(page).getByRole('button',{name:'继续播放',exact:true})).toBeVisible()
  await viewer(page).getByRole('button',{name:'继续播放',exact:true}).click()
  await expect.poll(()=>audio().evaluate(node=>(node as HTMLAudioElement).readyState)).toBeGreaterThanOrEqual(1)
  await audio().evaluate(node=>(node as HTMLAudioElement).pause())
  expect(await audio().evaluate(node=>(node as HTMLAudioElement).currentTime)).toBeGreaterThanOrEqual(.3)
  await open(page,'hello.txt');await open(page,'page.html');await open(page,'readme.md');await open(page,'shape.svg');await tab(page,'sound.wav')
  await expect.poll(()=>audio().evaluate(node=>(node as HTMLAudioElement).readyState)).toBeGreaterThanOrEqual(1)
  const restored=await audio().evaluate(node=>{const media=node as HTMLAudioElement;return{position:media.currentTime,rate:media.playbackRate,volume:media.volume,muted:media.muted,paused:media.paused}})
  expect(restored.position).toBeGreaterThanOrEqual(.3)
  expect(restored).toMatchObject({rate:.75,volume:.4,muted:true,paused:true})
})

for(const chat of [false,true])test(`footer copies the full workspace path and opens its directory independently (${chat?'chat':'workspace'})`,async({page,context},info)=>{
  const path='docs/工作区说明.md'
  core.directories.add('docs')
  core.put(path,'# 文件内容')
  await context.grantPermissions(['clipboard-read','clipboard-write'])
  await start(page,path,chat)
  await expect(panel(page).locator('.ew-current-file')).toHaveText(path)
  await panel(page).locator('.ew-current-file').click()
  await expect(viewer(page)).toBeVisible()
  await panel(page).getByRole('button',{name:'复制工作区路径',exact:true}).click()
  expect(await page.evaluate(()=>navigator.clipboard.readText())).toBe(path)
  await expect(panel(page).locator('.ew-notice')).toContainText('工作区路径已复制')
  for(const width of [320,390,1365]){
    await page.setViewportSize({width,height:760})
    expect((await panel(page).locator('footer').boundingBox())!.height).toBe(29)
    for(const name of ['打开所在目录','复制工作区路径']){const box=await panel(page).getByRole('button',{name,exact:true}).boundingBox();expect(box!.width).toBe(24);expect(box!.height).toBe(24)}
    const dismiss=panel(page).getByRole('button',{name:'关闭提示',exact:true})
    const size=await dismiss.boundingBox()
    expect(size!.width).toBe(24);expect(size!.height).toBe(24)
    const notice=await panel(page).locator('.ew-dismissible-notice').boundingBox()
    expect(notice!.height).toBe(29)
    expect(notice!.x+notice!.width-size!.x-size!.width).toBeLessThanOrEqual(13)
    expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
    await page.screenshot({path:info.outputPath(`notice-${width}.png`)})
  }
  await panel(page).getByRole('button',{name:'关闭提示',exact:true}).click()
  await expect(panel(page).locator('.ew-dismissible-notice')).toHaveCount(0)
  await panel(page).getByRole('button',{name:'打开所在目录',exact:true}).click()
  await expect(panel(page).getByRole('option',{name:/工作区说明/})).toBeVisible()
  await expect(viewer(page)).toHaveCount(0)
})

test('feedback follows every theme and separates clipboard success from failure',async({page,context},info)=>{
  await context.grantPermissions(['clipboard-read','clipboard-write'])
  await page.setViewportSize({width:390,height:760})
  await page.goto(app.url)
  for(const theme of builtInThemes){
    await page.evaluate(value=>localStorage.setItem('tuck.appearance.v1',JSON.stringify({selected:value,themes:[]})),theme.id)
    await start(page,'readme.md',true)
    await page.reload()
    await panel(page).getByRole('button',{name:'复制工作区路径',exact:true}).click()
    const notice=panel(page).locator('.ew-dismissible-notice')
    await expect(notice).toHaveAttribute('data-kind','success')
    await expect(notice).toHaveText('工作区路径已复制')
    const color=await notice.evaluate(el=>getComputedStyle(el).color)
    const rgb=(hex:string)=>`rgb(${[1,3,5].map(i=>parseInt(hex.slice(i,i+2),16)).join(', ')})`
    expect(color).toBe(rgb(theme.colors.success))
    expect((await notice.boundingBox())!.height).toBe(29)
    await page.screenshot({path:info.outputPath(`success-${theme.id}.png`)})
    await page.evaluate(()=>{navigator.clipboard.writeText=async()=>{throw new Error('denied')};document.execCommand=()=>false})
    await panel(page).getByRole('button',{name:'复制工作区路径',exact:true}).click()
    const error=panel(page).locator(':scope > .ew-error')
    await expect(error).toContainText('复制失败')
    await expect(notice).toHaveCount(0)
    expect(await error.evaluate(el=>getComputedStyle(el).color)).toBe(rgb(theme.colors.danger))
    await page.screenshot({path:info.outputPath(`error-${theme.id}.png`)})
  }
})

for(const failures of [0,1,2])test(`batch feedback distinguishes ${failures} failures`,async({page})=>{
  core.put('batch-a.txt','a');core.put('batch-b.txt','b')
  await page.route('**/files**',async route=>{
    const req=route.request()
    if(req.method()==='POST'){
      const body=req.postDataJSON()
      if(body.action==='DELETE'&&(failures===2||failures===1&&body.path==='batch-b.txt')){
        await route.fulfill({status:409,contentType:'application/json',body:JSON.stringify({error:'测试删除失败'})});return
      }
    }
    await route.continue()
  })
  await page.goto(app.url+'/#/workspace')
  await panel(page).getByRole('option',{name:/^batch-a.txt/}).click()
  await panel(page).getByRole('option',{name:/^batch-b.txt/}).click({modifiers:['Control']})
  await panel(page).getByRole('button',{name:'删除',exact:true}).click()
  await page.getByRole('dialog').getByRole('button',{name:'确定',exact:true}).click()
  const feedback=panel(page).locator('.ew-dismissible-notice')
  await expect(feedback).toHaveAttribute('data-kind',['success','warning','error'][failures]!)
  await expect(feedback).toHaveAttribute('role',failures===2?'alert':'status')
})

test('late clipboard feedback does not leak into a remounted workspace',async({page})=>{
  await start(page,'readme.md')
  await page.evaluate(()=>{navigator.clipboard.writeText=()=>new Promise<void>(resolve=>{(window as any).finishClipboard=resolve})})
  await panel(page).getByRole('button',{name:'复制工作区路径',exact:true}).click()
  await page.goto(app.url+'/#/tasks')
  await expect(panel(page)).toHaveCount(0)
  await start(page,'page.html')
  await page.evaluate(()=>{(window as any).finishClipboard()})
  await expect(panel(page).locator('.ew-dismissible-notice')).toHaveCount(0)
})
