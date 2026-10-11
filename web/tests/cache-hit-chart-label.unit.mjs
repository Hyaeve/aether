import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { parse, compileScript } from '@vue/compiler-sfc'
import { createSSRApp } from 'vue'
import { renderToString } from 'vue/server-renderer'

const source = await readFile(new URL('../src/components/CacheHitChart.vue', import.meta.url), 'utf8')
const { descriptor } = parse(source)
const script = compileScript(descriptor, { id: 'cache-hit-chart-label', inlineTemplate: true })
const compiled = script.content.replaceAll('from "vue"', `from ${JSON.stringify(import.meta.resolve('vue'))}`).replaceAll("from 'vue'", `from ${JSON.stringify(import.meta.resolve('vue'))}`)
const { default: CacheHitChart } = await import(`data:text/javascript;base64,${Buffer.from(compiled).toString('base64')}`)
const render = props => renderToString(createSSRApp(CacheHitChart, props))

test('default cache chart retains its visible and accessible labels', async () => {
  const sampled = await render({ hits: 80, misses: 20 })
  assert.match(sampled, /aria-label="缓存命中率 80%"/)
  assert.match(sampled, /<small>命中率<\/small>/)
  assert.match(sampled, /stroke-dasharray="80 100"/)
  const empty = await render({})
  assert.match(empty, /aria-label="缓存暂无访问记录"/)
  assert.match(empty, /<strong>—<\/strong>/)
})

test('success chart label describes successes with and without samples', async () => {
  const sampled = await render({ hits: 3, misses: 1, label: '成功率' })
  assert.match(sampled, /aria-label="成功率 75%"/)
  assert.match(sampled, /<small>成功率<\/small>/)
  assert.doesNotMatch(sampled, /缓存|命中/)
  const empty = await render({ label: '成功率' })
  assert.match(empty, /aria-label="成功率暂无记录"/)
  assert.match(empty, /<strong>—<\/strong>/)
  assert.doesNotMatch(empty, /缓存|命中/)
})
