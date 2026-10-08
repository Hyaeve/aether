import { test, expect } from '@playwright/test'

test('topbar shows live rates, returns to idle and clears failed samples', async ({ page }, info) => {
  let rates = { uploadRate: 2048, downloadRate: 1048576 }, failed = false
  await page.route('**/api/auth/status', r => r.fulfill({ json: { initialized: true, authenticated: true } }))
  await page.route('**/api/state', r => r.fulfill({ json: { username: 'traffic', storages: [], tasks: [], settings: {}, cache: {}, traffic: { uploaded: 999999999, downloaded: 999999999 } } }))
  await page.route('**/api/traffic', r => r.fulfill({ status: failed ? 503 : 200, json: rates }))
  await page.goto('/storage')
  await expect(page.locator('.storage-grid')).toBeVisible()
  await expect(page.locator('.metric-strip')).toHaveCount(0)
  await expect(page.locator('.traffic-stat.upload')).toHaveText('上传 2.0 KB/s')
  await expect(page.locator('.traffic-stat.download')).toHaveText('下载 1.0 MB/s')
  await page.screenshot({ path: info.outputPath('traffic.png') })
  rates = { uploadRate: 0, downloadRate: 0 }
  await expect(page.locator('.traffic-stat.upload')).toHaveText('上传 0 B/s')
  await expect(page.locator('.traffic-stat.download')).toHaveText('下载 0 B/s')
  failed = true
  await expect(page.locator('.traffic-stat.upload')).toHaveText('上传 --')
})
