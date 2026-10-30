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
  if(/\/quark\/c\d$/.test(p)&&r.method()==='PUT'){saves.push(r.postDataJSON());const id=p.split('/').at(-1);Object.assign(cards.find(card=>card.id===id),r.postDataJSON());json={id}}
  if(p.endsWith('/run'))json={message:'新增STRM 1 条，元数据 0 个'}
  if(p==='/api/links'&&r.method()==='POST'){links.push(r.postDataJSON());json={id:'new'}}
  if(p==='/api/local-directories')json={path:'/media',items:[]}
  return route.fulfill({json})
 })
 return {saves,links,cards}
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
  await expect(page.getByRole('button',{name:'夸克链库设置',exact:true}).locator('.lucide-chevron-left-icon')).toHaveCount(1)
  await expect(page.locator('.quark-library-stats dd')).toHaveText(['2','1'])
  await expect(page.locator('.quark-overview-totals')).toHaveText('链接 3STRM 24元数据 4失效 1')
  await captureScreenshot(page,{path:info.outputPath(`cards-${width}-${theme}.png`)})
  await page.getByRole('button',{name:'添加链接',exact:true}).click()
  const dialog=page.getByRole('dialog',{name:'添加夸克链接',exact:true})
  await expect(dialog.getByRole('button',{name:'级联清理',exact:true})).toHaveText('开启')
  const url=dialog.getByRole('textbox',{name:'分享链接',exact:true})
  await url.fill('https://pan.quark.cn/s/new?pwd=abc1')
  await dialog.getByRole('textbox',{name:'生成位置',exact:true}).fill('/media')
  await dialog.getByRole('switch',{name:'启用链接',exact:true}).check()
  await dialog.getByRole('textbox',{name:'媒体扩展名',exact:true}).fill('mkv;不规范')
  await dialog.getByRole('button',{name:'保存链接',exact:true}).click()
  await expect(page.locator('.toast').last()).toContainText('媒体扩展名不合规范')
  expect(saves.length).toBe(0)
  await page.locator('.toast').last().getByRole('button',{name:'关闭通知',exact:true}).click()
  await dialog.getByRole('textbox',{name:'媒体扩展名',exact:true}).fill('mkv')
  await expect(dialog.getByRole('button',{name:'漫画扩展名',exact:true})).toHaveCount(0)
  await expect(dialog.locator('.quark-extension-label').first().locator('.extension-preset')).toHaveCount(2)
  await dialog.getByRole('textbox',{name:'媒体扩展名',exact:true}).fill('mkv;cbz;cbr;zip;rar;epub;pdf')
  await expect(dialog.getByRole('textbox',{name:'媒体扩展名',exact:true})).toHaveValue('mkv;cbz;cbr;zip;rar;epub;pdf')
  await expect(dialog.getByRole('switch',{name:'启用链接',exact:true})).toHaveCSS('height','20px')
  await expect(dialog.getByRole('switch',{name:'启用链接',exact:true})).toHaveCSS('width','35px')
  await expect(dialog.locator('.switch-label')).toHaveCSS('flex-direction','row')
  for(const input of await dialog.locator('input:not([type=checkbox])').all())expect(await input.evaluate(e=>e.getBoundingClientRect().height)).toBe(40)
  expect(await dialog.evaluate(e=>e.scrollWidth<=e.clientWidth)).toBe(true)
  const rows=[]
  for(const names of [['分享链接','分享名称'],['生成位置','目录名称'],['Cron表达式','级联清理']]){
   const centers=[]
   for(const name of names)centers.push(await dialog.getByRole(name==='级联清理'?'button':'textbox',{name,exact:true}).evaluate(e=>{const r=e.getBoundingClientRect();return r.y+r.height/2}))
   expect(Math.abs(centers[0]-centers[1])).toBeLessThan(1)
   rows.push(centers[0])
  }
  expect(rows[1]).toBeGreaterThan(rows[0])
  expect(rows[2]).toBeGreaterThan(rows[1])
  await dialog.getByRole('button',{name:'级联清理',exact:true}).click()
  await page.getByRole('option',{name:'关闭',exact:true}).click()
  await dialog.getByRole('textbox',{name:'分享链接',exact:true}).focus()
  await page.mouse.move(1,1)
  await expect.poll(()=>dialog.locator('.extension-preset').evaluateAll(els=>els.every(e=>getComputedStyle(e,'::after').opacity==='0'))).toBe(true)
  await captureScreenshot(page,{path:info.outputPath(`editor-${width}-${theme}.png`)})
  await dialog.getByRole('button',{name:'保存链接',exact:true}).click()
  await expect(dialog).toHaveCount(0)
  expect(saves[0]).toMatchObject({directory:'/media',name:'',directoryName:'',enabled:true,cascadeCleanup:'off',mediaExtensions:'mkv;cbz;cbr;zip;rar;epub;pdf'})
  await page.getByRole('button',{name:'夸克链库设置',exact:true}).click()
  await expect(page.getByRole('heading',{name:'夸克链库设置',exact:true})).toBeVisible()
  await expect(page.getByRole('button',{name:'绑定 夸克影视',exact:true})).toHaveAttribute('aria-pressed','true')
  await expect(page.getByRole('spinbutton',{name:'失效链接保留时间',exact:true})).toHaveValue('15')
  await expect(page.getByRole('spinbutton',{name:'临时转存保活期',exact:true})).toHaveValue('12')
  await expect(page.getByRole('button',{name:'漫画扩展名',exact:true})).toHaveCount(0)
  await expect(page.locator('.quark-extension-label').first().locator('.extension-preset')).toHaveCount(2)
  await page.getByRole('textbox',{name:'默认媒体扩展名',exact:true}).fill('mkv;cbz;cbr;zip;rar;epub;pdf')
  await page.getByRole('button',{name:'绑定 夸克备用',exact:true}).click()
  await captureScreenshot(page,{path:info.outputPath(`settings-${width}-${theme}.png`)})
  await page.getByRole('button',{name:'保存设置',exact:true}).click()
  await expect.poll(()=>saves.length).toBe(2)
  expect(saves[1].storageIds).toEqual(['q','q2'])
  expect(saves[1].mediaExtensions).toBe('mkv;cbz;cbr;zip;rar;epub;pdf')
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

test('Quark output row is second for new and existing shares',async({page})=>{
 await mock(page)
 await page.goto('/virtual/quark')
 for(const title of ['添加夸克链接','编辑夸克链接']){
  await page.getByRole('button',{name:title==='添加夸克链接'?'添加链接':'编辑 分享目录 1',exact:true}).click()
  const editor=page.getByRole('dialog',{name:title,exact:true})
  const names=await editor.locator('.form-grid>label:not(.full) input').evaluateAll(els=>els.map(e=>e.getAttribute('aria-label')))
  expect(names).toEqual(['分享链接','分享名称','生成位置','目录名称','Cron表达式'])
  const output=await editor.getByRole('textbox',{name:'生成位置',exact:true}).boundingBox()
  const schedule=await editor.getByRole('textbox',{name:'Cron表达式',exact:true}).boundingBox()
  expect(output.y).toBeLessThan(schedule.y)
  await editor.getByRole('button',{name:'取消',exact:true}).click()
 }
})

test('background generation remains visible after reload and cleanup setting reopens',async({page})=>{
 const {cards,saves}=await mock(page)
 await page.route('**/api/virtual-library/quark/c0/run',route=>{
  cards[0].result={status:'running',message:'正在后台同步分享',lastRun:new Date().toISOString()}
  return route.fulfill({status:202,json:{status:'running',message:'已在后台开始生成，可离开或刷新页面'}})
 })
 await page.goto('/virtual/quark')
 await page.locator('.quark-library-card').first().getByRole('button',{name:'立即生成',exact:true}).click()
 const confirmation=page.getByRole('dialog',{name:'同步夸克分享',exact:true})
 await expect(confirmation).toContainText('无 STRM 的子目录')
 await confirmation.getByRole('button',{name:'确认同步',exact:true}).click()
 await expect(page.locator('.quark-library-card').first().getByRole('button',{name:'生成中',exact:true})).toBeDisabled()
 await page.reload()
 await expect(page.locator('.quark-library-card').first().getByRole('button',{name:'生成中',exact:true})).toBeDisabled()
 await expect(page.getByRole('button',{name:'编辑 分享目录 1',exact:true})).toBeDisabled()
 cards[0].result={status:'completed',message:'新增STRM 2 条，清理 1 个'}
 await page.reload()
 await page.getByRole('button',{name:'编辑 分享目录 1',exact:true}).click()
 const editor=page.getByRole('dialog',{name:'编辑夸克链接',exact:true})
 await expect(editor.getByRole('button',{name:'级联清理',exact:true})).toHaveText('开启')
 await editor.getByRole('button',{name:'级联清理',exact:true}).click()
 await page.getByRole('option',{name:'关闭',exact:true}).click()
 await editor.getByRole('button',{name:'保存链接',exact:true}).click()
 await page.getByRole('dialog',{name:'同步夸克分享',exact:true}).getByRole('button',{name:'确认同步',exact:true}).click()
 await expect(editor).toHaveCount(0)
 expect(saves[0].cascadeCleanup).toBe('off')
 await page.getByRole('button',{name:'编辑 分享目录 1',exact:true}).click()
 await expect(page.getByRole('dialog',{name:'编辑夸克链接',exact:true}).getByRole('button',{name:'级联清理',exact:true})).toHaveText('关闭')
})
