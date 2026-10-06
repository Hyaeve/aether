import { test, expect } from '@playwright/test'

test('dashboard wraps long names and logs without horizontal scrolling', async ({ page }, info) => {
  await page.route('**/api/auth/status', route => route.fulfill({ json: { initialized: true, authenticated: true } }))
  const message = `播放请求 ${'https://media.example.com/'.repeat(35)} 中文日志`.repeat(2)
  await page.route('**/api/state', route => route.fulfill({ json: {
    username: 'dashboard-test', storages: [{ id: 'long', name: 'StorageWithoutSpaces'.repeat(20), status: 'connected' }],
    tasks: [], settings: {}, cache: { entries: 10000, bytes: 134217728, hits: 99, misses: 1 },
    traffic: { downloaded: 999999999999 }, uptime: 99999999,
    logs: [{ time: '2026-10-07T10:00:00+08:00', message }]
  } }))
  await page.goto('/dashboard')
  await expect(page.locator('.activity-row')).toContainText(message)
  for (const width of [1920, 1440, 1024, 768, 390, 320]) {
    await page.setViewportSize({ width, height: 900 })
    await expect(page.locator('.dashboard-page')).toBeVisible()
    expect(await page.locator('.dashboard-page').evaluate(el => el.scrollWidth <= el.clientWidth + 1)).toBe(true)
    expect(await page.locator('.page-content').evaluate(el => el.scrollWidth <= el.clientWidth + 1)).toBe(true)
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true)
    if (width === 1440 || width === 390) await page.screenshot({ path: info.outputPath(`dashboard-${width}.png`), fullPage: true })
  }
})
