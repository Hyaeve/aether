import {test,expect} from '@playwright/test'
async function setup(page){
  const storages=[
    {id:'annual',name:'115年费存储',type:'115',tier:'annual'},
    {id:'forever',name:'115永久存储',type:'115',tier:'permanent'},
    {id:'silver',name:'移动白银',type:'mobile',tier:'silver'},
    {id:'gold',name:'移动黄金',type:'mobile',tier:'gold'},
    {id:'diamond',name:'移动钻石',type:'mobile',tier:'diamond'},
    {id:'tgold',name:'天翼黄金',type:'tianyi',tier:'gold'},
    {id:'platinum',name:'天翼铂金',type:'tianyi',tier:'platinum'},
    {id:'qsvip',name:'夸克超级会员',type:'quark',tier:'svip'},
    {id:'q88',name:'夸克88会员',type:'quark',tier:'88vip'},
    {id:'unknown',name:'未知会员',type:'mobile',tier:'unrecognized'},
  ].map(s=>({...s,enabled:true,config:{},usage:{used:25,total:100,membership:{tier:s.tier,checkedAt:new Date().toISOString()}}}))
  await page.route('**/api/**',r=>{
    const path=new URL(r.request().url()).pathname
    let json={}
    if(path==='/api/auth/status')json={initialized:true,authenticated:true}
    else if(path==='/api/state')json={username:'selection-theme',storages,tasks:[{id:'t',kind:'strm',name:'媒体库'}],settings:{},traffic:{},cache:{}}
    else if(path.endsWith('/usage'))json=storages.find(s=>path.includes(s.id))?.usage||{}
    else if(path==='/api/cloud/manage')json={items:[{id:'one',name:'同一条内容.mkv',size:100,time:new Date().toISOString(),status:'error',progress:.5}],edit:true}
    else if(path==='/api/strm-scrape/items')json=[{path:'Show/a.strm',title:'作品',kind:'movie',status:'pending'}]
    else if(path==='/api/strm-scrape/status')json={running:false}
    else if(path==='/api/transfers')json=[]
    return r.fulfill({json})
  })
}
for(const width of [1440,390])for(const theme of ['light','dark'])test(`unified selection, sidebar and member watermark ${width} ${theme}`,async({page},info)=>{
  await setup(page)
  await page.setViewportSize({width,height:950})
  await page.goto('/storage')
  await page.evaluate(theme=>document.documentElement.dataset.theme=theme,theme)
  await expect(page.locator('.storage-membership')).toHaveCount(9)
  for(const label of ['年费VIP','永久VIP','白银会员','钻石会员','铂金会员'])await expect(page.getByRole('img',{name:label,exact:true})).toBeVisible()
  for(const label of ['88VIP','SVIP'])await expect(page.getByRole('img',{name:label,exact:true})).toBeVisible()
  for(const card of await page.locator('.storage-card').all()){
    expect(await card.evaluate(el=>el.scrollWidth<=el.clientWidth)).toBe(true)
    expect(await card.locator('.storage-card-top').evaluate(el=>getComputedStyle(el).zIndex)).toBe('1')
  }
  if(width<760)await page.getByRole('button',{name:'打开导航',exact:true}).click()
  const sidebar=page.locator('.sidebar')
  const rgb=await sidebar.evaluate(el=>getComputedStyle(el).backgroundColor.match(/\d+/g).map(Number))
  if(theme==='light')expect(rgb.slice(0,3)).toEqual([217,224,239])
  else expect(rgb.slice(0,3).every(v=>v<120)).toBe(true)
  const active=sidebar.locator('[aria-current=page]')
  await expect(active).not.toHaveCSS('background-color',await sidebar.evaluate(el=>getComputedStyle(el).backgroundColor))
  if(width>=760){
    await active.hover()
    await expect.poll(()=>active.locator('span').evaluate(el=>getComputedStyle(el).transform)).not.toBe('none')
  }
  await page.screenshot({path:info.outputPath(`storage-sidebar-${width}-${theme}.png`)})
  if(width<760){
    await page.locator('.nav-overlay').click({position:{x:width-8,y:100}})
    await expect(page.locator('.nav-overlay')).toHaveCount(0)
  }
  let reference
  for(const kind of ['shares','downloads','trash']){
    await page.goto(`/storage/${kind}`)
    await page.evaluate(theme=>document.documentElement.dataset.theme=theme,theme)
    await expect(page.locator('html')).toHaveAttribute('data-theme',theme)
    const row=page.locator('.cloud-management-row').first()
    await row.click({button:'right'})
    const dock=page.getByRole('toolbar',{name:'所选云盘条目操作'})
    await expect(dock).toBeVisible()
    const color=await row.evaluate(el=>getComputedStyle(el).backgroundColor)
    if(reference)expect(color).toBe(reference)
    reference=color
    for(const button of await dock.getByRole('button').all()){
      await expect(button).toHaveCSS('border-top-width','0px')
      await expect(button).toHaveCSS('background-color','rgba(0, 0, 0, 0)')
    }
    expect(await dock.evaluate(el=>el.getBoundingClientRect().height)).toBeLessThan(width>600?43:86)
    if(kind==='downloads')await page.screenshot({path:info.outputPath(`cloud-selection-${width}-${theme}.png`)})
    await page.keyboard.press('Escape')
    await expect(dock).toHaveCount(0)
  }
  await page.goto('/tasks/scrape')
  await page.evaluate(theme=>document.documentElement.dataset.theme=theme,theme)
  await expect(page.locator('html')).toHaveAttribute('data-theme',theme)
  await page.locator('.scrape-card').first().click({button:'right'})
  const dock=page.getByRole('toolbar',{name:'所选作品操作'})
  await expect(dock).toBeVisible()
  const button=dock.getByRole('button',{name:'重置',exact:true})
  await button.hover()
  await expect.poll(()=>button.locator('span').evaluate(el=>getComputedStyle(el).transform)).not.toBe('none')
  expect(await dock.evaluate(el=>el.getBoundingClientRect().height)).toBeLessThan(width>600?43:86)
  await page.emulateMedia({reducedMotion:'reduce'})
  await expect(button.locator('span')).toHaveCSS('transform','none')
  await page.screenshot({path:info.outputPath(`selection-${width}-${theme}.png`)})
})
