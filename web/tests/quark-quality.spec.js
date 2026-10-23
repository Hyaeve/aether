import { test, expect } from '@playwright/test'
import { captureScreenshot } from './helpers/screenshot.js'

for (const width of [1440, 390]) {
  for (const theme of ['light', 'dark']) test(`Quark has four quality preferences at ${width}px ${theme}`, async ({ page }, info) => {
    await page.setViewportSize({ width, height: 900 })
    let config = { enabled: true, mode: 'adaptive', quality: '4k', allowDolby: false, uaListMode: 'proxy_list', ua: '' }
    const saved = []
    await page.route('**/api/**', route => {
      const path = new URL(route.request().url()).pathname
      let json = {}
      if (path === '/api/auth/status') json = { initialized: true, authenticated: true }
      if (path === '/api/state') json = { username: 'quark-quality', storages: [{ id: 'quark', type: 'quark', name: '夸克影音', enabled: true, config: {} }], tasks: [], settings: {}, cache: {}, traffic: {}, links: [] }
      if (path === '/api/quark-takeover') json = { enabled: true, bindings: [{ id: 'quark', name: '夸克影音', nickname: '测试账号', enabled: true, valid: true }] }
      if (path === '/api/quark-takeover/quark') {
        if (route.request().method() === 'PUT') { config = route.request().postDataJSON(); saved.push(config) }
        json = { config, authorized: true }
      }
      return route.fulfill({ json })
    })
    await page.goto('/tools')
    await page.evaluate(theme => document.documentElement.dataset.theme = theme, theme)
    const open = async () => {
      await page.getByRole('button', { name: '夸克 STRM 接管', exact: true }).getByText('夸克 STRM 接管', { exact: true }).click()
      return page.getByRole('dialog', { name: '夸克 STRM 接管', exact: true })
    }
    let modal = await open()
    let quality = modal.getByRole('button', { name: '最高画质', exact: true })
    await expect(quality).toHaveText('4K')
    await quality.click()
    const options = page.getByRole('listbox', { name: '最高画质选项', exact: true })
    await expect(options.getByRole('option')).toHaveText(['流畅', '高清', '超清', '4K'])
    await expect(options.getByRole('option', { name: '4K', exact: true })).toHaveAttribute('aria-selected', 'true')
    expect(await quality.evaluate(el => el.getBoundingClientRect().height)).toBe(40)
    expect(await options.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
    expect(await modal.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
    await captureScreenshot(page, { path: info.outputPath(`quark-quality-${width}-${theme}.png`) })
    for (const [index, [label, value]] of [['流畅', 'low'], ['高清', 'high'], ['超清', 'super'], ['4K', '4k']].entries()) {
      if (index) await quality.click()
      await page.getByRole('option', { name: label, exact: true }).click()
      await expect(page.getByRole('listbox')).toHaveCount(0)
      await expect(quality).toHaveText(label)
      await modal.getByRole('button', { name: '保存设置', exact: true }).click()
      await expect.poll(() => saved.length).toBe(index + 1)
      expect(saved[index]).toMatchObject({ quality: value, allowDolby: false, mode: 'adaptive' })
      await expect(modal.getByRole('button', { name: '保存设置', exact: true })).toBeDisabled()
    }
    await modal.getByRole('switch', { name: '允许杜比视界', exact: true }).check()
    await modal.getByRole('button', { name: '保存设置', exact: true }).click()
    await expect.poll(() => saved.length).toBe(5)
    expect(saved[4]).toMatchObject({ quality: '4k', allowDolby: true })
    await modal.getByRole('button', { name: '关闭', exact: true }).click()
    await expect(modal).toHaveCount(0)
    modal = await open()
    quality = modal.getByRole('button', { name: '最高画质', exact: true })
    await expect(quality).toHaveText('4K')
    await expect(modal.getByRole('switch', { name: '允许杜比视界', exact: true })).toBeChecked()
    await quality.focus()
    await page.keyboard.press('ArrowDown')
    await page.keyboard.press('Home')
    await page.keyboard.press('Enter')
    await expect(quality).toHaveText('流畅')
    await expect(quality).toBeFocused()
  })
}
