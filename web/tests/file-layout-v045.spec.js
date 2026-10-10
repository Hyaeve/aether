import { test, expect } from '@playwright/test'

async function setup(page, count=180) {
 await page.addInitScript(()=>localStorage.setItem('aether-file-history:layout-test',JSON.stringify([
  {storage:'local',id:'/a/b/c',history:[{id:'/',name:'很长的上上层目录名字'.repeat(6)},{id:'/a',name:'上层目录'},{id:'/a/b',name:'最深目录'}]},
  {storage:'quark',id:'c',history:[{id:'/',name:'A'},{id:'a',name:'B'},{id:'b',name:'C'}]}
 ])))
 await page.route('**/api/**',r=>{
  const u=new URL(r.request().url());let json={}
  if(u.pathname==='/api/auth/status')json={initialized:true,authenticated:true}
  if(u.pathname==='/api/state')json={username:'layout-test',storages:[{id:'local',name:'本地',type:'local',enabled:true,config:{}},{id:'quark',name:'夸克',type:'quark',enabled:true,config:{}}],tasks:[],settings:{},cache:{},traffic:{}}
  if(u.pathname==='/api/files')json=Array.from({length:count},(_,i)=>({id:`${i}`,name:`${i%2?'文件':'目录'}${i}.txt`,isDir:i%2===0,size:i*100,modified:`2026-10-${String(i%9+1).padStart(2,'0')}T10:00:00Z`,sizeKnown:true,countsKnown:true,folderCount:0,fileCount:0}))
  if(u.pathname==='/api/mounts'||u.pathname==='/api/transfers'||u.pathname==='/api/backups'||u.pathname==='/api/webdav/users')json=[]
  return r.fulfill({json})
 })
}
test('history has explicit omission, matching icons and button colors; details header and outside dismissal',async({page},info)=>{
 await setup(page,4);await page.goto('/files');await expect(page.locator('.file-row')).toHaveCount(4)
 const history=page.getByRole('button',{name:'历史访问',exact:true}),refresh=page.getByRole('button',{name:'刷新目录',exact:true})
 expect(await history.evaluate(el=>getComputedStyle(el).color)).toBe(await refresh.evaluate(el=>getComputedStyle(el).color))
 await history.click()
 const options=page.locator('.file-visit-history [role=option]')
 await expect(options.first().locator('.visit-path')).toContainText('/../')
 await expect(options.first().locator('.visit-deepest')).toHaveText('最深目录')
 await expect(options.last().locator('.visit-path')).toHaveText('/A/B/C')
 await page.locator('.file-visit-history [role=listbox]').evaluate(async el=>await Promise.allSettled(el.getAnimations().map(a=>a.finished)))
 for(const icon of await options.locator('.provider-icon').all()) { const rect=await icon.boundingBox();expect(rect.width).toBe(24);expect(rect.height).toBe(24) }
 await page.screenshot({path:info.outputPath('history-light.png')});await page.keyboard.press('Escape')
 await page.getByRole('button',{name:'目录0.txt',exact:true}).click({button:'right'});await page.getByRole('button',{name:'查看详情',exact:true}).click()
 const drawer=page.getByRole('dialog',{name:'文件夹详情',exact:true})
 await expect(drawer.locator('header')).toContainText('1 个项目')
 await drawer.evaluate(async el=>await Promise.allSettled(el.getAnimations().map(a=>a.finished)))
 const alignment=await drawer.locator('dl').evaluate(el=>{const dt=[...el.querySelectorAll('dt')].find(el=>el.textContent==='位置');return Math.abs(dt.getBoundingClientRect().top-dt.nextElementSibling.querySelector('button').getBoundingClientRect().top)})
 expect(alignment).toBeLessThan(2)
 await page.screenshot({path:info.outputPath('details-light.png')});await page.evaluate(()=>document.documentElement.dataset.theme='dark');await page.screenshot({path:info.outputPath('details-dark.png')})
 await refresh.click();await expect(drawer).toHaveCount(0)
 await page.getByRole('button',{name:'展开收藏栏',exact:true}).click();await expect(page.locator('.file-favorites')).not.toContainText('暂无收藏')
})
test('folders stay first for both directions of every sort and virtual files gradually scroll the outer header',async({page},info)=>{
 await setup(page);await page.goto('/files');await expect(page.locator('.file-row').first()).toBeVisible()
 for(const label of ['名称','大小','类型','修改时间'])for(let pass=0;pass<2;pass++){
  await page.getByRole('button',{name:label,exact:true}).click()
  expect(await page.locator('.file-row .file-name strong').first().textContent()).toMatch(/^目录/)
 }
 await expect(page.locator('.file-item-count')).toHaveText('共 180 项')
 const outer=page.locator('.page-scroll > .thin-scroll-area'),inner=page.locator('.file-view')
 const box=await inner.boundingBox();await page.mouse.move(box.x+100,box.y+70)
 const before=await outer.evaluate(el=>el.scrollTop)
 await page.mouse.wheel(0,12)
 await expect.poll(()=>outer.evaluate(el=>el.scrollTop)).toBeGreaterThan(before)
 expect(await outer.evaluate(el=>el.scrollTop)).toBeLessThanOrEqual(before+13)
 await page.mouse.wheel(0,700);await expect.poll(()=>inner.evaluate(el=>el.scrollTop)).toBeGreaterThan(0)
 expect((await page.locator('.file-browser').boundingBox()).y).toBeCloseTo(56,0)
 expect(await page.locator('.file-row').count()).toBeLessThan(45)
 await page.screenshot({path:info.outputPath('files-scrolled.png')})
 await page.mouse.wheel(0,-10000);await expect.poll(()=>outer.evaluate(el=>el.scrollTop)).toBe(0)
 await page.setViewportSize({width:390,height:844});expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
 await page.screenshot({path:info.outputPath('files-mobile.png')})
})
test('empty mount task and transfer panels share the eight pixel bottom inset',async({page},info)=>{
 await setup(page,0)
 for(const [url,selector] of [['/files/mounts','.workspace-empty'],['/tasks/ed2k','.task-empty'],['/tasks/automation','.automation-list'],['/transfer','.transfer-table'],['/transfer/backup','.backup-content'],['/files','.file-browser']]) {
  await page.goto(url);await expect(page.locator(selector)).toBeVisible()
  const box=await page.locator(selector).boundingBox();expect(box.y+box.height, url).toBeCloseTo(992,0)
  expect(await page.locator('.page-scroll > .thin-scroll-area').evaluate(el=>el.scrollHeight-el.clientHeight)).toBeLessThan(2)
 }
 await page.screenshot({path:info.outputPath('empty-files.png')})
})
