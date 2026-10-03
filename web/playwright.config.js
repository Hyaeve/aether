import { defineConfig } from '@playwright/test'
import { mkdtempSync, mkdirSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import path from 'node:path'

const testRoot = process.env.AETHER_E2E_ROOT || mkdtempSync(path.join(tmpdir(), 'aether-e2e-'))
process.env.AETHER_E2E_ROOT = testRoot
mkdirSync(path.join(testRoot, 'media', 'Movies'), { recursive: true })
writeFileSync(path.join(testRoot, 'media', 'Movies', 'Arrival.MP4'), 'test-video-content')
writeFileSync(path.join(testRoot, 'media', 'Movies', 'sample.mp4'), 'test-video-content')

export default defineConfig({
  testDir: './tests',
  timeout: 45000,
  workers: 1,
  use: { baseURL: 'http://127.0.0.1:15159', screenshot: 'only-on-failure', trace: 'retain-on-failure', viewport: { width: 1440, height: 1000 } },
  webServer: {
    command: `go run ./cmd/aether -config-dir "${path.join(testRoot, 'config')}" -data-dir "${path.join(testRoot, 'data')}"`,
    cwd: '..',
    url: 'http://127.0.0.1:15159/api/health',
    reuseExistingServer: false,
    timeout: 60000,
    env: {
      AETHER_ADDR: '127.0.0.1:15159',
      AETHER_WEB_DIR: 'web/dist'
    }
  }
})
