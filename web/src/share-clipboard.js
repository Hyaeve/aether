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

export function watchShareClipboard({ ready, open, blocked = () => {}, clipboard = navigator.clipboard, permissions = navigator.permissions, interval = 1500 }) {
  let stopped = false, reading = false, prompted = false, generation = 0, readable = false, previous = '', warned = false
  const editing = 'input, textarea, [contenteditable]:not([contenteditable="false"])'
  const available = () => !stopped && ready() && document.visibilityState === 'visible' && document.hasFocus() && !document.querySelector('.modal, .video-viewer, .image-viewer') && !document.activeElement?.closest?.(editing)
  function accept(text, explicit = false) {
    if (!available()) return
    const share = clipboardShares(text)
    if (!share) { previous = ''; return }
    if ((explicit || share.links !== previous) && open(share) !== false) {
      previous = share.links
    }
  }
  function unavailable(reason) { if (!warned) { warned = true; blocked(reason) } }
  async function read(gesture = false, explicit = false) {
    if (!clipboard?.readText) { if (explicit) unavailable('insecure'); return }
    if (reading || !available()) return
    reading = true
    const run = generation
    try {
      let granted = readable
      try { granted = (await permissions?.query({ name: 'clipboard-read' }))?.state === 'granted' || readable } catch {}
      // Poll only with existing permission; try prompting at most once per session.
      if (!granted && (!gesture || prompted && !explicit)) return
      if (!granted) prompted = true
      const text = await clipboard.readText()
      readable = true
      if (run === generation) accept(text, explicit)
    } catch { if (explicit && run === generation) unavailable(window.isSecureContext ? 'permission' : 'insecure') }
    finally { reading = false }
  }
  const paste = event => {
    if (!event.isTrusted || event.target?.closest?.(editing)) return
    accept(event.clipboardData?.getData('text/plain'), true)
  }
  const gesture = event => { if (event.isTrusted) read(true) }
  const focus = () => read()
  const visibility = () => { if (document.visibilityState === 'visible') read() }
  const activate = () => read(true, true)
  document.addEventListener('paste', paste)
  document.addEventListener('pointerup', gesture)
  window.addEventListener('focus', focus)
  document.addEventListener('visibilitychange', visibility)
  window.addEventListener('aether:listen-shares', activate)
  const timer = setInterval(read, interval)
  read()
  return () => { stopped = true; generation++; clearInterval(timer); document.removeEventListener('paste', paste); document.removeEventListener('pointerup', gesture); window.removeEventListener('focus', focus); document.removeEventListener('visibilitychange', visibility); window.removeEventListener('aether:listen-shares', activate) }
}
