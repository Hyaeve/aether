import test from 'node:test'
import assert from 'node:assert/strict'
import { clipboardShareBatches, shareInput } from '../src/share-links.js'

const base = 'https://yun.139.com/shareweb/#/w/i/2yA8vJBMPc6kg'

test('mobile passwords from labels and query aliases have one canonical pwd parameter', () => {
  for (const text of [
    `${base} 提取码: 4xu4`,
    `链接: ${base}\n提取码：4xu4`,
    `${base}?pwd=4xu4`,
    `${base}?password=4xu4 提取码: 4xu4`,
    `${base}?passwd=4xu4`,
    'https://yun.139.com/shareweb/?pwd=4xu4#/w/i/2yA8vJBMPc6kg',
    `[${base}](${base})\n访问码: 4xu4`
  ]) {
    const canonical = `${base}?pwd=4xu4`
    assert.deepEqual(clipboardShareBatches(text), [{ provider: 'mobile', links: canonical }])
    assert.deepEqual(shareInput(text), { lines: [canonical], conflict: false })
    assert.deepEqual(shareInput(canonical), { lines: [canonical], conflict: false })
  }
})

test('mobile normalization preserves case, other parameters and password conflicts', () => {
  assert.deepEqual(shareInput(`${base}?from=share&pwd=Xy12`), { lines: [`${base}?from=share&pwd=Xy12`], conflict: false })
  assert.deepEqual(shareInput(`${base} 提取码: Xy12\n${base}?pwd=Xy12`), { lines: [`${base}?pwd=Xy12`], conflict: false })
  for (const text of [`${base}?pwd=Ab12 提取码: Cd34`, `${base} 提取码: Ab12\n${base}?pwd=Cd34`]) {
    assert.equal(shareInput(text).conflict, true)
    assert.equal(shareInput(clipboardShareBatches(text)[0].links).conflict, true)
  }
  assert.equal(clipboardShareBatches(base)[0].links, base)
  const legacy = 'https://caiyun.139.com/m/i?legacy 提取码: Xy12'
  assert.equal(clipboardShareBatches(legacy)[0].links, 'https://caiyun.139.com/shareweb/#/w/i/legacy?pwd=Xy12')
})
