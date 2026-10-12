const urls = /https?:\/\/[^\s<>"\]\[）)，。]+/g
const passwordLabel = /(?:提取码|访问码|密码)\s*[:：]?\s*([a-zA-Z0-9]{4,8})(?![a-zA-Z0-9])/g
const passwordKeys = ['password', 'pwd', 'passwd', 'passcode', 'receive_code']
const providerOrder = ['115', 'mobile', 'quark']

function providerOf(url) {
  if (url.username || url.password || url.port || url.protocol !== 'https:') return ''
  const host = url.hostname.toLowerCase()
  if (['115.com', '115cdn.com', 'anxia.com'].includes(host) && /^\/s\/[a-z\d_-]+\/?$/i.test(url.pathname)) return '115'
  if (host === 'pan.quark.cn' && /^\/s\/[a-z\d_-]+\/?$/i.test(url.pathname)) return 'quark'
  if (['yun.139.com', 'caiyun.139.com'].includes(host) && (
    url.pathname === '/shareweb/' && /^#\/w\/[ir]\/[a-z\d_-]+\/?(?:\?.*)?$/i.test(url.hash) ||
    /^\/shareweb\/[a-z\d_-]+\/?$/i.test(url.pathname) ||
    ['/shareweb/', '/m/i'].includes(url.pathname) && (url.searchParams.has('linkID') || url.searchParams.has('linkId') || url.pathname === '/m/i' && /^[a-z\d_-]+$/i.test(url.search.slice(1)))
  )) return 'mobile'
  return ''
}

export function shareEntries(text) {
  if (typeof text !== 'string' || text.length > 200000) return []
  const matches = [...text.matchAll(urls)]
  const entries = []
  const seen = new Map()
  for (let i = 0; i < matches.length; i++) {
    const raw = matches[i][0]
    let url
    try { url = new URL(raw) } catch { continue }
    const provider = providerOf(url)
    // A following password belongs only to this URL, never to the next share.
    const tail = text.slice(matches[i].index + raw.length, matches[i + 1]?.index ?? text.length)
    const passwords = [...tail.matchAll(passwordLabel)].map(match => match[1])
    const fragmentQuery = new URLSearchParams(url.hash.includes('?') ? url.hash.slice(url.hash.indexOf('?') + 1) : '')
    for (const key of passwordKeys) passwords.unshift(...fragmentQuery.getAll(key).filter(Boolean), ...url.searchParams.getAll(key).filter(Boolean))
    const unique = [...new Set(passwords)]
    const conflict = unique.length > 1
    const password = unique[0] || ''
    const queryPasswords = passwordKeys.map(key => url.searchParams.get(key)).filter(Boolean)
    const link = raw + unique.filter(value => !queryPasswords.includes(value)).map(value => ` 提取码: ${value}`).join('')
    // Merge duplicate shares, including Markdown's repeated label and target URL.
    const previous = seen.get(raw)
    if (previous) {
      previous.passwords = [...new Set([...previous.passwords, ...unique])]
      previous.conflict ||= conflict || previous.passwords.length > 1
      previous.password = previous.passwords[0] || ''
      previous.link = raw + previous.passwords.filter(value => !queryPasswords.includes(value)).map(value => ` 提取码: ${value}`).join('')
      continue
    }
    const entry = { provider, url: raw, password, passwords: unique, conflict, link }
    entries.push(entry)
    seen.set(raw, entry)
  }
  return entries
}

export function shareInput(text) {
  const entries = shareEntries(text)
  return {
    lines: entries.length ? [...new Set(entries.map(entry => entry.link))] : [...new Set(text.split(/\r?\n/).map(line => line.trim()).filter(Boolean))],
    conflict: entries.some(entry => entry.conflict)
  }
}

export function clipboardShareBatches(text) {
  const entries = shareEntries(text).filter(entry => entry.provider && entry.url.length <= 4096)
  const batches = []
  for (const provider of providerOrder) {
    const matching = entries.filter(entry => entry.provider === provider)
    if (!matching.length) continue
    // Keep conflicting passwords visible for correction instead of silently choosing one.
    const content = [...new Set(matching.map(entry => entry.link))]
    for (let start = 0; start < content.length; start += 50) batches.push({ provider, links: content.slice(start, start + 50).join('\n') })
  }
  return batches
}

export function shareHighlight(text) {
  const ranges = []
  for (const match of text.matchAll(passwordLabel)) {
    ranges.push({ start: match.index, end: match.index + match[0].length })
  }
  for (const match of text.matchAll(/(?:[?&])(?:password|pwd|passwd|passcode|receive_code)=([^\s&#]+)/g)) {
    ranges.push({ start: match.index + 1, end: match.index + match[0].length })
  }
  const parts = []
  let offset = 0
  for (const range of ranges.sort((a, b) => a.start - b.start)) {
    if (range.start < offset) continue
    if (range.start > offset) parts.push({ text: text.slice(offset, range.start), password: false })
    parts.push({ text: text.slice(range.start, range.end), password: true })
    offset = range.end
  }
  parts.push({ text: text.slice(offset) + '\n', password: false })
  return parts
}
