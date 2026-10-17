import {test,expect,type Page} from '@playwright/test'
import {fixture} from '../../tests/fixtures/fixture.mjs'
import {workspaceFixture} from '../../tests/fixtures/workspace-fixture.mjs'

let app:Awaited<ReturnType<typeof fixture>>,core:Awaited<ReturnType<typeof workspaceFixture>>
test.beforeEach(async()=>{core=await workspaceFixture();app=await fixture({enterprisePlatform:core.platform})})
test.afterEach(async()=>{try{await app?.close()}finally{await core?.close()}})
async function open(page:Page){
  await page.goto(app.url+'/#/workspace?file=hello.txt')
  const panel=page.getByRole('region',{name:'企业文件管理器'}),editor=panel.locator('.ew-document:visible .cm-content')
  await expect(editor).toContainText('hello')
  return {panel,editor,status:panel.getByRole('status',{name:'文件保存状态',exact:true})}
}

test('save feedback waits for confirmation and content verification, and survives viewer eviction',async({page})=>{
  const {panel,editor,status}=await open(page)
  let releaseWrite=()=>{},releaseRead=()=>{}
  const writeGate=new Promise<void>(resolve=>{releaseWrite=resolve}),readGate=new Promise<void>(resolve=>{releaseRead=resolve})
  let writing=false
  await page.route('**/api/enterprise-workspace/content?*',async route=>{
    if(route.request().method()==='PUT'){writing=true;await writeGate}
    else if(writing&&route.request().method()==='GET'&&new URL(route.request().url()).searchParams.get('path')==='hello.txt')await readGate
    await route.continue()
  })
  try{
    await editor.fill('saved revision')
    await expect(status).toHaveText('有未保存修改')
    await panel.getByRole('button',{name:'保存',exact:true}).click()
    await expect(status).toHaveText('正在保存，等待服务端确认')
    releaseWrite()
    await expect(status).toHaveText('服务端已确认，正在核对文件内容')
    expect(core.files.get('hello.txt')?.toString()).toBe('saved revision')
    // Edits made while confirmation is pending are a newer, unsaved revision.
    await editor.fill('newer draft')
    releaseRead()
    await expect(status).toHaveText('有未保存修改')
    await editor.fill('saved revision')
    await expect(status).toHaveText('服务端已确认保存')
    for(const name of ['readme.md','page.html']){
      await panel.getByRole('tab',{name:'返回文件列表',exact:true}).click()
      await panel.getByRole('option',{name:new RegExp(name.replace('.','\\.'))}).dblclick()
      await expect(panel.locator('.ew-document:visible')).toHaveAttribute('aria-label',name)
    }
    await panel.getByRole('tab',{name:'hello.txt',exact:true}).click()
    await expect(status).toHaveText('服务端已确认保存')
    await panel.getByRole('button',{name:'重新读取',exact:true}).click()
    await expect(editor).toHaveText('saved revision')
    await expect(status).toHaveCount(0)
  }finally{releaseWrite();releaseRead()}
})

test('a lost save response keeps the draft and never displays confirmed save',async({page})=>{
  const {panel,editor,status}=await open(page)
  await page.route('**/api/enterprise-workspace/content?*',async route=>{
    if(route.request().method()==='PUT')await route.abort('connectionreset')
    else await route.continue()
  })
  await editor.fill('retained draft')
  await panel.getByRole('button',{name:'保存',exact:true}).click()
  await expect(status).toHaveText('保存未确认，请核对远端内容；当前草稿已保留')
  await expect(editor).toHaveText('retained draft')
  await expect(panel.getByRole('tab',{name:'hello.txt •',exact:true})).toBeVisible()
  await expect(panel.getByRole('button',{name:'保存',exact:true})).toBeEnabled()
})

test('a directory refresh failure after verified saving keeps the confirmed save visible',async({page})=>{
  const {panel,editor,status}=await open(page)
  await page.route('**/api/enterprise-workspace/files?*',route=>route.fulfill({status:503,json:{error:'directory unavailable'}}))
  await editor.fill('saved despite listing failure')
  await panel.getByRole('button',{name:'保存',exact:true}).click()
  await expect(status).toHaveText('服务端已确认保存')
  expect(core.files.get('hello.txt')?.toString()).toBe('saved despite listing failure')
  await expect(panel.locator('.ew-document:visible').getByRole('alert')).toContainText('无法读取目录')
})
