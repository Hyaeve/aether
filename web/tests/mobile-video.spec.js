import { test, expect } from '@playwright/test'

async function workspace(page, storages = [], tasks = []) {
  await page.route('**/api/auth/status', r => r.fulfill({ json: { initialized: true, authenticated: true } }))
  await page.route('**/api/state', r => r.fulfill({ json: { username: 'mobile-video', storages, tasks, settings: {}, traffic: {}, cache: {} } }))
  await page.route('**/api/plugins/*', r => r.fulfill({ json: {} }))
  await page.route('**/api/quark-takeover', r => r.fulfill({ json: { bindings: [] } }))
}

test('mobile QR fills Authorization and ignores a closed scan response', async ({ page }, info) => {
  await workspace(page)
  let polls = 0
  await page.route('**/api/authorization/mobile/start', r => r.fulfill({ json: { token: 'mobile-session', image: '/aether.svg', expiresIn: 300 } }))
  await page.route('**/api/authorization/mobile/poll', r => {
    polls++
    expect(r.request().postDataJSON()).toEqual({ token: 'mobile-session' })
    return r.fulfill({ json: polls === 1 ? { status: 'waiting' } : { status: 'success', authorization: 'cGM6MTM5MDAwMDAwMDA6cXJ0b2tlbg==' } })
  })
  await page.goto('/storage')
  await page.getByRole('button', { name: '添加存储池', exact: true }).click()
  await page.getByRole('button', { name: '移动云盘', exact: true }).click()
  await page.getByRole('button', { name: '扫码获取 Authorization', exact: true }).click()
  await expect(page.getByRole('dialog', { name: '移动扫码获取 Authorization', exact: true })).toBeVisible()
  await expect(page.getByLabel('Authorization', { exact: true })).toHaveValue('cGM6MTM5MDAwMDAwMDA6cXJ0b2tlbg==', { timeout: 10000 })
  await expect(page.getByRole('dialog', { name: '移动扫码获取 Authorization', exact: true })).toHaveCount(0)
  await page.screenshot({ path: info.outputPath('mobile-authorization.png') })
  let release
  const gate = new Promise(resolve => { release = resolve })
  await page.route('**/api/authorization/mobile/start', async r => { await gate; await r.fulfill({ json: { token: 'late', image: '/aether.svg' } }) })
  await page.getByRole('button', { name: '扫码获取 Authorization', exact: true }).click()
  await page.getByRole('dialog', { name: '移动扫码获取 Authorization', exact: true }).getByRole('button', { name: '关闭', exact: true }).click()
  release()
  await expect(page.getByLabel('Authorization', { exact: true })).toHaveValue('cGM6MTM5MDAwMDAwMDA6cXJ0b2tlbg==')
})

for (const scale of [1, 2, 3]) {
test.describe(`video geometry DPR ${scale}`, () => {
test.use({ deviceScaleFactor: scale })
test('video grid extracts a real frame and player follows only the video boundary', async ({ page }, info) => {
  const encoded = await page.evaluate(async () => {
    const canvas = document.createElement('canvas'); canvas.width = 320; canvas.height = 180
    const ctx = canvas.getContext('2d')
    const stream = canvas.captureStream(15), recorder = new MediaRecorder(stream, { mimeType: 'video/webm;codecs=vp8' }), chunks = []
    recorder.ondataavailable = e => chunks.push(e.data)
    const complete = new Promise(resolve => { recorder.onstop = resolve })
    recorder.start()
    for (let frame = 0; frame < 15; frame++) {
      ctx.fillStyle = '#174b64'; ctx.fillRect(0, 0, 320, 180)
      ctx.fillStyle = '#b7e4bd'; ctx.fillRect(20 + frame * 6, 35, 90, 100)
      await new Promise(resolve => setTimeout(resolve, 70))
    }
    recorder.stop(); await complete; stream.getTracks().forEach(track => track.stop())
    const blob = new Blob(chunks, { type: 'video/webm' })
    return await new Promise(resolve => { const reader = new FileReader(); reader.onload = () => resolve(reader.result.split(',')[1]); reader.readAsDataURL(blob) })
  })
  await workspace(page, [{ id: 'local', type: 'local', name: '视频', enabled: true, config: {} }])
  await page.route('**/frame.webm', r => r.fulfill({ contentType: 'video/webm', body: Buffer.from(encoded, 'base64') }))
  await page.route('**/api/files?**', r => r.fulfill({ json: [{ id: 'v', name: '预览.webm', url: '/frame.webm' }] }))
  await page.goto('/files')
  await page.getByRole('button', { name: '当前列表视图，切换网格' }).click()
  await expect(page.locator('.video-thumbnail video')).toHaveClass('ready')
  const frame = await page.locator('.video-thumbnail video').evaluate(video => {
    const canvas = document.createElement('canvas'); canvas.width = 320; canvas.height = 180
    const ctx = canvas.getContext('2d'); ctx.drawImage(video, 0, 0)
    return { pixel: [...ctx.getImageData(200, 20, 1, 1).data], paused: video.paused, width: video.videoWidth }
  })
  expect(frame.paused).toBe(true); expect(frame.width).toBe(320); expect(frame.pixel[2]).toBeGreaterThan(60)
  await page.getByRole('button', { name: '预览.webm', exact: true }).dblclick()
  await expect(page.locator('.video-viewer video')).toHaveAttribute('controls', '')
  await expect.poll(() => page.locator('.video-viewer video').evaluate(v => v.videoWidth)).toBe(320)
  await page.locator('.video-viewer video').evaluate(async video => { await video.play(); video.pause() })
  await page.locator('.video-viewer').evaluate(async el => { await Promise.allSettled(el.getAnimations({ subtree: true }).map(a => a.finished)) })
  for (const viewport of [{ width: 1440, height: 900 }, { width: 390, height: 780 }, { width: 780, height: 390 }, { width: 1440, height: 900 }]) {
    await page.setViewportSize(viewport)
    // Viewport emulation can finish before resize dispatch and Vue's layout update.
    // DOM rectangles and innerWidth are CSS pixels regardless of device scale.
    const readRects = () => page.locator('.video-viewer').evaluate(el => {
      const video = el.querySelector('video'), a = el.getBoundingClientRect(), b = video.getBoundingClientRect()
      const ratio = video.videoWidth / video.videoHeight
      const expectedWidth = Math.max(1, Math.min(1080, innerWidth - 32, (innerHeight - 48) * ratio))
      return { width: a.width, height: a.height, videoWidth: b.width, videoHeight: b.height, viewportWidth: innerWidth, viewportHeight: innerHeight, scale: devicePixelRatio, expectedWidth, expectedHeight: expectedWidth / ratio, x: a.x, y: a.y }
    })
    await expect.poll(async () => {
      const r = await readRects()
      return r.viewportWidth === viewport.width && r.viewportHeight === viewport.height && Math.abs(r.width - r.expectedWidth) < 1 && Math.abs(r.height - r.expectedHeight) < 1
    }).toBe(true)
    const rects = await readRects()
    expect(rects.scale).toBe(scale)
    expect(rects.width).toBeLessThan(viewport.width); expect(rects.height).toBeLessThan(viewport.height)
    expect(rects.width).toBeCloseTo(rects.videoWidth, 0); expect(rects.height).toBeCloseTo(rects.videoHeight, 0)
    expect(rects.x).toBeGreaterThanOrEqual(0); expect(rects.y).toBeGreaterThanOrEqual(0)
    expect(rects.x + rects.width).toBeLessThanOrEqual(viewport.width)
    expect(rects.y + rects.height).toBeLessThanOrEqual(viewport.height)
    await expect.poll(() => page.locator('.video-viewer video').evaluate(video => video.readyState)).toBeGreaterThanOrEqual(2)
    await page.locator('.video-viewer').evaluate(async el => { await Promise.allSettled(el.getAnimations({ subtree: true }).map(a => a.finished)) })
    await page.screenshot({ path: info.outputPath(`video-${viewport.width}x${viewport.height}.png`) })
  }
  await page.keyboard.press('Escape')
  await expect(page.locator('.video-viewer')).toHaveCount(0)
  await expect(page.getByRole('button', { name: '预览.webm', exact: true })).toBeFocused()
})
})
}

test('task scan time leaves a wider gap before run controls', async ({ page }) => {
  await workspace(page, [{ id: 's', type: 'mobile', name: '移动', enabled: true, config: {} }], [{ id: 't', storageId: 's', name: '每日任务', kind: 'strm', lastRun: '2026-10-09T12:00:00+08:00', enabled: true }])
  await page.goto('/tasks/strm')
  await expect(page.locator('.task-last-scan')).toHaveCSS('margin-right', '38px')
})
