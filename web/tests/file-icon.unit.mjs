import { test } from 'node:test'
import assert from 'node:assert/strict'
import { entryIcon } from '../src/file-icon.js'

test('rename and file views share every supported media and document icon', () => {
  assert.equal(entryIcon({name:'song.flac',isDir:true}), 'Folder')
  for(const extension of ['mp3','flac','wav','m4a','m4b','aac','ogg','oga','opus','wma','aiff','aif','alac'])assert.equal(entryIcon({name:'sample.'+extension.toUpperCase()}),'FileAudio2')
  for(const extension of ['mp4','m4v','mkv','avi','mov','webm','ts'])assert.equal(entryIcon({name:'sample.'+extension}),'FileVideo')
  for(const extension of ['zip','7z','rar','tar','tar.gz','tar.bz2','tar.xz','tgz','tbz2','txz'])assert.equal(entryIcon({name:'sample.'+extension}),'FileArchive')
  assert.equal(entryIcon({name:'stream.strm'}),'FileSymlink')
  assert.equal(entryIcon({name:'metadata.nfo'}),'FileText')
  assert.equal(entryIcon({name:'feature.cas'}),'FileJson')
})
