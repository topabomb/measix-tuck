import {test,expect} from '@playwright/test'
import {fixture} from '../../tests/fixtures/fixture.mjs'
import {workspaceFixture} from '../../tests/fixtures/workspace-fixture.mjs'

for(const entry of ['/','/chat'])test(`a completed default history read cannot reclaim navigation away from ${entry}`,async({page})=>{
  const app=await fixture()
  try {
    const task=await app.manager.create({title:'不应重新打开的对话'})
    app.manager.append(task,{role:'assistant',blocks:[{id:'history',type:'markdown',data:{markdown:'延迟返回的历史'}}]})
    // Hold the real response at its JSON boundary so native hash navigation and
    // history completion happen in one browser task, before hashchange commits.
    await page.addInitScript(()=>{
      const nativeFetch=window.fetch.bind(window)
      const gate=window as unknown as {historyWaiting:boolean;releaseHistory?:()=>void}
      window.fetch=async(...args)=>{
        const response=await nativeFetch(...args)
        const input=args[0],url=typeof input==='string'?input:input instanceof URL?input.href:input.url
        if(/\/api\/tasks\/[^/]+\/messages\?/.test(url)){
          const read=response.json.bind(response)
          response.json=async()=>{
            const data=await read()
            await new Promise<void>(resolve=>{gate.releaseHistory=resolve;gate.historyWaiting=true})
            return data
          }
        }
        return response
      }
    })
    await page.goto(app.url+'/#'+entry)
    await expect.poll(()=>page.evaluate(()=>(window as unknown as {historyWaiting?:boolean}).historyWaiting)).toBe(true)
    await page.evaluate(()=>{
      location.hash='/tasks'
      ;(window as unknown as {releaseHistory:()=>void}).releaseHistory()
    })
    await expect(page.getByRole('main',{name:'任务频道'})).toBeVisible()
    await expect(page).toHaveURL(app.url+'/#/tasks')
    await expect(page).toHaveTitle('任务 · Tuck')
    await expect(page.locator('.conversation-vlist')).toHaveCount(0)
  } finally {await app.close()}
})

test('workspace query navigation keeps the current conversation viewport and draft',async({page})=>{
  const core=await workspaceFixture(),app=await fixture({enterprisePlatform:core.platform})
  try {
    const task=await app.manager.create({title:'查询变化保留聊天'})
    app.manager.append(task,{role:'assistant',blocks:[{id:'history',type:'markdown',data:{markdown:'保持同一个聊天视口'}}]})
    await page.goto(app.url+`/#/chat/conversations/${task.id}`)
    const viewport=page.locator('.conversation-vlist')
    await expect(viewport).toContainText('保持同一个聊天视口')
    const original=await viewport.elementHandle()
    const draft=page.locator('textarea').first()
    await draft.fill('打开工作区时保留的草稿')
    await page.getByRole('button',{name:'工作区',exact:true}).click()
    const panel=page.getByRole('complementary',{name:'企业工作区'})
    await expect(panel.getByRole('option',{name:/hello\.txt/})).toBeVisible()
    await panel.getByRole('button',{name:'展开工作区',exact:true}).click()
    await panel.getByRole('button',{name:'还原工作区',exact:true}).click()
    await panel.getByRole('option',{name:/folder/}).dblclick()
    await expect(page).toHaveURL(/dir=folder/)
    await expect.poll(()=>viewport.evaluate((node,previous)=>node===previous,original)).toBe(true)
    await expect(draft).toHaveValue('打开工作区时保留的草稿')
    await panel.getByRole('button',{name:'收起企业工作区',exact:true}).click()
    await expect.poll(()=>viewport.evaluate((node,previous)=>node===previous,original)).toBe(true)
    await expect(viewport).toContainText('保持同一个聊天视口')
  } finally {await app.close();await core.close()}
})

test('an uncached deep link resolves the server task without waiting for SSE',async({page})=>{
  const app=await fixture()
  let release=()=>{}
  try {
    // Keep the real connection, but withhold task events to model a lagging
    // subscription independently from the authoritative single-task endpoint.
    await page.addInitScript(()=>{
      const NativeEventSource=window.EventSource
      window.EventSource=class extends NativeEventSource {
        constructor(url:string|URL,options?:EventSourceInit){super(url,options);this.addEventListener('message',event=>event.stopImmediatePropagation())}
      }
    })
    const initial=await app.manager.create({title:'已缓存对话'})
    await page.goto(app.url+`/#/chat/conversations/${initial.id}`)
    await expect(page.getByTestId('composer-input')).toBeVisible()
    const task=await app.manager.create({title:'事件尚未到达的新对话'})
    app.manager.append(task,{role:'assistant',blocks:[{id:'new-history',type:'markdown',data:{markdown:'由真实服务端读取的新历史'}}]})
    await expect(page.getByTestId(`session-${task.id}`)).toHaveCount(0)
    const requests:string[]=[]
    page.on('request',request=>{if(request.method()==='GET')requests.push(new URL(request.url()).pathname)})
    await page.goto(app.url+`/#/chat/conversations/${task.id}`)
    await expect(page.locator('.conversation-vlist')).toContainText('由真实服务端读取的新历史')
    await expect(page).toHaveTitle('事件尚未到达的新对话 · 对话 · Tuck')
    expect(requests.filter(path=>path===`/api/tasks/${task.id}`)).toHaveLength(1)
    expect(requests.filter(path=>path==='/api/tasks')).toHaveLength(0)

    await page.goto(app.url+'/#/chat/conversations/actually-missing-task')
    await expect(page.locator('.chat-entry')).toContainText('指定对话不存在')
    expect(requests.filter(path=>path==='/api/tasks/actually-missing-task')).toHaveLength(1)

    const slow=await app.manager.create({title:'离开后才返回的元数据'})
    let reached=()=>{}
    const started=new Promise<void>(resolve=>reached=resolve),held=new Promise<void>(resolve=>release=resolve)
    await page.route(`**/api/tasks/${slow.id}`,async route=>{const response=await route.fetch();reached();await held;await route.fulfill({response})})
    await page.goto(app.url+`/#/chat/conversations/${slow.id}`)
    await started
    await page.getByTestId('mode-tasks').click()
    await expect(page.getByRole('main',{name:'任务频道'})).toBeVisible()
    const returned=page.waitForResponse(response=>new URL(response.url()).pathname===`/api/tasks/${slow.id}`)
    release();await returned
    await expect(page).toHaveURL(app.url+'/#/tasks')
    await expect(page).toHaveTitle('任务 · Tuck')
    await expect(page.locator('.conversation-vlist')).toHaveCount(0)
  } finally {release();await app.close()}
})
