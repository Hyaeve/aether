import {test,expect} from '@playwright/test'
import {captureScreenshot} from './helpers/screenshot.js'

async function mock(page) {
 const settings={storageIds:['q'],invalidDays:15,retentionHours:12,mediaExtensions:'mkv;mp4',metadataExtensions:'nfo;jpg'}
 const cards=Array.from({length:3},(_,i)=>({id:`c${i}`,url:`https://pan.quark.cn/s/test${i}`,name:`分享目录 ${i+1}`,directory:'/media/links',directoryName:`Share${i}`,cron:'0 */6 * * *',mediaExtensions:'',metadataExtensions:'',enabled:true,result:{status:i===2?'invalid':'completed',invalidSince:i===2?'2026-10-23T12:00:00Z':'0001-01-01T00:00:00Z',lastRun:'2026-10-24T10:00:00Z',message:i===2?'分享已失效':'新增STRM 12 条，元数据 2 个，跳过 0 个'}}))
 const saves=[],links=[]
 await page.route('**/api/**',route=>{
  const r=route.request(),url=new URL(r.url()),p=url.pathname;let json={}
  if(p==='/api/auth/status')json={initialized:true,authenticated:true}
  if(p==='/api/state')json={username:'quark-library',storages:[{id:'q',type:'quark',name:'夸克影视',enabled:true,config:{}},{id:'q2',type:'quark',name:'夸克备用',enabled:true,config:{}}],tasks:[],links:[],settings:{},cache:{},traffic:{}}
  if(p==='/api/virtual-library/quark') {
   if(r.method()==='GET')json={cards,settings,stats:{strm:24,metadata:4}}
   else{saves.push(r.postDataJSON());json={id:'new',result:{message:'新增STRM 1 条'}}}
  }
  if(p==='/api/virtual-library/quark/settings'){Object.assign(settings,r.postDataJSON());saves.push(r.postDataJSON());json=settings}
  if(/\/quark\/c\d$/.test(p)&&r.method()==='PUT'){saves.push(r.postDataJSON());json={id:p.split('/').at(-1)}}
  if(p.endsWith('/run'))json={message:'新增STRM 1 条，元数据 0 个'}
  if(p==='/api/links'&&r.method()==='POST'){links.push(r.postDataJSON());json={id:'new'}}
  if(p==='/api/local-directories')json={path:'/media',items:[]}
  return route.fulfill({json})
 })
 return {saves,links}
}

for(const width of [1440,390])for(const theme of ['light','dark']){
 test(`Quark library cards and settings ${width} ${theme}`,async({page},info)=>{
  const {saves}=await mock(page)
  await page.setViewportSize({width,height:950})
  await page.goto('/virtual/quark')
  await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
  const cards=page.locator('.quark-library-card')
  await expect(cards).toHaveCount(3)
  const bounds=await cards.evaluateAll(els=>els.map(e=>{const r=e.getBoundingClientRect();return {x:r.x,y:r.y,right:r.right}}))
  if(width===1440){expect(Math.max(...bounds.map(b=>b.y))-Math.min(...bounds.map(b=>b.y))).toBeLessThan(1);expect(bounds[2].x).toBeGreaterThan(bounds[1].x)}
  else expect(bounds[1].y).toBeGreaterThan(bounds[0].y)
  expect(bounds.every(b=>b.x>=0&&b.right<=width)).toBe(true)
  await expect(cards.last()).toHaveClass(/invalid/)
  await expect(page.getByRole('link',{name:'夸克链库设置',exact:true}).locator('.lucide-braces-icon')).toHaveCount(1)
  await expect(page.locator('.quark-library-stats dd')).toHaveText(['3','24','4','1'])
  await captureScreenshot(page,{path:info.outputPath(`cards-${width}-${theme}.png`)})
  await page.getByRole('button',{name:'添加链接',exact:true}).click()
  const dialog=page.getByRole('dialog',{name:'添加夸克链接',exact:true})
  const url=dialog.getByRole('textbox',{name:'分享链接',exact:true})
  await url.fill('https://pan.quark.cn/s/new?pwd=abc1')
  await dialog.getByRole('textbox',{name:'生成位置',exact:true}).fill('/media')
  await dialog.getByRole('textbox',{name:'媒体扩展名',exact:true}).fill('mkv;不规范')
  await dialog.getByRole('button',{name:'保存链接',exact:true}).click()
  await expect(page.locator('.toast').last()).toContainText('媒体扩展名不合规范')
  expect(saves.length).toBe(0)
  await page.locator('.toast').last().getByRole('button',{name:'关闭通知',exact:true}).click()
  await dialog.getByRole('textbox',{name:'媒体扩展名',exact:true}).fill('mkv')
  await dialog.getByRole('button',{name:'漫画扩展名',exact:true}).click()
  await expect(dialog.getByRole('textbox',{name:'媒体扩展名',exact:true})).toHaveValue('mkv;cbz;cbr;zip;rar;epub;pdf')
  await expect(dialog.getByRole('switch',{name:'启用链接',exact:true})).toHaveCSS('height','20px')
  await expect(dialog.getByRole('switch',{name:'启用链接',exact:true})).toHaveCSS('width','35px')
  await expect(dialog.locator('.switch-label')).toHaveCSS('flex-direction','row')
  for(const input of await dialog.locator('input:not([type=checkbox])').all())expect(await input.evaluate(e=>e.getBoundingClientRect().height)).toBe(40)
  expect(await dialog.evaluate(e=>e.scrollWidth<=e.clientWidth)).toBe(true)
  const rowCenters=await dialog.getByRole('textbox').evaluateAll(els=>els.map(e=>{const r=e.getBoundingClientRect();return r.y+r.height/2}))
  expect(Math.abs(rowCenters[1]-rowCenters[2])).toBeLessThan(1)
  expect(Math.abs(rowCenters[3]-rowCenters[4])).toBeLessThan(1)
  await dialog.getByRole('textbox',{name:'分享链接',exact:true}).focus()
  await page.mouse.move(1,1)
  await expect.poll(()=>dialog.locator('.extension-preset').evaluateAll(els=>els.every(e=>getComputedStyle(e,'::after').opacity==='0'))).toBe(true)
  await captureScreenshot(page,{path:info.outputPath(`editor-${width}-${theme}.png`)})
  await dialog.getByRole('button',{name:'保存链接',exact:true}).click()
  await expect(dialog).toHaveCount(0)
  expect(saves[0]).toMatchObject({directory:'/media',name:'',directoryName:'',enabled:true})
  await page.getByRole('link',{name:'夸克链库设置',exact:true}).click()
  await expect(page.getByRole('heading',{name:'夸克链库设置',exact:true})).toBeVisible()
  await expect(page.getByRole('button',{name:'绑定 夸克影视',exact:true})).toHaveAttribute('aria-pressed','true')
  await expect(page.getByRole('spinbutton',{name:'失效链接保留时间',exact:true})).toHaveValue('15')
  await expect(page.getByRole('spinbutton',{name:'临时转存保活期',exact:true})).toHaveValue('12')
  await page.getByRole('button',{name:'绑定 夸克备用',exact:true}).click()
  await captureScreenshot(page,{path:info.outputPath(`settings-${width}-${theme}.png`)})
  await page.getByRole('button',{name:'保存设置',exact:true}).click()
  await expect.poll(()=>saves.length).toBe(2)
  expect(saves[1].storageIds).toEqual(['q','q2'])
  expect(await page.locator('.page-content').evaluate(e=>e.scrollWidth<=e.clientWidth)).toBe(true)
  await page.getByRole('button',{name:'返回页面',exact:true}).click()
  await expect(page).toHaveURL(/\/virtual\/quark$/)
 })
}

test('relay guard defaults on, disables for always-relay, and preserves preference',async({page})=>{
 const {links}=await mock(page)
 await page.goto('/links/manage')
 await page.getByRole('button',{name:'添加以太链接',exact:true}).click()
 const dialog=page.getByRole('dialog',{name:'添加以太链接',exact:true})
 await dialog.getByRole('button',{name:'Emby',exact:true}).click()
 await dialog.getByText('更多选项',{exact:true}).click()
 const guard=dialog.getByRole('switch',{name:'中继守护',exact:true})
 await expect(guard).toBeChecked()
 await expect(dialog.locator('.link-relay-guard')).toHaveCSS('flex-direction','row')
 await guard.uncheck()
 await dialog.getByRole('button',{name:'跳转模式',exact:true}).click()
 await page.getByRole('option',{name:'始终中继',exact:true}).click()
 await expect(guard).toBeDisabled()
 await dialog.getByRole('button',{name:'跳转模式',exact:true}).click()
 await page.getByRole('option',{name:'公网跳转',exact:true}).click()
 await expect(guard).toBeEnabled();await expect(guard).not.toBeChecked()
 await dialog.getByText('以链名称',{exact:true}).locator('input').fill('Emby')
 await dialog.getByText('服务地址',{exact:true}).locator('input').fill('http://media.test:8096')
 await dialog.getByText('反代端口',{exact:true}).locator('input').fill('15180')
 await dialog.getByLabel('API Key',{exact:true}).fill('key')
 await dialog.getByRole('button',{name:'保存以链',exact:true}).click()
 await expect.poll(()=>links.length).toBe(1)
 expect(links[0]).toMatchObject({relayGuard:false,mode:'public'})
})
