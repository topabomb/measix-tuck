import { expect, test, type Page } from '@playwright/test'

function numeric(text: string | null): number {
  return Number((text ?? '').replace(/[^0-9-]/g, ''))
}

async function openApp(page: Page): Promise<void> {
  await page.goto('./#/reference')
  await expect(page.getByTestId('active-session-id')).toBeVisible()
  if ((await page.getByTestId('active-session-id').textContent()) !== 'million') await switchSession(page, 'million')
}

async function physicalRemainingToBottom(page: Page): Promise<number> {
  return page.locator('.conversation-vlist').evaluate((element) => element.scrollHeight - element.scrollTop - element.clientHeight)
}

async function visibleAnchor(page: Page): Promise<{ id: string; top: number } | null> {
  return page.getByTestId('scrollport').evaluate((scrollport) => {
    const viewport = scrollport.getBoundingClientRect()
    const rows = [...scrollport.querySelectorAll<HTMLElement>('[data-render-unit]')]
      .map(row => ({ row, rect: row.getBoundingClientRect() }))
      .filter(({ rect }) => rect.bottom > viewport.top && rect.top < viewport.bottom)
      .sort((a, b) => Math.abs(a.rect.top - viewport.top) - Math.abs(b.rect.top - viewport.top))
    const first = rows[0]
    if (!first) return null
    return { id: first.row.dataset.renderUnit ?? '', top: first.rect.top - viewport.top }
  })
}

async function anchorDrift(page: Page, anchor: { id: string; top: number }): Promise<number> {
  return page.locator(`[data-render-unit="${anchor.id}"]`).evaluate((element, expectedTop) => {
    const viewport = element.closest('[data-testid="scrollport"]')
    if (!viewport) return Number.POSITIVE_INFINITY
    return Math.abs((element.getBoundingClientRect().top - viewport.getBoundingClientRect().top) - Number(expectedTop))
  }, anchor.top).catch(() => Number.POSITIVE_INFINITY)
}

async function jump(page: Page, index: number): Promise<void> {
  await page.getByTestId('jump-input').fill(String(index))
  await page.getByTestId('jump-button').click()
  await expect.poll(async () => {
    const range = (await page.getByTestId('segment-range').textContent()) ?? ''
    const [startText, endText] = range.split('–')
    return numeric(startText) <= index && numeric(endText) >= index
  }).toBe(true)

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

async function switchSession(page: Page, id: string): Promise<void> {
  await page.getByTestId(`session-${id}`).click()
  await expect(page.getByTestId('active-session-id')).toHaveText(id)
  await expect.poll(async () => numeric(await page.getByTestId('mounted-rows').textContent())).toBeGreaterThan(0)
}

async function exactLatestCount(page: Page): Promise<number> {
  const logical = numeric(await page.getByTestId('logical-count').textContent())
  const reader = numeric(await page.getByTestId('reader-position').textContent())
  const expected = Math.max(0, logical - 1 - reader)
  const badge = page.getByTestId('messages-after')
  if (expected === 0) {
    expect(await badge.count()).toBe(0)
  } else {
    await expect(badge).toBeVisible()
    expect(numeric(await badge.textContent())).toBe(expected)
  }
  expect(numeric(await page.getByTestId('messages-after-metric').textContent())).toBe(expected)
  return expected
}

test('1M history stays bounded while streaming, reader escape and reverse prepend remain pixel-stable', async ({ page }) => {
  const consoleErrors: string[] = []
  page.on('console', message => { if (message.type() === 'error') consoleErrors.push(message.text()) })

  await openApp(page)
  await expect(page.getByTestId('active-session-id')).toHaveText('million')
  await expect(page.getByTestId('logical-count')).toHaveText('1,000,000')

  expect(numeric(await page.getByTestId('mounted-rows').textContent())).toBeLessThan(180)
  const active = numeric(await page.getByTestId('active-units').textContent())
  expect(active).toBeGreaterThan(2048)
  expect(active).toBeLessThan(10_000)

  await page.getByLabel('Stream rate').selectOption('60')
  await page.getByTestId('stream-start').click()
  await expect.poll(async () => numeric(await page.getByTestId('live-chunks').textContent()), { timeout: 15_000 }).toBeGreaterThan(1)
  await expect.poll(async () => numeric(await page.getByTestId('stream-ticks').textContent())).toBeGreaterThan(30)
  await expect(page.locator('[data-live-unit="true"]').last()).toBeVisible()
  // At 60 Hz a new chunk can land between the height measurement and tail
  // correction. Check convergence, then require the settled tail to be exact.
  await expect.poll(() => physicalRemainingToBottom(page)).toBeLessThan(180)
  await page.getByRole('button', { name: 'Pause' }).click()
  await expect.poll(() => physicalRemainingToBottom(page)).toBeLessThan(2)
  await page.getByTestId('stream-start').click()

  const publishesBeforeEscape = numeric(await page.getByTestId('stream-ticks').textContent())
  await page.locator('.conversation-vlist').hover()
  await page.mouse.wheel(0, -1200)
  await expect(page.getByTestId('follow-state')).toHaveText('tail paused')
  await expect.poll(() => physicalRemainingToBottom(page)).toBeGreaterThan(400)
  await expect.poll(async () => numeric(await page.getByTestId('stream-ticks').textContent())).toBeGreaterThan(publishesBeforeEscape + 8)

  await page.getByRole('button', { name: 'Pause' }).click()
  await jump(page, 500_000)
  expect(await page.locator('[data-render-unit]').count()).toBeLessThan(180)
  expect(Math.abs(numeric(await page.getByTestId('reader-position').textContent()) - 500_000)).toBeLessThan(35)

  const anchorBefore = await visibleAnchor(page)
  expect(anchorBefore).not.toBeNull()
  const beforeSegment = await page.getByTestId('segment-range').textContent()
  await page.getByTestId('prepend-button').click()
  await expect.poll(async () => await page.getByTestId('segment-range').textContent()).not.toBe(beforeSegment)
  await expect.poll(() => anchorDrift(page, anchorBefore!), { timeout: 12_000 }).toBeLessThan(4)

  expect(await page.locator('[data-render-unit]').count()).toBeLessThan(180)
  expect(consoleErrors).toEqual([])
})

test('async Agent execution survives history browsing and Recent switching, including multi-chunk live state', async ({ page }) => {
  await openApp(page)
  await expect(page.getByTestId('active-session-id')).toHaveText('million')

  await page.getByLabel('Stream rate').selectOption('60')
  await page.getByTestId('stream-start').click()
  await expect.poll(async () => numeric(await page.getByTestId('live-chunks').textContent()), { timeout: 15_000 }).toBeGreaterThan(1)
  const beforeHistory = numeric(await page.getByTestId('stream-ticks').textContent())

  await jump(page, 500_000)
  await expect(page.getByTestId('jump-latest')).toBeVisible()
  await expect(page.getByTestId('follow-state')).toHaveText('tail paused')
  await expect.poll(async () => numeric(await page.getByTestId('stream-ticks').textContent())).toBeGreaterThan(beforeHistory + 8)
  const afterHistory = numeric(await page.getByTestId('stream-ticks').textContent())
  const historyReader = numeric(await page.getByTestId('reader-position').textContent())

  await switchSession(page, 'dsh-transport')
  await page.waitForTimeout(700)
  await switchSession(page, 'million')
  await expect.poll(async () => numeric(await page.getByTestId('stream-ticks').textContent())).toBeGreaterThan(afterHistory)
  expect(Math.abs(numeric(await page.getByTestId('reader-position').textContent()) - historyReader)).toBeLessThan(45)
  await expect(page.getByTestId('jump-latest')).toBeVisible()
  expect(await exactLatestCount(page)).toBeGreaterThan(400_000)

  const ticksBeforeLatest = numeric(await page.getByTestId('stream-ticks').textContent())
  const chunksBeforeLatest = numeric(await page.getByTestId('live-chunks').textContent())
  await page.getByTestId('jump-latest').click()
  await expect(page.getByTestId('jump-latest')).toHaveCount(0)
  await expect(page.getByTestId('reader-position')).toHaveText('#999,999')
  await expect.poll(() => physicalRemainingToBottom(page)).toBeLessThan(80)
  await expect.poll(async () => numeric(await page.getByTestId('stream-ticks').textContent())).toBeGreaterThan(ticksBeforeLatest)
  expect(numeric(await page.getByTestId('live-chunks').textContent())).toBeGreaterThanOrEqual(chunksBeforeLatest)
  await expect(page.locator('[data-live-unit="true"]').last()).toBeVisible()
})

test('Latest is an exact logical-tail control across session switches, not a scrollbar approximation', async ({ page }) => {
  await openApp(page)
  await switchSession(page, 'dsh-transport')

  await expect(page.getByTestId('jump-latest')).toHaveCount(0)
  await expect(page.getByTestId('reader-position')).toHaveText('#179,999')
  expect(await exactLatestCount(page)).toBe(0)

  await jump(page, 90_000)
  await expect(page.getByTestId('jump-latest')).toBeVisible()
  const beforeSwitchReader = numeric(await page.getByTestId('reader-position').textContent())
  const beforeSwitchAfter = await exactLatestCount(page)
  expect(beforeSwitchAfter).toBe(180_000 - 1 - beforeSwitchReader)

  await switchSession(page, 'event-normalization')
  await expect(page.getByTestId('jump-latest')).toHaveCount(0)
  await expect(page.getByTestId('reader-position')).toHaveText('#94,999')

  await switchSession(page, 'dsh-transport')
  await expect(page.getByTestId('jump-latest')).toBeVisible()
  expect(Math.abs(numeric(await page.getByTestId('reader-position').textContent()) - beforeSwitchReader)).toBeLessThan(45)
  await exactLatestCount(page)

  await page.getByTestId('jump-latest').click()
  await expect(page.getByTestId('jump-latest')).toHaveCount(0)
  await expect(page.getByTestId('reader-position')).toHaveText('#179,999')
  expect(await exactLatestCount(page)).toBe(0)
  await expect.poll(() => physicalRemainingToBottom(page)).toBeLessThan(80)
})

test('variable-height composer owns layout space, preserves history anchor and re-pins the tail', async ({ page }) => {
  await openApp(page)
  await switchSession(page, 'dsh-transport')
  await jump(page, 90_000)

  const anchor = await visibleAnchor(page)
  expect(anchor).not.toBeNull()
  const input = page.getByTestId('composer-input')
  const stage = page.getByTestId('scrollport')
  const initialInputHeight = await input.evaluate(element => element.getBoundingClientRect().height)
  const initialViewportHeight = await stage.evaluate(element => element.getBoundingClientRect().height)

  const longDraft = Array.from({ length: 12 }, (_, i) => `draft line ${i} ${'agent context '.repeat(9)}`).join('\n')
  await input.fill(longDraft)
  await expect.poll(async () => input.evaluate(element => element.getBoundingClientRect().height)).toBeGreaterThan(initialInputHeight + 50)

  const geometry = await page.evaluate(() => {
    const stageEl = document.querySelector<HTMLElement>('[data-testid="scrollport"]')!
    const composerEl = document.querySelector<HTMLElement>('[data-testid="composer-shell"]')!
    const listEl = document.querySelector<HTMLElement>('.conversation-vlist')!
    return {
      stageBottom: stageEl.getBoundingClientRect().bottom,
      composerTop: composerEl.getBoundingClientRect().top,
      stageHeight: stageEl.getBoundingClientRect().height,
      listBottom: listEl.getBoundingClientRect().bottom,
    }
  })
  expect(geometry.stageBottom).toBeLessThanOrEqual(geometry.composerTop + 1)
  expect(geometry.listBottom).toBeLessThanOrEqual(geometry.composerTop + 1)
  expect(geometry.stageHeight).toBeLessThan(initialViewportHeight - 40)
  await expect.poll(() => anchorDrift(page, anchor!), { timeout: 12_000 }).toBeLessThan(4)

  await input.fill('short draft')
  await expect.poll(async () => input.evaluate(element => element.getBoundingClientRect().height)).toBeLessThan(initialInputHeight + 15)
  await expect.poll(() => anchorDrift(page, anchor!), { timeout: 12_000 }).toBeLessThan(4)

  await input.fill(longDraft)
  await switchSession(page, 'event-normalization')
  await expect(page.getByTestId('composer-input')).toHaveValue('')
  await switchSession(page, 'dsh-transport')
  await expect(page.getByTestId('composer-input')).toHaveValue(longDraft)
  await expect.poll(async () => page.getByTestId('composer-input').evaluate(element => element.getBoundingClientRect().height)).toBeGreaterThan(initialInputHeight + 50)

  await page.getByTestId('jump-latest').click()
  await expect(page.getByTestId('jump-latest')).toHaveCount(0)
  await input.fill('short')
  await input.fill(longDraft)
  await expect.poll(() => physicalRemainingToBottom(page)).toBeLessThan(80)
  await expect(page.getByTestId('jump-latest')).toHaveCount(0)
})

test('a non-million Recent session renders realistic thinking/tool/code/diff/image/html nodes with isolated IDs', async ({ page }) => {
  const consoleErrors: string[] = []
  page.on('console', message => { if (message.type() === 'error') consoleErrors.push(message.text()) })

  await openApp(page)
  await switchSession(page, 'dsh-transport')
  await expect(page.getByTestId('logical-count')).toHaveText('180,000')

  await jump(page, 120_001)
  const scopedRow = page.locator('[data-message-index="120001"]').first()
  await expect(scopedRow).toHaveAttribute('data-session-id', 'dsh-transport')
  await expect(scopedRow).toHaveAttribute('data-render-unit', /^dsh-transport:/)

  let thinking = scopedRow.getByTestId('thinking-block')
  await expect(thinking).toBeVisible()
  const collapsed = await thinking.evaluate(element => element.getBoundingClientRect().height)
  await thinking.locator('button').click()
  thinking = page.locator('[data-message-index="120001"]').first().getByTestId('thinking-block')
  await expect(thinking.locator('.thinking-body')).toBeVisible()
  expect(await thinking.evaluate(element => element.getBoundingClientRect().height)).toBeGreaterThan(collapsed + 40)

  await jump(page, 120_002)
  let tool = page.locator('[data-message-index="120002"]').first().getByTestId('tool-block')
  await tool.locator('.tool-summary').click()
  tool = page.locator('[data-message-index="120002"]').first().getByTestId('tool-block')
  await expect(tool.locator('.tool-detail')).toBeVisible()
  await expect(tool.locator('.tool-pane')).toContainText(/path|rows|query/)

  await jump(page, 120_010)
  const codeRow = page.locator('[data-message-index="120010"]').filter({ has: page.getByTestId('code-block') }).first()
  const codeUnitId = await codeRow.getAttribute('data-render-unit')
  expect(codeUnitId).toBeTruthy()
  let code = codeRow.getByTestId('code-block')
  await expect(code.locator('.shiki')).toBeVisible({ timeout: 15_000 })
  const expandCode = code.getByRole('button', { name: '展开', exact: true })
  if (await expandCode.count()) {
    await expandCode.click()
    code = page.locator(`[data-render-unit="${codeUnitId}"]`).getByTestId('code-block')
    await expect(code.getByRole('button', { name: '收起', exact: true })).toBeVisible()
  }

  await jump(page, 120_018)
  const diffRow = page.locator('[data-message-index="120018"]').filter({ has: page.getByTestId('diff-block') }).first()
  const diffUnitId = await diffRow.getAttribute('data-render-unit')
  expect(diffUnitId).toBeTruthy()
  let diff = diffRow.getByTestId('diff-block')
  await expect(diff.locator('.diff-ellipsis')).toBeVisible()
  await diff.getByRole('button', { name: '展开', exact: true }).click()
  diff = page.locator(`[data-render-unit="${diffUnitId}"]`).getByTestId('diff-block')
  await expect(diff.getByRole('button', { name: '收起', exact: true })).toBeVisible()

  await jump(page, 120_022)
  const html = page.locator('[data-message-index="120022"]').first().locator('.html-card')
  await expect(html).toContainText('Generated interactive artifact')
  expect(await html.locator('script').count()).toBe(0)

  await jump(page, 120_021)
  const image = page.locator('[data-message-index="120021"]').first().locator('.image-card img')
  await expect(image).toBeVisible()
  expect(Number(await image.getAttribute('width'))).toBeGreaterThan(0)
  expect(Number(await image.getAttribute('height'))).toBeGreaterThan(0)

  expect(await page.locator('[data-render-unit]').count()).toBeLessThan(180)
  expect(consoleErrors).toEqual([])
})

test('Recent sessions isolate scroll/fold/draft state, restore semantic viewport, and keep hot-runtime LRU bounded', async ({ page }) => {
  await openApp(page)

  await expect.poll(async () => numeric(await page.getByTestId('stream-ticks').textContent())).toBeGreaterThan(3)
  const millionTicks = numeric(await page.getByTestId('stream-ticks').textContent())

  await switchSession(page, 'dsh-transport')
  await page.waitForTimeout(700)
  await switchSession(page, 'million')
  await expect.poll(async () => numeric(await page.getByTestId('stream-ticks').textContent())).toBeGreaterThan(millionTicks)
  await page.getByRole('button', { name: 'Pause' }).click()

  await jump(page, 120_001)
  let millionThinking = page.locator('[data-message-index="120001"]').first().getByTestId('thinking-block')
  await millionThinking.locator('button').click()
  millionThinking = page.locator('[data-message-index="120001"]').first().getByTestId('thinking-block')
  await expect(millionThinking.locator('.thinking-body')).toBeVisible()
  await page.getByTestId('composer-input').fill('million session draft')

  await switchSession(page, 'dsh-transport')
  await jump(page, 120_001)
  const dshThinking = page.locator('[data-message-index="120001"]').first().getByTestId('thinking-block')
  await expect(dshThinking).toBeVisible()
  expect(await dshThinking.locator('.thinking-body').count()).toBe(0)
  await expect(page.getByTestId('composer-input')).toHaveValue('')

  await jump(page, 90_000)
  const dshReader = numeric(await page.getByTestId('reader-position').textContent())
  expect(Math.abs(dshReader - 90_000)).toBeLessThan(35)

  await switchSession(page, 'tool-rendering')
  await switchSession(page, 'event-normalization')
  await switchSession(page, 'dynamic-heights')
  expect(numeric(await page.getByTestId('hot-sessions').textContent())).toBeLessThanOrEqual(3)

  await switchSession(page, 'dsh-transport')
  await expect.poll(async () => Math.abs(numeric(await page.getByTestId('reader-position').textContent()) - 90_000)).toBeLessThan(45)
  expect(numeric(await page.getByTestId('hot-sessions').textContent())).toBeLessThanOrEqual(3)

  await switchSession(page, 'million')
  await expect.poll(async () => Math.abs(numeric(await page.getByTestId('reader-position').textContent()) - 120_001)).toBeLessThan(45)
  millionThinking = page.locator('[data-message-index="120001"]').first().getByTestId('thinking-block')
  await expect(millionThinking.locator('.thinking-body')).toBeVisible()
  await expect(page.getByTestId('composer-input')).toHaveValue('million session draft')

  expect(await page.locator('[data-render-unit]').count()).toBeLessThan(180)
})
