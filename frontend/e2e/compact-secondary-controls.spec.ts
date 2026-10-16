import { expect, test } from '@playwright/test'
import { fixture } from '../../tests/fixtures/fixture.mjs'

test('delivery heading keeps a shared text baseline and wraps without overflow',async({page},info)=>{
  const app=await fixture()
  try {
    const task=await app.manager.create({title:'测试'})
    await page.goto(`${app.url}/#/chat/conversations/${task.id}/delivery`)
    const header=page.locator('.chat-delivery-content > header')
    for(const width of [320,390,1365]) {
      await page.setViewportSize({width,height:800})
      await expect(header.getByRole('heading',{name:'交付',exact:true})).toBeVisible()
      expect(await header.evaluate(el=>getComputedStyle(el).alignItems)).toBe('baseline')
      expect(await header.evaluate(el=>el.scrollWidth-el.clientWidth)).toBeLessThanOrEqual(1)
      await page.screenshot({path:info.outputPath(`delivery-baseline-${width}.png`)})
    }
    await header.getByRole('link',{name:'返回对话'}).click()
    await expect(page.getByTestId('composer-input')).toBeVisible()
  } finally {await app.close()}
})

test('code disclosures and child navigation stay compact across widths',async({page},info)=>{
  const app=await fixture()
  try {
    const task=await app.manager.create({title:'紧凑辅助入口'})
    app.manager.createChild(task,'compact-child','worker')
    app.manager.append(task,{role:'assistant',blocks:[
      {id:'code',type:'code',data:{filename:'very-long-example-filename.ts',language:'typescript',code:Array.from({length:80},(_,i)=>`const value${i} = ${i} // ${'long path / '.repeat(25)}`).join('\n')}},
      {id:'diff',type:'diff',data:{resource:{id:'example',kind:'file',uri:'very-long-example-filename.ts'},lines:Array.from({length:80},(_,i)=>`+ const value${i} = ${i} // ${'long path / '.repeat(25)}`)}},
    ]})
    await page.goto(`${app.url}/#/chat/conversations/${task.id}`)
    const picker=page.getByTestId('agent-switch')
    for(const width of [320,390,760,1365]) {
      await page.setViewportSize({width,height:850})
      await expect(picker).toBeVisible()
      expect(await picker.evaluate(el=>getComputedStyle(el).flexGrow)).toBe('0')
      await picker.click()
      await expect(page.getByTestId('agent-popover')).toBeVisible()
      await page.getByRole('button',{name:'关闭子代理列表'}).click()
      for(const kind of ['code','diff']) {
        // The projector may split a large diff into bounded render units.
        const card=page.getByTestId(`${kind}-block`).first(),button=card.getByRole('button',{name:'展开',exact:true})
        await button.scrollIntoViewIfNeeded()
        if(kind==='code')await expect(card.locator('.shiki')).toBeVisible()
        const collapsedHeight=(await card.boundingBox())!.height
        expect((await button.boundingBox())!.height).toBe(30)
        await button.click()
        await expect(card.getByRole('button',{name:'收起',exact:true})).toHaveAttribute('aria-expanded','true')
        await expect(card).toContainText('value44')
        if(kind==='code')await expect(card.locator('.shiki')).toBeVisible()
        await expect.poll(async()=> (await card.boundingBox())!.height-collapsedHeight).toBeGreaterThan(80)
        const scrollCount=await card.evaluate(el=>[...el.querySelectorAll('*')].filter(node=>{
          const style=getComputedStyle(node)
          return /(auto|scroll)/.test(style.overflowY)&&node.scrollHeight>node.clientHeight+1
        }).length)
        expect(scrollCount).toBe(1)
        const linkGeometry=await card.evaluate(el=>{
          const header=el.querySelector('header')!.getBoundingClientRect(),link=el.parentElement!.querySelector('.message-link')!.getBoundingClientRect(),toggle=el.querySelector('.fold-toggle')!.getBoundingClientRect()
          return {center:Math.abs(link.top+link.height/2-header.top-header.height/2),gap:link.left-toggle.right}
        })
        expect(linkGeometry.center).toBeLessThanOrEqual(1)
        expect(linkGeometry.gap).toBeGreaterThanOrEqual(0)
        expect(await card.locator('pre').evaluate(el=>{el.scrollTop=el.scrollHeight;return el.scrollTop})).toBeGreaterThan(0)
        expect(await card.locator('pre').evaluate(el=>{el.scrollLeft=el.scrollWidth;return el.scrollLeft})).toBeGreaterThan(0)
        await card.locator('pre').evaluate(el=>{el.scrollTop=0;el.scrollLeft=0})
        await card.locator('.fold-toggle').scrollIntoViewIfNeeded()
        await page.screenshot({path:info.outputPath(`${kind}-expanded-${width}.png`)})
        await card.getByRole('button',{name:'收起',exact:true}).click()
        await expect(button).toHaveAttribute('aria-expanded','false')
        await expect.poll(async()=> (await card.boundingBox())!.height).toBeCloseTo(collapsedHeight,0)
        expect(await card.evaluate(el=>el.scrollWidth-el.clientWidth)).toBeLessThanOrEqual(1)
      }
      await page.screenshot({path:info.outputPath(`compact-secondary-${width}.png`)})
      const shortDiff=page.getByTestId('diff-block').last()
      await shortDiff.locator('header').click()
      const shortAlignment=await shortDiff.evaluate(el=>{
        const h=el.querySelector('header')!.getBoundingClientRect(),l=el.parentElement!.querySelector('.message-link')!.getBoundingClientRect()
        return {height:h.height,offset:Math.abs(h.top+h.height/2-l.top-l.height/2)}
      })
      expect(shortAlignment.height).toBe(32)
      expect(shortAlignment.offset).toBeLessThanOrEqual(1)
    }
  } finally {await app.close()}
})
