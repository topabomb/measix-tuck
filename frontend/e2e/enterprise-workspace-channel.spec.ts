import {test,expect,type Page} from '@playwright/test'
import {fixture} from '../../tests/fixtures/fixture.mjs'
import {workspaceFixture} from '../../tests/fixtures/workspace-fixture.mjs'
import { openSettings, closeSettings } from './settings'

let app:Awaited<ReturnType<typeof fixture>>,core:Awaited<ReturnType<typeof workspaceFixture>>
test.beforeEach(async()=>{core=await workspaceFixture();app=await fixture({enterprisePlatform:core.platform})})
test.afterEach(async()=>{try{await app?.close()}finally{await core?.close()}})

const panel=(page:Page)=>page.locator('#enterprise-workspace-panel')
const rail=(page:Page)=>page.getByRole('navigation',{name:'工作模式',exact:true})
const reads=(path:string)=>core.requests.filter(request=>request.method==='GET'&&request.path===path&&request.url.includes('/content')).length
const position=(page:Page)=>new URLSearchParams(page.url().split('?').slice(1).join('?'))

test('stale workspace deep links recover to the current root without opening a same-name file',async({page})=>{
  core.put('folder/old.txt','must not open automatically')
  await page.goto(`${app.url}/#/workspace?space=old-local-space&dir=folder&file=folder%2Fold.txt&filter=missing&anchor=old`)
  await expect(panel(page).getByRole('option',{name:/hello\.txt/})).toBeVisible()
  await expect(panel(page).getByRole('alert')).toHaveCount(0)
  await expect(panel(page).getByRole('status')).toContainText('工作区链接已更新')
  expect(position(page).get('space')).toBe(core.space)
  for(const key of ['dir','file','filter','anchor'])expect(position(page).has(key)).toBe(false)
  expect(reads('folder/old.txt')).toBe(0)
  await expect(panel(page).getByRole('button',{name:'上传',exact:true})).toBeEnabled()
  await page.reload()
  await expect(panel(page).getByRole('option',{name:/hello\.txt/})).toBeVisible()
  expect(reads('folder/old.txt')).toBe(0)
})
async function projection(page:Page,value:()=>Record<string,unknown>) {
  await page.route(url=>url.pathname==='/api/enterprise-workspace',async route=>{
    const response=await route.fetch()
    await route.fulfill({response,json:{...await response.json(),...value()}})
  })
}

async function configureConnection(page:Page){
  await openSettings(page)
  await page.getByRole('navigation',{name:'设置分类'}).getByRole('link',{name:'企业平台',exact:true}).click()
  await page.getByText('更换企业平台接入',{exact:true}).click()
  await page.getByTestId('platform-json').fill('{"fixture":"workspace-space-check"}')
  await page.getByTestId('platform-enroll').click()
  const settings=page.locator('.tuck-platform-modal')
  await expect(settings.getByRole('status')).toHaveText('企业平台已接入')
  await closeSettings(page)
}

test('connection refresh recovers stale space links including navigation while runtime is pending',async({page})=>{
  const errors:string[]=[];page.on('pageerror',error=>errors.push(error.message))
  const runtime=await(await page.request.get(app.url+'/api/runtime')).json()
  await page.route('**/api/connection/enroll',route=>route.fulfill({json:runtime.connection}))
  let statuses=0,holdRuntime=false,runtimeHeld=false,release!:()=>void
  const delayed=new Promise<void>(resolve=>{release=resolve})
  await page.route(url=>url.pathname==='/api/enterprise-workspace',async route=>{
    statuses++;await route.fulfill({response:await route.fetch()})
  })
  await page.route('**/api/runtime',async route=>{
    const response=await route.fetch()
    if(holdRuntime){runtimeHeld=true;await delayed}
    await route.fulfill({response})
  })
  try{
    await page.goto(`${app.url}/#/workspace?space=other-space`)
    await expect(panel(page).getByRole('option',{name:/hello\.txt/})).toBeVisible()
    const before=statuses
    const synced=page.waitForResponse(response=>new URL(response.url()).pathname==='/api/enterprise-workspace')
    await configureConnection(page)
    await(await synced).finished()
    await page.evaluate(()=>new Promise<void>(resolve=>requestAnimationFrame(()=>requestAnimationFrame(()=>resolve()))))
    await expect.poll(()=>statuses).toBeGreaterThan(before)
    await expect(panel(page).getByRole('option',{name:/hello\.txt/})).toBeVisible()
    await expect(panel(page).getByRole('button',{name:'新建文件',exact:true})).toBeEnabled()
    await expect(panel(page).getByRole('button',{name:'上传',exact:true})).toBeEnabled()
    expect(core.requests.some(request=>request.url.includes('/content'))).toBe(false)

    await page.evaluate(space=>{location.hash=`/workspace?space=${space}`},core.space)
    await expect(panel(page).getByRole('option',{name:/hello\.txt/})).toBeVisible()
    holdRuntime=true
    await configureConnection(page)
    await expect.poll(()=>runtimeHeld).toBe(true)
    await page.evaluate(()=>{location.hash='/workspace?space=other-space'})
    await expect(panel(page).getByRole('option',{name:/hello\.txt/})).toBeVisible()
    const wrongStatuses=statuses
    const resynced=page.waitForResponse(response=>new URL(response.url()).pathname==='/api/enterprise-workspace')
    release()
    await(await resynced).finished()
    await page.evaluate(()=>new Promise<void>(resolve=>requestAnimationFrame(()=>requestAnimationFrame(()=>resolve()))))
    await expect.poll(()=>statuses).toBeGreaterThan(wrongStatuses)
    await expect(panel(page).getByRole('option',{name:/hello\.txt/})).toBeVisible()
    await expect(panel(page).getByRole('button',{name:'新建文件',exact:true})).toBeEnabled()
    await expect(panel(page).getByRole('button',{name:'上传',exact:true})).toBeEnabled()
    expect(position(page).get('space')).toBe(core.space)
    expect(core.requests.some(request=>['PUT','POST','DELETE','MOVE','COPY'].includes(request.method))).toBe(false)
    expect(errors).toEqual([])
  }finally{release()}
})

test('an obsolete connection projection cannot disable a newer successful workspace restore',async({page})=>{
  core.put('folder/current.txt','current restore owns the workspace')
  const errors:string[]=[];page.on('pageerror',error=>errors.push(error.message))
  const runtime=await(await page.request.get(app.url+'/api/runtime')).json()
  await page.route('**/api/connection/enroll',route=>route.fulfill({json:runtime.connection}))
  let hold=false,held=false,release!:()=>void
  const delayed=new Promise<void>(resolve=>{release=resolve})
  await page.route(url=>url.pathname==='/api/enterprise-workspace',async route=>{
    const response=await route.fetch()
    if(hold){hold=false;held=true;await delayed}
    await route.fulfill({response})
  })
  try{
    await page.goto(`${app.url}/#/workspace?space=${core.space}`)
    await expect(panel(page).getByRole('option',{name:/hello\.txt/})).toBeVisible()
    hold=true
    await configureConnection(page)
    await expect.poll(()=>held).toBe(true)
    await page.evaluate(space=>{location.hash=`/workspace?space=${space}&dir=folder`},core.space)
    await expect(panel(page).getByRole('option',{name:/current\.txt/})).toBeVisible()
    const oldResponse=page.waitForResponse(response=>new URL(response.url()).pathname==='/api/enterprise-workspace')
    release();await(await oldResponse).finished()
    await page.evaluate(()=>new Promise<void>(resolve=>requestAnimationFrame(()=>requestAnimationFrame(()=>resolve()))))
    await expect(panel(page).getByRole('alert')).toHaveCount(0)
    await expect(panel(page).getByRole('button',{name:'新建文件',exact:true})).toBeEnabled()
    await expect(panel(page).getByRole('button',{name:'上传',exact:true})).toBeEnabled()
    await panel(page).getByRole('option',{name:/current\.txt/}).dblclick()
    await expect(panel(page).locator('.ew-document:visible .cm-content')).toHaveText('current restore owns the workspace')
    expect(position(page).get('space')).toBe(core.space)
    expect(position(page).get('dir')).toBe('folder')
    expect(errors).toEqual([])
  }finally{release()}
})

for(const start of ['startup','restore'])test(`an obsolete ${start} projection cannot override the latest space URL`,async({page})=>{
  let release!:()=>void,requests=0
  const delayed=new Promise<void>(resolve=>{release=resolve})
  await page.route(url=>url.pathname==='/api/enterprise-workspace',async route=>{
    const response=await route.fetch(),body={...await response.json(),serviceState:'ENABLED'}
    if(++requests===1)await delayed
    await route.fulfill({response,json:body})
  })
  await page.goto(`${app.url}/#/${start==='startup'?'tasks':'workspace?space=other-space'}`)
  await expect.poll(()=>requests).toBe(1)
  const target=start==='startup'?'other-space':core.space
  await page.evaluate(space=>{location.hash=`/workspace?space=${space}`},target)
  if(start==='startup')await expect(panel(page).getByRole('option',{name:/hello\.txt/})).toBeVisible()
  else await expect(panel(page).getByRole('listbox',{name:'企业文件',exact:true}).getByRole('option',{name:/hello\.txt/})).toBeVisible()
  const oldResponse=page.waitForResponse(response=>new URL(response.url()).pathname==='/api/enterprise-workspace')
  release();await (await oldResponse).finished()
  await page.evaluate(()=>new Promise<void>(resolve=>requestAnimationFrame(()=>requestAnimationFrame(()=>resolve()))))
  await expect.poll(()=>requests).toBe(start==='startup'?3:2)
  if(start==='startup'){
    await expect(panel(page).getByRole('button',{name:'新建文件',exact:true})).toBeEnabled()
    await expect(panel(page).getByRole('button',{name:'上传',exact:true})).toBeEnabled()
    await expect(panel(page).getByRole('option',{name:/hello\.txt/})).toBeVisible()
    expect(core.requests.some(request=>request.url.includes('/content'))).toBe(false)
  }else{
    await expect(panel(page).getByRole('button',{name:'新建文件',exact:true})).toBeEnabled()
    await expect(panel(page).getByRole('alert')).toHaveCount(0)
  }
  expect(position(page).get('space')).toBe(core.space)
})

test('standalone workspace restores an encoded deep link and document after refresh without creating a conversation',async({page})=>{
  core.put('folder/中文 空格.txt','独立工作区深链内容')
  const query=new URLSearchParams({space:core.space,dir:'folder',file:'folder/中文 空格.txt',sort:'name',order:'desc',view:'grid'})
  const count=app.manager.tasks.size
  await page.goto(`${app.url}/#/workspace?${query}`)
  await expect(panel(page).locator('.ew-document:visible .cm-content')).toHaveText('独立工作区深链内容')
  await expect(page).toHaveTitle('工作区 · Tuck')
  await expect(rail(page).getByRole('link',{name:'工作区',exact:true})).toHaveAttribute('aria-current','page')
  await expect(panel(page).getByRole('separator')).toHaveCount(0)
  await expect(panel(page).getByRole('button',{name:/^(展开工作区|还原工作区|收起企业工作区)$/})).toHaveCount(0)
  await page.reload()
  await expect(panel(page).locator('.ew-document:visible .cm-content')).toHaveText('独立工作区深链内容')
  for(const [key,value] of query)expect(position(page).get(key)).toBe(value)
  expect(app.manager.tasks.size).toBe(count)
})

test('chat panel and workspace channel share cached documents and drafts without a leave confirmation',async({page})=>{
  const task=await app.manager.create({title:'跨频道草稿'})
  core.put('folder/second.txt','第二份已读取的文件')
  await page.goto(`${app.url}/#/chat/conversations/${task.id}?ew=1&dir=folder&file=folder%2Fsecond.txt`)
  await expect(panel(page).locator('.ew-document:visible .cm-content')).toHaveText('第二份已读取的文件')
  await panel(page).getByRole('tab',{name:'返回文件列表',exact:true}).click()
  await panel(page).getByRole('button',{name:'根目录',exact:true}).click()
  await panel(page).getByRole('option',{name:/hello\.txt/}).dblclick()
  await panel(page).locator('.ew-document:visible .cm-content').fill('跨频道保留的未保存草稿')
  const helloReads=reads('hello.txt'),secondReads=reads('folder/second.txt')
  await rail(page).getByRole('link',{name:'工作区',exact:true}).click()
  await expect(page).toHaveURL(/#\/workspace\?/)
  await expect(panel(page).locator('.ew-document:visible .cm-content')).toHaveText('跨频道保留的未保存草稿')
  await expect(page.getByRole('dialog')).toHaveCount(0)
  expect(position(page).get('file')).toBe('hello.txt')
  expect(position(page).get('space')).toBe(core.space)
  await panel(page).getByRole('tab',{name:'second.txt',exact:true}).click()
  await expect(panel(page).locator('.ew-document:visible .cm-content')).toHaveText('第二份已读取的文件')
  await rail(page).getByRole('link',{name:'对话',exact:true}).click()
  await expect(page).toHaveURL(new RegExp(`#/chat/conversations/${task.id}\\?`))
  await expect(panel(page).locator('.ew-document:visible .cm-content')).toHaveText('跨频道保留的未保存草稿')
  await expect(page.getByRole('dialog')).toHaveCount(0)
  expect(reads('hello.txt')).toBe(helloReads)
  expect(reads('folder/second.txt')).toBe(secondReads)
  expect(core.files.get('hello.txt')?.toString()).toBe('hello\r\nworld\r\n')
  await rail(page).getByRole('link',{name:'任务',exact:true}).click()
  const confirmation=page.getByRole('dialog',{name:/未保存修改/})
  await expect(confirmation).toBeVisible()
  await confirmation.getByRole('button',{name:'取消',exact:true}).click()
  await expect(panel(page).locator('.ew-document:visible .cm-content')).toHaveText('跨频道保留的未保存草稿')
})

for(const serviceState of ['NOT_CONFIGURED','DISABLED'])test(`${serviceState} disables the workspace channel without hiding its accessible entry`,async({page})=>{
  await projection(page,()=>({serviceState,filesAvailable:false,filesReason:'企业未启用工作区'}))
  await page.goto(`${app.url}/#/tasks`)
  const entry=rail(page).getByRole('button',{name:'工作区',exact:true})
  await expect(entry).toBeVisible()
  await expect(entry).toBeDisabled()
  await expect(rail(page).getByRole('link',{name:'工作区',exact:true})).toHaveCount(0)
  await expect(page).toHaveURL(`${app.url}/#/tasks`)
})

test('enabled but unavailable workspace remains reachable and refresh recovers its file browser',async({page})=>{
  let available=false
  await projection(page,()=>({serviceState:'ENABLED',filesAvailable:available,state:available?'CONNECTED':'DISCONNECTED',filesReason:available?'':'文件服务暂时不可用，请刷新重试'}))
  await page.goto(`${app.url}/#/tasks`)
  await rail(page).getByRole('link',{name:'工作区',exact:true}).click()
  await expect(panel(page).getByRole('alert')).toContainText('文件服务暂时不可用')
  await expect(panel(page).getByRole('button',{name:'新建文件',exact:true})).toBeDisabled()
  available=true
  await panel(page).getByRole('button',{name:'刷新',exact:true}).click()
  await expect(panel(page).getByRole('option',{name:/hello\.txt/})).toBeVisible()
  await expect(panel(page).getByRole('button',{name:'新建文件',exact:true})).toBeEnabled()
  await expect(panel(page).getByRole('alert')).toHaveCount(0)
})

test('workspace channel remembers directory filters and sort direction and exposes a compact mobile document view',async({page})=>{
  await page.setViewportSize({width:390,height:844})
  core.put('folder/report-a.html','<h1>移动工作区正文</h1>')
  core.put('folder/report-b.html','<h1>第二份正文</h1>')
  core.put('folder/unrelated.txt','not a report')
  const query=new URLSearchParams({space:core.space,dir:'folder',filter:'report-',sort:'name',order:'desc',view:'grid'})
  await page.goto(`${app.url}/#/workspace?${query}`)
  const files=panel(page).getByRole('listbox',{name:'企业文件',exact:true})
  await expect(files.getByRole('option')).toHaveCount(2)
  await expect(files.getByRole('option').first()).toContainText('report-b.html')
  await rail(page).getByRole('link',{name:'任务',exact:true}).click()
  await expect(page).toHaveURL(`${app.url}/#/tasks`)
  await rail(page).getByRole('link',{name:'工作区',exact:true}).click()
  await expect(panel(page).getByRole('textbox',{name:'筛选当前目录',exact:true})).toHaveValue('report-')
  for(const [key,value] of query)expect(position(page).get(key)).toBe(value)
  await page.reload()
  await expect(files.getByRole('option').first()).toContainText('report-b.html')
  await files.getByRole('option',{name:/report-a\.html/}).dblclick()
  await expect(panel(page).frameLocator('iframe[title="文件静态预览"]').getByRole('heading',{name:'移动工作区正文',exact:true})).toBeVisible()
  await expect(panel(page).getByRole('navigation',{name:'目录树',exact:true})).toHaveCount(0)
  await expect(panel(page).getByRole('button',{name:/分栏|显示目录树|隐藏目录树/})).toHaveCount(0)
  const source=panel(page).getByRole('button',{name:'源码',exact:true})
  await source.click()
  await expect(panel(page).locator('.ew-document:visible .cm-content')).toContainText('<h1>移动工作区正文</h1>')
  await panel(page).getByRole('button',{name:'预览',exact:true}).click()
  await expect(panel(page).frameLocator('iframe[title="文件静态预览"]').getByRole('heading',{name:'移动工作区正文',exact:true})).toBeVisible()
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
})
