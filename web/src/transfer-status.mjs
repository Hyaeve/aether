export const transferStates = [
  { value:'running', label:'进行', icon:'ArrowLeftRight', tone:'blue' },
  { value:'preparing', label:'预处理', icon:'Settings2', tone:'blue' },
  { value:'waiting', label:'等待', icon:'Clock3', tone:'blue' },
  { value:'queued', label:'排队', icon:'ListOrdered', tone:'orange' },
  { value:'paused', label:'暂停', icon:'Pause', tone:'orange' },
  { value:'completed', label:'成功', icon:'CircleCheck', tone:'green' },
  { value:'skipped', label:'跳过', icon:'SkipForward', tone:'green' },
  { value:'ignored', label:'忽略', icon:'FileX2', tone:'green' },
  { value:'failed', label:'失败', icon:'CircleAlert', tone:'red' },
  { value:'error', label:'错误', icon:'Info', tone:'red' },
]
export const transferState = item => transferStates.find(s=>s.value===item.status) || transferStates.at(-1)
export const transferPercent = item => item.total > 0 ? Math.min(100, Math.max(0, Math.floor((Number(item.done)||0)/item.total*10000)/100)) : null
export function transferSummary(items) {
  const total=items.reduce((n,i)=>n+Math.max(0,Number(i.total)||0),0)
  const done=items.reduce((n,i)=>n+Math.max(0,i.total>0?Math.min(i.total,Number(i.done)||0):Number(i.done)||0),0)
  const known=items.length>0 && items.every(i=>i.total>0 || ['completed','skipped','ignored'].includes(i.status))
  return { count:items.length, total, done, percent:known?transferPercent({total,done}):null }
}
export function canControlTransfer(item, action) {
  if(action==='retry') return item.status==='failed' && item.controls?.retry===true
  if(action==='delete') return item.controls?.delete ?? !(item.kind==='copy' && ['running','preparing','waiting','queued','paused'].includes(item.status))
  const pausable=item.controls?.pause ?? item.kind!=='copy'
  return pausable && (action==='pause'?['running','preparing','waiting','queued'].includes(item.status):item.status==='paused')
}
