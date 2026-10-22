import { test, expect } from '@playwright/test'

async function openWall(page) {
  await page.route('**/api/**', route => {
    const path = new URL(route.request().url()).pathname
    const json = path === '/api/auth/status' ? { initialized: true, authenticated: true }
      : path === '/api/state' ? { username: 'poster-responsive', storages: [], tasks: [{ id: 'library', kind: 'strm', name: '媒体库' }], settings: {}, traffic: {}, cache: {} }
        : path === '/api/strm-scrape/items' ? Array.from({ length: 1000 }, (_, i) => ({ path: `Film${i}.strm`, title: `影片 ${i} 测试长标题`, kind: 'movie', status: 'ok', tmdb: i + 1, poster: '/aether.svg' })) : {}
    return route.fulfill({ json })
  })
  await page.goto('/tasks/scrape')
  await expect(page.locator('.scrape-card').first()).toBeVisible()
}

for (const viewport of [
  { width: 1440, height: 1000, dpr: 1 },
  { width: 1440, height: 1000, dpr: 2 },
  { width: 1920, height: 1080, dpr: 1 },
  { width: 2560, height: 1440, dpr: 1 },
  { width: 1152, height: 800, dpr: 1.25 },
  { width: 768, height: 1024, dpr: 2 },
  { width: 390, height: 844, dpr: 3 },
  { width: 320, height: 700, dpr: 1 }
]) test(`poster proportions and virtual rows at ${viewport.width}px DPR ${viewport.dpr}`, async ({ browser }, info) => {
  const context = await browser.newContext({ viewport: { width: viewport.width, height: viewport.height }, deviceScaleFactor: viewport.dpr })
  try {
    const page = await context.newPage()
    await openWall(page)
    const wall = page.locator('.scrape-wall'), cards = wall.locator('.scrape-card')
    const columns = await wall.evaluate(el => getComputedStyle(el).gridTemplateColumns.split(' ').length)
    if (viewport.width >= 1440) expect(columns).toBe(7)
    const poster = await cards.first().locator('.scrape-poster').boundingBox()
    expect(Math.abs(poster.height / poster.width - 1.5)).toBeLessThan(.001)
    const card = await cards.first().boundingBox(), nextRow = await cards.nth(columns).boundingBox()
    expect(Math.abs(nextRow.y - card.y - card.height - 12)).toBeLessThan(.05)
    expect(Math.abs((await wall.boundingBox()).height - Math.ceil(1000 / columns) * (card.height + 12))).toBeLessThan(2)
    expect(await cards.count()).toBeLessThan(200)
    await cards.first().hover()
    for (const button of await cards.first().locator('.scrape-actions button').all()) {
      await expect(button).toHaveCSS('opacity', '1')
      const bounds = await button.boundingBox()
      expect(bounds.x).toBeGreaterThanOrEqual(poster.x)
      expect(bounds.x + bounds.width).toBeLessThanOrEqual(poster.x + poster.width)
      expect(bounds.y + bounds.height).toBeLessThan(poster.y + poster.height)
      expect(await button.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
    }
    for (const theme of ['light', 'dark']) {
      await page.evaluate(t => document.documentElement.dataset.theme = t, theme)
      await page.screenshot({ path: info.outputPath(`posters-${theme}.png`), animations: 'disabled' })
    }
    const scroll = page.locator('.page-scroll > .thin-scroll-area')
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    expect(await page.locator('.scrape-panel').evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
    await scroll.evaluate(el => { el.scrollTop = el.scrollHeight / 2 })
    await expect(cards.first()).not.toContainText('影片 0 ')
    await scroll.evaluate(el => { el.scrollTop = el.scrollHeight })
    await expect(cards.last()).toContainText('影片 999 ')
    await expect(cards.last()).toBeInViewport()
    await page.setViewportSize({ width: viewport.width === 390 ? 1440 : 390, height: 844 })
    await scroll.evaluate(el => { el.scrollTop = el.scrollHeight })
    await expect(cards.last()).toContainText('影片 999 ')
    await expect(cards.last()).toBeInViewport()
    await scroll.evaluate(el => { el.scrollTop = 0 })
    await expect(cards.first()).toContainText('影片 0 ')
  } finally { await context.close() }
})
