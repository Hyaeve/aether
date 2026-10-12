import { test, expect } from '@playwright/test'
import { readFileSync, existsSync } from 'node:fs'
import path from 'node:path'

test('real CAS and ED2K imports create portable signed files and expose completed progress', async ({ page }, info) => {
  const auth = await (await page.request.get('/api/auth/status')).json()
  const endpoint = auth.initialized ? '/api/auth/login' : '/api/auth/setup'
  const login = await page.request.post(endpoint, { data: { username: 'my-aether-owner', password: 'a1' } })
  expect(login.ok()).toBe(true)
  const current = await (await page.request.get('/api/state')).json()
  const target = current.strmRoot
  for (const kind of ['cas', 'ed2k']) {
    await page.goto(`/tasks/${kind}`)
    await page.getByRole('button', { name: '添加任务', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: `添加 ${kind.toUpperCase()} 任务`, exact: true })
    const names = [`Imported-${kind}-first`, `Imported-${kind}-second`]
    const text = kind === 'cas'
      ? names.map(name => JSON.stringify({ name: `${name}.mkv`, size: 12, sha256: 'a'.repeat(64), md5: '' })).join('\n')
      : names.map(name => `ed2k://|file|${name}.mkv|12|${'b'.repeat(32)}|/`).join('\n')
    await dialog.getByLabel('生成目录', { exact: true }).fill(target)
    if (kind === 'cas') await dialog.getByRole('textbox', { name: 'CAS 信息' }).fill(text)
    else {
      await dialog.getByRole('tab', { name: '文件解析' }).click()
      await dialog.locator('input[type=file]').setInputFiles({ name: 'import.txt', mimeType: 'text/plain', buffer: Buffer.from(text) })
    }
    await dialog.getByRole('button', { name: '解析', exact: true }).click()
    await expect(dialog.locator('.generation-preview article strong')).toHaveText(names.map(name => `${name}.${kind}.strm`))
    await dialog.getByRole('button', { name: '加入生成队列' }).click()
    await expect(dialog).toHaveCount(0)
    await expect.poll(async () => (await (await page.request.get('/api/generation/jobs')).json()).find(job => job.kind === kind)?.status).toBe('success')
    const job = (await (await page.request.get('/api/generation/jobs')).json()).find(job => job.kind === kind)
    expect([job.processed, job.total, job.success, job.failed]).toEqual([2, 2, 2, 0])
    await expect(page.locator('.generation-job-list')).toContainText('生成完成：2 项')
    for (const name of names) {
      const filename = path.join(target, `${name}.${kind}.strm`)
      expect(existsSync(filename)).toBe(true)
      const link = new URL(readFileSync(filename, 'utf8').trim())
      const claim = JSON.parse(Buffer.from(link.pathname.slice('/stream/'.length), 'base64url').toString())
      expect(claim.s || '').toBe('')
      expect(claim[kind].name).toBe(`${name}.mkv`)
      expect(link.searchParams.get('sign')).toBeTruthy()
      if (kind === 'cas') expect([claim.cas.sha256, claim.cas.md5]).toEqual(['a'.repeat(64), ''])
      else expect(claim.ed2k.hash).toBe('b'.repeat(32))
    }
    await page.screenshot({ path: info.outputPath(`${kind}-real-completed.png`) })
  }
})
