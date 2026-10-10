import { test, expect } from '@playwright/test'

test('rename previews preserve scrolled rows and ignore stale responses and incomplete rules', async ({page},info) => {
  const files=Array.from({length:100},(_,i)=>({id:`/old-${i}.txt`,name:`old-${i}.txt`,isDir:false}))
  let requests=0,release
  const gate=new Promise(resolve=>release=resolve)
  await page.route('**/api/**',r=>{
    const path=new URL(r.request().url()).pathname
    const json=path==='/api/auth/status'?{initialized:true,authenticated:true}:path==='/api/state'?{username:'preview-stable',storages:[{id:'pool',name:'本机',type:'local',enabled:true,config:{}}],settings:{},tasks:[],cache:{},traffic:{}}:path==='/api/files'?files:path==='/api/files/rename-rules'?[]:{}
    return r.fulfill({json})
  })
  await page.route('**/api/files/rename-preview',async r=>{
    const input=r.request().postDataJSON()
    if(++requests===1)await gate
    return r.fulfill({json:files.map(f=>({...f,newName:input.rules.reduce((name,rule)=>name.replaceAll(rule.find,rule.replace),f.name)}))})
  })
  await page.goto('/files')
  await page.getByRole('button',{name:'工具',exact:true}).click()
  await page.getByRole('menuitem',{name:'重命名',exact:true}).click()
  const viewport=page.locator('.rename-preview-scroll .thin-scroll-area'),rows=page.locator('.rename-preview-row')
  await viewport.evaluate(el=>{el.scrollTop=600;el.dispatchEvent(new Event('scroll'))})
  await expect.poll(()=>viewport.evaluate(el=>el.scrollTop)).toBe(600)
  await rows.first().evaluate(el=>window.stableComparisonRow=el)
  await page.getByLabel('替换为',{exact:true}).fill('new')
  await page.getByLabel('查找内容',{exact:true}).fill('old')
  await expect.poll(()=>requests).toBe(1)
  await expect(rows.first()).toContainText('新：old-')
  await page.getByLabel('替换为',{exact:true}).fill('latest')
  await expect(rows.first()).toContainText('新：latest-')
  release()
  await page.waitForTimeout(400)
  await expect(rows.first()).toContainText('新：latest-')
  expect(await rows.first().evaluate(el=>el===window.stableComparisonRow)).toBe(true)
  expect(await viewport.evaluate(el=>el.scrollTop)).toBe(600)
  await page.getByRole('button',{name:'添加规则',exact:true}).click()
  await expect(page.getByLabel('替换为',{exact:true})).toHaveCount(1)
  await page.getByLabel('替换为',{exact:true}).fill('ignored')
  await page.waitForTimeout(400)
  expect(requests).toBe(2)
  expect(await viewport.evaluate(el=>el.scrollTop)).toBe(600)
  await page.screenshot({path:info.outputPath('stable-preview.png')})
})
