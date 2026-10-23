import { expect } from '@playwright/test'
import { mkdirSync, writeFileSync, readFileSync, existsSync } from 'node:fs'
import path from 'node:path'

export async function verifyRealRename(page) {
  const root = path.join(process.env.AETHER_E2E_ROOT, 'rename-live')
  mkdirSync(root, { recursive: true })
  for (const name of ['Alpha.txt', 'Middle.txt', 'Zulu.txt']) writeFileSync(path.join(root, name), `contents of ${name}`)
  const added = await page.request.post('/api/storages', { data: { name: 'Rename integration', type: 'local', enabled: true, config: { root } } })
  expect(added.ok()).toBe(true)
  const storage = await added.json()
  try {
  let reads = 0
  page.on('request', request => { if (new URL(request.url()).pathname === '/api/files') reads++ })
  await page.addInitScript(() => localStorage.setItem('aether-files-view', 'list'))
  await page.goto(`/files?storage=${storage.id}`)
  const names = page.locator('.file-row .file-name strong')
  await expect(names).toHaveText(['Alpha.txt', 'Middle.txt', 'Zulu.txt'])
  const before = reads
  await page.getByRole('button', { name: 'Middle.txt', exact: true }).click()
  await page.keyboard.press('F2')
  await page.getByRole('textbox', { name: '新名称', exact: true }).fill('A-new.txt')
  const renamed = page.waitForResponse(response => new URL(response.url()).pathname === '/api/files/action')
  await page.getByRole('textbox', { name: '新名称', exact: true }).press('Enter')
  const result = await (await renamed).json()
  expect(result.renamed).toMatchObject({ id: '/A-new.txt', name: 'A-new.txt' })
  await expect(names).toHaveText(['Alpha.txt', 'A-new.txt', 'Zulu.txt'])
  expect(reads).toBe(before)
  expect(existsSync(path.join(root, 'Middle.txt'))).toBe(false)
  expect(readFileSync(path.join(root, 'A-new.txt'), 'utf8')).toBe('contents of Middle.txt')
  const signed = new URL(result.renamed.url)
  // The isolated server listens on loopback, not the host's advertised LAN IP.
  const content = await page.request.get(signed.pathname + signed.search)
  expect(content.ok()).toBe(true)
  expect(await content.text()).toBe('contents of Middle.txt')
  await page.getByRole('button', { name: '刷新目录', exact: true }).click()
  await expect(names).toHaveText(['A-new.txt', 'Alpha.txt', 'Zulu.txt'])
  } finally {
    const removed = await page.request.delete(`/api/storages/${storage.id}`)
    expect(removed.ok()).toBe(true)
  }
}
