import { test, expect } from '@playwright/test'

async function workspace(page) {
  await page.route('**/api/auth/status', r => r.fulfill({ json: { initialized: true, authenticated: true } }))
  await page.route('**/api/state', r => r.fulfill({ json: {
    username: 'cache-test', storages: [
      { id: 'olist', name: 'OpenList 影音', type: 'openlist', enabled: true, config: {} },
      { id: 'quark', name: 'Quark', type: 'quark', enabled: true, config: {} }
    ], tasks: [{ id: 'warm', name: '目录预热', kind: 'cache', storageId: 'quark', source: '/', status: 'running', processed: 42, interval: 60, message: '已缓存 42 个目录 · 电影', enabled: true }],
    settings: { cacheMaxItems: 10000 }, cache: { hits: 80, misses: 20, bytes: 2048, entries: 42, evictions: 7, expired: 3 }, traffic: {}, logs: [], strmRoot: '/data/strm'
  } }))
  await page.route('**/api/files?**', r => r.fulfill({ json: [{ id: '/B', name: 'B', isDir: true }] }))
}

test('real cache overview and explicit OpenList STRM source', async ({ page }, testInfo) => {
  await workspace(page)
  await page.goto('/tasks/cache')
  await expect(page.getByRole('region', { name: '缓存命中统计' })).toContainText('80%')
  await expect(page.locator('.cache-progress')).toContainText('已缓存 42 个目录')
  for (const theme of ['light', 'dark']) {
    await page.evaluate(t => document.documentElement.dataset.theme = t, theme)
    await page.screenshot({ path: testInfo.outputPath(`cache-${theme}.png`) })
  }
  await page.setViewportSize({ width: 390, height: 844 })
  await page.screenshot({ path: testInfo.outputPath('cache-mobile.png') })
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
  await expect(page.getByRole('switch', { name: '启用定时调度' })).toBeChecked()
  let saved
  await page.route('**/api/tasks', r => { saved = r.request().postDataJSON(); return r.fulfill({ json: saved }) })
  await page.getByRole('button', { name: '保存任务', exact: true }).click()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  expect(saved).toMatchObject({ storageId: 'olist', source: '/B', sourceLabel: 'B', encodePath: true })
})

test('Quark takeover is configurable with explicit broker consent', async ({ page }, testInfo) => {
  await workspace(page)
  await page.route('**/api/quark-takeover/quark', r => r.fulfill({ json: r.request().method() === 'GET' ? { authorized: false, config: { enabled: false, mode: 'adaptive', quality: '4k', device: 'test-device', uaListMode: 'proxy_list', broker: '', accessToken: '', refreshToken: '' } } : { ok: true } }))
  await page.goto('/tools')
  const card = page.getByRole('button', { name: /夸克 STRM 接管/ })
  await expect(card).not.toContainText('待实现')
  await expect(card.locator('img')).toHaveAttribute('src', '/providers/quark.png')
  await card.click()
  await page.getByRole('button', { name: '绑定夸克存储', exact: true }).click()
  await page.getByRole('option', { name: 'Quark', exact: true }).click()
  await expect(page.getByRole('button', { name: '扫码绑定' })).toBeDisabled()
  await page.getByLabel('TV Access Token', { exact: true }).fill('manual-access-token')
  await page.getByRole('switch', { name: '启用接管', exact: true }).check()
  await page.getByRole('button', { name: '保存设置', exact: true }).click()
  await expect(page.getByRole('status')).toContainText('夸克 STRM 接管已保存')
  await expect(page.getByLabel('TV Access Token', { exact: true })).toHaveValue('')
  await page.getByLabel('HTTPS 凭据换取服务').fill('https://broker.example')
  await expect(page.getByRole('button', { name: '扫码绑定' })).toBeDisabled()
  await page.getByRole('checkbox', { name: /我信任该服务/ }).check()
  await expect(page.getByRole('button', { name: '扫码绑定' })).toBeEnabled()
  for (const theme of ['light', 'dark']) {
    await page.evaluate(t => document.documentElement.dataset.theme = t, theme)
    await expect(page.getByRole('button', { name: '绑定夸克存储', exact: true })).toHaveCSS('background-color', theme === 'dark' ? 'rgb(36, 40, 50)' : 'rgb(255, 255, 255)')
    await page.screenshot({ path: testInfo.outputPath(`quark-${theme}.png`) })
  }
})
