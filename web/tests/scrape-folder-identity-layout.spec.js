import { test, expect } from '@playwright/test'
import { captureScreenshot } from './helpers/screenshot.js'

const layouts = [[1440, 1000, 'light'], [390, 844, 'dark'], [375, 812, 'light'], [844, 390, 'dark']]
const longTitle = '这是一部需要省略展示但仍保留完整识别名称的电影'.repeat(4)
const entries = [
  { id: 'Show', name: '一斩苍穹 (2026)', isDir: true },
  { id: 'Movie', name: '星际穿越 (2014)', isDir: true },
  { id: 'Long', name: `${longTitle} (2026)`, isDir: true },
  { id: 'Unknown', name: '尚未核验的目录', isDir: true }
]

async function fixture(page, theme) {
  const unexpected = [], paths = []
  await page.addInitScript(value => localStorage.setItem('aether-theme', value), theme)
  await page.route('**/api/**', route => {
    const request = route.request(), url = new URL(request.url()), path = url.pathname
    let json
    if (path === '/api/auth/status') json = { initialized: true, authenticated: true }
    else if (path === '/api/state') json = {
      username: 'scrape-identity-layout', storages: [], settings: {}, traffic: {}, cache: {},
      tasks: [{ id: 't', name: '剧集库', kind: 'strm', target: '/media' }]
    }
    else if (path === '/api/strm-scrape/status') json = { running: false }
    else if (path === '/api/strm-scrape/settings') json = {}
    else if (path === '/api/strm-scrape/directories') json = { root: '/media', directories: entries.map(item => item.id) }
    else if (path === '/api/strm-scrape/items') json = [
      { path: 'Show/Season 1/one.strm', directories: ['Show/Season 1'], title: '猜测剧名', matchedTitle: '一斩苍穹', kind: 'tv', status: 'ok', tmdb: 303287 },
      { path: 'Movie/one.strm', directories: ['Movie'], title: '猜测片名', matchedTitle: '星际穿越', kind: 'movie', status: 'ok', tmdb: 157336 },
      { path: 'Long/one.strm', directories: ['Long'], title: '猜测长名', matchedTitle: longTitle, kind: 'movie', status: 'ok', tmdb: 1234567 },
      { path: 'Unknown/one.strm', directories: ['Unknown'], title: '未核验', kind: 'movie', status: 'pending', tmdb: 777 }
    ]
    else if (path === '/api/strm-scrape/files') {
      const directory = url.searchParams.get('path')
      paths.push(directory)
      json = directory === '.' ? entries : [{ id: 'Show/Season 1', name: 'Season 1', isDir: true }]
    }
    else if (path === '/api/generation/jobs') json = []
    else if (path === '/api/strm-replace' || path === '/api/traffic') json = {}
    else {
      unexpected.push(`${request.method()} ${path}`)
      return route.fulfill({ status: 500, json: { error: `Unmocked API: ${path}` } })
    }
    return route.fulfill({ json })
  })
  return { unexpected, paths }
}

for (const [width, height, theme] of layouts) test.describe(`scrape folder identity ${width} ${theme}`, () => {
  test.use({ viewport: { width, height } })

  test('recognized type, title and TMDB occupy the right column instead of the filename subtitle', async ({ page }, info) => {
    const f = await fixture(page, theme)
    await page.goto('/tasks/scrape')
    await page.getByRole('button', { name: '海报墙视图', exact: true }).click()
    const rows = page.locator('.scrape-file-row')
    await expect(rows).toHaveCount(4)
    for (const [index, identity] of ['剧集一斩苍穹TMDB 303287', '电影星际穿越TMDB 157336', `电影${longTitle}TMDB 1234567`].entries()) {
      const row = rows.nth(index)
      await expect(row.locator('.scrape-folder-identity')).toHaveText(identity)
      await expect(row).toHaveClass(/recognized/)
      await expect.poll(() => row.evaluate(el => {
        const name = el.querySelector('.file-name').getBoundingClientRect()
        const identity = el.querySelector('.scrape-folder-identity').getBoundingClientRect()
        const actions = el.querySelector('.scrape-folder-actions').getBoundingClientRect()
        const id = el.querySelector('.scrape-folder-identity small')
        const bounds = el.getBoundingClientRect()
        return {
          rightOfName: identity.left >= name.right + 4,
          centered: Math.abs(identity.top + identity.height / 2 - name.top - name.height / 2) < 1,
          beforeActions: identity.right <= actions.left - 4,
          idFits: id.scrollWidth <= id.clientWidth,
          positiveNameWidth: el.querySelector('.scrape-file-label').clientWidth > 0,
          withinRow: identity.top >= bounds.top && identity.bottom <= bounds.bottom,
          height: bounds.height,
          noOverflow: el.scrollWidth <= el.clientWidth
        }
      })).toEqual({
        rightOfName: true, centered: true, beforeActions: true, idFits: true,
        positiveNameWidth: true, withinRow: true, height: 56, noOverflow: true
      })
      if (width > 600) {
        const centers = await row.locator('.scrape-folder-identity').evaluate(el =>
          [...el.children].map(child => {
            const r = child.getBoundingClientRect()
            return r.top + r.height / 2
          }))
        expect(Math.max(...centers) - Math.min(...centers)).toBeLessThan(1)
      }
    }
    await expect(rows.nth(3).locator('.scrape-folder-identity')).toHaveCount(0)
    await expect(rows.nth(3)).not.toHaveClass(/recognized/)
    const long = rows.nth(2).locator('.scrape-folder-identity > span')
    await expect(long).toHaveAttribute('data-tooltip', longTitle)
    expect(await long.evaluate(el => el.scrollWidth > el.clientWidth)).toBe(true)
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    await rows.first().scrollIntoViewIfNeeded()
    await captureScreenshot(page, { path: info.outputPath('recognized-right-column.png') })
    const first = rows.first()
    await expect(first.getByRole('button', { name: entries[0].name, exact: true }))
      .toHaveAttribute('aria-description', '剧集，一斩苍穹，TMDB 303287')
    await first.getByRole('button', { name: '识别', exact: true }).click()
    await expect(page.getByRole('dialog', { name: '选择媒体匹配', exact: true })).toBeVisible()
    await page.getByRole('dialog').getByRole('button', { name: '关闭', exact: true }).click()
    await first.getByRole('button', { name: '重置', exact: true }).click()
    await expect(page.getByRole('dialog')).toContainText('重置作品元数据')
    await page.getByRole('dialog').getByRole('button', { name: '取消', exact: true }).click()
    await first.getByRole('button', { name: entries[0].name, exact: true }).click()
    await expect(page.locator('.scrape-file-path')).toContainText('根目录Show')
    await expect(page.locator('.scrape-folder-identity')).toHaveText('剧集一斩苍穹TMDB 303287')
    expect(f.paths).toContain('Show')
    expect(f.unexpected).toEqual([])
  })
})
