import { test, expect } from '@playwright/test'
import { captureScreenshot } from './helpers/screenshot.js'

const fixtures = new WeakMap()
test.afterEach(async ({ page }, info) => {
  const fixture = fixtures.get(page)
  if (fixture) await info.attach('pagination-api', { body: JSON.stringify(fixture), contentType: 'application/json' })
})

async function fixture(page, theme) {
  const calls = [], unexpected = []
  fixtures.set(page, { calls, unexpected })
  let maximum = 3
  await page.addInitScript(value => localStorage.setItem('aether-theme', value), theme)
  await page.route('**/api/**', route => {
    const request = route.request(), url = new URL(request.url()), path = url.pathname
    let json
    if (path === '/api/auth/status') json = { initialized: true, authenticated: true }
    else if (path === '/api/state') json = { username: 'page-bounds', storages: [], tasks: [], settings: {}, cache: {}, traffic: {} }
    else if (path === '/api/traffic') json = {}
    else if (path === '/api/strm-replace') json = { tasks: [] }
    else if (path === '/api/115-rapid') json = { manual: {}, watches: [{ id: 'watch', name: '分页监控', mode: 'watch', source: '/source', enabled: true }] }
    else if (path === '/api/115-rapid/records' || path.startsWith('/api/archives/')) {
      const scoped = url.searchParams.has('watch'), pages = scoped ? 2 : maximum
      const current = Math.max(1, Math.min(pages, Number(url.searchParams.get('page')) || 1))
      calls.push({ path, scoped, requested: Number(url.searchParams.get('page')) || 1, page: current })
      json = { items: [], page: current, pages, total: pages === 1 ? 0 : pages * 30, pageSize: scoped ? 10 : 30 }
    } else if (path === '/api/strm-scrape/status') json = { running: false }
    else {
      unexpected.push(`${request.method()} ${path}`)
      return route.fulfill({ status: 500, json: { error: `Unmocked ${path}` } })
    }
    return route.fulfill({ json })
  })
  return { calls, unexpected, setMaximum: value => { maximum = value } }
}

for (const [width, theme] of [[1440, 'light'], [375, 'dark'], [844, 'dark']]) {
  for (const section of ['rapid', 'scrape', 'tasks']) test(`page jump clamps input and resets at the last page ${section} ${width} ${theme}`, async ({ page }, info) => {
    const f = await fixture(page, theme)
    await page.setViewportSize({ width, height: width === 844 ? 390 : 900 })
    await page.clock.install({ time: new Date('2026-10-30T00:00:00Z') })
    await page.clock.pauseAt(new Date('2026-10-30T00:01:00Z'))
    await page.goto(section === 'rapid' ? '/tools/115-rapid' : `/archives/${section}`)
    const form = page.getByRole('form', { name: section === 'rapid' ? '秒传记录分页' : '档案分页', exact: true })
    const input = form.getByRole('spinbutton', { name: '跳转页码', exact: true })
    await expect(form).toContainText('1 / 3')
    await page.clock.runFor(500)
    await expect(form.getByRole('button', { name: '下一页', exact: true })).toBeEnabled()
    const before = f.calls.length
    await input.fill('999')
    await expect(input).toHaveValue('3')
    expect(f.calls.length).toBe(before)
    await input.press('Enter')
    await expect(form).toContainText('3 / 3')
    await input.fill('999')
    await expect(input).toHaveValue('3')
    await form.getByRole('button', { name: '跳转到指定页', exact: true }).click()
    await expect(input).toHaveValue('3')
    await expect(form).toContainText('3 / 3')
    await input.fill('0')
    await expect(input).toHaveValue('1')
    await input.fill('2.8')
    await expect(input).toHaveValue('2')
    await input.fill('')
    await input.press('Tab')
    await expect(input).toHaveValue('1')
    await input.fill('2')
    await input.press('Enter')
    await expect(form).toContainText('2 / 3')
    f.setMaximum(1)
    if (section === 'rapid') await page.getByRole('button', { name: '刷新秒传记录', exact: true }).click()
    else await page.getByRole('button', { name: '刷新档案', exact: true }).click()
    await expect(form).toContainText('1 / 1')
    await expect(input).toHaveValue('1')
    await input.fill('77')
    await expect(input).toHaveValue('1')
    expect(f.calls.every(call => call.requested >= 1 && call.requested <= 3)).toBe(true)
    expect(f.unexpected).toEqual([])
    await page.clock.resume()
    await form.scrollIntoViewIfNeeded()
    await expect.poll(() => form.evaluate(el => {
      const box = el.getBoundingClientRect()
      return box.top >= 48 && box.bottom <= innerHeight
    })).toBe(true)
    await captureScreenshot(page, { path: info.outputPath(`page-bounds-${section}-${width}-${theme}.png`) })
  })
}

test('monitor records use their own page bounds', async ({ page }) => {
  const f = await fixture(page, 'light')
  await page.clock.install({ time: new Date('2026-10-30T00:00:00Z') })
  await page.clock.pauseAt(new Date('2026-10-30T00:01:00Z'))
  await page.goto('/tools/115-rapid')
  await page.clock.runFor(500)
  await page.getByRole('button', { name: '查看 分页监控 秒传记录', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '分页监控 · 秒传记录', exact: true })
  const input = dialog.getByRole('spinbutton', { name: '跳转页码', exact: true })
  await expect(dialog.locator('.rapid115-pagination')).toContainText('1 / 2')
  await expect(dialog.getByRole('button', { name: '跳转到指定页', exact: true })).toBeEnabled()
  await input.fill('999')
  await expect(input).toHaveValue('2')
  await input.press('Enter')
  await expect(dialog.locator('.rapid115-pagination')).toContainText('2 / 2')
  await input.fill('99')
  await expect(input).toHaveValue('2')
  expect(f.calls.filter(call => call.scoped).every(call => call.requested <= 2)).toBe(true)
  expect(f.unexpected).toEqual([])
  await page.clock.resume()
})
