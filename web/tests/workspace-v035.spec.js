import { test, expect } from '@playwright/test'
import { readFileSync } from 'node:fs'

async function mock(page) {
  await page.route('**/api/auth/status', r => r.fulfill({ json: { initialized: true, authenticated: true } }))
  await page.route('**/api/state', r => r.fulfill({ json: { username: 'v035', storages: [{ id: 'local', type: 'local', name: '资料', enabled: true, config: {} }], tasks: [], settings: {}, cache: {}, traffic: {}, logs: [] } }))
  await page.route('**/api/plugins/*', r => r.fulfill({ json: {} }))
  await page.route('**/api/quark-takeover', r => r.fulfill({ json: { bindings: [] } }))
}

test('media preview, grid thumbnails and Ctrl A selection', async ({ page }, info) => {
  await mock(page)
  await page.route('**/api/files?**', r => r.fulfill({ json: [{ id: '/a.png', name: 'a.png', url: '/test-image.png', size: 100 }, { id: '/b.mp4', name: 'b.mp4', url: '/test-video.mp4', size: 100 }] }))
  await page.route('**/test-image.png', r => r.fulfill({ contentType: 'image/png', body: readFileSync(new URL('../public/providers/quark.png', import.meta.url)) }))
  await page.route('**/test-video.mp4', r => r.fulfill({ status: 404, body: '' }))
  await page.goto('/files')
  await page.getByRole('button', { name: 'a.png', exact: true }).click()
  await page.keyboard.press('Control+a')
  await expect(page.locator('.file-row.selected')).toHaveCount(2)
  await page.getByRole('button', { name: 'a.png', exact: true }).dblclick()
  await expect(page.locator('.image-viewer img')).toBeVisible()
  await expect.poll(() => page.locator('.image-viewer img').evaluate(img => img.naturalWidth)).toBeGreaterThan(1)
  await expect(page.locator('.context-menu')).toHaveCount(0)
  await page.screenshot({ path: info.outputPath('image-preview.png') })
  await page.keyboard.press('Escape')
  await page.getByRole('button', { name: 'b.mp4', exact: true }).dblclick()
  await expect(page.locator('.video-viewer video')).toHaveAttribute('controls', '')
  await page.keyboard.press('Escape')
  await page.getByRole('button', { name: '当前列表视图，切换网格' }).click()
  await expect(page.locator('.file-thumbnail')).toBeVisible()
})

test('transfer menu controls and removed tools', async ({ page }, info) => {
  await mock(page)
  let status = 'running', removed = false
  await page.route('**/api/transfers', r => r.fulfill({ json: removed ? [] : [{ id: 'download', kind: 'download', name: 'movie.mp4', storage: '资料', status, done: 100, total: 200 }] }))
  await page.route('**/api/transfers/action', r => { const action = r.request().postDataJSON().action; status = action === 'pause' ? 'paused' : 'running'; removed = action === 'delete'; return r.fulfill({ json: { ok: true } }) })
  await page.goto('/transfer')
  await page.getByRole('tab', { name: '下载' }).click()
  await page.getByRole('cell', { name: 'movie.mp4' }).click({ button: 'right' })
  await page.getByRole('button', { name: '暂停', exact: true }).click()
  await expect(page.getByRole('cell', { name: '已暂停', exact: true })).toBeVisible()
  await page.getByRole('cell', { name: 'movie.mp4' }).click({ button: 'right' })
  await page.getByRole('button', { name: '继续', exact: true }).click()
  await expect(page.getByRole('cell', { name: '传输中', exact: true })).toBeVisible()
  await page.screenshot({ path: info.outputPath('transfer.png') })
  await page.getByRole('cell', { name: 'movie.mp4' }).click({ button: 'right' })
  await page.getByRole('button', { name: '删除', exact: true }).click()
  await expect(page.getByText('暂无传输任务')).toBeVisible()
  await page.goto('/tools')
  await expect(page.locator('.plugin-card')).toHaveCount(12)
  await expect(page.getByText('115 STRM 增强', { exact: true })).toHaveCount(0)
  await expect(page.getByText('115 分享 STRM', { exact: true })).toHaveCount(0)
})
