import {test} from 'node:test'
import assert from 'node:assert/strict'
import {isConfigRemoval,particleBudget,prepareConfigRemoval} from '../src/config-card-removal.js'

test('configuration deletes match exact resources rather than prefixes or file operations',()=>{
 const card={configPath:'/storages/pool'}
 assert.equal(isConfigRemoval(card,'/storages/pool','DELETE'),true)
 assert.equal(isConfigRemoval(card,'/api/storages/pool?confirm=1','DELETE'),true)
 for(const [url,method] of [['/storages/pool2','DELETE'],['/storages/pool/test','POST'],['/storages/pool','PUT'],['/files/action','POST'],['/transfers','DELETE']])assert.equal(isConfigRemoval(card,url,method,{}),false)
 const live={configPath:'/share-live/a',configActions:'remove delete'}
 assert.equal(isConfigRemoval(live,'/share-live/a/remove','POST'),true)
 assert.equal(isConfigRemoval(live,'/share-live/a/delete','POST'),true)
 assert.equal(isConfigRemoval(live,'/share-live/a/run','POST'),false)
 assert.equal(isConfigRemoval(live,'/files/action/delete','POST'),false)
 assert.equal(isConfigRemoval({configPath:'/files/action'},'/files/action/delete','POST'),false)
})
test('list updates animate only an explicitly confirmed removed entry, never toggles edits or reorder',()=>{
 const card={configCollection:'/115-signin',configList:'rules',configKey:'storageId',configId:'pool',configDeleting:'true'}
 assert.equal(isConfigRemoval(card,'/115-signin','PUT',{rules:[{storageId:'other'}]}),true)
 assert.equal(isConfigRemoval(card,'/115-signin','PUT',{rules:[{storageId:'pool',enabled:false}]}),false)
 assert.equal(isConfigRemoval({...card,configDeleting:'false'},'/115-signin','PUT',{rules:[]}),false)
 assert.equal(isConfigRemoval(card,'/115-signin','PUT',{}),false)
 const keyed={configCollection:'/115-simulcast',configList:'',configId:'pool',configDeleting:'true'}
 assert.equal(isConfigRemoval(keyed,'/115-simulcast','PUT',{other:{enabled:true}}),true)
 assert.equal(isConfigRemoval(keyed,'/115-simulcast','PUT',{pool:{enabled:false}}),false)
 assert.equal(isConfigRemoval(card,'/115-folder-cleanup','PUT',{rules:[]}),false)
})
test('particle counts and canvas pixels stay bounded even for large retina cards',()=>{
 for(const [w,h,dpr] of [[100,80,1],[500,150,3],[3840,2160,4],[14400,8000,8]]){
  const budget=particleBudget(w,h,dpr)
  assert.ok(budget.count>=90&&budget.count<=640)
  assert.ok(budget.scale<=1.5)
  assert.ok(budget.width*budget.height<=600000)
 }
 assert.doesNotThrow(()=>prepareConfigRemoval('/storages/a','DELETE')())
})
