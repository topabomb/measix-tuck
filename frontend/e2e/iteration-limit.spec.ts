import { expect, test } from '@playwright/test'
import { fixture } from '../../tests/fixtures/fixture.mjs'

test('native iteration confirmation survives refresh and offers explicit continue and stop actions', async ({page}) => {
  const app = await fixture({maxIterations:1})
  try {
    const task = await app.manager.create({title:'执行步数上限验收',approvalMode:'auto'})
    await page.goto(app.url)
    await page.request.post(`${app.url}/api/tasks/${task.id}/prompts`,{data:{prompt:'REJECT_ONLY'}})
    await expect(page.getByTestId('pending-interaction')).toContainText('已达本轮执行步数上限')
    await page.reload()
    await expect(page.locator('.run-status')).toHaveText('等待继续确认')
    for (const width of [1365,390]) {
      await page.setViewportSize({width,height:800})
      await expect(page.getByRole('button',{name:'继续 10 步'})).toBeInViewport()
      await expect(page.getByRole('button',{name:'结束本轮'})).toBeInViewport()
      expect(await page.evaluate(()=>document.documentElement.scrollWidth-innerWidth)).toBeLessThanOrEqual(1)
    }
    await page.getByRole('button',{name:'继续 10 步'}).click()
    await expect(page.getByTestId('pending-interaction')).toHaveCount(0)
    await expect(page.locator('.run-status')).toHaveText('已完成')
    await page.request.post(`${app.url}/api/tasks/${task.id}/prompts`,{data:{prompt:'REJECT_ONLY'}})
    await expect(page.getByRole('button',{name:'结束本轮'})).toBeVisible()
    await page.getByRole('button',{name:'结束本轮'}).click()
    await expect(page.getByTestId('pending-interaction')).toHaveCount(0)
    await expect(page.locator('.run-status')).toHaveText('已停止')
  } finally { await app.close() }
})
