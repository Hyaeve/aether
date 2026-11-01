import {test,expect} from '@playwright/test'
import {captureScreenshot} from './helpers/screenshot.js'

const defaults='mp4;mkv;avi;mov;wmv;flv;webm;m4v;ts;m2ts;mkvb;rm;3gp;iso;ass;srt;ssa;sub'
async function fixture(page){
 const writes=[],unexpected=[],cfg={connected:true,hasToken:true,nick:'阿里测试账号',userId:'alice',extensions:defaults,targets:[{id:'receiver',storageId:'p',directory:'10',path:'/家庭影音/阿里秒传接收目录'}],active:[]}
 let records=[],qrPoll=0,failConnect=false
 await page.route('**/api/**',async route=>{
  const r=route.request(),p=new URL(r.url()).pathname
  let json
  if(p==='/api/auth/status')json={initialized:true,authenticated:true}
  else if(p==='/api/state')json={username:'ali-test',storages:[{id:'p',name:'115影视存储池',type:'115',enabled:true,config:{root:'0'}},{id:'q',name:'夸克',type:'quark',enabled:true,config:{}}],tasks:[],settings:{},cache:{},traffic:{},links:[],fileVisits:[],fileFavorites:[]}
  else if(['/api/traffic','/api/plugins/proxy','/api/115-recycle-cleanup','/api/115-folder-cleanup','/api/115-signin','/api/115-simulcast/enabled'].includes(p))json={enabled:false,rules:[]}
  else if(p==='/api/quark-takeover')json={enabled:false,bindings:[]}
  else if(p==='/api/strm-replace')json={tasks:[]}
  else if(/^\/api\/plugins\/(emby|tmdb|ai)$/.test(p))json={enabled:false}
  else if(p==='/api/ali115'){
   if(r.method()==='PUT'){writes.push({p,body:r.postDataJSON()});cfg.extensions=r.postDataJSON().extensions}
   if(r.method()==='POST'){writes.push({p,body:r.postDataJSON()});return route.fulfill({status:409,json:{error:'同一115存储的同一目录不能重复添加'}})}
   json=cfg
  }
  else if(p==='/api/ali115/account'){writes.push({p,body:r.postDataJSON()});cfg.hasToken=true;if(failConnect){failConnect=false;cfg.connected=false;return route.fulfill({status:502,json:{error:'设备会话连接失败，凭据已保存'}})}cfg.connected=true;json=cfg}
  else if(p==='/api/ali115/targets/receiver'){writes.push({p});cfg.targets=[];json=cfg}
  else if(p==='/api/ali115/run'){
   const body=r.postDataJSON();writes.push({p,body});cfg.active=[{target:'receiver',done:0,total:3,message:'正在向115秒传'}]
   records=[{id:'run',name:'测试电影分享与字幕',link:body.link,source:'浏览器',status:'running',created:'2026-11-01T13:19:00+08:00',total:3,success:0,skipped:0,failed:0,message:'正在向115秒传',files:[]}];json={id:'run'}
  }
  else if(p==='/api/ali115/records')json=records
  else if(p==='/api/ali115/qr'){
   if(r.method()==='POST')json={id:'qr',image:'data:image/svg+xml;base64,'+Buffer.from('<svg xmlns="http://www.w3.org/2000/svg" width="256" height="256"><rect width="256" height="256" fill="white"/><path d="M30 30h60v60H30zM166 30h60v60h-60zM30 166h60v60H30z" fill="black"/></svg>').toString('base64')}
   else {qrPoll++;json={status:'CONFIRMED'}}
  }
  else if(p==='/api/files')json=[]
  else {unexpected.push(r.method()+' '+p);return route.fulfill({status:500,json:{error:'Unexpected API '+p}})}
  return route.fulfill({json})
 })
 return{cfg,writes,unexpected,get qrPoll(){return qrPoll},setRecords:value=>records=value,failNextConnect:()=>failConnect=true}
}

test('Ali115 reconnect uses saved rotating token after session failure',async({page})=>{
 const f=await fixture(page);f.cfg.connected=false;f.cfg.hasToken=false
 await page.setViewportSize({width:390,height:844});await page.goto('/tools')
 await page.getByRole('button',{name:'阿里云盘秒传',exact:true}).click();const modal=page.getByRole('dialog',{name:'阿里云盘秒传',exact:true})
 await modal.getByRole('tab',{name:'账号配置'}).click();await modal.getByLabel('refresh token',{exact:true}).fill('rotating-token-1234567890');f.failNextConnect()
 await modal.getByRole('button',{name:'连接账号',exact:true}).click();await expect(modal.getByLabel('refresh token',{exact:true})).toHaveValue('')
 await modal.getByRole('button',{name:'重试连接',exact:true}).click();await expect(page.locator('.toast-stack')).toContainText('账号已连接')
 expect(f.writes.filter(w=>w.p==='/api/ali115/account').at(-1).body.refreshToken).toBe('');expect(f.unexpected).toEqual([])
})

for(const [width,height,theme] of [[1440,1000,'light'],[390,844,'dark'],[375,812,'light'],[844,390,'dark']]){
 test(`Ali115 plugin account autosave and receiver flow ${width}`,async({page},info)=>{
  await page.addInitScript(()=>{Object.defineProperty(Crypto.prototype,'randomUUID',{value:undefined,configurable:true})})
  const f=await fixture(page);await page.setViewportSize({width,height});await page.goto('/tools');await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
  const cards=page.locator('.plugin-grid .plugin-card')
  await expect(cards.nth(5)).toHaveAttribute('aria-label','阿里云盘秒传')
  await expect(cards.nth(5).locator('.ali-rapid-icon img')).toHaveAttribute('src','/providers/aliyun.png')
  await expect(cards.nth(5).getByText('待实现')).toHaveCount(0)
  await cards.nth(5).click();const modal=page.getByRole('dialog',{name:'阿里云盘秒传',exact:true})
  await expect(modal.getByRole('tab',{name:'接收目录'})).toHaveAttribute('aria-selected','true')
  await expect(modal.locator('.ali-target')).toHaveCount(1)
  await modal.getByRole('tab',{name:'账号配置'}).click()
  const extensions=modal.getByRole('textbox',{name:'秒传文件扩展名',exact:true})
  await expect(extensions).toHaveValue(defaults)
  await extensions.fill('mkv;ass');await extensions.press('Enter');await expect(page.locator('.toast-stack')).toContainText('秒传文件扩展名已保存')
  await extensions.fill('mp4;srt');await modal.getByRole('tab',{name:'接收目录'}).click();await expect.poll(()=>f.cfg.extensions).toBe('mp4;srt')
  await modal.getByRole('tab',{name:'账号配置'}).click()
  await modal.getByLabel('refresh token',{exact:true}).fill('test-refresh-token-1234567890')
  await modal.getByRole('button',{name:'连接账号',exact:true}).click();await expect(modal.getByLabel('refresh token',{exact:true})).toHaveValue('')
  await modal.locator('.ali-account-status').scrollIntoViewIfNeeded()
  expect(await modal.evaluate(el=>el.scrollWidth<=el.clientWidth)).toBe(true)
  await captureScreenshot(page,{path:info.outputPath(`ali-account-${width}-${theme}.png`)})
  await modal.getByRole('tab',{name:'接收目录'}).click();await modal.getByRole('button',{name:'秒传到 /家庭影音/阿里秒传接收目录',exact:true}).click()
  const rapid=page.getByRole('dialog',{name:'阿里云盘分享秒传',exact:true})
  await rapid.getByRole('textbox',{name:'阿里云盘分享链接',exact:true}).fill('https://www.alipan.com/s/example?pwd=1234')
  await rapid.getByRole('button',{name:'开始秒传',exact:true}).click();const history=page.getByRole('dialog',{name:'阿里云盘秒传记录',exact:true})
  await expect(history.getByText('测试电影分享与字幕')).toBeVisible();await expect(history.locator('progress')).toHaveAttribute('max','3')
  await captureScreenshot(page,{path:info.outputPath(`ali-records-${width}-${theme}.png`)})
  await history.getByRole('button',{name:'关闭',exact:true}).click()
  await expect(modal.getByRole('button',{name:'秒传到 /家庭影音/阿里秒传接收目录',exact:true})).toBeDisabled()
  expect(f.writes.filter(v=>v.p==='/api/ali115/run')).toHaveLength(1)
  expect(f.unexpected).toEqual([])
 })
 test(`Ali115 duplicate picker, QR, history pagination and delete ${width}`,async({page},info)=>{
  const f=await fixture(page);await page.setViewportSize({width,height});await page.goto('/tools');await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
  await page.getByRole('button',{name:'阿里云盘秒传',exact:true}).click();const modal=page.getByRole('dialog',{name:'阿里云盘秒传',exact:true})
  await modal.getByRole('button',{name:'添加115秒传接收目录',exact:true}).click();const picker=page.getByRole('dialog',{name:'选择存储目录',exact:true})
  await expect(picker.getByRole('button',{name:/夸克/})).toHaveCount(0)
  await picker.getByRole('button',{name:'选择当前目录',exact:true}).click();await expect(page.locator('.toast-stack')).toContainText('不能重复添加');await expect(picker).toBeVisible()
  await picker.getByRole('button',{name:'关闭',exact:true}).click()
  await modal.getByRole('tab',{name:'账号配置'}).click();await modal.getByRole('button',{name:'扫码登录',exact:true}).click()
  const qr=page.getByRole('dialog',{name:'阿里云盘扫码登录',exact:true});await expect(qr.getByRole('img')).toBeVisible();await expect(qr).toHaveCount(0);expect(f.qrPoll).toBe(1)
  await modal.getByRole('tab',{name:'接收目录'}).click()
  await captureScreenshot(page,{path:info.outputPath(`ali-receivers-${width}-${theme}.png`)})
  f.setRecords(Array.from({length:21},(_,i)=>({id:String(i),name:`分享 ${i+1}`,link:'https://www.alipan.com/s/example',source:'TG Bot',status:'completed',created:'2026-11-01T13:19:00+08:00',success:2,skipped:1,failed:0,message:'秒传完成，临时文件已清理'})))
  await modal.getByRole('button',{name:'秒传记录 /家庭影音/阿里秒传接收目录',exact:true}).click();const history=page.getByRole('dialog',{name:'阿里云盘秒传记录',exact:true})
  await expect(history.locator('.ali-record')).toHaveCount(20);await history.getByRole('spinbutton',{name:'跳转页码'}).fill('99');await expect(history.getByRole('spinbutton',{name:'跳转页码'})).toHaveValue('2');await expect(history.locator('.ali-record')).toHaveCount(1)
  await history.getByRole('button',{name:'关闭',exact:true}).click();await modal.getByRole('button',{name:'删除接收目录 /家庭影音/阿里秒传接收目录',exact:true}).click()
  const confirm=page.getByRole('dialog',{name:'删除接收目录',exact:true});await confirm.getByRole('button',{name:'确认删除',exact:true}).click();await expect(modal.locator('.ali-target')).toHaveCount(0)
  expect(f.unexpected).toEqual([])
 })
}
