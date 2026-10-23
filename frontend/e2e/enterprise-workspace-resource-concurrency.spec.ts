import {test,expect} from '@playwright/test'
import {fixture} from '../../tests/fixtures/fixture.mjs'
import {workspaceFixture} from '../../tests/fixtures/workspace-fixture.mjs'

let app:Awaited<ReturnType<typeof fixture>>,core:Awaited<ReturnType<typeof workspaceFixture>>
test.beforeEach(async()=>{core=await workspaceFixture();app=await fixture({enterprisePlatform:core.platform})})
test.afterEach(async()=>{try{await app?.close()}finally{await core?.close()}})
const reads=(path:string)=>core.requests.filter(r=>r.method==='GET'&&r.path===path&&r.url.includes('/content')).length

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
