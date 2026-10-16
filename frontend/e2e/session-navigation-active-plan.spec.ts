import { expect, test, type Page } from '@playwright/test'

function numeric(text: string | null): number {
  return Number((text ?? '').replace(/[^0-9.-]/g, ''))
}

async function logicalCount(page: Page): Promise<number> {
  return numeric(await page.getByTestId('logical-count').textContent())
}

async function completeAgentReference(page: Page): Promise<void> {
  await page.goto('./#/reference')
  const pause = page.getByRole('button', { name: 'Pause' })
  if (await pause.isVisible()) await pause.click()
  await page.getByLabel('Stream rate').selectOption('60')
  await page.getByTestId('stream-start').click()
  await expect.poll(() => logicalCount(page), { timeout: 25_000 }).toBeGreaterThanOrEqual(84_009)
  await expect(page.locator('.run-status')).toContainText('Completed', { timeout: 25_000 })
}

test('active work-plan strip stays synchronized with producer plan snapshots', async ({ page }) => {
  await page.goto('./#/reference')

  const strip = page.getByTestId('active-plan-strip')
  await expect(strip).toBeVisible()
  await expect(strip).toHaveAttribute('data-total', '4')
  await expect(page.getByTestId('active-plan-summary')).toContainText(/Inspect|Correlate|Run the full release gate|Summarize/)

  await page.getByTestId('active-plan-summary').click()
  const popover = page.getByTestId('active-plan-popover')
  await expect(popover).toBeVisible()
  await expect(popover.locator('li')).toHaveCount(4)

  const pause = page.getByRole('button', { name: 'Pause' })
  if (await pause.isVisible()) await pause.click()
  await page.getByLabel('Stream rate').selectOption('60')
  await page.getByTestId('stream-start').click()

  await expect(strip).toHaveAttribute('data-completed', '4', { timeout: 30_000 })
  await expect(page.getByTestId('active-plan-summary')).toContainText('All plan items completed')
  await expect(page.locator('.run-status')).toContainText('Completed', { timeout: 30_000 })

  await page.getByTestId('reference-agent-plan').click()
  const historyPlan = page.locator('[data-message-index="83999"]').getByTestId('plan-block')
  await expect(historyPlan.locator('[data-plan-status="completed"]')).toHaveCount(4)
  await expect(strip).toHaveAttribute('data-completed', '4')
})

test('delegation opens a real child conversation and Host navigation returns to parent', async ({ page }) => {
  await completeAgentReference(page)
  await page.getByTestId('reference-agent-delegation').click()

  const childRun = page.locator('[data-testid="delegation-run"][data-child-session-id="child-review-contract"]')
  await expect(childRun).toBeVisible()
  await childRun.click()

  await expect(page.getByTestId('active-session-id')).toHaveText('child-review-contract')
  await expect(page.locator('.conversation-title')).toContainText('Review rendering contract')
  await expect(page.getByTestId('recent-sessions').locator('[data-testid="session-child-review-contract"]')).toHaveCount(0)
  await expect(page.getByText('Child review result', { exact: true })).toBeVisible()
  await expect(page.getByText(/detailed child transcript/i)).toBeVisible()
  await expect(page.getByTestId('parent-session-link')).toContainText('Agent loop investigation')

  await page.getByTestId('parent-session-link').click()
  await expect(page.getByTestId('active-session-id')).toHaveText('agent-loop')
  await expect(page.getByTestId('delegation-block')).toBeVisible()
  await expect(childRun).toBeVisible()
})
