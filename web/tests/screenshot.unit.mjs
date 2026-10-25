import test from 'node:test'
import assert from 'node:assert/strict'
import { captureScreenshot } from './helpers/screenshot.js'

function fixture(t, { paused = false, errors = [] } = {}) {
  const previous = { request: globalThis.requestAnimationFrame, cancel: globalThis.cancelAnimationFrame }
  const pending = new Map(), captures = []
  let sequence = 0
  globalThis.requestAnimationFrame = callback => {
    const id = ++sequence
    pending.set(id, callback)
    if (!paused) queueMicrotask(() => {
      const frame = pending.get(id)
      pending.delete(id)
      frame?.()
    })
    return id
  }
  globalThis.cancelAnimationFrame = id => pending.delete(id)
  t.after(() => {
    if (previous.request) globalThis.requestAnimationFrame = previous.request
    else delete globalThis.requestAnimationFrame
    if (previous.cancel) globalThis.cancelAnimationFrame = previous.cancel
    else delete globalThis.cancelAnimationFrame
  })
  const image = Buffer.from('captured image')
  const page = {
    evaluate: callback => callback(),
    screenshot: async options => {
      captures.push(options)
      if (errors.length) throw errors.shift()
      return image
    },
    isClosed: () => false
  }
  return { page, captures, pending, image }
}

test('screenshot preserves its path, disabled animations and capture deadline', async t => {
  const f = fixture(t)
  assert.equal(await captureScreenshot(f.page, { path: 'view.png' }), f.image)
  assert.deepEqual(f.captures, [{ path: 'view.png', animations: 'disabled', timeout: 5000 }])
  assert.equal(f.pending.size, 0)
})

test('a suspended frame clock cannot prevent the actual screenshot', { timeout: 2000 }, async t => {
  const f = fixture(t, { paused: true })
  assert.equal(await captureScreenshot(f.page, { path: 'suspended.png' }), f.image)
  assert.equal(f.captures.length, 1)
  assert.equal(f.pending.size, 0)
})

const transient = () => new Error('page.screenshot: Protocol error (Page.captureScreenshot): Unable to capture screenshot')

test('only transient compositor failures retry and still return a real capture', async t => {
  const f = fixture(t, { errors: [transient(), transient()] })
  assert.equal(await captureScreenshot(f.page, { path: 'retry.png' }), f.image)
  assert.equal(f.captures.length, 3)
  assert.ok(f.captures.every(options => options.timeout === 5000))
})

test('a third capture failure is reported instead of hiding the diagnostic', async t => {
  const f = fixture(t, { errors: [transient(), transient(), transient()] })
  await assert.rejects(captureScreenshot(f.page, {}), /Unable to capture screenshot/)
  assert.equal(f.captures.length, 3)
})

test('nontransient screenshot errors are not retried or swallowed', async t => {
  const f = fixture(t, { errors: [new Error('page is closed')] })
  await assert.rejects(captureScreenshot(f.page, {}), /page is closed/)
  assert.equal(f.captures.length, 1)
})
