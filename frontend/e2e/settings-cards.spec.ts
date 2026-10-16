import {test,expect} from '@playwright/test'
import {settingsFixture} from '../../tests/fixtures/settings-fixture.mjs'

test('MCP tool selection can disable the last tool and restore all tools for local and platform connections',async({page})=>{
 const f=await settingsFixture();try{
  let data=await f.read();data.config.localMcp=[{name:'proof_mcp',url:f.origin+'/mcp',enabled:true,agents:['root']}];expect((await f.save(data)).ok).toBe(true)
  expect((await fetch(f.url+'/api/connection/enroll',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({material:f.material})})).ok).toBe(true)
  data=await f.read();const managed=data.managedMcp[0]
  for(const item of [{path:'/settings/mcp/proof_mcp',tool:'echo',platform:false},{path:'/settings/mcp/platform/'+managed.mcpServerId,tool:'search_records',platform:true}]){
   await page.goto(f.url+'/#'+item.path)
   await expect(page.getByText('环境变量认证',{exact:true})).toHaveCount(0);await expect(page.getByRole('textbox',{name:'Token 环境变量（可选）',exact:true})).toHaveCount(0)
   await page.getByRole('button',{name:'读取工具列表',exact:true}).click()
   const all=page.getByRole('checkbox',{name:'使用全部工具',exact:true}),choice=page.getByRole('checkbox',{name:'启用 '+item.tool,exact:true})
   await expect(all).toBeChecked();await all.uncheck();await expect(choice).toBeEnabled();await choice.uncheck()
   await expect(page.getByText('此连接的工具已全部停用；保存后生效。',{exact:true})).toBeVisible()
   await page.getByRole('link',{name:'返回 MCP 列表',exact:true}).click()
   await page.getByRole('link',{name:item.platform?`查看平台 MCP ${managed.displayName}`:'编辑 MCP proof_mcp',exact:true}).click()
   await expect(all).not.toBeChecked();await page.getByRole('button',{name:'保存并生效',exact:true}).click();await expect(page.getByRole('status')).toHaveText('已保存并生效')
   await page.reload();await expect(all).not.toBeChecked();data=await f.read();expect(item.platform?data.config.platformMcpTools[managed.settingsKey]:data.config.localMcp[0].tools).toEqual([])
   await page.getByRole('button',{name:'读取工具列表',exact:true}).click();await expect(choice).not.toBeChecked();await expect(choice).toBeEnabled();await expect(page.getByRole('checkbox',{name:'启用 not_granted',exact:true})).toHaveCount(0)
   await all.check();await page.getByRole('button',{name:'保存并生效',exact:true}).click();await expect(page.getByRole('status')).toHaveText('已保存并生效');await page.reload();await expect(all).toBeChecked()
   data=await f.read();expect(item.platform?data.config.platformMcpTools[managed.settingsKey]:data.config.localMcp[0].tools).toBeUndefined()
  }
 }finally{await f.close()}
})

test('platform model local parameters share drafts and persistence without editing platform identity',async({page})=>{
 const f=await settingsFixture();try{
  expect((await fetch(f.url+'/api/connection/enroll',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({material:f.material})})).ok).toBe(true)
  const provider=f.snapshot.providers[0],model=f.snapshot.models[0],path=`/settings/providers/platform/${provider.providerId}/models/${model.modelId}`
  const before=await f.read(),key=before.managedModels.find((m:any)=>m.modelId===model.modelId).settingsKey
  await page.goto(f.url+'/#'+path)
  await expect(page.getByRole('checkbox',{name:'启用模型',exact:true})).toHaveCount(0);await expect(page.getByRole('textbox',{name:'显示名称',exact:true})).toHaveCount(0)
  await expect(page.getByRole('button',{name:'删除模型',exact:true})).toHaveCount(0);await expect(page.getByRole('button',{name:'验证连接',exact:true})).toHaveCount(0)
  await page.getByLabel('上下文窗口',{exact:true}).fill('96000');await page.getByLabel('最大输出',{exact:true}).fill('4096');await page.getByLabel('Temperature（可选）',{exact:true}).fill('0.35');await page.getByLabel('Top P（可选）',{exact:true}).fill('0.8')
  await page.getByRole('link',{name:'返回提供商',exact:true}).click();await page.getByRole('link',{name:`查看平台模型 ${model.displayName}`,exact:true}).click();await expect(page.getByLabel('上下文窗口',{exact:true})).toHaveValue('96000')
  await page.getByRole('button',{name:'保存并生效',exact:true}).click();await expect(page.getByRole('status')).toHaveText('已保存并生效');await page.reload()
  await expect(page.getByLabel('上下文窗口',{exact:true})).toHaveValue('96000');await expect(page.getByLabel('最大输出',{exact:true})).toHaveValue('4096');await expect(page.getByLabel('Temperature（可选）',{exact:true})).toHaveValue('0.35')
  expect((await f.read()).config.platformModelSettings[key]).toEqual({contextWindow:96000,maxOutputTokens:4096,temperature:.35,topP:.8})
  await page.getByLabel('最大输出',{exact:true}).fill('96000');await page.getByRole('button',{name:'保存并生效',exact:true}).click();await expect(page.getByRole('alert')).toBeVisible()
  await page.getByRole('button',{name:'恢复客户端默认',exact:true}).click();await expect(page.getByLabel('上下文窗口',{exact:true})).toHaveValue(String(before.config.modelDefaults.contextWindow));await expect(page.getByLabel('Temperature（可选）',{exact:true})).toHaveValue('')
  await page.getByRole('button',{name:'保存并生效',exact:true}).click();await expect(page.getByRole('status')).toHaveText('已保存并生效');await page.reload();expect((await f.read()).config.platformModelSettings[key]).toBeUndefined()
  await page.route('**/api/settings/runtime*',async route=>{const response=await route.fetch(),data=await response.json(),managed=data.managedModels.find((m:any)=>m.modelId===model.modelId);managed.platformDefinedFields=['contextWindow'];managed.contextWindow=48000;await route.fulfill({response,json:data})})
  await page.reload();await expect(page.getByLabel('上下文窗口',{exact:true})).toHaveAttribute('readonly','');await expect(page.getByLabel('上下文窗口',{exact:true})).toHaveValue('48000');await expect(page.getByLabel('最大输出',{exact:true})).toBeEditable()
 }finally{await f.close()}
})

test('platform MCP detail retains loading errors and retry with locally editable tool selection',async({page})=>{
 const f=await settingsFixture();try{
  expect((await fetch(f.url+'/api/connection/enroll',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({material:f.material})})).ok).toBe(true);
  await page.route('**/api/settings/runtime*',route=>route.fulfill({status:503,contentType:'application/json',body:JSON.stringify({error:'配置暂不可用'})}),{times:1});
  await page.goto(f.url+'/#/settings/mcp/platform/'+f.snapshot.mcp[0].mcpServerId);
  await expect(page.getByRole('alert')).toHaveText('配置暂不可用');await page.getByRole('button',{name:'重试',exact:true}).click();
  await expect(page.getByText('平台下发',{exact:true})).toBeVisible();await expect(page.getByRole('alert')).toHaveCount(0);await expect(page.getByRole('button',{name:'保存并生效',exact:true})).toBeVisible();await expect(page.getByRole('textbox',{name:'MCP 地址',exact:true})).toHaveCount(0);
 }finally{await f.close()}
})

test('platform and custom catalogs have distinct routes and Agent MCP choices persist',async({page})=>{
 const f=await settingsFixture();try{
  let data=await f.read();data.config.localMcp=[{name:'proof_mcp',displayName:'自定义记录',url:f.origin+'/mcp',enabled:true,agents:['root']}];expect((await f.save(data)).ok).toBe(true);
  expect((await fetch(f.url+'/api/connection/enroll',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({material:f.material})})).ok).toBe(true);
  const provider=f.snapshot.providers[0],model=f.snapshot.models[0],mcp=f.snapshot.mcp[0];
  await page.goto(f.url+'/#/settings/providers');
  await expect(page.getByRole('link',{name:`查看平台提供商 ${provider.displayName}`,exact:true})).toContainText('平台下发 · 只读');
  await expect(page.getByRole('link',{name:'编辑提供商 proof',exact:true})).toContainText('用户自定义');
  await page.getByRole('link',{name:`查看平台提供商 ${provider.displayName}`,exact:true}).click();
  await expect(page.getByRole('textbox')).toHaveCount(0);
  await page.getByRole('link',{name:`查看平台模型 ${model.displayName}`,exact:true}).click();
  await expect(page).toHaveURL(new RegExp(`/providers/platform/${provider.providerId}/models/${model.modelId}$`));
  await page.reload();await expect(page.getByRole('heading',{name:'本地运行参数',exact:true})).toBeVisible();
  await page.getByRole('link',{name:'返回提供商',exact:true}).click();await page.getByRole('link',{name:'返回提供商列表',exact:true}).click();
  await page.getByRole('navigation',{name:'设置分类'}).getByRole('link',{name:'MCP',exact:true}).click();
  await expect(page.getByRole('link',{name:`查看平台 MCP ${mcp.displayName}`,exact:true})).toContainText('平台下发');
  await expect(page.getByRole('link',{name:'编辑 MCP proof_mcp',exact:true})).toContainText('用户自定义');
  await page.getByRole('link',{name:`查看平台 MCP ${mcp.displayName}`,exact:true}).click();await expect(page.getByRole('textbox')).toHaveCount(0);
  await page.getByRole('link',{name:'root 已启用',exact:true}).click();
  const choice=page.getByRole('checkbox',{name:`启用平台 MCP ${mcp.displayName}`,exact:true});await expect(choice).toBeChecked();
  await expect(page.getByRole('checkbox',{name:'启用自定义 MCP 自定义记录',exact:true})).toBeChecked();
  await choice.uncheck();await page.getByRole('button',{name:'保存并生效',exact:true}).click();await expect(page.getByRole('status')).toHaveText('已保存并生效');
  await page.reload();await expect(choice).not.toBeChecked();data=await f.read();expect(data.config.agents.root.disabledEnterpriseMcp).toEqual([mcp.mcpServerId]);
  await choice.check();await page.getByRole('button',{name:'保存并生效',exact:true}).click();await expect(page.getByRole('status')).toHaveText('已保存并生效');
  const paths=['/settings/providers','/settings/providers/platform/'+provider.providerId+'/models/'+model.modelId,'/settings/mcp','/settings/mcp/platform/'+mcp.mcpServerId,'/settings/agents/root'];
  for(const width of [390,1600])for(const path of paths){
   await page.setViewportSize({width,height:800});await page.goto(f.url+'/#'+path);await expect(page.getByRole('region',{name:'运行配置',exact:true})).toHaveAttribute('aria-busy','false');
   expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
  }
 }finally{await f.close()}
})

test('combined model choices identify their source and require an explicit selection after source changes',async({page})=>{
 const f=await settingsFixture();try{
  const task=await f.app.manager.create({approvalMode:'auto'});
  const r=await fetch(f.url+'/api/connection/enroll',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({material:f.material})});expect(r.ok).toBe(true);
  await page.goto(f.url+'/#/chat/conversations/'+task.id);const select=page.getByRole('combobox',{name:'模型',exact:true});await expect(select).toHaveValue('proof-model');await expect(select.getByRole('option')).toHaveCount(4);await expect(select.getByRole('option',{name:'验证模型 · 本地',exact:true})).toBeEnabled();expect((await select.getByRole('option').allTextContents()).filter(t=>t.endsWith(' · 平台'))).toHaveLength(2);
  // A persisted selection can outlive its original source. Its same ID must be selectable again explicitly.
  task.modelSource='platform';f.app.store.saveTask(task);await page.reload();await expect(select).toHaveValue('');await select.selectOption('proof-model');await expect.poll(()=>task.modelSource).toBe('local');
 }finally{await f.close()}
})

test('settings returns through its own hierarchy and themed selects remain usable at narrow widths',async({page})=>{
 const f=await settingsFixture();try{
  await page.goto(f.url+'/#/tasks');await page.getByTestId('mode-settings').click();await page.getByRole('navigation',{name:'设置快捷菜单'}).getByRole('link',{name:'设置',exact:true}).click();await expect(page).toHaveURL(/#\/settings$/);
  await page.getByRole('navigation',{name:'设置首页'}).getByRole('link',{name:'外观',exact:true}).click();await page.getByRole('button',{name:'炭棕',exact:true}).click();
  await page.getByRole('link',{name:'返回设置',exact:true}).click();await page.getByRole('link',{name:'返回任务',exact:true}).click();await expect(page).toHaveURL(/#\/tasks$/);
  const header=await page.locator('.tasks-home-header h1').boundingBox(),list=await page.locator('.task-overview').boundingBox();expect(Math.abs(header!.x-list!.x)).toBeLessThan(2);
  await page.getByRole('region',{name:'通用助手',exact:true}).getByRole('link',{name:'新建任务',exact:true}).click();
  await expect(page.getByRole('combobox',{name:'工作目录模式',exact:true})).toHaveCount(0)
  await page.goto(f.url+'/#/settings/agents/root')
  for(const width of [1600,390]){
   await page.setViewportSize({width,height:800});const select=page.getByRole('combobox',{name:'工作目录模式',exact:true});await select.click();
   const option=page.getByRole('option',{name:'自动目录',exact:true});await expect(option).toBeVisible();
   expect(await option.evaluate(e=>getComputedStyle(e).backgroundColor)).toBe('rgb(218, 180, 134)');
   await page.getByRole('option',{name:'指定目录',exact:true}).click();await expect(page.getByRole('textbox',{name:'指定目录',exact:true})).toBeVisible();
   await select.selectOption('auto');expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
  }
  await page.getByRole('button',{name:'保存并生效',exact:true}).click();await expect(page.getByRole('status')).toHaveText('已保存并生效');await page.reload();await page.getByRole('link',{name:'返回 Agent 列表',exact:true}).click();await expect(page).toHaveURL(/#\/settings\/agents$/);
 }finally{await f.close()}
})

test('settings cards preserve detail routes, readable hover, model capability recovery and MCP state',async({page})=>{
 const f=await settingsFixture()
 try{
  const data=await f.read();data.config.providers[0].models[0].capabilities.push('REASONING');data.config.providers[0].models[0].reasoningEfforts=['high','xhigh'];expect((await f.save(data)).ok).toBe(true)
  const task=await f.app.manager.create({approvalMode:'auto'});f.app.manager.selectModel(task.id,'proof-model','high')
  await page.goto(f.url+'/#/settings/providers')
  const add=page.getByRole('button',{name:'添加提供商',exact:true}),select=page.getByRole('combobox',{name:'默认模型（可选）',exact:true})
  const buttonBox=await add.boundingBox(),selectBox=await select.boundingBox();expect(Math.abs(buttonBox!.y+buttonBox!.height-selectBox!.y-selectBox!.height)).toBeLessThan(2)
  const colors=await add.evaluate(e=>({background:getComputedStyle(e).backgroundColor,color:getComputedStyle(e).color}));await add.hover();expect(await add.evaluate(e=>({background:getComputedStyle(e).backgroundColor,color:getComputedStyle(e).color}))).toEqual(colors)
  await page.getByRole('link',{name:'编辑提供商 proof',exact:true}).click();await page.getByRole('link',{name:'编辑模型 proof-model',exact:true}).click()
  await expect(page).toHaveURL(/#\/settings\/providers\/proof\/models\/proof-model$/);await page.reload();await expect(page.getByRole('textbox',{name:'模型标识',exact:true})).toHaveAttribute('readonly','')
  await expect(page.getByRole('checkbox',{name:'更高（xhigh）',exact:true})).toBeChecked()
  const context=await page.getByRole('spinbutton',{name:'上下文窗口',exact:true}).inputValue(),output=await page.getByRole('spinbutton',{name:'最大输出',exact:true}).inputValue()
  await page.getByRole('spinbutton',{name:'上下文窗口',exact:true}).fill('');await page.getByRole('spinbutton',{name:'最大输出',exact:true}).fill('')
  await page.getByRole('link',{name:'返回提供商',exact:true}).click();await expect(page.getByRole('link',{name:'编辑模型 proof-model',exact:true})).toContainText('未填写')
  await page.getByRole('link',{name:'编辑模型 proof-model',exact:true}).click();await page.getByRole('spinbutton',{name:'上下文窗口',exact:true}).fill(context);await page.getByRole('spinbutton',{name:'最大输出',exact:true}).fill(output)
  await page.getByRole('button',{name:'验证连接',exact:true}).click();await expect(page.getByRole('status')).toContainText('连接成功');expect(f.requests.some((r:any)=>r.body?.messages?.[0]?.content==='Reply only OK.')).toBe(true)
  await page.getByRole('textbox',{name:'上游模型名称',exact:true}).fill('edited-upstream');await expect(page.getByRole('status')).toHaveCount(0);await page.getByRole('textbox',{name:'上游模型名称',exact:true}).fill('upstream-proof')
  await page.getByRole('checkbox',{name:'允许手动选择推理等级',exact:true}).uncheck()
  await page.getByRole('button',{name:'保存并生效',exact:true}).click();await expect(page.getByRole('status')).toHaveText('已保存并生效')
  await page.goto(f.url+'/#/chat/conversations/'+task.id)
  await expect(page.getByRole('combobox',{name:'推理强度',exact:true})).toBeEnabled();await page.getByRole('combobox',{name:'推理强度',exact:true}).selectOption('default');await expect.poll(()=>task.reasoningEffort).toBe('default')
  await page.goto(f.url+'/#/settings/mcp');await page.getByRole('button',{name:'添加 MCP',exact:true}).click()
  await page.getByRole('textbox',{name:'连接标识',exact:true}).fill('card_proof');await page.getByRole('textbox',{name:'连接标识',exact:true}).press('Tab')
  await page.getByRole('textbox',{name:'名称',exact:true}).fill('本地验证工具');await page.getByRole('textbox',{name:'MCP 地址',exact:true}).fill(f.origin+'/mcp')
  await page.getByRole('button',{name:'读取工具列表',exact:true}).click();await expect(page.getByRole('checkbox',{name:'启用 echo',exact:true})).toBeChecked();await page.getByRole('checkbox',{name:'使用全部工具',exact:true}).uncheck()
  await page.getByRole('button',{name:'保存并生效',exact:true}).click();await expect(page.getByRole('status')).toHaveText('已保存并生效')
  await page.reload();await expect(page).toHaveURL(/#\/settings\/mcp\/card_proof$/);await expect(page.getByRole('checkbox',{name:'启用 echo',exact:true})).toBeChecked()
  await page.getByRole('link',{name:'返回 MCP 列表',exact:true}).click();await expect(page.getByRole('link',{name:'编辑 MCP card_proof',exact:true})).toContainText('本地验证工具')
  for(const path of ['/settings/providers','/settings/providers/proof/models/proof-model','/settings/mcp/card_proof','/settings/appearance']){
   await page.setViewportSize({width:390,height:780});await page.goto(f.url+'/#'+path);await expect(page.locator('.settings-mode')).toBeVisible();expect(await page.evaluate(()=>document.documentElement.scrollWidth<=document.documentElement.clientWidth)).toBe(true)
  }
  await expect(page.getByRole('heading',{name:'主题',exact:true})).toBeVisible()
 }finally{await f.close()}
})
