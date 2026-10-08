import assert from 'node:assert/strict'
import { mkdtempSync } from 'node:fs'
import { tmpdir } from 'node:os'
import path from 'node:path'
import { createServer } from 'vite'
import { chromium, expect } from '@playwright/test'

// All API responses are mocked; this fixture never contacts a running service.
const output = mkdtempSync(path.join(tmpdir(), 'aether-directory-create-'))
const server = await createServer({ server: { host: '127.0.0.1', port: 0, proxy: {} } })
let browser
try {
  await server.listen()
  browser = await chromium.launch()
  for (const width of [1440, 390]) {
    const page = await browser.newPage({ viewport: { width, height: 900 } })
    const errors = [], writes = []
    page.on('pageerror', e => errors.push(e.message))
    let created = false, reject = true
    await page.route('**/api/**', route => {
      const req = route.request(), url = new URL(req.url())
      if (url.pathname === '/api/auth/status') return route.fulfill({ json: { initialized: true, authenticated: true } })
      if (url.pathname === '/api/state') return route.fulfill({ json: { username: 'test', storages: [{ id: 'local', type: 'local', name: '测试存储', enabled: true, config: {} }], tasks: [], settings: {}, cache: {}, traffic: {}, strmRoot: '/media' } })
      if (req.method() === 'POST') {
        writes.push({ path: url.pathname, body: req.postDataJSON() })
        if (reject) { reject = false; return route.fulfill({ status: 409, json: { error: '同名文件或目录已存在' } }) }
        created = true
        return route.fulfill({ json: { path: '/media/新文件夹', processed: 1 } })
      }
      if (url.pathname === '/api/files') return route.fulfill({ json: created ? [{ id: '/new', name: '新文件夹', isDir: true }] : [] })
      if (url.pathname === '/api/local-directories') return route.fulfill({ json: { path: '/media', items: created ? [{ name: '新文件夹', path: '/media/新文件夹' }] : [] } })
      return route.fulfill({ json: {} })
    })
    await page.goto(server.resolvedUrls.local[0] + 'tasks/strm')
    await page.getByRole('button', { name: '添加任务', exact: true }).click()
    for (const kind of ['source', 'local']) {
      created = false; reject = true
      await page.getByRole('button', { name: kind === 'source' ? '选择目录' : '选择生成目录', exact: true }).click()
      const dialog = page.getByRole('dialog', { name: kind === 'source' ? '选择存储目录' : '选择容器目录', exact: true })
      if (kind === 'source') await dialog.locator('.source-accounts button').click()
      const add = dialog.getByRole('button', { name: '新建文件夹', exact: true })
      await expect(add).toBeEnabled()
      await add.click()
      const input = dialog.getByLabel('文件夹名称', { exact: true })
      await expect(input).toBeFocused()
      await input.fill('新文件夹')
      await dialog.getByRole('button', { name: '确认创建', exact: true }).click()
      await expect(dialog.getByRole('alert')).toHaveText('同名文件或目录已存在')
      await expect(input).toHaveValue('新文件夹')
      assert.equal(await dialog.evaluate(el => el.scrollWidth <= el.clientWidth), true)
      const box = await input.boundingBox()
      assert.ok(box.x >= 0 && box.x + box.width <= width)
      await page.screenshot({ path: path.join(output, `${kind}-${width}.png`) })
      await dialog.getByRole('button', { name: '确认创建', exact: true }).click()
      await expect(dialog.getByRole('button', { name: '新文件夹', exact: true })).toBeVisible()
      const last = writes.at(-1)
      if (kind === 'source') assert.deepEqual(last, { path: '/api/files/action', body: { storageId: 'local', source: '/', action: 'mkdir', ids: [], name: '新文件夹' } })
      else assert.deepEqual(last, { path: '/api/local-directories', body: { path: '/media', name: '新文件夹' } })
      await dialog.getByRole('button', { name: '选择当前目录', exact: true }).click()
    }
    assert.deepEqual(errors, [])
    await page.close()
  }
  console.log(`Directory picker checks passed. Screenshots: ${output}`)
} finally {
  await browser?.close()
  await server.close()
}
