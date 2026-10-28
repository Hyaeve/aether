import { test, expect } from '@playwright/test'
import { captureScreenshot } from './helpers/screenshot.js'

async function mockLibrary(page, { empty = false, directory = '' } = {}) {
  const saves = [], settings = { storageIds: [], directory, invalidDays: 15, retentionHours: 12, mediaExtensions: 'mkv', metadataExtensions: 'nfo' }
  await page.route('**/api/**', route => {
    const request = route.request(), path = new URL(request.url()).pathname
    let json = {}
    if (path === '/api/auth/status') json = { initialized: true, authenticated: true }
    if (path === '/api/state') json = {
      username: 'library-defaults', storages: empty ? [] : [
        { id: 'q', type: 'quark', name: '夸克首选', enabled: true, config: {} },
        { id: 'q2', type: 'quark', name: '夸克备用', enabled: true, config: {} },
      ], tasks: [], links: [], settings: {}, cache: {}, traffic: {},
    }
    if (path === '/api/virtual-library/quark' && request.method() === 'GET') json = {
      settings, cards: [{ id: 'draft', url: 'https://pan.quark.cn/s/example', name: '机器人草稿', directory: '', directoryName: '分享目录', enabled: false, result: { message: '等待设置生成位置并启用链接' } }], stats: {},
    }
    if (path === '/api/virtual-library/quark/settings') {
      saves.push(request.postDataJSON())
      Object.assign(settings, request.postDataJSON())
      json = settings
    }
    if (path === '/api/virtual-library/quark/draft') {
      saves.push(request.postDataJSON())
      json = { id: 'draft' }
    }
    if (path === '/api/local-directories') json = { path: '/media/share-strm', items: [] }
    return route.fulfill({ json })
  })
  return { saves }
}

for (const width of [1440, 390]) for (const theme of ['light', 'dark']) {
  test(`Quark required first pool and default output ${width} ${theme}`, async ({ page }, info) => {
    const { saves } = await mockLibrary(page)
    await page.setViewportSize({ width, height: 950 })
    await page.goto('/virtual/quark/settings')
    await page.evaluate(value => { document.documentElement.dataset.theme = value }, theme)
    const primary = page.getByRole('button', { name: '绑定 夸克首选', exact: true })
    await expect(primary).toBeDisabled()
    await expect(primary).toHaveAttribute('aria-pressed', 'true')
    const secondary = page.getByRole('button', { name: '绑定 夸克备用', exact: true })
    await secondary.click()
    await expect(secondary).toHaveAttribute('aria-pressed', 'true')
    await secondary.click()
    await expect(secondary).toHaveAttribute('aria-pressed', 'false')
    const output = page.getByRole('textbox', { name: '默认生成位置', exact: true })
    await expect(output).toHaveValue('')
    await expect(output).toHaveCSS('height', '40px')
    await page.getByRole('button', { name: '选择默认生成位置', exact: true }).click()
    const picker = page.getByRole('dialog', { name: '选择容器目录', exact: true })
    await picker.getByRole('button', { name: '选择当前目录', exact: true }).click()
    await expect(output).toHaveValue('/media/share-strm')
    await page.getByRole('button', { name: '保存设置', exact: true }).click()
    await expect.poll(() => saves.length).toBe(1)
    expect(saves[0]).toMatchObject({ storageIds: ['q'], directory: '/media/share-strm' })
    expect(await page.locator('.quark-library-settings').evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
    await captureScreenshot(page, { path: info.outputPath(`defaults-${width}-${theme}.png`) })
  })
}

test('no Quark storage shows an explicit empty state', async ({ page }) => {
  await mockLibrary(page, { empty: true })
  await page.goto('/virtual/quark/settings')
  await expect(page.getByText('暂无夸克存储', { exact: true })).toBeVisible()
  await expect(page.locator('.quark-account')).toHaveCount(0)
})

test('draft requires an output before it can be enabled and retains the original card', async ({ page }, info) => {
  const { saves } = await mockLibrary(page)
  await page.goto('/virtual/quark')
  const card = page.getByRole('article', { name: '机器人草稿', exact: true })
  await expect(card.getByText('未设置生成位置', { exact: true })).toBeVisible()
  await expect(card.getByText('未启用', { exact: true })).toBeVisible()
  await expect(card.getByRole('button', { name: '立即生成', exact: true })).toBeDisabled()
  await card.getByRole('button', { name: '编辑 机器人草稿', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '编辑夸克链接', exact: true })
  const enabled = dialog.getByRole('switch', { name: '启用链接', exact: true })
  await expect(enabled).toBeDisabled()
  await dialog.getByRole('textbox', { name: '生成位置', exact: true }).fill('/media/strm')
  await expect(enabled).toBeEnabled()
  await enabled.check()
  await captureScreenshot(page, { path: info.outputPath('draft-enabled.png') })
  await dialog.getByRole('button', { name: '保存链接', exact: true }).click()
  await page.getByRole('dialog', { name: '同步夸克分享', exact: true }).getByRole('button', { name: '确认同步', exact: true }).click()
  await expect.poll(() => saves.length).toBe(1)
  expect(saves[0]).toMatchObject({ id: 'draft', directory: '/media/strm', enabled: true })
})
