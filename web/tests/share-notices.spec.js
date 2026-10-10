import { test, expect } from '@playwright/test'

for (const provider of ['quark', '115', 'mobile']) {
  test(`${provider} share result appears immediately, survives refresh and opens its log`, async ({ page }, info) => {
    const storage = { id: 'pool', name: '转存资料库', type: provider, enabled: true, config: { mode: 'native' } }
    let accepted = false, confirmed = false
    const now = new Date().toISOString()
    const message = provider === 'quark' ? '转存成功 · 2 项 · 跳过 1 项' : '转存已提交 · 2 项，请检查网盘结果 · 跳过 1 项'
    await page.route('**/api/**', r => {
      const path = new URL(r.request().url()).pathname
      let data = {}
      if (path === '/api/auth/status') data = { initialized: true, authenticated: true }
      if (path === '/api/state') data = { username: `share-${provider}`, storages: [storage], tasks: [], settings: {}, cache: {}, shareNotices: accepted && (provider !== 'quark' || confirmed) ? [{ id: 'share-result', storageId: 'pool', name: '转存资料库 · 分享转存', provider, status: provider === 'quark' ? 'completed' : 'submitted', time: now, message }] : [] }
      if (path === '/api/files') data = []
      if (path === '/api/files/share/preview') data = { preview: 'plan', batches: 1 }
      if (path === '/api/files/share/batch') { accepted = true; data = { status: 'submitted', taskId: provider === 'quark' ? 'job' : '' } }
      if (path === '/api/files/share/status') { confirmed = true; data = { status: 'completed' } }
      if (path === '/api/logs') data = [{ module: 'files', level: 'info', time: now, message: `[share-result] 转存资料库 · 分享转存：${message}` }]
      return r.fulfill({ json: data })
    })
    await page.goto('/files?storage=pool')
    await page.getByRole('button', { name: '工具', exact: true }).click()
    await page.getByRole('menuitem', { name: '分享转存', exact: true }).click()
    await page.getByLabel('分享链接', { exact: true }).fill(({ quark: 'https://pan.quark.cn/s/test', '115': 'https://115cdn.com/s/test?password=q526', mobile: 'https://yun.139.com/shareweb/#/w/i/test' })[provider])
    await page.getByRole('button', { name: '开始转存', exact: true }).click()
    await expect(page.locator('.toast')).toContainText(provider === 'quark' ? '转存已完成' : '全部批次已提交')
    await page.getByRole('button', { name: '任务通知', exact: true }).click()
    const list = page.getByRole('region', { name: '任务通知列表' })
    await expect(list).toContainText(message)
    await expect(list.locator('.notice-provider .provider-icon')).toBeVisible()
    await page.screenshot({ path: info.outputPath(`${provider}-share-notice.png`) })
    await page.reload()
    await page.getByRole('button', { name: '任务通知', exact: true }).click()
    await expect(list).toContainText(message)
    await list.getByRole('button', { name: /转存资料库 · 分享转存/ }).click()
    await expect(page).toHaveURL(/\/logs\?.*module=files/)
    expect(new URL(page.url()).searchParams.get('q')).toBe('[share-result]')
    await expect(page.locator('.log-viewport')).toContainText('[share-result]')
    await page.getByRole('button', { name: '任务通知', exact: true }).click()
    await page.getByRole('button', { name: '清除通知', exact: true }).click()
    await expect(list).toContainText('暂无通知')
    await page.reload()
    await page.getByRole('button', { name: '任务通知', exact: true }).click()
    await expect(list).toContainText('暂无通知')
  })
}
