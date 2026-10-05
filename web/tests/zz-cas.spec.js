import { test, expect } from '@playwright/test'
import { readFileSync } from 'node:fs'
import { createHash } from 'node:crypto'
import path from 'node:path'

test('local CAS generation and persisted named directory navigation', async ({ page }, testInfo) => {
  await page.goto('/')
  await page.getByLabel('账号', { exact: true }).fill('my-aether-owner')
  await page.getByLabel('密码', { exact: true }).fill('a1')
  await page.getByRole('button', { name: '登录工作空间', exact: true }).click()
  await page.getByRole('link', { name: '任务管理', exact: true }).click()
  await page.getByRole('link', { name: 'CAS 任务', exact: true }).click()
  await page.getByRole('button', { name: '添加任务', exact: true }).click()
  await page.getByLabel('任务名称').fill('本地 CAS 哈希')
  await expect(page.getByRole('button', { name: 'CAS 操作', exact: true })).toHaveCount(0)
  await page.getByLabel('生成目录').fill('local-cas')
  await page.getByRole('button', { name: '选择目录', exact: true }).click()
  await page.locator('.source-accounts').getByRole('button', { name: /家庭影音库/ }).click()
  await page.getByRole('dialog', { name: '选择存储目录' }).getByRole('button', { name: 'Movies', exact: true }).click()
  await page.getByRole('button', { name: '选择当前目录', exact: true }).click()
  await expect(page.locator('.source-trigger')).toContainText('Movies')
  await expect(page.getByLabel('还原文件保留时间', { exact: true })).toHaveValue('12')
  await page.screenshot({ path: testInfo.outputPath('local-cas-form.png'), fullPage: true })
  await page.getByRole('button', { name: '保存任务', exact: true }).click()
  const row = page.getByRole('row').filter({ hasText: '本地 CAS 哈希' })
  await row.getByRole('button', { name: '立即执行', exact: true }).click()
  await expect.poll(async () => {
    const state = await (await page.request.get('/api/state')).json()
    return state.tasks.find(t => t.name === '本地 CAS 哈希')?.status
  }).toBe('success')
  const output = readFileSync(path.join(process.env.AETHER_E2E_ROOT, 'data', 'strm', 'local-cas', 'Arrival.MP4.cas'), 'utf8')
  const info = JSON.parse(Buffer.from(output, 'base64').toString('utf8'))
  expect(info.sha256).toBe(createHash('sha256').update('test-video-content').digest('hex'))
  await page.reload()
  await row.getByRole('button', { name: '编辑任务', exact: true }).click()
  await expect(page.locator('.source-trigger')).toContainText('Movies')
  await page.getByRole('button', { name: '选择目录', exact: true }).click()
  await expect(page.locator('.source-path')).toHaveText('Movies')
  await page.getByRole('button', { name: '上级目录', exact: true }).click()
  await expect(page.locator('.source-path')).toHaveText('根目录')
})
