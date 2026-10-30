import { test, expect } from '@playwright/test'
import { captureScreenshot } from './helpers/screenshot.js'

async function fixture(page, theme) {
  await page.addInitScript(value => {
    if (!localStorage.getItem('aether-theme')) localStorage.setItem('aether-theme', value)
  }, theme)
  await page.route('**/api/**', route => {
    const path = new URL(route.request().url()).pathname
    let json
    if (path === '/api/auth/status') json = { initialized: true, authenticated: true }
    else if (path === '/api/state') json = { username: 'about-sidebar', storages: [], tasks: [], settings: {}, cache: {}, traffic: {} }
    else if (path === '/api/traffic') json = { uploadRate: 0, downloadRate: 0 }
    else if (path === '/api/strm-replace') json = { jobs: [] }
    else if (path === '/api/plugins/proxy') json = { enabled: false, address: '', username: '', password: '' }
    else return route.fulfill({ status: 500, json: { error: `Unmocked API: ${path}` } })
    return route.fulfill({ json })
  })
}

async function openSidebar(page) {
  const toggle = page.getByRole('button', { name: '打开导航', exact: true })
  if (await toggle.isVisible() && !await page.locator('.nav-overlay').count()) await toggle.click()
  await expect(page.locator('.sidebar')).toBeVisible()
}

async function closeSidebar(page) {
  if (await page.locator('.nav-overlay').count()) {
    await page.locator('.nav-overlay').click({ position: { x: page.viewportSize().width - 10, y: 20 } })
    await expect(page.locator('.nav-overlay')).toHaveCount(0)
  }
}

function palette(page) {
  return page.locator('.sidebar').evaluate(el => {
    const style = (selector, properties) => {
      const css = getComputedStyle(selector ? el.querySelector(selector) : el)
      return properties.map(property => css[property])
    }
    return {
      surface: style(null, ['backgroundColor', 'backgroundImage', 'color', 'colorScheme']),
      brand: style('.brand > span', ['color']),
      subtitle: style('.brand small', ['color', 'opacity']),
      group: style('.nav-group-title', ['color']),
      divider: getComputedStyle(el.querySelector('.nav-group-title'), '::after').backgroundColor,
      normal: style('.nav-group button', ['color', 'backgroundColor']),
      selected: style('.nav-group button.active', ['color', 'backgroundColor', 'boxShadow'])
    }
  })
}

async function hoverPalette(page) {
  const item = page.getByRole('navigation', { name: '主导航', exact: true }).getByRole('link', { name: '信息总览', exact: true })
  await item.hover()
  await expect(item).toHaveCSS('color', 'rgb(255, 255, 255)')
  await expect(item).toHaveCSS('background-color', 'rgb(65, 77, 121)')
  return item.evaluate(el => {
    const css = getComputedStyle(el)
    return { color: css.color, background: css.backgroundColor }
  })
}

async function checkTheme(page, info, width, height, theme) {
  await fixture(page, theme)
  await page.setViewportSize({ width, height })
  const proxyResponse = page.waitForResponse('**/api/plugins/proxy')
  await page.goto('/settings/account')
  expect((await proxyResponse).status()).toBe(200)
  await expect(page.locator('html')).toHaveAttribute('data-theme', theme)
  await openSidebar(page)
  await page.locator('.sidebar .brand').hover()
  const original = await palette(page)
  await page.evaluate(() => document.documentElement.dataset.theme = 'dark')
  await expect(page.locator('.sidebar')).toHaveCSS('color', 'rgb(219, 225, 245)')
  await expect(page.locator('.sidebar .nav-group button').first()).toHaveCSS('color', 'rgb(219, 225, 245)')
  await expect(page.locator('.sidebar .nav-group button.active')).toHaveCSS('color', 'rgb(255, 255, 255)')
  await expect(page.locator('.sidebar .nav-group button.active')).toHaveCSS('background-color', 'rgb(77, 91, 140)')
  const night = await palette(page)
  const nightHover = await hoverPalette(page)
  await page.locator('.sidebar .brand').hover()
  await page.evaluate(value => document.documentElement.dataset.theme = value, theme)
  await closeSidebar(page)
  await page.getByRole('link', { name: '关于以太', exact: true }).click()
  await expect(page.locator('.about-page')).toBeVisible()
  await expect(page.locator('html')).toHaveAttribute('data-theme', theme)
  expect(await page.evaluate(() => localStorage.getItem('aether-theme'))).toBe(theme)
  await openSidebar(page)
  await page.locator('.sidebar .brand').hover()
  await expect.poll(() => palette(page)).toEqual(night)
  await expect.poll(() => hoverPalette(page)).toEqual(nightHover)
  const settings = page.getByRole('navigation', { name: '主导航', exact: true }).getByRole('link', { name: '系统设置', exact: true })
  await expect(settings).toHaveAttribute('aria-current', 'page')
  await settings.focus()
  await expect(settings).toBeFocused()
  await page.locator('.sidebar .brand').hover()
  await page.locator('.sidebar nav').evaluate(el => { el.scrollTop = 0 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await captureScreenshot(page, { path: info.outputPath(`about-sidebar-${width}-${theme}.png`) })
  await closeSidebar(page)
  await page.getByRole('link', { name: '账号与安全', exact: true }).click()
  await expect(page.locator('.about-page')).toHaveCount(0)
  await expect(page.locator('html')).toHaveAttribute('data-theme', theme)
  await openSidebar(page)
  await page.locator('.sidebar .brand').hover()
  await expect.poll(() => palette(page)).toEqual(original)
  expect(await page.evaluate(() => localStorage.getItem('aether-theme'))).toBe(theme)
}

for (const [width, height] of [[1440, 900], [375, 812], [844, 390]]) for (const theme of ['light', 'dark']) {
  test(`about sidebar matches night without changing ${theme} preference at ${width}px`, async ({ page }, info) => {
    await checkTheme(page, info, width, height, theme)
  })
}

test('about sidebar stays night through theme controls and system scheme changes', async ({ page }) => {
  await fixture(page, 'light')
  await page.emulateMedia({ colorScheme: 'light' })
  await page.goto('/settings/about')
  await expect(page.locator('.about-page')).toBeVisible()
  const nightText = 'rgb(219, 225, 245)'
  await expect(page.locator('.sidebar')).toHaveCSS('color', nightText)
  await page.getByRole('button', { name: '主题：日光', exact: true }).click()
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
  await page.getByRole('button', { name: '主题：夜间', exact: true }).click()
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light')
  expect(await page.evaluate(() => localStorage.getItem('aether-theme'))).toBe('system')
  await expect(page.locator('.sidebar')).toHaveCSS('color', nightText)
  await page.emulateMedia({ colorScheme: 'dark' })
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
  await expect(page.locator('.sidebar')).toHaveCSS('color', nightText)
  await page.getByRole('link', { name: '账号与安全', exact: true }).click()
  await page.emulateMedia({ colorScheme: 'light' })
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light')
  await expect(page.locator('.sidebar')).toHaveCSS('color', 'rgb(65, 75, 104)')
  expect(await page.evaluate(() => localStorage.getItem('aether-theme'))).toBe('system')
  await page.goBack()
  await expect(page.locator('.about-page')).toBeVisible()
  await expect(page.locator('.sidebar')).toHaveCSS('color', nightText)
  await page.reload()
  await expect(page.locator('.about-page')).toBeVisible()
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light')
  await expect(page.locator('.sidebar')).toHaveCSS('color', nightText)
  await expect(page.getByRole('button', { name: '主题：跟随系统', exact: true })).toBeVisible()
  expect(await page.evaluate(() => localStorage.getItem('aether-theme'))).toBe('system')
})
