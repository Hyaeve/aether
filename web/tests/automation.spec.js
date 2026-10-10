import { test, expect } from '@playwright/test'

async function setup(page) {
  const tasks=[{id:'generate',name:'生成媒体库',kind:'strm',storageId:'local',enabled:true}]
  let rules=[]
  await page.route('**/api/**',async route=>{
    const req=route.request(), path=new URL(req.url()).pathname
    let json={}
    if(path==='/api/auth/status')json={initialized:true,authenticated:true}
    else if(path==='/api/state')json={username:'automation',storages:[{id:'local',type:'local',name:'本地',enabled:true,config:{root:'/media'}}],tasks,settings:{},cache:{},traffic:{},logs:[]}
    else if(path==='/api/files')json=[]
    else if(path==='/api/automations'){
      if(req.method()==='POST')rules.push({...req.postDataJSON(),id:'rule',status:'idle'})
      json=req.method()==='GET'?rules:{ok:true}
    }else if(path.startsWith('/api/automations/rule/')){rules[0].status=path.endsWith('/run')?'running':'cancelled';json={ok:true}}
    else if(path==='/api/automations/rule'){if(req.method()==='PUT')rules[0]={...req.postDataJSON(),id:'rule'};else if(req.method()==='DELETE')rules=[];json={ok:true}}
    await route.fulfill({json})
  })
}
test('link rules create ordered actions, edit, run, stop and delete',async({page},info)=>{
  await setup(page);await page.goto('/tasks/automation')
  await page.getByRole('button',{name:'添加联动',exact:true}).click()
  const modal=page.getByRole('dialog',{name:'添加联动',exact:true})
  await modal.getByLabel('联动名称').fill('媒体更新联动')
  await modal.getByRole('button',{name:'动作任务',exact:true}).click()
  await page.getByRole('option',{name:'生成媒体库',exact:true}).click()
  await modal.getByRole('button',{name:'添加动作',exact:true}).click()
  await modal.getByRole('button',{name:'联动动作',exact:true}).nth(1).click()
  await page.getByRole('option',{name:'刷新目录缓存',exact:true}).click()
  await expect(page.getByRole('listbox')).toHaveCount(0)
  await modal.evaluate(async el=>{await Promise.allSettled(el.getAnimations({subtree:true}).map(a=>a.finished))})
  await page.screenshot({path:info.outputPath('automation-editor.png')})
  await modal.getByRole('button',{name:'保存联动',exact:true}).click()
  await expect(page.locator('.automation-summary')).toContainText('媒体更新联动')
  await page.locator('.automation-summary').click()
  await expect(page.getByRole('dialog',{name:'编辑联动'}).getByLabel('联动名称')).toHaveValue('媒体更新联动')
  await page.getByRole('button',{name:'保存联动',exact:true}).click()
  await page.getByRole('button',{name:'执行联动',exact:true}).click()
  await expect(page.getByRole('button',{name:'停止联动',exact:true})).toBeVisible()
  await page.getByRole('button',{name:'停止联动',exact:true}).click()
  await page.getByRole('button',{name:'联动操作',exact:true}).click()
  await page.getByRole('menuitem',{name:'删除',exact:true}).click()
  await page.getByRole('dialog',{name:'删除联动'}).getByRole('button',{name:'确认',exact:true}).click()
  await expect(page.locator('.automation-row')).toHaveCount(0)
})
test('overflow tabs scroll without moving the page and preserve the right actions',async({page},info)=>{
  await setup(page);await page.setViewportSize({width:900,height:600});await page.goto('/tasks/strm')
  const nav=page.getByRole('navigation',{name:'任务栏目'})
  await expect(page.getByRole('button',{name:'添加任务',exact:true})).toBeVisible()
  expect(await nav.evaluate(el=>el.scrollWidth>el.clientWidth)).toBe(true)
  await nav.hover();await page.mouse.wheel(0,800)
  await expect.poll(()=>nav.evaluate(el=>el.scrollLeft)).toBeGreaterThan(0)
  const scroll=await page.locator('.page-scroll').count()?page.locator('.page-scroll'):page.locator('.page-content')
  const before=await scroll.evaluate(el=>el.scrollTop)
  await page.mouse.wheel(0,2000);await page.waitForTimeout(150)
  expect(await scroll.evaluate(el=>el.scrollTop)).toBe(before)
  await expect(nav).toHaveCSS('scrollbar-width','none')
  await nav.getByRole('link',{name:'联动任务',exact:true}).click()
  await expect(page.getByRole('button',{name:'添加联动',exact:true})).toBeVisible()
  await page.screenshot({path:info.outputPath('automation-overflow.png')})
  for(const width of [390,320]){
    await page.setViewportSize({width,height:700})
    expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1)).toBe(true)
    await expect(page.getByRole('button',{name:'添加联动',exact:true})).toBeVisible()
  }
})
test('share menu is present only for supported providers',async({page})=>{
  await setup(page)
  for(const [type,mode,supported] of [['local','',false],['webdav','',false],['openlist','',false],['tianyi','',false],['115','',true],['quark','',true],['mobile','native',true],['mobile','openlist',false]]){
    await page.route('**/api/state',route=>route.fulfill({json:{username:`menu-${type}-${mode}`,storages:[{id:'pool',name:type,type,enabled:true,config:{mode}}],tasks:[],settings:{},logs:[],cache:{},traffic:{}}}))
    await page.goto('/files');await page.getByRole('button',{name:'工具',exact:true}).click()
    await expect(page.getByRole('menuitem',{name:'分享转存',exact:true})).toHaveCount(supported?1:0)
  }
})
