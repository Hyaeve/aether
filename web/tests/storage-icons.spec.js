import { test, expect } from '@playwright/test'

test('saved storage cards use comparable local and WebDAV icon sizes', async ({ page }, info) => {
  await page.route('**/api/auth/status', route => route.fulfill({ json: { initialized: true, authenticated: true } }))
  await page.route('**/api/state', route => route.fulfill({ json: {
    username: 'icon-test', tasks: [], settings: {}, cache: {}, traffic: {}, logs: [],
    storages: ['local', 'webdav', 'quark'].map(type => ({ id: type, type, name: type, enabled: true, config: {} }))
  } }))
  await page.goto('/storage')
  await expect(page.locator('.storage-card')).toHaveCount(3)
  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: 900 })
    for (const type of ['local', 'webdav']) {
      const card = page.locator('.storage-card').filter({ has: page.getByRole('heading', { name: type, exact: true }) })
      const icon = card.locator('.provider-icon svg')
      await expect(icon).toHaveCSS('width', type === 'local' ? '39px' : '44px')
      await expect(icon).toHaveCSS('height', type === 'local' ? '39px' : '44px')
      await expect(card).toHaveCSS('height', '84px')
      expect(await card.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
    }
    await page.screenshot({ path: info.outputPath(`storage-icons-${width}.png`) })
  }
})
