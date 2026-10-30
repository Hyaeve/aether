import {test,expect} from '@playwright/test'

async function fixture(page){
  let saved,operation,progress=2
  const storages=[{id:'p',name:'115影音',type:'115',enabled:true,config:{}},{id:'m',name:'移动影音',type:'mobile',enabled:true,config:{mode:'native'}},{id:'q',name:'夸克影音',type:'quark',enabled:true,config:{}}]
  const targets=storages.flatMap(s=>Array.from({length:5},(_,i)=>({id:s.id+i,provider:s.type,storageId:s.id,parent:'parent'+i,path:'/Target '+i,shareTransfer:true,updateDays:7})))
  let links=[{id:'live',targetId:'q0',name:'影视分享',provider:'quark',url:'https://pan.quark.cn/s/code',directoryName:'剧集',cron:'0 */12 * * *',nextRun:'2026-11-05T13:00:00+08:00',status:'completed',message:'新增提交2个文件，跳过3个文件'}]
  const records=['bot','browser','cron'].map((source,i)=>({id:String(i),provider:storages[i].type,name:'分享资源 '+i,url:'https://pan.quark.cn/s/code',directoryName:'保存目录',source,time:'2026-10-29T13:19:00+08:00',status:i===1?'failed':'completed',message:'新增2个文件，跳过3个文件'}))
  await page.route('**/api/**',async route=>{
    const request=route.request(),p=new URL(request.url()).pathname,method=request.method()
    let json
    if(p==='/api/auth/status')json={initialized:true,authenticated:true}
    else if(p==='/api/state')json={username:'live-test',storages,tasks:[],settings:{},cache:{},traffic:{}}
    else if(p==='/api/notification-service')json={telegram:{userIds:[]},targets}
    else if(p==='/api/share-transfers')json=records
    else if(p==='/api/share-live')json=links
    else if(p==='/api/cron/validate')json={error:''}
    else if(p==='/api/notification-service/targets'){saved=request.postDataJSON();json={targets:saved.targets}}
    else if(p==='/api/share-live/live'&&method==='PUT'){saved=request.postDataJSON();links=[{...links[0],...saved}];json={ok:true}}
    else if(p.startsWith('/api/share-live/live/')){operation={path:p,body:request.postDataJSON()};if(!p.endsWith('/run'))links=[];json={ok:true}}
    else if(p==='/api/tasks/panel')json={items:[{id:'task:live',kind:'cas',source:'task',name:'真实进度',status:'running',message:'正在生成',processed:progress,total:10,totalKnown:true,unit:'项'}]}
    else return route.fulfill({status:500,json:{error:'未模拟接口 '+p}})
    await route.fulfill({json})
  })
  return {saved:()=>saved,operation:()=>operation,progress:value=>{progress=value}}
}

for(const width of [1440,390])for(const theme of ['light','dark'])test(`share records and three visible target rows ${width} ${theme}`,async({page},info)=>{
  await fixture(page)
  await page.setViewportSize({width,height:900})
  await page.goto('/notifications/transfers')
  await page.evaluate(theme=>document.documentElement.dataset.theme=theme,theme)
  await expect(page.locator('.telegram-target-card')).toHaveCount(15)
  await expect(page.locator('.telegram-target-avatar').first()).toHaveText('11')
  await expect(page.locator('.telegram-target-card>.provider-icon')).toHaveCount(0)
  for(const list of await page.locator('.telegram-target-list').all()){
    const value=await list.evaluate(el=>{const parent=el.getBoundingClientRect();const rows=[...el.children].map(child=>child.getBoundingClientRect());return {scroll:el.scrollHeight>el.clientHeight,third:rows[2].bottom<=parent.bottom+3,fourth:rows[3].top>=parent.bottom-3,bar:getComputedStyle(el).scrollbarWidth}})
    expect(value).toEqual({scroll:true,third:true,fourth:true,bar:'none'})
  }
  const colors=await page.locator('.telegram-pool').evaluateAll(els=>els.map(el=>getComputedStyle(el).backgroundColor))
  expect(new Set(colors).size).toBe(3)
  await expect(page.getByRole('region',{name:'转存记录'})).toContainText('Bot 交互')
  await expect(page.getByRole('region',{name:'转存记录'})).toContainText('浏览器转存')
  await expect(page.getByRole('region',{name:'转存记录'})).toContainText('活链计划')
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
  await page.screenshot({path:info.outputPath('share-records.png'),fullPage:true})
  await page.getByRole('button',{name:'活动链接 /Target 0',exact:true}).last().click()
  await expect(page.getByRole('dialog',{name:'活动链接',exact:true})).toContainText('影视分享')
  await page.getByRole('button',{name:'编辑活链 影视分享',exact:true}).click()
  const modal=page.getByRole('dialog',{name:'编辑分享链接',exact:true})
  const fields=await modal.locator('.share-live-form label').evaluateAll(els=>els.map(el=>{const r=el.getBoundingClientRect();return {x:r.x,y:r.y}}))
  expect(Math.abs(fields[0].y-fields[1].y)).toBeLessThan(1)
  expect(Math.abs(fields[2].y-fields[3].y)).toBeLessThan(1)
  expect(fields[2].y).toBeGreaterThan(fields[0].y)
  expect(fields[1].x).toBeGreaterThan(fields[0].x)
  await page.screenshot({path:info.outputPath('share-live-edit.png')})
  await page.keyboard.press('Escape')
  await expect(modal).toHaveCount(0)
})

test('target update days and live rename, remove-only and delete confirmation',async({page})=>{
  const data=await fixture(page)
  await page.goto('/notifications/transfers')
  await page.getByRole('button',{name:'添加夸克云盘转存目录',exact:true}).click()
  await expect(page.getByRole('spinbutton',{name:'更新周期',exact:true})).toHaveValue('7')
  await expect(page.getByRole('spinbutton',{name:'更新周期',exact:true})).toHaveAttribute('min','0')
  await expect(page.getByRole('spinbutton',{name:'更新周期',exact:true})).toHaveAttribute('max','365')
  await page.keyboard.press('Escape')
  await page.getByRole('button',{name:'活动链接 /Target 0',exact:true}).last().click()
  await page.getByRole('button',{name:'编辑活链 影视分享',exact:true}).click()
  await page.getByRole('textbox',{name:'活链目录名称',exact:true}).fill('新目录')
  await page.getByRole('textbox',{name:'活链Cron表达式',exact:true}).fill('')
  await page.getByRole('dialog',{name:'编辑分享链接',exact:true}).getByRole('button',{name:'保存',exact:true}).click()
  await expect.poll(()=>data.saved()?.directoryName).toBe('新目录')
  expect(data.saved().cron).toBe('')
  await page.getByRole('button',{name:'执行活链 影视分享',exact:true}).click()
  await expect.poll(()=>data.operation()?.path).toBe('/api/share-live/live/run')
  await page.getByRole('button',{name:'删除活链 影视分享',exact:true}).click()
  const confirm=page.getByRole('dialog',{name:'删除活动链接',exact:true})
  const remove=confirm.getByRole('button',{name:'移除记录',exact:true}),del=confirm.getByRole('button',{name:'确认删除',exact:true})
  expect((await remove.boundingBox()).x).toBeLessThan((await del.boundingBox()).x)
  await remove.click()
  await expect.poll(()=>data.operation()?.path).toBe('/api/share-live/live/remove')
  expect(data.operation().body).toEqual({})
  await page.reload()
  await fixture(page)
  await page.reload()
  await page.getByRole('button',{name:'活动链接 /Target 0',exact:true}).last().click()
  await page.getByRole('button',{name:'删除活链 影视分享',exact:true}).click()
  await page.getByRole('button',{name:'确认删除',exact:true}).click()
  await expect(page.getByRole('dialog',{name:'删除活动链接',exact:true})).toHaveCount(0)
})

test('task panel has no statistics and real progress moves on refresh',async({page})=>{
  const data=await fixture(page)
  await page.goto('/tasks/panel')
  await expect(page.locator('.task-panel-metrics,.metric-strip')).toHaveCount(0)
  const bar=page.getByRole('progressbar',{name:'真实进度进度',exact:true})
  await expect(bar).toHaveAttribute('aria-valuenow','20')
  data.progress(7)
  await page.getByRole('button',{name:'刷新任务面板',exact:true}).click()
  await expect(bar).toHaveAttribute('aria-valuenow','70')
  expect(await bar.locator('span').evaluate(el=>el.style.width)).toBe('70%')
})
