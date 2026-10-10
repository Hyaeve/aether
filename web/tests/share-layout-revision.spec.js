import { test, expect } from '@playwright/test'
import { rockPosition } from '../src/meteor.js'

async function workspace(page) {
  await page.route('**/api/**', r => {
    const path = new URL(r.request().url()).pathname
    let data = {}
    if (path === '/api/auth/status') data = { initialized: true, authenticated: true }
    if (path === '/api/state') data = { username:'layout-revision', storages:[{id:'local',type:'local',name:'资料',enabled:true,config:{}}], tasks:[], settings:{}, cache:{} }
    if (path === '/api/files') data = Array.from({length:300},(_,i)=>({id:`/目录${i}`,name:`目录${i}`,isDir:true,modified:'2026-10-09T10:00:00Z'}))
    if (path === '/api/files/rename-rules') data = [{id:'saved',name:'常用替换',rules:[{kind:'replace',find:'旧',replace:'新'}]}]
    if (path === '/api/logs') data = [{module:'storage',level:'info',time:new Date().toISOString(),message:'目录已读取'}]
    return r.fulfill({json:data})
  })
}

test('rename virtual row frames and rule footer remain visible while both panes scroll',async({page},info)=>{
  await workspace(page)
  await page.goto('/files')
  await page.getByRole('button',{name:'工具',exact:true}).click()
  await page.getByRole('menuitem',{name:'重命名',exact:true}).click()
  const rows=page.locator('.rename-preview-row')
  await expect(rows.first()).toBeVisible()
  expect(await rows.count()).toBeLessThan(40)
  const sizes=await rows.evaluateAll(el=>el.slice(0,2).map(e=>{const r=e.getBoundingClientRect();return {top:r.top,bottom:r.bottom,height:r.height}}))
  expect(sizes[0].height).toBe(66)
  expect(sizes[1].top-sizes[0].bottom).toBe(6)
  await expect(rows.first()).toHaveCSS('border-radius','8px')
  for(let i=0;i<18;i++)await page.getByRole('button',{name:'添加规则',exact:true}).click()
  const save=page.getByRole('button',{name:'保存为规则集',exact:true}), sets=page.getByRole('button',{name:'常用规则集',exact:true})
  await expect(save).toBeVisible();await expect(sets).toBeVisible()
  const before=await save.boundingBox()
  await page.locator('.rename-rules-scroll .thin-scroll-area').evaluate(el=>{el.scrollTop=el.scrollHeight})
  const after=await save.boundingBox()
  expect(Math.abs(before.y-after.y)).toBeLessThan(1)
  await page.locator('.rename-preview').evaluate(el=>{el.scrollTop=el.scrollHeight})
  await expect(rows.last()).toContainText('目录299')
  for(const pane of ['.rename-preview-scroll','.rename-rules-scroll']){
    await expect(page.locator(`${pane} .thin-scroll-rail`)).toBeVisible()
    expect(await page.locator(`${pane} .thin-scroll-thumb`).evaluate(el=>parseFloat(getComputedStyle(el,'::after').width)*devicePixelRatio)).toBeCloseTo(3,1)
  }
  await page.screenshot({path:info.outputPath('rename-footer-day.png')})
  await page.getByRole('button',{name:'主题：日光',exact:true}).click()
  await expect(page.locator('.rename-rule').last().locator('.rounded-select-trigger').first()).toHaveCSS('background-color','rgb(41, 44, 52)')
  await expect(rows.last().locator('.rename-changed > span')).toHaveCSS('color','rgb(227, 230, 237)')
  await page.screenshot({path:info.outputPath('rename-footer-night.png')})
  await page.setViewportSize({width:390,height:844})
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
})

test('storage picker toolbar is aligned and date values are larger than headers',async({page},info)=>{
  await workspace(page)
  await page.goto('/files')
  await page.getByRole('button',{name:'目录0',exact:true}).click({button:'right'})
  await page.getByRole('button',{name:'移动到',exact:true}).click()
  const toolbar=page.locator('.source-toolbar')
  await expect(toolbar).toBeVisible()
  const centers=await toolbar.evaluate(el=>[...el.children].map(c=>{const r=c.getBoundingClientRect();return r.top+r.height/2}))
  expect(Math.max(...centers)-Math.min(...centers)).toBeLessThan(1)
  await expect(page.locator('.source-columns')).toHaveCSS('font-size','12px')
  await expect(page.locator('.source-directory time').first()).toHaveCSS('font-size','14px')
  await page.screenshot({path:info.outputPath('picker-aligned.png')})
})

test('old persisted log filters are ignored on refresh and navigation',async({page})=>{
  await workspace(page)
  await page.addInitScript(()=>localStorage.setItem('aether-log-filters:layout-revision',JSON.stringify({query:'old',module:'tasks',level:'error',view:'raw'})))
  await page.goto('/logs')
  await expect(page.getByLabel('搜索日志')).toHaveValue('')
  await expect(page.getByRole('button',{name:'日志模块',exact:true})).toHaveText('全部模块')
  await page.getByLabel('搜索日志').fill('临时筛选')
  await page.getByRole('link',{name:'文件服务',exact:true}).click()
  await page.getByRole('link',{name:'系统日志',exact:true}).click()
  await expect(page.getByLabel('搜索日志')).toHaveValue('')
  await expect(page.locator('.log-entry')).toHaveCount(1)
})

test('rocks drift along the left bottom and right without leaving the canvas',()=>{
  for(let i=0;i<7;i++){
    expect(rockPosition(i,0,1000,1000)).not.toEqual(rockPosition(i,30,1000,1000))
    for(let t=0;t<=1000;t+=10){const p=rockPosition(i,t,1000,1000);expect(p.x).toBeGreaterThan(0);expect(p.x).toBeLessThan(1000);expect(p.y).toBeGreaterThan(0);expect(p.y).toBeLessThan(1000);if(i%3===0)expect(p.x).toBeLessThan(250);if(i%3===1)expect(p.y).toBeGreaterThan(800);if(i%3===2)expect(p.x).toBeGreaterThan(750)}
  }
})

test('galaxy background stays visible and animated at desktop and mobile sizes',async({page},info)=>{
  await page.route('**/api/auth/status',r=>r.fulfill({json:{initialized:true,authenticated:false}}))
  await page.goto('/')
  const canvas=page.locator('.starfield')
  for(const viewport of [{width:1440,height:1000},{width:768,height:900}]){
    await page.setViewportSize(viewport)
    await expect(canvas).toBeVisible()
    await expect.poll(()=>canvas.evaluate(el=>{
      const pixels=el.getContext('2d').getImageData(0,0,el.width,el.height).data
      let visible=0;for(let i=3;i<pixels.length;i+=4)if(pixels[i]>0)visible++
      return visible/(pixels.length/4)
    })).toBeGreaterThan(.01)
    const before=await canvas.evaluate(el=>el.toDataURL())
    await expect.poll(()=>canvas.evaluate(el=>el.toDataURL())).not.toBe(before)
    await page.screenshot({path:info.outputPath(`galaxy-${viewport.width}.png`)})
  }
  await workspace(page)
  await page.goto('/settings/about')
  await page.setViewportSize({width:390,height:844})
  await expect(canvas).toBeVisible()
  await expect.poll(()=>canvas.evaluate(el=>el.width)).toBeGreaterThan(0)
  await page.screenshot({path:info.outputPath('galaxy-about-mobile.png')})
})
