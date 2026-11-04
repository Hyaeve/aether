import {test,expect} from '@playwright/test'
import {captureScreenshot} from './helpers/screenshot.js'

async function fixture(page){
 const tiers=[['annual','115','annual'],['forever','115','permanent'],['silver','mobile','silver'],['gold','mobile','gold'],['diamond','mobile','diamond'],['tgold','tianyi','gold'],['platinum','tianyi','platinum'],['qsvip','quark','svip'],['q88','quark','88vip']]
 let storages=tiers.map(([id,type,tier])=>({id,name:`${type}影视存储 ${id}`,type,enabled:true,config:{root:'0'},usage:{used:10,total:100,membership:{tier,checkedAt:new Date().toISOString()}}}))
 let tasks=[{id:'t',name:'影视增量同步',kind:'strm',storageId:'annual',source:'10',sourceLabel:'影音',target:'/strm',mode:'incremental_update',enabled:true,status:'idle',cron:'*/15 * * * *',processed:0}]
 let links=[{id:'l',name:'家庭Emby',type:'emby',url:'http://emby.test:8096',port:15180,enabled:true,mode:'public',favorites:[]}]
 let cards=[{id:'v',name:'分享影视库',url:'https://pan.quark.cn/s/example',enabled:true,directory:'/strm',directoryName:'影视库',cron:'0 3 * * *',mediaExtensions:'mkv',metadataExtensions:'nfo',result:{status:'idle'}}]
 let rapid={manual:{source:'',storageId:'annual',target:'',targetLabel:'',extensions:'mkv',successAction:'skip',failureAction:'skip'},watches:[{id:'w',name:'影片监控',source:'/incoming',storageId:'annual',target:'10',targetLabel:'115 /影音',extensions:'mkv',mode:'watch',successAction:'skip',failureAction:'skip',enabled:true,filters:[]}]}
 let pending=null,resolve,fail=false
 const writes=[],unexpected=[]
 const complete=success=>{if(!resolve)throw Error('no pending delete');resolve(success);resolve=null}
 await page.route('**/api/**',async route=>{
  const r=route.request(),p=new URL(r.url()).pathname
  let json
  const removing=r.method()==='DELETE'||p==='/api/115-rapid'&&r.method()==='PUT'&&!r.postDataJSON().watches?.length
  if(removing){
   writes.push({p,body:r.postDataJSON()});pending=p
   const success=await new Promise(done=>{resolve=done})
   pending=null
   if(!success)return route.fulfill({status:409,json:{error:'配置正在使用，删除失败'}})
   if(p==='/api/storages/annual')storages=storages.filter(s=>s.id!=='annual')
   else if(p==='/api/tasks/t')tasks=[]
   else if(p==='/api/links/l')links=[]
   else if(p==='/api/virtual-library/quark/v')cards=[]
   else if(p==='/api/115-rapid')rapid=r.postDataJSON()
   else unexpected.push(r.method()+' '+p)
   return route.fulfill({json:{ok:true}})
  }
  if(p==='/api/auth/status')json={initialized:true,authenticated:true}
  else if(p==='/api/state')json={username:'card-revision',storages,tasks,links,settings:{},cache:{},traffic:{},logs:[],fileVisits:[],fileFavorites:[]}
  else if(/^\/api\/storages\/[^/]+\/usage$/.test(p))json=storages.find(s=>p.includes(`/${s.id}/`))?.usage||{}
  else if(p==='/api/strm-replace')json={tasks:[]}
  else if(p==='/api/tasks/panel')json={items:[]}
  else if(p==='/api/links')json=links
  else if(p==='/api/link-playback')json={recentEvents:[]}
  else if(p==='/api/virtual-library/quark')json={cards,settings:{storageIds:['qsvip'],directory:'/strm',mediaExtensions:'mkv',metadataExtensions:'nfo'},stats:{}}
  else if(p==='/api/115-rapid'){if(r.method()==='PUT')rapid=r.postDataJSON();json=rapid}
  else if(p==='/api/115-rapid/records')json={items:[],page:1,pages:1,running:false,total:0}
  else if(p==='/api/traffic'||p==='/api/plugins/proxy')json={}
  else {unexpected.push(r.method()+' '+p);return route.fulfill({status:500,json:{error:'Unexpected '+p}})}
  return route.fulfill({json})
 })
 return{writes,unexpected,complete,get pending(){return pending}}
}

const routes={storage:'/storage',task:'/tasks/strm',link:'/links/manage',library:'/virtual/quark',rapid:'/tools/115-rapid'}
const paths={storage:'/storages/annual',task:'/tasks/t',link:'/links/l',library:'/virtual-library/quark/v'}
async function askDelete(page,kind){
 if(kind==='storage'){await page.getByRole('button',{name:'存储操作 115影视存储 annual',exact:true}).click();await page.getByRole('button',{name:'删除存储',exact:true}).click()}
 if(kind==='task'){await page.getByRole('button',{name:'任务操作 影视增量同步',exact:true}).click();await page.locator('.task-menu').getByRole('button',{name:'删除任务',exact:true}).click()}
 if(kind==='link'){await page.getByRole('button',{name:'以链操作 家庭Emby',exact:true}).click();await page.getByRole('button',{name:'删除链接',exact:true}).click()}
 if(kind==='library')await page.getByRole('button',{name:'移除 分享影视库',exact:true}).click()
 if(kind==='rapid')await page.getByRole('button',{name:'删除 影片监控',exact:true}).click()
 return page.getByRole('dialog',{name:({storage:'删除存储池',task:'删除任务',link:'删除以链',library:'移除夸克链接',rapid:'删除监控'})[kind],exact:true})
}
async function confirm(page,kind,dialog){await dialog.getByRole('button',{name:({storage:'删除存储池',task:'删除任务',link:'确认删除',library:'确认移除',rapid:'删除'})[kind],exact:true}).click()}

for(const [width,height,theme] of [[1440,1000,'light'],[390,844,'dark'],[375,812,'light'],[844,390,'dark']]){
 test(`storage icon bounds and upper right membership badges ${width}`,async({page},info)=>{
  const f=await fixture(page);await page.setViewportSize({width,height});await page.goto('/storage');await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
  await expect(page.locator('.storage-membership')).toHaveCount(9)
  for(const label of ['年费VIP','永久VIP','白银会员','黄金会员','钻石会员','铂金会员','88VIP','SVIP'])await expect(page.getByRole('img',{name:label,exact:true}).first()).toBeVisible()
  for(const card of await page.locator('.storage-card').all()){
   const geometry=await card.evaluate(el=>{const box=n=>{const r=n.getBoundingClientRect();return{x:r.x,y:r.y,width:r.width,height:r.height,right:r.right,bottom:r.bottom}};return{card:box(el),icon:box(el.querySelector('.provider-icon')),logo:box(el.querySelector('.provider-logo')),badge:box(el.querySelector('.storage-membership')),menu:box(el.querySelector('.storage-menu-control>.icon-btn')),position:getComputedStyle(el.querySelector('.storage-membership')).position,bg:getComputedStyle(el).backgroundImage,overflow:el.scrollWidth>el.clientWidth}})
   expect(geometry.icon.width).toBe(48);expect(geometry.icon.height).toBe(48);expect(geometry.logo.width).toBe(48);expect(geometry.logo.height).toBe(48)
   expect(geometry.logo.x).toBe(geometry.icon.x);expect(geometry.logo.y).toBe(geometry.icon.y)
   expect(geometry.badge.height).toBeLessThanOrEqual(20);expect(geometry.badge.width).toBeLessThan(90)
   expect(geometry.badge.bottom).toBeLessThanOrEqual(geometry.menu.y);expect(geometry.badge.right).toBeLessThan(geometry.card.right)
   expect(geometry.position).toBe('static');expect(geometry.bg).toBe('none');expect(geometry.overflow).toBe(false)
   const dots=await card.locator('.storage-menu-control>.icon-btn svg circle').evaluateAll(nodes=>nodes.map(n=>({x:n.getAttribute('cx'),y:n.getAttribute('cy')})))
   expect(dots).toHaveLength(3);expect(new Set(dots.map(d=>d.y)).size).toBe(1);expect(new Set(dots.map(d=>d.x)).size).toBe(3)
  }
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
  const themeColors=await page.evaluate(()=>({root:getComputedStyle(document.documentElement).backgroundImage,silver:getComputedStyle(document.querySelector('.storage-membership.silver')).color,diamond:getComputedStyle(document.querySelector('.storage-membership.diamond')).color}))
  expect(themeColors).toEqual({root:'none',silver:theme==='dark'?'rgb(211, 224, 240)':'rgb(66, 90, 121)',diamond:theme==='dark'?'rgb(179, 231, 239)':'rgb(40, 102, 122)'})
  await captureScreenshot(page,{path:info.outputPath(`badges-${width}-${theme}.png`)})
  expect(f.unexpected).toEqual([])
 })
}

for(const kind of ['storage','task','link','library'])test(`confirmed ${kind} removal dissolves actual card and releases canvas`,async({page},info)=>{
 const f=await fixture(page);await page.goto(routes[kind]);const dialog=await askDelete(page,kind)
 await page.clock.install({time:new Date('2026-11-01T12:00:00Z')});await page.clock.pauseAt(new Date('2026-11-01T12:00:01Z'))
 await confirm(page,kind,dialog);await expect.poll(()=>f.pending).not.toBe(null)
 await expect(page.locator(`[data-config-path="${paths[kind]}"]`)).toHaveCount(1)
 await expect(page.locator('.config-card-dissolve')).toHaveCount(0)
 f.complete(true)
 const layer=page.locator('.config-card-dissolve');await expect(layer).toHaveCount(1);await expect(layer).toHaveAttribute('aria-hidden','true')
 const canvas=await layer.locator('canvas').elementHandle()
 await page.clock.runFor(420)
 expect(await canvas.evaluate(el=>{const d=el.getContext('2d').getImageData(0,0,el.width,el.height).data;return {pixels:el.width*el.height,painted:d.some((v,i)=>i%4===3&&v>0)}})).toEqual({pixels:await canvas.evaluate(el=>el.width*el.height),painted:true})
 expect(await canvas.evaluate(el=>el.width*el.height)).toBeLessThanOrEqual(600000)
 await expect(page.locator(`[data-config-path="${paths[kind]}"]`)).toHaveCount(0)
 await expect(layer).toHaveCSS('pointer-events','none')
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
 await page.screenshot({path:info.outputPath(`dust-${kind}.png`),animations:'disabled',timeout:5000})
 await page.clock.runFor(380);await expect(layer).toHaveCount(1)
 await page.clock.runFor(700);await expect(layer).toHaveCount(0);expect(await canvas.evaluate(el=>el.width===0&&el.height===0)).toBe(true)
 expect(f.writes).toHaveLength(1);expect(f.unexpected).toEqual([])
})

test('cancelled failed and reduced-motion deletes preserve confirmation semantics',async({page})=>{
 const f=await fixture(page);await page.goto('/storage');let dialog=await askDelete(page,'storage')
 await dialog.getByRole('button',{name:'取消',exact:true}).click();expect(f.writes).toHaveLength(0);await expect(page.locator('.config-card-dissolve')).toHaveCount(0)
 dialog=await askDelete(page,'storage');await confirm(page,'storage',dialog);await expect.poll(()=>f.pending).not.toBe(null);f.complete(false)
 await expect(page.locator('.toast-stack')).toContainText('删除失败');await expect(page.locator('[data-config-path="/storages/annual"]')).toHaveCount(1);await expect(page.locator('.config-card-dissolve')).toHaveCount(0)
 await page.emulateMedia({reducedMotion:'reduce'});await confirm(page,'storage',dialog);await expect.poll(()=>f.pending).not.toBe(null);f.complete(true)
 await expect(page.locator('[data-config-path="/storages/annual"]')).toHaveCount(0);await expect(page.locator('.config-card-dissolve')).toHaveCount(0)
 expect(f.writes).toHaveLength(2);expect(f.unexpected).toEqual([])
})

test('monitor PUT removal retains its card until commit and dissolves only on success',async({page})=>{
 const f=await fixture(page);await page.goto(routes.rapid);const dialog=await askDelete(page,'rapid')
 await confirm(page,'rapid',dialog);await expect.poll(()=>f.pending).toBe('/api/115-rapid')
 await expect(page.locator('[data-config-id="w"]')).toHaveCount(1);await expect(page.locator('.config-card-dissolve')).toHaveCount(0)
 f.complete(false);await expect(page.locator('.toast-stack')).toContainText('删除失败');await expect(page.locator('[data-config-id="w"]')).toHaveCount(1)
 await page.clock.install({time:new Date('2026-11-01T12:00:00Z')});await page.clock.pauseAt(new Date('2026-11-01T12:00:01Z'))
 await confirm(page,'rapid',dialog);await expect.poll(()=>f.pending).toBe('/api/115-rapid');f.complete(true)
 await expect(page.locator('.config-card-dissolve')).toHaveCount(1);await page.clock.runFor(1500);await expect(page.locator('[data-config-id="w"]')).toHaveCount(0);await expect(page.locator('.config-card-dissolve')).toHaveCount(0)
 expect(f.unexpected).toEqual([])
})

for(const cleanup of ['navigation','reduced','background'])test(`active removal releases canvas on ${cleanup}`,async({page})=>{
 const f=await fixture(page);await page.goto('/storage');const dialog=await askDelete(page,'storage')
 await page.clock.install({time:new Date('2026-11-01T12:00:00Z')});await page.clock.pauseAt(new Date('2026-11-01T12:00:01Z'))
 await confirm(page,'storage',dialog);await expect.poll(()=>f.pending).not.toBe(null);f.complete(true)
 const layer=page.locator('.config-card-dissolve');await expect(layer).toHaveCount(1);const canvas=await layer.locator('canvas').elementHandle()
 await page.clock.runFor(150)
 if(cleanup==='navigation')await page.getByRole('link',{name:'任务中心',exact:true}).click()
 else if(cleanup==='reduced')await page.emulateMedia({reducedMotion:'reduce'})
 else await page.evaluate(()=>{Object.defineProperty(document,'hidden',{configurable:true,get:()=>true});document.dispatchEvent(new Event('visibilitychange'))})
 await expect(layer).toHaveCount(0);expect(await canvas.evaluate(el=>el.width===0&&el.height===0)).toBe(true)
 expect(f.unexpected).toEqual([])
})

for(const [width,height,theme] of [[390,844,'dark'],[844,390,'light']])test(`mobile card dust stays within viewport ${width}`,async({page},info)=>{
 const f=await fixture(page);await page.setViewportSize({width,height});await page.goto('/storage');await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
 const dialog=await askDelete(page,'storage')
 await page.clock.install({time:new Date('2026-11-01T12:00:00Z')});await page.clock.pauseAt(new Date('2026-11-01T12:00:01Z'))
 await confirm(page,'storage',dialog);await expect.poll(()=>f.pending).not.toBe(null);f.complete(true)
 const layer=page.locator('.config-card-dissolve');await expect(layer).toHaveCount(1);await page.clock.runFor(420)
 expect(await layer.locator('canvas').evaluate(el=>el.width*el.height)).toBeLessThanOrEqual(600000)
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
 await page.screenshot({path:info.outputPath(`dust-mobile-${width}-${theme}.png`),animations:'disabled',timeout:5000})
 await page.clock.runFor(1500);await expect(layer).toHaveCount(0);expect(f.unexpected).toEqual([])
})
