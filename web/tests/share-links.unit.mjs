import test from 'node:test'
import assert from 'node:assert/strict'
import { clipboardShareBatches, shareInput, shareHighlight } from '../src/share-links.js'

const mobile = 'https://yun.139.com/shareweb/#/w/i/2ygb7gk5b1gfy'

test('multiline and inline passwords stay attached to their own URL', () => {
  const input = `链接: ${mobile}\n提取码: x2ol\nhttps://pan.quark.cn/s/abc 提取码：Ab12\nhttps://115cdn.com/s/def?password=q526`
  assert.deepEqual(clipboardShareBatches(input), [
    { provider: '115', links: 'https://115cdn.com/s/def?password=q526' },
    { provider: 'mobile', links: mobile + ' 提取码: x2ol' },
    { provider: 'quark', links: 'https://pan.quark.cn/s/abc 提取码: Ab12' }
  ])
  assert.deepEqual(shareInput(input).lines, [mobile + ' 提取码: x2ol', 'https://pan.quark.cn/s/abc 提取码: Ab12', 'https://115cdn.com/s/def?password=q526'])
})

test('markdown duplication, repeated shares and unrelated URLs do not mix passwords', () => {
  assert.equal(clipboardShareBatches(`[${mobile}](${mobile})\n提取码: x2ol\n${mobile}`)[0].links, mobile + ' 提取码: x2ol')
  const entries = clipboardShareBatches(`${mobile}\nhttps://example.com/doc 提取码: abcd\nhttps://pan.quark.cn/s/abc`)
  assert.equal(entries[0].links, mobile)
  assert.equal(entries[1].links, 'https://pan.quark.cn/s/abc')
})

test('conflicting query, text and repeated passwords require correction', () => {
  for (const input of [
    `${mobile}?password=abcd 提取码: efgh`,
    `${mobile}\n提取码: abcd\n${mobile}\n密码: efgh`,
    'https://115cdn.com/s/abc?password=abcd&password=efgh'
  ]) assert.equal(shareInput(clipboardShareBatches(input)[0].links).conflict, true)
  assert.equal(shareInput(`${mobile}\n提取码: x2ol`).conflict, false)
})

test('fragment passwords are forwarded as labelled text for backend compatibility', () => {
  assert.equal(clipboardShareBatches(mobile + '?password=Ab12')[0].links, mobile + '?password=Ab12 提取码: Ab12')
})

test('batching uses provider order and chunks at 50 without dropping shares', () => {
  const input = Array.from({ length: 111 }, (_, i) => `https://pan.quark.cn/s/q${i} 提取码: x2ol`).join('\n')
  const batches = clipboardShareBatches(input)
  assert.deepEqual(batches.map(batch => batch.links.split('\n').length), [50, 50, 11])
  assert.equal(batches.flatMap(batch => batch.links.split('\n')).length, 111)
})

test('only official https share URLs trigger clipboard windows', () => {
  for (const input of ['https://yun.139.com.evil.test/shareweb/#/w/i/abc', 'http://pan.quark.cn/s/abc', 'https://user:secret@115cdn.com/s/abc', 'https://115cdn.com:123/s/abc', 'https://pan.quark.cn/file/123', 'text', 'a'.repeat(200001)]) assert.deepEqual(clipboardShareBatches(input), [])
})

test('highlight tokens preserve editable text and treat markup as plain text', () => {
  const text = `${mobile} 提取码: Ab12\nhttps://115cdn.com/s/abc?password=q526\n<img src=x onerror=alert(1)>`
  const parts = shareHighlight(text)
  assert.equal(parts.map(part => part.text).join(''), text + '\n')
  assert.deepEqual(parts.filter(part => part.password).map(part => part.text), ['提取码: Ab12', 'password=q526'])
})
