import { test, expect } from '@playwright/test'

async function fixture(page) {
  const storages = [1, 2, 3].map(i => ({ id: `s${i}`, name: `存储${i}`, type: 'local', enabled: true, config: { root: '/media' } }))
  const tasks = [1, 2, 3].map(i => ({ id: `t${i}`, name: `任务${i}`, kind: 'strm', storageId: 's1', source: '/', target: `/output/${i}`, enabled: true, status: 'idle', processed: 0 }))
  const links = [1, 2, 3].map(i => ({ id: `l${i}`, name: `以链${i}`, type: 'emby', mode: 'always', enabled: true, port: 16000 + i, upstream: 'http://localhost:8096' }))
  const mounts = [1, 2, 3].map(i => ({ id: `m${i}`, name: `挂载${i}`, mountPoint: `/mnt/${i}`, source: '/', mode: 511, uid: 0, gid: 0, status: 'stopped' }))
  const moves = []
  await page.route('**/api/**', async route => {
    const path = new URL(route.request().url()).pathname
    if (path.endsWith('/reorder')) {
      const move = route.request().postDataJSON()
      moves.push({ path, ...move })
      const items = path.includes('storages') ? storages : path.includes('tasks') ? tasks : links
      const from = items.findIndex(item => item.id === move.id), to = items.findIndex(item => item.id === move.target)
      items.splice(to, 0, items.splice(from, 1)[0])
      return route.fulfill({ json: { ok: true } })
    }
    const data = path === '/api/auth/status' ? { initialized: true, authenticated: true, username: 'card-test' }
      : path === '/api/state' ? { storages, tasks, settings: {}, traffic: {}, cache: {}, username: 'card-test' }
        : path === '/api/links' ? links : path === '/api/mounts' ? mounts : path === '/api/backup-rules' ? [] : {}
    await route.fulfill({ json: data })
  })
  return moves
}

const surfaces = [
  { path: '/tasks/strm', selector: '.task-row-card', text: '.task-row-copy', control: '.task-run', id: 't', dialog: '编辑 STRM 任务' },
  { path: '/storage', selector: '.storage-card', text: '.storage-card-name', control: '.provider-toggle', id: 's', dialog: '编辑存储池' },
  { path: '/links/manage', selector: '.link-card', text: '.link-identity strong', control: '.link-toggle', id: 'l', dialog: '编辑以太链接' },
  { path: '/files/mounts', selector: '.mount-card', text: '.mount-identity', control: '.mount-toggle', id: 'm', dialog: '编辑挂载' }
]

for (const surface of surfaces) {
  test(`${surface.id}: long hold follows pointer, drops once, and keeps keyboard sorting`, async ({ page }, info) => {
    const moves = await fixture(page)
    await page.goto(surface.path)
    const cards = page.locator(surface.selector)
    await expect(cards).toHaveCount(3)
    const source = await cards.first().locator(surface.text).boundingBox()
    const cardBox = await cards.first().boundingBox()
    const target = await cards.nth(1).locator(surface.text).boundingBox()
    const x = source.x + source.width / 2, y = source.y + source.height / 2
    await page.mouse.move(x, y); await page.mouse.down()
    const ghost = page.locator('.card-drag-ghost')
    await expect(ghost).toBeVisible()
    await expect(cards.first()).toHaveClass(/dragging/)
    const gx = x + 24, gy = y + 19
    await page.mouse.move(gx, gy)
    await expect.poll(async () => (await ghost.boundingBox()).x).toBeCloseTo(cardBox.x + 24, 0)
    await expect.poll(async () => (await ghost.boundingBox()).y).toBeCloseTo(cardBox.y + 19, 0)
    expect(await page.locator('html').evaluate(el => getComputedStyle(el).cursor)).toBe('grabbing')
    await page.screenshot({ path: info.outputPath(`${surface.id}-following.png`) })
    await page.mouse.move(target.x + target.width / 2, target.y + target.height / 2)
    await expect(cards.nth(1)).toHaveClass(/drop-target/)
    await page.mouse.up()
    await expect(ghost).toHaveCount(0)
    await expect(page.getByRole('dialog')).toHaveCount(0)
    await expect(cards.first()).toHaveAttribute('data-id', `${surface.id}2`)
    if (surface.id !== 'm') expect(moves).toEqual([{ path: `/api/${surface.id === 't' ? 'tasks' : surface.id === 's' ? 'storages' : 'links'}/reorder`, id: `${surface.id}1`, target: `${surface.id}2` }])
    await cards.first().focus(); await page.keyboard.press('Alt+ArrowDown')
    await expect(cards.first()).toHaveAttribute('data-id', `${surface.id}1`)
    await cards.nth(1).focus(); await page.keyboard.press('Alt+ArrowUp')
    await expect(cards.first()).toHaveAttribute('data-id', `${surface.id}2`)
    if (surface.id === 'm') {
      await page.reload()
      await expect(cards.first()).toHaveAttribute('data-id', 'm2')
    }
  })

  test(`${surface.id}: short click edits, controls exclude drag, Escape and exit clean up`, async ({ page }) => {
    const moves = await fixture(page)
    await page.goto(surface.path)
    const card = page.locator(surface.selector).first()
    await card.locator(surface.text).click()
    await expect(page.getByRole('dialog')).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(page.getByRole('dialog')).toHaveCount(0)
    const control = await card.locator(surface.control).boundingBox()
    await page.mouse.move(control.x + control.width / 2, control.y + control.height / 2); await page.mouse.down()
    await page.waitForTimeout(190)
    await expect(page.locator('.card-drag-ghost')).toHaveCount(0)
    await page.mouse.move(control.x + 10, control.y + control.height + 12); await page.mouse.up()
    const box = await card.locator(surface.text).boundingBox()
    await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2); await page.mouse.down()
    await expect(page.locator('.card-drag-ghost')).toBeVisible()
    await page.keyboard.press('Escape'); await page.mouse.up()
    await expect(page.locator('.card-drag-ghost')).toHaveCount(0)
    await expect(page.getByRole('dialog')).toHaveCount(0)
    expect(moves).toHaveLength(0)
    await card.locator(surface.text).click()
    await expect(page.getByRole('dialog')).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(page.getByRole('dialog')).toHaveCount(0)
    await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2); await page.mouse.down()
    await expect(page.locator('.card-drag-ghost')).toBeVisible()
    await page.getByRole('link', { name: '仪表盘', exact: true }).dispatchEvent('click')
    await page.mouse.up()
    await expect(page.locator('.card-drag-ghost')).toHaveCount(0)
    await expect(page.locator('html')).not.toHaveClass(/card-drag-active/)
  })
}

test('task menu order and editor disclosures share link typography and reveal content', async ({ page }) => {
  await fixture(page)
  await page.setViewportSize({ width: 900, height: 520 })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.goto('/tasks/strm')
  await page.getByRole('button', { name: '任务操作 任务1', exact: true }).click()
  await expect(page.locator('.task-menu button')).toHaveText(['编辑任务', '全量重置', '删除任务'])
  await expect(page.locator('.task-menu button').first()).toHaveCSS('font-size', '14px')
  await page.getByRole('button', { name: '编辑任务', exact: true }).click()
  const disclosure = page.getByRole('button', { name: '更多选项', exact: true })
  await expect(disclosure).toHaveCSS('font-size', '14px')
  await disclosure.click()
  await expect(disclosure).toHaveAttribute('aria-expanded', 'true')
  await expect.poll(() => page.locator('.more-options').evaluate(el => el.getBoundingClientRect().bottom <= el.closest('.modal').getBoundingClientRect().bottom)).toBe(true)
  await page.keyboard.press('Escape')
  await page.goto('/links/manage')
  await page.locator('.link-identity strong').first().click()
  const summary = page.locator('.link-more summary')
  await expect(summary).toHaveCSS('font-size', '14px')
  await summary.click()
  await expect.poll(() => page.locator('.link-more').evaluate(el => el.open && el.getBoundingClientRect().bottom <= el.closest('.modal').getBoundingClientRect().bottom)).toBe(true)
  await page.keyboard.press('Escape')
  await page.goto('/storage')
  await page.getByRole('button', { name: '存储操作 存储1', exact: true }).click()
  await expect(page.locator('.storage-menu button').first()).toHaveCSS('font-size', '14px')
})

test('touch scrolling before hold and pointer cancellation do not edit or reorder', async ({ page }) => {
  const moves = await fixture(page)
  await page.goto('/tasks/strm')
  const card = page.locator('.task-row-card').first()
  const box = await card.boundingBox()
  await card.dispatchEvent('pointerdown', { pointerId: 77, pointerType: 'touch', isPrimary: true, button: 0, clientX: box.x + 120, clientY: box.y + 20 })
  await card.dispatchEvent('pointermove', { pointerId: 77, pointerType: 'touch', clientX: box.x + 120, clientY: box.y + 50 })
  await page.waitForTimeout(190)
  await card.dispatchEvent('pointerup', { pointerId: 77, pointerType: 'touch' })
  await card.dispatchEvent('click')
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await expect(page.locator('.card-drag-ghost')).toHaveCount(0)
  await card.dispatchEvent('pointerdown', { pointerId: 78, pointerType: 'touch', isPrimary: true, button: 0, clientX: box.x + 120, clientY: box.y + 20 })
  await expect(page.locator('.card-drag-ghost')).toBeVisible()
  await card.dispatchEvent('pointercancel', { pointerId: 78, pointerType: 'touch' })
  await expect(page.locator('.card-drag-ghost')).toHaveCount(0)
  expect(moves).toHaveLength(0)
})

test('real mobile touch holds follow and reorder all four card types', async ({ browser }, info) => {
  const context = await browser.newContext({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true })
  const page = await context.newPage()
  await fixture(page)
  const session = await context.newCDPSession(page)
  try {
    for (const surface of surfaces) {
      await page.goto(surface.path)
      const cards = page.locator(surface.selector)
      await expect(cards).toHaveCount(3)
      const box = await cards.first().locator(surface.text).boundingBox()
      const cardBox = await cards.first().boundingBox()
      const target = await cards.nth(1).locator(surface.text).boundingBox()
      const x = box.x + box.width / 2, y = box.y + box.height / 2
      await session.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [{ x, y, id: 1 }] })
      await expect(page.locator('.card-drag-ghost')).toBeVisible()
      await session.send('Input.dispatchTouchEvent', { type: 'touchMove', touchPoints: [{ x: x + 12, y: y + 22, id: 1 }] })
      await expect.poll(async () => (await page.locator('.card-drag-ghost').boundingBox())?.y).toBeCloseTo(cardBox.y + 22, 0)
      await page.screenshot({ path: info.outputPath(`${surface.id}-touch-following.png`) })
      await session.send('Input.dispatchTouchEvent', { type: 'touchMove', touchPoints: [{ x: target.x + target.width / 2, y: target.y + target.height / 2, id: 1 }] })
      await session.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] })
      await expect(cards.first()).toHaveAttribute('data-id', `${surface.id}2`)
      await expect(page.locator('.card-drag-ghost')).toHaveCount(0)
      await expect(page.getByRole('dialog')).toHaveCount(0)
    }
  } finally { await context.close() }
})

test('nested backup editor reveals expanded content inside its own viewport', async ({ page }) => {
  await fixture(page)
  await page.setViewportSize({ width: 390, height: 500 })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.goto('/transfer/backup')
  await page.locator('.backup-heading').getByRole('button', { name: '添加规则', exact: true }).click()
  await page.getByRole('navigation', { name: '备份配置步骤' }).getByRole('button', { name: '筛选规则', exact: true }).click()
  const summary = page.locator('.backup-more summary')
  await summary.click()
  await expect(summary).toHaveCSS('font-size', '14px')
  await expect.poll(() => page.locator('.backup-more').evaluate(el => el.open && el.getBoundingClientRect().bottom <= el.closest('.backup-editor').getBoundingClientRect().bottom)).toBe(true)
})
