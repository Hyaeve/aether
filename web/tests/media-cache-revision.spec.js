import {test,expect} from '@playwright/test'
import {captureScreenshot} from './helpers/screenshot.js'
test.use({browserName:process.env.AETHER_MEDIA_BROWSER||'chromium'})
const artwork=(id=42,kind='tv')=>({id,kind,title:'星河旅人'+id,originalTitle:'Journey',year:'2026',release:'2026-01-02',overview:'穿过漫长星河，寻找遥远故乡。这里是完整作品简介，供预览和详情展示。',poster:'/api/test-poster/'+id,backdrop:'/api/test-poster/backdrop',score:8.6,genres:['科幻','冒险'],runtime:50,status:'Returning Series',language:'zh',countries:['中国'],companies:['示例公司'],seasons:[{number:1,episodes:12},{number:2,episodes:10}],people:[{name:'演员甲',role:'角色甲',photo:'/api/test-poster/person'}],crew:[{name:'导演甲',role:'Director'}],recommendations:[{id:43,kind:'tv',title:'推荐作品',year:'2026',score:8,poster:'/api/test-poster/43'}],similar:[{id:44,kind:'movie',title:'相似作品',score:7,poster:'/api/test-poster/44'}]})
async function fixture(page){
 const writes=[],unknown=[],reads=[]
 const poster={id:'show',title:'星河旅人42',subtitle:'2026',overview:artwork().overview,kind:'Series',poster:'/api/test-poster/42',backdrop:'/api/test-poster/backdrop',episodes:23,tmdb:42,libraryId:'lib',libraryName:'家庭剧集',path:'/media/剧集/星河旅人 (2026)',webURL:'http://emby.test:8096/web/index.html#!/item?id=show'}
 const config={selected:'emby',missingSelected:'emby',items:[{id:'emby',type:'emby',name:'家庭Emby',address:'http://emby.test:8096',libraries:[{id:'lib',name:'家庭剧集'}]}]}
 await page.route('**/api/**',route=>{
  const r=route.request(),u=new URL(r.url()),p=u.pathname;reads.push(p);let json={}
  if(p.startsWith('/api/test-poster/'))return route.fulfill({contentType:'image/svg+xml',body:'<svg xmlns="http://www.w3.org/2000/svg" width="600" height="900"><defs><linearGradient id="g"><stop stop-color="#304b6e"/><stop offset="1" stop-color="#806079"/></linearGradient></defs><path fill="url(#g)" d="M0 0h600v900H0z"/><circle cx="300" cy="320" r="160" fill="#d4decc" opacity=".4"/><text x="70" y="650" fill="white" font-size="42">AETHER</text></svg>'})
  if(p==='/api/missing-poster')return route.fulfill({status:404,body:''})
  if(p==='/api/auth/status')json={initialized:true,authenticated:true}
  else if(p==='/api/state')json={username:'media-revision',storages:[],tasks:[],settings:{},traffic:{},cache:{}}
  else if(['/api/logs','/api/115-recycle-cleanup','/api/strm-replace/jobs','/api/generation/jobs'].includes(p))json=[]
  else if(p==='/api/strm-replace')json={tasks:[]}
  else if(p==='/api/traffic')json={}
  else if(p==='/api/strm-scrape/status')json={running:false}
  else if(p==='/api/discovery')json={sections:[{id:'trending',title:'流行趋势',items:[artwork(),artwork(45,'movie')],pages:1,page:1}]}
  else if(/^\/api\/discovery\/(tv|movie)\/\d+$/.test(p))json=artwork(Number(p.split('/').at(-1)),p.split('/')[3])
  else if(p==='/api/media-subscriptions'){if(r.method()==='GET')json={items:[],targets:[{id:'destination',name:'移动影视',path:'/家庭影音/剧集'}],sources:[],sourceReady:false};else{writes.push(r.postDataJSON());json={...r.postDataJSON(),key:'created'}}}
  else if(p==='/api/my-media/config')json=config
  else if(p==='/api/my-media/emby/dashboard')json={featured:[poster],recent:[{...poster,id:'episode',kind:'Episode',poster:'/api/missing-poster'}],stats:[{label:'剧集',value:1}],warnings:[]}
  else if(p==='/api/my-media/emby/sessions')json=[]
  else if(p==='/api/my-media/emby/scan')json={progress:{running:false},missing:{items:[{item:poster,missing:['S01E02','S02E05'],localEpisodes:21,expectedEpisodes:23}],updated:'2026-11-06T00:00:00Z'},quality:{}}
  else if(p==='/api/archives/cache'){const items=[{id:'movie',source:'tmdb',title:'星河旅人42',tmdb:42,kind:'tv',release:'2026-01-02',overview:artwork().overview,poster:poster.poster,expires:'2026-11-13T00:00:00Z'},{id:'folder',source:'directory',title:'移动影视',path:'/家庭影音/剧集',count:2,files:[{id:'1',name:'Episode.mkv'},{id:'2',name:'Season 1',isDir:true}],expires:'2026-11-07T00:00:00Z'}].filter(v=>!u.searchParams.get('source')||v.source===u.searchParams.get('source'));json={items,total:items.length,page:1,pages:1}}
  else{unknown.push(p);return route.fulfill({status:404,json:{error:'未模拟接口 '+p}})}
  return route.fulfill({json})
 })
 return {writes,unknown,reads}
}
for(const[width,theme]of[[1440,'light'],[1440,'dark'],[390,'dark'],[375,'light'],[844,'dark']])test(`recommendation aligned toolbar and movie detail ${width} ${theme}`,async({page},info)=>{
 const f=await fixture(page);await page.setViewportSize({width,height:width===844?500:1000});await page.goto('/discover/all');await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
 await expect(page.getByRole('button',{name:'刷新推荐',exact:true})).toHaveCount(0)
 const search=await page.getByRole('textbox',{name:'搜索推荐媒体'}).boundingBox(),tabs=await page.getByRole('navigation',{name:'资源推荐栏目'}).boundingBox();expect(Math.abs(search.y+search.height/2-tabs.y-tabs.height/2)).toBeLessThan(2);expect(search.width).toBeLessThanOrEqual(260)
 const poster=page.locator('.recommendation-poster').first(),box=await poster.boundingBox();expect(Math.abs(box.height/box.width-1.5)).toBeLessThan(.02)
 await poster.hover();await expect(poster.locator('.poster-caption')).toHaveCSS('opacity','1');await expect(poster.locator('.poster-hover-actions')).toHaveCSS('opacity','1');await captureScreenshot(page,{path:info.outputPath(`recommendations-${width}-${theme}.png`)})
 await page.getByRole('button',{name:'星河旅人42 详情',exact:true}).click();await expect(page.locator('.discovery-title')).toContainText('星河旅人42');await expect(page.locator('.discovery-hero-actions button')).toHaveText(['搜索资源','添加订阅']);await expect(page.locator('.discovery-facts')).toContainText('2026-01-02');await expect(page.locator('.discovery-people')).toContainText('演员甲')
 const back=await page.getByRole('button',{name:'返回推荐',exact:true}).boundingBox();expect(back.x+back.width).toBeGreaterThan(width*.8)
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
 const cover=await page.locator('.discovery-detail-poster').boundingBox(),hero=await page.locator('.discovery-hero').boundingBox();expect(cover.width).toBe(width<=700?112:190);expect(Math.abs(cover.height/cover.width-1.5)).toBeLessThan(.01);expect(hero.height).toBeLessThan(500)
 await captureScreenshot(page,{path:info.outputPath(`detail-${width}-${theme}.png`)})
 await page.getByRole('button',{name:'搜索资源',exact:true}).click();await expect(page.getByRole('dialog',{name:'搜索资源',exact:true})).toContainText('资源来源尚未接入');await page.getByRole('dialog').locator('.modal-footer').getByRole('button',{name:'关闭',exact:true}).click()
 await page.getByRole('button',{name:'添加订阅',exact:true}).click();await expect(page.getByRole('dialog',{name:'添加订阅',exact:true})).toBeVisible();expect(f.writes).toEqual([]);await page.getByRole('dialog').getByRole('button',{name:'取消',exact:true}).click();expect(f.unknown).toEqual([])
})
for(const[width,theme]of[[1440,'light'],[390,'dark']])test(`media hero missing detail subscription and cache archive ${width}`,async({page},info)=>{
 const f=await fixture(page);await page.setViewportSize({width,height:1000});await page.goto('/my-media/panel');await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
 await expect(page.locator('.media-feature')).toContainText('库中 23 集');await expect(page.locator('.media-feature-overview')).toHaveText(artwork().overview);await expect(page.getByRole('link',{name:'查看详情',exact:true})).toHaveAttribute('href','http://emby.test:8096/web/index.html#!/item?id=show')
 await expect(page.locator('.my-media-poster .poster-image-empty')).toContainText('剧集');await captureScreenshot(page,{path:info.outputPath(`panel-${width}-${theme}.png`)})
 const cover=await page.locator('.media-feature-cover').boundingBox(),feature=await page.locator('.media-feature').boundingBox();expect(cover.width).toBe(width<=760?95:130);expect(feature.height).toBeLessThan(500)
 await page.getByRole('navigation',{name:'我的媒体栏目'}).getByRole('link',{name:'缺集管理',exact:true}).click();await page.locator('.missing-poster').click();const detail=page.getByRole('dialog',{name:'缺集详情',exact:true});await expect(detail).toContainText('家庭剧集');await expect(detail).toContainText('/media/剧集/星河旅人 (2026)');await expect(detail).toContainText('S01E02、S02E05');await captureScreenshot(page,{path:info.outputPath(`missing-${width}-${theme}.png`)})
 await detail.getByRole('button',{name:'添加订阅',exact:true}).click();const editor=page.getByRole('dialog',{name:'添加订阅',exact:true});await expect(editor).toContainText('缺集范围：S01E02、S02E05');await expect(editor.getByRole('spinbutton',{name:'开始集数',exact:true})).toHaveValue('2');await editor.getByRole('button',{name:'保存订阅',exact:true}).click();await expect(editor.getByRole('alert')).toContainText('请选择转存目录');expect(f.writes).toEqual([])
 await editor.getByRole('button',{name:'转存目录',exact:true}).click();await page.getByRole('option',{name:'移动影视 · /家庭影音/剧集',exact:true}).click();await editor.getByRole('button',{name:'保存订阅',exact:true}).click();await expect(editor).toHaveCount(0);expect(f.writes[0]).toMatchObject({id:42,kind:'tv',targetId:'destination',season:1,startEpisode:2,missingEpisodes:['S01E02','S02E05']})
 await page.goto('/archives/cache');await page.evaluate(t=>document.documentElement.dataset.theme=t,theme);await expect(page.getByRole('region',{name:'缓存档案',exact:true})).toContainText('TMDB 42');await expect(page.locator('.cache-archive-row')).toHaveCount(2);await page.getByRole('button',{name:'缓存结果',exact:true}).click();await expect(page.locator('.cache-directory-results')).toContainText('Episode.mkv');await captureScreenshot(page,{path:info.outputPath(`cache-${width}-${theme}.png`)})
 await page.getByRole('button',{name:'缓存来源',exact:true}).click();await page.getByRole('option',{name:'存储目录',exact:true}).click();await expect(page.locator('.cache-archive-row')).toHaveCount(1);expect(f.unknown).toEqual([]);expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
})
test('poster keyboard actions and reduced motion stay usable',async({page})=>{
 await fixture(page);await page.emulateMedia({reducedMotion:'reduce'});await page.goto('/discover/all');const poster=page.locator('.recommendation-poster').first();await poster.hover();await expect(poster).toHaveCSS('transform','none');await poster.getByRole('button',{name:'添加订阅 星河旅人42',exact:true}).focus();await page.keyboard.press('Enter');await expect(page.getByRole('dialog',{name:'添加订阅',exact:true})).toBeVisible()
})
