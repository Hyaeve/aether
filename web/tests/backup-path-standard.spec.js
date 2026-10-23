import { test, expect } from '@playwright/test'

const parts = ['家庭影音资料','完整的中间目录','最深目录']
async function fixture(page, legacy=false) {
  let rule = {id:'backup',name:'路径备份',enabled:true,status:'idle',sources:[{storageId:'pan',path:'deep',label:'/家庭影音资料/完整的中间目录/最深目录'}],targets:[{storageId:'disk',path:'/',label:'根目录'}],filters:[]}
  const storages=[{id:'pan',type:'115',name:'媒体',enabled:true,config:{}},{id:'disk',type:'local',name:'本地',enabled:true,config:{}}]
  const ids=['/','library','middle','deep']
  if(legacy)rule.sources[0].label=parts[2]
  await page.route('**/api/**',async route=>{
    const r=route.request(),url=new URL(r.url());let json={}
    if(url.pathname==='/api/auth/status')json={initialized:true,authenticated:true}
    else if(url.pathname==='/api/state')json={username:'paths',storages,tasks:[],settings:{},cache:{},traffic:{},logs:[]}
    else if(url.pathname==='/api/backup-rules')json=[rule]
    else if(url.pathname==='/api/backup-rules/backup'){rule={...r.postDataJSON(),id:'backup'};json={ok:true}}
    else if(url.pathname==='/api/files/directory-trail')json={path:'/'+parts.join('/'),label:parts[2],trail:ids.slice(0,3).map((id,i)=>({id,name:i?parts[i-1]:'根目录'}))}
    else if(url.pathname==='/api/files'){const i=ids.indexOf(url.searchParams.get('path'));json=i>=0&&i<3?[{id:ids[i+1],name:parts[i],isDir:true}]:[]}
    else if(url.pathname==='/api/transfers')json=[]
    await route.fulfill({json})
  })
  return ()=>rule
}

for(const width of [1440,390])for(const theme of ['light','dark'])test(`backup switch and full clickable path ${width} ${theme}`,async({page},info)=>{
  await page.setViewportSize({width,height:900});const saved=await fixture(page)
  await page.goto('/transfer/backup');await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
  await page.locator('.backup-name').click()
  let modal=page.getByRole('dialog',{name:'编辑备份规则',exact:true})
  expect(await modal.evaluate(el=>[...el.querySelectorAll('.backup-fields input,.backup-fields .source-trigger')].map(field=>field.getBoundingClientRect()).filter(a=>a.width>0).every(a=>{const b=el.getBoundingClientRect();return a.left>=b.left&&a.right<=b.right}))).toBe(true)
  const toggle=modal.getByRole('switch',{name:'文件系统监听',exact:true})
  for(const checked of [false,true]){
    await toggle.setChecked(checked)
    if(checked)await expect(toggle).toBeChecked();else await expect(toggle).not.toBeChecked()
    await expect.poll(()=>toggle.evaluate(el=>new DOMMatrix(getComputedStyle(el,'::before').transform).m41)).toBe(checked?15:0)
    const box=await toggle.evaluate(el=>{const s=getComputedStyle(el),p=getComputedStyle(el,'::before'),m=new DOMMatrix(p.transform);return {width:el.offsetWidth,height:el.offsetHeight,padding:parseFloat(s.paddingLeft),size:parseFloat(p.width),y:parseFloat(s.paddingTop),x:m.m41}})
    expect(box.height).toBe(20);expect(box.width).toBe(35)
    expect(box.y+box.size).toBeLessThanOrEqual(box.height)
    expect(box.padding+box.x+box.size).toBeLessThanOrEqual(box.width-box.padding)
  }
  await page.screenshot({path:info.outputPath('backup-switch.png')})
  await modal.getByRole('button',{name:'选择备份源目录',exact:true}).click()
  let picker=page.getByRole('dialog',{name:'选择存储目录',exact:true}),crumbs=picker.getByRole('navigation',{name:'存储目录路径'})
  await expect(crumbs.getByRole('button',{name:parts[2],exact:true})).toBeVisible()
  expect(await crumbs.getByRole('button',{name:parts[2],exact:true}).evaluate(el=>el.scrollWidth<=el.clientWidth+1)).toBe(true)
  if(width===1440)for(const part of parts)await expect(crumbs.getByRole('button',{name:part,exact:true})).toBeVisible()
  else {await crumbs.getByRole('button',{name:'展开省略路径',exact:true}).click();await expect(crumbs.getByRole('button',{name:parts[0],exact:true})).toBeVisible()}
  await crumbs.getByRole('button',{name:parts[0],exact:true}).click()
  await expect(picker.locator('.source-directory')).toContainText(parts[1])
  await picker.getByRole('button',{name:parts[1],exact:true}).click();await picker.getByRole('button',{name:parts[2],exact:true}).click()
  await picker.getByRole('button',{name:'选择当前目录',exact:true}).click()
  await modal.getByRole('button',{name:'保存规则',exact:true}).click()
  expect(saved().sources[0].label).toBe('/'+parts.join('/'))
  await page.locator('.backup-name').click();modal=page.getByRole('dialog',{name:'编辑备份规则',exact:true})
  await modal.getByRole('button',{name:'选择备份源目录',exact:true}).click()
  picker=page.getByRole('dialog',{name:'选择存储目录',exact:true});crumbs=picker.getByRole('navigation',{name:'存储目录路径'})
  await expect(crumbs.getByRole('button',{name:parts[2],exact:true})).toBeVisible()
  expect(await crumbs.evaluate(el=>el.scrollHeight<=el.clientHeight+1)).toBe(true)
  expect(await picker.evaluate(el=>el.scrollWidth<=el.clientWidth+1)).toBe(true)
  await page.screenshot({path:info.outputPath('backup-path.png')})
})

test('legacy 115 ID restores every ancestor without scanning from root',async({page})=>{
  await fixture(page,true);await page.goto('/transfer/backup');await page.locator('.backup-name').click()
  await page.getByRole('button',{name:'选择备份源目录',exact:true}).click()
  const crumbs=page.getByRole('navigation',{name:'存储目录路径'})
  for(const part of parts)await expect(crumbs.getByRole('button',{name:part,exact:true})).toBeVisible()
  await crumbs.getByRole('button',{name:parts[1],exact:true}).click()
  await expect(page.locator('.source-directory')).toHaveText(parts[2]+'-')
})

test('an old opaque directory without an ancestry protocol cannot be saved as a fake full path',async({page})=>{
  await fixture(page,true)
  await page.route('**/api/state',route=>route.fulfill({json:{username:'paths',storages:[{id:'pan',type:'quark',name:'夸克',enabled:true,config:{}},{id:'disk',type:'local',name:'本地',enabled:true,config:{}}],tasks:[],settings:{},cache:{},logs:[]}}))
  await page.goto('/transfer/backup');await page.locator('.backup-name').click();await page.getByRole('button',{name:'选择备份源目录',exact:true}).click()
  const picker=page.getByRole('dialog',{name:'选择存储目录',exact:true})
  await expect(page.locator('.toast').filter({hasText:'旧目录记录不包含完整路径'})).toBeVisible()
  await expect(picker.getByRole('button',{name:'选择当前目录',exact:true})).toBeDisabled()
  await picker.getByRole('navigation',{name:'存储目录路径'}).getByRole('button',{name:'根目录',exact:true}).click()
  await expect(picker.getByRole('button',{name:parts[0],exact:true})).toBeVisible()
  await expect(picker.getByRole('button',{name:'选择当前目录',exact:true})).toBeEnabled()
})
