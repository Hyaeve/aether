import {test,expect} from '@playwright/test'

const longName='很长的影视分享目录名称'.repeat(12)
async function fixture(page){
 const calls=[],writes=[]
 const storages=[{id:'p',name:'115 家庭影音存储池名称非常长需要省略',type:'115',enabled:true,viewRevision:'one',config:{}},{id:'p2',name:'115 第二账号',type:'115',enabled:true,config:{}},{id:'q',name:'夸克',type:'quark',enabled:true,config:{}}]
 const job={source:'/media/很长的祖先目录/待传目录',storageId:'p',target:'10',targetLabel:'115 /很长的祖先目录/目标目录',extensions:'mkv',successAction:'keep',failureAction:'skip'}
 let rapid={manual:job,watches:[{...job,id:'one',name:'影视监控名称很长'.repeat(6),mode:'watch',enabled:true},{...job,id:'two',name:'定时监控',mode:'cron',cron:'*/15 * * * *',enabled:true}]}
 let cleanup={enabled:false,rules:[]}
 await page.route('**/api/**',route=>{
  const req=route.request(),u=new URL(req.url());let value={}
  if(u.pathname==='/api/auth/status')value={initialized:true,authenticated:true}
  else if(u.pathname==='/api/state')value={username:'cloud-rapid',storages,tasks:[],settings:{},cache:{hits:10000,misses:1000,entries:10000},traffic:{},logs:[]}
  else if(u.pathname==='/api/cloud/manage'){
   if(req.method()==='POST'){writes.push(req.postDataJSON());value={count:1}}
   else{
    calls.push(`${u.searchParams.get('kind')}:${u.searchParams.get('storage')}:${u.searchParams.get('page')}`)
    const page=Number(u.searchParams.get('page'))||1,kind=u.searchParams.get('kind')
    value={items:Array.from({length:page===1?60:2},(_,i)=>({id:`${page}-${i}`,name:i===0?longName:`影片${page}-${i}.mkv`,size:1024,url:'https://example.test/share/'+i,path:'/很长的祖先目录/'.repeat(12)+'最深目录',time:'2026-10-27T18:59:00+08:00',status:kind==='shares'?'有效':'running',progress:.45})),nextPage:page===1?2:0}
   }
  }
  else if(u.pathname==='/api/115-rapid'){if(req.method()==='PUT'){rapid=req.postDataJSON();writes.push(rapid)}value=rapid}
  else if(u.pathname==='/api/115-rapid/records'){
   const scoped=u.searchParams.get('watch'),size=scoped?10:30,current=Math.max(1,Math.min(scoped?3:2,Number(u.searchParams.get('page'))||1)),total=scoped?25:37
   value={items:Array.from({length:Math.min(size,total-(current-1)*size)},(_,i)=>({id:`r-${current}-${i}`,name:i===0?longName+'.mkv':`影片${current}-${i}.mkv`,source:i%2?'手动':'监控 · 影视',sourceKind:i%2?'manual':'monitor',size:1024,pickcode:'abcdefghijklmnopq',time:'2026-10-27T18:59:00+08:00',status:i%2?'success':'running',phase:'计算文件摘要',done:45,total:100})),page:current,pages:scoped?3:2,total,manualRunning:false,monitorRunning:true}
  }
  else if(u.pathname==='/api/115-folder-cleanup'){if(req.method()==='PUT'){cleanup=req.postDataJSON();cleanup.rules=cleanup.rules.map((r,i)=>({...r,id:r.id||`rule-${i}`}));writes.push(cleanup)}value=cleanup}
  else if(u.pathname==='/api/cron/validate'){value={error:req.postDataJSON().expression==='*/14 * * * *'?'Cron计划相邻执行间隔至少15分钟；如每14分钟执行则不允许':''}}
  else if(u.pathname==='/api/files')value=u.searchParams.get('path')==='/'?[{id:'10',name:'清理目录',isDir:true}]:[]
  else if(u.pathname==='/api/cloud/directory-trail')value={items:[{id:'0',name:'根目录'},{id:'10',name:'清理目录'}]}
  else if(u.pathname==='/api/115-recycle-cleanup')value={enabled:false,rules:[]}
  else if(u.pathname==='/api/automations'||u.pathname==='/api/backup-rules')value=[]
  else if(u.pathname==='/api/virtual-library/quark')value={cards:[],settings:{storageIds:['q'],invalidDays:15,retentionHours:12,mediaExtensions:'mkv',metadataExtensions:'nfo'},stats:{}}
  else if(u.pathname==='/api/115-simulcast/enabled')value={enabled:true}
  else if(u.pathname==='/api/quark-takeover')value={enabled:false,bindings:[]}
  else if(u.pathname.startsWith('/api/plugins/'))value={enabled:false}
  return route.fulfill({json:value})
 })
 return {calls,writes,get rapid(){return rapid},get cleanup(){return cleanup}}
}
async function go(page,label){await page.getByRole('link',{name:label,exact:true}).click()}
async function stable(page){await expect.poll(()=>page.locator('.page-content').evaluate(el=>el.getAnimations({subtree:true}).every(a=>a.playState==='finished'||a.effect?.getTiming().iterations===Infinity))).toBe(true)}

test('all cloud columns reuse session pages until explicit refresh and remember each selected pool',async({page})=>{
 const f=await fixture(page);await page.goto('/storage/shares')
 for(const label of ['分享栏','云下载','回收站']){
  await go(page,label);await expect(page.locator('.cloud-total')).toHaveText('60 项')
  const before=f.calls.length
  await go(page,'存储池');await go(page,label)
  await expect(page.locator('.cloud-total')).toHaveText('60 项');expect(f.calls).toHaveLength(before)
  await page.getByRole('button',{name:'刷新云盘栏目'}).click();await expect.poll(()=>f.calls.length).toBe(before+1)
 }
 await go(page,'分享栏')
 await page.getByRole('button',{name:'选择云盘存储池'}).click();await page.getByRole('option',{name:'115 第二账号',exact:true}).click()
 await expect.poll(()=>f.calls.at(-1)).toBe('shares:p2:1')
 await go(page,'存储池');const before=f.calls.length;await go(page,'分享栏')
 await expect(page.getByRole('button',{name:'选择云盘存储池'})).toContainText('115 第二账号');expect(f.calls).toHaveLength(before)
 const scroll=page.locator('.page-scroll>.thin-scroll-area')
 await scroll.evaluate(el=>el.scrollTop=el.scrollHeight)
 await expect(page.locator('.cloud-total')).toHaveText('62 项')
 const paged=f.calls.length
 await go(page,'存储池');await go(page,'分享栏')
 await expect(page.locator('.cloud-total')).toHaveText('62 项');expect(f.calls).toHaveLength(paged)
 await page.getByRole('button',{name:'刷新云盘栏目'}).click();await expect(page.locator('.cloud-total')).toHaveText('60 项')
})

for(const [width,theme] of [[1440,'light'],[1440,'dark'],[390,'light'],[320,'dark']]){
 test(`compact cloud tools, columns and stationary names ${width} ${theme}`,async({page},info)=>{
  await fixture(page);await page.setViewportSize({width,height:900});await page.goto('/storage/shares');await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
  for(const [label,kind] of [['分享栏','shares'],['云下载','downloads'],['回收站','trash']]){
   await go(page,label);await expect(page.locator('.cloud-total')).toHaveText('60 项');await stable(page)
   const toolbar=page.locator('.cloud-toolbar')
   const positions=await toolbar.evaluate(el=>[...el.children].map(c=>{const r=c.getBoundingClientRect();return {x:r.x,right:r.right,y:r.y+r.height/2,width:r.width}}))
   expect(positions.at(-1).width).toBe(width>760?180:width<=440?124:140)
   expect(Math.max(...positions.map(r=>r.y))-Math.min(...positions.map(r=>r.y))).toBeLessThan(1)
   expect(positions.every((r,i)=>!i||r.x>=positions[i-1].right)).toBe(true)
   expect(positions[0].x).toBeGreaterThanOrEqual(0)
   expect(await toolbar.evaluate(el=>el.getBoundingClientRect().left>=0&&el.getBoundingClientRect().right<=innerWidth)).toBe(true)
   const row=page.locator('.cloud-management-row').first(),name=row.locator('.cloud-row-name>span')
   const bounds=await row.evaluate(el=>({row:el.getBoundingClientRect().toJSON(),grid:getComputedStyle(el).gridTemplateColumns,actions:[...el.querySelectorAll('.cloud-row-actions button')].map(b=>b.getBoundingClientRect().toJSON())}))
   expect(bounds.actions.every(r=>r.left>=bounds.row.left&&r.right<=bounds.row.right),JSON.stringify(bounds)).toBe(true)
   await expect(row).toHaveCSS('height',`${width<=600?kind==='downloads'?104:92:kind==='downloads'?60:52}px`)
   if(width>1100){
    const columns=await row.evaluate(el=>getComputedStyle(el).gridTemplateColumns.split(' ').map(Number.parseFloat))
    if(kind==='downloads'){expect(columns[0]).toBeGreaterThan(columns[3]);expect(columns[2]).toBeGreaterThan(columns[3])}
    else expect(columns[0]).toBeGreaterThan(columns[2])
   }
   expect(await name.evaluate(el=>el.scrollWidth>el.clientWidth)).toBe(true)
   if(kind==='shares'){
    await page.clock.install();await page.clock.pauseAt(new Date())
    await name.hover();await page.clock.runFor(299);await expect(page.getByRole('tooltip')).toHaveCount(0)
    await page.clock.runFor(1);await expect(page.getByRole('tooltip')).toHaveText(longName)
    await page.keyboard.press('Escape');await page.clock.resume()
   }
   if(kind==='downloads')await expect(row.locator('progress')).toHaveCSS('height','4px')
   if(kind!=='shares'){
    const tail=await row.locator('.cloud-path').evaluate(el=>{const bdi=el.querySelector('bdi'),text=bdi.firstChild,range=document.createRange();range.setStart(text,text.length-4);range.setEnd(text,text.length);const r=range.getBoundingClientRect(),box=el.getBoundingClientRect();return r.left>=box.left&&r.right<=box.right})
    expect(tail).toBe(true)
   }
   if(kind==='trash')await expect(page.locator('.cloud-management-columns')).toContainText('原始路径')
   expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
   await page.screenshot({path:info.outputPath(`${kind}-${width}-${theme}.png`)})
  }
 })
}

test('rapid records use page scrolling, compact status and independent monitor pagination and sorting',async({page},info)=>{
 const f=await fixture(page);await page.goto('/tools/115-rapid')
 const panel=page.locator('.rapid115-records')
 await expect(panel.locator('.rapid115-record')).toHaveCount(30)
 await expect(panel.locator('.rapid115-source.monitor').first()).toHaveCSS('color','rgb(53, 107, 140)')
 await expect(panel.locator('.rapid115-source.manual').first()).toHaveCSS('color','rgb(201, 109, 18)')
 await expect(panel.locator('progress').first()).toHaveCSS('width','104px')
 await expect(panel.locator('progress').first()).toHaveCSS('height','4px')
 await expect(panel.locator('.rapid115-status svg').first()).toHaveClass(/lucide-loader-circle/)
 expect(await panel.locator('.rapid115-record-frame').evaluate(el=>!['auto','scroll'].includes(getComputedStyle(el).overflowY))).toBe(true)
 const cards=page.locator('.rapid115-watch')
 await expect(cards.first().locator('.watch-avatar')).toHaveText('影视')
 await expect(cards.first().locator('.watch-avatar')).toHaveCSS('height','48px')
 expect(await cards.first().locator('.watch-avatar').evaluate(el=>getComputedStyle(el).backgroundColor)).not.toBe(await cards.last().locator('.watch-avatar').evaluate(el=>getComputedStyle(el).backgroundColor))
 await cards.first().focus();await page.keyboard.press('Alt+ArrowDown')
 await expect.poll(()=>f.rapid.watches.map(w=>w.id)).toEqual(['two','one'])
 const from=await cards.first().locator('.watch-copy').boundingBox(),to=await cards.last().locator('.watch-copy').boundingBox()
 await page.mouse.move(from.x+15,from.y+12);await page.mouse.down();await page.waitForTimeout(180)
 await expect(page.locator('.card-drag-ghost')).toHaveCount(1)
 await page.mouse.move(to.x+15,to.y+12,{steps:8});await page.mouse.up()
 await expect.poll(()=>f.rapid.watches.map(w=>w.id)).toEqual(['one','two'])
 await cards.last().getByRole('button',{name:/查看/}).click()
 const dialog=page.getByRole('dialog',{name:/秒传记录/})
 await expect(dialog.locator('.rapid115-record')).toHaveCount(10)
 await expect(dialog.locator('.rapid115-record-columns>span')).toHaveText(['文件名称','文件大小','PickCode','执行时间','秒传状态'])
 await expect(dialog.locator('time').first()).toHaveText(/^\d{2}\/\d{2} \d{2}:\d{2}$/)
 expect(await dialog.locator('.rapid115-pickcode').first().evaluate(el=>el.scrollWidth<=el.clientWidth)).toBe(true)
 await dialog.getByRole('button',{name:'最后一页',exact:true}).click()
 await expect(dialog.locator('.rapid115-record')).toHaveCount(5)
 await expect(panel.locator('.rapid115-record')).toHaveCount(30)
 await page.screenshot({path:info.outputPath('monitor-record-dialog.png')})
 await page.keyboard.press('Escape')
 for(const [width,theme] of [[1440,'light'],[1440,'dark'],[390,'light'],[390,'dark']]){
  await page.setViewportSize({width,height:900});await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
  expect(await page.locator('.rapid115-section').evaluateAll(els=>new Set(els.map(el=>getComputedStyle(el).backgroundColor)).size)).toBe(1)
  if(theme==='light')await expect(panel.locator('.rapid115-record').first()).toHaveCSS('background-color','rgb(255, 255, 255)')
  await page.locator('.page-scroll>.thin-scroll-area').evaluate(el=>el.scrollTop=0);await stable(page)
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
  await page.screenshot({path:info.outputPath(`rapid-${width}-${theme}.png`)})
 }
})

test('monitor choices have concise explanations and invalid cron exposes a stationary warning',async({page})=>{
 await fixture(page);await page.goto('/tools/115-rapid');await page.getByRole('button',{name:'添加监控',exact:true}).click()
 const dialog=page.getByRole('dialog',{name:'添加监控',exact:true})
 await dialog.getByRole('button',{name:'秒传失败处理',exact:true}).click()
 await expect(dialog.getByRole('option')).toHaveText(['直接跳过','重试传输','重试删除'])
 const explanation=dialog.getByRole('option',{name:'重试传输',exact:true}).locator('svg')
 await explanation.hover();await expect(page.getByRole('tooltip')).toContainText('10、30、60分钟')
 await dialog.getByRole('option',{name:'直接跳过',exact:true}).click()
 await dialog.getByRole('button',{name:'监控模式',exact:true}).click();await dialog.getByRole('option',{name:'Cron表达式',exact:true}).click()
 await dialog.getByRole('textbox',{name:'Cron表达式',exact:true}).fill('*/14 * * * *')
 const warning=dialog.getByRole('button',{name:'Cron计划不可用原因'})
 await expect(warning).toBeVisible();await warning.hover()
 await expect(page.getByRole('tooltip')).toContainText('至少15分钟')
 await expect(dialog.getByRole('textbox',{name:'Cron表达式',exact:true})).toHaveAttribute('aria-invalid','true')
 await dialog.getByRole('textbox',{name:'Cron表达式',exact:true}).fill('*/15 * * * *')
 await expect(warning).toHaveCount(0)
})

test('115 folder cleanup plugin selects only 115 folders and confirms zero-day scope',async({page},info)=>{
 const f=await fixture(page);await page.goto('/tools');const cards=page.locator('.plugin-card')
 await expect(cards.first()).toHaveAttribute('aria-label','115签到')
 await expect(cards.nth(2)).toHaveAttribute('aria-label','115 同播复制')
 await expect(cards.nth(3)).toHaveAttribute('aria-label','115文件夹清理')
 await page.getByRole('button',{name:'115文件夹清理',exact:true}).click()
 await page.getByRole('button',{name:'添加文件夹',exact:true}).click()
 const editor=page.getByRole('dialog',{name:'添加清理文件夹',exact:true})
 await expect(editor.getByRole('spinbutton',{name:'清理阈值',exact:true})).toHaveValue('0')
 await expect(editor.locator('.number-unit')).toHaveText('天')
 await editor.getByRole('button',{name:'选择清理文件夹',exact:true}).click()
 const picker=page.getByRole('dialog',{name:'选择存储目录',exact:true})
 await expect(picker.locator('.source-accounts button')).toHaveCount(2)
 await picker.getByRole('button',{name:'清理目录',exact:true}).click()
 await picker.getByRole('button',{name:'选择当前目录',exact:true}).click()
 await editor.getByRole('button',{name:'保存',exact:true}).click()
 const confirm=page.getByRole('dialog',{name:'确认文件夹清理计划',exact:true})
 await expect(confirm).toContainText('全部文件');expect(f.writes).toHaveLength(0)
 await confirm.getByRole('button',{name:'确认保存',exact:true}).click()
 await expect.poll(()=>f.cleanup.rules.length).toBe(1);expect(f.cleanup.rules[0]).toMatchObject({storageId:'p',directory:'10',days:0,cron:'0 3 * * *'})
 await expect(page.getByRole('switch',{name:'启用文件夹清理',exact:true})).toHaveCount(0)
 await expect.poll(()=>f.cleanup.enabled).toBe(true)
 await page.getByRole('button',{name:'编辑文件夹清理 /清理目录',exact:true}).click()
 const edit=page.getByRole('dialog',{name:'编辑清理文件夹',exact:true}),days=edit.getByRole('spinbutton',{name:'清理阈值',exact:true})
 await days.fill('5');await days.press('e');await days.press('-');await expect(days).toHaveValue('5')
 for(const [width,theme] of [[1440,'light'],[1440,'dark'],[390,'light'],[390,'dark']]){
  await page.setViewportSize({width,height:900});await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
  expect(await edit.evaluate(el=>el.scrollWidth<=el.clientWidth)).toBe(true)
  await expect(edit.locator('.source-trigger')).toHaveCSS('background-color',theme==='light'?'rgb(255, 255, 255)':'rgb(41, 44, 52)')
  await expect(edit.locator('.source-trigger')).toHaveCSS('height','40px')
  await page.screenshot({path:info.outputPath(`folder-cleanup-${width}-${theme}.png`)})
 }
})

test('the shared minimum-Cron warning is present in every scheduled editor',async({page})=>{
 await fixture(page)
 for(const kind of ['task','automation','backup','quark','recycle','folder']){
  await page.goto(kind==='task'?'/tasks/strm':kind==='automation'?'/tasks/automation':kind==='backup'?'/transfer/backup':kind==='quark'?'/virtual/quark':'/tools')
  if(kind==='task')await page.getByRole('button',{name:'添加 STRM 任务卡片',exact:true}).click()
  if(kind==='automation'){await page.getByRole('button',{name:'添加联动',exact:true}).click();await page.getByRole('button',{name:'触发方式',exact:true}).click();await page.getByRole('option',{name:'定时触发',exact:true}).click()}
  if(kind==='backup'){await page.locator('.task-heading').getByRole('button',{name:'添加规则',exact:true}).click();await page.getByRole('dialog',{name:'添加备份规则',exact:true}).getByRole('button',{name:'扫描规则',exact:true}).click()}
  if(kind==='quark')await page.getByRole('button',{name:'添加链接',exact:true}).click()
  if(kind==='recycle'){await page.getByRole('button',{name:'115回收站清理',exact:true}).click();await page.getByRole('button',{name:'添加清理任务',exact:true}).click()}
  if(kind==='folder'){await page.getByRole('button',{name:'115文件夹清理',exact:true}).click();await page.getByRole('button',{name:'添加文件夹',exact:true}).click()}
  const input=page.locator('.modal').last().locator('.cron-input input')
  await input.fill('*/14 * * * *');await expect(input).toHaveAttribute('aria-invalid','true')
  await input.fill('*/15 * * * *');await expect(input).not.toHaveAttribute('aria-invalid','true')
 }
})

test('animated statistics visibly slow down through their final ten percent',async({page})=>{
 await fixture(page)
 const now=new Date('2026-10-27T00:00:00Z')
 await page.clock.install({time:now});await page.clock.pauseAt(now)
 await page.goto('/dashboard')
 const number=page.locator('.metric-strip .animated-number').nth(2)
 await expect(number).toHaveText('0')
 await page.clock.runFor(220);const first=Number(await number.textContent())
 await page.clock.runFor(220);const second=Number(await number.textContent())
 await page.clock.runFor(440);const near=Number(await number.textContent())
 await page.clock.runFor(220);const slower=Number(await number.textContent())
 expect(first).toBeGreaterThan(3000);expect(second).toBeGreaterThan(first)
 expect(near).toBeGreaterThan(9000);expect(near).toBeLessThan(10000)
 expect(slower-near).toBeLessThan((second-first)/3)
 await page.clock.runFor(1100);await expect(number).toHaveText('10000')
 await page.clock.resume();await page.emulateMedia({reducedMotion:'reduce'})
 await go(page,'辅助工具');await go(page,'信息总览')
 await expect(page.locator('.metric-strip .animated-number').nth(2)).toHaveText('10000')
})
