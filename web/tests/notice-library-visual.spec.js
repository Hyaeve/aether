import { test, expect } from '@playwright/test'

test('task unread badge follows the page completion poll and keeps read state after reload',async({page})=>{
  const now=new Date('2026-11-05T12:00:00Z'),unexpected=[]
  const task={id:'completed-poll',name:'影视同步',kind:'strm',status:'running',lastRun:now.toISOString(),processed:0}
  await page.clock.install({time:now})
  await page.route('**/api/**',route=>{
    const r=route.request(),p=new URL(r.url()).pathname
    if(p==='/api/auth/status')return route.fulfill({json:{initialized:true,authenticated:true}})
    if(p==='/api/state')return route.fulfill({json:{username:'poll-notice',storages:[],tasks:[task],settings:{},cache:{},traffic:{}}})
    if(p==='/api/traffic')return route.fulfill({json:{}})
    if(p==='/api/strm-replace')return route.fulfill({json:[]})
    unexpected.push(`${r.method()} ${p}`);return route.fulfill({status:500,json:{error:'Unexpected '+p}})
  })
  await page.goto('/storage')
  await expect(page.getByRole('button',{name:'任务通知',exact:true})).toBeVisible()
  await expect(page.locator('.notification-badge')).toHaveCount(0)
  Object.assign(task,{status:'success',processed:1,message:'新增 1'})
  const completion=page.waitForResponse(async r=>new URL(r.url()).pathname==='/api/state'&&r.ok()&&(await r.json()).tasks?.[0]?.status==='success')
  await page.clock.runFor(5000);await completion
  await expect(page.locator('.notification-badge')).toHaveText('1')
  await page.getByRole('button',{name:'任务通知',exact:true}).click()
  await expect(page.locator('.notification-dropdown')).toContainText('影视同步')
  await expect(page.locator('.notification-dropdown')).toContainText('已处理 1 项')
  await expect(page.locator('.notification-badge')).toHaveCount(0)
  expect(await page.evaluate(()=>JSON.parse(localStorage.getItem('aether-read:poll-notice')))).toEqual([`completed-poll:${now.toISOString()}:success`])
  await page.reload()
  await expect(page.getByRole('button',{name:'任务通知',exact:true})).toBeVisible()
  await expect(page.locator('.notification-badge')).toHaveCount(0)
  await page.getByRole('button',{name:'任务通知',exact:true}).click()
  await expect(page.locator('.notification-dropdown')).toContainText('影视同步')
  expect(unexpected).toEqual([])
})

test('notifications identify library actions and provider logos fill their bounds', async ({ page }, info) => {
	await page.route('**/api/115-recycle-cleanup', r => r.fulfill({ json: { enabled: false, rules: [] } }))
	await page.route('**/api/115-simulcast/enabled', r => r.fulfill({ json: { enabled: true } }))
	await page.route('**/api/115-folder-cleanup', r => r.fulfill({ json: { enabled: false, rules: [] } }))
	await page.route('**/api/115-signin', r => r.fulfill({ json: { enabled: false, rules: [] } }))
  await page.route('**/api/plugins/**', r => r.fulfill({ json: {} }))
  await page.route('**/api/quark-takeover', r => r.fulfill({ json: { enabled: false, bindings: [] } }))
  await page.route('**/api/auth/status', r => r.fulfill({ json: { initialized: true, authenticated: true } }))
  await page.route('**/api/state', r => r.fulfill({ json: { storages: [{ id: 'pan', type: '115', name: '115', enabled: true, config: {} }], tasks: [{ id: 'cache', kind: 'cache', storageId: 'pan', name: '目录扫描', status: 'success', lastRun: new Date().toISOString() }], settings: {}, cache: {}, libraryNotices: [{ id: 'event', libraryName: '家庭电影库', name: '降临', mediaType: 'movie', event: 'playback.start', time: new Date().toISOString() }] } }))
  await page.goto('/tools')
  await page.getByRole('button', { name: '任务通知', exact: true }).click()
  await expect(page.locator('.notification-list strong').filter({ hasText: '家庭电影库' })).toHaveText('家庭电影库')
  await expect(page.locator('.notification-list')).toContainText('开始播放 · 降临')
  await expect(page.locator('.notification-list')).not.toContainText('电影 · 降临')
  await expect(page.locator('.notice-provider .provider-logo')).toHaveCSS('width', '36px')
  await expect(page.locator('.notification-list .emby-notice-icon > img')).toHaveCSS('width', '36px')
  for (const logo of await page.locator('.simulcast-symbol .provider-logo').all()) await expect(logo).toHaveCSS('width', '44px')
  await page.screenshot({ path: info.outputPath('library-notice.png') })
})

test('scheduled task notification uses library title and concise task completion', async ({ page }) => {
  await page.route('**/api/auth/status', r => r.fulfill({ json: { initialized: true, authenticated: true } }))
  await page.route('**/api/state', r => r.fulfill({ json: { storages: [], tasks: [], settings: {}, cache: {}, libraryNotices: [{ id: 'scheduled', serverName: '月见草', libraryName: '电影库', name: '媒体库扫描', event: 'scheduledtasks.completed', time: new Date().toISOString() }] } }))
  await page.goto('/storage')
  await page.getByRole('button', { name: '任务通知', exact: true }).click()
  await expect(page.locator('.notification-list strong')).toHaveText('电影库')
  await expect(page.locator('.notification-list small')).toContainText('媒体库扫描 已完成')
  await expect(page.locator('.notification-list')).not.toContainText('scheduledtasks.completed')
})
