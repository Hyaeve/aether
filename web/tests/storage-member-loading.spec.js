import {test,expect} from '@playwright/test'
import {captureScreenshot} from './helpers/screenshot.js'

for(const [width,height,theme] of [[1440,1000,'light'],[390,844,'dark']])test(`membership badges do not wait for capacity ${width}`,async({page},info)=>{
 const storages=[
  {id:'pan',name:'115影视',type:'115',config:{cookie:'********'},tier:'annual'},
  {id:'mobile',name:'移动影视',type:'mobile',config:{mode:'native',authorization:'********'},tier:'diamond'},
  {id:'tianyi',name:'天翼影视',type:'tianyi',config:{mode:'native',username:'account',password:'********'},tier:'platinum'},
 ].map(s=>({...s,enabled:true}))
 let release
 const quota=new Promise(resolve=>release=resolve),unexpected=[],reads=[]
 await page.route('**/api/**',async route=>{
  const r=route.request(),p=new URL(r.url()).pathname
  let json
  if(p==='/api/auth/status')json={initialized:true,authenticated:true}
  else if(p==='/api/state')json={username:'badge-loading',storages,tasks:[],links:[],settings:{},traffic:{},cache:{}}
  else if(p==='/api/traffic')json={}
  else if(p==='/api/strm-replace')json={tasks:[]}
  else if(/^\/api\/storages\/[^/]+\/membership$/.test(p)){
   const s=storages.find(s=>p===`/api/storages/${s.id}/membership`)
   reads.push(s.id);s.usage={membership:{tier:s.tier,checkedAt:new Date().toISOString()}}
   json=s.usage
  }
  else if(p.endsWith('/usage')){await quota;json={used:25,total:100}}
  else{unexpected.push(p);return route.fulfill({status:500,json:{error:'Unexpected '+p}})}
  await route.fulfill({json}).catch(()=>{})
 })
 await page.setViewportSize({width,height});await page.goto('/storage');await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
 await expect(page.locator('.storage-membership')).toHaveCount(3)
 for(const label of ['年费VIP','钻石会员','铂金会员'])await expect(page.getByRole('img',{name:label,exact:true})).toBeVisible()
 await expect(page.getByRole('meter')).toHaveCount(0)
 expect([...reads].sort()).toEqual(['mobile','pan','tianyi'])
 release()
 await expect(page.getByRole('meter')).toHaveCount(3)
 await expect(page.locator('.storage-membership')).toHaveCount(3)
 await captureScreenshot(page,{path:info.outputPath(`independent-badges-${width}-${theme}.png`)})
 await page.getByRole('button',{name:'添加存储池',exact:true}).click()
 const option=page.locator('.driver-option[data-provider="115"]')
 const resting=await option.evaluate(el=>getComputedStyle(el).backgroundColor)
 await option.hover()
 await expect(option).not.toHaveCSS('background-color',resting)
 await expect(option).not.toHaveCSS('background-color','rgba(0, 0, 0, 0)')
 await captureScreenshot(page,{path:info.outputPath(`provider-hover-${width}-${theme}.png`)})
 expect(unexpected).toEqual([])
})
