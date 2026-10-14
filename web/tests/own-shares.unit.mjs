import {test} from 'node:test'
import assert from 'node:assert/strict'
import {state} from '../src/lib.js'
import {rememberOwnShare,withoutOwnShares} from '../src/own-shares.js'
test('own shares excluded without suppressing other shares or another administrator',()=>{
 state.username='own-share-test';rememberOwnShare('https://115cdn.com/s/own?password=ABCD&#')
 const text='https://115.com/s/own\n访问码: ABCD\nhttps://pan.quark.cn/s/other\n提取码: abcd'
 assert.equal(withoutOwnShares(text),'https://pan.quark.cn/s/other 提取码: abcd')
 state.username='another-admin';assert.ok(withoutOwnShares(text).includes('https://115.com/s/own'))
})
