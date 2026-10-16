import { expect, test, type Page } from '@playwright/test'
import { fixture } from '../../tests/fixtures/fixture.mjs'

const remaining = (page: Page) => page.locator('.conversation-vlist').evaluate(el => el.scrollHeight - el.clientHeight - el.scrollTop)

async function sampleSwitch(page: Page, sessionId: string, anchor: { id: string; top: number }) {
  await page.evaluate(({ sessionId, anchor }) => {
    const samples: { visible: boolean; drift: number | null; latest: boolean; rows: number }[] = []
    ;(window as any).__switchSamples = samples
    let count = 0
    const tick = () => {
      const shell = document.querySelector(`main[data-session-id="${sessionId}"]`)
      const list = shell?.querySelector('.conversation-vlist')
      if (list) {
        const row = [...list.querySelectorAll<HTMLElement>('[data-render-unit]')].find(el => el.dataset.renderUnit === anchor.id)
        samples.push({ visible: getComputedStyle(list).visibility === 'visible', drift: row ? Math.abs(row.getBoundingClientRect().top - anchor.top) : null, latest: !!shell?.querySelector('[data-testid="jump-latest"]'), rows: list.querySelectorAll('[data-render-unit]').length })
      }
      if (++count < 100) requestAnimationFrame(tick)
    }
    requestAnimationFrame(tick)
  }, { sessionId, anchor })
}

for (const deviceScaleFactor of [1, 2.5]) test.describe(`display scale ${deviceScaleFactor}`, () => {
test.use({ deviceScaleFactor })
test('wheel over Latest scrolls the conversation without requiring a click', async ({ page }) => {
  const app = await fixture()
  try {
    const task = await app.manager.create({ title: '悬浮按钮滚动' })
    app.manager.append(task, { role: 'assistant', blocks: [{ id: 'text', type: 'markdown', data: { markdown: '长正文\n\n'.repeat(100) } }] })
    await page.goto(`${app.url}/#/chat/conversations/${task.id}`)
    await expect.poll(() => remaining(page)).toBeLessThan(2)
    await page.locator('.conversation-vlist').hover()
    await page.mouse.wheel(0, -300)
    await expect.poll(() => remaining(page)).toBeGreaterThan(290)
    const before = await remaining(page)
    await page.getByTestId('jump-latest').hover()
    await page.mouse.wheel(0, -90)
    await expect.poll(() => remaining(page)).toBeGreaterThan(before + 1)
    await page.mouse.wheel(0, 10000)
    await expect.poll(() => remaining(page)).toBeLessThan(2)
  } finally { await app.close() }
})

test('expanded terminal scrolls internally then chains to the conversation at its boundary', async ({ page }) => {
  const app = await fixture()
  try {
    const task = await app.manager.create({ title: '终端滚动边界' })
    app.manager.append(task, { role: 'assistant', blocks: [{ id: 'terminal', type: 'terminal', data: { command: 'example', output: 'long terminal line\n'.repeat(120), status: 'success' } }] })
    app.manager.append(task, { role: 'assistant', blocks: [{ id: 'end', type: 'markdown', data: { markdown: '结尾正文\n\n'.repeat(20) } }] })
    await page.goto(`${app.url}/?inspect=1#/chat/conversations/${task.id}`)
    const terminal = page.getByTestId('terminal-block')
    await page.getByTestId('jump-input').fill('0')
    await page.getByTestId('jump-button').click()
    await expect(page.getByTestId('follow-state')).toHaveText('tail paused')
    await terminal.getByRole('button').click()
    const output = terminal.locator('pre')
    // Position the nested scroller in view through ordinary browser scrolling.
    await output.hover()
    await expect.poll(() => output.evaluate(el => el.scrollTop)).toBeLessThan(1)
    await page.mouse.wheel(0, 10000)
    await expect.poll(() => output.evaluate(el => el.scrollHeight - el.scrollTop - el.clientHeight)).toBeLessThan(2)
    await output.hover()
    // Start a separate gesture after Chromium releases the inner scroll latch.
    await page.waitForTimeout(250)
    await page.mouse.wheel(0, 10000)
    await expect.poll(() => remaining(page)).toBeLessThan(2)
    app.manager.append(task, { role: 'assistant', blocks: [{ id: 'new', type: 'markdown', data: { markdown: '后续终端正文\n\n'.repeat(30) + '终端边界跟随成功' } }] })
    await expect(page.getByText('终端边界跟随成功', { exact: true })).toBeInViewport()
  } finally { await app.close() }
})

test('downward wheel at the physical end resumes following with an expanded disclosure', async ({ page }) => {
  const app = await fixture()
  try {
    const task = await app.manager.create({ title: '展开后继续跟随' })
    app.manager.append(task, { role: 'assistant', blocks: [{ id: 'tool', type: 'tool-call', data: { name: 'read_file', callId: 'tail-tool', status: 'success', input: { path: 'input.json' } } }] })
    await page.goto(`${app.url}/#/chat/conversations/${task.id}`)
    await page.locator('[data-call-id="tail-tool"] .activity-summary').click()
    await page.locator('.conversation-vlist').hover({ position: { x: 15, y: 100 } })
    await page.mouse.wheel(0, -24)
    // Neither gesture can change the short list's scrollTop. Intent must still commit.
    await page.mouse.wheel(0, 500)
    await page.mouse.wheel(0, 500)
    app.manager.append(task, { role: 'assistant', blocks: [{ id: 'new', type: 'markdown', data: { markdown: '后续输出\n\n'.repeat(80) + '尾部已跟随' } }] })
    await expect(page.getByText('尾部已跟随', { exact: true })).toBeInViewport()
    await expect.poll(() => remaining(page)).toBeLessThan(2)
  } finally { await app.close() }
})

test('one small wheel gesture keeps reading intent through new content and layout measurements', async ({ page }) => {
  const app = await fixture()
  try {
    const task = await app.manager.create({ title: '小幅滚动控制权' })
    for (let i = 0; i < 90; i++) app.manager.append(task, { role: 'assistant', blocks: [{ id: `text-${i}`, type: 'markdown', data: { markdown: `消息 ${i}\n\n` + '有高度的正文。'.repeat(80) } }] })
    await page.goto(app.url)
    await expect.poll(() => remaining(page)).toBeLessThan(2)
    await page.locator('.conversation-vlist').hover()
    await page.mouse.wheel(0, -24)
    await expect.poll(() => remaining(page)).toBeGreaterThan(15)
    await expect(page.getByTestId('jump-latest')).toBeVisible()
    app.manager.append(task, { role: 'assistant', blocks: [{ id: 'new', type: 'markdown', data: { markdown: '新到达的正文\n\n'.repeat(15) } }] })
    await page.waitForTimeout(800)
    await expect(page.getByTestId('jump-latest')).toBeVisible()
    // While reading, appended messages may remain outside the bounded window.
    expect(await remaining(page)).toBeGreaterThan(15)
    await expect(page.getByTestId('messages-after')).toHaveText('1')
    await page.getByTestId('jump-latest').click()
    await expect(page.locator('.conversation-vlist')).toBeVisible()
    await expect.poll(() => remaining(page)).toBeLessThan(2)
    // Interrupt an explicit Latest navigation immediately, then change layout.
    await page.mouse.wheel(0, -60)
    await expect.poll(() => remaining(page)).toBeGreaterThan(40)
    await page.setViewportSize({ width: 1150, height: 760 })
    await page.waitForTimeout(800)
    await expect(page.getByTestId('jump-latest')).toBeVisible()
    expect(await remaining(page)).toBeGreaterThan(10)
  } finally { await app.close() }
})

test('session restoration never paints intermediate estimates or overwrites a rapid-switch checkpoint', async ({ page }) => {
  const app = await fixture()
  try {
    const task = await app.manager.create({ title: '长记录恢复' })
    app.store.transaction(() => {
      for (let i = 0; i < 900; i++) app.store.saveMessage(task.id, { id: `${task.id}:m-${i}`, index: i, turnId: 'turn', role: 'assistant', blocks: [{ id: `tool-${i}`, type: 'tool-call', data: { name: 'read_file', callId: `call-${i}`, status: 'success', input: {} } }] })
      task.logicalCount = 900; app.store.saveTask(task)
    })
    app.manager.append(task, { role: 'assistant', blocks: [{ id: 'long', type: 'markdown', data: { markdown: '## 长回答\n\n' + '长回答中的一段。\n\n'.repeat(45) } }] })
    const other = await app.manager.create({ title: '另一个对话' })
    await page.goto(app.url)
    await page.getByRole('link', { name: '长记录恢复 · 空闲', exact: true }).click()
    await expect(page.locator('.conversation-vlist')).toBeVisible()
    await expect.poll(() => remaining(page)).toBeLessThan(2)
    const beforeWheel = await page.locator('[data-render-unit]').last().evaluate(el => el.getBoundingClientRect().top)
    await page.locator('.conversation-vlist').hover(); await page.mouse.wheel(0, -350)
    await expect.poll(async () => (await page.locator('[data-render-unit]').last().boundingBox())!.y - beforeWheel).toBeGreaterThan(345)
    await page.waitForTimeout(200)
    expect((await page.locator('[data-render-unit]').last().boundingBox())!.y - beforeWheel).toBeLessThan(355)
    const saved = await page.locator('[data-render-unit]').last().evaluate(el => ({ id: el.getAttribute('data-render-unit')!, top: el.getBoundingClientRect().top }))
    for (let attempt = 0; attempt < 3; attempt++) {
      await page.getByRole('link', { name: '另一个对话 · 空闲', exact: true }).click()
      await expect(page.locator('main.conversation-shell')).toHaveAttribute('data-session-id', other.id)
      if (attempt === 1) {
        // Leave on the first mounted frame, before the restore can commit.
        await page.evaluate(id => {
          const observer = new MutationObserver(() => {
            if (document.querySelector('main.conversation-shell')?.getAttribute('data-session-id') !== id) return
            observer.disconnect()
            const button = [...document.querySelectorAll<HTMLButtonElement>('nav[aria-label="对话列表"] a')].find(el => el.textContent?.includes('另一个对话'))!
            button.click()
          })
          observer.observe(document.body, { childList:true, subtree:true })
        }, task.id)
        await page.getByRole('link', { name: '长记录恢复 · 空闲', exact: true }).click()
        await expect(page.locator('main.conversation-shell')).toHaveAttribute('data-session-id', other.id)
      }
      if (attempt === 2) await page.setViewportSize({ width:1050, height:720 })
      await sampleSwitch(page, task.id, saved)
      await page.getByRole('link', { name: '长记录恢复 · 空闲', exact: true }).click()
      await page.waitForTimeout(1800)
      const frames = await page.evaluate(() => (window as any).__switchSamples as { visible: boolean; drift: number | null; latest: boolean; rows: number }[])
      const painted = frames.filter(frame => frame.visible)
      expect(painted.length).toBeGreaterThan(20)
      expect(painted.every(frame => frame.drift !== null && frame.drift < 3)).toBe(true)
      expect(painted.every(frame => frame.latest)).toBe(true)
      expect(Math.max(...frames.map(frame => frame.rows))).toBeLessThan(100)
    }
  } finally { await app.close() }
})
})
