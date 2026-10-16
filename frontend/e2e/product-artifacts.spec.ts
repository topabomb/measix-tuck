import { openSettings, closeSettings } from './settings'
import { test, expect } from '@playwright/test'
import { readFile, writeFile } from 'node:fs/promises'
import { resolve } from 'node:path'
import { fixture, until } from '../../tests/fixtures/fixture.mjs'

test('artifact panel stays bounded, shares the workspace and separates viewing from editing',async({page})=>{
  const app=await fixture()
  try {
    const root=await app.manager.create({title:'产物验收'})
    const initialContent = '# 产物内容\n' + '预览长文件应从顶部开始\n'.repeat(100)
    await writeFile(resolve(root.workspace,'report.md'),initialContent)
    const child=app.manager.createChild(root,'artifact-child','worker','background')
    app.manager.settle(child,'completed')
    await page.goto(app.url)
    await page.getByTestId('agent-switch').click()
    await page.getByTestId(`agent-choice-${child.id}`).click()
    await expect(page.getByTestId('composer-input')).toHaveCount(0)
    await expect(page.locator('.session-summary')).toHaveText('运行详情',{useInnerText:true})
    await page.getByTestId('task-files').click()
    const panel=page.getByRole('dialog',{name:'任务产物',exact:true})
    await expect(panel).toContainText('父子会话共享工作目录')
    await expect(panel.locator('.tuck-file-row')).toHaveCount(1)
    expect((await panel.boundingBox())!.height).toBeLessThan(340)
    await expect(panel.getByRole('link',{name:'下载 report.md'})).toHaveAttribute('href',`/api/tasks/${root.id}/files?path=report.md&download=1`)
    await panel.locator('.tuck-file-row').click()
    const preview=page.getByRole('dialog',{name:'查看任务产物',exact:true}),content=preview.getByRole('textbox',{name:'文件内容'})
    await expect(content).toHaveValue(initialContent); await expect(content).toHaveAttribute('readonly','')
    expect(await content.evaluate(el => el.scrollTop)).toBe(0)
    await preview.getByRole('button',{name:'编辑',exact:true}).click()
    await content.fill('# 修改后的产物')
    await preview.getByRole('button',{name:'关闭文件预览',exact:true}).click()
    await expect(preview).toContainText('有未保存的修改')
    // A short viewport must shrink the editor, not scroll away the close/save controls.
    await page.setViewportSize({width:320,height:240})
    await expect.poll(()=>preview.locator('form').evaluate(el=>el.scrollHeight-el.clientHeight)).toBeLessThanOrEqual(1)
    for (const name of ['关闭文件预览','继续编辑','放弃修改','保存文件']) {
      const bounds=(await preview.getByRole('button',{name,exact:true}).boundingBox())!
      expect(bounds.y).toBeGreaterThanOrEqual(0)
      expect(bounds.y+bounds.height).toBeLessThanOrEqual(240)
    }
    await page.setViewportSize({width:1280,height:720})
    await preview.getByRole('button',{name:'继续编辑'}).click()
    let releaseSave!: () => void
    const saveGate = new Promise<void>(resolve => { releaseSave = resolve })
    await page.route('**/save-file', async route => { await saveGate; await route.continue() })
    await preview.getByRole('button',{name:'保存文件'}).click()
    await expect(preview.getByRole('button',{name:'保存文件'})).toBeDisabled()
    await content.fill('# 保存期间继续编辑')
    releaseSave()
    await expect(preview.getByRole('button',{name:'保存文件'})).toBeEnabled()
    expect(await readFile(resolve(root.workspace,'report.md'),'utf8')).toBe('# 修改后的产物')
    await expect(content).toHaveValue('# 保存期间继续编辑')
    await preview.getByRole('button',{name:'保存文件'}).click()
    await expect(content).toHaveAttribute('readonly','')
    expect(await readFile(resolve(root.workspace,'report.md'),'utf8')).toBe('# 保存期间继续编辑')
    await preview.getByRole('button',{name:'关闭文件预览',exact:true}).click()
    await Promise.all(Array.from({length:44},(_,i)=>writeFile(resolve(root.workspace,`file-${i}.txt`),'small file')))
    await panel.getByRole('button',{name:'刷新',exact:true}).click()
    await expect(panel.locator('.tuck-file-row')).toHaveCount(40)
    await panel.getByRole('button',{name:'下一页'}).click()
    await expect(panel.locator('.tuck-file-row')).toHaveCount(5)
    await panel.getByRole('textbox',{name:'筛选产物'}).fill('report.md')
    await expect(panel.locator('.tuck-file-row')).toHaveCount(1)
    await panel.getByRole('button',{name:'关闭文件'}).click()
    for(const theme of ['苔灰','暖砂','炭棕','曜黑（高对比）','石墨']) {
      await openSettings(page)
      await page.getByRole('navigation',{name:'设置分类'}).getByRole('link',{name:'外观',exact:true}).click()
      await page.getByRole('button',{name:`${theme}`}).click()
      await closeSettings(page)
      for(const width of [320,390,1365]) {
        await page.setViewportSize({width,height:640})
        if(width<=760){await page.getByTestId('session-menu').click();await page.getByRole('button',{name:'产物',exact:true}).click()}
        else await page.getByTestId('task-files').click()
        const rect=(await panel.boundingBox())!
        expect(rect.x).toBeGreaterThanOrEqual(0); expect(rect.x+rect.width).toBeLessThanOrEqual(width)
        expect(rect.y+rect.height).toBeLessThanOrEqual(640)
        expect(await panel.evaluate(el=>el.scrollWidth-el.clientWidth)).toBeLessThanOrEqual(1)
        const color=await panel.evaluate(el=>({actual:getComputedStyle(el).backgroundColor,expected:getComputedStyle(document.documentElement).getPropertyValue('--tuck-surface').trim()}))
        expect(color.actual).toBe(`rgb(${color.expected.slice(1).match(/../g)!.map(c=>parseInt(c,16)).join(', ')})`)
        await panel.getByRole('button',{name:'关闭文件'}).click()
        await expect(width<=760?page.getByTestId('session-menu'):page.getByTestId('task-files')).toBeFocused()
        const footer=(await page.locator('.child-session-caption').boundingBox())!
        expect(footer.y+footer.height).toBeLessThanOrEqual(640)
      }
    }
    await page.getByRole('link',{name:'← 返回父会话',exact:true}).click()
    await expect(page.getByTestId('composer-input')).toBeVisible()
  }finally{await app.close()}
})

test('background reports do not invent child sessions or usage statistics',async({page})=>{
  const app=await fixture({attachmentModels:true})
  try{
    const root=await app.manager.create({title:'后台统计'})
    app.manager.submit(root.id,'BACKGROUND_LOOP')
    await until(()=>!app.manager.runs.has(root.id))
    expect([...app.manager.tasks.values()].filter(task=>task.parentTaskId===root.id)).toHaveLength(0)
    await page.goto(`${app.url}/#/chat/conversations/${root.id}`)
    await page.getByTestId('agent-switch').click()
    await expect(page.getByTestId('pending-background-agent')).toContainText('已完成')
    await expect(page.getByTestId('pending-background-agent').getByRole('button')).toHaveCount(0)
  }finally{await app.close()}
})
