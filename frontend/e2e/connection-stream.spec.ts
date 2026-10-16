import { expect, test } from '@playwright/test'
import { fixture } from '../../tests/fixtures/fixture.mjs'

test('large live tool events do not force reconnect or delay following messages', async ({ page }) => {
  const app = await fixture()
  let streams = 0
  page.on('request', request => { if (new URL(request.url()).pathname === '/api/events') streams++ })
  try {
    const task = await app.manager.create({ title: '大结果连接验收' })
    task.status = 'working'; task.activity = { phase: 'processing', startedAt: Date.now() }
    app.manager.append(task, { role: 'user', blocks: [{ id: 'prompt', type: 'markdown', data: { markdown: '等待工具返回' } }] })
    await page.goto(app.url)
    await expect(page.getByTestId('run-feedback')).toContainText('正在请求')
    expect(streams).toBe(1)
    // Native evidence and the normalized message/state can arrive in one tick.
    app.manager.publish(task, { type: 'native', event: { type: 'tool_call_response', response: 'x'.repeat(2 * 1024 * 1024) } })
    app.manager.append(task, { role: 'assistant', blocks: [{ id: 'after', type: 'markdown', data: { markdown: '大结果后的消息已到达' } }] })
    await expect(page.getByText('大结果后的消息已到达', { exact: true })).toBeVisible()
    await expect(page.getByTestId('run-feedback')).toContainText('正在请求')
    expect(streams).toBe(1)
  } finally { await app.close() }
})
