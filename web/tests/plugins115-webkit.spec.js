import {test,expect} from '@playwright/test'
import {captureScreenshot} from './helpers/screenshot.js'
test.use({browserName:'webkit'})
for(const [width,height,theme] of [[375,812,'light'],[844,390,'dark']]){
 test(`folder tasks scroll without moving the footer ${width} ${theme}`,async({page},info)=>{
  await page.route('**/api/**',route=>{
   const path=new URL(route.request().url()).pathname
   let value={}
   if(path==='/api/auth/status')value={initialized:true,authenticated:true}
   else if(path==='/api/state')value={username:'webkit-plugin',storages:[{id:'p',type:'115',name:'115 家庭影音',enabled:true,config:{}}],tasks:[],settings:{},cache:{},traffic:{},logs:[]}
   else if(path==='/api/115-folder-cleanup')value={enabled:true,rules:Array.from({length:12},(_,i)=>({id:`r${i}`,storageId:'p',directory:String(i+10),label:`/影音/目录${i}`,cron:'0 3 * * *',days:i}))}
   else if(['/api/115-signin','/api/115-recycle-cleanup'].includes(path))value={enabled:false,rules:[]}
   else if(path==='/api/115-simulcast/enabled')value={enabled:true}
   else if(path==='/api/quark-takeover')value={enabled:false,bindings:[]}
   else if(path.startsWith('/api/plugins/'))value={enabled:false}
   else return route.fulfill({status:500,json:{error:`Unmocked ${path}`}})
   return route.fulfill({json:value})
  })
  await page.setViewportSize({width,height});await page.goto('/tools')
  await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
  await page.emulateMedia({reducedMotion:'reduce'})
  await page.getByRole('button',{name:'115 文件夹清理',exact:true}).click()
  const dialog=page.getByRole('dialog',{name:'115 文件夹清理',exact:true}),list=dialog.locator('.plugin-task-list'),add=dialog.getByRole('button',{name:'添加文件夹',exact:true})
  await expect(list.locator('.plugin-task-row')).toHaveCount(12)
  const before=await add.boundingBox()
  expect(before.y+before.height).toBeLessThan(height)
  expect(await list.evaluate(el=>el.scrollHeight>el.clientHeight)).toBe(true)
  await list.evaluate(el=>el.scrollTop=el.scrollHeight)
  await expect(list.getByRole('button',{name:'编辑文件夹清理 /影音/目录11',exact:true})).toBeInViewport()
  const after=await add.boundingBox()
  expect(Math.abs(before.y-after.y)).toBeLessThan(1)
  expect(await dialog.evaluate(el=>el.scrollWidth<=el.clientWidth)).toBe(true)
  await captureScreenshot(page,{path:info.outputPath(`folder-overflow-${width}-${theme}.png`)})
 })
}
