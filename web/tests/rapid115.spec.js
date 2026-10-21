import {test,expect} from '@playwright/test'

const defaults=()=>({source:'',storageId:'',target:'',targetLabel:'',extensions:'',successAction:'delete',failureAction:'skip'})
async function fixture(page){
  let config={manual:defaults(),watches:[]},items=[],running=false,runs=0
  const writes=[]
  await page.route('**/api/**',r=>{
    const req=r.request(),u=new URL(req.url());let value={}
    if(u.pathname==='/api/auth/status')value={initialized:true,authenticated:true}
    else if(u.pathname==='/api/state')value={username:'rapid115',storages:[{id:'p1',name:'115 主账号',type:'115',enabled:true,config:{}},{id:'other',name:'移动',type:'mobile',enabled:true,config:{}},{id:'p2',name:'115 第二账号',type:'115',enabled:true,config:{}}],tasks:[],settings:{},cache:{},logs:[]}
    else if(u.pathname==='/api/115-rapid'){if(req.method()==='PUT'){config=req.postDataJSON();writes.push(config)}value=config}
    else if(u.pathname==='/api/115-rapid/records')value={items,running}
    else if(u.pathname==='/api/115-rapid/run'){expect(req.postDataJSON()).toEqual({confirm:true});runs++;running=true;items=[{id:'r1',time:'2026-10-14T12:30:00+08:00',name:'影片.mp4',size:1048576,pickcode:'pickabc',status:'success',source:'手动'}];value={started:true}}
    else if(u.pathname==='/api/local-directories')value={path:'/media/incoming',items:[]}
    else if(u.pathname==='/api/files')value=[{id:'99',name:'目标',isDir:true}]
    return r.fulfill({json:value})
  })
  return {writes,get config(){return config},get runs(){return runs},setRecords(value){items=value},setConfig(value){config=value}}
}

test('115 rapid center is first plugin, persists manual directories and confirms execution',async({page},info)=>{
  const f=await fixture(page)
  await page.goto('/tools')
  await expect(page.locator('.plugin-card').first()).toHaveAttribute('aria-label','115 秒传')
  await expect.poll(()=>page.locator('.page-content').evaluate(el=>el.getAnimations().every(a=>a.playState==='finished'))).toBe(true)
  const iconBounds=await page.locator('.plugin-card').evaluateAll(cards=>cards.map(card=>{const icon=card.querySelector('.rapid115-icon,.plugin-symbol');const r=icon.getBoundingClientRect();return [r.width,r.height]}));expect(iconBounds.every(([w,h])=>w===44&&h===44)).toBe(true)
  await page.getByRole('button',{name:'115 秒传',exact:true}).click()
  await expect(page).toHaveURL(/\/tools\/115-rapid$/)
  await expect(page.getByRole('heading',{name:'115秒传中心',exact:true})).toBeVisible()
  await expect(page.getByRole('button',{name:'开始秒传',exact:true})).toBeDisabled()
  const manual=page.locator('.rapid115-section').filter({has:page.getByRole('heading',{name:'手动秒传',exact:true})})
  await expect(manual.getByRole('button',{name:'秒传成功处理',exact:true})).toContainText('清理源文件')
  await expect(manual.getByRole('button',{name:'秒传失败处理',exact:true})).toContainText('跳过')
  await expect(manual.getByLabel('秒传文件扩展名',{exact:true})).toHaveValue('')
  await manual.getByRole('button',{name:'选择待传目录',exact:true}).click()
  await page.getByRole('button',{name:'选择当前目录',exact:true}).click()
  await expect.poll(()=>f.config.manual.source).toBe('/media/incoming')
  await manual.getByRole('button',{name:'选择秒传目标目录',exact:true}).click()
  const picker=page.getByRole('dialog',{name:'选择存储目录',exact:true})
  await expect(picker.locator('.source-accounts button')).toHaveCount(2)
  await expect(picker.locator('.source-accounts button').first()).toHaveAttribute('aria-pressed','true')
  await picker.getByRole('button',{name:'目标',exact:true}).click()
  await picker.getByRole('button',{name:'选择当前目录',exact:true}).click()
  await expect.poll(()=>f.config.manual.target).toBe('99')
  await manual.getByRole('button',{name:'视频扩展名',exact:true}).click()
  await expect.poll(()=>f.config.manual.extensions.includes('mp4')).toBe(true)
  await manual.getByLabel('秒传文件扩展名',{exact:true}).fill('mp4;mkv')
  await manual.getByLabel('秒传文件扩展名',{exact:true}).press('Enter')
  await expect.poll(()=>f.config.manual.extensions).toBe('mp4;mkv')
  await page.getByRole('button',{name:'开始秒传',exact:true}).click()
  await expect(page.getByRole('dialog',{name:'确认开始115秒传',exact:true})).toBeVisible()
  expect(f.runs).toBe(0)
  await page.getByRole('button',{name:'确认秒传',exact:true}).click()
  await expect(page.locator('.rapid115-record')).toContainText('影片.mp4')
  await expect(page.locator('.rapid115-record')).toContainText('秒传成功')
  expect(f.runs).toBe(1)
  await page.screenshot({path:info.outputPath('rapid115-desktop.png')})
  await page.getByRole('button',{name:'返回页面',exact:true}).click()
  await expect(page).toHaveURL(/\/tools$/)
  await page.getByRole('button',{name:'115 秒传',exact:true}).click()
  await expect(manual.getByRole('textbox',{name:'待传目录',exact:true})).toHaveValue('/media/incoming')
  await page.reload()
  await expect(manual.getByRole('textbox',{name:'目标目录',exact:true})).toHaveValue('/目标')
})

test('115 rapid monitor edits use standard modal and monitor inner scroll chains to page',async({page},info)=>{
  const f=await fixture(page)
  f.setConfig({manual:{...defaults(),source:'/media/incoming',storageId:'p1',target:'99',targetLabel:'115 / 目标'},watches:[]})
  await page.goto('/tools/115-rapid')
  await page.getByRole('button',{name:'添加监控',exact:true}).click()
  const modal=page.getByRole('dialog',{name:'添加监控',exact:true})
  await expect(modal).toHaveCSS('width','640px')
  await modal.getByLabel('监控名称',{exact:true}).fill('实时监控')
  await expect(modal.getByRole('button',{name:'监控模式',exact:true})).toContainText('文件系统监控')
  await modal.getByRole('button',{name:'选择待传目录',exact:true}).click()
  await page.getByRole('button',{name:'选择当前目录',exact:true}).click()
  await modal.getByRole('button',{name:'选择秒传目标目录',exact:true}).click()
  await page.getByRole('dialog',{name:'选择存储目录',exact:true}).getByRole('button',{name:'选择当前目录',exact:true}).click()
  await modal.getByRole('button',{name:'监控模式',exact:true}).click()
  await modal.getByRole('option',{name:'Cron表达式',exact:true}).click()
  await expect(modal.getByRole('listbox')).toHaveCount(0)
  await modal.getByLabel('Cron表达式',{exact:true}).fill('*/5 * * * *')
  await modal.getByRole('button',{name:'添加',exact:true}).click()
  await expect(modal).toHaveCount(0)
  await expect(page.locator('.rapid115-watch')).toContainText('实时监控')
  expect(f.config.watches[0]).toMatchObject({mode:'cron',cron:'*/5 * * * *',source:'/media/incoming',storageId:'p1'})
  await page.getByRole('button',{name:'编辑 实时监控',exact:true}).click()
  await page.getByRole('dialog',{name:'编辑监控',exact:true}).getByLabel('监控名称',{exact:true}).fill('改名监控')
  await page.getByRole('button',{name:'保存',exact:true}).click()
  await expect(page.locator('.rapid115-watch')).toContainText('改名监控')
  await page.getByRole('button',{name:'停用 改名监控',exact:true}).click()
  await expect(page.locator('.rapid115-watch')).toHaveClass(/disabled/)
  await page.getByRole('button',{name:'删除 改名监控',exact:true}).click()
  await page.getByRole('dialog',{name:'删除监控',exact:true}).getByRole('button',{name:'删除',exact:true}).click()
  await expect(page.locator('.rapid115-watch')).toHaveCount(0)
  const watches=Array.from({length:8},(_,i)=>({...defaults(),id:String(i),name:'监控'+i,source:'/media/incoming',storageId:'p1',target:'99',targetLabel:'115 / 目标',mode:'watch',enabled:true}))
  f.setConfig({...f.config,watches});f.setRecords(Array.from({length:20},(_,i)=>({id:String(i),time:'2026-10-14T12:30:00+08:00',name:'音频'+i+'.flac',size:1024,status:i%2?'uploaded':'failed',source:'监控'})))
  await page.setViewportSize({width:1440,height:720});await page.reload()
  const list=page.locator('.rapid115-watch-list'),outer=page.locator('.page-scroll > .thin-scroll-area')
  await list.hover();await page.mouse.wheel(0,160)
  await expect.poll(()=>list.evaluate(el=>el.scrollTop)).toBeGreaterThan(0)
  expect(await outer.evaluate(el=>el.scrollTop)).toBe(0)
  await list.evaluate(el=>el.scrollTop=el.scrollHeight);await page.mouse.wheel(0,160)
  await expect.poll(()=>outer.evaluate(el=>el.scrollTop)).toBeGreaterThan(0)
  for(const [width,theme] of [[1440,'light'],[1440,'dark'],[390,'light'],[390,'dark']]){await page.setViewportSize({width,height:900});await page.evaluate(theme=>document.documentElement.dataset.theme=theme,theme);await expect(page.getByRole('button',{name:'添加监控',exact:true})).toBeEnabled();await outer.evaluate(el=>el.scrollTop=0);expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);await page.screenshot({path:info.outputPath(`rapid115-${theme}-${width}.png`),fullPage:true})}
})

test('STRM mode closes selection without a check and simulcast retention defaults to 12 hours',async({page},info)=>{
  await fixture(page)
  await page.route('**/api/strm-replace',r=>r.fulfill({json:{tasks:[]}}))
  await page.route('**/api/115-simulcast',r=>r.fulfill({json:{}}))
  await page.goto('/tools')
  await page.getByRole('button',{name:'STRM 替换',exact:true}).click()
  await page.getByRole('button',{name:'替换模式',exact:true}).click()
  await expect(page.getByRole('option',{name:'常规替换',exact:true}).locator('.lucide-check')).toHaveCount(0)
  await page.getByRole('option',{name:'正则替换',exact:true}).click()
  await expect(page.getByRole('listbox')).toHaveCount(0)
  await expect(page.getByRole('button',{name:'替换模式',exact:true})).toContainText('正则替换')
  await page.screenshot({path:info.outputPath('replace-mode.png')})
  await page.keyboard.press('Escape')
  await page.getByRole('button',{name:'115 同播复制',exact:true}).click()
  await page.getByRole('button',{name:'存储绑定',exact:true}).click()
  const dialog=page.getByRole('dialog',{name:'添加同播复制',exact:true})
  await expect(dialog.getByRole('spinbutton',{name:'复制保活',exact:true})).toHaveValue('12')
  await expect(dialog.locator('.number-unit')).toHaveText('h')
  const rects=await dialog.locator('.simulcast-binding>label').evaluateAll(els=>els.map(el=>{const r=el.getBoundingClientRect();return {x:r.x,y:r.y}}));expect(rects[0].y).toBe(rects[1].y);expect(rects[0].x).toBeLessThan(rects[1].x)
  await page.screenshot({path:info.outputPath('simulcast-retention.png')})
})

test('ED2K binding settings sit beside 115 accounts and autosave without a header save button',async({page},info)=>{
  await fixture(page)
  const writes=[];let pool={storageIds:['p1'],retentionHours:24,directory:'/最近接收/Aether',rotationMinutes:1440}
  await page.route('**/api/ed2k/pool',r=>{if(r.request().method()==='PUT'){pool=r.request().postDataJSON();writes.push(pool)}return r.fulfill({json:pool})})
  await page.route('**/api/ed2k/status',r=>r.fulfill({json:{stats:{},items:[]}}))
  await page.goto('/tasks/ed2k')
  const entry=page.getByRole('button',{name:'打开存储绑定池',exact:true})
  await expect(entry).toBeVisible();const before=await entry.locator('svg').evaluate(el=>getComputedStyle(el).transform)
  await entry.hover();await expect.poll(()=>entry.locator('svg').evaluate(el=>getComputedStyle(el).transform)).not.toBe(before)
  await entry.click()
  await expect(page.getByRole('button',{name:'保存设置',exact:true})).toHaveCount(0)
  const header=page.locator('.cas-pool-page>.task-heading')
  await expect(header.locator('.jump-title')).toHaveCount(0)
  await expect(header.getByRole('button',{name:'返回 ED2K 任务',exact:true})).toContainText('返回页面')
  const region=page.getByRole('region',{name:'ED2K 存储绑定池',exact:true})
  await expect(region.getByRole('spinbutton',{name:'轮换时间',exact:true})).toHaveValue('24')
  const rects=await region.locator('.cas-pool-groups,.cas-pool-advanced').evaluateAll(els=>els.map(el=>{const r=el.getBoundingClientRect();return {x:r.x,y:r.y,right:r.right}}));expect(rects[1].x).toBeGreaterThan(rects[0].right);expect(rects[0].y).toBe(rects[1].y)
  const positions=await region.locator('.cas-pool-options>.field').evaluateAll(els=>els.map(el=>{const r=el.getBoundingClientRect();return {x:r.x,y:r.y}}));expect(positions).toHaveLength(3);expect(new Set(positions.map(r=>r.x)).size).toBe(1)
  await region.getByRole('spinbutton',{name:'秒传保活',exact:true}).fill('48')
  await expect.poll(()=>writes.at(-1)?.retentionHours).toBe(48)
  await region.getByRole('button',{name:'绑定 115 第二账号',exact:true}).click()
  await expect.poll(()=>writes.at(-1)?.storageIds).toEqual(['p1','p2'])
  await page.screenshot({path:info.outputPath('ed2k-pool-right-settings.png')})
  await header.getByRole('button',{name:'返回 ED2K 任务',exact:true}).click()
  await expect(page.locator('.cas-pool-page')).toHaveClass(/leaving/)
  await expect(page).toHaveURL(/\/tasks\/ed2k$/)
})

test('rapid controls share editor height, icon presets and nested day/night surfaces; monitors reuse backup filters',async({page},info)=>{
 const f=await fixture(page);await page.goto('/tools/115-rapid');await expect(page.getByRole('button',{name:'添加监控',exact:true})).toBeEnabled()
 const manual=page.locator('.rapid115-top>.rapid115-section').first()
 const sizes=await manual.locator('.directory-input input,.rounded-select-trigger,.rapid115-extensions input').evaluateAll(els=>els.map(el=>Math.round(el.getBoundingClientRect().height)));expect(new Set(sizes)).toEqual(new Set([40]))
 await expect(page.getByRole('button',{name:'开始秒传',exact:true})).toHaveCSS('height','36px')
 await expect(manual.locator('.directory-input').first().locator('svg')).toHaveClass(/lucide-folder-open/)
 const rects=await manual.locator('.directory-input').first().evaluate(el=>({icon:el.querySelector('svg').getBoundingClientRect().x,text:el.querySelector('input').getBoundingClientRect().x}));expect(rects.icon).toBeGreaterThan(rects.text)
 const preset=manual.getByRole('button',{name:'视频扩展名'});const normal=await preset.evaluate(el=>getComputedStyle(el).color);await preset.hover();await expect.poll(()=>preset.evaluate(el=>getComputedStyle(el).color)).not.toBe(normal);const hover=await preset.evaluate(el=>getComputedStyle(el).color);await preset.click();await page.mouse.move(800,80);await expect(preset).toHaveAttribute('aria-pressed','true');expect(await preset.evaluate(el=>getComputedStyle(el).color)).not.toBe(hover);await expect(preset).toHaveCSS('background-color','rgba(0, 0, 0, 0)')
 await page.getByRole('button',{name:'添加监控',exact:true}).click();const modal=page.getByRole('dialog',{name:'添加监控',exact:true});await modal.getByLabel('监控名称').fill('筛选监控');await modal.getByRole('button',{name:'选择待传目录'}).click();await page.getByRole('button',{name:'选择当前目录',exact:true}).click();await modal.getByRole('button',{name:'选择秒传目标目录'}).click();await page.getByRole('dialog',{name:'选择存储目录'}).getByRole('button',{name:'选择当前目录',exact:true}).click()
 await modal.getByRole('button',{name:'添加规则',exact:true}).click();await page.getByRole('dialog',{name:'选择规则类型'}).getByRole('button',{name:'扩展名',exact:true}).click();await modal.getByRole('textbox',{name:'匹配内容 1',exact:true}).fill('.MKV');await modal.getByRole('textbox',{name:'匹配内容 1',exact:true}).press('Enter');await expect(modal.locator('.backup-token')).toHaveText('mkv')
 const luminance=value=>{const c=value.match(/[\d.]+/g).slice(0,3).map(Number);return c[0]+c[1]+c[2]}
 for(const theme of ['light','dark']){await page.evaluate(t=>document.documentElement.dataset.theme=t,theme);const inputColor=theme==='light'?'rgb(255, 255, 255)':'rgb(41, 44, 52)';await expect(modal.locator('.backup-filter input').last()).toHaveCSS('background-color',inputColor);for(const selector of ['.directory-input input','.rounded-select-trigger','.backup-token'])await expect(modal.locator(selector).first()).toHaveCSS('background-color',inputColor);const colors=await modal.evaluate(el=>[el,el.querySelector('.backup-filter-frame'),el.querySelector('.backup-filter'),el.querySelector('.backup-token-input input')].map(e=>getComputedStyle(e).backgroundColor));const levels=colors.map(luminance);expect(levels[3]>levels[2]&&levels[2]>levels[1]).toBe(true);await page.screenshot({path:info.outputPath(`rapid-monitor-filters-${theme}.png`)})}
 await modal.getByRole('button',{name:'添加',exact:true}).click();await expect.poll(()=>f.config.watches[0]?.filters?.[0]?.value).toBe('mkv')
})

test('simulcast directory picker preserves every chosen path segment',async({page})=>{
 await fixture(page);let saved={};await page.route('**/api/115-simulcast',r=>{if(r.request().method()==='PUT')saved=r.request().postDataJSON();return r.fulfill({json:saved})});await page.route('**/api/files?**',r=>{const path=new URL(r.request().url()).searchParams.get('path');return r.fulfill({json:path==='/'?[{id:'1',name:'影视',isDir:true}]:path==='1'?[{id:'2',name:'副本',isDir:true}]:[]})});await page.goto('/tools');await page.getByRole('button',{name:'115 同播复制',exact:true}).click();await page.getByRole('button',{name:'存储绑定',exact:true}).click();const modal=page.getByRole('dialog',{name:'添加同播复制'});await modal.getByRole('button',{name:'115 存储',exact:true}).click();await modal.getByRole('option',{name:'115 主账号'}).click();await modal.getByRole('button',{name:'选择复制目录'}).click();await page.getByRole('dialog',{name:'选择存储目录'}).getByRole('button',{name:'影视',exact:true}).click();await page.getByRole('button',{name:'副本',exact:true}).click();await page.getByRole('button',{name:'选择当前目录',exact:true}).click();await expect(modal.getByRole('button',{name:'选择复制目录'})).toContainText('/影视/副本');await modal.getByRole('button',{name:'保存',exact:true}).click();expect(saved.p1.directoryLabel).toBe('/影视/副本')
})

test('editable rapid directories start empty, resolve paths before saving and never reuse an invalid target',async({page})=>{
 const f=await fixture(page);await page.route('**/api/files?**',r=>{const p=new URL(r.request().url()).searchParams.get('path');return r.fulfill({json:p==='/'?[{id:'1',name:'影视',isDir:true}]:p==='1'?[{id:'2',name:'接收',isDir:true}]:[]})})
 await page.goto('/tools/115-rapid');const manual=page.locator('.rapid115-top>.rapid115-section').first(),source=manual.getByRole('textbox',{name:'待传目录',exact:true}),target=manual.getByRole('textbox',{name:'目标目录',exact:true})
 for(const input of [source,target]){await expect(input).toHaveValue('');expect(await input.getAttribute('placeholder')).toBeNull()}
 await source.fill('/media/incoming');await source.press('Enter');await expect.poll(()=>f.config.manual.source).toBe('/media/incoming')
 await target.fill('/影视/接收');await target.press('Enter');await expect.poll(()=>f.config.manual.target).toBe('2');expect(f.config.manual.storageId).toBe('p1')
 await expect(manual.getByRole('button',{name:'开始秒传',exact:true})).toBeEnabled()
 await target.fill('/不存在');await target.press('Enter');await expect(page.getByRole('alert')).toContainText('目标目录不存在');await expect(manual.getByRole('button',{name:'开始秒传',exact:true})).toBeDisabled()
 await target.fill('');await target.press('Enter');await expect.poll(()=>f.config.manual.target).toBe('');await expect(target).toHaveValue('')
 await page.getByRole('button',{name:'添加监控',exact:true}).click();const editor=page.getByRole('dialog',{name:'添加监控',exact:true});await expect(editor.getByRole('textbox',{name:'待传目录',exact:true})).toHaveValue('');await expect(editor.getByRole('textbox',{name:'目标目录',exact:true})).toHaveValue('');await editor.getByLabel('监控名称').fill('手动路径');await editor.getByRole('textbox',{name:'待传目录',exact:true}).fill('/media/watch');await editor.getByRole('textbox',{name:'目标目录',exact:true}).fill('/影视/接收');await editor.getByRole('textbox',{name:'目标目录',exact:true}).press('Enter');await editor.getByRole('button',{name:'添加',exact:true}).click();await expect.poll(()=>f.config.watches[0]?.target).toBe('2')
})

test('a late directory lookup cannot restore a target after it is cleared',async({page})=>{
 const f=await fixture(page);let release,started
 const requested=new Promise(resolve=>started=resolve),gate=new Promise(resolve=>release=resolve)
 await page.route('**/api/files?**',async r=>{started();await gate;await r.fulfill({json:[{id:'late',name:'迟到',isDir:true}]})})
 await page.goto('/tools/115-rapid');const target=page.getByRole('textbox',{name:'目标目录',exact:true})
 await target.fill('/迟到');await target.press('Enter');await requested
 await target.fill('');await target.press('Enter');release()
 await expect.poll(()=>f.config.manual.target).toBe('');await expect(target).toHaveValue('');await expect(page.getByRole('button',{name:'开始秒传',exact:true})).toBeDisabled()
})
