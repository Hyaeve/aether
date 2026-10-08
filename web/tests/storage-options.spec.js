import { test, expect } from '@playwright/test'

test('provider forms have aligned modes without an enabled switch', async ({ page }, info) => {
  await page.route('**/api/auth/status', r => r.fulfill({ json: { initialized: true, authenticated: true } }))
  await page.route('**/api/state', r => r.fulfill({ json: { storages: [], tasks: [], settings: {}, traffic: {}, cache: {} } }))
  await page.goto('/storage')
  for (const kind of ['115', 'mobile', 'tianyi', 'quark', 'openlist', 'webdav', 'local']) {
    await page.getByRole('button', { name: '添加存储池', exact: true }).click()
    await page.locator(`.driver-option[data-provider="${kind}"]`).click()
    const dialog = page.getByRole('dialog')
    await expect(dialog.getByText('启用此存储池', { exact: true })).toHaveCount(0)
    const cloud = ['115', 'mobile', 'tianyi', 'quark'].includes(kind)
    await expect(dialog.getByRole('button', { name: '删除模式', exact: true })).toHaveCount(cloud ? 1 : 0)
    if (cloud) {
      const download = await dialog.getByRole('button', { name: '下载模式', exact: true }).boundingBox()
      const deletion = await dialog.getByRole('button', { name: '删除模式', exact: true }).boundingBox()
      expect(download.y).toBe(deletion.y)
      expect(download.x).toBeLessThan(deletion.x)
    }
    if (kind === '115' || kind === 'tianyi') {
      const name = await dialog.getByLabel('存储池名称').boundingBox()
      const mode = await dialog.getByRole('button', { name: kind === '115' ? '设备类型' : '天翼接入模式', exact: true }).boundingBox()
      expect(name.y).toBe(mode.y)
    }
    if (kind === 'openlist') {
      await expect(dialog.getByRole('button', {name:'透传 UA 给上游',exact:true})).toHaveText('是')
      await expect(dialog.getByRole('button', {name:'列目录时刷新上游',exact:true})).toHaveText('否')
      await expect(dialog.getByLabel('缓存时间', { exact: true })).toHaveCount(0)
    }
    if (kind === 'quark') {
      await dialog.getByRole('button', { name: '下载模式', exact: true }).click()
      await expect(page.getByRole('option')).toHaveCount(1)
      await page.getByRole('option').click()
    }
    await page.screenshot({ path: info.outputPath(`${kind}-options.png`) })
    await page.keyboard.press('Escape')
  }
})

test('single 115 directory details display CID', async ({ page }) => {
  await page.route('**/api/auth/status', r => r.fulfill({ json: { initialized: true, authenticated: true } }))
  await page.route('**/api/state', r => r.fulfill({ json: { storages: [{ id: '115', type: '115', name: '115', enabled: true }], tasks: [], settings: {}, traffic: {}, cache: {} } }))
  await page.route('**/api/files?**', r => r.fulfill({ json: [{ id: '1234567890', name: 'Movies', isDir: true, sizeKnown: true, countsKnown:true, size: 0 }] }))
  await page.goto('/files')
  await page.locator('.file-row').click({ button: 'right' })
  await page.getByRole('button', { name: '查看详情', exact: true }).click()
  await expect(page.getByRole('dialog')).toContainText('CID')
  await expect(page.getByRole('dialog')).toContainText('1234567890')
  await expect(page.locator('.file-details dt')).toHaveText(['名称','类型','大小','包含','创建时间','修改时间','CID','位置'])
  await page.evaluate(() => { Object.defineProperty(navigator,'clipboard',{configurable:true,value:{writeText:async value => {window.copiedCID=value}}}) })
  await page.getByRole('button',{name:'复制 CID',exact:true}).click()
  expect(await page.evaluate(() => window.copiedCID)).toBe('1234567890')
})

test('plugin masks load asynchronously and descriptions follow the pointer', async ({ page }, info) => {
  await page.route('**/api/auth/status', r => r.fulfill({ json: { initialized: true, authenticated: true } }))
  await page.route('**/api/state', r => r.fulfill({ json: { storages: [], tasks: [], settings: {}, traffic: {}, cache: {} } }))
  await page.route('**/api/quark-takeover', r => r.fulfill({ json: { enabled: false, bindings: [] } }))
  await page.route('**/api/plugins/*', async r => {
    await new Promise(resolve => setTimeout(resolve, 50))
    await r.fulfill({ json: { enabled: false, apiURL: 'https://api.themoviedb.org', imageURL: 'https://image.tmdb.org', apiKey: '********', language: 'zh-CN', token: 'aether' } })
  })
  await page.route('**/api/plugins/tmdb/secret', r => r.fulfill({ json: r.request().postDataJSON().metadataOnly ? { length: 32 } : { value: 'x'.repeat(32) } }))
  await page.goto('/tools')
  const sizes = await page.locator('.plugin-card').evaluateAll(cards => cards.map(el => el.getBoundingClientRect().height))
  expect(new Set(sizes).size).toBe(1)
  const description = page.locator('.plugin-card').filter({ hasText: '夸克 STRM 接管' }).locator('.plugin-description')
  await description.hover()
  await expect(page.getByRole('tooltip')).toBeVisible()
  const first = await page.getByRole('tooltip').boundingBox()
  expect(first.width).toBeLessThanOrEqual(320)
  expect(await description.evaluate(el => getComputedStyle(el).webkitLineClamp)).toBe('2')
  const box = await description.boundingBox()
  await page.mouse.move(box.x + box.width / 2 + 10, box.y + box.height / 2 + 3)
  // Near the right edge X is clamped; the tooltip should still follow Y.
  await expect.poll(async () => (await page.getByRole('tooltip').boundingBox()).y).not.toBe(first.y)
  await page.getByRole('button', { name: 'TMDB 配置', exact: true }).click()
  await expect(page.getByLabel('API 密钥', { exact: true })).toHaveValue('*'.repeat(32))
  const apiBox = await page.getByLabel('API 域名', { exact: true }).boundingBox()
  const imageBox = await page.getByLabel('图片域名', { exact: true }).boundingBox()
  expect(apiBox.y).toBe(imageBox.y)
  expect(apiBox.x + apiBox.width).toBeLessThan(imageBox.x)
  const keyBox = await page.getByLabel('API 密钥', { exact: true }).boundingBox()
  const languageBox = await page.getByRole('button', { name: '语言', exact: true }).boundingBox()
  expect(keyBox.y).toBe(languageBox.y)
  expect(keyBox.x + keyBox.width).toBeLessThan(languageBox.x)
  await page.getByRole('button', { name: '语言', exact: true }).click()
  await page.getByRole('option', { name: '英文', exact: true }).click()
  await expect(page.getByRole('button', { name: '语言', exact: true })).toHaveText('英文')
  await expect(page.getByRole('dialog').getByRole('switch')).toHaveCount(0)
  await expect(page.getByText('搜索名称', { exact: true })).toHaveCount(0)
  await page.getByRole('button', { name: '显示内容', exact: true }).click()
  await expect(page.getByLabel('API 密钥', { exact: true })).toHaveValue('x'.repeat(32))
  await page.screenshot({ path: info.outputPath('tmdb-compact.png') })
})
