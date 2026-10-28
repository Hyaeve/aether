import {test,expect} from '@playwright/test'
import {captureScreenshot} from './helpers/screenshot.js'

test.use({browserName:'webkit',isMobile:true,hasTouch:true,deviceScaleFactor:3})

for(const [width,height,theme] of [[375,812,'light'],[844,390,'dark']]){
 test(`cleanup control and paired fields ${width} ${theme}`,async({page},info)=>{
  await page.route('**/api/**',route=>{
   const path=new URL(route.request().url()).pathname
   let json={}
   if(path==='/api/auth/status')json={initialized:true,authenticated:true}
   if(path==='/api/state')json={username:'quark-sync',storages:[],tasks:[],settings:{},traffic:{},cache:{}}
   if(path==='/api/virtual-library/quark')json={cards:[],settings:{directory:'',storageIds:[]},stats:{}}
   return route.fulfill({json})
  })
  await page.setViewportSize({width,height})
  await page.goto('/virtual/quark')
  await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
  await page.getByRole('button',{name:'添加链接',exact:true}).click()
  const editor=page.getByRole('dialog',{name:'添加夸克链接',exact:true})
  const cleanup=editor.getByRole('button',{name:'级联清理',exact:true})
  await cleanup.scrollIntoViewIfNeeded()
  await expect(cleanup).toHaveText('开启')
  await expect(cleanup).toHaveCSS('height','40px')
  expect(await editor.evaluate(e=>e.scrollWidth<=e.clientWidth)).toBe(true)
  await cleanup.focus()
  await cleanup.press('ArrowDown')
  await page.getByRole('listbox',{name:'级联清理选项',exact:true}).press('End')
  await page.getByRole('listbox',{name:'级联清理选项',exact:true}).press('Enter')
  await expect(cleanup).toHaveText('关闭')
  await expect(editor).toHaveCSS('scrollbar-width','none')
  await captureScreenshot(page,{path:info.outputPath(`cleanup-webkit-${width}-${theme}.png`)})
 })
}
