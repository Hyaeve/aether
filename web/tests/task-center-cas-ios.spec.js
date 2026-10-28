import {test,expect} from '@playwright/test'
import {captureScreenshot} from './helpers/screenshot.js'

async function fixture(page){
  let poll=0
  const tasks=[{id:'cas',name:'现有CAS',kind:'cas',storageId:'local',source:'/',mode:'full',enabled:true,status:'idle'}]
  await page.route('**/api/**',route=>{
    const p=new URL(route.request().url()).pathname
    let json={}
    if(p==='/api/auth/status')json={initialized:true,authenticated:true}
    else if(p==='/api/state')json={username:'task-center',storages:[{id:'local',name:'本地',type:'local',enabled:true,config:{}},{id:'mobile',name:'移动',type:'mobile',enabled:true,config:{}}],tasks,settings:{},traffic:{},cache:{}}
    else if(p==='/api/tasks/panel')json={items:[{id:'task:one',taskId:'one',source:'task',kind:'cas',name:'实时CAS',status:'running',message:'正在计算',processed:++poll,unit:'项',storageId:'local'},{id:'generation:one',source:'generation',kind:'ed2k',name:'解析队列',status:'queued',message:'等待执行',processed:0,total:10,totalKnown:true,unit:'项'},...['strm','cache','scrape','automation'].map(kind=>({id:`${kind}:one`,source:kind==='scrape'||kind==='automation'?kind:'task',kind,name:`实时${kind}`,status:'running',message:kind==='cache'?'正在缓存目录':'正在执行',processed:5,total:kind==='scrape'?20:0,totalKnown:kind==='scrape',unit:'项'}))]}
    else if(p==='/api/generation/jobs')json=[]
    else if(p==='/api/files')json=[]
    else if(p==='/api/tasks'&&route.request().method()==='POST'){const body=route.request().postDataJSON();tasks.push({...body,id:'new',status:'idle'});json=tasks.at(-1)}
    else if(p==='/api/tasks/cas'&&route.request().method()==='PUT'){Object.assign(tasks[0],route.request().postDataJSON());json={ok:true}}
    return route.fulfill({json})
  })
}

for(const width of [1440,390])for(const theme of ['light','dark'])test(`task center panel is first and refreshes live tasks ${width} ${theme}`,async({page},info)=>{
  await fixture(page)
  await page.setViewportSize({width,height:900})
  await page.goto('/tasks')
  await page.evaluate(theme=>document.documentElement.dataset.theme=theme,theme)
  await expect(page.locator('.breadcrumbs')).toContainText('任务中心')
  const tabs=page.getByRole('navigation',{name:'任务栏目'})
  await expect(tabs.getByRole('link').first()).toHaveText('任务面板')
  await expect(page.getByText('实时CAS',{exact:true})).toBeVisible()
  await expect(page.getByText('解析队列',{exact:true})).toBeVisible()
  await expect(page.locator('.task-panel-group')).toHaveCount(6)
  await expect.poll(async()=>Number(await page.locator('[data-id="task:one"]').getAttribute('data-processed'))).toBeGreaterThanOrEqual(2)
  await expect(page.locator('[data-id="scrape:one"] [role="progressbar"]')).toHaveAttribute('aria-valuenow','25')
  await expect(page.locator('[data-id="task:one"] [role="progressbar"]')).not.toHaveAttribute('aria-valuenow')
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
  await captureScreenshot(page,{path:info.outputPath(`task-panel-${width}-${theme}.png`)})
  await page.getByRole('textbox',{name:'搜索运行任务'}).fill('不会匹配的任务')
  await expect(page.getByText('没有匹配的运行任务',{exact:true})).toBeVisible()
})

test('CAS uses feature selector, auto-selects mobile source and preserves manual override',async({page})=>{
  await fixture(page)
  await page.goto('/tasks/cas')
  await page.getByRole('button',{name:'添加 CAS 任务卡片',exact:true}).click()
  await expect(page.getByRole('button',{name:'生成方式',exact:true})).toHaveCount(0)
  const mode=page.getByRole('button',{name:'CAS 信息',exact:true})
  await expect(mode).toHaveText('全计算')
  await page.getByRole('button',{name:'选择目录',exact:true}).click()
  const picker=page.getByRole('dialog',{name:'选择存储目录',exact:true})
  await picker.locator('.source-accounts').getByRole('button',{name:'移动 移动云盘',exact:true}).click()
  await picker.getByRole('button',{name:'选择当前目录',exact:true}).click()
  await expect(mode).toHaveText('移动CAS')
  await mode.click()
  await page.getByRole('option',{name:'天翼CAS',exact:true}).click()
  await expect(mode).toHaveText('天翼CAS')
  await page.getByRole('textbox',{name:'任务名称 *',exact:true}).fill('移动默认测试')
  const saved=page.waitForRequest(r=>new URL(r.url()).pathname==='/api/tasks'&&r.method()==='POST')
  await page.getByRole('button',{name:'保存任务',exact:true}).click()
  const body=(await saved).postDataJSON()
  expect([body.mode,body.casInfoMode]).toEqual(['incremental','tianyi'])
  await page.getByText('移动默认测试',{exact:true}).click()
  await expect(mode).toHaveText('天翼CAS')
})

test('mobile topbar begins below the complete top safe area',async({page},info)=>{
  await fixture(page)
  await page.setViewportSize({width:390,height:844})
  await page.goto('/tasks/cas')
  await page.evaluate(()=>document.documentElement.style.setProperty('--aether-safe-top','47px'))
  await expect.poll(()=>page.locator('.topbar').evaluate(el=>el.getBoundingClientRect().top)).toBe(47)
  const rect=await page.locator('.topbar').boundingBox()
  expect(rect.height).toBe(48)
  for(const button of await page.locator('.topbar button').all()){
    if(await button.isVisible())expect((await button.boundingBox()).y).toBeGreaterThanOrEqual(47)
  }
  expect((await page.locator('.page-scroll').boundingBox()).y).toBe(95)
  await captureScreenshot(page,{path:info.outputPath('safe-top-47.png')})
})

test('panel polling stops on navigation and does not restore late responses',async({page})=>{
  await fixture(page)
  let release,started=0
  const waiting=new Promise(resolve=>release=resolve)
  await page.route('**/api/tasks/panel',async route=>{
    started++
    await waiting
    await route.fulfill({json:{items:[{id:'late',kind:'cas',source:'task',name:'迟到状态',status:'running'}]}}).catch(()=>{})
  })
  await page.clock.install()
  await page.goto('/tasks/panel')
  await expect.poll(()=>started).toBe(1)
  await page.clock.fastForward(5000)
  expect(started).toBe(1)
  await page.getByRole('link',{name:'CAS 任务',exact:true}).click()
  release()
  await expect(page.getByRole('button',{name:'添加 CAS 任务卡片',exact:true})).toBeVisible()
  await page.clock.fastForward(5000)
  expect(started).toBe(1)
  await expect(page.getByText('迟到状态',{exact:true})).toHaveCount(0)
})

test('task panel reports a read error then refreshes to empty state',async({page})=>{
  await fixture(page)
  let failed=true
  await page.route('**/api/tasks/panel',route=>route.fulfill({status:failed?503:200,json:failed?{error:'任务状态暂不可用'}:{items:[]}}))
  await page.goto('/tasks/panel')
  await expect(page.getByRole('alert')).toHaveText('任务状态暂不可用')
  failed=false
  await page.getByRole('button',{name:'刷新任务面板',exact:true}).click()
  await expect(page.getByText('暂无正在进行的任务',{exact:true})).toBeVisible()
  await expect(page.getByRole('alert')).toHaveCount(0)
})

test('a stalled task panel request times out and allows another refresh',async({page})=>{
  await fixture(page)
  let release,calls=0
  const waiting=new Promise(resolve=>release=resolve)
  await page.route('**/api/tasks/panel',async route=>{
    calls++
    if(calls===1)await waiting
    await route.fulfill({json:{items:[]}}).catch(()=>{})
  })
  await page.clock.install()
  await page.clock.pauseAt(new Date())
  await page.goto('/tasks/panel')
  await expect.poll(()=>calls).toBe(1)
  await page.clock.runFor(10001)
  await expect(page.getByRole('alert')).toHaveText('任务状态读取超时')
  await page.getByRole('button',{name:'刷新任务面板',exact:true}).click()
  await expect(page.getByText('暂无正在进行的任务',{exact:true})).toBeVisible()
  expect(calls).toBe(2)
  release()
})
