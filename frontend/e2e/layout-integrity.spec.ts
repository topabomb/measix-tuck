import { expect, test, type Page } from '@playwright/test'

async function openLab(page: Page): Promise<void> {
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
  await expect.poll(async () => Number((await page.getByTestId('reader-position').textContent() ?? '').replace(/[^0-9-]/g, '')), { timeout: 15_000 }).toBe(index)
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

async function maxMountedRowOverlap(page: Page): Promise<number> {
  return page.getByTestId('scrollport').evaluate((viewport) => {
    const rows = [...viewport.querySelectorAll<HTMLElement>('[data-virtual-item="true"]')]
      .map(row => ({ id: row.dataset.renderUnit ?? '', rect: row.getBoundingClientRect() }))
      .filter(entry => entry.rect.height > 0)
      .sort((a, b) => a.rect.top - b.rect.top)
    let overlap = 0
    for (let i = 1; i < rows.length; i += 1) overlap = Math.max(overlap, rows[i - 1]!.rect.bottom - rows[i]!.rect.top)
    return overlap
  })
}

async function expectRowsDisjoint(page: Page): Promise<void> {
  await expect.poll(() => maxMountedRowOverlap(page), { timeout: 12_000 }).toBeLessThanOrEqual(1)
}

async function committedAnchor(page: Page): Promise<{ id: string; top: number } | null> {
  return page.getByTestId('scrollport').evaluate((stage) => {
    const readerText = document.querySelector('[data-testid="reader-position"]')?.textContent ?? ''
    const reader = Number(readerText.replace(/[^0-9-]/g, ''))
    const viewport = stage.getBoundingClientRect()
    const row = [...stage.querySelectorAll<HTMLElement>('[data-virtual-item="true"]')]
      .map(element => ({ element, index: Number(element.dataset.messageIndex), rect: element.getBoundingClientRect() }))
      .filter(entry => entry.index <= reader + 1 && entry.rect.bottom > viewport.top && entry.rect.top < viewport.bottom)
      .sort((a, b) => Math.abs(a.rect.top - viewport.top) - Math.abs(b.rect.top - viewport.top))[0]
    return row ? { id: row.element.dataset.renderUnit ?? '', top: row.rect.top - viewport.top } : null
  })
}

async function anchorDrift(page: Page, anchor: { id: string; top: number }): Promise<number> {
  return page.getByTestId('scrollport').evaluate((stage, expected) => {
    const row = [...stage.querySelectorAll<HTMLElement>('[data-render-unit]')].find(element => element.dataset.renderUnit === expected.id)
    if (!row) return Number.POSITIVE_INFINITY
    return Math.abs((row.getBoundingClientRect().top - stage.getBoundingClientRect().top) - expected.top)
  }, anchor)
}

test('architecture proof exposes layout-agnostic Engine, delegation, adapter and Reference responsibilities', async ({ page }) => {
  await openLab(page)
  await page.getByTestId('architecture-link').click()
  await expect(page.getByTestId('architecture-page')).toBeVisible()
  await expect(page.getByRole('heading', { name: /external runtime.*semantic engine.*reference host/i })).toBeVisible()
  await expect(page.getByRole('heading', { name: /general semantics go inward.*behavior policy stays outward/i })).toBeVisible()
  await expect(page.getByRole('heading', { name: /concepts that must not collapse into each other/i })).toBeVisible()
  await expect(page.getByRole('heading', { name: /correct semantics do not change the scaling law/i })).toBeVisible()

  for (const name of ['Canonical identity', 'Workbench semantics', 'Delegated child refs', 'SessionKernel', 'History source', 'Projection', 'Semantic viewport', 'Vue reference adapter']) {
    await expect(page.getByText(name, { exact: true })).toBeVisible()
  }
  for (const name of ['Plan ≠ Step', 'Tool category ≠ presentation intent', 'ResourceRef ≠ host action', 'Delegation ≠ subagent runtime', 'Child status ≠ parent status', 'Child reference ≠ child trace', 'Semantic content ≠ layout surface']) {
    await expect(page.getByText(name, { exact: true })).toBeVisible()
  }
  for (const name of ['External adapters', 'Framework-neutral Engine', 'Reference host']) {
    await expect(page.getByText(name, { exact: true })).toBeVisible()
  }

  await expect(page.getByText(/O\(delta \+ mutable tail\)/)).toBeVisible()
  await expect(page.getByText(/^Streaming terminal/)).toBeVisible()
  await expect(page.getByText(/^Delegation status update/)).toBeVisible()
  await expect(page.getByText(/Detailed child trace stays in an independently addressable conversation session/)).toBeVisible()
  await expect(page.getByText(/no core PresentationSurface/)).toBeVisible()
  await expect(page.getByText(/package publishing disabled/i)).toBeVisible()
  await page.getByTestId('launch-lab').click()
  await expect(page.getByTestId('active-session-id')).toBeVisible()
})

test('dynamic heterogeneous rows never overlap after disclosure, async measurement, composer resize and session remount', async ({ page }) => {
  await openLab(page)
  await switchSession(page, 'dsh-transport')

  await jump(page, 120_001)
  await expectRowsDisjoint(page)
  const thinking = page.locator('[data-message-index="120001"]').first().getByTestId('thinking-block')
  await thinking.locator('button').click()
  await expect(thinking.locator('.thinking-body')).toBeVisible()
  await expectRowsDisjoint(page)

  await jump(page, 120_002)
  const tool = page.locator('[data-message-index="120002"]').first().getByTestId('tool-block')
  await tool.locator('.tool-summary').click()
  await expect(tool.locator('.tool-detail')).toBeVisible()
  await expectRowsDisjoint(page)

  await jump(page, 120_010)
  const codeUnits = page.locator('[data-message-index="120010"] .shiki')
  await expect.poll(() => codeUnits.count(), { timeout: 15_000 }).toBeGreaterThan(0)
  await expect(codeUnits.first()).toBeVisible()
  await expectRowsDisjoint(page)

  const composer = page.getByTestId('composer-input')
  await composer.fill(Array.from({ length: 12 }, (_, i) => `line ${i} ${'dynamic agent content '.repeat(8)}`).join('\n'))
  await expect.poll(async () => composer.evaluate(el => el.getBoundingClientRect().height)).toBeGreaterThan(100)
  await expectRowsDisjoint(page)

  await switchSession(page, 'event-normalization')
  await expectRowsDisjoint(page)
  await switchSession(page, 'dsh-transport')
  await expectRowsDisjoint(page)
})

test('semantic viewport survives a different product layout and composer height policy', async ({ page }) => {
  await openLab(page)
  await switchSession(page, 'event-normalization')
  await jump(page, 50_000)

  await expect.poll(async () => page.getByTestId('scrollport').evaluate(stage => {
    const viewport = stage.getBoundingClientRect()
    const element = [...stage.querySelectorAll<HTMLElement>('[data-virtual-item="true"]')].find(row => {
      const rect = row.getBoundingClientRect()
      return rect.height > 0 && rect.bottom > viewport.top && rect.top < viewport.bottom
    })
    if (!element) return null
    const style = getComputedStyle(element)
    return { paddingTop: style.paddingTop, paddingBottom: style.paddingBottom, marginTop: style.marginTop, marginBottom: style.marginBottom }
  }), { timeout: 12_000 }).toEqual({ paddingTop: '0px', paddingBottom: '0px', marginTop: '0px', marginBottom: '0px' })

  const before = await committedAnchor(page)
  expect(before).not.toBeNull()
  const stageHeightBefore = await page.getByTestId('scrollport').evaluate(element => element.getBoundingClientRect().height)

  await page.evaluate(() => {
    const app = document.querySelector<HTMLElement>('.agent-app')!.style
    const engine = document.querySelector<HTMLElement>('[data-conversation-engine]')!.style
    app.setProperty('--session-sidebar-width', '328px')
    engine.setProperty('--conversation-content-width', '720px')
    engine.setProperty('--conversation-row-gap', '12px')
    engine.setProperty('--composer-max-height', '240px')
  })

  const composer = page.getByTestId('composer-input')
  await composer.fill(Array.from({ length: 15 }, (_, i) => `portable product line ${i} ${'responsive '.repeat(9)}`).join('\n'))
  await expect.poll(async () => composer.evaluate(el => el.getBoundingClientRect().height)).toBeGreaterThan(120)
  await expect.poll(async () => page.getByTestId('scrollport').evaluate(element => element.getBoundingClientRect().height), { timeout: 12_000 }).toBeLessThan(stageHeightBefore - 50)
  await expect.poll(() => anchorDrift(page, before!), { timeout: 12_000 }).toBeLessThan(5)
  await expectRowsDisjoint(page)
})
