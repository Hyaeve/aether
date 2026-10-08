// Standalone component contract test; no server/ToolsPage integration required.
import { createServer } from 'vite'
import vue from '@vitejs/plugin-vue'
import { chromium } from '@playwright/test'
import { mkdtemp } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import path from 'node:path'
import assert from 'node:assert/strict'

const root = path.resolve(import.meta.dirname, '..')
const output = await mkdtemp(path.join(tmpdir(), 'aether-simulcast-ui-'))
const server = await createServer({ root, configFile: false, plugins: [vue(), {
name: 'simulcast-fixture', configureServer(server) {
server.middlewares.use('/simulcast-test', async (_req, res) => {
  res.setHeader('Content-Type', 'text/html')
  res.end(await server.transformIndexHtml('/simulcast-test', `<!doctype html><html><head><meta name="viewport" content="width=device-width,initial-scale=1"></head><body><main id="app" style="max-width:800px;margin:24px auto;padding:16px"></main><script type="module">
  import { createApp } from '/node_modules/.vite/deps/vue.js';
  import Component from '/src/components/Pan115Simulcast.vue';
  import { state } from '/src/lib.js'; import '/src/style.css';
  state.storages = [{ id:'s1', type:'115', name:'115 电影资料库', enabled:true }, { id:'s2', type:'quark', name:'夸克', enabled:true }];
  createApp(Component).mount('#app');</script></body></html>`))
})
}
}], server: { host: '127.0.0.1', port: 0 } })
await server.listen()
const browser = await chromium.launch()
try {
  const page = await browser.newPage({ viewport: { width: 1280, height: 800 } })
  let configs = {}, reject = false
  const errors = []
  page.on('pageerror', e => { errors.push(e.message); console.error(e.message) })
  await page.route('**/api/115-simulcast', async route => {
    if (route.request().method() === 'PUT') {
      if (reject) return route.fulfill({ status: 400, json: { error: '模拟保存失败' } })
      configs = route.request().postDataJSON()
    }
    await route.fulfill({ json: configs })
  })
  await page.route('**/api/files?**', route => route.fulfill({ json: [] }))
  await page.goto(`http://127.0.0.1:${server.httpServer.address().port}/simulcast-test`)
  await page.getByRole('button', { name: '添加存储' }).click()
  await page.getByRole('button', { name: '115 存储', exact: true }).click()
  await page.getByRole('option', { name: '115 电影资料库' }).click()
  await page.getByRole('button', { name: '选择复制目录', exact: true }).click()
  await page.getByRole('button', { name: '选择当前目录' }).click()
  await page.getByRole('button', { name: '保存', exact: true }).click()
  await page.getByRole('button', { name: '编辑 115 电影资料库', exact: true }).waitFor()
  assert.equal(configs.s1.directory, '/')
  assert.equal(await page.getByRole('button', { name: '添加存储' }).isDisabled(), true)
  await page.screenshot({ path: path.join(output, 'desktop.png') })
  const toggle = page.getByRole('switch', { name: '启用 115 电影资料库 同播复制' })
  await toggle.click()
  await page.waitForFunction(() => !document.querySelector('.simulcast-row .switch').checked)
  assert.equal(configs.s1.enabled, false)
  reject = true
  await toggle.click()
  await page.getByRole('alert').waitFor()
  assert.equal(await toggle.isChecked(), false)
  reject = false
  await page.getByRole('button', { name: '编辑 115 电影资料库', exact: true }).click()
  await page.setViewportSize({ width: 375, height: 812 })
  await page.screenshot({ path: path.join(output, 'mobile-edit.png') })
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true)
  await page.getByRole('button', { name: '取消', exact: true }).click()
  await page.screenshot({ path: path.join(output, 'mobile.png') })
  await page.getByRole('button', { name: '删除 115 电影资料库', exact: true }).click()
  await page.getByRole('button', { name: '删除配置', exact: true }).click()
  await page.waitForFunction(() => !document.querySelector('.simulcast-row'))
  assert.deepEqual(configs, {})
  assert.deepEqual(errors, [])
  console.log(`Simulcast component passed; screenshots: ${output}`)
} finally {
  await browser.close()
  await server.close()
}
