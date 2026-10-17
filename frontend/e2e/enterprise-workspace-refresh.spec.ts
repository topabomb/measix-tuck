import { test, expect, type Page } from '@playwright/test'
import { fixture } from '../../tests/fixtures/fixture.mjs'
import { workspaceFixture } from '../../tests/fixtures/workspace-fixture.mjs'

let app:Awaited<ReturnType<typeof fixture>>,core:Awaited<ReturnType<typeof workspaceFixture>>
test.beforeEach(async()=>{core=await workspaceFixture();app=await fixture({enterprisePlatform:core.platform})})
test.afterEach(async()=>{await app?.close();await core?.close()})

const projectionRequest=(url:string)=>new URL(url).pathname==='/api/enterprise-workspace'
async function openWorkspace(page:Page,query=''){
  await page.clock.setFixedTime(new Date('2030-01-01T00:00:00Z'))
  const task=await app.manager.create({title:'自动刷新验收'})
  await page.goto(app.url+`/#/chat/conversations/${task.id}?ew=1${query}`)
  const panel=page.getByRole('complementary',{name:'企业工作区'})
  await expect(panel.getByRole('option',{name:/hello\.txt/})).toBeVisible()
  return panel
}
async function focus(page:Page,afterCooldown=true){
  if(afterCooldown)await page.clock.setFixedTime(new Date(await page.evaluate(()=>Date.now())+11000))
  await page.evaluate(()=>window.dispatchEvent(new Event('focus')))
}
async function holdProjection(page:Page){
  let release=()=>{},seen=()=>{},calls=0,cancelled=false
  const gate=new Promise<void>(resolve=>{release=resolve}),started=new Promise<void>(resolve=>{seen=resolve})
  page.on('request',request=>{if(projectionRequest(request.url()))calls++})
  page.on('requestfailed',request=>{if(projectionRequest(request.url()))cancelled=true})
  await page.route('**/api/enterprise-workspace',async route=>{
    seen();await gate
    // A cancelled background response must never surface this obsolete error.
    await route.fulfill({status:503,json:{error:'obsolete automatic refresh failure'}}).catch(()=>{})
  },{times:1})
  return {release,started,calls:()=>calls,cancelled:()=>cancelled}
}

test('failed focus refreshes respect the cooldown and successful navigation clears their feedback',async({page})=>{
  const panel=await openWorkspace(page)
  core.put('folder/inside.txt','inside')
  let fail=true,reads=0
  await page.route('**/api/enterprise-workspace/files?*',async route=>{
    if(fail&&new URL(route.request().url()).searchParams.get('path')===''){
      reads++;await route.fulfill({status:503,json:{error:'目录服务暂不可用'}});return
    }
    await route.continue()
  })
  await focus(page)
  await expect(panel.getByText(/自动刷新失败/)).toBeVisible()
  await expect(panel.getByRole('option',{name:/hello\.txt/})).toBeVisible()
  await focus(page,false);await focus(page,false)
  await page.evaluate(()=>new Promise<void>(resolve=>requestAnimationFrame(()=>requestAnimationFrame(()=>resolve()))))
  expect(reads).toBe(1)
  fail=false
  await panel.getByRole('button',{name:'输入路径',exact:true}).click()
  await panel.getByRole('textbox',{name:'工作区路径'}).fill('folder')
  await panel.getByRole('button',{name:'打开路径',exact:true}).click()
  await expect(panel.getByRole('option',{name:/inside\.txt/})).toBeVisible()
  await expect(panel.getByText(/自动刷新失败/)).toHaveCount(0)
})

test('focus refresh retains the list and yields immediately to foreground directory navigation',async({page})=>{
  const panel=await openWorkspace(page)
  core.put('folder/inside.txt','inside the requested directory')
  const list=panel.getByRole('listbox',{name:'企业文件'}),original=await list.elementHandle()
  const held=await holdProjection(page)
  try{
    await focus(page);await held.started
    await focus(page,false);await focus(page,false)
    expect(held.calls()).toBe(1)
    for(const name of ['新建文件','上传','刷新'])await expect(panel.getByRole('button',{name,exact:true})).toBeEnabled()
    await expect(list).toBeVisible()
    expect(await original!.evaluate(el=>el.isConnected)).toBe(true)
    await panel.getByRole('option',{name:/^.*folder.*文件夹/}).dblclick()
    await expect(panel.getByRole('option',{name:/inside\.txt/})).toBeVisible()
    await expect(page).toHaveURL(/dir=folder(?:&|$)/)
    await expect.poll(held.cancelled).toBe(true)
    held.release()
    await expect(panel.getByText('obsolete automatic refresh failure')).toHaveCount(0)
    await expect(panel.getByRole('option',{name:/hello\.txt/})).toHaveCount(0)
  }finally{held.release()}
})

for(const operation of ['save','upload'] as const)test(`foreground ${operation} preempts a delayed automatic refresh`,async({page})=>{
  const panel=await openWorkspace(page)
  if(operation==='save'){
    await panel.getByRole('option',{name:/hello\.txt/}).dblclick()
    await expect(panel.locator('.cm-content')).toBeVisible()
    await panel.locator('.cm-content').fill('saved while refresh was pending')
  }
  const held=await holdProjection(page)
  try{
    await focus(page);await held.started
    if(operation==='save'){
      await panel.getByRole('button',{name:'保存',exact:true}).click()
      await expect.poll(()=>core.files.get('hello.txt')?.toString()).toBe('saved while refresh was pending')
      await expect(panel.getByRole('button',{name:'保存',exact:true})).toBeDisabled()
    }else{
      const chooserReady=page.waitForEvent('filechooser')
      await panel.getByRole('button',{name:'上传',exact:true}).click()
      const chooser=await chooserReady
      await chooser.setFiles({name:'refresh-priority.txt',mimeType:'text/plain',buffer:Buffer.from('upload takes priority')})
      await expect.poll(()=>core.files.get('refresh-priority.txt')?.toString()).toBe('upload takes priority')
      await expect(panel.getByRole('region',{name:'上传进度'}).getByRole('status')).toHaveText('服务端已确认保存')
    }
    await expect.poll(held.cancelled).toBe(true)
    held.release()
    await expect(panel.getByText('obsolete automatic refresh failure')).toHaveCount(0)
  }finally{held.release()}
})

test('a delayed automatic directory response cannot replace a newer foreground location',async({page})=>{
  const panel=await openWorkspace(page)
  core.put('folder/foreground.txt','foreground directory wins')
  let release=()=>{},seen=()=>{}
  const gate=new Promise<void>(resolve=>{release=resolve}),started=new Promise<void>(resolve=>{seen=resolve})
  await page.route('**/api/enterprise-workspace/files?*',async route=>{
    const snapshot=await route.fetch()
    seen();await gate
    await route.fulfill({response:snapshot}).catch(()=>{})
  },{times:1})
  try{
    await focus(page);await started
    await panel.getByRole('option',{name:/^.*folder.*文件夹/}).dblclick()
    await expect(panel.getByRole('option',{name:/foreground\.txt/})).toBeVisible()
    await expect(page).toHaveURL(/dir=folder(?:&|$)/)
    release()
    await page.evaluate(()=>new Promise<void>(resolve=>requestAnimationFrame(()=>requestAnimationFrame(()=>resolve()))))
    await expect(panel.getByRole('option',{name:/foreground\.txt/})).toBeVisible()
    await expect(panel.getByRole('option',{name:/hello\.txt/})).toHaveCount(0)
  }finally{release()}
})

test('closing the panel cancels automatic reads and an obsolete error cannot lock the reopened panel',async({page})=>{
  const panel=await openWorkspace(page),errors:string[]=[]
  page.on('pageerror',error=>errors.push(error.message))
  const held=await holdProjection(page)
  try{
    await focus(page);await held.started
    await panel.getByRole('button',{name:'收起企业工作区',exact:true}).click()
    await expect(panel).toHaveCount(0)
    await expect.poll(held.cancelled).toBe(true)
    await page.getByRole('button',{name:'工作区',exact:true}).click()
    await expect(panel.getByRole('option',{name:/hello\.txt/})).toBeVisible()
    await expect(panel.getByRole('button',{name:'新建文件',exact:true})).toBeEnabled()
    held.release()
    await expect(panel.getByText('obsolete automatic refresh failure')).toHaveCount(0)
    expect(errors).toEqual([])
  }finally{held.release()}
})

test('automatic refresh errors are independent from file errors and operation notices',async({page})=>{
  const panel=await openWorkspace(page)
  await panel.getByRole('button',{name:'新建文件',exact:true}).click()
  const dialog=page.getByRole('dialog',{name:'新建文件名称',exact:true})
  await dialog.getByRole('textbox').fill('invalid/name')
  await dialog.getByRole('button',{name:'确定',exact:true}).click()
  const fileError=panel.getByText('名称不能包含路径分隔符',{exact:true})
  await expect(fileError).toBeVisible()
  await panel.getByRole('option',{name:/hello\.txt/}).click()
  await panel.getByRole('button',{name:/^操作(?: \(|$)/}).click()
  await panel.getByRole('button',{name:'复制',exact:true}).click()
  const notice=panel.getByRole('status').filter({hasText:'已复制 1 个项目'})
  await expect(notice).toBeVisible()
  await page.route('**/api/enterprise-workspace',route=>route.fulfill({status:503,json:{error:'automatic refresh temporarily unavailable'}}),{times:1})
  await focus(page)
  await expect(panel.getByText(/automatic refresh temporarily unavailable/)).toBeVisible()
  await expect(fileError).toBeVisible();await expect(notice).toBeVisible()
  await expect(panel.getByRole('option',{name:/hello\.txt/})).toBeVisible()
  core.put('refresh-recovered.txt','newly visible after recovery')
  await focus(page)
  await expect(panel.getByRole('option',{name:/refresh-recovered\.txt/})).toBeVisible()
  await expect(panel.getByText(/automatic refresh temporarily unavailable/)).toHaveCount(0)
  await expect(fileError).toBeVisible();await expect(notice).toBeVisible()
})

test('unchanged refresh preserves thumbnails, successful focus is cooled down and changed entries update',async({page})=>{
  const panel=await openWorkspace(page,'&view=grid')
  const image=panel.getByRole('option',{name:/sample\.png/}).locator('img')
  await expect(image).toBeVisible()
  const original=await image.getAttribute('src')
  const imageReads=()=>core.requests.filter((request:{method:string;path:string;url:string})=>request.method==='GET'&&request.path==='sample.png'&&new URL(request.url,'http://core.test').pathname.endsWith('/content')).length
  const baseline=imageReads()
  let projections=0
  page.on('request',request=>{if(projectionRequest(request.url()))projections++})
  const refreshed=page.waitForResponse(response=>new URL(response.url()).pathname==='/api/enterprise-workspace/files'&&response.request().method()==='GET')
  await focus(page);await (await refreshed).finished()
  await page.evaluate(()=>new Promise<void>(resolve=>requestAnimationFrame(()=>requestAnimationFrame(()=>resolve()))))
  await expect(image).toHaveAttribute('src',original!)
  expect(imageReads()).toBe(baseline)
  await focus(page,false);await focus(page,false)
  // A foreground browser round-trip lets any focus-triggered requests reach the listener.
  await page.getByRole('button',{name:'工作区',exact:true}).getAttribute('aria-expanded')
  expect(projections).toBe(1)
  await panel.getByRole('option',{name:/hello\.txt/}).click()
  core.files.delete('hello.txt')
  // PNG decoders allow trailing bytes; changing them changes the strong ETag without dimensions.
  core.put('sample.png',Buffer.concat([core.files.get('sample.png')!,Buffer.from('new-version')]))
  await focus(page)
  await expect.poll(imageReads).toBe(baseline+1)
  await expect(image).not.toHaveAttribute('src',original!)
  await expect.poll(()=>image.evaluate((element:HTMLImageElement)=>element.naturalWidth)).toBeGreaterThan(0)
  await expect(panel.getByRole('option',{name:/hello\.txt/})).toHaveCount(0)
  await expect(panel.getByRole('button',{name:/^操作(?: \(|$)/})).toBeDisabled()
  await expect(panel.locator('footer')).not.toContainText('已选')
})

test('thumbnails without ETags follow metadata changes and never indefinitely cache unversioned entries',async({page})=>{
  let unversioned=false,modifiedAt='2026-10-11T00:00:00Z'
  await page.route('**/api/enterprise-workspace/files?*',async route=>{
    if(route.request().method()!=='GET'){await route.continue();return}
    const response=await route.fetch(),value=await response.json()
    const entry=value.entries.find((item:{path:string})=>item.path==='sample.png')
    if(entry){delete entry.etag;if(unversioned)delete entry.modifiedAt;else entry.modifiedAt=modifiedAt}
    await route.fulfill({response,json:value})
  })
  const panel=await openWorkspace(page,'&view=grid')
  const image=panel.getByRole('option',{name:/sample\.png/}).locator('img')
  await expect(image).toBeVisible()
  const reads=()=>core.requests.filter(request=>request.method==='GET'&&request.path==='sample.png'&&new URL(request.url,'http://core.test').pathname.endsWith('/content')).length
  const baseline=reads()
  let previous=await image.getAttribute('src')
  const expectUpdated=async(count:number)=>{
    await focus(page)
    await expect.poll(reads).toBe(count)
    await expect(image).not.toHaveAttribute('src',previous!)
    await expect.poll(()=>image.evaluate((element:HTMLImageElement)=>element.naturalWidth)).toBeGreaterThan(0)
    previous=await image.getAttribute('src')
  }
  // No ETag is returned: size and modification time must invalidate the old image.
  core.put('sample.png',Buffer.concat([core.files.get('sample.png')!,Buffer.from('metadata-version')]))
  modifiedAt='2026-10-11T00:00:01Z'
  await expectUpdated(baseline+1)
  unversioned=true
  await expectUpdated(baseline+2)
  // Even an identical listing cannot establish freshness without any version fields.
  await expectUpdated(baseline+3)
})
