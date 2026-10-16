import { openSettings, closeSettings } from './settings'
import { expect, test } from '@playwright/test'
import { fixture } from '../../tests/fixtures/fixture.mjs'

test('mixed product rows remain compact and fully measured through disclosure and width changes',async({page},info)=>{
  const app=await fixture()
  try{
    const task=await app.manager.create({title:'混合条目密度验收'})
    for(let i=0;i<40;i++)app.manager.append(task,{role:'assistant',blocks:[{id:`history-${i}`,type:'tool-call',data:{callId:`history-${i}`,name:'read_file',input:{path:`history-${i}.md`},status:'success'}}]})
    app.manager.append(task,{role:'user',blocks:[{id:'user',type:'markdown',data:{markdown:'继续任务'}}]})
    app.manager.append(task,{role:'assistant',blocks:[
      {id:'call',type:'tool-call',data:{callId:'density-call',name:'shell',input:{command:'检查布局'},status:'success'}},
      {id:'terminal',type:'terminal',data:{command:'查看执行结果',output:'较长的终端输出\n'.repeat(60),status:'success'}},
      {id:'result',type:'tool-result',data:{callId:'density-call',name:'shell',output:'执行结果\n'.repeat(30),status:'success'}},
      {id:'thinking',type:'reasoning',data:{text:'检查消息高度和滚动位置。'.repeat(60),status:'complete'}},
      {id:'answer',type:'markdown',data:{markdown:'结果已核对。\n\n下一步继续验证。'}},
    ]})
    await page.goto(`${app.url}/#/chat/conversations/${task.id}`)
    await expect(page.getByTestId('conversation-loading')).toBeHidden()
    const geometry=()=>page.getByTestId('scrollport').evaluate(stage=>{
      const rows=[...stage.querySelectorAll<HTMLElement>('[data-virtual-item="true"]')].map(el=>({el,box:el.getBoundingClientRect()})).sort((a,b)=>a.box.top-b.box.top)
      return rows.reduce((max,{el,box},i)=>{
        const seat=el.querySelector('.node-seat')!.getBoundingClientRect(),style=getComputedStyle(el)
        return Math.max(max,Math.abs(seat.top-box.top),Math.abs(seat.bottom-box.bottom),Math.abs(parseFloat(style.marginTop)),Math.abs(parseFloat(style.paddingBottom)),i?Math.abs(rows[i-1]!.box.bottom-box.top):0)
      },0)
    })
    for(const width of [1365,390,320,840,1365]){
      await page.setViewportSize({width,height:760})
      const folded=page.locator('.agent-activity[data-open=false]')
      await expect.poll(()=>folded.count()).toBeGreaterThan(4)
      await expect.poll(()=>folded.evaluateAll(els=>Math.max(...els.map(el=>el.closest('.node-seat')!.getBoundingClientRect().height)))).toBeLessThanOrEqual(27)
      // Compact auxiliary rows retain a full-width, at least 26px target.
      await expect.poll(()=>folded.locator('.activity-summary').evaluateAll(els=>els.length?Math.min(...els.map(el=>el.getBoundingClientRect().height)):0)).toBeGreaterThanOrEqual(26)
      expect((await page.locator('.node-seat:has(> .role-user)').boundingBox())!.height).toBeLessThanOrEqual(50)
      await expect.poll(geometry).toBeLessThanOrEqual(1)
      for(const block of [page.locator('[data-call-id="density-call"]').first(),page.getByTestId('terminal-block'),page.locator('[data-call-id="density-call"]').last(),page.getByTestId('thinking-block')]){
        await block.locator('.activity-summary').click()
        await expect(block.locator('pre')).toBeVisible()
        const alignment=await block.evaluate(el=>{
          const heading=el.querySelector('.activity-summary')!.getBoundingClientRect(),link=el.parentElement!.querySelector('.message-link')!.getBoundingClientRect(),detail=el.querySelector('.activity-detail')!.getBoundingClientRect()
          return {gap:link.top-heading.bottom,inside:link.right<=detail.right&&link.bottom<=detail.bottom}
        })
        expect(alignment.gap).toBeGreaterThanOrEqual(2)
        expect(alignment.inside).toBe(true)
        expect((await block.locator('pre').boundingBox())!.height).toBeLessThanOrEqual(Math.min(760*.42,360)+1)
        await expect.poll(geometry).toBeLessThanOrEqual(1)
        await block.locator('.activity-summary').click()
        await expect(block.locator('pre')).toHaveCount(0)
        await expect.poll(geometry).toBeLessThanOrEqual(1)
      }
      expect(await page.evaluate(()=>document.body.scrollWidth-innerWidth)).toBeLessThanOrEqual(1)
      await page.screenshot({path:info.outputPath(`mixed-density-${width}.png`)})
    }
  }finally{await app.close()}
})

test('large session list stays bounded, searchable and keyboard reachable; streaming tails recover version gaps without empty rows', async ({page}) => {
  const app = await fixture()
  try {
    const task = await app.manager.create({ title:'流式密度验收' })
    app.request({op:'seedHistory',count:1200,workspace:task.workspace})
    const message = app.manager.append(task,{role:'assistant',live:true,blocks:[{id:'reason',type:'reasoning',revision:0,data:{text:'',status:'streaming'}},{id:'answer',type:'markdown',revision:0,data:{markdown:''}}]})
    const historyRequests: string[] = []
    page.on('request',request=>{if(request.url().includes('/messages?')) historyRequests.push(request.url())})
    await page.goto(app.url)
    await expect(page.getByTestId('composer-input')).toBeVisible()
    expect(await page.locator('.session-row').count()).toBeLessThan(50)
    await expect(page.getByTestId('markdown-block')).toHaveCount(0)
    await expect(page.getByTestId('thinking-block')).toHaveCount(0)
    expect(historyRequests).toHaveLength(1)
    const cold = app.manager.get('list-637')
    const old = app.store.message(cold.id,0)
    const oldBlock = old.blocks[0]!
    if (oldBlock.type !== 'markdown') throw new Error('Expected markdown history')
    oldBlock.data.markdown = '后台更新后的历史'
    app.manager.replace(cold,old)
    app.manager.append(cold,{role:'assistant',turnId:'history',blocks:[{id:'new',type:'markdown',data:{markdown:'后台新增消息'}}]})
    await page.getByTestId(`session-${task.id}`).press('End')
    await expect(page.getByTestId('session-list-0')).toBeFocused()
    expect(await page.locator('.session-row').count()).toBeLessThan(50)
    await page.getByLabel('搜索对话').fill('历史会话 0637')
    await expect(page.locator('.session-row')).toHaveCount(1)
    await page.getByTestId('session-list-637').click()
    await expect(page.getByTestId('session-list-637')).toHaveAttribute('aria-current','page')
    await expect(page.getByTestId('markdown-block').filter({hasText:'后台更新后的历史'})).toBeVisible()
    await expect(page.getByTestId('markdown-block').filter({hasText:'后台新增消息'})).toBeVisible()
    await page.getByLabel('搜索对话').fill('流式密度验收')
    await page.getByTestId(`session-${task.id}`).click()
    await page.getByLabel('搜索对话').fill('')
    app.manager.appendText(task,message,'reason','text','旧内容 '.repeat(10000)+'最新开始')
    const thought = page.getByTestId('thinking-block'), brief = thought.locator('.activity-brief')
    await expect(brief).toContainText('最新开始')
    const height = (await thought.boundingBox())!.height
    // Simulate a missed predecessor: persist v2 but only emit v3. The UI must fetch a snapshot.
    const reason = message.blocks[0]!
    if (reason.type !== 'reasoning') throw new Error('Expected reasoning block')
    reason.data.text += '缺失片段'; reason.revision = (reason.revision || 0) + 1; message.revision = (message.revision || 0) + 1
    app.store.saveMessage(task.id,message)
    let recoveryFailures = 0
    await page.route(`**/api/tasks/${task.id}/messages?start=0&count=1&*`,async route=>{
      if (recoveryFailures++ === 0) await route.fulfill({status:503,contentType:'application/json',body:JSON.stringify({error:'temporary history read failure'})})
      else await route.continue()
    })
    app.manager.appendText(task,message,'reason','text','恢复后的尾部')
    app.manager.appendText(task,message,'reason','text','后续事件不丢失')
    await expect(brief).toContainText('恢复后的尾部')
    await expect(brief).toContainText('后续事件不丢失')
    expect(recoveryFailures).toBeGreaterThanOrEqual(2)
    expect((await brief.textContent())!.length).toBeLessThanOrEqual(2048)
    expect((await thought.boundingBox())!.height).toBe(height)
    expect(await brief.evaluate(el => Math.abs(el.getBoundingClientRect().right - el.firstElementChild!.getBoundingClientRect().right))).toBeLessThanOrEqual(1)
    await expect(page.getByTestId('markdown-block')).toHaveCount(0)
    await thought.locator('button').click()
    await expect(thought.locator('pre')).toContainText('缺失片段恢复后的尾部')
    const detail = thought.locator('pre')
    await expect.poll(() => detail.evaluate(el => el.scrollHeight - el.clientHeight - el.scrollTop)).toBeLessThan(2)
    await detail.evaluate(el => { el.scrollTop = 0; el.dispatchEvent(new Event('scroll')) })
    app.manager.appendText(task,message,'reason','text','读旧内容时保持原位')
    await expect(detail).toContainText('读旧内容时保持原位')
    expect(await detail.evaluate(el => el.scrollTop)).toBeLessThan(2)
    await detail.evaluate(el => { el.scrollTop = el.scrollHeight; el.dispatchEvent(new Event('scroll')) })
    app.manager.appendText(task,message,'reason','text','\n新增末行'.repeat(100))
    await expect(detail).toContainText('新增末行')
    await expect.poll(() => detail.evaluate(el => el.scrollHeight - el.clientHeight - el.scrollTop)).toBeLessThan(2)
    await thought.locator('button').click()
    app.manager.appendText(task,message,'answer','markdown','这是正式答复。')
    await expect(page.getByTestId('markdown-block')).toContainText('这是正式答复。')
    const rows = await page.locator('[data-virtual-item=true]').evaluateAll(els=>els.map(e=>{const r=e.getBoundingClientRect();return {top:r.top,bottom:r.bottom}}).sort((a,b)=>a.top-b.top))
    for(let i=1;i<rows.length;i++) expect(rows[i-1]!.bottom-rows[i]!.top).toBeLessThanOrEqual(1)
    for (const width of [1440,390]) {
      await page.setViewportSize({width,height:900})
      expect(await page.evaluate(()=>document.body.scrollWidth-innerWidth)).toBeLessThanOrEqual(1)
      expect((await page.locator('.composer-shell').boundingBox())!.height).toBeLessThan(width===390?176:132)
      expect(await page.locator('.session-summary').evaluate(el=>el.getBoundingClientRect().height)).toBeLessThan(32)
    }
    await page.setViewportSize({width:1440,height:900})
    for(const theme of ['苔灰','暖砂','炭棕','曜黑（高对比）','石墨']) {
      await openSettings(page)
      await page.getByRole('navigation',{name:'设置分类'}).getByRole('link',{name:'外观',exact:true}).click()
      await page.getByRole('button',{name:`${theme}`}).click()
      expect(await page.locator('.settings-heading h2').evaluate(el=>getComputedStyle(el).fontSize)).toBe('16px')
      await closeSettings(page)
      expect(await page.getByTestId('mode-settings').evaluate(el=>getComputedStyle(el).fontSize)).toBe(await page.getByTestId('mode-chat').evaluate(el=>getComputedStyle(el).fontSize))
      expect(await page.locator('.session-row strong').first().evaluate(el=>getComputedStyle(el).fontSize)).toBe('13px')
    }
  } finally { await app.close() }
})
