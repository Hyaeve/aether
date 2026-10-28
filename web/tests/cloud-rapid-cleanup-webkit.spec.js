import {test,expect} from '@playwright/test'
import {captureScreenshot} from './helpers/screenshot.js'

test.use({browserName:'webkit',hasTouch:true,deviceScaleFactor:2,reducedMotion:'reduce'})

for(const [width,height] of [[1440,900],[375,812],[844,390]])for(const theme of ['light','dark']){
 test(`cloud and rapid tools remain operable in WebKit ${width} ${theme}`,async({page},info)=>{
  const job={source:'/media/待传目录',storageId:'p',target:'10',targetLabel:'115 /影音/目标目录',successAction:'keep',failureAction:'skip',extensions:''}
  const storages=[{id:'p',name:'115 家庭影音存储池名称非常长',type:'115',enabled:true,config:{}}]
  await page.route('**/api/**',route=>{
   const u=new URL(route.request().url());let json={}
   if(u.pathname==='/api/auth/status')json={initialized:true,authenticated:true}
   else if(u.pathname==='/api/state')json={username:'cloud-webkit',storages,tasks:[],settings:{},cache:{},traffic:{}}
   else if(u.pathname==='/api/cloud/manage')json={items:[{id:'1',name:'影片名称'.repeat(20),size:1024,path:'/祖先目录/影音/剧集/最深目录',url:'https://example.test/media',time:'2026-10-27T18:59:00+08:00',status:'running',progress:.45}]}
   else if(u.pathname==='/api/115-rapid')json={manual:job,watches:[{...job,id:'w',name:'影音监控',mode:'cron',cron:'*/15 * * * *',enabled:true}]}
   else if(u.pathname==='/api/115-rapid/records')json={items:[{id:'r',name:'影片名称'.repeat(12),pickcode:'abcdefghijklmnopq',size:1024,time:'2026-10-27T18:59:00+08:00',status:'running',phase:'计算文件摘要',sourceKind:'monitor',source:'监控 · 影音监控',done:45,total:100}],page:1,pages:1,total:1,manualRunning:false,monitorRunning:true}
   else if(u.pathname==='/api/cron/validate')json={error:'Cron计划相邻执行间隔至少15分钟'}
   return route.fulfill({json})
  })
  await page.setViewportSize({width,height})
  await page.goto('/storage/downloads');await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
  const row=page.locator('.cloud-management-row')
  await expect(row).toHaveCount(1);await expect(row.locator('progress')).toHaveCSS('height','4px')
  expect(await row.evaluate(el=>[...el.querySelectorAll('.cloud-row-actions button')].every(b=>b.getBoundingClientRect().right<=el.getBoundingClientRect().right))).toBe(true)
  const tools=await page.locator('.cloud-toolbar').evaluate(el=>[...el.children].map(c=>c.getBoundingClientRect().toJSON()))
  expect(tools[0].left).toBeGreaterThanOrEqual(0);expect(tools.at(-1).right).toBeLessThanOrEqual(width)
  await captureScreenshot(page,{path:info.outputPath(`cloud-webkit-${width}-${theme}.png`)})
  await page.goto('/tools/115-rapid');await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
  await expect(page.getByRole('button',{name:'开始秒传',exact:true})).toBeEnabled()
  const status=page.locator('.rapid115-records .rapid115-status')
  await expect(status.locator('progress')).toHaveAttribute('value','0.45')
  await expect(status.locator('progress')).toHaveCSS('height','4px')
  expect(await page.locator('.rapid115-records .rapid115-record-frame').evaluate(el=>!['scroll','auto'].includes(getComputedStyle(el).overflowY))).toBe(true)
  await captureScreenshot(page,{path:info.outputPath(`rapid-webkit-${width}-${theme}.png`)})
  await page.getByRole('button',{name:'查看 影音监控 秒传记录',exact:true}).click()
  const dialog=page.getByRole('dialog',{name:'影音监控 · 秒传记录',exact:true})
  await expect(dialog.locator('time')).toHaveText(/^\d{2}\/\d{2} \d{2}:\d{2}$/)
  expect(await dialog.locator('.rapid115-pickcode').evaluate(el=>el.scrollWidth<=el.clientWidth)).toBe(true)
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
  await dialog.getByRole('button',{name:'关闭',exact:true}).click()
  await page.getByRole('button',{name:'编辑 影音监控',exact:true}).click()
  const input=page.getByRole('textbox',{name:'Cron表达式',exact:true})
  await input.fill('*/14 * * * *')
  await expect(input).toHaveAttribute('aria-invalid','true')
  await expect(page.getByRole('button',{name:'Cron计划不可用原因'})).toBeVisible()
 })
}
