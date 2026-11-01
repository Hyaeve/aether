import {test,expect} from '@playwright/test'
import {captureScreenshot} from './helpers/screenshot.js'

async function fixture(page,mode='incremental_update',kind='strm'){
  const tasks=[{id:'mode-task',kind,name:'影视生成',storageId:'pool',source:'/Movies',sourceLabel:'影视',target:'/media/library',
    mode,enabled:true,status:'idle',mediaExtensions:'mkv;mp4',metadataExtensions:'nfo',apiInterval:200,cron:'',processed:0}]
  const writes=[],unexpected=[]
  await page.route('**/api/**',route=>{
    const r=route.request(),p=new URL(r.url()).pathname,method=r.method()
    let json
    if(p==='/api/auth/status')json={initialized:true,authenticated:true}
    else if(p==='/api/state')json={username:'strm-modes',storages:[{id:'pool',name:'本地影视',type:'local',enabled:true,config:{root:'/source'}}],tasks,settings:{},traffic:{},cache:{},strmRoot:'/media'}
    else if(p==='/api/tasks/mode-task'&&method==='PUT'){const body=r.postDataJSON();writes.push({p,method,body});Object.assign(tasks[0],body);json=tasks[0]}
    else if(p==='/api/tasks/mode-task/reset'&&method==='POST'){writes.push({p,method,body:r.postDataJSON()});json={ok:true}}
    else if(p==='/api/generation/jobs')json=[]
    else if(p==='/api/strm-replace')json={tasks:[]}
    else if(['/api/traffic','/api/plugins/proxy'].includes(p))json={}
    else if(p==='/api/cron/validate')json={valid:true}
    else{unexpected.push(method+' '+p);return route.fulfill({status:500,json:{error:'Unexpected API '+p}})}
    return route.fulfill({json})
  })
  return{tasks,writes,unexpected}
}

for(const [width,height,theme] of [[1440,1000,'light'],[390,844,'dark'],[375,812,'light'],[844,390,'dark']]){
  test(`STRM has two generation modes and defaults to update ${width}`,async({page},info)=>{
    const f=await fixture(page)
    await page.setViewportSize({width,height});await page.goto('/tasks/strm')
    await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
    await page.getByRole('button',{name:'添加任务',exact:true}).click()
    let dialog=page.getByRole('dialog',{name:'添加 STRM 任务',exact:true})
    const mode=()=>dialog.getByRole('button',{name:'生成方式',exact:true})
    await expect(mode()).toHaveText('增量更新')
    await expect(mode()).toHaveCSS('height','40px')
    await mode().click()
    await expect(page.getByRole('option')).toHaveCount(2)
    await expect(page.getByRole('option',{name:'增量更新',exact:true})).toBeVisible()
    await expect(page.getByRole('option',{name:'查漏补缺',exact:true})).toBeVisible()
    await expect(page.getByRole('option',{name:'全量生成',exact:true})).toHaveCount(0)
    await page.getByRole('option',{name:'增量更新',exact:true}).click()
    await captureScreenshot(page,{path:info.outputPath(`strm-modes-${width}-${theme}.png`)})
    await dialog.getByRole('button',{name:'取消',exact:true}).click()
    await page.locator('.task-row-copy').click()
    dialog=page.getByRole('dialog',{name:'编辑 STRM 任务',exact:true})
    await mode().click();await page.getByRole('option',{name:'查漏补缺',exact:true}).click()
    await dialog.getByRole('button',{name:'保存任务',exact:true}).click()
    await expect(dialog).toHaveCount(0)
    expect(f.writes[0].body.mode).toBe('incremental_missing')
    await page.locator('.task-row-copy').click()
    await expect(page.getByRole('dialog').getByRole('button',{name:'生成方式',exact:true})).toHaveText('查漏补缺')
    expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
    expect(f.unexpected).toEqual([])
  })
}

for(const [legacy,label] of [['incremental','查漏补缺'],['full','增量更新'],['','增量更新']]){
  test(`legacy STRM mode ${legacy||'empty'} has a canonical save value`,async({page})=>{
    const f=await fixture(page,legacy)
    await page.goto('/tasks/strm');await page.locator('.task-row-copy').click()
    const dialog=page.getByRole('dialog',{name:'编辑 STRM 任务',exact:true})
    await expect(dialog.getByRole('button',{name:'生成方式',exact:true})).toHaveText(label)
    await dialog.getByRole('button',{name:'保存任务',exact:true}).click()
    await expect(dialog).toHaveCount(0)
    expect(f.writes[0].body.mode).toBe(legacy==='incremental'?'incremental_missing':'incremental_update')
    expect(f.unexpected).toEqual([])
  })
}

test('full reset remains a separately confirmed menu action and preserves the saved mode',async({page})=>{
  const f=await fixture(page,'incremental_missing')
  await page.goto('/tasks/strm')
  await page.getByRole('button',{name:'任务操作 影视生成',exact:true}).click()
  await page.getByRole('button',{name:'全量重置',exact:true}).click()
  let dialog=page.getByRole('dialog',{name:'全量重置',exact:true})
  await expect(dialog).toContainText('不删除源文件')
  expect(f.writes).toEqual([])
  await dialog.getByRole('button',{name:'取消',exact:true}).click()
  expect(f.writes).toEqual([])
  await page.getByRole('button',{name:'任务操作 影视生成',exact:true}).click()
  await page.getByRole('button',{name:'全量重置',exact:true}).click()
  dialog=page.getByRole('dialog',{name:'全量重置',exact:true})
  await dialog.getByRole('button',{name:'确认重置',exact:true}).click()
  await expect(dialog).toHaveCount(0)
  expect(f.writes).toEqual([{p:'/api/tasks/mode-task/reset',method:'POST',body:{confirm:true}}])
  expect(f.tasks[0].mode).toBe('incremental_missing')
  expect(f.unexpected).toEqual([])
})

test('running STRM remains read-only and ED2K generation choices are unchanged',async({page})=>{
  const f=await fixture(page,'incremental_missing')
  f.tasks[0].status='running'
  await page.goto('/tasks/strm');await page.locator('.task-row-copy').click()
  await expect(page.getByRole('dialog',{name:'查看 STRM 任务',exact:true}).getByRole('button',{name:'生成方式',exact:true})).toBeDisabled()
  expect(f.writes).toEqual([])
  await page.getByRole('dialog').locator('.modal-footer').getByRole('button',{name:'关闭',exact:true}).click()
  f.tasks[0].kind='ed2k';f.tasks[0].mode='full';f.tasks[0].status='idle'
  await page.goto('/tasks/ed2k');await page.locator('.task-row-copy').click()
  await page.getByRole('dialog').getByRole('button',{name:'生成方式',exact:true}).click()
  await expect(page.getByRole('option',{name:'全量生成',exact:true})).toBeVisible()
  await expect(page.getByRole('option',{name:'增量生成',exact:true})).toBeVisible()
  expect(f.unexpected).toEqual([])
})
