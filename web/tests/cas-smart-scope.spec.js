import { test, expect } from '@playwright/test'
import { captureScreenshot } from './helpers/screenshot.js'

async function setup(page, theme = 'light') {
  const tasks = [
    { id: 'legacy', name: '旧默认任务', kind: 'cas', storageId: 'mobile', source: '/', target: '/generated', enabled: true, status: 'idle' },
    { id: 'explicit', name: '明确全计算', kind: 'cas', casInfoMode: 'all', storageId: 'mobile', source: '/', target: '/explicit', enabled: true, status: 'idle' }
  ]
  const storages = [
    { id: 'local', name: '本地', type: 'local', enabled: true, config: {} },
    { id: 'mobile', name: '移动', type: 'mobile', enabled: true, config: { mode: 'native' } },
    { id: 'tianyi', name: '天翼', type: 'tianyi', enabled: true, config: {} }
  ]
  const writes = [], unexpected = []
  await page.addInitScript(value => localStorage.setItem('aether-theme', value), theme)
  await page.route('**/api/**', route => {
    const request = route.request(), path = new URL(request.url()).pathname
    if (path === '/api/auth/status') return route.fulfill({ json: { initialized: true, authenticated: true } })
    if (path === '/api/state') return route.fulfill({ json: { username: 'smart-cas', storages, tasks, settings: {}, cache: {}, traffic: {} } })
    if (path === '/api/generation/jobs' || path === '/api/files') return route.fulfill({ json: [] })
    if (path === '/api/cron/validate') return route.fulfill({ json: { valid: true } })
    if (path === '/api/traffic') return route.fulfill({ json: {} })
    if (path === '/api/strm-replace') return route.fulfill({ json: { tasks: [] } })
    if (path === '/api/tasks' && request.method() === 'POST') {
      const body = request.postDataJSON()
      writes.push(body)
      const task = { ...body, id: 'new', status: 'idle' }
      tasks.push(task)
      return route.fulfill({ json: task })
    }
    if (path === '/api/tasks/explicit/run') return route.fulfill({ status: 409, json: { error: 'STRM 任务「剧集生成」正在使用重叠目录，请等待完成后操作' } })
    if (path === '/api/tasks/legacy/run') return route.fulfill({ json: { ok: true } })
    unexpected.push(`${request.method()} ${path}`)
    return route.fulfill({ status: 500, json: { error: `Unmocked API: ${path}` } })
  })
  return { writes, unexpected }
}

for (const [width, theme] of [[1440, 'light'], [390, 'dark'], [375, 'light'], [844, 'dark']]) test(`CAS smart calculation is default and preserves explicit choices ${width} ${theme}`, async ({ page }, info) => {
  const fixture = await setup(page, theme)
  await page.setViewportSize({ width, height: width === 844 ? 390 : 900 })
  await page.goto('/tasks/cas')
  await page.getByRole('button', { name: '添加 CAS 任务卡片', exact: true }).click()
  const mode = page.getByRole('button', { name: 'CAS 信息', exact: true })
  await expect(mode).toHaveText('智能计算')
  await mode.click()
  await expect(page.getByRole('listbox', { name: 'CAS 信息选项', exact: true }).getByRole('option')).toHaveText(['智能计算', '全计算', '移动CAS', '天翼CAS'])
  await page.getByRole('option', { name: '智能计算', exact: true }).click()
  for (const source of ['移动', '天翼', '本地']) {
    await page.getByRole('button', { name: '选择目录', exact: true }).click()
    const picker = page.getByRole('dialog', { name: '选择存储目录', exact: true })
    await picker.locator('.source-accounts button').filter({ hasText: source }).click()
    await picker.getByRole('button', { name: '选择当前目录', exact: true }).click()
    await expect(mode).toHaveText('智能计算')
  }
  await page.getByRole('textbox', { name: '任务名称 *', exact: true }).fill('智能生成')
  await captureScreenshot(page, { path: info.outputPath(`cas-smart-${width}-${theme}.png`) })
  await page.getByRole('button', { name: '保存任务', exact: true }).click()
  await expect.poll(() => fixture.writes.length).toBe(1)
  expect(fixture.writes[0]).toMatchObject({ casInfoMode: 'auto', mode: 'incremental', storageId: 'local' })
  await page.getByText('智能生成', { exact: true }).click()
  await expect(mode).toHaveText('智能计算')
  await page.keyboard.press('Escape')
  await page.getByText('旧默认任务', { exact: true }).click()
  await expect(mode).toHaveText('智能计算')
  await page.keyboard.press('Escape')
  await page.getByText('明确全计算', { exact: true }).click()
  await expect(mode).toHaveText('全计算')
  await page.getByRole('button', { name: '选择目录', exact: true }).click()
  const picker = page.getByRole('dialog', { name: '选择存储目录', exact: true })
  await picker.locator('.source-accounts button').filter({ hasText: '移动' }).click()
  await picker.getByRole('button', { name: '选择当前目录', exact: true }).click()
  await expect(mode).toHaveText('全计算')
  await page.keyboard.press('Escape')
  expect(fixture.unexpected).toEqual([])
})

test('task notices identify the started task and preserve named conflict detail', async ({ page }) => {
  const fixture = await setup(page)
  await page.goto('/tasks/cas')
  await page.locator('.task-row-card').filter({ hasText: '旧默认任务' }).getByRole('button', { name: '立即执行', exact: true }).click()
  await expect(page.locator('.toast-stack')).toContainText('旧默认任务')
  await page.locator('.task-row-card').filter({ hasText: '明确全计算' }).getByRole('button', { name: '立即执行', exact: true }).click()
  await expect(page.locator('.toast-stack')).toContainText('STRM 任务「剧集生成」正在使用重叠目录')
  expect(fixture.unexpected).toEqual([])
})
