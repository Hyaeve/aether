import test from 'node:test'
import assert from 'node:assert/strict'
import { readSession, rememberRoute, rememberedRoute, writeSession } from '../src/session-navigation.js'

const parents = [
  ['/tasks/cache/settings', '/tasks/cache'],
  ['/tasks/cas/pool', '/tasks/cas'],
  ['/tasks/ed2k/pool', '/tasks/ed2k'],
  ['/tools/115-rapid', '/tools'],
  ['/virtual/quark/settings', '/virtual/quark'],
  ['/transfer/backup/settings', '/transfer/backup']
]

function storage() {
  const items = new Map()
  return { getItem: key => items.get(key) ?? null, setItem: (key, value) => items.set(key, value) }
}

test('sidebar reads existing jump-page memories as their return columns', () => {
  const previous = globalThis.sessionStorage
  globalThis.sessionStorage = storage()
  try {
    for (const [child, parent] of parents) {
      const module = '/' + child.split('/')[1]
      writeSession('admin', `route:${module}`, child)
      assert.equal(rememberedRoute('admin', module), parent, child)
    }
  } finally { globalThis.sessionStorage = previous }
})

test('ordinary columns, account isolation and non-route session data stay intact', () => {
  const previous = globalThis.sessionStorage
  globalThis.sessionStorage = storage()
  try {
    for (const path of ['/tasks/ed2k', '/tools/market', '/settings/strm', '/links/cache', '/storage/shares', '/files/mounts', '/notifications/transfers']) {
      const module = '/' + path.split('/')[1]
      writeSession('admin', `route:${module}`, path)
      assert.equal(rememberedRoute('admin', module), path)
      assert.equal(rememberedRoute('other', module), module)
    }
    const directory = { storage: 'pool', id: 'opaque-id', history: [{ id: '/', name: 'root' }, { id: 'opaque-id', name: 'child' }] }
    writeSession('admin', 'directory', directory)
    writeSession('admin', 'transfer-mode', 'download')
    assert.deepEqual(readSession('admin', 'directory'), directory)
    assert.equal(readSession('admin', 'transfer-mode'), 'download')
    writeSession('admin', 'route:/tasks', '/settings/strm')
    assert.equal(rememberedRoute('admin', '/tasks'), '/tasks')
    sessionStorage.setItem('aether-session:admin:route:/tasks', 'broken-json')
    assert.equal(rememberedRoute('admin', '/tasks'), '/tasks')
  } finally { globalThis.sessionStorage = previous }
})

test('new route memories store parent columns without changing other session state', () => {
  const previous = globalThis.sessionStorage
  globalThis.sessionStorage = storage()
  try {
    for (const [child, parent] of parents) {
      const module = '/' + child.split('/')[1]
      rememberRoute('admin', child)
      assert.equal(readSession('admin', `route:${module}`), parent, child)
      assert.equal(rememberedRoute('admin', module), parent)
    }
    rememberRoute('admin', '/settings/strm')
    assert.equal(readSession('admin', 'route:/settings'), '/settings/strm')
    rememberRoute('admin', '/tools/market')
    assert.equal(readSession('admin', 'route:/tools'), '/tools/market')
    assert.equal(readSession('other', 'route:/tools'), null)
  } finally { globalThis.sessionStorage = previous }
})

test('unavailable browser session storage does not block navigation', () => {
  const previous = globalThis.sessionStorage
  globalThis.sessionStorage = {
    getItem() { throw new Error('unavailable') },
    setItem() { throw new Error('unavailable') }
  }
  try {
    assert.doesNotThrow(() => rememberRoute('admin', '/tasks/cache/settings'))
    assert.equal(rememberedRoute('admin', '/tasks'), '/tasks')
  } finally { globalThis.sessionStorage = previous }
})
