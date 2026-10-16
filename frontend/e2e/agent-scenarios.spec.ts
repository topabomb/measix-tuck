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
}

async function streamTicks(page: Page): Promise<number> {
  return numeric(await page.getByTestId('stream-ticks').textContent())
}

async function logicalCount(page: Page): Promise<number> {
  return numeric(await page.getByTestId('logical-count').textContent())
}

async function resumeUntilCount(page: Page, target: number): Promise<void> {
  // Pause at the rendered milestone, before another 60 Hz timer tick. Waiting
  // for a driver round-trip can otherwise advance into the next tool phase.
  await page.evaluate(target => {
    const count = document.querySelector('[data-testid="logical-count"]')!
    const pause = document.querySelector('[data-testid="stream-start"]')!.parentElement!.querySelector<HTMLButtonElement>('button.secondary')!
    const observer = new MutationObserver(() => {
      if (Number(count.textContent!.replaceAll(',', '')) < target) return
      observer.disconnect(); pause.click()
    })
    observer.observe(count, { childList: true, characterData: true, subtree: true })
  }, target)
  await page.getByTestId('stream-start').click()
  await expect.poll(() => logicalCount(page), { timeout: 15_000 }).toBe(target)
}

async function assertNoRowOverlap(page: Page): Promise<void> {
  await expect.poll(async () => page.getByTestId('scrollport').evaluate(stage => {
    const viewport = stage.getBoundingClientRect()
    const rows = [...stage.querySelectorAll<HTMLElement>('[data-virtual-item="true"]')]
      .map(row => row.getBoundingClientRect())
      .filter(rect => rect.height > 0 && rect.bottom > viewport.top && rect.top < viewport.bottom)
      .sort((a, b) => a.top - b.top)
    let worst = 0
    for (let i = 1; i < rows.length; i += 1) worst = Math.max(worst, rows[i - 1]!.bottom - rows[i]!.top)
    return worst
  }), { timeout: 12_000 }).toBeLessThanOrEqual(1)
}

async function bodyOverflow(page: Page): Promise<number> {
  return page.evaluate(() => Math.max(document.documentElement.scrollWidth, document.body.scrollWidth) - window.innerWidth)
}

test('preset conversations land on realistic canonical content without public fixture controls', async ({ page }) => {
  await openApp(page)
  await expect(page.getByTestId('scenario-launch')).toHaveCount(0)
  await switchSession(page, 'dsh-transport')

  await expect(page.getByText('Transport refactor verified', { exact: true })).toBeVisible({ timeout: 15_000 })
  await expect(page.locator('[data-message-index="179997"]').getByTestId('diff-block')).toBeVisible()
  await expect(page.locator('[data-message-index="179998"]').getByTestId('code-block')).toBeVisible()
  await expect(page.locator('[data-message-index="179995"]').getByTestId('tool-block')).toBeVisible()
})

test('default Reference proves coding-workbench rendering semantics through one real multi-step Turn', async ({ page }) => {
  await openApp(page)
  await expect(page.getByTestId('active-session-id')).toHaveText('agent-loop')
  await expect(page.getByTestId('playback-mode')).toHaveText('agent-loop')
  await expect(page.getByTestId('logical-count')).toHaveText('84,000')

  const seeded = page.locator('[data-message-index="83999"]')
  const initialPlan = seeded.getByTestId('plan-block')
  await expect(initialPlan).toBeVisible({ timeout: 15_000 })
  await expect(initialPlan.locator('[data-plan-status="in-progress"]')).toContainText('Inspect projection and resource boundaries')
  await expect(initialPlan.locator('[data-plan-status="pending"]')).toHaveCount(3)

  await page.getByRole('button', { name: 'Pause' }).click()
  await page.getByLabel('Stream rate').selectOption('60')
  await resumeUntilCount(page, 84_003)
  await jump(page, 84_000)
  const fsCall = page.locator('[data-message-index="84000"]').getByTestId('tool-block')
  await expect(fsCall).toHaveAttribute('data-category', 'filesystem')
  await expect(fsCall).toHaveAttribute('data-presentation-kind', 'resources')
  await expect(fsCall).toHaveAttribute('data-call-id', 'loop-read-renderer')
  await fsCall.locator('.tool-summary').click()
  await expect(fsCall.getByTestId('tool-resources')).toContainText('projection-engine.ts')
  await jump(page, 84_001)
  const fsResult = page.locator('[data-message-index="84001"]').getByTestId('tool-block')
  await expect(fsResult).toHaveAttribute('data-category', 'filesystem')
  await expect(fsResult).toHaveAttribute('data-call-id', 'loop-read-renderer')

  await resumeUntilCount(page, 84_006)
  await jump(page, 84_003)
  const searchCall = page.locator('[data-message-index="84003"]').getByTestId('tool-block')
  await expect(searchCall).toHaveAttribute('data-category', 'search')
  await expect(searchCall).toHaveAttribute('data-presentation-kind', 'resources')
  await expect(searchCall).toHaveAttribute('data-call-id', 'loop-search-boundaries')
  await jump(page, 84_004)
  await expect(page.locator('[data-message-index="84004"]').getByTestId('tool-block')).toHaveAttribute('data-call-id', 'loop-search-boundaries')

  await jump(page, 84_005)
  const delegation = page.locator('[data-message-index="84005"]').getByTestId('delegation-block')
  await expect(delegation).toContainText('Delegated verification')
  const foreground = delegation.locator('[data-run-id="review-rendering-contract"]')
  await expect(foreground).toHaveAttribute('data-mode', 'foreground')
  await expect(foreground).toHaveAttribute('data-status', 'completed')
  await expect(foreground).toHaveAttribute('data-child-session-id', 'child-review-contract')
  const background = delegation.locator('[data-mode="background"]')
  await expect(background).toHaveCount(2)
  await expect(delegation.locator('[data-mode="background"][data-status="running"]')).toHaveCount(2)

  // Re-pin the semantic tail before streaming the terminal phase. The earlier
  // historical jumps intentionally disable tail-following; incremental projection is
  // only expected for content that remains inside the bounded hot window.
  // The last delegation can fit at the physical bottom, where Latest is
  // correctly absent. Explicitly leave the tail before testing that control.
  await jump(page, 83_000)
  await page.getByTestId('jump-latest').click()
  await expect(page.getByTestId('jump-latest')).toHaveCount(0)
  const incrementalBeforeTerminal = numeric(await page.getByTestId('projection-incremental').textContent())
  await resumeUntilCount(page, 84_009)
  await jump(page, 84_006)
  const shellCall = page.locator('[data-message-index="84006"]').getByTestId('tool-block')
  await expect(shellCall).toHaveAttribute('data-category', 'shell')
  await expect(shellCall).toHaveAttribute('data-presentation-kind', 'terminal')
  await expect(shellCall).toHaveAttribute('data-call-id', 'loop-run-tests')
  await jump(page, 84_007)
  const shellResult = page.locator('[data-message-index="84007"]').getByTestId('tool-block')
  const terminal = page.locator('[data-message-index="84007"]').getByTestId('terminal-block')
  await expect(shellResult).toHaveAttribute('data-call-id', 'loop-run-tests')
  await expect(terminal).toHaveAttribute('data-call-id', 'loop-run-tests')
  await expect(terminal).toContainText('pnpm test')
  await expect(terminal).toContainText('vite build')
  await expect(terminal).toContainText('Chromium suite passed')
  await expect(terminal).toContainText('success')
  await expect(terminal).toContainText('exit 0')
  await expect.poll(async () => numeric(await page.getByTestId('projection-incremental').textContent()), { timeout: 15_000 }).toBeGreaterThan(incrementalBeforeTerminal)

  // Parent execution has advanced through shell while the two background children
  // independently reached terminal state in the earlier delegation block.
  await jump(page, 84_005)
  const settledDelegation = page.locator('[data-message-index="84005"]').getByTestId('delegation-block')
  await expect(settledDelegation.locator('[data-status="completed"]')).toHaveCount(3)
  await expect(settledDelegation.locator('[data-mode="background"][data-status="completed"]')).toHaveCount(2)
  await expect(settledDelegation.locator('[data-run-id="audit-terminal-projection"]')).toContainText('stable RenderUnit')
  await expect(settledDelegation.locator('[data-run-id="audit-resource-semantics"]')).toContainText('host-owned')

  await expect(page.getByTestId('active-turn-id')).toContainText('agent-loop:release-investigation')
  await expect(page.getByTestId('active-step-id')).toContainText(':step-4')
  await expect(page.getByTestId('active-tool-calls')).toHaveText('4')
  await expect(page.getByTestId('active-tool-categories')).toContainText('filesystem')
  await expect(page.getByTestId('active-tool-categories')).toContainText('search')
  await expect(page.getByTestId('active-tool-categories')).toContainText('shell')

  const beforeSynthesis = await streamTicks(page)
  await page.getByTestId('stream-start').click()
  await expect.poll(() => streamTicks(page), { timeout: 15_000 }).toBeGreaterThan(beforeSynthesis + 12)
  await jump(page, 84_008)
  await page.getByTestId('scrollport').hover()
  await page.mouse.wheel(0,320)
  const finalMarkdown = page.locator('[data-message-index="84008"]').getByTestId('markdown-block')
  await expect(finalMarkdown.first()).toContainText('Final synthesis', { timeout: 15_000 })
  expect(await finalMarkdown.locator('table').count()).toBeGreaterThanOrEqual(1)
  expect(await finalMarkdown.locator('input[type="checkbox"]').count()).toBeGreaterThanOrEqual(5)
  expect(await finalMarkdown.locator('blockquote').count()).toBeGreaterThanOrEqual(1)
  expect(await finalMarkdown.locator('pre code').count()).toBeGreaterThanOrEqual(1)
  await expect(page.locator('.run-status')).toContainText('Completed', { timeout: 15_000 })

  await jump(page, 83_999)
  const finalPlan = page.locator('[data-message-index="83999"]').getByTestId('plan-block')
  await expect(finalPlan.locator('[data-plan-status="completed"]')).toHaveCount(4)
  expect(numeric(await page.getByTestId('mounted-rows').textContent())).toBeLessThan(180)
  await assertNoRowOverlap(page)
})

test('live reasoning can expand during streaming and collapse without corrupting virtual geometry', async ({ page }) => {
  await openApp(page)
  await page.getByTestId('new-session').click()
  await expect(page.getByTestId('active-session-id')).toHaveText('new-1')

  await page.getByLabel('Stream rate').selectOption('5')
  const composer = page.getByTestId('composer-input')
  await composer.fill('Reason carefully about a variable-height Agent response, then answer in Markdown.')
  await page.locator('.send-button').click()

  await expect(page.locator('[data-message-index="1"]').getByTestId('thinking-block')).toBeVisible({ timeout: 15_000 })
  const thinking = page.locator('[data-message-index="1"]').getByTestId('thinking-block')
  await expect(thinking).toHaveAttribute('data-status', 'streaming')
  const collapsedHeight = await thinking.evaluate(element => element.getBoundingClientRect().height)

  await thinking.locator('.disclosure-head').click()
  await expect(thinking.locator('.thinking-body')).toBeVisible()
  const firstLength = (await thinking.locator('.thinking-body').textContent())?.length ?? 0
  const openHeightBefore = await thinking.evaluate(element => element.getBoundingClientRect().height)
  const ticksBefore = await streamTicks(page)

  await expect.poll(() => streamTicks(page), { timeout: 12_000 }).toBeGreaterThan(ticksBefore + 7)
  await expect.poll(async () => (await thinking.locator('.thinking-body').textContent())?.length ?? 0, { timeout: 12_000 }).toBeGreaterThan(firstLength)
  const openHeightAfter = await thinking.evaluate(element => element.getBoundingClientRect().height)
  expect(openHeightAfter).toBeGreaterThanOrEqual(openHeightBefore)
  expect(openHeightAfter).toBeGreaterThan(collapsedHeight + 20)
  await assertNoRowOverlap(page)

  await thinking.locator('.disclosure-head').click()
  await expect(thinking.locator('.thinking-body')).toHaveCount(0)
  const collapsedAgain = await thinking.evaluate(element => element.getBoundingClientRect().height)
  expect(collapsedAgain).toBeLessThan(openHeightAfter)
  expect(Math.abs(collapsedAgain - collapsedHeight)).toBeLessThan(12)
  expect(await bodyOverflow(page)).toBeLessThanOrEqual(1)
  await assertNoRowOverlap(page)
})

test('public multimodal handoff composes uploads, ASR tool correlation and audio artifacts', async ({ page }) => {
  await openApp(page)
  await switchSession(page, 'workspace-files')

  await jump(page, 47_994)
  const uploads = page.locator('[data-message-index="47994"]').getByTestId('attachments-block')
  await expect(uploads.getByTestId('attachment-image')).toHaveCount(1)
  await expect(uploads.getByTestId('attachment-file')).toHaveCount(2)
  await expect(uploads).toContainText('interaction-spec.pdf')
  await expect(uploads).toContainText('review-note.m4a')

  await jump(page, 47_996)
  const asrCall = page.locator('[data-message-index="47996"]').getByTestId('tool-block')
  await expect(asrCall).toHaveAttribute('data-category', 'asr')

  await jump(page, 47_997)
  const asrResult = page.locator('[data-message-index="47997"]').getByTestId('tool-block')
  await expect(asrResult).toHaveAttribute('data-category', 'asr')
  await asrResult.locator('.tool-summary').click()
  await expect(asrResult.locator('.tool-detail')).toContainText('0.96')

  await jump(page, 47_998)
  const audio = page.locator('[data-message-index="47998"]').getByTestId('audio-block')
  await expect(audio.getByTestId('audio-waveform')).toBeVisible()
  await expect(audio.getByTestId('audio-transcript')).toContainText('mixed streaming result')
  await assertNoRowOverlap(page)
})

test('Session diagnostics renderer suite still covers image generation, TTS and ASR variants', async ({ page }) => {
  await openApp(page)
  await switchSession(page, 'dsh-transport')
  await page.getByTestId('inject-agent-scenarios').click()
  await expect(page.getByTestId('logical-count')).toHaveText('180,013')

  await jump(page, 180_003)
  const imageCall = page.locator('[data-message-index="180003"]').getByTestId('tool-block')
  await expect(imageCall).toHaveAttribute('data-category', 'image-generation')
  await expect(imageCall).toContainText('image-gen-reference-v2')

  await jump(page, 180_005)
  const generated = page.locator('[data-message-index="180005"]').getByTestId('attachments-block')
  await expect(generated.getByTestId('attachment-image')).toHaveCount(4)
  await expect(generated).toContainText('image_gen_1')

  await jump(page, 180_008)
  const tts = page.locator('[data-message-index="180008"]').getByTestId('audio-block')
  await expect(tts).toContainText('Generated speech')
  await expect(tts.getByTestId('audio-waveform')).toBeVisible()

  await jump(page, 180_011)
  const asrResult = page.locator('[data-message-index="180011"]').getByTestId('tool-block')
  await expect(asrResult).toHaveAttribute('data-category', 'asr')
  await asrResult.locator('.tool-summary').click()
  await expect(asrResult.locator('.tool-detail')).toContainText('0.97')
})

test('repeated diagnostics scenario packs keep projection and mounted DOM bounded', async ({ page }) => {
  await openApp(page)
  await switchSession(page, 'dsh-transport')
  for (let i = 0; i < 6; i += 1) {
    await page.getByTestId('inject-agent-scenarios').click()
    await expect(page.getByTestId('logical-count')).toHaveText((180_000 + (i + 1) * 13).toLocaleString('en-US'))
  }

  expect(numeric(await page.getByTestId('mounted-rows').textContent())).toBeLessThan(180)
  expect(numeric(await page.getByTestId('projection-cache').textContent())).toBeLessThanOrEqual(4096)
  expect(numeric(await page.getByTestId('active-units').textContent())).toBeLessThan(5000)
  expect(await bodyOverflow(page)).toBeLessThanOrEqual(1)
  await assertNoRowOverlap(page)
})
