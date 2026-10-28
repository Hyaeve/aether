import { test } from 'node:test'
import assert from 'node:assert/strict'
import { archiveStatuses, archiveTriggers, archiveKinds, archiveStatusClass } from '../src/archive-display.js'

test('archive outcomes retain text in addition to semantic colors',()=>{
  for(const status of ['success','doubt','failed','running','cancelled','interrupted']) assert.ok(archiveStatuses[status])
  assert.equal(archiveStatusClass('success'),'success')
  assert.equal(archiveStatusClass('doubt'),'doubt')
  for(const status of ['failed','error','cancelled','interrupted']) assert.equal(archiveStatusClass(status),'failed')
  assert.equal(archiveStatusClass('running'),'doubt')
})
test('manual, monitored, scheduled and automation provenance remain distinct',()=>{
  assert.equal(new Set(Object.values(archiveTriggers)).size,4)
  assert.equal(archiveTriggers.manual,'手动操作')
  assert.equal(archiveTriggers.monitor,'监控执行')
  assert.equal(archiveKinds.automation,'联动')
})
