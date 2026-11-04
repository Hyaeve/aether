import {storageMembership} from './storage-membership.mjs'

// Redacted display metadata only. Never reused for server-side CAS entitlement.
const entries=new Map()
let owner=''
const revision=s=>JSON.stringify([s.id,s.type,s.enabled,s.viewRevision||s.config])
export function clearMembershipCache(){entries.clear();owner=''}
export function rememberMembership(username,storage,value){
  if(owner!==username){clearMembershipCache();owner=username}
  const stamp=revision(storage),checkedAt=value?Date.parse(value.checkedAt):Date.now(),old=entries.get(storage.id)
  if(old?.revision===stamp&&old.checkedAt>checkedAt)return
  entries.set(storage.id,{revision:stamp,value,checkedAt})
}
export function mergeMemberships(username,storages){
  if(owner!==username){clearMembershipCache();owner=username}
  const ids=new Set(storages.map(s=>s.id))
  for(const id of entries.keys())if(!ids.has(id))entries.delete(id)
  for(const s of storages){
    let cached=entries.get(s.id)
    if(cached&&(cached.revision!==revision(s)||!s.enabled||cached.value&&!storageMembership(s.type,cached.value))){entries.delete(s.id);cached=null}
    const incoming=s.usage?.membership
    if(s.enabled&&storageMembership(s.type,incoming)&&(!cached||Date.parse(incoming.checkedAt)>cached.checkedAt)){
      rememberMembership(username,s,incoming);cached=entries.get(s.id)
    }
    // Vue may wrap an identical value in a reactive proxy. Compare scalar
    // metadata rather than identity so a usage watcher cannot trigger itself.
    if(cached&&JSON.stringify(s.usage?.membership??null)!==JSON.stringify(cached.value))s.usage={...s.usage,membership:cached.value}
  }
  return storages
}
