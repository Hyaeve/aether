import { test, expect } from '@playwright/test'
import { captureScreenshot } from './helpers/screenshot.js'

const storageName = '中文ABC汉字115名称'
async function fixture(page, theme) {
  const unexpected = []
  await page.addInitScript(value => localStorage.setItem('aether-theme', value), theme)
  await page.route('**/api/**', route => {
    const path = new URL(route.request().url()).pathname
    let json
    if (path === '/api/auth/status') json = { initialized: true, authenticated: true }
    else if (path === '/api/state') json = {
      username: 'spacing', storages: [{ id: 'pool', name: storageName, type: 'local', enabled: true, config: {} }],
      tasks: [], settings: { sessionDays: 15 }, cache: {}, traffic: {}, logs: []
    }
    else if (path === '/api/traffic') json = {}
    else if (path === '/api/strm-replace') json = { jobs: [] }
    else if (path === '/api/plugins/proxy') json = { enabled: false, address: '', username: '', password: '' }
    else if (['/api/plugins/tmdb', '/api/plugins/ai', '/api/plugins/emby'].includes(path)) json = { enabled: false }
    else if (['/api/115-signin', '/api/115-folder-cleanup', '/api/115-recycle-cleanup'].includes(path)) json = { enabled: false, rules: [] }
    else if (path === '/api/115-simulcast/enabled') json = { enabled: true }
    else if (path === '/api/quark-takeover') json = { enabled: false, bindings: [] }
    else {
      unexpected.push(path)
      return route.fulfill({ status: 500, json: { error: `Unmocked API: ${path}` } })
    }
    return route.fulfill({ json })
  })
  return unexpected
}

for (const [width, height, theme] of [[1440, 1000, 'light'], [390, 844, 'dark'], [844, 390, 'light']]) {
  test.describe(`Han typography ${width} ${theme}`, () => {
    test.use({ viewport: { width, height }, reducedMotion: 'reduce' })
    test('only Han-to-Han glyph advances grow while text and mixed-script boundaries stay exact', async ({ page }, info) => {
      const unexpected = await fixture(page, theme)
      await page.goto('/storage')
      const heading = page.getByRole('heading', { name: storageName, exact: true })
      await expect(heading).toBeVisible()
      await expect(heading.locator('.han-gap')).toHaveText(['中', '汉', '名'])
      const geometry = await heading.evaluate(el => {
        function rangePositions() {
          const walker = document.createTreeWalker(el, NodeFilter.SHOW_TEXT), result = []
          while (walker.nextNode()) {
            const text = walker.currentNode
            for (let i = 0; i < text.length; i++) {
              const range = document.createRange()
              range.setStart(text, i); range.setEnd(text, i + 1)
              result.push(range.getBoundingClientRect().x)
            }
          }
          return result
        }
        const metrics = () => ({
          width: el.querySelector('.han-text').getBoundingClientRect().width,
          chars: [...el.querySelectorAll('.han-gap')].map(node => ({
            x: node.getBoundingClientRect().x, width: node.getBoundingClientRect().width,
            padding: getComputedStyle(node).paddingInlineEnd
          }))
        })
        const spacedRange = rangePositions(), spacedMetrics = metrics(), fontSize = parseFloat(getComputedStyle(el).fontSize)
        el.style.setProperty('--han-gap', '0em')
        const originalRange = rangePositions(), originalMetrics = metrics()
        el.style.removeProperty('--han-gap')
        const copy = el.cloneNode(true)
        Object.assign(copy.style, { userSelect: 'text', font: getComputedStyle(el).font, position: 'fixed', left: '-10000px' })
        const walker = document.createTreeWalker(copy, NodeFilter.SHOW_TEXT), texts = []
        while (walker.nextNode()) texts.push(walker.currentNode)
        // WebKit rounds Range text rectangles; inline element rectangles retain layout precision.
        for (const text of texts) {
          const fragment = document.createDocumentFragment()
          for (const character of text.data) {
            const probe = document.createElement('aether-probe')
            probe.textContent = character; fragment.append(probe)
          }
          text.replaceWith(fragment)
        }
        document.body.append(copy)
        const positions = () => [...copy.querySelectorAll('aether-probe')].map(node => node.getBoundingClientRect().x)
        const spaced = positions()
        copy.style.setProperty('--han-gap', '0em')
        const original = positions()
        const selection = getSelection(), range = document.createRange()
        range.selectNodeContents(copy); selection.removeAllRanges(); selection.addRange(range)
        const selected = selection.toString()
        selection.removeAllRanges()
        copy.remove()
        return { spaced, original, spacedRange, originalRange, spacedMetrics, originalMetrics, fontSize, selected, text: el.textContent, tracking: getComputedStyle(el).letterSpacing }
      })
      await info.attach('glyph-geometry', { body: JSON.stringify(geometry, null, 2), contentType: 'application/json' })
      expect(geometry.text).toBe(storageName)
      expect(geometry.selected).toBe(storageName)
      expect(['0px', 'normal']).toContain(geometry.tracking)
      for (let i = 0; i < geometry.spacedMetrics.chars.length; i++) {
        expect(Math.abs(geometry.spacedMetrics.chars[i].width - geometry.originalMetrics.chars[i].width - geometry.fontSize * .1)).toBeLessThan(.035)
      }
      for (let i = 0; i < storageName.length - 1; i++) {
        const extra = geometry.spaced[i + 1] - geometry.spaced[i] - (geometry.original[i + 1] - geometry.original[i])
        expect(Math.abs(extra - ([0, 5, 10].includes(i) ? geometry.fontSize * .1 : 0))).toBeLessThan(.035)
      }
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
      await captureScreenshot(page, { path: info.outputPath(`han-storage-${width}-${theme}.png`) })
      await page.goto('/settings/account')
      const username = page.getByRole('textbox', { name: '账号', exact: true })
      await username.fill(storageName)
      await expect(username).toHaveValue(storageName)
      expect(await username.evaluate(el => el.selectionStart)).toBe(storageName.length)
      expect(unexpected).toEqual([])
    })

    test('navigation, buttons, descriptions and dynamic records retain names without overflowing', async ({ page }, info) => {
      const unexpected = await fixture(page, theme)
      await page.goto('/tools')
      const tool = page.getByRole('button', { name: '115 自动签到', exact: true })
      await expect(tool).toBeVisible()
      await expect(tool.locator('strong')).toHaveText('115 自动签到')
      await expect(tool.locator('strong .han-gap')).toHaveCount(3)
      expect(await tool.locator('strong').evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
      await tool.click()
      const dialog = page.getByRole('dialog', { name: '115 自动签到', exact: true })
      await expect(dialog).toBeVisible()
      await expect(dialog.getByRole('button', { name: '添加签到', exact: true })).toBeVisible()
      await page.keyboard.press('Escape')
      await page.getByRole('button', { name: '搜索辅助工具', exact: true }).click()
      const search = page.getByRole('textbox', { name: '插件搜索关键词', exact: true })
      await search.fill('秒传')
      const result = page.locator('.plugin-search-result')
      await expect(result).toHaveCount(1)
      await expect(result).toContainText('115 秒传中心')
      await search.fill('签到')
      await expect(result).toHaveCount(1)
      await expect(result).toContainText('115 自动签到')
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
      await captureScreenshot(page, { path: info.outputPath(`han-tools-${width}-${theme}.png`) })
      expect(unexpected).toEqual([])
    })
  })
}
