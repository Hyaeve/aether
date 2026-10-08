import { test, expect } from '@playwright/test'

test('individual rename updates path before bulk preview and ignored files are excluded', async ({ page }) => {
  let file = { id: '/old.mkv', name: 'old.mkv', isDir: false }
  let preview, executed
  await page.route('**/api/auth/status', r => r.fulfill({ json: { initialized: true, authenticated: true } }))
  await page.route('**/api/state', r => r.fulfill({ json: { username: 'rename-single', storages: [{ id: 'local', type: 'local', name: 'Local', enabled: true, config: {} }], tasks: [], settings: {}, traffic: {}, cache: {} } }))
  await page.route('**/api/files?**', r => r.fulfill({ json: [file, { id: '/stay.mkv', name: 'stay.mkv' }] }))
  await page.route('**/api/files/rename-rules', r => r.fulfill({ json: [] }))
  await page.route('**/api/files/action', r => {
    const input = r.request().postDataJSON()
    expect(input).toMatchObject({ action: 'rename', ids: ['/old.mkv'], name: 'single.mkv' })
    file = { ...file, id: '/single.mkv', name: 'single.mkv' }
    return r.fulfill({ json: { processed: 1 } })
  })
  await page.route('**/api/files/rename-preview', r => {
    preview = r.request().postDataJSON()
    return r.fulfill({ json: [file, { id: '/stay.mkv', name: 'stay.mkv' }].filter(f => preview.ids.includes(f.id)).map(f => ({ ...f, newName: f.name.replace('mkv', 'mp4') })) })
  })
  await page.route('**/api/files/rename', r => { executed = r.request().postDataJSON(); return r.fulfill({ json: { processed: 1 } }) })
  await page.goto('/files')
  await page.getByRole('button', { name: '工具', exact: true }).click()
  await page.getByRole('menuitem', { name: '重命名', exact: true }).click()
  await page.locator('.rename-preview-row').first().hover()
  await page.locator('.rename-preview-row').first().getByRole('button', { name: '修改', exact: true }).click()
  await page.getByLabel('新名称', { exact: true }).fill('single.mkv')
  await page.getByRole('button', { name: '确认修改', exact: true }).click()
  await expect(page.locator('.rename-preview-row').first()).toContainText('single.mkv')
  await page.getByLabel('查找内容', { exact: true }).fill('mkv')
  await page.getByLabel('替换为', { exact: true }).fill('mp4')
  await expect.poll(() => preview?.ids).toEqual(['/single.mkv', '/stay.mkv'])
  await page.locator('.rename-preview-row').last().hover()
  await page.locator('.rename-preview-row').last().getByRole('button', { name: '忽略', exact: true }).click()
  await expect.poll(() => preview.ids).toEqual(['/single.mkv'])
  await page.getByRole('button', { name: '确认重命名', exact: true }).click()
  await expect.poll(() => executed?.ids).toEqual(['/single.mkv'])
  expect(executed.expected).toHaveLength(1)
})
