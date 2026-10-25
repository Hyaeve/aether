import { test, expect } from '@playwright/test'

const record = (id, kind, from, to) => ({
  id, kind, name: `${id}.mkv`, storage: '旧字段不应代替目录', source: '浏览器',
  from, to, status: 'running', done: 25, total: 100, speed: 4096, started: '2026-10-24T01:00:00Z'
})
const location = (storage, type, directory, path) => ({ storage, type, directory, path })

async function setup(page) {
  const entries = [
    record('copy', 'copy', location('115影视', '115', '源目录', '/电影/源目录'), location('夸克备份', 'quark', '目标目录', '/备份/目标目录')),
    record('upload', 'upload', location('浏览器', 'browser', '', ''), location('本地媒体', 'local', '剧集', '/媒体/剧集')),
    record('download', 'download', location('移动影视', 'mobile', '电影', '/收藏/电影'), location('WebDAV 客户端', 'client', '', '')),
    { id:'legacy', kind:'copy', name:'历史记录.mkv', storage:'旧存储', source:'浏览器', status:'failed', total:0, done:0, started:'2026-10-24T00:00:00Z' }
  ]
  await page.route('**/api/**', route => {
    const path = new URL(route.request().url()).pathname
    const json = path === '/api/auth/status' ? { initialized:true, authenticated:true }
      : path === '/api/state' ? { username:'transfer-locations', storages:[], tasks:[], settings:{}, cache:{}, traffic:{} }
      : path === '/api/transfers' ? entries : {}
    return route.fulfill({ json })
  })
}

for (const width of [1440, 701, 700, 390]) for (const theme of ['light', 'dark']) {
  test(`transfer endpoints stay visible and searchable ${width} ${theme}`, async ({ page }, info) => {
    await setup(page)
    await page.setViewportSize({ width, height:950 })
    await page.goto('/transfer')
    await page.evaluate(value => document.documentElement.dataset.theme=value, theme)
    const row=page.locator('.transfer-table tr').filter({hasText:'copy.mkv'})
    await expect(row.locator('.transfer-from')).toContainText('115影视')
    await expect(row.locator('.transfer-from')).toContainText('源目录')
    await expect(row.locator('.transfer-to')).toContainText('夸克备份')
    await expect(row.locator('.transfer-to')).toContainText('目标目录')
    await expect(row.locator('.transfer-started')).toHaveText(/^26\/10\/24 \d{2}:00$/)
    await expect(row.locator('.transfer-from [data-provider="115"]')).toBeVisible()
    await expect(row.locator('.transfer-to [data-provider="quark"]')).toBeVisible()
    for (const side of ['.transfer-from','.transfer-to']) {
      await expect(row.locator(side)).toBeVisible()
      expect(await row.locator(side).evaluate(el=>el.getBoundingClientRect().width)).toBeGreaterThan(80)
      expect(await row.locator(side).evaluate(el=>el.scrollHeight<=el.clientHeight)).toBe(true)
    }
    expect(await row.evaluate(el=>el.getBoundingClientRect().height)).toBe(width<=700?160:64)
    if(width<=700) {
      const widths=await row.evaluate(el=>({row:el.getBoundingClientRect().width,progress:el.querySelector('.transfer-progress').getBoundingClientRect().width}))
      expect(widths.progress).toBeGreaterThan(widths.row*.9)
    }
    const legacy=page.locator('.transfer-table tr').filter({hasText:'历史记录.mkv'})
    await expect(legacy.locator('.transfer-from')).toContainText('目录未提供')
    await expect(legacy.locator('.transfer-from')).not.toContainText('根目录')
    await page.getByRole('textbox',{name:'搜索传输任务',exact:true}).fill('目标目录')
    await expect(row).toBeVisible()
    await expect(legacy).toHaveCount(0)
    await page.getByRole('textbox',{name:'搜索传输任务',exact:true}).fill('/备份/')
    await expect(row).toBeVisible()
    await page.getByRole('textbox',{name:'搜索传输任务',exact:true}).fill('')
    expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
    await page.screenshot({path:info.outputPath(`endpoints-${width}-${theme}.png`)})
    await page.getByRole('tab',{name:/^上传/}).click()
    const upload=page.locator('.transfer-table tr').filter({hasText:'upload.mkv'})
    await expect(upload.locator('.transfer-from')).toContainText('浏览器')
    await expect(upload.locator('.transfer-from')).toContainText('目录不可获取')
    await expect(upload.locator('.transfer-to')).toContainText('剧集')
    await page.getByRole('tab',{name:/^下载/}).click()
    const download=page.locator('.transfer-table tr').filter({hasText:'download.mkv'})
    await expect(download.locator('.transfer-from')).toContainText('移动影视')
    await expect(download.locator('.transfer-to')).toContainText('WebDAV 客户端')
    await expect(download.locator('.transfer-to')).toContainText('目录不可获取')
  })
}

test('long endpoint names stay in their cells and mobile virtual rows keep their geometry', async ({page}, info) => {
  await setup(page)
  const name='这是非常长但必须保留最深目录含义的影片文件夹'
  const entries=Array.from({length:200},(_,index)=>record(`row-${index}`,'copy',
    location('长名称来源存储池'.repeat(5),'115',name,`/祖先目录/${name}`),
    location('长名称目标存储池'.repeat(5),'quark',name,`/备份目录/${name}`)))
  await page.route('**/api/transfers',route=>route.fulfill({json:entries}))
  await page.setViewportSize({width:390,height:844})
  await page.goto('/transfer')
  const viewport=page.locator('.transfer-viewport')
  expect(await page.locator('.transfer-table tbody tr:not(.transfer-spacer)').count()).toBeLessThan(25)
  const sides=page.locator('.transfer-table tbody tr:not(.transfer-spacer)').first().locator('.transfer-from,.transfer-to')
  for(const side of await sides.all()){
    expect(await side.evaluate(el=>el.scrollWidth<=el.clientWidth)).toBe(true)
    const text=side.locator('.transfer-location small>span')
    await expect(text).toHaveText(name)
    expect(await text.evaluate(el=>el.scrollWidth>el.clientWidth)).toBe(true)
  }
  await viewport.evaluate(el=>{el.scrollTop=el.scrollHeight})
  await expect(page.locator('.transfer-table tbody')).toContainText('row-199.mkv')
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
  const rows=page.locator('.transfer-table tbody tr:not(.transfer-spacer)')
  const bounds=await rows.evaluateAll(elements=>elements.map(el=>{const r=el.getBoundingClientRect();return {height:r.height,top:r.top,bottom:r.bottom}}))
  expect(bounds.every(b=>b.height===160)).toBe(true)
  expect(bounds.slice(1).every((b,index)=>Math.abs(b.top-bounds[index].bottom)<1)).toBe(true)
  await page.screenshot({path:info.outputPath('mobile-long-endpoints.png')})
  await page.setViewportSize({width:1440,height:950})
  await expect.poll(()=>rows.first().evaluate(el=>el.getBoundingClientRect().height)).toBe(64)
})
