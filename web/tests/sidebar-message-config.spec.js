import {test,expect} from '@playwright/test'
import {captureScreenshot} from './helpers/screenshot.js'

async function fixture(page){
 let config={telegram:{enabled:false,token:'',chatId:'',userIds:[]},targets:[],wecom:{enabled:false,corpId:'',secret:'',agentId:'',apiURL:'https://qyapi.weixin.qq.com',token:'',encodingAESKey:'',userIds:[]}}
 let tests=0
 const secrets={}
 await page.route('**/api/**',route=>{
  const r=route.request(),p=new URL(r.url()).pathname
  let json={}
  if(p==='/api/auth/status')json={initialized:true,authenticated:true}
  else if(p==='/api/state')json={username:'message-config',storages:[],tasks:[],settings:{},cache:{},traffic:{}}
  else if(p==='/api/notification-service')json=config
  else if(p==='/api/notification-service/wecom'){
   const value=r.postDataJSON().wecom
   for(const field of ['secret','token','encodingAESKey'])if(value[field])secrets[field]=value[field]
   config.wecom={...value,secret:'',token:'',encodingAESKey:'',hasSecret:!!secrets.secret,hasToken:!!secrets.token,hasEncodingAESKey:!!secrets.encodingAESKey}
   json=config
  }else if(p==='/api/notification-service/wecom/secret'){
   const input=r.postDataJSON(),value=secrets[input.field]||''
   json=input.metadataOnly?{length:value.length}:{value}
  }else if(p==='/api/notification-service/wecom/test'){tests++;json={ok:true}}
  else if(['/api/logs','/api/transfers','/api/backup-rules'].includes(p))json=[]
  return route.fulfill({json})
 })
 return {config:()=>config,tests:()=>tests}
}

for(const size of [
 {width:760,height:1024},{width:761,height:1024},{width:768,height:1024},
 {width:800,height:1024},{width:801,height:1024},{width:1440,height:1000},
 {width:844,height:390,touch:true},{width:844,height:490,touch:true},
 {width:844,height:500,touch:true},{width:844,height:501,touch:true}
])test.describe(`sidebar breakpoint ${size.width}x${size.height}${size.touch?' touch':''}`,()=>{
 test.use({viewport:{width:size.width,height:size.height},hasTouch:!!size.touch,isMobile:!!size.touch,deviceScaleFactor:2})
 test('sidebar offset matches the visible navigation',async({page},info)=>{
  await fixture(page);await page.goto('/dashboard')
  const sidebar=page.locator('.sidebar'),menu=page.getByRole('button',{name:'打开导航',exact:true})
  const collapsed=size.width<=760||size.touch&&size.height<=500,offset=collapsed?0:220
  for(const theme of ['light','dark']){
   await page.evaluate(value=>document.documentElement.dataset.theme=value,theme)
   await expect.poll(()=>page.locator('.main-shell').evaluate(el=>el.getBoundingClientRect().left)).toBe(offset)
   await expect.poll(()=>page.locator('.topbar').evaluate(el=>el.getBoundingClientRect().left)).toBe(offset)
   if(collapsed){
    await expect(menu).toBeVisible()
    await expect.poll(()=>sidebar.evaluate(el=>el.getBoundingClientRect().right)).toBeLessThanOrEqual(0)
    await menu.click()
    await expect.poll(()=>sidebar.evaluate(el=>el.getBoundingClientRect().left)).toBe(0)
    await expect(page.locator('.nav-overlay')).toBeVisible()
    await page.locator('.nav-overlay').click({position:{x:size.width-10,y:size.height-10}})
    await expect.poll(()=>sidebar.evaluate(el=>el.getBoundingClientRect().right)).toBeLessThanOrEqual(0)
   }else{
    await expect(menu).toBeHidden()
    await expect(sidebar).toBeVisible()
    expect((await sidebar.boundingBox()).width).toBe(offset)
   }
   expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
   if(size.width===768||size.height===490)await captureScreenshot(page,{path:info.outputPath(`navigation-${theme}.png`)})
  }
 })
})

for(const width of [1440,390])for(const theme of ['light','dark'])test(`sidebar is continuous, compact and scrolls without a track ${width} ${theme}`,async({page},info)=>{
 await fixture(page);await page.setViewportSize({width,height:900});await page.goto('/dashboard')
 await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
 if(width<800)await page.getByRole('button',{name:'打开导航',exact:true}).click()
 const nav=page.getByRole('navigation',{name:'主导航',exact:true}),links=nav.getByRole('link')
 await expect(links).toHaveText(['以太视界','文件服务','存储管理','传输中心','任务管理','虚拟链库','以太链接','辅助工具','通知服务','运行日志','系统设置'])
 const geometry=await nav.evaluate(el=>{
  const links=[...el.querySelectorAll('[role=link]')].map(e=>e.getBoundingClientRect()),brand=document.querySelector('.sidebar .brand').getBoundingClientRect()
  return {gaps:links.slice(1).map((r,i)=>r.top-links[i].bottom),brandGap:links[0].top-brand.bottom,width:el.closest('.sidebar').getBoundingClientRect().width}
 })
 expect(Math.max(...geometry.gaps)-Math.min(...geometry.gaps)).toBeLessThan(1)
 expect(geometry.brandGap).toBeLessThanOrEqual(6);expect(geometry.width).toBe(220)
 await expect(nav).toHaveCSS('scrollbar-width','none')
 await captureScreenshot(page,{path:info.outputPath(`sidebar-${width}-${theme}.png`)})
 await page.setViewportSize({width,height:390})
 await nav.evaluate(el=>{el.scrollTop=el.scrollHeight})
 await expect(nav.getByRole('link',{name:'系统设置',exact:true})).toBeInViewport()
 const top=await nav.evaluate(el=>el.scrollTop);expect(top).toBeGreaterThan(0)
 await nav.getByRole('link',{name:'运行日志',exact:true}).click()
 await expect(page.locator('.breadcrumbs strong')).toHaveText('运行日志')
 await expect(page.getByRole('region',{name:'运行日志',exact:true})).toBeVisible()
})

for(const width of [1440,375])for(const theme of ['light','dark'])test(`message configuration orders channels above a wider WeCom panel ${width} ${theme}`,async({page},info)=>{
 const f=await fixture(page)
 await page.setViewportSize({width,height:1000});await page.goto('/notifications')
 await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
 const tabs=page.getByRole('navigation',{name:'通知服务栏目'}),messageTab=tabs.getByRole('link',{name:'消息配置',exact:true})
 await expect(messageTab).toBeVisible()
 const messageIcon=messageTab.locator('svg')
 await expect(messageIcon).toHaveClass(/lucide-message-square-text/)
 await expect(messageIcon).toHaveAttribute('aria-hidden','true')
 await expect(messageIcon).toHaveAttribute('stroke-width','1.7')
 await expect(messageIcon).toHaveAttribute('fill','none')
 await expect(tabs.getByRole('link',{name:'链接转存',exact:true}).locator('svg')).toHaveClass(/lucide-folder-input/)
 const iconLayout=await tabs.evaluate(el=>{
  const links=[...el.querySelectorAll('a')],icons=links.map(link=>link.querySelector('svg').getBoundingClientRect())
  return {widths:icons.map(r=>r.width),heights:icons.map(r=>r.height),centerDifference:Math.abs(icons[0].y+icons[0].height/2-icons[1].y-icons[1].height/2),inherits:links.every(link=>getComputedStyle(link.querySelector('svg')).color===getComputedStyle(link).color)}
 })
 expect(iconLayout.widths[0]).toBe(iconLayout.widths[1]);expect(iconLayout.heights[0]).toBe(iconLayout.heights[1])
 expect(iconLayout.centerDifference).toBeLessThan(1);expect(iconLayout.inherits).toBe(true)
 await captureScreenshot(page,{path:info.outputPath(`message-tabs-${width}-${theme}.png`)})
 await expect(page.locator('.breadcrumbs [aria-current=page]')).toHaveText('消息配置')
 const tg=page.getByRole('form',{name:'TG Bot',exact:true}),wecom=page.getByRole('form',{name:'企业微信',exact:true})
 await expect(tg).toBeVisible();await expect(wecom).toBeVisible()
 const a=await tg.boundingBox(),b=await wecom.boundingBox()
 if(width===1440)expect(b.width).toBeCloseTo(2*a.width+24,0)
 else expect(a.width).toBe(b.width)
 expect(b.y).toBeGreaterThanOrEqual(a.y+a.height+20)
 await expect(wecom.locator('.lucide-circle-help-icon')).toHaveCount(0)
 for(const field of ['企业 ID','应用 Secret','Agent ID','API 地址','Token','EncodingAESKey','接收成员']){
  await expect(wecom.getByLabel(field,{exact:true})).toHaveCSS('height','40px')
 }
 await expect(wecom.getByLabel('API 地址',{exact:true})).toHaveValue('https://qyapi.weixin.qq.com')
 await wecom.getByLabel('企业 ID',{exact:true}).fill('ww-test')
 await wecom.getByLabel('应用 Secret',{exact:true}).fill('application-secret')
 await wecom.getByLabel('Agent ID',{exact:true}).fill('1000002')
 await wecom.getByLabel('接收成员',{exact:true}).fill('alice;bob')
 await wecom.getByLabel('Token',{exact:true}).fill('callback-token')
 await wecom.getByLabel('EncodingAESKey',{exact:true}).fill('KioqKioqKioqKioqKioqKioqKioqKioqKioqKioqKio')
 await wecom.getByRole('switch',{name:'启用企业微信',exact:true}).check()
 await wecom.getByRole('button',{name:'保存设置',exact:true}).click()
 await expect.poll(()=>f.config().wecom.corpId).toBe('ww-test')
 expect(f.config().wecom.userIds).toEqual(['alice','bob'])
 const secret=wecom.getByLabel('应用 Secret',{exact:true})
 await expect(secret).toHaveAttribute('type','password')
 await wecom.locator('.secret-input').first().getByRole('button',{name:'显示内容',exact:true}).click()
 await expect(secret).toHaveValue('application-secret');await expect(secret).toHaveAttribute('type','text')
 await wecom.locator('.secret-input').first().getByRole('button',{name:'隐藏内容',exact:true}).click()
 await wecom.getByRole('button',{name:'测试通知',exact:true}).click()
 await expect.poll(()=>f.tests()).toBe(1)
 await page.getByRole('link',{name:'链接转存',exact:true}).click()
 await page.getByRole('link',{name:'消息配置',exact:true}).click()
 await expect(wecom.getByLabel('企业 ID',{exact:true})).toHaveValue('ww-test')
 await page.reload();await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
 await expect(wecom.getByLabel('企业 ID',{exact:true})).toHaveValue('ww-test')
 await expect(wecom.getByLabel('应用 Secret',{exact:true})).toHaveAttribute('type','password')
 expect(await wecom.evaluate(el=>el.scrollWidth<=el.clientWidth)).toBe(true)
 await wecom.scrollIntoViewIfNeeded()
 await captureScreenshot(page,{path:info.outputPath(`wecom-${width}-${theme}.png`)})
})

test('WeCom save errors retain inputs and recover without changing TG settings',async({page})=>{
 const f=await fixture(page)
 await page.route('**/api/notification-service/wecom',route=>route.fulfill({status:400,json:{error:'EncodingAESKey 须为43位有效的企业微信编码密钥'}}))
 await page.goto('/notifications')
 const form=page.getByRole('form',{name:'企业微信',exact:true})
 await form.getByLabel('企业 ID',{exact:true}).fill('ww-test')
 await form.getByLabel('应用 Secret',{exact:true}).fill('application-secret')
 await form.getByLabel('EncodingAESKey',{exact:true}).fill('invalid')
 await form.getByRole('button',{name:'保存设置',exact:true}).click()
 await expect(page.locator('.toast.error').last()).toContainText('EncodingAESKey')
 await expect(form.getByLabel('企业 ID',{exact:true})).toHaveValue('ww-test')
 await expect(form.getByLabel('应用 Secret',{exact:true})).toHaveValue('application-secret')
 await expect(form.getByRole('button',{name:'保存设置',exact:true})).toBeEnabled()
 expect(f.config().telegram).toMatchObject({enabled:false,token:'',chatId:''})
})
