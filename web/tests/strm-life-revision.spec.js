import {test,expect} from '@playwright/test'
import {captureScreenshot} from './helpers/screenshot.js'

async function fixture(page){
  const writes=[],unexpected=[]
  const storages=[{id:'pan',name:'115影视',type:'115',enabled:true,config:{}},{id:'local',name:'本地',type:'local',enabled:true,config:{root:'/media'}},{id:'q',name:'夸克影视',type:'quark',enabled:true,config:{}},{id:'m',name:'移动影视',type:'mobile',enabled:true,config:{mode:'native'}}]
  const tasks=[{id:'life',name:'剧集监控',kind:'strm',storageId:'pan',source:'10',sourceLabel:'剧集',target:'/output',mode:'incremental_update',enabled:true,status:'idle',processed:0,mediaExtensions:'mkv',metadataExtensions:'nfo',apiInterval:200,cron:''}]
  const targets=[{id:'q1',provider:'quark',storageId:'q',parent:'parent',path:'/影视',shareTransfer:true},{id:'q2',provider:'quark',storageId:'q',parent:'next',path:'/归档',shareTransfer:true},{id:'m1',provider:'mobile',storageId:'m',parent:'mobile',path:'/移动',shareTransfer:true}]
  const settings={address:'http://localhost:15151',format:'pickcode_name',content:'original',overwriteMode:'skip'}
  const cards=provider=>[{id:'existing',url:provider==='quark'?'https://pan.quark.cn/s/old':'https://yun.139.com/shareweb/#/w/i/old',name:'旧分享',directory:'/output',directoryName:'Series',enabled:false,result:{status:'idle'}}]
  let rejectDirectory=true
  await page.route('**/api/**',route=>{
    const r=route.request(),u=new URL(r.url()),p=u.pathname,method=r.method()
    let json
    if(p==='/api/auth/status')json={initialized:true,authenticated:true}
    else if(p==='/api/state')json={username:'life-revision',storages,tasks,settings:{},cache:{},traffic:{},fileVisits:[],fileFavorites:[],strmRoot:'/media'}
    else if(['/api/traffic','/api/plugins/proxy'].includes(p))json={}
    else if(p==='/api/strm-replace')json={tasks:[]}
    else if(p==='/api/cron/validate')json={valid:true,error:''}
    else if(p==='/api/strm/settings'){if(method==='PUT'){writes.push({p,body:r.postDataJSON()});Object.assign(settings,r.postDataJSON())}json=settings}
    else if(p==='/api/tasks/life'&&method==='PUT'){writes.push({p,body:r.postDataJSON()});Object.assign(tasks[0],r.postDataJSON());json={ok:true}}
    else if(p==='/api/tasks/life/sync'){writes.push({p,body:r.postDataJSON()});json={ok:true}}
    else if(p==='/api/notification-service')json={telegram:{userIds:[]},targets}
    else if(p==='/api/share-transfers')json=[]
    else if(p==='/api/share-live'){if(method==='POST'){writes.push({p,body:r.postDataJSON()});json={id:'new-live'}}else json=[]}
    else if(p==='/api/share-live/resolve')json={name:'分享原名称'}
    else if(/^\/api\/virtual-library\/(mobile|quark)\/resolve$/.test(p))json={name:'Series'}
    else if(/^\/api\/virtual-library\/(mobile|quark)$/.test(p)){
      if(method==='POST'){writes.push({p,body:r.postDataJSON()});if(rejectDirectory){rejectDirectory=false;return route.fulfill({status:409,json:{error:'保存目录名称已存在，请修改目录名称',field:'directoryName'}})}json={id:'created'}}
      else json={cards:cards(p.split('/').at(-1)),settings:{storageIds:[p.endsWith('quark')?'q':'m'],directory:'/output',invalidDays:15,retentionHours:12,mediaExtensions:'mkv',metadataExtensions:'nfo'},stats:{}}
    }
    else if(p==='/api/files')json=[{id:'/film.mp4',name:'film.mp4',size:5},{id:'/second.mkv',name:'second.mkv',size:5}]
    else if(p==='/api/files/cas-export')json={directory:''}
    else if(p==='/api/files/cas-export/jobs'){writes.push({p,body:r.postDataJSON()});json={id:'export',status:'queued'}}
    else if(p==='/api/files/cas-export/jobs/export')json={id:'export',status:'running',processed:0,total:1,download:false}
    else {unexpected.push(method+' '+p);return route.fulfill({status:500,json:{error:'Unexpected API '+p}})}
    return route.fulfill({json})
  })
  return{writes,unexpected,tasks,settings}
}

for(const [width,height,theme] of [[1440,1000,'light'],[390,844,'dark'],[844,390,'dark']]){
  test(`115 life/tree source and cascade controls ${width}`,async({page},info)=>{
    const f=await fixture(page)
    await page.setViewportSize({width,height});await page.goto('/tasks/strm')
    await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
    await page.locator('.task-row-copy').click()
    const dialog=page.getByRole('dialog',{name:'编辑 STRM 任务',exact:true})
    await expect(dialog.getByRole('button',{name:'生成方式',exact:true})).toHaveText('生活事件')
    await dialog.getByRole('button',{name:'生成方式',exact:true}).click()
    await expect(page.getByRole('option')).toHaveCount(2)
    await page.getByRole('option',{name:'目录树',exact:true}).click()
    await expect(dialog.getByRole('textbox',{name:'Cron 表达式',exact:true})).toHaveAttribute('required','')
    await dialog.getByRole('textbox',{name:'Cron 表达式',exact:true}).fill('*/15 * * * *')
    await dialog.getByRole('button',{name:'更多选项',exact:true}).click()
    await dialog.getByRole('switch',{name:'级联删除',exact:true}).check()
    const extensions=dialog.getByRole('textbox',{name:'级联删除扩展名',exact:true})
    await expect(extensions).toHaveValue('jpg;jpeg;png;webp;nfo;srt;ass;ssa;sub;vtt')
    await extensions.scrollIntoViewIfNeeded()
    await expect.poll(()=>extensions.evaluate(el=>{const r=el.getBoundingClientRect(),modal=el.closest('.modal'),header=modal.querySelector('.modal-header').getBoundingClientRect(),footer=modal.querySelector('.modal-footer').getBoundingClientRect();return r.top>=header.bottom&&r.bottom<=footer.top})).toBe(true)
    await captureScreenshot(page,{path:info.outputPath(`life-editor-${width}-${theme}.png`)})
    await dialog.getByRole('button',{name:'保存任务',exact:true}).click()
    await expect(dialog).toHaveCount(0)
    expect(f.writes.at(-1).body).toMatchObject({strmSource:'tree',cascadeDelete:true,cron:'*/15 * * * *'})
    expect(f.unexpected).toEqual([])
  })
}

test('full sync is above reset, confirmed and does not clear the library first',async({page})=>{
  const f=await fixture(page);await page.goto('/tasks/strm')
  await page.getByRole('button',{name:'任务操作 剧集监控',exact:true}).click()
  const labels=await page.locator('.task-menu>button').allTextContents()
  expect(labels.findIndex(v=>v.includes('全量同步'))).toBeLessThan(labels.findIndex(v=>v.includes('全量重置')))
  await page.getByRole('button',{name:'全量同步',exact:true}).click()
  const dialog=page.getByRole('dialog',{name:'全量同步',exact:true})
  await expect(dialog).toContainText('不会先清空生成库')
  expect(f.writes).toEqual([])
  await dialog.getByRole('button',{name:'确认同步',exact:true}).click()
  await expect(dialog).toHaveCount(0)
  expect(f.writes).toEqual([{p:'/api/tasks/life/sync',body:{confirm:true}}])
  expect(f.unexpected).toEqual([])
})

test('STRM content defaults to original names and encoded paths save explicitly',async({page},info)=>{
  const f=await fixture(page);await page.goto('/settings/strm')
  const control=page.getByRole('button',{name:'STRM内容',exact:true})
  await expect(control).toHaveText('原始名称')
  await control.click();await page.getByRole('option',{name:'编码路径',exact:true}).click()
  await expect.poll(()=>f.settings.content).toBe('encoded')
  await captureScreenshot(page,{path:info.outputPath('strm-content.png')})
  expect(f.unexpected).toEqual([])
})

for(const provider of ['quark','mobile']){
  test(`manual ${provider} directory collisions stay in editor and mark only the directory field`,async({page},info)=>{
    const f=await fixture(page)
    await page.setViewportSize({width:390,height:900});await page.goto('/virtual/'+provider)
    await page.getByRole('button',{name:'添加链接',exact:true}).click()
    const dialog=page.getByRole('dialog',{name:provider==='quark'?'添加夸克链接':'添加移动链接',exact:true})
    await dialog.getByRole('textbox',{name:'分享链接',exact:true}).fill(provider==='quark'?'https://pan.quark.cn/s/new':'https://yun.139.com/shareweb/#/w/i/new')
    await dialog.getByRole('textbox',{name:'分享名称',exact:true}).focus()
    const directory=dialog.getByRole('textbox',{name:'目录名称',exact:true})
    await expect(directory).toHaveValue('Series')
    await expect(directory).toHaveAttribute('aria-invalid','true')
    await expect(dialog.getByRole('button',{name:'保存链接',exact:true})).toBeDisabled()
    await directory.fill('Available');await dialog.getByRole('button',{name:'保存链接',exact:true}).click()
    await expect(directory).toHaveAttribute('aria-invalid','true')
    await expect(dialog).toBeVisible()
    await captureScreenshot(page,{path:info.outputPath(`collision-${provider}.png`)})
    await directory.fill('Unique');await dialog.getByRole('button',{name:'保存链接',exact:true}).click()
    await expect(dialog).toHaveCount(0)
    expect(f.writes).toHaveLength(2)
    expect(f.unexpected).toEqual([])
  })
}

for(const format of ['encoded','plain']){
  test(`CAS ${format} export has back and two distinct actions`,async({page},info)=>{
    const f=await fixture(page)
    await page.setViewportSize({width:390,height:844});await page.goto('/files?storage=local')
    await page.getByRole('button',{name:'film.mp4',exact:true}).click({button:'right'});await page.keyboard.press('Escape')
    await page.getByRole('button',{name:'工具',exact:true}).click();await page.getByRole('menuitem',{name:'导出CAS',exact:true}).click()
    let dialog=page.getByRole('dialog',{name:'导出CAS',exact:true})
    await expect(dialog.getByRole('button',{name:'返回',exact:true})).toBeVisible()
    await dialog.getByRole('button',{name:'返回',exact:true}).click()
    const scope=page.getByRole('dialog',{name:'CAS导出范围',exact:true})
    await expect(scope.getByRole('checkbox',{name:'film.mp4',exact:true})).toBeChecked()
    await expect(scope.getByRole('checkbox',{name:'second.mkv',exact:true})).not.toBeChecked()
    await scope.getByRole('button',{name:'确认范围',exact:true}).click()
    dialog=page.getByRole('dialog',{name:'导出CAS',exact:true})
    const encoded=await dialog.getByRole('button',{name:'编码导出',exact:true}).boundingBox(),plain=await dialog.getByRole('button',{name:'明文导出',exact:true}).boundingBox()
    expect(encoded.x).toBeLessThan(plain.x)
    await captureScreenshot(page,{path:info.outputPath(`cas-export-${format}.png`)})
    await dialog.getByRole('button',{name:format==='encoded'?'编码导出':'明文导出',exact:true}).click()
    await page.getByRole('dialog',{name:'确认导出CAS',exact:true}).getByRole('button',{name:'确认导出',exact:true}).click()
    await expect(dialog).toHaveCount(0)
    expect(f.writes.at(-1).body).toMatchObject({format,confirm:true,ids:['/film.mp4']})
    expect(f.unexpected).toEqual([])
  })
}

test('active links allow manual add with an immutable resolved share name',async({page},info)=>{
  const f=await fixture(page);await page.goto('/notifications/transfers')
  await page.getByRole('button',{name:'活动链接 /影视',exact:true}).click()
  await page.getByRole('dialog',{name:'活动链接',exact:true}).getByRole('button',{name:'添加链接',exact:true}).click()
  const dialog=page.getByRole('dialog',{name:'添加分享链接',exact:true})
  await expect(dialog.getByRole('textbox',{name:'活链分享名称',exact:true})).toHaveAttribute('readonly','')
  await dialog.getByRole('textbox',{name:'活链分享链接',exact:true}).fill('https://pan.quark.cn/s/new')
  await dialog.getByRole('textbox',{name:'活链目录名称',exact:true}).focus()
  await expect(dialog.getByRole('textbox',{name:'活链分享名称',exact:true})).toHaveValue('分享原名称')
  await expect(dialog.getByRole('textbox',{name:'活链目录名称',exact:true})).toHaveValue('分享原名称')
  await captureScreenshot(page,{path:info.outputPath('live-add.png')})
  await dialog.getByRole('button',{name:'保存',exact:true}).click()
  await expect(dialog).toHaveCount(0)
  expect(f.writes.at(-1)).toMatchObject({p:'/api/share-live',body:{name:'分享原名称',targetId:'q1',cron:''}})
  expect(f.unexpected).toEqual([])
})
