import { test, expect } from '@playwright/test'
import { transferStates } from '../src/transfer-status.mjs'
import { captureScreenshot } from './helpers/screenshot.js'

async function setup(page, theme = 'light') {
  const unexpected = []
  const entries = ['copy', 'upload', 'download'].flatMap(kind => transferStates.map(option => ({
    id: `${kind}-${option.value}`, kind, status: option.value, name: `${kind}-${option.value}.mkv`,
    done: 25, total: 100, speed: 1024, started: '2026-10-30T00:00:00Z',
    from: { storage: '来源', type: 'mobile', directory: '源目录' },
    to: { storage: '目标', type: 'local', directory: '目标目录' }
  })))
  await page.addInitScript(value => localStorage.setItem('aether-theme', value), theme)
  await page.route('**/api/**', route => {
    const path = new URL(route.request().url()).pathname
    if (path === '/api/auth/status') return route.fulfill({ json: { initialized: true, authenticated: true } })
    if (path === '/api/state') return route.fulfill({ json: { username: 'transfer-filter', storages: [], tasks: [], settings: {}, cache: {}, traffic: {} } })
    if (path === '/api/transfers') return route.fulfill({ json: entries })
    if (path === '/api/backup-rules') return route.fulfill({ json: [] })
    if (path === '/api/traffic') return route.fulfill({ json: {} })
    if (path === '/api/strm-replace') return route.fulfill({ json: { tasks: [] } })
    unexpected.push(path)
    return route.fulfill({ status: 500, json: { error: `Unmocked API: ${path}` } })
  })
  return unexpected
}

const layouts = [[1440, 950, 'light'], [1440, 950, 'dark'], [375, 812, 'light'], [375, 812, 'dark'], [320, 740, 'light'], [844, 390, 'dark']]
for (const [width, height, theme] of layouts) test(`transfer filter has bordered trigger and checked rows ${width} ${theme}`, async ({ page }, info) => {
  const unexpected = await setup(page, theme)
  await page.setViewportSize({ width, height })
  await page.goto('/transfer')
  const trigger = page.getByRole('button', { name: '筛选传输状态', exact: true })
  await expect(trigger).toHaveText('筛选')
  await expect(trigger.locator('svg.lucide-filter-icon')).toHaveCount(1)
  await expect(trigger).toHaveCSS('border-top-width', '1px')
  await expect(trigger).toHaveCSS('border-radius', '5px')
  await expect(trigger).toHaveCSS('height', '36px')
  await expect(trigger).toHaveAttribute('aria-haspopup', 'menu')
  await expect(trigger).toHaveAttribute('aria-expanded', 'false')
  await trigger.click()
  const menu = page.getByRole('menu', { name: '传输状态筛选', exact: true })
  await expect(trigger).toHaveAttribute('aria-controls', await menu.getAttribute('id'))
  const options = menu.getByRole('menuitemcheckbox')
  await expect(options).toHaveCount(11)
  await expect(menu.locator('input')).toHaveCount(0)
  await expect(menu).toHaveCSS('scrollbar-width', 'none')
  const all = menu.getByRole('menuitemcheckbox', { name: '所有状态', exact: true })
  await expect(all).toHaveAttribute('aria-checked', 'true')
  await expect(all.locator('.transfer-filter-check svg')).toHaveCount(1)
  for (const option of await options.all()) {
    const geometry = await option.evaluate(el => {
      const rect = node => node.getBoundingClientRect()
      const icon = rect(el.querySelector('.transfer-filter-icon')), label = rect(el.querySelector('.transfer-filter-label')), check = rect(el.querySelector('.transfer-filter-check')), row = rect(el)
      return { ordered: icon.right <= label.left && label.right <= check.left, centered: Math.abs(icon.y + icon.height / 2 - check.y - check.height / 2), height: row.height, clipped: el.scrollWidth > el.clientWidth }
    })
    expect(geometry.ordered).toBe(true)
    expect(geometry.centered).toBeLessThan(1)
    expect(geometry.height).toBeGreaterThanOrEqual(36)
    expect(geometry.clipped).toBe(false)
    await expect(option.locator('.transfer-filter-label')).toHaveCSS('white-space', 'nowrap')
  }
  await menu.getByRole('menuitemcheckbox', { name: '失败', exact: true }).click()
  await expect(all).toHaveAttribute('aria-checked', 'false')
  await expect(menu.getByRole('menuitemcheckbox', { name: '失败', exact: true })).toHaveAttribute('aria-checked', 'true')
  await menu.getByRole('menuitemcheckbox', { name: '成功', exact: true }).click()
  await expect(page.locator('.transfer-table tbody tr:not(.transfer-spacer)')).toHaveCount(2)
  await expect(menu.locator('.transfer-filter-check svg')).toHaveCount(2)
  await menu.evaluate(el => { el.scrollTop = 0 })
  const bounds = await menu.boundingBox()
  expect(bounds.x).toBeGreaterThanOrEqual(8)
  expect(bounds.x + bounds.width).toBeLessThanOrEqual(width - 8)
  expect(bounds.y + bounds.height).toBeLessThanOrEqual(height - 8)
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await captureScreenshot(page, { path: info.outputPath(`filter-${width}-${theme}.png`) })
  await all.click()
  await expect(all).toHaveAttribute('aria-checked', 'true')
  await expect(menu.locator('.transfer-filter-check svg')).toHaveCount(1)
  await expect(page.locator('.transfer-table tbody tr:not(.transfer-spacer)')).toHaveCount(10)
  await trigger.click()
  await expect(menu).toHaveCount(0)
  expect(unexpected).toEqual([])
})

test('transfer filter supports keyboard navigation, focus return and outside dismissal', async ({ page }) => {
  const unexpected = await setup(page)
  await page.goto('/transfer')
  const trigger = page.getByRole('button', { name: '筛选传输状态', exact: true })
  const menu = page.getByRole('menu', { name: '传输状态筛选', exact: true })
  await trigger.focus()
  await page.keyboard.press('ArrowDown')
  await expect(menu.getByRole('menuitemcheckbox', { name: '所有状态', exact: true })).toBeFocused()
  await page.keyboard.press('End')
  const error = menu.getByRole('menuitemcheckbox', { name: '错误', exact: true })
  await expect(error).toBeFocused()
  await page.keyboard.press('Space')
  await expect(error).toHaveAttribute('aria-checked', 'true')
  await page.keyboard.press('ArrowUp')
  await page.keyboard.press('Enter')
  await expect(menu.getByRole('menuitemcheckbox', { name: '失败', exact: true })).toHaveAttribute('aria-checked', 'true')
  await expect(page.locator('.transfer-table tbody tr:not(.transfer-spacer)')).toHaveCount(2)
  await page.keyboard.press('Home')
  await page.keyboard.press('Enter')
  await expect(page.locator('.transfer-table tbody tr:not(.transfer-spacer)')).toHaveCount(10)
  await page.keyboard.press('Escape')
  await expect(menu).toHaveCount(0)
  await expect(trigger).toBeFocused()
  await page.keyboard.press('ArrowUp')
  await expect(menu.getByRole('menuitemcheckbox', { name: '错误', exact: true })).toBeFocused()
  await page.keyboard.press('Tab')
  await expect(menu).toHaveCount(0)
  await trigger.click()
  await page.getByRole('textbox', { name: '搜索传输任务', exact: true }).click()
  await expect(menu).toHaveCount(0)
  expect(unexpected).toEqual([])
})

test('transfer filter resets to all states on mode change, navigation and reload', async ({ page }) => {
  const unexpected = await setup(page)
  await page.goto('/transfer')
  const trigger = page.getByRole('button', { name: '筛选传输状态', exact: true })
  const menu = page.getByRole('menu', { name: '传输状态筛选', exact: true })
  const rows = page.locator('.transfer-table tbody tr:not(.transfer-spacer)')
  async function filterFailed() {
    await trigger.click()
    await menu.getByRole('menuitemcheckbox', { name: '失败', exact: true }).click()
    await expect(rows).toHaveCount(1)
  }
  async function checkAll() {
    await expect(rows).toHaveCount(10)
    await expect(menu).toHaveCount(0)
    await trigger.click()
    await expect(menu.getByRole('menuitemcheckbox', { name: '所有状态', exact: true })).toHaveAttribute('aria-checked', 'true')
    await trigger.click()
  }
  await filterFailed()
  await page.getByRole('tab', { name: /^上传/ }).click()
  await checkAll()
  await filterFailed()
  await page.getByRole('link', { name: '备份规则', exact: true }).click()
  await page.getByRole('link', { name: '传输任务', exact: true }).click()
  await checkAll()
  await filterFailed()
  await page.reload()
  await checkAll()
  expect(unexpected).toEqual([])
})
