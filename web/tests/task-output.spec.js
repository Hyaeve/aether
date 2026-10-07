import { test, expect } from '@playwright/test'

test('task output selects an arbitrary container directory inside the input', async ({ page }, info) => {
  await page.route('**/api/auth/status', r => r.fulfill({ json: { initialized: true, authenticated: true } }))
  await page.route('**/api/state', r => r.fulfill({ json: { storages: [{ id: 'local', name: '资料', type: 'local', enabled: true, config: {} }], tasks: [], settings: {}, traffic: {}, cache: {}, strmRoot: '/data/strm' } }))
  await page.route('**/api/local-directories?**', r => {
    const path = new URL(r.request().url()).searchParams.get('path') || '/'
    return r.fulfill({ json: { path, items: path === '/' ? [{ name: 'media', path: '/media' }] : [] } })
  })
  await page.goto('/tasks/strm')
  await page.getByRole('button', { name: '添加任务', exact: true }).click()
  await expect(page.getByLabel('生成目录', { exact: true })).toHaveValue('')
  const button = page.getByRole('button', { name: '选择生成目录', exact: true })
  const field = page.locator('.directory-input')
  const b = await button.boundingBox(), f = await field.boundingBox()
  expect(b.x + b.width).toBeLessThanOrEqual(f.x + f.width)
  await button.click()
  await page.getByRole('button', { name: 'media', exact: true }).click()
  await page.getByRole('button', { name: '选择当前目录', exact: true }).click()
  await expect(page.getByLabel('生成目录', { exact: true })).toHaveValue('/media')
  await page.screenshot({ path: info.outputPath('task-output-picker.png'), fullPage: true })
})
