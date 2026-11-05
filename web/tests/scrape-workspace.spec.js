import {test,expect} from '@playwright/test'
import {captureScreenshot} from './helpers/screenshot.js'
const browserName=process.env.AETHER_SCRAPE_BROWSER||'chromium'
test.use({browserName})
async function fixture(page){
 const writes=[],requests=[]
 const sources=['115','mobile','quark','cas'].map(provider=>({id:`${provider}-share:one`,name:`${provider}影视`,provider}))
 const organize={id:'organized',taskId:'organize',taskName:'自动归档',title:'蜗居',kind:'tv',year:'2009',tmdb:785101,poster:'/providers/quark.png',filename:'蜗居.S01E33.mkv',sourceStorage:'115主库',sourcePath:'/待整理/蜗居.S01E33.mkv',targetStorage:'115主库',targetPath:'/剧集/蜗居 (2009)/Season 1/蜗居 S01E33.mkv',method:'move',size:2400000000,status:'success',trigger:'manual',startedAt:'2026-11-04T12:00:00Z',finishedAt:'2026-11-04T12:01:00Z'}
 await page.route('**/api/**',route=>{
  const r=route.request(),u=new URL(r.url()),p=u.pathname;requests.push(p);let json={}
  if(p==='/api/auth/status')json={initialized:true,authenticated:true}
  else if(p==='/api/state')json={username:'scrape-platform',storages:[{id:'pool',type:'115',name:'115-月见风信',enabled:true,config:{}}],tasks:[{id:'strm',kind:'strm',name:'常规影视'}],scrapeLibraries:sources,settings:{},cache:{},traffic:{}}
  else if(p==='/api/strm-scrape/items')json=[]
  else if(p==='/api/strm-scrape/directories')json={root:'/media',directories:[]}
  else if(p==='/api/strm-scrape/status')json={running:false,done:1,total:1}
  else if(p==='/api/strm-scrape/settings')json={writeMode:'missing',episodes:true,fanart:false,actors:false,excluded:''}
  else if(p==='/api/directory-scrape/files')json={directory:'0',taskId:'directory:one',files:[{id:'folder',name:'剧集',isDir:true},{id:'video',name:'Arrival.2016.mkv',size:123},{id:'pointer',name:'Film.mkv.cas.strm',size:100}],items:[]}
  else if(p.startsWith('/api/directory-scrape/')){writes.push({action:p.split('/').pop(),...r.postDataJSON()});json={running:true,taskId:'directory:one'}}
  else if(p.startsWith('/api/archives/'))json={items:u.searchParams.get('mode')==='group'?[{id:'group',title:'蜗居',kind:'tv',poster:organize.poster,taskName:'自动归档',folder:'/剧集/蜗居',count:1,status:'success'}]:[organize],total:1,page:1,pages:1,pageSize:30}
  else if(['/api/generation/jobs','/api/logs','/api/115-recycle-cleanup','/api/strm-replace/jobs'].includes(p))json=[]
  return route.fulfill({json})
 })
 return {writes,requests}
}
test('scrape platform separates ordinary tasks and all four participating libraries',async({page})=>{
 await fixture(page);await page.goto('/scrape/library')
 await expect(page.getByRole('navigation',{name:'刮削平台栏目'}).getByRole('link')).toHaveText(['STRM刮削','链库刮削','目录刮削','刮削设置'])
 await expect(page.getByRole('button',{name:'打开分享目录 115影视',exact:true})).toBeVisible()
 await page.getByRole('button',{name:'打开分享目录 115影视',exact:true}).click()
 await expect(page.getByRole('button',{name:'链库卡片',exact:true})).toContainText('115影视')
 for(const [provider,label]of [['mobile','移动链库'],['quark','夸克链库'],['cas','CAS链库']]){await page.getByRole('button',{name:'链库类型',exact:true}).click();await page.getByRole('option',{name:label,exact:true}).click();await page.getByRole('button',{name:`打开分享目录 ${provider}影视`,exact:true}).click();await expect(page.getByRole('button',{name:'链库卡片',exact:true})).toContainText(`${provider}影视`)}
 await page.getByRole('link',{name:'STRM刮削',exact:true}).click();await expect(page.getByRole('button',{name:'STRM 任务',exact:true})).toContainText('常规影视')
 await page.goto('/tasks/scrape');await expect(page).toHaveURL(/\/scrape\/strm$/)
})

test('directory breadcrumbs and folder actions remain scoped without a global action row',async({page})=>{
 const f=await fixture(page),directories=[]
 await page.route('**/api/directory-scrape/files?**',route=>{
  const dir=new URL(route.request().url()).searchParams.get('directory');directories.push(dir)
  return route.fulfill({json:{directory:dir||'0',taskId:'directory:one',files:dir==='folder'?[{id:'season',name:'Season 1',isDir:true}]:dir==='season'?[{id:'video',name:'Episode.mkv',size:123}]:[{id:'folder',name:'剧集',isDir:true}],items:[]}})
 })
 await page.goto('/scrape/directory')
 await expect(page.locator('.directory-scrape-actions')).toHaveCount(0)
 await page.getByRole('button',{name:'识别 剧集',exact:true}).click();await page.getByRole('textbox',{name:'TMDB ID',exact:true}).fill('1')
 expect(f.writes).toHaveLength(0);await page.getByRole('button',{name:'识别并刮削',exact:true}).click()
 await expect.poll(()=>f.writes.length).toBe(1);expect(f.writes[0]).toMatchObject({action:'match',directory:'0',ids:['folder'],group:true})
 await page.getByRole('button',{name:'剧集',exact:true}).click();await page.getByRole('button',{name:'Season 1',exact:true}).click()
 const path=page.getByRole('navigation',{name:'刮削目录路径',exact:true});await expect(path).toContainText('根目录剧集Season 1')
 await path.getByRole('button',{name:'剧集',exact:true}).click();await expect(page.getByRole('button',{name:'Season 1',exact:true})).toBeVisible()
 await path.getByRole('button',{name:'根目录',exact:true}).click();await expect(page.getByRole('button',{name:'剧集',exact:true})).toBeVisible()
 await page.getByRole('button',{name:'重置 剧集',exact:true}).click();expect(f.writes).toHaveLength(1);await page.getByRole('button',{name:'确认重置',exact:true}).click()
 await expect.poll(()=>f.writes.length).toBe(2);expect(f.writes[1]).toMatchObject({action:'reset',ids:['folder'],directory:'0'})
 expect(directories).toContain('folder');expect(directories).toContain('season');expect(directories.at(-1)).toBe('0')
 const count=await page.getByRole('status',{name:'当前目录数量',exact:true}).boundingBox(),refresh=await page.getByRole('button',{name:'刷新目录',exact:true}).boundingBox();expect(count.x+count.width).toBeLessThanOrEqual(refresh.x)
})
for(const [width,theme]of [[1440,'light'],[390,'dark']])test(`scrape settings are a dedicated final tab and preserve saves ${width}`,async({page},info)=>{
 const f=await fixture(page),writes=[]
 let cfg={writeMode:'missing',episodes:true,fanart:false,actors:false,excluded:'预告片'},reads=0,failSave=true
 await page.route('**/api/strm-scrape/settings',route=>{
  if(route.request().method()==='GET'){
   if(++reads===1)return route.fulfill({status:503,json:{error:'读取设置失败'}})
   return route.fulfill({json:cfg})
  }
  writes.push(route.request().postDataJSON())
  if(failSave){failSave=false;return route.fulfill({status:503,json:{error:'保存设置失败'}})}
  cfg={...writes.at(-1)};return route.fulfill({json:cfg})
 })
 await page.setViewportSize({width,height:950});await page.goto('/scrape/settings');await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
 const tabs=page.getByRole('navigation',{name:'刮削平台栏目'}),panel=page.getByRole('region',{name:'刮削设置',exact:true})
 await expect(tabs.getByRole('link').last()).toHaveText('刮削设置');await expect(tabs.getByRole('link').last()).toHaveClass(/active/)
 await expect(page.locator('.breadcrumbs strong')).toHaveText('刮削平台');await expect(page.locator('.breadcrumbs [aria-current="page"]')).toHaveText('刮削设置');await expect(panel.getByRole('alert')).toContainText('读取设置失败')
 await expect(panel.getByRole('button',{name:'保存设置',exact:true})).toBeDisabled()
 expect(f.requests.some(p=>p.startsWith('/api/strm-scrape/items')||p==='/api/strm-scrape/status')).toBe(false)
 await panel.getByRole('button',{name:'重新加载',exact:true}).click();await expect(panel.getByRole('textbox',{name:'排除目录',exact:true})).toHaveValue('预告片')
 await panel.getByRole('button',{name:'写入策略',exact:true}).click();await page.getByRole('option',{name:'覆盖已有',exact:true}).click()
 await panel.getByRole('switch',{name:'分集 NFO 与预览图',exact:true}).uncheck();await panel.getByRole('switch',{name:'背景图',exact:true}).check();await panel.getByRole('switch',{name:'演员信息',exact:true}).check()
 await panel.getByRole('textbox',{name:'排除目录',exact:true}).fill('预告片;花絮')
 await panel.getByRole('button',{name:'保存设置',exact:true}).click();await expect(panel.getByRole('alert')).toContainText('保存设置失败');await expect(panel.getByRole('textbox',{name:'排除目录',exact:true})).toHaveValue('预告片;花絮')
 await panel.getByRole('button',{name:'保存设置',exact:true}).click();await expect(page.locator('.toast-stack')).toContainText('刮削设置已保存')
 expect(writes).toEqual(Array(2).fill({writeMode:'overwrite',episodes:false,fanart:true,actors:true,excluded:'预告片;花絮'}))
 await page.reload();await expect(panel.getByRole('textbox',{name:'排除目录',exact:true})).toHaveValue('预告片;花絮');await expect(panel.getByRole('button',{name:'写入策略',exact:true})).toHaveText('覆盖已有');await expect(panel.getByRole('switch',{name:'演员信息',exact:true})).toBeChecked()
 await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
 for(const label of await panel.locator('.switch-label').all())await expect(label).toHaveCSS('flex-direction','row')
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
 await captureScreenshot(page,{path:info.outputPath(`scrape-settings-${width}-${theme}.png`)})
 for(const section of ['strm','library','directory']){await tabs.getByRole('link',{name:{strm:'STRM刮削',library:'链库刮削',directory:'目录刮削'}[section],exact:true}).click();await expect(page.getByRole('button',{name:'刮削设置',exact:true})).toHaveCount(0);await expect(panel).toHaveCount(0)}
})
for(const [width,theme]of [[1440,'light'],[390,'dark']])test(`directory scrape list and explicit write confirmation ${width}`,async({page},info)=>{
 const f=await fixture(page);await page.setViewportSize({width,height:1000});await page.goto('/scrape/directory');await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
 await expect(page.locator('.directory-scrape-row')).toHaveCount(3);expect(f.writes).toHaveLength(0)
 await expect(page.getByRole('button',{name:'海报墙视图',exact:true})).toHaveCount(0)
 await expect(page.locator('.directory-scrape-actions')).toHaveCount(0)
 await expect(page.getByRole('status',{name:'当前目录数量',exact:true})).toHaveText('3 项')
 const count=await page.getByRole('status',{name:'当前目录数量',exact:true}).boundingBox(),refresh=await page.getByRole('button',{name:'刷新目录',exact:true}).boundingBox()
 expect(count.x+count.width).toBeLessThanOrEqual(refresh.x);expect(Math.abs(count.y+count.height/2-refresh.y-refresh.height/2)).toBeLessThan(1)
 await page.locator('.directory-scrape-row').filter({has:page.getByRole('button',{name:'Arrival.2016.mkv',exact:true})}).hover()
 await expect(page.locator('.directory-scrape-row').filter({has:page.getByRole('button',{name:'Arrival.2016.mkv',exact:true})}).locator('.scrape-folder-actions')).toHaveCSS('opacity','1')
 await captureScreenshot(page,{path:info.outputPath(`directory-hover-${width}-${theme}.png`)})
 await page.getByRole('button',{name:'识别 Arrival.2016.mkv',exact:true}).click()
 await expect(page.getByRole('dialog',{name:'手动识别目录媒体'})).toBeVisible();expect(f.writes).toHaveLength(0)
 await page.getByRole('textbox',{name:'TMDB ID',exact:true}).fill('1')
 await page.getByRole('button',{name:'识别并刮削',exact:true}).click();await expect.poll(()=>f.writes.length).toBe(1);expect(f.writes[0]).toMatchObject({storageId:'pool',directory:'0',ids:['video'],action:'match',tmdb:1,group:false})
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
 await captureScreenshot(page,{path:info.outputPath(`directory-${width}-${theme}.png`)})
})
for(const [width,theme]of [[1440,'light'],[390,'dark']])test(`organize and scrape collapsed groups retain poster ${width}`,async({page},info)=>{
 await fixture(page);await page.setViewportSize({width,height:1000});await page.goto('/archives/organize');await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
 await expect(page.locator('.archive-organize-row')).toContainText('/待整理/蜗居.S01E33.mkv');await expect(page.locator('.archive-path-flow')).toContainText('/剧集/蜗居 (2009)')
 for(const section of ['organize','scrape']){
  if(section==='scrape')await page.getByRole('link',{name:'刮削档案',exact:true}).click()
  if(await page.getByRole('button',{name:'列表模式',exact:true}).count())await page.getByRole('button',{name:'列表模式',exact:true}).click()
  await expect(page.locator('.archive-group-toggle')).toHaveAttribute('aria-expanded','false');await expect(page.locator('.archive-group-toggle .archive-poster img')).toBeVisible()
  const poster=await page.locator('.archive-group-toggle .archive-poster').boundingBox();expect(poster.width).toBe(44);expect(poster.height).toBe(66)
  const order=await page.locator('.archive-toolbar').evaluate(el=>[...el.children].map(n=>n.getAttribute('aria-label')||n.className));expect(order.at(-1)).toBe('分组模式');expect(order.at(-2)).toContain('search-field');expect(order[0]).toContain('rounded-select')
  await captureScreenshot(page,{path:info.outputPath(`${section}-collapsed-${width}-${theme}.png`)})
 }
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
})

test('all library settings frame their panels and default scraping applies only to new cards',async({page},info)=>{
 await fixture(page)
 const settings=Object.fromEntries(['115','mobile','quark','cas'].map(p=>[p,{storageId:'pool',storageIds:['pool'],directory:'/media',updateDays:7,rotationHours:24,invalidDays:15,retentionHours:12,mediaExtensions:'mkv',metadataExtensions:'nfo',playbackMode:'share',scrapeEnabled:false}]))
 const saves=[]
 await page.route('**/api/virtual-library/**',route=>{const r=route.request(),parts=new URL(r.url()).pathname.split('/'),provider=parts[3];if(r.method()==='PUT'){settings[provider]={...settings[provider],...r.postDataJSON()};saves.push({provider,...r.postDataJSON()});return route.fulfill({json:settings[provider]})}return route.fulfill({json:{settings:settings[provider],cards:[{id:'old',name:'旧卡片',url:'https://example.test',enabled:false,directory:'/media',directoryName:provider+'旧卡',scrapeEnabled:false,cascadeCleanup:'off',result:{status:'idle'}}],stats:{}}})})
 for(const provider of ['115','mobile','quark','cas']){
  await page.goto(`/virtual/${provider}/settings`);const panels=page.locator('.quark-settings-form>section');await expect(panels).toHaveCount(2);for(const panel of await panels.all()){await expect(panel).toHaveCSS('border-top-width','1px');await expect(panel).toHaveCSS('border-radius','14px')}
  await page.getByRole('button',{name:'默认链库刮削',exact:true}).click();await page.getByRole('option',{name:'参与',exact:true}).click();await page.getByRole('button',{name:'保存设置',exact:true}).click();await expect.poll(()=>saves.some(s=>s.provider===provider&&s.scrapeEnabled)).toBe(true)
  if(provider==='mobile')await captureScreenshot(page,{path:info.outputPath('mobile-library-settings.png')})
  await page.goto(`/virtual/${provider}`);await page.getByRole('button',{name:'添加链接',exact:true}).click();let dialog=page.getByRole('dialog');await dialog.getByRole('button',{name:'更多选项',exact:true}).click();await expect(dialog.getByRole('button',{name:'链库刮削',exact:true})).toHaveText('参与');await expect(dialog.getByRole('textbox',{name:'级联删除扩展名',exact:true})).toBeVisible();if(provider==='115')await expect(dialog.getByRole('textbox',{name:'Cron表达式',exact:true})).toHaveCount(0);await dialog.getByRole('button',{name:'取消',exact:true}).click()
  await page.getByRole('button',{name:'编辑 旧卡片',exact:true}).click();dialog=page.getByRole('dialog');await dialog.getByRole('button',{name:'更多选项',exact:true}).click();await expect(dialog.getByRole('button',{name:'链库刮削',exact:true})).toHaveText('不参与');await dialog.getByRole('button',{name:'取消',exact:true}).click()
 }
})

test('storage pool labels use the full selector space before ellipsis',async({page},info)=>{
 await fixture(page);await page.route('**/api/cloud/manage**',route=>route.fulfill({json:{items:[],edit:true}}));await page.goto('/storage/downloads')
 const select=page.getByRole('button',{name:'选择云盘存储池',exact:true});await expect(select).toContainText('115-月见风信')
 const layout=await select.evaluate(el=>{const label=el.querySelector('.select-label'),text=label.lastElementChild,icon=label.firstElementChild,rect=label.getBoundingClientRect(),t=text.getBoundingClientRect(),i=icon.getBoundingClientRect();return {labelRight:rect.right,textRight:t.right,room:rect.width-i.width-parseFloat(getComputedStyle(label).gap),textWidth:t.width,trigger:el.getBoundingClientRect().width}})
 expect(layout.trigger).toBe(135);expect(Math.abs(layout.labelRight-layout.textRight)).toBeLessThan(2);expect(layout.textWidth).toBeGreaterThanOrEqual(layout.room-2)
 await captureScreenshot(page,{path:info.outputPath('storage-selector-label.png')})
})
