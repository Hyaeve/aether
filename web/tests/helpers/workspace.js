import { expect } from '@playwright/test'
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs'
import path from 'node:path'
import { createServer } from 'node:net'

// These live fixtures only target Playwright's isolated server and media root.
export async function ensureWorkspaceAccount(request) {
  const response = await request.get('/api/auth/status')
  expect(new URL(response.url()).origin).toBe('http://127.0.0.1:15159')
  expect(response.ok()).toBe(true)
  const status = await response.json()
  if (!status.initialized) {
    const setup = await request.post('/api/auth/setup', { data: { username: 'my-aether-owner', password: 'a1' } })
    expect(setup.status()).toBe(201)
    expect((await request.post('/api/auth/logout')).ok()).toBe(true)
  }
}

export async function ensureWorkspaceMedia(page) {
  expect(process.env.AETHER_E2E_ROOT).toBeTruthy()
  const root = path.resolve(process.env.AETHER_E2E_ROOT, 'media')
  mkdirSync(path.join(root, 'Movies'), { recursive: true })
  for (const name of ['Arrival.MP4', 'sample.mp4']) {
    const file = path.join(root, 'Movies', name)
    if (!existsSync(file)) writeFileSync(file, 'test-video-content')
    expect(readFileSync(file, 'utf8')).toBe('test-video-content')
  }
  const response = await page.request.get('/api/state')
  expect(new URL(response.url()).origin).toBe('http://127.0.0.1:15159')
  expect(response.ok()).toBe(true)
  const state = await response.json()
  let storage = state.storages.find(item => item.name === '家庭影音库')
  if (!storage) {
    const created = await page.request.post('/api/storages', { data: { name: '家庭影音库', type: 'local', enabled: true, config: { root } } })
    expect(created.status()).toBe(201)
    const { id } = await created.json()
    const saved = await page.request.get('/api/state')
    expect(saved.ok()).toBe(true)
    storage = (await saved.json()).storages.find(item => item.id === id)
  }
  expect(storage).toMatchObject({ name: '家庭影音库', type: 'local', enabled: true })
  expect(path.resolve(storage.config.root)).toBe(root)
  await page.reload()
  await expect(page.getByRole('navigation', { name: '主导航' })).toBeVisible()
  return storage
}

export async function prepareWorkspaceMenuLink(request) {
  const socket = createServer()
  await new Promise((resolve, reject) => { socket.once('error', reject); socket.listen(0, '127.0.0.1', resolve) })
  const port = socket.address().port
  await new Promise((resolve, reject) => socket.close(error => error ? reject(error) : resolve()))
  const created = await request.post('/api/links', { data: { name: '菜单测试以链', type: 'emby', address: 'http://127.0.0.1:15998', port, apiKey: 'test', mode: 'always', enabled: false } })
  expect(created.ok()).toBe(true)
  const { id } = await created.json()
  const response = await request.get('/api/links')
  expect(response.ok()).toBe(true)
  const links = await response.json()
  expect(links.find(link => link.id === id)).toMatchObject({ name: '菜单测试以链', enabled: false })
  if (links[0].id !== id) {
    const reordered = await request.post('/api/links/reorder', { data: { id, target: links[0].id } })
    expect(reordered.ok()).toBe(true)
  }
}
