import { test, expect } from '@playwright/test'

async function setup(page) {
  let rules = []
  const stores = [{id:'source',name:'读物',type:'115',enabled:true,config:{}},{id:'target',name:'归档',type:'local',enabled:true,config:{}}]
  await page.route('**/api/**', async route => {
    const req=route.request(),path=new URL(req.url()).pathname
    let json={}
    if(path==='/api/auth/status')json={initialized:true,authenticated:true}
    else if(path==='/api/state')json={username:'backup-test',storages:stores,tasks:[],settings:{},cache:{},traffic:{},logs:[]}
    else if(path==='/api/files')json=[]
    else if(path==='/api/backup-rules'){
      if(req.method()==='POST')rules.push({...req.postDataJSON(),id:'rule',status:'idle',scanned:0,copied:0,skipped:0})
      json=req.method()==='GET'?rules:{ok:true}
    }else if(path.startsWith('/api/backup-rules/rule/')){rules[0].status=path.endsWith('/run')?'running':'stopped';json={ok:true}}
    else if(path==='/api/backup-rules/rule'){
      if(req.method()==='PUT')rules[0]={...req.postDataJSON(),id:'rule'}
      else if(req.method()==='DELETE')rules=[]
      json={ok:true}
    }else if(path==='/api/transfers')json=[]
    await route.fulfill({json})
  })
}

test('backup rules create, edit, run, stop, toggle and delete with aligned controls',async({page},info)=>{
  await setup(page);await page.goto('/transfer/backup')
  await page.getByRole('button',{name:'添加备份',exact:true}).click()
  const modal=page.getByRole('dialog',{name:'添加备份规则',exact:true})
  await modal.getByLabel('备份名称').fill('读物归档')
  for(const [button,store] of [['选择备份源目录','读物'],['选择备份目标目录','归档']]){
    await modal.getByRole('button',{name:button,exact:true}).click()
    const picker=page.getByRole('dialog',{name:'选择存储目录'})
    await picker.locator('.source-accounts button').filter({hasText:store}).click()
    await picker.getByRole('button',{name:'选择当前目录',exact:true}).click()
  }
  const inputHeight=await modal.locator('input').first().evaluate(el=>el.getBoundingClientRect().height)
  await modal.getByRole('button',{name:'2 备份规则',exact:true}).click()
  await modal.getByRole('button',{name:'同名文件策略',exact:true}).click()
  await page.getByRole('option',{name:'覆盖同名文件',exact:true}).click()
  expect(await modal.locator('.rounded-select-trigger').first().evaluate(el=>el.getBoundingClientRect().height)).toBe(inputHeight)
  await modal.getByRole('button',{name:'4 筛选规则',exact:true}).click()
  await modal.getByText('兼容筛选',{exact:true}).click()
  await modal.getByLabel('包含扩展名',{exact:true}).fill('epub;pdf')
  await modal.getByLabel('排除名称关键词',{exact:true}).fill('临时;未完成')
  await modal.locator('.thin-scroll-area').evaluate(el=>el.scrollTop=0)
  await modal.evaluate(async el=>{await Promise.allSettled(el.getAnimations({subtree:true}).map(a=>a.finished))})
  await page.screenshot({path:info.outputPath('backup-editor.png')})
  await modal.getByRole('button',{name:'保存规则',exact:true}).click()
  await expect(page.locator('.backup-name')).toContainText('读物归档')
  await page.locator('.backup-name').click()
  await page.getByRole('dialog',{name:'编辑备份规则'}).getByRole('button',{name:'3 扫描规则',exact:true}).click()
  await expect(page.getByRole('dialog',{name:'编辑备份规则'}).getByLabel('Cron 表达式')).toHaveValue('')
  await page.getByRole('button',{name:'保存规则',exact:true}).click()
  await page.getByRole('button',{name:'执行备份',exact:true}).click()
  await expect(page.getByRole('button',{name:'停止备份',exact:true})).toBeVisible()
  await expect(page.locator('.backup-card')).toHaveClass(/running/)
  await expect(page.locator('.backup-card')).toHaveCSS('animation-name','task-running')
  await expect(page.getByRole('button',{name:'停止备份',exact:true})).toHaveCSS('color','rgb(199, 93, 100)')
  await page.emulateMedia({reducedMotion:'reduce'})
  await expect(page.locator('.backup-card')).toHaveCSS('animation-name','none')
  await page.emulateMedia({reducedMotion:'no-preference'})
  await page.getByRole('button',{name:'停止备份',exact:true}).click()
  await page.getByRole('button',{name:'停用备份 读物归档',exact:true}).click()
  await expect(page.getByRole('button',{name:'执行备份',exact:true})).toBeDisabled()
  await page.getByRole('button',{name:'启用备份 读物归档',exact:true}).click()
  await page.screenshot({path:info.outputPath('backup-cards.png')})
  for(const width of [390,320]){
    await page.setViewportSize({width,height:740})
    expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1)).toBe(true)
    await expect(page.getByRole('button',{name:'添加备份',exact:true})).toBeVisible()
  }
  await page.evaluate(()=>document.documentElement.dataset.theme='dark')
  await page.waitForTimeout(5100)
  await page.screenshot({path:info.outputPath('backup-dark-mobile.png')})
  await page.getByRole('button',{name:'备份操作 读物归档',exact:true}).click()
  await page.getByRole('button',{name:'删除规则',exact:true}).click()
  await page.getByRole('dialog',{name:'删除备份规则'}).getByRole('button',{name:'确认',exact:true}).click()
  await expect(page.locator('.backup-card')).toHaveCount(0)
  await page.getByRole('link',{name:'传输任务',exact:true}).click()
  await expect.poll(async()=>page.locator('.transfer-modes').count()).toBe(1)
  await page.getByRole('link',{name:'备份规则',exact:true}).click()
  await expect(page.getByRole('button',{name:'添加备份',exact:true})).toBeVisible()
})

test('Nestify-style grouped backup editor supports multiple locations, filters, details and disabled copies',async({page},info)=>{
  await setup(page); await page.goto('/transfer/backup')
  await page.getByRole('button',{name:'添加备份',exact:true}).click()
  let modal=page.getByRole('dialog',{name:'添加备份规则',exact:true})
  await modal.getByLabel('备份名称').fill('多源归档')
  await modal.getByRole('button',{name:'添加源目录',exact:true}).click()
  await modal.getByRole('button',{name:'添加目标目录',exact:true}).click()
  await expect(modal.getByRole('button',{name:'保存规则',exact:true})).toBeDisabled()
  for(const [name,store] of [['选择备份源目录','读物'],['选择备份源目录 2','归档'],['选择备份目标目录','归档'],['选择备份目标目录 2','读物']]){
    await modal.getByRole('button',{name,exact:true}).click()
    const picker=page.getByRole('dialog',{name:'选择存储目录'})
    await picker.locator('.source-accounts button').filter({hasText:store}).click()
    await picker.getByRole('button',{name:'选择当前目录',exact:true}).click()
  }
  await modal.getByRole('button',{name:'3 扫描规则',exact:true}).click()
  await modal.getByLabel('自动扫描间隔',{exact:true}).fill('600')
  await expect(modal.getByLabel('Cron 表达式')).toHaveValue('')
  await modal.getByRole('button',{name:'4 筛选规则',exact:true}).click()
  await modal.getByRole('button',{name:'添加筛选规则',exact:true}).click()
  await modal.getByRole('button',{name:'筛选类型 1',exact:true}).click()
  await page.getByRole('option',{name:'扩展名',exact:true}).click()
  await modal.getByRole('button',{name:'筛选模式 1',exact:true}).click()
  await page.getByRole('option',{name:'白名单',exact:true}).click()
  await modal.getByLabel('匹配内容 1',{exact:true}).fill('mkv;mp4')
  await expect(modal.getByLabel('文件夹',{exact:true})).toBeDisabled()
  await modal.getByRole('button',{name:'添加筛选规则',exact:true}).click()
  await modal.getByRole('button',{name:'筛选类型 2',exact:true}).click()
  await page.getByRole('option',{name:'正则表达式',exact:true}).click()
  await modal.getByLabel('匹配内容 2',{exact:true}).fill('sample|trailer')
  await modal.locator('.thin-scroll-area').evaluate(el=>el.scrollTop=0)
  await modal.evaluate(async el=>{await Promise.allSettled(el.getAnimations({subtree:true}).filter(a=>a.effect.getTiming().iterations!==Infinity).map(a=>a.finished))})
  await page.screenshot({path:info.outputPath('backup-filter-editor.png')})
  const saved=page.waitForRequest(req=>req.method()==='POST' && new URL(req.url()).pathname==='/api/backup-rules')
  await modal.getByRole('button',{name:'保存规则',exact:true}).click()
  const payload=(await saved).postDataJSON()
  expect(payload.sources).toHaveLength(2); expect(payload.targets).toHaveLength(2)
  expect(payload.filters.map(f=>[f.type,f.mode])).toEqual([['extension','include'],['regex','exclude']])
  expect(payload.scanInterval).toBe(600); expect(payload.cron).toBe('')
  await expect(page.locator('.backup-tags')).toContainText('2 条筛选')
  await page.getByRole('button',{name:'备份操作 多源归档',exact:true}).click()
  await page.getByRole('button',{name:'执行详情',exact:true}).click()
  await expect(page.getByRole('dialog',{name:'备份执行详情'})).toContainText('已处理 / 复制总数')
  await page.getByRole('dialog',{name:'备份执行详情'}).getByRole('button',{name:'关闭',exact:true}).last().click()
  await page.getByRole('button',{name:'备份操作 多源归档',exact:true}).click()
  await page.getByRole('button',{name:'复制规则',exact:true}).click()
  modal=page.getByRole('dialog',{name:'添加备份规则',exact:true})
  await expect(modal.getByLabel('备份名称')).toHaveValue('多源归档 副本')
  for(const width of [1440,390,320]){
    await page.setViewportSize({width,height:800})
    await modal.getByRole('button',{name:'4 筛选规则',exact:true}).click()
    expect(await modal.evaluate(el=>el.scrollWidth<=el.clientWidth+1)).toBe(true)
    await page.evaluate(()=>document.documentElement.dataset.theme='dark')
  }
  await modal.evaluate(async el=>{await Promise.allSettled(el.getAnimations({subtree:true}).filter(a=>a.effect.getTiming().iterations!==Infinity).map(a=>a.finished))})
  await page.screenshot({path:info.outputPath('backup-filter-dark-mobile.png')})
  const copied=page.waitForRequest(req=>req.method()==='POST' && new URL(req.url()).pathname==='/api/backup-rules')
  await modal.getByRole('button',{name:'保存规则',exact:true}).click()
  expect((await copied).postDataJSON().enabled).toBe(false)
})

test('offline download tabs keep a readable uniform font',async({page})=>{
  await setup(page)
  await page.route('**/api/state',route=>route.fulfill({json:{username:'offline-tabs',storages:[{id:'pool',name:'天翼',type:'tianyi',enabled:true,config:{username:'user',password:'masked'}}],tasks:[],settings:{},cache:{},traffic:{},logs:[]}}))
  await page.goto('/files')
  await page.getByRole('button',{name:'工具',exact:true}).click()
  await page.getByRole('menuitem',{name:'离线下载',exact:true}).click()
  for(const name of ['链接下载','BT 下载','CAS 秒传'])await expect(page.getByRole('button',{name,exact:true})).toHaveCSS('font-size','15px')
})
