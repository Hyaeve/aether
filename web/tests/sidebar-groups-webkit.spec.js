import { test, expect } from '@playwright/test'
import { captureScreenshot } from './helpers/screenshot.js'

test.use({ browserName: 'webkit', hasTouch: true, deviceScaleFactor: 2, reducedMotion: 'reduce' })

for (const [width, height] of [[1440, 900], [375, 812], [844, 390]]) for (const theme of ['light', 'dark']) {
  test(`grouped sidebar remains accessible in WebKit ${width} ${theme}`, async ({ page }, info) => {
    await page.route('**/api/**', route => {
      const path = new URL(route.request().url()).pathname
      let json = {}
      if (path === '/api/auth/status') json = { initialized: true, authenticated: true }
      if (path === '/api/state') json = { username: 'sidebar-webkit', storages: [], tasks: [], settings: {}, traffic: {}, cache: {} }
      if (path === '/api/logs') json = []
      return route.fulfill({ json })
    })
    await page.setViewportSize({ width, height })
    await page.goto('/dashboard')
    await page.evaluate(value => document.documentElement.dataset.theme = value, theme)
    const toggle = page.getByRole('button', { name: '打开导航', exact: true })
    if (await toggle.isVisible()) await toggle.click()
    const nav = page.getByRole('navigation', { name: '主导航', exact: true })
    await expect(nav.getByRole('heading')).toHaveText(['以太视界', '以太工坊', '系统面板'])
    await expect(nav.getByRole('group', { name: '以太工坊', exact: true }).getByRole('link')).toHaveText(['传输中心', '任务管理', '虚拟链库', '以太链接', '辅助工具', '档案中心'])
    await expect(nav.getByRole('group', { name: '系统面板', exact: true }).getByRole('link')).toHaveText(['通知服务', '运行日志', '系统设置'])
    await expect(page.locator('.breadcrumbs strong')).toHaveText('信息总览')
    expect(await nav.locator('h2').first().evaluate(el => getComputedStyle(el, '::after').height)).toBe('1px')
    await expect(nav).toHaveCSS('scrollbar-width', 'none')
    await captureScreenshot(page, { path: info.outputPath(`sidebar-webkit-${width}-${theme}.png`) })
    await nav.evaluate(el => { el.scrollTop = el.scrollHeight })
    await expect(nav.getByRole('link', { name: '系统设置', exact: true })).toBeInViewport()
    await nav.getByRole('link', { name: '运行日志', exact: true }).click()
    await expect(page.locator('.breadcrumbs strong')).toHaveText('运行日志')
    if (width <= 760 || height <= 500) await expect(page.locator('.nav-overlay')).toHaveCount(0)
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  })
}
