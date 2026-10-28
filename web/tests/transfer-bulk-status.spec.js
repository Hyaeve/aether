import { test, expect } from '@playwright/test'
const statuses=['running','preparing','waiting','queued','paused','completed','skipped','ignored','failed','error']
async function setup(page, progress={}) {
  const calls=[]
  const entries=['copy','upload','download'].flatMap(kind=>statuses.map((status,i)=>({
    id:`${kind}-${status}`, kind, status, name:`${kind}-真实名称-${i}.mkv`,
    source:'文件管理', storage:'来源', done:25,total:100,speed:1024,
    started:'2026-10-28T08:00:00Z',message:status==='error'?'目标目录不可访问':'',
    controls:{pause:true,retry:status==='failed',delete:true},
    from:{storage:'移动云盘',type:'mobile',directory:'源目录'},
    to:{storage:'家庭影音',type:'local',directory:'剧集'},
    ...(i===0?progress:{})
  })))
  await page.route('**/api/**',async route=>{
    const path=new URL(route.request().url()).pathname
    let json={}
    if(path==='/api/auth/status') json={initialized:true,authenticated:true}
    else if(path==='/api/state') json={username:'transfer-workspace',storages:[],tasks:[],settings:{},cache:{},traffic:{}}
    else if(path==='/api/transfers') json=entries
    else if(path==='/api/transfers/action') { calls.push(route.request().postDataJSON()); json={ok:true} }
    await route.fulfill({json})
  })
  return calls
}
for(const width of [1440,390]) for(const theme of ['light','dark']) test(`transfer bulk selection, real progress and temporary filter ${width} ${theme}`,async({page},info)=>{
  const calls=await setup(page)
  await page.setViewportSize({width,height:950})
  await page.goto('/transfer')
  await page.evaluate(theme=>document.documentElement.dataset.theme=theme,theme)
  await expect(page.getByRole('columnheader',{name:'进度状态',exact:true})).toHaveCount(1)
  const summary=page.getByRole('tab',{name:/^复制/})
  await expect(summary).toContainText('10 项')
  await expect(summary).toContainText('25%')
  for(const mode of ['copy','upload','download']){
    await page.getByRole('tab',{name:new RegExp(`^${{copy:'复制',upload:'上传',download:'下载'}[mode]}`)}).click()
    const row=page.locator('.transfer-table tr').filter({hasText:`${mode}-真实名称-0.mkv`})
    await row.click({button:'right'})
    const dock=page.getByRole('toolbar',{name:'所选传输任务操作'})
    await expect(dock).toBeVisible()
    await expect(dock.getByRole('button',{name:'暂停',exact:true})).toBeEnabled()
    await dock.getByRole('button',{name:'暂停',exact:true}).click()
    await expect.poll(()=>calls.some(c=>c.id===`${mode}-running`&&c.action==='pause')).toBe(true)
    await page.keyboard.press('Escape')
    await expect(dock).toHaveCount(0)
  }
  await page.getByRole('button',{name:'筛选传输状态',exact:true}).click()
  await page.getByRole('checkbox',{name:'失败',exact:true}).check()
  await expect(page.locator('.transfer-table tbody tr:not(.transfer-spacer)')).toHaveCount(1)
  await page.getByRole('button',{name:'筛选传输状态',exact:true}).click()
  await page.getByRole('tab',{name:/^复制/}).click()
  await expect(page.locator('.transfer-table tbody tr:not(.transfer-spacer)')).toHaveCount(10)
  const first=page.locator('.transfer-table tbody tr:not(.transfer-spacer)').first()
  await first.click({button:'right'})
  const dock=page.getByRole('toolbar',{name:'所选传输任务操作'})
  const styles=await dock.getByRole('button',{name:'暂停',exact:true}).evaluate(el=>({border:getComputedStyle(el).borderTopWidth,background:getComputedStyle(el).backgroundColor}))
  expect(styles.border).toBe('0px')
  expect(styles.background).toBe('rgba(0, 0, 0, 0)')
  expect(await first.locator('progress').evaluate(el=>el.getBoundingClientRect().width)).toBeLessThanOrEqual(108)
  expect(await first.locator('progress').evaluate(el=>el.getBoundingClientRect().height)).toBe(4)
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
  await page.screenshot({path:info.outputPath(`transfer-${width}-${theme}.png`)})
})

test('transfer bulk resume, retry, deletion and filters retain real action semantics',async({page})=>{
  const calls=await setup(page)
  await page.goto('/transfer')
  const search=page.getByRole('textbox',{name:'搜索传输任务',exact:true})
  const dock=page.getByRole('toolbar',{name:'所选传输任务操作'})
  for(const [index,action,label] of [[4,'resume','恢复'],[8,'retry','重试'],[5,'delete','删除']]){
    await search.fill(`真实名称-${index}.mkv`)
    const row=page.locator('.transfer-table tbody tr:not(.transfer-spacer)').first()
    await row.click({button:'right'})
    await dock.getByRole('button',{name:label,exact:true}).click()
    if(action!=='resume'){
      const dialog=page.getByRole('dialog',{name:'确认'+label,exact:true})
      await expect(dialog).toContainText('1 项')
      await dialog.getByRole('button',{name:'确认',exact:true}).click()
    }
    await expect.poll(()=>calls.some(c=>c.action===action&&c.id===`copy-${statuses[index]}`)).toBe(true)
    await page.keyboard.press('Escape')
  }
  await search.fill('')
  await page.getByRole('button',{name:'筛选传输状态',exact:true}).click()
  await page.getByRole('checkbox',{name:'错误',exact:true}).check()
  await expect(page.locator('.transfer-table tbody tr:not(.transfer-spacer)')).toHaveCount(1)
  await page.reload()
  await expect(page.locator('.transfer-table tbody tr:not(.transfer-spacer)')).toHaveCount(10)
  await page.getByRole('button',{name:'筛选传输状态',exact:true}).click()
  await page.getByRole('checkbox',{name:'失败',exact:true}).check()
  await page.getByRole('link',{name:'备份规则',exact:true}).click()
  await page.getByRole('link',{name:'传输任务',exact:true}).click()
  await expect(page.locator('.transfer-table tbody tr:not(.transfer-spacer)')).toHaveCount(10)
})

for(const width of [1440,390]) for(const theme of ['light','dark']) test(`large downloads expose fractional byte progress ${width} ${theme}`,async({page},info)=>{
  await setup(page,{done:26_000_000,total:85_118_265_822,speed:26_000_000})
  await page.setViewportSize({width,height:950})
  await page.goto('/transfer?mode=download')
  await page.evaluate(value=>document.documentElement.dataset.theme=value,theme)
  await expect(page.locator('html')).toHaveAttribute('data-theme',theme)
  const row=page.locator('.transfer-table tr').filter({hasText:'download-真实名称-0.mkv'})
  await expect(row.locator('.transfer-status')).toHaveText('进行0.03%')
  await expect(page.getByRole('tab',{name:/^下载/})).toContainText('0.03%')
  await expect(row.locator('progress')).toHaveAttribute('value','0.03')
  const geometry=await row.locator('.transfer-status').evaluate(el=>({width:el.clientWidth,scroll:el.scrollWidth}))
  expect(geometry.scroll).toBeLessThanOrEqual(geometry.width)
  await page.screenshot({path:info.outputPath(`fractional-${width}-${theme}.png`)})
})
