import {test,expect} from '@playwright/test'
import {fixture} from '../../tests/fixtures/fixture.mjs'
import {workspaceFixture} from '../../tests/fixtures/workspace-fixture.mjs'

let app:Awaited<ReturnType<typeof fixture>>,core:Awaited<ReturnType<typeof workspaceFixture>>
let slowThumbnail=false,thumbnailClosed=false,finishThumbnail:(()=>void)|undefined
test.beforeEach(async()=>{
  slowThumbnail=false;thumbnailClosed=false;finishThumbnail=undefined
  core=await workspaceFixture({content:async(req,res,path)=>{
    if(slowThumbnail&&req.method==='GET'&&path==='grid-00.png'){
      res.once('close',()=>{thumbnailClosed=true})
      await new Promise<void>(resolve=>{finishThumbnail=resolve})
      return res.destroyed
    }
    return false
  }})
  app=await fixture({enterprisePlatform:core.platform})
})
test.afterEach(async()=>{finishThumbnail?.();try{await app?.close()}finally{await core?.close()}})
const reads=(path:string)=>core.requests.filter(r=>r.method==='GET'&&r.path===path&&r.url.includes('/content')).length

test('efficiency: first directory load also supplies the tree without listing the root twice',async({page})=>{
  await page.goto(app.url+'/#/workspace')
  const panel=page.locator('#enterprise-workspace-panel')
  await expect(panel.locator('.ew-file[title="hello.txt"]')).toBeVisible()
  await expect(panel.getByRole('button',{name:'刷新',exact:true})).toBeEnabled()
  await expect(panel.locator('.ew-tree .ew-tree-name[title="folder"]')).toBeVisible()
  expect(core.requests.filter(r=>r.method==='GET'&&r.path===''&&r.url.includes('/files'))).toHaveLength(1)
})

test('efficiency: selecting a file only renders changed rows in a large directory',async({page})=>{
  await page.addInitScript(()=>{
    const metrics=window as unknown as {workspaceDateFormats:number};metrics.workspaceDateFormats=0
    const format=Date.prototype.toLocaleString,stamp=Date.parse('2026-10-26T01:23:45Z')
    Date.prototype.toLocaleString=function(locales?:Intl.LocalesArgument,options?:Intl.DateTimeFormatOptions){if(this.getTime()===stamp)metrics.workspaceDateFormats++;return format.call(this,locales,options)}
  })
  await page.route('**/api/enterprise-workspace/files?**',async route=>{
    const response=await route.fetch(),value=await response.json()
    value.entries.push(...Array.from({length:1000},(_,i)=>({path:`render-${i}.txt`,kind:'FILE',size:1,etag:'"v1"',modifiedAt:'2026-10-26T01:23:45Z'})))
    await route.fulfill({response,json:value})
  })
  await page.goto(app.url+'/#/workspace')
  const panel=page.locator('#enterprise-workspace-panel'),first=panel.locator('.ew-file[title="render-0.txt"]')
  await expect(first).toBeVisible()
  const count=()=>page.evaluate(()=>(window as unknown as {workspaceDateFormats:number}).workspaceDateFormats)
  const before=await count()
  await first.click()
  await expect(first).toHaveAttribute('aria-selected','true')
  expect(await count()-before).toBe(1)
  const next=panel.locator('.ew-file[title="render-1.txt"]'),selected=await count()
  await next.click()
  await expect(next).toHaveAttribute('aria-selected','true')
  await expect(first).toHaveAttribute('aria-selected','false')
  expect(await count()-selected).toBe(2)
})

test('efficiency: changing sort order preserves an in-flight thumbnail of the same version',async({page})=>{
  for(let i=0;i<2;i++)core.put(`grid-0${i}.png`,core.files.get('sample.png')!)
  let attempts=0,release=()=>{}
  const gate=new Promise<void>(resolve=>{release=resolve})
  await page.route('**/api/enterprise-workspace/content?**',async route=>{
    if(route.request().method()==='GET'&&new URL(route.request().url()).searchParams.get('path')==='grid-00.png'){attempts++;await gate}
    await route.continue().catch(()=>{})
  })
  try{
    await page.goto(app.url+'/#/workspace?view=grid&filter=grid-')
    const panel=page.locator('#enterprise-workspace-panel')
    await expect.poll(()=>attempts).toBe(1)
    await expect(panel.locator('.ew-file[title="grid-01.png"] img')).toBeVisible()
    await panel.getByRole('button',{name:'显示选项',exact:true}).click()
    await panel.getByRole('button',{name:'降序',exact:true}).click()
    await expect(page).toHaveURL(/order=desc/)
    // Let observer and watcher updates run before releasing the original read.
    await page.evaluate(()=>new Promise<void>(resolve=>requestAnimationFrame(()=>requestAnimationFrame(()=>resolve()))))
    release()
    await expect(panel.locator('.ew-file[title="grid-00.png"] img')).toBeVisible()
    expect(attempts).toBe(1)
  }finally{release()}
})

for(const width of [390,1365])test(`grid thumbnails read only visible images and reuse them after scrolling at ${width}px`,async({page},info)=>{
  await page.setViewportSize({width,height:760})
  for(let i=0;i<44;i++)core.put(`grid-${String(i).padStart(2,'0')}.png`,core.files.get('sample.png')!)
  await page.goto(app.url+'/#/workspace?view=grid&filter=grid-')
  const panel=page.locator('#enterprise-workspace-panel'),first=panel.locator('.ew-file[title="grid-00.png"]'),last=panel.locator('.ew-file[title="grid-43.png"]')
  await expect(first.locator('img')).toBeVisible()
  const visible=await panel.locator('.ew-file').evaluateAll(nodes=>nodes.filter(node=>{const rect=node.getBoundingClientRect(),root=node.parentElement!.getBoundingClientRect();return rect.bottom>root.top&&rect.top<root.bottom}).map(node=>(node as HTMLElement).title))
  for(const path of visible)await expect(panel.locator(`.ew-file[title="${path}"] img`)).toBeVisible()
  const initialReads=core.requests.filter(r=>r.method==='GET'&&r.path.startsWith('grid-')&&r.url.includes('/content')).length
  expect(initialReads).toBe(visible.length)
  expect(initialReads).toBeLessThan(44)
  expect(reads('grid-43.png')).toBe(0)
  await last.scrollIntoViewIfNeeded()
  await expect(last.locator('img')).toBeVisible()
  expect(reads('grid-43.png')).toBe(1)
  const firstReads=reads('grid-00.png')
  await first.scrollIntoViewIfNeeded()
  await expect(first.locator('img')).toBeVisible()
  expect(reads('grid-00.png')).toBe(firstReads)
  const imageReads=core.requests.filter(r=>r.method==='GET'&&r.path.startsWith('grid-')&&r.url.includes('/content'))
  const lists=core.requests.filter(r=>r.method==='GET'&&r.url.includes('/files'))
  expect(new Set(imageReads.map(r=>r.path)).size).toBe(imageReads.length)
  expect(core.requests.filter(r=>r.method==='HEAD'&&r.path.startsWith('grid-'))).toHaveLength(0)
  const counts={initialImages:initialReads,imagesAfterScrolling:imageReads.length,lists:lists.length,projection:core.requests.filter(r=>new URL(r.url,'http://fixture').pathname.endsWith('/workspace')).length}
  console.info(`grid ${width}px request counts`,counts)
  await info.attach('request-counts',{body:JSON.stringify(counts),contentType:'application/json'})
  await page.screenshot({path:info.outputPath(`grid-${width}.png`)})
})

test('scrolling a grid thumbnail offscreen cancels its upstream read and permits a later reread',async({page})=>{
  slowThumbnail=true
  await page.setViewportSize({width:390,height:760})
  for(let i=0;i<44;i++)core.put(`grid-${String(i).padStart(2,'0')}.png`,core.files.get('sample.png')!)
  await page.goto(app.url+'/#/workspace?view=grid&filter=grid-')
  const panel=page.locator('#enterprise-workspace-panel'),first=panel.locator('.ew-file[title="grid-00.png"]'),last=panel.locator('.ew-file[title="grid-43.png"]')
  await expect.poll(()=>reads('grid-00.png')).toBe(1)
  await last.scrollIntoViewIfNeeded()
  await expect.poll(()=>thumbnailClosed).toBe(true)
  await expect(last.locator('img')).toBeVisible()
  slowThumbnail=false;finishThumbnail?.()
  await first.scrollIntoViewIfNeeded()
  await expect(first.locator('img')).toBeVisible()
  expect(reads('grid-00.png')).toBe(2)
  await expect(panel.getByRole('alert')).toHaveCount(0)
})

for(const failure of ['permission','version'])test(`a ${failure} failure leaves a file icon and refresh recovers without rereading other thumbnails`,async({page})=>{
  for(let i=0;i<2;i++)core.put(`grid-0${i}.png`,core.files.get('sample.png')!)
  let failed=true,attempts=0
  await page.route('**/api/enterprise-workspace/content?**',async route=>{
    if(route.request().method()==='GET'&&new URL(route.request().url()).searchParams.get('path')==='grid-00.png'){
      attempts++
      if(failed){
        if(failure==='permission'){await route.fulfill({status:403,json:{code:'forbidden'}});return}
        core.put('grid-00.png',Buffer.concat([core.files.get('grid-00.png')!,Buffer.from('changed-version')]))
      }
    }
    await route.continue()
  })
  const rejected=page.waitForResponse(response=>response.request().method()==='GET'&&new URL(response.url()).searchParams.get('path')==='grid-00.png')
  await page.goto(app.url+'/#/workspace?view=grid&filter=grid-')
  expect((await rejected).status()).toBe(failure==='permission'?403:412)
  const panel=page.locator('#enterprise-workspace-panel'),first=panel.locator('.ew-file[title="grid-00.png"]'),second=panel.locator('.ew-file[title="grid-01.png"]')
  await expect(second.locator('img')).toBeVisible()
  expect(attempts).toBe(1)
  await expect(first.locator('img')).toHaveCount(0)
  const otherReads=reads('grid-01.png')
  failed=false
  await panel.getByRole('button',{name:'刷新',exact:true}).click()
  await expect(first.locator('img')).toBeVisible()
  expect(attempts).toBe(2)
  expect(reads('grid-01.png')).toBe(otherReads)
})

test('an account change cancels an old thumbnail and releases the previous account cache',async({page})=>{
  slowThumbnail=true
  for(let i=0;i<2;i++)core.put(`grid-0${i}.png`,core.files.get('sample.png')!)
  await page.goto(app.url+'/#/workspace?view=grid&filter=grid-')
  const panel=page.locator('#enterprise-workspace-panel'),first=panel.locator('.ew-file[title="grid-00.png"]'),second=panel.locator('.ew-file[title="grid-01.png"]')
  await expect.poll(()=>reads('grid-00.png')).toBe(1)
  await expect(second.locator('img')).toBeVisible()
  const oldURL=await second.locator('img').getAttribute('src')
  const binding={...app.request({op:'binding'}),userId:'other-user'}
  app.request({op:'setBinding',binding})
  slowThumbnail=false
  await panel.getByRole('button',{name:'刷新',exact:true}).click()
  await expect.poll(()=>thumbnailClosed).toBe(true)
  finishThumbnail?.()
  await expect(first.locator('img')).toBeVisible()
  await expect(second.locator('img')).not.toHaveAttribute('src',oldURL!)
  expect(reads('grid-00.png')).toBe(2)
  expect(reads('grid-01.png')).toBe(2)
})

for(const fail of [false,true])test(`closing the last tab finishes before a ${fail?'failed':'slow'} directory refresh`,async({page})=>{
  await page.goto(app.url+'/#/workspace?file=hello.txt&view=grid')
  const panel=page.locator('#enterprise-workspace-panel')
  await expect(panel.locator('.cm-content')).toBeVisible()
  const initialImageReads=reads('sample.png')
  if(fail)await panel.locator('.cm-content').fill('draft explicitly discarded')
  let release:()=>void=()=>{},started=false
  await page.route('**/api/enterprise-workspace/files?**',async route=>{
    started=true;await new Promise<void>(resolve=>release=resolve)
    try{if(fail)await route.fulfill({status:503,contentType:'application/json',body:JSON.stringify({error:'directory unavailable'})});else await route.continue()}catch{}
  })
  try{
    await panel.getByRole('button',{name:'关闭 hello.txt',exact:true}).click()
    if(fail)await page.getByRole('dialog').getByRole('button',{name:'确定',exact:true}).click()
    await expect.poll(()=>started).toBe(true)
    // The editor must disappear before releasing the network request, so a
    // user cannot type a new draft into a tab already approved for closing.
    await expect(panel.locator('.ew-document')).toHaveCount(0)
    await expect(page).not.toHaveURL(/file=hello/)
    expect(reads('sample.png')).toBe(initialImageReads)
    release()
    if(fail){await expect(panel.getByText(/自动刷新失败.*directory unavailable/)).toBeVisible();expect(reads('sample.png')).toBe(initialImageReads)}
    else await expect(panel.locator('.ew-file[title="sample.png"] img')).toBeVisible()
    await expect(panel.locator('.ew-document')).toHaveCount(0)
    await expect(page.getByRole('dialog')).toHaveCount(0)
  }finally{release()}
})

for(const width of [390,1365])test(`four image resources run concurrently and fill vacancies at ${width}px`,async({page},info)=>{
  await page.setViewportSize({width,height:760})
  for(let i=0;i<6;i++)core.put(`concurrent-${i}.png`,core.files.get('sample.png')!)
  core.put('concurrent.html','<h1>并发图片</h1>'+Array.from({length:6},(_,i)=>`<img width="70" height="70" src="concurrent-${i}.png">`).join(''))
  const started:string[]=[],release=new Map<string,()=>void>();let hold=true
  await page.route('**/api/enterprise-workspace/content?**',async route=>{
    const path=new URL(route.request().url()).searchParams.get('path')!
    if(route.request().method()==='GET'&&path.startsWith('concurrent-')){
      started.push(path);if(hold)await new Promise<void>(resolve=>release.set(path,resolve))
    }
    try{await route.continue()}catch{}
  })
  try{
    await page.goto(app.url+'/#/workspace?file=concurrent.html')
    const frame=page.locator('.ew-document:visible').frameLocator('iframe')
    await expect(frame.locator('h1')).toBeVisible()
    await expect.poll(()=>started.length).toBe(4)
    release.get(started[1]!)!()
    await expect.poll(()=>started.length).toBe(5)
    expect(release.has('concurrent-0.png')).toBe(true)
    await expect(frame.locator('img').nth(1)).toHaveAttribute('data-workspace-state','loaded')
    await expect(frame.locator('img').first()).toHaveAttribute('data-workspace-state','loading')
    hold=false;for(const finish of release.values())finish()
    await expect(frame.locator('img[data-workspace-state="loaded"]')).toHaveCount(6)
    expect(started).toHaveLength(6)
    expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
    await page.screenshot({path:info.outputPath(`concurrent-${width}.png`)})
  }finally{hold=false;for(const finish of release.values())finish()}
})

test('styles load concurrently while their source cascade order survives out-of-order completion',async({page})=>{
  for(let i=0;i<5;i++)core.put(`order-${i}.css`,`h1 {color:rgb(${10+i}, 20, 30)}`)
  core.put('ordered.html',Array.from({length:5},(_,i)=>`<link rel="stylesheet" href="order-${i}.css">`).join('')+'<h1>样式顺序</h1>')
  const started:string[]=[],release=new Map<string,()=>void>();let hold=true
  await page.route('**/api/enterprise-workspace/content?**',async route=>{
    const path=new URL(route.request().url()).searchParams.get('path')!
    if(path.startsWith('order-')){started.push(path);if(hold)await new Promise<void>(resolve=>release.set(path,resolve))}
    try{await route.continue()}catch{}
  })
  try{
    await page.goto(app.url+'/#/workspace?file=ordered.html')
    await expect.poll(()=>started.length).toBe(4)
    release.get('order-1.css')!()
    await expect.poll(()=>started.length).toBe(5)
    hold=false;for(const path of [...release.keys()].reverse())release.get(path)!()
    await expect(page.locator('.ew-document:visible').frameLocator('iframe').locator('h1')).toHaveCSS('color','rgb(14, 20, 30)')
    expect(started).toHaveLength(5)
  }finally{hold=false;for(const finish of release.values())finish()}
})

test('directory thumbnails survive a tab visit, revalidate versions and release the previous directory page',async({page})=>{
  core.put('folder/other.png',core.files.get('sample.png')!)
  await page.goto(app.url+'/#/workspace?view=grid')
  const panel=page.locator('#enterprise-workspace-panel'),thumb=()=>panel.locator('.ew-file[title="sample.png"] img')
  await expect(thumb()).toBeVisible()
  const original=await thumb().getAttribute('src'),initialReads=reads('sample.png')
  await panel.getByRole('button',{name:'刷新',exact:true}).click()
  await expect(thumb()).toHaveAttribute('src',original!)
  expect(reads('sample.png')).toBe(initialReads)
  await panel.locator('.ew-file[title="hello.txt"]').dblclick()
  await expect(panel.locator('.ew-document:visible')).toHaveAttribute('aria-label','hello.txt')
  await panel.getByRole('tab',{name:'返回文件列表',exact:true}).click()
  await expect(thumb()).toHaveAttribute('src',original!)
  expect(reads('sample.png')).toBe(initialReads)
  await panel.locator('.ew-file[title="hello.txt"]').dblclick()
  core.put('sample.png',Buffer.concat([core.files.get('sample.png')!,Buffer.from('new-version')]))
  await panel.getByRole('tab',{name:'返回文件列表',exact:true}).click()
  await expect(thumb()).toBeVisible()
  await expect(thumb()).not.toHaveAttribute('src',original!)
  expect(reads('sample.png')).toBe(initialReads+1)
  const updated=await thumb().getAttribute('src')
  await panel.locator('.ew-file[title="hello.txt"]').dblclick()
  core.put('sample.png',Buffer.concat([core.files.get('sample.png')!,Buffer.from('closed-tab-version')]))
  await panel.getByRole('button',{name:'关闭 hello.txt',exact:true}).click()
  await expect(thumb()).toBeVisible()
  await expect(thumb()).not.toHaveAttribute('src',updated!)
  expect(reads('sample.png')).toBe(initialReads+2)
  await panel.locator('.ew-file[title="folder"]').dblclick()
  await expect(panel.locator('.ew-file[title="folder/other.png"] img')).toBeVisible()
  // The root breadcrumb returns through the normal directory reader.
  await panel.getByRole('navigation',{name:'目录路径'}).getByRole('button',{name:'根目录',exact:true}).click()
  await expect(thumb()).toBeVisible()
  expect(reads('sample.png')).toBe(initialReads+3)
})
