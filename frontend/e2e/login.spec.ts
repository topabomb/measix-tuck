import { openSettings, closeSettings } from './settings'
import { test, expect } from '@playwright/test'
import { scryptSync } from 'node:crypto'
import { fixture } from '../../tests/fixtures/fixture.mjs'
const password='test-login-only-1234', salt='0123456789abcdef0123456789abcdef'
const passwordHash=`scrypt$32768$8$3$${salt}$${scryptSync(password,salt,32,{N:32768,r:8,p:3,maxmem:64*1024*1024}).toString('hex')}`
let app: Awaited<ReturnType<typeof fixture>>
test.beforeAll(async () => { app=await fixture({localAssistants:true,security:{auth:{enabled:true,username:'tester',passwordHash}}}) })
test.afterAll(async () => { await app.close() })
test('login preserves a task delivery deep link and its return origin after refresh',async({page})=>{
  const task=await app.manager.create({title:'T-auth · 受保护的交付'})
  app.manager.settle(task,'completed')
  const url=`${app.url}/#/tasks/${task.id}/delivery?from=supplement`
  await page.goto(url)
  await expect(page).toHaveTitle('登录 · Tuck')
  await page.getByLabel('用户名',{exact:true}).fill('tester')
  await page.getByLabel('密码',{exact:true}).fill(password)
  await page.getByRole('button',{name:'登录',exact:true}).click()
  await expect(page.getByRole('region',{name:'任务交付：受保护的交付',exact:true})).toBeVisible()
  await expect(page).toHaveURL(url)
  await expect(page).toHaveTitle('查看交付 · 受保护的交付 · Tuck')
  await page.reload()
  await expect(page.getByRole('region',{name:'任务交付：受保护的交付',exact:true})).toBeVisible()
  await page.getByRole('link',{name:'返回补充任务',exact:true}).click()
  await expect(page.getByRole('heading',{name:'补充任务',exact:true})).toBeVisible()
})
test('login, remember, logout, history labels and compact row geometry',async({page,context})=>{
  const root=await app.manager.create({title:'通用历史'}), video=await app.manager.create({title:'已停止的视频',agent:'video'})
  app.manager.settle(video,'interrupted')
  await page.goto(app.url)
  await expect(page.getByRole('button',{name:'登录',exact:true})).toBeVisible()
  await expect(page.getByTestId('session-sidebar')).toHaveCount(0)
  await expect(page).toHaveTitle('登录 · Tuck')
  await page.getByLabel('用户名',{exact:true}).fill('tester')
  await page.getByLabel('密码',{exact:true}).fill('wrong')
  await page.getByRole('button',{name:'登录',exact:true}).click()
  await expect(page.getByRole('alert')).toHaveText('用户名或密码错误')
  await page.getByLabel('密码',{exact:true}).fill(password)
  await page.getByLabel('在此设备保持登录 30 天').check()
  await page.getByRole('button',{name:'登录',exact:true}).click()
  const row=page.getByTestId(`session-${video.id}`)
  await expect(row.locator('.session-assistant')).toHaveText('视频创作')
  await expect(row.locator('.session-state')).toHaveText('已停止')
  const badge=await row.locator('.session-assistant').boundingBox(), state=await row.locator('.session-state').boundingBox()
  expect(Math.abs(badge!.y-state!.y)).toBeLessThan(2)
  expect((await row.boundingBox())!.height).toBeLessThanOrEqual(48)
  await expect(page.getByTestId(`session-${root.id}`).locator('.session-assistant')).toHaveText('通用助手')
  const cookie=(await context.cookies()).find(cookie=>cookie.name==='tuck_session')!
  expect(cookie.httpOnly).toBe(true);expect(cookie.sameSite).toBe('Strict');expect(cookie.expires).toBeGreaterThan(Date.now()/1000+29*86400)
  expect(await page.evaluate(()=>document.cookie.includes('tuck_session'))).toBe(false)
  await page.reload()
  await expect(row).toBeVisible()
  await page.getByTestId(`session-${root.id}`).click()
  await page.getByTestId('composer-input').fill('METRICS_STREAM')
  await page.getByTestId('send-message').click()
  await expect(page.getByTestId('composer-input')).toHaveValue('')
  await openSettings(page)
  await page.setViewportSize({width:390,height:780});await page.goto(app.url+'/#/settings');await expect(page.getByRole('button',{name:'退出登录',exact:true})).toBeInViewport()
  await page.getByRole('button',{name:'退出登录',exact:true}).click()
  await expect(page.getByRole('button',{name:'登录',exact:true})).toBeVisible()
  await expect(page).toHaveTitle('登录 · Tuck')
  await expect(page.getByTestId('session-sidebar')).toHaveCount(0)
  expect((await context.cookies()).some(cookie=>cookie.name==='tuck_session')).toBe(false)
  for(const width of [390,830,1365]){
    await page.setViewportSize({width,height:740})
    await expect(page.getByRole('button',{name:'登录',exact:true})).toBeInViewport()
    expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
  }
})
