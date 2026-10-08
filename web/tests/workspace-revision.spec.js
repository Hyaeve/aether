import { test, expect } from '@playwright/test'
import { changedNameParts } from '../src/name-diff'

test('rename changes highlight separate replacements without marking unchanged text', () => {
  const parts = changedNameParts('Old Movie Old.mkv', 'New Movie New.mkv')
  expect(parts.filter(p => p.changed).map(p => p.text)).toEqual(['New', 'New'])
  expect(parts.map(p => p.text).join('')).toBe('New Movie New.mkv')
})

async function mock(page) {
  await page.route('**/api/auth/status', r => r.fulfill({ json: { initialized: true, authenticated: true } }))
  await page.route('**/api/state', r => r.fulfill({ json: { username: 'revision', storages: [{ id: 'local', type: 'local', name: '资料', enabled: true, status: 'error', config: {} }], tasks: [], settings: {}, cache: {}, traffic: {}, logs: [] } }))
  await page.route('**/api/files?**', r => r.fulfill({ json: [{ id: '/Movies', name: 'Movies', isDir: true, size: 20, sizeKnown: true, countsKnown: true, folderCount: 1, fileCount: 2 }, { id: '/film.mkv', name: 'film.mkv', size: 5 }] }))
}

test('storage shortcuts, health border and multi-selection overview', async ({ page }, info) => {
  await mock(page)
  await page.goto('/storage')
  await expect(page.locator('.storage-card')).toHaveClass(/storage-unhealthy/)
  await page.goto('/dashboard')
  await expect(page.getByText('存储连接', { exact: true })).toHaveCount(0)
  await page.locator('.storage-shortcuts button').click()
  await expect(page).toHaveURL(/storage=local/)
  await page.getByRole('button', { name: 'Movies', exact: true }).click({ button: 'right' })
  await page.getByRole('button', { name: '查看详情', exact: true }).click()
  await expect(page.getByRole('button', { name: '复制位置' })).toContainText('资料 / Movies')
  await page.keyboard.press('Escape')
  await page.getByRole('button', { name: 'Movies', exact: true }).click()
  await page.getByRole('button', { name: 'film.mkv', exact: true }).click({ modifiers: ['Shift'] })
  await page.getByRole('button', { name: 'film.mkv', exact: true }).click({ button: 'right' })
  await page.getByRole('button', { name: '查看详情', exact: true }).click()
  await expect(page.locator('.file-details dl')).toHaveCount(1)
  await expect(page.locator('.file-details')).toContainText('2 个文件夹，3 个文件')
  await page.screenshot({ path: info.outputPath('details.png') })
})

test('TMDB opens language upward and transfer controls share colors', async ({ page }, info) => {
  await mock(page)
  await page.route('**/api/plugins/*', r => r.fulfill({ json: { apiURL: 'https://api.example', imageURL: 'https://image.example', language: 'zh-CN' } }))
  await page.route('**/api/quark-takeover', r => r.fulfill({ json: { bindings: [] } }))
  await page.goto('/tools')
  await page.getByRole('button', { name: 'TMDB 配置', exact: true }).click()
  await page.getByRole('button', { name: '匹配语言', exact: true }).click()
  await expect(page.locator('.rounded-select.opens-up')).toHaveCount(1)
  await expect(page.getByRole('listbox')).toBeVisible()
  await page.waitForTimeout(300)
  const menu = await page.getByRole('listbox').boundingBox(), trigger = await page.getByRole('button', { name: '匹配语言', exact: true }).boundingBox()
  expect(menu.y + menu.height).toBeLessThanOrEqual(trigger.y)
  await page.screenshot({ path: info.outputPath('tmdb-upward.png') })
  await page.route('**/api/transfers', r => r.fulfill({ json: [{ id: 'up', kind: 'upload', name: 'movie.mkv', status: 'running', done: 1, total: 5 }] }))
  await page.goto('/transfer')
  await page.getByRole('tab', { name: '上传' }).click()
  await expect(page.locator('.transfer-table')).toContainText('movie.mkv')
  await page.getByRole('textbox', { name: '搜索传输任务' }).fill('absent')
  await expect(page.locator('.transfer-table')).toContainText('暂无传输任务')
  expect(await page.locator('.traffic-stat.upload b').evaluate(n => getComputedStyle(n).color)).toBe(await page.locator('.transfer-modes .upload svg').evaluate(n => getComputedStyle(n).color))
})
