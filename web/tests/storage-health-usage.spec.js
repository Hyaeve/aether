import { test, expect } from '@playwright/test'

test('saved mobile space is immediate across navigation and updates without clearing the meter', async ({ page }, info) => {
  const storage = { id:'mobile', name:'移动空间', type:'mobile', enabled:true, config:{ mode:'native', authorization:'', userDomainId:'domain-123' }, usage:{ used:256, total:1024 } }
  await page.route('**/api/**', r => {
    const path = new URL(r.request().url()).pathname
    const json = path === '/api/auth/status' ? {initialized:true,authenticated:true} : path === '/api/state' ? {username:'health-quota',storages:[storage],tasks:[],settings:{},traffic:{},cache:{}} : path === '/api/files' ? [] : {}
    return r.fulfill({json})
  })
  let release
  const gate = new Promise(resolve => release=resolve)
  await page.route('**/api/storages/*/usage', async r => {
    await gate
    await r.fulfill({json:{used:512,total:1024}}).catch(() => {})
  })
  await page.goto('/storage')
  const meter = page.getByRole('meter', {name:'云盘空间使用量'})
  await expect(meter).toHaveAttribute('aria-valuenow','256')
  await page.getByRole('link',{name:'文件服务',exact:true}).click()
  await page.getByRole('link',{name:'存储管理',exact:true}).click()
  await expect(meter).toHaveAttribute('aria-valuenow','256')
  release()
  await expect(meter).toHaveAttribute('aria-valuenow','512')
  await page.locator('.storage-card-name').click()
  await expect(page.getByLabel('账号域 ID',{exact:true})).toHaveValue('domain-123')
  const name = await page.getByLabel('存储池名称').boundingBox()
  const domain = await page.getByLabel('账号域 ID',{exact:true}).boundingBox()
  expect(domain.y).toBe(name.y)
  expect(domain.x).toBeGreaterThan(name.x)
  await page.setViewportSize({width:390,height:844})
  const mobileName = await page.getByLabel('存储池名称').boundingBox()
  const mobileDomain = await page.getByLabel('账号域 ID',{exact:true}).boundingBox()
  expect(mobileDomain.y).toBe(mobileName.y)
  expect(mobileDomain.x).toBeGreaterThan(mobileName.x)
  expect(mobileDomain.x + mobileDomain.width).toBeLessThan(390)
  await page.screenshot({path:info.outputPath('mobile-edit-domain.png')})
  await page.getByRole('dialog').getByRole('button',{name:'关闭',exact:true}).click()
  for (const [theme,width] of [['light',1440],['dark',390]]) {
    await page.setViewportSize({width,height:844})
    await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
    await expect(meter).toHaveAttribute('aria-valuenow','512')
    expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
    await page.screenshot({path:info.outputPath(`space-${theme}.png`)})
  }
})

test('background state polling updates saved quota without another capacity lookup', async ({page}) => {
  let used=128, lookups=0
  await page.route('**/api/**', r => {
    const path = new URL(r.request().url()).pathname
    if(path.endsWith('/usage')) {lookups++;return r.fulfill({json:{used:128,total:1024}})}
    const json=path==='/api/auth/status'?{initialized:true,authenticated:true}:path==='/api/state'?{username:'health-poll',storages:[{id:'m',name:'移动',type:'mobile',enabled:true,config:{},usage:{used,total:1024}}],tasks:[],settings:{},traffic:{},cache:{}}:{}
    return r.fulfill({json})
  })
  await page.goto('/storage')
  const meter=page.getByRole('meter')
  await expect(meter).toHaveAttribute('aria-valuenow','128')
  await expect.poll(()=>lookups).toBe(1)
  used=700
  await expect(meter).toHaveAttribute('aria-valuenow','700',{timeout:10000})
  expect(lookups).toBe(1)
})
