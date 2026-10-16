import { openSettings, closeSettings } from './settings'
import { expect, test, type Locator } from '@playwright/test'
import { fixture } from '../../tests/fixtures/fixture.mjs'

// Headless Chromium normally suppresses the native control in screenshots.
test.use({ launchOptions: { ignoreDefaultArgs: ['--hide-scrollbars'] } })

async function themedScrollbar(target: Locator) {
  const result = await target.evaluate(el => {
    const style = getComputedStyle(el)
    const rgb = (name: string) => `rgb(${style.getPropertyValue(name).trim().slice(1).match(/../g)!.map(x => parseInt(x, 16)).join(', ')})`
    return { actual: style.scrollbarColor, expected: `${rgb('--tuck-muted')} ${rgb('--tuck-surface')}`, width: style.scrollbarWidth }
  })
  expect(result.actual).toBe(result.expected)
  expect(result.width).toBe('auto')
}

test('native scrollbars inherit each theme across the workspace and settings page', async ({ page }, testInfo) => {
  const app = await fixture({ attachmentModels: true })
  try {
    await page.setViewportSize({ width: 1100, height: 500 })
    const task = await app.manager.create({ title: '主题滚动验收' })
    for (let i = 0; i < 30; i++) await app.manager.create({ title: `滚动条验收 ${i}` })
    for (let i = 0; i < 40; i++) app.manager.append(task, { role: 'assistant', blocks: [{ id: `text-${i}`, type: 'markdown', data: { markdown: `正文 ${i}\n\n` + '检验主题与滚动区域。'.repeat(20) } }] })
    await page.goto(app.url)
    await expect(page.locator('main.conversation-shell')).toHaveAttribute('data-session-id', task.id)
    await expect(page.locator('.conversation-vlist')).toBeVisible()
    for (const theme of ['墨蓝', '石墨', '暖砂', '炭棕', '苔灰', '曜黑（高对比）']) {
      await openSettings(page)
      await page.getByRole('navigation',{name:'设置分类'}).getByRole('link',{name:'外观',exact:true}).click()
      await page.getByRole('button', { name: theme, exact: true }).click()
      await closeSettings(page)
      for (const target of [page.locator('.session-list'), page.locator('.conversation-vlist'), page.getByTestId('composer-input')]) await themedScrollbar(target)
      await openSettings(page)
      await page.getByRole('navigation',{name:'设置分类'}).getByRole('link',{name:'企业平台',exact:true}).click()
      await page.setViewportSize({width:1280,height:360})
      const body = page.locator('.settings-standalone')
      await expect(body).toBeVisible()
      await expect.poll(()=>body.evaluate(el => el.scrollHeight - el.clientHeight)).toBeGreaterThan(20)
      await themedScrollbar(body)
      await body.hover({position:{x:4,y:4}}); await page.mouse.wheel(0, 200)
      await expect.poll(() => body.evaluate(el => el.scrollTop)).toBeGreaterThan(20)
      if (theme === '苔灰') await page.screenshot({ path: testInfo.outputPath('moss-scrollbars.png') })
      await closeSettings(page)
      await page.setViewportSize({width:1280,height:720})
    }
    await page.emulateMedia({ forcedColors: 'active' })
    expect(await page.locator('.conversation-vlist').evaluate(el => getComputedStyle(el).scrollbarColor)).toBe('auto')
  } finally { await app.close() }
})
