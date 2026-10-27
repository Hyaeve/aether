import { test, expect } from '@playwright/test'
import { captureScreenshot } from './helpers/screenshot.js'

async function fixture(page, running = false, empty = false) {
  const cards = empty ? [] : [
    { id: 'ok', name: '已完成的分享', enabled: true, result: { status: 'completed', lastRun: '2026-10-26T01:00:00Z' } },
    { id: 'bad', name: '已失效的分享', enabled: true, result: { status: 'invalid', lastRun: '2026-10-26T01:00:00Z', invalidSince: '2026-10-26T01:00:00Z' } },
    { id: 'draft', name: '未执行草稿', enabled: false, result: {} },
    ...(running ? [{ id: 'run', name: '正在更新的长分享目录名称'.repeat(8), enabled: true, result: { status: 'running', message: '正在读取分享' } }] : []),
  ]
  await page.route('**/api/**', route => {
    const p = new URL(route.request().url()).pathname
    const json = p === '/api/auth/status' ? { initialized: true, authenticated: true }
      : p === '/api/state' ? { storages: [], tasks: [], settings: {}, traffic: {}, cache: {} }
      : p === '/api/virtual-library/quark' ? { cards, settings: { storageIds: [] }, stats: { strm: empty ? 0 : 24, metadata: empty ? 0 : 4 } } : {}
    return route.fulfill({ json })
  })
}

for (const width of [1440, 768, 375]) for (const theme of ['light', 'dark']) {
  test(`Quark overview matches CAS layout and settings entry ${width} ${theme}`, async ({ page }, info) => {
    await fixture(page)
    await page.setViewportSize({ width, height: 950 })
    await page.goto('/virtual/quark')
    await page.evaluate(theme => document.documentElement.dataset.theme = theme, theme)
    const overview = page.getByRole('region', { name: '夸克链库统计', exact: true })
    const entry = overview.getByRole('button', { name: '夸克链库设置', exact: true })
    await expect(entry).toHaveCSS('height', '76px')
    await expect(entry.locator('.lucide-chevron-left-icon')).toHaveCount(1)
    await expect(overview.getByRole('img', { name: '成功率 50%', exact: true })).toBeVisible()
    await expect(overview.locator('.quark-overview-metrics dd')).toHaveText(['1', '1'])
    await expect(overview.locator('.quark-overview-totals')).toContainText('STRM 24')
    await expect(overview.locator('.quark-overview-totals')).toContainText('元数据 4')
    await expect(overview.getByRole('progressbar', { name: '暂无执行中的夸克链库任务', exact: true })).toHaveAttribute('value', '0')
    expect(await overview.evaluate(el => el.scrollWidth <= el.clientWidth && el.getBoundingClientRect().right <= innerWidth)).toBe(true)
    const metrics = await overview.locator('.quark-overview-metrics > div').evaluateAll(els => els.map(el => {
      const r = el.getBoundingClientRect(); return { x: r.x, top: r.top, bottom: r.bottom }
    }))
    expect(metrics[1].top - metrics[0].bottom).toBeGreaterThanOrEqual(20)
    await captureScreenshot(page, { path: info.outputPath(`overview-${width}-${theme}.png`) })
    await entry.focus()
    await page.keyboard.press('Enter')
    await expect(page).toHaveURL(/\/virtual\/quark\/settings$/)
    await page.getByRole('button', { name: '返回页面', exact: true }).click()
    await expect(page).toHaveURL(/\/virtual\/quark$/)
  })
}

test('Quark running and empty overview never invents a percentage progress', async ({ page }, info) => {
  await fixture(page, true)
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.setViewportSize({ width: 375, height: 812 })
  await page.goto('/virtual/quark')
  const overview = page.getByRole('region', { name: '夸克链库统计', exact: true })
  const progress = overview.getByRole('progressbar')
  await expect(progress).toHaveAttribute('aria-valuetext', '正在读取分享')
  await expect(progress).not.toHaveAttribute('aria-valuenow')
  await expect(overview.locator('progress:indeterminate')).toHaveCount(0)
  expect(await overview.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
  await captureScreenshot(page, { path: info.outputPath('overview-running-mobile.png') })
  await fixture(page, false, true)
  await page.reload()
  await expect(overview.getByRole('img', { name: '成功率暂无记录', exact: true })).toBeVisible()
  await expect(overview.locator('.quark-overview-metrics dd')).toHaveText(['0', '0'])
})

for (const width of [1440, 375]) for (const theme of ['light', 'dark']) {
  test(`CAS metric rows retain wider spacing ${width} ${theme}`, async ({ page }, info) => {
    await fixture(page)
    await page.setViewportSize({ width, height: 950 })
    await page.goto('/tasks/cas')
    await page.evaluate(theme => document.documentElement.dataset.theme = theme, theme)
    const overview = page.getByRole('region', { name: 'CAS 生成统计', exact: true })
    const rows = await overview.locator('.cas-metrics > div').evaluateAll(els => els.map(el => {
      const r = el.getBoundingClientRect(); return { top: r.top, bottom: r.bottom }
    }))
    expect(rows[1].top-rows[0].bottom).toBeGreaterThanOrEqual(20)
    await expect(overview.getByRole('button', { name: '打开存储绑定池', exact: true })).toHaveCSS('height', '76px')
    expect(await overview.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
    await captureScreenshot(page, { path: info.outputPath(`cas-overview-${width}-${theme}.png`) })
  })
}
