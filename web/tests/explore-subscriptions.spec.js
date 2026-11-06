import {test,expect} from '@playwright/test'
import {captureScreenshot} from './helpers/screenshot.js'

test.use({browserName:process.env.AETHER_EXPLORE_BROWSER||'chromium'})
const media=(kind,id=42)=>({id,kind,title:kind==='tv'?'星河旅人':'远方来信',originalTitle:'A Journey',year:'2026',overview:'穿越星河，寻找久别的故乡。',poster:'https://image.tmdb.org/t/p/w500/example.jpg',backdrop:'https://image.tmdb.org/t/p/w1280/example.jpg',score:8.6,genres:['科幻','冒险'],runtime:120,status:'Released',language:'zh',countries:['中国'],companies:['示例制作'],cast:['示例演员'],seasons:kind==='tv'?[{number:1,episodes:12,name:'第一季'}]:[]})
async function fixture(page,{many=false,unconfigured=false}={}){
 const writes=[],unknown=[],reads=[]
 let fail=false,cards=many?Array.from({length:21},(_,i)=>({...media('tv',i+1),key:'card-'+i,season:1,totalEpisodes:12,startEpisode:1})):[]
 await page.route('https://image.tmdb.org/**',route=>route.fulfill({contentType:'image/svg+xml',body:'<svg xmlns="http://www.w3.org/2000/svg" width="600" height="900"><defs><linearGradient id="g" x2="1" y2="1"><stop stop-color="#324773"/><stop offset="1" stop-color="#8d526d"/></linearGradient></defs><path fill="url(#g)" d="M0 0h600v900H0z"/><circle cx="340" cy="300" r="180" fill="#e1ddc7" opacity=".3"/><text x="65" y="640" fill="white" font-size="45">AETHER</text></svg>'}))
 await page.route('**/api/**',route=>{
  const r=route.request(),u=new URL(r.url()),p=u.pathname;reads.push(p);let json={}
  if(p==='/api/auth/status')json={initialized:true,authenticated:true}
  else if(p==='/api/state')json={username:'explore',storages:[],tasks:[],settings:{},traffic:{},cache:{}}
  else if(['/api/logs','/api/115-recycle-cleanup','/api/strm-replace/jobs','/api/strm-replace'].includes(p))json=[]
  else if(p==='/api/traffic')json={uploadRate:0,downloadRate:0}
  else if(p==='/api/discovery'){
   if(unconfigured)return route.fulfill({status:502,json:{error:'请先在以太识律的辅助配置中配置并启用TMDB'}})
   const c=u.searchParams.get('category'),q=u.searchParams.get('q'),group=u.searchParams.get('group'),pg=Number(u.searchParams.get('page'))||1
   const groups=c==='all'?[['trending','流行趋势'],['playing','正在热映'],['airing','正在播出']]:c==='anime'?[['anime-tv','热门动漫剧集'],['anime-movie','热门动画电影']]:c==='rankings'?[['top-movie','电影高分榜'],['top-tv','剧集高分榜']]:[[c==='tv'?'popular-tv':'playing',c==='tv'?'热门剧集':'正在热映']]
   json={sections:(q?[['search','搜索结果']]:group?groups.filter(g=>g[0]===group):groups).map(([id,title])=>({id,title,items:q&&q.includes('没有')?[]:[media(c==='movie'?'movie':'tv',pg===1?42:43),media('movie',44)],page:pg,pages:3}))}
  }else if(/^\/api\/discovery\/(movie|tv)\/\d+$/.test(p))json=media(p.split('/')[3],Number(p.split('/')[4]))
  else if(p==='/api/media-subscriptions'&&r.method()==='GET')json={items:cards,targets:[{id:'mobile',provider:'mobile',name:'移动影视',path:'/家庭影音/剧集'},{id:'115',provider:'115',name:'115影视',path:'/电影'}],sources:[],sourceReady:false}
  else if(p.startsWith('/api/media-subscriptions')){
   if(r.method()==='DELETE'){cards=cards.filter(c=>c.key!==p.split('/').at(-1));json={ok:true}}
   else{
    const input=r.postDataJSON();writes.push({method:r.method(),input})
    if(fail){fail=false;return route.fulfill({status:503,json:{error:'保存订阅失败，请重试'}})}
    const prior=cards.find(c=>c.key===p.split('/').at(-1)),card={...media(input.kind),...prior,...input,key:prior?.key||'saved-'+cards.length}
    if(prior)cards=cards.map(c=>c.key===prior.key?card:c);else cards.push(card)
    json=card
   }
  }else{unknown.push(p);return route.fulfill({status:404,json:{error:'未模拟接口 '+p}})}
  return route.fulfill({json})
 })
 return {writes,unknown,reads,cards:()=>cards,fail:()=>fail=true}
}

for(const [width,theme]of [[1440,'light'],[1440,'dark'],[390,'light'],[390,'dark']])test(`recommendation shelves details and subscription editing ${width} ${theme}`,async({page},info)=>{
 const f=await fixture(page);await page.setViewportSize({width,height:1000});await page.goto('/discover');await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
 await expect(page).toHaveURL(/\/discover\/all$/)
 const tabs=page.getByRole('navigation',{name:'资源推荐栏目'})
 await expect(tabs.getByRole('link')).toHaveText(['全部','剧集','电影','动漫','榜单']);await expect(tabs.locator('svg')).toHaveCount(5)
 await expect(page.getByRole('region',{name:'流行趋势'})).toBeVisible()
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
 await captureScreenshot(page,{path:info.outputPath(`recommendations-${width}-${theme}.png`)})
 await page.getByRole('button',{name:'星河旅人 详情',exact:true}).first().click()
 await expect(page.locator('.breadcrumbs [aria-current=page]')).toHaveText('媒体详情')
 await expect(page.getByRole('heading',{name:'星河旅人 (2026)',exact:true})).toBeVisible()
 await expect(page.locator('.discovery-facts')).toContainText('42')
 await captureScreenshot(page,{path:info.outputPath(`detail-${width}-${theme}.png`)})
 await page.getByRole('button',{name:'添加订阅',exact:true}).click()
 const dialog=page.getByRole('dialog',{name:'添加订阅',exact:true})
 expect((await dialog.boundingBox()).width).toBe(width===1440?820:width-32)
 await expect(dialog.getByRole('tab')).toHaveText(['基础','进阶'])
 await expect(dialog.getByRole('button',{name:'订阅站点',exact:true})).toBeDisabled()
 await expect(dialog.getByRole('spinbutton',{name:'总集数',exact:true})).toHaveValue('12')
 await dialog.getByRole('textbox',{name:'搜索关键词',exact:true}).fill('星河旅人 精选')
 for(const [label,value]of [['质量','WEB-DL'],['分辨率','4K'],['特效','HDR10'],['转存目录','移动影视 · /家庭影音/剧集'],['搜索周期','自定义']]){await dialog.getByRole('button',{name:label,exact:true}).click();await page.getByRole('option',{name:value,exact:true}).click()}
 await dialog.getByRole('spinbutton',{name:'间隔小时',exact:true}).fill('48')
 expect(await dialog.getByRole('spinbutton',{name:'间隔小时',exact:true}).evaluate(el=>Math.abs(el.closest('.number-control').getBoundingClientRect().width-el.closest('label').getBoundingClientRect().width))).toBeLessThan(1)
 await expect(dialog.getByRole('button',{name:'保存订阅',exact:true})).toBeInViewport();expect(await dialog.evaluate(el=>el.scrollWidth<=el.clientWidth)).toBe(true)
 await captureScreenshot(page,{path:info.outputPath(`subscription-basic-${width}-${theme}.png`)})
 await dialog.getByRole('tab',{name:'进阶',exact:true}).click()
 for(const [name,value]of [['包含','2160p'],['排除','预告'],['媒体分类','科幻剧集'],['自定义识别词','原名 => 识别名']])await dialog.getByRole('textbox',{name,exact:true}).fill(value)
 await expect(dialog).not.toContainText('优先规则组');await expect(dialog).not.toContainText('指定剧集组');await expect(dialog.getByRole('switch')).toHaveCount(0)
 await captureScreenshot(page,{path:info.outputPath(`subscription-advanced-${width}-${theme}.png`)})
 f.fail();await dialog.getByRole('button',{name:'保存订阅',exact:true}).click();await expect(dialog.getByRole('alert')).toContainText('保存订阅失败');await expect(dialog.getByRole('textbox',{name:'包含',exact:true})).toHaveValue('2160p')
 await dialog.getByRole('button',{name:'保存订阅',exact:true}).click();await expect(dialog).toHaveCount(0)
 expect(f.writes).toHaveLength(2);expect(f.writes[1].input).toEqual({id:42,kind:'tv',season:1,keyword:'星河旅人 精选',totalEpisodes:12,startEpisode:1,quality:'WEB-DL',resolution:'4K',effect:'HDR10',sourceIds:[],targetId:'mobile',searchHours:48,include:'2160p',exclude:'预告',mediaCategory:'科幻剧集',customWords:'原名 => 识别名'})
 await page.goto('/subscriptions');await expect(page.locator('.subscription-card')).toHaveCount(1);await expect(page.locator('.subscription-card')).toContainText('0 / 12');await expect(page.locator('.subscription-card')).toContainText('待接入资源来源')
 await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
 await captureScreenshot(page,{path:info.outputPath(`subscriptions-${width}-${theme}.png`)})
 await page.getByRole('button',{name:'编辑订阅 星河旅人',exact:true}).click();const edit=page.getByRole('dialog',{name:'编辑订阅',exact:true});await expect(edit.getByRole('textbox',{name:'搜索关键词'})).toHaveValue('星河旅人 精选');await edit.getByRole('tab',{name:'进阶'}).click();await expect(edit.getByRole('textbox',{name:'包含',exact:true})).toHaveValue('2160p');await edit.getByRole('button',{name:'取消',exact:true}).click()
 expect(f.unknown).toEqual([])
})

for(const width of [375,844])test(`fixed subscription menus follow modal scroll and hand off without blocking ${width}`,async({page},info)=>{
 const f=await fixture(page);await page.setViewportSize({width,height:width===844?500:844});await page.goto('/discover/tv/42')
 await page.getByRole('button',{name:'添加订阅',exact:true}).click()
 const dialog=page.getByRole('dialog',{name:'添加订阅',exact:true}),body=dialog.locator('.modal-body'),target=dialog.getByRole('button',{name:'转存目录',exact:true})
 await target.click()
 const menu=page.getByRole('listbox',{name:'转存目录选项',exact:true})
 await expect(menu).toBeVisible();await expect(menu).toHaveCSS('opacity','1')
 const before=await body.evaluate(el=>el.scrollTop);expect(before).toBeGreaterThan(0)
 await body.evaluate(el=>el.scrollBy({top:-18,behavior:'instant'}))
 await expect.poll(()=>body.evaluate(el=>el.scrollTop)).toBeLessThan(before)
 await expect(menu).toBeVisible();await expect(target).toHaveAttribute('aria-expanded','true')
 await expect.poll(async()=>{const [a,b]=await Promise.all([target.boundingBox(),menu.boundingBox()]);return Math.abs(a.x-b.x)<1&&Math.abs(a.width-b.width)<1&&Math.min(Math.abs(b.y-a.y-a.height-6),Math.abs(b.y+b.height-a.y+6))<1}).toBe(true)
 await menu.press('Escape');await expect(menu).toHaveCount(0);await expect(target).toBeFocused();await expect(dialog).toBeVisible()
 // Keep an exiting menu in the DOM to verify that it cannot block the next control.
 await page.addStyleTag({content:'.select-popup-leave-active{transition-duration:10s!important}'})
 await target.click();await expect(menu).toHaveCSS('opacity','1')
 await menu.getByRole('option',{name:'移动影视 · /家庭影音/剧集',exact:true}).click()
 const leaving=page.locator('[role="listbox"][aria-label="转存目录选项"]')
 await expect(leaving).toHaveAttribute('inert','');await expect(leaving).toHaveAttribute('aria-hidden','true');await expect(leaving).toHaveCSS('pointer-events','none');await expect(menu).toHaveCount(0)
 const period=dialog.getByRole('button',{name:'搜索周期',exact:true});await period.click()
 const periodMenu=page.getByRole('listbox',{name:'搜索周期选项',exact:true})
 await periodMenu.getByRole('option',{name:'自定义',exact:true}).click();await expect(dialog.getByRole('spinbutton',{name:'间隔小时',exact:true})).toHaveValue('24')
 await period.focus();await period.press('ArrowDown');await periodMenu.press('Home');await periodMenu.press('Enter');await expect(period).toContainText('跟随系统');await expect(dialog.getByRole('spinbutton',{name:'间隔小时',exact:true})).toHaveCount(0)
 await period.click();await expect(periodMenu).toBeVisible();await dialog.getByRole('tab',{name:'基础',exact:true}).click();await expect(periodMenu).toHaveCount(0)
 expect(f.writes).toEqual([]);expect(f.unknown).toEqual([])
 await captureScreenshot(page,{path:info.outputPath(`menu-handoff-${width}.png`)})
})

test('discovery categories search paging and configuration error',async({page})=>{
 const f=await fixture(page);await page.goto('/discover')
 for(const name of ['剧集','电影','动漫','榜单','全部']){await page.getByRole('navigation',{name:'资源推荐栏目'}).getByRole('link',{name,exact:true}).click();await expect(page.locator('.recommendation-section')).not.toHaveCount(0)}
 await page.getByRole('button',{name:'更多',exact:true}).first().click();await expect(page.locator('.recommendation-grid')).toHaveCount(1)
 const jump=page.getByRole('spinbutton',{name:'推荐跳转页码'});await jump.fill('999');await jump.press('Enter');await expect(jump).toHaveValue('3');await jump.fill('0');await jump.press('Enter');await expect(jump).toHaveValue('1')
 const search=page.getByRole('textbox',{name:'搜索推荐媒体'});await search.fill('没有此片');await search.press('Enter');await expect(page.getByRole('region',{name:'搜索结果'})).toContainText('暂无匹配的媒体')
 expect(f.unknown).toEqual([])
 await page.unroute('**/api/**');await fixture(page,{unconfigured:true});await page.reload();await expect(page.getByRole('alert')).toContainText('配置并启用TMDB');await expect(page.getByRole('link',{name:'TMDB配置',exact:true})).toHaveAttribute('href','/rules/assist');await expect(page.locator('.recommendation-poster')).toHaveCount(0)
})

test('movie subscriptions existing card reuse and confirmed cancellation',async({page})=>{
 const f=await fixture(page);await page.goto('/discover/movie/42')
 await page.getByRole('button',{name:'添加订阅',exact:true}).click();let dialog=page.getByRole('dialog',{name:'添加订阅',exact:true});await expect(dialog.getByRole('spinbutton',{name:'总集数',exact:true})).toHaveCount(0);await dialog.getByRole('button',{name:'保存订阅',exact:true}).click();await expect(dialog).toHaveCount(0)
 await page.getByRole('button',{name:'添加订阅',exact:true}).click();dialog=page.getByRole('dialog',{name:'编辑订阅',exact:true});await expect(dialog).toBeVisible();await dialog.getByRole('button',{name:'保存订阅',exact:true}).click();await expect(dialog).toHaveCount(0)
 expect(f.writes.map(w=>w.method)).toEqual(['POST','PUT']);expect(f.cards()).toHaveLength(1)
 await page.goto('/subscriptions/movie');await expect(page.locator('.subscription-card')).toHaveCount(1)
 await page.getByRole('button',{name:'取消订阅 远方来信',exact:true}).click();const confirm=page.getByRole('dialog',{name:'取消订阅',exact:true});await expect(confirm).toContainText('已转存的文件不受影响');expect(f.cards()).toHaveLength(1);await confirm.getByRole('button',{name:'返回',exact:true}).click();expect(f.cards()).toHaveLength(1)
 await page.getByRole('button',{name:'取消订阅 远方来信',exact:true}).click();await confirm.getByRole('button',{name:'确认取消',exact:true}).click();await expect(page.locator('.subscription-card')).toHaveCount(0)
 await page.getByRole('navigation',{name:'媒体订阅栏目'}).getByRole('link',{name:'资源来源',exact:true}).click();await expect(page.getByRole('region',{name:'资源来源',exact:true})).toContainText('待开放');expect(f.unknown).toEqual([])
})

test('subscription cards paginate twenty and clamp page jumps',async({page})=>{
 const f=await fixture(page,{many:true});await page.goto('/subscriptions/tv');await expect(page.locator('.subscription-card')).toHaveCount(20)
 const jump=page.getByRole('spinbutton',{name:'订阅跳转页码'});await jump.fill('99');await jump.press('Enter');await expect(jump).toHaveValue('2');await expect(page.locator('.subscription-card')).toHaveCount(1);await jump.fill('0');await jump.press('Enter');await expect(jump).toHaveValue('1');await expect(page.locator('.subscription-card')).toHaveCount(20)
 await page.getByRole('navigation',{name:'媒体订阅栏目'}).getByRole('link',{name:'电影订阅',exact:true}).click();await expect(page.locator('.subscription-card')).toHaveCount(0);await expect(page.getByRole('spinbutton',{name:'订阅跳转页码'})).toHaveCount(0);expect(f.unknown).toEqual([])
})
