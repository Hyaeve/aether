import { test, expect } from '@playwright/test'
import { captureScreenshot } from './helpers/screenshot.js'

const groups = {
  视频: 'mp4;mkv;avi;mov;wmv;flv;webm;m4v;ts;m2ts;mkvb;rm;3gp;iso',
  音频: 'mp3;flac;wav;aac;m4a;ogg;wma;ape;alac;opus',
  图片: 'jpg;jpeg;png;webp',
  数据: 'nfo;ass;srt;ssa;sub',
  文档: 'pdf;doc;docx;xls;xlsx;ppt;pptx;txt;md;csv;epub;mobi;cbz;cbr;zip;rar'
}

async function fixture(page) {
  await page.route('**/api/**', route => {
    const path = new URL(route.request().url()).pathname
    let json = {}
    if (path === '/api/auth/status') json = { initialized: true, authenticated: true }
    if (path === '/api/state') json = { username: 'filter-toggle', storages: [{ id: 'pan', type: '115', name: '115影视', enabled: true, config: {} }], tasks: [], settings: {}, cache: {}, traffic: {} }
    if (['/api/transfers', '/api/backup-rules'].includes(path)) json = []
    if (path === '/api/115-rapid') json = { manual: { source: '', storageId: '', target: '', extensions: '', successAction: 'delete', failureAction: 'skip' }, watches: [] }
    if (path === '/api/115-rapid/records') json = { items: [], running: false }
    return route.fulfill({ json })
  })
}

for (const width of [1440, 390]) for (const theme of ['light', 'dark']) test(`filter extension groups toggle independently in backup and monitor ${width} ${theme}`, async ({ page }, info) => {
  await page.setViewportSize({ width, height: 900 })
  await fixture(page)
  for (const kind of ['backup', 'monitor']) {
    await page.goto(kind === 'backup' ? '/transfer/backup' : '/tools/115-rapid')
    await page.evaluate(theme => document.documentElement.dataset.theme = theme, theme)
    if (kind === 'backup') await page.locator('.task-heading').getByRole('button', { name: '添加规则', exact: true }).click()
    else await page.getByRole('button', { name: '添加监控', exact: true }).click()
    const modal = page.getByRole('dialog', { name: kind === 'backup' ? '添加备份规则' : '添加监控', exact: true })
    if (kind === 'backup') await modal.getByRole('button', { name: '筛选规则', exact: true }).click()
    await modal.getByRole('button', { name: '添加规则', exact: true }).click()
    await page.getByRole('dialog', { name: '选择规则类型', exact: true }).getByRole('button', { name: '扩展名', exact: true }).click()
    const rule = modal.locator('.backup-filter').first()
    const tokens = rule.locator('.backup-token > span')
    await rule.getByRole('textbox', { name: '匹配内容 1', exact: true }).fill('my-custom;MP4')
    await rule.getByRole('textbox', { name: '匹配内容 1', exact: true }).press('Enter')
    for (const [name, value] of Object.entries(groups)) {
      const button = rule.getByRole('button', { name: `${name}扩展名`, exact: true })
      const before = await tokens.allTextContents()
      await expect(button).toHaveAttribute('aria-pressed', 'false')
      await button.click()
      await expect(button).toHaveAttribute('aria-pressed', 'true')
      await expect(tokens).toHaveText([...new Set([...before, ...value.split(';')])])
      await button.click()
      await expect(button).toHaveAttribute('aria-pressed', 'false')
      await expect(tokens).toHaveText(before.filter(extension => !value.split(';').includes(extension)))
    }
    await rule.getByRole('button', { name: '音频扩展名', exact: true }).click()
    await rule.getByRole('button', { name: '图片扩展名', exact: true }).click()
    await rule.getByRole('button', { name: '音频扩展名', exact: true }).click()
    await expect(tokens).toHaveText(['my-custom', ...groups.图片.split(';')])
    await expect(rule.getByRole('button', { name: '音频扩展名', exact: true })).toHaveAttribute('aria-pressed', 'false')
    await expect(rule.getByRole('button', { name: '图片扩展名', exact: true })).toHaveAttribute('aria-pressed', 'true')
    expect(await modal.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
    await captureScreenshot(page, { path: info.outputPath(`${kind}-extension-toggle-${width}-${theme}.png`) })
    await modal.getByRole('button', { name: '关闭', exact: true }).click()
  }
})
