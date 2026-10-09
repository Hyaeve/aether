import { test, expect } from '@playwright/test'
import { parseLyrics } from '../src/audio-metadata.js'

const png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII=', 'base64')
function music() {
  const frame = (id, value) => { const b = Buffer.alloc(10); b.write(id); b.writeUInt32BE(value.length, 4); return Buffer.concat([b, value]) }
  const tags = Buffer.concat([
    frame('TIT2', Buffer.from([3, ...Buffer.from('Aether Song')])),
    frame('TPE1', Buffer.from([3, ...Buffer.from('Aether Artist')])),
    frame('USLT', Buffer.concat([Buffer.from([3]), Buffer.from('eng\0Embedded first line\nEmbedded second line')])),
    frame('APIC', Buffer.concat([Buffer.from([3]), Buffer.from('image/png\0'), Buffer.from([3, 0]), png]))
  ])
  const id3 = Buffer.alloc(10); id3.write('ID3'); id3[3] = 3
  for (let i = 0; i < 4; i++) id3[9 - i] = tags.length >> (i * 7) & 127
  const chunk = Buffer.concat([id3, tags]), pad = chunk.length % 2
  const rate = 8000, data = Buffer.alloc(44 + rate * 20 * 2)
  data.write('RIFF'); data.write('WAVEfmt ', 8); data.writeUInt32LE(16, 16); data.writeUInt16LE(1, 20); data.writeUInt16LE(1, 22); data.writeUInt32LE(rate, 24); data.writeUInt32LE(rate * 2, 28); data.writeUInt16LE(2, 32); data.writeUInt16LE(16, 34); data.write('data', 36); data.writeUInt32LE(data.length - 44, 40)
  const header = Buffer.alloc(8); header.write('id3 '); header.writeUInt32LE(chunk.length, 4)
  const b = Buffer.concat([data, header, chunk, Buffer.alloc(pad)]); b.writeUInt32LE(b.length - 8, 4); return b
}
async function setup(page, lrc = false, tags = true, pending) {
  const bytes = music(), ranges = []
  await page.route('**/api/auth/status', r => r.fulfill({ json: { initialized: true, authenticated: true } }))
  await page.route('**/api/state', r => r.fulfill({ json: { username: 'audio-metadata', storages: [{ id: 'local', type: 'local', name: 'Music', enabled: true, config: {} }], tasks: [], settings: {}, traffic: {}, cache: {} } }))
  await page.route('**/api/plugins/*', r => r.fulfill({ json: {} }))
  await page.route('**/api/quark-takeover', r => r.fulfill({ json: { bindings: [] } }))
  await page.route('**/api/files?**', r => r.fulfill({ json: [{ id: 'song', name: 'music.wav', url: '/music.wav', size: bytes.length }, ...(lrc ? [{ id: 'lyrics', name: 'music.lrc' }, { id: 'cover', name: 'cover.png' }] : [])] }))
  await page.route('**/music.wav', r => {
    const range = r.request().headers().range
    if (!range) return r.fulfill({ body: bytes, contentType: 'audio/wav', headers: { 'Accept-Ranges': 'bytes' } })
    const [, a, b] = range.match(/bytes=(\d+)-(\d*)/), start = Number(a), end = b ? Math.min(Number(b), bytes.length - 1) : bytes.length - 1
    return r.fulfill({ status: 206, headers: { 'Content-Range': `bytes ${start}-${end}/${bytes.length}`, 'Accept-Ranges': 'bytes', 'Content-Type': 'audio/wav' }, body: bytes.subarray(start, end + 1) })
  })
  await page.route('**/api/files/audio-source?**', async r => {
    const id = new URL(r.request().url()).searchParams.get('id')
    if (id === 'lyrics' && pending) {
      pending.started(); await pending.wait
      return r.fulfill({ body: '[00:01]Late lyrics', contentType: 'text/plain' }).catch(() => {})
    }
    if (id === 'lyrics') return r.fulfill({ body: '[00:00.00]First line\n[00:05.00]Second line\n[00:10.00]Third line', contentType: 'text/plain' })
    if (id === 'cover') return r.fulfill({ body: png, contentType: 'image/png' })
    if (!tags) return r.fulfill({ status: 502, json: { error: 'unavailable' } })
    const range = r.request().headers().range
    ranges.push(range || 'HEAD')
    if (r.request().method() === 'HEAD') return r.fulfill({ headers: { 'Content-Length': String(bytes.length), 'Accept-Ranges': 'bytes', 'Content-Type': 'audio/wav' } })
    const [, a, b] = range.match(/bytes=(\d+)-(\d+)/), start = Number(a), end = Math.min(Number(b), bytes.length - 1)
    return r.fulfill({ status: 206, headers: { 'Content-Range': `bytes ${start}-${end}/${bytes.length}`, 'Accept-Ranges': 'bytes', 'Content-Type': 'audio/wav' }, body: bytes.subarray(start, end + 1) })
  })
  await page.goto('/files'); await page.getByRole('button', { name: 'music.wav', exact: true }).dblclick()
  return ranges
}

test('embedded artwork and lyrics are parsed without downloading the whole track', async ({ page }, info) => {
  const ranges = await setup(page)
  const panel = page.getByRole('dialog', { name: 'music.wav' })
  await expect(panel.getByRole('heading', { name: 'Aether Song' })).toBeVisible()
  await expect(panel.getByText('Aether Artist')).toBeVisible()
  await expect(panel.getByText('Embedded first line')).toBeVisible()
  await expect.poll(() => panel.locator('.audio-art img').evaluate(el => el.naturalWidth)).toBeGreaterThan(0)
  expect(ranges.some(r => r.startsWith('bytes='))).toBeTruthy()
  for (const range of ranges.filter(r => r.startsWith('bytes='))) { const [, a, b] = range.match(/bytes=(\d+)-(\d+)/); expect(Number(b) - Number(a)).toBeLessThan(1048576) }
  await page.screenshot({ path: info.outputPath('embedded-artwork.png') })
})

test('sidecar artwork and lyrics remain available when embedded tags cannot be read', async ({ page }) => {
  await setup(page, true, false)
  const panel = page.getByRole('dialog', { name: 'music.wav' })
  await panel.getByRole('button', { name: '切换到歌词页' }).click()
  await expect(panel.getByRole('button', { name: 'First line', exact: true })).toBeVisible()
  await expect.poll(() => panel.locator('.audio-art img').evaluate(el => el.naturalWidth)).toBeGreaterThan(0)
  await expect(panel.getByRole('heading', { name: 'music.wav' })).toBeVisible()
})

test('closing before lyric responses arrive cancels the request without restoring a drawer', async ({ page }) => {
  let release, started
  const wait = new Promise(resolve => { release = resolve })
  const requested = new Promise(resolve => { started = resolve })
  await setup(page, true, false, { wait, started })
  await requested
  const panel = page.getByRole('dialog', { name: 'music.wav' })
  await panel.getByRole('button', { name: '关闭音频播放' }).click()
  release()
  await expect(panel).toHaveCount(0)
  await expect(page.getByText('Late lyrics')).toHaveCount(0)
})

test('LRC parsing handles offsets, duplicate stamps, plain text and invalid timestamps', () => {
  expect(parseLyrics('[ar:Artist]\n[offset:-500]\n[00:01.50][00:04.00]歌词\n[00:99.00]invalid')).toEqual([{ time: 1, text: '歌词' }, { time: 3.5, text: '歌词' }])
  expect(parseLyrics('第一行\n第二行')).toEqual([{ time: null, text: '第一行' }, { time: null, text: '第二行' }])
})

test('sidecar LRC overrides embedded text and follows playback and seeking in dark mobile', async ({ page }, info) => {
  await page.setViewportSize({ width: 390, height: 780 }); await setup(page, true)
  await page.evaluate(() => document.documentElement.dataset.theme = 'dark')
  const panel = page.getByRole('dialog', { name: 'music.wav' })
  await expect.poll(() => panel.locator('.audio-art img').evaluate(el => el.naturalWidth)).toBeGreaterThan(0)
  await panel.getByRole('button', { name: '切换到歌词页' }).click()
  await expect(panel.getByRole('button', { name: 'First line', exact: true })).toBeVisible()
  await expect(panel.getByText('Embedded first line')).toHaveCount(0)
  const audio = panel.locator('audio')
  await expect.poll(() => audio.evaluate(el => el.readyState)).toBeGreaterThanOrEqual(2)
  await audio.evaluate(el => { el.pause(); el.currentTime = 6 })
  await expect(panel.getByRole('button', { name: 'Second line', exact: true })).toHaveAttribute('aria-current', 'true')
  await panel.getByRole('button', { name: 'Third line', exact: true }).click()
  await expect.poll(() => audio.evaluate(el => el.currentTime)).toBeCloseTo(10, 0)
  await expect(panel.getByRole('button', { name: 'Third line', exact: true })).toHaveAttribute('aria-current', 'true')
  await expect(panel.locator('.audio-ambient')).toBeVisible()
  await page.screenshot({ path: info.outputPath('mobile-lyrics.png') })
  await panel.focus(); await page.keyboard.press('Escape'); await expect(panel).toHaveCount(0)
})
