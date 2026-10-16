import {test,expect,type Locator} from '@playwright/test'
import {fixture} from '../../tests/fixtures/fixture.mjs'
import {workspaceFixture} from '../../tests/fixtures/workspace-fixture.mjs'

let app:Awaited<ReturnType<typeof fixture>>,core:Awaited<ReturnType<typeof workspaceFixture>>
test.beforeEach(async()=>{core=await workspaceFixture();core.put('folder/note.txt','responsive document');app=await fixture({enterprisePlatform:core.platform})})
test.afterEach(async()=>{try{await app?.close()}finally{await core?.close()}})

async function density(panel:Locator,compact:boolean){
  for(const name of ['后退','前进']){
    const button=panel.getByRole('button',{name,exact:true,includeHidden:true})
    if(compact)await expect(button).toBeHidden();else await expect(button).toBeVisible()
  }
  await expect(panel.getByRole('button',{name:'上一级',exact:true})).toBeVisible()
  await expect(panel.getByRole('button',{name:'输入路径',exact:true})).toBeVisible()
  const files=panel.getByRole('tab',{name:'返回文件列表',exact:true})
  if(await files.count()){
    await expect(files).toHaveAttribute('title','返回文件列表')
    await expect(files.locator('svg')).toBeVisible()
    if(compact)await expect(files.locator('span')).toBeHidden();else await expect(files.locator('span')).toBeVisible()
  }
}

for(const width of [390,560,1440])test(`workspace navigation and file-list entry fit a ${width}px window`,async({page})=>{
  await page.setViewportSize({width,height:900})
  await page.goto(`${app.url}/#/workspace?space=${core.space}&dir=folder`)
  const panel=page.locator('#enterprise-workspace-panel'),compact=width===390
  await expect(panel.getByRole('option',{name:/note\.txt/})).toBeVisible()
  await density(panel,compact)
  await expect(panel.getByRole('button',{name:'新建文件',exact:true})).toHaveText('新建文件')
  await expect(panel.getByRole('button',{name:'上传',exact:true})).toHaveText('上传')
  await expect(panel.getByRole('button',{name:'刷新',exact:true})).toHaveText('')
  await panel.getByRole('button',{name:'上一级',exact:true}).click()
  await expect(panel.getByRole('option',{name:/hello\.txt/})).toBeVisible()
  await panel.getByRole('button',{name:'输入路径',exact:true}).click()
  const path=panel.getByRole('textbox',{name:'工作区路径',exact:true})
  await path.fill('folder');await path.press('Enter')
  await panel.getByRole('option',{name:/note\.txt/}).dblclick()
  await expect(panel.locator('.cm-content')).toHaveText('responsive document')
  await density(panel,compact)
  await panel.locator('.cm-content').fill('draft survives compact file-list navigation')
  await panel.getByRole('tab',{name:'返回文件列表',exact:true}).click()
  await expect(panel.getByRole('option',{name:/note\.txt/})).toBeVisible()
  await panel.getByRole('option',{name:/note\.txt/}).click()
  const download=panel.getByRole('button',{name:'下载',exact:true})
  await expect(download).toBeEnabled();await expect(download.locator('svg')).toBeVisible()
  if(compact)await expect(download.locator('span')).toBeHidden();else await expect(download.locator('span')).toBeVisible()
  await panel.getByRole('tab',{name:'note.txt •',exact:true}).click()
  await expect(panel.locator('.cm-content')).toHaveText('draft survives compact file-list navigation')
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
})

test('desktop side-panel controls follow the resized panel rather than viewport width',async({page})=>{
  await page.setViewportSize({width:1440,height:900})
  await page.addInitScript(()=>localStorage.setItem('tuck-enterprise-width','420'))
  const task=await app.manager.create({title:'实际工作区宽度'})
  await page.goto(`${app.url}/#/chat/conversations/${task.id}?ew=1&file=hello.txt`)
  const panel=page.getByRole('complementary',{name:'企业工作区',exact:true})
  await expect(panel.locator('.cm-content')).toContainText('hello')
  await panel.locator('.cm-content').fill('stable resized draft')
  const editor=await panel.locator('.cm-content').elementHandle()
  await density(panel,true)
  const separator=panel.getByRole('separator',{name:'调整工作区宽度',exact:true})
  await separator.focus()
  for(let i=0;i<8;i++)await separator.press('ArrowLeft')
  await expect.poll(async()=>Math.round((await panel.boundingBox())!.width)).toBe(580)
  await density(panel,false)
  for(let i=0;i<12;i++)await separator.press('ArrowRight')
  await expect.poll(async()=>Math.round((await panel.boundingBox())!.width)).toBe(340)
  await density(panel,true)
  expect(await editor!.evaluate(node=>node===document.querySelector('#enterprise-workspace-panel .cm-content'))).toBe(true)
  await expect(panel.locator('.cm-content')).toHaveText('stable resized draft')
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
})
