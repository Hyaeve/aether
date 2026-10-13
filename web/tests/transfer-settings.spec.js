import { test, expect } from '@playwright/test'

async function setup(page) {
  const configs = {
    transfer: {version:1,tempDirectory:'',downloadSpeed:0,downloadUnit:'KB/s',uploadSpeed:0,uploadUnit:'KB/s',maxPreprocess:5,maxUploads:5,uploadDelay:10,crossCloudTemp:true,ignoredExtensions:['clfstmp','parts','tmp','downloading','aria2','temp','temp_video','~#0']},
    backup: {version:1,pauseBidirectional:false,queueUpper:10000,queueLower:0,maxConcurrentScan:1}
  }
  const writes = []
  await page.route('**/api/**', route => {
    const request=route.request(), path=new URL(request.url()).pathname
    const type=['transfer','backup'].find(type=>path===`/api/${type}/settings`)
    if(type) {
      if(request.method()==='PUT') { configs[type]=request.postDataJSON(); writes.push({type,value:configs[type]}) }
      return route.fulfill({json:configs[type]})
    }
    return route.fulfill({json:path==='/api/local-directories'?{path:'/data/temporary',items:[]}:path==='/api/auth/status'?{initialized:true,authenticated:true}:path==='/api/state'?{username:'settings-revision',storages:[],tasks:[],settings:{},cache:{},logs:[]}:['/api/backup-rules','/api/transfers'].includes(path)?[]:{}})
  })
  return {configs,writes}
}

test('global transfer tab saves speed units, concurrency and extension rules across navigation', async ({page},info) => {
  const {writes}=await setup(page); await page.goto('/settings/account')
  const tabs=page.locator('.settings-navigation a')
  await expect(tabs.nth(1)).toHaveText('传输设置')
  await tabs.nth(1).click()
  await expect(tabs.nth(1).locator('svg')).toHaveClass(/lucide-cable/)
  await expect(page.getByRole('link',{name:'传输中心',exact:true}).locator('svg')).toHaveClass(/lucide-folder-sync/)
  await expect(page.locator('.settings-section-title').filter({has:page.getByRole('heading',{name:'速度限制',exact:true})}).locator('svg')).toHaveClass(/lucide-sliders-horizontal/)
  await expect(page.getByRole('spinbutton',{name:'最大上传任务数',exact:true})).toHaveValue('5')
  await page.getByRole('spinbutton',{name:'上传延迟',exact:true}).fill('0')
  await page.getByRole('spinbutton',{name:'最大下载速度',exact:true}).fill('12')
  await page.getByRole('button',{name:'下载速度单位',exact:true}).click()
  await page.getByRole('option',{name:'MB/s',exact:true}).click()
  await page.getByRole('button',{name:'移除 tmp',exact:true}).click()
  await page.getByLabel('添加忽略扩展名').fill('.PARTIAL'); await page.getByLabel('添加忽略扩展名').press('Enter')
  await expect(page.getByRole('button',{name:'保存设置',exact:true})).toHaveCount(0)
  await expect.poll(()=>writes.filter(w=>w.type==='transfer').at(-1)?.value).toMatchObject({downloadSpeed:12,downloadUnit:'MB/s',uploadDelay:0,ignoredExtensions:expect.arrayContaining(['partial'])})
  expect(writes.filter(w=>w.type==='transfer').at(-1).value.ignoredExtensions).not.toContain('tmp')
  await expect(page.getByRole('heading',{name:'全局忽略文件扩展名',exact:true})).toBeVisible()
  await expect(page.getByRole('heading',{name:'备份资源限制',exact:true})).toHaveCount(0)
  const columns=await page.locator('.transfer-settings-columns > *').evaluateAll(nodes=>nodes.map(node=>node.getBoundingClientRect()))
  expect(columns).toHaveLength(2)
  expect(Math.abs(columns[0].width-columns[1].width)).toBeLessThan(2)
  expect(columns[0].x).toBeLessThan(columns[1].x)
  await page.getByRole('button',{name:'选择临时文件位置',exact:true}).click()
  await page.getByRole('button',{name:'选择当前目录',exact:true}).click()
  await expect.poll(()=>writes.filter(w=>w.type==='transfer').at(-1)?.value.tempDirectory).toBe('/data/temporary')
  for (const [theme,width] of [['light',1920],['dark',1920],['light',1440],['dark',1440],['light',390],['dark',390]]) {
    await page.evaluate(theme=>document.documentElement.dataset.theme=theme,theme); await page.setViewportSize({width,height:900})
    await expect(page.getByRole('button',{name:'下载速度单位',exact:true})).toHaveCSS('background-color',theme==='dark'?'rgb(41, 44, 52)':'rgb(255, 255, 255)')
    expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1)).toBe(true)
    const layout=await page.locator('.transfer-settings-form').evaluate(el=>{
      const rect=node=>{const {x,y,width,height,right}=node.getBoundingClientRect();return {x,y,width,height,right}}
      return {form:rect(el),parent:rect(el.parentElement),columns:[...el.querySelector('.transfer-settings-columns').children].map(rect),extensions:rect(el.querySelector('fieldset > .settings-section')),label:rect(el.querySelector('label[for="transfer-temp-path"]')),input:rect(el.querySelector('#transfer-temp-path'))}
    })
    expect(Math.abs(layout.form.right-layout.parent.right)).toBeLessThan(2)
    expect(Math.abs(layout.extensions.width-layout.form.width)).toBeLessThan(2)
    expect(layout.label.right).toBeLessThan(layout.input.x)
    expect(Math.abs(layout.label.y+layout.label.height/2-layout.input.y-layout.input.height/2)).toBeLessThan(2)
    if(width>=1440) {
      expect(Math.abs(layout.columns[0].width-(layout.form.width-16)/2)).toBeLessThan(2)
      expect(Math.abs(layout.columns[1].right-layout.form.right)).toBeLessThan(2)
    } else {
      expect(Math.abs(layout.columns[0].width-layout.form.width)).toBeLessThan(2)
      expect(layout.columns[1].y).toBeGreaterThan(layout.columns[0].y)
    }
    await page.screenshot({path:info.outputPath(`transfer-settings-${theme}-${width}.png`)})
  }
  await page.getByRole('link',{name:'账号与安全',exact:true}).click(); await page.getByRole('link',{name:'传输设置',exact:true}).click()
  await expect(page.getByRole('spinbutton',{name:'最大下载速度',exact:true})).toHaveValue('12')
  await expect(page.getByLabel('路径',{exact:true})).toHaveValue('/data/temporary')
})

test('transfer automatic saves serialize edits, retain drafts and recover from errors',async({page})=>{
  const {writes}=await setup(page)
  let release,first=true
  await page.route('**/api/transfer/settings',async route=>{
    if(route.request().method()!=='PUT')return route.fallback()
    if(first){first=false;await new Promise(resolve=>release=resolve)}
    return route.fallback()
  })
  await page.goto('/settings/transfer')
  const input=page.getByRole('spinbutton',{name:'最大下载速度',exact:true})
  await input.fill('10')
  await expect.poll(()=>!!release).toBe(true)
  await input.fill('20')
  await page.getByRole('spinbutton',{name:'最大上传任务数',exact:true}).fill('8')
  await expect(input).toHaveValue('20')
  release()
  await expect.poll(()=>writes.filter(w=>w.type==='transfer').at(-1)?.value).toMatchObject({downloadSpeed:20,maxUploads:8})
  expect(writes.filter(w=>w.type==='transfer').map(w=>w.value.downloadSpeed)).toEqual([10,20])
  await input.fill('')
  await input.blur()
  await page.getByLabel('路径',{exact:true}).fill('/data/tmp')
  await page.getByLabel('路径',{exact:true}).press('Enter')
  expect(writes.filter(w=>w.type==='transfer')).toHaveLength(2)
  await input.fill('30')
  await expect.poll(()=>writes.filter(w=>w.type==='transfer').at(-1)?.value.downloadSpeed).toBe(30)
  let fail=true
  await page.route('**/api/transfer/settings',route=>route.request().method()==='PUT'&&fail?route.fulfill({status:500,json:{error:'保存失败'}}):route.fallback())
  await input.fill('40')
  await expect(page.getByRole('alert')).toHaveText('保存失败')
  await expect(input).toHaveValue('40')
  fail=false;await page.getByRole('button',{name:'重试保存',exact:true}).click()
  await expect.poll(()=>writes.filter(w=>w.type==='transfer').at(-1)?.value.downloadSpeed).toBe(40)
  await input.fill('50')
  await page.getByRole('link',{name:'账号与安全',exact:true}).click()
  await expect.poll(()=>writes.filter(w=>w.type==='transfer').at(-1)?.value.downloadSpeed).toBe(50)
})

test('backup settings full page saves bounded hysteresis and unlimited mode', async ({page},info) => {
  const {writes}=await setup(page); await page.goto('/transfer/backup')
  await page.getByRole('button',{name:'备份设置',exact:true}).click()
  await expect(page).toHaveURL(/\/transfer\/backup\/settings$/)
  await expect(page.getByRole('spinbutton',{name:'队列上限',exact:true})).toHaveValue('10000')
  await page.getByRole('switch',{name:'暂停双向同步',exact:true}).check()
  await page.getByRole('spinbutton',{name:'队列上限',exact:true}).fill('100')
  await page.getByRole('button',{name:'队列下限模式',exact:true}).click(); await page.getByRole('option',{name:'自定义',exact:true}).click()
  await page.getByRole('spinbutton',{name:'队列下限',exact:true}).fill('30')
  await page.getByRole('spinbutton',{name:'最大并发扫描数',exact:true}).fill('2')
  await page.getByRole('button',{name:'保存设置',exact:true}).click(); await expect.poll(()=>writes.length).toBe(1)
  expect(writes[0].value).toMatchObject({pauseBidirectional:true,queueUpper:100,queueLower:30,maxConcurrentScan:2})
  await page.screenshot({path:info.outputPath('backup-resources.png')})
  await page.getByRole('button',{name:'队列上限模式',exact:true}).click(); await page.getByRole('option',{name:'无限制',exact:true}).click()
  await page.getByRole('button',{name:'保存设置',exact:true}).click(); await expect.poll(()=>writes.length).toBe(2)
  expect(writes[1].value).toMatchObject({queueUpper:0,queueLower:0})
  await page.getByRole('button',{name:'返回备份规则',exact:true}).click()
  await expect(page.locator('.task-heading').getByRole('button',{name:'添加规则',exact:true})).toBeVisible()
})
