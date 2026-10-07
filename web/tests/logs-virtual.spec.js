import { test, expect } from '@playwright/test'

test('20000 logs render only viewport rows in both modes', async ({ page }, info) => {
  await page.route('**/api/auth/status', route => route.fulfill({ json: { initialized: true, authenticated: true } }))
  await page.route('**/api/state', route => route.fulfill({ json: {
    username: 'virtual-logs', storages: [], tasks: [], settings: {}, cache: {}, traffic: {}, logs: []
  } }))
  const logs = Array.from({ length: 20000 }, (_, i) => ({
    time: '2026-10-07T10:00:00+08:00', level: i % 2 ? 'info' : 'warn', module: 'system',
    message: `record-${i}: ${'完整日志内容，包含路径和请求详情。'.repeat(i % 5 + 1)}`
  }))
  await page.route('**/api/logs', route => route.fulfill({ json: logs }))
  await page.goto('/logs')
  await expect(page.locator('.log-panel footer')).toHaveText('20000 条记录')
  const rows = page.locator('.log-entry')
  const viewport = page.locator('.log-viewport')
  await expect(rows.first()).toContainText('record-19999:')
  expect(await rows.count()).toBeLessThan(60)
  await viewport.evaluate(el => { el.scrollTop = el.scrollHeight })
  await expect(rows.last()).toContainText('record-0:')
  expect(await rows.count()).toBeLessThan(60)
  await page.getByRole('button', { name: '当前结构化列表，切换原始列表' }).click()
  await expect(rows.first()).toContainText('record-19999:')
  expect(await rows.count()).toBeLessThan(60)
  await page.setViewportSize({ width: 390, height: 844 })
  await expect.poll(() => rows.first().evaluate(el => el.getBoundingClientRect().height)).toBeGreaterThan(88)
  await viewport.evaluate(el => { el.scrollTop = el.scrollHeight })
  await expect(rows.last()).toContainText('record-0:')
  expect(await rows.count()).toBeLessThan(60)
  expect(await rows.evaluateAll(list => list.every((row, i) => !i || row.getBoundingClientRect().top >= list[i - 1].getBoundingClientRect().bottom - 1))).toBe(true)
  await expect.poll(() => rows.last().evaluate(el => Math.abs(el.getBoundingClientRect().bottom - el.closest('.log-viewport').getBoundingClientRect().bottom))).toBeLessThan(3)
  await page.screenshot({ path: info.outputPath('logs-20000-mobile.png') })
  await page.getByRole('textbox', { name: '搜索日志' }).fill('record-1234:')
  await expect(page.locator('.log-panel footer')).toHaveText('1 条记录')
  await expect(rows).toHaveCount(1)
  await expect(rows.first()).toContainText('record-1234:')
  await page.reload()
  await expect(page.locator('.log-panel footer')).toHaveText('1 条记录')
  await expect(rows.first()).toContainText('record-1234:')
})
