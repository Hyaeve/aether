import { test, expect } from '@playwright/test'

test('task rows have paired layout, scan times, direct actions and toggle feedback', async ({ page }, info) => {
  const tasks = [1, 2].map(id => ({ id: `${id}`, name: `任务 ${id}`, kind: 'strm', storageId: 'local', enabled: true, status: 'success', processed: 1, lastRun: '2026-10-08T01:00:00Z' }))
  await page.route('**/api/auth/status', r => r.fulfill({ json: { initialized: true, authenticated: true } }))
  await page.route('**/api/state', r => r.fulfill({ json: { storages: [{ id: 'local', name: '本机', type: 'local', enabled: true }], tasks, settings: {}, traffic: {}, cache: {} } }))
  await page.route('**/api/tasks/**', r => {
    const [, , id, action] = new URL(r.request().url()).pathname.split('/').slice(1)
    const task = tasks.find(t => t.id === id)
    if (r.request().method() === 'PUT') Object.assign(task, r.request().postDataJSON())
    else task.status = action === 'run' ? 'running' : 'cancelled'
    return r.fulfill({ json: { ok: true } })
  })
  await page.goto('/tasks/strm')
  const rows = page.locator('.task-row-card')
  await expect(rows).toHaveCount(2)
  const first = await rows.nth(0).boundingBox(), second = await rows.nth(1).boundingBox()
  expect(first.y).toBe(second.y)
  expect(first.x + first.width).toBeLessThan(second.x)
  await expect(rows.first()).toContainText('最后扫描')
  await page.getByRole('button', { name: '停用任务 任务 1', exact: true }).click()
  await expect(page.locator('.toast-stack')).toContainText('已停用任务 任务 1')
  await page.getByRole('button', { name: '启用任务 任务 1', exact: true }).click()
  await expect(page.locator('.toast-stack')).toContainText('已启用任务 任务 1')
  const run = rows.first().getByRole('button', { name: '立即执行', exact: true })
  const runBox = await run.boundingBox(), menuBox = await rows.first().getByRole('button', { name: '任务操作 任务 1' }).boundingBox()
  expect(runBox.x + runBox.width).toBeLessThanOrEqual(menuBox.x)
  await run.click()
  await expect(rows.first().locator('.task-provider-toggle')).toBeDisabled()
  await rows.first().getByRole('button', { name: '停止任务', exact: true }).click()
  await expect(page.locator('.toast-stack')).toContainText('正在停止任务')
  for (const theme of ['light', 'dark']) {
    await page.evaluate(t => document.documentElement.dataset.theme = t, theme)
    await page.screenshot({ path: info.outputPath(`tasks-${theme}.png`) })
  }
  await page.setViewportSize({ width: 390, height: 844 })
  expect(await rows.first().evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
  await page.screenshot({ path: info.outputPath('tasks-mobile.png') })
})

test('topbar menus share a compact gap and right edge on desktop and mobile', async ({ page }, info) => {
  await page.route('**/api/auth/status', r => r.fulfill({ json: { initialized: true, authenticated: true } }))
  await page.route('**/api/state', r => r.fulfill({ json: { storages: [], tasks: [], settings: {}, traffic: {}, cache: {} } }))
  await page.goto('/tasks/strm')
  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: 900 })
    await page.getByRole('button', { name: '任务通知', exact: true }).click()
    const topbar = await page.locator('.topbar').boundingBox()
    const notices = await page.locator('.notification-dropdown').boundingBox()
    expect(notices.y - topbar.y - topbar.height).toBeCloseTo(4, 0)
    expect(notices.x + notices.width).toBeLessThanOrEqual(width)
    await page.screenshot({ path: info.outputPath(`notifications-${width}.png`) })
    await page.getByRole('button', { name: '账号菜单', exact: true }).click()
    const account = await page.locator('.account-dropdown').boundingBox()
    expect(account.y).toBe(notices.y)
    expect(account.x + account.width).toBeCloseTo(notices.x + notices.width, 0)
    await page.getByRole('button', { name: '账号菜单', exact: true }).click()
  }
})
