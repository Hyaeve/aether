import {test} from 'node:test'
import assert from 'node:assert/strict'
import {mkdtempSync, mkdirSync, writeFileSync, rmSync} from 'node:fs'
import {tmpdir} from 'node:os'
import path from 'node:path'
import {normalizeVersion, checkVersions} from './version.mjs'

test('normalization carries last at ten and middle at one hundred', () => {
  for (const [input, expected] of [
    ['v0.4.15', '0.5.5'], ['v0.101.9', '1.1.9'], ['0.99.10', '1.0.0'],
    ['0.4.125', '0.16.5'], ['0.199.999', '2.98.9'], ['1.1.9', '1.1.9']
  ]) assert.equal(normalizeVersion(input), expected)
  assert.equal(normalizeVersion('0.5.5', 1n), '0.5.6')
  assert.equal(normalizeVersion('0.5.9', 1n), '0.6.0')
  assert.equal(normalizeVersion('0.99.9', 1n), '1.0.0')
  assert.equal(normalizeVersion('9007199254740993.99.9', 1n), '9007199254740994.0.0')
  for (const input of ['0.5', '0.05.5', '0.5.-1', 'v0.5.5-beta', ' 0.5.5']) assert.throws(() => normalizeVersion(input))
})

test('release checks reject overflow, inconsistent metadata and a mismatched tag', t => {
  const root = mkdtempSync(path.join(tmpdir(), 'aether-version-'))
  t.after(() => rmSync(root, {recursive:true, force:true}))
  for (const dir of ['web', 'internal/app', 'docs/releases']) mkdirSync(path.join(root, dir), {recursive:true})
  const write = (name, value) => writeFileSync(path.join(root, name), value)
  write('VERSION', '0.5.5\n')
  write('web/package.json', JSON.stringify({version:'0.5.5'}))
  write('web/package-lock.json', JSON.stringify({version:'0.5.5', packages:{'':{version:'0.5.5'}}}))
  write('internal/app/version.go', 'var Version = "0.5.5"\r\n')
  write('Dockerfile', 'ARG AETHER_VERSION=0.5.5\r\nARG AETHER_VERSION=0.5.5\r\n')
  write('README.md', '**v0.5.5** [release](docs/releases/v0.5.5.md)')
  write('docs/releases/v0.5.5.md', '# v0.5.5')
  assert.equal(checkVersions(root, 'v0.5.5'), '0.5.5')
  assert.throws(() => checkVersions(root, 'v0.4.15'))
  write('VERSION', '0.4.15')
  assert.throws(() => checkVersions(root))
  write('VERSION', '0.5.5')
  write('Dockerfile', 'ARG AETHER_VERSION=0.4.13\nARG AETHER_VERSION=0.5.5\n')
  assert.throws(() => checkVersions(root))
})
