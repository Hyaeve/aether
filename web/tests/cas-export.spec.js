import {test,expect} from '@playwright/test'
async function fixture(page){
 let directory='',complete=true;const requests=[]
 const files=[{id:'/Show',name:'Show',isDir:true},{id:'/film.mp4',name:'film.mp4',size:5},{id:'/song.flac',name:'song.flac',size:5}]
 await page.route('**/api/**',r=>{const u=new URL(r.request().url());let value={}
 if(u.pathname==='/api/auth/status')value={initialized:true,authenticated:true}
 else if(u.pathname==='/api/state')value={username:'cas-export',storages:[{id:'local',type:'local',name:'本地媒体',enabled:true,config:{root:'/media'}}],tasks:[],settings:{},cache:{},logs:[]}
 else if(u.pathname==='/api/files')value=u.searchParams.get('path')==='/'?files:[{id:'/Show/episode.mkv',name:'episode.mkv',size:5}]
 else if(u.pathname==='/api/files/cas-export'){if(r.request().method()==='PUT')directory=r.request().postDataJSON().directory;value={directory}}
 else if(u.pathname==='/api/files/cas-export/jobs'){requests.push(r.request().postDataJSON());value={id:'export'+requests.length,status:'queued'}}
 else if(u.pathname.endsWith('/download'))return r.fulfill({contentType:'application/octet-stream',headers:{'Content-Disposition':'attachment; filename="film.mp4.cas"'},body:'{"name":"film.mp4"}'})
 else if(u.pathname.includes('/cas-export/jobs/'))value={id:u.pathname.split('/').at(-1),status:complete?'success':'running',processed:1,total:2,download:true,name:'film.mp4.cas'}
 else if(u.pathname==='/api/local-directories')value={path:'/output',items:[]}
 return r.fulfill({json:value})
 });return {requests,get directory(){return directory},pending(){complete=false},finish(){complete=true}}
}
test('export queues only after confirmation, closes dialog and downloads completed result',async({page},info)=>{
 const f=await fixture(page);f.pending();await page.goto('/files');await page.getByRole('button',{name:'film.mp4',exact:true}).click({button:'right'});await page.keyboard.press('Escape');await page.getByRole('button',{name:'工具',exact:true}).click();await page.getByRole('menuitem',{name:'导出CAS'}).click()
 const modal=page.getByRole('dialog',{name:'导出CAS',exact:true});await expect(modal).toContainText('film.mp4');await modal.getByRole('button',{name:'视频扩展名'}).click();await modal.getByRole('button',{name:'选择导出目录'}).click();await page.getByRole('button',{name:'选择当前目录',exact:true}).click();await expect.poll(()=>f.directory).toBe('/output');await modal.getByLabel('导出目录',{exact:true}).fill('');await modal.getByLabel('导出目录',{exact:true}).press('Enter');await expect.poll(()=>f.directory).toBe('');await modal.getByRole('button',{name:'导出',exact:true}).click();expect(f.requests).toHaveLength(0);await page.screenshot({path:info.outputPath('cas-export-confirm.png')})
 await page.getByRole('button',{name:'确认导出',exact:true}).click();await expect(modal).toHaveCount(0);expect(f.requests[0]).toMatchObject({ids:['/film.mp4'],directory:'',confirm:true});const toast=page.locator('.toast').filter({hasText:'CAS导出'});await expect(toast).toContainText('1 / 2');await expect(toast.getByRole('button',{name:'关闭通知'})).toHaveCount(0);await expect(toast.locator('progress')).toHaveAttribute('value','0.5');await page.getByRole('link',{name:'存储管理',exact:true}).click();await expect(toast).toBeVisible();const download=page.waitForEvent('download');f.finish();expect((await download).suggestedFilename()).toBe('film.mp4.cas');await expect(page.locator('.toast progress')).toHaveCount(0)
})
test('empty selection opens virtual tree and applies only checked descendants',async({page},info)=>{
 const f=await fixture(page);await page.goto('/files');await page.getByRole('button',{name:'工具',exact:true}).click();await page.getByRole('menuitem',{name:'导出CAS'}).click();const tree=page.getByRole('dialog',{name:'CAS导出范围',exact:true});await expect(tree.getByRole('checkbox',{name:'全部目录',exact:true})).toBeChecked();await tree.getByRole('button',{name:'展开 Show',exact:true}).click();await tree.getByRole('checkbox',{name:'全部目录',exact:true}).uncheck();await tree.getByRole('checkbox',{name:'episode.mkv'}).check();await tree.getByRole('button',{name:'确认范围'}).click();const modal=page.getByRole('dialog',{name:'导出CAS',exact:true});await expect(modal).toContainText('Show/episode.mkv');await page.setViewportSize({width:390,height:844});await page.screenshot({path:info.outputPath('cas-export-mobile.png')});await modal.getByRole('button',{name:'导出',exact:true}).click();await page.getByRole('button',{name:'确认导出',exact:true}).click();await expect.poll(()=>f.requests.length).toBe(1);expect(f.requests[0].ids).toEqual(['/Show/episode.mkv'])
})

test('a lost export job clears the persistent progress notice instead of polling forever',async({page})=>{
 const f=await fixture(page);f.pending();await page.goto('/files')
 await page.getByRole('button',{name:'film.mp4',exact:true}).click({button:'right'});await page.keyboard.press('Escape')
 await page.getByRole('button',{name:'工具',exact:true}).click();await page.getByRole('menuitem',{name:'导出CAS'}).click()
 await page.getByRole('dialog',{name:'导出CAS',exact:true}).getByRole('button',{name:'导出',exact:true}).click();await page.getByRole('button',{name:'确认导出',exact:true}).click()
 await expect(page.locator('.toast progress')).toHaveCount(1)
 let reads=0
 await page.route('**/api/files/cas-export/jobs/export1',r=>{reads++;return r.fulfill({status:404,json:{error:'导出任务不存在或已过期'}})})
 await expect(page.locator('.toast progress')).toHaveCount(0)
 await expect(page.locator('.toast').filter({hasText:'任务已失效'})).toBeVisible()
 expect(reads).toBe(1)
})

test('CAS export appears only for local storage, not cloud sources',async({page})=>{
 await fixture(page)
 await page.route('**/api/state',r=>r.fulfill({json:{username:'cas-export',storages:['local','mobile','tianyi','115','quark','openlist','webdav'].map(type=>({id:type,type,name:type,enabled:true,config:{mode:'native',root:'/media'}})),tasks:[],settings:{},cache:{}}}))
 for(const provider of ['local','mobile','tianyi','115','quark','openlist','webdav']){
  await page.goto('/files?storage='+provider)
  await page.getByRole('button',{name:'工具',exact:true}).click()
  await expect(page.getByRole('menuitem',{name:'导出CAS'})).toHaveCount(provider==='local'?1:0)
  await page.keyboard.press('Escape')
 }
})
