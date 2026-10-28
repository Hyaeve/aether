export const archiveStatuses = {
  success: '成功', doubt: '存疑', failed: '失败', error: '失败',
  running: '执行中', cancelled: '已取消', interrupted: '已中断', 'health-paused': '健康暂停', idle: '等待执行'
}
export const archiveTriggers = { manual:'手动操作', monitor:'监控执行', scheduled:'计划执行', automation:'联动执行' }
export const archiveKinds = { strm:'STRM', cas:'CAS', ed2k:'ED2K', cache:'缓存', organize:'整理', automation:'联动' }
export function archiveStatusClass(status) {
  return ['error','cancelled','interrupted'].includes(status) ? 'failed' : ['running','health-paused','idle'].includes(status) ? 'doubt' : status
}
