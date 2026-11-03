import {test,expect} from '@playwright/test'
import {captureScreenshot} from './helpers/screenshot.js'
const browserName=process.env.AETHER_UI_BROWSER||'chromium'
test.use({browserName,...(browserName==='webkit'?{isMobile:true,hasTouch:true}:{})})

async function fixture(page){
  const writes=[]
  const task={id:'life',name:'115云影视',kind:'strm',storageId:'pan',source:'10',sourceLabel:'影视',target:'/output',enabled:true,status:'running',strmSource:'life',mode:'incremental_update',cron:'',mediaExtensions:'mkv',metadataExtensions:'nfo',cascadeDelete:true,cascadeExtensions:'jpg;png;nfo;srt;ass'}
  const progress={running:true,taskId:'life',message:'正在刮削第一个作品',total:2,done:0,queued:0}
  const transfer={version:1,downloadSpeed:0,downloadUnit:'KB/s',uploadSpeed:0,uploadUnit:'KB/s',maxPreprocess:5,maxUploads:5,uploadDelay:0,crossCloudTemp:true,ignoredExtensions:[],tempDirectory:'/data/temp'}
  await page.route('**/api/**',async route=>{
    const r=route.request(),p=new URL(r.url()).pathname
    let json={}
    if(p==='/api/auth/status')json={initialized:true,authenticated:true}
    else if(p==='/api/state')json={username:'queue-ui',storages:[{id:'pan',name:'115影视',type:'115',enabled:true,config:{}}],tasks:[task],settings:{},cache:{},traffic:{},fileVisits:[],fileFavorites:[]}
    else if(p==='/api/strm-replace')json={tasks:[]}
    else if(p==='/api/cron/validate')json={valid:true}
    else if(p==='/api/transfer/settings'){if(r.method()==='PUT'){writes.push(r.postDataJSON());Object.assign(transfer,r.postDataJSON())}json=transfer}
    else if(p==='/api/strm/settings')json={address:'http://localhost',format:'pickcode_name',content:'original'}
    else if(p==='/api/tasks/life/sync'){writes.push({sync:r.postDataJSON()});json={ok:true,queued:true}}
    else if(p==='/api/strm-scrape/items')json=[{path:'First.strm',title:'第一部电影',kind:'movie',tmdb:1,status:'pending'},{path:'Second.strm',title:'第二部电影',kind:'movie',tmdb:0,status:'pending'}]
    else if(p==='/api/strm-scrape/status')json=progress
    else if(p==='/api/strm-scrape/settings')json={writeMode:'missing',episodes:true,actors:false}
    else if(p==='/api/strm-scrape/match'){writes.push({match:r.postDataJSON()});progress.queued++;json=progress}
    else if(p==='/api/notifications')json=[]
    return route.fulfill({json})
  })
  return {task,writes}
}

for(const [width,height,theme] of [[1440,1000,'light'],[390,844,'dark'],[375,812,'light'],[844,390,'dark']]){
  test.describe(`${browserName} ${width} ${theme}`,()=>{
    test('settings align fields and menus stay inside scrollbar clearance',async({page},info)=>{
      const f=await fixture(page);await page.setViewportSize({width,height});await page.addInitScript(t=>localStorage.setItem('aether-theme',t),theme);await page.goto('/settings/transfer')
      const cap=page.getByRole('spinbutton',{name:'临时文件体积上限',exact:true})
      await expect(cap).toHaveValue('100');await expect(cap).toHaveAttribute('min','1')
      const layout=await page.locator('.temp-path-row').evaluate(el=>[...el.children].map(c=>{const r=c.getBoundingClientRect();return{x:r.x,y:r.y,width:r.width}}))
      expect(Math.abs(layout[0].width-layout[1].width)).toBeLessThan(1);expect(layout[0].y).toBe(layout[1].y)
      await cap.fill('25');await cap.press('Tab');await expect.poll(()=>f.writes.at(-1)?.tempMaxGiB).toBe(25)
      if(browserName==='webkit')await expect(page.locator('.temp-field .number-unit')).toHaveCSS('opacity','1')
      await captureScreenshot(page,{path:info.outputPath('transfer-limit.png')})
      await page.goto('/settings/strm')
      const content=page.locator('.strm-content-field')
      await expect(content.getByRole('button',{name:'STRM内容',exact:true})).toBeEnabled()
      const field=await content.evaluate(el=>{const a=el.querySelector('label').getBoundingClientRect(),b=el.querySelector('button').getBoundingClientRect();return{bottom:a.bottom,top:b.top,x:a.x,bx:b.x}})
      expect(field.bottom).toBeLessThan(field.top);expect(Math.abs(field.x-field.bx)).toBeLessThan(1)
      for(const [button,menu] of [['账号菜单','.account-dropdown'],['任务通知','.notification-dropdown']]){
        await page.getByRole('button',{name:button,exact:true}).click();await expect(page.locator(menu)).toBeVisible()
        const edge=await page.locator(menu).evaluate(el=>({right:el.getBoundingClientRect().right,left:el.getBoundingClientRect().left,viewport:innerWidth}))
        expect(edge.viewport-edge.right).toBeGreaterThanOrEqual(16);expect(edge.left).toBeGreaterThanOrEqual(0)
        await captureScreenshot(page,{path:info.outputPath(button==='账号菜单'?'account-clearance.png':'notice-clearance.png')});await page.keyboard.press('Escape')
      }
      await page.goto('/tasks/strm');await page.locator('.task-row-copy').click()
      await expect(page.locator('html')).toHaveAttribute('data-theme',theme)
      const dialog=page.getByRole('dialog',{name:'查看 STRM 任务',exact:true})
      await dialog.getByRole('button',{name:'更多选项',exact:true}).click()
      await dialog.locator('.cascade-delete-field').scrollIntoViewIfNeeded()
      const toggle=await dialog.getByRole('switch',{name:'级联删除',exact:true}).boundingBox(),extensions=await dialog.getByRole('textbox',{name:'级联删除扩展名',exact:true}).boundingBox()
      const alignment={switch:toggle.y+toggle.height/2,input:extensions.y+extensions.height/2}
      expect(Math.abs(alignment.switch-alignment.input)).toBeLessThan(2)
      await captureScreenshot(page,{path:info.outputPath('cascade-alignment.png')})
    })
  })
}

test('life poll accepts manual full sync as a queued operation',async({page})=>{
  const f=await fixture(page);await page.goto('/tasks/strm')
  await page.getByRole('button',{name:'任务操作 115云影视',exact:true}).click()
  await page.getByRole('button',{name:'全量同步',exact:true}).click()
  await page.getByRole('button',{name:'确认同步',exact:true}).click()
  await expect(page.locator('.toast-stack')).toContainText('全量同步已排队')
  expect(f.writes.at(-1)).toEqual({sync:{confirm:true}})
})

test('manual scrape allows matching the next poster while running',async({page},info)=>{
  const f=await fixture(page);await page.goto('/tasks/scrape')
  await expect(page.getByRole('button',{name:'停止',exact:true})).toBeVisible()
  await page.getByRole('button',{name:'匹配 第二部电影',exact:true}).click()
  const dialog=page.getByRole('dialog',{name:'选择媒体匹配',exact:true})
  await dialog.getByRole('spinbutton',{name:'TMDB ID',exact:true}).fill('124')
  await dialog.getByRole('button',{name:'确认匹配',exact:true}).click()
  await expect(dialog).toHaveCount(0);await expect(page.locator('.toast-stack')).toContainText('手动识别已排队')
  await expect(page.locator('.scrape-progress')).toContainText('排队 1 项')
  expect(f.writes.at(-1).match).toMatchObject({path:'Second.strm',tmdb:124,scrape:true,group:true})
  await expect(page.getByRole('button',{name:'重置元数据',exact:true}).first()).toBeDisabled()
  await expect(page.getByRole('button',{name:'匹配 第一部电影',exact:true})).toBeEnabled()
  await captureScreenshot(page,{path:info.outputPath('scrape-queued.png')})
})
