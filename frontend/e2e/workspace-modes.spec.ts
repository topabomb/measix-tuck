import {test,expect} from '@playwright/test'
import {resolve} from 'node:path'
import {settingsFixture} from '../../tests/fixtures/settings-fixture.mjs'

test('workspace settings save, channel defaults, and new task drafts remain independent',async({page})=>{
 const f=await settingsFixture()
 try{
  await page.goto(f.url+'/#/settings/agents/root')
  const settings=page.getByRole('region',{name:'工作目录配置',exact:true})
  await expect(settings.getByRole('checkbox')).toHaveCount(0)
  await expect(settings.getByRole('textbox',{name:'指定目录',exact:true})).toHaveCount(0)
  await settings.getByRole('combobox',{name:'工作目录模式',exact:true}).selectOption('specified')
  await expect(settings.getByRole('textbox',{name:'指定目录',exact:true})).toBeVisible()
  await settings.getByRole('combobox',{name:'工作目录模式',exact:true}).selectOption('none')
  await page.getByRole('link',{name:'返回 Agent 列表',exact:true}).click()
  await expect(page.getByRole('combobox',{name:'任务频道默认 Agent',exact:true})).toHaveCount(0)
  await page.getByRole('button',{name:'保存并生效',exact:true}).click()
  await expect(page.getByRole('status')).toHaveText('已保存并生效')
  await page.reload();await page.getByRole('link',{name:'编辑 root',exact:true}).click();await expect(settings.getByRole('combobox',{name:'工作目录模式',exact:true})).toHaveValue('none')
  await page.goto(f.url+'/#/tasks')
  await expect(page.getByRole('link',{name:'使用默认 Agent 新建任务',exact:true})).toHaveCount(0)
  await page.getByRole('region',{name:'视频创作',exact:true}).getByRole('link',{name:'新建任务',exact:true}).click()
  await expect(page).toHaveURL(/#\/tasks\/assistants\/video\/new$/)
  await expect(page.getByRole('combobox',{name:'工作目录模式',exact:true})).toHaveCount(0)
  await page.getByRole('textbox',{name:'标题',exact:true}).fill('目录模式验收')
  await page.getByRole('textbox',{name:'任务要求',exact:true}).fill('Verify the native default directory.')
  await page.getByRole('button',{name:'提交任务',exact:true}).click()
  await expect(page).toHaveURL(/#\/tasks$/)
  expect([...f.app.manager.tasks.values()].find((t:any)=>t.title.includes('目录模式验收'))).toMatchObject({workspaceMode:'auto'})
  await page.getByRole('region',{name:'视频创作',exact:true}).getByRole('link',{name:'新建任务',exact:true}).click()
  await expect(page.getByRole('combobox',{name:'工作目录模式',exact:true})).toHaveCount(0)
  await page.getByRole('navigation',{name:'工作模式'}).getByRole('link',{name:'对话',exact:true}).click()
  await page.getByRole('button',{name:'新对话',exact:true}).click()
  await expect(page.getByRole('combobox',{name:'工作目录模式',exact:true})).toHaveValue('none')
  await page.setViewportSize({width:390,height:780})
  expect(await page.getByRole('dialog',{name:'新对话',exact:true}).evaluate(el=>el.scrollWidth<=el.clientWidth)).toBe(true)
 }finally{await f.close()}
})

for(const mode of ['none','specified'] as const)test(`task channel uses Agent ${mode} directory without editable choices`,async({page})=>{
 const f=await settingsFixture()
 try{
  const data=await f.read(),directory=resolve(f.directory,'workspaces')
  data.config.workbench.assistants.find((a:any)=>a.agent==='root').workspace={mode,...(mode==='specified'?{directory}:{})}
  const response=await f.save(data);expect(response.ok,await response.clone().text()).toBe(true)
  await page.goto(f.url+'/#/tasks/assistants/root/new')
  await expect(page.getByRole('combobox',{name:'工作目录模式'})).toHaveCount(0)
  await expect(page.getByRole('textbox',{name:'指定目录'})).toHaveCount(0)
  if(mode==='specified')await expect(page.getByLabel('指定工作目录')).toHaveText(directory)
  else await expect(page.getByLabel('指定工作目录')).toHaveCount(0)
  await page.getByRole('textbox',{name:'标题',exact:true}).fill('固定目录验收')
  await page.getByRole('textbox',{name:'任务要求',exact:true}).fill('Reply OK.')
  await page.getByRole('button',{name:'提交任务',exact:true}).click();await expect(page).toHaveURL(/#\/tasks$/)
  const task=[...f.app.manager.tasks.values()].find((t:any)=>t.title.includes('固定目录验收')) as any
  expect(task.workspaceMode).toBe(mode);if(mode==='specified')expect(task.workspace).toBe(directory)
 }finally{await f.close()}
})


test('a retained task draft displays its original directory after Agent settings change',async({page})=>{
 const f=await settingsFixture();try{
  const directory=resolve(f.directory,'workspaces'),data=await f.read()
  data.config.workbench.assistants.find((a:any)=>a.agent==='root').workspace={mode:'specified',directory}
  expect((await f.save(data)).ok).toBe(true)
  await page.goto(f.url+'/#/tasks/assistants/root/new')
  await page.route('**/api/tasks/*/prompts',route=>route.fulfill({status:500,json:{error:'synthetic submission interruption'}}),{times:1})
  await page.getByRole('textbox',{name:'标题',exact:true}).fill('原目录重试')
  await page.getByRole('textbox',{name:'任务要求',exact:true}).fill('Reply OK.')
  await page.getByRole('button',{name:'提交任务',exact:true}).click()
  await expect(page.getByRole('alert')).toContainText('提交结果尚未确认')
  const updated=await f.read();updated.config.workbench.assistants.find((a:any)=>a.agent==='root').workspace={mode:'auto'}
  expect((await f.save(updated)).ok).toBe(true)
  await page.getByRole('navigation',{name:'任务路径'}).getByRole('link',{name:'任务',exact:true}).click()
  await page.getByRole('region',{name:'通用助手',exact:true}).getByRole('link',{name:'新建任务',exact:true}).click()
  await expect(page.getByLabel('指定工作目录')).toHaveText(directory)
  expect([...f.app.manager.tasks.values()].find((t:any)=>t.title.includes('原目录重试')).workspace).toBe(directory)
 }finally{await f.close()}
})
