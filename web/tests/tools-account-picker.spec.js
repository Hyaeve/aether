import { test, expect } from '@playwright/test'
import { captureScreenshot } from './helpers/screenshot.js'

const job = { source: '/media/incoming', storageId: 'pool', target: '', targetLabel: '', successAction: 'keep', failureAction: 'skip' }
async function fixture(page, theme = 'light', initial = {}) {
  let rapid = { manual: { ...job, ...initial }, watches: [{ ...job, id: 'watch', name: '影音监控', mode: 'watch', enabled: true }] }
  let proxy = { enabled: false, address: 'http://127.0.0.1:7890', username: 'original-user', password: '' }
  const unexpected = [], writes = [], tests = []
  await page.addInitScript(value => localStorage.setItem('aether-theme', value), theme)
  await page.route('**/api/**', route => {
    const req = route.request(), url = new URL(req.url()), path = url.pathname
    let json
    if (path === '/api/auth/status') json = { initialized: true, authenticated: true }
    else if (path === '/api/state') json = { username: 'tools-account', storages: [{ id: 'pool', type: '115', name: '家庭影音', enabled: true, config: {} }], tasks: [], settings: { sessionDays: 15 }, cache: {}, traffic: {}, logs: [] }
    else if (path === '/api/traffic') json = {}
    else if (path === '/api/strm-replace') json = { jobs: [] }
    else if (path === '/api/115-rapid') {
      if (req.method() === 'PUT') { rapid = req.postDataJSON(); writes.push({ path, value: rapid }) }
      json = rapid
    } else if (path === '/api/115-rapid/records') {
      const scoped = url.searchParams.has('watch'), current = Number(url.searchParams.get('page')) || 1
      json = { items: Array.from({ length: scoped && current === 1 ? 10 : 2 }, (_, i) => ({ id: String(i), name: `影片${current}-${i}.mkv`, size: 1024, status: 'success', pickcode: 'abcdefghijklmnopq', time: '2026-10-29T10:00:00Z' })), page: current, pages: 2, total: 12 }
    } else if (path === '/api/files') json = []
    else if (['/api/115-signin', '/api/115-folder-cleanup', '/api/115-recycle-cleanup'].includes(path)) json = { enabled: false, rules: [] }
    else if (path === '/api/115-simulcast/enabled') json = { enabled: true }
    else if (path === '/api/quark-takeover') json = { enabled: false, bindings: [] }
    else if (path === '/api/cron/validate') json = {}
    else if (path === '/api/plugins/proxy') {
      if (req.method() === 'PUT') { proxy = req.postDataJSON(); writes.push({ path, value: proxy }) }
      json = proxy
    } else if (path === '/api/plugins/proxy/test') { tests.push(req.postDataJSON()); json = { ok: true } }
    else if (path === '/api/plugins/proxy/secret') json = req.postDataJSON().metadataOnly ? { length: 0 } : { value: '' }
    else if (['/api/plugins/ai', '/api/plugins/tmdb', '/api/plugins/emby'].includes(path)) json = { enabled: false }
    else {
      unexpected.push(path)
      return route.fulfill({ status: 500, json: { error: `Unmocked API: ${path}` } })
    }
    return route.fulfill({ json })
  })
  return { unexpected, writes, tests, get rapid() { return rapid }, get proxy() { return proxy } }
}

test('known root remains selectable while listing is pending or failed', async ({ page }) => {
  const f = await fixture(page)
  let release, started
  const began = new Promise(resolve => { started = resolve })
  const gate = new Promise(resolve => { release = resolve })
  await page.route('**/api/files?**', async route => {
    started()
    await gate
    await route.fulfill({ status: 500, json: { error: '目录内容暂不可用' } })
  })
  await page.goto('/tools/115-rapid')
  await page.getByRole('button', { name: '选择秒传目标目录', exact: true }).click()
  await began
  const dialog = page.getByRole('dialog', { name: '选择存储目录', exact: true })
  const confirm = dialog.getByRole('button', { name: '选择当前目录', exact: true })
  await expect(dialog.locator('.source-list')).toHaveAttribute('aria-busy', 'true')
  await expect(confirm).toBeEnabled()
  await confirm.click()
  await expect(dialog).toHaveCount(0)
  await expect.poll(() => f.rapid.manual.target).toBe('/')
  release()
  await page.getByRole('button', { name: '选择秒传目标目录', exact: true }).click()
  await expect(dialog.getByRole('alert')).toHaveText('目录内容暂不可用')
  await expect(confirm).toBeEnabled()
  await confirm.click()
  expect(f.unexpected).toEqual([])
})

test('twenty thousand directories render virtually and a child can be confirmed before its contents load', async ({ page }) => {
  const f = await fixture(page)
  let release
  const gate = new Promise(resolve => { release = resolve })
  await page.route('**/api/files?**', async route => {
    const path = new URL(route.request().url()).searchParams.get('path')
    if (path !== '/') await gate
    return route.fulfill({ json: path === '/' ? Array.from({ length: 20000 }, (_, i) => ({ id: `dir-${i}`, name: `目录-${i}`, isDir: true })) : [] })
  })
  await page.goto('/tools/115-rapid')
  await page.getByRole('button', { name: '选择秒传目标目录', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '选择存储目录', exact: true })
  const list = dialog.locator('.virtual-directory-list')
  await expect(list).toBeVisible()
  expect(await list.locator('.source-directory').count()).toBeLessThan(35)
  await list.evaluate(el => { el.scrollTop = el.scrollHeight })
  await dialog.getByRole('button', { name: '目录-19999', exact: true }).click()
  await expect(dialog.locator('.source-list')).toHaveAttribute('aria-busy', 'true')
  await dialog.getByRole('button', { name: '选择当前目录', exact: true }).click()
  await expect.poll(() => f.rapid.manual.target).toBe('dir-19999')
  expect(f.rapid.manual.targetLabel).toBe('家庭影音 /目录-19999')
  release()
  expect(f.unexpected).toEqual([])
})

test('unverified saved directory identity stays blocked until explicitly reselected', async ({ page }) => {
  const f = await fixture(page, 'light', { target: 'saved', targetLabel: '旧目录' })
  const restored = []
  await page.route('**/api/files/directory-trail?**', route => {
    restored.push(new URL(route.request().url()).searchParams.get('path'))
    return route.fulfill({ status: 409, json: { error: '已保存目录身份已变化' } })
  })
  await page.goto('/tools/115-rapid')
  await page.getByRole('button', { name: '选择秒传目标目录', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '选择存储目录', exact: true })
  await expect(dialog.getByRole('alert')).toHaveText('已保存目录身份已变化')
  await expect(dialog.getByRole('button', { name: '选择当前目录', exact: true })).toBeDisabled()
  await dialog.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(dialog.getByRole('button', { name: '选择当前目录', exact: true })).toBeDisabled()
  await dialog.getByRole('navigation', { name: '存储目录路径' }).getByRole('button', { name: '根目录', exact: true }).click()
  await expect(dialog.locator('.source-list')).toHaveAttribute('aria-busy', 'false')
  await expect(dialog.getByRole('alert')).toHaveCount(0)
  await expect(dialog.getByRole('button', { name: '选择当前目录', exact: true })).toBeEnabled()
  await expect(dialog.getByRole('button', { name: '新建文件夹', exact: true })).toBeEnabled()
  expect(restored).toEqual(['saved', 'saved'])
  expect(f.unexpected).toEqual([])
})

for (const [width, theme] of [[1440, 'light'], [390, 'dark']]) {
  test.describe(`tools account ${width} ${theme}`, () => {
    test.use({ viewport: { width, height: 900 } })
    test('plugin titles share number spacing and descriptions require 300ms stationary hover', async ({ page }, info) => {
      const f = await fixture(page, theme)
      await page.goto('/tools')
      const cards = page.locator('.plugin-card')
      await expect(cards).toHaveCount(15)
      await expect(cards.locator('.plugin-heading strong').filter({ hasText: /^115/ })).toHaveText(['115 自动签到', '115 秒传中心', '115 同播复制', '115 文件夹清理', '115 回收站清理'])
      await expect(page.getByRole('button', { name: '代理配置', exact: true })).toHaveCount(0)
      const description = page.getByRole('button', { name: '夸克 STRM 接管', exact: true }).locator('.plugin-description')
      await description.scrollIntoViewIfNeeded()
      await expect.poll(() => page.locator('.page-content').evaluate(el => el.getAnimations({ subtree: true }).every(a => a.playState === 'finished' || a.effect?.getTiming().iterations === Infinity))).toBe(true)
      await page.clock.install(); await page.clock.pauseAt(new Date())
      await description.hover()
      await page.clock.runFor(299); await expect(page.getByRole('tooltip')).toHaveCount(0)
      await page.clock.runFor(1); await expect(page.getByRole('tooltip')).toHaveText(await description.textContent())
      const box = await description.boundingBox()
      await page.mouse.move(box.x + 10, box.y + 10)
      await expect(page.getByRole('tooltip')).toHaveCount(0)
      await page.clock.runFor(299); await expect(page.getByRole('tooltip')).toHaveCount(0)
      await page.clock.runFor(1); await expect(page.getByRole('tooltip')).toBeVisible()
      await page.clock.resume()
      await captureScreenshot(page, { path: info.outputPath(`tools-${width}-${theme}.png`) })
      await page.keyboard.press('Escape')
      await page.getByRole('button', { name: '115 自动签到', exact: true }).click()
      await expect(page.getByRole('dialog', { name: '115 自动签到', exact: true })).toBeVisible()
      await page.keyboard.press('Escape')
      await page.getByRole('button', { name: '115 秒传中心', exact: true }).click()
      await expect(page).toHaveURL(/\/tools\/115-rapid$/)
      expect(f.unexpected).toEqual([])
    })

    test('account and proxy panels have headings and proxy saves without submitting account credentials', async ({ page }, info) => {
      const f = await fixture(page, theme)
      await page.goto('/settings/account')
      await expect(page.getByRole('heading', { name: '账户配置', exact: true })).toBeVisible()
      await expect(page.locator('.account-panel-title svg')).toHaveClass(/lucide-user-round/)
      const proxy = page.getByRole('form', { name: '代理配置', exact: true })
      await expect(proxy.getByRole('heading', { name: '代理配置', exact: true })).toBeVisible()
      await expect(proxy.locator('header svg')).toHaveClass(/lucide-network/)
      await expect(proxy.getByRole('textbox', { name: '代理地址', exact: true })).toHaveValue('http://127.0.0.1:7890')
      await proxy.getByRole('switch', { name: '启用代理', exact: true }).check()
      await proxy.getByRole('textbox', { name: '用户名（可选）', exact: true }).fill('proxy-user')
      await proxy.getByLabel('密码（可选）', { exact: true }).fill('proxy-pass')
      await proxy.getByRole('button', { name: '保存配置', exact: true }).click()
      await expect.poll(() => f.proxy.enabled).toBe(true)
      expect(f.proxy.username).toBe('proxy-user')
      expect(f.proxy.password).toBe('proxy-pass')
      expect(f.writes.map(value => value.path)).toEqual(['/api/plugins/proxy'])
      await proxy.getByRole('button', { name: '测试连接', exact: true }).click()
      await expect.poll(() => f.tests.length).toBe(1)
      expect(f.tests[0].config).toMatchObject({ address: 'http://127.0.0.1:7890', username: 'proxy-user', password: 'proxy-pass' })
      expect(await page.evaluate(() => document.querySelectorAll('form form').length)).toBe(0)
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
      await captureScreenshot(page, { path: info.outputPath(`account-proxy-${width}-${theme}.png`) })
      expect(f.unexpected).toEqual([])
    })

    test('failure hints align immediately before the selection check and monitor records use a wider trackless dialog', async ({ page }, info) => {
      const f = await fixture(page, theme)
      await page.goto('/tools/115-rapid')
      await page.getByRole('button', { name: '添加监控', exact: true }).click()
      const editor = page.getByRole('dialog', { name: '添加监控', exact: true })
      await editor.getByRole('button', { name: '秒传失败处理', exact: true }).click()
      const options = editor.getByRole('option')
      await expect(options).toHaveCount(3)
      await expect(editor.locator('.rounded-select-popup')).toHaveCSS('transform', 'none')
      const geometry = await options.evaluateAll(els => els.map(el => {
        const icon = el.querySelector('.failure-option svg').getBoundingClientRect(), check = el.querySelector(':scope > svg')?.getBoundingClientRect()
        return { x: icon.x, right: icon.right, check: check?.left, optionRight: el.getBoundingClientRect().right }
      }))
      expect(Math.max(...geometry.map(r => r.x)) - Math.min(...geometry.map(r => r.x))).toBeLessThan(1)
      expect(geometry[0].right).toBeLessThan(geometry[0].check)
      expect(geometry[0].check - geometry[0].right).toBe(16)
      await page.keyboard.press('Escape'); await page.keyboard.press('Escape')
      await page.getByRole('button', { name: '查看 影音监控 秒传记录', exact: true }).click()
      const dialog = page.getByRole('dialog', { name: '影音监控 · 秒传记录', exact: true })
      await expect(dialog.locator('.rapid115-record')).toHaveCount(10)
      await expect(dialog).toHaveCSS('width', `${Math.min(860, width - 32)}px`)
      await expect(dialog).toHaveCSS('overflow', 'hidden')
      await expect(dialog.locator('.watch-record-dialog')).toHaveCSS('scrollbar-width', 'none')
      await expect(dialog.locator('.rapid115-record-frame')).toHaveCSS('scrollbar-width', 'none')
      expect(await dialog.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
      await dialog.locator('.rapid115-record-frame').evaluate(el => { el.scrollTop = el.scrollHeight })
      await expect(dialog.getByRole('button', { name: '最后一页', exact: true })).toBeInViewport()
      await dialog.getByRole('button', { name: '最后一页', exact: true }).click()
      await expect(dialog.locator('.rapid115-record')).toHaveCount(2)
      await expect(dialog.locator('.rapid115-record').first()).toBeInViewport()
      await captureScreenshot(page, { path: info.outputPath(`monitor-history-${width}-${theme}.png`) })
      expect(f.unexpected).toEqual([])
    })
  })
}
