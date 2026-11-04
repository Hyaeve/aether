const tiers = {
  '115':{vip:['VIP','Crown'],annual:['年费VIP','Crown'],permanent:['永久VIP','BadgeCheck']},
  mobile:{silver:['白银会员','ShieldCheck'],gold:['黄金会员','Crown'],diamond:['钻石会员','Diamond']},
  tianyi:{gold:['黄金会员','Crown'],platinum:['铂金会员','Gem']},
  quark:{vip:['VIP','Crown'],svip:['SVIP','Crown'],'svip-plus':['SVIP+','Gem'],'88vip':['88VIP','BadgeCheck'],mini:['迷你VIP','ShieldCheck']},
}
export function storageMembership(type,value,maxAge=24*60*60*1000) {
  const tier=tiers[type]?.[value?.tier]
  if(!tier) return null
  const checked=Date.parse(value.checkedAt)
  if(!Number.isFinite(checked)||checked>Date.now()+60000||Date.now()-checked>maxAge) return null
  if(value.expiresAt && (!Number.isFinite(Date.parse(value.expiresAt))||Date.parse(value.expiresAt)<=Date.now()))return null
  return { tier:value.tier,label:tier[0],icon:tier[1],checkedAt:value.checkedAt }
}
export const freshStorageMembership=(type,value)=>storageMembership(type,value,5*60*1000)
