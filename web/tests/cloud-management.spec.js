import {test,expect} from '@playwright/test'
async function fixture(page,count=30){
 const posts=[];let cleanup={enabled:false,rules:[]},simulcast=true,options={days:15,codeMode:'random',code:'',limit:0,autofill:true,encrypted:false}
 const items=Array.from({length:count},(_,i)=>({id:String(i),name:'媒体-'+i+'.mp4',size:1048576,url:'https://115cdn.com/s/share'+i,path:'/媒体/剧集',time:'2026-10-14T03:00:00Z',status:i%3?'error':'success',progress:i%3?.3:1,code:'ABCD'}))
 await page.route('**/api/**',r=>{const req=r.request(),u=new URL(req.url());let value={}
 if(u.pathname==='/api/auth/status')value={initialized:true,authenticated:true}
 else if(u.pathname==='/api/state')value={username:'cloud-management',storages:[{id:'p',name:'115主账号',type:'115',enabled:true,config:{}},{id:'local',name:'本地',type:'local',enabled:true,config:{}}],tasks:[],settings:{},cache:{},logs:[]}
 else if(u.pathname==='/api/cloud/manage'){if(req.method()==='POST'){posts.push(req.postDataJSON());value={count:req.postDataJSON().ids.length||30}}else value={items,edit:true,password:true}}
 else if(u.pathname==='/api/cloud/preferences'){if(req.method()==='PUT')options=req.postDataJSON();value=options}
 else if(u.pathname==='/api/cloud/shares'){posts.push(req.postDataJSON());value={id:'created',name:'影片',url:'https://115cdn.com/s/created?password=ABCD',code:'ABCD'}}
 else if(u.pathname==='/api/files')value=[{id:'10',name:'影片',isDir:true}]
 else if(u.pathname==='/api/115-recycle-cleanup'){if(req.method()==='PUT')cleanup=req.postDataJSON();value=cleanup}
 else if(u.pathname==='/api/115-simulcast/enabled'){if(req.method()==='PUT')simulcast=req.postDataJSON().enabled;value={enabled:simulcast}}
 return r.fulfill({json:value})
 });return {posts,get cleanup(){return cleanup},get options(){return options}}
}
test('cloud columns virtualize records and confirm ranged bulk actions with a fresh PIN',async({page},info)=>{
 const f=await fixture(page);await page.goto('/storage/downloads');await expect(page.getByRole('navigation',{name:'存储管理栏目'})).toBeVisible();await page.getByRole('button',{name:'选择云盘存储池'}).click();await expect(page.getByRole('option')).toHaveCount(2);await page.keyboard.press('Escape');const rows=page.locator('.cloud-management-row');await expect(rows.first()).toBeVisible();expect(await rows.count()).toBeLessThan(30);await rows.nth(1).click({button:'right'});await rows.nth(4).click({modifiers:['Shift']});const dock=page.getByRole('toolbar',{name:'所选云盘条目操作'});await expect(dock).toContainText('已选 4 项');await dock.getByRole('button',{name:'重试',exact:true}).click();const modal=page.getByRole('dialog',{name:'确认重试'});await expect(modal).toContainText('3 项');await modal.getByRole('button',{name:'确认',exact:true}).click();expect(f.posts[0]).toMatchObject({action:'retry',ids:['1','2','4'],confirm:true});await expect(dock).toHaveCount(0);await page.screenshot({path:info.outputPath('cloud-downloads-light.png')})
 await page.getByRole('link',{name:'回收站',exact:true}).click();await page.getByRole('button',{name:'清空回收站',exact:true}).click();const clear=page.getByRole('dialog',{name:'确认清空回收站'});await expect(clear.getByRole('textbox',{name:'115回收站密码',exact:true})).toHaveValue('');await clear.getByRole('textbox',{name:'115回收站密码',exact:true}).fill('1234');await clear.getByRole('button',{name:'确认',exact:true}).click();expect(f.posts.at(-1)).toMatchObject({action:'clear',ids:[],password:'1234'});await page.getByRole('button',{name:'清空回收站',exact:true}).click();await expect(page.getByRole('textbox',{name:'115回收站密码',exact:true})).toHaveValue('');await page.keyboard.press('Escape');await page.setViewportSize({width:390,height:844});await page.evaluate(()=>document.documentElement.dataset.theme='dark');expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);await page.screenshot({path:info.outputPath('cloud-trash-dark-mobile.png')})
})
test('file context creates a configured share with both copy commands',async({page},info)=>{
 const f=await fixture(page);await page.goto('/files');await page.getByRole('button',{name:'选择存储池'}).click();await page.getByRole('option',{name:'115主账号'}).click();await page.getByRole('button',{name:'影片',exact:true}).click({button:'right'});await page.getByRole('button',{name:'分享',exact:true}).click();const modal=page.getByRole('dialog',{name:'创建分享'});await expect(modal).toHaveCSS('width','640px');await expect(modal.getByRole('button',{name:'分享有效期'})).toContainText('15天');await expect(modal.getByRole('switch',{name:'自动填充访问码'})).toBeChecked();await modal.getByRole('button',{name:'访问码模式'}).click();await modal.getByRole('option',{name:'固定访问码'}).click();await expect(modal.getByRole('textbox',{name:'固定访问码',exact:true})).toHaveCount(0);await modal.getByRole('spinbutton',{name:'接收次数限制'}).fill('4');await modal.getByRole('button',{name:'创建分享'}).click();await expect(page.getByRole('dialog',{name:'分享链接'})).toBeVisible();expect(f.options).toMatchObject({codeMode:'fixed',code:'',limit:4});await expect(page.getByRole('button',{name:'复制分享内容'})).toBeVisible();await expect(page.getByRole('button',{name:'仅复制链接'})).toBeVisible();await page.screenshot({path:info.outputPath('cloud-share-result.png')})
})
test('cleanup plugin creates one task per pool and icons toggle both global switches',async({page},info)=>{
 const f=await fixture(page);await page.goto('/tools');await page.getByRole('button',{name:'115回收站清空',exact:true}).click();await page.getByRole('button',{name:'添加清空任务'}).click();const modal=page.getByRole('dialog',{name:'添加清空任务',exact:true});await expect(modal).toHaveCSS('width','640px');await expect(modal.getByRole('spinbutton',{name:'保留天数'})).toHaveValue('10');await modal.getByRole('textbox',{name:'回收站密码'}).fill('1234');await modal.getByRole('button',{name:'保存',exact:true}).click();await expect(page.getByRole('button',{name:'添加清空任务'})).toBeDisabled();expect(f.cleanup.rules).toHaveLength(1);await page.screenshot({path:info.outputPath('cleanup-rules.png')});await page.keyboard.press('Escape');await page.getByRole('button',{name:'启用115回收站清空',exact:true}).click();await expect(page.getByRole('button',{name:'停用115回收站清空',exact:true})).toHaveAttribute('aria-pressed','true');await page.getByRole('button',{name:'停用115 同播复制',exact:true}).click();await expect(page.getByRole('button',{name:'启用115 同播复制',exact:true})).toHaveAttribute('aria-pressed','false')
})

test('bulk record deletion uses explicit ordered batches below five hundred',async({page})=>{
 const f=await fixture(page,501);await page.goto('/storage/downloads')
 await page.locator('.cloud-management-row').first().click({button:'right'})
 const dock=page.getByRole('toolbar',{name:'所选云盘条目操作'});await dock.getByRole('button',{name:'全选',exact:true}).click()
 await expect(dock).toContainText('已选 501 项');await dock.getByRole('button',{name:'删除',exact:true}).click()
 const confirm=page.getByRole('dialog',{name:'确认删除记录',exact:true});await expect(confirm).toContainText('501 项')
 await expect(page.getByRole('button',{name:'选择云盘存储池'})).toBeDisabled()
 await confirm.getByRole('button',{name:'确认',exact:true}).click()
 await expect.poll(()=>f.posts.length).toBe(2)
 expect(f.posts.map(p=>p.ids.length)).toEqual([499,2])
 expect(f.posts.flatMap(p=>p.ids)).toEqual(Array.from({length:501},(_,i)=>String(i)))
 await expect(dock).toHaveCount(0)
})
