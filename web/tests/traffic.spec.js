import { test, expect } from '@playwright/test'

test('topbar shows live rates, returns to idle and clears failed samples', async ({ page }, info) => {
  let rates = { uploadRate: 2048, downloadRate: 1048576 }, failed = false
  await page.route('**/api/auth/status', r => r.fulfill({ json: { initialized: true, authenticated: true } }))
  await page.route('**/api/state', r => r.fulfill({ json: { username: 'traffic', storages: [], tasks: [], settings: {}, cache: {}, traffic: { uploaded: 999999999, downloaded: 999999999 } } }))
  await page.route('**/api/traffic', r => r.fulfill({ status: failed ? 503 : 200, json: rates }))
  await page.goto('/storage')
  await expect(page.locator('.storage-grid')).toBeVisible()
  await expect(page.locator('.metric-strip')).toHaveCount(0)
  await expect(page.locator('.traffic-stat.upload')).toHaveText('2.0 KB/s')
  await expect(page.locator('.traffic-stat.download')).toHaveText('1.0 MB/s')
  const colors = await page.locator('.traffic-stat b').evaluateAll(nodes => nodes.map(n => getComputedStyle(n).color))
  expect(colors[0]).not.toBe(colors[1])
  await page.screenshot({ path: info.outputPath('traffic.png') })
  rates = { uploadRate: 0, downloadRate: 0 }
  await expect(page.locator('.traffic-stat.upload')).toHaveText('0 B/s')
  await expect(page.locator('.traffic-stat.download')).toHaveText('0 B/s')
  failed = true
  await expect(page.locator('.traffic-stat.upload')).toHaveText('--')
})
