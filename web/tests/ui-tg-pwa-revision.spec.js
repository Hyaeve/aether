import { test, expect } from '@playwright/test'
import { captureScreenshot } from './helpers/screenshot.js'

async function fixture(page, count = 2) {
  const storages = ['115', 'mobile', 'quark'].map(type => ({ id: type, type, name: `${type} 影视`, enabled: true, config: { mode: 'native' } }))
  const targets = storages.flatMap(s => Array.from({ length: count }, (_, i) => ({ id: `${s.id}-${i}`, provider: s.type, storageId: s.id, parent: '0', path: `/影视/目录${i}`, shareTransfer: true })))
  await page.route('**/api/**', route => {
    const path = new URL(route.request().url()).pathname
    let json = {}
    if (path === '/api/auth/status') json = { initialized: true, authenticated: true }
    if (path === '/api/state') json = { storages, tasks: [], settings: {}, traffic: {}, cache: {} }
    if (path === '/api/notification-service') json = { telegram: { enabled: false, userIds: [] }, targets }
    if (path === '/api/files') json = [{ id: 'folder', name: '完整目录', isDir: true }]
    if (path === '/api/virtual-library/quark') json = { cards: [{ id: 'card', name: '分享目录', enabled: true, directory: '/media', directoryName: 'Show', result: {} }], settings: { storageIds: ['quark'] }, stats: {} }
    if (path === '/api/quark-takeover') json = { bindings: [] }
    if (path === '/api/115-recycle-cleanup') json = { rules: [] }
    return route.fulfill({ json })
  })
}

test('Quark summary precedes cards below tabs', async ({ page }) => {
  await fixture(page)
  await page.goto('/virtual/quark')
  const tabs = await page.locator('.task-heading').boundingBox()
  const summary = await page.locator('.quark-library-stats').boundingBox()
  const cards = await page.locator('.quark-library-grid').boundingBox()
  expect(summary.y).toBeGreaterThanOrEqual(tabs.y + tabs.height)
  expect(summary.y + summary.height).toBeLessThanOrEqual(cards.y)
})

test('plugin search has one integrated input header and descriptions', async ({ page }, info) => {
  await fixture(page)
  await page.goto('/tools')
  await page.getByRole('button', { name: '搜索辅助工具', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '搜索辅助工具', exact: true })
  await expect(dialog.locator('.modal-header')).toHaveCount(0)
  await expect(dialog.locator('.plugin-search-result').filter({hasText:'115 网盘 · SHA1 秒传'})).toContainText('通过 SHA1 哈希')
  await expect(dialog.locator('.plugin-search-result').nth(1).locator('.provider-logo')).toHaveCSS('width', '40px')
  await expect(dialog.getByRole('textbox', { name: '插件搜索关键词' })).toHaveAttribute('placeholder', '按插件名称或描述搜索')
  await captureScreenshot(page, { path: info.outputPath('search-desktop.png') })
})

for (const width of [1440, 701, 700, 390]) test(`directory toolbar new button stays inline ${width}`, async ({ page }) => {
  await fixture(page)
  await page.setViewportSize({ width, height: 950 })
  await page.goto('/notifications/transfers')
  await page.getByRole('button', { name: '添加夸克云盘转存目录', exact: true }).click()
  await page.getByRole('button', { name: '选择转存目录', exact: true }).click()
  const toolbar = page.locator('.source-toolbar'), button = toolbar.getByRole('button', { name: '新建文件夹', exact: true })
  await expect(button).toHaveText('新建')
  const path = await toolbar.locator('.directory-crumbs').boundingBox(), box = await button.boundingBox()
  expect(Math.abs(path.y + path.height / 2 - box.y - box.height / 2)).toBeLessThan(1)
})

for (const width of [1440, 390]) for (const theme of ['light', 'dark']) test(`fixed directory pools and clickable cards ${width} ${theme}`, async ({ page }, info) => {
  await fixture(page, 18)
  await page.setViewportSize({ width, height: 950 })
  await page.goto('/notifications/transfers')
  await page.evaluate(theme => document.documentElement.dataset.theme = theme, theme)
  const list = page.locator('.telegram-target-list').first()
  expect(await list.evaluate(el => el.scrollHeight > el.clientHeight)).toBe(true)
  const logos = await page.locator('.telegram-pool h2 .provider-logo').evaluateAll(els => els.map(el => el.getBoundingClientRect().width))
  expect(Math.max(...logos) - Math.min(...logos)).toBeLessThan(1)
  if (width > 1100) {
    const bottom = await page.locator('.telegram-pools').evaluate(el => el.getBoundingClientRect().bottom)
    expect(Math.abs(950 - bottom - 8)).toBeLessThan(1)
  }
  await page.locator('.telegram-target-card').first().locator('strong').click()
  await expect(page.getByRole('dialog', { name: '编辑转存目录', exact: true })).toBeVisible()
  await page.keyboard.press('Escape')
  await captureScreenshot(page, { path: info.outputPath(`pools-${width}-${theme}.png`) })
  expect(await page.locator('.notification-page').evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
})

test.describe('PWA integration', () => {
test.use({ serviceWorkers: 'allow' })
test('PWA manifest and secure worker are available without changing the favicon', async ({ page, request, context }) => {
  await fixture(page)
  await page.goto('/tools')
  await expect(page.locator('link[rel=manifest]')).toHaveAttribute('href', '/manifest.webmanifest')
  await expect(page.locator('link[rel=icon]')).toHaveAttribute('href', '/aether-favicon.svg?v=3')
  const response = await request.get('/manifest.webmanifest')
  expect(response.headers()['content-type']).toContain('manifest')
  const manifest = await response.json()
  expect(manifest.display).toBe('standalone')
  expect(manifest.icons.map(icon => icon.sizes)).toEqual(expect.arrayContaining(['192x192', '512x512']))
  await expect.poll(() => page.evaluate(async () => (await navigator.serviceWorker.getRegistration('/'))?.active?.scriptURL || '')).toContain('/sw.js')
  await expect.poll(() => page.evaluate(() => !!navigator.serviceWorker.controller)).toBe(true)
  const cached = await page.evaluate(async () => {
    const lists = await Promise.all((await caches.keys()).map(async key => (await (await caches.open(key)).keys()).map(request => new URL(request.url).pathname)))
    return lists.flat()
  })
  expect(cached).toEqual(['/pwa/offline.html'])
  await context.setOffline(true)
  await page.goto('/notifications/transfers')
  await expect(page.getByRole('heading', { name: 'Aether 暂时离线', exact: true })).toBeVisible()
  await context.setOffline(false)
  await page.evaluate(async () => {
    await navigator.serviceWorker.ready
    await Promise.all((await navigator.serviceWorker.getRegistrations()).map(r => r.unregister()))
  })
})
})
