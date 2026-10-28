import { test, expect } from '@playwright/test'
import { captureScreenshot } from './helpers/screenshot.js'

const username = 'sidebar-return-memory'
const pages = [
  { child: '/tasks/cache/settings', parent: '/tasks/cache', module: '任务中心', open: '打开缓存设置', back: '返回缓存任务' },
  { child: '/tasks/cas/pool', parent: '/tasks/cas', module: '任务中心', open: '打开存储绑定池', back: '返回 CAS 任务' },
  { child: '/tasks/ed2k/pool', parent: '/tasks/ed2k', module: '任务中心', open: '打开存储绑定池', back: '返回 ED2K 任务' },
  { child: '/tools/115-rapid', parent: '/tools', module: '辅助工具', open: '115 秒传', back: '返回页面' },
  { child: '/virtual/quark/settings', parent: '/virtual/quark', module: '虚拟链库', open: '夸克链库设置', back: '返回页面' },
  { child: '/transfer/backup/settings', parent: '/transfer/backup', module: '传输中心', open: '备份设置', back: '返回备份规则' }
]

async function mock(page) {
  const writes = []
  await page.route('**/api/**', route => {
    const request = route.request(), path = new URL(request.url()).pathname
    if (request.method() !== 'GET') writes.push(path)
    let json = {}
    if (path === '/api/auth/status') json = { initialized: true, authenticated: true }
    if (path === '/api/state') json = { username, storages: [], tasks: [], settings: {}, cache: {}, traffic: {}, logs: [] }
    if (['/api/transfers', '/api/backup-rules', '/api/generation/jobs', '/api/automations'].includes(path)) json = []
    if (path === '/api/115-rapid') json = { manual: {}, watches: [] }
    if (path === '/api/115-rapid/records') json = { items: [], page: 1, total: 0, pages: 1 }
    if (path === '/api/virtual-library/quark') json = { cards: [], settings: { storageIds: [], directory: '', invalidDays: 15, retentionHours: 12 }, stats: {} }
    if (path === '/api/plugin-market') json = { items: [], repositories: [], total: 0 }
    return route.fulfill({ json })
  })
  return writes
}

async function sidebar(page, name) {
  const toggle = page.getByRole('button', { name: '打开导航', exact: true })
  if (await toggle.isVisible()) await toggle.click()
  await page.getByRole('navigation', { name: '主导航' }).getByRole('link', { name, exact: true }).click()
}

for (const [width, theme] of [[1440, 'light'], [1440, 'dark'], [390, 'light'], [375, 'dark']]) for (const entry of pages) test(`jump page returns to parent through sidebar ${entry.child} ${width} ${theme}`, async ({ page }, info) => {
  const writes = await mock(page)
  await page.setViewportSize({ width, height: 900 })
  await page.goto(entry.child)
  await page.evaluate(theme => document.documentElement.dataset.theme = theme, theme)
  await expect(page.getByRole('button', { name: entry.back, exact: true })).toBeVisible()
  await expect(page).toHaveURL(new RegExp(`${entry.child}$`))
  const key = `aether-session:${username}:route:/${entry.child.split('/')[1]}`
  await expect.poll(() => page.evaluate(key => JSON.parse(sessionStorage.getItem(key)), key)).toBe(entry.parent)
  await sidebar(page, '信息总览')
  await expect(page).toHaveURL(/\/dashboard$/)
  await sidebar(page, entry.module)
  await expect(page).toHaveURL(new RegExp(`${entry.parent}$`))
  await expect(page.getByRole('button', { name: entry.back, exact: true })).toHaveCount(0)
  if (entry.child === '/tasks/cache/settings') {
    await captureScreenshot(page, { path: info.outputPath(`parent-${width}-${theme}.png`) })
  }
  expect(writes).toEqual([])
})

test('old stored child routes normalize while normal columns and browser history remain available', async ({ page }) => {
  await mock(page)
  await page.goto('/dashboard')
  await expect(page.locator('.app-shell')).toBeVisible()
  for (const entry of pages) {
    await page.evaluate(({ key, child }) => sessionStorage.setItem(key, JSON.stringify(child)), {
      key: `aether-session:${username}:route:/${entry.child.split('/')[1]}`, child: entry.child
    })
    await sidebar(page, entry.module)
    await expect(page).toHaveURL(new RegExp(`${entry.parent}$`))
    await sidebar(page, '信息总览')
  }
  for (const [path, module] of [['/tasks/ed2k', '任务中心'], ['/tools/market', '辅助工具'], ['/settings/strm', '系统设置'], ['/notifications/transfers', '通知服务']]) {
    await page.goto(path)
    await expect(page.locator('.app-shell')).toBeVisible()
    await sidebar(page, '信息总览')
    await sidebar(page, module)
    await expect(page).toHaveURL(new RegExp(`${path}$`))
  }
  await page.goto('/tasks/cache/settings?panel=cache#details')
  await expect(page.getByRole('button', { name: '返回缓存任务', exact: true })).toBeVisible()
  await page.reload()
  await expect(page).toHaveURL(/\/tasks\/cache\/settings\?panel=cache#details$/)
  await expect(page.getByRole('button', { name: '返回缓存任务', exact: true })).toBeVisible()
  await sidebar(page, '信息总览')
  await page.goBack()
  await expect(page).toHaveURL(/\/tasks\/cache\/settings\?panel=cache#details$/)
  await expect(page.getByRole('button', { name: '返回缓存任务', exact: true })).toBeVisible()
  await page.goForward()
  await expect(page).toHaveURL(/\/dashboard$/)
  await sidebar(page, '任务中心')
  await expect(page).toHaveURL(/\/tasks\/cache$/)
})

test('in-app jump entries preserve parent memory and their return buttons', async ({ page }) => {
  const writes = await mock(page)
  await page.emulateMedia({ reducedMotion: 'reduce' })
  for (const entry of pages) {
    await page.goto(entry.parent)
    await page.getByRole('button', { name: entry.open, exact: true }).click()
    await expect(page).toHaveURL(new RegExp(`${entry.child}$`))
    const key = `aether-session:${username}:route:/${entry.child.split('/')[1]}`
    await expect.poll(() => page.evaluate(key => JSON.parse(sessionStorage.getItem(key)), key)).toBe(entry.parent)
    await page.getByRole('button', { name: entry.back, exact: true }).click()
    await expect(page).toHaveURL(new RegExp(`${entry.parent}$`))
  }
  expect(writes).toEqual(['/api/115-rapid'])
})
