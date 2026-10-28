import {test} from 'node:test'
import assert from 'node:assert/strict'
import { storageMembership } from '../src/storage-membership.mjs'
test('membership badges require an explicit supported fresh server value',()=>{
  const value={tier:'diamond',checkedAt:new Date().toISOString()}
  assert.equal(storageMembership('mobile',value).label,'钻石会员')
  assert.equal(storageMembership('115',value),null)
  assert.equal(storageMembership('mobile',{...value,checkedAt:'2020-01-01'}),null)
  assert.equal(storageMembership('mobile',{...value,expiresAt:'2020-01-01'}),null)
  assert.equal(storageMembership('mobile',{...value,expiresAt:'unrecognized-date'}),null)
  assert.equal(storageMembership('mobile',{total:5000000000000}),null)
})
