import { test, expect } from '@playwright/test'
import { captureScreenshot } from './helpers/screenshot.js'

async function fixture(page) {
  let repos = [], revision = 'initial', saves = 0
  const plugin = { id: 'Organize', name: '目录整理', subtitle: '媒体文件 · 自动整理', description: '按目录与文件类型整理媒体资料，保留完整目录结构并记录每次执行结果。支持自定义分类规则。', version: '1.0.0', author: 'Aether', aether: true }
  await page.route('**/api/**', route => {
    const r = route.request(), url = new URL(r.url()), path = url.pathname
    let json = {}
    if (path === '/api/auth/status') json = { initialized: true, authenticated: true }
    else if (path === '/api/state') json = { username: 'market-owner', storages: [], tasks: [], settings: {}, traffic: {}, cache: {} }
    else if (path === '/api/plugin-market/repositories') {
      if (r.method() === 'PUT') {
        const value = r.postDataJSON()
        if (value.revision !== revision) return route.fulfill({ status: 409, json: { error: '仓库配置已变化，请重新打开后编辑' } })
        if (value.repositories.some(repo => !repo.url.startsWith('https://github.com/'))) return route.fulfill({ status: 400, json: { error: '仓库地址须为 HTTPS GitHub 地址' } })
        repos = value.repositories.map((repo, i) => ({ ...repo, id: repo.id || `repo-${i}` })); revision = `save-${++saves}`
      }
      json = { repositories: repos, revision }
    } else if (path === '/api/plugin-market') {
      const q = url.searchParams.get('q') || ''
      const plugins = repos.filter(repo => repo.enabled).filter(() => `${plugin.name} ${plugin.description}`.includes(q)).map(repo => ({ ...plugin, repository: repo.id, source: repo.name }))
      json = { plugins, total: plugins.length, failures: [], runtimeAvailable: false }
    } else if (path === '/api/quark-takeover') json = { enabled: false, bindings: [] }
    else if (path === '/api/115-recycle-cleanup') json = { enabled: false, rules: [] }
    else if (path.startsWith('/api/plugins/')) json = { enabled: false, apiKey: '', url: '', bindings: [] }
    return route.fulfill({ json })
  })
  return { repos: () => repos }
}

for (const width of [1440, 390]) for (const theme of ['light', 'dark']) test(`repository catalog and shared tool search ${width} ${theme}`, async ({ page }, info) => {
  const f = await fixture(page)
  await page.setViewportSize({ width, height: 1000 })
  await page.goto('/tools')
  await page.evaluate(theme => document.documentElement.dataset.theme = theme, theme)
  const cards = page.locator('.plugin-card')
  await expect(cards).toHaveCount(16)
  await expect(cards.first()).toHaveCSS('height', '130px')
  for (const card of await cards.all()) {
    const bounds = await card.evaluate(el => { const text = el.querySelector('.plugin-description'); const box = text.getBoundingClientRect(); const card = el.getBoundingClientRect(); return { textHeight: box.height, bottom: card.bottom - box.bottom, overflow: el.scrollWidth > el.clientWidth } })
    expect(bounds.textHeight).toBeLessThanOrEqual(40)
    expect(bounds.bottom).toBeLessThanOrEqual(37)
    expect(bounds.overflow).toBe(false)
  }
  await captureScreenshot(page, { path: info.outputPath(`tools-${width}-${theme}.png`) })
  await page.getByRole('button', { name: '搜索辅助工具', exact: true }).click()
  await page.getByRole('textbox', { name: '插件搜索关键词', exact: true }).fill('TMDB')
  await expect(page.locator('.plugin-search-result')).toHaveCount(2)
  await expect(page.locator('.plugin-search-result').first()).toContainText('TMDB 配置')
  await expect(page.locator('.plugin-search-result').last()).toContainText('代理配置')
  await page.getByRole('textbox', { name: '插件搜索关键词', exact: true }).press('ArrowDown')
  await page.keyboard.press('Enter')
  await expect(page.getByRole('dialog', { name: 'TMDB 配置', exact: true })).toBeVisible()
  await page.keyboard.press('Escape')
  await page.getByRole('link', { name: '插件市场', exact: true }).click()
  await page.locator('.plugin-float-tools').hover()
  await expect(page.locator('.plugin-float-repository')).toHaveCSS('opacity', '1')
  await page.locator('.plugin-float-repository').click()
  await page.getByRole('button', { name: '添加仓库', exact: true }).click()
  const add = page.getByRole('dialog', { name: '添加插件仓库', exact: true })
  await add.getByLabel('仓库名称', { exact: true }).fill('Aether 社区')
  await add.getByLabel('仓库地址', { exact: true }).fill('https://github.com/example/aether-plugins')
  await add.getByRole('button', { name: '保存', exact: true }).click()
  await expect(add).toHaveCount(0)
  expect(f.repos()[0]).toMatchObject({ name: 'Aether 社区', enabled: true, ref: 'main' })
  await page.getByRole('dialog', { name: '插件仓库', exact: true }).getByRole('button', { name: '关闭', exact: true }).click()
  await expect(page.locator('.market-grid .plugin-card')).toHaveCount(1)
  await expect(page.locator('.market-grid .plugin-card')).toContainText('目录整理')
  await page.reload()
  await page.evaluate(theme => document.documentElement.dataset.theme = theme, theme)
  await expect(page.locator('.market-grid .plugin-card')).toHaveCount(1)
  await captureScreenshot(page, { path: info.outputPath(`market-${width}-${theme}.png`) })
  await page.getByRole('button', { name: '搜索市场插件', exact: true }).click()
  await page.getByRole('textbox', { name: '插件搜索关键词', exact: true }).fill('目录')
  await expect(page.locator('.plugin-search-result')).toHaveCount(1)
  await page.locator('.plugin-search-result').click()
  await expect(page.getByRole('dialog', { name: '目录整理', exact: true })).toContainText('运行时尚未开放')
  await page.keyboard.press('Escape')
  await page.getByRole('button', { name: '插件仓库', exact: true }).first().click()
  await page.getByRole('switch', { name: '启用仓库 Aether 社区', exact: true }).click()
  await expect(page.getByRole('switch', { name: '启用仓库 Aether 社区', exact: true })).toHaveAttribute('aria-checked', 'false')
  await page.getByRole('dialog', { name: '插件仓库', exact: true }).getByRole('button', { name: '关闭', exact: true }).click()
  await expect(page.locator('.market-grid .plugin-card')).toHaveCount(0)
  expect(await page.locator('.page-content').evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
})

test('repository invalid URL stays editable and reports a bubble', async ({ page }) => {
  await fixture(page)
  await page.goto('/tools/market')
  await page.getByRole('button', { name: '插件仓库', exact: true }).first().click()
  await page.getByRole('button', { name: '添加仓库', exact: true }).click()
  const modal = page.getByRole('dialog', { name: '添加插件仓库', exact: true })
  await modal.getByLabel('仓库地址', { exact: true }).fill('https://example.com/plugins')
  await modal.getByRole('button', { name: '保存', exact: true }).click()
  await expect(modal).toBeVisible()
  await expect(page.locator('.toast').filter({ hasText: 'GitHub' })).toBeVisible()
})
