const tiers = {
  '115':{vip:['VIP','Crown'],annual:['年费VIP','Crown'],permanent:['永久VIP','BadgeCheck']},
  mobile:{silver:['白银会员','ShieldCheck'],gold:['黄金会员','Crown'],diamond:['钻石会员','Diamond']},
  tianyi:{gold:['黄金会员','Crown'],platinum:['铂金会员','Gem']},
}
export function storageMembership(type,value) {
  const tier=tiers[type]?.[value?.tier]
  if(!tier) return null
  const checked=Date.parse(value.checkedAt)
  if(!Number.isFinite(checked)||Date.now()-checked>5*60*1000) return null
  if(value.expiresAt && (!Number.isFinite(Date.parse(value.expiresAt))||Date.parse(value.expiresAt)<=Date.now()))return null
  return { tier:value.tier,label:tier[0],icon:tier[1] }
}
