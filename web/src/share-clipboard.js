export function clipboardShares(text) {
  if (typeof text !== 'string' || text.length > 200000) return null
  const found = []
  for (const raw of text.match(/https?:\/\/[^\s<>"\]\[）)]+/g) || []) {
    try {
      const url = new URL(raw), host = url.hostname.toLowerCase()
      if (url.username || url.password || url.port || url.protocol !== 'https:') continue
      let provider = ''
      if (['115.com', '115cdn.com', 'anxia.com'].includes(host) && /^\/s\/[a-z\d]+\/?$/i.test(url.pathname)) provider = '115'
      if (host === 'pan.quark.cn' && /^\/s\/[a-z\d]+\/?$/i.test(url.pathname)) provider = 'quark'
      if (host === 'yun.139.com' && url.pathname === '/shareweb/' && /^#\/w\/i\/[a-z\d]+\/?$/i.test(url.hash)) provider = 'mobile'
      if (provider && url.href.length <= 4096) found.push({ provider, url: url.href })
    } catch { /* Ignore non-URL clipboard text. */ }
  }
  if (!found.length) return null
  const provider = found[0].provider
  const links = [...new Set(found.filter(item => item.provider === provider).map(item => item.url))].slice(0, 50)
  return { provider, links: links.join('\n') }
}

export function watchShareClipboard({ ready, open, clipboard = navigator.clipboard, permissions = navigator.permissions, interval = 3000 }) {
  let stopped = false, reading = false, prompted = false, generation = 0
  const seen = new Set()
  const editing = 'input, textarea, [contenteditable]:not([contenteditable="false"])'
  const available = () => !stopped && ready() && document.visibilityState === 'visible' && document.hasFocus() && !document.querySelector('.modal, .video-viewer, .image-viewer') && !document.activeElement?.closest?.(editing)
  function accept(text) {
    if (!available()) return
    const share = clipboardShares(text)
    if (share && !seen.has(share.links) && open(share) !== false) {
      seen.add(share.links)
      if (seen.size > 100) seen.delete(seen.values().next().value)
    }
  }
  async function read(gesture = false) {
    if (!clipboard?.readText || reading || !available()) return
    reading = true
    const run = generation
    try {
      let granted = false
      try { granted = (await permissions?.query({ name: 'clipboard-read' }))?.state === 'granted' } catch {}
      // Poll only with existing permission; try prompting at most once per session.
      if (!granted && (!gesture || prompted)) return
      if (!granted) prompted = true
      const text = await clipboard.readText()
      if (run === generation) accept(text)
    } catch { /* Permission denial or insecure HTTP leaves manual paste available. */ }
    finally { reading = false }
  }
  const paste = event => {
    if (!event.isTrusted || event.target?.closest?.(editing)) return
    accept(event.clipboardData?.getData('text/plain'))
  }
  const gesture = event => { if (event.isTrusted) read(true) }
  const focus = () => read()
  document.addEventListener('paste', paste)
  document.addEventListener('pointerup', gesture)
  window.addEventListener('focus', focus)
  const timer = setInterval(read, interval)
  read()
  return () => { stopped = true; generation++; seen.clear(); clearInterval(timer); document.removeEventListener('paste', paste); document.removeEventListener('pointerup', gesture); window.removeEventListener('focus', focus) }
}
