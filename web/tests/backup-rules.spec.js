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
  await modal.getByRole('button',{name:'同名文件策略',exact:true}).click()
  await page.getByRole('option',{name:'覆盖同名文件',exact:true}).click()
  const dimensions=await modal.evaluate(el=>[el.querySelector('input').getBoundingClientRect().height,el.querySelector('.rounded-select-trigger').getBoundingClientRect().height])
  expect(dimensions[0]).toBe(dimensions[1])
  await modal.getByText('筛选规则',{exact:true}).click()
  await modal.getByLabel('包含扩展名',{exact:true}).fill('epub;pdf')
  await modal.getByLabel('排除名称关键词',{exact:true}).fill('临时;未完成')
  await modal.locator('.thin-scroll-area').evaluate(el=>el.scrollTop=0)
  await modal.evaluate(async el=>{await Promise.allSettled(el.getAnimations({subtree:true}).map(a=>a.finished))})
  await page.screenshot({path:info.outputPath('backup-editor.png')})
  await modal.getByRole('button',{name:'保存规则',exact:true}).click()
  await expect(page.locator('.backup-name')).toContainText('读物归档')
  await page.locator('.backup-name').click()
  await expect(page.getByRole('dialog',{name:'编辑备份规则'}).getByLabel('Cron 表达式')).toHaveValue('')
  await page.getByRole('button',{name:'保存规则',exact:true}).click()
  await page.getByRole('button',{name:'执行备份',exact:true}).click()
  await expect(page.getByRole('button',{name:'停止备份',exact:true})).toBeVisible()
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

test('offline download tabs keep a readable uniform font',async({page})=>{
  await setup(page)
  await page.route('**/api/state',route=>route.fulfill({json:{username:'offline-tabs',storages:[{id:'pool',name:'天翼',type:'tianyi',enabled:true,config:{username:'user',password:'masked'}}],tasks:[],settings:{},cache:{},traffic:{},logs:[]}}))
  await page.goto('/files')
  await page.getByRole('button',{name:'工具',exact:true}).click()
  await page.getByRole('menuitem',{name:'离线下载',exact:true}).click()
  for(const name of ['链接下载','BT 下载','CAS 秒传'])await expect(page.getByRole('button',{name,exact:true})).toHaveCSS('font-size','15px')
})
