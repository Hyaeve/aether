import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

test('CI installs frontend dependencies before running Vue-dependent unit tests', () => {
  const workflow = readFileSync(new URL('../.github/workflows/docker-amd64.yml', import.meta.url), 'utf8')
  const metadata = workflow.match(/- name: Verify version numbering and metadata\r?\n\s+run: ([^\r\n]+)/)?.[1]
  assert.equal(metadata, 'node --test scripts/*.test.mjs && node scripts/version.mjs --check')
  const install = workflow.indexOf('run: npm ci && npm run build')
  const units = workflow.indexOf('run: node --test web/tests/*.unit.mjs')
  const browsers = workflow.indexOf('run: npm run test:e2e')
  assert.ok(install >= 0 && units > install && browsers > units,
    'Frontend units must run after dependency installation and before browser tests')
  assert.match(workflow, /needs: \[verify, go-tests\]/)
})
