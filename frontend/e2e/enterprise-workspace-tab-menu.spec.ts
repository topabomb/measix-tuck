import {test,expect,type Page} from '@playwright/test'
import {fixture} from '../../tests/fixtures/fixture.mjs'
import {workspaceFixture} from '../../tests/fixtures/workspace-fixture.mjs'

let app:Awaited<ReturnType<typeof fixture>>,core:Awaited<ReturnType<typeof workspaceFixture>>
test.beforeEach(async()=>{core=await workspaceFixture();app=await fixture({enterprisePlatform:core.platform})})
test.afterEach(async()=>{try{await app?.close()}finally{await core?.close()}})
async function open(page:Page,names:string[]){
  for(const name of names)core.put(name,name)
  await page.goto(app.url+'/#/workspace')
  const panel=page.getByRole('region',{name:'企业文件管理器'})
  for(const name of names){
    await panel.getByRole('option',{name:new RegExp(name.replace('.','\\.'))}).dblclick()
    if(name==='menu-b.txt')await panel.locator('.ew-document:visible .cm-content').fill('keep until confirmed')
    if(name!==names.at(-1))await panel.getByRole('tab',{name:'返回文件列表',exact:true}).click()
  }
  return panel
}

test('tab menu closes other or all tabs and confirms unsaved drafts once',async({page})=>{
  const panel=await open(page,['menu-a.txt','menu-b.txt','menu-c.txt'])
  const more=panel.getByRole('button',{name:'更多标签操作',exact:true})
  await more.click();await panel.getByRole('button',{name:'关闭其他',exact:true}).click()
  const confirm=page.getByRole('dialog',{name:/关闭 2 个标签并放弃其中 1 个文件的未保存修改/})
  await expect(confirm).toContainText('menu-b.txt')
  await confirm.getByRole('button',{name:'取消',exact:true}).click()
  await expect(panel.locator('.ew-document-tab')).toHaveCount(3)
  await more.click();await panel.getByRole('button',{name:'关闭其他',exact:true}).click()
  await confirm.getByRole('button',{name:'确定',exact:true}).click()
  await expect(panel.locator('.ew-document-tab')).toHaveCount(1)
  await expect(panel.getByRole('tab',{name:'menu-c.txt',exact:true})).toHaveAttribute('aria-selected','true')
  expect(core.files.get('menu-b.txt')?.toString()).toBe('menu-b.txt')
  await more.click()
  await expect(panel.getByRole('button',{name:'关闭其他',exact:true})).toBeDisabled()
  await panel.getByRole('button',{name:'关闭所有',exact:true}).click()
  await expect(panel.getByRole('tablist')).toHaveCount(0)
  await expect(panel.getByRole('listbox',{name:'企业文件',exact:true})).toBeVisible()
  await expect(page).not.toHaveURL(/file=/)
})

test('the icon-only tab menu stays at the right edge while overflowing tabs scroll',async({page},info)=>{
  const panel=await open(page,Array.from({length:8},(_,index)=>`wide-tab-${index}.txt`))
  const more=panel.getByRole('button',{name:'更多标签操作',exact:true}),tabs=panel.getByRole('tablist')
  await expect(more).toHaveText('')
  await expect(more.locator('svg')).toBeVisible()
  for(const width of [1365,390,320]){
    await page.setViewportSize({width,height:900})
    const before=(await more.boundingBox())!,edge=(await panel.boundingBox())!
    expect(Math.abs(edge.x+edge.width-before.x-before.width-5)).toBeLessThan(2)
    await tabs.evaluate(el=>{el.scrollLeft=0})
    await expect(panel.locator('.ew-file-list-label')).toBeVisible()
    await expect(panel.locator('.ew-file-list-label')).toHaveText('目录')
    await panel.getByRole('tab',{name:'返回文件列表',exact:true}).click()
    await expect(panel.locator('.ew-create-tools')).toBeVisible()
    expect((await more.boundingBox())!.y).toBe(before.y)
    expect((await panel.locator('.ew-create-tools').boundingBox())!.y).toBeGreaterThanOrEqual((await panel.locator('.ew-tab-bar').boundingBox())!.y+(await panel.locator('.ew-tab-bar').boundingBox())!.height)
    await panel.getByRole('tab',{name:'wide-tab-7.txt',exact:true}).click()
    await expect(panel.locator('.ew-create-tools')).toHaveCount(0)
    expect((await more.boundingBox())!.y).toBe(before.y)
    await tabs.evaluate(el=>{el.scrollLeft=el.scrollWidth})
    expect((await more.boundingBox())!.x).toBe(before.x)
    await more.press('Enter')
    await expect(panel.getByRole('button',{name:'关闭所有',exact:true})).toBeVisible()
    await page.screenshot({path:info.outputPath(`tab-menu-${width}.png`)})
    await panel.getByRole('button',{name:'关闭所有',exact:true}).press('Escape')
    await expect(more).toBeFocused()
    expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
  }
})
