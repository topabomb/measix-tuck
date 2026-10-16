import { test, expect } from '@playwright/test'
import { settingsFixture } from '../../tests/fixtures/settings-fixture.mjs'
import { until } from '../../tests/fixtures/fixture.mjs'
import { openSettings, closeSettings } from './settings'

test('agent execution limits save, survive refresh and remain scoped to the selected agent',async({page},info)=>{
 const f=await settingsFixture();try{
  await page.goto(f.url+'/#/settings/agents/root')
  const region=page.getByRole('region',{name:'运行配置',exact:true})
  const steps=region.getByLabel('最大执行步数',{exact:true}),repeats=region.getByLabel('重复调用阈值',{exact:true})
  await expect(repeats).toHaveValue('5');await expect(steps).toHaveAttribute('min','0')
  await expect(region).toContainText('0 也使用默认值，不能关闭保护')
  await steps.fill('0');await repeats.fill('8')
  await region.getByRole('button',{name:'保存并生效',exact:true}).click();await expect(region.getByRole('status')).toHaveText('已保存并生效')
  const saved=await f.read();expect(saved.config.agents.root.max_iterations).toBe(0);expect(saved.config.agents.root.max_consecutive_tool_calls).toBe(8);expect(saved.config.agents.worker.max_consecutive_tool_calls).toBeUndefined()
  await page.reload();await expect(steps).toHaveValue('0');await expect(repeats).toHaveValue('8')
  for(const width of [1280,390]){
   await page.setViewportSize({width,height:800});expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
   await page.screenshot({path:info.outputPath(`agent-limits-${width}.png`),fullPage:true})
  }
  await repeats.fill('0');await region.getByRole('button',{name:'保存并生效',exact:true}).click();await expect(region.getByRole('status')).toHaveText('已保存并生效')
  await page.reload();await expect(repeats).toHaveValue('0')
  await page.goto(f.url+'/#/settings/agents/worker');await expect(repeats).toHaveValue('5');await expect(steps).not.toHaveValue('0')
 }finally{await f.close()}
})

test('runtime settings save with active-task confirmation and real native calls',async({page},info)=>{
 const f=await settingsFixture()
 try{
  const task=await f.app.manager.create({title:'配置界面验收',approvalMode:'auto'})
  await page.goto(`${f.url}/#/chat/conversations/${task.id}`)
  await openSettings(page);await page.getByRole('navigation',{name:'设置分类'}).getByRole('link',{name:'提供商与模型',exact:true}).click()
  const dialog=page.getByRole('region',{name:'运行配置',exact:true}); const navigation=page.getByRole('navigation',{name:'设置分类'})
  await dialog.getByRole('link',{name:'编辑提供商 proof',exact:true}).click()
  await expect(dialog.getByLabel('API Key',{exact:true})).toHaveValue('')
  await expect(dialog.getByLabel('提供商标识',{exact:true})).toHaveAttribute('readonly','')
  await dialog.getByLabel('API Key',{exact:true}).fill('typed-then-cleared');await dialog.getByLabel('API Key',{exact:true}).fill('')
  await dialog.getByRole('button',{name:'添加模型',exact:true}).click()
  await expect(dialog.getByLabel('显示名称',{exact:true})).toBeVisible();await dialog.getByRole('button',{name:'删除模型',exact:true}).click()
  await dialog.getByRole('link',{name:'编辑模型 proof-model',exact:true}).click()
  await dialog.getByLabel('上下文窗口',{exact:true}).fill('96000')
  await dialog.getByLabel('最大输出',{exact:true}).fill('4096')
  await dialog.getByLabel('Temperature（可选）',{exact:true}).fill('');await dialog.getByLabel('Temperature（可选）',{exact:true}).pressSequentially('0.35');await expect(dialog.getByLabel('Temperature（可选）',{exact:true})).toHaveValue('0.35')
  await page.screenshot({path:info.outputPath('runtime-models-wide.png')})
  await navigation.getByRole('link',{name:'Agent',exact:true}).click(); await dialog.getByRole('link',{name:'编辑 root',exact:true}).click(); await expect(page).toHaveURL(/#\/settings\/agents\/root$/)
  await dialog.getByLabel('指令',{exact:true}).fill('UI_SETTINGS_VERIFIED. Use the MCP proof_echo tool and report its result.')
  await dialog.getByRole('link',{name:'Docker Agent 原生工具',exact:true}).click()
  await expect(dialog.getByRole('checkbox',{name:'启用 filesystem / read_file',exact:true})).toBeChecked();await dialog.getByLabel('filesystem-0 允许访问的目录',{exact:true}).fill('.');await page.getByRole('link',{name:'返回 Agent 配置',exact:true}).click()
  await page.screenshot({path:info.outputPath('runtime-agents-wide.png')})
  await navigation.getByRole('link',{name:'MCP',exact:true}).click();await dialog.getByRole('button',{name:'添加 MCP',exact:true}).click()
  await dialog.getByLabel('连接标识',{exact:true}).fill('proof')
  await dialog.getByLabel('连接标识',{exact:true}).press('Tab')
  await dialog.getByLabel('MCP 地址',{exact:true}).fill(f.origin+'/mcp')
  await dialog.getByLabel('Bearer Token',{exact:true}).fill('synthetic-ui-key')
  await dialog.getByRole('button',{name:'读取工具列表',exact:true}).first().click();await expect(dialog.getByRole('checkbox',{name:'启用 echo',exact:true})).toBeChecked();await dialog.getByRole('checkbox',{name:'使用全部工具',exact:true}).uncheck()
  await page.setViewportSize({width:390,height:780});await page.screenshot({path:info.outputPath('runtime-mcp-narrow.png')})
  expect(await dialog.evaluate(el=>el.scrollWidth<=el.clientWidth)).toBe(true)
  await page.setViewportSize({width:1280,height:800});await navigation.getByRole('link',{name:'Agent',exact:true}).click();await dialog.getByRole('link',{name:'编辑 root',exact:true}).click()
  await expect(dialog.getByLabel('指令',{exact:true})).toHaveValue(/UI_SETTINGS_VERIFIED/);const binding=dialog.locator('.mcp-bindings input');await expect(binding).toBeChecked();await binding.uncheck();await expect(binding).not.toBeChecked();await binding.check();await navigation.getByRole('link',{name:'MCP',exact:true}).click();await dialog.getByRole('link',{name:'编辑 MCP proof',exact:true}).click();await expect(dialog.getByLabel('root',{exact:true})).toBeChecked();await navigation.getByRole('link',{name:'Agent',exact:true}).click()
  f.controls.slow=true;f.app.manager.submit(task.id,'Wait for settings verification');await until(()=>f.requests.some((r:any)=>r.path==='/v1/chat/completions'))
  const revision=(await f.read()).revision
  await dialog.getByRole('button',{name:'保存并生效',exact:true}).click();await expect(dialog).toContainText('不会自动恢复');await page.screenshot({path:info.outputPath('runtime-save-choice.png')})
  await dialog.getByRole('button',{name:'保存，暂不生效',exact:true}).click();await expect(dialog.getByRole('status')).toContainText('已保存，尚未生效');expect((await f.read()).revision).not.toBe(revision);expect(f.app.manager.runs.has(task.id)).toBe(true);expect((await f.read()).config.providers[0].hasApiKey).toBe(true);await page.screenshot({path:info.outputPath('runtime-saved-pending.png')})
  await dialog.getByRole('button',{name:'保存并生效',exact:true}).click();f.controls.slow=false
  await dialog.getByRole('button',{name:'中断任务并保存',exact:true}).click();await expect(dialog.getByRole('status')).toHaveText('已保存并生效')
  expect(task.lastTurnReason).toBe('interrupted');expect(task.turnCount).toBe(1);await dialog.getByRole('link',{name:'编辑 root',exact:true}).click();await dialog.getByLabel('说明',{exact:true}).fill('edited after save');await expect(dialog.getByRole('status')).toHaveCount(0)
  await dialog.getByLabel('说明',{exact:true}).fill((await f.read()).config.agents.root.description); await closeSettings(page)
  f.app.manager.submit(task.id,'Verify new settings');await until(()=>!f.app.manager.runs.has(task.id))
  const call=f.requests.findLast((r:any)=>r.path==='/v1/chat/completions');expect(call.body.max_completion_tokens || call.body.max_tokens).toBe(4096);expect(call.body.temperature).toBe(.35);expect(JSON.stringify(call.body.messages)).toContain('UI_SETTINGS_VERIFIED');expect(f.requests.some((r:any)=>r.body?.method==='tools/call')).toBe(true)
  await page.setViewportSize({width:1280,height:800});await page.reload();await openSettings(page);await navigation.getByRole('link',{name:'MCP',exact:true}).click();await dialog.getByRole('link',{name:'编辑 MCP proof',exact:true}).click();await expect(dialog.getByLabel('Bearer Token',{exact:true})).toHaveValue('');await expect(dialog.getByLabel('Bearer Token',{exact:true})).toHaveAttribute('placeholder',/已设置/);await page.reload();await expect(page).toHaveURL(/#\/settings\/mcp\/proof$/);await expect(dialog.getByLabel('连接标识',{exact:true})).toHaveValue('proof');await navigation.getByRole('link',{name:'Agent',exact:true}).click(); await dialog.getByRole('link',{name:'编辑 root',exact:true}).click();await page.getByRole('link',{name:'返回 Agent 列表',exact:true}).click();await dialog.getByRole('link',{name:'编辑 worker',exact:true}).click();await expect(page).toHaveURL(/#\/settings\/agents\/worker$/);await page.reload();await expect(page.getByRole('link',{name:'返回 Agent 列表',exact:true})).toBeVisible();await expect(dialog.getByLabel('说明',{exact:true})).toHaveValue((await f.read()).config.agents.worker.description)
 }finally{await f.close()}
})
