import { openSettings, closeSettings } from './settings'
import { test, expect } from '@playwright/test'
import { fixture } from '../../tests/fixtures/fixture.mjs'
const png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAIAAAD91JpzAAAACXBIWXMAAAPoAAAD6AG1e1JrAAAAEklEQVQImWMQWhImtCSMAUIBABxOBDE8jR2EAAAAAElFTkSuQmCC','base64')
let app: Awaited<ReturnType<typeof fixture>>
test.beforeAll(async () => { app = await fixture({attachmentModels:true}) })
test.afterAll(async () => { await app.close() })

test('paste, drag, picker, model gate, session draft and image-only submission', async ({page}) => {
  const task = await app.manager.create({title:'附件交互'})
  const other = await app.manager.create({title:'另一会话'})
  await page.goto(app.url)
  await page.getByTestId(`session-${task.id}`).click()
  const input = page.getByTestId('composer-input')
  // Exercise browser paste with mixed text and an actual File payload.
  await input.evaluate((element, data) => {
    const transfer = new DataTransfer()
    transfer.items.add(new File([Uint8Array.from(atob(data), c => c.charCodeAt(0))],'clipboard.png',{type:'image/png'}))
    transfer.setData('text/plain','一起看这张图片')
    element.dispatchEvent(new ClipboardEvent('paste',{bubbles:true,cancelable:true,clipboardData:transfer}))
  },png.toString('base64'))
  await expect(input).toHaveValue('一起看这张图片')
  await expect(page.locator('.draft-attachment[data-status=ready]')).toHaveCount(1)
  await page.getByTestId(`session-${other.id}`).click()
  await expect(page.getByRole('group',{name:'待发送附件'})).toHaveCount(0)
  await page.getByTestId(`session-${task.id}`).click()
  await expect(page.getByRole('group',{name:'待发送附件'})).toContainText('clipboard.png')
  await page.getByTestId('task-model').selectOption('text')
  await expect(page.getByText('当前模型不支持图片；请配置识图模型并启用附件识别工具，或切换聊天模型')).toBeVisible()
  await expect(page.getByTestId('send-message')).toBeDisabled()
  await page.getByTestId('task-model').selectOption('vision')
  await input.fill('')
  await expect(page.getByTestId('send-message')).toBeEnabled()
  await page.getByTestId('send-message').click()
  await expect(page.getByRole('group',{name:'待发送附件'})).toHaveCount(0)
  await expect(page.getByTestId('attachment-image')).toBeVisible()
  // Uploaded/history attachments may omit dimensions; use the decoded image.
  await page.getByTestId('attachment-image').click()
  await expect(page.locator('.media-preview-dialog footer')).toContainText('2 × 2')
  await expect(page.locator('.media-preview-dialog footer')).not.toContainText('?')
  await page.getByRole('button', { name: '关闭图片预览' }).click()
  await page.evaluate(() => {
    const transfer = new DataTransfer(); transfer.items.add(new File(['drop proof'],'dropped.txt',{type:'text/plain'}))
    document.dispatchEvent(new DragEvent('dragenter',{bubbles:true,cancelable:true,dataTransfer:transfer}))
    document.dispatchEvent(new DragEvent('drop',{bubbles:true,cancelable:true,dataTransfer:transfer}))
  })
  await expect(page.getByRole('group',{name:'待发送附件'})).toContainText('dropped.txt')
  await expect(page.locator('.attachment-drop')).toHaveCount(0)
  await page.getByTestId('attachment-input').setInputFiles({name:'picked.txt',mimeType:'text/plain',buffer:Buffer.from('picker proof')})
  await expect(page.locator('.draft-attachment[data-status=ready]')).toHaveCount(2)
  await page.getByRole('button',{name:'移除 dropped.txt',exact:true}).click()
  await page.getByRole('button',{name:'移除 picked.txt',exact:true}).click()
  await expect(page.getByRole('group',{name:'待发送附件'})).toHaveCount(0)
})

test('uploaded image bytes remain previewable with text or missing extensions', async ({page}) => {
  const task = await app.manager.create({title:'按实际图片内容预览'})
  await page.goto(app.url)
  await page.getByTestId(`session-${task.id}`).click()
  await page.getByTestId('task-model').selectOption('vision')
  await page.getByTestId('attachment-input').setInputFiles([
    {name:'evidence.txt',mimeType:'text/plain',buffer:png},
    {name:'extensionless',mimeType:'application/octet-stream',buffer:png},
  ])
  await expect(page.locator('.draft-attachment[data-status=ready]')).toHaveCount(2)
  await page.getByTestId('send-message').click()
  const images = page.getByTestId('attachment-image')
  await expect(images).toHaveCount(2)
  for (const image of [images.first(), images.last()]) {
    await image.click()
    await expect(page.locator('.media-preview-dialog footer')).toContainText('2 × 2')
    await page.getByRole('button',{name:'关闭图片预览'}).click()
  }
})

test('bounded rail, retry feedback, themes and diagnostics have matching geometry', async ({page}) => {
  await page.setViewportSize({width:1365,height:850})
  const task = await app.manager.create({title:'附件布局'})
  await page.goto(app.url)
  await page.getByTestId(`session-${task.id}`).click()
  let attempt = 0
  await page.route('**/upload',async route => {
    attempt++
    if (attempt === 1) await route.fulfill({status:503,contentType:'application/json',body:JSON.stringify({error:'上传失败，请重试'})})
    else if (attempt === 2) await route.fulfill({status:201,contentType:'application/json',body:JSON.stringify({name:'retry.txt',path:'uploads/retry.txt'})})
    else await route.continue()
  })
  await page.getByTestId('attachment-input').setInputFiles({name:'retry.txt',mimeType:'text/plain',buffer:Buffer.from('retry')})
  await expect(page.getByRole('button',{name:'重试 retry.txt'})).toBeVisible()
  await expect(page.getByTestId('send-message')).toBeDisabled()
  await page.getByRole('button',{name:'重试 retry.txt'}).click()
  await expect(page.getByText('上传接口版本不一致，请在任务结束后重启后端')).toBeVisible()
  await expect(page.getByTestId('send-message')).toBeDisabled()
  await page.getByRole('button',{name:'重试 retry.txt'}).click()
  await expect(page.locator('.draft-attachment[data-status=ready]')).toHaveCount(1)
  await page.getByTestId('attachment-input').setInputFiles(Array.from({length:7},(_,i)=>({name:`image-${i}.png`,mimeType:'image/png',buffer:png})))
  await expect(page.locator('.draft-attachment[data-status=ready]')).toHaveCount(8)
  for (const theme of ['苔灰', '暖砂','炭棕', '曜黑（高对比）', '石墨']) {
    await openSettings(page)
    await page.getByRole('navigation',{name:'设置分类'}).getByRole('link',{name:'外观',exact:true}).click()
    await page.getByRole('button',{name:`${theme}`}).click()
    await closeSettings(page)
    const colors = await page.locator('.draft-attachment').first().evaluate(element => {
      const style = getComputedStyle(element)
      return [style.color, style.backgroundColor]
    })
    expect(colors[0]).not.toEqual(colors[1])
  }
  for (const width of [1365,830,390]) {
    await page.setViewportSize({width,height:850})
    const remove=await page.getByRole('button',{name:'移除 retry.txt',exact:true}).boundingBox()
    expect(remove!.width).toBe(28);expect(remove!.height).toBe(28)
    const rail = await page.locator('.draft-attachments').boundingBox()
    expect(rail!.height).toBeLessThan(85)
    expect(await page.evaluate(()=>document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    for (let i=0;i<2;i++) {
      await page.getByTestId('session-menu').click(); await page.getByTestId('diagnostics-open').click()
      await expect(page.getByRole('complementary',{name:'运行详情'})).toBeVisible()
      const panel = await page.getByRole('complementary',{name:'运行详情'}).boundingBox()
      expect(panel!.x + panel!.width).toBeLessThanOrEqual(width+1)
      await page.getByRole('button',{name:'Close diagnostics'}).click()
      const shell = await page.locator('main.conversation-shell').boundingBox()
      expect(shell!.x + shell!.width).toBeGreaterThan(width-2)
    }
  }
  await page.screenshot({path:'test-results/attachment-mobile.png'})
})

test('background reports survive reload without inventing a native child session', async ({page}) => {
  let release!: () => void
  const gate = new Promise<void>(resolve => { release = resolve })
  let childHeld = false, parentHeld = false
  const native = await fixture({beforeResponse: async (prompt, body) => {
    // The child marker belongs to its system instruction, not necessarily the
    // last user message. Hold both child completion and the parent's next step
    // so neither a fast result nor the parent's wait timeout ends the pending state.
    const child = JSON.stringify(body.messages).includes('CHILD_ONLY') && !body.tools?.some(tool => tool.function?.name === 'transfer_task')
    const spawned = prompt.includes('BACKGROUND_LOOP') && body.messages?.some(message => message.tool_calls?.some(call => call.function.name === 'run_background_agent'))
    if(child) childHeld = true
    if(spawned) parentHeld = true
    if(child || spawned) await gate
  }})
  try {
    const root = await native.manager.create({title:'后台生命周期'})
    await page.goto(native.url)
    await page.getByTestId('composer-input').fill('BACKGROUND_LOOP')
    await page.getByTestId('send-message').click()
    await expect(page.getByTestId('agent-switch')).toBeVisible()
    await page.getByTestId('agent-switch').click()
    await expect(page.getByTestId('pending-background-agent')).toContainText('已启动')
    await expect.poll(() => childHeld && parentHeld).toBe(true)
    await page.reload()
    await expect(page.getByTestId('agent-switch')).toBeVisible()
    await page.getByTestId('agent-switch').click()
    await expect(page.getByTestId('pending-background-agent')).toBeVisible()
    release()
    await expect.poll(()=>root.lastTurnReason).toBe('completed')
    await expect(page.getByTestId('pending-background-agent')).toContainText('已完成')
    expect([...native.manager.tasks.values()].filter(task=>task.parentTaskId===root.id)).toHaveLength(0)
    await expect(page.getByTestId('pending-background-agent').getByRole('button')).toHaveCount(0)
    await page.reload()
    await page.getByTestId('agent-switch').click()
    await expect(page.getByTestId('pending-background-agent')).toContainText('已完成')
  } finally { release?.(); await native.close() }
})
