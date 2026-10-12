import { test, expect } from '@playwright/test'
import { captureScreenshot } from './helpers/screenshot.js'

const initialPool = () => ({ mobile: { storageIds: [], retentionHours: 12 }, tianyi: { storageIds: [], retentionHours: 12 }, preferred: 'mobile', rotationMinutes: 720, configured: true })
const account = (id, name, type, config = { mode: 'native' }, enabled = true) => ({ id, name, type, config, enabled })

async function workspace(page, tasks = []) {
  const snapshot = {
    username: 'cas-pool-ui', tasks, settings: {}, cache: {}, traffic: {}, logs: [], strmRoot: '/data/strm',
    storages: [
      account('m1', '移动影音一', 'mobile'), account('m2', '移动影音二', 'mobile'),
      account('t1', '天翼影音一', 'tianyi'), account('t2', '天翼旧账号', 'tianyi', { username: 'redacted', password: '********' }),
      account('m-gateway', '移动协议账号', 'mobile', { mode: 'openlist' }), account('m-legacy', '移动非原生旧账号', 'mobile', {}),
      account('t-gateway', '天翼协议账号', 'tianyi', { mode: 'openlist' }), account('m-off', '移动停用账号', 'mobile', { mode: 'native' }, false),
      account('quark', '夸克影音', 'quark', {}), account('local', '本地影音', 'local', {})
    ]
  }
  const saved = []
  let pool = initialPool()
  await page.route('**/api/**', route => {
    const request = route.request(), pathname = new URL(request.url()).pathname
    if (pathname === '/api/cas/pool') {
      if (request.method() === 'PUT') { saved.push(request.postDataJSON()); pool = { ...request.postDataJSON(), configured: true } }
      return route.fulfill({ json: pool })
    }
    const json = pathname === '/api/cas/status' ? { items: [] } : pathname === '/api/auth/status' ? { initialized: true, authenticated: true }
      : pathname === '/api/state' ? snapshot : pathname === '/api/files' ? [] : {}
    return route.fulfill({ json })
  })
  return { snapshot, saved }
}
const poolRegion = page => page.getByRole('region', { name: 'CAS 存储绑定池', exact: true })
async function openPool(page) {
  await page.getByRole('button', { name: '打开存储绑定池', exact: true }).click()
  const pool = poolRegion(page)
  await expect(pool).toBeVisible()
  await expect(page.getByRole('button', { name: '保存设置', exact: true })).toBeEnabled()
  await expect(page).toHaveURL(/\/tasks\/cas\/pool$/)
  await expect(page.getByRole('region', { name: 'CAS 生成统计', exact: true })).toHaveCount(0)
  await expect.poll(() => pool.evaluate(el => el.getAnimations().every(animation => animation.playState === 'finished'))).toBe(true)
  return pool
}
async function advanced(pool) { await expect(pool.getByRole('region', { name: '高级设置', exact: true })).toBeVisible() }

test('CAS success rate uses executed terminal tasks, true generation counters and live indeterminate progress', async ({ page }) => {
  const lastRun = '2026-10-11T08:00:00Z'
  const tasks = [
    { id: 's1', status: 'success', casGenerated: 8 }, { id: 's2', status: 'success', casGenerated: 2 },
    { id: 'e', status: 'error', casFailed: 3 }, { id: 'c', status: 'cancelled', casGenerated: 1 },
    { id: 'i', status: 'interrupted', casGenerated: 2, casFailed: 2 },
    { id: 'r', name: '实时 CAS 扫描', status: 'running', processed: 3, casGenerated: 4, casFailed: 1, message: '正在生成 · Movies' },
    { id: 'idle', status: 'idle' }, { id: 'queued', status: 'queued' },
    { id: 'never', status: 'success', lastRun: '0001-01-01T00:00:00Z' },
    { id: 'bad-date', status: 'success', lastRun: 'invalid-date' }, { id: 'no-date', status: 'success', lastRun: undefined },
    { id: 'other', kind: 'strm', status: 'success', casGenerated: 100, casFailed: 100 }
  ].map(task => ({ kind: 'cas', name: task.id, lastRun, ...task }))
  const { snapshot } = await workspace(page, tasks)
  await page.goto('/tasks/cas')
  const overview = page.getByRole('region', { name: 'CAS 生成统计', exact: true })
  await expect(overview.getByRole('img', { name: '成功率 40%', exact: true })).toBeVisible()
  await expect(overview.locator('.cas-metrics dd')).toHaveText(['17', '6'])
  await expect(overview.locator('.cas-progress-count')).toHaveText('已处理 3 项 · 已生成 4 · 异常 1')
  const progress = overview.getByRole('progressbar', { name: '实时 CAS 扫描：扫描中，已处理 3 项', exact: true })
  await expect(progress).toHaveCSS('height', '6px')
  await expect(progress).not.toHaveAttribute('aria-valuenow')
  await expect(progress).toHaveAttribute('aria-valuetext', '扫描中，已处理 3 项')
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await expect(overview.locator('.cas-progress-motion')).toHaveCSS('animation-name', 'none')
  snapshot.tasks.find(t => t.id === 'r').casGenerated = 6
  snapshot.tasks.find(t => t.id === 'r').processed = 7
  await expect(overview.locator('.cas-progress-count')).toHaveText('已处理 7 项 · 已生成 6 · 异常 1', { timeout: 10000 })
  await expect(overview.locator('.cas-metrics dd')).toHaveText(['19', '6'])
  snapshot.tasks.find(t => t.id === 'r').status = 'success'
  await expect(overview.getByRole('img', { name: '成功率 50%', exact: true })).toBeVisible({ timeout: 10000 })
  await expect(overview.getByLabel('暂无执行中的 CAS 任务')).toBeVisible()
})

test('CAS pool requires the first native bindings and persists additional accounts and provider settings', async ({ page }) => {
  const { saved } = await workspace(page)
  await page.goto('/tasks/cas')
  const pool = await openPool(page)
  await expect(pool.locator('.cas-binding-toggle')).toHaveCount(4)
  for(const name of ['移动影音一','天翼影音一']){await expect(pool.getByRole('button',{name:'绑定 '+name,exact:true})).toHaveAttribute('aria-pressed','true');await expect(pool.getByRole('button',{name:'绑定 '+name,exact:true})).toBeDisabled()}
  for (const name of ['移动协议账号', '移动非原生旧账号', '天翼协议账号', '移动停用账号', '夸克影音', '本地影音']) await expect(pool).not.toContainText(name)
  await pool.getByRole('button', { name: '绑定 移动影音二', exact: true }).click()
  await pool.getByRole('button', { name: '绑定 天翼旧账号', exact: true }).click()
  await advanced(pool)
  await expect(pool.getByRole('button', { name: '优先秒传', exact: true })).toContainText('移动云盘')
  await expect(pool.getByRole('spinbutton', { name: '轮换时间', exact: true })).toHaveValue('12')
  for (const name of ['移动云盘保留时间', '天翼云盘保留时间']) await expect(pool.getByRole('spinbutton', { name, exact: true })).toHaveValue('12')
  await pool.getByRole('button', { name: '优先秒传', exact: true }).click()
  await pool.getByRole('option', { name: '天翼云盘', exact: true }).click()
  await pool.getByRole('spinbutton', { name: '轮换时间', exact: true }).fill('15')
  await pool.getByRole('spinbutton', { name: '移动云盘保留时间', exact: true }).fill('24')
  await pool.getByRole('spinbutton', { name: '天翼云盘保留时间', exact: true }).fill('48')
  await page.getByRole('button', { name: '保存设置', exact: true }).click()
  await expect(pool.getByRole('status')).toHaveText('存储绑定池已保存')
  expect(saved).toEqual([{ mobile: { storageIds: ['m1', 'm2'], retentionHours: 24 }, tianyi: { storageIds: ['t1','t2'], retentionHours: 48 }, preferred: 'tianyi', rotationMinutes: 900 }])
  await page.getByRole('button', { name: '返回 CAS 任务', exact: true }).click()
  const restored = await openPool(page)
  await expect(restored.getByRole('button', { name: '绑定 移动影音二', exact: true })).toHaveAttribute('aria-pressed', 'true')
  await expect(restored.getByRole('button', { name: '绑定 天翼旧账号', exact: true })).toHaveAttribute('aria-pressed', 'true')
  await advanced(restored)
  await expect(restored.getByRole('spinbutton', { name: '天翼云盘保留时间', exact: true })).toHaveValue('48')
})

test('CAS numeric limits reject blank, zero, fractions and above-max values and save both endpoints without adding bindings', async ({ page }) => {
  const { saved } = await workspace(page)
  await page.goto('/tasks/cas')
  const pool = await openPool(page)
  await advanced(pool)
  for (const [name, min, max, fraction] of [['轮换时间', 1 / 60, 24, '.025'], ['移动云盘保留时间', 1, 8760, '1.5'], ['天翼云盘保留时间', 1, 8760, '1.5']]) {
    const input = pool.getByRole('spinbutton', { name, exact: true })
    await expect(input).toHaveAttribute('min', String(min))
    await expect(input).toHaveAttribute('max', String(max))
    for (const value of ['', '0', fraction, String(max + 1)]) {
      await input.fill(value)
      await page.getByRole('button', { name: '保存设置', exact: true }).click()
      expect(await input.evaluate(el => el.validity.valid)).toBe(false)
      expect(saved).toHaveLength(0)
    }
    await input.fill('1')
  }
  await page.getByRole('button', { name: '保存设置', exact: true }).click()
  await expect(pool.getByRole('status')).toHaveText('存储绑定池已保存')
  expect(saved[0]).toEqual({ mobile: { storageIds: ['m1'], retentionHours: 1 }, tianyi: { storageIds: ['t1'], retentionHours: 1 }, preferred: 'mobile', rotationMinutes: 60 })
  await pool.getByRole('spinbutton', { name: '轮换时间', exact: true }).fill('24')
  await pool.getByRole('spinbutton', { name: '移动云盘保留时间', exact: true }).fill('8760')
  await pool.getByRole('spinbutton', { name: '天翼云盘保留时间', exact: true }).fill('8760')
  await page.getByRole('button', { name: '保存设置', exact: true }).click()
  await expect(pool.getByRole('status')).toHaveText('存储绑定池已保存')
  expect(saved[1]).toMatchObject({ rotationMinutes: 1440, mobile: { storageIds: ['m1'], retentionHours: 8760 }, tianyi: { storageIds: ['t1'], retentionHours: 8760 } })
  await pool.getByRole('spinbutton', { name: '轮换时间', exact: true }).fill(String(1 / 60))
  await page.getByRole('button', { name: '保存设置', exact: true }).click()
  await expect(pool.getByRole('status')).toHaveText('存储绑定池已保存')
  expect(saved[2].rotationMinutes).toBe(1)
})

test('CAS full page adapts to themes and mobile with flat advanced options, a single back arrow and keyboard return', async ({ page }, testInfo) => {
  await workspace(page)
  await page.goto('/tasks/cas')
  await expect(page.getByRole('img', { name: '成功率暂无记录', exact: true })).toBeVisible()
  for (const [width, height] of [[1440, 1000], [390, 844], [375, 500], [844, 390]]) {
    await page.setViewportSize({ width, height })
    for (const theme of ['light', 'dark']) {
      await page.evaluate(value => document.documentElement.dataset.theme = value, theme)
      const pool = await openPool(page)
      const back = page.getByRole('button', { name: '返回 CAS 任务', exact: true })
      await expect(page.locator('.cas-pool-trigger')).toHaveCount(0)
      await expect(back).toHaveCount(1)
      await expect(back).toBeFocused()
      await expect(back.locator('svg')).toHaveClass(/lucide-chevron-right/)
      await expect(pool.locator('.cas-pool-list-frame').first()).toHaveCSS('border-radius', '8px')
      await expect(pool.locator('.cas-pool-list-frame').first()).toHaveCSS('border-top-width', '1px')
      await expect(pool.locator('.provider-icon').first()).toHaveCSS('background-color', 'rgba(0, 0, 0, 0)')
      await expect(pool.locator('.cas-pool-list .cas-pool-account')).toHaveCount(6)
      await expect(pool.locator('details')).toHaveCount(0)
      await advanced(pool)
      await pool.getByRole('button', { name: '优先秒传', exact: true }).click()
      await page.keyboard.press('Escape')
      await expect(pool).toBeVisible()
      await expect(pool.getByRole('listbox')).toHaveCount(0)
      expect(await pool.evaluate(el => el.scrollWidth <= el.clientWidth && document.documentElement.scrollWidth <= innerWidth)).toBe(true)
      if (width === 1440) expect(await pool.locator('.cas-pool-group').evaluateAll(groups => {
        const [a,b] = groups.map(el => el.getBoundingClientRect())
        return Math.abs(a.top-b.top) < 1 && b.left >= a.right
      })).toBe(true)
      await captureScreenshot(page, { path: testInfo.outputPath(`cas-pool-${theme}-${width}x${height}.png`), fullPage: true })
      await back.focus()
      await page.keyboard.press('Escape')
      await expect(pool).toHaveCount(0)
      await expect(page).toHaveURL(/\/tasks\/cas$/)
      await expect(page.getByRole('button', { name: '打开存储绑定池', exact: true })).toBeVisible()
    }
  }
  await page.emulateMedia({ reducedMotion: 'reduce' })
  const pool = await openPool(page)
  await expect(pool.locator('.cas-pool-account').first()).toHaveCSS('transition-duration', '0s')
  await page.getByRole('button', { name: '返回 CAS 任务', exact: true }).click()
  await expect(pool).toHaveCount(0)
})

test('CAS load and save errors retain edits, support retry, and always allow closing', async ({ page }) => {
  await workspace(page)
  let reads = 0, writes = 0, submitted
  await page.route('**/api/cas/pool', route => {
    if (route.request().method() === 'GET') return route.fulfill(++reads === 1 ? { status: 503, json: { error: '绑定池暂时不可用' } } : { json: initialPool() })
    submitted = route.request().postDataJSON()
    return route.fulfill(++writes === 1 ? { status: 400, json: { error: '保存绑定池失败' } } : { json: submitted })
  })
  await page.goto('/tasks/cas')
  await page.getByRole('button', { name: '打开存储绑定池', exact: true }).click()
  const pool = poolRegion(page)
  await expect(pool.getByRole('alert')).toHaveText('绑定池暂时不可用')
  await expect(page.getByRole('button', { name: '保存设置', exact: true })).toBeDisabled()
  await expect(page.getByRole('button', { name: '返回 CAS 任务', exact: true })).toBeEnabled()
  await pool.getByRole('button', { name: '重新加载', exact: true }).click()
  await expect(page.getByRole('button', { name: '保存设置', exact: true })).toBeEnabled()
  await pool.getByRole('button', { name: '绑定 移动影音二', exact: true }).click()
  await page.getByRole('button', { name: '保存设置', exact: true }).click()
  await expect(pool.getByRole('alert')).toHaveText('保存绑定池失败')
  await expect(pool.getByRole('button', { name: '绑定 移动影音二', exact: true })).toHaveAttribute('aria-pressed', 'true')
  await page.getByRole('button', { name: '保存设置', exact: true }).click()
  await expect(pool.getByRole('status')).toHaveText('存储绑定池已保存')
  expect(submitted.mobile.storageIds).toEqual(['m1','m2'])
})

for (const method of ['GET', 'PUT']) {
  test(`CAS closing during ${method} isolates a late response from the reopened pool`, async ({ page }) => {
    await workspace(page)
    let release, finish, pending = false, held = false
    const gate = new Promise(resolve => release = resolve), settled = new Promise(resolve => finish = resolve)
    await page.route('**/api/cas/pool', async route => {
      if (!held && route.request().method() === method) {
        held = true; pending = true
        await gate
        const stale = initialPool(); stale.mobile.storageIds = ['m1']; stale.rotationMinutes = 99
        await route.fulfill({ json: stale }).catch(() => {})
        finish()
      } else await route.fulfill({ json: initialPool() })
    })
    try {
      await page.goto('/tasks/cas')
      await page.getByRole('button', { name: '打开存储绑定池', exact: true }).click()
      const pool = poolRegion(page)
      if (method === 'PUT') {
        await expect(page.getByRole('button', { name: '保存设置', exact: true })).toBeEnabled()
        await pool.getByRole('button', { name: '绑定 移动影音二', exact: true }).click()
        await page.getByRole('button', { name: '保存设置', exact: true }).click()
      }
      await expect.poll(() => pending).toBe(true)
      await expect(page.getByRole('button', { name: '返回 CAS 任务', exact: true })).toBeEnabled()
      await page.getByRole('button', { name: '返回 CAS 任务', exact: true }).click()
      await expect(pool).toHaveCount(0)
      const reopened = await openPool(page)
      release(); await settled
      await expect(reopened.getByRole('button', { name: '绑定 移动影音二', exact: true })).toHaveAttribute('aria-pressed', 'false')
      await expect(reopened.getByRole('status')).toHaveCount(0)
      await expect(reopened.getByRole('alert')).toHaveCount(0)
      await advanced(reopened)
      await expect(reopened.getByRole('spinbutton', { name: '轮换时间', exact: true })).toHaveValue('12')
      await expect(page.locator('.toast').filter({ hasText: '存储绑定池已保存' })).toHaveCount(0)
    } finally { release() }
  })
}

test('CAS unavailable saved bindings remain visible until explicitly removed', async ({ page }) => {
  const { saved } = await workspace(page)
  const existing = initialPool(); existing.mobile.storageIds = ['m-off', 'deleted-account']
  await page.route('**/api/cas/pool', route => {
    if (route.request().method() === 'PUT') saved.push(route.request().postDataJSON())
    return route.fulfill({ json: route.request().method() === 'PUT' ? saved.at(-1) : existing })
  })
  await page.goto('/tasks/cas')
  const pool = await openPool(page)
  await expect(pool).toContainText('移动停用账号 · 不可用')
  await expect(pool).toContainText('deleted-account · 不可用')
  await page.getByRole('button', { name: '保存设置', exact: true }).click()
  await expect(pool.getByRole('alert')).toHaveText('请移除移动云盘中不可用的绑定')
  expect(saved).toHaveLength(0)
  await pool.getByRole('button', { name: '移除不可用绑定 移动停用账号', exact: true }).click()
  await pool.getByRole('button', { name: '移除不可用绑定 deleted-account', exact: true }).click()
  await page.getByRole('button', { name: '保存设置', exact: true }).click()
  await expect(pool.getByRole('status')).toHaveText('存储绑定池已保存')
  expect(saved[0].mobile.storageIds).toEqual(['m1'])
})

test('CAS first ordered native account cannot be disabled and survives reopening', async ({ page }) => {
  const { snapshot, saved } = await workspace(page)
  snapshot.storages = [snapshot.storages[1], snapshot.storages[0], ...snapshot.storages.slice(2)]
  let response = { ...initialPool(), configured: false }
  await page.route('**/api/cas/pool', route => {
    if (route.request().method() === 'PUT') { saved.push(route.request().postDataJSON()); response = { ...saved.at(-1), configured: true } }
    return route.fulfill({ json: response })
  })
  await page.goto('/tasks/cas')
  const pool = await openPool(page)
  await expect(pool.getByRole('button', { name: '绑定 移动影音二', exact: true })).toHaveAttribute('aria-pressed', 'true')
  await expect(pool.getByRole('button', { name: '绑定 移动影音一', exact: true })).toHaveAttribute('aria-pressed', 'false')
  await expect(pool.getByRole('button', { name: '绑定 天翼影音一', exact: true })).toHaveAttribute('aria-pressed', 'true')
  await expect(pool.getByRole('button', { name: '绑定 天翼旧账号', exact: true })).toHaveAttribute('aria-pressed', 'false')
  expect(saved).toHaveLength(0)
  await expect(pool.getByRole('button', { name: '绑定 移动影音二', exact: true })).toBeDisabled()
  await expect(pool.getByRole('button', { name: '绑定 天翼影音一', exact: true })).toBeDisabled()
  await page.getByRole('button', { name: '保存设置', exact: true }).click()
  await expect(pool.getByRole('status')).toHaveText('存储绑定池已保存')
  expect(saved[0]).toMatchObject({ mobile: { storageIds: ['m2'] }, tianyi: { storageIds: ['t1'] }, rotationMinutes: 720 })
  await page.getByRole('button', { name: '返回 CAS 任务', exact: true }).click()
  const reopened = await openPool(page)
  await expect(reopened.getByRole('button',{name:'绑定 移动影音二',exact:true})).toHaveAttribute('aria-pressed','true')
  await page.reload()
  await expect(page.getByRole('button', { name: '保存设置', exact: true })).toBeEnabled()
  await expect(poolRegion(page).getByRole('button',{name:'绑定 移动影音二',exact:true})).toHaveAttribute('aria-pressed','true')
})

test('CAS fixed three-row two-column lists fill blanks, preserve storage order and scroll with three-physical-pixel rail', async ({ page }) => {
  const { snapshot } = await workspace(page)
  snapshot.storages.unshift(...Array.from({ length: 6 }, (_, index) => account(`extra-${index}`, `移动额外 ${index}`, 'mobile')))
  await page.goto('/tasks/cas')
  const pool = await openPool(page)
  const mobile = pool.getByRole('region', { name: '移动云盘', exact: true })
  const tianyi = pool.getByRole('region', { name: '天翼云盘', exact: true })
  await expect(mobile.locator('.cas-pool-blank')).toHaveCount(0)
  await expect(tianyi.locator('.cas-pool-blank')).toHaveCount(1)
  await expect(mobile.locator('.cas-binding-toggle').first()).toHaveAttribute('aria-label', '绑定 移动额外 0')
  await expect(mobile.locator('.cas-pool-list')).toHaveCSS('height', '204px')
  expect(await mobile.locator('header > .provider-icon').evaluate(el => Math.abs(el.getBoundingClientRect().left-el.parentElement.getBoundingClientRect().left))).toBeLessThan(1)
  await expect(mobile.locator('.scroll-rail')).toBeVisible()
  await expect(tianyi.locator('.scroll-rail')).toBeHidden()
  await expect(mobile.locator('.cas-binding-toggle').first()).toHaveAttribute('aria-pressed','true')
  await mobile.locator('.cas-pool-list').hover()
  await page.mouse.wheel(0, 500)
  await expect.poll(() => mobile.locator('.cas-pool-list').evaluate(el => el.scrollTop)).toBeGreaterThan(0)
  expect(await mobile.locator('.scroll-rail-thumb').evaluate(el => {
    const width = parseFloat(getComputedStyle(el, '::after').width)
    return Math.abs(width * devicePixelRatio * (visualViewport?.scale || 1) - 3) < .1
  })).toBe(true)
  const toggle = mobile.getByRole('button', { name: '绑定 移动影音二', exact: true })
  await toggle.click()
  await expect(toggle).toHaveAttribute('aria-pressed', 'true')
  await expect(toggle.locator('..')).not.toHaveClass(/binding-disabled/)
  await toggle.click()
  await expect(toggle.locator('..')).toHaveClass(/binding-disabled/)
})

test('CAS temporary dialog selects files and cleans only selected keys', async ({ page }) => {
  await workspace(page)
  let reads = 0, cleanups = 0
  const items = [{ key:'playing',size:1024, storageId: 'm1', name: 'playing.mkv', retentionHours: 12, active: true, expiresAt: '2000-01-01T00:00:00Z' }, {key:'expired',size:2048, storageId: 't1', name: 'expired.mkv', retentionHours: 24, active: false, expiresAt: '2000-01-01T00:00:00Z' }]
  await page.route('**/api/cas/status', route => {
    reads++
    return route.fulfill({ json: { stats:{success:4,failed:1,mobile:3,tianyi:1},items: cleanups ? items.slice(0, 1) : items } })
  })
  await page.route('**/api/cas/cleanup', route => {
    expect(route.request().method()).toBe('POST');expect(route.request().postDataJSON()).toEqual({keys:['expired']}); cleanups++
    return route.fulfill({ json: { removed: 1 } })
  })
  await page.goto('/tasks/cas/pool')
  const strip = page.getByRole('region', { name: 'CAS 秒传统计', exact: true })
  await expect(strip.locator('dd')).toHaveText(['4','1','3','1'])
  await expect(strip.getByRole('img',{name:'秒传成功率 80%',exact:true})).toBeVisible()
  await strip.getByRole('button',{name:'临时文件',exact:true}).click()
  const dialog=page.getByRole('dialog',{name:'秒传临时文件',exact:true})
  await expect(dialog).toContainText('移动影音一')
  await expect(dialog).toContainText('2.0 KB')
  await dialog.locator('.temporary-item').filter({hasText:'expired.mkv'}).click()
  await dialog.getByRole('button',{name:'清理',exact:true}).click()
  await page.getByRole('button',{name:'确认清理',exact:true}).click()
  await expect(dialog).not.toContainText('expired.mkv')
  await expect(dialog).toContainText('playing.mkv')
  expect(cleanups).toBe(1)
})

test('CAS legacy minute rotation stays unchanged until edited and temporary status failures remain retryable', async ({ page }) => {
  const { saved } = await workspace(page)
  let statusReads = 0
  await page.route('**/api/cas/pool', route => {
    if (route.request().method() === 'PUT') saved.push(route.request().postDataJSON())
    const legacy = initialPool(); delete legacy.configured; legacy.rotationMinutes = 17
    return route.fulfill({ json: route.request().method() === 'PUT' ? saved.at(-1) : legacy })
  })
  await page.route('**/api/cas/status', route => route.fulfill(++statusReads === 1 ? { status: 503, json: { error: '临时文件暂不可用' } } : { json: { items: [] } }))
  await page.route('**/api/cas/cleanup', route => route.fulfill({ status: 502, json: { error: '过期清理失败' } }))
  await page.goto('/tasks/cas/pool')
  const pool = poolRegion(page)
  await expect(page.getByRole('button', { name: '保存设置', exact: true })).toBeEnabled()
  await expect(pool.getByRole('button',{name:'绑定 移动影音一',exact:true})).toHaveAttribute('aria-pressed','true')
  await expect(pool.getByRole('spinbutton', { name: '轮换时间', exact: true })).toHaveValue(String(17 / 60))
  await page.getByRole('button', { name: '保存设置', exact: true }).click()
  await expect(pool.getByRole('status')).toHaveText('存储绑定池已保存')
  expect(saved[0].rotationMinutes).toBe(17)
  await expect(page.locator('.toast').filter({hasText:'临时文件暂不可用'})).toBeVisible()
  await page.getByRole('button',{name:'临时文件',exact:true}).click()
  await expect.poll(()=>statusReads).toBeGreaterThanOrEqual(2)
  await expect(page.getByRole('dialog',{name:'秒传临时文件',exact:true})).toContainText('共 0 项')
  await expect(page.getByRole('button', { name: '返回 CAS 任务', exact: true })).toBeEnabled()
})
