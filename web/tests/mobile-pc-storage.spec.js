import { test, expect, webkit } from '@playwright/test'
import { captureScreenshot } from './helpers/screenshot'

async function checkClientSelection(page, info, width, theme) {
    await page.setViewportSize({ width, height: width === 844 ? 390 : width === 375 ? 812 : width === 390 ? 844 : 1000 })
    let saved, storage = { id: 'mobile', name: '移动大文件', type: 'mobile', enabled: true, cacheTTL: 0, config: { mode: 'native', root: '/', userDomainId: 'domain-id', authorization: '********' } }
    await page.route('**/api/**', async route => {
      const request = route.request(), pathname = new URL(request.url()).pathname
      if (pathname === '/api/storages/mobile' && request.method() === 'PUT') {
        saved = request.postDataJSON()
        storage = structuredClone(saved)
        return route.fulfill({ json: { ok: true } })
      }
      if (pathname === '/api/storages/mobile/secret') return route.fulfill({ json: { length: 32 } })
      return route.fulfill({ json: pathname === '/api/auth/status' ? { initialized: true, authenticated: true } : pathname === '/api/state' ? { username: 'mobile-pc-ui', storages: [storage], tasks: [], settings: {}, cache: {}, traffic: {} } : {} })
    })
    await page.goto('/storage')
    await page.evaluate(value => document.documentElement.dataset.theme = value, theme)
    await page.locator('.storage-card-name').click()
    const dialog = page.getByRole('dialog')
    const client = dialog.getByRole('button', { name: '移动客户端类型', exact: true })
    await expect(client).toContainText('PC')
    expect(await client.evaluate(el => el.getBoundingClientRect().height)).toBe(40)
    expect(await dialog.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
    await client.evaluate(el => el.scrollIntoView({ block: 'center', inline: 'nearest' }))
    await expect.poll(() => client.evaluate(el => {
      const field = el.getBoundingClientRect(), panel = el.closest('.modal')
      return field.top >= panel.querySelector('.modal-header').getBoundingClientRect().bottom && field.bottom <= panel.querySelector('.modal-footer').getBoundingClientRect().top
    })).toBe(true)
    await captureScreenshot(page, { path: info.outputPath(`mobile-pc-${width}-${theme}.png`) })
    await dialog.getByRole('button', { name: '保存存储池', exact: true }).click()
    await expect.poll(() => saved?.config.clientType).toBe('pc')
    expect(saved.config.authorization).toBe('********')
    expect(saved.config.userDomainId).toBe('domain-id')
    await page.locator('.storage-card-name').click()
    await expect(client).toContainText('PC')
    await client.focus()
    await client.press('ArrowDown')
    await page.getByRole('listbox', { name: '移动客户端类型选项' }).press('ArrowUp')
    await page.getByRole('listbox', { name: '移动客户端类型选项' }).press('Enter')
    await dialog.getByRole('button', { name: '保存存储池', exact: true }).click()
    await expect.poll(() => saved?.config.clientType).toBe('web')
    await page.locator('.storage-card-name').click()
    await expect(client).toContainText('Web')
    await dialog.getByRole('button', { name: '保存存储池', exact: true }).click()
    await expect.poll(() => saved?.config.clientType).toBe('web')
}

for (const [width, theme] of [[1440, 'light'], [1440, 'dark'], [390, 'light'], [390, 'dark']]) {
  test(`mobile client type persists without changing credentials at ${width}px ${theme}`, async ({ page }, info) => {
    await checkClientSelection(page, info, width, theme)
  })
}

for (const [width, theme] of [[375, 'light'], [844, 'dark']]) {
  test(`mobile client selection WebKit ${width}px ${theme}`, async ({ baseURL }, info) => {
    const browser = await webkit.launch()
    const context = await browser.newContext({ baseURL, viewport: { width, height: width === 844 ? 390 : 812 }, serviceWorkers: 'block', isMobile: true, hasTouch: true })
    try { await checkClientSelection(await context.newPage(), info, width, theme) }
    finally { await context.close(); await browser.close() }
  })
}

test('new mobile storage defaults to PC and other providers do not expose its client setting', async ({ page }) => {
  let saved, storages = []
  await page.route('**/api/**', route => {
    const request = route.request(), pathname = new URL(request.url()).pathname
    if (pathname === '/api/storages' && request.method() === 'POST') {
      saved = request.postDataJSON()
      storages = [{ ...saved, id: 'created' }]
      return route.fulfill({ json: { id: 'created' } })
    }
    return route.fulfill({ json: pathname === '/api/auth/status' ? { initialized: true, authenticated: true } : pathname === '/api/state' ? { username: 'mobile-pc-create', storages, tasks: [], settings: {}, cache: {}, traffic: {} } : {} })
  })
  await page.goto('/storage')
  await page.getByRole('button', { name: '添加存储池', exact: true }).click()
  await page.locator('.driver-option[data-provider="mobile"]').click()
  const dialog = page.getByRole('dialog')
  const client = dialog.getByRole('button', { name: '移动客户端类型', exact: true })
  await expect(client).toContainText('PC')
  await page.getByLabel('存储池名称').fill('新移动')
  await page.getByLabel('Authorization', { exact: true }).fill('cGM6YWNjb3VudDp0ZXN0LXRva2Vu')
  await dialog.getByRole('button', { name: '保存存储池', exact: true }).click()
  await expect.poll(() => saved?.config.clientType).toBe('pc')
  await page.getByRole('button', { name: '添加存储池', exact: true }).click()
  await page.locator('.driver-option[data-provider="quark"]').click()
  await expect(client).toHaveCount(0)
})
