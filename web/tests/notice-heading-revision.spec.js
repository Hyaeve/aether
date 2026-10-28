import { test, expect } from '@playwright/test'
import { captureScreenshot } from './helpers/screenshot.js'

const taskName = '长标题通知 · 正在归档的家庭影音目录与媒体文件'.repeat(5)
const timestamp = '2026-10-26T10:15:00+08:00'

async function fixture(page) {
  await page.route('**/api/**', route => {
    const p = new URL(route.request().url()).pathname
    return route.fulfill({ json: p === '/api/auth/status' ? { initialized: true, authenticated: true }
      : p === '/api/state' ? {
        username: 'notice-heading', settings: {}, cache: {}, traffic: {}, logs: [],
        storages: [{ id: 'quark', type: 'quark', name: '夸克存储' }],
        tasks: [{ id: 'cas', kind: 'cas', storageId: 'quark', name: taskName, status: 'success', casGenerated: 10, lastRun: timestamp }],
        libraryNotices: [{ id: 'library', libraryName: '家庭影音媒体库的完整长标题'.repeat(4), name: '影片名称', event: 'playback.start', time: '2026-10-26T10:14:00+08:00' }],
        shareNotices: [{ id: 'share', name: '分享转存 · 目录名称'.repeat(6), message: '转存完成 · 目标目录的详细说明'.repeat(6), time: '2026-10-26T10:13:00+08:00', provider: 'quark' }],
      } : p === '/api/logs' ? [] : {} })
  })
}

for (const width of [1440, 390, 320]) for (const theme of ['light', 'dark']) {
  test(`notification timestamp stays right aligned beside the title ${width} ${theme}`, async ({ page }, info) => {
    await fixture(page)
    await page.setViewportSize({ width, height: 900 })
    await page.goto('/storage')
    await page.evaluate(theme => document.documentElement.dataset.theme = theme, theme)
    await page.getByRole('button', { name: '任务通知', exact: true }).click()
    const popup = page.getByRole('region', { name: '任务通知列表', exact: true })
    const rows = popup.locator('.notification-list > button')
    await expect(rows).toHaveCount(3)
    await expect(rows.first().locator('strong')).toHaveText(taskName)
    await expect(rows.first().locator('small time')).toHaveCount(0)
    for (const row of await rows.all()) {
      const time = row.locator('time')
      await expect(time).toHaveCSS('font-size', '13px')
      await expect(time).toHaveCSS('white-space', 'nowrap')
      await expect(time).toHaveText(/^\d{2}\/\d{2}\/\d{2} \d{2}:\d{2}$/)
      await expect(row).toHaveCSS('height', '84px')
      const layout = await row.evaluate(el => {
        const title = el.querySelector('strong'), time = el.querySelector('time'), details = el.querySelector('small')
        const heading = title.parentElement
        const a = title.getBoundingClientRect(), b = time.getBoundingClientRect(), c = details.getBoundingClientRect(), h = heading.getBoundingClientRect()
        return {
          sameParent: time.parentElement === heading,
          centers: Math.abs(a.top+a.height/2-b.top-b.height/2),
          gap: b.left-a.right, right: h.right-b.right, below: c.top >= Math.max(a.bottom, b.bottom),
          colors: [getComputedStyle(time).color, getComputedStyle(details).color],
          clippedTitle: title.scrollWidth > title.clientWidth,
          timeFits: time.scrollWidth <= time.clientWidth,
        }
      })
      expect(layout.sameParent).toBe(true)
      expect(layout.centers).toBeLessThan(1)
      expect(layout.gap).toBeGreaterThanOrEqual(7.9)
      expect(layout.right).toBeGreaterThanOrEqual(5.9)
      expect(layout.right).toBeLessThan(6.1)
      expect(layout.below).toBe(true)
      expect(layout.colors[0]).toBe(layout.colors[1])
      expect(layout.clippedTitle).toBe(true)
      expect(layout.timeFits).toBe(true)
    }
    expect(await popup.evaluate(el => el.scrollWidth <= el.clientWidth && el.getBoundingClientRect().left >= 0 && el.getBoundingClientRect().right <= innerWidth)).toBe(true)
    await captureScreenshot(page, { path: info.outputPath(`notification-heading-${width}-${theme}.png`) })
    await rows.first().click()
    await expect(page).toHaveURL(/\/logs\?/)
    const url = new URL(page.url())
    expect(url.searchParams.get('q')).toBe(taskName)
    expect(url.searchParams.get('time')).toBe(timestamp)
    await expect(popup).toHaveCount(0)
  })
}
