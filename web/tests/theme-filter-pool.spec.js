import {test,expect} from '@playwright/test'
import {transferStates} from '../src/transfer-status.mjs'
import {captureScreenshot} from './helpers/screenshot.js'

async function fixture(page,theme='light'){
 const unexpected=[]
 const storages=[{id:'p',type:'115',name:'家庭影音主账号超长存储池名称',enabled:true,config:{}},{id:'q',type:'quark',name:'夸克备用账号',enabled:true,config:{}}]
 await page.addInitScript(t=>localStorage.setItem('aether-theme',t),theme)
 await page.route('**/api/**',route=>{
  const p=new URL(route.request().url()).pathname
  let json
  if(p==='/api/auth/status')json={initialized:true,authenticated:true}
  else if(p==='/api/state')json={username:'theme-filter-pool',storages,tasks:[],settings:{},traffic:{},cache:{},logs:[]}
  else if(p==='/api/transfers')json=['copy','upload','download'].flatMap(kind=>transferStates.map(s=>({id:`${kind}-${s.value}`,kind,status:s.value,name:`${kind}-${s.value}.mkv`,done:25,total:100,from:{storage:'源',directory:'电影'},to:{storage:'目标',directory:'电影'}})))
  else if(p==='/api/cloud/manage')json={items:[{id:'one',name:'影片.mkv',size:100,path:'/电影',status:'success',url:'https://example.test/share',time:'2026-10-31T00:00:00Z'}]}
  else if(p==='/api/strm-replace'||p==='/api/backup-rules')json=[]
  else if(['/api/traffic','/api/notifications','/api/plugins/proxy'].includes(p))json={}
  else{unexpected.push(p);return route.fulfill({status:500,json:{error:`Unexpected API ${p}`}})}
  return route.fulfill({json})
 })
 return unexpected
}

for(const theme of ['light','dark'])test(`neutral light layers and preserved night surfaces ${theme}`,async({page},info)=>{
 const unexpected=await fixture(page,theme)
 await page.goto('/settings/account')
 const keys=['--bg','--layer-shell','--layer-frame','--surface','--layer-item','--layer-rule','--input']
 const colors=await page.evaluate(keys=>Object.fromEntries(keys.map(k=>[k,getComputedStyle(document.documentElement).getPropertyValue(k).trim()])),keys)
 if(theme==='light'){
  expect(colors).toEqual({'--bg':'#f1f3f6','--layer-shell':'#f3f5f8','--layer-frame':'#f6f8fa','--surface':'#f8fafc','--layer-item':'#fafcfd','--layer-rule':'#fdfefe','--input':'#fff'})
  const channels=keys.map(k=>colors[k]==='#fff'?[255,255,255]:colors[k].slice(1).match(/../g).map(c=>parseInt(c,16)))
  for(let i=1;i<channels.length;i++)for(let c=0;c<3;c++)expect(channels[i][c]).toBeGreaterThanOrEqual(channels[i-1][c])
  expect(channels[0][2]-channels[0][0]).toBeLessThanOrEqual(6)
  await expect(page.locator('body')).toHaveCSS('background-color','rgb(241, 243, 246)')
  await expect(page.locator('.sidebar')).toHaveCSS('background-color','rgb(217, 224, 239)')
  await expect(page.locator('.account-panel').first()).toHaveCSS('background-color','rgb(248, 250, 252)')
  await expect(page.locator('.credential-input').first()).toHaveCSS('background-color','rgb(255, 255, 255)')
 }else{
  expect(colors).toEqual({'--bg':'#111216','--layer-shell':'#131419','--layer-frame':'#17191e','--surface':'#1b1d23','--layer-item':'#22252d','--layer-rule':'#262831','--input':'#292c34'})
 }
 await captureScreenshot(page,{path:info.outputPath(`layers-${theme}.png`)})
 expect(unexpected).toEqual([])
})

test('transfer status replaces the previous choice and never toggles the same status off',async({page},info)=>{
 const unexpected=await fixture(page)
 await page.goto('/transfer')
 const trigger=page.getByRole('button',{name:'筛选传输状态',exact:true}),menu=page.getByRole('menu',{name:'传输状态筛选'}),rows=page.locator('.transfer-table tbody tr:not(.transfer-spacer)')
 for(const status of ['失败','成功','成功']){
  await trigger.click()
  await menu.getByRole('menuitemradio',{name:status,exact:true}).click()
  await expect(menu).toHaveCount(0)
  await expect(trigger).toBeFocused()
  await expect(rows).toHaveCount(1)
  await trigger.click()
  await expect(menu.locator('[aria-checked=true]')).toHaveCount(1)
  await expect(menu.getByRole('menuitemradio',{name:status,exact:true})).toHaveAttribute('aria-checked','true')
  await expect(menu.locator('.transfer-filter-check svg')).toHaveCount(1)
  await expect(menu.getByRole('menuitemcheckbox')).toHaveCount(0)
  await trigger.click()
 }
 await trigger.click()
 await captureScreenshot(page,{path:info.outputPath('single-filter.png')})
 await menu.getByRole('menuitemradio',{name:'所有状态',exact:true}).click()
 await expect(rows).toHaveCount(10)
 expect(unexpected).toEqual([])
})

for(const [width,height,theme] of [[1440,950,'light'],[600,900,'light'],[375,812,'light'],[390,844,'dark'],[844,390,'dark']]){
 test(`cloud pool triggers use three quarters width ${width} ${theme}`,async({page},info)=>{
  const unexpected=await fixture(page,theme)
  await page.setViewportSize({width,height})
  for(const kind of ['shares','downloads','trash']){
   await page.goto(`/storage/${kind}`)
   const trigger=page.getByRole('button',{name:'选择云盘存储池',exact:true})
   await expect(trigger).toBeEnabled()
   const expected=width<=440?93:width<=760?105:135
   await expect(trigger).toHaveCSS('width',`${expected}px`)
   const b=await trigger.boundingBox()
   expect(b.x).toBeGreaterThanOrEqual(0);expect(b.x+b.width).toBeLessThanOrEqual(width)
   await expect(trigger.locator('.provider-icon')).toBeVisible()
   await expect(trigger.locator('.select-label>span:last-child')).toHaveCSS('text-overflow','ellipsis')
   expect(await trigger.evaluate(el=>el.scrollWidth<=el.clientWidth)).toBe(true)
   const layout=()=>page.locator('.cloud-management-panel').evaluate(el=>{
    const box=el.getBoundingClientRect()
    return {left:box.left,right:box.right,viewport:innerWidth,ancestors:[...document.querySelectorAll('html,body,.main-shell,.page-content')].map(node=>({class:node.className,scrollLeft:node.scrollLeft}))}
   })
   await info.attach(`${kind}-before-menu`,{body:JSON.stringify(await layout()),contentType:'application/json'})
   await trigger.click()
   await expect(page.getByRole('option',{name:'家庭影音主账号超长存储池名称',exact:true})).toBeVisible()
   await page.getByRole('option',{name:'家庭影音主账号超长存储池名称',exact:true}).click()
   await info.attach(`${kind}-after-menu`,{body:JSON.stringify(await layout()),contentType:'application/json'})
   await expect.poll(async()=>{
    const box=await layout()
    return box.left>=0&&box.right<=box.viewport
   }).toBe(true)
   expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
  }
  await captureScreenshot(page,{path:info.outputPath(`pool-${width}-${theme}.png`)})
  expect(unexpected).toEqual([])
 })
}
