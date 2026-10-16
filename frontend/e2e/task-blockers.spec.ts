import {test,expect} from '@playwright/test'
import {fixture,until} from '../../tests/fixtures/fixture.mjs'

test('answering a native question visibly resumes work before completion and preserves the return path',async({page},info)=>{
  let release=()=>{},releaseNext=()=>{},calls=0
  const held=new Promise<void>(resolve=>release=resolve)
  const nextHeld=new Promise<void>(resolve=>releaseNext=resolve)
  const app=await fixture({localAssistants:true,beforeResponse:()=>++calls===2?held:calls===3?nextHeld:undefined})
  try {
    const task=await app.manager.create({title:'T-Back · 回答后继续执行',approvalMode:'auto'})
    app.manager.submit(task.id,'SCHEMA_QUESTION')
    await until(()=>task.pendingInteraction)
    await page.goto(`${app.url}/#/tasks`)
    await page.getByRole('listitem',{name:'回答后继续执行',exact:true}).getByRole('link',{name:'处理请求',exact:true}).click()
    await page.getByLabel('风格',{exact:true}).selectOption({label:'简洁'})
    await page.getByLabel('是否附带说明',{exact:true}).selectOption({label:'否'})
    await page.getByRole('button',{name:'提交回答',exact:true}).click()
    await expect(page.getByText('请求已处理，已恢复执行。',{exact:true})).toBeVisible()
    await expect(page.getByRole('region',{name:'待处理请求'})).toHaveCount(0)
    expect(task.status).toBe('working')
    await page.screenshot({path:info.outputPath('resumed-task.png')})
    release()
    await until(()=>task.lastTurnReason==='completed')
    await expect(page.getByText('本轮已结束，尚未提交本轮交付。',{exact:true})).toBeVisible()
    // A supplement from another client starts a fresh turn in this mounted page.
    app.manager.submit(task.id,'继续核对结果')
    await expect(page.getByText('正在执行，无需操作。',{exact:true})).toBeVisible()
    await expect(page.getByText('请求已处理，已恢复执行。',{exact:true})).toHaveCount(0)
    releaseNext()
    await until(()=>task.lastTurnReason==='completed')
    await page.getByRole('link',{name:'查看交付',exact:true}).click()
    await page.getByRole('link',{name:'返回任务详情',exact:true}).click()
    await expect(page.getByRole('heading',{name:'回答后继续执行',exact:true})).toBeVisible()
  } finally {release();releaseNext();await app.close()}
})

test('concurrent questions survive reload and a stale response cannot contaminate the next request',async({page},info)=>{
  const app=await fixture({localAssistants:true})
  let release=()=>{}
  try {
    const task=await app.manager.create({title:'T-Pair · 核对素材与尺寸',approvalMode:'auto'})
    app.manager.submit(task.id,'MULTI_QUESTION')
    await until(()=>app.manager.interactions.get(task.id)?.length===2)
    await page.goto(`${app.url}/#/tasks`)
    const row=page.getByRole('listitem',{name:'核对素材与尺寸',exact:true})
    await expect(row).toContainText('等待回答')
    await row.getByRole('link',{name:'处理请求',exact:true}).click()
    await expect(page).toHaveURL(`${app.url}/#/tasks/${task.id}`)
    await page.reload()
    const first=task.pendingInteraction!
    const firstTitle=first.title
    await expect(page.getByRole('form',{name:firstTitle,exact:true})).toBeVisible()
    await page.getByRole('textbox',{name:firstTitle,exact:true}).fill('原页面的回答')
    const held=new Promise<void>(resolve=>release=resolve)
    let intercepted=false
    await page.route('**/api/tasks/*/interaction',async route=>{
      intercepted=true;await held
      await route.fulfill({status:409,contentType:'application/json',body:JSON.stringify({error:'旧请求已失效'})})
    })
    await page.getByRole('button',{name:'提交回答',exact:true}).click()
    await until(()=>intercepted)
    await expect(page.getByRole('button',{name:'提交回答',exact:true})).toBeDisabled()
    // Another client answers while this browser's original POST is delayed.
    const response=await page.request.post(`${app.url}/api/tasks/${task.id}/interaction`,{data:{interactionId:first.id,kind:'question',answer:'{"value":"其他页面的回答"}'}})
    expect(response.status()).toBe(200)
    await expect.poll(()=>app.manager.get(task.id).pendingInteraction?.title).not.toBe(firstTitle)
    const secondTitle=task.pendingInteraction!.title
    expect(secondTitle).not.toBe(firstTitle)
    await expect(page.getByRole('form',{name:secondTitle,exact:true})).toBeVisible()
    release()
    await expect(page.getByRole('button',{name:'提交回答',exact:true})).toBeEnabled()
    await expect(page.getByRole('alert')).toHaveCount(0)
    await page.unroute('**/api/tasks/*/interaction')
    for(const width of [1280,390,320]) {
      await page.setViewportSize({width,height:780})
      const main=page.getByRole('main',{name:'任务频道'})
      expect(await main.evaluate(el=>el.scrollWidth-el.clientWidth)).toBeLessThanOrEqual(1)
      await page.getByRole('button',{name:'提交回答',exact:true}).scrollIntoViewIfNeeded()
      await expect(page.getByRole('button',{name:'提交回答',exact:true})).toBeInViewport()
      await expect(page.getByRole('button',{name:'跳过',exact:true})).toBeInViewport()
      await page.screenshot({path:info.outputPath(`pending-question-${width}.png`)})
    }
    await page.getByRole('button',{name:'跳过',exact:true}).click()
    await until(()=>!app.manager.runs.has(task.id))
    await expect(page.getByRole('region',{name:'待处理请求'})).toHaveCount(0)
    await expect(page.getByRole('link',{name:'查看交付',exact:true})).toBeVisible()
    expect(task.lastTurnReason).toBe('completed')
    expect(app.manager.interactions.has(task.id)).toBe(false)
  } finally {release();await app.close()}
})

test('a delegated user question is answered through the task page without opening chat',async({page})=>{
  const app=await fixture({localAssistants:true})
  try {
    const task=await app.manager.create({title:'T-Sub_ · 子助手核对',approvalMode:'auto'})
    app.manager.submit(task.id,'CHILD_QUESTION')
    await until(()=>task.pendingInteraction)
    await page.goto(`${app.url}/#/tasks/${task.id}`)
    await page.getByRole('textbox',{name:'素材名称',exact:true}).fill('sample.png')
    await page.getByRole('button',{name:'提交回答',exact:true}).click()
    await until(()=>!app.manager.runs.has(task.id))
    await expect(page.getByRole('region',{name:'待处理请求'})).toHaveCount(0)
    await expect(page.getByRole('alert')).toHaveCount(0)
    expect(task.lastTurnReason).toBe('completed')
    expect(task.lastFailure).toBeNull()
  } finally {await app.close()}
})

test('ending at the native iteration limit releases the slot and offers supplement',async({page})=>{
  const app=await fixture({localAssistants:true,maxIterations:1})
  try {
    const task=await app.manager.create({title:'T-Stop · 步数结束核对',approvalMode:'auto'})
    app.manager.submit(task.id,'REJECT_ONLY')
    await until(()=>task.pendingInteraction?.kind==='approval' && task.pendingInteraction.purpose==='iterations')
    await page.goto(`${app.url}/#/tasks/${task.id}`)
    await page.getByRole('button',{name:'结束本轮',exact:true}).click()
    await until(()=>!app.manager.runs.has(task.id))
    expect(task.lastTurnReason).toBe('interrupted')
    await page.getByRole('link',{name:'返回任务',exact:true}).click()
    const row=page.getByRole('listitem',{name:'步数结束核对',exact:true})
    await expect(row).toContainText('已停止')
    await row.getByRole('link',{name:'补充任务',exact:true}).click()
    await expect(page.getByRole('form',{name:'补充任务',exact:true})).toBeVisible()
    await expect(page.getByRole('button',{name:'提交补充',exact:true})).toBeEnabled()
  } finally {await app.close()}
})
