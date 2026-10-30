import { test, expect } from '@playwright/test'
import { captureScreenshot } from './helpers/screenshot.js'

const longName = '星际穿越.Interstellar.2014.4K.REMUX.TrueHD.7.1.'.repeat(6) + '原盘.mkv'
const records = [
  { id: 'skip', name: longName, status: 'failed', message: '未命中秒传，已跳过并保留源文件' },
  { id: 'delete', name: '重试后删除.mkv', status: 'deleted', retries: 3, message: '重试3次秒传仍未命中，已删除源文件' },
  { id: 'unsafe', name: '删除未完成.mkv', status: 'failed', retries: 3, message: '源文件已变化，已保留源文件' },
  { id: 'retry', name: '等待重试.mkv', status: 'retrying', retries: 1, message: '秒传未成功，30分钟后进行第2次重试' },
  { id: 'success', name: '首次成功.mkv', status: 'success', message: '秒传成功' },
  { id: 'uploaded', name: '传输成功.mkv', status: 'uploaded', message: '传输成功' }
].map(item => ({ ...item, time: '2026-10-29T15:00:00+08:00', size: 1024, sourceKind: 'monitor', source: '监控 · 影音监控' }))

async function fixture(page, theme) {
  const unexpected = []
  await page.addInitScript(value => localStorage.setItem('aether-theme', value), theme)
  await page.route('**/api/**', route => {
    const path = new URL(route.request().url()).pathname
    let json
    if (path === '/api/auth/status') json = { initialized: true, authenticated: true }
    else if (path === '/api/state') json = {
      username: 'file-rapid-feedback', storages: [{ id: 'pool', name: '家庭影音', type: '115', enabled: true, config: {} }],
      tasks: [], settings: {}, cache: {}, traffic: {}, logs: [], fileFavorites: []
    }
    else if (path === '/api/files') json = [{ id: 'movie', name: '影片.mkv', size: 1024 }]
    else if (path === '/api/traffic') json = { uploadRate: 0, downloadRate: 0 }
    else if (path === '/api/strm-replace') json = { jobs: [] }
    else if (path === '/api/115-rapid') json = { manual: {}, watches: [{ id: 'watch', name: '影音监控', mode: 'watch', enabled: true }] }
    else if (path === '/api/115-rapid/records') json = { items: records, page: 1, pages: 1, total: records.length }
    else {
      unexpected.push(path)
      return route.fulfill({ status: 500, json: { error: `Unmocked API: ${path}` } })
    }
    return route.fulfill({ json })
  })
  return unexpected
}

async function waitForEntry(page) {
  await expect.poll(() => page.locator('.page-content').evaluate(el => el.getAnimations({ subtree: true }).every(a => a.playState === 'finished' || a.effect?.getTiming().iterations === Infinity))).toBe(true)
}

for (const [width, theme] of [[1440, 'light'], [1440, 'dark'], [390, 'light'], [390, 'dark']]) {
  test.describe(`file rapid feedback ${width} ${theme}`, () => {
    test.use({ viewport: { width, height: 900 } })

    test('favorites toggle stays highlighted when expanded and restores its state', async ({ page }, info) => {
      const unexpected = await fixture(page, theme)
      await page.goto('/files')
      const toggle = page.getByRole('button', { name: '展开收藏栏', exact: true })
      const appearance = () => toggle.evaluate(el => ({
        color: getComputedStyle(el).color,
        background: getComputedStyle(el).backgroundColor
      }))
      await page.mouse.move(0, 0)
      await expect(toggle).toHaveAttribute('aria-expanded', 'false')
      const collapsed = await appearance()
      await toggle.click()
      await page.mouse.move(0, 0)
      await expect(toggle).toHaveAttribute('aria-expanded', 'true')
      const expected = await toggle.evaluate(el => {
        const probe = document.createElement('span')
        probe.style.color = 'var(--primary)'
        probe.style.backgroundColor = 'var(--primary-soft)'
        el.append(probe)
        const result = { color: getComputedStyle(probe).color, background: getComputedStyle(probe).backgroundColor }
        probe.remove()
        return result
      })
      await expect.poll(appearance).toEqual(expected)
      expect(expected).not.toEqual(collapsed)
      await page.reload()
      await expect(toggle).toHaveAttribute('aria-expanded', 'true')
      await page.mouse.move(0, 0)
      await expect.poll(appearance).toEqual(expected)
      await captureScreenshot(page, { path: info.outputPath(`favorites-${width}-${theme}.png`) })
      await toggle.click()
      await page.mouse.move(0, 0)
      await expect(toggle).toHaveAttribute('aria-expanded', 'false')
      await expect.poll(appearance).toEqual(collapsed)
      await page.reload()
      await expect(toggle).toHaveAttribute('aria-expanded', 'false')
      expect(unexpected).toEqual([])
    })

    test('rapid outcomes distinguish retained and deleted sources in both record views', async ({ page }) => {
      const unexpected = await fixture(page, theme)
      await page.goto('/tools/115-rapid')
      const labels = ['失败跳过', '失败删除', '失败跳过', '等待第2次重试', '秒传成功', '传输成功']
      const verify = async root => {
        await expect(root.locator('.rapid115-status-label > span')).toHaveText(labels)
        for (let index = 0; index < records.length; index++) {
          await expect(root.locator('.rapid115-status-label').nth(index)).toHaveAttribute('data-tooltip', records[index].message)
          await expect(root.locator('progress').nth(index)).toHaveAttribute('aria-label', `${records[index].name}：${labels[index]}`)
        }
        await expect(root.locator('.rapid115-status.deleted svg')).toHaveClass(/lucide-trash2-icon/)
        const failures = root.locator('.rapid115-status.failed, .rapid115-status.deleted')
        const colors = await failures.evaluateAll(els => els.map(el => getComputedStyle(el).color))
        expect(new Set(colors).size).toBe(1)
        const deleted = root.locator('.rapid115-status.deleted .rapid115-status-label')
        await expect(deleted).toHaveAttribute('data-tooltip-always', 'true')
        await waitForEntry(page)
        await deleted.evaluate(el => el.scrollIntoView({ block: 'center', behavior: 'instant' }))
        await page.mouse.move(0, 0)
        await deleted.hover()
        await expect(page.getByRole('tooltip')).toHaveText(records[1].message)
        await page.keyboard.press('Escape')
      }
      await verify(page.locator('.rapid115-records'))
      await page.getByRole('button', { name: '查看 影音监控 秒传记录', exact: true }).click()
      await verify(page.getByRole('dialog', { name: '影音监控 · 秒传记录', exact: true }))
      expect(unexpected).toEqual([])
    })

    test('rapid filename tooltip fills its narrower width and reflows the full name', async ({ page }, info) => {
      const unexpected = await fixture(page, theme)
      await page.goto('/tools/115-rapid')
      await waitForEntry(page)
      const avatar = page.locator('.watch-avatar').first()
      await expect(avatar).toHaveText('影音')
      expect(await avatar.evaluate(el => {
        const range = document.createRange(), walker = document.createTreeWalker(el, NodeFilter.SHOW_TEXT), tops = new Set()
        while (walker.nextNode()) {
          range.selectNodeContents(walker.currentNode)
          for (const rect of range.getClientRects()) if (rect.width > 0) tops.add(rect.top)
        }
        return tops.size
      })).toBe(1)
      const name = page.locator('.rapid115-record-name').first()
      await name.scrollIntoViewIfNeeded()
      await page.clock.install()
      await page.clock.pauseAt(new Date())
      await page.mouse.move(0, 0)
      await name.hover()
      await page.clock.runFor(299)
      const tip = page.getByRole('tooltip')
      await expect(tip).toHaveCount(0)
      await page.clock.runFor(1)
      await expect(tip).toHaveText(longName)
      await page.clock.resume()
      const layout = await tip.evaluate(el => {
        const style = getComputedStyle(el), bounds = el.getBoundingClientRect()
        const range = document.createRange(), walker = document.createTreeWalker(el, NodeFilter.SHOW_TEXT), lines = []
        while (walker.nextNode()) {
          range.selectNodeContents(walker.currentNode)
          for (const rect of range.getClientRects()) {
            if (!rect.width) continue
            const line = lines.find(item => Math.abs(item.top - rect.top) < .5)
            if (line) { line.left = Math.min(line.left, rect.left); line.right = Math.max(line.right, rect.right) }
            else lines.push({ top: rect.top, left: rect.left, right: rect.right })
          }
        }
        lines.sort((a, b) => a.top - b.top)
        return {
          width: bounds.width, left: bounds.left, right: bounds.right,
          contentWidth: el.clientWidth - parseFloat(style.paddingLeft) - parseFloat(style.paddingRight),
          lineWidths: lines.map(line => line.right - line.left), overflow: el.scrollWidth > el.clientWidth,
          background: style.backgroundColor
        }
      })
      expect(layout.width).toBe(Math.min(480, width - 16))
      expect(layout.left).toBeGreaterThanOrEqual(8)
      expect(layout.right).toBeLessThanOrEqual(width - 8)
      expect(layout.overflow).toBe(false)
      expect(layout.lineWidths.length).toBeGreaterThan(1)
      for (const line of layout.lineWidths.slice(0, -1)) {
        expect(line).toBeGreaterThan(layout.contentWidth - 28)
        expect(line).toBeLessThanOrEqual(layout.contentWidth + 1)
      }
      expect(layout.background).not.toBe('rgb(255, 255, 255)')
      await captureScreenshot(page, { path: info.outputPath(`rapid-name-${width}-${theme}.png`) })
      await page.keyboard.press('Escape')
      await expect(tip).toHaveCount(0)
      expect(unexpected).toEqual([])
    })
  })
}
