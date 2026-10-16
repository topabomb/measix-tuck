import { openSettings, closeSettings } from './settings'
import { expect, test } from '@playwright/test'
import { fixture, until } from '../../tests/fixtures/fixture.mjs'

test('send hover paints only the compact face and keeps its hit area centered', async ({page},info) => {
  const app=await fixture()
  try {
    await app.manager.create({title:'发送按钮悬停'})
    await page.goto(app.url)
    await page.getByTestId('composer-input').fill('检查按钮，不发送')
    const button=page.getByTestId('send-message')
    await expect(button).toBeEnabled()
    for(const theme of ['default','曜黑（高对比）']) {
      if(theme!=='default') {
        await openSettings(page)
        await page.getByRole('navigation',{name:'设置分类'}).getByRole('link',{name:'外观',exact:true}).click()
        await page.getByRole('button',{name:theme,exact:true}).click()
        await closeSettings(page)
      }
      for(const width of [320,390,1365]) {
        await page.setViewportSize({width,height:800})
        await page.mouse.move(0,0)
        const before=await button.boundingBox()
        await button.hover()
        const geometry=await button.evaluate(el=>{
          const face=el.querySelector('.control-face')!, outer=el.getBoundingClientRect(), inner=face.getBoundingClientRect()
          return {background:getComputedStyle(el).backgroundColor,filter:getComputedStyle(el).filter,height:outer.height,faceHeight:inner.height,center:Math.abs((outer.top+outer.bottom)-(inner.top+inner.bottom))/2}
        })
        expect(geometry.background).toBe('rgba(0, 0, 0, 0)')
        expect(geometry.filter).toBe('none')
        expect(geometry.height).toBe(width<=760?44:30)
        expect(geometry.faceHeight).toBe(30)
        expect(geometry.center).toBeLessThanOrEqual(.5)
        expect(await button.locator('.control-face').evaluate(el=>{
          const probe=document.createElement('span');probe.style.color='var(--tuck-accent)';el.append(probe)
          const same=getComputedStyle(el).backgroundColor===getComputedStyle(probe).color;probe.remove();return same
        })).toBe(true)
        expect(await button.boundingBox()).toEqual(before)
        await page.screenshot({path:info.outputPath(`send-hover-${theme}-${width}.png`)})
      }
    }
  } finally {await app.close()}
})

test('queue UI supports editing, removing, immediate delivery and draft-safe failures', async ({ page }) => {
  let release!: () => void
  let modelStarted = false
  const held = new Promise<void>(resolve => { release = resolve })
  const app = await fixture({ beforeResponse: prompt => { if (prompt.includes('STEER_TARGET')) { modelStarted = true; return held } } })
  try {
    const task = await app.manager.create({ title: '消息控制验收' })
    await page.goto(app.url)
    await expect(page.getByTestId('composer-input')).toBeVisible()
    app.manager.submit(task.id, 'STEER_TARGET')
    await until(() => modelStarted)
    await expect(page.getByTestId('composer-stop')).toBeVisible()
    await expect(page.locator('.composer-shortcut')).toHaveText('Enter 排队 · Ctrl / ⌘ + Enter 立即发送')
    await page.getByTestId('composer-input').fill('等待下一轮')
    await page.getByTestId('send-message').click()
    await expect(page.getByTestId('queue-banner')).toContainText('等待下一轮')
    await page.getByTestId('queue-banner').getByRole('button', { name: '编辑', exact: true }).click()
    await page.getByLabel('编辑排队消息').fill('编辑后的立即消息')
    await page.getByRole('button', { name: '保存', exact: true }).click()
    await page.getByTestId('queue-banner').getByRole('button', { name: '立即发送' }).click()
    await expect(page.getByTestId('queue-banner')).toHaveCount(0)
    await expect(page.getByText('消息已发送', { exact: true })).toHaveCount(0)
    release()
    await until(() => !app.manager.runs.has(task.id))
    expect(task.turnCount).toBe(1)
    const history = await app.docker.request(`/api/sessions/${task.nativeSessionId}`)
    expect(history.messages.filter((item: any) => item.message?.content === '编辑后的立即消息')).toHaveLength(1)

    app.manager.submit(task.id, 'SLOW')
    await expect(page.getByTestId('composer-stop')).toBeVisible()
    await page.getByTestId('composer-input').fill('停止后仍然保留')
    await page.getByTestId('send-message').click()
    await page.getByTestId('composer-stop').click()
    await expect(page.getByTestId('queue-banner')).toContainText('停止后仍然保留')
    await page.reload()
    await expect(page.getByTestId('queue-banner')).toContainText('停止后仍然保留')
    await page.getByLabel('移除排队消息').click()
    await expect(page.getByTestId('queue-banner')).toHaveCount(0)
    await page.route('**/api/tasks/*/prompts', route => route.fulfill({ status: 503, contentType: 'application/json', body: '{"error":"发送失败验收"}' }))
    await page.getByTestId('composer-input').fill('失败不能清空我的草稿')
    await page.getByTestId('send-message').click()
    await expect(page.getByRole('alert')).toContainText('发送失败验收')
    await expect(page.getByTestId('composer-input')).toHaveValue('失败不能清空我的草稿')
  } finally { release(); await app.close() }
})

test('focused composer is compact, theme-correct and shows context without a known limit', async ({ page }) => {
  const app = await fixture()
  try {
    const task = await app.manager.create({ title: '输入区样式与上下文验收' })
    Object.assign(task, { context: { projectedTokens: 5147, contextWindow: 0 }, turnCount: 2, modelCalls: 4 })
    app.store.saveTask(task)
    await page.goto(app.url)
    const context = page.locator('.session-summary').getByRole('button', { name: /上下文/ })
    await expect(context).toHaveText('上下文 5.1K')
    await expect(context.locator('svg')).toHaveCount(0)
    await context.click()
    await expect(page.getByRole('dialog', { name: '上下文详情' })).toContainText('5,147 tokens')
    await expect(page.getByRole('dialog', { name: '上下文详情' })).toContainText('未配置')
    await page.getByLabel('关闭摘要').click()
    for (const theme of ['苔灰', '暖砂','炭棕', '曜黑（高对比）', '石墨']) {
      await openSettings(page)
      await page.getByRole('navigation',{name:'设置分类'}).getByRole('link',{name:'外观',exact:true}).click()
      await page.getByRole('button', { name: `${theme}` }).click()
      await closeSettings(page)
      await page.getByTestId('composer-input').click()
      const styles = await page.evaluate(() => {
        const style = (selector: string) => getComputedStyle(document.querySelector(selector)!)
        return {
          outline: style('[data-testid="composer-input"]').outlineStyle,
          focusBorder: style('.workbench-input-box').borderTopColor,
          selectBorder: style('[data-testid="approval-mode"]').borderTopWidth,
          attachmentBorder: style('.attach-button').borderTopWidth,
          attachmentBackground: style('.attach-button').backgroundColor,
          accent: style('html').getPropertyValue('--tuck-accent').trim(),
        }
      })
      expect(styles.outline).toBe('none')
      expect(styles.selectBorder).toBe('0px')
      expect(styles.attachmentBorder).toBe('0px')
      expect(styles.attachmentBackground).toBe('rgba(0, 0, 0, 0)')
      expect(styles.focusBorder).toBe(`rgb(${styles.accent.slice(1).match(/../g)!.map(c => parseInt(c, 16)).join(', ')})`)
    }
    for (const width of [1775, 1365, 830, 390]) {
      await page.setViewportSize({ width, height: 800 })
      const geometry = await page.evaluate(() => {
        const rect = (selector: string) => document.querySelector(selector)!.getBoundingClientRect()
        const buttons = [...document.querySelectorAll('.session-summary > button')].map(el => el.getBoundingClientRect())
        return { overflow: document.body.scrollWidth - innerWidth, footer: rect('.composer-footer').height, bottom: rect('.workbench-composer').bottom, gap: buttons[1]!.left - buttons[0]!.right, overlap: rect('[data-testid="composer-input"]').bottom - rect('.workbench-toolbar').top }
      })
      expect(geometry.overflow).toBeLessThanOrEqual(1)
      expect(geometry.bottom).toBeLessThanOrEqual(800)
      expect(geometry.gap).toBeLessThanOrEqual(12)
      expect(geometry.overlap).toBeLessThanOrEqual(0)
      if (width >= 1365) expect(geometry.footer).toBeLessThan(32)
      const before = await page.getByTestId('scrollport').boundingBox()
      await context.click()
      await expect(page.getByRole('dialog', { name: '上下文详情' })).toBeInViewport()
      expect(await page.getByTestId('scrollport').boundingBox()).toEqual(before)
      await page.getByLabel('关闭摘要').click()
    }
    Object.assign(task, { context: { projectedTokens: 5147, contextWindow: 10000 } }); app.store.saveTask(task)
    await page.reload()
    await expect(context).toHaveText('上下文 51%')
    await expect(context.locator('.ring-value')).toHaveAttribute('stroke-dasharray', '51 100')
    await expect(context).toHaveAttribute('title', '5,147 / 10,000 tokens')
    Object.assign(task, { context: { projectedTokens: 12000, contextWindow: 10000 } }); app.store.saveTask(task)
    await page.reload()
    await expect(context).toHaveText('上下文 120%')
    await expect(context.locator('.ring-value')).toHaveAttribute('stroke-dasharray', '100 100')
    await expect(context.locator('.context-ring')).toHaveClass(/danger/)
    Object.assign(task, { context: { projectedTokens: 0, contextWindow: 0 } }); app.store.saveTask(task)
    await page.reload()
    await expect(context).toHaveText('上下文 待上报')
    await expect(context.locator('svg')).toHaveCount(0)
    Object.assign(task, { context: { projectedTokens: 0, contextWindow: 240640 } }); app.store.saveTask(task)
    await page.reload()
    await expect(context).toHaveText('上下文 待上报')
    Object.assign(task, { context: { projectedTokens: 0, contextWindow: 240640, reported: true } }); app.store.saveTask(task)
    await page.reload()
    await expect(context).toHaveText('上下文 0%')
  } finally { await app.close() }
})

test('approval mode is persistent, effective and leaves current approval explicit', async ({ page },info) => {
  const app = await fixture()
  try {
    const task = await app.manager.create({ title: '审批验收' })
    await page.goto(app.url)
    await page.getByTestId('composer-input').fill('REJECT_ONLY')
    await page.getByTestId('send-message').click()
    await expect(page.getByTestId('pending-interaction')).toContainText('write_file')
    for(const width of [320,390,1365]) {
      await page.setViewportSize({width,height:800})
      for(const id of ['deny-interaction','approve-interaction']) {
        const button=page.getByTestId(id)
        await button.hover()
        const geometry=await button.evaluate(el=>{
          const outer=el.getBoundingClientRect(), face=el.querySelector('.control-face')!.getBoundingClientRect()
          return {height:outer.height,faceHeight:face.height,center:Math.abs(outer.top+outer.bottom-face.top-face.bottom)/2,background:getComputedStyle(el).backgroundColor}
        })
        expect(geometry.height).toBe(width<=760?44:30)
        expect(geometry.faceHeight).toBe(30)
        expect(geometry.center).toBeLessThanOrEqual(.5)
        expect(geometry.background).toBe('rgba(0, 0, 0, 0)')
      }
      await page.screenshot({path:info.outputPath(`approval-hover-${width}.png`)})
      const request=page.getByTestId('pending-interaction'), details=request.locator('details'), actions=request.locator('.interaction-actions')
      const folded=(await request.boundingBox())!
      expect(folded.height).toBeLessThan(width<=760?100:86)
      expect(Math.abs((await details.boundingBox())!.y-(await actions.boundingBox())!.y)).toBeLessThanOrEqual(8)
      await details.locator('summary').click()
      expect((await actions.boundingBox())!.y).toBeGreaterThanOrEqual((await details.boundingBox())!.y+(await details.boundingBox())!.height)
      await details.locator('summary').click()
    }
    await page.getByTestId('approval-mode').selectOption('auto')
    await expect(page.getByTestId('approval-mode')).toHaveValue('auto')
    await expect(page.getByTestId('pending-interaction')).toBeVisible()
    await page.getByTestId('approve-interaction').click()
    await expect(page.getByTestId('pending-interaction')).toHaveCount(0)
    await until(() => !app.manager.runs.has(task.id))
    await page.reload()
    await expect(page.getByTestId('approval-mode')).toHaveValue('auto')
    await page.getByTestId('composer-input').fill('REJECT_ONLY')
    await page.getByTestId('send-message').click()
    await expect.poll(() => task.turnCount).toBe(2)
    await until(() => !app.manager.runs.has(task.id))
    await expect(page.getByTestId('pending-interaction')).toHaveCount(0)
    await page.getByTestId('approval-mode').selectOption('manual')
    await page.getByTestId('composer-input').fill('REJECT_ONLY')
    await page.getByTestId('send-message').click()
    await expect(page.getByTestId('pending-interaction')).toBeVisible()
    await page.getByTestId('deny-interaction').click()
  } finally { await app.close() }
})

test('product layout keeps composer, queue and approval reachable at narrow and wide sizes', async ({ page }) => {
  const app = await fixture()
  try {
    const task = await app.manager.create({ title: '很长的对话标题'.repeat(16) })
    app.manager.submit(task.id, 'REJECT_ONLY')
    await until(() => task.pendingInteraction)
    for (let index = 0; index < 12; index++) app.manager.submit(task.id, `排队内容 ${index} ` + '长内容'.repeat(30))
    await page.goto(app.url)
    for (const [width, height] of [[1365, 800], [830, 812], [390, 740]]) {
      await page.setViewportSize({ width, height })
      await page.getByTestId('composer-input').fill('多行输入\n'.repeat(8))
      await expect(page.getByTestId('send-message')).toBeInViewport()
      await expect(page.getByTestId('approve-interaction')).toBeVisible()
      const geometry = await page.evaluate(() => {
        const rect = (selector: string) => document.querySelector(selector)!.getBoundingClientRect()
        return { overflow: document.body.scrollWidth - innerWidth, composer: rect('[data-testid="composer-shell"]').bottom, header: rect('.conversation-header').height, stage: rect('[data-testid="scrollport"]').height, overlap: rect('[data-testid="scrollport"]').bottom - rect('[data-testid="composer-shell"]').top }
      })
      expect(geometry.overflow).toBeLessThanOrEqual(1)
      expect(geometry.composer).toBeLessThanOrEqual(height + 1)
      expect(geometry.header).toBeLessThan(80)
      expect(geometry.stage).toBeGreaterThan(120)
      expect(geometry.overlap).toBeLessThanOrEqual(1)
    }
  } finally { await app.close() }
})
