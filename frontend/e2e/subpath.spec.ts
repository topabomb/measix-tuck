import { openSettings, closeSettings } from './settings'
import {test, expect} from '@playwright/test'
import {scryptSync} from 'node:crypto'
import {fixture} from '../../tests/fixtures/fixture.mjs'
import {createServer, request} from 'node:http'
const password='prefix-test-only-1234', salt='0123456789abcdef0123456789abcdef'
const passwordHash=`scrypt$32768$8$3$${salt}$${scryptSync(password,salt,32,{N:32768,r:8,p:3,maxmem:64*1024*1024}).toString('hex')}`
const png=Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAIAAAD91JpzAAAACXBIWXMAAAPoAAAD6AG1e1JrAAAAEklEQVQImWMQWhImtCSMAUIBABxOBDE8jR2EAAAAAElFTkSuQmCC','base64')

test('one build supports prefix login, SSE, upload, old history URLs, downloads and themes', async({page,context})=>{
  const app=await fixture({attachmentModels:true,delivery:true,security:{auth:{enabled:true,username:'tester',passwordHash}}})
  // External routing fixture: the product has no knowledge of this prefix.
  const prefix='/any/entry'
  const proxy=createServer((req,res)=>{
    if (!req.url?.startsWith(prefix+'/')) {res.writeHead(404);res.end();return}
    const upstream=request(app.url+(req.url!.slice(prefix.length)||'/'),{method:req.method,headers:req.headers},response=>{
      res.writeHead(response.statusCode!,response.headers); response.pipe(res)
    })
    upstream.on('error',()=>res.destroy()); res.on('close',()=>upstream.destroy()); req.pipe(upstream)
  })
  await new Promise<void>(done=>proxy.listen(0,'127.0.0.1',done))
  const origin=`http://127.0.0.1:${(proxy.address() as {port:number}).port}`, url=origin+prefix+'/'
  try {
    const task=await app.manager.create({title:'墨蓝主题与附件验收'})
    const leaked:string[]=[]
    page.on('request',r=>{const u=new URL(r.url()); if(u.origin===origin && /^\/(api|assets|themes)\//.test(u.pathname)) leaked.push(u.pathname)})
    await page.goto(url+`#/chat/conversations/${task.id}`)
    await expect(page).toHaveURL(url+`#/chat/conversations/${task.id}`)
    await expect(page.locator('html')).toHaveAttribute('data-tuck-theme','tuck')
    await expect(page).toHaveTitle('登录 · Tuck')
    await page.getByLabel('用户名',{exact:true}).fill('tester')
    await page.getByLabel('密码',{exact:true}).fill(password)
    await page.getByRole('button',{name:'登录',exact:true}).click()
    await expect(page.getByTestId('composer-input')).toBeVisible()
    await expect(page).toHaveTitle('墨蓝主题与附件验收 · 对话 · Tuck')
    expect((await context.cookies()).find(c=>c.name==='tuck_session')?.path).toBe('/')
    await page.getByTestId('attachment-input').setInputFiles({name:'品牌图.png',mimeType:'image/png',buffer:png})
    await expect(page.locator('.draft-attachment[data-status=ready]')).toHaveCount(1)
    await page.getByTestId('send-message').click()
    const img=page.getByTestId('attachment-image').locator('img')
    await expect(img).toHaveAttribute('src',new RegExp(`${prefix}/api/tasks/${task.id}/files`))
    await expect.poll(()=>img.evaluate((el:HTMLImageElement)=>el.naturalWidth)).toBe(2)
    await page.reload()
    await expect.poll(()=>img.evaluate((el:HTMLImageElement)=>el.naturalWidth)).toBe(2)
    await page.getByRole('button',{name:'产物',exact:true}).click()
    await expect(page.getByRole('link',{name:/下载/}).first()).toHaveAttribute('href',new RegExp(`^${prefix}/api/`))
    await page.keyboard.press('Escape')
    await openSettings(page)
    await page.getByRole('navigation',{name:'设置分类'}).getByRole('link',{name:'外观',exact:true}).click()
    await page.getByRole('button',{name:'石墨',exact:true}).click()
    await page.reload()
    await expect(page.locator('html')).toHaveAttribute('data-tuck-theme','graphite')
    await openSettings(page)
    await page.getByRole('navigation',{name:'设置分类'}).getByRole('link',{name:'外观',exact:true}).click()
    await page.getByRole('button',{name:'墨蓝',exact:true}).click()
    await closeSettings(page)
    for(const width of [390,830,1365]) {
      await page.setViewportSize({width,height:800})
      await expect(page.getByTestId('composer-input')).toBeInViewport()
      expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
    }
    // Explicit delivery snapshots use deployment-relative APIs, including summary links.
    task.title = 'T-Path · 子路径交付'
    const file = task.artifacts[0]!
    app.manager.append(task,{role:'assistant',blocks:[{id:'delivery-link',type:'markdown',data:{markdown:`[查看图片](/api/tasks/${task.id}/files?path=${encodeURIComponent(file.path)})`}}]})
    const deliveredPath=file.path.replaceAll('\\','/')
    await app.deliver(task,`[下载图片](${encodeURI(deliveredPath)})`,[deliveredPath])
    app.manager.settle(task,'completed')
    await page.goto(url+`#/tasks/${task.id}/delivery`)
    await expect(page.getByRole('heading',{name:'子路径交付'})).toBeVisible()
    await expect(page).toHaveTitle('查看交付 · 子路径交付 · Tuck')
    await expect(page.getByRole('article',{name:'交付摘要'}).getByRole('link',{name:'下载图片'})).toHaveAttribute('href',new RegExp(`^${prefix}/api/`))
    const downloadLink=page.getByRole('list',{name:'交付文件'}).getByRole('link',{name:/下载/})
    await expect(downloadLink).toHaveAttribute('href',new RegExp(`^${prefix}/api/`))
    const deliveryDownload = page.waitForEvent('download')
    await downloadLink.click()
    expect((await deliveryDownload).suggestedFilename()).toContain('品牌图.png')
    app.manager.append(task,{role:'tool',blocks:[{id:'inline-media',type:'attachments',data:{items:[{id:'inline-image',name:'内联产物.png',kind:'image',mimeType:'image/png',src:'data:image/png;base64,'+png.toString('base64')}]}}]})
    await page.goto(url+`#/chat/conversations/${task.id}`)
    const generated=page.getByRole('button',{name:'预览 内联产物.png',exact:true}).locator('img')
    await expect(generated).toHaveAttribute('src',new RegExp(`^${prefix}/api/tasks/${task.id}/message-media`))
    await expect.poll(()=>generated.evaluate((el:HTMLImageElement)=>el.naturalWidth)).toBe(2)
    await page.reload()
    await expect.poll(()=>generated.evaluate((el:HTMLImageElement)=>el.naturalWidth)).toBe(2)
    expect(leaked).toEqual([])
    await page.setViewportSize({width:1365,height:800})
    await openSettings(page)
    await expect(page).toHaveURL(url+'#/settings')
    await page.getByRole('button',{name:'退出登录',exact:true}).click()
    await expect(page).toHaveTitle('登录 · Tuck')
    expect((await context.cookies()).some(c=>c.name==='tuck_session')).toBe(false)
  } finally {proxy.closeAllConnections(); await new Promise<void>(done=>proxy.close(()=>done())); await app.close()}
})
