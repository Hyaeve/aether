import { test, expect } from '@playwright/test'

async function fixture(page) {
  const files = [{id:'Show/one.strm',name:'one.strm',size:40}, {id:'Show/tvshow.nfo',name:'tvshow.nfo',size:50}, {id:'Show/cover.png',name:'cover.png',size:70,url:'/providers/quark.png'}]
  let saved
  await page.route('**/api/**', r => {
    const u = new URL(r.request().url()), action = u.pathname.split('/').at(-1)
    let json = {}
    if (action === 'status' && u.pathname.includes('/auth/')) json = {initialized:true,authenticated:true}
    if (action === 'state') json = {username:'scrape-files',storages:[{id:'local',type:'local',name:'Local',enabled:true,config:{}}],tasks:[{id:'t',kind:'strm',name:'作品库'}],settings:{},cache:{},traffic:{}}
    if (action === 'items') json = [{path:'Show/one.strm',directories:['Show'],title:'Show',kind:'tv',status:'ok',tmdb:1}]
    if (action === 'directories') json = {root:'/media/strm',directories:['Show']}
    if (action === 'files') {
      const path = u.searchParams.get('path')
      if (r.request().method() === 'PUT') { saved = r.request().postDataJSON(); json = {ok:true} }
      else if (path === '.') json = Array.from({length:1000},(_,i)=>({id:i === 0 ? 'Show' : `Folder${i}`,name:i === 0 ? 'Show' : `Folder${i}`,isDir:true}))
      else if (path === 'Show') json = files
      else json = {content:path.endsWith('.nfo') ? '<tvshow><title>Show</title></tvshow>' : 'http://aether/d/file', revision:'v1'}
    }
    return r.fulfill({json})
  })
  return () => saved
}

test('scrape folders are virtual, searchable, persistent and preview editable library files', async ({page},info) => {
  const saved = await fixture(page)
  await page.goto('/tasks/scrape')
  await expect(page.getByRole('button',{name:'海报墙视图',exact:true})).toBeVisible()
  const refresh = await page.getByRole('button',{name:'刷新刮削索引'}).boundingBox(), search = await page.getByRole('textbox',{name:'搜索刮削记录'}).boundingBox()
  expect(refresh.x + refresh.width).toBeLessThan(search.x)
  await page.getByRole('button',{name:'海报墙视图',exact:true}).click()
  await expect(page.getByRole('button',{name:'文件夹视图',exact:true})).toBeVisible()
  await expect(page.locator('.scrape-file-row').first()).toContainText('Show')
  expect(await page.locator('.scrape-file-row').count()).toBeLessThan(60)
  await expect(page.locator('.scrape-progress')).toHaveCount(0)
  await expect(page.locator('.scrape-panel')).toHaveCSS('border-radius','8px')
  expect((await page.locator('.scrape-panel').boundingBox()).height).toBeGreaterThan(55000)
  await expect(page.locator('.scrape-panel > footer')).toHaveText('1000 项')
  expect(await page.locator('.scrape-files').evaluate(el=>el.scrollHeight-el.clientHeight)).toBeLessThan(2)
  await page.locator('.page-scroll > .thin-scroll-area').evaluate(el=>el.scrollTop = el.scrollHeight)
  await expect(page.locator('.scrape-file-row').last()).toContainText('Folder999')
  expect((await page.locator('.scrape-toolbar').boundingBox()).y).toBeLessThan(0)
  expect((await page.locator('.topbar').boundingBox()).y).toBe(0)
  await page.screenshot({path:info.outputPath('scrape-folder-bottom.png')})
  await page.getByRole('textbox',{name:'搜索刮削记录'}).fill('Show')
  await expect(page.locator('.scrape-file-row')).toHaveCount(1)
  await expect(page.locator('.scrape-panel > footer')).toHaveText('1 项')
  expect((await page.locator('.scrape-file-row').boundingBox()).y).toBeGreaterThanOrEqual((await page.locator('.scrape-toolbar').boundingBox()).y+(await page.locator('.scrape-toolbar').boundingBox()).height)
  await page.locator('.scrape-file-row').hover()
  await expect(page.locator('.scrape-folder-actions')).toHaveCSS('opacity','1')
  await expect(page.locator('.scrape-folder-actions')).toContainText('重置识别')
  await page.getByRole('textbox',{name:'搜索刮削记录'}).clear()
  await page.reload()
  await expect(page.getByRole('button',{name:'文件夹视图',exact:true})).toBeVisible()
  await page.getByRole('button',{name:'Show',exact:true}).click()
  await expect(page.locator('.scrape-panel > footer')).toHaveText('3 项')
  await expect(page.locator('.scrape-file-path')).toContainText('根目录Show')
  await expect(page.getByRole('button',{name:'one.strm',exact:true}).locator('svg')).toHaveClass(/lucide-file-symlink/)
  await expect(page.getByRole('button',{name:'tvshow.nfo',exact:true}).locator('svg')).toHaveClass(/lucide-file-text/)
  await page.getByRole('button',{name:'one.strm',exact:true}).click()
  await expect(page.getByRole('textbox',{name:'文件内容'})).toHaveValue('http://aether/d/file')
  await page.getByRole('button',{name:'取消',exact:true}).click()
  await page.getByRole('button',{name:'tvshow.nfo',exact:true}).click()
  await page.getByRole('textbox',{name:'文件内容'}).fill('<tvshow><title>Edited</title></tvshow>')
  await page.screenshot({path:info.outputPath('nfo-editor.png')})
  await page.getByRole('button',{name:'保存',exact:true}).click()
  await expect.poll(saved).toEqual({content:'<tvshow><title>Edited</title></tvshow>',revision:'v1'})
  await page.getByRole('button',{name:'cover.png',exact:true}).click()
  await expect(page.locator('.image-viewer img.loaded')).toBeVisible()
  await page.keyboard.press('Escape')
  await page.getByRole('button',{name:'根目录',exact:true}).click()
  await expect(page.getByRole('button',{name:'Show',exact:true})).toBeVisible()
  for (const theme of ['light','dark']) {
    await page.evaluate(theme=>document.documentElement.dataset.theme=theme,theme)
    await expect(page.locator('.scrape-file-row .file-name').first()).toHaveCSS('color',theme==='dark'?'rgb(227, 230, 237)':'rgb(39, 49, 62)')
    await expect(page.getByRole('button',{name:'STRM 任务',exact:true})).toHaveCSS('background-color',theme==='dark'?'rgb(41, 44, 52)':'rgb(255, 255, 255)')
    await page.screenshot({path:info.outputPath(`scrape-folder-${theme}.png`)})
  }
  await page.setViewportSize({width:390,height:844})
  expect(await page.locator('.scrape-panel').evaluate(el=>el.scrollWidth <= el.clientWidth)).toBe(true)
  await page.screenshot({path:info.outputPath('scrape-folder-mobile.png')})
})

for(const [width,theme] of [[1440,'light'],[390,'dark']])test(`parent folder and single STRM actions have exact scopes ${width}`,async({page},info)=>{
 await fixture(page);const writes=[]
 await page.route('**/api/strm-scrape/match',route=>{writes.push(route.request().postDataJSON());return route.fulfill({json:{running:false}})})
 await page.route('**/api/strm-scrape/reset',route=>{writes.push(route.request().postDataJSON());return route.fulfill({json:{ok:true}})})
 await page.setViewportSize({width,height:900});await page.goto('/scrape/strm');await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
 await page.getByRole('button',{name:'海报墙视图',exact:true}).click()
 await page.getByRole('textbox',{name:'搜索刮削记录',exact:true}).fill('Folder1')
 await expect(page.getByRole('button',{name:'识别 Folder1',exact:true})).toBeVisible()
 await page.getByRole('button',{name:'识别 Folder1',exact:true}).click()
 await page.getByLabel('TMDB ID',{exact:true}).fill('7');await page.getByRole('button',{name:'确认匹配',exact:true}).click()
 await expect.poll(()=>writes.length).toBe(1)
 await expect(page.getByRole('dialog',{name:'选择媒体匹配',exact:true})).toHaveCount(0)
 expect(writes[0]).toMatchObject({taskId:'t',path:'',scope:'Folder1',group:false,scrape:true,tmdb:7})
 await page.getByRole('textbox',{name:'搜索刮削记录',exact:true}).fill('Show');await page.getByRole('button',{name:'Show',exact:true}).click()
 await page.getByRole('textbox',{name:'搜索刮削记录',exact:true}).clear()
 await expect(page.getByRole('button',{name:'识别 one.strm',exact:true})).toBeVisible()
 await expect(page.getByRole('button',{name:'识别 tvshow.nfo',exact:true})).toHaveCount(0)
 await page.getByRole('button',{name:'重置 one.strm',exact:true}).click();await page.getByRole('button',{name:'确认重置',exact:true}).click()
 await expect.poll(()=>writes.length).toBe(2);expect(writes[1]).toMatchObject({taskId:'t',path:'Show/one.strm',scope:'',group:false,confirmed:true})
 await page.getByRole('button',{name:'识别 one.strm',exact:true}).click();await page.getByLabel('TMDB ID',{exact:true}).fill('8');await page.getByRole('button',{name:'确认匹配',exact:true}).click()
 await expect.poll(()=>writes.length).toBe(3);expect(writes[2]).toMatchObject({path:'Show/one.strm',group:false,scrape:true,tmdb:8});expect(writes[2].scope).toBeUndefined()
 await expect(page.getByRole('dialog',{name:'选择媒体匹配',exact:true})).toHaveCount(0)
 await page.locator('.scrape-file-row').filter({has:page.getByRole('button',{name:'one.strm',exact:true})}).hover()
 await page.screenshot({path:info.outputPath(`single-strm-${width}.png`)})
})

test('local provider and WebDAV user cards adapt to both themes', async ({page},info) => {
  await fixture(page)
  await page.route('**/api/webdav/users',r=>r.fulfill({json:[{id:'u',username:'Reader',enabled:true,grants:[]}]}))
  await page.goto('/storage')
  const local = page.locator('.storage-card .provider-icon[data-provider=local]').first()
  await expect(local).toHaveCSS('color','rgb(23, 27, 43)')
  await page.evaluate(()=>document.documentElement.dataset.theme='dark')
  await expect(local).toHaveCSS('color','rgb(200, 213, 237)')
  await page.goto('/files/webdav')
  await expect(page.locator('.dav-user-card')).toBeVisible()
  await page.evaluate(()=>document.documentElement.dataset.theme='light')
  await expect(page.locator('.dav-user-card')).toHaveCSS('background-color','rgb(255, 255, 255)')
  await page.evaluate(()=>document.documentElement.dataset.theme='dark')
  await expect(page.locator('.dav-user-card')).toHaveCSS('background-color','rgb(41, 44, 52)')
  await page.screenshot({path:info.outputPath('dav-dark.png')})
})

test('series and season directories both expose work actions without a type column',async({page})=>{
 await fixture(page)
 await page.route('**/api/strm-scrape/items?**',r=>r.fulfill({json:[{path:'Show/Season 1/one.strm',directories:['Show/Season 1'],title:'Show',kind:'tv',status:'ok'}]}))
 await page.route('**/api/strm-scrape/files?**',r=>{
  const p=new URL(r.request().url()).searchParams.get('path')
  return r.fulfill({json:p==='.'?[{id:'Show',name:'Show',isDir:true}]:[{id:'Show/Season 1',name:'Season 1',isDir:true}]})
 })
 await page.goto('/tasks/scrape')
 await page.getByRole('button',{name:'海报墙视图',exact:true}).click()
 await page.locator('.scrape-file-row').hover()
 await expect(page.locator('.scrape-folder-actions')).toContainText('重置识别')
 await expect(page.locator('.scrape-file-row > small')).toHaveCount(0)
 await page.getByRole('button',{name:'Show',exact:true}).click()
 await page.locator('.scrape-file-row').hover()
 await expect(page.locator('.scrape-folder-actions')).toContainText('重置识别')
})
