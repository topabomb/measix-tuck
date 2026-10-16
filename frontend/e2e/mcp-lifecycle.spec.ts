import {test,expect} from '@playwright/test'
import {settingsFixture} from '../../tests/fixtures/settings-fixture.mjs'
async function register(f:Awaited<ReturnType<typeof settingsFixture>>) {
 const data=await f.read();data.config.localMcp=[{name:'proof',displayName:'生命周期 MCP',enabled:true,url:f.origin+'/mcp',agents:['root']}]
 const r=await f.save(data);expect(r.status).toBe(200)
}

test('MCP first verification can recover and deleting requires saving before refresh',async({page},info)=>{
 const f=await settingsFixture()
 try{
  f.controls.mcpVersion='2024-11-05';await register(f)
  await page.goto(f.url+'/#/settings/mcp/proof')
  const editor=page.getByRole('region',{name:'MCP 配置',exact:true})
  await expect(editor.getByText('待验证',{exact:true})).toBeVisible()
  await expect(editor.getByText('尚未验证',{exact:true})).toBeVisible()
  f.controls.mcpVersion='2025-03-26'
  await editor.getByRole('button',{name:'读取工具列表',exact:true}).click()
  await expect(editor.getByText('最近验证成功',{exact:true})).toBeVisible()
  await expect(editor.getByRole('checkbox',{name:'启用 echo',exact:true})).toBeVisible()
  await page.screenshot({path:info.outputPath('mcp-verified-wide.png')})
  await page.setViewportSize({width:390,height:780})
  await page.screenshot({path:info.outputPath('mcp-verified-narrow.png')})
  expect(await editor.evaluate(el=>el.scrollWidth<=el.clientWidth)).toBe(true)
  await editor.getByRole('button',{name:'删除 MCP',exact:true}).click()
  await expect(page.getByText('有未保存修改 · 提供商、Agent 和 MCP 一起保存',{exact:true})).toBeVisible()
  await page.getByRole('button',{name:'保存并生效',exact:true}).click()
  await expect(page.getByText('已保存并生效',{exact:true})).toBeVisible()
  await page.reload();await expect(page.getByRole('button',{name:'添加 MCP',exact:true})).toBeVisible()
  await expect(page.getByRole('link',{name:'编辑 MCP proof',exact:true})).toHaveCount(0)
  expect((await f.read()).config.localMcp).toEqual([])
  expect(f.app!.request({op:'mcpCacheCount',key:'local:proof'})).toBe(0)
 }finally{await f.close()}
})

test('MCP displays validation facts while native retry reports the final tool result',async({page})=>{
 const f=await settingsFixture()
 try{
  await register(f);await page.goto(f.url+'/#/settings/mcp/proof')
  const editor=page.getByRole('region',{name:'MCP 配置',exact:true})
  await expect(editor.getByText('最近验证成功',{exact:true})).toBeVisible()
  f.controls.mcpFailure={method:'tools/call',drop:true}
  const task=await f.app.manager.create({approvalMode:'auto'});f.app.manager.submit(task.id,'Run the MCP proof tool')
  await expect.poll(()=>f.app.manager.runs.has(task.id)).toBe(false)
  expect(f.controls.mcpExecuted).toBe(2)
  const messages:{blocks:{type:string;data:{status?:string}}[]}[]=f.app.store.messages(task.id)
  const result=messages.flatMap(message=>message.blocks).filter(block=>block.type==='tool-result').at(-1)
  expect(result?.data.status).toBe('success')
  await expect(editor.getByText('最近验证成功',{exact:true})).toBeVisible()
  f.controls.mcpOffline=true
  await editor.getByRole('button',{name:'读取工具列表',exact:true}).click()
  await expect(editor.getByText('最近验证失败',{exact:true})).toBeVisible()
  await expect(editor.locator('dd').filter({hasText:/^1$/})).toBeVisible()
  f.controls.mcpOffline=false
  await editor.getByRole('button',{name:'读取工具列表',exact:true}).click()
  await expect(editor.getByText('最近验证成功',{exact:true})).toBeVisible()
 }finally{await f.close()}
})
