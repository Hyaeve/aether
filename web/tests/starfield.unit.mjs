import { test } from 'node:test'
import assert from 'node:assert/strict'
import { createSpiralPair } from '../src/starfield.js'

test('exactly two independent spirals cover left and bottom regions with variable sizes', () => {
  let seed=123
  const random=()=>{seed=(seed*1664525+1013904223)>>>0;return seed/4294967296}
  const regions=new Set(),sizes=[]
  for(let i=0;i<1000;i++){
    const pair=createSpiralPair(random)
    assert.equal(pair.length,2)
    for(const [x,y,size] of pair){
      assert.ok(x>=.12&&x<=.86&&y>=.14&&y<=.87)
      assert.ok((y-.14)/.73>=(x-.12)/.74-1e-10)
      assert.ok(size>=.65&&size<=1.5)
      regions.add(x>.5?'right-bottom':y<.35?'left-top':y>.64?'left-bottom':'middle')
      sizes.push(size)
    }
  }
  assert.equal(regions.size,4)
  assert.ok(Math.max(...sizes)-Math.min(...sizes)>.8)
  const adjacent=createSpiralPair(()=>.2)
  assert.deepEqual(adjacent[0],adjacent[1])
})
