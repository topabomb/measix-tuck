import { expect, type Page } from '@playwright/test'

const sources = new WeakMap<Page, string>()
export async function openSettings(page: Page) {
  const mode = new URL(page.url()).hash.match(/^#\/(chat|tasks|workspace|orchestration)/)?.[1]
  if (mode) sources.set(page, mode)
  if((page.viewportSize()?.width||1365)<=760){
    await page.getByRole('button',{name:'更多频道与设置',exact:true}).click()
    await page.getByRole('dialog',{name:'更多频道与设置',exact:true}).getByRole('link',{name:'设置',exact:true}).click()
  }else{
    await page.getByTestId('mode-settings').click()
    await page.getByRole('navigation',{name:'设置快捷菜单'}).getByRole('link',{name:'设置',exact:true}).click()
  }
  await expect(page.locator('main.settings-mode')).toBeVisible()
}

// Return through the rail to exercise the channel's remembered location.
export async function closeSettings(page: Page) {
  await page.getByTestId(`mode-${sources.get(page) || 'chat'}`).click()
  await expect(page.locator('main.settings-mode')).toHaveCount(0)
}
