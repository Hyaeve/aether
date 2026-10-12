import { test, expect } from '@playwright/test'

test('task and backup health pauses are red, block execution and recover without changing enable switches', async ({page}, info) => {
  let paused = true
  const reason = '存储池「云盘」健康异常，暂时停止'
  await page.route('**/api/**', async route => {
    const path = new URL(route.request().url()).pathname
    let json = {}
    if (path === '/api/auth/status') json = {initialized:true,authenticated:true}
    if (path === '/api/state') json = {username:'health-test',storages:[{id:'s',name:'云盘',type:'115',enabled:true,status:paused?'error':'connected'}],tasks:[{id:'t',name:'视频任务',kind:'strm',storageId:'s',enabled:true,status:'idle',processed:0,healthPause:paused?reason:''}],settings:{},traffic:{},cache:{}}
    if (path === '/api/backup-rules') json = [{id:'b',name:'云盘备份',enabled:true,status:'idle',sourceId:'s',targetId:'s',healthPause:paused?reason:''}]
    await route.fulfill({json})
  })
  await page.goto('/tasks/strm')
  const task = page.locator('.task-row-card')
  await expect(task).toHaveClass(/health-paused/)
  await expect(task.getByRole('button',{name:'健康异常，暂时停止',exact:true})).toBeDisabled()
  await expect(task.locator('.task-result')).toBeVisible()
  await expect(task.locator('.task-result')).toHaveText(reason)
  await page.screenshot({path:info.outputPath('task-health-light.png')})
  await page.evaluate(()=>document.documentElement.dataset.theme='dark')
  await expect.poll(()=>page.evaluate(()=>getComputedStyle(document.documentElement).getPropertyValue('--surface').trim())).toBe('#1b1d23')
  await expect.poll(()=>task.locator('.task-row-copy strong').evaluate(el=>getComputedStyle(el).color)).toBe('rgb(227, 230, 237)')
  await page.screenshot({path:info.outputPath('task-health-dark.png')})
  paused = false
  await page.reload()
  await expect(task).not.toHaveClass(/health-paused/)
  await expect(task.getByRole('button',{name:'立即执行',exact:true})).toBeEnabled()
  await expect(task.getByRole('button',{name:'停用任务 视频任务',exact:true})).toHaveAttribute('aria-pressed','true')
  paused = true
  await page.goto('/transfer/backup')
  const backup = page.locator('.backup-card')
  await expect(backup).toHaveClass(/health-paused/)
  await expect(backup.getByRole('button',{name:'健康异常，暂时停止',exact:true})).toBeDisabled()
  await expect(backup.locator('.backup-name small')).toHaveText('健康异常，暂时停止')
  await page.evaluate(()=>document.documentElement.dataset.theme='dark')
  await expect.poll(()=>page.evaluate(()=>getComputedStyle(document.documentElement).getPropertyValue('--surface').trim())).toBe('#1b1d23')
  await expect.poll(()=>backup.locator('.backup-name strong').evaluate(el=>getComputedStyle(el).color)).toBe('rgb(227, 230, 237)')
  await page.screenshot({path:info.outputPath('backup-health-dark.png')})
  paused = false
  await expect(backup).not.toHaveClass(/health-paused/)
  await expect(backup.getByRole('button',{name:'执行备份',exact:true})).toBeEnabled()
  await expect(backup.getByRole('button',{name:'停用备份 云盘备份',exact:true})).toHaveAttribute('aria-pressed','true')
})
