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

test('playback list scrolls its short page header first and keeps virtual rows and matching rail',async({page},info)=>{
 await setup(page)
 for(const [url,list,shell,rows] of [['/links/cache','.playback-scroller','.playback-list-shell','.playback-event']]){
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
 await page.locator('.task-heading').getByRole('button',{name:'添加规则',exact:true}).click()
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
 for(const [url,selector] of [['/links/cache','.playback-scroller']]){
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

test('new mount defaults to 0755 while editing preserves explicit existing permissions',async({page})=>{
 await setup(page)
 const mounts=[{id:'old',name:'已有挂载',source:'/',mountPoint:'/mnt/old/AetherDrive',mode:0o777,uid:0,gid:0,status:'stopped',readOnly:false,automount:true}]
 let saved
 await page.route('**/api/mounts',r=>{
  if(r.request().method()==='POST'){saved=r.request().postDataJSON();mounts.push({...saved,id:'new',status:'stopped'});return r.fulfill({json:{ok:true}})}
  return r.fulfill({json:mounts})
 })
 await page.goto('/files/mounts')
 await page.getByRole('button',{name:'添加挂载',exact:true}).click()
 const modal=page.getByRole('dialog',{name:'添加挂载',exact:true})
 await modal.getByText('高级设置',{exact:true}).click()
 await expect(modal.getByLabel('权限',{exact:true})).toHaveValue('0755')
 await modal.getByLabel('挂载名称',{exact:true}).fill('新挂载')
 await modal.getByLabel('挂载点',{exact:true}).fill('/mnt/new')
 await modal.getByRole('button',{name:'保存挂载',exact:true}).click()
 await expect(modal).toHaveCount(0)
 expect(saved.mode).toBe(0o755)
 await page.locator('.mount-card').filter({hasText:'已有挂载'}).click()
 const editor=page.getByRole('dialog',{name:'编辑挂载',exact:true})
 await editor.getByText('高级设置',{exact:true}).click()
 await expect(editor.getByLabel('权限',{exact:true})).toHaveValue('0777')
})

test('wheel inside an already scrolled list still docks the page first without moving its rows',async({page})=>{
 await setup(page)
 await page.route('**/api/files?**',r=>r.fulfill({json:Array.from({length:180},(_,i)=>({id:String(i),name:`file-${i}.txt`}))}))
 for(const [url,selector,boundary] of [['/files','.file-view','.file-browser'],['/links/cache','.playback-scroller','.playback-list-shell']]){
  await page.goto(url)
  const list=page.locator(selector),outer=page.locator('.page-scroll > .thin-scroll-area')
  await expect(list).toBeVisible()
  await list.evaluate(el=>{el.scrollTop=100;el.closest('.page-scroll .thin-scroll-area').scrollTop=0})
  const rect=await list.boundingBox();await page.mouse.move(rect.x+100,rect.y+70)
  await page.mouse.wheel(0,12)
  await expect.poll(()=>outer.evaluate(el=>el.scrollTop)).toBeCloseTo(12,0)
  expect(await list.evaluate(el=>el.scrollTop)).toBe(100)
  for(let i=0;i<3;i++)await page.mouse.wheel(0,8)
  await expect.poll(()=>outer.evaluate(el=>el.scrollTop)).toBeCloseTo(36,0)
  expect(await list.evaluate(el=>el.scrollTop)).toBe(100)
  await page.mouse.wheel(0,600)
  await expect.poll(()=>list.evaluate(el=>el.scrollTop)).toBeGreaterThan(100)
  await expect.poll(async()=>Math.round((await page.locator(boundary).boundingBox()).y)).toBe(56)
 }
})

test('inside and outside file and cache wheels share page-first descent and list-first ascent',async({page})=>{
 await setup(page)
 await page.route('**/api/files?**',r=>r.fulfill({json:Array.from({length:180},(_,i)=>({id:String(i),name:`file-${i}.txt`}))}))
 for(const [url,selector] of [['/files','.file-view'],['/links/cache','.playback-scroller']]){
  await page.goto(url)
  const list=page.locator(selector),outer=page.locator('.page-scroll > .thin-scroll-area')
  await expect(list).toBeVisible()
  for(const inside of [true,false]){
   await list.evaluate(el=>{el.scrollTop=0;el.closest('.page-scroll .thin-scroll-area').scrollTop=0})
   const box=await list.boundingBox()
   await page.mouse.move(inside?box.x+100:245,inside?box.y+70:350)
   await page.mouse.wheel(0,12)
   await expect.poll(()=>outer.evaluate(el=>el.scrollTop)).toBeCloseTo(12,0)
   expect(await list.evaluate(el=>el.scrollTop)).toBe(0)
   await page.mouse.wheel(0,500)
   await expect.poll(()=>list.evaluate(el=>el.scrollTop+el.closest('.thin-scroll-area').scrollTop)).toBeCloseTo(512,0)
   const docked=await outer.evaluate(el=>el.scrollTop)
   const oldList=await list.evaluate(el=>el.scrollTop)
   await page.mouse.wheel(0,-80)
   await expect.poll(()=>list.evaluate(el=>el.scrollTop)).toBeCloseTo(oldList-80,0)
   expect(await outer.evaluate(el=>el.scrollTop)).toBe(docked)
   await page.mouse.wheel(0,-10000)
   await expect.poll(()=>outer.evaluate(el=>el.scrollTop)).toBe(0)
   expect(await list.evaluate(el=>el.scrollTop)).toBe(0)
  }
 }
})

test('logs wheel scrolls the full virtual page and only the breadcrumb remains fixed',async({page},info)=>{
 await setup(page);await page.goto('/logs')
 const outer=page.locator('.page-scroll > .thin-scroll-area'),list=page.locator('.log-viewport')
 await expect(page.locator('.log-entry').first()).toBeVisible()
 await page.locator('.log-entry').first().hover();await page.mouse.wheel(0,700)
 await expect.poll(()=>outer.evaluate(el=>el.scrollTop)).toBeGreaterThan(600)
 expect(await list.evaluate(el=>el.scrollTop)).toBe(0)
 expect((await page.locator('.log-toolbar').boundingBox()).y).toBeLessThan(0)
 expect((await page.locator('.topbar').boundingBox()).y).toBe(0)
 expect(await page.locator('.log-entry').count()).toBeLessThan(60)
 await page.screenshot({path:info.outputPath('log-page-scroll.png')})
})
