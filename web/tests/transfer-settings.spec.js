import { test, expect } from '@playwright/test'

async function setup(page) {
  const configs = {
    transfer: {version:1,downloadSpeed:0,downloadUnit:'KB/s',uploadSpeed:0,uploadUnit:'KB/s',maxPreprocess:5,maxUploads:5,uploadDelay:10,crossCloudTemp:true,ignoredExtensions:['clfstmp','parts','tmp','downloading','aria2','temp','temp_video','~#0']},
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
    return route.fulfill({json:path==='/api/auth/status'?{initialized:true,authenticated:true}:path==='/api/state'?{username:'settings-revision',storages:[],tasks:[],settings:{},cache:{},logs:[]}:['/api/backup-rules','/api/transfers'].includes(path)?[]:{}})
  })
  return {configs,writes}
}

test('global transfer tab saves speed units, concurrency and extension rules across navigation', async ({page},info) => {
  const {writes}=await setup(page); await page.goto('/settings/account')
  const tabs=page.locator('.settings-navigation a')
  await expect(tabs.nth(1)).toHaveText('传输设置')
  await tabs.nth(1).click()
  await expect(page.getByRole('spinbutton',{name:'最大上传任务数',exact:true})).toHaveValue('5')
  await page.getByRole('spinbutton',{name:'上传延迟',exact:true}).fill('0')
  await page.getByRole('spinbutton',{name:'最大下载速度',exact:true}).fill('12')
  await page.getByRole('button',{name:'下载速度单位',exact:true}).click()
  await page.getByRole('option',{name:'MB/s',exact:true}).click()
  await page.getByRole('button',{name:'移除 tmp',exact:true}).click()
  await page.getByLabel('添加忽略扩展名').fill('.PARTIAL'); await page.getByLabel('添加忽略扩展名').press('Enter')
  await page.getByRole('button',{name:'保存设置',exact:true}).click()
  await expect.poll(()=>writes.length).toBe(1)
  expect(writes[0].value).toMatchObject({downloadSpeed:12,downloadUnit:'MB/s',uploadDelay:0})
  expect(writes[0].value.ignoredExtensions).toContain('partial'); expect(writes[0].value.ignoredExtensions).not.toContain('tmp')
  for (const [theme,width] of [['light',1440],['dark',390]]) {
    await page.evaluate(theme=>document.documentElement.dataset.theme=theme,theme); await page.setViewportSize({width,height:900})
    expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1)).toBe(true)
    await page.screenshot({path:info.outputPath(`transfer-settings-${theme}.png`)})
  }
  await page.getByRole('link',{name:'账号与安全',exact:true}).click(); await page.getByRole('link',{name:'传输设置',exact:true}).click()
  await expect(page.getByRole('spinbutton',{name:'最大下载速度',exact:true})).toHaveValue('12')
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
