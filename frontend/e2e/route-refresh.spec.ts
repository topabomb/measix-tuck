import {test,expect} from '@playwright/test'
import {fixture} from '../../tests/fixtures/fixture.mjs'
import {writeFile} from 'node:fs/promises'
import {join} from 'node:path'

test('task filter, page and return destinations survive refresh throughout supplement and delivery',async({page})=>{
  const app=await fixture({localAssistants:true,attachmentModels:true,delivery:true})
  try {
    for(let i=0;i<35;i++) {
      const task=await app.manager.create({title:`T-r${String(i).padStart(3,'0')} · 归档结果 ${i}`})
      task.createdAt=new Date(Date.now()-(i+2)*86400000).toISOString()
      app.manager.append(task,{role:'assistant',blocks:[{id:'answer',type:'markdown',data:{markdown:`已完成核对 ${i}`}}]})
      await app.deliver(task,`已完成核对 ${i}`)
      app.manager.settle(task,'completed')
    }
    await page.goto(`${app.url}/#/tasks/assistants/root`)
    await expect(page.getByRole('button',{name:'今天',exact:true})).toHaveAttribute('aria-pressed','true')
    await page.getByRole('button',{name:'全部',exact:true}).click()
    await page.getByRole('button',{name:'下一页',exact:true}).click()
    const listUrl=`${app.url}/#/tasks/assistants/root?period=all&page=2`
    await expect(page).toHaveURL(listUrl)
    await page.reload()
    await expect(page.getByRole('navigation',{name:'任务分页'})).toContainText('2 / 2')
    await expect(page.getByRole('list',{name:'任务列表'}).getByRole('listitem')).toHaveCount(5)
    await page.getByRole('link',{name:'新建任务',exact:true}).click()
    await expect(page.getByRole('heading',{name:'新建任务',exact:true})).toBeVisible()
    await page.reload()
    await expect(page.getByRole('heading',{name:'新建任务',exact:true})).toBeVisible()
    await page.getByRole('link',{name:'返回全部任务',exact:true}).click()
    await expect(page).toHaveURL(listUrl)
    const task=[...app.manager.tasks.values()][30]
    await page.goto(`${app.url}/#/tasks/${task.id}?returnTo=${encodeURIComponent('/tasks/assistants/root?period=all&page=2')}`)
    await page.getByRole('link',{name:'补充任务',exact:true}).click()
    await page.getByRole('link',{name:'查看上次交付',exact:true}).click()
    await expect(page.getByRole('article',{name:'交付摘要'})).toBeVisible()
    await page.reload()
    await page.getByRole('link',{name:'返回补充任务',exact:true}).click()
    await expect(page.getByRole('heading',{name:'补充任务',exact:true})).toBeVisible()
    await page.reload()
    await page.getByRole('link',{name:'返回任务详情',exact:true}).click()
    await expect(page.getByRole('heading',{name:'归档结果 30',exact:true})).toBeVisible()
    await page.reload()
    await page.getByRole('link',{name:'返回全部任务',exact:true}).click()
    await expect(page).toHaveURL(listUrl)
    await page.getByRole('link',{name:'补充任务',exact:true}).first().click()
    await page.getByRole('link',{name:'查看上次交付',exact:true}).click()
    await expect(page.getByRole('article',{name:'交付摘要'})).toBeVisible()
    await page.reload()
    await page.getByRole('link',{name:'返回补充任务',exact:true}).click()
    await expect(page.getByRole('heading',{name:'补充任务',exact:true})).toBeVisible()
    await page.getByRole('link',{name:'返回全部任务',exact:true}).click()
    await expect(page).toHaveURL(listUrl)
  } finally {await app.close()}
})

test('delivery key-file search survives refresh without changing its return destination',async({page},info)=>{
  const app=await fixture({localAssistants:true,attachmentModels:true,delivery:true})
  try {
    const task=await app.manager.create({title:'T-file · 多文件交付'})
    await Promise.all(Array.from({length:10},(_,i)=>writeFile(join(task.workspace,`result-${String(i).padStart(2,'0')}.txt`),'verified')))
    await writeFile(join(task.workspace,'unrelated.txt'),'other')
    await app.deliver(task,'多文件交付',Array.from({length:10},(_,i)=>`result-${String(i).padStart(2,'0')}.txt`))
    app.manager.settle(task,'completed')
    const path=`/tasks/${task.id}/delivery?from=supplement`
    await page.goto(`${app.url}/#${path}`)
    await page.getByRole('searchbox',{name:'搜索文件'}).fill('result-09')
    await expect(page).toHaveURL(/fileSearch=result-09/)
    await page.reload()
    await expect(page.getByRole('searchbox',{name:'搜索文件'})).toHaveValue('result-09')
    await expect(page.getByRole('navigation',{name:'文件分页'})).toHaveCount(0)
    await expect(page.getByRole('list',{name:'交付文件'}).getByRole('listitem')).toHaveCount(1)
    await page.screenshot({path:info.outputPath('delivery-refreshed.png')})
    await page.getByRole('link',{name:'返回补充任务',exact:true}).click()
    await expect(page.getByRole('heading',{name:'补充任务',exact:true})).toBeVisible()
  } finally {await app.close()}
})

test('assistant, mode and task detail routes stay identifiable after refresh; bad identifiers do not create conversations',async({page},info)=>{
  const app=await fixture({localAssistants:true})
  try {
    const task=await app.manager.create({title:'T-page · 路由核对'})
    await page.route('**/api/runtime',async route=>{
      const response=await route.fetch(),body=await response.json()
      body.assistants.items.push({agent:'new',displayName:'路径同名助手'})
      await route.fulfill({response,json:body})
    })
    await page.goto(`${app.url}/#/tasks/assistants/new`)
    await expect(page.getByRole('heading',{name:'路径同名助手',exact:true})).toBeVisible()
    await page.reload()
    await expect(page.getByRole('heading',{name:'路径同名助手',exact:true})).toBeVisible()
    await page.getByRole('link',{name:'新建任务',exact:true}).click()
    await expect(page.getByRole('heading',{name:'新建任务',exact:true})).toBeVisible()
    await page.reload()
    await page.getByRole('link',{name:'返回全部任务',exact:true}).click()
    await expect(page.getByRole('heading',{name:'路径同名助手',exact:true})).toBeVisible()
    for(const [path,title] of [['/tasks','让 Tuck 执行任务'],['/goals','目标'],['/orchestration','编排'],[`/tasks/${task.id}`,'路由核对']]) {
      await page.goto(`${app.url}/#${path}`)
      await expect(page.getByRole('heading',{name:title,exact:true})).toBeVisible()
      await page.reload()
      await expect(page).toHaveURL(`${app.url}/#${path}`)
      await expect(page.getByRole('heading',{name:title,exact:true})).toBeVisible()
    }
    await page.screenshot({path:info.outputPath('task-detail-refreshed.png')})
    const count=app.manager.tasks.size
    for(const path of ['/chat/conversations/missing',`/chat/conversations/${task.id}/messages/9007199254740992`,'/tasks/assistants/missing/new','/tasks/missing/delivery']) {
      await page.goto(`${app.url}/#${path}`)
      await expect(page.getByRole('alert')).toBeVisible()
      await page.reload()
      await expect(page.getByRole('alert')).toBeVisible()
    }
    expect(app.manager.tasks.size).toBe(count)
  } finally {await app.close()}
})

test('canonical deep routes refresh to the same page and unknown routes stay explicit',async({page})=>{
  const app=await fixture({localAssistants:true})
  try {
    const task=await app.manager.create({title:'T-link · 可直接打开'})
    app.manager.append(task,{role:'assistant',blocks:[{id:'answer',type:'markdown',data:{markdown:'可直接定位的回复'}}]})
    app.manager.settle(task,'completed')
    for(const suffix of ['delivery','supplement']) {
      const path=`/tasks/${task.id}/${suffix}`
      await page.goto(`${app.url}/#${path}/`)
      await expect(page).toHaveURL(`${app.url}/#${path}`)
      await page.reload()
      await expect(page.getByRole('heading',{name:suffix==='delivery'?'可直接打开':'补充任务',exact:true})).toBeVisible()
    }
    await page.goto(`${app.url}/#/tasks/assistants/root/new/`)
    await expect(page).toHaveURL(`${app.url}/#/tasks/assistants/root/new`)
    await page.reload()
    await expect(page.getByRole('heading',{name:'新建任务',exact:true})).toBeVisible()
    await page.goto(`${app.url}/#/chat/conversations/${task.id}/messages/0/`)
    await expect(page).toHaveURL(`${app.url}/#/chat/conversations/${task.id}/messages/0`)
    await page.reload()
    await expect(page.locator('.linked-message')).toContainText('可直接定位的回复')
    await page.goto(`${app.url}/#/tasks/nonexistent/page`)
    await expect(page.getByRole('heading',{name:'页面不存在'})).toBeVisible()
  } finally {await app.close()}
})
