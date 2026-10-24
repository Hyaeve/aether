import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import vm from 'node:vm'

const worker = readFileSync(new URL('../public/sw.js', import.meta.url), 'utf8')
function harness() {
  const events = {}, stored = [], deleted = []
  const context = { self: { location: { origin: 'https://aether.test' }, clients: { claim: async () => {} }, addEventListener: (type, fn) => { events[type] = fn } },
    Request: class extends Request { constructor(url, options) { super(new URL(url, 'https://aether.test'), options) } }, Response, URL, fetch: async () => { throw new Error('offline') },
    caches: { open: async () => ({ add: async request => stored.push(request.url) }), match: async () => new Response('public offline'),
      keys: async () => ['other-application', 'aether-public-offline-v0', 'aether-public-offline-v1'], delete: async key => deleted.push(key) } }
  vm.runInNewContext(worker, context)
  return { events, stored, deleted }
}

test('PWA worker never intercepts APIs, mutations, media or foreign requests', () => {
  const { events } = harness()
  for (const path of ['/api/state', '/api/notification-service/secret', '/dav/a.mkv', '/stream/private', '/q/private/a.mkv', '/s/private', '/d/private', '/assets/main.js']) {
    events.fetch({ request: { method: 'GET', url: `https://aether.test${path}`, mode: 'navigate' }, respondWith: () => assert.fail(`intercepted ${path}`) })
  }
  events.fetch({ request: { method: 'POST', url: 'https://aether.test/files', mode: 'navigate' }, respondWith: () => assert.fail('intercepted mutation') })
  events.fetch({ request: { method: 'GET', url: 'https://foreign.test/tools', mode: 'navigate' }, respondWith: () => assert.fail('intercepted foreign origin') })
})

test('PWA stores only public offline document and preserves unrelated caches', async () => {
  const { events, stored, deleted } = harness()
  await new Promise((resolve, reject) => events.install({ waitUntil: promise => promise.then(resolve, reject) }))
  await new Promise((resolve, reject) => events.activate({ waitUntil: promise => promise.then(resolve, reject) }))
  assert.deepEqual(stored, ['https://aether.test/pwa/offline.html'])
  assert.deepEqual(deleted, ['aether-public-offline-v0'])
  let result
  events.fetch({ request: { method: 'GET', url: 'https://aether.test/notifications/transfers', mode: 'navigate' }, respondWith: promise => { result = promise } })
  assert.equal(await (await result).text(), 'public offline')
})
