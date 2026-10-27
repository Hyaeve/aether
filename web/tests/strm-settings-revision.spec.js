import { test, expect } from '@playwright/test'
import { captureScreenshot } from './helpers/screenshot.js'

for (const width of [1440, 390]) for (const theme of ['light', 'dark']) {
  test(`STRM overwrite selector persists beside its label ${width} ${theme}`, async ({ page }, info) => {
    let saved = { address: 'http://localhost:15151', format: 'pickcode_name' }, fail = false
    const writes = []
    await page.route('**/api/**', route => {
      const p = new URL(route.request().url()).pathname
      if (p === '/api/strm/settings') {
        if (route.request().method() === 'PUT') {
          writes.push(route.request().postDataJSON())
          if (fail) return route.fulfill({ status: 400, json: { error: '保存覆盖模式失败' } })
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
    const selector = page.getByRole('button', { name: 'STRM覆盖模式', exact: true })
    await expect(selector).toHaveText('直接跳过')
    await expect(selector).toHaveCSS('height', '40px')
    const layout = await selector.evaluate(el => {
      const a = el.closest('.field').querySelector('label').getBoundingClientRect(), b = el.getBoundingClientRect()
      return { gap: b.left-a.right, centers: Math.abs(a.top+a.height/2-b.top-b.height/2) }
    })
    expect(layout.gap).toBeGreaterThan(0)
    expect(layout.centers).toBeLessThan(1)
    await selector.click()
    await page.getByRole('option', { name: '覆盖生成', exact: true }).click()
    await expect.poll(() => saved.overwriteMode).toBe('overwrite')
    await page.reload()
    await page.evaluate(theme => document.documentElement.dataset.theme = theme, theme)
    await expect(selector).toHaveText('覆盖生成')
    expect(await page.locator('.strm-settings').evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
    await captureScreenshot(page, { path: info.outputPath(`overwrite-${width}-${theme}.png`) })
    fail = true
    await selector.click()
    await page.getByRole('option', { name: '直接跳过', exact: true }).click()
    await expect(page.locator('.toast').last()).toContainText('保存覆盖模式失败')
    await expect(selector).toBeEnabled()
    await expect(page.locator('.strm-settings .error-message')).toHaveCount(0)
    expect(writes.at(-1).overwriteMode).toBe('skip')
  })
}
