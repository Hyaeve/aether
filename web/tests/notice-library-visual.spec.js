import { test, expect } from '@playwright/test'

test('notifications identify library actions and provider logos fill their bounds', async ({ page }, info) => {
  await page.route('**/api/plugins/**', r => r.fulfill({ json: {} }))
  await page.route('**/api/quark-takeover', r => r.fulfill({ json: { enabled: false, bindings: [] } }))
  await page.route('**/api/auth/status', r => r.fulfill({ json: { initialized: true, authenticated: true } }))
  await page.route('**/api/state', r => r.fulfill({ json: { storages: [{ id: 'pan', type: '115', name: '115', enabled: true, config: {} }], tasks: [{ id: 'cache', kind: 'cache', storageId: 'pan', name: '目录扫描', status: 'success', lastRun: new Date().toISOString() }], settings: {}, cache: {}, libraryNotices: [{ id: 'event', libraryName: '家庭电影库', name: '降临', mediaType: 'movie', event: 'playback.start', time: new Date().toISOString() }] } }))
  await page.goto('/tools')
  await page.getByRole('button', { name: '任务通知', exact: true }).click()
  await expect(page.locator('.notification-list strong').filter({ hasText: '家庭电影库' })).toHaveText('家庭电影库')
  await expect(page.locator('.notification-list')).toContainText('开始播放 · 降临')
  await expect(page.locator('.notification-list')).not.toContainText('电影 · 降临')
  await expect(page.locator('.notice-provider .provider-logo')).toHaveCSS('width', '36px')
  await expect(page.locator('.notification-list .emby-notice-icon > img')).toHaveCSS('width', '36px')
  await expect(page.locator('.simulcast-symbol .provider-logo')).toHaveCSS('width', '44px')
  await page.screenshot({ path: info.outputPath('library-notice.png') })
})

test('scheduled task notification uses library title and concise task completion', async ({ page }) => {
  await page.route('**/api/auth/status', r => r.fulfill({ json: { initialized: true, authenticated: true } }))
  await page.route('**/api/state', r => r.fulfill({ json: { storages: [], tasks: [], settings: {}, cache: {}, libraryNotices: [{ id: 'scheduled', serverName: '月见草', libraryName: '电影库', name: '媒体库扫描', event: 'scheduledtasks.completed', time: new Date().toISOString() }] } }))
  await page.goto('/storage')
  await page.getByRole('button', { name: '任务通知', exact: true }).click()
  await expect(page.locator('.notification-list strong')).toHaveText('电影库')
  await expect(page.locator('.notification-list small')).toContainText('媒体库扫描 已完成')
  await expect(page.locator('.notification-list')).not.toContainText('scheduledtasks.completed')
})
