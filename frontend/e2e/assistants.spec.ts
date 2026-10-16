import { openSettings, closeSettings } from './settings'
import { test, expect } from '@playwright/test'
import { fixture } from '../../tests/fixtures/fixture.mjs'
let app: Awaited<ReturnType<typeof fixture>>
test.beforeAll(async () => { app = await fixture({localAssistants:true}) })
test.afterAll(async () => { await app.close() })

test('assistant selection, filtered history, draft isolation and responsive header', async ({page}) => {
  const root = await app.manager.create({title:'原有通用对话'})
  const video = await app.manager.create({title:'视频对话',agent:'video'})
  await app.manager.create({title:'图片对话',agent:'image'})
  await app.manager.create({title:'发布对话',agent:'publishing'})
  await page.goto(app.url)
  const filter = page.getByRole('combobox',{name:'筛选助手'})
  await expect(filter.locator('option')).toHaveCount(5)
  await page.getByTestId(`session-${root.id}`).click()
  await expect(page.getByTestId('active-assistant')).toHaveText('通用助手',{useInnerText:true})
  await page.getByTestId('composer-input').fill('旧对话草稿')
  await filter.selectOption('video')
  await expect(page.getByTestId(`session-${root.id}`)).toHaveCount(0)
  await page.getByTestId(`session-${video.id}`).click()
  await expect(page.getByTestId('active-assistant')).toHaveText('视频创作',{useInnerText:true})
  const headerIcon = await page.getByTestId('active-assistant').locator('svg').innerHTML()
  expect(await page.locator('.assistant-filter > svg.assistant-icon').innerHTML()).toBe(headerIcon)
  await filter.selectOption('')
  const assistantIcons = page.locator('.session-assistant svg')
  await expect(assistantIcons).toHaveCount(4)
  for (const icon of await assistantIcons.all()) expect(await icon.innerHTML()).toBe(headerIcon)
  const filterBox = (await filter.boundingBox())!, searchBox = (await page.getByRole('textbox',{name:'搜索对话'}).boundingBox())!
  expect(filterBox.height).toBe(32); expect(filterBox.x).toBe(searchBox.x); expect(filterBox.width).toBe(searchBox.width)
  const adornments = await page.locator('.assistant-filter').evaluate(el => { const box=el.getBoundingClientRect(),icon=el.querySelector('.assistant-icon')!.getBoundingClientRect(),arrow=el.querySelector('.filter-chevron')!.getBoundingClientRect(); return {left:icon.x-box.x,right:box.right-arrow.right,center:Math.abs(icon.y+icon.height/2-arrow.y-arrow.height/2)} })
  expect(adornments).toEqual({left:10,right:10,center:0})
  const selectLayout = await filter.evaluate(el => ({supported:CSS.supports('appearance','base-select'),center:getComputedStyle(el).alignItems,picker:getComputedStyle(el,'::picker-icon').display}))
  if(selectLayout.supported) { expect(selectLayout.center).toBe('center');expect(selectLayout.picker).toBe('none') }
  await filter.selectOption('video')
  await expect(page.getByTestId('composer-input')).toHaveValue('')
  await page.getByTestId('new-session').click()
  const choice = page.getByRole('combobox',{name:'新对话助手'})
  await expect(choice).toHaveValue('video')
  await expect(choice.locator('option:not([disabled])')).toHaveCount(4)
  await choice.selectOption('image')
  await page.getByRole('button',{name:'创建',exact:true}).click()
  await expect(page.getByTestId('active-assistant')).toHaveText('图片创作',{useInnerText:true})
  expect(await page.getByTestId('active-assistant').locator('svg').innerHTML()).toBe(headerIcon)
  await expect(filter).toHaveValue('image')
  await filter.selectOption('root')
  await page.getByTestId(`session-${root.id}`).click()
  await expect(page.getByTestId('composer-input')).toHaveValue('旧对话草稿')
  for (const theme of ['苔灰','暖砂','炭棕','曜黑（高对比）','石墨']) {
    await openSettings(page)
    await page.getByRole('navigation',{name:'设置分类'}).getByRole('link',{name:'外观',exact:true}).click()
    await page.getByRole('button',{name:`${theme}`}).click()
    await closeSettings(page)
    const colors = await filter.evaluate(el => {
      const css = getComputedStyle(el)
      const expected = css.getPropertyValue('--tuck-text').trim()
      return {actual:css.color, expected:`rgb(${expected.slice(1).match(/../g)!.map(c => parseInt(c,16)).join(', ')})`}
    })
    expect(colors.actual).toBe(colors.expected)
    await expect(filter).toBeInViewport()
  }
  for (const width of [1365,830,390]) {
    await page.setViewportSize({width,height:850})
    await expect(page.getByTestId('active-assistant')).toBeVisible()
    const header = await page.locator('.conversation-header').boundingBox()
    const composer = await page.getByTestId('composer-input').boundingBox()
    expect(header!.height).toBe(48)
    expect(composer!.y + composer!.height).toBeLessThan(850)
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  }
  await page.reload()
  await expect(page.getByTestId('active-assistant')).toHaveText('助手',{useInnerText:true})
  await expect(page.getByTestId('active-assistant')).toHaveAttribute('title','通用助手')
});
