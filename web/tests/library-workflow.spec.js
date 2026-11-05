import {test,expect} from '@playwright/test'
import {ensureWorkspaceAccount} from './helpers/workspace.js'
import {captureScreenshot} from './helpers/screenshot.js'
const browserName=process.env.AETHER_WORKFLOW_BROWSER||'chromium'
test.use({browserName,...(browserName==='webkit'?{isMobile:true,hasTouch:true}:{})})

async function library(page,provider='mobile',count=1){
 const cards=Array.from({length:count},(_,i)=>({id:`c${i}`,name:`作品 ${i+1}`,url:`https://example.test/share/${i}`,storageId:'pool',directory:'/media',directoryName:`作品${i+1}`,enabled:true,cron:i%4===0?'':'0 13 * * *',result:{status:i%4===1?'invalid':i%4===2?'failed':'completed',message:'任务结果'}}))
 const settings={storageId:'pool',storageIds:['pool'],directory:'/media',mediaExtensions:'mkv',metadataExtensions:'nfo',rotationHours:24,updateDays:7,invalidDays:15,retentionHours:12,playbackMode:'share'},writes=[]
 await page.route('**/api/**',route=>{
  const r=route.request(),p=new URL(r.url()).pathname,base='/api/virtual-library/'+provider;let json={}
  if(p==='/api/auth/status')json={initialized:true,authenticated:true}
  else if(p==='/api/state')json={username:'workflow',storages:[{id:'pool',type:provider,name:'解析账号',enabled:true,config:{mode:'native'}}],tasks:[],settings:{},traffic:{},cache:{}}
  else if(p===base&&r.method()==='GET')json={cards,settings,stats:{strm:count}}
  else if(p===base+'/settings'){writes.push(r.postDataJSON());Object.assign(settings,r.postDataJSON());json=settings}
  else if(p===base+'/resolve')json={name:'新的分享'}
  else if(p.startsWith(base)&&r.method()!=='GET'){writes.push(r.postDataJSON());json={id:'new',message:'已加入执行队列'}}
  else if(p==='/api/generation/jobs')json=[]
  return route.fulfill({json})
 })
 return {cards,writes}
}

for(const [width,height,theme]of [[1440,1000,'light'],[390,844,'dark'],[375,812,'light'],[844,390,'dark']])test(`library short list fills page and actions align ${width}`,async({page},info)=>{
 await library(page);await page.setViewportSize({width,height});await page.goto('/virtual/mobile');await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
 const panel=page.getByRole('region',{name:'移动分享链接',exact:true});await expect(panel).toBeVisible()
 const controls=await panel.locator('.share-row-actions button').evaluateAll(nodes=>nodes.map(n=>({name:n.textContent||n.ariaLabel,x:n.getBoundingClientRect().x})))
 expect(controls[0].name).toBe('执行');expect(controls[0].x).toBeLessThan(controls[1].x)
 const alignment=await panel.locator('.share-library-search').evaluate(el=>{const icon=el.querySelector('svg').getBoundingClientRect(),input=el.querySelector('input').getBoundingClientRect();return Math.abs(icon.y+icon.height/2-input.y-input.height/2)});expect(alignment).toBeLessThan(2)
 const bounds=await panel.boundingBox(),gap=await panel.evaluate(el=>parseFloat(getComputedStyle(el.closest('.page-content')).paddingBottom));if(height>500)expect(Math.abs(bounds.y+bounds.height-(height-gap))).toBeLessThan(3)
 expect(await panel.evaluate(el=>el.scrollHeight<=el.clientHeight)).toBe(true)
 await expect(panel.getByRole('button',{name:'下一页',exact:true})).toHaveCount(0);await expect(panel.getByRole('spinbutton',{name:'跳转页码',exact:true})).toHaveCount(0)
 await panel.getByRole('button',{name:'筛选链库状态',exact:true}).click();await page.getByRole('option',{name:'封存中',exact:true}).click();await expect(panel).toContainText('共 1 条')
 await panel.getByRole('searchbox',{name:'搜索链库',exact:true}).fill('不存在');await expect(panel).toContainText('没有匹配的分享链接')
 await panel.getByRole('searchbox',{name:'搜索链库',exact:true}).clear()
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
 await captureScreenshot(page,{path:info.outputPath(`library-${width}-${theme}.png`)})
})

test('library filters are single selection, reset pages, and jump values clamp',async({page})=>{
 await library(page,'quark',45);await page.goto('/virtual/quark');const panel=page.getByRole('region',{name:'夸克分享链接',exact:true})
 const jump=panel.getByRole('spinbutton',{name:'跳转页码',exact:true});await jump.scrollIntoViewIfNeeded();await jump.fill('999');await expect(jump).toHaveValue('3');await jump.press('Enter');await expect(panel.getByRole('status',{name:'当前页'})).toHaveText('3 / 3')
 await jump.fill('0');await expect(jump).toHaveValue('1');await jump.press('Enter');await expect(panel.getByRole('status',{name:'当前页'})).toHaveText('1 / 3')
 const filter=panel.getByRole('button',{name:'筛选链库状态',exact:true});await filter.scrollIntoViewIfNeeded();await filter.click();await page.getByRole('option',{name:'失效项',exact:true}).click();await expect(panel).toContainText('共 11 条');await expect(jump).toHaveCount(0)
 await filter.click();await expect(page.getByRole('option',{selected:true})).toHaveCount(1);await expect(page.getByRole('option',{name:'失效项',exact:true})).toHaveAttribute('aria-selected','true');await page.getByRole('option',{name:'失败',exact:true}).click();await expect(panel).toContainText('共 11 条')
 await page.getByRole('searchbox',{name:'搜索链库',exact:true}).fill('作品 3');await expect(panel).toContainText('共 4 条')
})

test('115 uses card management without live settings, archive filter or Cron',async({page},info)=>{
 const f=await library(page,'115');await page.goto('/virtual/115');const panel=page.getByRole('region',{name:'115分享链接',exact:true})
 await expect(panel.getByText('活链',{exact:true})).toHaveCount(0);await expect(panel.getByText('Cron',{exact:true})).toHaveCount(0)
 await panel.getByRole('button',{name:'筛选链库状态',exact:true}).click();await expect(page.getByRole('option',{name:'封存中',exact:true})).toHaveCount(0);await page.keyboard.press('Escape')
 await page.getByRole('button',{name:'编辑 作品 1',exact:true}).click();const dialog=page.getByRole('dialog',{name:'编辑115链接',exact:true});await expect(dialog.getByRole('textbox',{name:'Cron表达式',exact:true})).toHaveCount(0);await expect(dialog.getByRole('button',{name:'解析存储池',exact:true})).toContainText('解析账号');await dialog.getByRole('button',{name:'取消',exact:true}).click()
 await page.goto('/virtual/115/settings');await expect(page.getByRole('spinbutton',{name:'更新周期',exact:true})).toHaveCount(0);await expect(page.getByRole('spinbutton',{name:'轮换时间',exact:true})).toHaveCount(0)
 await page.getByRole('button',{name:'保存设置',exact:true}).click();await expect.poll(()=>f.writes.length).toBe(1);expect(f.writes[0]).toEqual({storageId:'pool',directory:'/media',scrapeEnabled:false})
 await captureScreenshot(page,{path:info.outputPath('115-settings.png')})
})

test('library update period defaults to seven days and is saved independently',async({page})=>{
 const f=await library(page,'mobile',0);await page.goto('/virtual/mobile/settings');const input=page.getByRole('spinbutton',{name:'更新周期',exact:true});await expect(input).toHaveValue('7');await input.fill('3');await page.getByRole('button',{name:'保存设置',exact:true}).click();await expect.poll(()=>f.writes.length).toBe(1);expect(f.writes[0].updateDays).toBe(3)
})

test('default preset families have protected last cards and equal large editors',async({page},info)=>{
 await ensureWorkspaceAccount(page.request);expect((await page.request.post('/api/auth/login',{data:{username:'my-aether-owner',password:'a1'}})).ok()).toBe(true)
 await page.setViewportSize({width:1440,height:1000})
 for(const [kind,label]of [['naming','命名规则'],['categories','媒体分类'],['upgrade','洗版策略']]){
  await page.goto('/rules/'+kind);const cards=page.locator('.rule-preset-card');await expect(cards).toHaveCount(1);await expect(cards.getByRole('button',{name:/删除预设/})).toBeDisabled();await cards.getByRole('button',{name:/编辑预设/}).click();const dialog=page.getByRole('dialog',{name:`编辑${label}预设`,exact:true});await expect(dialog).toBeVisible();expect((await dialog.boundingBox()).width).toBe(960);expect((await dialog.boundingBox()).height).toBe(860);expect(await dialog.evaluate(el=>{const r=el.getBoundingClientRect(),f=el.querySelector('.modal-footer').getBoundingClientRect();return Math.abs(r.bottom-f.bottom)})).toBeLessThan(2)
  await captureScreenshot(page,{path:info.outputPath(`default-${kind}.png`)});await dialog.getByRole('button',{name:'取消',exact:true}).click()
 }
})
