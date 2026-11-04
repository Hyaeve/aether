import {test,expect} from '@playwright/test'
import {captureScreenshot} from './helpers/screenshot.js'

const browserName=process.env.AETHER_BULK_UI_BROWSER||'chromium'
test.use({browserName,...(browserName==='webkit'?{isMobile:true,hasTouch:true}:{})})
const layouts=[[1440,1000,'light'],[390,844,'dark'],[375,812,'light'],[844,390,'dark']]

async function fixture(page){
 const pools=[{id:'p',type:'115',name:'115影视',enabled:true,config:{cookie:'********'},viewRevision:'p1'},{id:'m',type:'mobile',name:'移动影视',enabled:true,config:{mode:'native',authorization:'********'},viewRevision:'m1'},{id:'t',type:'tianyi',name:'天翼影视',enabled:true,config:{refreshToken:'********'},viewRevision:'t1'}]
 const rules={naming:[{id:'n',name:'默认命名'}],categories:[{id:'c',name:'默认分类'}],upgrade:[{id:'u',name:'默认洗版'}]}
 const items=[{id:'a',name:'电影归档',storageId:'p',source:'10',sourcePath:'/待整理',targetStorageId:'p',target:'11',targetPath:'/电影',method:'move',namingRuleId:'n',mediaCategoryRuleId:'c',upgradeRuleId:''},{id:'b',name:'目录待完善',storageId:'removed',source:'20',targetStorageId:'p',target:'21',method:'copy',namingRuleId:'n',mediaCategoryRuleId:'c'}]
 await page.route('**/api/**',route=>{
  const u=new URL(route.request().url()),p=u.pathname
  let json={}
  if(p==='/api/auth/status')json={initialized:true,authenticated:true}
  else if(p==='/api/state')json={username:'bulk-ui',storages:pools,tasks:[],settings:{},cache:{},traffic:{}}
  else if(p==='/api/rule-presets')json=rules[u.searchParams.get('kind')]
  else if(p==='/api/organize-tasks')json=items
  else if(p==='/api/organize-options')json={rules,aiAvailable:false}
  else if(/^\/api\/virtual-library\/(115|mobile|quark|cas)$/.test(p))json={cards:[{id:'one',name:'星际穿越分享',shareName:'星际穿越分享',enabled:true,directory:'/media',directoryName:'星际穿越',cron:'',result:{status:'completed',message:'已生成 12 个文件'}}],settings:{storageIds:[],directory:'/media'},stats:{strm:12}}
  else if(p.endsWith('/usage'))json={used:1,total:10}
  else if(p==='/api/generation/jobs')json=[]
  return route.fulfill({json})
 })
 return {pools,items}
}
for(const [width,height,theme]of layouts)test(`rule add surfaces and organize overview ${width} ${theme}`,async({page},info)=>{
 const errors=[];page.on('pageerror',e=>errors.push(e.message))
 await fixture(page);await page.setViewportSize({width,height});await page.addInitScript(t=>localStorage.setItem('aether-theme',t),theme)
 for(const kind of ['naming','categories','upgrade']){
  await page.goto('/rules/'+kind);await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
  const add=page.locator('.rule-preset-add');await expect(add).toBeEnabled()
  const colors=await add.evaluate(el=>({add:getComputedStyle(el).backgroundColor,page:getComputedStyle(document.querySelector('.page-content')).backgroundColor,bg:getComputedStyle(document.documentElement).getPropertyValue('--bg').trim()}))
  expect(colors.add).not.toBe('rgba(0, 0, 0, 0)')
  expect(await add.evaluate(el=>getComputedStyle(el).backgroundColor!==getComputedStyle(document.querySelector('.thin-scroll-area')).backgroundColor)).toBe(true)
  await add.focus();await expect(add).toBeFocused();expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
  if(kind==='naming')await captureScreenshot(page,{path:info.outputPath(`rule-add-${width}-${theme}.png`)})
 }
 await page.goto('/tasks/organize');await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
 const overview=page.getByRole('region',{name:'整理归档统计',exact:true});await expect(overview.getByRole('img',{name:'配置完整率 50%',exact:true})).toBeVisible()
 for(const [label,count]of [['任务总数','2'],['配置完整','1'],['待完善','1']])await expect(overview.locator('dl>div').filter({has:page.locator('dt',{hasText:label})}).locator('dd')).toHaveText(count)
 await expect(page.locator('.organize-task-card')).toHaveCount(2)
 await expect(page.getByRole('article',{name:'整理归档任务 电影归档',exact:true})).toContainText('配置完整')
 await expect(page.getByRole('article',{name:'整理归档任务 目录待完善',exact:true})).toContainText('待完善配置')
 await expect(page.getByRole('button',{name:'添加整理归档任务',exact:true})).not.toHaveCSS('background-color','rgba(0, 0, 0, 0)')
 expect(await overview.evaluate(el=>el.scrollWidth<=el.clientWidth)).toBe(true)
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
 await captureScreenshot(page,{path:info.outputPath(`organize-${width}-${theme}.png`)})
 expect(errors).toEqual([])
})

test('all four library row icons fill their own rounded square with name initials',async({page},info)=>{
 await fixture(page)
 for(const provider of ['115','mobile','quark','cas']){
  await page.goto('/virtual/'+provider)
  const row=page.locator('.share-library-row').first(),avatar=row.locator('.share-row-avatar')
  await expect(avatar).toHaveText('星际');await expect(avatar.locator('svg,img,.provider-icon')).toHaveCount(0)
  const rect=await avatar.boundingBox();expect(rect.width).toBe(32);expect(rect.height).toBe(32)
  await expect(avatar).toHaveCSS('border-radius','8px')
 }
 await captureScreenshot(page,{path:info.outputPath('library-text-icons.png')})
})

test('membership badges survive slow refresh, stale state navigation and credential changes',async({page},info)=>{
 const errors=[];page.on('pageerror',e=>errors.push(e.message))
 const f=await fixture(page),counts={};let release=false
 const values={p:{tier:'annual'},m:{tier:'diamond'},t:{tier:'platinum'}}
 for(const pool of f.pools)pool.usage={membership:{...values[pool.id],checkedAt:new Date(Date.now()-6*60*1000).toISOString()}}
 await page.route('**/api/storages/*/membership',async route=>{
  const id=route.request().url().split('/').at(-2);counts[id]=(counts[id]||0)+1
  if(!release){await route.fulfill(id==='m'?{json:{membership:null}}:{status:503,json:{error:'暂时不可用'}});return}
  await route.fulfill({json:{membership:{...values[id],checkedAt:new Date().toISOString()}}})
 })
 await page.goto('/storage');await expect(page.locator('.storage-membership')).toHaveCount(3)
 for(const label of ['年费VIP','钻石会员','铂金会员'])await expect(page.getByRole('img',{name:label,exact:true})).toBeVisible()
 await expect.poll(()=>Object.keys(counts).length).toBe(3)
 // SPA navigation triggers /state again; the unchanged six-minute value stays visible.
 await page.getByRole('link',{name:'信息总览',exact:true}).click();await page.getByRole('link',{name:'存储管理',exact:true}).click()
 await expect(page.locator('.storage-membership')).toHaveCount(3)
 release=true;f.pools[1].viewRevision='m2';delete f.pools[1].usage
 await page.getByRole('link',{name:'信息总览',exact:true}).click();await page.getByRole('link',{name:'存储管理',exact:true}).click()
 await expect(page.getByRole('img',{name:'钻石会员',exact:true})).toBeVisible()
 await expect.poll(()=>counts.m).toBeGreaterThan(1)
 await captureScreenshot(page,{path:info.outputPath('badges-mobile-tianyi.png')})
 expect(errors).toEqual([])
})
