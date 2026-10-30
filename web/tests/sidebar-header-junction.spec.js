import { test, expect } from '@playwright/test'
import { captureScreenshot } from './helpers/screenshot.js'

for (const size of [{ width: 1440, height: 900, dpr: 1 }, { width: 761, height: 900, dpr: 2 }, { width: 1024, height: 900, dpr: 2, touch: true, safe: 47 }]) {
  test.describe(`sidebar header junction ${size.width}`, () => {
    test.use({ viewport: size, deviceScaleFactor: size.dpr, hasTouch: !!size.touch })
    for (const theme of ['light', 'dark']) test(`rounded corner exposes the header surface in ${theme}`, async ({ page }, info) => {
      await page.addInitScript(value => localStorage.setItem('aether-theme', value), theme)
      await page.route('**/api/**', route => {
        const path = new URL(route.request().url()).pathname
        let json
        if (path === '/api/auth/status') json = { initialized: true, authenticated: true }
        else if (path === '/api/state') json = { username: 'header-junction', storages: [], tasks: [], settings: {}, cache: {}, traffic: {} }
        else if (path === '/api/transfers' || path === '/api/backup-rules') json = []
        else if (path === '/api/traffic') json = { uploadRate: 0, downloadRate: 0 }
        else if (path === '/api/strm-replace') json = { jobs: [] }
        else return route.fulfill({ status: 500, json: { error: `Unmocked API: ${path}` } })
        return route.fulfill({ json })
      })
      await page.goto('/transfer')
      await expect(page.locator('.transfer-content')).toBeVisible()
      await expect(page.locator('html')).toHaveAttribute('data-theme', theme)
      if (size.safe) await page.evaluate(value => document.documentElement.style.setProperty('--aether-safe-top', `${value}px`), size.safe)
      const geometry = await page.locator('.sidebar').evaluate(el => {
        const sidebar = el.getBoundingClientRect(), header = document.querySelector('.topbar').getBoundingClientRect()
        return {
          right: sidebar.right, bottom: sidebar.bottom, headerLeft: header.left, headerTop: header.top,
          radius: getComputedStyle(el).borderTopRightRadius,
          headerColor: getComputedStyle(document.querySelector('.topbar')).backgroundColor,
          pageColor: getComputedStyle(document.querySelector('.app-shell')).backgroundColor
        }
      })
      expect(geometry.headerLeft).toBe(geometry.right)
      expect(geometry.headerTop).toBe(size.safe || 0)
      expect(geometry.radius).toBe('22px')
      const screenshot = await captureScreenshot(page, { path: info.outputPath(`junction-${size.width}-${theme}.png`) })
      const pixels = await page.evaluate(async ({ image, geometry }) => {
        const img = new Image()
        img.src = `data:image/png;base64,${image}`
        await img.decode()
        const canvas = document.createElement('canvas')
        canvas.width = img.naturalWidth; canvas.height = img.naturalHeight
        const ctx = canvas.getContext('2d')
        ctx.drawImage(img, 0, 0)
        const scale = img.naturalWidth / innerWidth
        const pixel = (x, y) => [...ctx.getImageData(Math.floor((x + .5) * scale), Math.floor((y + .5) * scale), 1, 1).data]
        const corners = [pixel(geometry.right - 2, 1), pixel(geometry.right - 4, 3)]
        const bottom = pixel(geometry.right - 2, geometry.bottom - 2)
        const inside = pixel(geometry.right - 2, 80)
        const color = value => {
          ctx.clearRect(0, 0, 1, 1); ctx.fillStyle = value; ctx.fillRect(0, 0, 1, 1)
          return [...ctx.getImageData(0, 0, 1, 1).data]
        }
        return { corners, bottom, inside, header: color(geometry.headerColor), page: color(geometry.pageColor) }
      }, { image: screenshot.toString('base64'), geometry })
      for (const corner of pixels.corners) expect(corner).toEqual(pixels.header)
      expect(pixels.bottom).toEqual(pixels.page)
      expect(pixels.inside).not.toEqual(pixels.header)
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    })
  })
}
