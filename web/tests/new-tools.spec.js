import { test, expect } from '@playwright/test'

test('new plugins open from tools and STRM replacement reaches activity', async ({ page }, info) => {
  let job = null, configs = {}
  const tasks = []
  await page.route('**/api/**', r => {
    const url = new URL(r.request().url()), method = r.request().method()
    let data = {}
    if (url.pathname === '/api/auth/status') data = { initialized: true, authenticated: true }
    else if (url.pathname === '/api/state') data = { username: 'new-tools', storages: [{ id: 'pan', name: '115 影视', type: '115', enabled: true, config: {} }], tasks, settings: {}, cache: {}, libraryNotices: [], logs: [] }
    else if (url.pathname === '/api/quark-takeover') data = { bindings: [], enabled: false }
    else if (url.pathname === '/api/115-simulcast') {
      if (method === 'PUT') configs = r.request().postDataJSON()
      data = configs
    } else if (url.pathname === '/api/strm-replace') {
      if (method === 'POST') {
        const body = r.request().postDataJSON()
        expect(body.confirmed).toBe(true)
        expect(body.find).toBe('old.example')
        job = { id: 'replace', status: 'running', changed: 0, processed: 0, scanned: 0, time: new Date().toISOString() }
        data = job
      } else data = { tasks: job ? [job] : [] }
    }
    return r.fulfill({ json: data })
  })
  await page.goto('/tools')
  await expect(page.locator('.plugin-card').first()).toHaveAttribute('aria-label', '115 同播复制')
  await page.getByRole('button', { name: '115 同播复制', exact: true }).click()
  await expect(page.getByRole('heading', { name: '115 同播复制', exact: true })).toBeVisible()
  await page.keyboard.press('Escape')
  await page.getByRole('button', { name: 'STRM 替换', exact: true }).click()
  await page.getByLabel('容器目录', { exact: true }).fill('/media')
  await page.getByLabel('匹配字段', { exact: true }).fill('old.example')
  await page.getByLabel('替换字段', { exact: true }).fill('new.example')
  await page.getByRole('button', { name: '执行替换', exact: true }).click()
  await page.getByRole('button', { name: '确认替换', exact: true }).click()
  await expect(page.getByRole('heading', { name: '确认替换 STRM 内容', exact: true })).toHaveCount(0)
  await page.locator('.modal-footer').getByRole('button', { name: '关闭', exact: true }).click()
  await page.getByRole('button', { name: '任务通知', exact: true }).click()
  await expect(page.locator('.notification-list')).toContainText('STRM 替换')
  job = { ...job, status: 'completed', changed: 7, processed: 9, updatedAt: new Date().toISOString() }
  await expect(page.locator('.notification-list')).toContainText('已替换 7 个文件', { timeout: 10000 })
  await page.screenshot({ path: info.outputPath('replacement-activity.png') })
})
