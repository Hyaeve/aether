import {test,expect} from '@playwright/test'
import {captureScreenshot} from './helpers/screenshot.js'
test.use({browserName:'webkit',isMobile:true,hasTouch:true,deviceScaleFactor:3})

for(const [width,height,safe,theme]of[[390,844,47,'light'],[844,390,0,'dark']])test(`iOS safe-area task center controls ${width} ${theme}`,async({page},info)=>{
  await page.route('**/api/**',r=>{
    const p=new URL(r.request().url()).pathname
    const json=p==='/api/auth/status'?{initialized:true,authenticated:true}:p==='/api/state'?{username:'ios-task',storages:[],tasks:[],settings:{},traffic:{},cache:{}}:p==='/api/tasks/panel'?{items:[]}:{}
    return r.fulfill({json})
  })
  await page.setViewportSize({width,height})
  await page.goto('/tasks')
  await page.evaluate(({safe,theme})=>{document.documentElement.style.setProperty('--aether-safe-top',`${safe}px`);document.documentElement.dataset.theme=theme},{safe,theme})
  await expect(page.getByText('暂无正在进行的任务',{exact:true})).toBeVisible()
  await expect.poll(()=>page.locator('.topbar').evaluate(el=>el.getBoundingClientRect().top)).toBe(safe)
  await page.getByRole('button',{name:'任务通知',exact:true}).click()
  const dropdown=await page.locator('.notification-dropdown').boundingBox()
  expect(dropdown.y).toBeGreaterThanOrEqual(safe+48)
  await page.getByRole('button',{name:'任务通知',exact:true}).click()
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
  await captureScreenshot(page,{path:info.outputPath(`task-safe-webkit-${width}-${theme}.png`)})
})
