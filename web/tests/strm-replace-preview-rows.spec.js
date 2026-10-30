import {test,expect} from '@playwright/test'
import {captureScreenshot} from './helpers/screenshot.js'

async function setup(page){
  const samples=Array.from({length:80},(_,i)=>({name:`/media/很长的剧集目录/第${i}集以及完整原始名称.strm`,before:`https://old.example/${'long-source/'.repeat(15)}影片${i}.mkv`,after:`https://new.example/${'long-target/'.repeat(15)}影片${i}.mkv`,matches:[[8,11]],replacements:[[8,11]]}))
  const writes=[],unexpected=[]
  await page.route('**/api/**',route=>{
    const req=route.request(),p=new URL(req.url()).pathname
    if(p==='/api/auth/status')return route.fulfill({json:{initialized:true,authenticated:true}})
    if(p==='/api/state')return route.fulfill({json:{username:'replace-rows',storages:[],tasks:[],settings:{},traffic:{},cache:{}}})
    if(p==='/api/strm-replace/preview')return route.fulfill({json:{count:120,samples}})
    if(p==='/api/strm-replace'){if(req.method()==='POST')writes.push(req.postDataJSON());return route.fulfill({json:{tasks:[]}})}
    if(['/api/traffic','/api/quark-takeover','/api/115-recycle-cleanup','/api/115-folder-cleanup','/api/115-signin','/api/115-simulcast/enabled','/api/plugins/emby','/api/plugins/tmdb','/api/plugins/ai'].includes(p))return route.fulfill({json:{}})
    unexpected.push(`${req.method()} ${p}`)
    return route.fulfill({status:500,json:{error:`Unmocked API: ${p}`}})
  })
  return {samples,writes,unexpected}
}

const layouts=[...([1440,390].flatMap(width=>['light','dark'].map(theme=>({width,height:850,theme})))),{width:375,height:812,theme:'light'},{width:844,height:390,theme:'dark'}]
for(const {width,height,theme} of layouts)test(`replacement preview uses single rows and stationary full text ${width} ${theme}`,async({page},info)=>{
  const fixture=await setup(page)
  await page.addInitScript(theme=>localStorage.setItem('aether-theme',theme),theme)
  await page.setViewportSize({width,height})
  await page.goto('/tools')
  await page.getByRole('button',{name:'STRM 替换',exact:true}).click()
  const editor=page.getByRole('dialog',{name:'STRM 内容替换',exact:true})
  await editor.getByLabel('替换目录',{exact:true}).fill('/media')
  await editor.getByLabel('匹配字段',{exact:true}).fill('old')
  await editor.getByLabel('替换字段',{exact:true}).fill('new')
  await editor.getByRole('button',{name:'预览',exact:true}).click()
  const dialog=page.getByRole('dialog',{name:'替换预览',exact:true})
  await expect(dialog).toBeVisible()
  await expect(editor).toHaveCount(0)
  const rows=dialog.locator('.replace-preview-row')
  await expect(rows).toHaveCount(80)
  const scroll=dialog.locator('.replace-preview-list')
  await expect(scroll).toHaveCSS('scrollbar-width','none')
  expect(await scroll.evaluate(el=>el.scrollHeight>el.clientHeight)).toBe(true)
  expect(await dialog.evaluate(el=>el.scrollWidth<=el.clientWidth)).toBe(true)
  const first=rows.first()
  for(const text of await first.locator('.replace-preview-text').all()){
    await expect(text).toHaveCSS('white-space','nowrap')
    await expect(text).toHaveCSS('text-overflow','ellipsis')
  }
  await expect(first.locator('mark.match')).toHaveText('old')
  await expect(first.locator('mark.replacement')).toHaveText('new')
  await page.clock.install({time:new Date('2030-01-01T00:00:00Z')})
  await page.clock.pauseAt(new Date('2030-01-01T00:01:00Z'))
  const original=first.locator('.replace-preview-before')
  await original.hover()
  await page.clock.runFor(200)
  const position=await original.boundingBox()
  await page.mouse.move(position.x+10,position.y+10)
  await page.clock.runFor(299)
  await expect(page.getByRole('tooltip')).toHaveCount(0)
  await page.clock.runFor(1)
  await expect(page.getByRole('tooltip')).toHaveText(fixture.samples[0].before)
  await page.clock.resume()
  await captureScreenshot(page,{path:info.outputPath(`replace-tooltip-${width}-${theme}.png`)})
  await page.mouse.move(2,2)
  await expect(page.getByRole('tooltip')).toHaveCount(0)
  await scroll.evaluate(el=>{el.scrollTop=el.scrollHeight})
  await expect(rows.last()).toBeVisible()
  await captureScreenshot(page,{path:info.outputPath(`replace-preview-${width}-${theme}.png`)})
  await dialog.getByRole('button',{name:'返回',exact:true}).click()
  await expect(editor.getByLabel('匹配字段',{exact:true})).toHaveValue('old')
  await expect(editor.getByLabel('替换字段',{exact:true})).toHaveValue('new')
  expect(fixture.writes).toEqual([])
  expect(fixture.unexpected).toEqual([])
})
