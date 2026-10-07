import { test, expect } from '@playwright/test'

test('notifications fit Aether light and dark surfaces without overflow', async ({ page }, testInfo) => {
  await page.route('**/api/auth/status', r => r.fulfill({ json: { initialized: true, authenticated: true } }))
  await page.route('**/api/state', r => r.fulfill({ json: { username: 'notice-test', storages: [{ id: 'cloud', name: '天翼', type: 'tianyi', enabled: true, config: {} }], tasks: [], settings: {}, logs: [], cache: {}, traffic: {} } }))
  let failed = false
  await page.route('**/api/storages/cloud/test', r => r.fulfill({
    status: failed ? 400 : 200,
    json: failed ? { error: '天翼要求设备验证，请在官方客户端完成验证后重试。' } : { ok: true }
  }))
  await page.goto('/storage')
  for (const theme of ['light', 'dark']) {
    await page.evaluate(theme => document.documentElement.dataset.theme = theme, theme)
    for (const error of [false, true]) {
      failed = error
      await page.getByRole('button', { name: '存储操作 天翼', exact: true }).click()
      await page.getByRole('button', { name: '测试连接', exact: true }).click()
      const toast = page.locator('.toast')
      await expect(toast).toBeVisible()
      await expect(toast).toHaveCSS('opacity', '1')
      await expect(toast).toHaveAttribute('role', error ? 'alert' : 'status')
      const geometry = await toast.evaluate(el => {
        const box = el.getBoundingClientRect()
        return { width: el.scrollWidth <= el.clientWidth, left: box.left, right: box.right, viewport: innerWidth }
      })
      expect(geometry.width).toBe(true)
      expect(geometry.left).toBeGreaterThanOrEqual(0)
      expect(geometry.right).toBeLessThanOrEqual(geometry.viewport)
      await page.screenshot({ path: testInfo.outputPath(`toast-${theme}-${error ? 'error' : 'success'}.png`) })
      await page.getByRole('button', { name: '关闭通知', exact: true }).click()
      await expect(toast).toHaveCount(0)
    }
  }
  await page.setViewportSize({ width: 375, height: 812 })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.getByRole('button', { name: '存储操作 天翼', exact: true }).click()
  await page.getByRole('button', { name: '测试连接', exact: true }).click()
  await expect(page.locator('.toast')).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await page.screenshot({ path: testInfo.outputPath('toast-mobile.png') })
})
