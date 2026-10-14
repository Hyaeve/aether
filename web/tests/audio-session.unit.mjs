import {test} from 'node:test'
import assert from 'node:assert/strict'
import {audioSession,openAudio,removeAudio,closeAudio} from '../src/audio-session.js'

test('clicked songs accumulate across storage and removal advances without reintroducing removed songs',()=>{
 closeAudio()
 const a={id:'same',storage:'a',name:'A'},b={id:'same',storage:'b',name:'B'},c={id:'c',storage:'a',name:'C'}
 openAudio(a);openAudio(b);openAudio(c);openAudio({...b,name:'B updated'})
 assert.equal(audioSession.queue.length,3);assert.equal(audioSession.file.name,'B updated')
 removeAudio(a);assert.equal(audioSession.file.name,'B updated')
 removeAudio(b);assert.equal(audioSession.file.name,'C');assert.equal(audioSession.queue.length,1)
 removeAudio(c);assert.equal(audioSession.file,null);assert.equal(audioSession.queue.length,0)
 openAudio(a);openAudio(b);removeAudio(b);assert.equal(audioSession.file.name,'A')
 closeAudio()
})
