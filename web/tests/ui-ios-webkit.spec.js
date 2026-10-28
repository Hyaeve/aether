import { test, expect } from '@playwright/test'
import { captureScreenshot } from './helpers/screenshot.js'

test.use({ browserName: 'webkit', isMobile: true, hasTouch: true, deviceScaleFactor: 3 })

for (const size of [{ width: 375, height: 812 }, { width: 844, height: 390 }]) for (const theme of ['light', 'dark']) {
  test(`iOS WebKit search and library editor ${size.width} ${theme}`, async ({ page }, info) => {
    await page.route('**/api/**', route => {
      const path = new URL(route.request().url()).pathname
      let json = {}
      if (path === '/api/auth/status') json = { initialized: true, authenticated: true }
      if (path === '/api/state') json = { storages: [], tasks: [], settings: {}, traffic: {}, cache: {} }
      if (path === '/api/virtual-library/quark') json = { cards: [], settings: {}, stats: {} }
      if (path === '/api/quark-takeover') json = { bindings: [] }
      if (path === '/api/115-recycle-cleanup') json = { rules: [] }
      return route.fulfill({ json })
    })
    await page.setViewportSize(size)
    await page.goto('/tools')
    await page.evaluate(theme => document.documentElement.dataset.theme = theme, theme)
    await page.getByRole('button', { name: '搜索辅助工具', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: '搜索辅助工具', exact: true })
    await expect(dialog.locator('.plugin-search-result')).toHaveCount(15)
    expect(await dialog.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
    await captureScreenshot(page, { path: info.outputPath(`search-webkit-${size.width}-${theme}.png`) })
    await page.evaluate(() => document.documentElement.style.setProperty('--aether-visible-height', '320px'))
    const keyboardBox = await dialog.boundingBox()
    expect(keyboardBox.y + keyboardBox.height).toBeLessThanOrEqual(310)
    await expect(dialog.getByRole('button', { name: '关闭', exact: true })).toBeVisible()
    await page.evaluate(() => document.documentElement.style.setProperty('--aether-visible-height', `${window.visualViewport.height}px`))
    await dialog.getByRole('button', { name: '关闭', exact: true }).click()
    await page.goto('/virtual/quark')
    await page.evaluate(theme => document.documentElement.dataset.theme = theme, theme)
    await page.getByRole('button', { name: '添加链接', exact: true }).click()
    const editor = page.getByRole('dialog', { name: '添加夸克链接', exact: true })
    await expect(editor).toHaveCSS('scrollbar-width', 'none')
    expect(await editor.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
    await captureScreenshot(page, { path: info.outputPath(`editor-webkit-${size.width}-${theme}.png`) })
  })
}
