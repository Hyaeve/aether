import { test, expect } from '@playwright/test'

const examples = [
  ['https://yun.139.com/shareweb/#/w/i/2pU95iLhPMAro?pwd=bib9', 'pwd=bib9'],
  ['https://pan.quark.cn/s/2d2604889f4c?pwd=wGkx', 'pwd=wGkx'],
  ['https://115cdn.com/s/swsm2gq3np7?password=vvve', 'password=vvve']
]

async function openEditor(page) {
  await page.route('**/api/**', route => {
    const path = new URL(route.request().url()).pathname
    let json = {}
    if (path === '/api/auth/status') json = { initialized: true, authenticated: true }
    if (path === '/api/state') json = {
      username: 'share-parameters', storages: ['115', 'mobile', 'quark'].map(type => ({ id: type, type, name: type, enabled: true, config: { mode: 'native' } })),
      tasks: [], settings: {}, traffic: {}, cache: {}, links: []
    }
    if (['/api/files', '/api/files/visits', '/api/files/favorites', '/api/links', '/api/transfers'].includes(path)) json = []
    if (path === '/api/files/share-preferences') json = { last: {}, directories: {} }
    if (path === '/api/files/share/info') json = { directory: '' }
    return route.fulfill({ json })
  })
  await page.goto('/files?storage=mobile')
  await page.getByRole('button', { name: '工具', exact: true }).click()
  await page.getByRole('menuitem', { name: '分享转存', exact: true }).click()
  return page.getByRole('dialog', { name: '分享转存', exact: true })
}

for (const width of [1440, 390]) {
  for (const theme of ['light', 'dark']) test(`complete password parameters are colored without changing input at ${width}px ${theme}`, async ({ page }, info) => {
    await page.setViewportSize({ width, height: 900 })
    const modal = await openEditor(page)
    await page.evaluate(theme => document.documentElement.dataset.theme = theme, theme)
    const input = modal.getByRole('textbox', { name: '分享链接', exact: true })
    for (const [url, parameter] of examples) {
      await input.fill(url)
      await expect(modal.locator('mark')).toHaveText(parameter)
      await expect(input).toHaveValue(url)
      await expect(modal.getByRole('button', { name: '转存', exact: true })).toBeEnabled()
    }
    const content = examples.map(([url]) => url).join('\n') + '\u00a0'
    await input.fill(content)
    await expect(modal.locator('mark')).toHaveText(examples.map(([, parameter]) => parameter))
    const colors = await modal.locator('.share-link-editor').evaluate(el => {
      const pre = getComputedStyle(el.querySelector('pre'))
      const marks = [...el.querySelectorAll('mark')].map(mark => getComputedStyle(mark))
      const canvas = document.createElement('canvas')
      canvas.width = canvas.height = 1
      const ctx = canvas.getContext('2d')
      const pixel = (...colors) => {
        ctx.clearRect(0, 0, 1, 1)
        for (const color of colors) { ctx.fillStyle = color; ctx.fillRect(0, 0, 1, 1) }
        return [...ctx.getImageData(0, 0, 1, 1).data].slice(0, 3)
      }
      const luminance = rgb => rgb.map(value => {
        value /= 255
        return value <= .04045 ? value / 12.92 : ((value + .055) / 1.055) ** 2.4
      }).reduce((sum, value, index) => sum + value * [.2126, .7152, .0722][index], 0)
      return {
        normal: pre.color, highlighted: marks.map(mark => mark.color), backgrounds: marks.map(mark => mark.backgroundColor),
        shadows: marks.map(mark => mark.boxShadow), radii: marks.map(mark => mark.borderRadius),
        contrast: marks.map(mark => {
          const fg = luminance(pixel(mark.color)), bg = luminance(pixel(pre.backgroundColor, mark.backgroundColor))
          return (Math.max(fg, bg) + .05) / (Math.min(fg, bg) + .05)
        })
      }
    })
    expect(colors.highlighted.every(color => color !== colors.normal)).toBe(true)
    expect(colors.highlighted.every(color => color === colors.highlighted[0])).toBe(true)
    expect(colors.backgrounds.every(color => color === 'rgba(0, 0, 0, 0)')).toBe(true)
    expect(colors.shadows.every(value => value === 'none')).toBe(true)
    expect(colors.radii.every(value => value === '0px')).toBe(true)
    expect(Math.min(...colors.contrast)).toBeGreaterThanOrEqual(4.5)
    await page.screenshot({ path: info.outputPath(`parameters-${theme}.png`), animations: 'disabled' })
    expect(await modal.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)

    const markdown = examples.map(([url]) => `[${url}](${url})`).join('\n')
    await input.fill(markdown)
    await expect(modal.locator('mark')).toHaveText(examples.flatMap(([, parameter]) => [parameter, parameter]))
    await expect(input).toHaveValue(markdown)
    const edited = examples[0][0].replace('bib9', 'Xy12')
    await input.fill(edited)
    await expect(modal.locator('mark')).toHaveText('pwd=Xy12')
    await input.fill(edited.replace('?pwd=Xy12', ''))
    await expect(modal.locator('mark')).toHaveCount(0)
  })
}

test('mobile pasted and typed access codes normalize before tree and transfer requests', async ({ page }) => {
  const modal = await openEditor(page)
  const calls = []
  await page.route('**/api/files/share/*', route => {
    const body = route.request().postDataJSON(), action = new URL(route.request().url()).pathname.split('/').at(-1)
    calls.push({ action, ...body })
    const json = action === 'info' ? { directory: '' } : action === 'tree' ? { items: [{ id: 'file', name: 'film.mkv', isDir: false }], tree: [{ path: 'film.mkv', name: 'film.mkv', isDir: false }] } : action === 'preview' ? { preview: 'normalized', items: [{ name: 'film.mkv' }], batches: 1 } : { status: 'submitted', taskId: 'job' }
    return route.fulfill({ json })
  })
  const base = 'https://yun.139.com/shareweb/#/w/i/2yA8vJBMPc6kg', canonical = `${base}?pwd=4xu4`
  const input = modal.getByRole('textbox', { name: '分享链接', exact: true })
  await input.evaluate((el, text) => {
    const data = new DataTransfer()
    data.setData('text/plain', text)
    el.dispatchEvent(new ClipboardEvent('paste', { clipboardData: data, bubbles: true, cancelable: true }))
  }, `链接: ${base}\n提取码: 4xu4`)
  await expect(input).toHaveValue(canonical)
  await expect(modal.locator('mark')).toHaveText('pwd=4xu4')
  await input.fill(`${base} 提取码: 4xu4`)
  await input.blur()
  await expect(input).toHaveValue(canonical)
  await modal.getByRole('button', { name: '解析', exact: true }).click()
  const tree = page.getByRole('dialog', { name: '分享内容', exact: true })
  await expect(tree).toBeVisible()
  await tree.getByRole('button', { name: '应用', exact: true }).click()
  await modal.getByRole('button', { name: '转存', exact: true }).click()
  await expect(modal).toHaveCount(0)
  await expect.poll(() => calls.filter(call => call.action === 'batch').length).toBe(1)
  expect(calls.filter(call => ['info', 'tree', 'preview'].includes(call.action)).every(call => call.url === canonical)).toBe(true)
  await page.getByRole('button', { name: '工具', exact: true }).click()
  await page.getByRole('menuitem', { name: '分享转存', exact: true }).click()
  const again = page.getByRole('dialog', { name: '分享转存', exact: true })
  await again.getByRole('textbox', { name: '分享链接', exact: true }).fill(`${base}?pwd=Ab12 提取码: Cd34`)
  await again.getByRole('textbox', { name: '分享链接', exact: true }).blur()
  await expect(again.getByRole('textbox', { name: '分享链接', exact: true })).toHaveValue(`${base}?pwd=Ab12 提取码: Cd34`)
  await expect(again.getByRole('button', { name: '转存', exact: true })).toBeDisabled()
})
