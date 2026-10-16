import { openSettings, closeSettings } from './settings'
import { expect, test, type Page } from '@playwright/test'
import { fixture } from '../../tests/fixtures/fixture.mjs'

const remaining = (page: Page) => page.locator('.conversation-vlist').evaluate(el => Math.max(0, el.scrollHeight - el.clientHeight - el.scrollTop))
const anchor = (page: Page) => page.getByTestId('scrollport').evaluate(stage => {
  const top = stage.getBoundingClientRect().top
  const row = [...stage.querySelectorAll<HTMLElement>('[data-render-unit]')].find(el => el.getBoundingClientRect().top >= top && el.getBoundingClientRect().bottom <= stage.getBoundingClientRect().bottom)
  return row ? { id: row.dataset.renderUnit!, top: row.getBoundingClientRect().top } : null
})
const drift = (page: Page, saved: { id: string; top: number }) => page.locator(`[data-render-unit="${saved.id}"]`).evaluate((el, top) => Math.abs(el.getBoundingClientRect().top - top), saved.top)

test('stop feedback persists until HTTP confirmation after the task becomes idle',async({page})=>{
  const app=await fixture()
  let release!:()=>void
  const gate=new Promise<void>(done=>{release=done})
  try{
    const task=await app.manager.create({title:'停止确认反馈'})
    task.status='working';app.store.saveTask(task)
    await page.goto(`${app.url}/#/chat/conversations/${task.id}`)
    await page.route('**/api/tasks/*/abort',async route=>{await gate;await route.fulfill({status:200,contentType:'application/json',body:'{"ok":true}'})})
    await page.getByTestId('composer-stop').click()
    await expect(page.getByTestId('run-feedback')).toContainText('正在停止')
    app.manager.settle(task,'interrupted',{code:'user_stop',message:'已停止'})
    await expect(page.getByTestId('composer-stop')).toHaveCount(0)
    await expect(page.getByTestId('run-feedback')).toContainText('正在停止')
    release()
    await expect(page.getByTestId('run-feedback')).toHaveCount(0)
  }finally{release();await app.close()}
})

test('send acknowledges immediately and feedback spans first output and compaction gaps', async ({ page }) => {
  let release!: () => void
  const gate = new Promise<void>(resolve => { release = resolve })
  const app = await fixture({ beforeResponse: () => gate })
  try {
    const task = await app.manager.create({ title: '首字反馈验收' })
    await page.goto(app.url)
    let releaseHttp!: () => void
    const httpGate = new Promise<void>(resolve => { releaseHttp = resolve })
    await page.route('**/api/tasks/*/prompts', async route => { await httpGate; await route.continue() })
    await page.getByTestId('composer-input').fill('WAIT_FOR_FIRST_OUTPUT')
    await page.getByTestId('send-message').click()
    await expect(page.getByTestId('send-message')).toHaveText('发送中…')
    await expect(page.getByTestId('run-feedback')).toContainText('正在发送')
    releaseHttp()
    await expect(page.getByTestId('composer-input')).toHaveValue('')
    await expect(page.getByTestId('run-feedback')).toContainText('正在请求')
    await expect(page.getByTestId('composer-stop')).toBeEnabled()
    await expect(page.getByTestId('jump-latest')).toHaveCount(0)
    expect(task.logicalCount).toBe(1)
    expect(app.store.message(task.id, 0).role).toBe('user')
    const box = await page.getByTestId('scrollport').boundingBox()
    await app.manager.normalizer.handle(task, { type: 'session_compaction', session_id: task.nativeSessionId, status: 'started' })
    await expect(page.getByTestId('run-feedback')).toContainText('正在压缩上下文')
    await app.manager.normalizer.handle(task, { type: 'session_compaction', session_id: task.nativeSessionId, status: 'completed', outcome: 'applied' })
    await expect(page.getByTestId('run-feedback')).toContainText('压缩完成，继续处理中')
    expect(await page.getByTestId('scrollport').boundingBox()).toEqual(box)
    release()
    await expect(page.getByTestId('run-feedback')).toHaveCount(0)
    await expect(page.getByTestId('jump-latest')).toHaveCount(0)
  } finally { release(); await app.close() }
})

test('Latest follows physical tail across silent steps, keeps reader anchor and does not rerender history on timer ticks', async ({ page }) => {
  const app = await fixture()
  try {
    const task = await app.manager.create({ title: 'Latest 与运行状态验收' })
    app.store.transaction(() => {
      for (let index = 0; index < 350; index++) app.store.saveMessage(task.id, { id: `${task.id}:m-${index}`, index, turnId: `turn-${index}`, role: 'assistant', blocks: [{ id: `text-${index}`, type: 'markdown', data: { markdown: `记录 ${index}\n\n` + '用于验证阅读位置与实际行高。'.repeat(12) } }] })
      task.logicalCount = 350; app.store.saveTask(task)
    })
    await page.goto(app.url)
    await expect.poll(() => remaining(page)).toBeLessThan(2)
    await expect(page.getByTestId('jump-latest')).toHaveCount(0)
    await app.manager.normalizer.handle(task, { type: 'stream_started', session_id: task.nativeSessionId })
    await expect(page.getByTestId('run-feedback')).toBeVisible()
    await expect.poll(() => remaining(page)).toBeLessThan(2)
    const activeHeight = (await page.getByTestId('scrollport').boundingBox())!.height
    await app.manager.normalizer.handle(task, { type: 'session_compaction', session_id: task.nativeSessionId, status: 'started' })
    await app.manager.normalizer.handle(task, { type: 'session_compaction', session_id: task.nativeSessionId, status: 'completed', outcome: 'applied' })
    await expect(page.getByTestId('run-feedback')).toContainText('继续处理中')
    expect((await page.getByTestId('scrollport').boundingBox())!.height).toBe(activeHeight)
    await page.locator('.conversation-vlist').hover(); await page.mouse.wheel(0, -1000)
    await expect.poll(() => remaining(page)).toBeGreaterThan(500)
    await expect(page.getByTestId('jump-latest')).toBeVisible()
    const saved = (await anchor(page))!
    task.activePlan={items:Array.from({length:24},(_,i)=>({id:`layout-plan-${i}`,text:`阅读中的计划 ${i+1}`,status:i===0?'in-progress':'pending'}))}
    app.manager.publish(task,{type:'state',task})
    const plan=page.getByTestId('active-plan-summary')
    await expect(plan).toBeVisible();await expect.poll(()=>drift(page,saved)).toBeLessThan(3)
    await plan.click();await expect(page.getByTestId('active-plan-popover')).toBeVisible()
    await expect.poll(()=>drift(page,saved)).toBeLessThan(3)
    await plan.click();await expect(page.getByTestId('active-plan-popover')).toBeHidden()
    task.lastFailure={code:'layout_error',message:'阅读期间出现的错误提示'};app.manager.publish(task,{type:'state',task})
    await expect(page.getByTestId('last-turn-failure')).toBeVisible();await expect.poll(()=>drift(page,saved)).toBeLessThan(3)
    task.lastFailure=null;app.manager.publish(task,{type:'state',task})
    await expect(page.getByTestId('last-turn-failure')).toHaveCount(0);await expect.poll(()=>drift(page,saved)).toBeLessThan(3)
    const beforeCount = task.logicalCount
    const afterCount = Number((await page.getByTestId('jump-latest').locator('em').textContent())!.replaceAll(',', ''))
    await app.manager.normalizer.handle(task, { type: 'tool_call', session_id: task.nativeSessionId, tool_call: { id: 'tool', function: { name: 'test_tool', arguments: '{}' } } })
    await app.manager.normalizer.handle(task, { type: 'tool_call_response', session_id: task.nativeSessionId, tool_call_id: 'tool', response: 'ok' })
    await expect(page.getByTestId('jump-latest').locator('em')).toHaveText(String(afterCount + 2))
    await expect(page.getByTestId('run-feedback')).toContainText('正在请求')
    await expect.poll(() => drift(page, saved)).toBeLessThan(3)
    const time = await page.getByTestId('run-feedback-time').textContent()
    await page.evaluate(() => {
      ;(window as any).__feedbackMutations = 0
      const observer = new MutationObserver(records => { (window as any).__feedbackMutations += records.length })
      // Observe message content, not Virtua's delayed is-scrolling/position styles.
      for (const seat of document.querySelectorAll('.conversation-vlist .node-seat')) observer.observe(seat, { subtree: true, childList: true, attributes: true, characterData: true })
      ;(window as any).__feedbackObserver = observer
    })
    await expect(page.getByTestId('run-feedback-time')).not.toHaveText(time!)
    expect(await page.evaluate(() => (window as any).__feedbackMutations)).toBe(0)
    await page.evaluate(() => (window as any).__feedbackObserver.disconnect())
    expect(task.logicalCount).toBe(beforeCount + 2)
    expect(await page.locator('[data-virtual-item]').count()).toBeLessThan(100)
    app.manager.settle(task, 'completed')
    await expect(page.getByTestId('run-feedback')).toHaveCount(0)
    await expect.poll(() => drift(page, saved)).toBeLessThan(3)
    const latest = page.getByTestId('jump-latest')
    const button = (await latest.boundingBox())!, stage = (await page.getByTestId('scrollport').boundingBox())!
    expect(button.y + button.height).toBeLessThanOrEqual(stage.y + stage.height)
    await latest.click(); await expect(latest).toHaveCount(0)
    await expect.poll(() => remaining(page)).toBeLessThan(2)
    // New messages while idle and silent tool/model gaps must also follow.
    app.manager.append(task, { role: 'assistant', blocks: [{ id: 'late', type: 'markdown', data: { markdown: '追加内容\n\n'.repeat(20) } }] })
    // Wait for SSE delivery before testing a scroll inside the new final row.
    // The old DOM is already at the bottom and cannot acknowledge this append.
    await expect(page.locator('.markdown-body').filter({hasText:'追加内容'}).last()).toBeVisible()
    await expect.poll(() => remaining(page)).toBeLessThan(2)
    await expect(latest).toHaveCount(0)
    // Reading the upper part of the last tall message still needs Latest,
    // even though the logical messages-after count is zero.
    await page.locator('.conversation-vlist').hover(); await page.mouse.wheel(0, -160)
    await expect.poll(() => remaining(page)).toBeGreaterThan(100)
    await expect(latest).toBeVisible()
    await expect(latest.locator('em')).toHaveCount(0)
    await latest.click(); await expect.poll(() => remaining(page)).toBeLessThan(2)
  } finally { await app.close() }
})

test('waiting feedback is bounded across themes, small screens and reconnects', async ({ page }) => {
  const app = await fixture()
  try {
    const task = await app.manager.create({ title: '状态高度验收' })
    task.status = 'working'; task.activity = { phase: 'processing', startedAt: Date.now() - 5000 }
    app.manager.append(task, { role: 'user', blocks: [{ id: 'prompt', type: 'markdown', data: { markdown: '等待回答' } }] })
    await page.goto(app.url)
    await expect(page.getByTestId('run-feedback')).toContainText('正在请求')
    await expect(page.locator('.run-feedback-icon')).toHaveClass(/animated/)
    expect(await page.locator('.run-feedback-icon').evaluate(el => getComputedStyle(el).width)).toBe('16px')
    expect(await page.locator('.run-feedback-icon').evaluate(el => getComputedStyle(el).animationName)).toMatch(/^tuck-breathe/)
    for (const theme of ['墨蓝', '苔灰', '暖砂','炭棕', '曜黑（高对比）', '石墨']) {
      await openSettings(page)
      await page.getByRole('navigation',{name:'设置分类'}).getByRole('link',{name:'外观',exact:true}).click()
      await page.getByRole('button', { name: `${theme}` }).click()
      await closeSettings(page)
      expect((await page.getByTestId('run-feedback').boundingBox())!.height).toBe(20)
      expect(await page.getByTestId('run-feedback').evaluate(el => getComputedStyle(el).justifyContent)).toBe('flex-start')
    }
    for (const width of [390, 830, 1365]) {
      await page.setViewportSize({ width, height: 740 })
      await expect(page.getByTestId('run-feedback')).toBeInViewport()
      await expect(page.getByTestId('composer-input')).toBeInViewport()
      expect(await page.evaluate(() => document.body.scrollWidth - innerWidth)).toBeLessThanOrEqual(1)
      await expect(page.getByTestId('jump-latest')).toHaveCount(0)
    }
    await page.emulateMedia({ reducedMotion: 'reduce' })
    expect(await page.locator('.run-feedback-icon').evaluate(el => getComputedStyle(el).animationName)).toBe('none')
    await page.context().setOffline(true); app.server.closeAllConnections()
    await expect(page.getByTestId('run-feedback')).toContainText('连接中断，正在同步')
    await expect(page.locator('.run-feedback-icon')).not.toHaveClass(/animated/)
    await expect(page.getByTestId('run-feedback-time')).toHaveCount(0)
    await page.context().setOffline(false)
    await expect(page.getByTestId('run-feedback')).toContainText('正在请求')
  } finally { await app.close() }
})

test('Latest reaches the rendered end even when the final logical message is an empty streaming placeholder', async ({ page }) => {
  const app = await fixture()
  try {
    const task = await app.manager.create({ title: '隐藏的首字占位' })
    for (let index = 0; index < 80; index++) app.manager.append(task, { role: 'assistant', blocks: [{ id: `text-${index}`, type: 'markdown', data: { markdown: `历史 ${index}\n\n` + '正文。'.repeat(100) } }] })
    await page.goto(app.url)
    await expect.poll(() => remaining(page)).toBeLessThan(2)
    await page.locator('.conversation-vlist').hover(); await page.mouse.wheel(0, -1000)
    await expect(page.getByTestId('jump-latest')).toBeVisible()
    app.manager.append(task, { role: 'assistant', blocks: [{ id: 'empty', type: 'markdown', data: { markdown: '' } }] })
    await expect(page.getByTestId('jump-latest')).toBeVisible()
    await page.getByTestId('jump-latest').click()
    await expect(page.getByTestId('jump-latest')).toHaveCount(0)
    await expect.poll(() => remaining(page)).toBeLessThan(2)
    app.manager.append(task, { role: 'assistant', blocks: [{ id: 'reply', type: 'markdown', data: { markdown: '首字终于到达\n\n'.repeat(20) } }] })
    await expect(page.getByText('首字终于到达', { exact: true }).last()).toBeInViewport()
    await expect(page.getByTestId('jump-latest')).toHaveCount(0)
  } finally { await app.close() }
})

for (const navigation of ['wheel', 'jump'] as const) test(`manual ${navigation} history navigation wins over a delayed send acknowledgement`, async ({ page }) => {
  let releaseModel!: () => void, releaseHttp!: () => void
  const modelGate = new Promise<void>(resolve => { releaseModel = resolve })
  const httpGate = new Promise<void>(resolve => { releaseHttp = resolve })
  const app = await fixture({ beforeResponse: () => modelGate })
  try {
    const task = await app.manager.create({ title: '发送等待期间阅读历史' })
    for (let index = 0; index < 90; index++) app.manager.append(task, { role: 'assistant', blocks: [{ id: `old-${index}`, type: 'markdown', data: { markdown: `历史 ${index}\n\n` + '正文。'.repeat(80) } }] })
    await page.goto(app.url + (navigation === 'jump' ? '/?inspect=1' : ''))
    await expect.poll(() => remaining(page)).toBeLessThan(2)
    await page.route('**/api/tasks/*/prompts', async route => { const response = await route.fetch(); await httpGate; await route.fulfill({ response }) })
    await page.getByTestId('composer-input').fill('NEW_WAIT')
    await page.getByTestId('send-message').click()
    await expect(page.getByTestId('run-feedback')).toContainText('正在请求')
    if (navigation === 'jump') {
      await page.getByTestId('jump-input').fill('20')
      await page.getByTestId('jump-button').click()
      await expect(page.locator('[data-message-index="20"]').first()).toBeInViewport()
    } else { await page.locator('.conversation-vlist').hover(); await page.mouse.wheel(0, -1000) }
    await expect.poll(() => remaining(page)).toBeGreaterThan(500)
    const saved = (await anchor(page))!
    releaseHttp()
    await expect(page.getByTestId('composer-input')).toHaveValue('')
    await expect.poll(() => drift(page, saved)).toBeLessThan(3)
    await expect(page.getByTestId('jump-latest')).toBeVisible()
    releaseModel()
    await expect(page.getByTestId('run-feedback')).toHaveCount(0)
    await expect.poll(() => drift(page, saved)).toBeLessThan(3)
    if (navigation === 'jump') await expect(page.locator('[data-message-index="20"]').first()).toBeInViewport()
    await page.getByTestId('jump-latest').click()
    await expect.poll(() => remaining(page)).toBeLessThan(2)
    await expect(page.getByTestId('jump-latest')).toHaveCount(0)
  } finally { releaseHttp(); releaseModel(); await app.close() }
})

test('folding tools and incidental pointer clicks never turn a pinned reader into history mode', async ({ page }) => {
  const app = await fixture()
  try {
    const task = await app.manager.create({ title: '底部工具折叠' })
    for (let index = 0; index < 60; index++) app.manager.append(task, { role: 'assistant', blocks: [{ id: `tool-${index}`, type: 'tool-call', data: { name: 'read_file', callId: `call-${index}`, input: { details: '工具内容\n'.repeat(100) }, status: 'success' } }] })
    await page.goto(app.url)
    await expect.poll(() => remaining(page)).toBeLessThan(2)
    const last = page.locator('[data-message-index="59"]').getByTestId('tool-block')
    // Ordinary pointer clicks used to arm scroll intent. Subsequent virtual
    // measurement scroll events must not be mistaken for reading history.
    await last.getByRole('button').click()
    await expect(last.locator('pre')).toBeVisible()
    await expect.poll(() => remaining(page)).toBeLessThan(2)
    await last.getByRole('button').click()
    await expect(last.locator('pre')).toHaveCount(0)
    await expect.poll(() => remaining(page)).toBeLessThan(2)
    await expect(page.getByTestId('jump-latest')).toHaveCount(0)
    app.manager.append(task, { role: 'assistant', blocks: [{ id: 'after-fold', type: 'markdown', data: { markdown: '折叠后的新回答\n\n'.repeat(20) } }] })
    await expect(page.getByText('折叠后的新回答', { exact: true }).last()).toBeInViewport()
    await expect.poll(() => remaining(page)).toBeLessThan(2)
    await expect(page.getByTestId('jump-latest')).toHaveCount(0)
    // Return by scrolling, not just by pressing Latest.
    await page.locator('.conversation-vlist').hover(); await page.mouse.wheel(0, -500)
    await expect(page.getByTestId('jump-latest')).toBeVisible()
    await page.mouse.wheel(0, 10000)
    await expect.poll(() => remaining(page)).toBeLessThan(2)
    await expect(page.getByTestId('jump-latest')).toHaveCount(0)
    await page.locator('.conversation-vlist').press('PageUp')
    await expect(page.getByTestId('jump-latest')).toBeVisible()
    await page.locator('.conversation-vlist').press('End')
    await expect.poll(() => remaining(page)).toBeLessThan(2)
    await expect(page.getByTestId('jump-latest')).toHaveCount(0)
  } finally { await app.close() }
})
