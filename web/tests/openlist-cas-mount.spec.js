import { test, expect } from '@playwright/test'

async function mock(page) {
  await page.route('**/api/auth/status', r => r.fulfill({ json: { initialized: true, authenticated: true } }))
  await page.route('**/api/state', r => r.fulfill({ json: {
    username: 'owner', storages: [{ id: 'mobile', name: '移动影音', type: 'mobile', enabled: true, config: { mode: 'native' } }],
    tasks: [], settings: {}, cache: {}, traffic: {}, logs: []
  } }))
}

test('OpenList access modes and duplicate names', async ({ page }, info) => {
  await mock(page)
  await page.goto('/storage')
  await page.getByRole('button', { name: '添加存储池', exact: true }).click()
  await page.locator('.driver-option[data-provider="openlist"]').click()
  await expect(page.getByRole('button', { name: '接入模式', exact: true })).toContainText('API令牌')
  await expect(page.getByLabel('API令牌', { exact: true })).toBeVisible()
  await expect(page.getByText('目录访问密码（可选）', { exact: true })).toHaveCount(0)
  await page.getByLabel('服务地址', { exact: false }).fill('https://olist.example')
  await page.getByRole('button', { name: '接入模式', exact: true }).click()
  await page.getByRole('option', { name: '账号密码', exact: true }).click()
  await expect(page.getByLabel('API令牌', { exact: true })).toHaveCount(0)
  await page.getByLabel('账号', { exact: true }).fill('owner')
  await page.getByLabel('密码', { exact: true }).fill('secret')
  const a = await page.getByLabel('账号', { exact: true }).boundingBox()
  const b = await page.getByLabel('密码', { exact: true }).boundingBox()
  expect(Math.abs(a.y - b.y)).toBeLessThan(2)
  await page.getByLabel('存储池名称').fill('移动影音')
  await page.getByRole('button', { name: '保存存储池', exact: true }).click()
  await expect(page.getByRole('alert')).toContainText('存储池名称已存在')
  await page.screenshot({ path: info.outputPath('openlist-account.png') })
})

test('mount details occupy full-width separate rows', async ({ page }, info) => {
  await mock(page)
  await page.route('**/api/mounts', r => r.fulfill({ json: [{ id: 'mount', name: 'CloudDrive', mountPoint: '/NetDisk', source: '/', sourceLabel: '根目录', storageId: 'mobile', uid: 0, gid: 0, mode: 493, status: 'stopped' }] }))
  await page.goto('/files/mounts')
  const rows = page.locator('.mount-details > div')
  await expect(rows).toHaveCount(2)
  const top = await page.locator('.mount-card-top').boundingBox()
  const source = await rows.nth(0).boundingBox(), permission = await rows.nth(1).boundingBox()
  expect(source.x).toBeCloseTo(top.x, 0)
  expect(source.width).toBeCloseTo(top.width, 0)
  expect(permission.y).toBeGreaterThan(source.y + source.height)
  await expect(rows.nth(1)).toContainText('0755')
  await page.screenshot({ path: info.outputPath('mount-details.png') })
})

test('mobile offline accepts CAS uploads separately from torrents', async ({ page }) => {
  await mock(page)
  await page.route('**/api/files?**', r => r.fulfill({ json: [] }))
  let sent = ''
  await page.route('**/api/files/offline', r => {
    sent = r.request().postData()
    return r.fulfill({ json: [{ name: 'film.mp4', success: true, message: '秒传完成' }] })
  })
  await page.goto('/files')
  await page.getByRole('button', { name: '工具', exact: true }).click()
  await page.getByRole('menuitem', { name: '离线下载', exact: true }).click()
  await page.getByRole('button', { name: 'CAS 秒传', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'CAS 秒传', exact: true })
  await expect(dialog.locator('input[type=file]')).toHaveAttribute('accept', '.cas')
  await dialog.locator('input[type=file]').setInputFiles({ name: 'film.mp4.cas', mimeType: 'application/octet-stream', buffer: Buffer.from('test-cas') })
  await page.getByRole('button', { name: '开始秒传', exact: true }).click()
  await expect(dialog.locator('.offline-results')).toContainText('film.mp4')
  expect(sent).toContain('name="mode"')
  expect(sent).toContain('name="cas"; filename="film.mp4.cas"')
  expect(sent).not.toContain('name="torrents"')
})
