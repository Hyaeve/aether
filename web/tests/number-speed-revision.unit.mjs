import {test} from 'node:test'
import assert from 'node:assert/strict'
import {readFileSync} from 'node:fs'
import {numberEasing} from '../src/number-motion.js'

test('number motion reaches the slow final ten percent without a long tail',()=>{
  assert.ok(numberEasing(.3)>.7&&numberEasing(.3)<.8)
  assert.ok(numberEasing(.45)>.9)
  assert.ok(numberEasing(.65)-numberEasing(.6)<(numberEasing(.15)-numberEasing(.1))/8)
  const component=readFileSync(new URL('../src/components/AnimatedNumber.vue',import.meta.url),'utf8')
  assert.match(component,/duration:.*default: 1500/)
})
