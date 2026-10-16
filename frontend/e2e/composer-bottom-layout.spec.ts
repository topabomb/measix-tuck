import {test,expect} from '@playwright/test'
import {fixture} from '../../tests/fixtures/fixture.mjs'

const plan={title:'底部计划',items:Array.from({length:24},(_,i)=>({id:`step-${i}`,text:`第 ${i+1} 项：验证底部布局与操作入口`,status:i===0?'in-progress':'pending'}))}

test('errors and run feedback precede the plan next to the input at every width',async({page},info)=>{
  const app=await fixture({attachmentModels:true})
  try{
    const task=await app.manager.create({title:'底部顺序验收'})
    Object.assign(task,{status:'working',activity:{phase:'processing',startedAt:Date.now()},activePlan:plan,lastFailure:{code:'layout_check',message:'布局验收：保留错误信息'},queue:[{id:'queued',prompt:'待发送的验证消息'}]})
    for(let i=0;i<18;i++)app.manager.append(task,{role:'assistant',blocks:[{id:`density-${i}`,type:'tool-call',data:{callId:`density-call-${i}`,name:'read_file',input:{path:`layout-example-${i}.md`},status:'success'}}]})
    app.store.saveTask(task)
    for(const width of [320,390,840,1365]){
      await page.setViewportSize({width,height:760})
      await page.goto(`${app.url}/#/chat/conversations/${task.id}`)
      const error=page.getByTestId('last-turn-failure'),feedback=page.getByTestId('run-feedback'),summary=page.getByTestId('active-plan-summary'),input=page.locator('.workbench-input-box')
      await expect(feedback).toContainText('正在请求')
      await expect(summary).toContainText('0/24')
      await expect(page.getByTestId('composer-stop')).toBeEnabled()
      await expect(page.getByTestId('conversation-loading')).toBeHidden()
      expect((await page.getByTestId('queue-banner').boundingBox())!.height).toBeLessThanOrEqual(27)
      expect((await summary.boundingBox())!.height).toBeLessThanOrEqual(27)
      if(width>760)expect((await page.locator('.run-feedback-row').boundingBox())!.height).toBeLessThanOrEqual(27)
      // This representative one-line state should leave room for message
      // history while keeping the primary mobile controls easy to hit. Allow
      // border rounding across device scales; the former spacing exceeds this.
      expect((await page.getByTestId('composer-shell').boundingBox())!.height).toBeLessThanOrEqual(width<=760?270:248)
      expect((await page.getByTestId('scrollport').boundingBox())!.height).toBeGreaterThanOrEqual(width<=760?387:460)
      if(width<=760){
        for(const button of [page.getByRole('button',{name:'添加文件',exact:true}),page.getByRole('button',{name:'对话设置',exact:true}),page.getByTestId('send-message')])expect((await button.boundingBox())!.height).toBeGreaterThanOrEqual(44)
      }
      if(width<=760)expect((await page.getByTestId('composer-stop').boundingBox())!.height).toBe(32)
      const ordered=await page.locator('.workbench-composer').evaluate(el=>{
        const nodes=['[data-testid="last-turn-failure"]','[data-testid="run-feedback"]','[data-testid="active-plan-strip"]','.workbench-input-box'].map(s=>el.querySelector(s)!)
        return nodes.slice(1).every((node,i)=>!!(nodes[i]!.compareDocumentPosition(node)&Node.DOCUMENT_POSITION_FOLLOWING))
      })
      expect(ordered).toBe(true)
      const e=(await error.boundingBox())!,r=(await feedback.boundingBox())!,p=(await summary.boundingBox())!,box=(await input.boundingBox())!
      expect(e.y+e.height).toBeLessThanOrEqual(r.y+1)
      expect(r.y+r.height).toBeLessThanOrEqual(p.y+1)
      expect(box.y-p.y-p.height).toBeLessThanOrEqual(3)
      expect(await page.getByTestId('active-plan-strip').evaluate(el=>getComputedStyle(el).zIndex)).toBe('auto')
      await summary.click()
      const context=page.locator('.composer-context'),details=page.getByTestId('active-plan-popover')
      await expect(details).toBeVisible()
      expect((await details.boundingBox())!.height).toBeLessThanOrEqual(160)
      expect(await context.evaluate(el=>el.clientHeight)).toBeLessThanOrEqual(280)
      await context.evaluate(el=>{el.scrollTop=el.scrollHeight})
      await expect(page.getByTestId('composer-input')).toBeInViewport({ratio:1})
      await page.getByText('第 24 项：验证底部布局与操作入口',{exact:true}).scrollIntoViewIfNeeded()
      await expect(page.getByText('第 24 项：验证底部布局与操作入口',{exact:true})).toBeInViewport()
      await summary.scrollIntoViewIfNeeded();await summary.click()
      await context.evaluate(el=>{el.scrollTop=0})
      await expect(page.getByTestId('composer-stop')).toBeInViewport({ratio:1})
      expect(await page.evaluate(()=>document.body.scrollWidth-innerWidth)).toBeLessThanOrEqual(1)
      await page.screenshot({path:info.outputPath(`feedback-plan-${width}.png`)})
    }
    task.status='waiting';task.pendingInteraction={id:'layout-approval',kind:'approval',title:'确认验证操作',detail:'{}',toolName:'write_file',callId:'layout-call'}
    app.manager.publish(task,{type:'state',task})
    await expect(page.getByTestId('pending-interaction')).toBeVisible()
    expect(await page.locator('.composer-context').evaluate(el=>!!(el.querySelector('[data-testid="pending-interaction"]')!.compareDocumentPosition(el.querySelector('[data-testid="queue-banner"]')!)&Node.DOCUMENT_POSITION_FOLLOWING))).toBe(true)
    await expect(page.getByTestId('approve-interaction')).toBeInViewport()
  }finally{await app.close()}
})

test('native model choices remain above the expanded plan and work in phone and desktop layouts',async({page},info)=>{
  const app=await fixture({attachmentModels:true})
  try{
    const task=await app.manager.create({title:'计划与菜单层级验收'})
    Object.assign(task,{activePlan:plan,lastFailure:{code:'layout_check',message:'布局验收：错误应在计划上方'}});app.store.saveTask(task)
    for(const width of [320,390,840,1365]){
      await page.setViewportSize({width,height:760})
      await page.goto(`${app.url}/#/chat/conversations/${task.id}`)
      await page.getByTestId('active-plan-summary').click()
      const mobile=width<=760
      if(mobile)await page.getByRole('button',{name:'对话设置',exact:true}).click()
      const select=page.getByLabel('模型',{exact:true})
      await select.click()
      await expect(select).toHaveJSProperty('value',task.modelId)
      const choices=select.locator('option:not([disabled])')
      await expect(choices).toHaveCount(2)
      expect(await choices.evaluateAll(items=>items.every(el=>{
        const b=el.getBoundingClientRect(),target=document.elementFromPoint((b.left+b.right)/2,(b.top+b.bottom)/2)
        return b.left>=0&&b.right<=innerWidth&&b.top>=0&&b.bottom<=innerHeight&&(target===el||el.contains(target))
      }))).toBe(true)
      await page.screenshot({path:info.outputPath(`model-plan-${width}.png`)})
      const target=task.modelId==='vision'?'text':'vision'
      await select.locator(`option[value="${target}"]`).click()
      await expect.poll(()=>task.modelId).toBe(target)
      await expect(select).toHaveValue(target)
      if(mobile){await page.getByRole('button',{name:'关闭对话设置',exact:true}).click();await expect(page.getByRole('button',{name:'对话设置',exact:true})).toBeFocused()}
      await page.getByTestId('active-plan-summary').scrollIntoViewIfNeeded();await page.getByTestId('active-plan-summary').click()
      await expect(page.getByTestId('active-plan-popover')).toBeHidden()
      await expect(page.getByTestId('composer-input')).toBeInViewport({ratio:1})
    }
  }finally{await app.close()}
})
