import { test, expect } from '@playwright/test'
import { captureScreenshot } from './helpers/screenshot.js'

async function fixture(page) {
  let config = { telegram: { enabled: false, token: '', chatId: '', userIds: [], hasToken: false }, targets: [] }
  let tests = 0, next = 0
  const pools = [{ id: '115', type: '115', name: '115影视' }, { id: 'mobile', type: 'mobile', name: '移动影视' }, { id: 'q1', type: 'quark', name: '夸克一' }, { id: 'q2', type: 'quark', name: '夸克二' }].map(item => ({ ...item, enabled: true, config: { mode: 'native' } }))
  await page.route('**/api/**', route => {
    const r = route.request(), url = new URL(r.url()), path = url.pathname
    let json = {}
    if (path === '/api/auth/status') json = { initialized: true, authenticated: true }
    else if (path === '/api/state') json = { username: 'tg-service', storages: pools, tasks: [], settings: {}, traffic: {}, cache: {} }
    else if (path === '/api/notification-service') {
      if (r.method() === 'PUT') { const tg = r.postDataJSON().telegram; config.telegram = { ...tg, token: '', hasToken: !!tg.token || config.telegram.hasToken } }
      json = config
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
  await page.goto('/settings/account')
  const standard = (await page.locator('.account-settings').boundingBox()).width
  await page.goto('/notifications')
  await page.evaluate(theme => document.documentElement.dataset.theme = theme, theme)
  await expect(page).toHaveURL(/\/notifications\/telegram$/)
  if (width < 700) await page.getByRole('button', { name: '打开导航', exact: true }).click()
  const nav = (await page.getByRole('navigation', { name: '主导航', exact: true }).getByRole('link').allTextContents()).map(text => text.trim())
  expect(nav.indexOf('通知服务')).toBe(nav.indexOf('辅助工具') + 1)
  if (width < 700) await page.locator('.nav-overlay').click({ position: { x: width - 8, y: 50 } })
  expect((await page.locator('.notification-form').boundingBox()).width).toBe(standard)
  await page.getByLabel('Bot Token', { exact: true }).fill('123456789:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa')
  await page.getByRole('textbox', { name: 'Chat ID', exact: true }).fill('123')
  await page.getByRole('textbox', { name: '授权用户 ID', exact: true }).fill('123;456')
  await page.getByRole('switch', { name: '启用 TG 通知', exact: true }).check()
  await page.getByRole('button', { name: '保存设置', exact: true }).click()
  await expect(page.getByLabel('Bot Token', { exact: true })).toHaveValue('')
  await page.getByRole('button', { name: '测试通知', exact: true }).click()
  await expect.poll(() => f.tests()).toBe(1)
  for (const field of ['Bot Token', 'Chat ID', '授权用户 ID']) await expect(page.getByLabel(field, { exact: true })).toHaveCSS('height', '40px')
  await captureScreenshot(page, { path: info.outputPath(`telegram-${width}-${theme}.png`) })
  await page.getByRole('link', { name: '链接转存', exact: true }).click()
  await expect(page.locator('.telegram-pool')).toHaveCount(3)
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
  await expect(page.locator('.directory-path-label')).toContainText('/Library/Movies')
  await page.locator('.source-accounts button').filter({ hasText: '夸克一' }).click()
  await page.getByRole('button', { name: '选择当前目录', exact: true }).click()
  await edit.getByRole('button', { name: '保存', exact: true }).click()
  await expect(cards.first()).toContainText('夸克一')
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
