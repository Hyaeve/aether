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
 await expect(page.getByRole('navigation',{name:'刮削平台栏目'}).getByRole('link')).toHaveText(['STRM刮削','链库刮削','目录刮削'])
 await expect(page.getByRole('button',{name:'链库卡片',exact:true})).toContainText('115影视')
 for(const [provider,label]of [['mobile','移动链库'],['quark','夸克链库'],['cas','CAS链库']]){await page.getByRole('button',{name:'链库类型',exact:true}).click();await page.getByRole('option',{name:label,exact:true}).click();await expect(page.getByRole('button',{name:'链库卡片',exact:true})).toContainText(`${provider}影视`)}
 await page.getByRole('link',{name:'STRM刮削',exact:true}).click();await expect(page.getByRole('button',{name:'STRM 任务',exact:true})).toContainText('常规影视')
 await page.goto('/tasks/scrape');await expect(page).toHaveURL(/\/scrape\/strm$/)
})
for(const [width,theme]of [[1440,'light'],[390,'dark']])test(`directory scrape list and explicit write confirmation ${width}`,async({page},info)=>{
 const f=await fixture(page);await page.setViewportSize({width,height:1000});await page.goto('/scrape/directory');await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
 await expect(page.locator('.directory-scrape-row')).toHaveCount(3);expect(f.writes).toHaveLength(0)
 await expect(page.getByRole('button',{name:'海报墙视图',exact:true})).toHaveCount(0)
 await page.getByRole('checkbox',{name:'选择 Arrival.2016.mkv',exact:true}).check();await page.getByRole('button',{name:'刮削',exact:true}).click()
 await expect(page.getByRole('dialog',{name:'确认目录刮削'})).toContainText('NFO和海报');expect(f.writes).toHaveLength(0)
 await page.getByRole('button',{name:'确认刮削',exact:true}).click();await expect.poll(()=>f.writes.length).toBe(1);expect(f.writes[0]).toMatchObject({storageId:'pool',directory:'0',ids:['video'],action:'run'})
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
  await page.goto(`/virtual/${provider}`);await page.getByRole('button',{name:'添加链接',exact:true}).click();let dialog=page.getByRole('dialog');await dialog.getByRole('button',{name:'更多选项',exact:true}).click();await expect(dialog.getByRole('button',{name:'链库刮削',exact:true})).toHaveText('参与');await expect(dialog.getByRole('button',{name:'级联清理',exact:true})).toBeVisible();if(provider==='115')await expect(dialog.getByRole('textbox',{name:'Cron表达式',exact:true})).toHaveCount(0);await dialog.getByRole('button',{name:'取消',exact:true}).click()
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
