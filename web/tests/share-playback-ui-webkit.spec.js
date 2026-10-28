import {test,expect} from '@playwright/test'
import {captureScreenshot} from './helpers/screenshot.js'
test.use({browserName:'webkit',hasTouch:true,deviceScaleFactor:2,reducedMotion:'reduce'})

for(const [width,height,theme]of[[375,812,'light'],[844,390,'dark']])test(`logs return to top and eight pixel rail WebKit ${width} ${theme}`,async({page},info)=>{
  await page.route('**/api/**',r=>{
    const p=new URL(r.request().url()).pathname
    const json=p==='/api/auth/status'?{initialized:true,authenticated:true}:p==='/api/state'?{username:'logs-webkit',settings:{},storages:[],tasks:[],traffic:{},cache:{}}:p==='/api/logs'?Array.from({length:600},(_,i)=>({time:'2026-10-28T12:00:00Z',module:'tasks',level:'info',message:`记录 ${i}` })):{}
    return r.fulfill({json})
  })
  await page.setViewportSize({width,height})
  await page.goto('/logs')
  await page.evaluate(theme=>document.documentElement.dataset.theme=theme,theme)
  await expect(page.locator('.log-entry').first()).toContainText('记录 599')
  const host=page.locator('.page-scroll > .thin-scroll-area')
  await host.evaluate(el=>{el.scrollTop=el.clientHeight*2})
  const button=page.getByRole('button',{name:'回到顶部',exact:true})
  await expect(button).toBeVisible()
  await expect.poll(()=>page.locator('.page-scroll > .thin-scroll-rail .thin-scroll-thumb').evaluate(el=>parseFloat(getComputedStyle(el,'::after').width)*devicePixelRatio)).toBeCloseTo(8,1)
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
  await captureScreenshot(page,{path:info.outputPath(`logs-top-webkit-${width}-${theme}.png`)})
  await button.focus()
  await page.keyboard.press('Enter')
  await expect.poll(()=>host.evaluate(el=>el.scrollTop)).toBe(0)
  await expect(button).toHaveCount(0)
  await expect(page.locator('.log-viewport')).toBeFocused()
})
