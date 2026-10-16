import { test, expect } from '@playwright/test'
import { fixture } from '../../tests/fixtures/fixture.mjs'

test('sidebar adapts without remounting the viewport; slow history has bounded branded feedback and latest selection wins', async ({page}, testInfo) => {
  const app = await fixture({localAssistants:true})
  let release = () => {}
  try {
    const slow = await app.manager.create({title:'等待加载的对话',agent:'video'})
    app.manager.append(slow,{role:'assistant',blocks:[{id:'slow',type:'markdown',data:{markdown:'延迟加载内容'}}]})
    const next = await app.manager.create({title:'另一个加载目标',agent:'image'})
    app.manager.append(next,{role:'assistant',blocks:[{id:'next',type:'markdown',data:{markdown:'最新选择内容'}}]})
    const initial = await app.manager.create({title:'布局验证对话'})
    // Control the initial fixture selection, not task timestamp sorting.
    await page.route('**/api/tasks', async route => {
      const response = await route.fetch(), value = await response.json()
      value.tasks.sort((a:{id:string},b:{id:string})=>Number(b.id===initial.id)-Number(a.id===initial.id))
      await route.fulfill({response,json:value})
    })
    await page.goto(app.url)
    await expect(page.locator('main.conversation-shell')).toHaveAttribute('data-session-id',initial.id)
    await expect(page.getByTestId('conversation-loading')).toHaveCount(0)
    const controls = await page.locator('.sidebar-head-actions > button').evaluateAll(elements => elements.map(el => {
      const box=el.getBoundingClientRect(),icon=el.querySelector('svg')!.getBoundingClientRect()
      return {height:box.height,center:box.y+box.height/2,iconCenter:icon.y+icon.height/2}
    }))
    expect(controls).toHaveLength(2)
    expect(controls.every(control=>control.height===30 && control.iconCenter===control.center)).toBe(true)
    expect(controls[0]!.center).toBe(controls[1]!.center)
    expect(await page.getByTestId(`session-${initial.id}`).locator('strong').evaluate(el=>getComputedStyle(el).fontWeight)).toBe('600')
    expect(await page.getByTestId(`session-${slow.id}`).locator('strong').evaluate(el=>getComputedStyle(el).fontWeight)).toBe('400')
    const before = await page.getByTestId('scrollport').boundingBox()
    await page.getByRole('button',{name:'收起对话列表'}).click()
    await expect(page.getByTestId('session-sidebar')).toBeHidden()
    expect((await page.getByTestId('scrollport').boundingBox())!.width).toBeGreaterThan(before!.width)
    expect((await page.getByTestId('scrollport').boundingBox())!.height).toBe(before!.height)
    await expect(page.locator('main.conversation-shell')).toHaveAttribute('data-session-id',initial.id)
    await page.reload()
    await expect(page.getByTestId('session-sidebar')).toBeHidden()
    await page.getByRole('button',{name:'展开对话列表'}).click()
    await expect(page.getByTestId('session-sidebar')).toBeVisible()
    const held = new Promise<void>(resolve => { release=resolve })
    await page.route(`**/api/tasks/${slow.id}/messages?*`, async route=>{ const response=await route.fetch(); await held; await route.fulfill({response}) })
    await page.getByTestId(`session-${slow.id}`).click()
    await expect(page.getByTestId('conversation-loading')).toBeVisible()
    await expect(page.getByRole('status').filter({hasText:'正在加载对话'})).toBeVisible()
    const geometry = await page.getByTestId('conversation-loading').evaluate(el=>{const box=el.getBoundingClientRect(),stage=el.parentElement!.getBoundingClientRect(),mark=el.querySelector('svg')!.getBoundingClientRect();return {height:box.height,stage:stage.height,icon:mark.width}})
    expect(geometry.height).toBe(geometry.stage); expect(geometry.icon).toBeLessThanOrEqual(16)
    expect(await page.getByTestId('composer-input').evaluate(el=>!!el.closest('[inert]'))).toBe(true)
    await page.getByTestId(`session-${next.id}`).click()
    await expect(page.locator('main.conversation-shell')).toHaveAttribute('data-session-id',next.id)
    release()
    await expect(page.getByTestId('conversation-loading')).toHaveCount(0)
    await expect(page.getByText('最新选择内容',{exact:true})).toBeVisible()
    expect(await page.getByTestId(`session-${next.id}`).locator('strong').evaluate(el=>getComputedStyle(el).fontWeight)).toBe('600')
    expect(await page.getByTestId(`session-${initial.id}`).locator('strong').evaluate(el=>getComputedStyle(el).fontWeight)).toBe('400')
    for (const width of [830,760,390]) {
      await page.setViewportSize({width,height:740})
      expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
      if (width<=760) {
        await expect(page.getByTestId('session-sidebar')).toBeHidden()
        await page.getByRole('button',{name:'展开对话列表'}).click()
        await expect(page.getByTestId('session-sidebar')).toBeVisible()
        await expect(page.locator('main.conversation-shell')).toHaveAttribute('inert','')
        await page.keyboard.press('Escape')
        await expect(page.getByRole('button',{name:'展开对话列表'})).toBeFocused()
        await expect(page.getByTestId('session-sidebar')).toBeHidden()
        await page.getByRole('button',{name:'展开对话列表'}).click()
        const backdrop = page.locator('.mobile-session-backdrop')
        await backdrop.click({position:{x:(await backdrop.boundingBox())!.width-10,y:400}})
        await expect(page.getByTestId('session-sidebar')).toBeHidden()
      }
      await expect(page.locator('main.conversation-shell')).toHaveAttribute('data-session-id',next.id)
    }
    await page.setViewportSize({width:1365,height:850})
    await expect(page.getByTestId('session-sidebar')).toBeVisible()
    await page.screenshot({path:testInfo.outputPath('workbench-layout.png')})
  } finally { release(); await app.close() }
})
