import {test,expect} from '@playwright/test'
import {captureScreenshot} from './helpers/screenshot.js'

async function fixture(page){
 let settings={enabled:false,linkIds:[]},reject=false
 const writes=[],unexpected=[]
 const links=[['emby','Emby客厅','/strm'],['audiobookshelf','有声书库','/books'],['fnos','飞牛影视','/video'],['emby','尚未映射','']].map(([type,name,mediaRoot],i)=>({id:'l'+i,type,name,mediaRoot,upstreamRoot:'/media',enabled:true,address:'http://media.test:8096',port:15180+i,mode:'public'}))
 await page.route('**/api/**',async route=>{
  const r=route.request(),p=new URL(r.url()).pathname;let json
  if(p==='/api/auth/status')json={initialized:true,authenticated:true}
  else if(p==='/api/state')json={username:'deep-delete',storages:[],tasks:[],links,settings:{},logs:[],cache:{},traffic:{}}
  else if(p==='/api/media-deep-delete'){
   if(r.method()==='PUT'){writes.push(r.postDataJSON());if(reject)return route.fulfill({status:409,json:{error:'媒体深度删除正在执行，请稍后修改配置'}});settings=r.postDataJSON();return route.fulfill({json:{enabled:settings.enabled,linkIds:settings.linkIds}})}
   json={settings,records:[{id:'record',linkName:'Emby客厅',name:'电影.strm',time:'2026-11-01T13:00:00Z',status:'success',count:2,message:'已删除2个真实源文件'}]}
  }
  else if(p==='/api/115-folder-cleanup'||p==='/api/115-signin'||p==='/api/115-recycle-cleanup')json={enabled:false,rules:[]}
  else if(p==='/api/115-simulcast/enabled')json={enabled:false}
  else if(p==='/api/quark-takeover')json={enabled:false,bindings:[]}
  else if(/^\/api\/plugins\/(emby|tmdb|ai|proxy)$/.test(p))json={enabled:false}
  else if(p==='/api/strm-replace')json={tasks:[]}
  else if(p==='/api/traffic')json={}
  else{unexpected.push(r.method()+' '+p);return route.fulfill({status:500,json:{error:'Unexpected '+p}})}
  await route.fulfill({json})
 })
 return {writes,unexpected,setReject(value){reject=value}}
}
for(const [width,height,theme] of [[1440,1000,'light'],[390,844,'dark'],[375,812,'light'],[844,390,'dark']])test(`media deep delete settings confirmation and records ${width}`,async({page},info)=>{
 const f=await fixture(page);await page.setViewportSize({width,height});await page.goto('/tools');await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
 await expect(page.locator('.plugin-card')).toHaveCount(11)
 const names=await page.locator('.plugin-heading strong').allTextContents();expect(names[names.indexOf('Emby 通知')+1]).toBe('媒体深度删除')
 const plugin=page.getByRole('button',{name:'媒体深度删除',exact:true});await expect(plugin.locator('.status')).toHaveCount(0);await plugin.click()
 const dialog=page.getByRole('dialog',{name:'媒体深度删除',exact:true});await expect(dialog.getByRole('switch',{name:'启用媒体深度删除'})).not.toBeChecked()
 await expect(dialog.getByRole('checkbox',{name:'联动 尚未映射'})).toBeDisabled()
 const row=dialog.locator('.deep-link').first();const aligned=await row.evaluate(el=>{const r=el.getBoundingClientRect(),icon=el.querySelector('svg').getBoundingClientRect(),label=el.querySelector('span').getBoundingClientRect(),check=el.querySelector('input').getBoundingClientRect();return {height:r.height,sameRow:Math.abs((icon.y+icon.height/2)-(check.y+check.height/2))<1,rightOf:label.x>icon.right&&check.x>label.x}})
 expect(aligned.height).toBeLessThanOrEqual(80);expect(aligned.sameRow).toBe(true);expect(aligned.rightOf).toBe(true)
 const bounds=await dialog.locator('.deep-body').evaluate(el=>{const r=el.getBoundingClientRect(),s=getComputedStyle(el);return{client:el.clientWidth,scroll:el.scrollWidth,inside:[...el.querySelectorAll('.deep-link')].every(n=>{const b=n.getBoundingClientRect();return b.left>=r.left+parseFloat(s.paddingLeft)-1&&b.right<=r.right-parseFloat(s.paddingRight)+1})}})
 expect(bounds.scroll).toBeLessThanOrEqual(bounds.client);expect(bounds.inside).toBe(true)
 await expect(dialog.getByRole('button',{name:'联动配置',exact:true})).toHaveCSS('background-color','rgba(0, 0, 0, 0)')
 await dialog.getByRole('switch',{name:'启用媒体深度删除'}).check();await expect(dialog.getByRole('button',{name:'保存配置'})).toBeDisabled()
 await dialog.getByRole('checkbox',{name:'联动 Emby客厅'}).check();await dialog.getByRole('button',{name:'保存配置'}).click()
 let confirmation=page.getByRole('dialog',{name:'启用媒体深度删除',exact:true});await expect(confirmation).toContainText('真实源文件');await confirmation.getByRole('button',{name:'返回',exact:true}).click();expect(f.writes).toHaveLength(0)
 await dialog.getByRole('button',{name:'保存配置'}).click();confirmation=page.getByRole('dialog',{name:'启用媒体深度删除',exact:true});await confirmation.getByRole('button',{name:'确认启用',exact:true}).click()
 await expect(page.locator('.toast-stack')).toContainText('媒体深度删除配置已保存');expect(f.writes).toEqual([{enabled:true,linkIds:['l0'],confirmed:true}])
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
 await captureScreenshot(page,{path:info.outputPath(`deep-settings-${width}-${theme}.png`)})
 await dialog.getByRole('button',{name:'删除记录',exact:true}).click();await expect(dialog).toContainText('已删除2个真实源文件');await captureScreenshot(page,{path:info.outputPath(`deep-records-${width}-${theme}.png`)})
 expect(f.unexpected).toEqual([])
})
test('deep delete rejected save remains editable with selected scope',async({page})=>{
 const f=await fixture(page);f.setReject(true);await page.goto('/tools');await page.getByRole('button',{name:'媒体深度删除',exact:true}).click()
 const dialog=page.getByRole('dialog',{name:'媒体深度删除',exact:true});await dialog.getByRole('switch').check();await dialog.getByRole('checkbox',{name:'联动 飞牛影视'}).check();await dialog.getByRole('button',{name:'保存配置'}).click();await page.getByRole('dialog',{name:'启用媒体深度删除',exact:true}).getByRole('button',{name:'确认启用'}).click()
 await expect(dialog.getByRole('alert')).toHaveText('媒体深度删除正在执行，请稍后修改配置');await expect(dialog.getByRole('alert')).toBeVisible();await expect(page.getByRole('dialog',{name:'启用媒体深度删除',exact:true})).toHaveCount(0);await expect(dialog.getByRole('checkbox',{name:'联动 飞牛影视'})).toBeChecked();expect(f.unexpected).toEqual([])
})
