import {test,expect} from '@playwright/test'
import {platformFixture} from '../../tests/fixtures/platform-fixture.mjs'

test('chat defaults follow the chosen assistant while explicit selections and existing conversations remain stable',async({page},info)=>{
  const f=await platformFixture({native:true})
  try {
    const [a,b]=f.snapshot.models
    f.models.agentDefaults.agents.image.modelId=b.modelId
    const enrolled=await fetch(f.url+'/api/connection/enroll',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({material:f.material})})
    expect(enrolled.ok).toBeTruthy()
    await page.goto(f.url+'/#/chat')
    await page.getByTestId('new-session').click()
    const dialog=page.getByRole('dialog',{name:'新对话',exact:true})
    const select=dialog.getByTestId('new-task-model')
    await expect(select).toHaveValue('')
    await expect(select.locator('option:checked')).toHaveText('助手默认 · '+a.displayName)
    await dialog.getByLabel('新对话助手',{exact:true}).selectOption('image')
    await expect(select.locator('option:checked')).toHaveText('助手默认 · '+b.displayName)
    await page.screenshot({path:info.outputPath('assistant-default-model.png')})
    const request=page.waitForRequest(r=>r.method()==='POST'&&r.url().endsWith('/api/tasks'))
    await dialog.getByRole('button',{name:'创建',exact:true}).click()
    expect((await request).postDataJSON()).not.toHaveProperty('modelId')
    await expect(dialog).toHaveCount(0)
    await expect(page.getByTestId('task-model')).toHaveValue(b.modelId)
    const id=page.url().split('/').at(-1)!
    expect(f.app!.manager.get(id).modelId).toBe(b.modelId)
    await page.reload()
    await expect(page.getByTestId('task-model')).toHaveValue(b.modelId)
    await page.getByTestId('new-session').click()
    await select.selectOption(a.modelId)
    await dialog.getByLabel('新对话助手',{exact:true}).selectOption('root')
    await dialog.getByLabel('新对话助手',{exact:true}).selectOption('image')
    await expect(select).toHaveValue(a.modelId)
    await dialog.getByRole('button',{name:'创建',exact:true}).click()
    await expect(dialog).toHaveCount(0)
    await expect(page.getByTestId('task-model')).toHaveValue(a.modelId)
  } finally {await f.close()}
})

test('task attachment validation uses the assistant default and submission binds it without model controls',async({page},info)=>{
  const f=await platformFixture({native:true})
  try {
    const [a,b]=f.snapshot.models
    b.inputModalities=['TEXT']
    f.models.agentDefaults.agents.root.modelId=b.modelId
    const enrolled=await fetch(f.url+'/api/connection/enroll',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({material:f.material})})
    expect(enrolled.ok).toBeTruthy()
    await page.goto(f.url+'/#/tasks/assistants/root/new')
    await expect(page.getByRole('combobox')).toHaveCount(0)
    await page.getByLabel('标题',{exact:true}).fill('验证助手默认模型')
    await page.getByLabel('任务要求',{exact:true}).fill('简单回复')
    await page.locator('input[type=file]').setInputFiles({name:'test.png',mimeType:'image/png',buffer:Buffer.from('image-fixture')})
    await expect(page.getByRole('alert')).toContainText('此助手当前不支持图片附件')
    await expect(page.getByRole('button',{name:'提交任务',exact:true})).toBeDisabled()
    await page.screenshot({path:info.outputPath('default-model-attachment-capability.png')})
    await page.getByRole('button',{name:'移除 test.png',exact:true}).click()
    await page.getByRole('button',{name:'提交任务',exact:true}).click()
    await expect(page).toHaveURL(/#\/tasks$/)
    const task=[...f.app!.manager.tasks.values()].find(t=>t.title.includes('验证助手默认模型'))!
    expect(task.modelId).toBe(b.modelId)
    expect(task.modelId).not.toBe(a.modelId)
    await expect.poll(()=>task.lastTurnReason).toBe('completed')
    expect(f.requests.some(r=>r.path.endsWith('/responses'))).toBeTruthy()
  } finally {await f.close()}
})

test('invalid defaults and removed task models fail clearly and recover after explicit configuration',async({page},info)=>{
  const f=await platformFixture({native:true})
  try {
    const [a,b]=f.snapshot.models
    f.models.agentDefaults.agents.root.modelId='missing-default'
    const post=async(path:string,body:unknown={})=>{
      const response=await fetch(f.url+path,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)})
      expect(response.ok).toBeTruthy()
    }
    await post('/api/connection/enroll',{material:f.material})
    await page.goto(f.url+'/#/chat')
    await expect(page.getByRole('alert')).toContainText('agents.root.modelId')
    await page.getByRole('button',{name:'新对话',exact:true}).click()
    await expect(page.getByTestId('new-task-model')).toBeVisible()
    await page.getByRole('button',{name:'取消',exact:true}).click()
    await page.goto(f.url+'/#/tasks/assistants/root/new')
    const model=page.getByRole('combobox',{name:'模型',exact:true})
    await expect(model).toHaveCount(0)
    await expect(page.getByRole('alert')).toContainText('已被移除或当前不可用')
    await expect(page.getByRole('button',{name:'提交任务',exact:true})).toBeDisabled()
    f.models.agentDefaults.agents.root.modelId=b.modelId
    await page.reload()
    await expect(page.getByRole('alert')).toHaveCount(0)
    await page.getByLabel('标题',{exact:true}).fill('默认模型恢复')
    await page.getByLabel('任务要求',{exact:true}).fill('完成测试任务')
    await page.setViewportSize({width:390,height:780})
    await page.screenshot({path:info.outputPath('missing-default-mobile.png')})
    await page.getByRole('button',{name:'提交任务',exact:true}).click()
    await expect(page).toHaveURL(/#\/tasks$/)
    const task=[...f.app!.manager.tasks.values()].find(t=>t.title.includes('默认模型恢复'))!
    await expect.poll(()=>task.lastTurnReason).toBe('completed')
    expect(task.modelId).toBe(b.modelId)
    expect(task.reasoningEffort).toBe('default')
    const nativeId=task.nativeSessionId
    await page.goto(f.url+`/#/tasks/${task.id}/supplement`)
    await expect(model).toHaveCount(0)
    b.enabled=false
    f.snapshot.managedGeneration++
    f.controls.state.activeManagedGeneration=f.snapshot.managedGeneration
    await post('/api/connection/sync')
    await page.reload()
    await expect(model).toHaveCount(0)
    await expect(page.getByRole('alert')).toContainText('已被移除或当前不可用')
    await expect(page.getByRole('button',{name:'提交补充',exact:true})).toBeDisabled()
    await page.goto(f.url+`/#/chat/conversations/${task.id}`)
    await page.getByRole('button',{name:'对话设置',exact:true}).click()
    await page.getByTestId('task-model').selectOption(a.modelId)
    await expect.poll(()=>task.modelId).toBe(a.modelId)
    await page.keyboard.press('Escape')
    await page.goto(f.url+`/#/tasks/${task.id}/supplement`)
    await page.getByLabel('补充要求',{exact:true}).fill('继续原任务')
    await page.screenshot({path:info.outputPath('unavailable-model-recovery-mobile.png')})
    await page.getByRole('button',{name:'提交补充',exact:true}).click()
    await expect(page).toHaveURL(/#\/tasks$/)
    await expect.poll(()=>task.turnCount).toBe(2)
    await expect.poll(()=>task.lastTurnReason).toBe('completed')
    expect(task.modelId).toBe(a.modelId)
    expect(task.nativeSessionId).toBe(nativeId)
  } finally {await f.close()}
})
