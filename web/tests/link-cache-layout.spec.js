import { test, expect } from '@playwright/test'

test('prefetched links render before list refresh, with compact playback and aligned favorites', async ({ page }, info) => {
  const links = [{ id: 'abs', name: '有声书', type: 'audiobookshelf', mode: 'always', port: 18080, enabled: true }]
  await page.route('**/api/auth/status', r => r.fulfill({ json: { initialized: true, authenticated: true } }))
  await page.route('**/api/state', r => r.fulfill({ json: {
    username: 'layout', links, storages: [{ id: 'local', name: '本地文件', type: 'local', enabled: true, config: {} }],
    settings: {}, tasks: [], traffic: {}, cache: {}, logs: []
  } }))
  let release
  await page.route('**/api/links', async r => {
    await new Promise(resolve => { release = resolve })
    await r.fulfill({ json: links })
  })
  await page.route('**/api/link-playback', r => r.fulfill({ json: { recentEvents: [{
    time: '2026-10-07T12:34:56+08:00', upstream: 'abs', userAgent: 'Komic-iOS/1.0',
    outcome: 'transcode', target: 'https://cdn.example/audiobook.m4a', client: '192.168.1.12', cacheSource: 'hit', cacheTtlSeconds: 7200, durationMs: 12000000
  }] } }))
  await page.route('**/api/files?**', r => r.fulfill({ json: [{ id: '/book', name: '有声书', isDir: true }] }))
  await page.goto('/links/manage')
  await expect(page.locator('.link-card')).toHaveCount(1)
  await expect.poll(() => !!release).toBe(true)
  release()
  await page.getByRole('button', { name: '添加以太链接', exact: true }).click()
  await expect(page.getByRole('button', { name: 'AudioBookShelf', exact: true })).toBeVisible()
  await page.keyboard.press('Escape')
  await page.getByRole('link', { name: '直链缓存', exact: true }).click()
  await expect(page.locator('.playback-result')).toHaveText('适配')
  const columns = await page.locator('.playback-scroller th').evaluateAll(nodes => nodes.map(n => n.getBoundingClientRect().width))
  expect(columns[1]).toBeGreaterThanOrEqual(110)
  expect(columns[7]).toBeLessThanOrEqual(80)
  for (const theme of ['light', 'dark']) {
    await page.evaluate(t => document.documentElement.dataset.theme = t, theme)
    await page.screenshot({ path: info.outputPath(`playback-${theme}.png`) })
  }
  await page.getByRole('link', { name: '文件服务', exact: true }).click()
  await expect(page.locator('.file-name svg').first()).toBeVisible()
  const toggle = await page.locator('.file-toolbar > button:first-child svg').boundingBox()
  const file = await page.locator('.file-name svg').first().boundingBox()
  expect(Math.abs(toggle.x + toggle.width / 2 - file.x - file.width / 2)).toBeLessThan(3)
  await page.getByRole('button', { name: '展开收藏栏', exact: true }).click()
  await expect(page.locator('.file-favorites')).toHaveCSS('opacity', '1')
  await page.screenshot({ path: info.outputPath('favorites.png') })
})
