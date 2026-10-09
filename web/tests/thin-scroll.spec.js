import { test, expect } from '@playwright/test'

test('workspace scroll starts below header and hairline survives device/pinch scales', async ({ browser }, info) => {
  for (const scale of [1, 1.25, 1.5, 2, 3]) {
    const context = await browser.newContext({ viewport: { width: 1280, height: 720 }, deviceScaleFactor: scale })
    const page = await context.newPage()
    await page.route('**/api/auth/status', r => r.fulfill({ json: { initialized: true, authenticated: true } }))
    await page.route('**/api/state', r => r.fulfill({ json: { storages: [], tasks: [], settings: {}, cache: {}, libraryNotices: Array.from({ length: 8 }, (_, i) => ({ id: String(i), name: `电影${i}`, event: 'library.new', time: new Date().toISOString() })) } }))
    await page.goto('/storage')
    await page.locator('.page-content').evaluate(el => { el.style.minHeight = '2400px' })
    const bar = await page.locator('.topbar').boundingBox(), scroll = await page.locator('.page-scroll').boundingBox()
    expect(scroll.y).toBe(bar.y + bar.height)
    await page.getByRole('button', { name: '任务通知', exact: true }).click()
    await expect(page.locator('.notice-scroll .thin-scroll-thumb')).toBeVisible()
    for (const selector of ['.page-scroll', '.notice-scroll']) {
      const thumb = page.locator(`${selector} .thin-scroll-thumb`)
      const target = selector === '.notice-scroll' ? 2 : 1
      await expect.poll(() => thumb.evaluate(el => parseFloat(getComputedStyle(el, '::after').width) * devicePixelRatio)).toBeGreaterThanOrEqual(target - .05)
      await expect.poll(() => thumb.evaluate(el => parseFloat(getComputedStyle(el, '::after').width) * devicePixelRatio)).toBeLessThanOrEqual(target + .05)
    }
    await page.locator('.page-scroll > .thin-scroll-area').evaluate(el => { el.scrollTop = 900 })
    expect(await page.evaluate(() => window.scrollY)).toBe(0)
    expect((await page.locator('.topbar').boundingBox()).y).toBe(0)
    if (scale === 2) await page.screenshot({ path: info.outputPath('thin-scroll-2x.png') })
    const cdp = await context.newCDPSession(page)
    await cdp.send('Emulation.setPageScaleFactor', { pageScaleFactor: 2 })
    await expect.poll(() => page.locator('.notice-scroll .thin-scroll-thumb').evaluate(el => parseFloat(getComputedStyle(el, '::after').width) * devicePixelRatio * visualViewport.scale)).toBeLessThanOrEqual(2.05)
    await context.close()
  }
})
