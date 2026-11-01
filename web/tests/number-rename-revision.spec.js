import { test, expect } from '@playwright/test'
import { captureScreenshot } from './helpers/screenshot.js'

async function fixture(page, type = '115', entries = [{ id: '123', name: '原始影片.mkv', size: 128 }]) {
  let reads = 0
  const writes = []
  await page.addInitScript(() => localStorage.setItem('aether-files-view', 'list'))
  await page.route('**/api/**', route => {
    const url = new URL(route.request().url())
    let json
    if (url.pathname === '/api/auth/status') json = { initialized: true, authenticated: true }
    else if (url.pathname === '/api/state') json = { username: 'number-rename', storages: [{ id: 's', type, name: '测试存储', enabled: true, config: {} }], tasks: [], settings: {}, cache: { entries: 10000 }, traffic: {} }
    else if (url.pathname === '/api/files') { reads++; json = entries }
    else if (url.pathname === '/api/files/action') {
      const input = route.request().postDataJSON()
      writes.push(input)
      json = { processed: 1, renamed: { id: '456', name: input.name, pickCode: 'newpickcode', url: '/stream/new-reference' } }
    } else if (url.pathname === '/api/file-favorites' || url.pathname === '/api/file-visits') json = []
    else if (url.pathname === '/api/tmdb') json = {}
    else return route.fulfill({ status: 500, json: { error: `Unmocked: ${url.pathname}` } })
    return route.fulfill({ json })
  })
  return { writes, reads: () => reads }
}

for (const width of [1440, 390]) for (const theme of ['light', 'dark']) {
  test(`inline file and folder rename spans the name column ${width} ${theme}`, async ({ page }, info) => {
    await page.setViewportSize({ width, height: 900 })
    await fixture(page, '115', [{ id: '123', name: '原始影片.mkv', size: 128 }, { id: '124', name: '影视文件夹', isDir: true }])
    await page.goto('/files')
    await page.evaluate(theme => document.documentElement.dataset.theme = theme, theme)
    for (const name of ['原始影片.mkv', '影视文件夹']) {
      await page.getByRole('button', { name, exact: true }).click()
      await page.keyboard.press('F2')
      const input = page.getByRole('textbox', { name: '新名称', exact: true })
      await expect(input).toHaveValue(name)
      const sizes = await input.evaluate(el => {
        const cell = el.closest('td'), style = getComputedStyle(cell)
        const box = el.getBoundingClientRect(), td = cell.getBoundingClientRect()
        return {
          width: box.width, expected: td.width - parseFloat(style.paddingLeft) - parseFloat(style.paddingRight),
          right: box.right, next: cell.nextElementSibling.getBoundingClientRect().left
        }
      })
      expect(Math.abs(sizes.width - sizes.expected)).toBeLessThan(1)
      expect(sizes.right).toBeLessThanOrEqual(sizes.next)
      await captureScreenshot(page, { path: info.outputPath(`rename-${name}-${width}-${theme}.png`) })
      await page.keyboard.press('Escape')
      await expect(input).toHaveCount(0)
    }
  })
}

test('115 extension rename replaces the file identity in place without another list request', async ({ page }) => {
  const mock = await fixture(page)
  await page.goto('/files')
  const old = page.getByRole('button', { name: '原始影片.mkv', exact: true })
  await old.click()
  const reads = mock.reads()
  await page.keyboard.press('F2')
  const input = page.getByRole('textbox', { name: '新名称', exact: true })
  await input.fill('新影片.mp4')
  await input.press('Enter')
  const confirmation = page.getByRole('dialog', { name: '确认修改名称' })
  await expect(confirmation).toContainText('原文件移入115回收站')
  expect(mock.writes).toHaveLength(0)
  await confirmation.getByRole('button', { name: '确认修改', exact: true }).click()
  await expect(page.getByRole('button', { name: '新影片.mp4', exact: true })).toBeVisible()
  expect(mock.reads()).toBe(reads)
  await expect.poll(() => mock.writes.length).toBe(1)
  expect(mock.writes[0]).toMatchObject({ action: 'rename', ids: ['123'], name: '新影片.mp4' })
  await expect(page.getByRole('status', { name: '正在同步名称' })).toHaveCount(0)
  await page.keyboard.press('F2')
  await expect(input).toHaveValue('新影片.mp4')
  await input.fill('再次修改.mp4')
  await input.press('Enter')
  await expect.poll(() => mock.writes.length).toBe(2)
  expect(mock.writes[1].ids).toEqual(['456'])
})

for (const [width, theme] of [[1440, 'light'], [390, 'dark']]) test(`statistics reach their exact value within 1000ms and slow their final steps ${width} ${theme}`, async ({ page }, info) => {
  await page.setViewportSize({ width, height: 900 })
  await fixture(page)
  await page.clock.install({ time: new Date('2026-10-29T00:00:00Z') })
  await page.clock.pauseAt(new Date('2026-10-29T00:00:01Z'))
  await page.goto('/dashboard')
  await page.evaluate(value => document.documentElement.dataset.theme = value, theme)
  const number = page.locator('.metric-strip .animated-number').nth(2)
  await expect(number).toHaveText('0')
  await page.clock.runFor(200)
  const first = Number(await number.textContent())
  await page.clock.runFor(200)
  const second = Number(await number.textContent())
  await page.clock.runFor(200)
  const near = Number(await number.textContent())
  await page.clock.runFor(200)
  const late = Number(await number.textContent())
  expect(first).toBeGreaterThan(3000)
  expect(second).toBeGreaterThan(first)
  expect(near).toBeGreaterThan(9000)
  expect(late - near).toBeLessThan((second - first) / 3)
  await page.clock.runFor(200)
  await expect(number).toHaveText('10000')
  await page.clock.resume()
  await captureScreenshot(page, { path: info.outputPath(`statistics-1000ms-${width}-${theme}.png`) })
})

test('115 extension rename requires confirmation, blocks double submit and preserves the original on failure', async ({ page }, info) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await fixture(page)
  let calls = 0, finish
  await page.route('**/api/files/action', route => {
    calls++
    return new Promise(resolve => {
      finish = async () => { await route.fulfill({ status: 400, json: { error: '115未命中秒传，已保留原文件' } }); resolve() }
    })
  })
  await page.goto('/files')
  await page.evaluate(() => document.documentElement.dataset.theme = 'dark')
  await page.getByRole('button', { name: '原始影片.mkv', exact: true }).click()
  await page.keyboard.press('F2')
  const input = page.getByRole('textbox', { name: '新名称', exact: true })
  await input.fill('原始影片.mp4')
  await input.press('Enter')
  const dialog = page.getByRole('dialog', { name: '确认修改名称' })
  await expect(dialog).toContainText('秒传失败保留原文件')
  await captureScreenshot(page, { path: info.outputPath('115-extension-confirm-mobile-dark.png') })
  expect(calls).toBe(0)
  await dialog.getByRole('button', { name: '放弃修改', exact: true }).click()
  await expect(input).toHaveCount(0)
  expect(calls).toBe(0)
  await page.keyboard.press('F2')
  await input.fill('原始影片.mp4')
  await input.press('Enter')
  await dialog.getByRole('button', { name: '确认修改', exact: true }).click()
  await expect.poll(() => calls).toBe(1)
  await expect(dialog).toHaveCount(0)
  await expect(input).toHaveCount(0)
  await expect(page.getByRole('button', { name: '原始影片.mp4', exact: true })).toBeVisible()
  await expect(page.getByRole('status', { name: '正在同步名称' })).toBeVisible()
  await page.keyboard.press('F2')
  await page.keyboard.press('Enter')
  expect(calls).toBe(1)
  await finish()
  await expect(page.getByRole('status', { name: '正在同步名称' })).toHaveCount(0)
  await expect(page.locator('.toast-stack')).toContainText('115未命中秒传，已保留原文件')
  await expect(page.getByRole('button', { name: '原始影片.mkv', exact: true })).toBeVisible()
  expect(calls).toBe(1)
})
