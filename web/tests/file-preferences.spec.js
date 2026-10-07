import { test, expect } from '@playwright/test'

test('client view persists and toolbox animates in both directions', async ({ page, context }, info) => {
  await context.route('**/api/auth/status', r => r.fulfill({ json: { initialized: true, authenticated: true } }))
  await context.route('**/api/state', r => r.fulfill({ json: { username: 'preferences', storages: [{ id: 'local', name: '文件库', type: 'local', enabled: true, config: {} }], tasks: [], settings: {}, traffic: {}, cache: {}, logs: [] } }))
  await context.route('**/api/files?**', r => r.fulfill({ json: [{ id: '/movie.mp4', name: 'movie.mp4', size: 42 }] }))
  await page.goto('/files')
  await page.getByRole('button', { name: '当前列表视图，切换网格' }).click()
  expect(await page.evaluate(() => localStorage.getItem('aether-files-view'))).toBe('grid')
  await page.reload()
  await expect(page.getByRole('button', { name: '当前网格视图，切换列表' })).toBeVisible()
  const tab = await context.newPage()
  await tab.goto('/files')
  await expect(tab.getByRole('button', { name: '当前网格视图，切换列表' })).toBeVisible()
  await tab.close()
  await page.getByRole('button', { name: '当前网格视图，切换列表' }).click()
  await page.reload()
  await expect(page.getByRole('button', { name: '当前列表视图，切换网格' })).toBeVisible()
  await page.emulateMedia({ reducedMotion: 'no-preference' })
  await page.evaluate(() => {
    window.menuClasses = []
    new MutationObserver(records => {
      for (const record of records) {
        if (record.target.classList?.contains('file-create-menu')) window.menuClasses.push(record.target.className)
      }
    }).observe(document.body, { subtree: true, attributes: true, attributeFilter: ['class'] })
  })
  const trigger = page.getByRole('button', { name: '工具', exact: true })
  await trigger.click()
  await expect(page.getByRole('menu')).toHaveCSS('opacity', '1')
  await expect(trigger.locator('.tools-chevron')).toHaveClass(/expanded/)
  await page.screenshot({ path: info.outputPath('toolbox.png') })
  await trigger.click()
  await expect(page.getByRole('menu')).toHaveCount(0)
  const classes = await page.evaluate(() => window.menuClasses.join(' '))
  expect(classes).toContain('select-popup-enter-active')
  expect(classes).toContain('select-popup-leave-active')
})
