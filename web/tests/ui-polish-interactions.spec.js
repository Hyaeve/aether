import { test, expect } from '@playwright/test'
import { captureScreenshot } from './helpers/screenshot.js'

async function setup(page,count=120) {
  await page.addInitScript(()=>localStorage.setItem('aether-file-history:polish',JSON.stringify([{storage:'local',id:'/a/b',history:[{id:'/',name:'A'},{id:'/a',name:'B'}]}])))
  await page.route('**/api/**',r=>{
    const path=new URL(r.request().url()).pathname
    const files=Array.from({length:count},(_,i)=>({id:`/entry-${i}`,name:`项目 ${i}.txt`,isDir:false,size:i+1,modified:'2026-10-12T00:00:00Z'}))
    let json={}
    if(path==='/api/auth/status')json={initialized:true,authenticated:true}
    if(path==='/api/state')json={username:'polish',storages:[{id:'local',name:'本地资料',type:'local',enabled:true,config:{}}],tasks:[],settings:{},cache:{},traffic:{}}
    if(path==='/api/files')json=files
    if(path==='/api/files/visits')json=r.request().postDataJSON().items
    if(path==='/api/files/rename-rules')json=[]
    return r.fulfill({json})
  })
  await page.goto('/files')
  await expect(page.locator('.file-row').first()).toBeVisible()
}

for (const width of [1440,390]) for (const theme of ['light','dark']) test(`history click opens and toggles without hover activation at ${width} ${theme}`,async({page},info)=>{
  await page.setViewportSize({width,height:900})
  await page.addInitScript(value=>localStorage.setItem('aether-theme',value),theme)
  await setup(page,4)
  const history=page.getByRole('button',{name:'历史访问',exact:true}),menu=page.locator('.file-visit-history [role=listbox]')
  await page.clock.install()
  await history.hover();await page.clock.runFor(1200)
  await expect(history).toHaveAttribute('aria-expanded','false')
  await expect(menu).toHaveCount(0)
  await history.click();await expect(menu).toBeVisible();await expect(menu).toBeFocused()
  await expect(history).toHaveAttribute('aria-expanded','true')
  await page.mouse.move(0,0);await page.clock.fastForward(1000)
  await expect(menu).toBeVisible()
  await expect(menu).toHaveCSS('transform','none')
  const menuBounds=await menu.boundingBox()
  expect(menuBounds.x).toBeGreaterThanOrEqual(16)
  expect(menuBounds.x+menuBounds.width).toBeLessThanOrEqual(width-16)
  await expect(menu.locator('.visit-deepest')).toHaveText('B')
  await captureScreenshot(page,{path:info.outputPath(`history-click-${width}-${theme}.png`)})
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
  await history.click();await expect(menu).toHaveCount(0)
  await history.click();await expect(menu).toBeVisible()
  await page.locator('.breadcrumbs').click();await expect(menu).toHaveCount(0)
  await history.focus();await page.keyboard.press('Enter');await expect(menu).toBeFocused()
  await page.keyboard.press('Escape');await expect(menu).toHaveCount(0);await expect(history).toBeFocused()
  await page.keyboard.press('ArrowDown');await expect(menu).toBeFocused()
  await page.keyboard.press('Enter')
  await expect(menu).toHaveCount(0)
  await expect(page.locator('.path-bar')).toContainText('B')
  const buttons=page.locator('.file-toolbar > button')
  const names=await buttons.evaluateAll(els=>els.map(el=>el.getAttribute('aria-label')))
  expect(names.indexOf('刷新目录')).toBe(names.findIndex(n=>n?.includes('切换'))-1)
})

test('details enter and leave keep both scroll offsets and use a real exit transition',async({page})=>{
  await setup(page)
  const outer=page.locator('.page-scroll > .thin-scroll-area'),inner=page.locator('.file-view')
  await outer.evaluate(el=>el.scrollTop=24)
  const offsets=async()=>[await outer.evaluate(el=>el.scrollTop),await inner.evaluate(el=>el.scrollTop)]
  const before=await offsets()
  await page.locator('.file-row').first().click({button:'right'})
  await page.getByRole('button',{name:'查看详情',exact:true}).click()
  const drawer=page.getByRole('dialog',{name:'文件详情',exact:true})
  await expect(drawer).toBeFocused()
  expect(await offsets()).toEqual(before)
  await page.locator('.breadcrumbs').click()
  await expect(drawer).toHaveClass(/details-slide-leave-active/)
  await expect(drawer).toHaveCount(0)
  expect(await offsets()).toEqual(before)
})

test('fixed workbench keeps its count at the list end and directory beside the title on both viewports',async({page},info)=>{
  await setup(page)
  await page.getByRole('button',{name:'工具',exact:true}).click()
  await page.getByRole('menuitem',{name:'命名工作台',exact:true}).click()
  const outer=page.locator('.page-scroll > .thin-scroll-area'),left=page.locator('.rename-preview-scroll > .thin-scroll-area'),count=page.locator('.rename-count')
  await expect(page.locator('.rename-current-directory')).toContainText('本地资料')
  expect(await page.locator('.rename-current-directory').evaluate(el=>el.previousElementSibling.textContent)).toBe('命名工作台')
  for(const width of [1440,390]) {
    await page.setViewportSize({width,height:844})
    await left.evaluate(el=>el.scrollTop=0)
    await expect.poll(()=>outer.evaluate(el=>el.scrollHeight-el.clientHeight)).toBeLessThan(2)
    await expect(count).not.toBeInViewport()
    await left.evaluate(el=>el.scrollTop=el.scrollHeight)
    await expect(count).toBeInViewport()
    await expect(count).toHaveText('120 项')
    expect(await page.locator('.rename-preview-row').count()).toBeLessThan(25)
    expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
    await page.screenshot({path:info.outputPath(`rename-fixed-${width}.png`)})
  }
})
