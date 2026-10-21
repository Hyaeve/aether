import { test, expect } from '@playwright/test'

const folder = '归档目录与完整剧集资料'.repeat(5)
const file = '特别篇影片名称与版本说明'.repeat(5) + '.mkv'

async function setup(page, mode, theme) {
  await page.route('**/api/**', route => {
    const path = new URL(route.request().url()).pathname
    const json = path === '/api/auth/status' ? { initialized: true, authenticated: true }
      : path === '/api/state' ? { username: 'file-tooltip', storages: [{ id: 'local', type: 'local', name: '本地', enabled: true, config: {} }], tasks: [], settings: {}, traffic: {}, cache: {} }
        : path === '/api/files' ? [{ id: '/folder', name: folder, isDir: true }, { id: '/file', name: file, size: 100 }, { id: '/short', name: '短.txt', size: 5 }] : {}
    return route.fulfill({ json })
  })
  await page.goto('/files')
  await expect(page.locator('.file-view strong')).toHaveCount(3)
  if (mode === 'grid') await page.getByRole('button', { name: '当前列表视图，切换网格', exact: true }).click()
  await page.evaluate(theme => { document.documentElement.dataset.theme = theme }, theme)
  await page.mouse.move(10, 40)
  await page.clock.install()
  await page.clock.pauseAt(new Date())
}

for (const width of [1440, 390]) for (const mode of ['list', 'grid']) for (const theme of ['light', 'dark']) {
  test(`${width}px ${mode} ${theme} truncated names require 350ms stationary hover and reset on every movement`, async ({ page }, info) => {
    await page.setViewportSize({ width, height: 900 })
    await setup(page, mode, theme)
    const tooltip = page.getByRole('tooltip')
    for (const name of [folder, file]) {
      const label = page.locator('.file-view strong').filter({ hasText: name })
      expect(await label.evaluate(el => el.scrollWidth > el.clientWidth || el.scrollHeight > el.clientHeight)).toBe(true)
      const box = await label.boundingBox(), x = box.x + 8, y = box.y + box.height / 2
      await page.mouse.move(x, y)
      await page.clock.runFor(349); await expect(tooltip).toHaveCount(0)
      await page.mouse.move(x + 1, y)
      await page.clock.runFor(349); await expect(tooltip).toHaveCount(0)
      await page.clock.runFor(1); await expect(tooltip).toHaveText(name)
      await expect(tooltip).toHaveCSS('border-radius', '10px')
      await page.screenshot({ path: info.outputPath(`${mode}-${theme}-${name === folder ? 'folder' : 'file'}.png`) })
      await page.mouse.move(x + 2, y); await expect(tooltip).toHaveCount(0)
      await page.clock.runFor(349); await expect(tooltip).toHaveCount(0)
      await page.clock.runFor(1); await expect(tooltip).toHaveText(name)
      await page.keyboard.press('Escape'); await expect(tooltip).toHaveCount(0)
      await page.clock.runFor(500); await expect(tooltip).toHaveCount(0)
      await page.mouse.move(10, 40)
    }
    const short = page.locator('.file-view strong').filter({ hasText: '短.txt' })
    expect(await short.evaluate(el => el.scrollWidth <= el.clientWidth && el.scrollHeight <= el.clientHeight)).toBe(true)
    await short.hover(); await page.clock.runFor(500); await expect(tooltip).toHaveCount(0)
    const label = page.locator('.file-view strong').filter({ hasText: file })
    await label.hover(); await page.clock.runFor(200)
    await page.mouse.move(10, 40); await page.clock.runFor(500); await expect(tooltip).toHaveCount(0)
    await label.hover(); await page.clock.runFor(350); await expect(tooltip).toHaveText(file)
    await page.locator('.file-view').dispatchEvent('scroll'); await expect(tooltip).toHaveCount(0)
    await page.clock.runFor(500); await expect(tooltip).toHaveCount(0)
  })
}
