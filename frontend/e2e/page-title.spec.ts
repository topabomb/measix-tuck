import {test,expect} from '@playwright/test'
import {fixture} from '../../tests/fixtures/fixture.mjs'

test('document titles track routes, loaded names, refresh, history and renames',async({page})=>{
  const app=await fixture({localAssistants:true})
  try {
    const task=await app.manager.create({title:'T-name · 标题核对'})
    app.manager.append(task,{role:'assistant',blocks:[{id:'answer',type:'markdown',data:{markdown:'完成'}}]})
    app.manager.settle(task,'completed')
    const ordinary=await app.manager.create({title:'普通对话'})
    // These are bootstrap entries, not stable pages: the default conversation
    // owns the final URL and title once its history has loaded.
    for(const entry of ['/','/chat']) {
      const destination=app.url+`/#/chat/conversations/${task.id}`
      await page.goto(app.url+'/#'+entry)
      await expect(page).toHaveURL(destination)
      await expect(page.locator('.conversation-vlist')).toContainText('完成')
      await expect(page).toHaveTitle('标题核对 · 对话 · Tuck')
      await page.reload()
      await expect(page).toHaveURL(destination)
      await expect(page.locator('.conversation-vlist')).toContainText('完成')
      await expect(page).toHaveTitle('标题核对 · 对话 · Tuck')
    }
    const cases=[
      ['/tasks','任务 · Tuck'],
      ['/workspace','工作区 · Tuck'],
      ['/tasks/assistants/root?period=all&page=1','通用助手 · 全部任务 · Tuck'],
      ['/tasks/assistants/root/new','新建任务 · 通用助手 · Tuck'],
      [`/tasks/${task.id}`,'标题核对 · 任务详情 · Tuck'],
      [`/tasks/${task.id}/supplement`,'补充任务 · 标题核对 · Tuck'],
      [`/tasks/${task.id}/delivery?from=supplement`,'查看交付 · 标题核对 · Tuck'],
      [`/chat/conversations/${task.id}`,'标题核对 · 对话 · Tuck'],
      [`/chat/conversations/${task.id}/messages/0`,'标题核对 · 消息 0 · 对话 · Tuck'],
      ['/goals','目标 · Tuck'],
      ['/orchestration','编排 · Tuck'],
      ['/missing','页面不存在 · Tuck'],
      ['/tasks/missing','任务详情 · Tuck'],
      [`/tasks/${ordinary.id}`,'任务详情 · Tuck'],
      ['/reference','参考工作台 · Tuck'],
      ['/architecture','架构说明 · Tuck'],
    ]
    for(const [path,title] of cases) {
      await page.goto(app.url+'/#'+path)
      await expect(page).toHaveTitle(title!)
      await page.reload()
      await expect(page).toHaveTitle(title!)
    }
    await page.goto(app.url+`/#/tasks/${task.id}/supplement`)
    await expect(page.getByText('尚未提交交付，可从对话或工作目录查看已有内容。')).toBeVisible()
    await page.goto(app.url+`/#/tasks/${task.id}/delivery?from=supplement`)
    await expect(page).toHaveTitle('查看交付 · 标题核对 · Tuck')
    await page.goBack()
    await expect(page).toHaveTitle('补充任务 · 标题核对 · Tuck')
    await page.goForward()
    await expect(page).toHaveTitle('查看交付 · 标题核对 · Tuck')
    const renamed=await fetch(`${app.url}/api/tasks/${task.id}/rename`,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({title:'新的名称'})})
    expect(renamed.ok).toBeTruthy()
    await expect(page).toHaveTitle('查看交付 · 新的名称 · Tuck')
    await page.reload()
    await expect(page).toHaveTitle('查看交付 · 新的名称 · Tuck')
    await page.goto(app.url+'/#/tasks')
    await expect(page).toHaveTitle('任务 · Tuck')
  } finally {await app.close()}
})
