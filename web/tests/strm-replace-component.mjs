import assert from 'node:assert/strict'
import { mkdtempSync } from 'node:fs'
import { tmpdir } from 'node:os'
import path from 'node:path'
import { createServer } from 'vite'
import vue from '@vitejs/plugin-vue'
import { chromium } from '@playwright/test'

// Standalone, mocked component fixture. Never contacts an Aether service.
const output = mkdtempSync(path.join(tmpdir(), 'aether-strm-replace-ui-'))
const server = await createServer({
  configFile: false, root: process.cwd(), plugins: [vue(), {
    name: 'strm-replace-fixture',
    configureServer(server) {
      server.middlewares.use('/__strm-replace-test', async (req, res) => {
        res.setHeader('Content-Type', 'text/html')
        res.end(await server.transformIndexHtml('/__strm-replace-test', `<!doctype html><html><head><meta name="viewport" content="width=device-width, initial-scale=1"></head><body><div id="app"></div><script type="module">
          import { createApp, h } from '/node_modules/.vite/deps/vue.js'
          import StrmReplace from '/src/components/StrmReplace.vue'
          import '/src/style.css'
          createApp({render: () => h(StrmReplace)}).mount('#app')
        </script></body></html>`))
      })
    }
  }], server: { host: '127.0.0.1', port: 0 }
})
let browser
try {
  await server.listen()
  browser = await chromium.launch()
  for (const viewport of [{ width: 1440, height: 1000 }, { width: 390, height: 844 }]) {
    const page = await browser.newPage({ viewport })
    const errors = []
    page.on('pageerror', error => errors.push(error.message))
    let posts = 0, task = null
    await page.route('**/api/**', async route => {
      const request = route.request()
      if (request.url().includes('/local-directories')) return route.fulfill({ json: { path: '/media', items: [] } })
      assert.ok(request.url().endsWith('/strm-replace'))
      if (request.method() === 'POST') {
        posts++
        assert.deepEqual(request.postDataJSON(), { directory: '/media', find: 'old', replace: '', confirmed: true })
        task = { id: 'job', status: 'running', scanned: 3, processed: 2, changed: 1, skipped: 0 }
        return route.fulfill({ status: 202, json: task })
      }
      return route.fulfill({ json: { tasks: task ? [task] : [] } })
    })
    await page.goto(server.resolvedUrls.local[0] + '__strm-replace-test')
    await page.getByRole('button', { name: '选择容器目录', exact: true }).click()
    await page.getByRole('button', { name: '选择当前目录' }).click()
    await page.getByLabel('匹配字段', { exact: true }).fill('old')
    await page.getByRole('button', { name: '执行替换' }).click()
    assert.equal(posts, 0)
    await page.getByRole('button', { name: '取消', exact: true }).click()
    assert.equal(posts, 0)
    await page.getByRole('button', { name: '执行替换' }).click()
    await page.screenshot({ path: path.join(output, `confirm-${viewport.width}.png`) })
    await page.getByRole('button', { name: '确认替换', exact: true }).click()
    await page.getByText('正在替换', { exact: true }).waitFor()
    assert.equal(posts, 1)
    assert.equal(await page.getByRole('button', { name: '执行替换' }).isDisabled(), true)
    const bounds = await page.getByRole('dialog', { name: 'STRM 内容替换', exact: true }).boundingBox()
    assert.ok(bounds.x >= 0 && bounds.x + bounds.width <= viewport.width + 1)
    await page.screenshot({ path: path.join(output, `progress-${viewport.width}.png`) })
    task = { ...task, status: 'failed', error: '文件已被外部修改，未覆盖' }
    await page.getByText('文件已被外部修改，未覆盖').waitFor()
    assert.deepEqual(errors, [])
    await page.close()
  }
  console.log(`STRM component desktop/mobile checks passed; screenshots: ${output}`)
} finally {
  await browser?.close()
  await server.close()
}
