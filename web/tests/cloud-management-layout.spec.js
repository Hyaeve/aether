import {test,expect} from '@playwright/test'
import {captureScreenshot} from './helpers/screenshot.js'

for(const width of [1440,390])for(const theme of ['light','dark']){
 test(`cloud execution columns and dates ${width} ${theme}`,async({page},info)=>{
  const timestamp=new Date(2026,9,24,18,59,42).toISOString()
  await page.setViewportSize({width,height:950})
  await page.route('**/api/**',route=>{
   const u=new URL(route.request().url())
   let json={}
   if(u.pathname==='/api/auth/status')json={initialized:true,authenticated:true}
   if(u.pathname==='/api/state')json={username:'cloud-layout',storages:[{id:'p',name:'115影视',type:'115',enabled:true,config:{}}],tasks:[],settings:{},cache:{},traffic:{}}
   if(u.pathname==='/api/cloud/manage')json={items:['success','error','running','error'].map((status,i)=>({id:String(i),name:`电影-${i}.mkv`,size:12,url:`https://115.com/s/example${i}`,path:'/影视/收藏',time:timestamp,status,progress:[.1,.3,.5,0][i]})),nextPage:0}
   return route.fulfill({json})
  })
  await page.goto('/storage/downloads')
  await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
  const rows=page.locator('.cloud-management-row')
  await expect(rows).toHaveCount(4)
  await expect(rows.first().locator('time')).toHaveText('26/10/24 18:59')
  await expect(rows.first().locator('progress')).toHaveAttribute('value','1')
  await expect(rows.nth(1).locator('progress')).toHaveAccessibleName('失败')
  await expect(rows.nth(2).locator('progress')).toHaveAccessibleName('进行中')
  await expect(rows.nth(3).locator('progress')).toHaveAttribute('value','0')
  await expect(rows.nth(3).locator('progress')).toHaveCSS('border-top-width','1px')
  const red=await page.evaluate(()=>{
   const hex=getComputedStyle(document.documentElement).getPropertyValue('--red').trim().slice(1)
   return `rgb(${[0,2,4].map(i=>parseInt(hex.slice(i,i+2),16)).join(', ')})`
  })
  await expect(rows.nth(3).locator('progress')).toHaveCSS('border-top-color',red)
  for(const selector of ['.cloud-path','.cloud-execution']){
   const contained=await rows.first().evaluate((el,selector)=>{const r=el.getBoundingClientRect(),c=el.querySelector(selector).getBoundingClientRect();return c.left>=r.left&&c.right<=r.right&&c.top>=r.top&&c.bottom<=r.bottom},selector)
   expect(contained).toBe(true)
  }
  await expect(rows.first().locator('.cloud-link')).toHaveText('https://115.com/s/example0')
  await expect(rows.first().locator('.cloud-path')).toHaveText('/影视/收藏')
  await expect(page.locator('.cloud-management-columns')).toContainText('执行状态')
  await expect(page.locator('.cloud-execution small')).toHaveCount(0)
  const boxes=await page.locator('.cloud-total,button[aria-label="刷新云盘栏目"]').evaluateAll(els=>els.map(el=>{const r=el.getBoundingClientRect();return {left:r.left,right:r.right}}))
  expect(boxes[0].right).toBeLessThanOrEqual(boxes[1].left)
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
  await captureScreenshot(page,{path:info.outputPath(`downloads-${width}-${theme}.png`)})
  await page.getByRole('link',{name:'分享栏',exact:true}).click()
  await expect(page.locator('.cloud-management-columns')).toContainText('时间')
  await expect(rows.first().locator('time')).toHaveText('26/10/24 18:59')
  await captureScreenshot(page,{path:info.outputPath(`shares-${width}-${theme}.png`)})
  await page.getByRole('link',{name:'回收站',exact:true}).click()
  await expect(page.locator('.cloud-management-columns')).toContainText('删除时间')
  await expect(rows.first().locator('time')).toHaveText('26/10/24 18:59')
  await captureScreenshot(page,{path:info.outputPath(`trash-${width}-${theme}.png`)})
 })
}
