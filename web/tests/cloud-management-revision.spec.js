import {test,expect} from '@playwright/test'

async function fixture(page) {
 const calls=[],posts=[]
 const pools=[{id:'p',type:'115',name:'115主账号',enabled:true,config:{}},{id:'m',type:'mobile',name:'移动主账号',enabled:true,config:{mode:'native'}},{id:'q',type:'quark',name:'夸克',enabled:true,config:{}},{id:'t',type:'tianyi',name:'天翼',enabled:true,config:{}}]
 const items=Array.from({length:100},(_,i)=>({id:String(i),name:`媒体-${i}.mkv`,url:`https://115cdn.com/s/code${i}`,path:'/剧集/Season 1',size:12,time:'2026-10-15T03:00:00Z',status:'error'}))
 let failure=false
 await page.route('**/api/**',r=>{
  const u=new URL(r.request().url()),body=r.request().method()==='POST'?r.request().postDataJSON():null
  let value={}
  if(u.pathname==='/api/auth/status')value={initialized:true,authenticated:true}
  else if(u.pathname==='/api/state')value={username:'cloud-revision',storages:pools,tasks:[],settings:{},cache:{},traffic:{}}
  else if(u.pathname==='/api/cloud/manage'){
   if(body){posts.push({storage:u.searchParams.get('storage'),...body});value={count:body.ids.length}}
   else{calls.push(u.searchParams.toString());if(u.searchParams.get('storage')==='cas')value={items:[{...items[0],id:'m-record',storageId:'m',storageName:'移动主账号',provider:'mobile'},{...items[1],id:'t-record',storageId:'t',storageName:'天翼',provider:'tianyi'}],nextPage:0};else if(u.searchParams.get('page')==='2'){if(failure)return r.fulfill({status:502,json:{error:'115云下载读取失败'}});value={items:[{...items[0],id:'last',name:'最后一项'}],nextPage:0}}else value={items,edit:true,password:true,nextPage:2}}
  }
  return r.fulfill({json:value})
 })
 return {calls,posts,fail:()=>{failure=true}}
}

test('cloud pages load on scroll, preserve loaded rows on failure and toast errors',async({page},info)=>{
 const f=await fixture(page);await page.goto('/storage/downloads')
 await expect(page.locator('.cloud-total')).toHaveText('100 项')
 expect(f.calls).toHaveLength(1)
 const visible=await page.locator('.page-scroll>.thin-scroll-area').evaluate(el=>Math.min(100,Math.ceil(el.clientHeight/60)+6))
 await expect(page.locator('.cloud-management-row')).toHaveCount(visible)
 f.fail();await page.locator('.page-scroll .thin-scroll-area').evaluate(el=>{el.scrollTop=el.scrollHeight})
 await expect(page.locator('.toast.error')).toContainText('115云下载读取失败')
 await expect(page.locator('.cloud-total')).toHaveText('100 项')
 await expect(page.locator('.cloud-management-panel .error-message')).toHaveCount(0)
 await page.locator('.page-scroll .thin-scroll-area').evaluate(el=>{el.scrollTop=0})
 await expect(page.locator('.cloud-management-row').first()).toContainText('媒体-0')
 await page.getByRole('button',{name:'选择云盘存储池'}).click()
 await expect(page.getByRole('option')).toHaveCount(2)
 await page.getByRole('option',{name:'CAS 秒传'}).click()
 await expect(page.locator('.cloud-target strong')).toHaveText(['移动主账号','天翼'])
 await expect(page.locator('.cloud-target bdi')).toHaveText(['/剧集/Season 1','/剧集/Season 1'])
 const rows=page.locator('.cloud-management-row');await rows.first().click({button:'right'});await rows.nth(1).click({modifiers:['Shift']})
 await expect(rows.first()).toHaveClass(/selected/)
 const selectedBackground=await rows.first().evaluate(el=>getComputedStyle(el).backgroundColor)
 await page.keyboard.press('Escape');expect(await rows.first().evaluate(el=>getComputedStyle(el).backgroundColor)).not.toBe(selectedBackground)
 await page.screenshot({path:info.outputPath('cas-aggregate-light.png')})
 await page.evaluate(()=>document.documentElement.dataset.theme='dark');await page.setViewportSize({width:390,height:844});await page.screenshot({path:info.outputPath('cas-aggregate-dark-mobile.png')})
 await page.getByRole('link',{name:'回收站',exact:true}).click();await page.getByRole('button',{name:'选择云盘存储池'}).click();await expect(page.getByRole('option')).toHaveCount(1)
})

test('shares multi-select exposes confirmed cancellation and appends pages without resetting scroll',async({page})=>{
 const f=await fixture(page);await page.goto('/storage/shares')
 const view=page.locator('.page-scroll .thin-scroll-area');await view.evaluate(el=>{el.scrollTop=el.scrollHeight})
 await expect(page.locator('.cloud-total')).toHaveText('101 项')
 expect(await view.evaluate(el=>el.scrollTop)).toBeGreaterThan(0)
 await view.evaluate(el=>{el.scrollTop=0})
 const rows=page.locator('.cloud-management-row');await rows.first().click({button:'right'});await rows.nth(2).click({modifiers:['Shift']})
 const dock=page.getByRole('toolbar',{name:'所选云盘条目操作'});await expect(dock).toContainText('已选 3 项');await dock.getByRole('button',{name:'删除分享',exact:true}).click()
 const modal=page.getByRole('dialog',{name:'确认撤回分享'});await expect(modal).toContainText('3 项');expect(f.posts).toHaveLength(0);await modal.getByRole('button',{name:'确认',exact:true}).click()
 await expect.poll(()=>f.posts.length).toBe(1);expect(f.posts[0]).toMatchObject({action:'cancel',ids:['0','1','2'],confirm:true})
})

test('unified CAS bulk operation groups records by real storage rather than pseudo pool',async({page})=>{
 const f=await fixture(page);await page.goto('/storage/downloads');await page.getByRole('button',{name:'选择云盘存储池'}).click();await page.getByRole('option',{name:'CAS 秒传'}).click()
 const rows=page.locator('.cloud-management-row');await rows.first().click({button:'right'});const dock=page.getByRole('toolbar',{name:'所选云盘条目操作'});await dock.getByRole('button',{name:'全选',exact:true}).click();await dock.getByRole('button',{name:'删除',exact:true}).click();await page.getByRole('dialog',{name:'确认清理记录'}).getByRole('button',{name:'确认',exact:true}).click()
 await expect.poll(()=>f.posts.length).toBe(2);expect(f.posts.map(p=>[p.storage,p.ids])).toEqual([['m',['m-record']],['t',['t-record']]])
})
