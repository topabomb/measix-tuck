import {test,expect} from '@playwright/test'
import {settingsFixture} from '../../tests/fixtures/settings-fixture.mjs'
import {until} from '../../tests/fixtures/fixture.mjs'

test('optional default saves, task requires a choice, and removed models fail without substitution',async({page},info)=>{
 const f=await settingsFixture();try{
  await page.goto(f.url+'/#/settings/providers')
  const defaults=page.getByRole('combobox',{name:'默认模型（可选）',exact:true})
  await defaults.selectOption('')
  await page.getByRole('button',{name:'保存并生效',exact:true}).click()
  await expect(page.getByRole('status')).toHaveText('已保存并生效')
  await page.reload();await expect(defaults).toHaveValue('')
  await page.goto(f.url+'/#/tasks')
  await page.getByRole('region',{name:'通用助手',exact:true}).getByRole('link',{name:'新建任务'}).click()
  const choice=page.getByRole('combobox',{name:'模型',exact:true}),submit=page.getByRole('button',{name:'提交任务',exact:true})
  await expect(choice).toBeVisible();await expect(submit).toBeDisabled()
  await page.getByLabel('标题',{exact:true}).fill('明确选择模型')
  await page.getByLabel('任务要求',{exact:true}).fill('Reply OK')
  await choice.selectOption('proof-model');await expect(submit).toBeEnabled()
  await page.screenshot({path:info.outputPath('task-explicit-model-wide.png')})
  await page.setViewportSize({width:390,height:844})
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
  await page.screenshot({path:info.outputPath('task-explicit-model-narrow.png')})
  await submit.click();await expect(page).toHaveURL(/#\/tasks$/)
  await until(()=>[...f.app.manager.tasks.values()].some((t:any)=>t.lastTurnReason==='completed'))
  const task=[...f.app.manager.tasks.values()].find((t:any)=>t.lastTurnReason==='completed')!
  expect(task.modelId).toBe('proof-model')
  await page.setViewportSize({width:1280,height:800})
  const enrolled=await fetch(f.url+'/api/connection/enroll',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({material:f.material})});expect(enrolled.ok).toBe(true)
  await page.goto(f.url+'/#/settings/providers')
  await defaults.selectOption(f.snapshot.models[1].modelId)
  await page.getByRole('button',{name:'保存并生效',exact:true}).click();await expect(page.getByRole('status')).toHaveText('已保存并生效')
  await page.reload();await expect(defaults).toHaveValue(f.snapshot.models[1].modelId)
  f.models.catalog.delete(f.snapshot.models[1].modelId)
  await page.goto(f.url+'/#/tasks')
  await page.reload()
  await page.getByRole('region',{name:'通用助手',exact:true}).getByRole('link',{name:'新建任务'}).click()
  await expect(choice).toHaveCount(0);await expect(page.getByRole('alert')).toContainText('已被移除或当前不可用')
  await expect(submit).toBeDisabled()
  await page.screenshot({path:info.outputPath('task-missing-default.png')})
  expect(task.modelId).toBe('proof-model')
 }finally{await f.close()}
})
