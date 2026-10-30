import { test, expect } from '@playwright/test'
import { captureScreenshot } from './helpers/screenshot.js'

const parentName = '需要从前端省略的很长的上级目录'.repeat(8)
const directory = '/parent/series'

async function openWorkbench(page, type = 'local') {
  const unexpected = []
  await page.route('**/api/**', route => {
    const url = new URL(route.request().url())
    let json
    if (url.pathname === '/api/auth/status') json = { initialized: true, authenticated: true }
    else if (url.pathname === '/api/state') json = {
      username: 'rename-heading', storages: [{ id: 'pool', name: '影音资料', type, enabled: true, config: {} }],
      tasks: [], settings: {}, cache: {}, traffic: {}, logs: []
    }
    else if (url.pathname === '/api/traffic') json = { uploadRate: 0, downloadRate: 0 }
    else if (url.pathname === '/api/strm-replace') json = { jobs: [] }
    else if (url.pathname === '/api/files/rename-rules' || url.pathname === '/api/files/visits') json = []
    else if (url.pathname === '/api/files') {
      const parent = url.searchParams.get('path')
      json = parent === directory ? [{ id: 'episode', name: '第01集.mkv', size: 1024 }]
        : parent === '/parent' ? [{ id: directory, name: '剧集', isDir: true }]
          : [{ id: '/parent', name: parentName, isDir: true }]
    } else {
      unexpected.push(url.pathname)
      return route.fulfill({ status: 500, json: { error: `Unmocked API: ${url.pathname}` } })
    }
    return route.fulfill({ json })
  })
  await page.goto('/files?storage=pool')
  await page.getByRole('button', { name: parentName, exact: true }).dblclick()
  await page.getByRole('button', { name: '剧集', exact: true }).dblclick()
  await page.getByRole('button', { name: '工具', exact: true }).click()
  await page.getByRole('menuitem', { name: '命名工作台', exact: true }).click()
  await expect(page.locator('.rename-preview-row')).toContainText('第01集.mkv')
  return unexpected
}

async function expectMatchingControls(page, width) {
  const actions = page.locator('.rename-heading-actions')
  const current = page.locator('.rename-current-directory')
  const size = width > 1100 ? '14px' : '12px'
  for (const text of [current, current.locator('strong'), current.locator('.visit-deepest'), ...await actions.locator('.btn').all()]) {
    await expect(text).toHaveCSS('font-size', size)
  }
  const icon = current.locator('.provider-icon > svg, .provider-icon > img, .provider-icon > .dav-symbol')
  const reference = await actions.locator('.btn > svg').first().boundingBox()
  const actual = await icon.boundingBox()
  expect(actual.width).toBe(reference.width)
  expect(actual.height).toBe(reference.height)
  expect(actual.width).toBe(18)
  await expect(current.locator('.visit-path')).toHaveAttribute('aria-label', `${parentName}/剧集`)
  await expect(current.locator('.visit-path')).toContainText('/../')
  await expect(current.locator('.visit-deepest')).toHaveText('剧集')
  expect(await current.locator('.visit-deepest').evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
  expect(await current.locator('.visit-path').evaluate(el => el.scrollHeight <= el.clientHeight)).toBe(true)
  const bounds = await current.boundingBox()
  const controls = await actions.boundingBox()
  if (Math.abs(bounds.y - controls.y) < 20) expect(bounds.x + bounds.width).toBeLessThanOrEqual(controls.x)
  expect(bounds.width).toBeGreaterThan(0)
  expect(bounds.x + bounds.width).toBeLessThanOrEqual(width)
  expect(controls.x + controls.width).toBeLessThanOrEqual(width)
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
}

for (const viewport of [
  { width: 1440, height: 1000 }, { width: 1024, height: 900 },
  { width: 375, height: 812 }, { width: 844, height: 390 }
]) {
  test.describe(`rename heading ${viewport.width}`, () => {
    test.use({ viewport })
    for (const theme of ['light', 'dark']) test(`directory matches action sizing in ${theme}`, async ({ page }, info) => {
      await page.addInitScript(value => localStorage.setItem('aether-theme', value), theme)
      const unexpected = await openWorkbench(page)
      await expect(page.locator('html')).toHaveAttribute('data-theme', theme)
      await expectMatchingControls(page, viewport.width)
      await captureScreenshot(page, { path: info.outputPath(`rename-heading-${viewport.width}-${theme}.png`) })
      await page.getByRole('button', { name: '返回文件管理', exact: true }).click()
      await expect(page.getByRole('button', { name: '第01集.mkv', exact: true })).toBeVisible()
      expect(unexpected).toEqual([])
    })
  })
}

test('rename directory cloud and WebDAV icons match header actions', async ({ page }) => {
  for (const type of ['115', 'mobile', 'tianyi', 'quark', 'openlist', 'webdav']) {
    await page.unroute('**/api/**')
    const unexpected = await openWorkbench(page, type)
    await expectMatchingControls(page, 1440)
    const logo = page.locator('.rename-current-directory img')
    if (await logo.count()) expect(await logo.evaluate(el => el.complete && el.naturalWidth > 0)).toBe(true)
    expect(unexpected).toEqual([])
  }
})
