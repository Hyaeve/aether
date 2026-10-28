import { test, expect } from '@playwright/test'

const shareURL = type => ({ '115': 'https://115cdn.com/s/test', quark: 'https://pan.quark.cn/s/test', mobile: 'https://yun.139.com/shareweb/#/w/i/test' }[type])
const storages = ['115', 'mobile', 'quark'].flatMap(type => [1, 2].map(i => ({ id: `${type}-${i}`, type, name: `${type}账号${i}`, enabled: true, config: { mode: 'native' } })))

async function setup(page, preferences = { last: {}, directories: {} }) {
  await page.addInitScript(() => {
    localStorage.setItem('aether-clipboard-enabled:target-owner', 'true')
    localStorage.setItem('aether-clipboard-enabled:target-owner:consent', '1')
    window.shareText = ''
    Object.defineProperty(navigator, 'clipboard', { value: { readText: async () => window.shareText } })
    Object.defineProperty(navigator, 'permissions', { value: { query: async () => ({ state: 'granted' }) } })
  })
  const writes = [], transfers = []
  await page.route('**/api/**', async route => {
    const request = route.request(), url = new URL(request.url())
    let json = {}
    if (url.pathname === '/api/auth/status') json = { initialized: true, authenticated: true }
    else if (url.pathname === '/api/state') json = { username: 'target-owner', storages: [...storages, { id: 'disabled', type: '115', name: '停用账号', enabled: false }, { id: 'legacy-mobile', type: 'mobile', name: '旧版移动', enabled: true, config: {} }], tasks: [], settings: {}, cache: {} }
    else if (url.pathname === '/api/files/share-preferences') {
      if (request.method() === 'PUT') {
        const body = request.postDataJSON()
        writes.push(body)
        preferences.last[storages.find(s => s.id === body.storageId).type] = body.storageId
        preferences.directories[body.storageId] = { parent: body.parent, label: body.label, history: body.history }
        json = { ok: true }
      } else json = preferences
    } else if (url.pathname === '/api/files') {
      json = url.searchParams.get('path') === '/' ? [{ id: 'library', name: '资料库', isDir: true }] : url.searchParams.get('path') === 'library' ? [{ id: 'destination', name: '电影', isDir: true }] : []
    } else if (url.pathname === '/api/files/share/info') json={directory:''}
    else if (url.pathname === '/api/files/share/preview') {
      transfers.push(request.postDataJSON())
      json = { preview: 'preview', batches: 1 }
    } else if (url.pathname.startsWith('/api/files/share/')) json = { status: 'completed' }
    else if (url.pathname === '/api/quark-takeover') json = { enabled: false, bindings: [] }
    else if (url.pathname === '/api/files/visits' || url.pathname === '/api/files/favorites') json = []
    return route.fulfill({ json })
  })
  return { writes, transfers }
}

async function trigger(page, type, suffix = '') {
  await page.evaluate(text => { window.shareText = text; window.dispatchEvent(new Event('focus')) }, shareURL(type) + suffix)
  const modal = page.getByRole('dialog', { name: '分享转存', exact: true })
  await expect(modal).toBeVisible()
  return modal
}

for (const type of ['115', 'mobile', 'quark']) {
  test(`clipboard ${type} chooses all matching pools and restores its last target across browsers`, async ({ page, browser }, info) => {
    const preferences = { last: {}, directories: {} }
    const first = await setup(page, preferences)
    await page.goto('/storage')
    const modal = await trigger(page, type)
    await modal.getByRole('button', { name: '选择转存目录', exact: true }).click()
    const picker = page.getByRole('dialog', { name: '选择存储目录', exact: true })
    await expect(picker.locator('.source-accounts button')).toHaveCount(2)
    await expect(picker.locator('.source-accounts')).not.toContainText('停用账号')
    await expect(picker.locator('.source-accounts')).not.toContainText('旧版移动')
    await picker.locator('.source-accounts').getByRole('button', { name: `${type}账号2` }).click()
    await picker.getByRole('button', { name: '资料库', exact: true }).click()
    await picker.getByRole('button', { name: '电影', exact: true }).click()
    await picker.getByRole('button', { name: '选择当前目录', exact: true }).click()
    await expect(modal.getByRole('button', { name: '转存存储池', exact: true })).toHaveText(`${type}账号2`)
    await expect.poll(() => first.writes.length).toBe(1)
    expect(first.writes[0]).toMatchObject({ storageId: `${type}-2`, parent: 'destination', label: '电影' })
    await modal.getByRole('button', { name: '取消', exact: true }).click()

    const context = await browser.newContext({ baseURL: 'http://127.0.0.1:15159', viewport: { width: 390, height: 844 } })
    try {
      const other = await context.newPage()
      const second = await setup(other, preferences)
      await other.goto('/storage')
      const restored = await trigger(other, type, 'new')
      await expect(restored.getByRole('button', { name: '转存存储池', exact: true })).toHaveText(`${type}账号2`)
      await expect(restored.getByRole('button', { name: '选择转存目录', exact: true })).toHaveText('电影')
      await restored.getByRole('button', { name: '选择转存目录', exact: true }).click()
      const nextPicker = other.getByRole('dialog', { name: '选择存储目录', exact: true })
      await expect(nextPicker.locator('.source-accounts [aria-pressed=true]')).toContainText(`${type}账号2`)
      await expect(nextPicker.getByRole('navigation', { name: '存储目录路径' })).toContainText('资料库')
      await expect(nextPicker.getByRole('navigation', { name: '存储目录路径' })).toContainText('电影')
      await nextPicker.getByRole('button', { name: '选择当前目录', exact: true }).click()
      await other.evaluate(() => document.documentElement.dataset.theme = 'dark')
      expect(await restored.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
      await other.screenshot({ path: info.outputPath(`${type}-remembered-dark-mobile.png`) })
      await restored.getByRole('button', { name: '转存', exact: true }).click()
      await expect.poll(() => second.transfers.length).toBe(1)
      expect(second.transfers[0].storageId).toBe(`${type}-2`)
    } finally { await context.close() }
  })
}

test('unavailable remembered pool falls back, and dropdown selections persist separately by provider', async ({ page }) => {
  const preferences = { last: { '115': 'disabled', quark: 'quark-2' }, directories: {} }
  const { writes } = await setup(page, preferences)
  await page.goto('/storage')
  const modal = await trigger(page, '115')
  await expect(modal.getByRole('button', { name: '转存存储池', exact: true })).toHaveText('115账号1')
  await modal.getByRole('button', { name: '转存存储池', exact: true }).click()
  await page.getByRole('option', { name: '115账号2', exact: true }).click()
  await expect.poll(() => writes.length).toBe(1)
  expect(writes[0]).toMatchObject({ storageId: '115-2', parent: '/' })
  await modal.getByRole('button', { name: '取消', exact: true }).click()
  const quark = await trigger(page, 'quark')
  await expect(quark.getByRole('button', { name: '转存存储池', exact: true })).toHaveText('quark账号2')
})

test('manual transfer infers the link provider and can choose another matching account', async ({ page }) => {
  const { transfers } = await setup(page)
  await page.goto('/files?storage=115-1')
  await page.getByRole('button', { name: '工具', exact: true }).click()
  await page.getByRole('menuitem', { name: '分享转存', exact: true }).click()
  const modal = page.getByRole('dialog', { name: '分享转存', exact: true })
  await modal.getByRole('textbox', { name: '分享链接', exact: true }).fill(shareURL('quark'))
  await modal.getByRole('button', { name: '选择转存目录', exact: true }).click()
  const picker = page.getByRole('dialog', { name: '选择存储目录', exact: true })
  await expect(picker.locator('.source-accounts button')).toHaveCount(2)
  await expect(picker.locator('.source-accounts')).not.toContainText('115账号')
  await picker.locator('.source-accounts').getByRole('button', { name: 'quark账号2' }).click()
  await picker.getByRole('button', { name: '选择当前目录', exact: true }).click()
  await expect(modal.getByRole('button', { name: '选择转存目录', exact: true })).toContainText('quark账号2')
  await modal.getByRole('button', { name: '转存', exact: true }).click()
  await expect.poll(() => transfers.length).toBe(1)
  expect(transfers[0].storageId).toBe('quark-2')
})

test('reopening a clipboard dialog waits for its previous destination save', async ({ page }) => {
  const preferences = { last: {}, directories: {} }
  await setup(page, preferences)
  let releaseSave, started = false
  await page.route('**/api/files/share-preferences', async route => {
    if (route.request().method() === 'PUT') {
      const body = route.request().postDataJSON()
      started = true
      await new Promise(resolve => { releaseSave = resolve })
      preferences.last['115'] = body.storageId
      preferences.directories[body.storageId] = body
    }
    return route.fulfill({ json: preferences })
  })
  await page.goto('/storage')
  const modal = await trigger(page, '115')
  await modal.getByRole('button', { name: '转存存储池', exact: true }).click()
  await page.getByRole('option', { name: '115账号2', exact: true }).click()
  await expect.poll(() => started).toBe(true)
  await modal.getByRole('button', { name: '取消', exact: true }).click()
  const next = await trigger(page, '115', 'next')
  await expect(next.getByRole('button', { name: '转存存储池', exact: true })).toBeDisabled()
  releaseSave()
  await expect(next.getByRole('button', { name: '转存存储池', exact: true })).toBeEnabled()
  await expect(next.getByRole('button', { name: '转存存储池', exact: true })).toHaveText('115账号2')
})

for (const width of [1440, 390]) {
  for (const theme of ['light', 'dark']) {
    test(`plugin cards separate title, subtitle and description at ${width} ${theme}`, async ({ page }, info) => {
      await setup(page)
      await page.setViewportSize({ width, height: 1000 })
      await page.goto('/tools')
      await page.evaluate(value => document.documentElement.dataset.theme = value, theme)
      const cards = page.locator('.plugin-card')
      await expect(cards).toHaveCount(16)
      const layout = await cards.evaluateAll(elements => elements.map(el => {
        const bounds = node => { const r = node.getBoundingClientRect(); return { x: r.x, y: r.y, right: r.right, bottom: r.bottom } }
        return { card: bounds(el), symbol: bounds(el.querySelector('.plugin-symbol,.rapid115-icon')), title: bounds(el.querySelector('strong')), subtitle: bounds(el.querySelector('.plugin-subtitle')), description: bounds(el.querySelector('.plugin-description')), subtitleText: el.querySelector('.plugin-subtitle').textContent }
      }))
      for (const row of layout) {
        expect(row.subtitleText.length).toBeGreaterThan(0)
        expect(row.title.bottom).toBeLessThanOrEqual(row.subtitle.y)
        expect(row.subtitle.bottom).toBeLessThanOrEqual(row.description.y)
        expect(row.symbol.bottom).toBeLessThanOrEqual(row.description.y)
        expect(row.description.bottom).toBeLessThanOrEqual(row.card.bottom)
        expect(row.title.right).toBeLessThanOrEqual(row.card.right)
        expect(row.title.y - row.card.y).toBeCloseTo(layout[0].title.y - layout[0].card.y, 0)
        expect(row.description.y - row.card.y).toBeCloseTo(layout[0].description.y - layout[0].card.y, 0)
      }
      await page.screenshot({ path: info.outputPath(`tools-${width}-${theme}.png`), fullPage: true })
    })
  }
}
