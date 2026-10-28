import {test,expect} from '@playwright/test'
import {captureScreenshot} from './helpers/screenshot.js'

async function fixture(page) {
  await page.route('**/api/**',route=>{
    const p=new URL(route.request().url()).pathname
    const task={id:'one',name:'用于核对最近通知时间位置的完整长标题'.repeat(3),kind:'strm',status:'success',lastRun:'2026-10-28T12:00:00Z'}
    let json={}
    if(p==='/api/auth/status')json={initialized:true,authenticated:true}
    else if(p==='/api/state')json={username:'log-back-top',storages:[],tasks:[task],settings:{},traffic:{},cache:{}}
    else if(p==='/api/logs')json=Array.from({length:900},(_,i)=>({time:`2026-10-28T12:${String(i%60).padStart(2,'0')}:00Z`,level:'info',module:'tasks',message:`记录 ${i}：${'用于检查长日志与返回顶部。'.repeat(12)}`}))
    return route.fulfill({json})
  })
}

for(const width of [1440,375])for(const theme of ['light','dark'])test(`logs back to top, thicker page rail and inset notice time ${width} ${theme}`,async({page},info)=>{
  await fixture(page)
  await page.setViewportSize({width,height:900})
  await page.goto('/logs')
  await page.evaluate(theme=>document.documentElement.dataset.theme=theme,theme)
  const host=page.locator('.page-scroll > .thin-scroll-area')
  const top=page.getByRole('button',{name:'回到顶部',exact:true})
  await expect(page.locator('.log-entry').first()).toContainText('记录 899')
  await expect(top).toHaveCount(0)
  for(const raw of [false,true]){
    if(raw)await page.getByRole('button',{name:'当前结构化列表，切换原始列表',exact:true}).click()
    await host.evaluate(el=>{el.scrollTop=el.clientHeight*2})
    await expect(top).toBeVisible()
    await expect(top.locator('svg')).toHaveClass(/lucide-arrow-up/)
    const rect=await top.boundingBox()
    expect(rect.x+rect.width).toBeLessThanOrEqual(width-20)
    expect(rect.y+rect.height).toBeLessThanOrEqual(880)
    if(!raw)await captureScreenshot(page,{path:info.outputPath(`logs-back-${width}-${theme}.png`)})
    await top.click()
    await expect.poll(()=>host.evaluate(el=>el.scrollTop)).toBe(0)
    await expect(top).toHaveCount(0)
    await expect(page.locator('.log-entry').first()).toContainText('记录 899')
  }
  const rail=page.locator('.page-scroll > .thin-scroll-rail .thin-scroll-thumb')
  await expect.poll(()=>rail.evaluate(el=>parseFloat(getComputedStyle(el,'::after').width)*devicePixelRatio)).toBeCloseTo(8,1)
  await page.getByRole('button',{name:'任务通知',exact:true}).click()
  const time=page.locator('.notification-list time').first()
  await expect(time).toHaveCSS('margin-inline-end','6px')
  await expect(time).toHaveCSS('font-size','13px')
  await expect(time).toHaveCSS('white-space','nowrap')
  await captureScreenshot(page,{path:info.outputPath(`notice-inset-${width}-${theme}.png`)})
  await page.getByRole('button',{name:'任务通知',exact:true}).click()
  await page.goto('/dashboard')
  await expect(top).toHaveCount(0)
})
