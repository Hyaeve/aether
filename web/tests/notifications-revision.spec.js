import { test, expect } from '@playwright/test'

test('activity list shows three rows and clear survives browser refresh', async ({ page }) => {
  await page.route('**/api/auth/status', r => r.fulfill({ json: { initialized: true, authenticated: true } }))
  const records = Array.from({ length: 6 }, (_, i) => ({ id: `notice-${i}`, name: `影片 ${i}`, event: i === 0 ? 'playback.start' : 'library.new', time: new Date(Date.now() - i * 1000).toISOString() }))
  await page.route('**/api/state', r => r.fulfill({ json: { username: 'notices', storages: [], tasks: [], settings: {}, cache: {}, logs: [], libraryNotices: records } }))
  await page.goto('/storage')
  await page.getByRole('button', { name: '任务通知', exact: true }).click()
  await expect(page.locator('.notification-list > button')).toHaveCount(6)
  await expect(page.locator('.notification-list')).toContainText('开始播放')
  const sizes = await page.locator('.notification-list').evaluate(n => ({ height: n.clientHeight, row: n.firstElementChild.getBoundingClientRect().height, scroll: n.scrollHeight }))
  expect(sizes.height).toBe(sizes.row * 3)
  expect(sizes.scroll).toBeGreaterThan(sizes.height)
  await page.getByRole('button', { name: '清除通知', exact: true }).click()
  await expect(page.getByText('暂无通知', { exact: true })).toBeVisible()
  await page.reload()
  await page.getByRole('button', { name: '任务通知', exact: true }).click()
  await expect(page.getByText('暂无通知', { exact: true })).toBeVisible()
})
