import { test } from 'node:test'
import assert from 'node:assert/strict'
import { createCloudSessionCache, cloudListings, cloudListingKey, rememberCloudPool, cloudSelectedPool } from '../src/cloud-session-cache.js'
import { state } from '../src/lib.js'

test('cloud session remembers loaded pages, refresh replaces them and concurrent readers share a request', async () => {
  let calls = 0, release
  const cache = createCloudSessionCache(async (_kind, _storage, page) => {
    calls++
    if (page === 2) await new Promise(resolve => { release = resolve })
    return { items: [{ id: String(page) }], nextPage: page + 1 }
  })
  await cache.load('owner-shares', 'shares', 'p')
  await cache.load('owner-shares', 'shares', 'p')
  assert.equal(calls, 1)
  const a = cache.load('owner-shares', 'shares', 'p', 2)
  const b = cache.load('owner-shares', 'shares', 'p', 2)
  assert.equal(calls, 2)
  release()
  assert.deepEqual((await a).items, (await b).items)
  assert.equal(cache.peek('owner-shares').items.length, 2)
  await cache.load('owner-shares', 'shares', 'p', 1, true)
  assert.equal(cache.peek('owner-shares').items.length, 1)
  assert.equal(calls, 3)
})

test('invalidated cloud requests cannot restore an old session and failures are not cached', async () => {
  let release, fail = false
  const cache = createCloudSessionCache(async () => {
    if (fail) throw new Error('network')
    await new Promise(resolve => { release = resolve })
    return { items: [{ id: 'private' }] }
  })
  const pending = cache.load('old', 'shares', 'p')
  cache.clear()
  release()
  assert.equal(await pending, null)
  assert.equal(cache.peek('old'), null)
  fail = true
  await assert.rejects(cache.load('new', 'shares', 'p'))
  assert.equal(cache.peek('new'), null)
})

test('refresh supersedes older responses and session identity never retains private rows', async () => {
  const releases=[]
  const cache=createCloudSessionCache(()=>new Promise(resolve=>releases.push(resolve)))
  const older=cache.load('key','shares','p')
  const fresh=cache.load('key','shares','p',1,true)
  releases[1]({items:[{id:'current'}]})
  await fresh
  releases[0]({items:[{id:'obsolete'}]})
  assert.equal(await older,null)
  assert.deepEqual(cache.peek('key').items,[{id:'current'}])
  state.username='owner'
  state.authenticated=true
  state.storages=[{id:'p',type:'115',config:{cookie:'********'},viewRevision:'one'}]
  const key=cloudListingKey('shares','p')
  state.storages[0].viewRevision='two'
  assert.notEqual(cloudListingKey('shares','p'),key)
  rememberCloudPool('shares','p')
  assert.equal(cloudSelectedPool('shares'),'p')
  state.authenticated=false
  assert.equal(cloudSelectedPool('shares'),undefined)
  assert.equal(cloudListings.peek(key),null)
})
