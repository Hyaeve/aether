import { test } from 'node:test'
import assert from 'node:assert/strict'
import { numberEasing } from '../src/number-motion.js'
test('number animation slows its steps while approaching the final ten percent', () => {
  assert.equal(numberEasing(0), 0)
  assert.equal(numberEasing(1), 1)
  assert.ok(numberEasing(.45) > .9)
  assert.ok(numberEasing(.55) - numberEasing(.45) < (numberEasing(.2) - numberEasing(.1)) / 4)
  for (let i=1;i<=100;i++) assert.ok(numberEasing(i/100)>=numberEasing((i-1)/100))
})
