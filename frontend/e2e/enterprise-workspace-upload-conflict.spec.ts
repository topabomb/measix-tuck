import {test,expect,type Page} from '@playwright/test'
import {createHash} from 'node:crypto'
import {fixture} from '../../tests/fixtures/fixture.mjs'
import {workspaceFixture} from '../../tests/fixtures/workspace-fixture.mjs'

let app:Awaited<ReturnType<typeof fixture>>,core:Awaited<ReturnType<typeof workspaceFixture>>
test.beforeEach(async()=>{core=await workspaceFixture();app=await fixture({enterprisePlatform:core.platform})})
test.afterEach(async()=>{try{await app?.close()}finally{await core?.close()}})
const version=(text:string)=>'"'+createHash('sha256').update(text).digest('hex')+'"'
const file=(name:string,text:string)=>({name,mimeType:'text/plain',buffer:Buffer.from(text)})
const writes=()=>core.requests.filter(r=>r.method==='PUT')
async function upload(page:Page,files:ReturnType<typeof file>[]){
  await page.goto(`${app.url}/#/workspace`)
  const panel=page.getByRole('region',{name:'企业文件管理器'})
  await expect(panel.getByRole('option',{name:/hello\.txt/})).toBeVisible()
  const picker=page.waitForEvent('filechooser');await panel.getByRole('button',{name:'上传',exact:true}).click()
  await (await picker).setFiles(files)
  return {panel,dialog:page.getByRole('dialog',{name:/存在同名项目/})}
}

test('same-name upload defaults to confirmed overwrite and reuses the policy with each file version',async({page})=>{
  const old='hello\r\nworld\r\n',oldMarkdown=core.files.get('readme.md')!.toString()
  const {panel,dialog}=await upload(page,[file('hello.txt','first upload'),file('readme.md','second upload')])
  await expect(dialog.getByRole('combobox',{name:'同名处理方式'})).toHaveValue('替换')
  await expect(dialog).toContainText('本次上传的所有同名文件')
  expect(writes()).toHaveLength(0)
  await dialog.getByRole('button',{name:'确定',exact:true}).click()
  await expect(panel.getByRole('region',{name:'上传进度'}).getByRole('status')).toHaveText(['已完成','已完成'])
  expect(core.files.get('hello.txt')!.toString()).toBe('first upload');expect(core.files.get('readme.md')!.toString()).toBe('second upload')
  expect(writes().map(r=>r.headers['if-match'])).toEqual([version(old),version(oldMarkdown)])
  expect(writes().every(r=>!r.headers['if-none-match'])).toBe(true)
})

for(const choice of ['副本','跳过'])test(`same-name ${choice} leaves the original file intact`,async({page})=>{
  const old=core.files.get('hello.txt')!.toString()
  const {panel,dialog}=await upload(page,[file('hello.txt','chosen upload')])
  await dialog.getByRole('combobox',{name:'同名处理方式'}).selectOption(choice)
  await dialog.getByRole('button',{name:'确定',exact:true}).click()
  await expect(panel.getByRole('region',{name:'上传进度'}).getByRole('status')).toHaveText(choice==='副本'?'已完成':'已跳过')
  expect(core.files.get('hello.txt')!.toString()).toBe(old)
  if(choice==='副本'){
    expect(core.files.get('hello (1).txt')!.toString()).toBe('chosen upload')
    expect(writes()[0]!.headers['if-none-match']).toBe('*');expect(writes()[0]!.headers['if-match']).toBeUndefined()
  }else expect(writes()).toHaveLength(0)
})

test('cancelling a same-name choice stops later queued uploads',async({page})=>{
  const old=core.files.get('hello.txt')!.toString()
  const {panel,dialog}=await upload(page,[file('hello.txt','cancelled'),file('queued.txt','also cancelled')])
  await dialog.getByRole('button',{name:'取消',exact:true}).click()
  await expect(panel.getByRole('region',{name:'上传进度'}).getByRole('status')).toHaveText('已取消，未传输该文件')
  await expect(panel.getByRole('button',{name:'刷新',exact:true})).toBeEnabled()
  expect(writes()).toHaveLength(0);expect(core.files.get('hello.txt')!.toString()).toBe(old);expect(core.files.has('queued.txt')).toBe(false)
})

test('a concurrent update rejects overwrite and never automatically retries it',async({page})=>{
  const old=core.files.get('hello.txt')!.toString()
  const {panel,dialog}=await upload(page,[file('hello.txt','stale overwrite')])
  core.put('hello.txt','someone else updated')
  await dialog.getByRole('button',{name:'确定',exact:true}).click()
  await expect(panel.getByRole('alert')).toContainText('同名文件已变化，未覆盖')
  await expect(panel.getByRole('button',{name:'刷新',exact:true})).toBeEnabled()
  expect(core.files.get('hello.txt')!.toString()).toBe('someone else updated')
  expect(writes()).toHaveLength(1);expect(writes()[0]!.headers['if-match']).toBe(version(old))
})

test('a newly appeared destination rejects create rather than overwriting it',async({page})=>{
  await page.route('**/api/enterprise-workspace/content?*',async route=>{
    if(route.request().method()==='PUT'&&new URL(route.request().url()).searchParams.get('path')==='new.txt')core.put('new.txt','created concurrently')
    await route.continue()
  })
  const {panel}=await upload(page,[file('new.txt','must not replace')])
  await expect(panel.getByRole('alert')).toContainText('目标已存在，未覆盖')
  await expect(panel.getByRole('button',{name:'刷新',exact:true})).toBeEnabled()
  expect(core.files.get('new.txt')!.toString()).toBe('created concurrently')
  expect(writes()).toHaveLength(1);expect(writes()[0]!.headers['if-none-match']).toBe('*')
})

test('a same-name directory disables overwrite while allowing a separate file',async({page})=>{
  const {panel,dialog}=await upload(page,[file('folder','separate file')])
  await expect(dialog.getByRole('combobox',{name:'同名处理方式'})).toHaveValue('副本')
  await expect(dialog.getByRole('option',{name:'覆盖文件',exact:true})).toHaveAttribute('disabled','')
  await expect(dialog).toContainText('同名目录或无有效版本的文件不能覆盖')
  await dialog.getByRole('button',{name:'确定',exact:true}).click()
  await expect(panel.getByRole('region',{name:'上传进度'}).getByRole('status')).toHaveText('已完成')
  expect(core.directories.has('folder')).toBe(true);expect(core.files.has('folder')).toBe(false);expect(core.files.get('folder (1)')!.toString()).toBe('separate file')
})
