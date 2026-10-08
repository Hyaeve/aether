import { after, before, test } from 'node:test'
import assert from 'node:assert/strict'
import { mkdtemp } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { createServer } from 'vite'
import { chromium, expect } from '@playwright/test'

let server, browser, baseURL, output
before(async () => {
  server = await createServer({ root: fileURLToPath(new URL('..', import.meta.url)), server: { host: '127.0.0.1', port: 0, proxy: {} } })
  await server.listen()
  baseURL = `http://127.0.0.1:${server.httpServer.address().port}`
  browser = await chromium.launch()
  output = await mkdtemp(path.join(tmpdir(), 'aether-task-ui-'))
  console.log(`Screenshots: ${output}`)
})
after(async () => { await browser?.close(); await server?.close() })

async function fixture(t, tasks = []) {
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } })
  t.after(() => page.close())
  const storages = [
    { id: 'local', type: 'local', name: '本地源', enabled: true, config: {} },
    { id: 'pan', type: '115', name: '115 绑定', enabled: true, config: {} },
    { id: 'disabled', type: '115', name: '停用 115', enabled: false, config: {} },
    { id: 'quark', type: 'quark', name: '夸克', enabled: true, config: {} }
  ]
  const data = { storages, tasks, settings: {}, cache: { hits: 3, misses: 1, entries: 4 }, traffic: {}, links: [] }
  const writes = []
  await page.route('**/api/**', route => {
    const request = route.request(), url = new URL(request.url())
    let json = {}
    if (url.pathname === '/api/auth/status') json = { initialized: true, authenticated: true }
    else if (url.pathname === '/api/state') json = data
    else if (url.pathname === '/api/files') json = []
    else if (url.pathname === '/api/links') json = [{ id: 'link', name: '影院', type: 'emby', enabled: true, address: 'https://example.com' }]
    else if (url.pathname === '/api/tasks/reorder') {
      const { id, target } = request.postDataJSON()
      const from = tasks.findIndex(task => task.id === id), to = tasks.findIndex(task => task.id === target)
      tasks.splice(to, 0, tasks.splice(from, 1)[0])
    } else if (url.pathname.startsWith('/api/tasks') && request.method() !== 'GET') writes.push(request.postDataJSON())
    return route.fulfill({ json })
  })
  return { page, data, writes }
}

test('extension order, retained defaults, icon-only colors and responsive form', async t => {
  const { page } = await fixture(t)
  await page.goto(`${baseURL}/tasks/strm`)
  await page.getByRole('button', { name: '添加任务', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog.locator('.form-grid').first().locator(':scope > :nth-last-child(2) input')).toHaveAttribute('id', 'task-media-extensions')
  await expect(dialog.locator('.form-grid').first().locator(':scope > :last-child input')).toHaveAttribute('id', 'task-metadata-extensions')
  for (const name of ['视频扩展名', '音频扩展名', '图片扩展名', '数据扩展名']) {
    await dialog.getByRole('button', { name, exact: true }).click()
    await expect(dialog.getByRole('button', { name, exact: true })).toHaveAttribute('aria-pressed', 'true')
  }
  await dialog.getByRole('button', { name: '更多选项' }).click()
  const heading = await dialog.locator('.extension-heading').first().boundingBox()
  const presets = await dialog.locator('.extension-heading > span').first().boundingBox()
  assert.ok(Math.abs(heading.x + heading.width - presets.x - presets.width) < 2)
  await expect(dialog.locator('.more-options > :first-child input')).toHaveValue('iso')
  await dialog.getByRole('button', { name: '保留音频扩展名' }).click()
  await expect(dialog.getByLabel('保留扩展名', { exact: true })).toHaveValue(/iso;mp3;/)
  await dialog.getByRole('button', { name: '保留音频扩展名' }).click()
  await expect(dialog.getByLabel('保留扩展名', { exact: true })).toHaveValue('iso')
  for (const theme of ['light', 'dark']) {
    await page.evaluate(theme => document.documentElement.dataset.theme = theme, theme)
    const styles = await dialog.locator('.extension-preset[aria-pressed=true]').evaluateAll(elements => elements.map(el => ({ color: getComputedStyle(el).color, background: getComputedStyle(el).backgroundColor })))
    assert.equal(new Set(styles.map(s => s.color)).size, 4)
    assert.ok(styles.every(s => s.background === 'rgba(0, 0, 0, 0)'))
    await expect(dialog.getByRole('button', { name: '视频扩展名', exact: true })).toHaveCSS('color', theme === 'dark' ? 'rgb(208, 213, 223)' : 'rgb(37, 42, 52)')
    await page.screenshot({ path: path.join(output, `extensions-${theme}.png`) })
  }
  await page.setViewportSize({ width: 390, height: 844 })
  assert.ok(await dialog.evaluate(el => el.scrollWidth <= el.clientWidth))
  await page.screenshot({ path: path.join(output, 'extensions-mobile.png') })
})

test('ED2K binding requires enabled 115 selection and keeps a local source', async t => {
  const { page, writes } = await fixture(t)
  await page.goto(`${baseURL}/tasks/ed2k`)
  await page.getByRole('button', { name: '添加任务', exact: true }).click()
  await page.getByLabel('任务名称').fill('ED2K test')
  const name = await page.getByLabel('任务名称').boundingBox(), binding = await page.getByRole('button', { name: '绑定存储', exact: true }).boundingBox()
  assert.ok(Math.abs(name.y - binding.y) < 3)
  await page.getByRole('button', { name: '选择目录', exact: true }).click()
  await expect(page.locator('.source-accounts button')).toHaveCount(1)
  await page.locator('.source-accounts button').click()
  await page.getByRole('button', { name: '选择当前目录', exact: true }).click()
  await expect(page.getByRole('button', { name: '保存任务', exact: true })).toBeDisabled()
  await page.getByRole('button', { name: '绑定存储', exact: true }).click()
  await expect(page.getByRole('option')).toHaveCount(1)
  await page.getByRole('option', { name: '115 绑定', exact: true }).click()
  await page.getByRole('button', { name: '保存任务', exact: true }).click()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  assert.equal(writes[0].ed2kBindingId, 'pan')
  assert.equal(writes[0].storageId, 'local')
  assert.equal(writes[0].retainedExtensions, 'iso')
})

test('task buttons have no title, menu omits ordering, keyboard still reorders', async t => {
  const tasks = [1, 2].map(id => ({ id: String(id), name: `任务 ${id}`, kind: 'strm', storageId: 'local', enabled: true, processed: 0 }))
  const { page } = await fixture(t, tasks)
  await page.goto(`${baseURL}/tasks/strm`)
  const rows = page.locator('.task-row-card')
  await expect(rows).toHaveCount(2)
  await expect(rows.locator('button[title]')).toHaveCount(0)
  await rows.first().getByRole('button', { name: '任务操作 任务 1' }).click()
  await expect(page.locator('.task-menu button')).toHaveCount(2)
  await expect(page.getByRole('button', { name: /^(上移|下移)$/ })).toHaveCount(0)
  await rows.first().focus()
  await page.keyboard.press('Alt+ArrowDown')
  await expect(rows.first()).toHaveAttribute('aria-label', '任务 2')
})

test('compact cache shows idle and indeterminate running states on desktop and mobile', async t => {
  const { page, data } = await fixture(t)
  await page.goto(`${baseURL}/tasks/cache`)
  await expect(page.locator('.cache-progress-status')).toHaveText('空闲')
  await expect(page.locator('.cache-overview')).not.toContainText('估算')
  await expect(page.locator('.cache-metrics svg').nth(3)).toHaveClass(/circle-dashed/i)
  await expect(page.locator('.cache-metrics svg').nth(4)).toHaveClass(/log-out/i)
  data.tasks.push({ id: 'cache', kind: 'cache', name: '缓存扫描', storageId: 'local', status: 'running', processed: 12 })
  await page.reload()
  await expect(page.locator('.cache-progress-status')).toHaveText('扫描中')
  await expect(page.locator('.cache-progress progress')).not.toHaveAttribute('value')
  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: 900 })
    assert.ok((await page.locator('.cache-progress progress').boundingBox()).height >= 20)
    assert.ok(await page.locator('.cache-overview').evaluate(el => el.scrollWidth <= el.clientWidth))
    await page.screenshot({ path: path.join(output, `cache-${width}.png`) })
  }
})

test('storage and links arm at 160ms, not before', async t => {
  const { page } = await fixture(t)
  for (const [url, selector] of [['/storage', '.storage-card'], ['/links', '.link-card']]) {
    await page.goto(`${baseURL}${url}`)
    const card = page.locator(selector).first()
    await expect(card).toBeVisible()
    await page.clock.install()
    await page.clock.pauseAt(new Date(Date.now() + 1000))
    await card.dispatchEvent('pointerdown', { button: 0, pointerId: 1 })
    await page.clock.runFor(159)
    await expect(card).not.toHaveClass(/drag-armed/)
    await page.clock.runFor(1)
    await expect(card).toHaveClass(/drag-armed/)
    await card.dispatchEvent('pointerup', { pointerId: 1 })
  }
})
