import {test,expect} from '@playwright/test'

test('completed transfer toast contains names, opens its target across pages and lasts ten seconds',async({page})=>{
 await page.route('**/api/**',r=>{const u=new URL(r.request().url());let value={}
  if(u.pathname==='/api/auth/status')value={initialized:true,authenticated:true}
  else if(u.pathname==='/api/state')value={username:'toast-owner',storages:[{id:'p',type:'115',name:'115主账号',enabled:true,config:{}}],tasks:[],settings:{},cache:{}}
  else if(u.pathname==='/api/files')value=u.searchParams.get('path')==='destination'?[{id:'result',name:'转存结果.mkv'}]:[]
  else if(u.pathname.endsWith('/preview'))value={preview:'plan',batches:1,items:[{name:'影片合集'}],batchNames:[['影片合集']]}
  else if(u.pathname.endsWith('/batch'))value={status:'submitted'}
  return r.fulfill({json:value})
 })
 await page.addInitScript(()=>localStorage.setItem('aether-share-directory:toast-owner:p',JSON.stringify({path:'destination',label:'目标目录',history:[{id:'/',name:'目标目录'}]})))
 await page.clock.install()
 await page.goto('/files?storage=p');await page.getByRole('button',{name:'工具',exact:true}).click();await page.getByRole('menuitem',{name:'分享转存',exact:true}).click();await page.getByLabel('分享链接',{exact:true}).fill('https://115cdn.com/s/abc?password=ABCD');await page.getByRole('button',{name:'转存',exact:true}).click()
 const target=page.getByRole('button',{name:'打开转存目标目录'});await expect(target).toHaveText('转存成功：115主账号 · 影片合集')
 await page.getByRole('link',{name:'存储管理',exact:true}).click();await target.click();await expect(page.locator('.path-bar [aria-current=location]')).toHaveText('目标目录');await expect(page.getByRole('button',{name:'转存结果.mkv',exact:true})).toBeVisible()
 await page.clock.runFor(8500);await expect(target).toBeVisible();await page.clock.runFor(2000);await expect(target).toHaveCount(0)
})

test('share copy falls back when clipboard API is missing and fixed code uses provider default',async({page})=>{
 let options={days:15,codeMode:'random',code:'OLD1',limit:0,autofill:true},submitted
 await page.addInitScript(()=>{Object.defineProperty(navigator,'clipboard',{value:undefined,configurable:true});document.execCommand=command=>{window.copiedText=document.activeElement.value;return command==='copy'}})
 await page.route('**/api/**',r=>{const u=new URL(r.request().url());let value={}
  if(u.pathname==='/api/auth/status')value={initialized:true,authenticated:true}
  else if(u.pathname==='/api/state')value={username:'copy-owner',storages:[{id:'p',type:'115',name:'115',enabled:true,config:{}}],tasks:[],settings:{},cache:{}}
  else if(u.pathname==='/api/files')value=[{id:'f',name:'影片',isDir:true}]
  else if(u.pathname==='/api/cloud/preferences'){if(r.request().method()==='PUT')options=r.request().postDataJSON();value=options}
  else if(u.pathname==='/api/cloud/shares'){submitted=r.request().postDataJSON();value={id:'created',name:'影片',url:'https://115cdn.com/s/created',code:'ABCD'}}
  return r.fulfill({json:value})
 })
 await page.goto('/files?storage=p');await page.getByRole('button',{name:'影片',exact:true}).click({button:'right'});await page.getByRole('button',{name:'分享',exact:true}).click();const modal=page.getByRole('dialog',{name:'创建分享'});await modal.getByRole('button',{name:'访问码模式'}).click();await modal.getByRole('option',{name:'固定访问码'}).click();await expect(modal.getByRole('textbox',{name:'固定访问码'})).toHaveCount(0);await modal.getByRole('button',{name:'创建分享'}).click()
 await page.getByRole('button',{name:'复制分享内容'}).click();await expect.poll(()=>page.evaluate(()=>window.copiedText)).toContain('访问码：ABCD');await page.getByRole('button',{name:'仅复制链接'}).click();await expect.poll(()=>page.evaluate(()=>window.copiedText)).toBe('https://115cdn.com/s/created?password=ABCD');expect(submitted.options.code).toBe('')
})
