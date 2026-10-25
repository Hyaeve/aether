import test from 'node:test'
import assert from 'node:assert/strict'
import {compactDate} from '../src/date-time.mjs'

test('compact dates use two digit year and minute precision in local time',()=>{
  assert.equal(compactDate(new Date(2026,9,24,18,59,43).toISOString()),'26/10/24 18:59')
  assert.equal(compactDate('bad'),'--')
  assert.equal(compactDate('0001-01-01T00:00:00Z','尚未扫描'),'尚未扫描')
})
