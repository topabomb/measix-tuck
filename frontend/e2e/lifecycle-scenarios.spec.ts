import { expect, test, type Page } from '@playwright/test'

async function open(page: Page): Promise<void> {
  await page.goto('./#/reference')
  await expect(page.getByTestId('active-session-id')).toBeVisible()
}

async function switchTo(page: Page, id: string): Promise<void> {
  await page.getByTestId(`session-${id}`).click()
  await expect(page.getByTestId('active-session-id')).toHaveText(id)
}

test('partial child failure falls back explicitly while the parent session still completes', async ({ page }) => {
  await open(page)
  await switchTo(page, 'resilience-fallback')

  await expect(page.locator('.run-status')).toContainText('Completed')
  await expect(page.getByTestId('active-plan-strip')).toHaveAttribute('data-completed', '3')
  await expect(page.getByTestId('active-plan-strip')).toHaveAttribute('data-total', '3')

  const delegation = page.getByTestId('delegation-block')
  await expect(delegation).toContainText('Parallel specialist review')
  await expect(delegation.locator('[data-status="failed"]')).toHaveCount(1)
  await expect(delegation.locator('[data-status="completed"]')).toHaveCount(2)
  await expect(delegation.locator('[data-run-id="customer-risk-specialist"]')).toContainText('CRM live query returned 503')

  const fallback = page.locator('[data-call-id="fallback-crm-export"]')
  await expect(fallback).toHaveCount(2)
  await expect(page.getByText('Launch-risk brief', { exact: true })).toBeVisible()
  await expect(page.getByText(/used the most recent allowed cached export/)).toBeVisible()
})

test('an interrupted Turn remains historical evidence after the user steers a later Turn', async ({ page }) => {
  await open(page)
  await switchTo(page, 'steered-migration')

  await expect(page.locator('.run-status')).toContainText('Completed')
  await expect(page.getByTestId('active-plan-strip')).toHaveAttribute('data-completed', '3')
  await expect(page.getByTestId('active-plan-strip')).toHaveAttribute('data-total', '3')

  const terminal = page.getByTestId('terminal-block')
  await expect(terminal).toContainText('interrupted')
  await expect(terminal).toContainText('exit 130')
  await expect(terminal).toContainText('Stopped by user before any write phase')

  await expect(page.getByText(/Change direction: do not run the migration/)).toBeVisible()
  await expect(page.getByText('Read-only impact report', { exact: true })).toBeVisible()
  await expect(page.getByText(/new instruction starts a separate Turn and becomes authoritative/)).toBeVisible()
})

test('clarification blocker can be answered and the same session can continue execution', async ({ page }) => {
  await open(page)
  await switchTo(page, 'android-protocol')

  await expect(page.getByTestId('pending-interaction')).toHaveAttribute('data-kind', 'question')
  await page.getByTestId('question-answer').fill('Keep the last accepted configuration for one refresh window.')
  await page.getByTestId('approve-interaction').click()
  await expect(page.getByTestId('pending-interaction')).toHaveCount(0)

  const before = Number((await page.getByTestId('logical-count').textContent())?.replace(/,/g, '') ?? '0')
  const composer = page.getByTestId('composer-input')
  await composer.fill('Continue with that fallback decision and finish the rollout guidance.')
  await composer.press('Enter')

  await expect(page.locator('.run-status')).toContainText('Working')
  await expect.poll(async () => Number((await page.getByTestId('logical-count').textContent())?.replace(/,/g, '') ?? '0')).toBeGreaterThan(before)
})

test('Diagnostics lifecycle shortcuts navigate only to existing Reference sessions', async ({ page }) => {
  await open(page)

  await page.getByTestId('reference-lifecycle-fallback').click()
  await expect(page.getByTestId('active-session-id')).toHaveText('resilience-fallback')
  await expect(page.getByText('Launch-risk brief', { exact: true })).toBeVisible()

  await page.getByTestId('reference-lifecycle-steer').click()
  await expect(page.getByTestId('active-session-id')).toHaveText('steered-migration')
  await expect(page.getByText('Read-only impact report', { exact: true })).toBeVisible()

  await page.getByTestId('reference-lifecycle-clarify').click()
  await expect(page.getByTestId('active-session-id')).toHaveText('android-protocol')
  await expect(page.getByTestId('pending-interaction')).toHaveAttribute('data-kind', 'question')
})
