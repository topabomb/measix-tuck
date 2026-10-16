import { expect, test } from '@playwright/test'
import { fixture, until } from '../../tests/fixtures/fixture.mjs'

test('native user_prompt renders typed fields, validates, survives reload and submits one structured answer',async({page})=>{
  const app=await fixture()
  try {
    const task=await app.manager.create({title:'结构化问题验收'})
    app.manager.submit(task.id,'SCHEMA_QUESTION')
    await until(()=>task.pendingInteraction)
    await page.goto(app.url)
    await expect(page.getByTestId('pending-interaction')).toContainText('生成设置')
    await expect(page.getByTestId('pending-interaction')).not.toContainText('"properties"')
    await page.reload()
    await page.getByTestId('approve-interaction').click()
    expect(task.pendingInteraction).toBeTruthy()
    for(const width of [1365,390]) {
      await page.setViewportSize({width,height:740})
      await expect(page.getByTestId('approve-interaction')).toBeInViewport()
      await expect(page.getByTestId('composer-input')).toBeInViewport()
      expect(await page.evaluate(()=>document.body.scrollWidth-innerWidth)).toBeLessThanOrEqual(1)
    }
    await page.getByLabel('风格',{exact:true}).selectOption({label:'详细'})
    await page.getByLabel('是否附带说明',{exact:true}).selectOption({label:'否'})
    await page.getByLabel('数量',{exact:true}).fill('3')
    await page.route('**/api/tasks/*/interaction',route=>route.fulfill({status:503,contentType:'application/json',body:'{"error":"验收临时错误"}'}))
    await page.getByTestId('approve-interaction').click()
    await expect(page.getByLabel('风格',{exact:true})).toHaveValue('详细')
    await page.unroute('**/api/tasks/*/interaction')
    await page.getByTestId('approve-interaction').click()
    await until(()=>!app.manager.runs.has(task.id))
    await expect(page.getByTestId('pending-interaction')).toHaveCount(0)
    const history=await app.docker.request(`/api/sessions/${task.nativeSessionId}`)
    const result=history.messages.map((entry:any)=>entry.message).find((msg:any)=>msg?.role==='tool' && msg.content.includes('"action"'))
    expect(JSON.parse(result!.content!)).toEqual({action:'accept',content:{style:'详细',confirm:false,count:3}})
    app.manager.submit(task.id,'SCHEMA_QUESTION 跳过')
    await expect(page.getByTestId('pending-interaction')).toBeVisible()
    await page.getByTestId('deny-interaction').click()
    await until(()=>!app.manager.runs.has(task.id))
    await expect(page.getByTestId('pending-interaction')).toHaveCount(0)
    const skipped=await app.docker.request(`/api/sessions/${task.nativeSessionId}`)
    expect(skipped.messages.some((entry:any)=>entry.message?.role==='tool' && entry.message.content.includes('"action":"decline"'))).toBe(true)
    app.manager.submit(task.id,'SCHEMA_QUESTION 旧版本兼容')
    await until(()=>task.pendingInteraction)
    const question=task.pendingInteraction!
    if(question.kind==='question') { question.title=question.detail; question.detail=JSON.stringify(question.schema); delete question.schema }
    app.store.saveTask(task)
    await page.reload()
    await expect(page.getByTestId('pending-interaction')).toContainText('需要你回答')
    await expect(page.getByLabel('风格',{exact:true})).toBeVisible()
    await expect(page.getByTestId('pending-interaction')).not.toContainText('"properties"')
    await page.getByTestId('deny-interaction').click()
    await until(()=>!app.manager.runs.has(task.id))
  } finally { await app.close() }
})
