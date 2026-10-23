import { test, expect } from '@playwright/test'

for (const fails of [false, true]) test(`mobile share over 5000 files ${fails ? 'stops on a failed batch' : 'submits every batch in order'}`, async ({ page }) => {
  const files = Array.from({ length: 5201 }, (_, i) => ({ id: String(i), name: `${i}.mkv`, isDir: false }))
  const batches = Array.from({ length: Math.ceil(files.length / 499) }, (_, i) => files.slice(i * 499, (i + 1) * 499).map(file => file.name))
  const submitted = []
  await page.route('**/api/**', route => {
    const request = route.request(), path = new URL(request.url()).pathname
    let json = {}
    if (path === '/api/auth/status') json = { initialized: true, authenticated: true }
    else if (path === '/api/state') json = { username: 'large-mobile', storages: [{ id: 'mobile', type: 'mobile', name: '移动影视', enabled: true, config: { mode: 'native' } }], tasks: [], settings: {}, cache: {}, traffic: {} }
    else if (path === '/api/files') json = []
    else if (path === '/api/files/share-preferences') json = { last: {}, directories: {} }
    else if (path === '/api/files/share/info') json = { directory: '' }
    else if (path === '/api/files/share/preview') json = { preview: 'large', items: files, batches: batches.length, batchNames: batches }
    else if (path === '/api/files/share/batch') {
      const body = request.postDataJSON()
      submitted.push(body)
      if (fails && body.batch === 3) return route.fulfill({ status: 400, json: { error: '本批次结果未确认；请检查网盘结果，勿直接重复提交' } })
      json = { status: 'submitted', taskId: `job-${body.batch}`, done: body.batch + 1, total: batches.length }
    }
    return route.fulfill({ json })
  })
  await page.goto('/files?storage=mobile')
  await page.getByRole('button', { name: '工具', exact: true }).click()
  await page.getByRole('menuitem', { name: '分享转存', exact: true }).click()
  await page.getByRole('textbox', { name: '分享链接', exact: true }).fill('https://yun.139.com/shareweb/#/w/i/code')
  await page.getByRole('button', { name: '转存', exact: true }).click()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  if (fails) await expect(page.locator('.toast.error')).toContainText('勿直接重复提交')
  else await expect(page.locator('.toast-stack')).toContainText('转存已提交：移动影视')
  expect(submitted.map(body => body.batch)).toEqual(Array.from({ length: fails ? 4 : 11 }, (_, i) => i))
  expect(submitted.every(body => body.storageId === 'mobile' && body.preview === 'large' && body.parent === '/')).toBe(true)
  await expect(page.locator('.toast-stack')).not.toContainText('转存成功')
})
