import { expect, test, type Page } from '@playwright/test'

async function open(page: Page) { await page.goto('./#/reference'); await expect(page.getByTestId('active-session-id')).toBeVisible() }
async function switchTo(page: Page, id: string) { await page.getByTestId(`session-${id}`).click(); await expect(page.getByTestId('active-session-id')).toHaveText(id) }
async function send(page: Page, text: string) { const input = page.getByTestId('composer-input'); await input.fill(text); await input.press('Enter') }

test('Recent separates live execution, blockers and last-turn outcomes', async ({ page }) => {
  await open(page)
  await expect(page.getByTestId('session-agent-loop')).toContainText('Working')
  await expect(page.getByTestId('session-million')).toContainText('Working')
  await expect(page.getByTestId('session-office-briefing')).toContainText('Completed')
  await expect(page.getByTestId('session-office-followup')).toContainText(/Blocked.*Needs approval/i)
  await expect(page.getByTestId('session-dsh-transport')).toContainText('Completed')
  await expect(page.getByTestId('session-tool-rendering')).toContainText(/Blocked.*Needs approval/i)
  await expect(page.getByTestId('session-android-protocol')).toContainText(/Blocked.*Needs answer/i)
  await expect(page.getByTestId('session-dynamic-heights')).toContainText('Interrupted')
  await expect(page.getByTestId('session-context-cache')).toContainText(/Failed.*PROVIDER_TIMEOUT/)
  await expect(page.getByTestId('running-kernels')).toHaveText(/[2-9]/)
  await expect(page.getByTestId('blocked-sessions')).toHaveText('3')
  await expect(page.getByTestId('failed-sessions')).toHaveText('1')
})

test('a failed last turn is resumable and new execution clears the failure surface', async ({ page }) => {
  await open(page)
  await switchTo(page, 'context-cache')
  await expect(page.locator('.run-status')).toContainText('Failed')
  await expect(page.getByTestId('last-turn-failure')).toContainText('PROVIDER_TIMEOUT')
  await expect(page.getByTestId('last-turn-reason')).toHaveText('error')

  await send(page, 'resume after the provider timeout')
  await expect(page.locator('.run-status')).toContainText('Working')
  await expect(page.getByTestId('last-turn-failure')).toHaveCount(0)
  await expect(page.getByTestId('logical-count')).toHaveText('700,002')
  await expect(page.getByTestId('last-turn-reason')).toHaveText('none')
})

test('approval and question use distinct session-owned resolution contracts', async ({ page }) => {
  await open(page)
  await switchTo(page, 'android-protocol')
  const question = page.getByTestId('pending-interaction')
  await expect(question).toHaveAttribute('data-kind', 'question')
  await expect(question).toContainText('Choose Android fallback behavior')
  await expect(page.getByTestId('question-answer')).toBeVisible()
  await expect(page.getByTestId('approve-interaction')).toHaveText('Answer')
  await expect(page.getByTestId('approve-interaction')).toBeDisabled()
  await expect(page.getByTestId('composer-input')).toBeDisabled()

  // The blocker is session-owned and survives viewport/runtime eviction with the
  // answer UI reconstructed from canonical blocker type, not from local component state.
  for (const id of ['dsh-transport', 'event-normalization', 'workspace-files', 'dynamic-heights']) await switchTo(page, id)
  await switchTo(page, 'android-protocol')
  await expect(page.getByTestId('pending-interaction')).toHaveAttribute('data-kind', 'question')
  await page.getByTestId('question-answer').fill('Keep the last accepted configuration for one refresh window.')
  await expect(page.getByTestId('approve-interaction')).toBeEnabled()
  await page.getByTestId('approve-interaction').click()
  await expect(page.getByTestId('pending-interaction')).toHaveCount(0)
  await expect(page.getByTestId('composer-input')).toBeEnabled()
  await expect(page.locator('.run-status')).toContainText('Idle')
  await expect(page.getByTestId('last-turn-reason')).toHaveText('none')

  await switchTo(page, 'tool-rendering')
  await expect(page.getByTestId('pending-interaction')).toHaveAttribute('data-kind', 'approval')
  await expect(page.getByTestId('question-answer')).toHaveCount(0)
  await expect(page.getByTestId('approve-interaction')).toHaveText('Approve')
})

test('token, cache and context projections are session-owned and update during a turn', async ({ page }) => {
  await open(page)
  await switchTo(page, 'event-normalization')

  const inputBefore = await page.getByTestId('stats-input-tokens').textContent()
  const outputBefore = await page.getByTestId('stats-output-tokens').textContent()
  await expect(page.getByTestId('stats-cache-hit')).toContainText(/cache \d+%/)
  await expect(page.getByTestId('stats-context')).toContainText(/\d+% context/)

  await send(page, 'measure durable token accounting after this new prompt')
  await expect.poll(async () => await page.getByTestId('stats-input-tokens').textContent()).not.toBe(inputBefore)
  await expect.poll(async () => await page.getByTestId('stats-output-tokens').textContent()).not.toBe(outputBefore)

  const outputDuring = await page.getByTestId('stats-output-tokens').textContent()
  await switchTo(page, 'workspace-files')
  await switchTo(page, 'event-normalization')
  await expect(page.getByTestId('stats-output-tokens')).not.toHaveText(outputBefore ?? '')
  await expect(page.getByTestId('stats-cache-hit')).toContainText(/cache \d+%/)
  expect(outputDuring).not.toBeNull()
})
