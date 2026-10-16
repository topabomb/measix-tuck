import { expect, test } from '@playwright/test'
import { fixture } from '../../tests/fixtures/fixture.mjs'
import { workspaceFixture } from '../../tests/fixtures/workspace-fixture.mjs'

test('composer statistics stay compact in a narrow chat column while every detail remains available',async({page})=>{
  const core=await workspaceFixture(),app=await fixture({enterprisePlatform:core.platform})
  try {
    const task=await app.manager.create({title:'工作区窄列统计'})
    Object.assign(task,{
      turnCount:123,usageSource:'live',modelCalls:456,
      usage:{inputTokens:2000000,cacheReadTokens:6000000,cacheWriteTokens:2000000,outputTokens:4000000,reasoningTokens:1000000},
      context:{projectedTokens:140037,contextWindow:240640,reported:true},
      timings:{streamMs:40000,streamTokens:4000,streamSamples:3,toolMs:1200,toolSamples:2},
      turnTiming:{turnId:'summary-layout',startedAt:1000,observedAt:62000,endedAt:62000,waitingMs:3000},
    })
    app.store.saveTask(task)
    await page.addInitScript(()=>localStorage.setItem('tuck-enterprise-width','900'))
    await page.setViewportSize({width:1440,height:900})
    await page.goto(app.url+`/#/chat/conversations/${task.id}`)
    const footer=page.locator('.composer-footer'),summary=page.locator('.session-summary')
    await expect(summary).toBeVisible()
    await expect.poll(()=>footer.evaluate(el=>el.getBoundingClientRect().height)).toBeLessThanOrEqual(30)
    await page.getByRole('button',{name:'工作区',exact:true}).click()
    const panel=page.getByRole('complementary',{name:'企业工作区'})
    await expect(panel.getByRole('option',{name:/hello\.txt/})).toBeVisible()
    const composer=page.locator('.workbench-composer')
    await expect.poll(()=>composer.evaluate(el=>el.getBoundingClientRect().width)).toBeLessThan(480)
    for(const viewportWidth of [1440,390]){
      if(viewportWidth===390){await panel.getByRole('button',{name:'收起企业工作区',exact:true}).click();await page.setViewportSize({width:390,height:844})}
      await expect.poll(()=>footer.evaluate(el=>el.getBoundingClientRect().height)).toBeLessThanOrEqual(56)
      const buttons=summary.locator(':scope > button')
      await expect(buttons).toHaveCount(3)
      expect(await buttons.evaluateAll(elements=>new Set(elements.map(el=>Math.round(el.getBoundingClientRect().top))).size)).toBe(1)
      expect(await footer.evaluate(el=>el.scrollWidth-el.clientWidth)).toBeLessThanOrEqual(1)
      await expect(page.getByTestId('composer-input')).toHaveAttribute('aria-description',viewportWidth<=760?/回车换行/:/Enter 发送/)
      await summary.getByRole('button',{name:/次调用/}).click()
      await expect(page.getByRole('dialog',{name:'会话统计'})).toContainText('456')
      await expect(page.getByRole('dialog',{name:'会话统计'})).toContainText('100 tok/s')
      await page.getByLabel('关闭摘要').click()
      await summary.getByRole('button',{name:/累计/}).click()
      await expect(page.getByRole('dialog',{name:'累计用量'})).toContainText('14,000,000 tokens')
      await expect(page.getByRole('dialog',{name:'累计用量'})).toContainText('其中推理')
      await page.getByLabel('关闭摘要').click()
      await summary.getByRole('button',{name:/上下文/}).click()
      const context=page.getByRole('dialog',{name:'上下文详情'})
      await expect(context).toContainText('140,037 tokens')
      await expect(context).toContainText('240,640 tokens')
      await expect(context).toBeInViewport()
      await page.getByLabel('关闭摘要').click()
    }
  }finally{await app.close();await core.close()}
})

test('statistics distinguish context, cumulative usage and sampled timing without moving history', async ({ page }) => {
  const app = await fixture()
  try {
    const task = await app.manager.create({ title: '统计口径验收' })
    Object.assign(task, {
      usageSource: 'live', modelCalls: 4,
      usage: { inputTokens: 2000, cacheReadTokens: 6000, cacheWriteTokens: 2000, outputTokens: 4000, reasoningTokens: 1000 },
      context: { projectedTokens: 14037, contextWindow: 240640, reported: true },
      timings: { streamMs: 40000, streamTokens: 4000, streamSamples: 3, toolMs: 1200, toolSamples: 2 },
    })
    app.store.saveTask(task)
    await page.goto(app.url)
    const summary = page.locator('.session-summary')
    await expect(summary).toContainText('累计 14K tokens') // reasoning is already part of output
    await expect(summary).toContainText('≈100 tok/s')
    for (const width of [1365, 390]) {
      await page.setViewportSize({ width, height: 740 })
      const before = await page.getByTestId('scrollport').boundingBox()
      await summary.getByRole('button', { name: /累计/ }).click()
      const usage = page.getByRole('dialog', { name: '累计用量' })
      await expect(usage).toContainText('14,000 tokens')
      await expect(usage).toContainText('输入合计')
      await expect(usage).toContainText('10,000 tokens')
      await expect(usage).toContainText('其中推理')
      await expect(usage).toContainText('60%')
      await expect(usage).toBeInViewport()
      expect(await usage.locator('.summary-body').evaluate(el => el.scrollHeight - el.clientHeight)).toBeLessThanOrEqual(1)
      expect((await usage.boundingBox())!.height).toBeLessThan(365)
      await expect(usage.locator('p')).toHaveCount(0)
      await page.getByLabel('关闭摘要').click()
      await summary.getByRole('button', { name: /次调用/ }).click()
      const activity = page.getByRole('dialog', { name: '会话统计' })
      await expect(activity).toContainText('40 秒')
      await expect(activity).toContainText('1.2 秒')
      await expect(activity).toContainText('100 tok/s')
      await expect(activity).not.toContainText('计时样本')
      await expect(activity).not.toContainText('消息记录')
      await expect(activity).toContainText('上一轮耗时未记录')
      expect(await activity.locator('.summary-body').evaluate(el => el.scrollHeight - el.clientHeight)).toBeLessThanOrEqual(1)
      await expect(activity.locator('p')).toHaveCount(0)
      expect(await page.getByTestId('scrollport').boundingBox()).toEqual(before)
      expect(await page.evaluate(() => document.body.scrollWidth - innerWidth)).toBeLessThanOrEqual(1)
      const rect = await activity.boundingBox()
      expect(rect!.y).toBeGreaterThanOrEqual(0)
      expect(rect!.x + rect!.width).toBeLessThanOrEqual(width)
      await page.getByLabel('关闭摘要').click()
    }
    delete task.timings; task.context = { projectedTokens: 0, contextWindow: 240640 }
    app.store.saveTask(task)
    await page.reload()
    await expect(summary).not.toContainText('tok/s')
    await expect(summary).toContainText('上下文 待上报')
    await summary.getByRole('button', { name: /次调用/ }).click()
    await expect(page.getByRole('dialog', { name: '会话统计' })).toContainText('未采集')
  } finally { await app.close() }
})
