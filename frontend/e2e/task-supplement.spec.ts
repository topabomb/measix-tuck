import {test,expect} from '@playwright/test'
import {readFile} from 'node:fs/promises'
import {fixture,until} from '../../tests/fixtures/fixture.mjs'

test('ended tasks supplement the same native conversation with attachments and clear action tiers',async({page},info)=>{
  const app=await fixture({localAssistants:true,attachmentModels:true,delivery:true})
  try {
    const task=await app.manager.create({title:'T-Keep · 交付内容核对',approvalMode:'manual'})
    app.manager.submit(task.id,'初始内容核对')
    await until(()=>task.lastTurnReason==='completed'&&!app.manager.runs.has(task.id))
    const nativeId=task.nativeSessionId
    await page.goto(`${app.url}/#/tasks`)
    const row=page.getByRole('listitem').filter({hasText:'交付内容核对'})
    await expect(row).toContainText('1 轮')
    const settingsRequests:string[]=[]
    page.on('request',request=>{if(request.method()==='POST'&&/\/(model|approval-mode)$/.test(request.url())) settingsRequests.push(request.url())})
    await expect(row.getByRole('link',{name:'查看交付',exact:true})).toBeVisible()
    for(const width of [1280,390,320]) {
      await page.setViewportSize({width,height:780})
      const button=row.getByRole('link',{name:'补充任务',exact:true})
      await button.scrollIntoViewIfNeeded()
      await expect(button).toBeInViewport()
      expect(await page.getByRole('main',{name:'任务频道'}).evaluate(el=>el.scrollWidth-el.clientWidth)).toBeLessThanOrEqual(1)
      await page.screenshot({path:info.outputPath(`supplement-overview-${width}.png`)})
    }
    await page.setViewportSize({width:390,height:780})
    await row.getByRole('link',{name:'补充任务',exact:true}).click()
    await expect(page.getByRole('heading',{name:'补充任务',exact:true})).toBeVisible()
    await expect(page.getByRole('form',{name:'补充任务',exact:true})).toBeVisible()
    await expect(page.locator('.task-source')).toHaveText('交付内容核对')
    await expect(page.getByRole('link',{name:'查看对话',exact:true})).toHaveCount(0)
    await expect(page.getByLabel('标题',{exact:true})).toHaveCount(0)
    await expect(page.getByRole('combobox')).toHaveCount(0)
    await page.getByLabel('补充要求',{exact:true}).fill('补充参考资料')
    await page.locator('input[type=file]').setInputFiles({name:'supplement.txt',mimeType:'text/plain',buffer:Buffer.from('补充的核对内容')})
    await page.getByRole('link',{name:'目标',exact:true}).click()
    await page.getByRole('navigation',{name:'工作模式'}).getByRole('link',{name:'任务',exact:true}).click()
    await expect(page.getByLabel('补充要求',{exact:true})).toHaveValue('补充参考资料')
    await expect(page.getByText('supplement.txt',{exact:true})).toBeVisible()
    await expect(page.getByRole('list',{name:'附件',exact:true}).getByRole('listitem',{name:'supplement.txt',exact:true})).toBeVisible()
    await page.screenshot({path:info.outputPath('supplement-form-mobile.png')})
    // An attachment alone is a supported native user message too.
    await page.getByLabel('补充要求',{exact:true}).fill('')
    await page.getByRole('button',{name:'提交补充',exact:true}).click()
    await expect(page).toHaveURL(/#\/tasks$/)
    await until(()=>task.lastTurnReason==='completed'&&!app.manager.runs.has(task.id))
    expect(task.turnCount).toBe(2)
    expect(task.approvalMode).toBe('manual')
    expect(settingsRequests).toEqual([])
    expect(app.manager.tasks.size).toBe(1)
    expect(task.nativeSessionId).toBe(nativeId)
    expect(task.title).toBe('T-Keep · 交付内容核对')
    const native=await app.docker.request(`/api/sessions/${nativeId}/recovery`)
    expect(JSON.stringify(native.messages)).toContain('supplement.txt')
    await row.getByRole('link',{name:'查看交付',exact:true}).click()
    const file=page.getByRole('link',{name:/^下载 .*supplement\.txt$/})
    await expect(file).toHaveCount(0)
    await app.deliver(task,'已核对补充资料',[(task as typeof task & {uploads:{path:string}[]}).uploads[0].path])
    await page.reload()
    await expect(file).toBeVisible()
    const download=page.waitForEvent('download');await file.click()
    expect(await readFile((await (await download).path())!,'utf8')).toBe('补充的核对内容')
    await page.screenshot({path:info.outputPath('supplement-delivery-mobile.png')})
  } finally {await app.close()}
})

test('a quickly completed supplement outside the recent two still opens its result',async({page})=>{
  const app=await fixture({localAssistants:true})
  try {
    const task=await app.manager.create({title:'T-Old_ · 较早的任务',approvalMode:'auto'})
    task.createdAt='2020-01-01T00:00:00Z';app.manager.settle(task,'completed')
    for(let i=0;i<2;i++) {
      const newer=await app.manager.create({title:`T-new${i} · 较新的任务 ${i}`})
      app.manager.settle(newer,'completed')
    }
    await page.goto(`${app.url}/#/tasks/${task.id}/supplement`)
    await page.getByLabel('补充要求',{exact:true}).fill('补充核对结果')
    await page.route('**/api/tasks/*/prompts',async route=>{
      const response=await route.fetch()
      await until(()=>task.lastTurnReason==='completed'&&!app.manager.runs.has(task.id))
      await route.fulfill({response})
    })
    await page.getByRole('button',{name:'提交补充',exact:true}).click()
    await expect(page).toHaveURL(`${app.url}/#/tasks/assistants/root?period=all`)
    await expect(page.getByRole('listitem',{name:'较早的任务',exact:true})).toBeInViewport()
    await expect(page.getByRole('listitem',{name:'较早的任务',exact:true}).getByRole('link',{name:'查看交付',exact:true})).toBeVisible()
  } finally {await app.close()}
})

test('supplements respect capacity and a competing run cannot turn them into queued messages',async({page})=>{
  let release=()=>{},post=()=>{}
  const held=new Promise<void>(resolve=>release=resolve)
  const app=await fixture({localAssistants:true,beforeResponse:prompt=>prompt.includes('RACE_HOLD')?held:undefined})
  try {
    const task=await app.manager.create({title:'T-Race · 已停止的任务',approvalMode:'auto'})
    app.manager.settle(task,'interrupted')
    for(let i=0;i<2;i++) {
      const active=await app.manager.create({title:`T-act${i} · 占用 ${i}`})
      app.manager.submit(active.id,'SCHEMA_QUESTION')
    }
    await page.goto(`${app.url}/#/tasks/assistants/root`)
    const row=page.getByRole('listitem').filter({hasText:'已停止的任务'})
    await row.getByRole('link',{name:'补充任务',exact:true}).click()
    await expect(page.getByRole('button',{name:'提交补充',exact:true})).toBeDisabled()
    await expect(page.getByRole('form',{name:'补充任务',exact:true}).getByRole('status')).toContainText('进行中任务已满')
    for(const active of app.manager.tasks.values()) if(active.id!==task.id) await page.request.post(`${app.url}/api/tasks/${active.id}/abort`,{data:{}})
    await expect(page.getByRole('button',{name:'提交补充',exact:true})).toBeEnabled()
    await page.getByLabel('补充要求',{exact:true}).fill('应保留这份补充，不要进入其他轮次')
    const gate=new Promise<void>(resolve=>post=resolve)
    let posted=false
    await page.route('**/api/tasks/*/prompts',async route=>{posted=true;await gate;await route.continue()})
    await page.getByRole('button',{name:'提交补充',exact:true}).click()
    await expect.poll(()=>posted).toBe(true)
    app.manager.submit(task.id,'RACE_HOLD 其他浏览器发起的一轮')
    await until(()=>task.status==='working')
    post()
    await expect(page.getByRole('alert')).toContainText('任务状态已变化')
    await expect(page.getByLabel('补充要求',{exact:true})).toHaveValue('应保留这份补充，不要进入其他轮次')
    await expect(page.getByRole('button',{name:'提交补充',exact:true})).toBeDisabled()
    expect(task.queue).toHaveLength(0)
    expect(task.turnCount).toBe(1)
    release()
    await until(()=>task.lastTurnReason==='completed'&&!app.manager.runs.has(task.id))
    await page.reload()
    task.queue=[{id:'pending',prompt:'原来尚未发送的内容'}];task.queuePaused=true;task.queuedPrompts=1
    app.manager.publish(task,{type:'state',task})
    await expect(page.getByRole('region',{name:'任务需要处理'})).toBeVisible()
    await expect(page.getByRole('link',{name:'进入对话处理',exact:true})).toBeVisible()
    await expect(page.getByRole('button',{name:'提交补充',exact:true})).toHaveCount(0)
    await expect(page.getByRole('link',{name:'查看对话',exact:true})).toHaveCount(0)
    await expect(page.getByRole('link',{name:'返回全部任务',exact:true})).toBeVisible()
  } finally {release();post();await app.close()}
})
