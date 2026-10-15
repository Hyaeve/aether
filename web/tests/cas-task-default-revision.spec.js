import {test,expect} from '@playwright/test'

test('CAS new task excludes scraping and completion selectors align, existing inclusion is preserved',async({page})=>{
 await page.route('**/api/**',r=>{const u=new URL(r.request().url());let value={}
  if(u.pathname==='/api/auth/status')value={initialized:true,authenticated:true}
  else if(u.pathname==='/api/state')value={storages:[{id:'l',name:'本地',type:'local',enabled:true,config:{}}],tasks:[{id:'old',name:'已有CAS',kind:'cas',enabled:true,storageId:'l',source:'/',mode:'full'}],settings:{},cache:{}}
  else if(u.pathname==='/api/generation/jobs')value=[]
  return r.fulfill({json:value})
 })
 await page.goto('/tasks/cas');await page.getByRole('button',{name:'添加 CAS 任务卡片'}).click();await page.getByRole('button',{name:'更多选项',exact:true}).click()
 const completion=page.getByRole('button',{name:'完成动作',exact:true}),scrape=page.getByRole('button',{name:'STRM 刮削',exact:true})
 await expect(scrape).toHaveText('不参与');const a=await completion.boundingBox(),b=await scrape.boundingBox();expect(Math.abs(a.y-b.y)).toBeLessThan(1);expect(a.height).toBe(b.height)
 await page.keyboard.press('Escape');await page.getByRole('button',{name:'任务操作 已有CAS',exact:true}).click();await page.getByRole('button',{name:'编辑任务',exact:true}).click();await page.getByRole('button',{name:'更多选项',exact:true}).click();await expect(scrape).toHaveText('参与')
})
