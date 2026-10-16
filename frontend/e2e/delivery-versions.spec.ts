import { test, expect } from '@playwright/test'
import { writeFile, readFile } from 'node:fs/promises'
import { resolve } from 'node:path'
import { createServer, request as proxyRequest } from 'node:http'
import { platformFixture } from '../../tests/fixtures/platform-fixture.mjs'
import { fixture, until } from '../../tests/fixtures/fixture.mjs'
import { openSettings, closeSettings } from './settings'

test('UI delivery requirement continues a completed native turn, validates files and ends after successful delivery',async({page},info)=>{
  const f=await platformFixture({native:true}),app=f.app!
  try{
    await fetch(f.url+'/api/connection/enroll',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({material:f.material})})
    let attempts=0,reminders=0
    f.controls.chatReply=(body:any)=>{
      const last=body.messages.findLastIndex((m:any)=>m.role==='user'),recent=body.messages.slice(last+1)
      const reminder=String(body.messages[last]?.content).includes('<tuck_automation type="delivery_required">')
      if(!reminder)expect(['执行报告验收','关闭后的普通执行']).toContain(body.messages[last]?.content)
      if(reminder){
        if(!recent.some((m:any)=>m.role==='tool'))reminders++
        const results=recent.filter((m:any)=>m.role==='tool')
        if(results.length<2){attempts++;return {tool_calls:[{index:0,id:'same-provider-id',type:'function',function:{name:'tuck_deliver_result',arguments:JSON.stringify({summary:'设置生效，已验证并交付报告。',files:[{path:results.length?'report.txt':'missing.txt'}]})}}]}}
      }
      return {content:'本轮执行完成。'}
    }
    const task=await app.manager.create({title:'T-chck · 2',modelId:f.snapshot.models[0].modelId,approvalMode:'auto'})
    await writeFile(resolve(task.workspace,'report.txt'),'actual native delivery')
    await page.goto(`${f.url}/#/chat/conversations/${task.id}`)
    await page.getByRole('button',{name:'交付摘要',exact:true}).click();await expect(page.getByRole('dialog',{name:'交付摘要',exact:true})).toContainText('尚无交付');await expect(page.getByRole('dialog',{name:'交付摘要',exact:true})).toContainText('已关闭');await page.getByRole('button',{name:'关闭交付摘要'}).click()
    await openSettings(page);await page.locator('main.settings-mode a[href="#/settings/orchestration"]').filter({visible:true}).first().click()
    for(const width of [1280,390]){
      await page.setViewportSize({width,height:780})
      const alignment=await page.locator('.require-delivery').evaluate(el=>{const input=el.querySelector('input')!.getBoundingClientRect(),label=el.querySelector('span')!.getBoundingClientRect();return {width:input.width,gap:label.left-input.right,dy:Math.abs((input.top+input.bottom-label.top-label.bottom)/2)}})
      expect(alignment.width).toBe(16);expect(alignment.gap).toBe(8);expect(alignment.dy).toBeLessThanOrEqual(1)
    }
    await page.setViewportSize({width:1280,height:780})
    await page.getByLabel('启用交付要求',{exact:true}).check();await expect.poll(()=>app.store.orchestration().requireDelivery).toBe(true)
    await page.screenshot({path:info.outputPath('require-delivery-enabled.png')})
    await closeSettings(page)
    await page.getByTestId('composer-input').fill('执行报告验收');await page.getByTestId('composer-input').press('Enter')
    await expect.poll(()=>task.latestDelivery?.version).toBe(1)
    await until(()=>task.status==='idle'&&!app.manager.runs.has(task.id))
    expect(task.turnCount).toBe(2);expect(attempts).toBe(2);expect(reminders).toBe(1)
    await expect(page.getByTestId('orchestration-note')).toContainText('Tuck 自动要求补交付')
    await expect(page.locator('.role-user')).not.toContainText('Tuck 编排规则')
    const deliveryCard=page.getByTestId('delivery-activity').filter({hasText:'已提交交付'})
    await expect(deliveryCard).toContainText('V1 · 1 个文件')
    await expect(deliveryCard).toContainText('设置生效，已验证并交付报告。')
    await expect(page.getByTestId('delivery-activity').filter({hasText:'交付失败'})).toContainText('does not exist')
    await page.getByTestId('delivery-activity').filter({hasText:'交付失败'}).locator('summary').click()
    await expect(page.getByTestId('delivery-activity').filter({hasText:'交付失败'}).locator('pre')).toContainText('missing.txt')
    for(const width of [1280,390]) {
      await page.setViewportSize({width,height:780})
      await expect(deliveryCard).toBeVisible()
      expect(await deliveryCard.evaluate(el=>el.scrollWidth<=el.clientWidth)).toBe(true)
    }
    await deliveryCard.getByRole('link',{name:'查看交付 V1'}).click()
    await expect(page).toHaveURL(new RegExp(`/chat/conversations/${task.id}/delivery\\?version=1`))
    await page.getByRole('link',{name:'← 返回对话',exact:true}).click()
    await page.reload();await expect(page.getByTestId('delivery-activity').filter({hasText:'已提交交付'})).toContainText('设置生效，已验证并交付报告。')
    await page.setViewportSize({width:1280,height:780})
    await page.getByRole('button',{name:'交付摘要',exact:true}).click();await page.getByRole('dialog',{name:'交付摘要',exact:true}).getByRole('link',{name:'查看交付',exact:true}).click()
    await expect(page).toHaveURL(new RegExp(`/chat/conversations/${task.id}/delivery`))
    for(const width of [1280,390]){
      await page.setViewportSize({width,height:780})
      await expect(page.locator('.chat-delivery header h1')).toHaveText('交付')
      await expect(page.getByTestId('conversation-title')).toHaveText('对话：2')
      await expect(page.locator('.delivery-version')).toContainText('V1 · 本轮交付')
      expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
      await page.screenshot({path:info.outputPath(`numeric-conversation-title-${width}.png`)})
    }
    await page.reload();await expect(page.getByTestId('conversation-title')).toHaveText('对话：2')
    await expect(page.getByRole('article',{name:'交付摘要'})).toContainText('设置生效')
    const download=page.waitForEvent('download');await page.getByRole('link',{name:'下载 report.txt',exact:true}).click()
    expect(await readFile((await(await download).path())!,'utf8')).toBe('actual native delivery')
    await page.getByTestId('mode-tasks').click()
    await page.getByRole('link',{name:'查看交付',exact:true}).click()
    await expect(page).toHaveURL(new RegExp(`/tasks/${task.id}/delivery`))
    await expect(page.getByRole('navigation',{name:'任务路径'})).toBeVisible()
    await page.getByTestId('mode-chat').click();await expect(page).toHaveURL(new RegExp(`/chat/conversations/${task.id}/delivery`))
    await page.getByRole('link',{name:'← 返回对话',exact:true}).click()
    await openSettings(page);await page.locator('main.settings-mode a[href="#/settings/orchestration"]').filter({visible:true}).first().click()
    await page.getByLabel('启用交付要求',{exact:true}).uncheck();await expect.poll(()=>app.store.orchestration().requireDelivery).toBe(false)
    await closeSettings(page)
    await page.getByTestId('composer-input').fill('关闭后的普通执行');await page.getByTestId('send-message').click()
    await until(()=>task.turnCount===3&&task.status==='idle'&&!app.manager.runs.has(task.id))
    expect(task.latestDelivery?.version).toBe(1);expect(reminders).toBe(1)
    await page.getByTestId('session-menu').click();await page.getByRole('button',{name:'交付摘要',exact:true}).click();await expect(page.getByRole('dialog',{name:'交付摘要',exact:true})).toContainText('本轮尚未提交交付');await expect(page.getByRole('dialog',{name:'交付摘要',exact:true})).toContainText('已关闭')
    await expect(page.getByRole('dialog',{name:'交付摘要',exact:true})).toContainText('设置生效，已验证并交付报告')
    await page.getByRole('dialog',{name:'交付摘要',exact:true}).getByRole('link',{name:'查看交付',exact:true}).click()
    await expect(page.getByRole('status').filter({hasText:'本轮尚未提交交付'})).toBeVisible()
  }finally{await f.close()}
})

test('ordinary conversations expose immutable downloads, direct URLs, channel navigation and orchestration settings',async({page},info)=>{
  const f=await platformFixture({native:true})
  const app=f.app!
  const proxy=createServer((req,res)=>{const upstream=proxyRequest(f.url+(req.url||'/').replace(/^\/tuck\//,'/'),{method:req.method,headers:req.headers},response=>{res.writeHead(response.statusCode||502,response.headers);response.pipe(res)});upstream.on('error',()=>res.destroy());req.pipe(upstream);req.on('close',()=>{if(!req.complete)upstream.destroy()});res.on('close',()=>upstream.destroy())})
  await new Promise<void>(done=>proxy.listen(0,'127.0.0.1',done))
  const url=`http://127.0.0.1:${(proxy.address() as {port:number}).port}/tuck`
  try{
    const enrollment=await fetch(f.url+'/api/connection/enroll',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({material:f.material})});expect(enrollment.status).toBe(200)
    f.controls.chatReply=(body:any)=>{
      const recent=body.messages.slice(body.messages.findLastIndex((m:any)=>m.role==='user')+1)
      return recent.some((m:any)=>m.role==='tool')?{content:'已交付。'}:{tool_calls:[{index:0,id:'provider-call',type:'function',function:{name:'tuck_deliver_result',arguments:JSON.stringify({summary:`已完成 [成果](result.txt)，未列出的 [草稿](draft.txt) 不属于交付。[绝对地址成果](${url}/api/tasks/${task.id}/files?path=result.txt) [绝对地址草稿](${url}/api/tasks/${task.id}/files?path=draft.txt) [内部地址成果](/api/tasks/${task.id}/files?path=result.txt)`,files:[{path:'result.txt'},{path:'report.pdf'}]})}}]}
    }
    const task=await app.manager.create({title:'普通对话的交付',modelId:f.snapshot.models[0].modelId,approvalMode:'auto'})
    const stream='BT /F1 12 Tf 50 750 Td (Delivery PDF) Tj ET',objects=['<< /Type /Catalog /Pages 2 0 R >>','<< /Type /Pages /Kids [3 0 R] /Count 1 >>','<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>','<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>',`<< /Length ${stream.length} >>\nstream\n${stream}\nendstream`]
    let pdf='%PDF-1.4\n';const offsets=[0];for(const [i,object]of objects.entries()){offsets.push(pdf.length);pdf+=`${i+1} 0 obj\n${object}\nendobj\n`}const xref=pdf.length;pdf+='xref\n0 6\n0000000000 65535 f \n'+offsets.slice(1).map(n=>String(n).padStart(10,'0')+' 00000 n \n').join('')+`trailer\n<< /Size 6 /Root 1 0 R >>\nstartxref\n${xref}\n%%EOF\n`;await writeFile(resolve(task.workspace,'report.pdf'),pdf)
    for(const value of ['first version','second version']){await writeFile(resolve(task.workspace,'result.txt'),value);app.manager.submit(task.id,value);await until(()=>task.status==='idle'&&!app.manager.runs.has(task.id))}
    expect(task.latestDelivery?.version).toBe(2)
    const options=await app.manager.forkOptions(task.id),fork=await app.manager.fork(task.id,options.at(-1)!.index)
    await page.goto(`${url}/#/chat/conversations/${fork.task.id}`)
    const inherited=page.getByTestId('delivery-activity').filter({hasText:'已提交交付'}).first()
    await expect(inherited).toContainText('来自原对话')
    await expect(inherited.getByRole('link',{name:'查看交付 V1'})).toHaveAttribute('href',`#/chat/conversations/${task.id}/delivery?version=1`)
    await inherited.getByRole('link',{name:'查看交付 V1'}).click()
    await expect(page.locator('.delivery-version')).toContainText('V1')
    await expect(page.getByRole('article',{name:'任务回答',exact:true})).toHaveText('已交付。')
    await expect(page.getByRole('heading',{name:'最后一轮回答'})).toHaveCount(0)
    await page.goto(`${url}/#/chat/conversations/${task.id}`)
    const deliveryLink=page.getByRole('button',{name:'交付摘要',exact:true})
    for(const width of [1280,800,390]){
      await page.setViewportSize({width,height:780})
      if(width<=760)await page.getByTestId('session-menu').click()
      await expect(deliveryLink).toBeInViewport()
      expect(await page.locator('.conversation-header-actions').evaluate(el=>{const nodes=[...el.children].map(c=>c.getBoundingClientRect()).filter(r=>r.width>0);return nodes.every((r,i)=>!i||r.left>=nodes[i-1].right)})).toBe(true)
      expect(await deliveryLink.evaluate(el=>el.scrollWidth<=el.clientWidth)).toBe(true)
      await deliveryLink.click()
      const panel=page.getByRole('dialog',{name:'交付摘要',exact:true})
      await expect(panel).toBeInViewport()
      await expect(panel).toContainText('2 个文件')
      expect(await panel.evaluate(el=>{const b=el.getBoundingClientRect();return b.left>=0&&b.right<=innerWidth&&b.top>=0&&b.bottom<=innerHeight})).toBe(true)
      const panelBox=(await panel.boundingBox())!,headerBox=(await page.locator('.conversation-header').boundingBox())!
      if(width<=760){expect(panelBox.x).toBe(12);expect(panelBox.width).toBe(width-24)}
      else{
        const buttonBox=(await deliveryLink.boundingBox())!
        expect(panelBox.y-buttonBox.y-buttonBox.height).toBeCloseTo(8,0)
        expect(panelBox.x).toBeCloseTo(Math.max(headerBox.x+12,buttonBox.x+buttonBox.width-panelBox.width),0)
        expect(panelBox.x+panelBox.width).toBeLessThanOrEqual(headerBox.x+headerBox.width-11)
      }
      await page.screenshot({path:info.outputPath(`delivery-summary-${width}.png`)})
      await page.keyboard.press('Escape');await expect(panel).toBeHidden();await expect(width<=760?page.getByTestId('session-menu'):deliveryLink).toBeFocused()
    }
    await page.setViewportSize({width:1280,height:780})
    await page.screenshot({path:info.outputPath('chat-delivery-header.png')})
    await deliveryLink.click();await page.getByRole('dialog',{name:'交付摘要',exact:true}).getByRole('link',{name:'查看交付',exact:true}).click()
    await expect(page).toHaveURL(new RegExp(`/chat/conversations/${task.id}/delivery`))
    await expect(page.getByTestId('mode-chat')).toHaveAttribute('aria-current','page')
    await expect(page.getByRole('navigation',{name:'任务路径'})).toHaveCount(0)
    await expect(page.getByLabel('交付版本')).toHaveValue('2')
    await page.getByLabel('交付版本').selectOption('1')
    await expect(page).toHaveURL(/version=1/)
    await page.reload()
    await expect(page.getByLabel('交付版本')).toHaveValue('1')
    await expect(page.getByRole('article',{name:'交付摘要'}).getByRole('link',{name:'草稿'})).toHaveCount(0)
    await expect(page.getByRole('article',{name:'交付摘要'}).getByRole('link',{name:'绝对地址草稿'})).toHaveCount(0)
    await expect(page.getByRole('article',{name:'交付摘要'}).getByRole('link',{name:'绝对地址成果'})).toHaveAttribute('href',new RegExp(`/tuck/api/tasks/${task.id}/deliveries/1/files/`))
    await expect(page.getByRole('article',{name:'交付摘要'}).getByRole('link',{name:'内部地址成果'})).toHaveAttribute('href',new RegExp('/tuck/api/tasks/'+task.id+'/deliveries/1/files/'))
    await expect(page.getByRole('button',{name:'查看',exact:true})).toHaveCount(0)
    await expect(page.locator('.delivery-version')).toContainText('V1 · 历史版本')
    const pending=page.waitForEvent('download');await page.getByRole('link',{name:'下载 result.txt'}).click();expect(await readFile((await(await pending).path())!,'utf8')).toBe('first version')
    await page.screenshot({path:info.outputPath('history-wide.png')})
    await page.setViewportSize({width:390,height:780});expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);await page.screenshot({path:info.outputPath('history-mobile.png')})
    const pdfDownload=page.waitForEvent('download');await page.getByRole('link',{name:'下载 report.pdf'}).click();expect(await readFile((await(await pdfDownload).path())!,'utf8')).toBe(pdf)
    await page.getByTestId('mode-tasks').click();await expect(page).toHaveURL(/#\/tasks$/)
    await expect(page.getByRole('heading',{name:'让 Tuck 执行任务'})).toBeVisible()
    await page.getByTestId('mode-chat').click();await expect(page.getByLabel('交付版本')).toHaveValue('1')
    await page.getByRole('link',{name:'← 返回对话',exact:true}).click();await expect(page).toHaveURL(new RegExp(`/chat/conversations/${task.id}$`))
    await openSettings(page);await page.locator('main.settings-mode a[href="#/settings/orchestration"]').filter({visible:true}).first().click();await page.getByLabel('启用交付要求',{exact:true}).check();await expect.poll(()=>app.store.orchestration().requireDelivery).toBe(true)
    await closeSettings(page);await page.reload();await openSettings(page);await page.locator('main.settings-mode a[href="#/settings/orchestration"]').filter({visible:true}).first().click();await expect(page.getByLabel('启用交付要求',{exact:true})).toBeChecked()
    await page.getByLabel('启用交付要求',{exact:true}).uncheck();await expect.poll(()=>app.store.orchestration().requireDelivery).toBe(false)
    await closeSettings(page)
    await page.goto(`${url}/#/tasks/${task.id}/delivery`)
    await expect(page.getByRole('alert')).toHaveText('任务不存在或不属于任务频道')
    await expect(page.getByRole('article',{name:'交付摘要'})).toHaveCount(0)
    app.manager.agentDefaults.workbench.assistants.find(a=>a.agent===task.agent)!.deliveryTool=false
    await page.goto(`${url}/#/chat/conversations/${task.id}`)
    await page.reload()
    await page.getByTestId('session-menu').click()
    await page.getByRole('button',{name:'交付摘要',exact:true}).click();await expect(page.getByRole('dialog',{name:'交付摘要',exact:true})).toContainText('交付工具已停用，仍可查看历史交付')
    const empty=await app.manager.create({title:'未启用交付的对话',modelId:f.snapshot.models[0].modelId,approvalMode:'auto'})
    await page.goto(`${url}/#/chat/conversations/${empty.id}`)
    await expect(page.getByTestId('composer-input')).toBeVisible()
    await expect(page.getByRole('button',{name:'交付摘要',exact:true})).toHaveCount(0)
  }finally{proxy.closeAllConnections();await new Promise<void>(done=>proxy.close(()=>done()));await f.close()}
})


test('persisted Tuck provenance renders as orchestration while matching user text stays user-authored',async({page})=>{
 const f=await fixture({localAssistants:true});try{
  const task=await f.manager.create({title:'来源验收',agent:'root'})
  f.manager.append(task,{role:'user',blocks:[{id:'user',type:'markdown',data:{markdown:'Tuck 自动要求补交付：这是用户原文'}}]})
  f.manager.append(task,{role:'user',source:{kind:'delivery_required'},blocks:[{id:'reminder',type:'markdown',data:{markdown:'Actual reminder text',provenance:{origin:'delivery-required'}}}]})
  f.manager.append(task,{role:'tool',blocks:[{id:'result-without-link',type:'tool-result',data:{name:'tuck_deliver_result',callId:'result-without-link',status:'success',output:JSON.stringify({status:'completed',version:1,file_count:0})}}]})
  await page.goto(f.url+'/#/chat/conversations/'+task.id)
  for(let i=0;i<2;i++){
   await expect(page.getByTestId('orchestration-note')).toHaveCount(1)
   await expect(page.locator('.role-user')).toHaveCount(1)
   await expect(page.locator('.role-user .markdown-body')).toHaveText('Tuck 自动要求补交付：这是用户原文')
   await expect(page.getByRole('article',{name:'交付记录'})).toBeVisible()
   await expect(page.getByRole('article',{name:'交付记录'}).getByRole('link')).toHaveCount(0)
   if(i===0)await page.reload()
  }
 }finally{await f.close()}
})

test('delivery history stops pagination at the oldest retained version',async({page})=>{
 const f=await fixture({delivery:true,attachmentModels:true,localAssistants:true});try{
  const task=await f.manager.create({title:'保留版本分页'})
  for(let i=1;i<=12;i++)await f.deliver(task,`Version ${i}`)
  const list=await(await fetch(`${f.url}/api/tasks/${task.id}/deliveries?limit=5`)).json()
  expect(list.items.map((d:any)=>d.version)).toEqual([12,11,10,9,8]);expect(list.hasMore).toBe(true)
  const tail=await(await fetch(`${f.url}/api/tasks/${task.id}/deliveries?limit=5&beforeVersion=8`)).json()
  expect(tail.items.map((d:any)=>d.version)).toEqual([7,6,5,4,3]);expect(tail.hasMore).toBe(false)
  await page.goto(`${f.url}/#/chat/conversations/${task.id}/delivery`)
  await expect(page.getByLabel('交付版本')).toBeVisible();await expect(page.getByRole('button',{name:'加载更早版本',exact:true})).toHaveCount(0)
 }finally{await f.close()}
})
