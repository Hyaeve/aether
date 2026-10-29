import { test, expect } from '@playwright/test'

async function setupWorkspace(page, storages = [], { failurePath, failureStatus } = {}) {
  const unexpected = [], requests = []
  const replies = {
    '/api/auth/status': {initialized:true,authenticated:true},
    '/api/state': {username:'quark-workspace',storages,tasks:[],settings:{},cache:{},traffic:{}},
    '/api/traffic': {upload:0,download:0},
    '/api/strm-replace': {tasks:[]},
    '/api/115-folder-cleanup': {enabled:false,rules:[]},
    '/api/115-signin': {enabled:false,rules:[]},
    '/api/115-recycle-cleanup': {enabled:false,rules:[]},
    '/api/115-simulcast/enabled': {enabled:true},
    '/api/quark-takeover': {bindings:[],broker:'https://broker.example'},
    ...Object.fromEntries(['emby','tmdb','ai','proxy'].map(kind=>[`/api/plugins/${kind}`,{enabled:false}]))
  }
  await page.route('**/api/**', route => {
    const request = route.request(), path = new URL(request.url()).pathname
    requests.push(`${request.method()} ${path}`)
    if (path === failurePath) return route.fulfill({status:failureStatus,json:{error:'配置读取失败'}})
    if (request.method() === 'GET' && Object.hasOwn(replies,path)) return route.fulfill({json:replies[path]})
    unexpected.push(`${request.method()} ${path}`)
    return route.fulfill({status:500,json:{error:'测试未定义接口'}})
  })
  return {requests,unexpected}
}

function configurationResponse(page, path) {
  return page.waitForResponse(response => new URL(response.url()).pathname === path && response.request().method() === 'GET')
}

test('Quark workspace has a binding sidebar, per-account settings and confirmed unbind', async ({ page }, info) => {
  const storages = ['one','two'].map((id,i) => ({id, name:`夸克 ${i+1}`, type:'quark',enabled:true}))
  const fixture = await setupWorkspace(page,storages)
  let bindings = storages.map(s => ({...s, valid:true,nickname:`账号 ${s.id}`}))
  const configs = Object.fromEntries(storages.map(s => [s.id,{enabled:true, quality:'4k',allowDolby:false,mode:'adaptive',uaListMode:'proxy_list',ua:''}]))
  await page.route('**/api/quark-takeover', r => r.fulfill({json:{enabled:true,bindings}}))
  await page.route('**/api/quark-takeover/*', r => {
    const id = new URL(r.request().url()).pathname.split('/').at(-1)
    if(r.request().method()==='PUT') configs[id]=r.request().postDataJSON()
    if(r.request().method()==='DELETE') bindings=bindings.filter(b=>b.id!==id)
    return r.fulfill({json:{config:configs[id]}})
  })
  const configurations = Promise.all(['/api/115-folder-cleanup','/api/115-signin'].map(path=>configurationResponse(page,path)))
  await page.goto('/tools')
  expect((await configurations).map(response=>response.status())).toEqual([200,200])
  await page.getByRole('button',{name:'夸克 STRM 接管',exact:true}).getByText('夸克 STRM 接管',{exact:true}).click()
  const dialog = page.getByRole('dialog',{name:'夸克 STRM 接管',exact:true})
  await expect(dialog.locator('.takeover-account h3')).toHaveText('夸克 1')
  await expect(dialog.getByRole('button',{name:'保存设置',exact:true})).toBeDisabled()
  const left = await dialog.locator('.binding-workspace').boundingBox(), right = await dialog.locator('.takeover-settings').boundingBox()
  expect(left.x+left.width).toBeLessThanOrEqual(right.x+1)
  await dialog.getByRole('button',{name:'接管模式',exact:true}).click()
  await page.getByRole('option',{name:'策略分流',exact:true}).click()
  await dialog.getByRole('button',{name:'直连名单',exact:true}).click()
  await dialog.getByRole('textbox',{name:'UA 关键词',exact:true}).fill('Hills;Infuse')
  await dialog.getByRole('switch',{name:'允许杜比视界',exact:true}).check()
  await dialog.getByRole('button',{name:'保存设置',exact:true}).click()
  await expect.poll(()=>configs.one.ua).toBe('Hills\nInfuse')
  expect(configs.one.uaListMode).toBe('direct_list')
  expect(configs.one.allowDolby).toBe(true)
  for(const theme of ['light','dark']) {
    await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
    await page.screenshot({path:info.outputPath(`quark-workspace-${theme}.png`)})
  }
  await dialog.getByRole('button',{name:'设置绑定 夸克 2',exact:true}).click()
  await expect(dialog.locator('.takeover-account h3')).toHaveText('夸克 2')
  await expect(dialog.getByRole('button',{name:'接管模式',exact:true})).toHaveText('智能变轨')
  await dialog.getByRole('button',{name:'解除绑定 夸克 2',exact:true}).click()
  await page.getByRole('dialog',{name:'解绑夸克 TV',exact:true}).getByRole('button',{name:'取消',exact:true}).click()
  expect(bindings.length).toBe(2)
  await dialog.getByRole('button',{name:'解除绑定 夸克 2',exact:true}).click()
  await page.getByRole('button',{name:'确认解绑',exact:true}).click()
  await expect(dialog.locator('.binding-row')).toHaveCount(1)
  await page.setViewportSize({width:390,height:844})
  expect(await dialog.evaluate(el=>el.scrollWidth<=el.clientWidth)).toBe(true)
  await page.screenshot({path:info.outputPath('quark-workspace-mobile.png')})
  expect(fixture.unexpected).toEqual([])
})

test('empty Quark workspace keeps settings visible and explains missing storage',async({page})=>{
  const fixture = await setupWorkspace(page)
  await page.goto('/tools')
  await page.getByRole('button',{name:'夸克 STRM 接管',exact:true}).getByText('夸克 STRM 接管',{exact:true}).click()
  const dialog=page.getByRole('dialog',{name:'夸克 STRM 接管',exact:true})
  await expect(dialog.getByRole('button',{name:'最高画质',exact:true})).toBeVisible()
  await expect(dialog.getByRole('button',{name:'接管模式',exact:true})).toBeVisible()
  await dialog.getByRole('button',{name:'添加绑定',exact:true}).click()
  await expect(page.getByText('请先添加并启用夸克存储池',{exact:true})).toBeVisible()
  await expect(page.getByRole('dialog',{name:'选择绑定的存储'})).toHaveCount(0)
  expect(fixture.unexpected).toEqual([])
})

for (const path of ['/api/115-folder-cleanup','/api/115-signin']) {
  test(`tool configuration ${path} authorization failures still require login`,async({page})=>{
    const fixture = await setupWorkspace(page,[],{failurePath:path,failureStatus:401})
    const response = configurationResponse(page,path)
    await page.goto('/tools')
    expect((await response).status()).toBe(401)
    await expect(page.getByRole('button',{name:'登录工作空间',exact:true})).toBeVisible()
    await expect(page.getByRole('button',{name:'夸克 STRM 接管',exact:true})).toHaveCount(0)
    expect(fixture.unexpected).toEqual([])
  })
}

test('non-authorization tool configuration failures keep the Quark workspace usable',async({page})=>{
  const fixture = await setupWorkspace(page,[],{failurePath:'/api/115-folder-cleanup',failureStatus:500})
  const response = configurationResponse(page,'/api/115-folder-cleanup')
  await page.goto('/tools')
  expect((await response).status()).toBe(500)
  await page.getByRole('button',{name:'夸克 STRM 接管',exact:true}).getByText('夸克 STRM 接管',{exact:true}).click()
  await expect(page.getByRole('dialog',{name:'夸克 STRM 接管',exact:true})).toBeVisible()
  await expect(page.getByRole('button',{name:'登录工作空间',exact:true})).toHaveCount(0)
  expect(fixture.unexpected).toEqual([])
})
