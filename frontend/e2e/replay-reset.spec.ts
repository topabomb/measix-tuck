import {expect,test} from '@playwright/test'
import {fixture,until} from '../../tests/fixtures/fixture.mjs'

for (const width of [1365,390]) test(`expired replay restores history, draft and native approval at ${width}px`, async ({page},info) => {
  const app=await fixture()
  const errors:string[]=[]
  page.on('pageerror',error=>{if(!error.message.includes('ResizeObserver'))errors.push(error.message)})
  try {
    const task=await app.manager.create({title:'缓存重同步验证',approvalMode:'manual'})
    app.store.transaction(()=>{
      for(let index=0;index<500;index++)app.store.saveMessage(task.id,{
        id:`${task.id}:m-${index}`,index,revision:0,turnId:'history',role:'assistant',
        blocks:[{id:`text-${index}`,type:'markdown',data:{markdown:`历史记录 ${index}`}}],
      })
      task.logicalCount=500;app.store.saveTask(task)
    })
    app.manager.submit(task.id,'REJECT_ONLY')
    await until(()=>task.pendingInteraction?.kind==='approval')
    const approvalId=task.pendingInteraction!.id
    await page.setViewportSize({width,height:844})
    let reset=false,resetCount=0
    await page.route('**/api/events?**',async route=>{
      if(reset){reset=false;resetCount++;await route.fulfill({status:200,contentType:'text/event-stream',body:'event: reset\ndata: {"reason":"cursor_expired"}\n\n'})}
      else await route.continue()
    })
    await page.goto(`${app.url}/?inspect=1#/chat/conversations/${task.id}/messages/200`)
    await expect(page.getByTestId('reader-position')).toHaveText('#200')
    const row=page.locator('[data-message-index="200"]').first()
    await expect(row).toBeInViewport()
    await page.getByRole('button',{name:'Close diagnostics',exact:true}).click()
    await page.getByTestId('composer-input').fill('尚未发送的草稿')
    await expect(page.getByTestId('pending-interaction')).toContainText('write_file')
    const before=await row.boundingBox()
    const message=app.store.message(task.id,200)
    app.store.saveMessage(task.id,{...message,revision:7,blocks:[{id:'text-200',type:'markdown',revision:7,data:{markdown:'更新记录 200'}}]})
    reset=true
    app.server.closeAllConnections()
    await expect.poll(()=>resetCount).toBe(1)
    await expect(row).toContainText('更新记录 200')
    await expect(page.getByTestId('composer-input')).toHaveValue('尚未发送的草稿')
    await expect(page.getByTestId('reader-position')).toHaveText('#200')
    await expect(page.getByTestId('pending-interaction')).toContainText('write_file')
    expect(app.manager.get(task.id).pendingInteraction?.id).toBe(approvalId)
    expect(Math.abs((await row.boundingBox())!.y-before!.y)).toBeLessThan(3)
    expect(await page.evaluate(()=>document.documentElement.scrollWidth-innerWidth)).toBeLessThanOrEqual(1)
    await expect(page.getByTestId('approve-interaction')).toBeEnabled()
    await page.screenshot({path:info.outputPath(`reset-${width}.png`)})
    // The same native approval is still actionable after the cache reset.
    await page.getByTestId('approve-interaction').click()
    await until(()=>app.manager.get(task.id).lastTurnReason==='completed')
    await expect(page.getByTestId('pending-interaction')).toHaveCount(0)
    await expect(page.getByTestId('composer-input')).toHaveValue('尚未发送的草稿')
    expect(errors).toEqual([])
  } finally { await app.close() }
})
