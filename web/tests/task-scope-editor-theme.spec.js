import {test,expect} from '@playwright/test'
import {captureScreenshot} from './helpers/screenshot.js'

async function setup(page,{status='running'}={}){
  let task={id:'one',name:'正在生成',kind:'cas',storageId:'local',source:'/source',sourceLabel:'source',target:'/generated',casOperation:'generate',casInfoMode:'all',mode:'incremental',apiInterval:200,cron:'',status,enabled:true}
  const writes=[],unexpected=[]
  await page.route('**/api/**',route=>{
    const req=route.request(),path=new URL(req.url()).pathname
    if(path==='/api/auth/status')return route.fulfill({json:{initialized:true,authenticated:true}})
    if(path==='/api/state')return route.fulfill({json:{username:'task-scope-theme',storages:[{id:'local',name:'本地',type:'local',enabled:true,config:{}}],tasks:[task],settings:{},traffic:{},cache:{}}})
    if(path==='/api/tasks/one'&&req.method()==='PUT'){writes.push(req.postDataJSON());task={...task,...req.postDataJSON()};return route.fulfill({json:task})}
    if(path==='/api/generation/jobs'||path==='/api/transfers')return route.fulfill({json:[]})
    if(path==='/api/traffic'||path==='/api/strm-replace'||path==='/api/cron/validate')return route.fulfill({json:{}})
    unexpected.push(`${req.method()} ${path}`)
    return route.fulfill({status:500,json:{error:`Unmocked API: ${path}`}})
  })
  return {writes,unexpected,finish(){task={...task,status:'success'}},start(){task={...task,status:'running'}}}
}

for(const width of [1440,390])test(`running CAS editor is viewable and read-only ${width}`,async({page},info)=>{
  await page.clock.install()
  const fixture=await setup(page)
  await page.setViewportSize({width,height:950})
  await page.goto('/tasks/cas')
  await page.locator('.task-row-card').click()
  const dialog=page.getByRole('dialog',{name:'查看 CAS 任务',exact:true})
  await expect(dialog).toBeVisible()
  await expect(dialog.getByLabel('任务名称')).toHaveValue('正在生成')
  await expect(dialog.getByLabel('任务名称')).toBeDisabled()
  await expect(dialog.getByRole('button',{name:'CAS 信息',exact:true})).toBeDisabled()
  await expect(dialog.getByRole('button',{name:'选择目录',exact:true})).toBeDisabled()
  await dialog.getByRole('button',{name:'更多选项',exact:true}).click()
  await expect(dialog.getByRole('button',{name:'完成动作',exact:true})).toBeDisabled()
  await expect(dialog.getByRole('button',{name:'保存任务',exact:true})).toBeDisabled()
  await captureScreenshot(page,{path:info.outputPath(`readonly-cas-${width}.png`)})
  const closeButtons=dialog.getByRole('button',{name:'关闭',exact:true})
  await closeButtons.last().focus()
  await page.keyboard.press('Tab')
  await expect(closeButtons.first()).toBeFocused()
  await page.keyboard.press('Shift+Tab')
  await expect(closeButtons.last()).toBeFocused()
  await closeButtons.last().click()
  await page.getByRole('button',{name:'任务操作 正在生成',exact:true}).click()
  await page.getByRole('button',{name:'查看任务',exact:true}).click()
  await expect(dialog).toBeVisible()
  fixture.finish()
  await page.clock.fastForward(5000)
  await expect(page.locator('.task-row-card')).not.toHaveClass(/running/)
  await expect(dialog.getByRole('button',{name:'保存任务',exact:true})).toBeDisabled()
  await dialog.locator('form').evaluate(form=>form.requestSubmit())
  expect(fixture.writes).toEqual([])
  expect(fixture.unexpected).toEqual([])
})

test('an editor becomes permanently read-only when its task starts',async({page})=>{
  await page.clock.install()
  const fixture=await setup(page,{status:'idle'})
  await page.goto('/tasks/cas')
  await page.locator('.task-row-card').click()
  const dialog=page.getByRole('dialog')
  await expect(dialog.getByRole('button',{name:'保存任务',exact:true})).toBeEnabled()
  await dialog.getByRole('button',{name:'CAS 信息',exact:true}).click()
  await expect(page.getByRole('listbox')).toBeVisible()
  fixture.start()
  await page.clock.fastForward(5000)
  await expect(dialog).toHaveAttribute('aria-label','查看 CAS 任务')
  await expect(page.getByRole('listbox')).toHaveCount(0)
  await expect(dialog.getByLabel('任务名称')).toBeDisabled()
  await expect(dialog.getByRole('button',{name:'保存任务',exact:true})).toBeDisabled()
  await page.keyboard.press('Escape')
  await expect(dialog).toHaveCount(0)
  expect(fixture.writes).toEqual([])
  expect(fixture.unexpected).toEqual([])
})

for(const theme of ['light','dark'])test(`day blue surface hierarchy and night preservation ${theme}`,async({page},info)=>{
  await setup(page)
  await page.addInitScript(theme=>localStorage.setItem('aether-theme',theme),theme)
  await page.goto('/tasks/cas')
  await expect(page.locator('html')).toHaveAttribute('data-theme',theme)
  const colors=await page.evaluate(()=>{
    const root=getComputedStyle(document.documentElement),keys=['--bg','--surface','--input','--layer-shell','--layer-frame','--layer-item','--layer-rule']
    return Object.fromEntries(keys.map(k=>[k,root.getPropertyValue(k).trim()]))
  })
  if(theme==='light'){
    expect(colors).toEqual({'--bg':'#e9ecf5','--surface':'#f5f7fb','--input':'#fff','--layer-shell':'#edf0f7','--layer-frame':'#f3f5fa','--layer-item':'#fafbfe','--layer-rule':'#fff'})
    await expect(page.locator('.sidebar')).toHaveCSS('background-color','rgb(217, 224, 239)')
  }else{
    expect(colors['--bg']).toBe('#111216')
    expect(colors['--layer-rule']).toBe('#262831')
  }
  await captureScreenshot(page,{path:info.outputPath(`surfaces-${theme}.png`)})
})
