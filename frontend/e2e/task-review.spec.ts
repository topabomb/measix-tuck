import {test,expect} from '@playwright/test'
import {fixture} from '../../tests/fixtures/fixture.mjs'
import {writeFile,unlink,readFile} from 'node:fs/promises'
import {join} from 'node:path'

test('delivery refresh preserves key-file snapshots after working files are removed',async({page})=>{
  const app=await fixture({localAssistants:true,attachmentModels:true,delivery:true})
  try {
    const task=await app.manager.create({title:'T-Page · 交付分页核对'})
    const names=Array.from({length:10},(_,index)=>`result-${String(index).padStart(2,'0')}.txt`)
    const paths=names.map(name=>join(task.workspace,name))
    await Promise.all(paths.map(path=>writeFile(path,'result')))
    await app.deliver(task,'已交付 10 份关键结果文件。',names)
    app.manager.settle(task,'completed')
    await page.goto(`${app.url}/#/tasks/${task.id}/delivery`)
    await expect(page.locator('.delivery-files li')).toHaveCount(10)
    await Promise.all(paths.map(path=>unlink(path)))
    await page.getByRole('button',{name:'刷新交付',exact:true}).click()
    await expect(page.locator('.delivery-files li')).toHaveCount(10)
    await expect(page.getByRole('navigation',{name:'文件分页',exact:true})).toHaveCount(0)
    await expect(page.getByRole('link',{name:'下载 result-00.txt',exact:true})).toBeVisible()
    const download=page.waitForEvent('download')
    await page.getByRole('link',{name:'下载 result-00.txt',exact:true}).click()
    expect(await readFile((await(await download).path())!,'utf8')).toBe('result')
  } finally {await app.close()}
})

test('delivery keeps wide content inside its own surface and separates read failures from empty results',async({page},info)=>{
  const app=await fixture({localAssistants:true,attachmentModels:true,delivery:true})
  try {
    const task=await app.manager.create({title:'T-Wide · 多列交付核对'})
    app.manager.append(task,{role:'user',blocks:[{id:'u',type:'markdown',data:{markdown:'核对交付'}}]})
    const columns=Array.from({length:12},(_,i)=>`字段 ${i+1}`)
    const table=[`|${columns.join('|')}|`,`|${columns.map(()=> '---').join('|')}|`,`|${columns.map(()=> '清晰交付内容').join('|')}|`].join('\n')
    const summary=`结果已核对。\n\n${table}\n\n${'reference-'.repeat(40)}`
    expect(summary.length).toBeLessThanOrEqual(1000)
    await app.deliver(task,summary)
    app.manager.settle(task,'completed')
    await page.setViewportSize({width:390,height:780})
    await page.goto(`${app.url}/#/tasks/${task.id}/delivery`)
    await expect(page.getByRole('table')).toBeVisible()
    const main=page.getByRole('main',{name:'任务频道'})
    expect(await main.evaluate(el=>el.scrollWidth-el.clientWidth)).toBeLessThanOrEqual(1)
    await page.screenshot({path:info.outputPath('delivery-table-mobile.png')})
    await page.route('**/api/tasks/*/deliveries?limit=*',route=>route.fulfill({status:503}))
    await page.reload()
    await expect(page.locator('.task-conclusion')).toContainText('结果已核对')
    await expect(page.getByRole('alert')).toContainText('版本列表读取失败')
    await expect(page.getByText('无交付文件。',{exact:true})).toBeVisible()
    await page.unroute('**/api/tasks/*/deliveries?limit=*')
    await page.route('**/api/tasks/*/deliveries/latest',route=>route.fulfill({status:503}))
    await page.reload()
    await expect(page.getByRole('alert')).toContainText('交付读取失败')
    await expect(page.getByText('尚未提交交付',{exact:false})).toHaveCount(0)
    await expect(page.getByText('无交付文件。',{exact:true})).toHaveCount(0)
    await page.screenshot({path:info.outputPath('delivery-read-failure.png')})
    await page.unroute('**/api/tasks/*/deliveries/latest')
    await page.getByRole('button',{name:'刷新交付',exact:true}).click()
    await expect(page.locator('.task-conclusion')).toContainText('结果已核对')
    await expect(page.getByRole('alert')).toHaveCount(0)
  } finally {await app.close()}
})

test('task submission uses configured defaults without technical settings controls or mutations',async({page})=>{
  const app=await fixture({localAssistants:true,attachmentModels:true})
  try {
    const settingsRequests:string[]=[]
    page.on('request',request=>{if(request.method()==='POST'&&/\/(model|approval-mode)$/.test(request.url())) settingsRequests.push(request.url())})
    await page.goto(`${app.url}/#/tasks/assistants/root/new`)
    await expect(page.getByRole('combobox')).toHaveCount(0)
    await expect(page.getByRole('main')).not.toContainText(/模型|推理|审批/)
    await page.getByLabel('标题',{exact:true}).fill('默认设置核对')
    await page.getByLabel('任务要求',{exact:true}).fill('简单回复即可')
    const request=page.waitForRequest(request=>request.url().endsWith('/api/tasks')&&request.method()==='POST')
    await page.getByRole('button',{name:'提交任务',exact:true}).click()
    const body=(await request).postDataJSON()
    expect(body.approvalMode).toBe('auto')
    expect(body).not.toHaveProperty('modelId')
    expect(body).not.toHaveProperty('reasoningEffort')
    await expect(page).toHaveURL(/#\/tasks$/)
    const task=[...app.manager.tasks.values()][0]
    expect(task.modelId).toBe('vision')
    expect(task.approvalMode).toBe('auto')
    expect(settingsRequests).toEqual([])
  } finally {await app.close()}
})

test('task page scroll memory survives short pages, browser back and mode switches',async({page})=>{
  const app=await fixture({localAssistants:true})
  try {
    for(let i=0;i<25;i++) {
      const task=await app.manager.create({title:`T-${String(i).padStart(4,'0')} · 滚动记录 ${i}`})
      app.manager.settle(task,'completed')
    }
    await page.setViewportSize({width:390,height:640})
    await page.goto(`${app.url}/#/tasks/assistants/root`)
    const main=page.getByRole('main',{name:'任务频道'})
    await expect(main.locator('.task-run-meta')).toHaveCount(25)
    await expect(main.locator('.task-run-meta').filter({hasText:'读取中…'})).toHaveCount(0)
    const row=page.locator('.task-list').getByRole('listitem').filter({hasText:'滚动记录 12'})
    await row.getByRole('link',{name:'查看交付',exact:true}).scrollIntoViewIfNeeded()
    const before=await main.evaluate(el=>el.scrollTop)
    expect(before).toBeGreaterThan(300)
    await row.getByRole('link',{name:'查看交付',exact:true}).click()
    await expect(page.getByRole('heading',{name:'滚动记录 12'})).toBeVisible()
    await page.goBack()
    await expect(main.locator('.task-run-meta')).toHaveCount(25)
    await expect(main.locator('.task-run-meta').filter({hasText:'读取中…'})).toHaveCount(0)
    await expect.poll(()=>main.evaluate(el=>el.scrollTop)).toBeCloseTo(before,0)
    await page.getByRole('link',{name:'目标',exact:true}).click()
    await page.getByRole('navigation',{name:'工作模式'}).getByRole('link',{name:'任务',exact:true}).click()
    await expect.poll(()=>main.evaluate(el=>el.scrollTop)).toBeCloseTo(before,0)
  } finally {await app.close()}
})

test('task history filters creation time and confines conversation links to the history page',async({page},info)=>{
  const app=await fixture({localAssistants:true})
  try {
    for(const [code,title,hours] of [['Now_', '今天下达',1],['Week','本周下达',48],['Past','更早下达',240]] as const) {
      const task=await app.manager.create({title:`T-${code} · ${title}`})
      task.createdAt=new Date(Date.now()-hours*3600000).toISOString();app.manager.settle(task,'completed')
    }
    await page.goto(`${app.url}/#/tasks`)
    await expect(page.getByRole('heading',{name:'让 Tuck 执行任务',exact:true})).toBeVisible()
    await expect(page.getByText('为 Agent 浏览器自动化优化',{exact:true})).toBeVisible()
    for(const width of [1920,390]) {
      await page.setViewportSize({width,height:780})
      await expect.poll(()=>page.evaluate(()=>Math.abs(document.querySelector('.tasks-home-header > div')!.getBoundingClientRect().left-document.querySelector('.task-overview')!.getBoundingClientRect().left))).toBeLessThan(1)
      await page.screenshot({path:info.outputPath(`task-home-heading-${width}.png`)})
    }
    await expect(page.getByRole('main').locator('a[href*="/chat/"]')).toHaveCount(0)
    await expect(page.getByRole('link',{name:'今天下达',exact:true})).toHaveCount(0)
    await page.getByRole('region',{name:'通用助手',exact:true}).getByRole('link',{name:'全部任务'}).click()
    await expect(page.locator('.tasks-home-header')).toHaveCount(0)
    const filter=page.getByRole('group',{name:'下达时间'})
    await expect(filter.getByRole('button',{name:'今天',exact:true})).toHaveAttribute('aria-pressed','true')
    await expect(page.getByRole('searchbox')).toHaveCount(0)
    await expect(page.locator('.task-list').getByRole('listitem')).toHaveCount(1)
    await expect(page.locator('.task-list').getByRole('listitem')).toContainText('今天下达')
    await filter.getByRole('button',{name:'最近 7 天',exact:true}).click()
    await expect(page.locator('.task-list').getByRole('listitem')).toHaveCount(2)
    await filter.getByRole('button',{name:'全部',exact:true}).click()
    await expect(page.locator('.task-list').getByRole('listitem')).toHaveCount(3)
    for(const width of [1280,390,320]) {
      await page.setViewportSize({width,height:780})
      const main=page.getByRole('main',{name:'任务频道'})
      expect(await main.evaluate(el=>el.scrollWidth-el.clientWidth)).toBeLessThanOrEqual(1)
      await expect(filter.getByRole('button',{name:'全部',exact:true})).toBeInViewport()
      const bounds=await filter.getByRole('button').evaluateAll(items=>items.map(item=>item.getBoundingClientRect().top))
      expect(Math.max(...bounds)-Math.min(...bounds)).toBeLessThanOrEqual(1)
      expect(await filter.getByRole('button').evaluateAll(items=>items.every(item=>item.scrollWidth<=item.clientWidth))).toBe(true)
      await page.screenshot({path:info.outputPath(`task-history-${width}.png`)})
    }
    await page.locator('.task-list').getByRole('listitem').filter({hasText:'今天下达'}).getByRole('link',{name:'查看交付'}).click()
    await expect(page).toHaveURL(/\/delivery\?returnTo=/)
    expect(new URL(page.url().split('#')[1],app.url).searchParams.get('returnTo')).toBe('/tasks/assistants/root?period=all')
    await expect(page.getByRole('link',{name:'查看对话',exact:true})).toHaveCount(0)
    await page.goBack()
    await expect(filter.getByRole('button',{name:'全部',exact:true})).toHaveAttribute('aria-pressed','true')
    await expect(page.getByRole('link',{name:'今天下达',exact:true})).toHaveAttribute('title','Chat details')
    await page.getByRole('link',{name:'今天下达',exact:true}).click()
    await expect(page).toHaveURL(/#\/chat\/conversations\//)
  } finally {await app.close()}
})
