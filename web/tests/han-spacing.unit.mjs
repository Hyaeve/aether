import test from 'node:test'
import assert from 'node:assert/strict'
import { createSSRApp } from 'vue'
import { renderToString } from 'vue/server-renderer'
import { compileTemplate, parse } from 'vue/compiler-sfc'
import { HanText, hanSpacingParts, hanSpacingCount } from '../src/han-spacing.js'
import { transformHanSpacing } from '../han-spacing-plugin.mjs'

test('spacing marks only consecutive Han boundaries without changing characters', () => {
  for (const text of ['信息总览', '115 自动签到', '中文A汉字1名称', '中文/目录.mkv', '汉 字', '中文，名称', '𠀀𠀁ABC', '🎬中文']) {
    const parts = hanSpacingParts(text)
    assert.equal(parts.map(part => typeof part === 'string' ? part : part.character).join(''), text)
  }
  assert.equal(hanSpacingCount('信息总览'), 3)
  assert.equal(hanSpacingCount('中文A汉字1名称'), 3)
  assert.equal(hanSpacingCount('115 自动签到'), 3)
  assert.equal(hanSpacingCount('A汉B1字2 / 😃'), 0)
  assert.equal(hanSpacingCount('𠀀𠀁'), 1)
})

test('renderer preserves display values, escaping and mixed-script boundaries', async () => {
  const render = parts => renderToString(createSSRApp(HanText, { parts }))
  assert.equal(await render(['ABC 115 / -']), 'ABC 115 / -')
  assert.equal(await render([null, '中文A名称', 12, '<>&']),
    '<aether-text class="han-text"><aether-char class="han-gap">中</aether-char>文A<aether-char class="han-gap">名</aether-char>称12&lt;&gt;&amp;</aether-text>')
  assert.equal(await render([undefined, false, 0]), 'false0')
})

test('template transform groups text and expressions without altering attributes or form values', () => {
  const source = `<template><button aria-label="保存任务">保存 {{ count }} 项</button><span>{{ item.name }}</span><input placeholder="文件名称" /><textarea>{{ raw }}</textarea><select><option>中文原值</option></select><svg><text>图形文字</text></svg></template>`
  const transformed = transformHanSpacing(source).code
  assert.match(transformed, /aria-label="保存任务"/)
  assert.equal((transformed.match(/<AetherHanText/g) || []).length, 2)
  assert.match(transformed, /placeholder="文件名称"/)
  assert.match(transformed, /<textarea>{{ raw }}<\/textarea>/)
  assert.match(transformed, /<option>中文原值<\/option>/)
  assert.match(transformed, /<text>图形文字<\/text>/)
  const descriptor = parse(transformed).descriptor
  assert.deepEqual(compileTemplate({ source: descriptor.template.content, filename: 'Test.vue', id: 'test' }).errors, [])
})

test('template transform preserves escaped expressions, slots, control flow and source maps', () => {
  const source = `<template><section><template v-for="row in rows" :key="row.id"><button v-if="row.ok">{{ row.name + '&quot;&lt;&amp;' }}</button><span v-else>等待任务</span></template><slot name="footer">默认内容</slot><div contenteditable="true">输入内容</div><p>ABC 115</p></section></template>`
  const transformed = transformHanSpacing(source, 'Test.vue')
  const descriptor = parse(transformed.code).descriptor
  assert.deepEqual(compileTemplate({ source: descriptor.template.content, filename: 'Test.vue', id: 'test' }).errors, [])
  assert.match(transformed.code, /<div contenteditable="true">输入内容<\/div>/)
  assert.match(transformed.code, /<p>ABC 115<\/p>/)
  assert.ok(transformed.map.sources.includes('Test.vue'))
  assert.equal(transformHanSpacing('<template><p>English 115</p></template>'), null)
})
