import {test,expect} from '@playwright/test'
import {captureScreenshot} from './helpers/screenshot.js'

async function fixture(page){
  const previews=[],saves=[],metadata=[]
  let pending
  await page.route('**/api/**',async route=>{
    const req=route.request(),u=new URL(req.url());let json={}
    if(u.pathname==='/api/auth/status')json={initialized:true,authenticated:true}
    if(u.pathname==='/api/state')json={username:'save-directory',storages:[{id:'pan',name:'115影视',type:'115',enabled:true,config:{}}],tasks:[],settings:{},cache:{},traffic:{}}
    if(u.pathname==='/api/files/share-preferences')json={last:{},directories:{}}
    if(u.pathname==='/api/files')json=[]
    if(u.pathname==='/api/files/share/info'){
      const body=req.postDataJSON();metadata.push(body)
      if(body.url.includes('slow'))await new Promise(resolve=>pending=resolve)
      json={directory:body.url.includes('file')?'':'Original'}
    }
    if(u.pathname==='/api/files/share/preview'){const body=req.postDataJSON();previews.push(body);json={preview:'preview',batches:1,items:[{name:'Original',isDir:true}]}}
    if(u.pathname==='/api/files/share/batch'){saves.push(req.postDataJSON());json={status:'completed',destination:'renamed-id',directory:'Custom'}}
    return route.fulfill({json})
  })
  await page.goto('/files?storage=pan')
  await page.getByRole('button',{name:'工具',exact:true}).click()
  await page.getByRole('menuitem',{name:'分享转存',exact:true}).click()
  return {previews,saves,metadata,release:()=>pending?.(),modal:page.getByRole('dialog',{name:'分享转存',exact:true})}
}

for(const width of [1440,390])for(const theme of ['light','dark'])test(`single share save directory defaults, renames and navigates ${width} ${theme}`,async({page},info)=>{
  await page.setViewportSize({width,height:900})
  const f=await fixture(page),m=f.modal,input=m.getByRole('textbox',{name:'保存目录',exact:true})
  await page.evaluate(theme=>document.documentElement.dataset.theme=theme,theme)
  await expect(input).toHaveValue('');await expect(input).toBeDisabled()
  await m.getByRole('textbox',{name:'分享链接',exact:true}).fill('https://115.com/s/folder')
  await expect(input).toHaveValue('Original')
  const boxes=await m.locator('.share-directory-row').evaluate(el=>[el.querySelector('.source-trigger'),el.querySelector('input')].map(e=>{const r=e.getBoundingClientRect();return {y:r.y,h:r.height}}))
  expect(boxes[0].h).toBe(40);expect(boxes[1].h).toBe(40);expect(Math.abs(boxes[0].y-boxes[1].y)).toBeLessThan(1)
  await input.fill('Custom')
  await captureScreenshot(page,{path:info.outputPath(`share-save-${width}-${theme}.png`)})
  expect(await m.evaluate(el=>el.scrollWidth<=el.clientWidth)).toBe(true)
  await m.getByRole('button',{name:'转存',exact:true}).click()
  await expect.poll(()=>f.saves.length).toBe(1)
  expect(f.previews[0]).toEqual({storageId:'pan',url:'https://115.com/s/folder',excluded:[],saveDirectory:'Custom'})
  await expect(page.locator('.path-bar [aria-current=location]')).toHaveText('Custom')
  await expect(page.locator('.toast-stack')).toContainText('Original')
})

test('file shares stay flat, multi links clear and disable the custom name',async({page})=>{
  const f=await fixture(page),m=f.modal,links=m.getByRole('textbox',{name:'分享链接',exact:true}),input=m.getByRole('textbox',{name:'保存目录',exact:true})
  await links.fill('https://115.com/s/file')
  await expect.poll(()=>f.metadata.length).toBe(1)
  await expect(input).toBeEnabled();await expect(input).toHaveValue('')
  await input.fill('Custom')
  await links.fill('https://115.com/s/file\nhttps://115.com/s/folder')
  await expect(input).toBeDisabled();await expect(input).toHaveValue('')
  await m.getByRole('button',{name:'转存',exact:true}).click()
  await expect.poll(()=>f.previews.length).toBe(2)
  expect(f.previews.every(body=>!Object.hasOwn(body,'saveDirectory'))).toBe(true)
})

test('late metadata does not overwrite edits or restore a disabled multi-link name',async({page})=>{
  const f=await fixture(page),m=f.modal,links=m.getByRole('textbox',{name:'分享链接',exact:true}),input=m.getByRole('textbox',{name:'保存目录',exact:true})
  await links.fill('https://115.com/s/slow')
  await expect.poll(()=>f.metadata.length).toBe(1)
  await input.fill('Mine');f.release()
  await expect(m.locator('.share-directory-row [aria-busy]')).toHaveAttribute('aria-busy','false')
  await expect(input).toHaveValue('Mine')
  await input.fill('../bad');await m.getByRole('button',{name:'转存',exact:true}).click()
  await expect(page.locator('.toast-stack')).toContainText('保存目录名称无效')
  expect(f.saves).toHaveLength(0)
  await links.fill('https://115.com/s/slow2')
  await expect.poll(()=>f.metadata.length).toBe(2)
  await links.fill('https://115.com/s/first\nhttps://115.com/s/second');f.release()
  await expect(input).toBeDisabled();await expect(input).toHaveValue('')
})

test('submitting before the default arrives waits without writing and uses that default',async({page})=>{
  const f=await fixture(page),m=f.modal
  await m.getByRole('textbox',{name:'分享链接',exact:true}).fill('https://115.com/s/slow')
  await expect.poll(()=>f.metadata.length).toBe(1)
  await m.getByRole('button',{name:'转存',exact:true}).click()
  await expect(m.getByRole('button',{name:'转存',exact:true})).toBeDisabled()
  expect(f.previews).toHaveLength(0);expect(f.saves).toHaveLength(0)
  f.release()
  await expect.poll(()=>f.saves.length).toBe(1)
  expect(f.previews[0].saveDirectory).toBe('Original')
})

test('explicitly clearing a folder name submits an empty directory rather than restoring its default',async({page})=>{
  const f=await fixture(page),m=f.modal,input=m.getByRole('textbox',{name:'保存目录',exact:true})
  await m.getByRole('textbox',{name:'分享链接',exact:true}).fill('https://115.com/s/folder')
  await expect(input).toHaveValue('Original')
  expect(f.previews).toHaveLength(0);expect(f.saves).toHaveLength(0)
  await input.fill('')
  await m.getByRole('button',{name:'转存',exact:true}).click()
  await expect.poll(()=>f.saves.length).toBe(1)
  expect(f.previews[0].saveDirectory).toBe('')
})

test('a failed default read shows a bubble and never silently submits a flat destination',async({page})=>{
  const f=await fixture(page),m=f.modal
  await page.route('**/api/files/share/info',route=>route.fulfill({status:400,json:{error:'分享已失效'}}))
  await m.getByRole('textbox',{name:'分享链接',exact:true}).fill('https://115.com/s/folder')
  await m.getByRole('button',{name:'转存',exact:true}).click()
  await expect(page.locator('.toast-stack')).toContainText('分享已失效')
  await expect(m).toBeVisible();expect(f.previews).toHaveLength(0);expect(f.saves).toHaveLength(0)
})
