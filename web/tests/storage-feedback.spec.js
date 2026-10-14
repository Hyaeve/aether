import {test,expect} from '@playwright/test'
test('cloud meters keep equal thickness and toggling a pool reports the resulting state',async({page},info)=>{
 const pools=['115','mobile','tianyi','tianyi'].map((type,i)=>({id:String(i),type,name:`存储 ${i}`,enabled:true,config:{},usage:{total:100,used:20+i*30}}))
 await page.route('**/api/**',r=>{
  const u=new URL(r.request().url());let value={}
  if(u.pathname==='/api/auth/status')value={initialized:true,authenticated:true}
  else if(u.pathname==='/api/state')value={username:'storage-feedback',storages:pools,tasks:[],settings:{},cache:{}}
  else if(u.pathname.endsWith('/usage'))value=pools.find(s=>u.pathname.includes('/'+s.id+'/'))?.usage||{}
  else if(r.request().method()==='PUT'){const input=r.request().postDataJSON();const pool=pools.find(s=>s.id===input.id);Object.assign(pool,input)}
  return r.fulfill({json:value})
 })
 await page.goto('/storage');const tracks=page.locator('.storage-quota-track')
 await expect(tracks).toHaveCount(4)
 for(const width of [1440,390]){
  await page.setViewportSize({width,height:1000})
  for(const track of await tracks.all()){await expect(track).toHaveCSS('height','4px');expect((await track.boundingBox()).height).toBe(4)}
 }
 const toggle=page.getByRole('button',{name:'停用存储池 存储 0',exact:true})
 await toggle.click();await expect(page.locator('.toast').filter({hasText:'已停用存储池 存储 0'})).toBeVisible()
 await page.getByRole('button',{name:'启用存储池 存储 0',exact:true}).click();await expect(page.locator('.toast').filter({hasText:'已启用存储池 存储 0'})).toBeVisible()
 await page.screenshot({path:info.outputPath('storage-feedback-mobile.png')})
})
