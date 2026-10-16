import { expect, test, type Page } from '@playwright/test'
import { fixture } from '../../tests/fixtures/fixture.mjs'
import { workspaceFixture } from '../../tests/fixtures/workspace-fixture.mjs'

let app:Awaited<ReturnType<typeof fixture>>, core:Awaited<ReturnType<typeof workspaceFixture>>
test.beforeAll(async()=>{core=await workspaceFixture();app=await fixture({enterprisePlatform:core.platform})})
test.afterAll(async()=>{await app?.close();await core?.close()})
test.use({viewport:{width:1600,height:900}})

const remaining=(page:Page)=>page.locator('.conversation-vlist').evaluate(element=>element.scrollHeight-element.clientHeight-element.scrollTop)
async function anchor(page:Page) {
  return page.locator('.conversation-vlist').evaluate(element=>{
    const viewport=element.getBoundingClientRect()
    const rows=[...element.querySelectorAll<HTMLElement>('[data-render-unit]')].map(row=>({id:row.dataset.renderUnit!,rect:row.getBoundingClientRect()})).filter(row=>row.rect.height>0&&row.rect.bottom>viewport.top&&row.rect.top<viewport.bottom).sort((a,b)=>Math.abs(a.rect.top-viewport.top)-Math.abs(b.rect.top-viewport.top))
    if(!rows[0])throw new Error('No visible conversation row')
    return {id:rows[0].id,top:rows[0].rect.top-viewport.top}
  })
}
async function expectAnchor(page:Page,saved:{id:string;top:number}) {
  await expect.poll(()=>page.locator('.conversation-vlist').evaluate((element,saved)=>{
    const row=[...element.querySelectorAll<HTMLElement>('[data-render-unit]')].find(row=>row.dataset.renderUnit===saved.id)
    return row?Math.abs(row.getBoundingClientRect().top-element.getBoundingClientRect().top-saved.top):Infinity
  },saved)).toBeLessThan(5)
}
async function expectLayout(page:Page) {
  await expect.poll(()=>page.locator('main.conversation-shell').evaluate(shell=>{
    const viewport=shell.querySelector<HTMLElement>('.conversation-vlist')!,box=viewport.getBoundingClientRect(),shellBox=shell.getBoundingClientRect()
    const rows=[...viewport.querySelectorAll<HTMLElement>('[data-virtual-item="true"]')].map(row=>row.getBoundingClientRect()).filter(rect=>rect.height>0).sort((a,b)=>a.top-b.top)
    const overlap=rows.reduce((max,row,index)=>index?Math.max(max,rows[index-1]!.bottom-row.top):max,0)
    return {pageOverflow:document.documentElement.scrollWidth-window.innerWidth,viewportOverflow:viewport.scrollWidth-viewport.clientWidth,outsideShell:Math.max(shellBox.left-box.left,box.right-shellBox.right),overlap}
  })).toEqual({pageOverflow:0,viewportOverflow:0,outsideShell:0,overlap:0})
  const heading=page.locator('.ew-heading')
  if(await heading.count()) {
    const lines=await page.locator('.conversation-header,.ew-heading').evaluateAll(headers=>headers.map(header=>header.getBoundingClientRect().bottom))
    expect(Math.max(...lines)-Math.min(...lines)).toBeLessThan(0.5)
  }
}

test('workspace opening and resizing preserve the rich conversation reader and bounded geometry',async({page})=>{
  const task=await app.manager.create({title:'工作区与长消息布局'})
  app.store.transaction(()=>{
    for(let index=0;index<120;index++)app.store.saveMessage(task.id,{id:`${task.id}:m-${index}`,index,role:'assistant',turnId:`turn-${index}`,blocks:[{id:`text-${index}`,type:'markdown',data:{markdown:`历史记录 ${index}\n\n`+'用于验证历史阅读位置。'.repeat(15)}}]})
    task.logicalCount=120;app.store.saveTask(task)
  })
  const code='```typescript\n'+Array.from({length:18},(_,index)=>`const line${index} = "${'long_code_value_'.repeat(20)}"`).join('\n')+'\n```'
  const table='| 项目 | 结果 | 详细数据 |\n| --- | --- | --- |\n'+Array.from({length:7},(_,index)=>`| row-${index} | ${'column-value-'.repeat(12)} | ${'tool-result-'.repeat(12)} |`).join('\n')
  app.manager.append(task,{role:'assistant',blocks:[{id:'rich',type:'markdown',data:{markdown:'# 代码与表格\n\n'+code+'\n\n'+table}},{id:'tool',type:'tool-result',data:{callId:'workspace-layout-tool',name:'read_file',status:'success',output:('工具结果 '+ 'wide_result_'.repeat(20)+'\n').repeat(12)}}]})
  await page.goto(`${app.url}/#/chat/conversations/${task.id}`)
  const viewport=page.locator('.conversation-vlist'),original=await viewport.elementHandle()
  await expect(viewport.locator('table')).toBeAttached()
  await expect(viewport.locator('pre code').first()).toContainText('const line17')
  const tool=page.locator('[data-call-id="workspace-layout-tool"] .activity-summary')
  await tool.click();await expect(tool).toHaveAttribute('aria-expanded','true')
  await viewport.hover({position:{x:20,y:150}});await page.mouse.wheel(0,-400)
  await expect(page.getByTestId('jump-latest')).toBeVisible()
  const saved=await anchor(page)
  await page.getByRole('button',{name:'工作区',exact:true}).click()
  const panel=page.getByRole('complementary',{name:'企业工作区'})
  await expect(panel.getByRole('option',{name:/hello\.txt/})).toBeVisible()
  await expectAnchor(page,saved);await expectLayout(page)
  const before=await panel.boundingBox(),separator=panel.getByRole('separator',{name:'调整工作区宽度'})
  await separator.focus();await separator.press('ArrowLeft');await separator.press('ArrowLeft');await separator.press('ArrowLeft')
  await expect.poll(async()=> (await panel.boundingBox())!.width).toBeGreaterThan(before!.width+50)
  await expectAnchor(page,saved);await expectLayout(page)
  const left=page.getByRole('separator',{name:'调整对话列表宽度'})
  await left.focus();await left.press('ArrowRight');await left.press('ArrowRight')
  await expectAnchor(page,saved);await expectLayout(page)
  await panel.getByRole('button',{name:'收起企业工作区',exact:true}).click()
  await expect(panel).toHaveCount(0);await expectAnchor(page,saved);await expectLayout(page)
  expect(await original!.evaluate(element=>element.isConnected&&element===document.querySelector('.conversation-vlist'))).toBe(true)
  app.manager.append(task,{role:'assistant',blocks:[{id:'new',type:'markdown',data:{markdown:'布局切换期间到达的完整消息'}}]})
  await expect(page.getByTestId('messages-after')).toHaveText('1')
  await expectAnchor(page,saved)
  await page.getByTestId('jump-latest').click()
  await expect(page.getByText('布局切换期间到达的完整消息',{exact:true})).toBeInViewport()
  await expect.poll(()=>remaining(page)).toBeLessThan(2)
  expect(await viewport.locator('[data-render-unit]').count()).toBeLessThan(100)
})

test('streaming survives workspace toggles without replacing the viewport and drafts survive conversation switches',async({page})=>{
  const task=await app.manager.create({title:'工作区与实时输出'}),other=await app.manager.create({title:'草稿切换目标'})
  task.status='working';task.activity={phase:'processing',startedAt:Date.now()}
  app.manager.append(task,{role:'user',blocks:[{id:'prompt',type:'markdown',data:{markdown:'持续输出期间浏览企业文件'}}]})
  const live=app.manager.append(task,{role:'assistant',live:true,blocks:[{id:'answer',type:'markdown',revision:0,data:{markdown:'流式开头'}}]})
  let streams=0;page.on('request',request=>{if(new URL(request.url()).pathname==='/api/events')streams++})
  await page.goto(`${app.url}/#/chat/conversations/${task.id}`)
  await expect(page.getByTestId('markdown-block').filter({hasText:'流式开头'})).toBeVisible()
  const viewport=page.locator('.conversation-vlist'),original=await viewport.elementHandle()
  const panel=page.getByRole('complementary',{name:'企业工作区'})
  let expected='流式开头'
  for(let index=0;index<3;index++){
    const chunk=`\n\n持续片段 ${index}。${'正在输出。'.repeat(10)}`;expected+=chunk;app.manager.appendText(task,live,'answer','markdown',chunk)
    await page.getByRole('button',{name:'工作区',exact:true}).click()
    await expect(panel.getByRole('option',{name:/hello\.txt/})).toBeVisible()
    await expect(viewport).toContainText(`持续片段 ${index}`)
    await page.getByRole('separator',{name:'调整对话列表宽度'}).press('ArrowRight')
    await expectLayout(page)
    await panel.getByRole('button',{name:'收起企业工作区',exact:true}).click()
    expect(await original!.evaluate(element=>element.isConnected&&element===document.querySelector('.conversation-vlist'))).toBe(true)
  }
  expect(streams).toBe(1)
  const persisted=app.store.message(task.id,live.index).blocks[0]
  if(persisted.type!=='markdown')throw new Error('Expected the streamed Markdown block')
  expect(persisted.data.markdown).toBe(expected)
  await expect.poll(()=>remaining(page)).toBeLessThan(2)
  await page.getByRole('button',{name:'工作区',exact:true}).click()
  await panel.getByRole('option',{name:/hello\.txt/}).dblclick()
  const editor=panel.locator('.cm-content');await expect(editor).toContainText('hello')
  await editor.fill('跨对话未保存草稿')
  await page.getByTestId(`session-${other.id}`).click()
  await expect(page.locator('main.conversation-shell')).toHaveAttribute('data-session-id',other.id)
  await expect(editor).toContainText('跨对话未保存草稿')
  await page.getByTestId(`session-${task.id}`).click()
  await expect(page.locator('main.conversation-shell')).toHaveAttribute('data-session-id',task.id)
  await expect(editor).toContainText('跨对话未保存草稿')
  await expect(viewport).toContainText('持续片段 2')
  expect(core.files.get('hello.txt')?.toString()).toBe('hello\r\nworld\r\n')
  app.manager.settle(task,'completed')
  await expect(page.locator('.conversation-header')).toContainText('已完成')
})

for(const width of [1050,1100])test(`workspace overlays the full chat height at viewport width ${width}`,async({page})=>{
  await page.setViewportSize({width,height:900})
  const task=await app.manager.create({title:`工作区响应断点 ${width}`})
  app.manager.append(task,{role:'assistant',blocks:[{id:'text',type:'markdown',data:{markdown:'断点切换不应把工作区放入第二行。'}}]})
  await page.goto(`${app.url}/#/chat/conversations/${task.id}`)
  const chat=page.locator('main.conversation-shell')
  await expect(chat).toBeVisible()
  const before=(await chat.boundingBox())!
  await page.getByRole('button',{name:'工作区',exact:true}).click()
  const panel=page.getByRole('complementary',{name:'企业工作区'})
  await expect(panel.getByRole('option',{name:/hello\.txt/})).toBeVisible()
  const host=(await page.locator('.agent-app').boundingBox())!,workspace=(await panel.boundingBox())!,after=(await chat.boundingBox())!
  expect(Math.abs(workspace.y-host.y)).toBeLessThan(1)
  expect(Math.abs(workspace.height-host.height)).toBeLessThan(1)
  expect(Math.abs(workspace.x+workspace.width-host.x-host.width)).toBeLessThan(1)
  expect(Math.abs(after.height-before.height)).toBeLessThan(1)
  expect(Math.abs(after.width-before.width)).toBeLessThan(1)
  await expectLayout(page)
  await panel.getByRole('button',{name:'收起企业工作区',exact:true}).click()
  await expect(chat.getByText('断点切换不应把工作区放入第二行。',{exact:true})).toBeInViewport()
})

test('oversized saved panel preferences retain usable chat width and resize from the rendered width',async({page})=>{
  await page.setViewportSize({width:1440,height:900})
  await page.addInitScript(()=>localStorage.setItem('tuck-enterprise-width','900'))
  const task=await app.manager.create({title:'工作区最大宽度'})
  app.manager.append(task,{role:'assistant',blocks:[{id:'text',type:'markdown',data:{markdown:'聊天正文需要保留可用宽度。'.repeat(40)}}]})
  await page.goto(`${app.url}/#/chat/conversations/${task.id}?ew=1`)
  const chat=page.locator('main.conversation-shell'),panel=page.getByRole('complementary',{name:'企业工作区'})
  await expect(panel.getByRole('option',{name:/hello\.txt/})).toBeVisible()
  const initial=(await panel.boundingBox())!
  expect((await chat.boundingBox())!.width).toBeGreaterThanOrEqual(479)
  expect(initial.width).toBeLessThan(900)
  const separator=panel.getByRole('separator',{name:'调整工作区宽度'})
  await separator.focus();await separator.press('ArrowRight')
  await expect.poll(async()=>(await panel.boundingBox())!.width).toBeLessThan(initial.width-15)
  const box=(await separator.boundingBox())!
  await page.mouse.move(box.x+box.width/2,box.y+80);await page.mouse.down();await page.mouse.move(box.x+box.width/2+40,box.y+80,{steps:8});await page.mouse.up()
  await expect.poll(async()=>(await panel.boundingBox())!.width).toBeLessThan(initial.width-50)
  await expectLayout(page)
  await page.getByRole('button',{name:'收起对话列表',exact:true}).click()
  expect((await chat.boundingBox())!.width).toBeGreaterThanOrEqual(479)
  await expectLayout(page)
})

test('both column splitters remember dragging, reset on double click and preserve minimum chat and workspace widths',async({page})=>{
  await page.setViewportSize({width:1440,height:900})
  const task=await app.manager.create({title:'三栏拖动记忆'})
  app.manager.append(task,{role:'assistant',blocks:[{id:'text',type:'markdown',data:{markdown:'对话列表和工作区分别调整，正文始终可读。'.repeat(20)}}]})
  await page.goto(`${app.url}/#/chat/conversations/${task.id}?ew=1`)
  const panel=page.getByRole('complementary',{name:'企业工作区'}),sidebar=page.getByTestId('session-sidebar'),chat=page.locator('main.conversation-shell')
  await expect(panel.getByRole('option',{name:/hello\.txt/})).toBeVisible()
  const left=page.getByRole('separator',{name:'调整对话列表宽度'}),right=panel.getByRole('separator',{name:'调整工作区宽度'})
  // Match the native-browser drag order and midpoint where a partial left drag
  // was observed: enlarge the right panel first, then drag the left splitter.
  const initialRight=(await right.boundingBox())!,initialLeft=(await left.boundingBox())!
  await page.mouse.move(initialRight.x+initialRight.width/2,450);await page.mouse.down();await page.mouse.move(initialRight.x+initialRight.width/2-80,450,{steps:8});await page.mouse.up()
  await expect.poll(async()=>(await panel.boundingBox())!.width).toBe(500)
  await page.mouse.move(initialLeft.x+initialLeft.width/2,450);await page.mouse.down()
  for(let step=1;step<=8;step++){await page.mouse.move(initialLeft.x+initialLeft.width/2+step*10,450);await page.waitForTimeout(32)}
  await page.mouse.up()
  await expect.poll(async()=>(await sidebar.boundingBox())!.width).toBe(300)
  await expect.poll(async()=>(await panel.boundingBox())!.width).toBe(500)
  await left.dblclick();await right.dblclick()
  const box=(await left.boundingBox())!
  await page.mouse.move(box.x+box.width/2,box.y+80);await page.mouse.down();await page.mouse.move(box.x+box.width/2+100,box.y+80,{steps:10});await page.mouse.up()
  await expect.poll(async()=>(await sidebar.boundingBox())!.width).toBe(320)
  const rightBox=(await right.boundingBox())!
  await page.mouse.move(rightBox.x+rightBox.width/2,rightBox.y+80);await page.mouse.down();await page.mouse.move(rightBox.x+rightBox.width/2-40,rightBox.y+80,{steps:8});await page.mouse.up()
  await expect.poll(async()=>(await panel.boundingBox())!.width).toBe(460)
  await page.reload()
  await expect(panel.getByRole('option',{name:/hello\.txt/})).toBeVisible()
  expect((await sidebar.boundingBox())!.width).toBe(320)
  expect((await panel.boundingBox())!.width).toBe(460)
  await page.setViewportSize({width:1107,height:900})
  await expect.poll(async()=>(await chat.boundingBox())!.width).toBeGreaterThanOrEqual(479)
  expect((await panel.boundingBox())!.width).toBeGreaterThanOrEqual(339)
  expect((await sidebar.boundingBox())!.width).toBeGreaterThanOrEqual(220)
  expect(await page.evaluate(()=>localStorage.getItem('tuck.sidebar.width'))).toBe('320')
  await expectLayout(page)
  await page.setViewportSize({width:1100,height:900})
  await expect(left).toBeHidden()
  expect((await sidebar.boundingBox())!.width).toBe(220)
  await page.setViewportSize({width:1600,height:900})
  await expect.poll(async()=>(await sidebar.boundingBox())!.width).toBe(320)
  await left.dblclick();await right.dblclick()
  await expect.poll(async()=>(await sidebar.boundingBox())!.width).toBe(220)
  await expect.poll(async()=>(await panel.boundingBox())!.width).toBe(420)
  expect(await page.evaluate(()=>({left:localStorage.getItem('tuck.sidebar.width'),right:localStorage.getItem('tuck-enterprise-width')}))).toEqual({left:'220',right:'420'})
  await expectLayout(page)
})
