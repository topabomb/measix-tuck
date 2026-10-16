import {expect,test,type Page} from '@playwright/test'
import {fixture} from '../../tests/fixtures/fixture.mjs'

const remaining = (page:Page) => page.locator('.conversation-vlist').evaluate(el => el.scrollHeight-el.clientHeight-el.scrollTop)

test('wheel loads the pending tail when folding leaves no scrollable distance',async({page})=>{
  const app=await fixture()
  try {
    const task=await app.manager.create({title:'折叠后到达两条消息'})
    app.manager.append(task,{role:'assistant',blocks:[{id:'tool',type:'tool-call',data:{callId:'short-tool',name:'read_file',status:'success',input:{path:'short.txt'}}}]})
    await page.goto(`${app.url}/#/chat/conversations/${task.id}`)
    const tool=page.locator('[data-call-id="short-tool"] .activity-summary')
    await tool.click()
    await page.locator('.conversation-vlist').hover({position:{x:15,y:100}})
    await page.mouse.wheel(0,-24)
    await tool.click()
    for(let i=0;i<2;i++) app.manager.append(task,{role:'assistant',blocks:[{id:`new-${i}`,type:'markdown',data:{markdown:`新到达的消息 ${i}`}}]})
    await expect(page.getByTestId('messages-after')).toHaveText('2')
    expect(await remaining(page)).toBeLessThan(2)
    await page.locator('.conversation-vlist').hover({position:{x:15,y:100}})
    await page.mouse.wheel(0,120)
    await expect(page.getByText('新到达的消息 1',{exact:true})).toBeInViewport()
    await expect(page.getByTestId('jump-latest')).toHaveCount(0)
  } finally {await app.close()}
})

test('reading, folding and scrolling a short live conversation never drops arriving results',async({page},info)=>{
  const app=await fixture()
  try {
    const task=await app.manager.create({title:'连续展开与消息更新'})
    const text=(value:string)=>app.manager.append(task,{role:'assistant',blocks:[{id:'answer',type:'markdown',data:{markdown:value}}]})
    for(let i=0;i<56;i++) text(`历史消息 ${i}。`+'正文用于检查滚动。'.repeat(15))
    app.manager.append(task,{role:'assistant',blocks:[{id:'tool',type:'tool-call',data:{callId:'expand-test',name:'read_file',status:'success',input:{path:'example.txt',content:'工具详情\n'.repeat(40)}}}]})
    await page.goto(`${app.url}/#/chat/conversations/${task.id}`)
    await expect.poll(()=>remaining(page)).toBeLessThan(2)
    const tool=page.locator('[data-call-id="expand-test"] .activity-summary')
    await tool.click()
    await expect(tool).toHaveAttribute('aria-expanded','true')
    await page.locator('.conversation-vlist').hover({position:{x:15,y:100}})
    await page.mouse.wheel(0,-24)
    await expect(page.getByTestId('jump-latest')).toBeVisible()
    text('新增工具返回结果 57')
    text('新增最终回复 58')
    await expect(page.getByTestId('messages-after')).toHaveText('2')
    // Reach the old bottom by scrolling, without Latest/reload reconstructing it.
    await page.mouse.wheel(0,500)
    await expect(page.getByText('新增最终回复 58',{exact:true})).toBeAttached()
    await page.mouse.wheel(0,500)
    await expect(page.getByText('新增最终回复 58',{exact:true})).toBeInViewport()
    for(let i=0;i<3;i++) {
      await tool.click()
      text(`折叠期间到达的回复 ${i}`)
      await page.locator('.conversation-vlist').hover({position:{x:15,y:100}})
      await page.mouse.wheel(0,1500)
      await expect(page.getByText(`折叠期间到达的回复 ${i}`,{exact:true})).toBeInViewport()
    }
    app.manager.settle(task,'completed')
    await expect(page.locator('.conversation-header')).toContainText('已完成')
    await expect(page.getByText('新增工具返回结果 57',{exact:true})).toBeAttached()
    await expect(page.getByText('新增最终回复 58',{exact:true})).toBeAttached()
    await page.screenshot({path:info.outputPath('expanded-live-tail.png')})
    await page.reload()
    await expect(page.getByText('折叠期间到达的回复 2',{exact:true})).toBeInViewport()
    await page.screenshot({path:info.outputPath('refreshed-live-tail.png')})
  } finally {await app.close()}
})
