import { test, expect } from '@playwright/test'

async function setup(page) {
  const root=[{id:'a',name:'资料',isDir:true},{id:'root',name:'match-root.txt',size:5},{id:'other',name:'other.txt'}]
  const listings={'/':root,a:[{id:'b',name:'match-folder',isDir:true},{id:'file',name:'match-child.txt',size:6}],b:[]}
  const deep=[root[1],{id:'file',name:'match-child.txt',parent:'a',trail:[{id:'/',name:'资料'}]},{id:'b',name:'match-folder',isDir:true,parent:'a',trail:[{id:'/',name:'资料'}]}]
  deep[0]={...deep[0],parent:'/',trail:[]}
  const calls={search:[],actions:[]}
  await page.route('**/api/**',r=>{
    const url=new URL(r.request().url())
    if(url.pathname==='/api/auth/status')return r.fulfill({json:{initialized:true,authenticated:true}})
    if(url.pathname==='/api/state')return r.fulfill({json:{username:'file-search',storages:[{id:'pool',type:'115',name:'资料库',enabled:true,config:{}}],tasks:[],settings:{},cache:{}}})
    if(url.pathname==='/api/files')return r.fulfill({json:listings[url.searchParams.get('path')]||[]})
    if(url.pathname==='/api/files/search'){calls.search.push(url);return r.fulfill({json:deep})}
    if(url.pathname==='/api/files/action'){calls.actions.push(r.request().postDataJSON());return r.fulfill({json:{processed:1}})}
    return r.fulfill({json:{}})
  })
  await page.goto('/files')
  return {calls,deep}
}

test('typing filters only the current directory immediately and Enter searches descendants',async({page},info)=>{
  const {calls}=await setup(page)
  const input=page.getByRole('textbox',{name:'搜索当前目录',exact:true})
  await input.fill('match')
  await expect(page.locator('.file-row')).toHaveCount(1)
  await expect(page.getByRole('button',{name:'match-root.txt',exact:true})).toBeVisible()
  expect(calls.search).toHaveLength(0)
  await input.press('Enter')
  await expect(page.getByRole('status').filter({hasText:'深度搜索 · 3 个结果'})).toBeVisible()
  await expect(page.locator('.file-row')).toHaveCount(3)
  expect(calls.search).toHaveLength(1)
  expect(calls.search[0].searchParams.get('path')).toBe('/')
  await page.screenshot({path:info.outputPath('deep-search-list.png')})
  await page.getByRole('button',{name:'当前列表视图，切换网格',exact:true}).click()
  await expect(page.locator('.file-grid-item')).toHaveCount(3)
  await page.setViewportSize({width:390,height:900})
  await expect(page.locator('.file-result-path').filter({hasText:'资料'}).first()).toBeVisible()
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1)).toBe(true)
  await page.screenshot({path:info.outputPath('deep-search-grid-mobile.png')})
  await input.fill('other')
  await expect(page.getByRole('button',{name:'other.txt',exact:true})).toBeVisible()
  await expect(page.locator('.file-search-status')).toHaveCount(0)
  expect(calls.search).toHaveLength(1)
  await input.fill('')
  await expect(page.locator('.file-grid-item')).toHaveCount(3)
})

test('deep directory result restores opaque breadcrumbs; file operations use its actual parent',async({page})=>{
  const {calls}=await setup(page)
  const input=page.getByRole('textbox',{name:'搜索当前目录',exact:true})
  await input.fill('match');await input.press('Enter')
  await page.getByRole('button',{name:'match-folder',exact:true}).dblclick()
  await expect(page.locator('.path-bar')).toContainText('资料')
  await expect(page.locator('.path-bar')).toContainText('match-folder')
  await expect(input).toHaveValue('')
  await page.getByRole('button',{name:'根目录',exact:true}).click()
  await input.fill('match');await input.press('Enter')
  await page.getByRole('button',{name:'match-child.txt',exact:true}).click({button:'right'})
  await expect(page.locator('.context-menu')).toBeVisible()
  await expect(page.locator('.path-bar')).toContainText('资料')
  await page.getByRole('button',{name:'重命名',exact:true}).click()
  await page.getByRole('textbox',{name:'新名称',exact:true}).fill('renamed.txt')
  await page.getByRole('textbox',{name:'新名称',exact:true}).press('Enter')
  await expect.poll(()=>calls.actions.length).toBe(1)
  expect(calls.actions[0]).toMatchObject({source:'a',ids:['file'],action:'rename',name:'renamed.txt'})
})

test('editing query and leaving the page discard delayed recursive results',async({page})=>{
  await setup(page)
  let release,started=false
  await page.route('**/api/files/search?**',async r=>{started=true;await new Promise(resolve=>release=resolve);await r.fulfill({json:[{id:'stale',name:'match-stale.txt',parent:'/',trail:[]}]}).catch(()=>{})})
  const input=page.getByRole('textbox',{name:'搜索当前目录',exact:true})
  await input.fill('match');await input.press('Enter')
  await expect.poll(()=>started).toBe(true)
  await input.fill('other');release()
  await expect(page.getByRole('button',{name:'other.txt',exact:true})).toBeVisible()
  await expect(page.getByRole('button',{name:'match-stale.txt',exact:true})).toHaveCount(0)
  await expect(page.locator('.file-search-status')).toHaveCount(0)
  started=false
  await input.fill('match');await input.press('Enter')
  await expect.poll(()=>started).toBe(true)
  await page.getByRole('link',{name:'仪表盘',exact:true}).click();release()
  await expect(page).toHaveURL(/\/dashboard$/)
  await page.getByRole('link',{name:'文件服务',exact:true}).click()
  await expect(input).toHaveValue('')
  await expect(page.getByRole('button',{name:'match-stale.txt',exact:true})).toHaveCount(0)
})

test('deep search errors preserve the local filter and refresh exits deep mode',async({page})=>{
  await setup(page)
  await page.route('**/api/files/search?**',r=>r.fulfill({status:400,json:{error:'目录读取失败'}}))
  const input=page.getByRole('textbox',{name:'搜索当前目录',exact:true})
  await input.fill('match');await input.press('Enter')
  await expect(page.getByRole('alert')).toHaveText('目录读取失败')
  await expect(page.locator('.file-row')).toHaveCount(1)
  await page.getByRole('button',{name:'刷新目录',exact:true}).click()
  await expect(page.getByRole('alert')).toHaveCount(0)
  await expect(input).toHaveValue('match')
})
