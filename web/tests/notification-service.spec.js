import { test, expect } from '@playwright/test'
import { captureScreenshot } from './helpers/screenshot.js'

async function fixture(page) {
  let config = { telegram: { enabled: false, token: '', chatId: '', userIds: [], hasToken: false }, targets: [] }
  let tests = 0, next = 0
  let secret = ''
  const pools = [{ id: '115', type: '115', name: '115影视' }, { id: 'mobile', type: 'mobile', name: '移动影视' }, { id: 'q1', type: 'quark', name: '夸克一' }, { id: 'q2', type: 'quark', name: '夸克二' }].map(item => ({ ...item, enabled: true, config: { mode: 'native' } }))
  await page.route('**/api/**', route => {
    const r = route.request(), url = new URL(r.url()), path = url.pathname
    let json = {}
    if (path === '/api/auth/status') json = { initialized: true, authenticated: true }
    else if (path === '/api/state') json = { username: 'tg-service', storages: pools, tasks: [], settings: {}, traffic: {}, cache: {} }
    else if (path === '/api/notification-service') {
      if (r.method() === 'PUT') { const tg = r.postDataJSON().telegram; if (tg.token) secret = tg.token; config.telegram = { ...tg, token: '', hasToken: !!secret } }
      json = config
    } else if (path === '/api/notification-service/secret') {
      json = r.postDataJSON().metadataOnly ? { length: secret.length } : { value: secret }
    } else if (path === '/api/notification-service/targets') {
      config.targets = r.postDataJSON().targets.map(item => ({ ...item, id: item.id || `target-${++next}` }))
      json = config
    } else if (path === '/api/notification-service/test') { tests++; json = { ok: true } }
    else if (path === '/api/files') {
      const parent = url.searchParams.get('path')
      json = parent === '/' ? [{ id: 'library', name: 'Library', isDir: true }] : parent === 'library' ? [{ id: 'movies', name: 'Movies', isDir: true }] : []
    } else if (path === '/api/logs') json = [{ module: 'notifications', level: 'info', time: '2026-10-23T10:00:00Z', message: '[TG:test] 夸克一：已提交' }]
    return route.fulfill({ json })
  })
  return { config: () => config, tests: () => tests }
}

for (const width of [1440, 390]) for (const theme of ['light', 'dark']) test(`notification configuration and directory pools persist ${width} ${theme}`, async ({ page }, info) => {
  const f = await fixture(page)
  await page.setViewportSize({ width, height: 1000 })
  await page.goto('/notifications')
  await page.evaluate(theme => document.documentElement.dataset.theme = theme, theme)
  await expect(page).toHaveURL(/\/notifications\/telegram$/)
  if (width < 700) await page.getByRole('button', { name: '打开导航', exact: true }).click()
  const nav = (await page.getByRole('navigation', { name: '主导航', exact: true }).getByRole('link').allTextContents()).map(text => text.trim())
  expect(nav.indexOf('通知服务')).toBe(nav.indexOf('档案中心') + 1)
  if (width < 700) await page.locator('.nav-overlay').click({ position: { x: width - 8, y: 50 } })
  const tg = page.getByRole('form', { name: 'TG Bot', exact: true })
  const qq = page.getByRole('form', { name: 'QQ Bot', exact: true })
  const feishu = page.getByRole('form', { name: '飞书通知', exact: true })
  await expect(qq).toBeVisible();await expect(feishu).toBeVisible()
  const bounds=await Promise.all([qq,feishu,tg].map(form=>form.boundingBox()))
  expect(Math.max(...bounds.map(box=>box.width))-Math.min(...bounds.map(box=>box.width))).toBeLessThan(1)
  if(width===1440)expect(Math.max(...bounds.map(box=>box.y))-Math.min(...bounds.map(box=>box.y))).toBeLessThan(1)
  await page.getByLabel('Bot Token', { exact: true }).fill('123456789:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa')
  await page.getByRole('textbox', { name: 'Chat ID', exact: true }).fill('123')
  await page.getByRole('textbox', { name: '授权用户 ID', exact: true }).fill('123;456')
  await tg.getByRole('switch', { name: '启用 TG Bot', exact: true }).check()
  await tg.getByRole('button', { name: '保存设置', exact: true }).click()
  await expect(page.getByLabel('Bot Token', { exact: true })).toHaveAttribute('type', 'password')
  await expect(page.getByLabel('Bot Token', { exact: true })).toHaveValue(/^\*+$/)
  await tg.getByRole('button', { name: '显示内容', exact: true }).click()
  await expect(page.getByLabel('Bot Token', { exact: true })).toHaveValue('123456789:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa')
  await expect(page.getByLabel('Bot Token', { exact: true })).toHaveAttribute('type', 'text')
  await tg.getByRole('button', { name: '隐藏内容', exact: true }).click()
  await page.reload()
  await page.evaluate(theme => document.documentElement.dataset.theme = theme, theme)
  await expect(page.getByLabel('Bot Token', { exact: true })).toHaveAttribute('type', 'password')
  await tg.getByRole('button', { name: '显示内容', exact: true }).click()
  await expect(page.getByLabel('Bot Token', { exact: true })).toHaveValue('123456789:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa')
  await tg.getByRole('button', { name: '隐藏内容', exact: true }).click()
  await tg.getByRole('button', { name: '测试通知', exact: true }).click()
  await expect.poll(() => f.tests()).toBe(1)
  for (const field of ['Bot Token', 'Chat ID', '授权用户 ID']) await expect(page.getByLabel(field, { exact: true })).toHaveCSS('height', '40px')
  await captureScreenshot(page, { path: info.outputPath(`telegram-${width}-${theme}.png`) })
  await page.getByRole('link', { name: '链接转存', exact: true }).click()
  await expect(page.locator('.telegram-pool')).toHaveCount(3)
  await expect(page.locator('.telegram-pool').first()).toHaveCSS('border-radius', '8px')
  await expect(page.locator('.telegram-pool h2').first()).toHaveCSS('font-size', '17px')
  for (const name of ['115 云盘', '移动云盘', '夸克云盘']) await expect(page.getByRole('button', { name: `添加${name}转存目录`, exact: true })).toBeVisible()
  async function add(name) {
    await page.getByRole('button', { name: '添加夸克云盘转存目录', exact: true }).click()
    const modal = page.getByRole('dialog', { name: '添加转存目录', exact: true })
    expect(Math.round((await modal.boundingBox()).width)).toBe(Math.min(640, width - 32))
    await modal.getByRole('button', { name: '选择转存目录', exact: true }).click()
    await expect(page.locator('.source-accounts button')).toHaveCount(2)
    await page.locator('.source-accounts button').filter({ hasText: name }).click()
    await page.getByRole('button', { name: 'Library', exact: true }).click()
    await page.getByRole('button', { name: 'Movies', exact: true }).click()
    await page.getByRole('button', { name: '选择当前目录', exact: true }).click()
    await expect(modal.getByRole('button', { name: '选择转存目录', exact: true })).toHaveText(`${name} · /Library/Movies`)
    await modal.getByRole('button', { name: '添加', exact: true }).click()
    await expect(modal).toHaveCount(0)
  }
  await add('夸克一')
  await add('夸克二')
  const cards = page.locator('.telegram-target-card')
  await expect(cards).toHaveCount(2)
  await cards.nth(1).focus()
  await page.keyboard.press('Alt+ArrowUp')
  await expect(cards.first()).toContainText('夸克二')
  expect(f.config().targets.map(item => item.storageId)).toEqual(['q2', 'q1'])
  await cards.first().getByRole('button', { name: '编辑转存目录 /Library/Movies', exact: true }).click()
  const edit = page.getByRole('dialog', { name: '编辑转存目录', exact: true })
  await edit.getByRole('button', { name: '选择转存目录', exact: true }).click()
  const crumbs = page.getByRole('navigation', { name: '存储目录路径', exact: true })
  await expect(crumbs.getByRole('button', { name: 'Movies', exact: true })).toBeVisible()
  if (!await crumbs.getByRole('button', { name: 'Library', exact: true }).isVisible()) await crumbs.getByRole('button', { name: '展开省略路径', exact: true }).click()
  await crumbs.getByRole('button', { name: 'Library', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Movies', exact: true })).toBeVisible()
  await crumbs.getByRole('button', { name: '根目录', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Library', exact: true })).toBeVisible()
  await page.locator('.source-accounts button').filter({ hasText: '夸克一' }).click()
  await page.getByRole('button', { name: '选择当前目录', exact: true }).click()
  await edit.getByRole('button', { name: '保存', exact: true }).click()
  await expect(cards.first()).toContainText('夸克一')
  await expect(cards.first().locator('.provider-icon')).toHaveCSS('background-color', 'rgba(0, 0, 0, 0)')
  await expect(cards.first().locator('.provider-logo')).toHaveAttribute('src', '/providers/quark-mark.png')
  const whitePixels = await cards.first().locator('.provider-logo').evaluate(async img => {
    await img.decode()
    const canvas = document.createElement('canvas'); canvas.width = img.naturalWidth; canvas.height = img.naturalHeight
    const ctx = canvas.getContext('2d'); ctx.drawImage(img, 0, 0)
    const pixels = ctx.getImageData(0, 0, canvas.width, canvas.height).data
    let count = 0
    for (let i = 0; i < pixels.length; i += 4) if (pixels[i] > 245 && pixels[i + 1] > 245 && pixels[i + 2] > 245 && pixels[i + 3] > 230) count++
    return count
  })
  expect(whitePixels).toBe(0)
  await page.reload()
  await page.evaluate(theme => document.documentElement.dataset.theme = theme, theme)
  await expect(cards).toHaveCount(2)
  await expect(cards.first()).toContainText('夸克一')
  expect(await page.locator('.notification-page').evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
  await captureScreenshot(page, { path: info.outputPath(`directory-pools-${width}-${theme}.png`) })
  await cards.first().getByRole('button', { name: '删除转存目录 /', exact: true }).click()
  await page.getByRole('dialog', { name: '删除转存目录', exact: true }).getByRole('button', { name: '确认删除', exact: true }).click()
  await expect(cards).toHaveCount(1)
  await page.goto('/logs?module=notifications')
  await expect(page.locator('.log-viewport')).toContainText('夸克一')
})

test('directory cards drag in their provider pool and preserve the order after reload', async ({ page }) => {
  const f = await fixture(page)
  f.config().targets.push({ id: 'first', provider: 'quark', storageId: 'q1', parent: '0', path: '/First' }, { id: 'second', provider: 'quark', storageId: 'q2', parent: '0', path: '/Second' })
  await page.goto('/notifications/transfers')
  const cards = page.locator('.telegram-target-card')
  await expect(cards).toHaveCount(2)
  const first = await cards.first().boundingBox(), second = await cards.last().boundingBox()
  await page.mouse.move(first.x + first.width * .4, first.y + first.height / 2)
  await page.mouse.down()
  await page.waitForTimeout(190)
  await page.mouse.move(second.x + second.width * .4, second.y + second.height / 2, { steps: 8 })
  await page.mouse.up()
  await expect(cards.first()).toContainText('/Second')
  expect(f.config().targets.map(item => item.id)).toEqual(['second', 'first'])
  await page.reload()
  await expect(cards.first()).toContainText('/Second')
})

test('a changed saved directory is reported without silently replacing its identity', async ({ page }) => {
  const f = await fixture(page)
  f.config().targets.push({ id: 'old', provider: 'quark', storageId: 'q1', parent: 'old-movies', path: '/Library/Movies' })
  await page.goto('/notifications/transfers')
  await page.getByRole('button', { name: '编辑转存目录 /Library/Movies', exact: true }).click()
  await page.getByRole('button', { name: '选择转存目录', exact: true }).click()
  await expect(page.locator('.toast').filter({ hasText: '目录身份已变化' })).toBeVisible()
  const picker = page.getByRole('dialog', { name: '选择存储目录', exact: true })
  await expect(picker.getByRole('button', { name: 'Library', exact: true })).toBeVisible()
  expect(f.config().targets[0].parent).toBe('old-movies')
  await page.keyboard.press('Escape')
  await page.keyboard.press('Escape')
  expect(f.config().targets[0]).toMatchObject({ parent: 'old-movies', path: '/Library/Movies' })
})

test('a directory lookup cannot reopen a cancelled editor or replace a new picker', async ({ page }) => {
  const f = await fixture(page)
  f.config().targets.push({ id: 'saved', provider: 'quark', storageId: 'q1', parent: 'movies', path: '/Library/Movies' })
  let release, started = false
  const wait = new Promise(resolve => { release = resolve })
  await page.route('**/api/files?**', async route => { started = true; await wait; await route.fulfill({ json: [{ id: 'library', name: 'Library', isDir: true }] }) })
  await page.goto('/notifications/transfers')
  await page.getByRole('button', { name: '编辑转存目录 /Library/Movies', exact: true }).click()
  await page.getByRole('button', { name: '选择转存目录', exact: true }).click()
  await expect.poll(() => started).toBe(true)
  await page.keyboard.press('Escape')
  await page.getByRole('button', { name: '添加夸克云盘转存目录', exact: true }).click()
  await page.getByRole('button', { name: '选择转存目录', exact: true }).click()
  await expect(page.getByRole('dialog', { name: '选择存储目录', exact: true })).toBeVisible()
  release()
  await expect(page.locator('.source-accounts [aria-pressed=true]')).toHaveCount(0)
  expect(f.config().targets).toHaveLength(1)
})

for (const width of [1440, 390]) for (const theme of ['light', 'dark']) test(`115 directory purposes and unnumbered cards ${width} ${theme}`, async ({ page }, info) => {
  const f = await fixture(page)
  f.config().targets.push({ id: 'legacy', provider: '115', storageId: '115', parent: 'movies', path: '/Library/Movies' })
  await page.setViewportSize({ width, height: 1000 })
  await page.goto('/notifications/transfers')
  await page.evaluate(theme => document.documentElement.dataset.theme = theme, theme)
  const pool = page.getByRole('region', { name: '115 云盘转存目录池', exact: true })
  const card = pool.locator('.telegram-target-card').first()
  await expect(page.locator('.telegram-order')).toHaveCount(0)
  await expect(card.locator('.telegram-purpose-share')).toHaveText('分享转存')
  await expect(card.locator('.telegram-purpose-offline')).toHaveCount(0)
  await card.getByRole('button', { name: '编辑转存目录 /Library/Movies', exact: true }).click()
  const edit = page.getByRole('dialog', { name: '编辑转存目录', exact: true })
  await expect(edit.getByRole('switch', { name: '分享转存', exact: true })).toBeChecked()
  await expect(edit.getByRole('switch', { name: 'ED2K离线', exact: true })).not.toBeChecked()
  await edit.getByRole('switch', { name: 'ED2K离线', exact: true }).check()
  await captureScreenshot(page, { path: info.outputPath(`purposes-edit-${width}-${theme}.png`) })
  await edit.getByRole('button', { name: '保存', exact: true }).click()
  await expect(card.locator('.telegram-purpose-offline')).toHaveText('ED2K离线')
  expect(f.config().targets[0]).toMatchObject({ shareTransfer: true, ed2kOffline: true })
  for (const selector of ['.telegram-purpose-share', '.telegram-purpose-offline']) {
    const contrast = await card.locator(selector).evaluate(el => {
      const ctx = document.createElement('canvas').getContext('2d')
      const luminance = color => {
        ctx.fillStyle = color; ctx.fillRect(0, 0, 1, 1)
        const values = [...ctx.getImageData(0, 0, 1, 1).data].slice(0, 3).map(value => {
          const v = value / 255
          return v <= .04045 ? v / 12.92 : ((v + .055) / 1.055) ** 2.4
        })
        return values[0] * .2126 + values[1] * .7152 + values[2] * .0722
      }
      const style = getComputedStyle(el), a = luminance(style.color), b = luminance(style.backgroundColor)
      return (Math.max(a, b) + .05) / (Math.min(a, b) + .05)
    })
    expect(contrast).toBeGreaterThanOrEqual(4.5)
  }
  await captureScreenshot(page, { path: info.outputPath(`purposes-both-${width}-${theme}.png`) })
  await card.getByRole('button', { name: '编辑转存目录 /Library/Movies', exact: true }).click()
  await edit.getByRole('switch', { name: '分享转存', exact: true }).uncheck()
  await edit.getByRole('button', { name: '保存', exact: true }).click()
  await page.reload()
  await page.evaluate(theme => document.documentElement.dataset.theme = theme, theme)
  await expect(card.locator('.telegram-purpose-share')).toHaveCount(0)
  await expect(card.locator('.telegram-purpose-offline')).toHaveText('ED2K离线')
  await expect(card.getByRole('button', { name: '编辑转存目录 /Library/Movies', exact: true }).locator('svg.lucide-sliders-horizontal-icon')).toHaveCount(1)
  await captureScreenshot(page, { path: info.outputPath(`purposes-card-${width}-${theme}.png`) })
  expect(await page.locator('.notification-page').evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
  await pool.getByRole('button', { name: '添加115 云盘转存目录', exact: true }).click()
  const add = page.getByRole('dialog', { name: '添加转存目录', exact: true })
  await expect(add.getByRole('switch', { name: '分享转存', exact: true })).toBeChecked()
  await expect(add.getByRole('switch', { name: 'ED2K离线', exact: true })).not.toBeChecked()
  await add.getByRole('button', { name: '选择转存目录', exact: true }).click()
  await expect(page.locator('.source-accounts button')).toHaveCount(1)
  await page.locator('.source-accounts button').click()
  await page.getByRole('button', { name: '选择当前目录', exact: true }).click()
  await add.getByRole('button', { name: '添加', exact: true }).click()
  expect(f.config().targets[1]).toMatchObject({ provider: '115', shareTransfer: true, ed2kOffline: false, path: '/' })
  await card.getByRole('button', { name: '编辑转存目录 /Library/Movies', exact: true }).click()
  await edit.getByRole('switch', { name: 'ED2K离线', exact: true }).uncheck()
  await edit.getByRole('button', { name: '保存', exact: true }).click()
  await expect(card.locator('.telegram-purpose-disabled')).toHaveText('未启用')
  expect(f.config().targets[0]).toMatchObject({ shareTransfer: false, ed2kOffline: false })
  await page.getByRole('button', { name: '添加移动云盘转存目录', exact: true }).click()
  await expect(page.getByRole('dialog', { name: '添加转存目录', exact: true }).getByRole('switch')).toHaveCount(0)
})
