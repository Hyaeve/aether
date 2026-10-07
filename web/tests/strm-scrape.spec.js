import { test, expect } from '@playwright/test'

test('STRM scrape workspace scans, rematches and saves settings without shifting tabs', async ({ page }, info) => {
  await page.route('**/api/auth/status', r => r.fulfill({ json: { initialized: true, authenticated: true } }))
  await page.route('**/api/state', r => r.fulfill({ json: { storages: [], tasks: [{ id: 'strm', name: '影片', kind: 'strm' }], settings: {}, traffic: {}, cache: {} } }))
  let progress = { running: false, total: 0, done: 0 }, started = false
  let settings = { writeMode: 'missing', episodes: true, fanart: false, actors: false, excluded: '' }
  const items = Array.from({ length: 1000 }, (_, i) => ({ path: `Film${i}.strm`, title: `Film ${i}`, kind: 'movie', status: 'pending', tmdb: 0 }))
  await page.route('**/api/strm-scrape/**', async r => {
    const action = new URL(r.request().url()).pathname.split('/').at(-1)
    if (action === 'items') return r.fulfill({ json: started ? items : [] })
    if (action === 'settings') {
      if (r.request().method() === 'PUT') settings = r.request().postDataJSON()
      return r.fulfill({ json: settings })
    }
    if (action === 'status') return r.fulfill({ json: progress })
    if (action === 'candidates') return r.fulfill({json:[{id:123,title:'Film 0',year:'2026',poster:'/aether.svg'}]})
    if (action === 'match') {
      const input = r.request().postDataJSON()
      Object.assign(items.find(i => i.path === input.path), { tmdb: input.tmdb, kind: input.kind })
      return r.fulfill({ json: { ok: true } })
    }
    started = true
    const response = { running: true, total: 1000, done: 0, taskId: 'strm' }
    progress = { ...response, running: false, message: '索引已刷新' }
    return r.fulfill({ status: 202, json: response })
  })
  await page.goto('/tasks/cache')
  const before = await page.locator('.content-tabs').boundingBox()
  await page.getByRole('link', { name: 'STRM 刮削', exact: true }).click()
  const after = await page.locator('.content-tabs').boundingBox()
  expect(after.x).toBe(before.x)
  expect(after.y).toBe(before.y)
  await expect(page.getByRole('button', { name: '开始刮削', exact: true })).toBeDisabled()
  await page.getByRole('button', { name: 'STRM 任务', exact: true }).click()
  await page.getByRole('option', { name: '影片', exact: true }).click()
  await page.getByRole('button', { name: '刷新刮削索引', exact: true }).click()
  await expect(page.locator('.scrape-card').first()).toBeVisible()
  expect(await page.locator('.scrape-card').count()).toBeLessThan(100)
  await page.getByRole('button', { name: '重新匹配', exact: true }).first().click()
  await expect(page.locator('.candidate-card img')).toBeVisible()
  const candidateType = await page.locator('.candidate-search .rounded-select').boundingBox()
  const candidateInput = await page.getByRole('textbox',{name:'候选名称'}).boundingBox()
  expect(candidateType.x + candidateType.width).toBeLessThan(candidateInput.x)
  await page.screenshot({path:info.outputPath('scrape-candidates.png')})
  await page.locator('.candidate-card').click()
  await expect(page.getByLabel('TMDB ID')).toHaveValue('123')
  await page.getByRole('button', { name: '确认匹配', exact: true }).click()
  await expect(page.locator('.scrape-card').first()).toContainText('TMDB 123')
  await page.getByRole('button', { name: '刮削设置', exact: true }).click()
  await page.getByRole('button', { name: '写入策略', exact: true }).click()
  await page.getByRole('option', { name: '覆盖已有', exact: true }).click()
  await page.getByLabel('背景图', { exact: true }).check()
  await page.getByRole('button', { name: '保存设置', exact: true }).click()
  await expect.poll(() => settings.fanart).toBe(true)
  for (const theme of ['light', 'dark']) {
    await page.evaluate(t => document.documentElement.dataset.theme = t, theme)
    await page.screenshot({ path: info.outputPath(`scrape-${theme}.png`) })
  }
  await page.setViewportSize({width:390,height:844})
  await page.screenshot({path:info.outputPath('scrape-mobile.png')})
  expect(await page.locator('.scrape-panel').evaluate(el=>el.scrollWidth <= el.clientWidth)).toBe(true)
})
