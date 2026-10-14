import { test, expect } from '@playwright/test'

test('tab favicon keeps the original mark while exposing a larger plugin canvas', async ({ page, request }, info) => {
  const html = await (await request.get('/')).text()
  expect(html).toContain('sizes="128x128" href="/aether-favicon.svg?v=3"')
  const original = await request.get('/aether.svg'), favicon = await request.get('/aether-favicon.svg')
  expect(original.ok()).toBe(true)
  expect(favicon.ok()).toBe(true)
  const expected='<svg xmlns="http://www.w3.org/2000/svg" width="128" height="128" viewBox="0 0 64 64"><rect width="64" height="64" rx="11" fill="#3a4980"/><g transform="matrix(1.4 0 0 1.4 -12.8 -12.8)"><path d="M17 46 31 17h4l13 29h-7l-8-20-9 20z" fill="#fff"/><path d="m14 39 37-8" stroke="#b4b9d6" stroke-width="3" stroke-linecap="round"/><circle cx="49" cy="20" r="3" fill="#a8dfd2"/></g></svg>'
  expect((await favicon.text()).trim()).toBe(expected)
  const assets = [await original.text(),await favicon.text()].map(svg => `data:image/svg+xml;base64,${Buffer.from(svg).toString('base64')}`)
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
  expect(pixels[0][1]).toBeGreaterThan(0)
  expect(pixels[1][1]).toBeGreaterThan(0)
  await page.screenshot({ path: info.outputPath('favicon-light-dark.png') })
})
