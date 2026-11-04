import {test} from 'node:test'
import assert from 'node:assert/strict'
import {queueRecognitionSave,waitForRecognitionSave} from '../src/recognition-autosave.js'

test('recognition saves keep immutable snapshots and wait for the final queued write',async()=>{
 const calls=[],value={blacklist:['first']};let release
 const first=queueRecognitionSave('owner',value,async body=>{calls.push(body);await new Promise(resolve=>release=resolve)})
 value.blacklist[0]='mutated'
 const last=queueRecognitionSave('owner',{blacklist:['last']},async body=>calls.push(body))
 await new Promise(resolve=>setImmediate(resolve))
 assert.deepEqual(calls,[{blacklist:['first']}])
 let settled=false;const barrier=waitForRecognitionSave('owner').then(()=>settled=true)
 assert.equal(settled,false);release();await Promise.all([first,last,barrier])
 assert.deepEqual(calls,[{blacklist:['first']},{blacklist:['last']}]);assert.equal(settled,true)
})

test('failed recognition writes do not block a retry or another account',async()=>{
 const failed=queueRecognitionSave('old',{minSizeMB:1},async()=>{throw Error('offline')})
 await assert.rejects(failed,/offline/);await waitForRecognitionSave('old')
 let release;const slow=queueRecognitionSave('old',{},async()=>new Promise(resolve=>release=resolve))
 const result=await queueRecognitionSave('new',{minSizeMB:2},async body=>body.minSizeMB)
 assert.equal(result,2);release();await slow
 assert.equal(await queueRecognitionSave('old',{minSizeMB:3},async body=>body.minSizeMB),3)
})
