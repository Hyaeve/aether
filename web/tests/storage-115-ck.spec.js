import { test, expect } from '@playwright/test'

const ck = 'UID=100_A1; CID=example; SEID=example-long-cookie'
async function mockWorkspace(page, storages = []) {
  await page.route('**/api/auth/status', r => r.fulfill({ json: { initialized: true, authenticated: true } }))
  await page.route('**/api/state', r => r.fulfill({ json: {
    username: 'ck-test', storages, tasks: [], settings: {
      sessionDays: 15, cacheTTL: 30, cacheMaxItems: 10000, cacheMemoryMB: 128,
      snapshotInterval: 10, cacheEnabled: true, cachePersist: true
    }, cache: {}, traffic: {}, logs: []
  } }))
}
async function add115(page) {
  await page.goto('/storage')
  await page.getByRole('button', { name: '添加存储池', exact: true }).click()
  await page.getByRole('button', { name: '115 网盘', exact: true }).click()
}

for (const field of ['cookie', 'ck']) {
  test(`115 QR fills ${field} and device without OAuth or popup`, async ({ page }, testInfo) => {
    await mockWorkspace(page)
    let popups = 0, saved
    page.on('popup', () => popups++)
    await page.route('**/api/authorization/115/start', r => {
      expect(r.request().postDataJSON()).toEqual({ device: 'ios' })
      return r.fulfill({ json: { token: 'session-115', image: '/aether.svg', expiresIn: 300 } })
    })
    await page.route('**/api/authorization/115/poll', r => {
      expect(r.request().postDataJSON()).toEqual({ token: 'session-115', device: 'ios' })
      return r.fulfill({ json: { status: 'success', [field]: ck, device: 'qandroid' } })
    })
    await page.route('**/api/storages', r => {
      saved = r.request().postDataJSON()
      return r.fulfill({ json: { id: 'new-115' } })
    })
    await add115(page)
    await expect(page.getByLabel('Access Token')).toHaveCount(0)
    await expect(page.getByRole('button', { name: '设备类型', exact: true })).toHaveText('网页版')
    await page.getByRole('button', { name: '设备类型', exact: true }).click()
    await expect(page.getByRole('option')).toHaveText(['网页版', '安卓', 'iOS', '电视', '支付宝小程序', '微信小程序', 'Android（新版）'])
    await page.getByRole('option', { name: 'iOS', exact: true }).click()
    await page.getByRole('button', { name: '扫码获取 CK', exact: true }).click()
    await expect(page.getByAltText('115 授权二维码')).toBeVisible()
    await expect(page.getByLabel('CK', { exact: true })).toHaveValue(ck)
    await expect(page.getByRole('button', { name: '设备类型', exact: true })).toHaveText('Android（新版）')
    expect(popups).toBe(0)
    const input = page.getByLabel('CK', { exact: true })
    const before = await input.boundingBox()
    await page.getByRole('button', { name: '显示内容', exact: true }).click()
    await expect(input).toHaveAttribute('type', 'text')
    expect(await input.boundingBox()).toEqual(before)
    await page.getByRole('button', { name: '隐藏内容', exact: true }).click()
    expect(await input.boundingBox()).toEqual(before)
    const grid = await page.locator('.form-grid').boundingBox()
    expect(Math.abs((await input.locator('..').boundingBox()).width - grid.width)).toBeLessThan(2)
    await page.screenshot({ path: testInfo.outputPath('115-ck-desktop.png'), fullPage: true })
    await page.setViewportSize({ width: 390, height: 844 })
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    await page.screenshot({ path: testInfo.outputPath('115-ck-mobile.png'), fullPage: true })
    await page.getByLabel('存储池名称').fill('扫码存储')
    await page.getByRole('button', { name: '保存存储池', exact: true }).click()
    await expect(page.getByRole('dialog')).toHaveCount(0)
    expect(saved.config).toMatchObject({ cookie: ck, device: 'qandroid' })
    expect(saved.config).not.toHaveProperty('accessToken')
    expect(saved.config).not.toHaveProperty('refreshToken')
  })
}

test('115 retry and closed QR ignore late credentials', async ({ page }) => {
  await mockWorkspace(page)
  let attempts = 0, reply
  await page.route('**/api/authorization/115/start', r => {
    attempts++
    return r.fulfill({ json: { token: `session-${attempts}`, image: '/aether.svg', expiresIn: 300 } })
  })
  await page.route('**/api/authorization/115/poll', async r => {
    if (attempts === 1) return r.fulfill({ json: { status: 'expired' } })
    if (attempts === 2) return r.fulfill({ json: { status: 'success' } })
    await new Promise(resolve => { reply = resolve })
    await r.fulfill({ json: { status: 'success', cookie: 'stale-cookie', device: 'ios' } })
  })
  await add115(page)
  await page.getByLabel('CK', { exact: true }).fill('manual-cookie')
  await page.getByRole('button', { name: '扫码获取 CK', exact: true }).click()
  await expect(page.getByRole('alert')).toHaveText('二维码已失效，请重新获取')
  await page.getByRole('button', { name: '重新获取', exact: true }).click()
  await expect(page.getByRole('alert')).toHaveText('授权未返回有效 CK，请重新获取')
  await page.getByRole('button', { name: '重新获取', exact: true }).click()
  await expect.poll(() => !!reply).toBe(true)
  await page.getByRole('dialog', { name: '115 扫码获取 CK', exact: true }).getByRole('button', { name: '关闭', exact: true }).click()
  const response = page.waitForResponse('**/api/authorization/115/poll')
  reply()
  await response
  await expect(page.getByLabel('CK', { exact: true })).toHaveValue('manual-cookie')
  await expect(page.getByRole('button', { name: '设备类型', exact: true })).toHaveText('网页版')
})

test('115 editing reveals saved CK and drops old OAuth fields on save', async ({ page }) => {
  await mockWorkspace(page, [{ id: 'existing-115', type: '115', name: '115 资料', enabled: true,
    config: { cookie: '********', device: 'tv', accessToken: '********', refreshToken: '********' } }])
  let saved
  await page.route('**/api/storages/existing-115/secret', r => {
    const body = r.request().postDataJSON()
    expect(body.field).toBe('cookie')
    return r.fulfill({ json: body.metadataOnly ? { length: ck.length } : { value: ck } })
  })
  await page.route('**/api/storages/existing-115', r => {
    saved = r.request().postDataJSON()
    return r.fulfill({ json: {} })
  })
  await page.goto('/storage')
  await page.getByRole('heading', { name: '115 资料', exact: true }).click()
  const input = page.getByLabel('CK', { exact: true })
  await expect(input).toHaveValue('*'.repeat(ck.length))
  await expect(page.getByRole('button', { name: '设备类型', exact: true })).toHaveText('电视')
  const before = await input.boundingBox()
  await page.getByRole('button', { name: '显示内容', exact: true }).click()
  await expect(input).toHaveValue(ck)
  expect(await input.boundingBox()).toEqual(before)
  await page.getByRole('button', { name: '隐藏内容', exact: true }).click()
  await expect(input).toHaveValue('*'.repeat(ck.length))
  expect(await input.boundingBox()).toEqual(before)
  await page.getByRole('button', { name: '保存存储池', exact: true }).click()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  expect(saved.config.cookie).toBe(ck)
  expect(saved.config).not.toHaveProperty('accessToken')
  expect(saved.config).not.toHaveProperty('refreshToken')
})

test('cache text and account surface remain scoped and theme aware', async ({ page }, testInfo) => {
  await mockWorkspace(page)
  await page.goto('/tasks/cache/settings')
  for (const label of ['全局缓存时间', '缓存条目上限', '缓存内存上限', '持久化快照间隔']) {
    await expect(page.getByLabel(label, { exact: true })).toHaveCSS('font-size', '16px')
  }
  await page.goto('/settings/account')
  await expect(page.locator('.account-settings')).toHaveCSS('background-color', 'rgb(250, 251, 252)')
  await expect(page.getByRole('textbox',{name:'账号',exact:true})).toHaveCSS('background-color','rgba(0, 0, 0, 0)')
  await expect(page.locator('.account-settings .credential-input').first()).toHaveCSS('background-color','rgb(255, 255, 255)')
  expect(await page.locator('.account-settings').evaluate(el => el.clientWidth)).toBeLessThanOrEqual(380)
  expect(await page.locator('.account-settings').evaluate(el => {
    const fields = [...el.querySelectorAll('.credential-input, .number-control')].map(n => n.getBoundingClientRect())
    return fields.every(r => r.width === fields[0].width && r.height === fields[0].height)
  })).toBe(true)
  await page.screenshot({ path: testInfo.outputPath('account-light.png'), fullPage: true })
  await page.getByRole('button', { name: '主题：日光', exact: true }).click()
  await expect(page.locator('.account-settings')).toHaveCSS('background-color', 'rgb(27, 29, 35)')
  await page.screenshot({ path: testInfo.outputPath('account-dark.png'), fullPage: true })
})
