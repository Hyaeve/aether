import { test, expect } from '@playwright/test'

test('share transfer previews selection, submits once and reports confirmed Quark completion', async ({ page }, info) => {
  const storages = ['115', 'mobile', 'quark'].map(type => ({ id: type, type, name: type, enabled: true, config: { mode: 'native' } }))
  await page.route('**/api/auth/status', r => r.fulfill({ json: { initialized: true, authenticated: true } }))
  await page.route('**/api/state', r => r.fulfill({ json: { storages, tasks: [], settings: {}, traffic: {}, cache: {} } }))
  let refreshes = 0, saves = 0, payload
  await page.route('**/api/files?**', r => { refreshes++; return r.fulfill({ json: [] }) })
  await page.route('**/api/files/share/*', r => {
    const action = new URL(r.request().url()).pathname.split('/').at(-1)
    if (action === 'preview') return r.fulfill({ json: { preview: 'private-session', items: [{ id: 'film', name: 'Arrival.2016.mkv', isDir: false }, { id: 'show', name: 'Show', isDir: true }] } })
    if (action === 'status') return r.fulfill({ json: { status: 'completed', message: '网盘已完成转存', taskId: 'job' } })
    saves++; payload = r.request().postDataJSON()
    return r.fulfill({ json: { status: 'submitted', message: '转存已提交，请刷新目标目录确认结果', taskId: 'job' } })
  })
  await page.goto('/files')
  await page.getByRole('button', { name: '工具', exact: true }).click()
  await page.getByRole('menuitem', { name: '分享转存', exact: true }).click()
  await page.getByRole('button', { name: '转存目标存储', exact: true }).click()
  await expect(page.getByRole('option')).toHaveCount(3)
  await page.getByRole('option', { name: 'quark', exact: true }).click()
  await page.getByLabel('分享链接', { exact: true }).fill('https://pan.quark.cn/s/abc')
  await page.getByLabel('提取码', { exact: true }).fill('1234')
  await page.getByRole('button', { name: '解析分享', exact: true }).click()
  await expect(page.locator('.share-items label')).toHaveCount(2)
  const check = await page.locator('.share-items input').first().boundingBox()
  const name = await page.locator('.share-items span').first().boundingBox()
  expect(check.x + check.width).toBeLessThan(name.x)
  expect(Math.abs(check.y - name.y)).toBeLessThan(10)
  await page.locator('.share-items label').filter({ hasText: 'Show' }).getByRole('checkbox').uncheck()
  for (const theme of ['light', 'dark']) {
    await page.evaluate(t => document.documentElement.dataset.theme = t, theme)
    await page.screenshot({ path: info.outputPath(`share-${theme}.png`) })
  }
  await page.setViewportSize({ width: 390, height: 844 })
  await page.screenshot({ path: info.outputPath('share-mobile.png') })
  expect(await page.getByRole('dialog').evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
  await page.getByRole('button', { name: '确认转存', exact: true }).click()
  await expect(page.locator('.share-result')).toContainText('网盘已完成转存')
  expect(saves).toBe(1)
  expect(payload).toEqual({ storageId: 'quark', preview: 'private-session', parent: '/', ids: ['film'] })
  expect(refreshes).toBeGreaterThan(1)
  await expect(page.getByRole('button', { name: '确认转存', exact: true })).toHaveCount(0)
})

test('task provider logos fill their button without an extra colored box', async ({ page }, info) => {
  const storages = ['115', 'mobile', 'quark'].map(type => ({ id: type, type, name: type, enabled: true, config: {} }))
  await page.route('**/api/auth/status', r => r.fulfill({ json: { initialized: true, authenticated: true } }))
  await page.route('**/api/state', r => r.fulfill({ json: { storages, tasks: storages.map(s => ({ id:s.id, storageId:s.id, name:s.name, kind:'strm', enabled:true, processed:0 })), settings:{}, traffic:{}, cache:{} } }))
  await page.goto('/tasks/strm')
  for (const icon of await page.locator('.task-provider-toggle .provider-icon').all()) {
    const logo = await icon.locator('img').boundingBox(), box = await icon.boundingBox()
    expect(logo.width).toBe(42)
    expect(logo.width).toBe(box.width)
    await expect(icon).toHaveCSS('background-color', 'rgba(0, 0, 0, 0)')
  }
  await page.screenshot({ path: info.outputPath('task-provider-icons.png') })
})
