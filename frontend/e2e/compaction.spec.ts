import { openSettings, closeSettings } from './settings'
import { expect, test } from '@playwright/test'
import { fixture } from '../../tests/fixtures/fixture.mjs'

test('compaction is visible, survives refresh and preserves geometry across outcomes and themes', async ({page}) => {
  const app = await fixture()
  try {
    const task = await app.manager.create({title:'上下文压缩验收'})
    task.status='working'
    task.modelCalls=103
    task.usage={inputTokens:200000,cacheReadTokens:1000000,cacheWriteTokens:0,outputTokens:20000,reasoningTokens:0}
    task.context={projectedTokens:214668,contextWindow:240640,reported:true}
    app.manager.append(task,{role:'assistant',blocks:[{id:'answer',type:'markdown',data:{markdown:'工具已执行，正在继续任务。'}}]})
    app.store.saveTask(task)
    await page.goto(app.url)
    await expect(page.getByLabel('上下文 89%',{exact:true})).toBeVisible()
    const before=await page.getByTestId('scrollport').boundingBox()
    const start=() => app.manager.normalizer.handle(task,{type:'session_compaction',session_id:task.nativeSessionId,status:'started',timestamp:new Date(Date.now()-171000).toISOString()})
    const finish=(outcome:string)=>app.manager.normalizer.handle(task,{type:'session_compaction',session_id:task.nativeSessionId,status:'completed',outcome,timestamp:new Date().toISOString()})
    await start()
    await expect(page.locator('.run-status')).toHaveText('压缩上下文中')
    await expect(page.getByTestId('compaction-status')).toContainText('压缩中 2:')
    expect(await page.getByTestId('scrollport').boundingBox()).toEqual(before)
    await page.reload()
    await expect(page.getByTestId('compaction-status')).toContainText('压缩中 2:')
    for (const width of [1365,390]) {
      await page.setViewportSize({width,height:740})
      await expect(page.getByLabel('正在压缩上下文',{exact:true})).toBeInViewport()
      expect(await page.evaluate(()=>document.body.scrollWidth-innerWidth)).toBeLessThanOrEqual(1)
      expect((await page.locator('.session-summary').boundingBox())!.height).toBeLessThan(32)
      await page.getByLabel('正在压缩上下文',{exact:true}).click()
      await expect(page.getByRole('dialog',{name:'上下文详情'})).toContainText('214,668')
      await expect(page.getByTestId('compaction-result')).toHaveText('正在压缩上下文')
      await expect(page.getByRole('dialog',{name:'上下文详情'})).toBeInViewport()
      await page.getByLabel('关闭摘要').click()
    }
    await page.setViewportSize({width:1365,height:740})
    for (const theme of ['苔灰','暖砂','炭棕','曜黑（高对比）','石墨']) {
      await openSettings(page)
      await page.getByRole('navigation',{name:'设置分类'}).getByRole('link',{name:'外观',exact:true}).click()
      await page.getByRole('button',{name:`${theme}`}).click()
      await closeSettings(page)
      expect(await page.locator('.compaction-spinner').evaluate(el=>getComputedStyle(el).borderTopColor)).toBe(await page.locator('.context-summary').evaluate(el=>getComputedStyle(el).color))
    }
    await page.emulateMedia({reducedMotion:'reduce'})
    expect(await page.locator('.compaction-spinner').evaluate(el=>getComputedStyle(el).animationName)).toBe('none')
    await page.context().setOffline(true)
    // Chromium offline emulation alone can leave an already-open SSE socket alive.
    app.server.closeAllConnections()
    await expect(page.getByTestId('compaction-status')).toContainText('压缩待同步')
    await page.context().setOffline(false)
    await expect(page.getByTestId('compaction-status')).toContainText('压缩中')
    await finish('applied')
    await app.manager.normalizer.handle(task,{type:'token_usage',session_id:task.nativeSessionId,usage:{context_length:27115,context_limit:240640,snapshot_only:true}})
    await expect(page.getByLabel('上下文 11%',{exact:true})).toBeVisible()
    await expect(page.locator('.compaction-spinner')).toHaveCount(0)
    await page.getByLabel('上下文 11%',{exact:true}).click()
    await expect(page.getByTestId('compaction-result')).toHaveText('压缩完成')
    await page.getByLabel('关闭摘要').click()
    for (const [outcome,label] of [['skipped','已跳过压缩'],['failed','压缩失败'],['unexpected','压缩结果未确认']]) {
      await start(); await finish(outcome!)
      await expect(page.getByTestId('compaction-status')).toHaveText(label!)
    }
    await start(); app.manager.settle(task,'interrupted')
    await expect(page.getByTestId('compaction-status')).toHaveText('压缩已中断')
    await expect(page.locator('.run-status')).not.toContainText('压缩上下文中')
  } finally { await app.close() }
})
