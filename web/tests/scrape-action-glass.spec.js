import { test, expect } from '@playwright/test'

async function openPoster(page, running = false) {
  const poster = await page.evaluate(() => {
    const canvas = document.createElement('canvas')
    canvas.width = 240; canvas.height = 320
    const ctx = canvas.getContext('2d')
    for (let y = 0; y < 320; y += 8) for (let x = 0; x < 240; x += 8) {
      ctx.fillStyle = (x + y) % 16 ? '#b72f71' : '#ece496'
      ctx.fillRect(x, y, 8, 8)
    }
    return canvas.toDataURL('image/png').split(',')[1]
  })
  await page.route('**/glass-poster.png', r => r.fulfill({ contentType: 'image/png', body: Buffer.from(poster, 'base64') }))
  await page.route('**/api/**', r => {
    const path = new URL(r.request().url()).pathname
    const json = path === '/api/auth/status' ? { initialized: true, authenticated: true } : path === '/api/state' ? {
      username: 'scrape-glass', storages: [], tasks: [{ id: 'library', kind: 'strm', name: '媒体库' }], settings: {}, traffic: {}, cache: {}
    } : path === '/api/strm-scrape/items' ? [{ path: 'Show/a.strm', title: '作品', kind: 'movie', status: 'ok', tmdb: 1, poster: '/glass-poster.png' }]
      : path === '/api/strm-scrape/status' ? { running } : {}
    return r.fulfill({ json })
  })
  await page.goto('/tasks/scrape')
  const card = page.locator('.scrape-card').first()
  await expect.poll(() => card.locator('img').evaluate(img => img.complete && img.naturalWidth > 0)).toBe(true)
  return card
}

for (const theme of ['light', 'dark']) test(`poster glass stays blurred throughout reveal in ${theme} theme`, async ({ page }, info) => {
  const card = await openPoster(page)
  await page.evaluate(t => { document.documentElement.dataset.theme = t }, theme)
  await card.hover({ position: { x: 20, y: 20 } })
  // Freeze just before the end, where ancestor opacity used to change the backdrop root.
  const bands = await card.evaluate(el => {
    for (const animation of el.getAnimations({ subtree: true })) {
      animation.pause()
      animation.currentTime = Number(animation.effect.getTiming().duration) * .99
    }
    const rect = el.getBoundingClientRect()
    return [...el.querySelectorAll('.scrape-actions button')].map(button => {
      const b = button.getBoundingClientRect()
      return { x: b.x - rect.x + 10, y: b.y - rect.y + 4, width: b.width - 20, height: 6 }
    })
  })
  const before = await card.screenshot({ path: info.outputPath('glass-revealing.png') })
  await card.evaluate(el => {
    for (const animation of el.getAnimations({ subtree: true })) animation.finish()
  })
  const after = await card.screenshot({ path: info.outputPath('glass-revealed.png') })
  const changes = await page.evaluate(async ({ before, after, bands }) => {
    const read = async encoded => {
      const bitmap = await createImageBitmap(await (await fetch('data:image/png;base64,' + encoded)).blob())
      const canvas = document.createElement('canvas')
      canvas.width = bitmap.width; canvas.height = bitmap.height
      const ctx = canvas.getContext('2d')
      ctx.drawImage(bitmap, 0, 0); bitmap.close()
      return ctx
    }
    const a = await read(before), b = await read(after)
    return bands.map(band => {
      const x = Math.round(band.x), y = Math.round(band.y), w = Math.floor(band.width), h = band.height
      const first = a.getImageData(x, y, w, h).data, last = b.getImageData(x, y, w, h).data
      let difference = 0
      for (let i = 0; i < first.length; i++) if (i % 4 !== 3) difference += Math.abs(first[i] - last[i])
      return difference / (w * h * 3)
    })
  }, { before: before.toString('base64'), after: after.toString('base64'), bands })
  for (const change of changes) expect(change).toBeLessThan(2)
  await expect(card.locator('.scrape-actions')).toHaveCSS('opacity', '1')
  await expect(card.locator('.scrape-actions button').first()).toHaveCSS('backdrop-filter', 'blur(12px)')
  await card.getByRole('button', { name: '识别作品', exact: true }).hover()
  await expect(card.getByRole('button', { name: '识别作品', exact: true })).toHaveCSS('transform', 'matrix(1, 0, 0, 1, 0, -3)')
  await page.mouse.move(0, 0)
  await expect(card.locator('.scrape-actions button').first()).toHaveCSS('opacity', '0')
})

test('poster actions remain keyboard accessible with reduced motion', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' })
  const card = await openPoster(page)
  await card.locator('.scrape-poster').focus()
  const identify = card.getByRole('button', { name: '识别作品', exact: true })
  await expect(identify).toHaveCSS('opacity', '1')
  await expect(identify).toHaveCSS('transition-property', 'none')
  await page.keyboard.press('Tab'); await page.keyboard.press('Tab'); await page.keyboard.press('Enter')
  await expect(page.getByRole('dialog', { name: '选择媒体匹配', exact: true })).toBeVisible()
})

test('touch posters keep glass actions visible and disabled actions dimmed', async ({ browser }, info) => {
  const context = await browser.newContext({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true })
  try {
    const page = await context.newPage()
    const card = await openPoster(page, true)
    const reset = card.getByRole('button', { name: '重置元数据', exact: true })
    await expect(reset).toBeDisabled()
    await expect(reset).toHaveCSS('opacity', '0.45')
    await expect(reset).toHaveCSS('backdrop-filter', 'blur(12px)')
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    await page.screenshot({ path: info.outputPath('glass-touch.png') })
  } finally { await context.close() }
})
