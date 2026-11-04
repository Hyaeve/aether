import {test,expect} from '@playwright/test'
import {captureScreenshot} from './helpers/screenshot.js'

test.use({browserName:process.env.AETHER_SIDEBAR_BROWSER||'chromium'})
async function setup(page){
 await page.route('**/api/**',route=>{
  const p=new URL(route.request().url()).pathname
  let json={}
  if(p==='/api/auth/status')json={initialized:true,authenticated:true}
  else if(p==='/api/state')json={username:'sidebar-hint',storages:[],tasks:[],settings:{},cache:{},traffic:{}}
  else if(p==='/api/logs')json=[]
  return route.fulfill({json})
 })
}

for(const [width,height,theme]of [[1440,600,'light'],[1440,600,'dark'],[390,700,'dark'],[844,390,'light']])test(`sidebar edge hint follows overflow without adding a track ${width} ${theme}`,async({page},info)=>{
 await setup(page);await page.setViewportSize({width,height});await page.goto('/dashboard')
 await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
 const toggle=page.getByRole('button',{name:'打开导航',exact:true})
 if(await toggle.isVisible()){
  await toggle.click()
  await expect(page.locator('.sidebar')).toHaveCSS('transform','matrix(1, 0, 0, 1, 0, 0)')
 }
 const nav=page.getByRole('navigation',{name:'主导航',exact:true}),hint=page.locator('.sidebar-scroll-hint')
 await expect(hint).toBeVisible();await expect(hint).toHaveAttribute('aria-hidden','true');await expect(hint).toHaveCSS('pointer-events','none');await expect(nav).toHaveCSS('scrollbar-width','none')
 const placement=()=>page.evaluate(()=>{const el=document.querySelector('.sidebar-scroll-hint'),r=el.getBoundingClientRect(),side=el.closest('aside').getBoundingClientRect(),nav=el.closest('aside').querySelector('nav').getBoundingClientRect();return{top:r.top,bottom:r.bottom,width:r.width,right:side.right-r.right,navTop:nav.top,navBottom:nav.bottom}})
 const top=await placement();expect(top.width).toBe(2);expect(top.right).toBe(0);expect(top.top).toBeGreaterThanOrEqual(top.navTop);expect(top.bottom).toBeLessThan(top.navBottom)
 const distance=await nav.evaluate(el=>(el.scrollHeight-el.clientHeight)/2)
 await nav.hover()
 await page.mouse.wheel(0,distance)
 await expect.poll(async()=> (await placement()).top).toBeGreaterThan(top.top+10)
 let previous=-1,stable=0
 await expect.poll(async()=>{const value=await page.evaluate(()=>document.querySelector('.sidebar nav').scrollTop);stable=Math.abs(value-previous)<.1?stable+1:0;previous=value;return stable}).toBeGreaterThanOrEqual(2)
 const middle=await placement()
 await nav.getByRole('link',{name:'系统设置',exact:true}).focus()
 await expect.poll(async()=> (await placement()).top).toBeGreaterThan(middle.top+10)
 await nav.evaluate(el=>{el.scrollTop=el.scrollHeight})
 await expect.poll(async()=>{const r=await placement();return r.navBottom-r.bottom}).toBeLessThanOrEqual(20)
 const end=await placement();expect(end.bottom).toBeLessThanOrEqual(end.navBottom);expect(end.navBottom-end.bottom).toBeLessThanOrEqual(20)
 await captureScreenshot(page,{path:info.outputPath(`sidebar-edge-${width}-${theme}.png`)})
 await nav.hover()
 await page.mouse.wheel(0,-2*await page.evaluate(()=>document.querySelector('.sidebar nav').scrollHeight))
 await expect.poll(()=>page.evaluate(()=>document.querySelector('.sidebar nav').scrollTop)).toBe(0)
 await expect.poll(async()=> (await placement()).top).toBeCloseTo(top.top,0)
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
})

test('sidebar edge hint hides when navigation fits and keeps local about theme',async({page})=>{
 await setup(page);await page.setViewportSize({width:1440,height:1400});await page.goto('/dashboard')
 const hint=page.locator('.sidebar-scroll-hint')
 await expect(hint).toBeHidden()
 await page.setViewportSize({width:1440,height:600});await expect(hint).toBeVisible()
 await page.emulateMedia({reducedMotion:'reduce'})
 await page.getByRole('navigation',{name:'主导航',exact:true}).evaluate(el=>{el.style.fontSize='200%';el.querySelectorAll('[role=link]').forEach(node=>node.style.fontSize='30px')})
 await expect(hint).toBeVisible();await expect(hint).toHaveCSS('transition-duration','0s')
 await page.goto('/settings/about');await expect(page.locator('.sidebar')).toHaveAttribute('data-theme','dark');await expect(hint).toBeVisible()
 expect(await hint.evaluate(el=>getComputedStyle(el).getPropertyValue('--sidebar-brand').trim())).toBe('#fff')
 await page.setViewportSize({width:1440,height:1400});await expect(hint).toBeHidden()
})
