import { test, expect } from '@playwright/test'

async function mock(page, extra = {}) {
  await page.route('**/api/**', r => {
    const path = new URL(r.request().url()).pathname
    const json = path === '/api/auth/status' ? {initialized:true, authenticated:true} : path === '/api/state' ? {username:'surface-revision',storages:[],tasks:[],settings:{},traffic:{},cache:{},...extra} : path.endsWith('/items') || path === '/api/files' ? [] : {}
    return r.fulfill({json})
  })
}

test('history excludes root and first-level visits, and uses the clock arrow button', async ({page}, info) => {
  await mock(page, {storages:[{id:'pool',name:'云盘',type:'quark',enabled:true,config:{}}]})
  await page.route('**/api/files?**', r => {
    const id = new URL(r.request().url()).searchParams.get('path')
    return r.fulfill({json:id === 'a' ? [{id:'b',name:'B',isDir:true}] : id === 'b' ? [] : [{id:'a',name:'A',isDir:true}]})
  })
  await page.goto('/files')
  const history = page.getByRole('button',{name:'历史访问',exact:true})
  await expect(history.locator('svg')).toHaveClass(/lucide-history/)
  await expect(history).toBeDisabled()
  await page.getByRole('button',{name:'A',exact:true}).dblclick()
  await expect(page.getByRole('button',{name:'B',exact:true})).toBeVisible()
  await expect(history).toBeDisabled()
  await page.getByRole('button',{name:'B',exact:true}).dblclick()
  await expect(history).toBeEnabled()
  await history.click()
  await expect(page.getByRole('option',{name:'云盘 / B',exact:true})).toBeVisible()
  await expect(page.locator('.file-visit-history .rounded-select-popup')).toHaveCSS('right','-28px')
  await page.locator('.file-visit-history .rounded-select-popup').evaluate(async el=>{await Promise.allSettled(el.getAnimations().map(a=>a.finished))})
  await page.screenshot({path:info.outputPath('history.png')})
})

for (const kind of ['cas','ed2k']) test(`${kind} tasks use live borders and expose scrape participation`, async ({page}, info) => {
  await mock(page, {storages:[{id:'local',name:'本机',type:'local',enabled:true,config:{}}],tasks:[{id:'t',kind,name:'生成任务',storageId:'local',source:'/',enabled:true,status:'running',processed:3,lastRun:new Date().toISOString()}]})
  await page.goto(`/tasks/${kind}`)
  const card = page.locator('.task-row-card')
  await expect(card).toHaveClass(/running/)
  await expect.poll(() => card.evaluate(el => getComputedStyle(el).animationName)).toMatch(/task-running/)
  await expect(page.getByRole('button',{name:'停止任务',exact:true})).toHaveCSS('color','rgb(199, 93, 100)')
  await page.screenshot({path:info.outputPath(`${kind}-running.png`)})
  await page.emulateMedia({reducedMotion:'reduce'})
  await expect(card).toHaveCSS('animation-name','none')
  await page.getByRole('button',{name:'添加任务',exact:true}).click()
  await page.getByRole('button',{name:'更多选项',exact:true}).click()
  const select = page.getByRole('button',{name:'STRM 刮削',exact:true})
  await expect(select).toHaveText('参与')
  await select.click(); await page.getByRole('option',{name:'不参与',exact:true}).click()
  await expect(select).toHaveText('不参与')
  const a = await page.getByLabel('保留扩展名',{exact:true}).boundingBox(), b = await select.boundingBox()
  expect(Math.abs(a.y-b.y)).toBeLessThan(5)
})

test('scrape status stars, uniform selected border and translucent actions match both themes', async ({page}, info) => {
  await mock(page, {tasks:[{id:'c',kind:'cas',name:'CAS 库'},{id:'e',kind:'ed2k',name:'ED2K 库'},{id:'hidden',kind:'ed2k',name:'排除库',scrapeExcluded:true}]})
  await page.route('**/api/strm-scrape/items?**', r => r.fulfill({json:[{path:'Show/a.cas.strm',title:'作品名称',kind:'tv',status:'ok',tmdb:1}]}))
  await page.goto('/tasks/scrape')
  const card = page.locator('.scrape-card').first()
  await expect(card.locator('.scrape-poster')).toContainText('剧集')
  await page.getByRole('button',{name:'STRM 任务',exact:true}).click()
  await expect(page.getByRole('option',{name:'CAS 库',exact:true})).toBeVisible()
  await expect(page.getByRole('option',{name:'ED2K 库',exact:true})).toBeVisible()
  await expect(page.getByRole('option',{name:'排除库',exact:true})).toHaveCount(0)
  await page.keyboard.press('Escape')
  const star = card.locator('.work-star')
  await expect(star).not.toHaveAttribute('data-tooltip')
  expect(await star.evaluate(el=>getComputedStyle(el).clipPath)).toContain('polygon')
  await page.getByRole('button',{name:'刮削状态',exact:true}).click()
  await expect(page.locator('[role=option] .select-marker')).toHaveCount(7)
  await page.getByRole('option',{name:'全部状态',exact:true}).click()
  await card.hover()
  await expect(card.locator('.scrape-actions button').first()).toHaveCSS('backdrop-filter','blur(12px)')
  await card.click({button:'right',position:{x:20,y:20}})
  const widths = await card.evaluate(el=>{const s=getComputedStyle(el,'::after');return [s.borderTopWidth,s.borderRightWidth,s.borderBottomWidth,s.borderLeftWidth]})
  expect(widths).toEqual(['2px','2px','2px','2px'])
  for(const theme of ['light','dark']) {
    await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
    await page.mouse.move(0,0)
    await page.screenshot({path:info.outputPath(`scrape-${theme}.png`)})
  }
})

test('cloud quota does not block cards and missing quota falls back to account name', async ({page}, info) => {
  await mock(page, {storages:[{id:'one',name:'115',type:'115',enabled:true,config:{}},{id:'two',name:'移动',type:'mobile',enabled:true,config:{}}]})
  let release
  const gate = new Promise(resolve=>release=resolve)
  await page.route('**/api/storages/*/usage', async r => {
    await gate
    return r.fulfill({json:r.request().url().includes('/one/') ? {used:256,total:1024} : {used:0,total:0,username:'真实账号'}})
  })
  await page.goto('/storage')
  await expect(page.locator('.storage-card')).toHaveCount(2)
  release()
  await expect(page.getByRole('meter')).toHaveAttribute('aria-valuetext','256 B / 1.0 KB')
  await expect(page.locator('.storage-card').last()).toContainText('真实账号')
  for(const [theme,width] of [['light',1440],['dark',390]]) {
    await page.setViewportSize({width,height:844})
    await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
    expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
    await page.screenshot({path:info.outputPath(`quota-${theme}.png`)})
  }
})

test('log settings use the account panel input layers', async ({page}, info) => {
  await mock(page,{settings:{logDays:7,logMaxEntries:5000}})
  await page.goto('/settings/logs')
  for(const theme of ['light','dark']) {
    await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
    const colors=await page.locator('.log-settings-panel').evaluate(el=>({panel:getComputedStyle(el).backgroundColor,input:getComputedStyle(el.querySelector('.number-control')).backgroundColor}))
    expect(colors.panel).toBe(theme==='light'?'rgb(250, 251, 252)':'rgb(27, 29, 35)')
    expect(colors.input).toBe(theme==='light'?'rgb(255, 255, 255)':'rgb(41, 44, 52)')
    await page.screenshot({path:info.outputPath(`logs-${theme}.png`)})
  }
})
