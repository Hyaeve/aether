import { test, expect } from '@playwright/test'

test('poster hover controls and directory scope preserve series groups', async ({page}, info) => {
  await page.route('**/api/auth/status',r=>r.fulfill({json:{initialized:true,authenticated:true}}))
  await page.route('**/api/state',r=>r.fulfill({json:{storages:[],tasks:[{id:'strm',kind:'strm',name:'媒体库'}],settings:{},cache:{},logs:[]}}))
  let submitted
  await page.route('**/api/strm-scrape/**',r=>{
    const action=new URL(r.request().url()).pathname.split('/').at(-1)
    if(action==='items')return r.fulfill({json:[
      {path:'Show/Season 1/01.strm',directories:['Show/Season 1','Show/Season 2'],title:'电视剧',kind:'tv',count:12,status:'ok',poster:'/aether.svg'},
      {path:'Movies/Arrival/a.strm',directories:['Movies/Arrival'],title:'电影',kind:'movie',count:1,status:'pending'}]})
    if(action==='identify')submitted=r.request().postDataJSON()
    if(action==='directories')return r.fulfill({json:{root:'/data/strm',directories: new URL(r.request().url()).searchParams.get('path')==='Show' ? ['Show/Season 1','Show/Season 2'] : ['Show','Movies']}})
    return r.fulfill({json:{running:false}})
  })
  await page.goto('/tasks/scrape')
  const card=page.locator('.scrape-card').first()
  await expect(card).toBeVisible()
  await page.mouse.move(0,0)
  await expect(card.locator('.scrape-actions')).toHaveCSS('opacity','0')
  await card.hover()
  await expect(card.locator('.scrape-actions')).toHaveCSS('opacity','1')
  await page.getByRole('button',{name:'筛选库目录',exact:true}).click()
  await page.getByRole('button',{name:'展开 Show',exact:true}).click()
  await expect(page.getByRole('checkbox',{name:'Show/Season 2',exact:true})).toBeChecked()
  await page.getByRole('checkbox',{name:'Movies',exact:true}).uncheck()
  await page.getByRole('button',{name:'确认范围',exact:true}).click()
  await expect(page.locator('.scrape-card')).toHaveCount(1)
  await page.getByRole('button',{name:'识别 STRM 库',exact:true}).click()
  expect(submitted).toBeUndefined()
  await page.getByRole('button',{name:'确认识别',exact:true}).click()
  expect(submitted.excludedScopes).toEqual(['Movies'])
  await page.screenshot({path:info.outputPath('scope-desktop.png')})
  await page.setViewportSize({width:390,height:844})
  await page.screenshot({path:info.outputPath('scope-mobile.png')})
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
})
