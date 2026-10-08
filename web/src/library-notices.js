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
