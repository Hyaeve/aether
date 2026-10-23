import { test, expect } from '@playwright/test'
import { captureScreenshot } from './helpers/screenshot.js'

for (const width of [1440, 390]) for (const theme of ['light', 'dark']) for (const mode of ['list', 'grid']) {
  test(`rename stays in place without reloading ${mode} ${width} ${theme}`, async ({ page }, info) => {
    await page.setViewportSize({ width, height: 900 })
    await page.addInitScript(mode => localStorage.setItem('aether-files-view', mode), mode)
    let reads = 0, finish
    const entries = ['Alpha', 'Middle', 'Zulu'].map(name => ({ id: `/${name}`, name, isDir: true }))
    await page.route('**/api/**', route => {
      const url = new URL(route.request().url())
      let json = []
      if (url.pathname === '/api/auth/status') json = { initialized: true, authenticated: true }
      if (url.pathname === '/api/state') json = { username: 'rename-place', storages: [{ id: 'local', type: 'local', name: '本地', enabled: true, config: {} }], tasks: [], settings: {}, cache: {}, traffic: {} }
      if (url.pathname === '/api/files') { reads++; json = entries }
      if (url.pathname === '/api/files/action') {
        const data = route.request().postDataJSON()
        expect(data).toMatchObject({ action: 'rename', ids: ['/Middle'], name: 'A-new' })
        return new Promise(resolve => { finish = async () => { entries[1] = { id: '/A-new', name: 'A-new', isDir: true }; await route.fulfill({ json: { processed: 1, renamed: { id: '/A-new', name: 'A-new' } } }); resolve() } })
      }
      return route.fulfill({ json })
    })
    await page.goto('/files')
    await page.evaluate(theme => document.documentElement.dataset.theme = theme, theme)
    const rows = page.locator(mode === 'list' ? '.file-row' : '.file-grid-item')
    await expect(rows).toHaveCount(3)
    const nameSelector = mode === 'list' ? '.file-name strong' : '.file-grid-name strong'
    const originalReads = reads
    await rows.nth(1).click()
    const before = await rows.nth(1).boundingBox()
    await page.keyboard.press('F2')
    await page.getByRole('textbox', { name: '新名称', exact: true }).fill('A-new')
    await page.getByRole('textbox', { name: '新名称', exact: true }).press('Enter')
    await expect.poll(() => !!finish).toBe(true)
    await expect(rows).toHaveCount(3)
    await expect(page.getByText('正在读取目录…', { exact: true })).toHaveCount(0)
    await finish()
    await expect(rows.locator(nameSelector)).toHaveText(['Alpha', 'A-new', 'Zulu'])
    expect(reads).toBe(originalReads)
    const after = await rows.nth(1).boundingBox()
    expect(after.x).toBe(before.x)
    expect(after.y).toBe(before.y)
    await captureScreenshot(page, { path: info.outputPath(`rename-${mode}-${width}-${theme}.png`) })
    await rows.nth(1).dblclick()
    await expect.poll(() => reads).toBe(originalReads + 1)
    expect(new URL(page.url()).pathname).toBe('/files')
  })
}

test('renaming a virtualized scrolled row preserves scroll and viewport position', async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('aether-files-view', 'list'))
  const entries = Array.from({ length: 180 }, (_, i) => ({ id: `/File-${String(i).padStart(3, '0')}.txt`, name: `File-${String(i).padStart(3, '0')}.txt`, size: 12 }))
  let reads = 0
  await page.route('**/api/**', route => {
    const url = new URL(route.request().url())
    let json = []
    if (url.pathname === '/api/auth/status') json = { initialized: true, authenticated: true }
    if (url.pathname === '/api/state') json = { username: 'rename-scroll', storages: [{ id: 'local', type: 'local', name: '本地', enabled: true, config: {} }], tasks: [], settings: {}, cache: {}, traffic: {} }
    if (url.pathname === '/api/files') { reads++; json = entries }
    if (url.pathname === '/api/files/action') {
      const input = route.request().postDataJSON()
      json = { processed: 1, renamed: { id: `/${input.name}`, name: input.name, url: '/stream/new-signed-reference' } }
    }
    return route.fulfill({ json })
  })
  await page.goto('/files')
  const view = page.locator('.file-view')
  await expect(page.locator('.file-row').first()).toBeVisible()
  await view.evaluate(el => { el.scrollTop = 2100 })
  const target = page.getByRole('button', { name: 'File-044.txt', exact: true })
  await expect(target).toBeInViewport()
  const y = (await target.boundingBox()).y
  const scroll = await view.evaluate(el => el.scrollTop)
  const before = reads
  await target.click()
  await page.keyboard.press('F2')
  await page.getByRole('textbox', { name: '新名称', exact: true }).fill('A-renamed.txt')
  await page.getByRole('textbox', { name: '新名称', exact: true }).press('Enter')
  const renamed = page.getByRole('button', { name: 'A-renamed.txt', exact: true })
  await expect(renamed).toBeVisible()
  expect((await renamed.boundingBox()).y).toBe(y)
  expect(await view.evaluate(el => el.scrollTop)).toBe(scroll)
  expect(reads).toBe(before)
})
