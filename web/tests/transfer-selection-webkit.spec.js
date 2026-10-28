import {test,expect} from '@playwright/test'
import {captureScreenshot} from './helpers/screenshot.js'
test.use({browserName:'webkit',hasTouch:true,deviceScaleFactor:2,reducedMotion:'reduce'})
for(const [width,height,theme] of [[375,812,'light'],[844,390,'dark']])test(`transfer selection remains usable in WebKit ${width} ${theme}`,async({page},info)=>{
  const progress={done:25,total:100}
  await page.route('**/api/**',r=>{
    const path=new URL(r.request().url()).pathname
    const json=path==='/api/auth/status'?{initialized:true,authenticated:true}:path==='/api/state'?{username:'transfer-webkit',storages:[],tasks:[],settings:{},traffic:{},cache:{}}:path==='/api/transfers'?[{id:'one',kind:'copy',name:'影片.mkv',status:'running',...progress,controls:{pause:true,delete:true},from:{storage:'来源',type:'mobile',directory:'影片'},to:{storage:'目标',type:'local',directory:'媒体'}}]:{}
    return r.fulfill({json})
  })
  await page.setViewportSize({width,height})
  await page.goto('/transfer')
  await page.evaluate(theme=>document.documentElement.dataset.theme=theme,theme)
  const row=page.locator('.transfer-table tbody tr').first()
  await row.click({button:'right'})
  const dock=page.getByRole('toolbar',{name:'所选传输任务操作'})
  await expect(dock).toBeVisible()
  await expect(dock.getByRole('button',{name:'暂停',exact:true})).toBeEnabled()
  await expect(dock.getByRole('button',{name:'暂停',exact:true}).locator('span')).toHaveCSS('transform','none')
  await expect(row.locator('progress')).toHaveAttribute('value','25')
  progress.done=26_000_000
  progress.total=85_118_265_822
  await page.getByRole('button',{name:'刷新传输任务',exact:true}).click()
  await expect(row.locator('progress')).toHaveAttribute('value','0.03')
  await expect(row.locator('.transfer-status')).toHaveText('进行0.03%')
  expect(await row.locator('progress').evaluate(el=>el.getBoundingClientRect().width)).toBeLessThanOrEqual(108)
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
  await captureScreenshot(page,{path:info.outputPath(`transfer-webkit-${width}-${theme}.png`)})
  await page.keyboard.press('Escape')
  await expect(dock).toHaveCount(0)
})
