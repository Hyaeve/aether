import { test } from 'node:test'
import assert from 'node:assert/strict'
import { transferStates, transferState, transferSummary, transferPercent, canControlTransfer } from '../src/transfer-status.mjs'
test('transfer statuses have explicit semantic colors and unknown totals stay unknown',()=>{
  assert.equal(transferStates.length,10)
  assert.equal(transferState({status:'waiting'}).tone,'blue')
  assert.equal(transferState({status:'queued'}).tone,'orange')
  assert.equal(transferState({status:'ignored'}).tone,'green')
  assert.equal(transferState({status:'upstream-detail'}).label,'错误')
  assert.equal(transferPercent({status:'completed',done:20,total:100}),20)
  assert.equal(transferPercent({total:0,done:20}),null)
  assert.equal(transferPercent({total:85_118_265_822,done:26_000_000}),0.03)
  assert.equal(transferPercent({total:1_000_000,done:999_999}),99.99)
  assert.deepEqual(transferSummary([{total:100,done:25},{total:100,done:100}]),{count:2,total:200,done:125,percent:62.5})
  assert.equal(transferSummary([{total:85_118_265_822,done:26_000_000}]).percent,0.03)
  assert.equal(transferSummary([{total:100,done:25},{total:0,done:20,status:'preparing'}]).percent,null)
  assert.equal(canControlTransfer({kind:'copy',status:'running'},'pause'),false)
  assert.equal(canControlTransfer({kind:'copy',status:'running',controls:{pause:true}},'pause'),true)
  assert.equal(canControlTransfer({kind:'upload',status:'failed'},'retry'),false)
  assert.equal(canControlTransfer({status:'failed',controls:{retry:true}},'retry'),true)
})
