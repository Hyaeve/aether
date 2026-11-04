import {test} from 'node:test'
import assert from 'node:assert/strict'
import { storageMembership,freshStorageMembership } from '../src/storage-membership.mjs'
import {clearMembershipCache,rememberMembership,mergeMemberships} from '../src/storage-membership-cache.mjs'
import {reactive,watch,nextTick} from 'vue'
test('membership badges require an explicit supported fresh server value',()=>{
  const value={tier:'diamond',checkedAt:new Date().toISOString()}
  assert.equal(storageMembership('mobile',value).label,'钻石会员')
  assert.equal(storageMembership('115',value),null)
  assert.equal(storageMembership('mobile',{...value,checkedAt:'2020-01-01'}),null)
  assert.equal(storageMembership('mobile',{...value,expiresAt:'2020-01-01'}),null)
  assert.equal(storageMembership('mobile',{...value,expiresAt:'unrecognized-date'}),null)
  assert.equal(storageMembership('mobile',{total:5000000000000}),null)
  for(const [tier,label] of [['svip','SVIP'],['88vip','88VIP'],['svip-plus','SVIP+'],['vip','VIP'],['mini','迷你VIP']]){
    assert.equal(storageMembership('quark',{...value,tier}).label,label)
    assert.equal(storageMembership('quark',{...value,tier,checkedAt:'2020-01-01'}),null)
  }
  assert.equal(storageMembership('quark',{...value,tier:'unrecognized'}),null)
})

test('last known badges stay visible during refresh, without extending entitlement freshness',()=>{
 const value={tier:'platinum',checkedAt:new Date(Date.now()-6*60*1000).toISOString()}
 assert.equal(storageMembership('tianyi',value).label,'铂金会员')
 assert.equal(freshStorageMembership('tianyi',value),null)
 assert.equal(storageMembership('tianyi',{...value,checkedAt:new Date(Date.now()-25*60*60*1000).toISOString()}),null)
 assert.equal(storageMembership('tianyi',{...value,expiresAt:new Date(Date.now()-1).toISOString()}),null)
})

test('badge cache survives old state snapshots and isolates credentials, sessions and removed pools',()=>{
 clearMembershipCache()
 const s={id:'p',type:'mobile',enabled:true,viewRevision:'rev1',config:{authorization:'********'}}
 const membership={tier:'diamond',checkedAt:new Date().toISOString()}
 rememberMembership('owner',s,membership)
 const old=()=>[{...s,usage:{total:100}}]
 assert.equal(mergeMemberships('owner',old())[0].usage.membership.tier,'diamond')
 assert.equal(mergeMemberships('owner',old())[0].usage.total,100)
 assert.equal(mergeMemberships('owner',[{...s,viewRevision:'rev2'}])[0].usage,undefined)
 rememberMembership('owner',s,membership)
 assert.equal(mergeMemberships('another',old())[0].usage.membership,undefined)
 rememberMembership('owner',s,membership);clearMembershipCache()
 assert.equal(mergeMemberships('owner',old())[0].usage.membership,undefined)
 rememberMembership('owner',s,membership);mergeMemberships('owner',[])
 assert.equal(mergeMemberships('owner',old())[0].usage.membership,undefined)
 rememberMembership('owner',s,membership);rememberMembership('owner',s,null)
 assert.equal(mergeMemberships('owner',[{...s,usage:{membership}}])[0].usage.membership,null)
})

test('merging reactive state metadata settles without a self-triggering usage watcher',async()=>{
 clearMembershipCache()
 const pool={id:'reactive',type:'tianyi',enabled:true,viewRevision:'1'}
 const value={tier:'gold',checkedAt:new Date().toISOString()}
 rememberMembership('owner',pool,value)
 const state=reactive({storages:[pool]})
 let updates=0
 const stop=watch(()=>state.storages.map(s=>s.usage),()=>{updates++;mergeMemberships('owner',state.storages)},{deep:true})
 mergeMemberships('owner',state.storages)
 await nextTick();await nextTick()
 assert.equal(state.storages[0].usage.membership.tier,'gold')
 assert.equal(updates,1)
 stop();clearMembershipCache()
})
