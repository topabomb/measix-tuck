import { expect, test } from '@playwright/test'
import { fixture } from '../../tests/fixtures/fixture.mjs'

test('desktop summary switches between triggers and restores focus to the latest one',async({page})=>{
  const app=await fixture()
  try{
    const task=await app.manager.create({title:'切换统计摘要'})
    await page.setViewportSize({width:1365,height:800})
    await page.goto(`${app.url}/#/chat/conversations/${task.id}`)
    const stats=page.locator('.session-summary')
    await stats.getByRole('button',{name:/次调用/}).click()
    await expect(page.getByRole('dialog',{name:'会话统计',exact:true})).toBeVisible()
    const context=stats.getByRole('button',{name:/上下文/})
    await context.click()
    await expect(page.getByRole('dialog',{name:'上下文详情',exact:true})).toBeVisible()
    await expect(page.getByRole('dialog',{name:'会话统计',exact:true})).toHaveCount(0)
    await page.keyboard.press('Escape')
    await expect(page.getByRole('dialog',{name:'上下文详情',exact:true})).toBeHidden()
    await expect(context).toBeFocused()
  }finally{await app.close()}
})

test('turn clock persists, stays local, fits narrow layouts and freezes on disconnect or interruption', async ({page}) => {
  const app=await fixture()
  try {
    const task=await app.manager.create({title:'本轮耗时验收'})
    task.status='working'; task.modelCalls=133; task.turnCount=6
    task.context={projectedTokens:14037,contextWindow:240640,reported:true}
    task.timings={streamMs:1134000,streamTokens:57000,streamSamples:133,toolMs:695000,toolSamples:133}
    task.turnTiming={turnId:'test-turn',startedAt:Date.now()-222000,observedAt:Date.now(),waitingMs:35000}
    app.manager.append(task,{role:'assistant',blocks:[{id:'answer',type:'markdown',data:{markdown:'正在继续处理任务。'}}]})
    app.store.saveTask(task)
    await page.goto(app.url)
    const clock=page.getByTestId('turn-clock')
    await expect(clock).toContainText('本轮 3:')
    const first=await clock.innerText()
    const initialCursor=(await (await page.request.get(`${app.url}/api/tasks`)).json()).cursor
    const before=await page.getByTestId('scrollport').boundingBox()
    await expect.poll(()=>clock.innerText()).not.toBe(first)
    expect(await page.getByTestId('scrollport').boundingBox()).toEqual(before)
    expect((await (await page.request.get(`${app.url}/api/tasks`)).json()).cursor).toBe(initialCursor)
    await page.reload()
    await expect(clock).toContainText('本轮 3:')
    for(const width of [1365,390]) {
      await page.setViewportSize({width,height:740})
      const rect=await page.getByTestId('scrollport').boundingBox()
      await expect(clock).toBeInViewport()
      expect((await page.locator('.session-summary').boundingBox())!.height).toBeLessThan(32)
      await page.locator('.session-summary').getByRole('button',{name:/次调用/}).click()
      await expect(page.getByTestId('turn-waiting')).toHaveText('35 秒')
      await expect(page.getByRole('dialog',{name:'会话统计'})).toBeInViewport()
      expect(await page.evaluate(()=>document.documentElement.scrollWidth-innerWidth)).toBeLessThanOrEqual(1)
      expect(await page.getByTestId('scrollport').boundingBox()).toEqual(rect)
      await page.getByLabel('关闭摘要').click()
    }
    await page.context().setOffline(true); app.server.closeAllConnections()
    await expect(clock).toContainText('待同步')
    const frozen=await clock.innerText()
    await page.waitForTimeout(1100)
    expect(await clock.innerText()).toBe(frozen)
    await page.context().setOffline(false)
    await expect(clock).toContainText('本轮')
    app.manager.settle(task,'interrupted',{code:'service_restarted',message:'测试中断'})
    await expect(clock).toContainText('上一轮')
    await page.locator('.session-summary').getByRole('button',{name:/次调用/}).click()
    await expect(page.getByTestId('turn-duration')).toContainText('截至最后记录')
    await page.getByLabel('关闭摘要').click()
    task.turnTiming={turnId:'completed',startedAt:1000,observedAt:223000,endedAt:223000,waitingMs:35000}
    app.manager.settle(task,'completed')
    await expect(clock).toHaveText('上一轮 3:42')
    await page.reload()
    await expect(clock).toHaveText('上一轮 3:42')
  } finally {await app.close()}
})
