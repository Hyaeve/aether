import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRenderer, h, nextTick, reactive } from 'vue'
import * as vue from 'vue'
import { compileScript, parse } from 'vue/compiler-sfc'
import { transformSync } from 'esbuild'
import * as motion from '../src/number-motion.js'
import { numberDuration, numberEasing, numberFrameInterval } from '../src/number-motion.js'

test('all counter durations have a finite 1500ms upper bound', () => {
  for (const [input, expected] of [[0, 0], [-10, 0], [500, 500], [1500, 1500], [9000, 1500], [NaN, 1500], [Infinity, 1500]]) {
    assert.equal(numberDuration(input), expected)
  }
})

test('the real counter settles at its original deadline even when the value changes mid-animation', async () => {
  const descriptor = parse(readFileSync(new URL('../src/components/AnimatedNumber.vue', import.meta.url), 'utf8')).descriptor
  const compiled = compileScript(descriptor, { id: 'number-clock-test', inlineTemplate: true }).content
  const code = transformSync(compiled, { format: 'cjs' }).code
  const module = { exports: {} }
  new Function('require', 'module', 'exports', code)(name => {
    if (name === 'vue') return vue
    if (name === '../number-motion') return motion
    throw new Error(`Unexpected dependency: ${name}`)
  }, module, module.exports)
  const saved = Object.fromEntries(['performance', 'requestAnimationFrame', 'cancelAnimationFrame', 'setTimeout', 'clearTimeout', 'matchMedia'].map(key => [key, Object.getOwnPropertyDescriptor(globalThis, key)]))
  let now = 0, id = 0, reduced = false
  const scheduled = new Map()
  const schedule = (callback, delay) => { scheduled.set(++id, { time: now + delay, callback }); return id }
  Object.defineProperties(globalThis, {
    performance: { configurable: true, value: { now: () => now } },
    requestAnimationFrame: { configurable: true, value: callback => schedule(() => callback(now), 16) },
    cancelAnimationFrame: { configurable: true, value: key => scheduled.delete(key) },
    setTimeout: { configurable: true, value: (callback, delay) => schedule(callback, delay) },
    clearTimeout: { configurable: true, value: key => scheduled.delete(key) },
    matchMedia: { configurable: true, value: () => ({ matches: reduced }) }
  })
  const renderer = createRenderer({
    createElement: () => ({ text: '', props: {} }), createText: text => ({ text }),
    createComment: () => ({}), setText: (el, text) => { el.text = text },
    setElementText: (el, text) => { el.text = text }, parentNode: () => null, nextSibling: () => null,
    patchProp: (el, key, old, value) => { el.props[key] = value },
    insert: (el, parent) => { parent.child = el }, remove: () => {}
  })
  async function advance(duration) {
    const end = now + duration
    for (;;) {
      const next = [...scheduled.entries()].filter(([, job]) => job.time <= end).sort((a, b) => a[1].time - b[1].time)[0]
      if (!next) break
      scheduled.delete(next[0]); now = next[1].time; next[1].callback()
    }
    now = end
    await nextTick()
  }
  let app
  try {
    for (const value of [1, 7, 10000, 987654321012]) {
      const root = {}, props = reactive({ value, duration: 9000 })
      const started = now
      app = renderer.createApp({ render: () => h(module.exports.default, props) })
      app.mount(root)
      assert.equal(root.child.text, '0')
      await advance(300)
      props.value = value * 2
      await nextTick()
      await advance(1200)
      assert.equal(now - started, 1500)
      assert.equal(root.child.text, String(value * 2))
      assert.equal(scheduled.size, 0)
      app.unmount(); app = null
    }
    app = renderer.createApp({ render: () => h(module.exports.default, { value: 1000 }) })
    app.mount({})
    assert.ok(scheduled.size > 0)
    app.unmount(); app = null
    assert.equal(scheduled.size, 0)
    reduced = true
    const root = {}
    app = renderer.createApp({ render: () => h(module.exports.default, { value: 12345 }) })
    app.mount(root)
    assert.equal(root.child.text, '12345')
    assert.equal(scheduled.size, 0)
  } finally {
    app?.unmount()
    for (const [key, descriptor] of Object.entries(saved)) {
      if (descriptor) Object.defineProperty(globalThis, key, descriptor)
      else delete globalThis[key]
    }
  }
})

test('count-up magnitude scales the step and slows update cadence near completion', () => {
  for (const value of [1, 7, 80, 10000, 987654321, Number.MAX_SAFE_INTEGER]) {
    const samples = [0, .1, .2, .4, .6, .8, .95, 1].map(t => Math.round(value * numberEasing(t)))
    assert.equal(samples[0], 0)
    assert.equal(samples.at(-1), value)
    for (let i = 1; i < samples.length; i++) assert.ok(samples[i] >= samples[i - 1])
    assert.ok(numberFrameInterval(.95, value) > numberFrameInterval(.2, value))
    assert.ok(numberFrameInterval(.95, value) <= 96)
  }
  assert.ok(numberFrameInterval(.95, 1000000) > numberFrameInterval(.95, 5))
})
