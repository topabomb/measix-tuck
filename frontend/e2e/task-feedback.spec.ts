import {test,expect} from '@playwright/test'
import {writeFile} from 'node:fs/promises'
import {join} from 'node:path'
import {fixture} from '../../tests/fixtures/fixture.mjs'

test('supplement uses the explicit delivery summary while delivery retains the full answer',async({page})=>{
  const app=await fixture({localAssistants:true,attachmentModels:true,delivery:true})
  try {
    const task=await app.manager.create({title:'T-Long · 长回复交付'})
    const answer='原始完整回复开头。\n\n'+'中间说明。'.repeat(2000)+'\n\n最终结论：文件已经整理完成。'
    app.manager.append(task,{role:'assistant',blocks:[{id:'answer',type:'markdown',data:{markdown:answer}}]})
    await app.deliver(task,'正式交付摘要：文件已经整理完成。')
    app.manager.settle(task,'completed')
    const requests:string[]=[]
    page.on('request',request=>{if(request.url().includes(`/tasks/${task.id}/result`))requests.push(request.url())})
    await page.goto(`${app.url}/#/tasks`)
    const row=page.getByRole('listitem',{name:'长回复交付',exact:true})
    await expect(row.getByRole('group',{name:'交付概况'})).toContainText('正式交付摘要')
    await row.getByRole('link',{name:'补充任务',exact:true}).click()
    const previous=page.getByRole('region',{name:'上次结果',exact:true})
    await expect(previous).toContainText('正式交付摘要')
    await expect(previous).not.toContainText('原始完整回复开头')
    expect(requests).toEqual([])
    await previous.getByRole('link',{name:'查看上次交付',exact:true}).click()
    await expect(page.getByRole('article',{name:'任务回答',exact:true})).toContainText('原始完整回复开头。')
    await expect(page.getByRole('article',{name:'任务回答',exact:true})).toContainText('最终结论：文件已经整理完成。')
    expect(requests.some(url=>url.endsWith(`/tasks/${task.id}/result`))).toBe(true)
  } finally {await app.close()}
})

test('task previews follow live activity; ended tasks require an explicit delivery',async({page},info)=>{
  const app=await fixture({localAssistants:true})
  try {
    const task=await app.manager.create({title:'T-Tail · 跟随最新输出'})
    const message=app.manager.append(task,{role:'assistant',live:true,blocks:[{id:'reason',type:'reasoning',data:{text:'开头不应停留。'.repeat(180)+'正在检查第一项'}}]})
    task.status='working';task.activeAssistantIndex=message.index;task.activity={phase:'reasoning',startedAt:Date.now()-3000}
    app.manager.publish(task,{type:'state',task})
    await page.goto(`${app.url}/#/tasks`)
    const row=page.getByRole('listitem',{name:'跟随最新输出',exact:true})
    const preview=row.getByLabel('实时过程预览：思考',{exact:true})
    await expect(preview).toContainText('正在检查第一项')
    await expect(preview).toHaveAttribute('role','note')
    await expect(preview).toHaveAccessibleDescription('过程片段，非操作指令或最终结果。')
    const before=await row.getByLabel('当前阶段用时',{exact:true}).innerText()
    await page.clock.install();await page.clock.fastForward(3000)
    await expect(row.getByLabel('当前阶段用时',{exact:true})).not.toHaveText(before)
    const history:string[]=[]
    page.on('request',r=>{if(r.url().includes('/messages?'))history.push(r.url())})
    app.manager.appendText(task,message,'reason','text','，现在已检查到最后一项。')
    await expect(preview).toContainText('现在已检查到最后一项。')
    for(const width of [1280,390]) {
      await page.setViewportSize({width,height:780})
      expect(await preview.evaluate(el=>Math.abs(el.getBoundingClientRect().right-el.firstElementChild!.getBoundingClientRect().right))).toBeLessThan(1)
      expect(await page.getByRole('main').evaluate(el=>el.scrollWidth-el.clientWidth)).toBeLessThanOrEqual(1)
      await page.screenshot({path:info.outputPath(`live-tail-${width}.png`)})
    }
    expect(history).toEqual([])
    const reply=app.manager.append(task,{role:'assistant',live:true,blocks:[{id:'reply',type:'markdown',data:{markdown:'回复正文。'.repeat(200)+'最新输出的句子'}}]})
    task.activeAssistantIndex=reply.index;task.activity={phase:'responding',startedAt:Date.now()}
    app.manager.publish(task,{type:'state',task})
    await expect(row.getByLabel('实时过程预览：回复',{exact:true})).toContainText('最新输出的句子')
    await expect(row.getByLabel('实时过程预览：思考',{exact:true})).toHaveCount(0)
    await row.getByRole('link',{name:'查看任务',exact:true}).click()
    const detailPreview=page.getByLabel('实时过程预览：回复',{exact:true})
    await expect(detailPreview).toHaveAccessibleDescription('过程片段，非操作指令或最终结果。')
    app.manager.appendText(task,reply,'reply','markdown','\n正在整理结果。\n正在核对交付。\n最新一行：材料已核对。')
    await expect(detailPreview).toContainText('最新一行：材料已核对。')
    for(const width of [1280,390]) {
      await page.setViewportSize({width,height:780})
      const geometry=await detailPreview.evaluate(el=>({height:el.getBoundingClientRect().height,line:parseFloat(getComputedStyle(el).lineHeight),right:Math.abs(el.getBoundingClientRect().right-el.firstElementChild!.getBoundingClientRect().right),chars:el.textContent!.length}))
      expect(geometry.height).toBe(geometry.line)
      expect(geometry.right).toBeLessThan(1)
      expect(geometry.chars).toBeLessThanOrEqual(200)
      expect(await page.getByRole('main').evaluate(el=>el.scrollWidth-el.clientWidth)).toBeLessThanOrEqual(1)
      await page.screenshot({path:info.outputPath(`detail-live-tail-${width}.png`)})
    }
    // The same single-line surface follows reasoning, but never renders tool output.
    task.activeAssistantIndex=message.index;task.activity={phase:'reasoning',startedAt:Date.now()}
    app.manager.publish(task,{type:'state',task})
    await expect(page.getByLabel('实时过程预览：思考',{exact:true})).toContainText('现在已检查到最后一项。')
    await expect(detailPreview).toHaveCount(0)
    task.activity={phase:'tool',detail:'read_file',startedAt:Date.now()}
    app.manager.publish(task,{type:'state',task})
    await expect(page.getByLabel('实时过程预览：思考',{exact:true})).toHaveCount(0)
    task.activeAssistantIndex=reply.index;task.activity={phase:'responding',startedAt:Date.now()}
    app.manager.publish(task,{type:'state',task})
    await page.getByRole('link',{name:'返回任务',exact:true}).click()
    await expect(row.getByLabel('实时过程预览：回复',{exact:true})).toContainText('最新一行：材料已核对。')
    app.manager.appendText(task,reply,'reply','markdown','。最终结论：所有材料已核对。')
    reply.live=false;app.manager.replace(task,reply);app.manager.settle(task,'completed')
    await expect(row.getByRole('group',{name:'交付概况'})).toContainText('尚未提交交付')
    await expect(row.getByLabel('实时过程预览：回复',{exact:true})).toHaveCount(0)
    await expect(row.getByLabel('当前阶段用时',{exact:true})).toHaveCount(0)
    const result=await (await page.request.get(`${app.url}/api/tasks/${task.id}/result`)).json()
    expect(result.reply.length).toBeGreaterThan(800)
    expect(result.reply).toMatch(/最终结论：所有材料已核对。$/)
  } finally {await app.close()}
})

test('first-visit attachments update immediately without task events or submission',async({page})=>{
  const app=await fixture({localAssistants:true,attachmentModels:true})
  try {
    await page.goto(`${app.url}/#/tasks/assistants/root/new`)
    await expect(page.getByRole('button',{name:'提交任务',exact:true})).toBeEnabled()
    await expect(page.getByLabel('任务要求',{exact:true})).toHaveAttribute('aria-describedby','task-form-guide')
    await expect(page.getByRole('button',{name:'添加附件',exact:true})).toHaveAccessibleDescription('提交时上传，最多 8 个，单个不超过 16 MiB。')
    await expect(page.getByRole('list',{name:'附件',exact:true})).toHaveCount(0)
    const image={name:'reference.png',mimeType:'image/png',buffer:Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aT0cAAAAASUVORK5CYII=','base64')}
    const chooser=page.waitForEvent('filechooser')
    await page.getByRole('button',{name:'添加附件',exact:true}).click()
    await (await chooser).setFiles(image)
    const attachments=page.getByRole('list',{name:'附件',exact:true})
    await expect(attachments.getByRole('listitem',{name:'reference.png',exact:true})).toBeVisible()
    await expect(attachments).toContainText('待上传')
    expect(app.manager.tasks.size).toBe(0)
    await attachments.getByRole('button',{name:'移除 reference.png',exact:true}).click()
    await expect(attachments.getByRole('listitem')).toHaveCount(0)
    await page.locator('input[type=file]').setInputFiles(image)
    await expect(attachments.getByRole('listitem')).toHaveCount(1)
  } finally {await app.close()}
})

test('ended result summaries, supplement context and live progress share clear state boundaries',async({page},info)=>{
  const app=await fixture({localAssistants:true,attachmentModels:true,delivery:true})
  try {
    const task=await app.manager.create({title:'T-Read · 图片核对'})
    const at=Date.now()
    task.createdAt=new Date(at-120000).toISOString()
    task.turnTiming={turnId:'turn-1',startedAt:at-120000,observedAt:at-118000,endedAt:at-118000,waitingMs:500}
    app.manager.publish(task,{type:'state',task})
    app.manager.append(task,{role:'user',blocks:[{id:'u',type:'markdown',data:{markdown:'核对图片'}}]})
    app.manager.append(task,{role:'assistant',blocks:[{id:'call',type:'tool-call',data:{callId:'read',name:'read_file',input:{path:'reference.txt'}}}]})
    app.manager.append(task,{role:'assistant',blocks:[{id:'a',type:'markdown',data:{markdown:'**核对完成**，请使用修订后的图片。'}}]})
    await writeFile(join(task.workspace,'result.txt'),'verified')
    await app.deliver(task,'核对完成，请使用修订后的图片。',['result.txt'])
    task.turnCount=2
    // The fixture skips real turn execution; supply the durable v3 rollup.
    Object.assign(task,{historyTiming:{previousMs:2000,previousTurns:1,incomplete:false}})
    task.turnTiming={turnId:'turn-2',startedAt:at-65000,observedAt:at,endedAt:at,waitingMs:0}
    task.usage={inputTokens:1000,outputTokens:200,cacheReadTokens:100,cacheWriteTokens:50,reasoningTokens:20}
    app.manager.settle(task,'completed')
    const historyRequests:string[]=[],statisticsRequests:string[]=[]
    page.on('request',r=>{if(/\/messages\?/.test(r.url()))historyRequests.push(r.url());if(r.url().endsWith('/statistics'))statisticsRequests.push(r.url())})
    await page.goto(`${app.url}/#/tasks`)
    const row=page.getByRole('listitem',{name:'图片核对',exact:true})
    await expect(row).toContainText('核对完成，请使用修订后的图片。')
    await expect(row).toContainText('1 个文件')
    await expect(row).toContainText('2 轮')
    await expect(row).toContainText('开始')
    await expect(row).toContainText('用时 1分7秒')
    await expect(row).toContainText('工具 1 次')
    await expect(row).toContainText('总 Token 1.4K')
    const readsBeforeUpdate=statisticsRequests.length
    task.usage.outputTokens=400
    app.manager.publish(task,{type:'state',task})
    await expect(row).toContainText('总 Token 1.6K')
    expect(statisticsRequests.length).toBe(readsBeforeUpdate)
    expect(historyRequests).toEqual([])
    for(const width of [1280,390,320]) {
      await page.setViewportSize({width,height:780})
      expect(await page.getByRole('main',{name:'任务频道'}).evaluate(el=>el.scrollWidth-el.clientWidth)).toBeLessThanOrEqual(1)
      await page.screenshot({path:info.outputPath(`feedback-${width}.png`)})
    }
    await row.getByRole('link',{name:'补充任务',exact:true}).click()
    const previous=page.getByRole('region',{name:'上次结果',exact:true})
    await expect(previous).toContainText('核对完成，请使用修订后的图片。')
    await expect(previous.getByRole('link',{name:'下载 result.txt',exact:true})).toBeVisible()
    await expect(previous.getByRole('link',{name:'查看上次交付',exact:true})).toBeVisible()
    await page.screenshot({path:info.outputPath('supplement-context-mobile.png')})
    // An existing uploaded file and a previous answer must not become the new turn's delivery.
    task.status='working';task.lastTurnReason=null;task.turnCount=3;task.turnId='turn-3'
    Object.assign(task,{historyTiming:{previousMs:67000,previousTurns:2,incomplete:false}})
    task.turnTiming={turnId:'turn-3',startedAt:Date.now()-2000,observedAt:Date.now(),waitingMs:0}
    task.activity={phase:'processing',startedAt:Date.now()}
    app.manager.publish(task,{type:'task',task})
    await page.clock.install()
    await page.goto(`${app.url}/#/tasks`)
    await expect(row).toContainText('正在请求…')
    await expect(row).toContainText('工具 1 次')
    const elapsedBefore=await row.locator('.task-elapsed').innerText()
    const requestCount=statisticsRequests.length
    await page.clock.fastForward(3000)
    await expect(row.locator('.task-elapsed')).not.toHaveText(elapsedBefore)
    expect(statisticsRequests.length).toBe(requestCount)
    task.activity={phase:'reasoning',startedAt:Date.now()}
    app.manager.publish(task,{type:'state',task})
    await expect(row).toContainText('思考中…')
    expect(statisticsRequests.length).toBe(requestCount)
    await expect(row).toContainText('上次交付');await expect(row).toContainText('核对完成')
    await expect(row.getByRole('link',{name:'查看交付',exact:true})).toBeVisible()
    await row.getByRole('link',{name:'查看任务',exact:true}).click()
    await expect(page.getByText('正在执行，无需操作。',{exact:true})).toBeVisible()
    await expect(page.getByRole('heading',{name:'图片核对',exact:true}).getByRole('link')).toHaveAttribute('href',`#/chat/conversations/${task.id}`)
    await expect(page.getByRole('button',{name:'停止任务',exact:true})).toBeVisible()
    await page.screenshot({path:info.outputPath('running-progress-mobile.png')})
    await page.goto(`${app.url}/#/tasks/${task.id}/delivery`)
    await expect(page.getByRole('main')).toContainText('本轮尚未结束')
    await expect(page.getByRole('link',{name:'下载 result.txt',exact:true})).toBeVisible()
    expect((await (await page.request.get(`${app.url}/api/tasks/${task.id}/result`)).json()).reply).toBe('')
  } finally {await app.close()}
})
