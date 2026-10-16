import {test,expect} from '@playwright/test'
import {fixture,until} from '../../tests/fixtures/fixture.mjs'

test('failed or paused tasks lead to the conversation without delivery or supplement actions',async({page},info)=>{
  const app=await fixture({localAssistants:true,attachmentModels:true,delivery:true})
  try {
    const task=await app.manager.create({title:'T-fail · 图片提交失败'})
    await app.deliver(task,'上轮已有交付。')
    task.queue=[{id:'queued',prompt:'查看图片'}];task.queuedPrompts=1;task.queuePaused=true
    app.manager.settle(task,'error',{code:'execution_error',message:'图片消息过大，请减少图片或缩小尺寸'})
    for(const width of [1280,390]) {
      await page.setViewportSize({width,height:780})
      for(const path of ['/tasks','/tasks/assistants/root',`/tasks/${task.id}`,`/tasks/${task.id}/delivery`,`/tasks/${task.id}/supplement`]) {
        await page.goto(app.url+'/#'+path)
        const main=page.getByRole('main',{name:'任务频道'})
        await expect(main.getByRole('link',{name:'进入对话处理',exact:true})).toHaveAttribute('href',`#/chat/conversations/${task.id}`)
        await expect(main.getByRole('link',{name:'查看交付',exact:true})).toHaveCount(0)
        await expect(main.getByRole('link',{name:'补充任务',exact:true})).toHaveCount(0)
        await expect(main.getByRole('button',{name:'提交补充',exact:true})).toHaveCount(0)
        expect(await main.evaluate(el=>el.scrollWidth-el.clientWidth)).toBeLessThanOrEqual(1)
      }
      await page.screenshot({path:info.outputPath(`task-recovery-${width}.png`)})
      await page.getByRole('link',{name:'进入对话处理',exact:true}).click()
      await expect(page).toHaveURL(app.url+`/#/chat/conversations/${task.id}`)
      await expect(page.getByRole('textbox',{name:'消息',exact:true})).toBeVisible()
    }
  } finally {await app.close()}
})

test('return links stay unboxed, aligned and separated from content in each channel',async({page},info)=>{
  const app=await fixture({localAssistants:true,attachmentModels:true,delivery:true})
  try {
    const task=await app.manager.create({title:'T-link · 返回布局'})
    await app.deliver(task,'已完成返回链路验收。');app.manager.settle(task,'completed')
    const surfaces=[
      {name:'task-list',path:'/tasks/assistants/root',link:'.task-navigation > .back-link',content:'.assistant-section'},
      {name:'task-delivery',path:`/tasks/${task.id}/delivery`,link:'.task-navigation > .back-link',content:'.task-delivery'},
      {name:'settings',path:'/settings/agents',link:'.settings-back',content:'.settings-heading h2'},
      {name:'chat-delivery',path:`/chat/conversations/${task.id}/delivery`,link:'.delivery-back',content:'.chat-delivery header h1'},
    ]
    for(const width of [1280,390])for(const surface of surfaces){
      await page.setViewportSize({width,height:780});await page.goto(app.url+'/#'+surface.path)
      const link=page.locator(surface.link);await expect(link).toBeVisible();await expect(link).toHaveAttribute('href',/^#\//)
      await link.hover();await link.focus()
      const style=await link.evaluate(el=>{const s=getComputedStyle(el);return {tag:el.tagName,background:s.backgroundColor,border:s.borderWidth,padding:s.padding,color:s.color,outline:s.outlineWidth}})
      expect(style.tag).toBe('A');expect(style.background).toBe('rgba(0, 0, 0, 0)');expect(style.border).toBe('0px');expect(style.padding).toBe('0px')
      await page.keyboard.press('Tab');await page.keyboard.press('Shift+Tab');await expect(link).toBeFocused()
      expect(await link.evaluate(el=>getComputedStyle(el).outlineWidth)).toBe('2px')
      if(surface.name.startsWith('task-')){
        const back=(await link.boundingBox())!,content=(await page.locator(surface.content).first().boundingBox())!,nav=(await page.locator('.task-navigation').boundingBox())!
        expect(back.x).toBeCloseTo(content.x,0);expect(content.y-nav.y-nav.height).toBeGreaterThanOrEqual(12)
      }
      expect(await page.evaluate(()=>document.documentElement.scrollWidth-innerWidth)).toBeLessThanOrEqual(1)
      await link.evaluate(el=>(el as HTMLElement).blur())
      await page.screenshot({path:info.outputPath(`return-${surface.name}-${width}.png`)})
      await link.click();await expect(page).not.toHaveURL(app.url+'/#'+surface.path)
    }
    const child=app.manager.createChild(task,'return-child','worker','background');app.manager.settle(child,'completed')
    await page.goto(`${app.url}/#/chat/conversations/${child.id}?custom=keep`)
    const parent=page.getByTestId('parent-session-link'),footer=page.locator('.child-session-caption .back-link')
    await expect(parent).toHaveAttribute('href',`#/chat/conversations/${task.id}?custom=keep`)
    await expect(footer).toHaveAttribute('href',`#/chat/conversations/${task.id}?custom=keep`)
    await footer.click();await expect(page).toHaveURL(`${app.url}/#/chat/conversations/${task.id}?custom=keep`)
  }finally{await app.close()}
})

test('delivery previews distinguish current and previous turns without duplicating execution status',async({page})=>{
  const app=await fixture({localAssistants:true,attachmentModels:true,delivery:true})
  try {
    const task=await app.manager.create({title:'T-prvw · 交付摘要布局'})
    await app.deliver(task,'摘要开头。'+ '交付内容保持静态显示并保留开头。'.repeat(45))
    app.manager.settle(task,'completed')
    await page.goto(`${app.url}/#/tasks`)
    const row=page.getByRole('listitem',{name:'交付摘要布局',exact:true}),preview=row.getByRole('group',{name:'交付概况'})
    await expect(preview).toHaveAttribute('data-delivery-state','current-ended')
    await expect(preview.locator('strong')).toHaveText('本轮交付 · V1 · 0 个文件')
    for(const width of [1440,390]) {
      await page.setViewportSize({width,height:800})
      const layout=await row.evaluate(el=>{
        const status=el.querySelector('.task-status')!.getBoundingClientRect(),excerpt=el.querySelector('.task-result-excerpt')!,text=excerpt.getBoundingClientRect(),header=el.querySelector('.task-file-count')!.getBoundingClientRect()
        return {height:text.height,left:text.left,statusLeft:status.left,headerLeft:header.left,overflow:el.scrollWidth-el.clientWidth,weight:getComputedStyle(el.querySelector('.task-file-count')!).fontWeight}
      })
      expect(layout.height).toBeLessThanOrEqual(60);expect(layout.height).toBeGreaterThan(20)
      expect(layout.left).toBeCloseTo(layout.statusLeft,0);expect(layout.left).toBe(layout.headerLeft)
      expect(layout.overflow).toBeLessThanOrEqual(1);expect(Number(layout.weight)).toBeGreaterThanOrEqual(600)
    }
    task.turnId='next-turn';task.status='working';app.manager.publish(task,{type:'task',task})
    await expect(preview).toHaveAttribute('data-delivery-state','previous')
    await expect(preview.locator('strong')).toHaveText('上次交付 · V1 · 0 个文件')
    for(const width of [1440,390]) {
      await page.setViewportSize({width,height:800})
      const style=await preview.locator('.task-result-excerpt').evaluate(el=>({height:el.getBoundingClientRect().height,ellipsis:getComputedStyle(el).textOverflow,scroll:el.scrollLeft}))
      expect(style).toEqual({height:20,ellipsis:'ellipsis',scroll:0})
    }
  }finally{await app.close()}
})

test('configuration descriptions stay on home; uncertain submission stays recoverable and idle details explain their state',async({page})=>{
  const app=await fixture({localAssistants:true,attachmentModels:true})
  try {
    await page.goto(`${app.url}/#/tasks`)
    const group=page.getByRole('region',{name:'通用助手',exact:true})
    const description=await group.locator('.task-assistant-description').innerText()
    await group.getByRole('link',{name:'新建任务',exact:true}).click()
    await expect(page.getByRole('main')).not.toContainText(description)
    await page.route('**/api/tasks/*/prompts',route=>route.fulfill({status:503,contentType:'application/json',body:JSON.stringify({error:'暂不可用'})}))
    await page.getByLabel('标题',{exact:true}).fill('核对返回路径')
    await page.getByLabel('任务要求',{exact:true}).fill('保留待确认的任务要求')
    await page.getByRole('button',{name:'提交任务',exact:true}).click()
    await expect(page.getByRole('alert')).toContainText('提交结果尚未确认')
    await page.getByRole('link',{name:'全部任务',exact:true}).click()
    await expect(page.getByRole('main')).not.toContainText(description)
    await expect(page.getByRole('listitem',{name:'核对返回路径',exact:true})).toContainText('未开始')
    await page.getByRole('link',{name:'返回新建任务',exact:true}).click()
    await expect(page.getByLabel('任务要求',{exact:true})).toHaveValue('保留待确认的任务要求')
    await expect(page.getByRole('button',{name:'提交任务',exact:true})).toBeDisabled()
    const task=[...app.manager.tasks.values()][0]
    await page.goto(`${app.url}/#/tasks/${task.id}`)
    await expect(page).toHaveURL(`${app.url}/#/tasks/${task.id}`)
    await expect(page.getByRole('main')).toContainText('任务尚未开始')
    const titleLink=page.getByRole('heading',{name:'核对返回路径',exact:true}).getByRole('link')
    await expect(titleLink).toHaveAttribute('title','Chat details')
    await titleLink.click()
    await expect(page).toHaveURL(`${app.url}/#/chat/conversations/${task.id}`)
    await expect(page.locator('.root-title')).toHaveText('核对返回路径')
    await page.goto(`${app.url}/#/tasks/${task.id}/delivery`)
    await expect(page.getByRole('main')).toContainText('尚未提交交付')
    await expect(page.getByRole('link',{name:'查看任务',exact:true})).toHaveCount(0)
  } finally {await app.close()}
})

test('supplement delivery returns to its draft, history filter and scroll; direct links have a parent',async({page},info)=>{
  const app=await fixture({localAssistants:true,attachmentModels:true,delivery:true})
  try {
    const task=await app.manager.create({title:'T-Back · 核对图片',approvalMode:'auto'})
    app.manager.submit(task.id,'TEXT_DELIVERY')
    await until(()=>!app.manager.runs.has(task.id))
    expect(task.latestDelivery?.version).toBe(1)
    await page.goto(`${app.url}/#/tasks`)
    const group=page.getByRole('region',{name:'通用助手',exact:true})
    await group.getByRole('link',{name:'全部任务',exact:true}).click()
    await page.getByRole('button',{name:'全部',exact:true}).click()
    await group.getByRole('link',{name:'新建任务',exact:true}).click()
    await page.getByLabel('标题',{exact:true}).fill('保留的新任务')
    await page.getByRole('link',{name:'返回全部任务',exact:true}).click()
    await expect(page.getByRole('button',{name:'全部',exact:true})).toHaveAttribute('aria-pressed','true')
    await page.getByRole('link',{name:'补充任务',exact:true}).click()
    await page.getByLabel('补充要求',{exact:true}).fill('保留这段补充说明')
    await page.locator('input[type=file]').setInputFiles({name:'reference.txt',mimeType:'text/plain',buffer:Buffer.from('reference')})
    await page.getByRole('link',{name:'查看上次交付',exact:true}).click()
    await expect(page.getByRole('navigation',{name:'任务路径'})).toContainText('补充任务')
    await expect(page.getByRole('navigation',{name:'任务路径'}).locator('[aria-current=page]')).toContainText('上次交付')
    await page.getByRole('link',{name:'返回补充任务',exact:true}).click()
    await expect(page.getByLabel('补充要求',{exact:true})).toHaveValue('保留这段补充说明')
    await expect(page.getByRole('list',{name:'附件',exact:true})).toContainText('reference.txt')
    await page.getByRole('link',{name:'查看上次交付',exact:true}).click()
    await page.goBack()
    await expect(page.getByLabel('补充要求',{exact:true})).toHaveValue('保留这段补充说明')
    await page.getByRole('link',{name:'返回全部任务',exact:true}).click()
    await group.getByRole('link',{name:'新建任务',exact:true}).click()
    await expect(page.getByLabel('标题',{exact:true})).toHaveValue('保留的新任务')
    await page.goto(`${app.url}/#/tasks/${task.id}/delivery?from=supplement`)
    await page.reload()
    await expect(page.getByRole('link',{name:'返回补充任务',exact:true})).toBeVisible()
    await page.getByRole('link',{name:'返回补充任务',exact:true}).click()
    await expect(page.getByRole('heading',{name:'补充任务',exact:true})).toBeVisible()
    await page.setViewportSize({width:390,height:780})
    await page.getByRole('link',{name:'查看上次交付',exact:true}).click()
    expect(await page.getByRole('main').evaluate(el=>el.scrollWidth-el.clientWidth)).toBeLessThanOrEqual(1)
    await page.screenshot({path:info.outputPath('delivery-return-mobile.png')})
  } finally {await app.close()}
})

test('overview hierarchy and task columns align across widths; missing statistics are not zero',async({page},info)=>{
  const app=await fixture({localAssistants:true})
  try {
    for(let i=0;i<4;i++) {
      const task=await app.manager.create({title:`T-row${i} · ${i===0?'较长的任务名称：核对多份材料并整理交付，保持路径和状态清楚':'核对材料 '+i}`})
      task.turnCount=1
      if(i<2) {task.status='working';task.activity={phase:'processing',startedAt:Date.now()};app.manager.publish(task,{type:'state',task})}
      else {
        app.manager.append(task,{role:'assistant',blocks:[{id:'answer',type:'markdown',data:{markdown:'核对完成，已整理结论。'}}]})
        app.manager.settle(task,'completed')
      }
    }
    await page.goto(`${app.url}/#/tasks`)
    const group=page.getByRole('region',{name:'通用助手',exact:true})
    await expect(group.getByRole('region',{name:'进行中',exact:true})).toBeVisible()
    await expect(group.getByRole('region',{name:'最近结束',exact:true})).toBeVisible()
    await expect(group.getByRole('listitem')).toHaveCount(4)
    await expect(group.locator('.assistant-count')).toHaveText('进行中 2 个')
    for(const width of [1920,1280,760,390,320]) {
      await page.setViewportSize({width,height:900})
      expect(await page.getByRole('main').evaluate(el=>el.scrollWidth-el.clientWidth)).toBeLessThanOrEqual(1)
      if(width>=1280) {
        const rects=await group.locator('.task-row').evaluateAll(rows=>rows.map(row=>{
          const name=row.querySelector('.task-name')!.getBoundingClientRect(),status=row.querySelector('.task-status')!.getBoundingClientRect(),action=row.querySelector('.row-actions')!.getBoundingClientRect()
          return {x:status.x,ax:action.right,dy:Math.abs(status.y+status.height/2-action.y-action.height/2),overlap:status.right>name.left}
        }))
        expect(new Set(rects.map(r=>Math.round(r.x))).size).toBe(1)
        expect(new Set(rects.map(r=>Math.round(r.ax))).size).toBe(1)
        expect(rects.every(r=>r.dy<1&&!r.overlap)).toBe(true)
      } else if(width<=390) {
        const gaps=await group.locator('.task-row').evaluateAll(rows=>rows.map(row=>row.querySelector('.row-actions')!.getBoundingClientRect().top-row.querySelector('.task-row-info')!.getBoundingClientRect().bottom))
        expect(gaps.every(gap=>gap>=0)).toBe(true)
      }
      await page.screenshot({path:info.outputPath(`overview-${width}.png`)})
    }
    await page.route('**/api/tasks/*/statistics',route=>route.fulfill({status:503}))
    await page.reload()
    await expect(group.getByText('统计读取失败',{exact:true})).toHaveCount(4)
    await expect(group).not.toContainText('用时 未记录')
    await expect(group).not.toContainText('工具 0 次')
  } finally {await app.close()}
})

test('chat marks task-channel conversations on its second line and shows raw identity in diagnostics',async({page},info)=>{
  const app=await fixture({localAssistants:true})
  try {
    const task=await app.manager.create({title:'T-From · 核对任务来源'})
    const ordinary=await app.manager.create({title:'普通对话'})
    app.manager.settle(task,'completed');app.manager.settle(ordinary,'completed')
    await page.goto(`${app.url}/#/chat/conversations/${task.id}`)
    const row=page.getByTestId(`session-${task.id}`)
    await expect(row.locator('.session-detail').getByRole('img',{name:'来自任务频道'})).toBeVisible()
    await expect(row).not.toContainText('T-From')
    await expect(page.getByTestId(`session-${ordinary.id}`).getByRole('img',{name:'来自任务频道'})).toHaveCount(0)
    await page.getByTestId('session-menu').click()
    await page.getByTestId('diagnostics-open').click()
    const panel=page.getByRole('complementary',{name:'运行详情'})
    await expect(panel.getByRole('region',{name:'对话信息'})).toContainText('任务频道')
    await expect(panel.locator('.original-title')).toHaveText('T-From · 核对任务来源')
    await expect(panel).toContainText('创建时间')
    await page.screenshot({path:info.outputPath('chat-task-origin.png')})
    await page.getByRole('button',{name:'Close diagnostics'}).click()
    await page.getByRole('combobox',{name:'筛选助手'}).selectOption('root')
    await expect(row.locator('.session-detail').getByRole('img',{name:'来自任务频道'})).toBeVisible()
    // Source is derived before stripping the title; even the same display title
    // must invalidate the lightweight descriptor when its convention changes.
    task.title='核对任务来源';app.manager.publish(task,{type:'task',task})
    await expect(row.getByRole('img',{name:'来自任务频道'})).toHaveCount(0)
  } finally {await app.close()}
})

test('chat scrollport and composer share horizontal bounds with classic scrollbars and sidebar changes',async({page},info)=>{
  const app=await fixture()
  try {
    const task=await app.manager.create({title:'对齐核对'})
    for(let i=0;i<18;i++) app.manager.append(task,{role:'assistant',blocks:[{id:`text-${i}`,type:'markdown',data:{markdown:'核对宽屏消息与输入区域的边界。'.repeat(12)}}]})
    await page.goto(`${app.url}/#/chat/conversations/${task.id}`)
    // Headless Chromium normally uses overlay scrollbars. Reserve a real gutter
    // to exercise the Windows classic-scrollbar content-box difference.
    await expect(page.locator('.node-seat').first()).toBeVisible()
    await expect(page.locator('.workbench-composer')).toBeVisible()
    await page.addStyleTag({content:'.conversation-vlist {scrollbar-gutter:stable!important} .conversation-vlist::-webkit-scrollbar {width:16px} '})
    for(const width of [1920,1280,760,390]) {
      await page.setViewportSize({width,height:900})
      await expect(page.getByText('正在加载对话…',{exact:true})).toHaveCount(0)
      await expect.poll(()=>page.evaluate(()=>{
        const seat=document.querySelector('.node-seat')!,composer=document.querySelector('.workbench-composer')!,rect=seat.getBoundingClientRect(),box=composer.getBoundingClientRect(),style=getComputedStyle(seat)
        return Math.max(Math.abs(rect.left+parseFloat(style.paddingLeft)-box.left),Math.abs(rect.right-parseFloat(style.paddingRight)-box.right))
      })).toBeLessThan(1)
      await page.screenshot({path:info.outputPath(`chat-aligned-${width}.png`)})
    }
    await page.setViewportSize({width:1920,height:900})
    await page.getByRole('button',{name:'收起对话列表',exact:true}).click()
    await expect.poll(()=>page.evaluate(()=>{
      const seat=document.querySelector('.node-seat')!,r=seat.getBoundingClientRect(),box=document.querySelector('.workbench-composer')!.getBoundingClientRect()
      return Math.abs(r.left+parseFloat(getComputedStyle(seat).paddingLeft)-box.left)
    })).toBeLessThan(1)
    await expect(page.getByRole('navigation',{name:'任务路径'})).toHaveCount(0)
  } finally {await app.close()}
})
