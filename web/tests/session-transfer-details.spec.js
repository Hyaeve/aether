import { test, expect } from '@playwright/test'

async function mock(page, transfers = []) {
  let computedSize = false
  const paths = []
  let calculations = 0
  await page.route('**/api/**', async r => {
    const u = new URL(r.request().url()); let json = {}
    if(u.pathname === '/api/auth/status') json = {initialized:true,authenticated:true}
    if(u.pathname === '/api/state') json = {username:'session-revision',storages:[{id:'pan',name:'115资料',type:'115',enabled:true,config:{}}],tasks:[],settings:{},cache:{},traffic:{},logs:[]}
    if(u.pathname === '/api/files') {
      paths.push(u.searchParams.get('path'))
      if(u.searchParams.get('refresh') === 'true') computedSize = false
      json = u.searchParams.get('path') === '/' ? [{id:'cid-a',name:'A',isDir:true,sizeKnown:computedSize,countsKnown:computedSize,size:computedSize?12345:0,folderCount:1,fileCount:2}] : u.searchParams.get('path') === 'cid-a' ? [{id:'cid-b',name:'B',isDir:true}] : [{id:'file',name:'保留目录.txt',isDir:false,size:123}]
    }
    if(u.pathname === '/api/files/directory-size') { calculations++; computedSize = true; json = {size:12345,sizeKnown:true,countsKnown:true,folderCount:1,fileCount:2} }
    if(u.pathname === '/api/transfers') json = transfers
    if(u.pathname === '/api/automations') json = []
    await r.fulfill({json})
  })
  return {paths,calculations:()=>calculations}
}

test('session restores opaque directory and module tabs; new session keeps only pool', async({page,browser},info) => {
  const fixture = await mock(page)
  await page.goto('/files')
  await page.getByRole('button',{name:'A',exact:true}).dblclick()
  await page.getByRole('button',{name:'B',exact:true}).dblclick()
  await expect(page.getByRole('button',{name:'保留目录.txt',exact:true})).toBeVisible()
  await page.getByRole('link',{name:'任务管理',exact:true}).click()
  await page.getByRole('link',{name:'ED2K 任务',exact:true}).click()
  await page.getByRole('link',{name:'文件服务',exact:true}).click()
  await expect(page.getByRole('button',{name:'保留目录.txt',exact:true})).toBeVisible()
  expect(fixture.paths.at(-1)).toBe('cid-b')
  await page.getByRole('link',{name:'任务管理',exact:true}).click()
  await expect(page).toHaveURL(/\/tasks\/ed2k$/)
  await page.getByRole('link',{name:'文件服务',exact:true}).click()
  await page.getByRole('button',{name:'历史访问',exact:true}).click()
  const option=page.getByRole('option',{name:'115资料 / B',exact:true})
  await expect(option.locator('[data-provider="115"]')).toBeVisible()
  await expect(option).toContainText('A')
  await page.screenshot({path:info.outputPath('history-provider-path.png')})
  await page.keyboard.press('Escape')
  const fresh=await browser.newContext({storageState:await page.context().storageState()})
  try {
    const other=await fresh.newPage();const newFixture=await mock(other)
    await other.goto(new URL('/files',page.url()).href)
    await expect(other.getByRole('button',{name:'A',exact:true})).toBeVisible()
    expect(newFixture.paths.at(-1)).toBe('/')
  } finally { await fresh.close() }
  await page.evaluate(()=>sessionStorage.clear())
  await page.reload()
  await expect(page.getByRole('button',{name:'选择存储池',exact:true})).toContainText('115资料')
  await expect(page.getByRole('button',{name:'A',exact:true})).toBeVisible()
  expect(fixture.paths.at(-1)).toBe('/')
})

test('topbar shortcuts select transfer mode and determinate rows expose size and speed',async({page},info)=>{
  await mock(page,Array.from({length:200},(_,i)=>({id:String(i),name:`文件${i}.mkv`,storage:'115资料',source:'FUSE',kind:i%2?'download':'upload',status:'running',done:25,total:100,speed:4096,started:'2026-10-10T01:00:00Z'})))
  await page.goto('/dashboard')
  await page.getByRole('button',{name:'上传速率',exact:true}).click()
  await expect(page.getByRole('tab',{name:/^上传/})).toHaveAttribute('aria-selected','true')
  await expect(page.locator('.transfer-table tbody')).toContainText('25%')
  await expect(page.locator('.transfer-table tbody')).toContainText('4.0 KB/s')
  await expect(page.locator('.transfer-table progress').first()).toHaveAttribute('value','25')
  expect(await page.locator('.transfer-table tbody tr').count()).toBeLessThan(40)
  const list=page.locator('.transfer-viewport'),outer=page.locator('.page-scroll > .thin-scroll-area')
  await list.hover();await page.mouse.wheel(0,800)
  await expect.poll(()=>outer.evaluate(el=>el.scrollTop)).toBeGreaterThan(0)
  await expect.poll(()=>list.evaluate(el=>el.scrollTop)).toBeGreaterThan(0)
  expect(Math.abs((await page.locator('.transfer-header').boundingBox()).y-48)).toBeLessThan(3)
  expect(await page.locator('.page-scroll > .thin-scroll-rail').isVisible()).toBe(false)
  await page.screenshot({path:info.outputPath('transfer-fixed-header.png')})
  await page.getByRole('button',{name:'下载速率',exact:true}).click()
  await expect(page.getByRole('tab',{name:/^下载/})).toHaveAttribute('aria-selected','true')
  await page.getByRole('link',{name:'文件服务',exact:true}).click()
  await page.getByRole('link',{name:'传输中心',exact:true}).click()
  await expect(page.getByRole('tab',{name:/^下载/})).toHaveAttribute('aria-selected','true')
  await page.setViewportSize({width:390,height:844})
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
  await page.screenshot({path:info.outputPath('transfer-mobile.png')})
  await page.evaluate(()=>document.documentElement.dataset.theme='dark')
  await page.screenshot({path:info.outputPath('transfer-mobile-dark.png')})
})

test('details slides inside browser, cached size survives navigation but refresh clears without calculating',async({page},info)=>{
  const fixture = await mock(page)
  await page.goto('/files')
  await page.getByRole('button',{name:'A',exact:true}).click({button:'right'})
  await page.getByRole('button',{name:'查看详情',exact:true}).click()
  const drawer=page.getByRole('dialog',{name:'文件详情',exact:true})
  await expect(drawer).toContainText('12.1 KB')
  await drawer.evaluate(async el=>{await Promise.allSettled(el.getAnimations().map(a=>a.finished))})
  const browser=await page.locator('.file-browser').boundingBox(),box=await drawer.boundingBox()
  expect(box.x).toBeGreaterThan(browser.x+browser.width/2)
  expect(Math.abs(box.x+box.width-browser.x-browser.width)).toBeLessThan(3)
  await expect(drawer.locator('.details-content')).toHaveCSS('scrollbar-width','none')
  const fonts=await drawer.locator('dl').evaluate(el=>({label:getComputedStyle(el.querySelector('dt')).fontSize,value:getComputedStyle(el.querySelector('dd')).fontSize}))
  expect(fonts.label).toBe(fonts.value)
  await page.screenshot({path:info.outputPath('details-light.png')})
  await page.evaluate(()=>document.documentElement.dataset.theme='dark')
  await page.screenshot({path:info.outputPath('details-dark.png')})
  await drawer.getByRole('button',{name:'关闭',exact:true}).click()
  await page.getByRole('link',{name:'存储管理',exact:true}).click()
  await page.getByRole('link',{name:'文件服务',exact:true}).click()
  await expect(page.locator('.file-row')).toContainText('12.1 KB')
  await page.getByRole('button',{name:'刷新目录',exact:true}).click()
  await expect(page.locator('.file-row')).not.toContainText('12.1 KB')
  expect(fixture.calculations()).toBe(1)
  await page.getByRole('button',{name:'A',exact:true}).click({button:'right'})
  await page.getByRole('button',{name:'查看详情',exact:true}).click()
  await expect(drawer).toContainText('12.1 KB')
  expect(fixture.calculations()).toBe(2)
  await page.keyboard.press('Escape');await expect(drawer).toHaveCount(0)
})

test('empty tasks and transfers fill the bottom without an extra scrolling page',async({page},info)=>{
  await mock(page)
  await page.goto('/tasks/ed2k')
  const box=await page.locator('.task-empty').boundingBox()
  expect(box.y+box.height).toBeGreaterThanOrEqual(998)
  await page.screenshot({path:info.outputPath('empty-task-panel.png')})
  await page.goto('/transfer')
  const outer=page.locator('.page-scroll > .thin-scroll-area')
  expect(await outer.evaluate(el=>el.scrollHeight-el.clientHeight)).toBeLessThan(2)
  await expect(page.locator('.transfer-header')).toContainText('大小')
})

test('automation columns show workflow, results and schedule with standard editor',async({page},info)=>{
  await mock(page)
  await page.route('**/api/automations',r=>r.fulfill({json:[{id:'one',name:'自动更新',enabled:true,trigger:'cron',nextRun:new Date(Date.now()+86400000).toISOString(),lastResult:'success',status:'idle',steps:Array.from({length:8},()=>({kind:'refresh'}))},{id:'two',name:'手动联动',enabled:false,trigger:'manual',status:'idle',steps:[{kind:'delay',seconds:10}]}]}))
  await page.goto('/tasks/automation')
  await expect(page.locator('.automation-row').first()).toContainText('明天')
  await expect(page.locator('.automation-row').first()).toContainText('成功')
  await expect(page.locator('.automation-row').last()).toContainText('--')
  await expect(page.locator('.automation-row').last()).toHaveClass(/disabled/)
  await page.locator('.automation-flow').first().hover()
  await expect(page.locator('.automation-row').first().locator('.automation-actions')).toBeHidden()
  await page.locator('.automation-columns').hover()
  await page.getByRole('button',{name:'联动操作',exact:true}).first().click()
  await expect(page.getByRole('menuitem',{name:'停用',exact:true})).toBeVisible()
  await page.getByRole('menuitem',{name:'编辑',exact:true}).click()
  const editor=page.getByRole('dialog',{name:'编辑联动',exact:true})
  expect((await editor.boundingBox()).width).toBe(640)
  await expect(editor.locator('.automation-form')).toHaveCSS('scrollbar-width','none')
  await editor.getByRole('button',{name:'取消',exact:true}).click()
  await page.screenshot({path:info.outputPath('automation-columns.png')})
  await page.setViewportSize({width:390,height:844});await page.evaluate(()=>document.documentElement.dataset.theme='dark')
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
  await page.screenshot({path:info.outputPath('automation-mobile-dark.png')})
})
