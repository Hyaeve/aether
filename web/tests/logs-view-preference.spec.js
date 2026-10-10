import { test, expect } from '@playwright/test'

const rawLabel = '当前原始列表，切换结构化列表'
const structuredLabel = '当前结构化列表，切换原始列表'
const key = user => `aether-log-view:${user}`
const logs = Array.from({ length: 240 }, (_, i) => ({
  time: '2026-10-10T10:00:00+08:00', level: i % 2 ? 'info' : 'error',
  module: i % 2 ? 'system' : 'tasks', message: `日志-${i}`
}))

async function mockWorkspace(page, user = () => 'log-preference') {
  await page.route('**/api/**', route => {
    const path = new URL(route.request().url()).pathname
    let json = {}
    if (path === '/api/auth/status') json = { initialized: true, authenticated: true }
    if (path === '/api/state') json = { username: user(), storages: [], tasks: [], settings: {}, cache: {}, traffic: {}, logs: [] }
    if (path === '/api/logs') json = logs
    return route.fulfill({ json })
  })
}

async function expectNoFilters(page) {
  await expect(page.getByRole('textbox', { name: '搜索日志' })).toHaveValue('')
  await expect(page.getByRole('button', { name: '日志级别', exact: true })).toHaveText('全部级别')
  await expect(page.getByRole('button', { name: '日志模块', exact: true })).toHaveText('全部模块')
}

test('only the log view survives reload and navigation for the browser account', async ({ page }) => {
  await mockWorkspace(page)
  await page.goto('/logs')
  await expect(page.locator('.log-panel footer')).toHaveText('240 条记录')
  await page.getByRole('button', { name: structuredLabel }).click()
  await page.getByRole('textbox', { name: '搜索日志' }).fill('日志-2')
  await page.getByRole('button', { name: '日志级别', exact: true }).click()
  await page.getByRole('option', { name: '错误', exact: true }).click()
  await page.getByRole('button', { name: '日志模块', exact: true }).click()
  await page.getByRole('option', { name: '任务管理', exact: true }).click()
  expect(await page.evaluate(k => localStorage.getItem(k), key('log-preference'))).toBe('raw')
  expect(await page.evaluate(() => localStorage.getItem('aether-log-filters:log-preference'))).toBeNull()
  await page.reload()
  await expect(page.getByRole('button', { name: rawLabel })).toBeVisible()
  await expectNoFilters(page)
  await page.getByRole('link', { name: '存储管理', exact: true }).click()
  await page.getByRole('link', { name: '系统日志', exact: true }).click()
  await expect(page.getByRole('button', { name: rawLabel })).toBeVisible()
  await expectNoFilters(page)
  await page.getByRole('button', { name: rawLabel }).click()
  await page.reload()
  await expect(page.getByRole('button', { name: structuredLabel })).toBeVisible()
})

test('accounts have independent view preferences and invalid values default safely', async ({ page }) => {
  let user = 'logs-alice'
  await mockWorkspace(page, () => user)
  await page.goto('/logs')
  await page.getByRole('button', { name: structuredLabel }).click()
  user = 'logs-bob'
  await page.reload()
  await expect(page.getByRole('button', { name: structuredLabel })).toBeVisible()
  expect(await page.evaluate(k => localStorage.getItem(k), key('logs-alice'))).toBe('raw')
  expect(await page.evaluate(k => localStorage.getItem(k), key('logs-bob'))).toBeNull()
  await page.evaluate(k => localStorage.setItem(k, 'invalid-mode'), key('logs-bob'))
  await page.reload()
  await expect(page.getByRole('button', { name: structuredLabel })).toBeVisible()
  user = 'logs-alice'
  await page.reload()
  await expect(page.getByRole('button', { name: rawLabel })).toBeVisible()
})

test('a blocked preference store does not prevent switching log views', async ({ page }) => {
  await page.addInitScript(() => {
    const get = Storage.prototype.getItem, set = Storage.prototype.setItem
    Storage.prototype.getItem = function (key) {
      if (key.startsWith('aether-log-view:')) throw new DOMException('Blocked', 'SecurityError')
      return get.call(this, key)
    }
    Storage.prototype.setItem = function (key, value) {
      if (key.startsWith('aether-log-view:')) throw new DOMException('Blocked', 'SecurityError')
      return set.call(this, key, value)
    }
  })
  await mockWorkspace(page)
  await page.goto('/logs')
  await page.getByRole('button', { name: structuredLabel }).click()
  await expect(page.locator('.log-entry.raw').first()).toBeVisible()
  await page.reload()
  await expect(page.getByRole('button', { name: structuredLabel })).toBeVisible()
})

test('day stripes differ from the page background and keep their parity while virtual scrolling', async ({ page }, info) => {
  await mockWorkspace(page)
  await page.goto('/logs')
  const rows = page.locator('.log-entry')
  await expect(page.locator('.log-panel footer')).toHaveText('240 条记录')
  const colors = await rows.evaluateAll(list => ({
    white: getComputedStyle(list[0]).backgroundColor,
    gray: getComputedStyle(list[1]).backgroundColor,
    outer: getComputedStyle(document.querySelector('.main-shell')).backgroundColor
  }))
  expect(colors.white).toBe('rgb(255, 255, 255)')
  expect(colors.gray).not.toBe(colors.white)
  expect(colors.gray).not.toBe(colors.outer)
  await page.locator('.log-viewport').evaluate(el => { el.scrollTop = 44 * 40 })
  await expect(rows.first()).toHaveAttribute('data-index', '35')
  const stripes = await rows.evaluateAll(list => list.map(row => ({
    odd: Number(row.dataset.index) % 2 === 1, color: getComputedStyle(row).backgroundColor
  })))
  for (const row of stripes) expect(row.color).toBe(row.odd ? colors.gray : colors.white)
  await page.screenshot({ path: info.outputPath('log-stripes-day.png') })
  await page.getByRole('button', { name: '主题：日光', exact: true }).click()
  const nightColors = await rows.evaluateAll(list => list.slice(0, 2).map(row => getComputedStyle(row).backgroundColor))
  expect(nightColors).not.toContain(colors.white)
  expect(nightColors[0]).not.toBe(nightColors[1])
  await page.screenshot({ path: info.outputPath('log-stripes-night.png') })
})
