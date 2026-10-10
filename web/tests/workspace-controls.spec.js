import {test,expect} from '@playwright/test'

async function setup(page) {
 let saved
 await page.route('**/api/**',r=>{
  const u=new URL(r.request().url());let json={}
  if(u.pathname==='/api/auth/status')json={initialized:true,authenticated:true}
  if(u.pathname==='/api/state')json={username:'workspace-controls',storages:[{id:'s',name:'Local',type:'local',enabled:true,config:{}}],tasks:[],settings:{},cache:{},links:[]}
  if(u.pathname==='/api/files')json=[{id:'/sample.strm',name:'sample.strm'},{id:'/sample.nfo',name:'sample.nfo'}]
  if(u.pathname==='/api/files/text') {
   if(r.request().method()==='PUT'){saved=r.request().postDataJSON();json={ok:true}}
   else json={content:u.searchParams.get('id').endsWith('.nfo')?'<movie><title>Example</title></movie>':'https://example.test/media',revision:'original'}
  }
  if(u.pathname==='/api/logs')json=Array.from({length:20000},(_,i)=>({time:new Date().toISOString(),module:'system',level:'info',message:`record-${i}`}))
  if(u.pathname==='/api/link-playback')json={recentEvents:Array.from({length:20000},(_,i)=>({time:new Date().toISOString(),target:`https://example.test/${i}`,outcome:'redirect',userAgent:'Browser'}))}
  if(u.pathname==='/api/links' || u.pathname==='/api/backup-rules' || u.pathname==='/api/transfers')json=[]
  return r.fulfill({json})
 })
 return ()=>saved
}

test('file manager shares compact STRM/NFO editor and history matches refresh control',async({page},info)=>{
 const saved=await setup(page);await page.goto('/files')
 const history=page.getByRole('button',{name:'历史访问',exact:true}),refresh=page.getByRole('button',{name:'刷新目录',exact:true})
 await expect(history).toHaveCSS('border-top-width','0px')
 const h=await history.locator('svg').boundingBox(),f=await refresh.locator('svg').boundingBox()
 expect(h.width).toBe(f.width);expect(h.height).toBe(f.height)
 for(const name of ['sample.strm','sample.nfo']){
  await page.getByRole('button',{name,exact:true}).dblclick()
  const dialog=page.getByRole('dialog',{name,exact:true}),input=dialog.getByRole('textbox',{name:'文件内容'})
  expect((await dialog.boundingBox()).width).toBe(640)
  expect((await input.boundingBox()).height).toBeLessThanOrEqual(260)
  await input.fill('edited '+name);await dialog.getByRole('button',{name:'保存',exact:true}).click()
  await expect.poll(saved).toEqual({content:'edited '+name,revision:'original'})
 }
 await page.getByRole('button',{name:'sample.nfo',exact:true}).dblclick()
 await page.screenshot({path:info.outputPath('shared-editor-light.png')})
 await page.evaluate(()=>document.documentElement.dataset.theme='dark')
 await page.screenshot({path:info.outputPath('shared-editor-dark.png')})
})

test('log and playback lists scroll their short page header first, keep virtual rows and matching rails',async({page},info)=>{
 await setup(page)
 for(const [url,list,shell,rows] of [['/logs','.log-viewport','.log-list-shell','.log-entry'],['/links/cache','.playback-scroller','.playback-list-shell','.playback-event']]){
  await page.goto(url);await expect(page.locator(rows).first()).toBeVisible()
  const outer=page.locator('.page-scroll > .thin-scroll-area'),inner=page.locator(list)
  expect(await page.locator(rows).count()).toBeLessThan(60)
  await inner.hover();await page.mouse.wheel(0,45)
  await expect.poll(()=>outer.evaluate(el=>el.scrollTop)).toBeGreaterThan(0)
  await inner.hover();await page.mouse.wheel(0,500)
  await expect.poll(()=>inner.evaluate(el=>el.scrollTop)).toBeGreaterThan(0)
  const top=(await inner.boundingBox()).y
  expect(Math.abs(top-56)).toBeLessThan(4)
  await expect(page.locator(shell+' .scroll-rail-thumb')).toBeVisible()
  await expect.poll(()=>page.locator(shell+' .scroll-rail-thumb').evaluate(el=>parseFloat(getComputedStyle(el,'::after').width)*devicePixelRatio)).toBeCloseTo(3,1)
  await page.screenshot({path:info.outputPath(url.includes('logs')?'logs-header-scrolled.png':'links-header-scrolled.png')})
  expect(await page.evaluate(()=>window.scrollY)).toBe(0)
 }
})

test('backup standard editor exposes monitoring and source completion policies',async({page},info)=>{
 await setup(page);await page.goto('/transfer/backup')
 await page.getByRole('button',{name:'添加备份',exact:true}).click()
 const modal=page.getByRole('dialog',{name:'添加备份规则',exact:true})
 expect((await modal.boundingBox()).width).toBe(640)
 await modal.getByRole('button',{name:'备份规则',exact:true}).click()
 await modal.getByRole('button',{name:'完成规则',exact:true}).click()
 await page.getByRole('option',{name:'删除已备份源文件和空文件夹',exact:true}).click()
 await modal.getByRole('button',{name:'基本设置',exact:true}).click()
 await modal.getByRole('switch',{name:'文件系统监听',exact:true}).check()
 await page.screenshot({path:info.outputPath('backup-monitor-light.png')})
 await page.evaluate(()=>document.documentElement.dataset.theme='dark')
 await page.screenshot({path:info.outputPath('backup-monitor-dark.png')})
})

test('both transfer tabs keep their framed content at the viewport bottom',async({page})=>{
 await setup(page)
 for(const url of ['/transfer','/transfer/backup']){
  await page.goto(url)
  const content=page.locator('.transfer-content')
  await expect(content).toBeVisible()
  expect((await content.boundingBox()).y+(await content.boundingBox()).height).toBeGreaterThanOrEqual(980)
 }
})

test('nested wheel smoothly consumes header and list distance, reverses and respects reduced motion',async({page})=>{
 await setup(page)
 for(const [url,selector] of [['/logs','.log-viewport'],['/links/cache','.playback-scroller']]){
  await page.goto(url);const list=page.locator(selector)
  await expect(list).toBeVisible()
  const values=await list.evaluate(async el=>{
   const page=el.closest('.page-scroll .thin-scroll-area'),samples=[]
   const value=()=>page.scrollTop+el.scrollTop
   el.dispatchEvent(new WheelEvent('wheel',{deltaY:500,bubbles:true,cancelable:true}))
   for(let i=0;i<24;i++){await new Promise(requestAnimationFrame);samples.push(value())}
   return samples
  })
  expect(values[0]).toBeLessThan(500);expect(values.some(v=>v>0&&v<450)).toBe(true)
  expect(values.at(-1)).toBeCloseTo(500,0)
  expect(values.every((v,i)=>!i||v>=values[i-1])).toBe(true)
  await list.evaluate(el=>el.dispatchEvent(new WheelEvent('wheel',{deltaY:-300,bubbles:true,cancelable:true})))
  await expect.poll(()=>list.evaluate(el=>el.scrollTop+el.closest('.page-scroll .thin-scroll-area').scrollTop)).toBeCloseTo(200,0)
  await page.emulateMedia({reducedMotion:'reduce'})
  await list.evaluate(el=>el.dispatchEvent(new WheelEvent('wheel',{deltaY:-200,bubbles:true,cancelable:true})))
  expect(await list.evaluate(el=>el.scrollTop+el.closest('.page-scroll .thin-scroll-area').scrollTop)).toBe(0)
  await page.emulateMedia({reducedMotion:'no-preference'})
 }
})

test('mount switches stay adjacent to their labels',async({page})=>{
 await setup(page);await page.route('**/api/mounts',r=>r.fulfill({json:[]}))
 await page.goto('/files/mounts')
 await page.getByRole('button',{name:'添加挂载',exact:true}).click()
 for(const name of ['只读','自动挂载']){
  const control=page.getByRole('switch',{name,exact:true})
  const gap=await control.evaluate(el=>el.getBoundingClientRect().left-el.previousElementSibling.getBoundingClientRect().right)
  expect(gap).toBeGreaterThanOrEqual(8);expect(gap).toBeLessThanOrEqual(12)
 }
})
