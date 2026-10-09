import { test, expect } from '@playwright/test'

for (const succeeds of [true, false]) {
  test(`115 share ${succeeds ? 'submission navigates to remembered destination' : 'preview failure stays in current directory'}`, async ({ page }) => {
    await page.route('**/api/auth/status', r => r.fulfill({ json: { initialized: true, authenticated: true } }))
    await page.route('**/api/state', r => r.fulfill({ json: { username: 'share-owner', storages: [{ id: 'p115', type: '115', name: '115', enabled: true, config: {} }], tasks: [], settings: {}, traffic: {}, cache: {} } }))
    const requests = []
    await page.route('**/api/files?**', r => {
      const query = new URL(r.request().url()).searchParams
      requests.push({ path: query.get('path'), refresh: query.get('refresh') })
      const items = query.get('path') === '/' ? [{ id: 'parent', name: '转存库', isDir: true }] : query.get('path') === 'parent' ? [{ id: 'destination', name: '电影', isDir: true }] : [{ id: 'result', name: '已转存.mkv', isDir: false }]
      return r.fulfill({ json: items })
    })
    let submissions = 0
    await page.route('**/api/files/share/*', r => {
      if (r.request().url().endsWith('/preview')) return r.fulfill(succeeds ? { json: { preview: 'p', batches: 1 } } : { status: 502, json: { error: '115 分享预览失败 HTTP 405' } })
      submissions++
      expect(r.request().postDataJSON().parent).toBe('destination')
      return r.fulfill({ json: { status: 'submitted' } })
    })
    await page.goto('/files?storage=p115')
    await page.getByRole('button', { name: '工具', exact: true }).click()
    await page.getByRole('menuitem', { name: '分享转存', exact: true }).click()
    await page.getByRole('button', { name: '选择转存目录', exact: true }).click()
    const picker = page.getByRole('dialog', { name: '选择存储目录', exact: true })
    await picker.getByRole('button', { name: '转存库', exact: true }).click()
    await picker.getByRole('button', { name: '电影', exact: true }).click()
    await picker.getByRole('button', { name: '选择当前目录', exact: true }).click()
    await page.getByLabel('分享链接', { exact: true }).fill('https://115.com/s/test?password=1234')
    await page.getByRole('button', { name: '开始转存', exact: true }).click()
    await expect(page.getByRole('dialog')).toHaveCount(0)
    if (succeeds) {
      await expect(page.locator('.path-bar [aria-current=location]')).toHaveText('电影')
      await expect(page.getByRole('button', { name: '已转存.mkv', exact: true })).toBeVisible()
      expect(requests.at(-1)).toEqual({ path: 'destination', refresh: 'true' })
      await page.locator('.path-bar').getByRole('button', { name: '转存库', exact: true }).click()
      await expect(page.locator('.path-bar [aria-current=location]')).toHaveText('转存库')
      expect(requests.at(-1).path).toBe('parent')
      await page.locator('.path-bar').getByRole('button', { name: '根目录', exact: true }).click()
      await expect(page.getByRole('button', { name: '转存库', exact: true })).toBeVisible()
      expect(requests.at(-1).path).toBe('/')
    } else {
      await expect(page.locator('.toast.error')).toContainText('HTTP 405')
      await expect(page.locator('.path-bar [aria-current=location]')).toHaveText('根目录')
    }
    expect(submissions).toBe(succeeds ? 1 : 0)
  })
}

test('share transfer previews selection, submits once and reports confirmed Quark completion', async ({ page }, info) => {
  const storages = ['115', 'mobile', 'quark'].map(type => ({ id: type, type, name: type, enabled: true, config: { mode: 'native' } }))
  await page.route('**/api/auth/status', r => r.fulfill({ json: { initialized: true, authenticated: true } }))
  await page.route('**/api/state', r => r.fulfill({ json: { storages, tasks: [], settings: {}, traffic: {}, cache: {} } }))
  let refreshes = 0, saves = 0, payload
  await page.route('**/api/files?**', r => { refreshes++; return r.fulfill({ json: [] }) })
  await page.route('**/api/files/share/*', r => {
    const action = new URL(r.request().url()).pathname.split('/').at(-1)
    if (action === 'preview') return r.fulfill({ json: { preview: 'private-session', batches: 1, items: [{ id: 'film', name: 'Arrival.2016.mkv', isDir: false }, { id: 'show', name: 'Show', isDir: true }] } })
    if (action === 'status') return r.fulfill({ json: { status: 'completed', message: '网盘已完成转存', taskId: 'job' } })
    saves++; payload = r.request().postDataJSON()
    return r.fulfill({ json: { status: 'submitted', message: '转存已提交，请刷新目标目录确认结果', taskId: 'job' } })
  })
  await page.goto('/files?storage=quark')
  await page.getByRole('button', { name: '工具', exact: true }).click()
  await page.getByRole('menuitem', { name: '分享转存', exact: true }).click()
  await expect(page.getByRole('button', { name: '转存目标存储', exact: true })).toHaveCount(0)
  await expect(page.getByLabel('提取码', { exact: true })).toHaveCount(0)
  await expect(page.getByLabel('分享链接', { exact: true })).toHaveAttribute('placeholder', '夸克网盘分享链接，一行一条')
  await page.getByLabel('分享链接', { exact: true }).fill('https://pan.quark.cn/s/abc\nhttps://pan.quark.cn/s/def')
  for (const theme of ['light', 'dark']) {
    await page.evaluate(t => document.documentElement.dataset.theme = t, theme)
    await page.screenshot({ path: info.outputPath(`share-${theme}.png`) })
  }
  await page.setViewportSize({ width: 390, height: 844 })
  await page.screenshot({ path: info.outputPath('share-mobile.png') })
  expect(await page.getByRole('dialog').evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
  await page.getByRole('button', { name: '开始转存', exact: true }).click()
  await expect(page.locator('.toast')).toContainText('转存已完成')
  expect(saves).toBe(2)
  expect(payload).toEqual({ storageId: 'quark', preview: 'private-session', parent: '/', batch: 0 })
  expect(refreshes).toBeGreaterThan(1)
  await expect(page.getByRole('button', { name: '确认转存', exact: true })).toHaveCount(0)
})

test('task provider logos fill their button without an extra colored box', async ({ page }, info) => {
  const storages = ['115', 'mobile', 'quark'].map(type => ({ id: type, type, name: type, enabled: true, config: {} }))
  await page.route('**/api/auth/status', r => r.fulfill({ json: { initialized: true, authenticated: true } }))
  await page.route('**/api/state', r => r.fulfill({ json: { storages, tasks: storages.map(s => ({ id:s.id, storageId:s.id, name:s.name, kind:'strm', enabled:true, processed:0 })), settings:{}, traffic:{}, cache:{} } }))
  await page.goto('/tasks/strm')
  for (const icon of await page.locator('.task-provider-toggle .provider-icon').all()) {
    const logo = await icon.locator('img').boundingBox(), box = await icon.boundingBox()
    expect(logo.width).toBe(42)
    expect(logo.width).toBe(box.width)
    await expect(icon).toHaveCSS('background-color', 'rgba(0, 0, 0, 0)')
  }
  await page.screenshot({ path: info.outputPath('task-provider-icons.png') })
})
