import { test, expect } from '@playwright/test'

for (const kind of ['cas', 'ed2k']) test(`${kind} task editor removes legacy retained extensions on new and edited tasks`, async ({ page }, info) => {
  let tasks = [{ id: 'legacy', kind, name: '旧任务', storageId: 'local', source: '/', target: '/media', mode: 'incremental', enabled: true, apiInterval: 200, cacheTTL: 0, retainedExtensions: 'iso', mediaExtensions: 'mkv;mp4', excludeFiles: 'sample', scrapeExcluded: false }]
  const saved = []
  await page.route('**/api/**', route => {
    const request = route.request(), path = new URL(request.url()).pathname
    if (path === '/api/auth/status') return route.fulfill({ json: { initialized: true, authenticated: true } })
    if (path === '/api/state') return route.fulfill({ json: { username: 'feature-naming', storages: [{ id: 'local', name: '本地', type: 'local', enabled: true, config: {} }], tasks, settings: {}, traffic: {}, cache: {} } })
    if (['POST', 'PUT'].includes(request.method()) && path.startsWith('/api/tasks')) {
      const body = request.postDataJSON()
      saved.push(body)
      const task = { ...body, id: request.method() === 'PUT' ? 'legacy' : 'new' }
      tasks = request.method() === 'PUT' ? [task] : [...tasks, task]
      return route.fulfill({ json: task })
    }
    return route.fulfill({ json: path === '/api/generation/jobs' || path === '/api/files' ? [] : {} })
  })
  await page.goto(`/tasks/${kind}`)
  await page.getByRole('button', { name: '任务操作 旧任务', exact: true }).click()
  await page.getByRole('button', { name: '编辑任务', exact: true }).click()
  await page.getByRole('button', { name: '更多选项', exact: true }).click()
  const completion=page.getByRole('button',{name:'完成动作',exact:true})
  await expect(completion).toHaveText('保留源文件')
  await expect(page.locator('.more-options .field').first()).toContainText('完成动作')
  await completion.click();await page.getByRole('option',{name:'删除源文件',exact:true}).click()
  await expect(page.getByLabel('保留扩展名', { exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '保留视频扩展名', exact: true })).toHaveCount(0)
  await expect(page.getByLabel('媒体扩展名', { exact: true })).toHaveValue('mkv;mp4')
  await page.getByRole('button', { name: '保存任务', exact: true }).click()
  await expect.poll(() => saved.length).toBe(1)
  expect(saved[0]).not.toHaveProperty('retainedExtensions')
  expect(saved[0]).toMatchObject({ kind, completionAction:'delete', target: '/media', mediaExtensions: 'mkv;mp4', excludeFiles: 'sample', scrapeExcluded: false })
  await page.getByRole('button', { name: `添加 ${kind.toUpperCase()} 任务卡片`, exact: true }).click()
  await page.getByRole('button', { name: '更多选项', exact: true }).click()
  await expect(page.getByLabel('保留扩展名', { exact: true })).toHaveCount(0)
  await page.getByLabel('任务名称').fill('新任务')
  await page.getByRole('button', { name: '选择目录', exact: true }).click()
  await page.locator('.source-accounts button').filter({ hasText: '本地' }).click()
  await page.getByRole('button', { name: '选择当前目录', exact: true }).click()
  await page.getByRole('button', { name: '保存任务', exact: true }).click()
  await expect.poll(() => saved.length).toBe(2)
  expect(saved[1]).not.toHaveProperty('retainedExtensions')
  expect(saved[1]).toMatchObject({ kind, completionAction:'keep', name: '新任务', storageId: 'local' })
  await page.getByRole('button', { name: '任务操作 新任务', exact: true }).click()
  await page.getByRole('button', { name: '编辑任务', exact: true }).click()
  await page.getByRole('button', { name: '更多选项', exact: true }).click()
  for (const [theme, width] of [['light', 1440], ['dark', 390]]) {
    await page.setViewportSize({ width, height: 1000 })
    await page.evaluate(theme => document.documentElement.dataset.theme = theme, theme)
    await expect(page.getByRole('button', { name: '完成动作', exact: true })).toHaveCSS('background-color', theme === 'dark' ? 'rgb(41, 44, 52)' : 'rgb(255, 255, 255)')
    await expect(page.getByLabel('保留扩展名', { exact: true })).toHaveCount(0)
    await expect(page.getByRole('button', { name: 'STRM 刮削', exact: true })).toBeVisible()
    await page.screenshot({ path: info.outputPath(`${kind}-options-${theme}.png`) })
  }
})
