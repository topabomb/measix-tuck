import { test, expect } from '@playwright/test'
import { fixture } from '../../tests/fixtures/fixture.mjs'
import { workspaceFixture } from '../../tests/fixtures/workspace-fixture.mjs'
import { scryptSync } from 'node:crypto'

// Resolve a non-localhost HTTP origin to the loopback-only test service. This
// preserves the browser's real insecure-context API restrictions without
// publishing the fixture on the LAN or overriding browser security.
test.use({launchOptions:{args:['--host-resolver-rules=MAP tuck-http.test 127.0.0.1']},viewport:{width:854,height:960}})

test('plain HTTP supports HTML, Markdown and SVG previews and their file navigation',async({page},testInfo)=>{
  const password='http-preview-test-only',salt='0123456789abcdef0123456789abcdef'
  const passwordHash=`scrypt$32768$8$3$${salt}$${scryptSync(password,salt,32,{N:32768,r:8,p:3,maxmem:64*1024*1024}).toString('hex')}`
  const core=await workspaceFixture()
  let app:Awaited<ReturnType<typeof fixture>>|undefined
  try {
    app=await fixture({enterprisePlatform:core.platform,security:{auth:{enabled:true,username:'tester',passwordHash}}})
    core.put('http.css','h1 {color:rgb(24, 36, 48)}')
    core.put('http.html','<!doctype html><html><head><link rel="stylesheet" href="http.css"></head><body><h1>HTTP 静态预览</h1><img src="sample.png" alt="本地图片"><a href="readme.md">打开 Markdown</a><script>parent.hacked=true</script></body></html>')
    const task=await app.manager.create({title:'局域网 HTTP 预览'})
    const origin=app.url.replace('127.0.0.1','tuck-http.test')
    await page.goto(origin+`/#/chat/conversations/${task.id}?ew=1&file=http.html`)
    expect(await page.evaluate(()=>({secure:isSecureContext,uuid:typeof crypto.randomUUID,random:typeof crypto.getRandomValues}))).toEqual({secure:false,uuid:'undefined',random:'function'})
    await page.getByLabel('用户名',{exact:true}).fill('tester')
    await page.getByLabel('密码',{exact:true}).fill(password)
    await page.getByRole('button',{name:'登录',exact:true}).click()
    const panel=page.getByRole('complementary',{name:'企业工作区'}),frame=panel.frameLocator('iframe[title="文件静态预览"]:visible')
    await expect(frame.locator('h1')).toHaveText('HTTP 静态预览')
    await expect(frame.locator('h1')).toHaveCSS('color','rgb(24, 36, 48)')
    await expect.poll(()=>frame.locator('img').evaluate(node=>(node as HTMLImageElement).naturalWidth)).toBe(2)
    expect(await page.evaluate(()=>Object.hasOwn(window,'hacked'))).toBe(false)
    await frame.getByRole('link',{name:'打开 Markdown',exact:true}).click()
    await expect(panel.getByRole('tab',{name:'readme.md',exact:true})).toHaveAttribute('aria-selected','true')
    await expect(frame.locator('h1')).toHaveText('Workspace')
    await expect.poll(()=>frame.locator('img').evaluate(node=>(node as HTMLImageElement).naturalWidth)).toBe(2)
    await frame.getByRole('link',{name:'hello',exact:true}).click()
    await expect(panel.locator('.ew-document:visible .cm-content')).toContainText('hello')
    await page.goto(origin+`/#/chat/conversations/${task.id}?ew=1&file=shape.svg`)
    await expect(frame.locator('svg rect')).toHaveAttribute('fill','teal')
    await expect(panel.getByRole('alert')).toHaveCount(0)
    expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
    await page.screenshot({path:testInfo.outputPath('plain-http-preview.png')})
  } finally {try{await app?.close()}finally{await core.close()}}
})
