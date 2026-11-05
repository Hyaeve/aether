import test from 'node:test'
import assert from 'node:assert/strict'
import {readFileSync} from 'node:fs'

test('backend Docker context includes the embedded current release notes package',()=>{
 const read=name=>readFileSync(new URL('../'+name,import.meta.url),'utf8'),version=read('VERSION').trim(),docker=read('Dockerfile')
 const backend=docker.split(/^FROM /m).find(stage=>stage.startsWith('golang:'))
 assert.ok(backend,'Backend stage is required')
 const copies=backend.slice(0,backend.indexOf('go build')).split(/\r?\n/).filter(line=>line.startsWith('COPY ')).flatMap(line=>line.trim().split(/\s+/).slice(1,-1))
 const covered=file=>copies.some(source=>source==='.'||file===source||file.startsWith(source.replace(/\/$/,'')+'/')||new RegExp('^'+source.split('*').map(p=>p.replace(/[.*+?^${}()|[\]\\]/g,'\\$&')).join('[^/]*')+'$').test(file))
 for(const file of ['docs/release_notes.go',`docs/releases/v${version}.md`])assert.ok(covered(file),'Missing backend build input '+file)
 assert.match(read(`docs/releases/v${version}.md`),new RegExp('^# v'+version.replaceAll('.','\\.')))
})
