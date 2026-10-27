import { test, expect, webkit } from '@playwright/test'
import { captureScreenshot } from './helpers/screenshot.js'

async function fixture(page) {
  const config = { qqBot: { enabled: false, appId: '', groupId: '', hasSecret: false }, feishu: { enabled: false, hasWebhook: false, hasSecret: false }, telegram: { enabled: false, token: '', chatId: '', userIds: [] }, targets: [], wecom: {} }
  const writes = [], tests = [], secrets = {}
  await page.route('**/api/**', route => {
    const req = route.request(), path = new URL(req.url()).pathname
    let json = {}
    if (path === '/api/auth/status') json = { initialized: true, authenticated: true }
    if (path === '/api/state') json = { username: 'new-notification-channels', storages: [], tasks: [], settings: {}, cache: {}, traffic: {} }
    if (path === '/api/notification-service') json = config
    const channel = path.split('/')[3]
    if (['qqBot', 'feishu'].includes(channel)) {
      if (path.endsWith('/secret')) {
        const input = req.postDataJSON(), value = secrets[`${channel}.${input.field}`] || ''
        json = input.metadataOnly ? { length: value.length } : { value }
      } else if (path.endsWith('/test')) {
        tests.push(channel); json = { ok: true }
      } else if (req.method() === 'PUT') {
        const value = req.postDataJSON()[channel]
        writes.push({ channel, value })
        for (const field of channel === 'qqBot' ? ['secret'] : ['secret', 'webhook']) {
          if (value.clearSecrets.includes(field)) delete secrets[`${channel}.${field}`]
          else if (value[field]) secrets[`${channel}.${field}`] = value[field]
        }
        config[channel] = { ...value, secret: '', webhook: '', hasSecret: !!secrets[`${channel}.secret`], hasWebhook: !!secrets[`${channel}.webhook`] }
        json = config
      }
    }
    return route.fulfill({ json })
  })
  return { writes, tests, config }
}

for (const width of [1920, 1440, 1024, 390]) for (const theme of ['light', 'dark']) test(`notification channels layout ${width} ${theme}`, async ({ page }, info) => {
  await fixture(page)
  await page.setViewportSize({ width, height: 1100 })
  await page.goto('/notifications')
  await page.evaluate(theme => document.documentElement.dataset.theme = theme, theme)
  const panels = page.locator('.notification-config-panels')
  await expect(panels.locator(':scope > form')).toHaveCount(4)
  expect(await panels.locator(':scope > form').evaluateAll(forms => forms.map(form => form.getAttribute('aria-label')))).toEqual(['QQ Bot', '飞书通知', 'TG Bot', '企业微信'])
  const boxes = await panels.locator(':scope > form').evaluateAll(forms => forms.map(form => {
    const r = form.getBoundingClientRect()
    return { x: r.x, y: r.y, width: r.width, bottom: r.bottom }
  }))
  if (width >= 1440) {
    expect(Math.max(...boxes.slice(0, 3).map(box => box.y)) - Math.min(...boxes.slice(0, 3).map(box => box.y))).toBeLessThan(1)
    expect(boxes[0].x).toBeLessThan(boxes[1].x); expect(boxes[1].x).toBeLessThan(boxes[2].x)
    expect(boxes[3].width).toBeCloseTo(2 * boxes[0].width + 24, 0)
    expect(boxes[3].y).toBeGreaterThan(boxes[2].bottom)
  } else if (width === 1024) {
    expect(boxes[0].y).toBe(boxes[1].y)
    expect(boxes[2].y).toBeGreaterThan(boxes[0].y)
    expect(boxes[3].width).toBeCloseTo(2 * boxes[0].width + 24, 0)
  } else {
    expect(boxes.every((box, i) => i === 0 || box.y > boxes[i - 1].y)).toBe(true)
  }
  const wecom = page.getByRole('form', { name: '企业微信', exact: true })
  await expect(wecom.locator('.lucide-circle-help-icon')).toHaveCount(0)
  const columns = await wecom.locator('.wecom-fields').evaluate(el => getComputedStyle(el).gridTemplateColumns.split(' ').length)
  expect(columns).toBe(width >= 1600 ? 3 : width > 760 ? 2 : 1)
  for (const input of await panels.locator('input:not([type=checkbox])').all()) await expect(input).toHaveCSS('height', '40px')
  expect(await panels.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await captureScreenshot(page, { path: info.outputPath(`channels-${width}-${theme}.png`) })
})

test('QQ Bot and Feishu save, reveal, test and clear independently across reloads', async ({ page }) => {
  const f = await fixture(page)
  await page.goto('/notifications')
  const qq = page.getByRole('form', { name: 'QQ Bot', exact: true }), feishu = page.getByRole('form', { name: '飞书通知', exact: true })
  await qq.getByLabel('AppID', { exact: true }).fill('123456789')
  await qq.getByLabel('AppSecret', { exact: true }).fill('qq-private-secret')
  await qq.getByLabel('群 OpenID', { exact: true }).fill('group_openid_example')
  await qq.getByRole('switch').check()
  await qq.getByRole('button', { name: '保存设置', exact: true }).click()
  await expect.poll(() => f.writes.length).toBe(1)
  await feishu.getByLabel('Webhook 地址', { exact: true }).fill('https://open.feishu.cn/open-apis/bot/v2/hook/private-hook')
  await feishu.getByLabel('签名密钥', { exact: true }).fill('signing-secret')
  await feishu.getByRole('switch').check()
  await feishu.getByRole('button', { name: '保存设置', exact: true }).click()
  await expect.poll(() => f.writes.length).toBe(2)
  for (const form of [qq, feishu]) await form.getByRole('button', { name: '测试通知', exact: true }).click()
  await expect.poll(() => f.tests).toEqual(['qqBot', 'feishu'])
  await page.reload()
  await expect(qq.getByLabel('AppID', { exact: true })).toHaveValue('123456789')
  await expect(qq.getByLabel('AppSecret', { exact: true })).toHaveAttribute('type', 'password')
  await qq.getByRole('button', { name: '显示内容', exact: true }).click()
  await expect(qq.getByLabel('AppSecret', { exact: true })).toHaveValue('qq-private-secret')
  await feishu.locator('.secret-input').first().getByRole('button', { name: '显示内容', exact: true }).click()
  await expect(feishu.getByLabel('Webhook 地址', { exact: true })).toHaveValue('https://open.feishu.cn/open-apis/bot/v2/hook/private-hook')
  await feishu.getByRole('switch').uncheck()
  await feishu.getByLabel('Webhook 地址', { exact: true }).fill('')
  await feishu.getByRole('button', { name: '保存设置', exact: true }).click()
  await expect.poll(() => f.config.feishu.hasWebhook).toBe(false)
  expect(f.config.qqBot.hasSecret).toBe(true)
  expect(f.writes.at(-1).value.clearSecrets).toEqual(['webhook'])
  expect(await page.evaluate(() => JSON.stringify(localStorage))).not.toContain('qq-private-secret')
})

test('notification channel failures use a toast and keep the form editable', async ({ page }) => {
  await fixture(page)
  await page.route('**/api/notification-service/qqBot', route => route.fulfill({ status: 400, json: { error: '启用 QQ Bot 须填写群 OpenID' } }))
  await page.goto('/notifications')
  const qq = page.getByRole('form', { name: 'QQ Bot', exact: true })
  await qq.getByLabel('AppID', { exact: true }).fill('123456789')
  await qq.getByRole('button', { name: '保存设置', exact: true }).click()
  await expect(page.locator('.toast.error').last()).toContainText('群 OpenID')
  await expect(qq.getByLabel('AppID', { exact: true })).toHaveValue('123456789')
  await expect(qq.getByRole('button', { name: '保存设置', exact: true })).toBeEnabled()
})

test.describe('notification channels on mobile WebKit', () => {
  for (const size of [{ width: 375, height: 812 }, { width: 844, height: 390 }]) test(`QQ, Feishu and WeCom remain usable ${size.width}`, async ({ baseURL }, info) => {
    const browser = await webkit.launch()
    const context = await browser.newContext({ baseURL, serviceWorkers: 'block', isMobile: true, hasTouch: true, deviceScaleFactor: 3, viewport: size })
    const page = await context.newPage()
    try {
      await fixture(page)
      await page.setViewportSize(size)
      await page.emulateMedia({ reducedMotion: 'reduce' })
      await page.goto('/notifications')
      const qq = page.getByRole('form', { name: 'QQ Bot', exact: true })
      await qq.getByLabel('AppID', { exact: true }).fill('123456789')
      await qq.getByRole('switch').check()
      await expect(qq.getByRole('switch')).toBeChecked()
      await expect(page.getByRole('form', { name: '飞书通知', exact: true })).toBeVisible()
      const wecom = page.getByRole('form', { name: '企业微信', exact: true })
      await expect(wecom.locator('.lucide-circle-help-icon')).toHaveCount(0)
      await wecom.getByLabel('企业 ID', { exact: true }).fill('ww-example')
      await expect(wecom.getByLabel('企业 ID', { exact: true })).toHaveValue('ww-example')
      expect(await wecom.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
      await captureScreenshot(page, { path: info.outputPath(`notification-webkit-${size.width}.png`) })
    } finally {
      await context.close()
      await browser.close()
    }
  })
})
