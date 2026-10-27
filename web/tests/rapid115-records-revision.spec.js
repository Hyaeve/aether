import {test,expect} from '@playwright/test'

async function fixture(page){
 let config={manual:{source:'/media/incoming',storageId:'p',target:'99',targetLabel:'115 / 目标',extensions:'',successAction:'keep',failureAction:'skip'},watches:[]}
 const items=Array.from({length:67},(_,i)=>({id:String(i),time:'2026-10-26T10:00:00+08:00',name:i===0?'完整的影片名称'.repeat(12)+'第一集.mkv':`影片 ${i}.mkv`,size:1048576,pickcode:'abcdefghijklmnopq',status:i===0?'running':i%3===0?'failed':'success',source:'监控 · 影音',done:i===0?25:1048576,total:i===0?100:1048576,phase:i===0?'计算文件摘要':''}))
 await page.route('**/api/**',route=>{
  const req=route.request(),u=new URL(req.url());let value={}
  if(u.pathname==='/api/auth/status')value={initialized:true,authenticated:true}
  else if(u.pathname==='/api/state')value={username:'rapid-revision',storages:[{id:'p',name:'115',type:'115',enabled:true,config:{}}],tasks:[],settings:{},cache:{},logs:[]}
  else if(u.pathname==='/api/115-rapid'){if(req.method()==='PUT')config=req.postDataJSON();value=config}
  else if(u.pathname==='/api/115-rapid/records'){const current=Math.max(1,Math.min(3,Number(u.searchParams.get('page'))||1));value={items:items.slice((current-1)*30,current*30),page:current,pages:3,total:items.length,running:true,manualRunning:false,monitorRunning:true}}
  else if(u.pathname==='/api/files')value=[]
  return route.fulfill({json:value})
 })
 return {items,get config(){return config}}
}

test('monitor activity does not disable manual start and deletion is monitor only',async({page})=>{
 await fixture(page);await page.goto('/tools/115-rapid')
 await expect(page.getByRole('button',{name:'开始秒传',exact:true})).toBeEnabled()
 const manual=page.locator('.rapid115-top>.rapid115-section').first()
 await manual.getByRole('button',{name:'秒传失败处理',exact:true}).click()
 await expect(page.getByRole('option',{name:'删除',exact:true})).toHaveCount(0)
 await page.keyboard.press('Escape')
 await page.getByRole('button',{name:'添加监控',exact:true}).click()
 const dialog=page.getByRole('dialog',{name:'添加监控'})
 await dialog.getByRole('button',{name:'秒传失败处理',exact:true}).click()
 await page.getByRole('option',{name:'删除',exact:true}).click()
 await expect(dialog.getByRole('button',{name:'秒传失败处理',exact:true})).toContainText('删除')
})

test('rapid records have thirty item pages, bounded jumps and fixed parallel surfaces',async({page},info)=>{
 await fixture(page);await page.goto('/tools/115-rapid')
 const panel=page.locator('.rapid115-records'),rows=panel.locator('.rapid115-record')
 await expect(rows).toHaveCount(30)
 await expect(panel.getByRole('button',{name:'第一页',exact:true}).locator('svg')).toHaveClass(/lucide-chevrons-left/)
 await expect(panel.getByRole('button',{name:'最后一页',exact:true}).locator('svg')).toHaveClass(/lucide-chevrons-right/)
 await expect(panel.locator('.rapid115-record-columns>span')).toHaveText(['文件名称','文件大小','PickCode','触发来源','执行时间','秒传状态'])
 const height=await panel.evaluate(el=>el.getBoundingClientRect().height)
 await panel.getByRole('button',{name:'最后一页',exact:true}).click()
 await expect(rows).toHaveCount(7);await expect(rows.first()).toContainText('影片 60.mkv')
 expect(await panel.evaluate(el=>el.getBoundingClientRect().height)).toBe(height)
 await panel.getByRole('button',{name:'第一页',exact:true}).click();await expect(rows).toHaveCount(30)
 await panel.getByRole('spinbutton',{name:'跳转页码',exact:true}).fill('999')
 await panel.getByRole('spinbutton',{name:'跳转页码',exact:true}).press('Enter')
 await expect(rows).toHaveCount(7);await expect(panel.getByRole('spinbutton',{name:'跳转页码'})).toHaveValue('3')
 await panel.getByRole('button',{name:'上一页',exact:true}).click();await expect(rows.first()).toContainText('影片 30.mkv')
 for(const [width,theme] of [[1440,'light'],[1440,'dark'],[390,'light'],[390,'dark']]){
  await page.setViewportSize({width,height:900});await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
  await expect.poll(()=>page.locator('.rapid115-section').evaluateAll(els=>new Set(els.map(el=>getComputedStyle(el).backgroundColor)).size)).toBe(1)
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
  if(theme==='light')await expect(rows.first()).toHaveCSS('background-color','rgb(255, 255, 255)')
  await panel.scrollIntoViewIfNeeded()
  await expect(panel.getByRole('button',{name:'跳转到指定页',exact:true})).toBeInViewport()
  await expect.poll(()=>page.locator('.rapid115-page').evaluate(el=>el.getAnimations({subtree:true}).every(a=>a.playState==='finished'||a.effect?.getTiming().iterations===Infinity))).toBe(true)
  await page.screenshot({path:info.outputPath(`rapid-records-${width}-${theme}.png`),fullPage:true})
 }
})

test('rapid rows expose stationary names and real stage progress without indeterminate motion',async({page})=>{
 const f=await fixture(page);await page.goto('/tools/115-rapid')
 const row=page.locator('.rapid115-record').first(),name=row.locator('.rapid115-record-name'),progress=row.getByRole('progressbar')
 await expect(name).toHaveCSS('white-space','nowrap')
 await expect(name).toHaveAttribute('data-tooltip-delay','300')
 expect(await name.evaluate(el=>el.scrollWidth>el.clientWidth)).toBe(true)
 await expect(row.locator('.rapid115-pickcode')).toHaveText('abcdefghijklmnopq')
 expect(await row.locator('.rapid115-pickcode').evaluate(el=>el.scrollWidth<=el.clientWidth)).toBe(true)
 await expect(progress).toHaveAttribute('value','0.25')
 await expect(row).toContainText('计算文件摘要')
 f.items[0].done=70
 await expect(progress).toHaveAttribute('value','0.7')
 await expect(page.locator('progress:indeterminate')).toHaveCount(0)
 await name.hover();await expect(page.getByRole('tooltip')).toHaveText(f.items[0].name)
 const box=await name.boundingBox();await page.mouse.move(box.x+12,box.y+10)
 await expect(page.getByRole('tooltip')).toHaveCount(0)
 await page.mouse.move(1,1)
})

test('deleting after retries requires confirmation and invalid Cron stays editable with a bubble',async({page})=>{
 const f=await fixture(page);let attempted=0
 await page.route('**/api/115-rapid',route=>{
  if(route.request().method()==='PUT'){attempted++;return route.fulfill({status:400,json:{error:'带重试的Cron计划相邻执行间隔必须严格大于120min'}})}
  return route.fallback()
 })
 await page.goto('/tools/115-rapid');await page.getByRole('button',{name:'添加监控',exact:true}).click()
 const editor=page.getByRole('dialog',{name:'添加监控'})
 await editor.getByLabel('监控名称',{exact:true}).fill('重试删除')
 await editor.getByRole('textbox',{name:'待传目录',exact:true}).fill('/media/watch')
 await editor.getByRole('textbox',{name:'目标目录',exact:true}).fill('/')
 await editor.getByRole('textbox',{name:'目标目录',exact:true}).press('Tab')
 await editor.getByRole('button',{name:'监控模式',exact:true}).click();await page.getByRole('option',{name:'Cron表达式',exact:true}).click()
 await editor.getByRole('textbox',{name:'Cron表达式',exact:true}).fill('0 */2 * * *')
 await editor.getByRole('button',{name:'秒传失败处理',exact:true}).click();await page.getByRole('option',{name:'删除',exact:true}).click()
 await editor.getByRole('button',{name:'添加',exact:true}).click()
 const confirmation=page.getByRole('dialog',{name:'确认重试后删除'})
 await expect(confirmation).toBeVisible();expect(attempted).toBe(0);expect(f.config.watches).toHaveLength(0)
 await confirmation.getByRole('button',{name:'确认保存',exact:true}).click()
 await expect(page.locator('.toast.error')).toContainText('严格大于120min')
 await expect(editor).toBeVisible();await expect(editor.getByRole('textbox',{name:'Cron表达式',exact:true})).toHaveValue('0 */2 * * *')
 await expect(editor.getByRole('button',{name:'添加',exact:true})).toBeEnabled()
})

test('cloud download bars match the timestamp width on desktop and mobile',async({page},info)=>{
 await fixture(page)
 await page.route('**/api/cloud/manage?**',route=>route.fulfill({json:{items:[{id:'task',name:'影片.mkv',size:1048576,time:'2026-10-26T10:00:00+08:00',status:'running',progress:.4,url:'https://example.test/file',path:'/影音'}]}}))
 await page.goto('/storage/downloads')
 for(const width of [1440,390]){
  await page.setViewportSize({width,height:900})
  const execution=page.locator('.cloud-execution').first()
  await expect(execution.locator('progress')).toHaveAttribute('value','0.4')
  const sizes=await execution.evaluate(el=>({time:el.querySelector('time').getBoundingClientRect().width,bar:el.querySelector('progress').getBoundingClientRect().width}))
  expect(Math.abs(sizes.time-sizes.bar)).toBeLessThan(1)
  await page.screenshot({path:info.outputPath(`cloud-progress-width-${width}.png`)})
 }
})
