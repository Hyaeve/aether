import { test, expect } from '@playwright/test'

test('ED2K sources include local and 115 only; cache progress remains compact', async ({ page }, info) => {
  const storages = ['local', '115', 'mobile', 'tianyi', 'quark'].map(type => ({ id: type, name: `源-${type}`, type, enabled: true, config: { mode: 'native' } }))
  await page.route('**/api/auth/status', r => r.fulfill({ json: { initialized: true, authenticated: true } }))
  await page.route('**/api/state', r => r.fulfill({ json: { storages, tasks: [], settings: {}, cache: {} } }))
  await page.goto('/tasks/ed2k')
  await page.getByRole('button', { name: '添加任务', exact: true }).click()
  await page.getByRole('button', { name: '选择目录', exact: true }).click()
  const accounts = page.locator('.source-accounts')
  await expect(accounts).toContainText('源-local')
  await expect(accounts).toContainText('源-115')
  for (const type of ['mobile', 'tianyi', 'quark']) await expect(accounts).not.toContainText(`源-${type}`)
  await page.screenshot({ path: info.outputPath('ed2k-sources.png') })
  await page.goto('/tasks/cache')
  await expect(page.locator('.cache-progress-track')).toHaveCSS('height', '6px')
  await page.screenshot({ path: info.outputPath('cache-compact.png') })
})
