import {test,expect} from '@playwright/test'
import {readFileSync} from 'node:fs'
import {captureScreenshot} from './helpers/screenshot.js'
test.use({browserName:process.env.AETHER_MEDIA_BROWSER||'chromium'})
const version=readFileSync(new URL('../../VERSION',import.meta.url),'utf8').trim()
const svg='<svg xmlns="http://www.w3.org/2000/svg" width="900" height="600"><defs><linearGradient id="g" x2="1" y2="1"><stop stop-color="#324c72"/><stop offset="1" stop-color="#78617f"/></linearGradient></defs><path fill="url(#g)" d="M0 0h900v600H0z"/><circle cx="640" cy="180" r="150" fill="#d8dec9" opacity=".4"/><text x="60" y="460" fill="white" font-size="55">星河旅人 AETHER</text></svg>'
const poster=(i,audio=false)=>({id:'item'+i,title:(audio?'书籍':'星河旅人')+i,subtitle:audio?'作者 · 播讲':'2026 · 剧集',overview:'穿过漫长星河，寻找故乡。',poster:'/api/test-image/'+i,backdrop:'/api/test-image/backdrop'+i})
async function fixture(page,{login=false,many=false}={}){
 const writes=[],unknown=[],reads=[]
 let authenticated=!login,fail=false,noteVersion=version
 const items=[{id:'emby',name:'家庭影视',type:'emby',enabled:false,address:'http://media.test:8096',libraries:[{id:'lib',name:'电影剧集'}]},{id:'abs',name:'有声书架',type:'audiobookshelf',enabled:false,address:'http://books.test',libraries:[{id:'books',name:'有声书'}]},{id:'fn',name:'飞牛影视',type:'fnos',enabled:true,address:'http://fn.test',libraries:[{id:'lib',name:'影视'}]}]
 const config={selected:'emby',missingSelected:'emby',items}
 const status={progress:{running:false},missing:{updated:'2026-11-05T00:00:00Z',scanned:21,unknown:1,items:Array.from({length:many?21:2},(_,i)=>({item:poster(i),localEpisodes:10,expectedEpisodes:12,missing:['S01E02','S01E04']}))},quality:{updated:'2026-11-05T00:00:00Z',items:20,size:1073741824,sizeKnown:19,resolutions:{'4K':15,'1080P':5},codecs:{HEVC:15,AVC:5}}}
 await page.route('**/api/**',async route=>{
  const r=route.request(),u=new URL(r.url()),p=u.pathname,m=r.method();reads.push(m+' '+p);let json={}
  if(p.startsWith('/api/test-image/'))return route.fulfill({contentType:'image/svg+xml',body:svg})
  if(p==='/api/auth/status')json={initialized:true,authenticated}
  else if(p==='/api/auth/login'){authenticated=true;json={ok:true}}
  else if(p==='/api/auth/logout'){authenticated=false;json={ok:true}}
  else if(p==='/api/state')json={username:'media-test',storages:[],tasks:[],links:[],settings:{},traffic:{},cache:{}}
  else if(p==='/api/version/notes')json={version:noteVersion,body:'# 更新日志\n- 新增追剧日历\n- 我的媒体展示'}
  else if(['/api/logs','/api/115-recycle-cleanup','/api/strm-replace/jobs'].includes(p))json=[]
  else if(p==='/api/strm-replace')json={tasks:[]}
  else if(p==='/api/traffic')json={}
  else if(p==='/api/tasks/panel')json={items:[]}
  else if(p.startsWith('/api/archives/'))json={items:[],total:0,page:1,pages:1}
  else if(p==='/api/strm-scrape/status')json={running:false}
  else if(p==='/api/my-media/config'){
   if(m==='PUT'){const input=r.postDataJSON();writes.push(input);if(fail){fail=false;return route.fulfill({status:503,json:{error:'媒体配置保存失败'}})}if(input.selected)config.selected=input.selected;if(input.missingSelected)config.missingSelected=input.missingSelected;if(input.linkId)items.find(v=>v.id===input.linkId).libraries=input.libraries}
   json=config
  }else if(/^\/api\/my-media\/[^/]+\/libraries$/.test(p))json=[{id:'lib',name:'电影剧集'},{id:'private',name:'私藏'}]
  else if(p.endsWith('/dashboard')){const audio=p.includes('/abs/');json={recent:Array.from({length:7},(_,i)=>poster(i,audio)),stats:audio?[{label:'音频条目',value:30},{label:'总时长',value:12.5,unit:'h'}]:[{label:'影片',value:40},{label:'剧集',value:20},{label:'总集数',value:200}],warnings:[]}}
  else if(p.endsWith('/sessions'))json=[{id:'s1',user:'用户甲',item:poster(0,p.includes('/abs/')),position:1800,duration:3600,paused:false}]
  else if(p.endsWith('/ranking'))json={items:Array.from({length:many?21:2},(_,i)=>({item:poster(i,p.includes('/abs/')),value:30-i})),unit:p.includes('/abs/')?'h':'次'}
  else if(p.endsWith('/scan')){if(m==='POST'){writes.push(r.postDataJSON());status.progress={running:true,processed:2,total:21,message:'正在核对剧集'}}json=status}
  else if(p==='/api/media-calendar'){const month=u.searchParams.get('month');json={items:[{id:'cal1',title:'星河旅人',date:month+'-05',poster:'/api/test-image/1',tmdb:42,season:1,episode:2}],warnings:[]}}
  else {unknown.push(m+' '+p);return route.fulfill({status:404,json:{error:'未模拟 '+p}})}
  return route.fulfill({json})
 })
 return{writes,unknown,reads,config,fail:()=>fail=true,note:v=>noteVersion=v,finish:()=>status.progress={running:false,message:'扫描完成'}}
}
for(const[width,theme]of[[1440,'light'],[1440,'dark'],[390,'light'],[390,'dark']])test(`media dashboard library selection and calendar ${width} ${theme}`,async({page},info)=>{
 const f=await fixture(page);await page.setViewportSize({width,height:1000});await page.goto('/my-media');await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
 await expect(page.getByRole('navigation',{name:'我的媒体栏目'}).getByRole('link')).toHaveText(['媒体面板','内容排行','缺集管理','控制中心'])
 const hero=page.getByRole('region',{name:'推荐媒体'}),indicators=hero.getByRole('button',{name:/展示海报/})
 await expect(indicators).toHaveCount(5);await indicators.nth(3).hover();await expect(hero.getByRole('heading',{level:1})).toHaveText('星河旅人3');await expect(indicators.nth(3)).toHaveAttribute('aria-pressed','true')
 const geometry=await hero.evaluate(el=>{const p=el.getBoundingClientRect(),b=el.querySelector('.media-feature-indicators').getBoundingClientRect();return{right:p.right-b.right,bottom:p.bottom-b.bottom}});expect(geometry.right).toBeLessThan(35);expect(geometry.bottom).toBeLessThan(30)
 await expect(page.getByRole('progressbar',{name:'用户甲播放进度'})).toHaveAttribute('value','50');await expect(page.locator('.my-media-poster')).toHaveCount(7)
 expect(f.writes).toEqual([]);expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
 await captureScreenshot(page,{path:info.outputPath(`media-panel-${width}-${theme}.png`)})
 await page.getByRole('button',{name:'媒体服务',exact:true}).click();await expect(page.getByRole('option')).toHaveText(['家庭影视','有声书架','飞牛影视']);await page.getByRole('option',{name:'有声书架',exact:true}).click()
 await expect(page.locator('.audio-feature')).toContainText('书籍0');await expect(page.locator('.media-stat-grid').first()).toContainText('12.5');expect(f.writes).toEqual([{selected:'abs'}])
 await page.reload();await expect(page.getByRole('button',{name:'媒体服务',exact:true})).toContainText('有声书架');await expect(page.locator('.audio-feature')).toBeVisible();await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
 await captureScreenshot(page,{path:info.outputPath(`audio-panel-${width}-${theme}.png`)})
 await page.goto('/calendar');await page.evaluate(t=>document.documentElement.dataset.theme=t,theme);await expect(page.locator('.calendar-week span')).toHaveText(['周一','周二','周三','周四','周五','周六','周日']);const cells=await page.locator('.calendar-grid button').count();expect(cells).toBeGreaterThanOrEqual(28);expect(cells).toBeLessThanOrEqual(42);expect(cells%7).toBe(0)
 await page.getByRole('button',{name:/^\d{4}-\d{2}-05 1集$/}).click();await expect(page.locator('.calendar-agenda')).toContainText('S01E02');const before=await page.locator('.media-calendar header strong').textContent();await page.getByRole('button',{name:'下个月',exact:true}).click();await expect(page.locator('.media-calendar header strong')).not.toHaveText(before)
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);await captureScreenshot(page,{path:info.outputPath(`calendar-${width}-${theme}.png`)});expect(f.unknown).toEqual([])
})
test('control save failure preserves libraries, missing and ranking paginate and scans require a click',async({page},info)=>{
 const f=await fixture(page,{many:true});await page.goto('/my-media/control');await expect(page.locator('.media-display-row')).toHaveCount(3)
 await page.getByRole('button',{name:'编辑媒体展示 家庭影视'}).click();const d=page.getByRole('dialog',{name:'编辑媒体展示'});await expect(d.getByRole('checkbox',{name:'电影剧集'})).toBeChecked();await d.getByRole('checkbox',{name:'私藏'}).check();f.fail();await d.getByRole('button',{name:'保存',exact:true}).click();await expect(d.getByRole('alert')).toContainText('保存失败');await expect(d.getByRole('checkbox',{name:'私藏'})).toBeChecked();await d.getByRole('button',{name:'保存',exact:true}).click();await expect(d).toHaveCount(0)
 expect(f.writes.at(-1)).toEqual({linkId:'emby',libraries:[{id:'lib',name:'电影剧集'},{id:'private',name:'私藏'}]})
 await page.getByRole('link',{name:'缺集管理',exact:true}).click();await expect(page.locator('.missing-poster')).toHaveCount(20);expect(f.writes.filter(w=>w.kind)).toEqual([])
 const jump=page.getByRole('spinbutton',{name:'媒体跳转页码'});await jump.fill('99');await jump.press('Enter');await expect(jump).toHaveValue('2');await expect(page.locator('.missing-poster')).toHaveCount(1);await jump.fill('0');await jump.press('Enter');await expect(jump).toHaveValue('1')
 await page.locator('.missing-poster').first().hover();await expect(page.locator('.missing-hover').first()).toHaveCSS('opacity','1');await expect(page.locator('.missing-hover').first()).toContainText('S01E02');await captureScreenshot(page,{path:info.outputPath('missing-panel.png')})
 await page.getByRole('textbox',{name:'搜索缺集作品'}).fill('星河旅人20');await expect(page.locator('.missing-poster')).toHaveCount(1);await expect(jump).toHaveCount(0)
 await page.getByRole('button',{name:'缺集扫描',exact:true}).click();await expect(page.getByRole('button',{name:'缺集扫描',exact:true})).toBeDisabled();expect(f.writes.at(-1)).toEqual({kind:'missing'});f.finish()
 await page.getByRole('link',{name:'内容排行',exact:true}).click();await expect(page.locator('.media-rank-grid>button')).toHaveCount(20);await page.getByRole('button',{name:'下一页',exact:true}).click();await expect(page.locator('.media-rank-grid>button')).toHaveCount(1);await expect(page.locator('.media-rank-grid>button')).toContainText('#21');expect(f.unknown).toEqual([])
})
test('successful login and relogin land on overview',async({page})=>{
 const f=await fixture(page,{login:true});await page.goto('/my-media/panel');for(let i=0;i<2;i++){
  await page.getByRole('textbox',{name:'账号',exact:true}).fill('admin');await page.getByRole('textbox',{name:'密码',exact:true}).fill('long-password');await page.getByRole('button',{name:'登录工作空间',exact:true}).click();await expect(page).toHaveURL(/\/dashboard$/);await expect(page.locator('.breadcrumbs [aria-current=page]')).toHaveText('信息总览')
  if(i===0){await page.goto('/my-media/ranking');await page.getByRole('button',{name:'账号菜单',exact:true}).click();await page.getByRole('button',{name:'退出登录',exact:true}).click()}
 }expect(f.unknown).toEqual([])
})
test('switching service loads its scan while an old service response is pending',async({page})=>{
 const f=await fixture(page);let release
 await page.route('**/api/my-media/emby/scan',async route=>{await new Promise(resolve=>release=resolve);await route.fulfill({json:{progress:{},quality:{items:7,updated:'2026-11-05T00:00:00Z'},missing:{items:[]}}})})
 await page.route('**/api/my-media/fn/scan',route=>route.fulfill({json:{progress:{},quality:{items:321,updated:'2026-11-05T00:00:00Z',resolutions:{'4K':321},codecs:{}},missing:{items:[]}}}))
 try{await page.goto('/my-media/panel');await expect.poll(()=>!!release).toBe(true);await page.getByRole('button',{name:'媒体服务',exact:true}).click();await page.getByRole('option',{name:'飞牛影视',exact:true}).click();await expect(page.locator('.media-resolution-ring')).toContainText('321');release();await expect(page.locator('.media-resolution-ring')).toContainText('321');expect(f.unknown).toEqual([])}finally{release?.()}
})
for(const method of ['知道了','关闭','Escape','outside'])test(`release notice once per browser version closes via ${method}`,async({page})=>{
 const f=await fixture(page);await page.addInitScript(()=>{if(!sessionStorage.getItem('notice-reset')){localStorage.removeItem('aether-release-seen');sessionStorage.setItem('notice-reset','1')}});await page.goto('/my-media/panel')
 const modal=page.getByRole('dialog',{name:`更新日志 · v${version}`,exact:true});await expect(modal).toContainText('新增追剧日历')
 if(method==='Escape')await page.keyboard.press('Escape');else if(method==='outside')await page.locator('.modal-backdrop').click({position:{x:3,y:3}});else await modal.getByRole('button',{name:method,exact:true}).click()
 await expect(modal).toHaveCount(0);expect(await page.evaluate(()=>localStorage.getItem('aether-release-seen'))).toBe(version);await page.reload();await expect(page.locator('.media-feature')).toBeVisible();await expect(modal).toHaveCount(0)
 f.note('99.0.0');await page.evaluate(()=>localStorage.setItem('aether-release-seen','old'));await page.reload();await expect(page.getByRole('dialog',{name:'更新日志 · v99.0.0'})).toBeVisible();expect(f.unknown).toEqual([])
})
for(const width of [1440,390])test(`empty archives and task panel use the standard bottom gap ${width}`,async({page},info)=>{
 const f=await fixture(page);await page.setViewportSize({width,height:1000})
 for(const[path,selector]of[['/archives/organize','.archive-panel'],['/archives/scrape','.archive-panel'],['/archives/tasks','.archive-panel'],['/tasks/panel','.task-panel-empty']]){
  await page.goto(path);const panel=page.locator(selector);await expect(panel).toBeVisible();await expect.poll(()=>panel.evaluate(el=>{const b=el.getBoundingClientRect();return Math.abs(innerHeight-b.bottom-parseFloat(getComputedStyle(document.documentElement).getPropertyValue('--workspace-end-gap')))})).toBeLessThan(2)
 }
 await captureScreenshot(page,{path:info.outputPath(`empty-task-panel-${width}.png`)});expect(f.unknown).toEqual([])
})
