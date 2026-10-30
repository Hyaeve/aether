import { test } from 'node:test'
import assert from 'node:assert/strict'
import { newRenameRule, renameRuleTypes, serializedRenameRule } from '../src/rename-rules.js'

test('six rules keep their order and conversion defaults are independent', () => {
  assert.deepEqual(renameRuleTypes.map(r => r.value), ['replace', 'number', 'insert', 'delete', 'move', 'convert'])
  assert.deepEqual(newRenameRule('convert'), { kind: 'convert', caseMode: 'none', chineseMode: 'none', widthMode: 'none' })
  const a = newRenameRule('delete'), b = newRenameRule('delete')
  a.deleteTypes.push('english')
  assert.deepEqual(b.deleteTypes, [])
})

test('old saved replace rules gain defaults without losing their flags', () => {
  const restored = newRenameRule('replace', { kind: 'replace', find: '旧', replace: '新', firstOnly: true })
  assert.deepEqual(serializedRenameRule(restored), { kind: 'replace', find: '旧', replace: '新', findType: 'literal', caseSensitive: false, firstOnly: true })
})

test('incomplete text rules do not replace stable previews or block other rules', () => {
  assert.equal(serializedRenameRule(newRenameRule('replace')), null)
  assert.equal(serializedRenameRule(newRenameRule('insert')), null)
  assert.equal(serializedRenameRule(newRenameRule('delete')), null)
  assert.deepEqual(serializedRenameRule(newRenameRule('number')), { kind: 'number', name: '', start: 1, digits: 1, numberPosition: 'before', separator: '、' })
})

test('only current rule fields are sent and false flags and empty separator survive', () => {
  const rule = newRenameRule('number', { find: 'old', name: '新', separator: '', start: 7 })
  assert.deepEqual(serializedRenameRule(rule), { kind: 'number', name: '新', start: 7, digits: 1, numberPosition: 'before', separator: '' })
  assert.equal(serializedRenameRule(newRenameRule('delete', { text: 'a' })).firstOnly, false)
  assert.equal(serializedRenameRule({ kind: 'unknown' }), null)
})
