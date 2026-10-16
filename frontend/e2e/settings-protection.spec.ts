import {test,expect} from '@playwright/test'
import {settingsFixture} from '../../tests/fixtures/settings-fixture.mjs'
test('configuration protection gates categories, preserves partial saves, locks other tabs and never locks appearance',async({page,context},info)=>{
 const f=await settingsFixture(),password='synthetic-ui-protection-password'
 try{
  await page.goto(f.url+'/#/settings/protection')
  await page.getByLabel('保护密码',{exact:true}).fill(password);await page.getByLabel('确认新密码',{exact:true}).fill(password)
  for(const name of ['Agent','MCP','企业平台','编排'])await page.getByRole('checkbox',{name:new RegExp('^'+name)}).uncheck()
  await page.getByRole('checkbox',{name:/^提供商与模型/}).check()
  await page.getByRole('button',{name:'保存保护设置',exact:true}).click();await expect(page.getByRole('heading',{name:'此配置已锁定'})).toBeVisible()
  await page.goto(f.url+'/#/settings/providers/proof');await expect(page.getByRole('heading',{name:'此配置已锁定'})).toBeVisible();await expect(page.getByLabel('API 基础地址',{exact:true})).toHaveCount(0)
  await page.getByLabel('配置保护密码',{exact:true}).fill('wrong');await page.getByRole('button',{name:'解锁配置',exact:true}).last().click();await expect(page.getByRole('alert')).toContainText('不正确')
  await page.goto(f.url+'/#/settings/agents/root');await expect(page.getByLabel('指令',{exact:true})).toBeVisible();await page.getByLabel('说明',{exact:true}).fill('protected providers remain unchanged')
  const saved=page.waitForRequest(r=>r.url().endsWith('/api/settings/runtime')&&r.method()==='PUT');await page.getByRole('button',{name:'保存并生效',exact:true}).click();expect((await saved).postDataJSON().config).not.toHaveProperty('providers');await expect(page.getByRole('status')).toHaveText('已保存并生效')
  await page.goto(f.url+'/#/settings/providers/proof');await page.getByLabel('配置保护密码',{exact:true}).fill(password);await page.getByRole('button',{name:'解锁配置',exact:true}).last().click();await expect(page.getByLabel('API 基础地址',{exact:true})).toHaveValue(f.origin+'/v1')
  await page.getByLabel('名称',{exact:true}).fill('UNSAVED_PROTECTED_DRAFT')
  const other=await context.newPage();await other.goto(f.url+'/#/settings/appearance');await other.getByRole('button',{name:'锁定配置',exact:true}).click();await expect(page.getByLabel('名称',{exact:true})).toHaveCount(0);await expect(page.getByRole('heading',{name:'此配置已锁定'})).toBeVisible()
  await expect(other.getByRole('heading',{name:'此配置已锁定'})).toHaveCount(0);await other.close()
  await page.setViewportSize({width:390,height:780});await page.screenshot({path:info.outputPath('settings-locked-narrow.png')});expect(await page.locator('.settings-page').evaluate(e=>e.scrollWidth<=e.clientWidth)).toBe(true)
  await page.getByLabel('配置保护密码',{exact:true}).fill(password);await page.getByRole('button',{name:'解锁配置',exact:true}).last().click();await expect(page.getByLabel('名称',{exact:true})).toHaveValue('验证提供商')
  await page.goto(f.url+'/#/settings/protection');await page.getByRole('button',{name:'移除配置保护',exact:true}).click();await expect(page.getByLabel('保护密码',{exact:true})).toBeVisible();await expect(page.getByText('尚未启用配置保护。',{exact:false})).toBeVisible()
 }finally{await f.close()}
})
