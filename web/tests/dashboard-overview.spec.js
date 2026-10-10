import { test, expect } from '@playwright/test'

async function setup(page, overrides = {}) {
  const types = ['115','mobile','tianyi','quark','openlist','webdav','local']
  const data = {
    username:'dashboard-overview',
    storages:Array.from({length:12},(_,i)=>({id:`pool${i}`,type:types[i%types.length],name:`${['影视','音乐','阅读','照片','备份','共享'][i%6]} ${i+1}`,enabled:i!==8})),
    tasks:[], settings:{webdavEnabled:true}, traffic:{downloaded:201326592}, uptime:95040,
    cache:{hits:72,misses:28,entries:1250,bytes:8388608},
    logs:[
      {time:'2026-10-09T10:00:00+08:00',module:'storage',level:'error',message:'存储服务读取失败，请检查连接'},
      {time:'2026-10-09T10:05:00+08:00',module:'system',level:'warn',message:'已清理过期缓存'},
      {time:'2026-10-09T10:08:00+08:00',module:'tasks',level:'success',message:'音乐缓存已完成，共处理 1250 个目录'}
    ], ...overrides
  }
  await page.route('**/api/**', route => {
    const path = new URL(route.request().url()).pathname
    return route.fulfill({json:path==='/api/auth/status'?{initialized:true,authenticated:true}:path==='/api/state'?data:path==='/api/logs'?data.logs:{}})
  })
  await page.goto('/dashboard')
  return data
}

test('cache ring uses live counters and supports no samples, all hits and all misses', async ({page}) => {
  const data = await setup(page)
  const cache = page.getByRole('region',{name:'缓存命中统计',exact:true})
  await expect(cache.getByRole('img',{name:'缓存命中率 72%'})).toBeVisible()
  await expect(cache.locator('.cache-hit')).toHaveAttribute('stroke-dasharray','72 100')
  await expect(cache.locator('.hit dd')).toHaveText('72')
  await expect(cache.locator('.miss dd')).toHaveText('28')
  for(const [hits,misses,label,text] of [[0,0,'缓存暂无访问记录','—'],[100,0,'缓存命中率 100%','100%'],[0,100,'缓存命中率 0%','0%']]) {
    Object.assign(data.cache,{hits,misses})
    await page.reload()
    await expect(cache.getByRole('img',{name:label})).toBeVisible()
    await expect(cache.locator('.cache-chart strong')).toHaveText(text)
    if(hits===0) await expect(cache.locator('.cache-hit')).not.toBeVisible()
  }
  await cache.getByRole('button',{name:'缓存任务',exact:true}).click()
  await expect(page).toHaveURL(/\/tasks\/cache$/)
  await expect(page.getByRole('img',{name:'缓存命中率 0%'})).toBeVisible()
})

test('shortcut viewport exposes six pools, scrolls without a scrollbar and retains keyboard navigation', async ({page},info) => {
  await setup(page)
  const shortcuts = page.getByRole('region',{name:'存储快捷访问',exact:true})
  await expect(shortcuts.locator('.provider-icon').first()).toHaveCSS('width','40px')
  await expect(shortcuts.locator('.provider-icon').first()).toHaveCSS('background-color','rgba(0, 0, 0, 0)')
  await expect(shortcuts.locator('.provider-icon').first()).toHaveCSS('border-top-width','0px')
  const visibleCount = () => shortcuts.evaluate(el=>{
    const bounds=el.getBoundingClientRect()
    return [...el.querySelectorAll('button')].filter(b=>{const r=b.getBoundingClientRect();return r.left>=bounds.left-1&&r.right<=bounds.right+1}).length
  })
  for(const width of [1920,1440,1024,768,390,320]) {
    await page.setViewportSize({width,height:1000})
    await shortcuts.evaluate(el=>el.scrollLeft=0)
    await expect.poll(visibleCount).toBe(6)
    await expect(shortcuts).toHaveCSS('scrollbar-width','none')
    expect(await shortcuts.evaluate(el=>el.scrollWidth>el.clientWidth)).toBe(true)
    expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1)).toBe(true)
    expect(await page.locator('.page-content').evaluate(el=>el.scrollWidth<=el.clientWidth+1)).toBe(true)
    if(width===1440||width===390)await page.screenshot({path:info.outputPath(`dashboard-overview-${width}.png`),fullPage:true})
  }
  await shortcuts.hover()
  await page.mouse.wheel(0,350)
  await expect.poll(()=>shortcuts.evaluate(el=>el.scrollLeft)).toBeGreaterThan(0)
  await expect(shortcuts.locator('button').nth(8)).toBeDisabled()
  await shortcuts.locator('button').nth(10).focus()
  await expect(shortcuts.locator('button').nth(10)).toBeFocused()
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL(/storage=pool10/)
})

test('recent activity shows module, severity and compact time, with themed layout and a precise log link',async({page},info)=>{
  const data=await setup(page)
  const rows=page.locator('.activity-row')
  await expect(rows).toHaveCount(3)
  await expect(rows.first()).toContainText('任务管理')
  await expect(rows.first()).toContainText('完成')
  await expect(rows.first().locator('time')).toHaveText(/\d{2}-\d{2} \d{2}:\d{2}/)
  await expect(rows.first().locator('time')).toHaveAttribute('datetime',data.logs[2].time)
  const error=rows.filter({hasText:'存储服务读取失败'})
  await expect(error).toHaveAttribute('data-level','error')
  const light=await error.locator('.activity-level').evaluate(el=>getComputedStyle(el).color)
  await page.getByRole('button',{name:'主题：日光',exact:true}).click()
  await expect.poll(()=>error.locator('.activity-level').evaluate(el=>getComputedStyle(el).color)).not.toBe(light)
  await page.screenshot({path:info.outputPath('dashboard-overview-dark.png'),fullPage:true})
  await rows.first().click()
  await expect(page).toHaveURL(/\/logs\?notice=dashboard/)
  await expect(page.getByRole('textbox',{name:'搜索日志',exact:true})).toHaveValue(data.logs[2].message)
  await page.goto('/dashboard')
  data.logs=[];data.storages=[]
  await page.reload()
  await expect(page.getByText('暂无活动记录',{exact:true})).toBeVisible()
  await expect(page.getByRole('button',{name:'添加存储池',exact:true})).toBeVisible()
})
