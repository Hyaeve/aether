import { test, expect } from '@playwright/test'

for (const legacy of [false, true]) {
  test(`favorite keeps its exact directory after nested navigation (legacy=${legacy})`, async ({ page }) => {
    const requests = []
    const trail = [{ id: '/', name: '资料' }, { id: 'folder-a', name: '收藏目录' }]
    await page.addInitScript(({ trail, legacy }) => {
      if (sessionStorage.getItem('favorite-fixture')) return
      sessionStorage.setItem('favorite-fixture', '1')
      localStorage.setItem('aether-files:favorite-test', JSON.stringify({
        open: true, mode: 'list',
        favorites: [{ storage: 'pool', id: 'folder-b', name: '收藏目录', history: legacy ? [...trail, { id: 'folder-b', name: '子目录' }, { id: 'folder-c', name: '孙目录' }] : trail }]
      }))
    }, { trail, legacy })
    await page.route('**/api/auth/status', r => r.fulfill({ json: { initialized: true, authenticated: true } }))
    await page.route('**/api/state', r => r.fulfill({ json: {
      username: 'favorite-test', storages: [{ id: 'pool', type: 'quark', name: '云盘', enabled: true, config: {} }],
      tasks: [], settings: {}, cache: {}, traffic: {}, logs: []
    } }))
    await page.route('**/api/files?**', r => {
      const path = new URL(r.request().url()).searchParams.get('path')
      requests.push(path)
      const tree = {
        '/': [{ id: 'folder-a', name: '资料', isDir: true }],
        'folder-a': [{ id: 'folder-b', name: '收藏目录', isDir: true }],
        'folder-b': [{ id: 'folder-c', name: '子目录', isDir: true }],
        'folder-c': [{ id: 'folder-d', name: '孙目录', isDir: true }],
        'folder-d': [{ id: 'file', name: 'deep.txt', isDir: false }]
      }
      return r.fulfill({ json: tree[path] || [] })
    })
    await page.goto('/files')
    const favorite = page.locator('.file-favorites').getByRole('button', { name: '收藏目录', exact: true })
    const breadcrumbs = page.getByRole('navigation', { name: '文件路径' })
    const savedTrail = () => page.evaluate(() => JSON.parse(localStorage.getItem('aether-files:favorite-test')).favorites[0].history)
    await favorite.click()
    await expect(page.locator('.file-view').getByRole('button', { name: '子目录', exact: true })).toBeVisible()
    await expect(breadcrumbs.locator('.path-measure > span')).toHaveText(['资料', '收藏目录'])
    await page.locator('.file-view').getByRole('button', { name: '子目录', exact: true }).dblclick()
    await page.locator('.file-view').getByRole('button', { name: '孙目录', exact: true }).dblclick()
    await expect(page.locator('.file-view')).toContainText('deep.txt')
    await expect.poll(savedTrail).toEqual(trail)
    await favorite.click()
    await expect(page.locator('.file-view').getByRole('button', { name: '子目录', exact: true })).toBeVisible()
    expect(requests.at(-1)).toBe('folder-b')
    await expect(breadcrumbs.locator('.path-measure > span')).toHaveText(['资料', '收藏目录'])
    await breadcrumbs.getByRole('button', { name: '资料', exact: true }).click()
    await expect(page.locator('.file-view').getByRole('button', { name: '收藏目录', exact: true })).toBeVisible()
    expect(requests.at(-1)).toBe('folder-a')
    await page.reload()
    await favorite.click()
    await expect(breadcrumbs.locator('.path-measure > span')).toHaveText(['资料', '收藏目录'])
    await expect(page.locator('.file-view').getByRole('button', { name: '子目录', exact: true })).toBeVisible()
    await expect.poll(savedTrail).toEqual(trail)
  })
}
