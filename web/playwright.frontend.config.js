import { defineConfig } from '@playwright/test'

export default defineConfig({
  testDir: './tests',
  testMatch: 'storage-115-ck.spec.js',
  timeout: 30000,
  workers: 1,
  use: {
    baseURL: 'http://127.0.0.1:15160',
    viewport: { width: 1440, height: 1000 },
    screenshot: 'only-on-failure',
    trace: 'retain-on-failure'
  },
  webServer: {
    command: 'npx vite preview --host 127.0.0.1 --port 15160 --strictPort',
    url: 'http://127.0.0.1:15160',
    reuseExistingServer: false
  }
})
