import {expect,test,type Page} from '@playwright/test'
import {fixture} from '../../tests/fixtures/fixture.mjs'

type FixtureApp=Awaited<ReturnType<typeof fixture>>
function seed(app:FixtureApp,task:Awaited<ReturnType<FixtureApp['manager']['create']>>,count=5000){
  const svg=`<svg xmlns="http://www.w3.org/2000/svg" width="240" height="140"><rect width="240" height="140" fill="#557788"/><desc>${'image fixture '.repeat(20000)}</desc></svg>`
  const src='data:image/svg+xml;base64,'+Buffer.from(svg).toString('base64')
  app.store.transaction(()=>{
    for(let index=0;index<count;index++)app.store.saveMessage(task.id,{id:`${task.id}:m-${index}`,index,turnId:'history',role:'assistant',blocks:index%64===0
      ?[{id:`media-${index}`,type:'attachments',data:{items:[{id:`image-${index}`,name:`图片 ${index}`,kind:'image',mimeType:'image/svg+xml',width:240,height:140,src}]}}]
      :index%10===0?[{id:`answer-${index}`,type:'markdown',data:{markdown:`正文 ${index}。\n\n`+'用于核对宽窄屏重排与锚点。'.repeat(8)}}]
      :[{id:`tool-${index}`,type:'tool-call',data:{name:'read_file',callId:`history-${index}`,input:{path:`example-${index}.txt`,details:'完整详情\n'.repeat(50)},status:'success'}}]})
    task.logicalCount=count;app.store.saveTask(task)
  })
}

async function geometry(page:Page){
  return page.getByTestId('scrollport').evaluate(stage=>{
    const boxes=Array.from(stage.querySelectorAll('[data-virtual-item="true"]')).map(el=>el.getBoundingClientRect()).sort((a,b)=>a.top-b.top)
    let overlap=0
    for(let index=1;index<boxes.length;index++)overlap=Math.max(overlap,boxes[index-1]!.bottom-boxes[index]!.top)
    return {overlap,count:boxes.length,overflow:document.documentElement.scrollWidth-innerWidth}
  })
}

for(const width of [320,390,1365])test(`history requests and lazy media remain bounded with real anchors at ${width}px`,async({page},info)=>{
  const app=await fixture()
  try{
    const task=await app.manager.create({title:'历史按需加载验证'});seed(app,task)
    const reads:{start:number;count:number;bytes:number;body:string}[]=[]
    const completed:Promise<void>[]=[]
    page.on('response',response=>{
      if(!response.url().includes(`/api/tasks/${task.id}/messages?`))return
      completed.push((async()=>{const url=new URL(response.url()),body=await response.text();reads.push({start:Number(url.searchParams.get('start')),count:Number(url.searchParams.get('count')),bytes:Buffer.byteLength(body),body})})())
    })
    await page.setViewportSize({width,height:844})
    await page.goto(`${app.url}/?inspect=1#/chat/conversations/${task.id}/messages/2500`)
    await expect(page.getByTestId('reader-position')).toHaveText('#2,500')
    await expect(page.locator('[data-message-index="2500"]').first()).toBeInViewport()
    await expect(page.getByTestId('conversation-loading')).toHaveCount(0)
    await page.getByRole('button',{name:'Close diagnostics',exact:true}).click()
    await Promise.all(completed)
    expect(reads.map(read=>read.count)).toEqual([64,64])
    expect(reads.reduce((sum,read)=>sum+read.count,0)).toBe(128)
    expect(reads.reduce((sum,read)=>sum+read.bytes,0)).toBeLessThan(200000)
    expect(reads.every(read=>!read.body.includes('base64'))).toBe(true)
    await info.attach('history-load-metrics',{contentType:'application/json',body:JSON.stringify({width,historyMessages:5000,requests:reads.map(({start,count,bytes})=>({start,count,bytes})),totalBytes:reads.reduce((sum,read)=>sum+read.bytes,0)})})
    expect(await geometry(page)).toMatchObject({overflow:0})
    expect((await geometry(page)).overlap).toBeLessThan(1)
    const tool=page.locator('[data-message-index="2501"] .activity-summary')
    await tool.press('Enter');await expect(tool).toHaveAttribute('aria-expanded','true')
    await expect(page.locator('[data-message-index="2501"] pre')).toBeVisible()
    await page.getByRole('button',{name:'预览 图片 2496',exact:true}).press('Enter')
    const preview=page.getByRole('dialog',{name:'图片预览 图片 2496'})
    await expect(preview).toBeVisible()
    await expect.poll(()=>preview.locator('.preview-body').evaluate(el=>(el as HTMLImageElement).naturalWidth)).toBe(240)
    await page.getByRole('button',{name:'关闭图片预览',exact:true}).click()
    await page.screenshot({path:info.outputPath(`history-${width}.png`)})
    const before=reads.length
    await page.locator('.conversation-vlist').hover({position:{x:5,y:80}})
    await page.mouse.wheel(0,-30000)
    await expect(page.getByTestId('segment-range')).toHaveText('2,372 – 2,499')
    await Promise.all(completed)
    expect(reads.slice(before).map(read=>({start:read.start,count:read.count}))).toEqual([{start:2372,count:64}])
    expect((await geometry(page)).overlap).toBeLessThan(1)
    expect((await geometry(page)).count).toBeLessThan(180)
    await page.getByTestId('jump-latest').click()
    await expect(page.getByTestId('segment-range')).toHaveText('4,872 – 4,999')
    await expect.poll(()=>page.locator('.conversation-vlist').evaluate(el=>el.scrollHeight-el.clientHeight-el.scrollTop)).toBeLessThan(2)
  }finally{await app.close()}
})

test('a failed history edge read preserves the current window and retries only that page',async({page})=>{
  const app=await fixture()
  try{
    const task=await app.manager.create({title:'历史失败可重试'});seed(app,task)
    let attempts=0
    await page.route(`**/api/tasks/${task.id}/messages?start=4808&count=64&*`,async route=>{
      if(attempts++===0)await route.fulfill({status:503,json:{error:'temporary history failure'}})
      else await route.continue()
    })
    await page.goto(`${app.url}/?inspect=1#/chat/conversations/${task.id}`)
    await expect(page.getByTestId('conversation-loading')).toHaveCount(0)
    await expect(page.getByTestId('segment-range')).toHaveText('4,872 – 4,999')
    await page.getByRole('button',{name:'Close diagnostics',exact:true}).click()
    await page.locator('.conversation-vlist').hover({position:{x:5,y:80}});await page.mouse.wheel(0,-30000)
    await expect(page.getByRole('button',{name:'历史加载失败，重试'})).toBeVisible()
    await expect(page.getByTestId('segment-range')).toHaveText('4,872 – 4,999')
    await page.getByRole('button',{name:'历史加载失败，重试'}).click()
    await expect(page.getByTestId('segment-range')).toHaveText('4,808 – 4,935')
    expect(attempts).toBe(2)
    expect((await geometry(page)).overlap).toBeLessThan(1)
  }finally{await app.close()}
})

test('Latest waits for its cold range while real SSE appends continue, then follows the actual new tail',async({page})=>{
  const app=await fixture();let release=()=>{}
  try{
    const task=await app.manager.create({title:'延迟历史与实时尾部'});seed(app,task,1000)
    const errors:string[]=[];page.on('pageerror',error=>errors.push(error.message))
    await page.goto(`${app.url}/?inspect=1#/chat/conversations/${task.id}/messages/100`)
    await expect(page.getByTestId('conversation-loading')).toHaveCount(0)
    await page.getByRole('button',{name:'Close diagnostics',exact:true}).click()
    const held=new Promise<void>(resolve=>release=resolve)
    let waiting=false
    await page.route(`**/api/tasks/${task.id}/messages?start=872&count=64&*`,async route=>{const response=await route.fetch();waiting=true;await held;await route.fulfill({response})})
    await page.getByTestId('jump-latest').click()
    await expect.poll(()=>waiting).toBe(true)
    for(let index=0;index<6;index++)app.manager.append(task,{role:'assistant',blocks:[{id:`live-${index}`,type:'markdown',data:{markdown:`异步读取期间的实际消息 ${index}`}}]})
    await expect(page.getByTestId('messages-after')).toHaveText('905')
    release()
    await expect(page.getByText('异步读取期间的实际消息 5',{exact:true})).toBeInViewport()
    await expect.poll(()=>page.locator('.conversation-vlist').evaluate(el=>el.scrollHeight-el.clientHeight-el.scrollTop)).toBeLessThan(2)
    expect(errors).toEqual([])
  }finally{release();await app.close()}
})

test('a failed Latest read retains the reader and supports explicit retry',async({page})=>{
  const app=await fixture()
  try{
    const task=await app.manager.create({title:'尾部读取失败'});seed(app,task,1000)
    let attempts=0
    await page.route(`**/api/tasks/${task.id}/messages?start=872&count=64&*`,async route=>{
      if(attempts++===0)await route.fulfill({status:503,json:{error:'tail unavailable'}})
      else await route.continue()
    })
    await page.goto(`${app.url}/?inspect=1#/chat/conversations/${task.id}/messages/100`)
    await expect(page.getByTestId('conversation-loading')).toHaveCount(0)
    await page.getByRole('button',{name:'Close diagnostics',exact:true}).click()
    await page.getByTestId('jump-latest').click()
    await expect(page.getByRole('button',{name:'历史加载失败，重试'})).toBeVisible()
    await expect(page.getByTestId('reader-position')).toHaveText('#100')
    await page.getByRole('button',{name:'历史加载失败，重试'}).click()
    await expect.poll(()=>page.locator('.conversation-vlist').evaluate(el=>el.scrollHeight-el.clientHeight-el.scrollTop)).toBeLessThan(2)
    await expect(page.getByTestId('reader-position')).toHaveText('#999')
    expect(attempts).toBe(2)
  }finally{await app.close()}
})
