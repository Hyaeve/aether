import { test, expect } from '@playwright/test'

const files = [
  { id: 'song', name: '星际漫游.wav', url: '/sample-audio.wav', size: 80000 },
  { id: 'unsupported', name: '旧音乐.wma', url: '/unsupported-audio.wma' },
  { id: 'archive', name: '资料.zip', url: '/archive.zip', size: 1000 },
  { id: 'text', name: '说明.txt', url: '/readme.txt' },
  { id: 'folder', name: '目录.zip', isDir: true }
]
async function workspace(page) {
  await page.route('**/api/auth/status', r => r.fulfill({ json: { initialized: true, authenticated: true } }))
  await page.route('**/api/state', r => r.fulfill({ json: { username: 'audio-extract', storages: [{ id: 'local', type: 'local', name: '音频资料', enabled: true, config: {} }], tasks: [], settings: {}, traffic: {}, cache: {} } }))
  await page.route('**/api/plugins/*', r => r.fulfill({ json: {} }))
  await page.route('**/api/quark-takeover', r => r.fulfill({ json: { bindings: [] } }))
  await page.route('**/api/files?**', r => r.fulfill({ json: files }))
}
function wav() {
  const rate = 8000, samples = rate * 20, b = Buffer.alloc(44 + samples * 2)
  b.write('RIFF'); b.writeUInt32LE(b.length - 8, 4); b.write('WAVEfmt ', 8); b.writeUInt32LE(16, 16)
  b.writeUInt16LE(1, 20); b.writeUInt16LE(1, 22); b.writeUInt32LE(rate, 24); b.writeUInt32LE(rate * 2, 28)
  b.writeUInt16LE(2, 32); b.writeUInt16LE(16, 34); b.write('data', 36); b.writeUInt32LE(samples * 2, 40)
  for (let i = 0; i < samples; i++) b.writeInt16LE(Math.round(Math.sin(i / rate * 440 * Math.PI * 2) * 1200), 44 + i * 2)
  return b
}

test('audio uses the native browser engine with custom controls in a right sliding drawer', async ({ page }, info) => {
  await workspace(page)
  await page.route('**/sample-audio.wav', r => r.fulfill({ contentType: 'audio/wav', body: wav() }))
  await page.goto('/files')
  const row = page.locator('.file-row').filter({ hasText: '星际漫游.wav' })
  await expect(row.locator('.audio-file-icon')).toHaveCount(1)
  await row.getByRole('button', { name: '星际漫游.wav' }).dblclick()
  const drawer = page.getByRole('dialog', { name: '星际漫游.wav' }), audio = drawer.locator('audio')
  await expect(audio).not.toHaveAttribute('controls')
  await expect(drawer.getByRole('slider', { name: '播放进度' })).toBeVisible()
  await expect.poll(() => audio.evaluate(a => a.readyState)).toBeGreaterThanOrEqual(2)
  await audio.evaluate(async a => { await a.play() })
  await expect.poll(() => audio.evaluate(a => a.currentTime)).toBeGreaterThan(0)
  await audio.evaluate(a => a.pause())
  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: 780 })
    if (width === 390) await page.evaluate(() => document.documentElement.dataset.theme = 'dark')
    await drawer.evaluate(async el => { await Promise.allSettled(el.getAnimations().map(a => a.finished)) })
    const rect = await drawer.boundingBox()
    expect(rect.x + rect.width).toBeCloseTo(width, 0); expect(rect.y).toBe(48)
    expect(rect.width).toBeLessThanOrEqual(width)
    const bounds = await drawer.getByRole('slider', { name: '播放进度' }).boundingBox()
    expect(bounds.x).toBeGreaterThanOrEqual(rect.x); expect(bounds.x + bounds.width).toBeLessThanOrEqual(width)
    await page.screenshot({ path: info.outputPath(`audio-${width}.png`) })
  }
  await drawer.focus(); await page.keyboard.press('Escape')
  await expect(drawer).toHaveCount(0)
  await expect(row.getByRole('button', { name: '星际漫游.wav' })).toBeFocused()
})

test('unsupported audio gives a native decode error and download fallback', async ({ page }) => {
  await workspace(page)
  await page.route('**/unsupported-audio.wma', r => r.fulfill({ contentType: 'audio/x-ms-wma', body: 'not a decodable stream' }))
  await page.goto('/files')
  await page.getByRole('button', { name: '旧音乐.wma', exact: true }).dblclick()
  const drawer = page.getByRole('dialog', { name: '旧音乐.wma' })
  await expect(drawer.getByRole('status')).toContainText('浏览器无法播放')
  await expect(drawer.getByRole('link', { name: '下载音频' })).toHaveAttribute('href', '/unsupported-audio.wma?download=1')
})

test('archive-only extract menu is directly below download and passwords can retry', async ({ page }, info) => {
  await workspace(page)
  let requests = [], reads = 0
  await page.route('**/api/files?**', r => { reads++; return r.fulfill({ json: files }) })
  await page.route('**/api/files/extract', r => {
    const data = r.request().postDataJSON(); requests.push(data)
    return data.password === 'correct' ? r.fulfill({ json: { processed: 2, directory: data.name } }) : r.fulfill({ status: 400, json: { error: '解压失败，请检查密码' } })
  })
  await page.goto('/files')
  for (const name of ['说明.txt', '目录.zip', '星际漫游.wav']) {
    await page.getByRole('button', { name, exact: true }).click({ button: 'right' })
    await expect(page.locator('.context-menu').getByRole('button', { name: '解压', exact: true })).toHaveCount(0)
    await page.keyboard.press('Escape')
  }
  await page.getByRole('button', { name: '资料.zip', exact: true }).click({ button: 'right' })
  const menu = page.locator('.context-menu')
  expect((await menu.getByRole('button').allTextContents()).slice(0, 2)).toEqual(['下载', '解压'])
  await menu.getByRole('button', { name: '解压', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '解压压缩包' })
  await expect(dialog.getByLabel('解压目录', { exact: true })).toHaveValue('资料')
  const password = dialog.getByLabel('解压密码', { exact: true })
  await password.fill('wrong')
  await dialog.getByRole('button', { name: '显示内容' }).click()
  await expect(password).toHaveAttribute('type', 'text'); await expect(password).toHaveValue('wrong')
  await dialog.getByRole('button', { name: '隐藏内容' }).click()
  await expect(password).toHaveAttribute('type', 'password')
  await dialog.getByRole('button', { name: '确认解压' }).click()
  await expect(dialog.getByRole('alert')).toContainText('检查密码')
  await page.screenshot({ path: info.outputPath('extract-password.png') })
  await password.fill('correct'); await dialog.getByLabel('解压目录', { exact: true }).fill('新目录')
  await dialog.getByRole('button', { name: '确认解压' }).click()
  await expect(dialog).toHaveCount(0)
  expect(requests[1]).toEqual({ storageId: 'local', parent: '/', id: 'archive', name: '新目录', password: 'correct' })
  expect(reads).toBeGreaterThanOrEqual(2)
  await expect(page.getByText('解压完成，共 2 个文件', { exact: true })).toBeVisible()
})

test('multi selection does not expose extraction', async ({ page }) => {
  await workspace(page); await page.goto('/files')
  await page.getByRole('button', { name: '资料.zip', exact: true }).click()
  await page.getByRole('button', { name: '说明.txt', exact: true }).click({ modifiers: ['Control'] })
  await page.getByRole('button', { name: '资料.zip', exact: true }).click({ button: 'right' })
  await expect(page.locator('.context-menu').getByRole('button', { name: '解压', exact: true })).toHaveCount(0)
})
