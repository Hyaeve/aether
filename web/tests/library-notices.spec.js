import { test, expect } from '@playwright/test'
import { episodeRanges, libraryNoticeText } from '../src/library-notices.js'
import { createMeteorBatch, rockPosition } from '../src/meteor.js'

test('episode ranges preserve gaps and meteor batches cover the right side', () => {
  expect(episodeRanges([1, 3, 4, 5, 7, 3])).toBe('1、3-5、7')
  expect(episodeRanges(Array.from({ length: 10 }, (_, i) => i + 1))).toBe('1-10')
  expect(libraryNoticeText({ name: 'Arrival', mediaType: 'movie' })).toBe('电影 · Arrival')
  expect(libraryNoticeText({ name: 'Pilot', mediaType: 'episode', series: 'Show' })).toContain('季数未知 · 集数未知')
  for (const value of [0, .25, .5, .99]) {
    const batch = createMeteorBatch(() => value)
    expect(batch[0].x).toBeLessThan(.6)
    expect(batch.at(-1).x).toBeGreaterThanOrEqual(.58)
    expect(batch.at(-1).x - batch[0].x).toBeGreaterThan(.4)
  }
  const first = rockPosition(0, 0, 1000, 800), later = rockPosition(0, 30, 1000, 800)
  expect(later.x).not.toBe(first.x)
  expect(later.y).toBeGreaterThan(first.y)
  for(let i=0;i<7;i++) for(const time of [0,30,1000]) { const p=rockPosition(i,time,1000,800);expect(p.x).toBeGreaterThan(0);expect(p.x).toBeLessThan(1000);expect(p.y).toBeGreaterThan(0);expect(p.y).toBeLessThan(800);if(i%3===0)expect(p.x).toBeLessThan(250);if(i%3===1)expect(p.y).toBeGreaterThan(650);if(i%3===2)expect(p.x).toBeGreaterThan(750) }
})

test('activity popover shows media season and compact episode ranges', async ({ page }, info) => {
  const now = Date.now()
  await page.route('**/api/auth/status', r => r.fulfill({ json: { initialized: true, authenticated: true } }))
  await page.route('**/api/state', r => r.fulfill({ json: { username: 'notices', storages: [], tasks: [], settings: {}, cache: {}, traffic: {}, logs: [], libraryNotices: [
    { id: 'show', name: '单集名', series: '漫长的季节', mediaType: 'episode', season: 1, episodes: [1, 3, 4, 5, 7], time: new Date(now - 60000).toISOString() },
    { id: 'film', name: '降临', mediaType: 'movie', time: new Date(now - 120000).toISOString() }
  ] } }))
  await page.goto('/dashboard')
  await page.getByRole('button', { name: '任务通知', exact: true }).click()
  await expect(page.locator('.notification-dropdown')).toContainText('电视剧 · 漫长的季节 · 第1季 · 1、3-5、7集')
  await expect(page.locator('.notification-dropdown')).toContainText('电影 · 降临')
  await expect.poll(() => page.locator('.emby-notice-icon img').evaluateAll(imgs => imgs.length > 0 && imgs.every(i => i.complete && i.naturalWidth > 0))).toBe(true)
  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: 900 })
    expect(await page.locator('.notification-dropdown').evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
    await page.screenshot({ path: info.outputPath(`notice-${width}.png`) })
  }
})
