import { test, expect } from '@playwright/test'

async function mock(page, count=4) {
 const storages=[{id:'pan',type:'115',name:'115影视',enabled:true,config:{}},{id:'mobile',type:'mobile',name:'移动',enabled:true,config:{mode:'native'},usage:{total:100,used:120}},{id:'tianyi',type:'tianyi',name:'天翼',enabled:true,config:{},usage:{total:100,used:100}}]
 await page.route('**/api/**',route=>{
  const u=new URL(route.request().url());let json={}
  if(u.pathname==='/api/auth/status')json={authenticated:true,initialized:true}
  if(u.pathname==='/api/state')json={username:'preview-tests',storages,tasks:[],settings:{},cache:{},traffic:{}}
  if(u.pathname==='/api/files')json=Array.from({length:count},(_,i)=>({id:String(i),name:`文件${i}.txt`,isDir:false,size:10}))
  if(u.pathname.endsWith('/usage'))json=storages.find(s=>u.pathname.includes(s.id))?.usage||{}
  return route.fulfill({json})
 })
}
async function openShare(page){await page.goto('/files?storage=pan');await page.getByRole('button',{name:'工具',exact:true}).click();await page.getByRole('menuitem',{name:'分享转存',exact:true}).click();await page.getByLabel('分享链接',{exact:true}).fill('https://115.com/s/test?password=abcd')}

test('share tree applies only checked files; return discards modifications',async({page},info)=>{
 await mock(page)
 const exclusions=[]
 await page.route('**/api/files/share/*',route=>{
  const body=route.request().postDataJSON()
  if(route.request().url().endsWith('/info'))return route.fulfill({json:{directory:'Show'}})
  if(/\/(preview|tree)$/.test(route.request().url())){
   exclusions.push(body.excluded)
   return route.fulfill({json:{preview:'key',batches:1,items:[{id:'show',name:'Show',isDir:true}],tree:[{path:'Show',name:'Show',isDir:true},{path:'Show/one.mkv',name:'one.mkv',isDir:false},{path:'Show/two.mkv',name:'two.mkv',isDir:false}]}})
  }
  return route.fulfill({json:{status:'submitted'}})
 })
 await openShare(page);await page.getByRole('button',{name:'解析',exact:true}).click()
 const tree=page.getByRole('dialog',{name:'分享内容',exact:true})
 await tree.getByRole('checkbox',{name:'选择 Show/one.mkv',exact:true}).uncheck()
 await tree.getByRole('button',{name:'返回',exact:true}).click()
 await page.getByRole('button',{name:'解析',exact:true}).click()
 await expect(tree.getByRole('checkbox',{name:'选择 Show/one.mkv',exact:true})).toBeChecked()
 await tree.getByRole('checkbox',{name:'选择 Show/two.mkv',exact:true}).uncheck()
 await expect(tree.locator('.share-tree')).toHaveCSS('scrollbar-width','none')
 await page.screenshot({path:info.outputPath('share-tree-light.png')})
 await tree.getByRole('button',{name:'应用',exact:true}).click();await page.getByRole('button',{name:'转存',exact:true}).click()
 await expect.poll(()=>exclusions.at(-1)).toEqual(['Show/two.mkv'])
})

test('share root selection excludes every top-level item rather than its display label',async({page})=>{
 await mock(page)
 let excluded
 await page.route('**/api/files/share/*',route=>{
  const body=route.request().postDataJSON()
  if(route.request().url().endsWith('/info'))return route.fulfill({json:{directory:''}})
  if(route.request().url().endsWith('/tree'))return route.fulfill({json:{items:[{name:'A',isDir:true},{name:'B',isDir:true},{name:'readme.txt',isDir:false}],tree:[{path:'A',name:'A',isDir:true},{path:'A/one.mkv',name:'one.mkv',isDir:false},{path:'B',name:'B',isDir:true},{path:'readme.txt',name:'readme.txt',isDir:false}]}})
  excluded=body.excluded
  return route.fulfill({json:{preview:'key',batches:0,items:[]}})
 })
 await openShare(page);await page.getByRole('button',{name:'解析',exact:true}).click()
 const tree=page.getByRole('dialog',{name:'分享内容',exact:true})
 await tree.getByRole('checkbox',{name:'选择分享 A、B',exact:true}).uncheck()
 await expect(tree.getByRole('checkbox',{name:'选择 A',exact:true})).not.toBeChecked()
 await expect(tree.getByRole('checkbox',{name:'选择 B',exact:true})).not.toBeChecked()
 await expect(tree.getByRole('checkbox',{name:'选择 readme.txt',exact:true})).not.toBeChecked()
 await tree.getByRole('button',{name:'应用',exact:true}).click();await page.getByRole('button',{name:'转存',exact:true}).click()
 await expect.poll(()=>excluded).toEqual(['A','B','readme.txt'])
})

test('file count appears only at list end or when no scroll is necessary',async({page})=>{
 await mock(page,500);await page.goto('/files');const count=page.locator('.file-item-count')
 await expect(page.locator('.file-row').first()).toBeVisible();await expect(count).toBeHidden()
 await page.locator('.file-view').evaluate(el=>{el.scrollTop=el.scrollHeight})
 await expect(count).toBeVisible();await expect(count).toHaveText('500 项')
 await page.locator('.file-view').evaluate(el=>{el.scrollTop=0});await expect(count).toBeHidden()
 await page.getByRole('textbox',{name:'搜索当前目录',exact:true}).fill('文件499')
 await expect(count).toBeVisible();await expect(count).toHaveText('1 项')
})

test('115 snapshot cards save the selected output and execute confirmed generation',async({page},info)=>{
 await mock(page)
 const cards=[],writes=[],runs=[],unexpected=[]
 const settings={storageId:'pan',directory:'/media',scrapeEnabled:false}
 await page.route('**/api/virtual-library/115**',route=>{
  const request=route.request(),p=new URL(request.url()).pathname,base='/api/virtual-library/115'
  if(p===base&&request.method()==='GET')return route.fulfill({json:{cards,settings,stats:{strm:runs.length}}})
  if(p===base+'/resolve')return route.fulfill({json:{name:'Show'}})
  if(p===base&&request.method()==='POST'){
   const input=request.postDataJSON();writes.push(input);const card={...input,id:'show',result:{status:'idle',message:'等待执行'}};cards.push(card)
   return route.fulfill({status:201,json:card})
  }
  if(p===base+'/show/run'&&request.method()==='POST'){
   runs.push(p);cards[0].result={status:'completed',lastRun:'2026-11-05T00:00:00Z',message:'生成 1 个 STRM：Show/one.mkv.share.strm'}
   return route.fulfill({status:202,json:cards[0].result})
  }
  unexpected.push(request.method()+' '+p);return route.fulfill({status:500,json:{error:'测试未定义接口'}})
 })
 await page.route('**/api/local-directories**',route=>route.fulfill({json:{path:'/media',items:[]}}))
 await page.goto('/virtual/115')
 await expect(page.getByRole('navigation',{name:'虚拟链库栏目'}).getByRole('link')).toHaveCount(4)
 await page.getByRole('button',{name:'添加链接',exact:true}).click()
 let dialog=page.getByRole('dialog',{name:'添加115链接',exact:true})
 await dialog.getByRole('textbox',{name:'分享链接',exact:true}).fill('https://115.com/s/code?password=abcd')
 await dialog.getByRole('textbox',{name:'分享名称',exact:true}).focus()
 await expect(dialog.getByRole('textbox',{name:'分享名称',exact:true})).toHaveValue('Show')
 await expect(dialog.getByRole('textbox',{name:'目录名称',exact:true})).toHaveValue('Show')
 await dialog.getByRole('button',{name:'选择生成位置',exact:true}).click();await page.getByRole('button',{name:'选择当前目录',exact:true}).click()
 await expect(dialog.getByRole('textbox',{name:'生成位置',exact:true})).toHaveValue('/media')
 await expect(dialog.getByRole('button',{name:'解析存储池',exact:true})).toHaveText('115影视')
 await expect(dialog.getByRole('textbox',{name:'Cron表达式',exact:true})).toHaveCount(0)
 await dialog.getByRole('button',{name:'保存链接',exact:true}).click()
 await expect.poll(()=>writes.length).toBe(1)
 expect(writes[0]).toEqual({id:'',storageId:'pan',url:'https://115.com/s/code?password=abcd',name:'Show',cron:'',directory:'/media',directoryName:'Show',mediaExtensions:'',metadataExtensions:'',enabled:true,cascadeCleanup:'off',scrapeEnabled:false,excludeDirs:'',excludeFiles:'',excludeTypes:''})
 const card=page.getByRole('article',{name:'Show',exact:true})
 await expect(card).toContainText('/media/Show');expect(runs).toEqual([])
 await card.getByRole('button',{name:'执行',exact:true}).click()
 const confirmation=page.getByRole('dialog',{name:'同步115分享',exact:true});await expect(confirmation).toBeVisible();expect(runs).toEqual([])
 await confirmation.getByRole('button',{name:'确认同步',exact:true}).click()
 await expect.poll(()=>runs).toEqual(['/api/virtual-library/115/show/run'])
 await expect(card).toContainText('Show/one.mkv.share.strm');await expect(page.locator('.toast-stack')).toContainText('生成 1 个 STRM')
 await page.reload();await expect(card).toContainText('Show/one.mkv.share.strm')
 await card.getByRole('button',{name:'编辑 Show',exact:true}).click();dialog=page.getByRole('dialog',{name:'编辑115链接',exact:true})
 await expect(dialog.getByRole('textbox',{name:'目录名称',exact:true})).toHaveValue('Show')
 await page.evaluate(()=>document.documentElement.dataset.theme='dark')
 await expect(dialog.getByRole('textbox',{name:'生成位置',exact:true})).toHaveCSS('background-color','rgb(41, 44, 52)')
 await expect(dialog.getByRole('button',{name:'解析存储池',exact:true})).toHaveCSS('background-color','rgb(41, 44, 52)')
 await expect(page.getByRole('navigation',{name:'虚拟链库栏目'}).locator('a:not(.active)').first()).toHaveCSS('color','rgb(237, 240, 248)')
 await expect(page.getByRole('navigation',{name:'虚拟链库栏目'}).locator('a.active')).toHaveCSS('color','rgb(244, 237, 189)')
 await page.screenshot({path:info.outputPath('virtual-library-dark.png')})
 await dialog.getByRole('button',{name:'取消',exact:true}).click()
 await page.getByRole('button',{name:'115链库设置',exact:true}).click();await expect(page).toHaveURL(/\/virtual\/115\/settings$/)
 await expect(page.getByRole('button',{name:'默认解析存储池',exact:true})).toHaveText('115影视')
 expect(unexpected).toEqual([])
})

test('full and over-capacity storage cards keep gradient meters in both themes',async({page},info)=>{
 await mock(page);await page.goto('/storage')
 for(const name of ['移动','天翼']){const card=page.locator('.storage-card').filter({has:page.getByRole('heading',{name,exact:true})});await expect(card).toHaveClass(/quota-full/);await expect(card.getByRole('meter')).toBeVisible();expect(await card.locator('.storage-quota-track').evaluate(el=>Math.abs(el.getBoundingClientRect().width-el.firstElementChild.getBoundingClientRect().width))).toBeLessThan(.1);expect(await card.locator('.storage-quota-track i').evaluate(el=>getComputedStyle(el).backgroundImage)).toContain('203, 75, 100')}
 await page.screenshot({path:info.outputPath('quota-full-light.png')});await page.evaluate(()=>document.documentElement.dataset.theme='dark');await page.screenshot({path:info.outputPath('quota-full-dark.png')})
})

test('literal and regex replacement preview preserves inputs and confirms exact changed count',async({page},info)=>{
 await mock(page);let executions=0,requests=[],polls=0
 await page.route('**/api/strm-replace**',route=>{
  const body=route.request().postDataJSON()
  if(route.request().url().endsWith('/preview')){
   requests.push(body)
   if(body.find==='[')return route.fulfill({status:400,json:{error:'正则表达式无效'}})
   return route.fulfill({json:{count:3,samples:[{name:'Show/one.strm',before:'http://old/42',after:'http://new/42',matches:[[7,13]],replacements:[[7,13]]}]}})
  }
  if(route.request().method()==='POST'){executions++;expect(body.mode).toBe('regex');expect(body.confirmed).toBe(true);expect(body.count).toBeUndefined();return route.fulfill({json:{id:'job',status:'running',changed:0,processed:0,scanned:0}})}
  polls++;return route.fulfill({json:{tasks:[]}})
 })
 await page.goto('/tools');await page.getByRole('button',{name:'STRM 替换',exact:true}).click()
 await page.getByRole('textbox',{name:'替换目录',exact:true}).fill('/media')
 await page.getByRole('textbox',{name:'匹配字段',exact:true}).fill('old/(\\d+)')
 await page.getByRole('textbox',{name:'替换字段',exact:true}).fill('new/$1')
 await page.getByRole('button',{name:'替换模式',exact:true}).click();await page.getByRole('option',{name:'正则替换',exact:true}).click()
 await page.getByRole('button',{name:'预览',exact:true}).click()
 const preview=page.getByRole('dialog',{name:'替换预览',exact:true})
 await expect(preview).toContainText('3 个 STRM 文件');await expect(preview.locator('mark.match')).toHaveText('old/42');await expect(preview.locator('mark.replacement')).toHaveText('new/42')
 expect(executions).toBe(0);expect(requests[0].mode).toBe('regex')
 await page.screenshot({path:info.outputPath('replacement-preview-light.png')})
 await page.setViewportSize({width:390,height:844});await page.evaluate(()=>document.documentElement.dataset.theme='dark');await page.screenshot({path:info.outputPath('replacement-preview-mobile-dark.png')})
 await preview.getByRole('button',{name:'返回',exact:true}).click();await expect(page.getByLabel('替换字段',{exact:true})).toHaveValue('new/$1')
 await page.getByLabel('匹配字段',{exact:true}).fill('[');await page.getByRole('button',{name:'预览',exact:true}).click();await expect(page.getByRole('alert')).toContainText('正则表达式无效')
 const previousPolls=polls
 await expect.poll(()=>polls).toBeGreaterThan(previousPolls)
 await expect(page.getByRole('alert')).toContainText('正则表达式无效')
 await page.getByLabel('匹配字段',{exact:true}).fill('old/(\\d+)');await page.getByRole('button',{name:'执行替换',exact:true}).click()
 const confirm=page.getByRole('dialog',{name:'确认替换 STRM 内容',exact:true});await expect(confirm).toContainText('将替换 3 个 STRM 文件');expect(executions).toBe(0)
 await confirm.getByRole('button',{name:'确认替换',exact:true}).click();expect(executions).toBe(1)
})

test('scrape all-state has no marker, status markers shrink and night tabs are white',async({page},info)=>{
 await mock(page)
 await page.route('**/api/strm-scrape/**',route=>route.fulfill({json:route.request().url().includes('/items')?[]:{}}))
 await page.goto('/tasks/scrape')
 const state=page.getByRole('button',{name:'刮削状态',exact:true})
 await expect(state.locator('.select-marker')).toHaveCount(0);await state.click()
 const all=page.getByRole('option',{name:'全部状态',exact:true});await expect(all.locator('.select-marker')).toHaveCount(0)
 for(const marker of await page.getByRole('listbox',{name:'刮削状态选项'}).locator('.select-marker').all()){
  await expect(marker).toHaveCSS('width','6px')
  expect(await marker.evaluate(el=>{const a=el.getBoundingClientRect(),b=el.parentElement.getBoundingClientRect();return Math.abs(a.y+a.height/2-b.y-b.height/2)})).toBeLessThan(1)
 }
 await page.keyboard.press('Escape');await page.evaluate(()=>document.documentElement.dataset.theme='dark')
 await expect(page.locator('.content-tabs a:not(.active)').first()).toHaveCSS('color','rgb(237, 240, 248)')
 await page.goto('/settings/about');await expect(page.locator('.settings-navigation a.active')).toHaveCSS('color','rgb(244, 237, 189)');await expect(page.locator('.settings-navigation a:not(.active)').first()).toHaveCSS('color','rgb(237, 240, 248)')
 await page.screenshot({path:info.outputPath('about-night-tabs.png')})
})

test('virtual library tabs use unframed provider outlines in both themes',async({page},info)=>{
 await mock(page);await page.goto('/virtual/115')
 const tabs=page.getByRole('navigation',{name:'虚拟链库栏目',exact:true})
 await expect(tabs.locator('img')).toHaveCount(0)
 await expect(tabs.locator('.provider-icon')).toHaveCount(0)
 for(const theme of ['light','dark']){
  await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
  const icons=tabs.locator('.provider-line-icon')
  await expect(icons).toHaveCount(3)
  await expect(tabs.getByRole('link',{name:'CAS链库',exact:true}).locator('svg.lucide-box-icon')).toHaveCount(1)
  for(const icon of await icons.all()){
   await expect(icon).toHaveCSS('width','19px');await expect(icon).toHaveAttribute('stroke','currentColor')
   await expect(icon).toHaveAttribute('fill','none');await expect(icon).toHaveAttribute('stroke-linecap','round')
   expect(await icon.locator('path').count()).toBeGreaterThan(0)
  }
  await page.screenshot({path:info.outputPath(`virtual-provider-outlines-${theme}.png`)})
 }
})

for(const width of [1440,390])for(const theme of ['light','dark'])test(`virtual library columns and CAS generation ${width} ${theme}`,async({page},info)=>{
 await mock(page)
 await page.setViewportSize({width,height:900})
 await page.goto('/virtual/115')
 await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
 const tabs=page.getByRole('navigation',{name:'虚拟链库栏目',exact:true})
 expect((await tabs.getByRole('link').allTextContents()).map(text=>text.trim())).toEqual(['115链库','移动链库','夸克链库','CAS链库'])
 await expect(tabs.getByRole('link',{name:'天翼链库',exact:true})).toHaveCount(0)
 await tabs.getByRole('link',{name:'移动链库',exact:true}).click()
 await expect(page).toHaveURL(/\/virtual\/mobile$/)
 await expect(tabs.getByRole('link',{name:'移动链库',exact:true})).toHaveClass(/active/)
 await tabs.getByRole('link',{name:'CAS链库',exact:true}).click()
 await expect(page).toHaveURL(/\/virtual\/cas$/)
 await expect(tabs.getByRole('link',{name:'CAS链库',exact:true})).toHaveClass(/active/)
 await expect(tabs.getByRole('link',{name:'CAS链库',exact:true}).locator('svg.lucide-box-icon')).toHaveCount(1)
 await expect(page.getByRole('region',{name:'CAS分享链接',exact:true})).toContainText('暂无分享链接')
 await expect(page.getByRole('region',{name:'CAS分享链接',exact:true}).locator('svg.lucide-box-icon')).toHaveCount(1)
 await expect(page.getByRole('button',{name:'添加链接',exact:true})).toBeVisible()
 await expect(page.getByRole('button',{name:'CAS链库设置',exact:true})).toBeVisible()
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
 await page.screenshot({path:info.outputPath(`cas-library-${width}-${theme}.png`)})
 await page.reload()
 await expect(tabs.getByRole('link',{name:'CAS链库',exact:true})).toHaveClass(/active/)
})

test('removed Tianyi virtual library address returns to the available 115 library',async({page})=>{
 await mock(page)
 await page.goto('/virtual/tianyi')
 await expect(page).toHaveURL(/\/virtual\/115$/)
 await expect(page.getByRole('navigation',{name:'虚拟链库栏目',exact:true}).getByRole('link',{name:'115链库',exact:true})).toHaveClass(/active/)
 await expect(page.getByRole('region',{name:'115分享链接',exact:true})).toBeVisible()
 await page.getByRole('button',{name:'添加链接',exact:true}).click()
 await expect(page.getByRole('dialog',{name:'添加115链接',exact:true}).getByRole('textbox',{name:'分享链接',exact:true})).toBeVisible()
})
