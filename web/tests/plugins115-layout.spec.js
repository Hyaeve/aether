import {test,expect} from '@playwright/test'
import {captureScreenshot} from './helpers/screenshot.js'

const longName='这是一部文件名称非常长需要完整显示的电影'.repeat(8)+'.mkv'
async function fixture(page){
 const storages=[{id:'p',name:'115 家庭影音',type:'115',enabled:true,accountID:'123',config:{}},{id:'p2',name:'115 第二账号',type:'115',enabled:true,accountID:'456',config:{}}]
 let folder={enabled:true,rules:[{id:'r',storageId:'p',directory:'10',label:'/影音/待清理',cron:'0 3 * * *',days:5}]},signin={enabled:true,rules:[]}
 const calls=[]
 await page.route('**/api/**',route=>{
  const req=route.request(),u=new URL(req.url());let value={}
  if(u.pathname==='/api/auth/status')value={initialized:true,authenticated:true}
  else if(u.pathname==='/api/state')value={username:'plugin-layout',storages,tasks:[],settings:{},logs:[],cache:{},traffic:{}}
  else if(u.pathname==='/api/115-signin'){if(req.method()==='PUT'){signin=req.postDataJSON();calls.push('save-signin')}value=signin}
  else if(u.pathname==='/api/115-signin/run'){calls.push('signin');value={message:'签到成功'}}
  else if(u.pathname==='/api/115-folder-cleanup'){if(req.method()==='PUT')folder=req.postDataJSON();value=folder}
  else if(u.pathname==='/api/115-folder-cleanup/run'){calls.push('folder-run');value={count:2}}
  else if(u.pathname==='/api/115-simulcast')value={p:{enabled:true,directory:'10',directoryLabel:'/影音/同播',retentionHours:12}}
  else if(u.pathname==='/api/115-simulcast/enabled')value={enabled:true}
  else if(u.pathname==='/api/115-recycle-cleanup')value={enabled:true,rules:[{storageId:'p',cron:'0 3 * * *',days:10}]}
  else if(u.pathname==='/api/quark-takeover')value={enabled:false,bindings:[]}
  else if(u.pathname.startsWith('/api/plugins/'))value={enabled:false}
  else if(u.pathname==='/api/cron/validate')value={error:req.postDataJSON().expression==='*/14 * * * *'?'Cron计划相邻执行间隔至少15分钟':''}
  else if(u.pathname==='/api/115-rapid')value={manual:{},watches:[]}
  else if(u.pathname==='/api/115-rapid/records')value={items:[{id:'a',name:longName,sourceKind:'monitor',source:'监控 · 家庭影视资料监控',status:'success',size:100,time:'2026-10-28T18:59:00+08:00'},{id:'b',name:'music.flac',sourceKind:'manual',source:'手动',status:'success',size:100,time:'2026-10-28T18:59:00+08:00'}],page:1,pages:1,total:2}
  else return route.fulfill({status:500,json:{error:`Unmocked ${u.pathname}`}})
  return route.fulfill({json:value})
 })
 return {calls,get signin(){return signin}}
}

for(const [width,theme] of [[1440,'light'],[1440,'dark'],[390,'light'],[390,'dark'],[320,'light']]){
 test(`rapid record icon source and filename tooltip ${width} ${theme}`,async({page},info)=>{
  await fixture(page);await page.setViewportSize({width,height:900});await page.goto('/tools/115-rapid')
  await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
  const panel=page.locator('.rapid115-records')
  await expect(panel.locator('h3 svg')).toHaveClass(/lucide-list-checks/)
  await expect(panel.locator('.rapid115-source.monitor')).toHaveText('家庭影视资…')
  await expect(panel.locator('.rapid115-source.manual')).toHaveText('手动执行')
  await expect(panel.locator('.rapid115-source.monitor')).toHaveCSS('color',theme==='light'?'rgb(53, 107, 140)':'rgb(154, 200, 228)')
  await expect.poll(()=>page.locator('.page-content').evaluate(el=>el.getAnimations({subtree:true}).every(a=>a.playState==='finished'||a.effect?.getTiming().iterations===Infinity))).toBe(true)
  const filename=panel.locator('.rapid115-record-name').first()
  await filename.evaluate(el=>el.scrollIntoView({block:'center',behavior:'instant'}))
  await expect.poll(async()=>{const r=await filename.boundingBox();return r.y+r.height<850}).toBe(true)
  await page.mouse.move(0,0)
  await filename.hover()
  const tip=page.getByRole('tooltip')
  await expect(tip).toHaveText(longName)
  expect(await tip.evaluate(el=>el.getBoundingClientRect().width)).toBe(Math.min(480,width-16))
  expect(await tip.evaluate(el=>getComputedStyle(el).backgroundColor)).not.toBe('rgb(255, 255, 255)')
  await captureScreenshot(page,{path:info.outputPath(`rapid-tooltip-${width}-${theme}.png`)})
 })
 test(`115 plugin dialogs share task rows and footer controls ${width} ${theme}`,async({page},info)=>{
  const f=await fixture(page);await page.setViewportSize({width,height:900});await page.goto('/tools')
  await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
  await expect(page.locator('.plugin-card').first()).toHaveAttribute('aria-label','115 自动签到')
  for(const name of ['115 自动签到','115 文件夹清理']){
   const card=page.getByRole('button',{name,exact:true})
   await expect(card.locator('.provider-logo')).toHaveAttribute('src',/115/)
   await expect(card.locator('.plugin-symbol>svg')).toHaveCount(1)
  }
  for(const [name,add] of [['115 同播复制','添加同播'],['115 回收站清理','添加清理任务'],['115 文件夹清理','添加文件夹']]){
   await page.getByRole('button',{name,exact:true}).click()
   const dialog=page.getByRole('dialog',{name,exact:true})
   await expect(dialog.locator('.modal-footer').getByRole('button',{name:add,exact:true})).toBeVisible()
   await expect(dialog.getByRole('switch')).toHaveCount(0)
   await expect(dialog.locator('.plugin-task-row .provider-icon')).toHaveCSS('width','40px')
   await expect(dialog.locator('.plugin-task-row strong')).toHaveCSS('font-size','16px')
   await expect(dialog.locator('.plugin-task-list')).toHaveCSS('scrollbar-width','none')
   expect(await dialog.evaluate(el=>el.scrollWidth<=el.clientWidth)).toBe(true)
   await captureScreenshot(page,{path:info.outputPath(`${name}-${width}-${theme}.png`)})
   if(name==='115 文件夹清理'){
    await dialog.getByRole('button',{name:/执行文件夹清理/}).click()
    expect(f.calls).not.toContain('folder-run')
    await page.getByRole('dialog',{name:'执行文件夹清理',exact:true}).getByRole('button',{name:'确认清理'}).click()
    await expect.poll(()=>f.calls.includes('folder-run')).toBe(true)
    await dialog.getByRole('button',{name:/编辑文件夹清理/}).click()
    const editor=page.getByRole('dialog',{name:'编辑清理文件夹',exact:true})
    const y=await editor.locator('.folder-cleanup-schedule .cron-input,.folder-cleanup-schedule .number-control').evaluateAll(els=>els.map(el=>el.getBoundingClientRect().top))
    expect(y).toHaveLength(2);expect(Math.abs(y[0]-y[1])).toBeLessThan(1)
    await page.keyboard.press('Escape')
   }
   await page.keyboard.press('Escape')
  }
  await page.getByRole('button',{name:'115 自动签到',exact:true}).click()
  const dialog=page.getByRole('dialog',{name:'115 自动签到',exact:true})
  await dialog.getByRole('button',{name:'添加签到',exact:true}).click()
  const editor=page.getByRole('dialog',{name:'添加签到',exact:true})
  await editor.getByRole('textbox',{name:'签到Cron表达式'}).fill('*/14 * * * *')
  await expect(editor.getByRole('button',{name:'Cron计划不可用原因'})).toBeVisible()
  await editor.getByRole('textbox',{name:'签到Cron表达式'}).fill('0 8 * * *')
  await editor.getByRole('button',{name:'保存',exact:true}).click()
  await expect.poll(()=>f.signin.rules.length).toBe(1)
  await expect(dialog.locator('.plugin-task-row')).toHaveCount(1)
  await dialog.getByRole('button',{name:'添加签到',exact:true}).click()
  await page.getByRole('dialog',{name:'添加签到',exact:true}).getByRole('button',{name:'签到115存储池'}).click()
  await expect(page.getByRole('option')).toHaveText(['115 第二账号'])
  await page.getByRole('option',{name:'115 第二账号',exact:true}).click()
  await page.getByRole('dialog',{name:'添加签到',exact:true}).getByRole('button',{name:'取消',exact:true}).click()
  await dialog.getByRole('button',{name:/执行签到/}).click()
  await expect.poll(()=>f.calls.includes('signin')).toBe(true)
  await captureScreenshot(page,{path:info.outputPath(`signin-${width}-${theme}.png`)})
  await dialog.getByRole('button',{name:/编辑签到/}).click()
  await page.getByRole('dialog',{name:'编辑签到',exact:true}).getByRole('textbox',{name:'签到Cron表达式'}).fill('0 9 * * *')
  await page.getByRole('dialog',{name:'编辑签到',exact:true}).getByRole('button',{name:'保存',exact:true}).click()
  await expect.poll(()=>f.signin.rules[0].cron).toBe('0 9 * * *')
  await dialog.getByRole('button',{name:/删除签到/}).click()
  expect(f.signin.rules).toHaveLength(1)
  await page.getByRole('dialog',{name:'删除签到任务',exact:true}).getByRole('button',{name:'确认删除',exact:true}).click()
  await expect.poll(()=>f.signin.rules.length).toBe(0)
 })
}
