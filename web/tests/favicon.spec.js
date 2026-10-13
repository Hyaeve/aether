import { test, expect } from '@playwright/test'

test('tab favicon enlarges the mark at 16 and 32px without changing the page logo', async ({ page, request }, info) => {
  const html = await (await request.get('/')).text()
  expect(html).toContain('sizes="any" href="/aether-favicon.svg"')
  const original = await request.get('/aether.svg'), favicon = await request.get('/aether-favicon.svg')
  expect(original.ok()).toBe(true)
  expect(favicon.ok()).toBe(true)
  const assets = [await original.text(), await favicon.text()].map(svg => `data:image/svg+xml;base64,${Buffer.from(svg).toString('base64')}`)
  await page.setContent(`<style>body{margin:0;font:14px sans-serif}.row{display:flex;gap:32px;align-items:center;padding:24px;background:#eff0f3;color:#222}.dark{background:#202127;color:white}.row img{margin:0 8px;vertical-align:middle}.zoom{width:128px;height:128px;image-rendering:pixelated}</style>${['light','dark'].map(theme => `<div class="row ${theme}">${assets.map((src, i) => `<div>${i ? 'Updated' : 'Original'}<img src="${src}" width="16" height="16"><img src="${src}" width="32" height="32"><br><img class="zoom" src="${src}"></div>`).join('')}</div>`).join('')}`)
  const pixels = await page.evaluate(async sources => {
    const results = []
    for (const size of [16, 32]) {
      const counts = []
      for (const src of sources) {
        const image = new Image(); image.src = src; await image.decode()
        const canvas = document.createElement('canvas'); canvas.width = canvas.height = size
        const ctx = canvas.getContext('2d'); ctx.drawImage(image, 0, 0, size, size)
        const data = ctx.getImageData(0, 0, size, size).data
        let bright = 0
        for (let i = 0; i < data.length; i += 4) if (data[i] > 150 && data[i + 1] > 150 && data[i + 2] > 150 && data[i + 3] > 200) bright++
        counts.push(bright)
      }
      results.push(counts)
    }
    return results
  }, assets)
  for (const [before, after] of pixels) expect(after).toBeGreaterThan(before * 1.5)
  await page.screenshot({ path: info.outputPath('favicon-light-dark.png') })
})
