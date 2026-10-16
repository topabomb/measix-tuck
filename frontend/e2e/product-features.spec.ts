import { openSettings, closeSettings } from './settings'
import { test, expect } from '@playwright/test'
import { fixture, until } from '../../tests/fixtures/fixture.mjs'
import { readFile } from 'node:fs/promises'

test('plan and delegation summaries mount details on demand with bounded height', async ({ page }) => {
  const app = await fixture()
  try {
    const task=await app.manager.create({title:'摘要高度验收'})
    app.store.saveMessage(task.id,{id:'progress',index:0,turnId:'progress-turn',role:'assistant',blocks:[
      {id:'plan',type:'plan',data:{items:Array.from({length:40},(_,i)=>({id:`p${i}`,text:`第 ${i} 项工作，包含需要完整换行展示的说明`,status:'completed'}))}},
      {id:'delegation',type:'delegation',data:{runs:Array.from({length:40},(_,i)=>({runId:`r${i}`,title:`worker ${i}`,mode:'background',status:i===0?'failed':'completed'}))}},
    ]}); task.logicalCount=1; app.store.saveTask(task)
    await page.goto(app.url)
    for (const kind of ['plan-block','delegation-block']) {
      const record=page.getByTestId(kind)
      await expect(record).toBeVisible()
      expect((await record.boundingBox())!.height).toBeLessThanOrEqual(36)
      await expect(record.locator('.progress-body')).toHaveCount(0)
      await record.getByRole('button').click()
      await expect(record.locator('.progress-body')).toBeVisible()
      expect((await record.locator('.progress-body').boundingBox())!.height).toBeLessThanOrEqual(301)
      await record.locator('.progress-summary').click()
      await expect(record.locator('.progress-body')).toHaveCount(0)
    }
    await expect(page.getByTestId('delegation-block').getByRole('img',{name:'失败',exact:true})).toBeVisible()
    await page.setViewportSize({width:390,height:844})
    await page.getByTestId('plan-block').getByRole('button').click()
    await expect(page.getByTestId('composer-input')).toBeVisible()
    for (const width of [320, 390, 1365]) {
      await page.setViewportSize({width,height:844})
      const plan=page.getByTestId('plan-block')
      const title=plan.locator('.progress-summary')
      await expect(title).toHaveAttribute('aria-expanded','true')
      const layout=await plan.evaluate(el=>{
        const title=el.querySelector('.progress-summary')!
        const arrow=title.lastElementChild!.getBoundingClientRect()
        const link=el.parentElement!.querySelector('.message-link')!.getBoundingClientRect()
        const box=title.getBoundingClientRect()
        return {height:box.height,gap:link.left-arrow.right,center:Math.abs(link.y+link.height/2-box.y-box.height/2)}
      })
      expect(layout.height).toBe(32)
      expect(layout.gap).toBeGreaterThanOrEqual(0)
      expect(layout.center).toBeLessThanOrEqual(1)
      await title.click()
      await expect(plan.locator('..').locator('.message-link')).toBeHidden()
      await title.click()
    }
    // Expanding a row schedules the existing virtualizer's height measurement.
    // Check the resulting layout after that measurement has settled.
    const geometry=()=>page.getByTestId('scrollport').evaluate(el=>{
      const rows=[...el.querySelectorAll('[data-virtual-item=true]')].map(e=>e.getBoundingClientRect()).sort((a,b)=>a.top-b.top)
      return {overlap:Math.max(0,...rows.slice(1).map((r,i)=>rows[i]!.bottom-r.top)),overflow:document.body.scrollWidth-innerWidth}
    })
    await expect.poll(async()=>{const result=await geometry();return Math.max(result.overlap,result.overflow)}).toBeLessThanOrEqual(1)
  } finally {await app.close()}
})

test('syntax colors remain paired, media stays compact and theme switches preserve code DOM', async ({ page }) => {
  const app = await fixture()
  try {
    const task = await app.manager.create({ title: '代码与附件验收' })
    const svg = 'data:image/svg+xml,' + encodeURIComponent('<svg xmlns="http://www.w3.org/2000/svg" width="1200" height="2400"><rect width="1200" height="2400" fill="#4477aa"/></svg>')
    app.store.saveMessage(task.id, { id:'media-test', index:0, turnId:'media', role:'assistant', blocks:[
      { id:'code-test', type:'code', data:{ language:'typescript', code:'// A readable comment\nconst value = "text"\nexport function result(count: number) { return count + 42 }' } },
      { id:'attachments-test', type:'attachments', data:{ items:Array.from({length:8}, (_,i)=>({id:`file-${i}`,kind:i===0?'image':'file',name:i===0?'long-image.svg':`report-${i}.txt`,mimeType:i===0?'image/svg+xml':'text/plain',width:1200,height:2400,sizeBytes:120,src:i===0?svg:undefined})) } },
    ] })
    task.logicalCount=1; app.store.saveTask(task)
    await page.goto(app.url)
    await expect(page.locator('.shiki')).toBeVisible()
    const original = await page.locator('.shiki').innerHTML()
    const initialHeight = (await page.locator('.shiki').boundingBox())!.height
    for (const name of ['墨蓝','苔灰','暖砂','炭棕','曜黑（高对比）','石墨']) {
      await openSettings(page)
      await page.getByRole('navigation',{name:'设置分类'}).getByRole('link',{name:'外观',exact:true}).click()
      await page.getByRole('button',{name:`${name}`}).click()
      await closeSettings(page)
      const contrast = await page.locator('.shiki').evaluate(pre => {
        const luminance = (color:string) => {
          const rgb=(color.match(/[\d.]+/g) || []).slice(0,3).map(Number).map(c=>c/255).map(c=>c<=.04045?c/12.92:((c+.055)/1.055)**2.4)
          return rgb[0]!*.2126+rgb[1]!*.7152+rgb[2]!*.0722
        }
        const background=getComputedStyle(pre).backgroundColor
        const bg=luminance(background)
        const ratios=[...pre.querySelectorAll('span[style]')].filter(e=>e.textContent?.trim()).map(e=>{const fg=luminance(getComputedStyle(e).color);return (Math.max(bg,fg)+.05)/(Math.min(bg,fg)+.05)})
        return {background, minimum:Math.min(...ratios)}
      })
      expect(contrast.background).not.toBe('rgba(0, 0, 0, 0)')
      expect(contrast.minimum).toBeGreaterThanOrEqual(4.5)
      expect(await page.locator('.shiki').innerHTML()).toBe(original)
      expect((await page.locator('.shiki').boundingBox())!.height).toBe(initialHeight)
    }
    const image=page.getByTestId('attachment-image')
    expect((await image.boundingBox())!.height).toBeLessThanOrEqual(170)
    expect(await page.getByTestId('attachment-file').count()).toBe(3)
    await image.click()
    await expect(page.getByRole('dialog',{name:'图片预览 long-image.svg'})).toBeVisible()
    await expect(page.getByRole('button',{name:'关闭图片预览'})).toBeFocused()
    for (const width of [320,390,1365]) {
      await page.setViewportSize({width,height:844})
      const geometry=await page.getByRole('dialog',{name:'图片预览 long-image.svg'}).evaluate(el=>{
        const box=el.getBoundingClientRect(),header=el.querySelector('.preview-heading')!.getBoundingClientRect()
        const close=el.querySelector('button')!.getBoundingClientRect()
        return {height:header.height,close:close.height,overflow:el.scrollHeight-el.clientHeight,left:box.left,right:box.right,viewport:innerWidth}
      })
      expect(geometry.height).toBe(44)
      expect(geometry.close).toBe(30)
      expect(geometry.overflow).toBeLessThanOrEqual(1)
      expect(geometry.left).toBeGreaterThanOrEqual(0)
      expect(geometry.right).toBeLessThanOrEqual(geometry.viewport)
    }
    await page.keyboard.press('Tab')
    await expect(page.getByRole('link',{name:'打开图片 ↗'})).toBeFocused()
    await page.keyboard.press('Tab')
    await expect(page.getByRole('button',{name:'关闭图片预览'})).toBeFocused()
    await page.getByRole('button',{name:'关闭图片预览'}).click()
    await page.getByRole('button',{name:'展开其余 4 项'}).click()
    expect(await page.getByTestId('attachment-file').count()).toBe(7)
    expect((await page.locator('.attachment-items').boundingBox())!.height).toBeLessThanOrEqual(333)
    await page.setViewportSize({width:390,height:844})
    expect(await page.evaluate(()=>document.body.scrollWidth-innerWidth)).toBeLessThanOrEqual(1)
  } finally { await app.close() }
})

test('themes, external palette, summaries and native fork remain usable in the product shell', async ({ page }, testInfo) => {
  const app = await fixture()
  try {
    const task = await app.manager.create({ title: '产品验收' })
    app.manager.submit(task.id, 'FIRST'); await until(() => !app.manager.runs.has(task.id))
    app.manager.submit(task.id, 'SECOND'); await until(() => !app.manager.runs.has(task.id))
    await page.goto(app.url)
    await expect(page.getByTestId('composer-input')).toBeVisible()
    await openSettings(page)
    await page.getByRole('navigation',{name:'设置分类'}).getByRole('link',{name:'外观',exact:true}).click()
    await expect(page.getByRole('button', { name: '纸白', exact:true })).toHaveCount(0)
    await expect(page.locator('.theme-preview .tuck-mark')).toHaveCount(6)
    await expect(page.locator('.theme-preview strong')).toHaveText(Array(6).fill('Tuck'))
    const cards = await page.locator('.theme-grid > button').evaluateAll(els=>els.map(el=>({label:el.getAttribute('aria-label'),x:el.getBoundingClientRect().x,y:el.getBoundingClientRect().y})))
    expect(cards.map(card=>card.label)).toEqual(['墨蓝','石墨','暖砂','炭棕','苔灰','曜黑（高对比）'])
    for (let index=0;index<cards.length;index+=2) { expect(cards[index]!.y).toBe(cards[index+1]!.y); expect(cards[index]!.x).toBeLessThan(cards[index+1]!.x) }
    for (const name of ['苔灰', '暖砂','炭棕', '曜黑（高对比）', '石墨']) {
      await page.getByRole('button', { name: `${name}` }).click()
      await expect(page.getByRole('button', { name: `${name}` })).toHaveAttribute('aria-pressed', 'true')
      const bounds = await page.getByRole('region',{name:'外观设置',exact:true}).boundingBox()
      expect(bounds!.x).toBeGreaterThanOrEqual(0); expect(bounds!.y).toBeGreaterThanOrEqual(0)
      if (name === '苔灰') {
        const preview = await page.getByRole('button',{name,exact:true}).locator('.theme-preview').evaluate(el=>({height:el.getBoundingClientRect().height,bg:getComputedStyle(el).backgroundColor,mark:getComputedStyle(el.querySelector('svg')!).color,text:getComputedStyle(el).color}))
        expect(preview).toEqual({height:46,bg:'rgb(169, 174, 167)',mark:'rgb(82, 59, 41)',text:'rgb(37, 42, 36)'})
        expect(await page.locator('.theme-preview .tuck-mark').first().evaluate(el=>el.getBoundingClientRect().height)).toBe(32)
        expect(await page.locator('.theme-preview strong').first().evaluate(el=>getComputedStyle(el).fontSize)).toBe('20px')
        expect(await page.locator('.theme-preview').first().evaluate(el=>{const box=el.getBoundingClientRect(),mark=el.querySelector('svg')!.getBoundingClientRect();return Math.round(mark.x-box.x)})).toBe(11)
        await page.screenshot({path:testInfo.outputPath('moss-brand-themes.png')})
      }
    }
    await page.getByText('加载外部主题', { exact: true }).click()
    const external = await readFile(new URL('../public/themes/ocean.json', import.meta.url))
    await page.getByLabel('导入主题文件').setInputFiles({ name: 'ocean.json', mimeType: 'application/json', buffer: external })
    await expect(page.getByRole('button', { name: '海洋' })).toHaveAttribute('aria-pressed', 'true')
    await page.getByLabel('主题地址').fill(app.url + '/themes/ocean.json')
    await page.getByRole('button', { name:'加载', exact:true }).click()
    // Ocean is already selected from the file import. Its pressed state cannot
    // signal completion of the second, asynchronous URL import.
    await expect(page.getByRole('button', { name:'加载', exact:true })).toBeEnabled()
    await expect(page.getByRole('button', { name: '海洋' })).toHaveAttribute('aria-pressed', 'true')
    await page.getByLabel('导入主题文件').setInputFiles({ name: 'invalid.json', mimeType: 'application/json', buffer: Buffer.from('{}') })
    await expect(page.getByRole('alert')).toBeVisible()
    await expect(page.getByRole('button', { name: '海洋' })).toHaveAttribute('aria-pressed', 'true')
    await closeSettings(page)
    await page.reload()
    await expect(page.locator('html')).toHaveAttribute('data-tuck-theme', 'ocean')
    await page.getByRole('button', { name: /2 轮 ·/ }).click()
    await expect(page.getByRole('dialog', { name: '会话统计' })).toContainText('模型调用')
    await page.getByRole('button', { name: '关闭摘要' }).click()
    await page.getByTestId('session-menu').click()
    await page.getByRole('button', { name: '创建分支', exact: true }).click()
    await expect(page.getByRole('dialog', { name: '创建分支' })).toContainText('共享工作目录')
    await page.getByRole('dialog', { name: '创建分支' }).getByRole('button', { name: '创建分支' }).click()
    await expect(page.getByTestId('composer-input')).toHaveValue('SECOND')
    await expect(page.getByRole('button', { name: '来源', exact:true })).toBeVisible()
    expect(task.turnCount).toBe(2)
  } finally { await app.close() }
})

test('product activity folding, theme changes and Latest keep long history bounded and disjoint', async ({ page }) => {
  const app = await fixture()
  try {
    const task = await app.manager.create({ title: '长历史布局验收' })
    const peers = []
    for (let i=0;i<3;i++) peers.push(await app.manager.create({title:`切换验收 ${i}`}))
    app.store.transaction(() => {
      for (let index = 0; index < 5000; index++) app.store.saveMessage(task.id, { id: `${task.id}:m-${index}`, index, turnId: `${task.id}:turn-${index}`, role: 'assistant', blocks: [{ id: `activity-${index}`, type: 'tool-call', data: { name: 'read_file', callId: `call-${index}`, input: { path: `file-${index}.txt`, details: '多行工具参数与输出\n'.repeat(100) }, status: 'success' } }] })
      task.logicalCount = 5000; app.store.saveTask(task)
    })
    await page.goto(app.url + '?inspect=1')
    await page.getByTestId(`session-${task.id}`).click()
    await page.getByTestId('jump-input').fill('2500'); await page.getByTestId('jump-button').click()
    const row = page.locator('[data-message-index="2500"]').first()
    await expect(row).toBeVisible()
    // A mounted row may still be at the estimated position during navigation.
    // Expand only after the jump commits, so the saved baseline is a reader
    // checkpoint rather than an intermediate frame of the preceding jump.
    await expect(page.getByTestId('reader-position')).toHaveText('#2,500')
    const activity = row.getByTestId('tool-block')
    const folded = await activity.boundingBox(); expect(folded!.height).toBeLessThanOrEqual(38)
    await activity.getByRole('button').click()
    await expect(activity.locator('pre')).toBeVisible()
    expect((await activity.locator('pre').boundingBox())!.height).toBeLessThanOrEqual(365)
    const geometry = async () => page.getByTestId('scrollport').evaluate(stage => {
      const rows = [...stage.querySelectorAll<HTMLElement>('[data-virtual-item="true"]')].map(el => el.getBoundingClientRect()).sort((a,b) => a.top - b.top)
      let overlap = 0
      for (let i=1;i<rows.length;i++) overlap = Math.max(overlap, rows[i-1]!.bottom-rows[i]!.top)
      return { overlap, count:rows.length, overflow:document.body.scrollWidth-innerWidth }
    })
    await expect.poll(async () => (await geometry()).overlap).toBeLessThanOrEqual(1)
    expect((await geometry()).count).toBeLessThan(180)
    const anchorTop = (await activity.boundingBox())!.y
    // Evict the source viewport, then restore an expanded row in the middle of
    // persisted history. This exercises the production prefetch + LRU path.
    for (const peer of peers) await page.getByTestId(`session-${peer.id}`).click()
    await page.getByTestId(`session-${task.id}`).click()
    await expect(activity.locator('pre')).toBeVisible()
    await expect.poll(async () => Math.abs((await activity.boundingBox())!.y-anchorTop)).toBeLessThan(4)
    await expect.poll(async () => (await geometry()).overlap).toBeLessThanOrEqual(1)
    const latest = page.getByTestId('jump-latest')
    await expect(latest).toBeVisible()
    const latestBox = await latest.boundingBox(), stage = await page.getByTestId('scrollport').boundingBox()
    expect(latestBox!.y + latestBox!.height).toBeLessThanOrEqual(stage!.y + stage!.height)
    await openSettings(page)
    await page.getByRole('navigation',{name:'设置分类'}).getByRole('link',{name:'外观',exact:true}).click()
    await page.getByRole('button', { name:'苔灰' }).click()
    await closeSettings(page)
    await expect.poll(async () => (await geometry()).overlap).toBeLessThanOrEqual(1)
    await latest.click()
    await expect(page.locator('[data-message-index="4999"]').first()).toBeVisible()
    await expect(latest).toBeHidden()
    expect((await geometry()).overflow).toBeLessThanOrEqual(1)
  } finally { await app.close() }
})
