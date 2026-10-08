export function recentNotices(items, now = Date.now()) {
  const sorted = items.filter(i => Number.isFinite(Date.parse(i.lastRun))).sort((a,b) => Date.parse(b.lastRun)-Date.parse(a.lastRun))
  const recent = sorted.filter(i => now-Date.parse(i.lastRun) <= 24*60*60*1000)
  return recent.length ? recent : sorted.slice(0,5)
}
export function noticeResult(task) {
  const label = {success:'完成', error:'失败', cancelled:'已取消', interrupted:'已中断'}[task.status] || '进行中'
  const count = Number.isFinite(task.processed) ? ` · 已处理 ${task.processed} ${task.kind === 'cache' ? '个目录' : '项'}` : ''
  return `${label}${count}${task.message ? ` · ${task.message}` : ''}`
}
