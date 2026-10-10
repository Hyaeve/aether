import { test, expect } from '@playwright/test'
import { captureScreenshot } from './helpers/screenshot.js'

async function workspace(page) {
  await page.route('**/api/plugins/*', r => r.fulfill({ json: { enabled: false, token: '' } }))
  await page.route('**/api/auth/status', r => r.fulfill({ json: { initialized: true, authenticated: true } }))
  const snapshot = {
    username: 'cache-test', storages: [
      { id: 'olist', name: 'OpenList 影音', type: 'openlist', enabled: true, config: {} },
      { id: 'quark', name: 'Quark', type: 'quark', enabled: true, config: {} }
    ], tasks: [{ id: 'warm', name: '目录预热', kind: 'cache', storageId: 'quark', source: '/', status: 'running', processed: 42, interval: 60, message: '已缓存 42 个目录 · 电影', enabled: true }],
    settings: { cacheMaxItems: 10000 }, cache: { hits: 80, misses: 20, bytes: 2048, entries: 42, evictions: 7, expired: 3 }, traffic: {}, logs: [], strmRoot: '/data/strm'
  }
  await page.route('**/api/state', r => r.fulfill({ json: snapshot }))
  await page.route('**/api/files?**', r => r.fulfill({ json: [{ id: '/B', name: 'B', isDir: true }] }))
  return snapshot
}

test('real cache overview and explicit OpenList STRM source', async ({ page }, testInfo) => {
  await workspace(page)
  await page.goto('/tasks/cache')
  await expect(page.getByRole('region', { name: '缓存命中统计' })).toContainText('80%')
  await expect(page.locator('.cache-progress')).toContainText('已缓存 42 个目录')
  await expect(page.getByText('当前缓存任务', { exact: true })).toHaveCount(0)
  await expect(page.locator('.cache-progress-track')).toHaveCSS('height', '6px')
  await expect(page.getByRole('progressbar', { name: '目录预热：扫描中，已缓存 42 个目录' })).toHaveAttribute('aria-valuetext', '扫描中，已缓存 42 个目录')
  await expect(page.locator('.cache-progress-motion')).toHaveCSS('animation-name', /^cache-scan(?:-|$)/)
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await expect(page.locator('.cache-progress-motion')).toHaveCSS('animation-name', 'none')
  await page.emulateMedia({ reducedMotion: 'no-preference' })
  await page.getByRole('button', { name: '添加任务', exact: true }).click()
  await expect(page.getByRole('spinbutton', { name: '扫描层级', exact: true })).toHaveValue('4')
  await page.getByRole('button', { name: '取消', exact: true }).click()
  for (const theme of ['light', 'dark']) {
    await page.evaluate(t => document.documentElement.dataset.theme = t, theme)
    await captureScreenshot(page, { path: testInfo.outputPath(`cache-${theme}.png`) })
  }
  await page.setViewportSize({ width: 390, height: 844 })
  await captureScreenshot(page, { path: testInfo.outputPath('cache-mobile.png') })
  expect(await page.locator('.cache-overview').evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.goto('/tasks/strm')
  await page.getByRole('button', { name: '添加任务', exact: true }).click()
  await expect(page.getByRole('button', { name: '保存任务', exact: true })).toBeDisabled()
  await expect(page.getByText('启用定时调度', { exact: true })).toHaveCount(0)
  await page.getByRole('button', { name: '选择目录', exact: true }).click()
  await page.getByRole('dialog', { name: '选择存储目录' }).getByRole('button', { name: /OpenList 影音/ }).click()
  await page.getByRole('button', { name: 'B', exact: true }).click()
  await page.getByRole('button', { name: '选择当前目录', exact: true }).click()
  await expect(page.getByRole('switch', { name: '编码路径' })).not.toBeChecked()
  await page.getByRole('switch', { name: '编码路径' }).check()
  await page.getByLabel('任务名称').fill('OpenList 同步')
  await page.getByLabel('Cron 表达式').fill('0 2 * * *')
  await expect(page.getByRole('switch', { name: '启用定时调度' })).toHaveCount(0)
  let saved
  await page.route('**/api/tasks', r => { saved = r.request().postDataJSON(); return r.fulfill({ json: saved }) })
  await page.getByRole('button', { name: '保存任务', exact: true }).click()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  expect(saved).toMatchObject({ storageId: 'olist', source: '/B', sourceLabel: 'B', encodePath: true })
})

test('cache scan counts update live and new depth saves four without changing existing all-depth tasks', async ({ page }) => {
  const snapshot = await workspace(page)
  snapshot.tasks.push({ id: 'old', name: '全层缓存', kind: 'cache', storageId: 'quark', source: '/', depth: 0, interval: 60, status: 'idle', enabled: true })
  await page.goto('/tasks/cache')
  await expect(page.locator('.cache-progress-count')).toHaveText('已缓存 42 个目录')
  snapshot.tasks[0].processed = 57
  snapshot.tasks[0].message = '已缓存 57 个目录 · 剧集'
  await expect(page.locator('.cache-progress-count')).toHaveText('已缓存 57 个目录', { timeout: 10000 })
  await expect(page.getByRole('progressbar')).toHaveAttribute('aria-valuetext', '扫描中，已缓存 57 个目录')
  await page.getByRole('button', { name: '任务操作 全层缓存', exact: true }).click()
  await page.getByRole('button', { name: '编辑任务', exact: true }).click()
  await expect(page.getByRole('spinbutton', { name: '扫描层级', exact: true })).toHaveValue('0')
  await page.getByRole('button', { name: '取消', exact: true }).click()
  await page.getByRole('button', { name: '添加任务', exact: true }).click()
  await page.getByLabel('任务名称').fill('四层缓存')
  await page.getByRole('button', { name: '选择目录', exact: true }).click()
  await page.getByRole('dialog', { name: '选择存储目录' }).getByRole('button', { name: /Quark/ }).click()
  await page.getByRole('button', { name: '选择当前目录', exact: true }).click()
  let saved
  await page.route('**/api/tasks', r => { saved = r.request().postDataJSON(); return r.fulfill({ json: saved }) })
  await page.getByRole('button', { name: '保存任务', exact: true }).click()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  expect(saved).toMatchObject({ kind: 'cache', depth: 4, storageId: 'quark' })
  snapshot.tasks[0].status = 'success'
  await expect(page.getByLabel('暂无执行中的缓存任务')).toBeVisible({ timeout: 10000 })
  await expect(page.locator('.cache-progress-status')).toHaveText('空闲')
})

test('Quark takeover binds once by QR and toggles from its icon', async ({ page }, testInfo) => {
  await workspace(page)
  let bindings = [], enabled = true, authorize = false
  await page.route('**/api/quark-takeover/quark', r => r.fulfill({ json: { config: { enabled: true, mode: 'adaptive', quality: '4k', allowDolby: false, uaListMode: 'proxy_list', ua: '' } } }))
  await page.route('**/api/quark-takeover', r => {
    if (r.request().method() === 'PUT') enabled = r.request().postDataJSON().enabled
    return r.fulfill({ json: { enabled, bindings, broker: 'https://broker.example' } })
  })
  await page.route('**/api/quark-takeover/quark/qr', r => r.fulfill({ json: { session: 'scan', image: 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/l9sAAAAASUVORK5CYII=' } }))
  await page.route('**/api/quark-takeover/quark/poll', r => {
    if (!authorize) return r.fulfill({ json: { authorized: false } })
    bindings = [{ id: 'quark', name: 'Quark', nickname: '同账号', enabled: true, valid: true }]
    return r.fulfill({ json: { authorized: true } })
  })
  await page.goto('/tools')
  const card = page.getByRole('button', { name: '夸克 STRM 接管', exact: true })
  await expect(card).not.toContainText('待实现')
  await expect(card).toContainText('夸克网盘 · TV 版 302 直链')
  await expect(card.locator('img')).toHaveAttribute('src', '/providers/quark.png')
  for (const theme of ['light', 'dark']) {
    await page.evaluate(t => document.documentElement.dataset.theme = t, theme)
    await captureScreenshot(page, { path: testInfo.outputPath(`quark-card-${theme}.png`) })
  }
  await card.getByText('夸克 STRM 接管', { exact: true }).click()
  await page.getByRole('button', { name: '添加绑定', exact: true }).click()
  await expect(page.getByRole('button', { name: '绑定夸克存储', exact: true })).toContainText('Quark')
  await expect(page.getByRole('checkbox', { name: /同意通过第三方/ })).toBeChecked()
  await expect(page.getByAltText('夸克 TV 授权二维码')).toBeVisible()
  await captureScreenshot(page, { path: testInfo.outputPath('quark-qr.png') })
  authorize = true
  await expect(page.getByRole('dialog', { name: '选择绑定的存储', exact: true })).toHaveCount(0)
  await page.getByRole('button', { name: '添加绑定', exact: true }).click()
  await expect(page.getByText('所有夸克存储均已绑定', { exact: true })).toBeVisible()
  await expect(page.locator('.binding-row')).toContainText('同账号')
  await captureScreenshot(page, { path: testInfo.outputPath('quark-bound.png') })
  await page.setViewportSize({ width: 390, height: 844 })
  expect(await page.getByRole('dialog').evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
  await captureScreenshot(page, { path: testInfo.outputPath('quark-bound-mobile.png') })
  await page.getByRole('dialog').getByRole('button', { name: '关闭', exact: true }).click()
  await page.getByRole('button', { name: '停用夸克 STRM 接管', exact: true }).click()
  await expect(page.getByRole('button', { name: '启用夸克 STRM 接管', exact: true })).toHaveAttribute('aria-pressed', 'false')
})
