import { test, expect } from '@playwright/test'

test('management favorites are shared by independent browsers without overwriting other clients', async ({ browser, baseURL }) => {
  const contexts = [await browser.newContext({baseURL}), await browser.newContext({baseURL})]
  let records = [], fail = false
  const pool = {id:'pool',name:'云盘',type:'quark',enabled:true,config:{}}
  for (const context of contexts) await context.route('**/api/**', route => {
    const req=route.request(), url=new URL(req.url()), path=url.pathname
    if(path==='/api/auth/status')return route.fulfill({json:{initialized:true,authenticated:true}})
    if(path==='/api/state')return route.fulfill({json:{username:'shared-owner',storages:[pool],fileFavorites:records.filter(f=>!f.deleted),tasks:[],settings:{},logs:[],cache:{},traffic:{}}})
    if(path==='/api/files/favorites') {
      if(req.method()==='POST') {
        if(fail)return route.fulfill({status:500,json:{error:'收藏保存失败'}})
        const {action,items}=req.postDataJSON()
        for(const item of items) {
          const index=records.findIndex(f=>f.storage===item.storage&&f.id===item.id)
          if(action==='import'&&index>=0)continue
          const value=action==='remove'?{...item,deleted:true}:item
          if(index<0)records.push(value);else records[index]=value
        }
      }
      return route.fulfill({json:records.filter(f=>!f.deleted)})
    }
    if(path==='/api/files')return route.fulfill({json:url.searchParams.get('path')==='/'?[{id:'a',name:'目录A',isDir:true},{id:'b',name:'目录B',isDir:true}]:[]})
    return route.fulfill({json:{}})
  })
  try {
    const first=await contexts[0].newPage(),second=await contexts[1].newPage()
    await first.goto('/files')
    await first.getByRole('button',{name:'展开收藏栏',exact:true}).click()
    await first.locator('.file-view').getByRole('button',{name:'目录A',exact:true}).dblclick()
    await first.getByRole('button',{name:'收藏当前目录',exact:true}).click()
    await expect(first.getByRole('button',{name:'取消收藏目录',exact:true})).toBeEnabled()
    await second.goto('/files')
    await second.getByRole('button',{name:'展开收藏栏',exact:true}).click()
    await expect(second.locator('.file-favorites').getByRole('button',{name:'目录A',exact:true})).toBeVisible()
    expect(await second.evaluate(()=>JSON.parse(localStorage.getItem('aether-files:shared-owner')||'{}').favorites)).toBeUndefined()
    await second.locator('.file-view').getByRole('button',{name:'目录B',exact:true}).dblclick()
    await second.getByRole('button',{name:'收藏当前目录',exact:true}).click()
    await expect(second.locator('.file-favorites').getByRole('button',{name:'目录B',exact:true})).toBeVisible()
    // The first client still has an older view; per-entry removal must preserve B.
    await first.getByRole('button',{name:'取消收藏目录',exact:true}).click()
    await expect(first.locator('.file-favorites').getByRole('button',{name:'目录B',exact:true})).toBeVisible()
    await expect(first.locator('.file-favorites').getByRole('button',{name:'目录A',exact:true})).toHaveCount(0)
    await expect(second.locator('.file-favorites').getByRole('button',{name:'目录A',exact:true})).toHaveCount(0,{timeout:7000})
    await second.reload()
    await expect(second.locator('.file-favorites').getByRole('button',{name:'目录A',exact:true})).toHaveCount(0)
    await expect(second.locator('.file-favorites').getByRole('button',{name:'目录B',exact:true})).toBeVisible()
    fail=true
    await second.getByRole('button',{name:'取消收藏目录',exact:true}).click()
    await expect(second.getByRole('alert')).toContainText('收藏保存失败')
    await expect(second.locator('.file-favorites').getByRole('button',{name:'目录B',exact:true})).toBeVisible()
    fail=false
    await second.getByRole('button',{name:'取消收藏目录',exact:true}).click()
    await expect(second.locator('.file-favorites').getByRole('button',{name:'目录B',exact:true})).toHaveCount(0)
  } finally { for(const context of contexts)await context.close() }
})

test('failed legacy migration retains browser favorites for retry',async({page})=>{
  let fail=true,records=[]
  await page.addInitScript(()=>{
    if(!localStorage.getItem('aether-files:legacy-owner'))localStorage.setItem('aether-files:legacy-owner',JSON.stringify({open:true,favorites:[{storage:'pool',id:'a',name:'旧收藏',history:[{id:'/',name:'旧收藏'}]}]}))
  })
  await page.route('**/api/**',route=>{
    const req=route.request(),path=new URL(req.url()).pathname
    if(path==='/api/auth/status')return route.fulfill({json:{initialized:true,authenticated:true}})
    if(path==='/api/state')return route.fulfill({json:{username:'legacy-owner',storages:[{id:'pool',type:'quark',name:'云盘',enabled:true,config:{}}],fileFavorites:records,tasks:[],settings:{},logs:[],cache:{}}})
    if(path==='/api/files/favorites') {
      if(fail)return route.fulfill({status:500,json:{error:'迁移暂时失败'}})
      if(req.method()==='POST')records=req.postDataJSON().items
      return route.fulfill({json:records})
    }
    return route.fulfill({json:path==='/api/files'?[]:{}})
  })
  await page.goto('/files')
  await expect(page.getByRole('alert')).toContainText('迁移暂时失败')
  expect(await page.evaluate(()=>JSON.parse(localStorage.getItem('aether-files:legacy-owner')).favorites.length)).toBe(1)
  fail=false
  await page.reload()
  await expect(page.locator('.file-favorites').getByRole('button',{name:'旧收藏',exact:true})).toBeVisible()
  expect(await page.evaluate(()=>JSON.parse(localStorage.getItem('aether-files:legacy-owner')).favorites)).toBeUndefined()
})
