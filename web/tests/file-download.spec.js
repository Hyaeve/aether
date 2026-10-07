import { test, expect } from '@playwright/test'

test('download is first in list and grid menus and folders require confirmation', async ({ page }, info) => {
  await page.route('**/api/auth/status', r => r.fulfill({ json: { initialized: true, authenticated: true } }))
  await page.route('**/api/state', r => r.fulfill({ json: { username: 'download', storages: [{ id: '115', name: '115', type: '115', enabled: true, config: {} }], tasks: [], settings: {}, traffic: {}, cache: {} } }))
  await page.route('**/api/files?**', r => r.fulfill({ json: [{ id: 'f', name: 'movie.mp4', url: '/stream/token?sign=signed', size: 42 }, { id: 'dir', name: 'Movies', isDir: true }] }))
  await page.goto('/files')
  await page.evaluate(() => {
    HTMLAnchorElement.prototype.click = function () { window.downloadClicked = { href: this.href, name: this.download, rel: this.rel } }
  })
  for (const selector of ['.file-row', '.file-grid-item']) {
    await page.locator(selector).filter({ hasText: 'movie.mp4' }).click({ button: 'right' })
    await expect(page.locator('.context-menu button').first()).toHaveText('下载')
    await page.locator('.context-menu').getByRole('button', { name: '下载', exact: true }).click()
    const download = await page.evaluate(() => window.downloadClicked)
    expect(download.href).toContain('/stream/token?sign=signed&download=1')
    expect(download.name).toBe('movie.mp4')
    await page.locator(selector).filter({ hasText: 'Movies' }).click({ button: 'right' })
    await page.locator('.context-menu button').first().click()
    await expect(page.getByRole('dialog', {name:'下载文件夹'})).toContainText('ZIP')
    await page.getByRole('button', {name:'取消',exact:true}).click()
    if (selector === '.file-row') await page.getByRole('button', { name: '当前列表视图，切换网格' }).click()
  }
  await page.locator('.file-grid-item').filter({ hasText: 'movie.mp4' }).click({ button: 'right' })
  await page.screenshot({ path: info.outputPath('download-menu.png') })
  await page.keyboard.press('Escape')
  await page.evaluate(() => { window.open = url => { window.archiveDownload = url } })
  await page.locator('.file-grid-item').filter({hasText:'Movies'}).click({button:'right'})
  await page.locator('.context-menu button').first().click()
  await page.getByRole('button', {name:'确认下载'}).click()
  expect(await page.evaluate(() => window.archiveDownload)).toContain('/api/files/archive?storage=115&parent=%2F&id=dir')
})
