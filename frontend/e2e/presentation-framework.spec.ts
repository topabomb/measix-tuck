import { expect, test, type Page } from '@playwright/test'

function numeric(text: string | null): number { return Number((text ?? '').replace(/[^0-9-]/g, '')) }

async function openApp(page: Page): Promise<void> {
  await page.goto('./#/reference')
  await expect(page.getByTestId('active-session-id')).toBeVisible()
}

async function switchSession(page: Page, id: string): Promise<void> {
  await page.getByTestId(`session-${id}`).click()
  await expect(page.getByTestId('active-session-id')).toHaveText(id)
}

async function jump(page: Page, index: number): Promise<void> {
  await page.getByTestId('jump-input').fill(String(index))
  await page.getByTestId('jump-button').click()
  await expect(page.locator(`[data-message-index="${index}"]`).first()).toBeVisible({ timeout: 15_000 })
  await expect.poll(async () => numeric(await page.getByTestId('reader-position').textContent()), { timeout: 15_000 }).toBe(index)
  await settleNavigationFrames(page)
}

async function settleNavigationFrames(page: Page, count = 5): Promise<void> {
  await page.evaluate((frames) => new Promise<void>(resolve => {
    let remaining = frames
    const next = () => {
      remaining -= 1
      if (remaining <= 0) resolve()
      else requestAnimationFrame(next)
    }
    requestAnimationFrame(next)
  }), count)
}

async function visibleAnchor(page: Page): Promise<{ id: string; top: number } | null> {
  return page.getByTestId('scrollport').evaluate(stage => {
    const viewport = stage.getBoundingClientRect()
    const readerText = document.querySelector<HTMLElement>('[data-testid="reader-position"]')?.textContent ?? '0'
    const reader = Number(readerText.replace(/[^0-9-]/g, ''))
    const rows = [...stage.querySelectorAll<HTMLElement>('[data-virtual-item="true"]')]
      .map(row => ({ row, rect: row.getBoundingClientRect(), messageIndex: Number(row.dataset.messageIndex ?? '-1') }))
      .filter(({ rect, messageIndex }) => rect.bottom > viewport.top && rect.top < viewport.bottom && messageIndex <= reader + 1)
      .sort((a, b) => Math.abs(a.rect.top - viewport.top) - Math.abs(b.rect.top - viewport.top))
    const first = rows[0]
    return first?.row.dataset.renderUnit ? { id: first.row.dataset.renderUnit, top: first.rect.top - viewport.top } : null
  })
}

async function anchorDrift(page: Page, anchor: { id: string; top: number }): Promise<number> {
  return page.locator(`[data-render-unit="${anchor.id}"]`).evaluate((element, expectedTop) => {
    const viewport = element.closest<HTMLElement>('[data-testid="scrollport"]')
    if (!viewport) return Number.POSITIVE_INFINITY
    return Math.abs((element.getBoundingClientRect().top - viewport.getBoundingClientRect().top) - Number(expectedTop))
  }, anchor.top).catch(() => Number.POSITIVE_INFINITY)
}

async function assertNoRowOverlap(page: Page): Promise<void> {
  const overlap = await page.getByTestId('scrollport').evaluate((stage) => {
    const viewport = stage.getBoundingClientRect()
    const rows = [...stage.querySelectorAll<HTMLElement>('[data-virtual-item="true"]')]
      .map(row => row.getBoundingClientRect())
      .filter(rect => rect.bottom > viewport.top && rect.top < viewport.bottom)
      .sort((a, b) => a.top - b.top)
    let worst = 0
    for (let i = 1; i < rows.length; i += 1) worst = Math.max(worst, rows[i - 1]!.bottom - rows[i]!.top)
    return worst
  })
  expect(overlap).toBeLessThanOrEqual(1)
}

async function bodyOverflow(page: Page): Promise<number> {
  return page.evaluate(() => Math.max(document.documentElement.scrollWidth, document.body.scrollWidth) - window.innerWidth)
}

test('runtime ContentBlock fixtures use the canonical projector and renderer registries', async ({ page }) => {
  await openApp(page)
  await switchSession(page, 'dsh-transport')
  await expect(page.getByTestId('logical-count')).toHaveText('180,000')
  await page.getByTestId('inject-mixed-five').click()
  await expect(page.getByTestId('logical-count')).toHaveText('180,025')

  await jump(page, 180_001)
  const mixedOneRows = page.locator('[data-message-index="180001"]')
  await expect(mixedOneRows.getByTestId('thinking-block')).toBeVisible()
  await expect(mixedOneRows.getByTestId('diff-block')).toBeVisible()
  await expect(mixedOneRows.getByTestId('markdown-block')).toBeVisible()

  await jump(page, 180_002)
  await expect(page.locator('[data-message-index="180002"]').getByTestId('tool-block')).toBeVisible()

  await jump(page, 180_006)
  await expect(page.locator('[data-message-index="180006"]').getByTestId('image-block')).toBeVisible()

  await jump(page, 180_011)
  const html = page.locator('[data-message-index="180011"]').locator('.html-card')
  await expect(html).toBeVisible()
  expect(await html.locator('script').count()).toBe(0)

  await jump(page, 180_016)
  const code = page.locator('[data-message-index="180016"]').getByTestId('code-block')
  await expect(code).toBeVisible()
  await expect(code.locator('.shiki')).toBeVisible({ timeout: 15_000 })

  expect(numeric(await page.getByTestId('mounted-rows').textContent())).toBeLessThan(180)
  await assertNoRowOverlap(page)
})

test('Markdown compatibility gallery covers GFM structures, sanitization and long chunked documents', async ({ page }) => {
  await openApp(page)
  await switchSession(page, 'dsh-transport')
  await page.getByTestId('inject-markdown-gallery').click()
  await expect(page.getByTestId('logical-count')).toHaveText('180,006')

  await jump(page, 180_000)
  const basics = page.locator('[data-message-index="180000"]').first().getByTestId('markdown-block')
  await expect(basics.locator('h1')).toHaveText('Markdown compatibility')
  await expect(basics.locator('blockquote')).toBeVisible()
  await expect(basics.locator('del')).toHaveText('strike')

  await jump(page, 180_001)
  const tasks = page.locator('[data-message-index="180001"]').first().getByTestId('markdown-block')
  await expect(tasks.locator('input[type="checkbox"]')).toHaveCount(3)

  await jump(page, 180_002)
  await expect(page.locator('[data-message-index="180002"]').first().locator('.markdown-body table')).toBeVisible()

  await jump(page, 180_003)
  await expect(page.locator('[data-message-index="180003"]').first().locator('.markdown-body pre code')).toHaveCount(2)

  await jump(page, 180_004)
  const raw = page.locator('[data-message-index="180004"]').first().getByTestId('markdown-block')
  expect(await raw.locator('script').count()).toBe(0)
  expect(await page.evaluate(() => (window as unknown as { __markdownUnsafe?: boolean }).__markdownUnsafe)).toBeUndefined()

  await jump(page, 180_005)
  await expect(page.locator('[data-message-index="180005"]').first().locator('.markdown-body h3').first()).toBeVisible()
  // A tall first chunk may fill the viewport by itself. Verify distinct parts
  // across navigation, not simultaneous mounting of offscreen chunks.
  const firstPart = page.locator('[data-message-index="180005"]').first()
  const firstId = await firstPart.getAttribute('data-render-unit')
  await expect(firstPart.locator('.markdown-card')).toHaveClass(/has-next-part/)
  await page.getByTestId('jump-latest').click()
  const lastPart = page.locator('[data-message-index="180005"]').last()
  await expect(lastPart).toContainText('Section 24')
  await expect(lastPart.locator('.markdown-card')).toHaveClass(/continuation/)
  expect(await lastPart.getAttribute('data-render-unit')).not.toBe(firstId)
  await assertNoRowOverlap(page)
})

test('million-message stress keeps ordinary Markdown deltas on the incremental projection path', async ({ page }) => {
  await openApp(page)
  await switchSession(page, 'million')
  await expect(page.getByTestId('playback-mode')).toHaveText('stress')
  await page.getByLabel('Stream rate').selectOption('60')
  await page.getByRole('button', { name: 'Pause' }).click()
  await settleNavigationFrames(page, 2)

  const fullBefore = numeric(await page.getByTestId('projection-full-projects').textContent())
  const incrementalBefore = numeric(await page.getByTestId('projection-incremental').textContent())
  const ticksBefore = numeric(await page.getByTestId('stream-ticks').textContent())

  await page.getByTestId('stream-start').click()
  await expect.poll(async () => numeric(await page.getByTestId('stream-ticks').textContent()), { timeout: 15_000 }).toBeGreaterThan(ticksBefore + 24)
  await expect.poll(async () => numeric(await page.getByTestId('projection-incremental').textContent()), { timeout: 15_000 }).toBeGreaterThan(incrementalBefore + 18)
  const fullAfter = numeric(await page.getByTestId('projection-full-projects').textContent())

  expect(fullAfter - fullBefore).toBeLessThanOrEqual(2)
  expect(numeric(await page.getByTestId('projection-cache').textContent())).toBeLessThanOrEqual(4096)
})

test('responsive reflow preserves containment, semantic anchor and mobile session access', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  await openApp(page)
  await switchSession(page, 'dsh-transport')
  await page.getByTestId('inject-markdown-gallery').click()
  await expect(page.getByTestId('logical-count')).toHaveText('180,006')
  await jump(page, 180_002)
  const desktopAnchor = await visibleAnchor(page)
  expect(desktopAnchor).not.toBeNull()

  await page.setViewportSize({ width: 390, height: 844 })
  await expect.poll(() => anchorDrift(page, desktopAnchor!), { timeout: 12_000 }).toBeLessThan(6)
  expect(await bodyOverflow(page)).toBeLessThanOrEqual(1)
  await expect(page.getByTestId('mobile-session-toggle')).toBeVisible()

  const closeDiagnostics = page.locator('.diagnostics-panel .icon-button')
  if (await closeDiagnostics.isVisible()) await closeDiagnostics.click()
  await page.getByTestId('mobile-session-toggle').click()
  await expect(page.getByTestId('session-sidebar')).toBeVisible()
  await page.getByTestId('session-event-normalization').click()
  await expect(page.locator('.conversation-shell')).toHaveAttribute('data-session-id', 'event-normalization')
  await page.getByTestId('mobile-session-toggle').click()
  await page.getByTestId('session-dsh-transport').click()

  await page.getByTestId('diagnostics-open').click()
  await jump(page, 180_002)
  const table = page.locator('[data-message-index="180002"]').first().locator('.markdown-body table')
  await expect(table).toBeVisible()
  expect(await bodyOverflow(page)).toBeLessThanOrEqual(1)

  await jump(page, 180_003)
  const pre = page.locator('[data-message-index="180003"]').first().locator('.markdown-body pre').first()
  await expect(pre).toBeVisible()
  expect(await bodyOverflow(page)).toBeLessThanOrEqual(1)

  await page.getByTestId('inject-mixed-five').click()
  await jump(page, 180_007)
  const image = page.locator('[data-message-index="180007"]').getByTestId('image-block').locator('img')
  await expect(image).toBeVisible()
  const imageFits = await image.evaluate(element => {
    const row = element.closest<HTMLElement>('[data-virtual-item="true"]')!
    return element.getBoundingClientRect().width <= row.getBoundingClientRect().width + 1
  })
  expect(imageFits).toBe(true)
  expect(await bodyOverflow(page)).toBeLessThanOrEqual(1)
  await assertNoRowOverlap(page)

  const phoneAnchor = await visibleAnchor(page)
  expect(phoneAnchor).not.toBeNull()
  await page.setViewportSize({ width: 980, height: 820 })
  await expect.poll(() => anchorDrift(page, phoneAnchor!), { timeout: 12_000 }).toBeLessThan(6)
  expect(await bodyOverflow(page)).toBeLessThanOrEqual(1)
  await assertNoRowOverlap(page)
})
