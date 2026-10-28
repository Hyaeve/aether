import { test, expect } from '@playwright/test'
import { captureScreenshot } from './helpers/screenshot.js'

const original = `Original.Show.S01E01.${'BluRay.1080p.'.repeat(18)}strm`
async function fixture(page) {
  const calls = []
  const records = [
    { id:'one',taskId:'t',taskName:'剧集库',path:`Show/${original}`,filename:original,title:'识别的剧集',kind:'tv',tmdb:1399,poster:'/providers/quark.png',trigger:'manual',action:'scrape',status:'success',message:'元数据已写入',startedAt:'2026-10-27T08:10:00Z',finishedAt:'2026-10-27T08:10:02Z' },
    { id:'two',taskId:'t',taskName:'剧集库',path:'Show/two.strm',filename:'two.strm',title:'存疑作品',kind:'movie',trigger:'manual',action:'identify',status:'doubt',message:'存在多个同名匹配',startedAt:'2026-10-27T08:10:00Z',finishedAt:'2026-10-27T08:10:02Z' },
    { id:'three',taskId:'t',taskName:'剧集库',path:'Failed/one.strm',filename:'one.strm',title:'识别失败',kind:'movie',trigger:'monitor',action:'scrape',status:'failed',message:'读取失败',startedAt:'2026-10-27T08:10:00Z',finishedAt:'2026-10-27T08:10:02Z' }
  ]
  await page.addInitScript(() => localStorage.setItem('aether-files:archives-ui', JSON.stringify({open:true})))
  await page.route('**/api/**', route => {
    const r = route.request(), u = new URL(r.url()), p = u.pathname
    let json = {}
    if (p === '/api/auth/status') json = {initialized:true,authenticated:true}
    else if (p === '/api/state') json = {username:'archives-ui',storages:[{id:'pool',name:'本地存储',type:'local',enabled:true,config:{}}],tasks:[{id:'t',kind:'strm',name:'剧集库'}],settings:{},fileFavorites:[],cache:{},traffic:{}}
    else if (p === '/api/archives/scrape') {
      calls.push(u.search)
      json = {items:records,total:3,page:1,pages:1,pageSize:30}
      if (u.searchParams.get('mode') === 'group') json = {items:[{id:'g',title:'识别的剧集',kind:'tv',tmdb:1399,poster:'/providers/quark.png',taskName:'剧集库',folder:'Show',count:2,status:'doubt'}],total:1,page:1,pages:1,pageSize:30}
      if (u.searchParams.get('group')) json = {items:records.slice(0,2),total:2,page:1,pages:1,pageSize:30}
    } else if (p === '/api/archives/tasks') json = {items:[{id:'task-run',taskId:'t',taskName:'剧集库',taskKind:'strm',trigger:'scheduled',status:'success',processed:20,startedAt:'2026-10-27T08:10:00Z',finishedAt:'2026-10-27T08:12:00Z',message:'执行完成，处理20项'}],total:1,page:1,pages:1,pageSize:30}
    else if (p.endsWith('/retry')) { calls.push(r.postDataJSON()); json = {running:true} }
    else if (p === '/api/strm-scrape/status') json = {running:false}
    else if (p === '/api/strm-scrape/items') json = [{path:'Show/Season 1/one.strm',directories:['Show/Season 1'],title:'猜测剧名',matchedTitle:'标准剧名',kind:'tv',status:'ok',tmdb:1399}]
    else if (p === '/api/strm-scrape/files') json = [{id:'Show',name:'Show',isDir:true}]
    else if (p === '/api/strm-scrape/directories') json = {root:'/media',directories:['Show']}
    else if (p === '/api/files') json = []
    else if (['/api/generation/jobs','/api/logs','/api/115-recycle-cleanup','/api/strm-replace/jobs'].includes(p)) json = []
    return route.fulfill({json})
  })
  return calls
}

test('my plugins uses the reference blocks icon without renaming its sidebar', async ({page},info) => {
  await fixture(page); await page.goto('/tools')
  await expect(page.getByRole('navigation',{name:'主导航'}).getByRole('link',{name:'辅助工具',exact:true})).toBeVisible()
  const tab = page.getByRole('navigation',{name:'辅助工具栏目'}).getByRole('link',{name:'我的插件',exact:true})
  await expect(tab).toBeVisible()
  await expect(tab.locator('svg')).toHaveClass(/lucide-blocks/)
  await expect(page.locator('.breadcrumbs [aria-current=page]')).toHaveText('我的插件')
  await captureScreenshot(page,{path:info.outputPath('my-plugins.png')})
})

for (const width of [1440,375]) for (const theme of ['light','dark']) {
  test(`archives list, groups and task history ${width} ${theme}`, async ({page},info) => {
    const calls = await fixture(page)
    await page.setViewportSize({width,height:1000}); await page.goto('/archives/scrape')
    await page.evaluate(value => document.documentElement.dataset.theme=value,theme)
    if(width<760) await page.getByRole('button',{name:'打开导航',exact:true}).click()
    const links=page.getByRole('navigation',{name:'主导航'}).getByRole('link')
    const names=await links.allTextContents()
    expect(names.indexOf('档案中心')).toBe(names.indexOf('辅助工具')+1)
    expect(names.indexOf('档案中心')).toBe(names.indexOf('通知服务')-1)
    await expect(page.locator('.breadcrumbs strong')).toHaveText('档案中心')
    await expect(page.getByRole('navigation',{name:'档案中心栏目',exact:true}).getByRole('link')).toHaveText(['整理档案','刮削档案','任务档案'])
    if(width<760) await page.locator('.nav-overlay').click({position:{x:width-5,y:400}})
    await expect(page.locator('.archive-row')).toHaveCount(3)
    await expect(page.locator('.archive-row').first()).toContainText('识别的剧集')
    await expect(page.locator('.archive-status.success')).toContainText('成功')
    await expect(page.locator('.archive-status.doubt')).toContainText('存疑')
    await expect(page.locator('.archive-status.failed')).toContainText('失败')
    await expect(page.locator('.archive-row').last()).toContainText('监控执行')
    await expect(page.locator('.archive-row').last().locator('.archive-poster img')).toHaveCount(0)
    await captureScreenshot(page,{path:info.outputPath(`archives-list-${width}-${theme}.png`)})
    await page.getByRole('button',{name:'分组模式',exact:true}).click()
    await expect(page.locator('.archive-group-toggle')).toContainText('识别的剧集')
    await page.locator('.archive-group-toggle').click()
    await expect(page.locator('.archive-group .archive-row')).toHaveCount(2)
    await captureScreenshot(page,{path:info.outputPath(`archives-group-${width}-${theme}.png`)})
    await page.locator('.archive-group .archive-row').first().getByRole('button',{name:'重新识别刮削',exact:true}).click()
    await expect(page.getByRole('dialog')).toContainText('重新识别')
    await page.getByRole('button',{name:'确认识别',exact:true}).click()
    await expect.poll(()=>calls.some(c=>typeof c==='object'&&c.confirmed)).toBe(true)
    await page.getByRole('link',{name:'任务档案',exact:true}).click()
    await expect(page.locator('.archive-task-row')).toContainText('剧集库')
    await expect(page.locator('.archive-task-row')).toContainText('计划执行')
    await captureScreenshot(page,{path:info.outputPath(`archives-task-${width}-${theme}.png`)})
    await page.getByRole('link',{name:'整理档案',exact:true}).click()
    await expect(page.locator('.archive-empty')).toContainText('暂未实装')
    expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
    await captureScreenshot(page,{path:info.outputPath(`archives-empty-${width}-${theme}.png`)})
  })
}

test('archive filename tooltip requires 300ms stationary hover and cancels on movement', async ({page}) => {
  await fixture(page); await page.goto('/archives/scrape')
  const name=page.locator('.archive-filename').first()
  await expect(name).toBeVisible()
  expect(await name.evaluate(el=>el.scrollWidth>el.clientWidth)).toBe(true)
  await page.clock.install()
  await page.clock.pauseAt(new Date())
  await name.hover()
  await page.clock.runFor(299)
  await expect(page.getByRole('tooltip')).toHaveCount(0)
  await page.clock.runFor(1)
  await expect(page.getByRole('tooltip')).toHaveText(original)
  const r=await name.boundingBox()
  await page.mouse.move(r.x+10,r.y+5)
  await expect(page.getByRole('tooltip')).toHaveCount(0)
  await page.clock.runFor(299)
  await expect(page.getByRole('tooltip')).toHaveCount(0)
  await page.clock.runFor(1)
  await expect(page.getByRole('tooltip')).toHaveText(original)
  await page.keyboard.press('Escape')
  await expect(page.getByRole('tooltip')).toHaveCount(0)
  await page.clock.resume()
})

for(const width of [1440,768,375])test(`favorites and storage selector share the widened right edge ${width}`,async({page},info)=>{
  await fixture(page);await page.setViewportSize({width,height:1000});await page.goto('/files')
  await expect(page.locator('.file-favorites')).toBeVisible()
  const layout=await page.locator('.file-browser').evaluate(el=>{
    const a=el.querySelector('.file-favorites').getBoundingClientRect(),b=el.querySelector('.file-pool-select').getBoundingClientRect()
    return {width:a.width,edge:Math.abs(a.right-b.right)}
  })
  expect(layout.edge).toBeLessThan(1)
  expect(layout.width).toBeGreaterThan(width<=760?134:176)
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
  for(const theme of ['light','dark']) {
    await page.evaluate(value=>document.documentElement.dataset.theme=value,theme)
    await captureScreenshot(page,{path:info.outputPath(`favorites-${width}-${theme}.png`)})
  }
})

test('scrape list identifies recognized folders with canonical title and TMDB id',async({page},info)=>{
  await fixture(page);await page.goto('/tasks/scrape')
  await page.getByRole('button',{name:'海报墙视图',exact:true}).click()
  const row=page.locator('.scrape-file-row').first()
  await expect(row.locator('.scrape-folder-identity')).toHaveText('剧集标准剧名TMDB 1399')
  await expect(row).toHaveClass(/recognized/)
  await expect(row.getByRole('button',{name:'Show',exact:true})).toHaveAttribute('aria-description','剧集，标准剧名，TMDB 1399')
  await captureScreenshot(page,{path:info.outputPath('recognized-folder.png')})
})

test('archive pagination, search and sidebar memory preserve their own section',async({page})=>{
  await fixture(page)
  const requests=[]
  await page.route('**/api/archives/scrape?**',route=>{
    const u=new URL(route.request().url()),requested=Number(u.searchParams.get('page')||1)
    requests.push(u)
    const pageNumber=Math.min(3,Math.max(1,requested))
    return route.fulfill({json:{items:[{id:'file',taskId:'t',title:'作品',filename:`第${pageNumber}页.strm`,kind:'movie',status:'success',trigger:'manual',startedAt:'2026-10-27T00:00:00Z'}],total:65,page:pageNumber,pages:3}})
  })
  await page.goto('/archives/scrape')
  await page.getByRole('button',{name:'最后一页',exact:true}).click()
  await expect(page.locator('.archive-filename')).toHaveText('第3页.strm')
  await page.getByRole('spinbutton',{name:'跳转页码',exact:true}).fill('999')
  await page.getByRole('button',{name:'跳转到指定页',exact:true}).click()
  await expect(page.locator('.archive-pagination input')).toHaveValue('3')
  await page.getByRole('textbox',{name:'搜索档案'}).fill('原文件名称')
  await expect.poll(()=>requests.at(-1).searchParams.get('q')).toBe('原文件名称')
  await expect(page.locator('.archive-filename')).toHaveText('第1页.strm')
  await page.getByRole('navigation',{name:'主导航'}).getByRole('link',{name:'辅助工具',exact:true}).click()
  await page.getByRole('navigation',{name:'主导航'}).getByRole('link',{name:'档案中心',exact:true}).click()
  await expect(page).toHaveURL(/\/archives\/scrape$/)
  await expect(page.locator('.breadcrumbs [aria-current=page]')).toHaveText('刮削档案')
})

test('retry is confirmed, preserves errors in the dialog and respects the scraper lock',async({page})=>{
  await fixture(page);let submissions=0
  await page.route('**/api/archives/scrape/one/retry',r=>{
    submissions++
    expect(r.request().postDataJSON()).toEqual({confirmed:true})
    return r.fulfill({status:409,json:{error:'原 STRM 文件已变化'}})
  })
  await page.goto('/archives/scrape')
  await page.locator('.archive-row').first().getByRole('button',{name:'重新识别刮削',exact:true}).click()
  expect(submissions).toBe(0)
  await page.getByRole('button',{name:'确认识别',exact:true}).click()
  await expect(page.locator('.toast')).toContainText('原 STRM 文件已变化')
  await expect(page.getByRole('dialog')).toBeVisible()
  await page.getByRole('button',{name:'取消',exact:true}).click()
  await page.route('**/api/strm-scrape/status',r=>r.fulfill({json:{running:true,taskId:'t'}}))
  await page.getByRole('button',{name:'刷新档案',exact:true}).click()
  await expect(page.locator('.archive-row').first().getByRole('button',{name:'重新识别刮削',exact:true})).toBeDisabled()
  expect(submissions).toBe(1)
})

test('unverified and conflicting folder matches do not claim a canonical identity',async({page})=>{
  await fixture(page)
  await page.route('**/api/strm-scrape/items?**',r=>r.fulfill({json:[
    {path:'Show/one.strm',directories:['Show'],title:'猜测名称',kind:'tv',status:'pending',tmdb:1},
    {path:'Mixed/one.strm',directories:['Mixed'],title:'作品一',kind:'movie',status:'ok',tmdb:2},
    {path:'Mixed/two.strm',directories:['Mixed'],title:'作品二',kind:'movie',status:'ok',tmdb:3}
  ]}))
  await page.route('**/api/strm-scrape/files?**',r=>r.fulfill({json:[{id:'Show',name:'Show',isDir:true},{id:'Mixed',name:'Mixed',isDir:true}]}))
  await page.goto('/tasks/scrape')
  await page.getByRole('button',{name:'海报墙视图',exact:true}).click()
  await expect(page.locator('.scrape-file-row')).toHaveCount(2)
  await expect(page.locator('.scrape-folder-identity')).toHaveCount(0)
  await expect(page.locator('.scrape-folder-actions')).toHaveCount(2)
})

test('automation card history uses completed steps rather than file counts',async({page})=>{
  await fixture(page)
  await page.route('**/api/archives/tasks?**',r=>r.fulfill({json:{items:[{id:'automation',taskKind:'automation',taskName:'整理联动',processed:2,trigger:'manual',status:'success',startedAt:'2026-10-27T00:00:00Z'}],page:1,pages:1,total:1}}))
  await page.goto('/archives/tasks')
  await expect(page.locator('.archive-task-row')).toContainText('联动 · 2 步')
  await expect(page.locator('.archive-task-row')).toContainText('手动操作')
})

test.describe('archives narrow touch landscape',()=>{
  test.use({viewport:{width:844,height:390},hasTouch:true,isMobile:true,deviceScaleFactor:2,reducedMotion:'reduce'})
  test('groups remain operable in short touch viewports',async({page},info)=>{
    await fixture(page);await page.goto('/archives/scrape')
    await page.getByRole('button',{name:'分组模式',exact:true}).click()
    await page.locator('.archive-group-toggle').click()
    await expect(page.locator('.archive-group .archive-row')).toHaveCount(2)
    expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
    await captureScreenshot(page,{path:info.outputPath('archive-touch-landscape.png')})
  })
})
