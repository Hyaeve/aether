import { test, expect } from '@playwright/test'
import { captureScreenshot } from './helpers/screenshot.js'

for (const width of [1440, 390]) for (const theme of ['light', 'dark']) {
  test(`STRM settings keep address and format without a conflicting overwrite selector ${width} ${theme}`, async ({ page }, info) => {
    let saved = { address: 'http://localhost:15151', format: 'pickcode_name', overwriteMode:'overwrite' }, fail = false
    const writes = []
    await page.route('**/api/**', route => {
      const p = new URL(route.request().url()).pathname
      if (p === '/api/strm/settings') {
        if (route.request().method() === 'PUT') {
          writes.push(route.request().postDataJSON())
          if (fail) return route.fulfill({ status: 400, json: { error: '保存STRM配置失败' } })
          saved = route.request().postDataJSON()
        }
        return route.fulfill({ json: saved })
      }
      return route.fulfill({ json: p === '/api/auth/status' ? { initialized: true, authenticated: true }
        : p === '/api/state' ? { username: 'strm-settings', storages: [], tasks: [], settings: {}, cache: {}, traffic: {} } : {} })
    })
    await page.setViewportSize({ width, height: 900 })
    await page.goto('/settings/strm')
    await page.evaluate(theme => document.documentElement.dataset.theme = theme, theme)
    await expect(page.getByRole('button',{name:'STRM覆盖模式',exact:true})).toHaveCount(0)
    const selector = page.getByRole('button', { name: '115 STRM 格式', exact: true })
    await expect(selector).toHaveText('pickcode_name')
    await expect(selector).toHaveCSS('height', '40px')
    await selector.click()
    await page.getByRole('option', { name: 'pickcode', exact: true }).click()
    await expect.poll(() => saved.format).toBe('pickcode')
    expect(saved.overwriteMode).toBe('overwrite')
    await page.reload()
    await page.evaluate(theme => document.documentElement.dataset.theme = theme, theme)
    await expect(selector).toHaveText('pickcode')
    expect(await page.locator('.strm-settings').evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
    await captureScreenshot(page, { path: info.outputPath(`strm-settings-${width}-${theme}.png`) })
    fail = true
    await selector.click()
    await page.getByRole('option', { name: 'pickcode_name', exact: true }).click()
    await expect(page.locator('.toast').last()).toContainText('保存STRM配置失败')
    await expect(selector).toBeEnabled()
    await expect(page.locator('.strm-settings .error-message')).toHaveCount(0)
    expect(writes.at(-1).format).toBe('pickcode_name')
    expect(writes.at(-1).overwriteMode).toBe('overwrite')
  })
}
