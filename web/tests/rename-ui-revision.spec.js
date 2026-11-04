import { test, expect } from '@playwright/test'

async function fixture(page, initial = '/files', type = 'local', name = 'Original.mkv') {
  const unexpected=[], previews=[], writes=[]
  const files=[{id:'/Original.mkv',name,isDir:false},{id:'/Second.mkv',name:'Second.mkv',isDir:false}]
  await page.route('**/api/**',async route=>{
    const req=route.request(),p=new URL(req.url()).pathname
    let json
    if(p==='/api/auth/status')json={initialized:true,authenticated:true}
    else if(p==='/api/state')json={username:'rename-ui',storages:[{id:'p',type,name:'影视资料',enabled:true,config:{}}],tasks:[],settings:{},traffic:{},cache:{},logs:[]}
    else if(p==='/api/files')json=files
    else if(p==='/api/files/rename-rules'||p==='/api/files/visits'||p==='/api/strm-replace'||p==='/api/logs')json=[]
    else if(p==='/api/files/rename-preview'){
      const input=req.postDataJSON();previews.push(input)
      json=files.filter(f=>input.ids.includes(f.id)).map((f,i)=>({...f,newName:input.rules[0]?.kind==='number'?`${String(input.rules[0].start+i).padStart(input.rules[0].digits,'0')}${f.name}`:f.name.replace('Original','Changed')}))
    }else if(p==='/api/files/rename-history')json=[{id:'a',storageId:'p',source:'/',pathLabel:'/',items:[]},{id:'b',storageId:'p',source:'/Another',pathLabel:'/Another',items:[]}]
    else if(p==='/api/files/rename'){writes.push(req.postDataJSON());json={processed:1}}
    else if(['/api/traffic','/api/notifications','/api/transfers'].includes(p))json={}
    else if(['/api/115-folder-cleanup','/api/115-signin','/api/115-recycle-cleanup','/api/115-simulcast/enabled','/api/quark-takeover'].includes(p)||p.startsWith('/api/plugins/'))json={enabled:false,rules:[],bindings:[]}
    else {unexpected.push(`${req.method()} ${p}`);return route.fulfill({status:501,json:{error:'Unexpected test API'}})}
    await route.fulfill({json})
  })
  await page.goto(initial)
  if(initial!=='/files')return {unexpected,previews,writes}
  await page.getByRole('button',{name:'工具',exact:true}).click()
  await page.getByRole('menuitem',{name:'命名工作台',exact:true}).click()
  await expect(page.locator('.rename-preview-row')).toHaveCount(2)
  return {unexpected,previews,writes}
}
async function select(page,label,option){
  await page.getByRole('button',{name:label,exact:true}).click()
  await page.getByRole('option',{name:option,exact:true}).click()
}

test('rule headers keep selectors inline and numbering supports zero and no separator',async({page},info)=>{
  const f=await fixture(page)
  const rule=page.locator('.rename-rule').first()
  await expect(rule.locator('header').getByRole('button',{name:'规则类型',exact:true})).toBeVisible()
  await expect(page.getByRole('button',{name:'删除规则',exact:true})).toHaveCount(0)
  await select(page,'规则类型','修改名称/添加序号')
  await expect(page.getByRole('button',{name:'分隔符',exact:true})).toHaveText('无')
  await page.getByRole('button',{name:'分隔符',exact:true}).click()
  await expect(page.getByRole('option')).toHaveText(['无','破折号（-）','句号（.）','空格','下划线（_）','顿号（、）'])
  await page.getByRole('option',{name:'无',exact:true}).click()
  await page.getByLabel('开始序号',{exact:true}).fill('0')
  await page.getByLabel('序号最少位数',{exact:true}).fill('0')
  await expect(page.getByLabel('序号最少位数',{exact:true})).toHaveValue('1')
  await page.getByLabel('序号最少位数',{exact:true}).fill('0')
  await expect(page.getByLabel('序号最少位数',{exact:true})).toHaveValue('1')
  await expect.poll(()=>f.previews.at(-1)?.rules[0]).toMatchObject({start:0,digits:1,separator:''})
  await expect(page.locator('.rename-changed').first()).toContainText('0Original.mkv')
  await page.getByRole('button',{name:'添加规则',exact:true}).click()
  await page.locator('.rename-heading h2').click()
  await expect(page.getByRole('group',{name:'可选规则'})).toHaveCount(0)
  await page.getByRole('button',{name:'添加规则',exact:true}).click()
  await page.getByRole('group',{name:'可选规则'}).getByRole('button',{name:'添加字符',exact:true}).click()
  await expect(page.getByRole('button',{name:'删除规则',exact:true})).toHaveCount(2)
  const sizes=await page.locator('.rename-rules').evaluate(el=>[...el.querySelectorAll('.rename-rule header .rounded-select-trigger,.rename-field>input,.rename-field .number-control,.rename-field .rounded-select-trigger')].map(e=>e.getBoundingClientRect().height))
  expect(new Set(sizes).size,JSON.stringify(sizes)).toBe(1);expect(sizes[0]).toBe(32)
  await page.screenshot({path:info.outputPath('rename-headers.png')})
  expect(f.unexpected).toEqual([])
})

for(const [width,height,theme] of [[1440,1000,'light'],[390,844,'dark'],[375,812,'light'],[844,390,'dark']]){
  test(`compact rule headers and original-name reference ${width} ${theme}`,async({page},info)=>{
    await page.setViewportSize({width,height})
    await page.addInitScript(t=>localStorage.setItem('aether-theme',t),theme)
    const f=await fixture(page)
    await page.getByRole('button',{name:'规则类型',exact:true}).click()
    await page.getByRole('option',{name:'修改名称/添加序号',exact:true}).click()
    const header=page.locator('.rename-rule header').first()
    const initial=await header.getByRole('button',{name:'规则类型',exact:true}).boundingBox()
    await page.getByRole('button',{name:'添加规则',exact:true}).click()
    await page.getByRole('group',{name:'可选规则'}).getByRole('button',{name:'字符转换',exact:true}).click()
    await header.scrollIntoViewIfNeeded()
    const compact=await header.getByRole('button',{name:'规则类型',exact:true}).boundingBox()
    const remove=header.getByRole('button',{name:'删除规则',exact:true});await expect(remove).toHaveCSS('background-color','rgba(0, 0, 0, 0)');await expect(remove).toHaveCSS('border-top-width','0px');await expect(remove.locator('svg')).toHaveAttribute('width','12')
    expect(compact.width).toBeLessThan(initial.width)
    for(const control of await header.locator('strong,button').all()){
      const b=await control.boundingBox()
      expect(b.x).toBeGreaterThanOrEqual(0);expect(b.x+b.width).toBeLessThanOrEqual(width)
      expect(Math.abs(b.y+b.height/2-(compact.y+compact.height/2))).toBeLessThan(1)
    }
    expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
    await page.screenshot({path:info.outputPath('rule-header-layout.png')})
    await page.locator('.rename-preview-row').first().hover()
    await page.locator('.rename-preview-row').first().getByRole('button',{name:'修改',exact:true}).click()
    const modal=page.getByRole('dialog',{name:'重命名',exact:true})
    await expect(modal.locator('.rename-edit-original')).toHaveText('原名称：Original.mkv')
    const original=await modal.locator('.rename-edit-original').boundingBox(),input=await modal.getByLabel('新名称',{exact:true}).boundingBox()
    expect(original.y+original.height).toBeLessThan(input.y)
    await page.screenshot({path:info.outputPath('single-rename-original.png')})
    expect(f.unexpected).toEqual([])
  })
}

test('rename action tooltips require 500ms stationary and use ignore visibility semantics',async({page})=>{
  await fixture(page)
  await page.clock.install({time:new Date('2026-10-31T00:00:00Z')})
  await page.clock.pauseAt(new Date('2026-10-31T00:00:01Z'))
  const row=page.locator('.rename-preview-row').first(),button=row.getByRole('button',{name:'忽略',exact:true})
  await button.hover()
  await expect(button.locator('svg')).toHaveClass(/eye-off/)
  await page.clock.runFor(499)
  await expect(page.getByRole('tooltip')).toHaveCount(0)
  await page.clock.runFor(1)
  await expect(page.getByRole('tooltip')).toHaveText('忽略')
  const b=await button.boundingBox()
  await page.mouse.move(b.x+4,b.y+4)
  await expect(page.getByRole('tooltip')).toHaveCount(0)
  await page.clock.runFor(499)
  await expect(page.getByRole('tooltip')).toHaveCount(0)
  await page.clock.runFor(1)
  await expect(page.getByRole('tooltip')).toHaveText('忽略')
  await button.click()
  await expect(row.getByRole('button',{name:'取消忽略'}).locator('svg')).toHaveClass(/eye-icon/)
  await page.mouse.move(1,1)
  const edit=row.getByRole('button',{name:'修改',exact:true})
  await edit.hover();await page.clock.runFor(499);await expect(page.getByRole('tooltip')).toHaveCount(0)
  await page.clock.runFor(1);await expect(page.getByRole('tooltip')).toHaveText('修改')
  await page.mouse.move(1,1)
  await page.clock.resume()
  await page.getByRole('button',{name:'添加规则',exact:true}).click()
  await page.getByRole('group',{name:'可选规则'}).getByRole('button',{name:'添加字符',exact:true}).click()
  const remove=page.getByRole('button',{name:'删除规则',exact:true}).last()
  await page.clock.pauseAt(new Date('2026-10-31T00:01:00Z'))
  await page.clock.runFor(500)
  await remove.scrollIntoViewIfNeeded()
  await page.clock.runFor(500)
  await page.mouse.move(1,1)
  await remove.hover();await page.clock.runFor(499);await expect(page.getByRole('tooltip')).toHaveCount(0)
  await page.clock.runFor(1);await expect(page.getByRole('tooltip')).toHaveText('删除规则')
})

test('my plugin descriptions show only actual overflow after 300ms stationary',async({page})=>{
  await page.setViewportSize({width:1440,height:1000})
  const f=await fixture(page,'/tools')
  await expect(page.locator('.plugin-description')).toHaveCount(11)
  const descriptions=page.locator('.plugin-description')
  const geometries=await descriptions.evaluateAll(els=>els.map(el=>({clipped:el.scrollWidth>el.clientWidth||el.scrollHeight>el.clientHeight,text:el.textContent})))
  const short=geometries.findIndex(g=>!g.clipped),long=geometries.findIndex(g=>g.clipped)
  expect(short).toBeGreaterThanOrEqual(0)
  await page.clock.install({time:new Date('2026-10-31T00:00:00Z')});await page.clock.pauseAt(new Date('2026-10-31T00:00:01Z'))
  await descriptions.nth(short).hover();await page.clock.runFor(1000)
  await expect(page.getByRole('tooltip')).toHaveCount(0)
  // Narrowing the viewport exercises the same description's real line clamp.
  if(long<0)await page.setViewportSize({width:375,height:812})
  const clipped=await descriptions.evaluateAll(els=>els.findIndex(el=>el.scrollWidth>el.clientWidth||el.scrollHeight>el.clientHeight))
  expect(clipped).toBeGreaterThanOrEqual(0)
  const target=descriptions.nth(clipped),text=await target.textContent()
  await target.hover();await page.clock.runFor(299);await expect(page.getByRole('tooltip')).toHaveCount(0)
  await page.clock.runFor(1);await expect(page.getByRole('tooltip')).toHaveText(text)
  await page.mouse.move(1,1);await expect(page.getByRole('tooltip')).toHaveCount(0)
  expect(f.unexpected).toEqual([])
})

for(const [width,theme] of [[1440,'light'],[390,'dark']]){
  test(`rename confirmation prioritizes changed extension and expands clipped names ${width} ${theme}`,async({page},info)=>{
    await page.setViewportSize({width,height:844})
    const name='Interstellar.2014.'+'Very.Long.Media.Name.'.repeat(8)+'mkv'
    const replacement=name.slice(0,-3)+'mp4'
    await fixture(page,'/files?storage=p','115',name)
    await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
    await page.getByRole('button',{name,exact:true}).click();await page.keyboard.press('F2')
    await page.getByRole('textbox',{name:'新名称',exact:true}).fill(replacement)
    await page.getByRole('textbox',{name:'新名称',exact:true}).press('Enter')
    const modal=page.getByRole('dialog',{name:'确认修改名称'})
    await expect(modal).toContainText('原文件移入115回收站')
    const old=modal.locator('.rename-confirm-old'),next=modal.locator('.rename-confirm-new')
    expect((await next.boundingBox()).y).toBeGreaterThan((await old.boundingBox()).y)
    const changed=next.locator('.rename-name-part.changed').last()
    await expect(changed).toHaveText('p4')
    await expect(changed).toHaveCSS('flex-shrink','0')
    expect(await changed.evaluate(el=>el.scrollWidth<=el.clientWidth)).toBe(true)
    expect(await next.locator('.rename-name').evaluate(el=>[...el.querySelectorAll('.priority')].map(n=>n.textContent).join('').endsWith('.mp4'))).toBe(true)
    expect(await next.locator('.priority:not(.changed)').evaluateAll(els=>els.every(el=>el.scrollWidth<=el.clientWidth))).toBe(true)
    expect(await modal.evaluate(el=>el.scrollWidth<=el.clientWidth)).toBe(true)
    await page.clock.install({time:new Date('2026-10-31T00:00:00Z')});await page.clock.pauseAt(new Date('2026-10-31T00:00:01Z'))
    await next.locator('.rename-name').hover()
    await page.clock.runFor(299);await expect(page.getByRole('tooltip')).toHaveCount(0)
    await page.clock.runFor(1);await expect(page.getByRole('tooltip')).toHaveText(replacement)
    await page.clock.resume()
    await page.screenshot({path:info.outputPath('rename-comparison-tooltip.png')})
  })
}

test('short rename confirmation names do not produce unnecessary tooltips',async({page})=>{
  await fixture(page,'/files?storage=p','115','short.mkv')
  await page.getByRole('button',{name:'short.mkv',exact:true}).click();await page.keyboard.press('F2')
  await page.getByRole('textbox',{name:'新名称',exact:true}).fill('short.mp4')
  await page.getByRole('textbox',{name:'新名称',exact:true}).press('Enter')
  await page.clock.install({time:new Date('2026-10-31T00:00:00Z')});await page.clock.pauseAt(new Date('2026-10-31T00:00:01Z'))
  await page.locator('.rename-confirm-new .rename-name').hover();await page.clock.runFor(1000)
  await expect(page.getByRole('tooltip')).toHaveCount(0)
})

test('workbench and history survive sidebar navigation without replaying actions',async({page})=>{
  const f=await fixture(page)
  await page.getByLabel('查找内容',{exact:true}).fill('Original')
  await page.getByLabel('替换为',{exact:true}).fill('Changed')
  await expect(page.locator('.rename-changed').first()).toContainText('Changed.mkv')
  await page.locator('.rename-preview-row').last().hover()
  await page.locator('.rename-preview-row').last().getByRole('button',{name:'忽略',exact:true}).click()
  await page.locator('.sidebar').getByRole('link',{name:'运行日志',exact:true}).click()
  await page.locator('.sidebar').getByRole('link',{name:'文件服务',exact:true}).click()
  await expect(page.locator('.rename-workbench')).toBeVisible()
  await expect(page.getByLabel('替换为',{exact:true})).toHaveValue('Changed')
  await expect(page.locator('.rename-preview-row').last()).toHaveClass(/ignored/)
  await page.reload()
  await expect(page.locator('.rename-workbench')).toBeVisible()
  await expect(page.getByLabel('替换为',{exact:true})).toHaveValue('Changed')
  await expect(page.locator('.rename-preview-row').last()).toHaveClass(/ignored/)
  await page.getByRole('button',{name:'历史记录',exact:true}).click()
  await page.getByRole('button',{name:'选择历史目录'}).click()
  await page.getByRole('option').filter({hasText:'Another'}).click()
  await page.locator('.sidebar').getByRole('link',{name:'运行日志',exact:true}).click()
  await page.locator('.sidebar').getByRole('link',{name:'文件服务',exact:true}).click()
  await expect(page.locator('.rename-history-page')).toBeVisible()
  await expect(page.getByRole('button',{name:'选择历史目录'})).toContainText('Another')
  await page.getByRole('button',{name:'返回命名工作台'}).click()
  await page.getByRole('button',{name:'返回文件管理'}).click()
  await page.locator('.sidebar').getByRole('link',{name:'运行日志',exact:true}).click()
  await page.locator('.sidebar').getByRole('link',{name:'文件服务',exact:true}).click()
  await expect(page.locator('.rename-workbench')).toHaveCount(0)
  expect(f.writes).toEqual([]);expect(f.unexpected).toEqual([])
})
