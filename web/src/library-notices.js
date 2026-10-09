export function episodeRanges(values = []) {
  const numbers = [...new Set(values.filter(n => Number.isInteger(n) && n >= 0))].sort((a, b) => a - b)
  const ranges = []
  for (let i = 0; i < numbers.length; i++) {
    const start = numbers[i]
    let end = start
    while (numbers[i + 1] === end + 1) end = numbers[++i]
    ranges.push(start === end ? `${start}` : `${start}-${end}`)
  }
  return ranges.join('、')
}
export function libraryNoticeText(notice) {
  if (notice.mediaType === 'episode') {
    const season = Number.isInteger(notice.season) ? notice.season === 0 ? '特别篇' : `第${notice.season}季` : '季数未知'
    const episodes = episodeRanges(notice.episodes)
    return `电视剧 · ${notice.series || notice.name} · ${season} · ${episodes ? `${episodes}集` : '集数未知'}`
  }
  return `${notice.mediaType === 'movie' ? '电影 · ' : ''}${notice.name}`
}

export function embyNoticeDisplay(notice) {
  const scheduled = notice.event === 'scheduledtasks.completed'
  const legacy = scheduled ? /^(.*?)\s+上\s+(.+?)\s+已完成$/.exec(notice.name || '') : null
  const name = notice.libraryName || notice.serverName || legacy?.[1] || 'Emby'
  if (scheduled) return { name, message: `${notice.taskName || legacy?.[2] || notice.name || '计划任务'} 已完成` }
  const labels = { 'playback.start': '开始播放', 'playback.stop': '停止播放', 'playback.pause': '暂停播放', 'playback.unpause': '继续播放', 'library.new': '入库', 'system.notificationtest': '测试通知' }
  if (notice.event?.startsWith('playback.')) {
    const title = notice.series || notice.name || ''
    const episodes = notice.mediaType === 'episode' && Number.isInteger(notice.season) ? ` ${notice.episodes?.map(e => `S${String(notice.season).padStart(2,'0')}E${String(e).padStart(2,'0')}`).join('、') || ''}` : ''
    return { name, message: `${notice.userName ? `${notice.userName} ` : ''}${labels[notice.event] || notice.event} · ${title}${episodes}` }
  }
  return { name, message: `${labels[notice.event || 'library.new'] || notice.event} · ${libraryNoticeText(notice)}` }
}
