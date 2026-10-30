import { test, expect } from '@playwright/test'

async function setupParticipation(page, { validationStatus = 200 } = {}) {
  let task = {id:'one',name:'媒体库',kind:'strm',storageId:'local',source:'/',target:'/media',enabled:true,retainedExtensions:'iso',apiInterval:200,mode:'incremental',cron:'0 2 * * *',cacheTTL:0}
  const validations = [], unexpected = []
  await page.route('**/api/**', route => {
    const request = route.request(), path = new URL(request.url()).pathname
    if (path === '/api/auth/status') return route.fulfill({json:{initialized:true,authenticated:true}})
    if (path === '/api/state') return route.fulfill({json:{username:'scrape-participation',storages:[{id:'local',name:'本地',type:'local',enabled:true,config:{}}],tasks:[task,{...task,id:'hidden',name:'隐藏库',scrapeExcluded:true}],settings:{},cache:{},traffic:{}}})
    if (path === '/api/traffic') return route.fulfill({json:{upload:0,download:0}})
    if (path === '/api/strm-replace') return route.fulfill({json:{tasks:[]}})
    if (path === '/api/cron/validate' && request.method() === 'POST') {
      const input = request.postDataJSON()
      validations.push(input)
      const error = validationStatus === 401 ? '会话已失效' : input.expression === '*/14 * * * *' ? 'Cron计划相邻执行间隔至少15分钟' : ''
      return route.fulfill({status:validationStatus,json:{error}})
    }
    if (path === '/api/tasks/one' && request.method() === 'PUT') {
      task = {...task,...request.postDataJSON()}
      return route.fulfill({json:task})
    }
    if (path.startsWith('/api/strm-scrape/')) return route.fulfill({json:path.endsWith('/items')?[]:{}})
    unexpected.push(`${request.method()} ${path}`)
    return route.fulfill({status:500,json:{error:'测试未定义接口'}})
  })
  return { get task() { return task }, validations, unexpected }
}

function cronResponse(page) {
  return page.waitForResponse(response => new URL(response.url()).pathname === '/api/cron/validate' && response.request().method() === 'POST')
}

test('STRM participation defaults on, saves exclusion and filters libraries', async ({ page }, info) => {
  const fixture = await setupParticipation(page)
  await page.goto('/tasks/strm')
  await page.getByRole('button',{name:'任务操作 媒体库',exact:true}).click()
  const validation = cronResponse(page)
  await page.getByRole('button',{name:'编辑任务',exact:true}).click()
  expect((await validation).status()).toBe(200)
  expect(fixture.validations).toEqual([{expression:'0 2 * * *',retry:false,shareLive:false}])
  await expect(page.getByRole('dialog',{name:'编辑 STRM 任务',exact:true})).toBeVisible()
  await page.getByRole('button',{name:'更多选项',exact:true}).click()
  const select=page.getByRole('button',{name:'STRM 刮削',exact:true})
  await expect(select).toHaveText('参与')
  const input=await page.getByLabel('保留扩展名',{exact:true}).boundingBox(), box=await select.boundingBox()
  expect(Math.abs(input.y-box.y)).toBeLessThan(5)
  await expect(page.getByRole('button',{name:'视频扩展名',exact:true}).locator('svg')).toHaveClass(/lucide-film/)
  await expect(page.getByRole('button',{name:'数据扩展名',exact:true}).locator('svg')).toHaveClass(/lucide-file-json/)
  await select.click();await page.getByRole('option',{name:'不参与',exact:true}).click()
  await page.screenshot({path:info.outputPath('scrape-participation.png')})
  await page.getByRole('button',{name:'保存任务',exact:true}).click()
  await expect.poll(()=>fixture.task.scrapeExcluded).toBe(true)
  await page.getByRole('link',{name:'STRM 刮削',exact:true}).click()
  await expect(page.getByRole('button',{name:'STRM 任务',exact:true})).toBeDisabled()
  expect(fixture.unexpected).toEqual([])
})

test('Cron validation warnings keep the STRM participation editor usable', async ({ page }) => {
  const fixture = await setupParticipation(page)
  await page.goto('/tasks/strm')
  await page.getByRole('button',{name:'任务操作 媒体库',exact:true}).click()
  const initial = cronResponse(page)
  await page.getByRole('button',{name:'编辑任务',exact:true}).click()
  expect((await initial).status()).toBe(200)
  const dialog = page.getByRole('dialog',{name:'编辑 STRM 任务',exact:true})
  const cron = dialog.getByRole('textbox',{name:'Cron 表达式',exact:true})
  const invalid = cronResponse(page)
  await cron.fill('*/14 * * * *')
  expect((await invalid).status()).toBe(200)
  await expect(cron).toHaveAttribute('aria-invalid','true')
  await expect(dialog.getByRole('button',{name:'Cron计划不可用原因',exact:true})).toBeVisible()
  await dialog.getByRole('button',{name:'更多选项',exact:true}).click()
  await dialog.getByRole('button',{name:'STRM 刮削',exact:true}).click()
  await page.getByRole('option',{name:'不参与',exact:true}).click()
  await expect(dialog.getByRole('button',{name:'STRM 刮削',exact:true})).toHaveText('不参与')
  const valid = cronResponse(page)
  await cron.fill('0 3 * * *')
  expect((await valid).status()).toBe(200)
  await expect(cron).not.toHaveAttribute('aria-invalid','true')
  await expect(dialog.getByRole('button',{name:'Cron计划不可用原因',exact:true})).toHaveCount(0)
  await expect(dialog).toBeVisible()
  await expect(page.getByRole('button',{name:'登录工作空间',exact:true})).toHaveCount(0)
  expect(fixture.validations.map(input => input.expression)).toEqual(['0 2 * * *','*/14 * * * *','0 3 * * *'])
  expect(fixture.unexpected).toEqual([])
})

test('Cron authorization failures still close the editor and require login', async ({ page }) => {
  const fixture = await setupParticipation(page,{validationStatus:401})
  await page.goto('/tasks/strm')
  await page.getByRole('button',{name:'任务操作 媒体库',exact:true}).click()
  const validation = cronResponse(page)
  await page.getByRole('button',{name:'编辑任务',exact:true}).click()
  expect((await validation).status()).toBe(401)
  await expect(page.getByRole('dialog',{name:'编辑 STRM 任务',exact:true})).toHaveCount(0)
  await expect(page.getByRole('button',{name:'登录工作空间',exact:true})).toBeVisible()
  expect(fixture.validations).toEqual([{expression:'0 2 * * *',retry:false,shareLive:false}])
  expect(fixture.unexpected).toEqual([])
})
