import { openSettings, closeSettings } from './settings'
import { expect, test, type Locator, type Page } from '@playwright/test'
import { fixture, until } from '../../tests/fixtures/fixture.mjs'

async function keyboardFocus(page:Page, target:Locator, contrast=false) {
  const before=await target.boundingBox()
  await target.focus()
  await page.keyboard.press('Tab')
  await page.keyboard.press('Shift+Tab')
  await expect(target).toBeFocused()
  const style=await target.evaluate((el,contrast)=>{
    const css=getComputedStyle(el)
    const color=css.getPropertyValue(contrast?'--tuck-accentText':'--tuck-accent').trim()
    return {outline:css.outlineStyle,width:css.outlineWidth,offset:css.outlineOffset,color:css.outlineColor,expected:`rgb(${color.slice(1).match(/../g)!.map(c=>parseInt(c,16)).join(', ')})`,shadow:css.boxShadow}
  },contrast)
  expect(style).toMatchObject({outline:'solid',width:'2px',offset:'-2px',color:style.expected,shadow:'none'})
  const after=await target.boundingBox()
  expect(after!.width).toBe(before!.width); expect(after!.height).toBe(before!.height)
}

test('focus is contained and theme-correct across forms, sidebar, dialogs and teleported agent menus',async({page})=>{
  const app=await fixture()
  try {
    const root=await app.manager.create({title:'焦点验收'})
    for(let i=0;i<8;i++) app.manager.createChild(root,`focus-child-${i}`,`worker-${i}`)
    app.manager.submit(root.id,'SCHEMA_QUESTION')
    await until(()=>root.pendingInteraction)
    await page.goto(app.url)
    for(const theme of ['苔灰','暖砂','炭棕','曜黑（高对比）','石墨']) {
      await openSettings(page)
      await page.getByRole('navigation',{name:'设置分类'}).getByRole('link',{name:'外观',exact:true}).click()
      const selected=page.getByRole('button',{name:`${theme}`})
      await selected.click()
      await expect(selected).toHaveAttribute('aria-pressed','true')
      expect(await selected.evaluate(el=>getComputedStyle(el).outlineStyle)).toBe('none')
      await keyboardFocus(page,selected)
      await closeSettings(page)
      for(const field of ['是否附带说明','数量','备注','风格']) await keyboardFocus(page,page.getByLabel(field,{exact:true}))
      await keyboardFocus(page,page.getByTestId('approve-interaction'),true)
      await keyboardFocus(page,page.getByPlaceholder('搜索对话'))
      await page.getByTestId('agent-switch').click()
      await keyboardFocus(page,page.getByRole('textbox',{name:'搜索子代理'}))
      await page.keyboard.press('Escape')
      await page.getByTestId('new-session').click()
      await keyboardFocus(page,page.getByPlaceholder('新对话',{exact:true}))
      await page.keyboard.press('Escape')
      await openSettings(page)
      await page.getByRole('navigation',{name:'设置分类'}).getByRole('link',{name:'企业平台',exact:true}).click()
      await keyboardFocus(page,page.getByTestId('platform-json'))
      await closeSettings(page)
      await page.getByTestId('composer-input').click()
      expect(await page.getByTestId('composer-input').evaluate(el=>getComputedStyle(el).outlineStyle)).toBe('none')
    }
    await page.setViewportSize({width:390,height:740})
    await keyboardFocus(page,page.getByLabel('风格',{exact:true}))
    await expect(page.getByTestId('approve-interaction')).toBeInViewport()
    expect(await page.evaluate(()=>document.body.scrollWidth-innerWidth)).toBeLessThanOrEqual(1)
    expect((await page.locator('.composer-context').boundingBox())!.height).toBeLessThanOrEqual(740*.34+1)
    await keyboardFocus(page,page.getByTestId('approve-interaction'),true)
    await expect(page.getByTestId('approve-interaction')).toBeInViewport()
    await page.emulateMedia({forcedColors:'active'})
    await keyboardFocusForced(page,page.getByLabel('风格',{exact:true}))
  } finally {await app.close()}
})

async function keyboardFocusForced(page:Page,target:Locator) {
  await target.focus(); await page.keyboard.press('Tab'); await page.keyboard.press('Shift+Tab')
  await expect(target).toBeFocused()
  expect(await target.evaluate(el=>getComputedStyle(el).outlineWidth)).toBe('2px')
  expect(await target.evaluate(el=>getComputedStyle(el).outlineOffset)).toBe('-2px')
}
