import {test,expect} from '@playwright/test'
import {fixture} from '../../tests/fixtures/fixture.mjs'

test('an evicted reader restores a visible next-message anchor with its expanded predecessor',async({page})=>{
  const app=await fixture()
  try {
    const task=await app.manager.create({title:'下一行阅读锚点'})
    const peers=[]
    for(let index=0;index<3;index++)peers.push(await app.manager.create({title:`淘汰视口 ${index}`}))
    app.store.transaction(()=>{
      for(let index=0;index<5000;index++)app.store.saveMessage(task.id,{id:`${task.id}:m-${index}`,index,turnId:`turn-${index}`,role:'assistant',blocks:[{id:`activity-${index}`,type:'tool-call',data:{name:'read_file',callId:`call-${index}`,input:{path:`file-${index}.txt`,details:'核对长工具输出\n'.repeat(100)},status:'success'}}]})
      task.logicalCount=5000;app.store.saveTask(task)
    })
    await page.goto(`${app.url}/?inspect=1#/chat/conversations/${task.id}`)
    await page.getByTestId('jump-input').fill('2500')
    await page.getByTestId('jump-button').click()
    await expect(page.getByTestId('reader-position')).toHaveText('#2,500')
    const activity=page.locator('[data-call-id="call-2500"]')
    // Keyboard activation avoids introducing an unrelated pointer scroll into
    // the cold-window regression; the production folding handler still runs.
    await activity.getByRole('button').press('Enter')
    await expect(activity.locator('pre')).toBeVisible()
    const next=page.locator('[data-virtual-item="true"][data-message-index="2501"]')
    const offset=()=>next.evaluate(row=>row.getBoundingClientRect().top-document.querySelector('[data-testid="scrollport"]')!.getBoundingClientRect().top)
    // A programmatic layout adjustment does not change the semantic reader.
    // Make reader+1 the visible leading edge selected by captureSnapshot.
    await next.evaluate(row=>{
      const list=document.querySelector<HTMLElement>('.conversation-vlist')!
      const stage=document.querySelector('[data-testid="scrollport"]')!
      list.scrollTop+=row.getBoundingClientRect().top-stage.getBoundingClientRect().top-35
    })
    await expect.poll(async()=>Math.abs(await offset()-35)).toBeLessThan(1)
    await expect(page.getByTestId('reader-position')).toHaveText('#2,500')
    const before=(await activity.boundingBox())!.y
    for(const peer of peers){await page.getByTestId(`session-${peer.id}`).click();await expect(page.locator('[data-conversation-engine]')).toHaveAttribute('data-session-id',peer.id)}
    await page.getByTestId(`session-${task.id}`).click()
    await expect(activity.locator('pre')).toBeVisible()
    await expect.poll(async()=>Math.abs(await offset()-35)).toBeLessThan(1)
    await expect.poll(async()=>Math.abs((await activity.boundingBox())!.y-before)).toBeLessThan(4)
    await expect(page.getByTestId('reader-position')).toHaveText('#2,500')
  } finally {await app.close()}
})
