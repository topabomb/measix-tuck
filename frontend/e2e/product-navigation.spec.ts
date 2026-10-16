import { openSettings, closeSettings } from './settings'
import { test, expect } from '@playwright/test'
import { fixture } from '../../tests/fixtures/fixture.mjs'
import { workspaceFixture } from '../../tests/fixtures/workspace-fixture.mjs'
import { mkdir } from 'node:fs/promises'
import { join } from 'node:path'

test('assistant directory stays left aligned on narrow screens and copies its full path',async({page,context},info)=>{
  const app=await fixture({localAssistants:true})
  try{
    const task=await app.manager.create({title:'目录复制验收'})
    const directory=join(task.workspace,'reference files with spaces','long-directory-for-layout-and-copy-verification')
    await mkdir(directory,{recursive:true});task.workspace=directory;app.store.saveTask(task)
    await context.grantPermissions(['clipboard-read','clipboard-write'],{origin:app.url})
    await page.goto(`${app.url}/#/chat/conversations/${task.id}`)
    for(const width of [1280,390]){
      await page.setViewportSize({width,height:780});await page.getByTestId('active-assistant').click()
      const panel=page.getByRole('dialog',{name:'助手信息',exact:true}),path=panel.locator('.directory-path code')
      const directoryDetails=panel.locator('.directory-details')
      if(await directoryDetails.getAttribute('open')===null)await directoryDetails.locator('summary').click()
      await expect(path).toHaveText(directory)
      const geometry=await panel.evaluate(el=>{
        const p=el.querySelector('.directory-path')!,text=p.querySelector('code')!,label=el.querySelector('dt')!,button=p.querySelector('button')!,box=el.getBoundingClientRect()
        return {align:getComputedStyle(p).textAlign,left:text.getBoundingClientRect().left,labelLeft:label.getBoundingClientRect().left,overlap:text.getBoundingClientRect().right-button.getBoundingClientRect().left,overflow:el.scrollWidth-el.clientWidth,inside:box.left>=0&&box.right<=innerWidth}
      })
      expect(geometry.align).toBe('left');expect(geometry.left).toBe(geometry.labelLeft);expect(geometry.overlap).toBeLessThanOrEqual(0);expect(geometry.overflow).toBeLessThanOrEqual(1);expect(geometry.inside).toBe(true)
      await panel.getByRole('button',{name:'复制工作目录'}).click();await expect(panel.getByRole('button',{name:'复制工作目录'})).toHaveText('已复制')
      expect(await page.evaluate(()=>navigator.clipboard.readText())).toBe(directory)
      await page.screenshot({path:info.outputPath(`assistant-directory-${width}.png`)})
      await page.keyboard.press('Escape');await expect(panel).toBeHidden()
    }
  }finally{await app.close()}
})

test('assistant header opens bounded information without navigation',async({page})=>{
  const app=await fixture({localAssistants:true,attachmentModels:true})
  try {
    const task=await app.manager.create({title:'助手信息验收',approvalMode:'auto'})
    await page.goto(`${app.url}/#/chat/conversations/${task.id}`)
    const entry=page.getByTestId('active-assistant'),panel=page.getByRole('dialog',{name:'助手信息',exact:true})
    for(const width of [1440,390]) {
      await page.setViewportSize({width,height:800})
      await entry.click()
      await expect(panel).toBeVisible()
      await expect(panel).toContainText('通用助手');await expect(panel).toContainText('自动批准')
      await expect(panel.getByRole('link',{name:'查看助手配置 ›',exact:true})).toHaveAttribute('href','#/settings/agents/root')
      await expect(panel.getByRole('link',{name:'打开远程工作区 ›',exact:true})).toHaveAttribute('href','#/workspace')
      const bounds=await panel.boundingBox();expect(bounds!.x).toBeGreaterThanOrEqual(0);expect(bounds!.x+bounds!.width).toBeLessThanOrEqual(width)
      expect(await panel.evaluate(el=>el.scrollWidth-el.clientWidth)).toBeLessThanOrEqual(1)
      await expect(page).toHaveURL(`${app.url}/#/chat/conversations/${task.id}`)
      await page.keyboard.press('Escape');await expect(panel).toBeHidden()
    }
    await page.goto(`${app.url}/#/chat/assistants/root`)
    await expect(page.getByRole('heading',{name:'页面不存在',exact:true})).toBeVisible()
  }finally{await app.close()}
})

test('workspace leaves the active child name readable at 744, 584 and 480 pixel chat widths',async({page})=>{
  const core=await workspaceFixture(),app=await fixture({enterprisePlatform:core.platform})
  try{
    const root=await app.manager.create({title:'父会话 · 企业工作区标题检查'})
    const child=app.manager.createChild(root,'workspace-header-child','reviewer','background')
    child.title='reviewer · 企业工作区标题布局验证'
    app.manager.settle(child,'completed');app.store.saveTask(child)
    await page.setViewportSize({width:1440,height:900})
    await page.addInitScript(()=>localStorage.setItem('tuck-enterprise-width','420'))
    await page.goto(app.url+`/#/chat/conversations/${child.id}?ew=1`)
    const panel=page.getByRole('complementary',{name:'企业工作区'}),header=page.locator('.conversation-header'),picker=page.getByTestId('agent-switch')
    await expect(panel.getByRole('option',{name:/hello\.txt/})).toBeVisible()
    const separator=panel.getByRole('separator',{name:'调整工作区宽度'})
    for(const [expectedWidth,steps] of [[744,0],[584,8],[480,6]] as const){
      if(steps){await separator.focus();for(let step=0;step<steps;step++)await separator.press('ArrowLeft')}
      await expect.poll(()=>header.evaluate(el=>Math.round(el.getBoundingClientRect().width))).toBe(expectedWidth)
      expect((await header.boundingBox())!.height).toBe(48)
      expect((await picker.locator('span').first().boundingBox())!.width).toBeGreaterThan(80)
      await expect(picker.locator('span').first()).toHaveText(child.title)
      await expect(header.getByRole('button',{name:'产物',exact:true})).toBeVisible()
      await expect(header.getByRole('button',{name:'工作区',exact:true})).toBeVisible()
      await expect(page.getByTestId('active-assistant')).toHaveAccessibleName('所属主助手信息')
      expect(await header.evaluate(el=>{
        const title=el.querySelector('.conversation-title')!.getBoundingClientRect(),context=el.querySelector('.conversation-header-context')!.getBoundingClientRect(),actions=el.querySelector('.conversation-header-actions')!.getBoundingClientRect(),heading=el.querySelector('.conversation-heading')!.getBoundingClientRect()
        return Math.max(title.right-context.left,heading.right-actions.left,el.scrollWidth-el.clientWidth)
      })).toBeLessThanOrEqual(1)
      await picker.click()
      await expect(page.getByTestId(`agent-choice-${child.id}`)).toHaveAttribute('aria-current','page')
      await expect(page.getByTestId(`agent-choice-${root.id}`)).toBeVisible()
      await page.keyboard.press('Escape')
      await expect(picker).toBeFocused()
    }
    const workspaceIcon=header.getByRole('button',{name:'工作区',exact:true}).locator('svg')
    expect(await workspaceIcon.innerHTML()).toBe(await page.getByTestId('mode-workspace').locator('svg').innerHTML())
    await expect(workspaceIcon).toHaveAttribute('viewBox','0 0 64 64')
    await page.getByTestId('active-assistant').click()
    await expect(page.getByRole('dialog',{name:'所属主助手信息',exact:true})).toContainText('主对话模型')
    await page.keyboard.press('Escape')
    await page.getByTestId('parent-session-link').click()
    await expect(page.getByTestId('composer-input')).toBeVisible()
  }finally{await app.close();await core.close()}
})

test('top navigation preserves hierarchy and status with bounded menus across themes and sizes', async ({page}) => {
  const app = await fixture()
  try {
    const root = await app.manager.create({title:'父会话 · 很长的项目名称用于检查顶部不换行'})
    const children = Array.from({length:65},(_,i)=>app.manager.createChild(root,`fixture-${i}`,`worker-${i}`,i%2?'background':'foreground'))
    const failed=children[1]!, waiting=children[2]!, running=children[3]!
    app.manager.settle(failed,'error',{code:'fixture',message:'子代理验证失败'})
    Object.assign(waiting,{status:'waiting',pendingInteraction:{id:'fixture-q',kind:'question',title:'请确认',detail:'等待父会话处理'}}); app.store.saveTask(waiting)
    Object.assign(running,{status:'working'}); app.store.saveTask(running)
    const nested=app.manager.createChild(children[0]!, 'nested','nested-worker')
    app.manager.append(root,{role:'assistant',turnId:'visual',blocks:[{id:'thought',type:'reasoning',data:{text:'用于验证折叠状态的主题和样式',status:'complete'}}]})
    await page.goto(app.url)
    const nav=page.getByTestId('agent-switch'), menu=page.getByTestId('agent-popover')
    await expect(nav).toHaveText(/子代理66/)
    await expect(menu.locator('[data-session-choice]')).toHaveCount(0)
    const before=await page.getByTestId('scrollport').boundingBox()
    await nav.click()
    await expect(menu).toBeVisible()
    await expect(menu.locator('[data-session-choice]')).toHaveCount(30)
    await menu.getByRole('textbox',{name:'搜索子代理'}).press('ArrowUp')
    await expect(menu.locator('[data-session-choice]').last()).toBeFocused()
    expect(await page.getByTestId('scrollport').boundingBox()).toEqual(before)
    await expect(nav).toContainText('待处理 1 · 执行 1 · 异常 1')
    await expect(menu).toContainText('待处理 1 · 执行 1 · 异常 1')
    await expect(page.getByTestId(`agent-choice-${failed.id}`)).toContainText('失败')
    await expect(page.getByTestId(`agent-choice-${waiting.id}`)).toContainText('等待回答')
    await menu.getByRole('button',{name:'下一页'}).click()
    await expect(menu).toContainText('2 / 3')
    await menu.getByRole('textbox',{name:'搜索子代理'}).fill('nested-worker')
    await expect(menu.locator('[data-session-choice]')).toHaveCount(1)
    await page.getByTestId(`agent-choice-${nested.id}`).click()
    await expect(page.getByTestId('parent-session-link')).toHaveAttribute('title','返回 worker-0 · 子任务')
    await expect(page.getByText('由父 Agent 管理，此处只读')).toBeVisible()
    await nav.click()
    await page.getByTestId(`agent-choice-${failed.id}`).click()
    await expect(page.getByTestId('session-navigation')).toContainText('worker-1')
    await page.getByTestId('parent-session-link').click()
    await expect(page.getByTestId('composer-input')).toBeVisible()

    for(const theme of ['苔灰','暖砂','炭棕','曜黑（高对比）','石墨']) {
      await openSettings(page)
      await page.getByRole('navigation',{name:'设置分类'}).getByRole('link',{name:'外观',exact:true}).click()
      await page.getByRole('button',{name:`${theme}`}).click()
      await closeSettings(page)
      const surfaces=await page.locator('.activity-summary').evaluateAll(elements=>elements.map(el=>({background:getComputedStyle(el).backgroundColor,border:getComputedStyle(el).borderTopWidth})))
      expect(surfaces.length).toBe(1)
      for(const surface of surfaces) {expect(surface.background).toBe('rgba(0, 0, 0, 0)'); expect(surface.border).toBe('0px')}
      await nav.click()
      const colors=await menu.evaluate(el=>({bg:getComputedStyle(el).backgroundColor,text:getComputedStyle(el).color,expected:getComputedStyle(document.documentElement).getPropertyValue('--tuck-surface').trim(),textExpected:getComputedStyle(document.documentElement).getPropertyValue('--tuck-text').trim()}))
      const rgb=(hex:string)=>`rgb(${hex.slice(1).match(/../g)!.map(c=>parseInt(c,16)).join(', ')})`
      expect(colors.bg).toBe(rgb(colors.expected)); expect(colors.text).toBe(rgb(colors.textExpected))
      const triggerColors = await nav.evaluate(el => ({ actual:getComputedStyle(el).backgroundColor, expected:getComputedStyle(document.documentElement).getPropertyValue('--tuck-hover').trim(), border:getComputedStyle(el).borderTopWidth }))
      expect(triggerColors.actual).toBe(rgb(triggerColors.expected)); expect(triggerColors.border).toBe('1px')
      const failureColor=await page.getByTestId(`agent-choice-${failed.id}`).locator('.failure').first().evaluate(el=>({actual:getComputedStyle(el).color,expected:getComputedStyle(document.documentElement).getPropertyValue('--tuck-danger').trim()}))
      expect(failureColor.actual).toBe(rgb(failureColor.expected))
      await page.keyboard.press('Escape')
      await expect(menu).not.toBeVisible()
      await expect(nav).toBeFocused()
    }
    for(const width of [390,830,1365]) {
      await page.setViewportSize({width,height:844})
      await nav.click()
      const rect=(await menu.boundingBox())!
      expect(rect.x).toBeGreaterThanOrEqual(0); expect(rect.x+rect.width).toBeLessThanOrEqual(width)
      expect(rect.y+rect.height).toBeLessThanOrEqual(844)
      expect((await page.locator('.conversation-header').boundingBox())!.height).toBe(48)
      expect((await nav.locator('span').first().boundingBox())!.width).toBeGreaterThan(28)
      expect((await nav.locator('.agent-count').boundingBox())!.width).toBeGreaterThan(8)
      expect(await page.evaluate(()=>document.body.scrollWidth-innerWidth)).toBeLessThanOrEqual(1)
      await page.getByTestId('composer-input').click()
      await expect(menu).not.toBeVisible()
      await expect(menu.locator('[data-session-choice]')).toHaveCount(0)
    }
  } finally { await app.close() }
})

test('child navigation summarizes observed states and keeps background progress distinct from live history', async ({page},info) => {
  const app = await fixture()
  try {
    const root = await app.manager.create({title:'子代理进展'})
    root.status='working'
    root.turnId='test-turn-1'
    root.backgroundRuns=[
      {id:'pending',agent:'worker',title:'检索资料'},
      {id:'reported',agent:'worker',title:'核对来源',status:'running',summary:'已找到资料，正在核对原文',observedAt:new Date().toISOString(),turnId:root.turnId},
      {id:'done',agent:'reviewer',title:'检查结果',status:'completed',summary:'核对完成',observedAt:new Date().toISOString()},
    ]
    app.store.saveTask(root)
    const live=app.manager.createChild(root,'live-child','worker')
    Object.assign(live,{status:'working',activity:{phase:'tool',detail:'搜索文件',startedAt:Date.now()}});app.store.saveTask(live)
    await page.goto(app.url)
    const nav=page.getByTestId('agent-switch'), menu=page.getByTestId('agent-popover')
    await expect(nav).toContainText('执行 2 · 完成 1 · 待同步 1')
    for(const width of [1365,390]) {
      await page.setViewportSize({width,height:844})
      await nav.click()
      await expect(menu).toContainText('状态待同步')
      await expect(menu).toContainText('已找到资料，正在核对原文')
      await expect(page.getByTestId(`agent-choice-${live.id}`)).toContainText('正在执行 · 搜索文件')
      await expect(menu.locator('.background-progress').first()).toHaveAttribute('aria-label','子代理最近输出，仅供进展参考')
      expect(await page.evaluate(()=>document.body.scrollWidth-innerWidth)).toBeLessThanOrEqual(1)
      const rect=(await menu.boundingBox())!
      expect(rect.x+rect.width).toBeLessThanOrEqual(width)
      await page.screenshot({path:info.outputPath(`child-progress-${width}.png`)})
      await page.keyboard.press('Escape')
    }
    root.turnId='test-turn-2';app.manager.publish(root,{type:'task',task:root})
    await expect(nav).toContainText('执行 1 · 完成 1 · 待同步 2')
    app.manager.settle(root,'interrupted')
    await expect(nav).toContainText('执行 1 · 完成 1 · 待同步 2')
    await page.reload()
    await expect(nav).toContainText('执行 1 · 完成 1 · 待同步 2')
  } finally {await app.close()}
})

test('small child picker has a centered toggle, explicit destinations and no unnecessary search', async ({page}) => {
  const app = await fixture()
  try {
    const root = await app.manager.create({title:'测试'})
    const child = app.manager.createChild(root,'navigation-child','worker','background')
    app.manager.settle(child,'completed')
    await page.goto(app.url)
    const nav = page.getByTestId('agent-switch'), menu = page.getByTestId('agent-popover')
    for (const width of [1365,390]) {
      await page.setViewportSize({width,height:800})
      const geometry = await nav.evaluate(el => {
        const rect = el.getBoundingClientRect(), arrow = el.querySelector('svg')!.getBoundingClientRect()
        return {offset:Math.abs(rect.y + rect.height/2 - arrow.y - arrow.height/2),height:rect.height,width:arrow.width,transform:getComputedStyle(el.querySelector('svg')!).transform}
      })
      expect(geometry.offset).toBeLessThanOrEqual(.5)
      expect(geometry.height).toBe(30); expect(geometry.width).toBe(14); expect(geometry.transform).toBe('none')
      await nav.click()
      await expect(nav).toHaveAttribute('aria-expanded','true')
      await expect(menu.getByRole('textbox')).toHaveCount(0)
      await expect(page.getByTestId(`agent-choice-${root.id}`)).toContainText('✓ 当前会话')
      await expect(page.getByTestId(`agent-choice-${root.id}`)).toBeFocused()
      await expect(page.getByTestId(`agent-choice-${child.id}`)).toContainText('查看 →')
      expect(await nav.locator('svg').evaluate(el => getComputedStyle(el).transform)).not.toBe('none')
      await page.keyboard.press('ArrowDown')
      await page.keyboard.press('Enter')
      await expect(menu).not.toBeVisible()
      await expect(page.getByTestId('parent-session-link')).toBeVisible()
      await page.getByTestId('parent-session-link').click()
      await expect(nav).toHaveAttribute('aria-expanded','false')
    }
  } finally { await app.close() }
})

test('slow child navigation cannot replace a newer selection',async({page})=>{
  const app=await fixture()
  try {
    const root=await app.manager.create({title:'切换顺序'})
    const slow=app.manager.createChild(root,'slow-child','slow-worker')
    const fast=app.manager.createChild(root,'fast-child','fast-worker')
    for(const child of [slow,fast]) {
      for(let i=0;i<3;i++) app.manager.append(child,{role:'assistant',turnId:'fixture',blocks:[{id:`t-${i}`,type:'markdown',data:{markdown:`${child.agent} ${i}`}}]})
    }
    await page.goto(app.url)
    let release:()=>void=()=>{}
    const gate=new Promise<void>(resolve=>{release=resolve})
    let reached:()=>void=()=>{}
    const seen=new Promise<void>(resolve=>{reached=resolve})
    await page.route(`**/api/tasks/${slow.id}/messages?start=0&count=3&*`,async route=>{reached(); await gate; await route.continue()})
    await page.getByTestId('agent-switch').click()
    await page.getByTestId(`agent-choice-${slow.id}`).click()
    await seen
    await page.getByTestId('agent-switch').click()
    await page.getByTestId(`agent-choice-${fast.id}`).click()
    await expect(page.getByTestId('session-navigation')).toContainText('fast-worker')
    const delivered=page.waitForResponse(response=>response.url().includes(`/api/tasks/${slow.id}/messages?start=0&count=3`))
    release(); await delivered
    await expect(page.getByText('fast-worker 0',{exact:true})).toBeVisible()
    await page.getByTestId('agent-switch').click()
    await expect(page.getByTestId(`agent-choice-${fast.id}`)).toHaveAttribute('aria-current','page')
  } finally {await app.close()}
})

test('product dialogs contain focus, match themes and fit a narrow viewport',async({page})=>{
  const app=await fixture()
  try {
    await page.goto(app.url)
    for(const theme of ['苔灰','暖砂','炭棕','曜黑（高对比）','石墨']) {
      await openSettings(page)
      await page.getByRole('navigation',{name:'设置分类'}).getByRole('link',{name:'外观',exact:true}).click()
      await page.getByRole('button',{name:`${theme}`}).click()
      await closeSettings(page)
      await page.getByTestId('new-session').click()
      const dialog=page.getByRole('dialog',{name:'新对话',exact:true})
      await expect(dialog).toBeVisible()
      await expect(dialog.getByPlaceholder('新对话',{exact:true})).toBeFocused()
      for(let i=0;i<9;i++) {await page.keyboard.press('Tab'); expect(await dialog.evaluate(el=>el.contains(document.activeElement))).toBe(true)}
      const color=await dialog.evaluate(el=>({bg:getComputedStyle(el).backgroundColor,scheme:getComputedStyle(el).colorScheme}))
      expect(color.bg).not.toBe('rgba(0, 0, 0, 0)'); expect(color.scheme).toBe(['苔灰','暖砂'].includes(theme)?'light':'dark')
      await page.keyboard.press('Escape'); await expect(dialog).not.toBeVisible()
      await expect(page.getByTestId('new-session')).toBeFocused()
    }
    await page.setViewportSize({width:390,height:640})
    await page.getByTestId('mobile-session-toggle').click()
    await openSettings(page)
    await page.getByRole('link',{name:'返回设置',exact:true}).click()
    await page.getByRole('navigation',{name:'设置首页'}).getByRole('link',{name:'企业平台',exact:true}).click()
    const dialog=page.locator('.tuck-platform-modal')
    const rect=(await dialog.boundingBox())!
    expect(rect.x).toBeGreaterThanOrEqual(0); expect(rect.x+rect.width).toBeLessThanOrEqual(390)
    await page.keyboard.press('Escape'); await expect(dialog).toBeVisible()
    await closeSettings(page)
  } finally {await app.close()}
})

test('leaving the product during connection or viewport restoration releases subscriptions',async({page})=>{
  const app=await fixture()
  try {
    await page.addInitScript(()=>{
      const observed=new Set<ResizeObserver>(), streams=new Set<EventSource>()
      const BaseObserver=window.ResizeObserver, BaseSource=window.EventSource
      window.ResizeObserver=class extends BaseObserver {
        observe(target:Element,options?:ResizeObserverOptions) { observed.add(this); super.observe(target,options) }
        disconnect() { observed.delete(this); super.disconnect() }
      }
      window.EventSource=class extends BaseSource {
        constructor(url:string | URL,options?:EventSourceInit) { super(url,options); streams.add(this) }
        close() { streams.delete(this); super.close() }
      }
      Object.defineProperty(window,'productResources',{get:()=>({observers:observed.size,streams:streams.size})})
    })
    let release:()=>void=()=>{}
    const gate=new Promise<void>(resolve=>{release=resolve})
    let connecting=false
    await page.route('**/api/tasks',async route=>{connecting=true;await gate; await route.continue()},{times:1})
    await page.goto(app.url)
    // Leave an actual in-flight connection, after the initial route has started it.
    await expect.poll(()=>connecting).toBe(true)
    await page.evaluate(()=>{location.hash='/reference'})
    await expect(page.locator('.app-shell')).toHaveCount(0)
    const arrived=page.waitForResponse(response=>response.url()===`${app.url}/api/tasks`)
    release(); await arrived
    // The reference workspace has its own viewports; only the abandoned product stream must close.
    await expect.poll(()=>page.evaluate(()=>(window as unknown as {productResources:{streams:number}}).productResources.streams)).toBe(0)
    await page.goto(app.url)
    await expect(page.getByTestId('composer-input')).toBeVisible()
    await page.evaluate(()=>{location.hash='architecture'})
    await expect(page.locator('[data-conversation-engine]')).toHaveCount(0)
    await expect.poll(()=>page.evaluate(()=>(window as unknown as {productResources:{streams:number;observers:number}}).productResources)).toEqual({streams:0,observers:0})
  } finally {await app.close()}
})
