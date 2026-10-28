import {test,expect} from '@playwright/test'
import {captureScreenshot} from './helpers/screenshot.js'

async function setup(page){
 let tasks=[]
 await page.route('**/api/**',route=>{
  const u=new URL(route.request().url())
  let json={}
  if(u.pathname==='/api/auth/status')json={initialized:true,authenticated:true}
  if(u.pathname==='/api/state')json={username:'retained-navigation',storages:[{id:'local',type:'local',name:'本地',enabled:true,config:{}}],tasks,settings:{},cache:{},traffic:{}}
  if(u.pathname==='/api/tasks'&&route.request().method()==='POST'){json={...route.request().postDataJSON(),id:'retained'};tasks=[json]}
  if(u.pathname==='/api/files')json=[{id:'v',name:'film.mp4',url:'/missing-film.mp4'}]
  if(u.pathname==='/api/files/video-info')json={size:1024,format:{format_name:'mp4'},streams:[{index:0,codec_type:'video',codec_name:'h264'}]}
  if(u.pathname==='/api/files/video-subtitles')json={tracks:[{index:2,title:'中文',supported:true}]}
  if(['/api/transfers','/api/backup-rules','/api/generation/jobs','/api/files/visits'].includes(u.pathname))json=[]
  return route.fulfill({json})
 })
 await page.route('**/missing-film.mp4',route=>route.fulfill({status:404}))
}

for(const width of [1440,390,375])for(const theme of ['light','dark'])test(`STRM retained extensions save empty and expose comic toggle ${width} ${theme}`,async({page},info)=>{
 await setup(page)
 await page.setViewportSize({width,height:1000})
 await page.goto('/tasks/strm')
 await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
 await page.getByRole('button',{name:'添加 STRM 任务卡片',exact:true}).click()
 const dialog=page.getByRole('dialog',{name:'添加 STRM 任务',exact:true})
 await dialog.getByRole('button',{name:'更多选项',exact:true}).click()
 const input=dialog.getByRole('textbox',{name:'保留扩展名',exact:true})
 await expect(input).toHaveValue('iso')
 const comic=dialog.getByRole('button',{name:'保留漫画扩展名',exact:true})
 await comic.click();await expect(input).toHaveValue('iso;cbz;cbr;zip;rar;epub;pdf')
 await expect(comic.locator('svg')).toHaveClass(/lucide-book-open/)
 const heading=input.locator('..').locator('.extension-heading')
 const geometry=await heading.evaluate(el=>{
  const label=el.querySelector('label'),presets=el.querySelector('.extension-presets')
  const a=label.getBoundingClientRect(),b=presets.getBoundingClientRect(),parent=el.getBoundingClientRect()
  return {labelHeight:a.height,lineHeight:parseFloat(getComputedStyle(label).lineHeight),overlap:a.right>b.left,overflow:b.right>parent.right}
 })
 expect(geometry.labelHeight).toBeLessThanOrEqual(geometry.lineHeight)
 expect(geometry.overlap).toBe(false);expect(geometry.overflow).toBe(false)
 await captureScreenshot(page,{path:info.outputPath(`retained-comic-${width}-${theme}.png`)})
 await comic.click();await expect(input).toHaveValue('iso')
 await input.fill('');await expect(input).toHaveValue('')
 await dialog.getByRole('textbox',{name:/^任务名称/}).fill('保留扩展测试')
 await dialog.getByRole('button',{name:'选择目录',exact:true}).click()
 await page.locator('.source-accounts button').filter({hasText:'本地'}).click()
 await page.getByRole('button',{name:'选择当前目录',exact:true}).click()
 await dialog.getByRole('button',{name:'保存任务',exact:true}).click()
 await expect(dialog).toHaveCount(0)
 await page.locator('.task-row-card').filter({hasText:'保留扩展测试'}).click()
 const edit=page.getByRole('dialog',{name:'编辑 STRM 任务',exact:true})
 await edit.getByRole('button',{name:'更多选项',exact:true}).click()
 await expect(edit.getByRole('textbox',{name:'保留扩展名',exact:true})).toHaveValue('')
})

for(const width of [1440,390])for(const theme of ['light','dark'])test(`breadcrumbs show module and current tab ${width} ${theme}`,async({page},info)=>{
 await setup(page);await page.setViewportSize({width,height:900})
 for(const [path,module,tab] of [['/files','文件服务','文件管理'],['/storage','存储管理','存储池'],['/storage/shares','存储管理','分享栏'],['/transfer/backup','传输中心','备份规则'],['/tasks/strm','任务中心','STRM 任务'],['/virtual/mobile','虚拟链库','移动链库'],['/tools/market','辅助工具','插件市场'],['/settings/account','系统设置','账号与安全']]){
  await page.goto(path);await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
  const crumbs=page.locator('.breadcrumbs')
  await expect(crumbs).toContainText(module);await expect(crumbs).toContainText(tab)
  await expect(crumbs.locator('[aria-current=page]')).toHaveText(tab)
  const fits=await crumbs.evaluate(el=>{const r=el.getBoundingClientRect(),actions=document.querySelector('.topbar-actions').getBoundingClientRect();return r.right<=actions.left&&el.scrollWidth<=el.clientWidth})
  expect(fits).toBe(true)
 }
 await captureScreenshot(page,{path:info.outputPath(`breadcrumbs-${width}-${theme}.png`)})
 await page.goto('/dashboard')
 if(width<=760)await page.getByRole('button',{name:'打开导航',exact:true}).click()
 await expect(page.getByRole('navigation',{name:'主导航'}).getByRole('link',{name:'信息总览',exact:true})).toHaveAttribute('aria-current','page')
 await expect(page.locator('.breadcrumbs')).toContainText('信息总览')
})

test('video details close on leaving fullscreen and all popup surfaces match',async({page},info)=>{
 await setup(page)
 await page.addInitScript(()=>Object.defineProperty(document,'fullscreenEnabled',{configurable:true,get:()=>false}))
 await page.goto('/files');await page.getByRole('button',{name:'film.mp4',exact:true}).dblclick()
 const viewer=page.locator('.video-viewer')
 await expect(viewer.getByRole('button',{name:'视频信息',exact:true})).toHaveCount(0)
 await expect(viewer).not.toContainText('网速')
 await viewer.getByRole('button',{name:'全屏',exact:true}).click()
 await viewer.getByRole('button',{name:'视频信息',exact:true}).click()
 const details=viewer.getByRole('region',{name:'视频信息',exact:true})
 await expect(details).toBeVisible();await expect(details).not.toContainText('网速')
 await expect(details).toHaveCSS('font-size','14px')
 const surface=await details.evaluate(el=>{const s=getComputedStyle(el);return {background:s.backgroundColor,filter:s.backdropFilter,layer:s.zIndex}})
 expect(Number(surface.layer)).toBeGreaterThan(0)
 for(const [name,selector] of [['字幕','.video-subtitle-menu'],['音轨','.video-audio-menu'],['静音','.video-volume-popup']]){
  await viewer.getByRole('button',{name,exact:true}).hover()
  const popup=viewer.locator(selector)
  await expect(popup).toHaveCSS('background-color',surface.background)
  await expect(popup).toHaveCSS('backdrop-filter',surface.filter)
  await expect(popup).toHaveCSS('z-index',surface.layer)
 }
 await captureScreenshot(page,{path:info.outputPath('video-popup-surfaces.png')})
 await viewer.getByRole('button',{name:'退出全屏',exact:true}).click()
 await expect(details).toHaveCount(0)
 await viewer.getByRole('button',{name:'全屏',exact:true}).click()
 await expect(details).toHaveCount(0)
})

test('fullscreen details remain inside a small phone and landscape with reduced motion',async({page},info)=>{
 await setup(page)
 await page.emulateMedia({reducedMotion:'reduce'})
 await page.addInitScript(()=>Object.defineProperty(document,'fullscreenEnabled',{configurable:true,get:()=>false}))
 await page.goto('/files');await page.getByRole('button',{name:'film.mp4',exact:true}).dblclick()
 const viewer=page.locator('.video-viewer')
 await viewer.getByRole('button',{name:'全屏',exact:true}).click()
 await viewer.getByRole('button',{name:'视频信息',exact:true}).click()
 const details=viewer.getByRole('region',{name:'视频信息',exact:true})
 for(const viewport of [{width:375,height:812},{width:844,height:390}])for(const theme of ['light','dark']){
  await page.setViewportSize(viewport)
  await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
  await expect(details).toBeVisible()
  await expect(details).toHaveCSS('font-size','14px')
  await expect.poll(()=>details.evaluate(el=>{
   const a=el.getBoundingClientRect(),b=el.closest('.video-viewer').getBoundingClientRect()
   return a.left>=b.left&&a.right<=b.right&&a.top>=b.top&&a.bottom<=b.bottom&&el.scrollWidth<=el.clientWidth
  })).toBe(true)
  await expect(details.getByRole('button',{name:'关闭视频信息',exact:true})).toBeInViewport()
  await captureScreenshot(page,{path:info.outputPath(`video-info-${viewport.width}-${theme}.png`)})
 }
 await details.getByRole('button',{name:'关闭视频信息',exact:true}).click()
 await expect(details).toHaveCount(0)
 await expect(viewer).toBeFocused()
 await page.keyboard.press('Escape');await expect(viewer.getByRole('button',{name:'全屏',exact:true})).toBeVisible()
 await page.keyboard.press('Escape');await expect(viewer).toHaveCount(0)
})
