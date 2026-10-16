import { expect, test, type Page } from '@playwright/test'

function numeric(text: string | null): number { return Number((text ?? '').replace(/[^0-9-]/g, '')) }
async function open(page: Page) { await page.goto('./#/reference'); await expect(page.getByTestId('active-session-id')).toBeVisible() }
async function switchTo(page: Page, id: string) { await page.getByTestId(`session-${id}`).click(); await expect(page.getByTestId('active-session-id')).toHaveText(id) }
async function send(page: Page, text: string) { const input = page.getByTestId('composer-input'); await input.fill(text); await input.press('Enter') }
async function jump(page: Page, index: number) {
  await page.getByTestId('jump-input').fill(String(index))
  await page.getByTestId('jump-button').click()
  await expect(page.locator(`[data-message-index="${index}"]`).first()).toBeVisible({ timeout: 15_000 })
  await expect.poll(async () => Math.abs(numeric(await page.getByTestId('reader-position').textContent()) - index), { timeout: 15_000 }).toBeLessThan(64)
}

test('many asynchronous SessionKernels outlive the 3-hot viewport LRU', async ({ page }) => {
  await open(page)
  for (const [id, prompt] of [
    ['dsh-transport', 'continue the transport refactor'],
    ['event-normalization', 'normalize the next provider event'],
    ['workspace-files', 'continue the multimodal handoff'],
    ['dynamic-heights', 'resume the responsive artifact review'],
  ] as const) {
    await switchTo(page, id)
    await send(page, prompt)
    await expect(page.locator('.run-status')).toContainText('Working')
  }

  expect(numeric(await page.getByTestId('running-kernels').textContent())).toBeGreaterThanOrEqual(5)
  expect(numeric(await page.getByTestId('hot-sessions').textContent())).toBeLessThanOrEqual(3)
  await expect(page.getByTestId('session-dsh-transport')).toContainText('Working')
  await expect(page.getByTestId('session-dsh-transport')).toHaveClass(/unread/)

  await switchTo(page, 'dsh-transport')
  await expect(page.getByTestId('logical-count')).toHaveText('180,002')
  await expect.poll(async () => numeric(await page.getByTestId('stream-ticks').textContent())).toBeGreaterThan(2)
  expect(numeric(await page.getByTestId('hot-sessions').textContent())).toBeLessThanOrEqual(3)
})

test('a historical conversation can be resumed, stopped, resumed again, evicted and restored', async ({ page }) => {
  await open(page)
  await switchTo(page, 'event-normalization')
  await expect(page.getByTestId('logical-count')).toHaveText('95,000')

  await send(page, 'first continuation after historical restore')
  await expect(page.getByTestId('logical-count')).toHaveText('95,002')
  await page.getByTestId('composer-stop').click()
  await expect(page.locator('.run-status')).toContainText('Interrupted')

  await send(page, 'second continuation after stopping the first run')
  await expect(page.getByTestId('logical-count')).toHaveText('95,004')
  await expect(page.locator('[data-message-index="95002"]')).toContainText('second continuation')

  for (const id of ['dsh-transport', 'workspace-files', 'dynamic-heights', 'context-cache']) await switchTo(page, id)
  expect(numeric(await page.getByTestId('hot-sessions').textContent())).toBeLessThanOrEqual(3)

  await switchTo(page, 'event-normalization')
  await expect(page.getByTestId('logical-count')).toHaveText('95,004')
  await expect.poll(async () => numeric(await page.getByTestId('stream-ticks').textContent())).toBeGreaterThan(1)
  await jump(page, 95_002)
  await expect(page.locator('[data-message-index="95002"]')).toContainText('second continuation')
})

test('pending approval is session-owned and survives switching plus viewport eviction', async ({ page }) => {
  await open(page)
  await switchTo(page, 'tool-rendering')
  await expect(page.getByTestId('pending-interaction')).toContainText('Approve production config edit')

  for (const id of ['dsh-transport', 'event-normalization', 'workspace-files', 'dynamic-heights']) await switchTo(page, id)
  await switchTo(page, 'tool-rendering')
  await expect(page.getByTestId('pending-interaction')).toBeVisible()
  await page.getByTestId('approve-interaction').click()
  await expect(page.getByTestId('pending-interaction')).toHaveCount(0)
  // Resolving a blocker only clears session interaction state. The execution adapter,
  // not SessionKernel, decides whether the blocked Turn continues or completes.
  await expect(page.locator('.run-status')).toContainText('Idle')
  await expect(page.getByTestId('last-turn-reason')).toHaveText('none')

  await send(page, 'continue after approval')
  await expect(page.getByTestId('logical-count')).toHaveText('420,002')
  await expect(page.locator('.run-status')).toContainText('Working')
})

test('working sessions queue follow-ups independently of the mounted viewport', async ({ page }) => {
  await open(page)
  await expect(page.getByTestId('active-session-id')).toHaveText('agent-loop')
  await expect(page.locator('.run-status')).toContainText('Working')
  await send(page, 'follow up after the current renderer investigation')
  await expect(page.getByTestId('queue-banner')).toContainText('1 follow-up')
  await expect(page.getByTestId('queued-prompts')).toHaveText('1')

  await switchTo(page, 'dsh-transport')
  await expect(page.getByTestId('session-agent-loop')).toContainText('1 queued')
  await switchTo(page, 'agent-loop')
  await expect(page.getByTestId('queue-banner')).toContainText('1 follow-up')
})

test('New session is a real empty resumable conversation and the public sidebar stays scenario-focused', async ({ page }) => {
  await open(page)
  await expect(page.getByTestId('session-search')).toHaveCount(0)
  await expect(page.getByTestId('scenario-launch')).toHaveCount(0)

  await page.getByTestId('new-session').click()
  await expect(page.getByTestId('active-session-id')).toHaveText('new-1')
  await expect(page.getByTestId('empty-conversation')).toBeVisible()
  await expect(page.locator('.run-status')).toContainText('Idle')
  await expect(page.getByTestId('last-turn-reason')).toHaveText('none')
  await send(page, 'implement a fresh agent task')
  await expect(page.getByTestId('logical-count')).toHaveText('2')
  await expect(page.locator('[data-message-index="0"]')).toContainText('implement a fresh agent task')

  await switchTo(page, 'dsh-transport')
  await expect(page.getByText('Transport refactor verified', { exact: true })).toBeVisible({ timeout: 15_000 })
})
