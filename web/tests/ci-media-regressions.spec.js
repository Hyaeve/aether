import { test, expect } from '@playwright/test'
import { readFileSync } from 'node:fs'

const movie = readFileSync(new URL('./fixtures/thumbnail.webm', import.meta.url))
async function workspace(page, files) {
  await page.route('**/api/**', route => {
    const path = new URL(route.request().url()).pathname
    return route.fulfill({ json: path === '/api/auth/status' ? { initialized: true, authenticated: true }
      : path === '/api/state' ? { username: 'ci-media', storages: [{ id: 'local', type: 'local', name: '资料', enabled: true, config: {} }], tasks: [], settings: {}, traffic: {}, cache: {} }
        : path === '/api/files' ? files : {} })
  })
}

for (const frameCallbacks of [true, false]) test(`thumbnail waits for a decoded frame (frame callbacks: ${frameCallbacks})`, async ({ page }) => {
  await page.addInitScript(frameCallbacks => {
    if (!frameCallbacks) { HTMLVideoElement.prototype.requestVideoFrameCallback = undefined; return }
    const presented = new WeakSet(), callback = HTMLVideoElement.prototype.requestVideoFrameCallback
    HTMLVideoElement.prototype.requestVideoFrameCallback = function (handler) {
      return callback.call(this, (now, metadata) => { presented.add(this); handler(now, metadata) })
    }
    const draw = CanvasRenderingContext2D.prototype.drawImage
    CanvasRenderingContext2D.prototype.drawImage = function (source, ...args) {
      if (source instanceof HTMLVideoElement && !presented.has(source)) {
        this.fillStyle = '#000'; this.fillRect(0, 0, this.canvas.width, this.canvas.height)
      } else draw.call(this, source, ...args)
    }
  }, frameCallbacks)
  await workspace(page, [{ id: 'v', name: '预览.webm', url: '/stream/frame.webm' }])
  await page.route('**/api/files/thumbnail?**', route => route.request().method() === 'POST'
    ? route.fulfill({ contentType: 'image/jpeg', body: route.request().postDataBuffer() })
    : route.fulfill({ status: 404, headers: { 'X-Thumbnail-Revision': 'ci-frame' } }))
  await page.route('**/api/files/thumbnail-source?**', route => route.fulfill({ contentType: 'video/webm', body: movie }))
  await page.goto('/files')
  await page.getByRole('button', { name: '当前列表视图，切换网格' }).click()
  const thumbnail = page.locator('.video-thumbnail img')
  await expect.poll(() => thumbnail.evaluate(img => img.complete && img.naturalWidth === 320).catch(() => false)).toBe(true)
  const blue = await thumbnail.evaluate(img => {
    const canvas = document.createElement('canvas'); canvas.width = 320; canvas.height = 180
    const ctx = canvas.getContext('2d'); ctx.drawImage(img, 0, 0)
    return ctx.getImageData(200, 20, 1, 1).data[2]
  })
  expect(blue).toBeGreaterThan(60)
  await expect(page.locator('.video-thumbnail video')).not.toHaveAttribute('src')
})

test('leaving the grid cancels a pending frame and ignores its late callback', async ({ page }) => {
  await page.addInitScript(() => {
    const request = HTMLVideoElement.prototype.requestVideoFrameCallback, cancel = HTMLVideoElement.prototype.cancelVideoFrameCallback
    window.cancelledFrames = 0
    HTMLVideoElement.prototype.requestVideoFrameCallback = function (handler) {
      return request.call(this, (now, metadata) => { window.lateFrame = () => handler(now, metadata) })
    }
    HTMLVideoElement.prototype.cancelVideoFrameCallback = function (id) { window.cancelledFrames++; cancel.call(this, id) }
  })
  await workspace(page, [{ id: 'v', name: '预览.webm', url: '/stream/frame.webm' }])
  let writes = 0
  await page.route('**/api/files/thumbnail?**', route => {
    if (route.request().method() === 'POST') { writes++; return route.fulfill({ status: 204 }) }
    return route.fulfill({ status: 404, headers: { 'X-Thumbnail-Revision': 'cancel-frame' } })
  })
  await page.route('**/api/files/thumbnail-source?**', route => route.fulfill({ contentType: 'video/webm', body: movie }))
  await page.goto('/files')
  await page.getByRole('button', { name: '当前列表视图，切换网格' }).click()
  await expect.poll(() => page.evaluate(() => typeof window.lateFrame)).toBe('function')
  await page.getByRole('button', { name: '当前网格视图，切换列表' }).click()
  await expect.poll(() => page.evaluate(() => window.cancelledFrames)).toBe(1)
  await page.evaluate(async () => { window.lateFrame(); await new Promise(requestAnimationFrame) })
  expect(writes).toBe(0)
  await expect(page.locator('.video-thumbnail')).toHaveCount(0)
})

test('subtitle menu stays usable when its focused option is disabled during loading', async ({ page }) => {
  await workspace(page, [{ id: '/video.mp4', name: 'video.mp4', url: '/stream/missing.mp4' }])
  await page.route('**/stream/missing.mp4', route => route.fulfill({ status: 404 }))
  let release
  const pending = new Promise(resolve => { release = resolve })
  await page.route('**/api/files/video-subtitles?**', async route => {
    if (!new URL(route.request().url()).searchParams.has('track')) return route.fulfill({ json: { tracks: [{ index: 2, title: '中文', supported: true }] } })
    await pending
    return route.fulfill({ contentType: 'text/vtt', body: 'WEBVTT\n\n00:00:00.000 --> 00:00:02.000\n中文字幕\n' })
  })
  await page.goto('/files')
  await page.getByRole('button', { name: 'video.mp4', exact: true }).dblclick()
  await page.getByRole('button', { name: '字幕', exact: true }).hover()
  const menu = page.getByRole('menu', { name: '字幕选择' }), chinese = page.getByRole('menuitemradio', { name: '中文', exact: true })
  await chinese.focus()
  await chinese.click()
  await expect(chinese).toBeDisabled()
  // Some engines blur a disabled button; reproduce that without moving the pointer.
  await chinese.evaluate(el => el.blur())
  await page.waitForTimeout(250)
  await expect(menu).toBeVisible()
  release()
  await expect(page.locator('.video-viewer track')).toHaveAttribute('src', /^blob:/)
  await page.getByRole('menuitemradio', { name: '关闭字幕', exact: true }).click()
  await expect(page.locator('.video-viewer track')).toHaveCount(0)
  await page.mouse.move(2, 2)
  await expect(menu).toHaveCount(0)
})

test('reopening details during its exit transition preserves the new multi-selection', async ({ page }) => {
  await workspace(page, [
    { id: '/Movies', name: 'Movies', isDir: true, size: 20, sizeKnown: true, countsKnown: true, folderCount: 1, fileCount: 2 },
    { id: '/film.mkv', name: 'film.mkv', size: 5 }
  ])
  await page.goto('/files')
  await page.addStyleTag({ content: '.details-slide-leave-active { transition-duration: 2s !important; }' })
  const folder = page.getByRole('button', { name: 'Movies', exact: true }), file = page.getByRole('button', { name: 'film.mkv', exact: true })
  await folder.click({ button: 'right' })
  await page.getByRole('button', { name: '查看详情', exact: true }).click()
  const drawer = page.locator('.file-details-drawer')
  await expect(drawer).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(drawer).toHaveClass(/details-slide-leave-active/)
  await folder.click()
  await file.click({ modifiers: ['Shift'] })
  await expect(page.locator('.file-row.selected')).toHaveCount(2)
  await file.click({ button: 'right' })
  await page.getByRole('button', { name: '查看详情', exact: true }).click()
  await expect(drawer).toBeFocused()
  await expect(drawer).not.toHaveClass(/details-slide-leave-active/)
  await expect(drawer).toContainText('2 个文件夹，3 个文件')
})
