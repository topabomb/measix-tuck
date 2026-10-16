import {pathToFileURL} from 'node:url'
import { test, expect, type Page } from '@playwright/test'
import { readFile, readdir } from 'node:fs/promises'
import { fixture } from '../../tests/fixtures/fixture.mjs'
import { workspaceFixture } from '../../tests/fixtures/workspace-fixture.mjs'

let app:Awaited<ReturnType<typeof fixture>>,core:Awaited<ReturnType<typeof workspaceFixture>>
test.beforeAll(async()=>{core=await workspaceFixture();app=await fixture({enterprisePlatform:core.platform})})
test.afterAll(async()=>{await app?.close();await core?.close()})
async function workspace(page:Page,title:string) {
  const task=await app.manager.create({title})
  await page.goto(app.url+`/#/chat/conversations/${task.id}?ew=1&wide=1`)
  const panel=page.getByRole('complementary',{name:'企业工作区'})
  await expect(panel.getByRole('option',{name:/hello\.txt/})).toBeVisible()
  return panel
}

test('HTML Markdown and JSON highlight while plain text retains the full editor',async({page})=>{
  const files=[['syntax.html','<!doctype html><h1 class="title">Hello</h1>'],['syntax.md','# Heading\n\n**Bold** and `code`'],['syntax.json','{"enabled":true,"count":3}'],['syntax.txt','Plain text\nSecond line']]
  for(const [path,content] of files)core.put(path!,content!)
  const panel=await workspace(page,'语法高亮验收')
  for(const [path,content] of files){
    if(await panel.getByRole('tab',{name:'返回文件列表',exact:true}).count())await panel.getByRole('tab',{name:'返回文件列表',exact:true}).click()
    await panel.getByRole('option',{name:new RegExp(path!.replace('.','\\.'))}).dblclick()
    await panel.getByRole('button',{name:'源码',exact:true}).click()
    await expect.poll(()=>panel.locator('.ew-document:visible .cm-line').allTextContents()).toEqual(content!.split('\n'))
    if(!path!.endsWith('.txt'))await expect(panel.locator('.ew-document:visible .cm-line span[class]')).not.toHaveCount(0)
    await expect(panel.locator('.workspace-basic-editor')).toHaveCount(0)
  }
})

for(const file of [{path:'hello.txt',chunk:'TextEditor',label:'编辑器'},{path:'document.pdf',chunk:'PdfPreview',label:'PDF 预览'}])test(`${file.label} module failures show feedback and reload recovers the same file`,async({page})=>{
  const asset=new RegExp(`/assets/${file.chunk}-[^/]+\\.js$`)
  await page.route(asset,route=>route.abort('failed'))
  const panel=await workspace(page,'文档模块加载失败')
  await panel.getByRole('option',{name:new RegExp(file.path.replace('.','\\.'))}).dblclick()
  if(file.chunk==='TextEditor') {
    await expect(panel.getByRole('status')).toContainText('已切换为基础文本编辑')
    await expect(panel.getByRole('textbox',{name:'文件源码',exact:true})).toHaveValue('hello\nworld\n')
  }else await expect(panel.getByRole('alert')).toContainText(`${file.label}加载失败，请刷新页面重试`)
  await expect(panel.getByRole('button',{name:'文档操作',exact:true})).toBeVisible()
  await page.unroute(asset)
  await page.reload()
  await expect(panel.getByRole('tab',{name:file.path,exact:true})).toHaveAttribute('aria-selected','true')
  if(file.chunk==='TextEditor')await expect(panel.locator('.cm-content')).toContainText('hello')
  else await expect(panel.locator('.pdf-sheet canvas')).toBeVisible()
  await expect(panel.getByRole('alert')).toHaveCount(0)
})

test('language highlighting failure keeps code editable and shows a recovery hint',async({page})=>{
  core.put('highlight.py','print("original")\n')
  const errors:string[]=[];page.on('pageerror',error=>errors.push(error.message))
  // The editor's CodeMirror parser and chat's Shiki Python grammar are distinct.
  const assets=new URL('assets/',process.env.TUCK_FRONTEND_DIR ? pathToFileURL(process.env.TUCK_FRONTEND_DIR+'/') : new URL('../dist/',import.meta.url))
  const modules=await Promise.all((await readdir(assets)).filter(name=>name.endsWith('.js.map')).map(async name=>{
    const map=JSON.parse(await readFile(new URL(name,assets),'utf8')) as {sources:string[]}
    return map.sources.some(source=>source.includes('/@codemirror/lang-python/'))?name.slice(0,-4):''
  }))
  const parsers=modules.filter(Boolean);expect(parsers.length).toBeGreaterThan(0)
  let blocked=0
  await page.route(url=>parsers.some(name=>url.pathname===`/assets/${name}`),route=>{blocked++;return route.abort('failed')})
  const panel=await workspace(page,'代码高亮加载失败')
  await panel.getByRole('option',{name:/highlight\.py/}).dblclick()
  await expect(panel.getByRole('status')).toContainText('语法高亮未加载，仍可编辑')
  await expect(panel.locator('.cm-content')).toContainText('print("original")')
  await panel.locator('.cm-content').fill('print("edited")\n')
  await panel.getByRole('button',{name:'保存',exact:true}).click()
  await expect.poll(()=>core.files.get('highlight.py')?.toString()).toBe('print("edited")\n')
  expect(blocked).toBeGreaterThan(0)
  expect(errors).toEqual([])
})

test('CSV page follows remaining rows after deletion and source edits',async({page})=>{
  core.put('pages.csv',Array.from({length:101},(_,index)=>`row-${index+1}`).join('\n')+'\n')
  const panel=await workspace(page,'CSV 页码边界')
  await panel.getByRole('option',{name:/pages\.csv/}).dblclick()
  await panel.getByRole('button',{name:'下页',exact:true}).click()
  await panel.getByRole('textbox',{name:'第 101 行第 1 列',exact:true}).focus()
  await panel.getByRole('button',{name:'删当前行',exact:true}).click()
  await expect(panel.getByText('1 / 1',{exact:true})).toBeVisible()
  await expect(panel.getByRole('textbox',{name:'第 1 行第 1 列',exact:true})).toHaveValue('row-1')
  await panel.getByRole('button',{name:'加行',exact:true}).click()
  await panel.getByRole('button',{name:'下页',exact:true}).click()
  await expect(panel.getByText('2 / 2',{exact:true})).toBeVisible()
  await panel.getByRole('button',{name:'源码',exact:true}).click()
  await panel.locator('.cm-content').fill('remaining\n')
  await panel.getByRole('button',{name:'预览',exact:true}).click()
  await expect(panel.getByText('1 / 1',{exact:true})).toBeVisible()
  await expect(panel.getByRole('textbox',{name:'第 1 行第 1 列',exact:true})).toHaveValue('remaining')
})

test('image navigation respects boundaries and never jumps into another directory',async({page})=>{
  core.put('second.png',core.files.get('sample.png')!)
  core.put('folder/other.png',core.files.get('sample.png')!)
  const panel=await workspace(page,'图片目录边界')
  await panel.getByRole('option',{name:/sample\.png/}).dblclick()
  await expect(panel.locator('.ew-document:visible .image-stage img')).toBeVisible()
  await expect(panel.getByRole('button',{name:'上一张',exact:true})).toBeDisabled()
  await panel.getByRole('button',{name:'下一张',exact:true}).click()
  await expect(panel.getByRole('tab',{name:'second.png',exact:true})).toHaveAttribute('aria-selected','true')
  await expect(panel.getByRole('button',{name:'下一张',exact:true})).toBeDisabled()
  await panel.getByRole('button',{name:'上一张',exact:true}).click()
  await expect(panel.getByRole('tab',{name:'sample.png',exact:true})).toHaveAttribute('aria-selected','true')
  if(await panel.getByRole('tab',{name:'返回文件列表',exact:true}).count())await panel.getByRole('tab',{name:'返回文件列表',exact:true}).click()
  await panel.getByRole('option',{name:/folder/}).dblclick()
  await expect(panel.getByRole('option',{name:/other\.png/})).toBeVisible()
  await panel.getByRole('tab',{name:'sample.png',exact:true}).click()
  await expect(panel.locator('.ew-document:visible .image-stage img')).toBeVisible()
  await expect(panel.getByRole('button',{name:'上一张',exact:true})).toBeDisabled()
  await panel.getByRole('button',{name:'下一张',exact:true}).click()
  await expect(panel.getByRole('tab',{name:'second.png',exact:true})).toHaveAttribute('aria-selected','true')
  await expect(panel.getByRole('button',{name:'下一张',exact:true})).toBeDisabled()
  await expect(panel.getByRole('tab',{name:'other.png',exact:true})).toHaveCount(0)
})

test('uppercase JSON formatting preserves exact numbers and duplicate members when saved',async({page})=>{
  core.put('record.JSON','{"id":9007199254740993,"id":-0,"limit":1e400}')
  const panel=await workspace(page,'JSON 保真格式化')
  await panel.getByRole('option',{name:/record\.JSON/}).dblclick()
  await expect(panel.locator('.cm-content')).toContainText('9007199254740993')
  await panel.getByRole('button',{name:'文档操作',exact:true}).click()
  await panel.getByRole('button',{name:'格式化 JSON',exact:true}).click()
  await panel.getByRole('button',{name:'保存',exact:true}).click()
  await expect.poll(()=>core.files.get('record.JSON')?.toString()).toBe('{\n  "id": 9007199254740993,\n  "id": -0,\n  "limit": 1e400\n}\n')
})
