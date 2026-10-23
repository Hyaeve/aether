import { test, expect } from '@playwright/test'
import { createHash } from 'node:crypto'
import { readFileSync } from 'node:fs'

const image = readFileSync(new URL('../src/assets/icons/refresh.png', import.meta.url))
const imageHash = 'a4967359638c4953c7323a2e55190a663361715d1d0a7ef848b57bf7c5cce874'

async function setup(page) {
  const requests = []
  await page.route('**/api/**', route => {
    const path = new URL(route.request().url()).pathname
    requests.push(path)
    let json = {}
    if (path === '/api/auth/status') json = { initialized: true, authenticated: true }
    if (path === '/api/state') json = {
      username: 'refresh-icons', settings: {}, cache: {}, traffic: {}, links: [],
      storages: [{ id: 's', name: 'Local', type: 'local', enabled: true, config: {} }, { id: 'pan', name: '115', type: '115', enabled: true, config: {} }],
      tasks: [{ id: 'library', kind: 'strm', name: 'Library' }]
    }
    if (['/api/files', '/api/files/visits', '/api/logs', '/api/transfers', '/api/links', '/api/backup-rules', '/api/strm-scrape/items'].includes(path)) json = []
    if (path === '/api/cloud/manage') json = { items: [], nextPage: 0 }
    if (path === '/api/link-playback') json = { recentEvents: [] }
    if (path === '/api/version/check') json = { current: '0.6.1', message: '当前版本一致' }
    return route.fulfill({ json })
  })
  return requests
}

async function assertImage(page, button) {
  const svg = button.locator('svg.refresh-icon')
  await expect(svg).toBeVisible()
  await expect(svg).toHaveAttribute('aria-hidden', 'true')
  const href = await svg.locator('image').getAttribute('href')
  const result = await page.request.get(href)
  expect(result.ok()).toBe(true)
  expect(createHash('sha256').update(await result.body()).digest('hex')).toBe(imageHash)
  const mask = await svg.locator('mask').getAttribute('id')
  await expect(svg.locator('rect')).toHaveAttribute('mask', `url(#${mask})`)
  return svg
}

async function finishTransitions(page) {
  await page.evaluate(async () => {
    await new Promise(requestAnimationFrame)
    for (const animation of document.getAnimations()) {
      if (animation.effect?.getTiming().iterations !== Infinity) animation.finish()
    }
  })
}

test('refresh asset is the supplied PNG without alteration', () => {
  expect(createHash('sha256').update(image).digest('hex')).toBe(imageHash)
})

for (const [url, label, endpoint] of [
  ['/files', '刷新目录', '/api/files'],
  ['/logs', '刷新日志', '/api/logs'],
  ['/transfer', '刷新传输任务', '/api/transfers'],
  ['/links/cache', '刷新播放流水', '/api/link-playback'],
  ['/storage/shares', '刷新云盘栏目', '/api/cloud/manage'],
  ['/storage/downloads', '刷新云盘栏目', '/api/cloud/manage'],
  ['/storage/trash', '刷新云盘栏目', '/api/cloud/manage'],
  ['/tasks/scrape', '刷新刮削索引', '/api/strm-scrape/scan'],
  ['/settings/about', '检查更新', '/api/version/check']
]) test(`${label} uses the shared image and still requests ${endpoint} (${url})`, async ({ page }) => {
  const requests = await setup(page)
  await page.goto(url)
  const button = page.getByRole('button', { name: label, exact: true })
  await expect(button).toBeEnabled()
  await assertImage(page, button)
  const before = requests.filter(path => path === endpoint).length
  await button.click()
  await expect.poll(() => requests.filter(path => path === endpoint).length).toBeGreaterThan(before)
})

for (const viewport of [{ width: 1440, height: 1000, dpr: 1 }, { width: 390, height: 844, dpr: 3 }]) {
  for (const theme of ['light', 'dark']) test(`refresh shape, color and geometry at ${viewport.width}px ${theme}`, async ({ browser }, info) => {
    const context = await browser.newContext({ viewport, deviceScaleFactor: viewport.dpr })
    try {
      const page = await context.newPage()
      await setup(page)
      await page.goto('/files')
      await page.evaluate(theme => document.documentElement.dataset.theme = theme, theme)
      const button = page.getByRole('button', { name: '刷新目录', exact: true })
      await expect(button).toBeEnabled()
      const svg = await assertImage(page, button)
      const box = await svg.boundingBox()
      expect(box.width).toBe(20)
      expect(box.height).toBe(20)
      const history = await page.getByRole('button', { name: '历史访问', exact: true }).locator('svg').boundingBox()
      expect(history.width).toBe(box.width)
      expect(history.height).toBe(box.height)
      await finishTransitions(page)
      const color = await svg.evaluate(el => getComputedStyle(el).color)
      await expect(svg).toHaveCSS('fill', color)
      const clip = { x: Math.floor(box.x), y: Math.floor(box.y), width: Math.ceil(box.x + box.width) - Math.floor(box.x), height: Math.ceil(box.y + box.height) - Math.floor(box.y) }
      const screenshot = await page.screenshot({ clip, animations: 'disabled' })
      const pixels = await page.evaluate(async ({ screenshot, source, color, box, clip }) => {
        const load = src => new Promise((resolve, reject) => { const img = new Image(); img.onload = () => resolve(img); img.onerror = reject; img.src = src })
        const actual = await load(`data:image/png;base64,${screenshot}`)
        const reference = await load(`data:image/png;base64,${source}`)
        const canvas = document.createElement('canvas')
        canvas.width = actual.width; canvas.height = actual.height
        const ctx = canvas.getContext('2d')
        ctx.drawImage(actual, 0, 0)
        const data = ctx.getImageData(0, 0, canvas.width, canvas.height).data
        ctx.clearRect(0, 0, canvas.width, canvas.height)
        const scale = canvas.width / clip.width
        // SVG first rasterizes its mask at icon size, then positions that surface.
        const mask = document.createElement('canvas')
        mask.width = box.width * scale; mask.height = box.height * scale
        mask.getContext('2d').drawImage(reference, 0, 0, mask.width, mask.height)
        ctx.drawImage(mask, (box.x - clip.x) * scale, (box.y - clip.y) * scale)
        const alpha = ctx.getImageData(0, 0, canvas.width, canvas.height).data
        ctx.clearRect(0, 0, canvas.width, canvas.height)
        ctx.fillStyle = color; ctx.fillRect(0, 0, 1, 1)
        const target = ctx.getImageData(0, 0, 1, 1).data
        const difference = [0, 1, 2].map(c => target[c] - data[c])
        const contrast = difference.reduce((sum, d) => sum + d * d, 0)
        let solid = 0, correct = 0, clear = 0, clearCorrect = 0, coverage = 0, expectedCoverage = 0
        for (let i = 0; i < data.length; i += 4) {
          expectedCoverage += alpha[i + 3] / 255
          coverage += Math.max(0, Math.min(1, difference.reduce((sum, d, c) => sum + (data[i + c] - data[c]) * d, 0) / contrast))
          if (alpha[i + 3] > 250) {
            solid++
            if ([0, 1, 2].every(c => Math.abs(data[i + c] - target[c]) < 12)) correct++
          }
          if (alpha[i + 3] < 5) {
            clear++
            if ([0, 1, 2].every(c => Math.abs(data[i + c] - data[c]) < 12)) clearCorrect++
          }
        }
        return { solid, correct, clear, clearCorrect, contrast, coverage, expectedCoverage }
      }, { screenshot: screenshot.toString('base64'), source: image.toString('base64'), color, box, clip })
      expect(pixels.contrast).toBeGreaterThan(1000)
      expect(pixels.expectedCoverage).toBeGreaterThan(50 * viewport.dpr ** 2)
      expect(Math.abs(pixels.coverage - pixels.expectedCoverage) / pixels.expectedCoverage).toBeLessThan(.05)
      expect(pixels.solid).toBeGreaterThan(0)
      expect(pixels.correct / pixels.solid, JSON.stringify({ box, clip, ...pixels })).toBeGreaterThan(.95)
      expect(pixels.clearCorrect / pixels.clear).toBeGreaterThan(.95)
      await button.hover()
      await finishTransitions(page)
      const hoverColor = await svg.evaluate(el => getComputedStyle(el).color)
      expect(hoverColor).not.toBe(color)
      await expect(svg).toHaveCSS('fill', hoverColor)
      expect(await svg.boundingBox()).toEqual(box)
      await page.screenshot({ path: info.outputPath(`refresh-${theme}.png`), animations: 'disabled' })
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    } finally { await context.close() }
  })
}

test('refresh retains disabled and rotating busy states until the directory response completes', async ({ page }) => {
  await setup(page)
  await page.goto('/files')
  const button = page.getByRole('button', { name: '刷新目录', exact: true })
  await expect(button).toBeEnabled()
  let release
  const response = new Promise(resolve => { release = resolve })
  await page.route('**/api/files?**', async route => { await response; await route.fulfill({ json: [] }) })
  try {
    await button.click()
    await expect(button).toBeDisabled()
    const svg = button.locator('svg.refresh-icon')
    await expect(svg).toHaveClass(/spin/)
    await expect(svg).toHaveCSS('animation-name', 'rotate')
    const transform = await svg.evaluate(el => getComputedStyle(el).transform)
    await expect.poll(() => svg.evaluate(el => getComputedStyle(el).transform)).not.toBe(transform)
  } finally { release() }
  await expect(button).toBeEnabled()
  await expect(button.locator('svg')).not.toHaveClass(/spin/)
})

test('directory picker refresh is shared while the non-refresh media-upgrade symbol stays unchanged', async ({ page }) => {
  const requests = await setup(page)
  await page.goto('/tasks/strm')
  await page.getByRole('button', { name: '添加任务', exact: true }).first().click()
  await page.getByRole('button', { name: '选择目录', exact: true }).click()
  const picker = page.getByRole('dialog', { name: '选择存储目录', exact: true })
  await picker.getByRole('button', { name: /Local/ }).click()
  const button = picker.getByRole('button', { name: '刷新', exact: true })
  await expect(button).toBeEnabled()
  await assertImage(page, button)
  const before = requests.filter(path => path === '/api/files').length
  await button.click()
  await expect.poll(() => requests.filter(path => path === '/api/files').length).toBeGreaterThan(before)
  await page.goto('/tools')
  const plugin = page.getByRole('button', { name: '洗版策略', exact: true })
  await expect(plugin.locator('svg.lucide-refresh-cw-icon')).toBeVisible()
  await expect(plugin.locator('svg path')).toHaveCount(4)
  await expect(plugin.locator('.refresh-icon')).toHaveCount(0)
})
