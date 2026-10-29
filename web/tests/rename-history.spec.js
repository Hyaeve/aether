import {test,expect} from '@playwright/test'
test('rename history uses page virtualization, discontinuous selection and guarded restoration feedback',async({page},info)=>{
 const storage={id:'local',name:'文库',type:'local',enabled:true,config:{}}
 let records=Array.from({length:1000},(_,i)=>({id:String(i),fileId:'/A/new'+i+'.txt',name:'old'+i+'.txt',newName:'new'+i+'.txt',isDir:i===0,modified:'2026-10-12T08:01:00Z'})),restores=[]
 await page.route('**/api/**',route=>{const request=route.request(),path=new URL(request.url()).pathname;let json={};if(path==='/api/auth/status')json={initialized:true,authenticated:true};if(path==='/api/state')json={username:'rename-history',storages:[storage],tasks:[],settings:{},cache:{},logs:[]};if(path==='/api/files')json=[{id:'/old.txt',name:'old.txt'}];if(path==='/api/files/rename-rules')json=[];if(path==='/api/files/rename-history'){records=records.filter(r=>!r.status);json=[{id:'A',storageId:'local',source:'/A',pathLabel:'/Books/Deep/A',items:records},{id:'B',storageId:'local',source:'/B',pathLabel:'/Books/B',items:[]}]};if(path==='/api/files/rename-restore'){const input=request.postDataJSON();restores.push(input);json=records.filter(r=>input.ids.includes(r.id)).map(r=>({...r,status:r.id==='5'?'failed':'success',error:r.id==='5'?'项目已改名':''}));records=records.map(r=>json.find(j=>j.id===r.id)||r)}return route.fulfill({json})})
 await page.goto('/files');await page.getByRole('button',{name:'工具',exact:true}).click();await page.getByRole('menuitem',{name:'命名工作台',exact:true}).click();await page.getByRole('button',{name:'历史记录',exact:true}).click()
 await expect(page.locator('.history-heading')).toContainText('历史命名记录');await expect(page.locator('.rename-history-panel footer')).toHaveText('1000 项');expect(await page.locator('.rename-history-row').count()).toBeLessThan(35)
 const row=n=>page.locator('.rename-history-row').filter({hasText:'old'+n+'.txt'})
 await row(0).click({button:'right'});await row(2).click({modifiers:['Shift']});await row(5).click();await row(6).click({modifiers:['Shift']});await expect(page.locator('.rename-history-row.selected')).toHaveCount(5)
 await expect(row(0).locator('mark')).toHaveText(['old','new'])
 await page.getByRole('button',{name:'恢复',exact:true}).click();await page.getByRole('button',{name:'确认恢复',exact:true}).click();await expect(row(0)).toHaveClass(/success/);await expect(row(5)).toHaveClass(/failed/);expect(restores[0].ids).toEqual(['0','1','2','5','6'])
 await page.screenshot({path:info.outputPath('rename-history-light.png')})
 await page.getByRole('button',{name:'全选',exact:true}).click();await page.keyboard.press('Escape');await expect(page.locator('.history-check')).toHaveCount(0)
 await row(3).click({button:'right'});await page.getByRole('heading',{name:'历史命名记录',exact:true}).click();await expect(page.locator('.history-check')).toHaveCount(0)
 await page.locator('.page-scroll > .thin-scroll-area').evaluate(el=>el.scrollTop=el.scrollHeight);await expect(row(999)).toBeVisible();expect(await page.locator('.rename-history-row').count()).toBeLessThan(35)
 const rail=page.locator('.page-scroll > .thin-scroll-rail .thin-scroll-thumb');await expect(rail).toBeVisible();expect(await rail.evaluate(el=>parseFloat(getComputedStyle(el,'::after').width)*devicePixelRatio)).toBeCloseTo(8,1)
 await page.locator('.page-scroll > .thin-scroll-area').evaluate(el=>el.scrollTop=0);await page.getByRole('button',{name:'返回命名工作台',exact:true}).click();await page.getByRole('button',{name:'历史记录',exact:true}).click();await expect(row(0)).toHaveCount(0)
 await page.getByRole('button',{name:'选择历史目录',exact:true}).click();await page.getByRole('option').filter({hasText:'/Books/B'}).click();await expect(page.locator('.rename-history-panel footer')).toHaveText('0 项')
 await page.setViewportSize({width:390,height:844});await page.evaluate(()=>document.documentElement.dataset.theme='dark');expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);await expect.poll(()=>page.locator('.history-directory-trigger').evaluate(el=>el.getAnimations().every(a=>a.playState==='finished'))).toBe(true);await page.screenshot({path:info.outputPath('rename-history-dark-mobile.png')})
 const panel=await page.locator('.rename-history-panel').boundingBox();expect(panel.y+panel.height).toBeCloseTo(836,0)
})

test('history directory selector fills the heading gap, keeps its width on hover and preserves deepest paths',async({page},info)=>{
 const directories=Array.from({length:5},(_,i)=>({id:String(i),storageId:'local',source:'/directory-'+i,pathLabel:`/Older/${'An-Extremely-Long-Parent-Directory-That-Must-Be-Omitted'.repeat(3)}/Directory-${i}`,items:[{id:'file'+i,name:'old.txt',newName:'new.txt',modified:'2026-10-13T08:00:00Z'}]}))
 await page.route('**/api/**',r=>{const path=new URL(r.request().url()).pathname;return r.fulfill({json:path==='/api/auth/status'?{initialized:true,authenticated:true}:path==='/api/state'?{username:'fixed-history',storages:[{id:'local',name:'文库',type:'local',enabled:true,config:{}}],tasks:[],settings:{},cache:{},logs:[]}:path==='/api/files'?[{id:'/old.txt',name:'old.txt'}]:path==='/api/files/rename-history'?directories:[]})})
 const open=async()=>{await page.getByRole('button',{name:'工具',exact:true}).click();await page.getByRole('menuitem',{name:'命名工作台',exact:true}).click();await page.getByRole('button',{name:'历史记录',exact:true}).click()}
 await page.goto('/files');await open()
 const selector=page.locator('.history-directory-control'),trigger=page.getByRole('button',{name:'选择历史目录',exact:true})
 await expect(trigger).toContainText('Directory-0')
 const start=await selector.boundingBox(),title=await page.getByRole('heading',{name:'历史命名记录',exact:true}).boundingBox(),all=await page.getByRole('button',{name:'全选',exact:true}).boundingBox()
 expect(start.x).toBeCloseTo(title.x+title.width+10,0);expect(start.x+start.width).toBeCloseTo(all.x-10,0)
 await selector.hover();expect((await selector.boundingBox()).width).toBeCloseTo(start.width,0)
 await trigger.click();const menu=page.getByRole('listbox',{name:'历史目录'}),options=menu.getByRole('option')
 expect((await menu.boundingBox()).width).toBeCloseTo(start.width,0)
 await expect(options.locator('.visit-deepest')).toHaveText(directories.map((_,i)=>`Directory-${i}`))
 await expect(options.first()).toContainText('/../')
 await options.nth(3).click();await expect(trigger).toContainText('Directory-3');expect((await selector.boundingBox()).width).toBeCloseTo(start.width,0)
 await page.mouse.move(1400,800);expect((await selector.boundingBox()).width).toBeCloseTo(start.width,0)
 await page.screenshot({path:info.outputPath('fixed-history-desktop.png')})
 await page.setViewportSize({width:390,height:844});await page.evaluate(()=>document.documentElement.dataset.theme='dark')
 await trigger.click();await expect(menu).toBeVisible()
 const mobile=await selector.boundingBox();expect((await menu.boundingBox()).width).toBeCloseTo(mobile.width,0)
 for(const el of [trigger,...await options.all()]){
   await expect(el.locator('strong')).toHaveText('文库');await expect(el.locator('.provider-icon')).toBeVisible()
   expect(await el.locator('.visit-deepest').evaluate(el=>el.scrollWidth<=el.clientWidth)).toBe(true)
 }
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
 await page.screenshot({path:info.outputPath('fixed-history-dark-mobile.png')})
})
