import {test,expect} from '@playwright/test'
import {captureScreenshot} from './helpers/screenshot.js'

async function fixture(page,{held=false}={}){
  const writes=[],unexpected=[]
  const task={id:'sync',name:'影视同步',kind:'strm',storageId:'local',source:'/',sourceLabel:'影片',target:'/output',mode:'incremental_update',enabled:true,status:held?'delete-paused':'idle',processed:0,mediaExtensions:'mkv',metadataExtensions:'nfo',apiInterval:200,cron:'*/15 * * * *'}
  if(held)Object.assign(task,{strmDeleteState:{id:'guard',state:'waiting',count:1001,retryAt:'2026-11-02T13:19:00+08:00'},message:'删除超限，源存储健康检查未通过，任务暂停',healthPause:'存储池健康异常，暂时停止'})
  let finishCheck
  const checkDone=new Promise(resolve=>{finishCheck=resolve})
  await page.route('**/api/**',async route=>{
    const r=route.request(),p=new URL(r.url()).pathname
    let json
    if(p==='/api/auth/status')json={initialized:true,authenticated:true}
    else if(p==='/api/state')json={username:'delete-guard',storages:[{id:'local',name:'本地影音',type:'local',enabled:true,config:{root:'/media'}}],tasks:[task],settings:{},cache:{},traffic:{},fileVisits:[],fileFavorites:[],strmRoot:'/media'}
    else if(['/api/traffic','/api/plugins/proxy'].includes(p))json={}
    else if(p==='/api/strm-replace')json={tasks:[]}
    else if(p==='/api/cron/validate')json={valid:true,error:''}
    else if(p==='/api/tasks/sync'&&r.method()==='PUT'){writes.push({p,body:r.postDataJSON()});Object.assign(task,r.postDataJSON());json={ok:true}}
    else if(p==='/api/tasks/sync/check'){
      writes.push({p});task.strmDeleteState.state='checking';task.message='删除超限，正在检查源存储'
      await checkDone;json={ok:true}
    }
    else {unexpected.push(r.method()+' '+p);return route.fulfill({status:500,json:{error:'Unexpected API '+p}})}
    await route.fulfill({json})
  })
  return{task,writes,unexpected,finishCheck}
}

for(const [width,height,theme] of [[1440,1000,'light'],[390,844,'dark'],[375,812,'light'],[844,390,'dark']]){
  test(`delete threshold default, half column and save ${width}`,async({page},info)=>{
    const f=await fixture(page)
    await page.setViewportSize({width,height});await page.goto('/tasks/strm')
    await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
    await page.locator('.task-row-copy').click()
    const dialog=page.getByRole('dialog',{name:'编辑 STRM 任务',exact:true})
    await dialog.getByRole('button',{name:'更多选项',exact:true}).click()
    const input=dialog.getByRole('spinbutton',{name:'删除阈值',exact:true})
    await expect(input).toHaveValue('1000')
    await expect(input).toHaveAttribute('min','0')
    await expect(input).toHaveAttribute('step','1')
    await expect(dialog.locator('.delete-threshold-field .number-unit')).toHaveText('条')
    await dialog.locator('.delete-threshold-field').scrollIntoViewIfNeeded()
    if(await page.evaluate(()=>matchMedia('(hover:none)').matches)){
      const unit=dialog.locator('.delete-threshold-field .number-unit')
      await expect(unit).toHaveCSS('opacity','1')
      expect(await unit.evaluate(el=>{const a=el.getBoundingClientRect(),b=el.parentElement.querySelector('.number-steppers').getBoundingClientRect();return a.right<=b.left})).toBe(true)
    }
    const layout=await dialog.locator('.delete-threshold-field').evaluate(el=>{
      const rect=el.getBoundingClientRect(),grid=el.parentElement.getBoundingClientRect()
      const modal=el.closest('.modal'),header=modal.querySelector('.modal-header').getBoundingClientRect(),footer=modal.querySelector('.modal-footer').getBoundingClientRect()
      return{ratio:rect.width/grid.width,inside:rect.top>=header.bottom&&rect.bottom<=footer.top,overflow:document.documentElement.scrollWidth>innerWidth,scrollbar:getComputedStyle(el.closest('.modal-body')).scrollbarWidth}
    })
    expect(layout.ratio).toBeGreaterThan(.4);expect(layout.ratio).toBeLessThan(.51)
    expect(layout.inside).toBe(true);expect(layout.overflow).toBe(false);expect(layout.scrollbar).toBe('none')
    const positions=await dialog.locator('.more-options').evaluate(el=>{
      const threshold=el.querySelector('.delete-threshold-field .number-control').getBoundingClientRect()
      const excluded=el.querySelector('input[placeholder="回收站;预告片"]').getBoundingClientRect()
      const cascade=el.querySelector('.cascade-delete-field').getBoundingClientRect()
      const scrape=el.querySelector('[aria-label="STRM 刮削"]').getBoundingClientRect()
      return{thresholdTop:threshold.top,thresholdRight:threshold.right,excludedTop:excluded.top,excludedLeft:excluded.left,cascadeTop:cascade.top,cascadeBottom:cascade.bottom,cascadeWidth:cascade.width,gridWidth:el.getBoundingClientRect().width,scrapeBottom:scrape.bottom}
    })
    expect(Math.abs(positions.thresholdTop-positions.excludedTop)).toBeLessThan(1)
    expect(positions.thresholdRight).toBeLessThanOrEqual(positions.excludedLeft)
    expect(positions.cascadeTop).toBeGreaterThan(positions.scrapeBottom)
    expect(positions.cascadeBottom).toBeLessThan(positions.thresholdTop)
    expect(Math.abs(positions.cascadeWidth-positions.gridWidth)).toBeLessThan(1)
    await captureScreenshot(page,{path:info.outputPath(`threshold-${width}-${theme}.png`)})
    await input.fill('0');await dialog.getByRole('button',{name:'保存任务',exact:true}).click()
    await expect(dialog).toHaveCount(0);expect(f.writes.at(-1).body.deleteThreshold).toBe(0)
    await page.locator('.task-row-copy').click()
    await page.getByRole('button',{name:'更多选项',exact:true}).click()
    await expect(page.getByRole('spinbutton',{name:'删除阈值',exact:true})).toHaveValue('0')
    expect(f.unexpected).toEqual([])
  })
  test(`yellow hold, single recovery and locked execution ${width}`,async({page},info)=>{
    const f=await fixture(page,{held:true})
    await page.setViewportSize({width,height});await page.goto('/tasks/strm')
    await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
    const card=page.locator('.task-row-card'),check=card.getByRole('button',{name:'检查源存储健康',exact:true})
    await expect(card).toHaveClass(/delete-paused/)
    await expect(card).not.toHaveClass(/health-paused/)
    await expect(card.locator('.task-result')).toContainText('删除超限')
    await expect(card.locator('.task-result')).toBeVisible()
    expect(await card.evaluate(el=>getComputedStyle(el).borderTopColor)).toBe(theme==='light'?'rgb(181, 135, 64)':'rgb(217, 177, 112)')
    await expect(card.getByRole('button',{name:'立即执行',exact:true})).toHaveCount(0)
    await card.getByRole('button',{name:'任务操作 影视同步',exact:true}).click()
    await expect(page.getByRole('button',{name:'全量同步',exact:true})).toBeDisabled()
    await expect(page.getByRole('button',{name:'全量重置',exact:true})).toBeDisabled()
    await page.keyboard.press('Escape')
    await check.hover()
    expect(await card.evaluate(el=>getComputedStyle(el).borderTopColor)).toBe(theme==='light'?'rgb(181, 135, 64)':'rgb(217, 177, 112)')
    await expect(card.locator('.task-result')).toBeVisible()
    await captureScreenshot(page,{path:info.outputPath(`hold-${width}-${theme}.png`)})
    await check.click();await expect(check).toBeDisabled()
    await expect.poll(()=>f.writes.filter(v=>v.p.endsWith('/check')).length).toBe(1)
    f.finishCheck()
    await expect(check).toHaveAttribute('aria-busy','true')
    await expect(check).toBeDisabled()
    await page.locator('.task-row-copy').click()
    const dialog=page.getByRole('dialog',{name:'查看 STRM 任务',exact:true})
    await dialog.getByRole('button',{name:'更多选项',exact:true}).click()
    await expect(dialog.getByRole('spinbutton',{name:'删除阈值',exact:true})).toBeDisabled()
    await expect(dialog.getByRole('button',{name:'保存任务',exact:true})).toBeDisabled()
    expect(f.unexpected).toEqual([])
  })
}
